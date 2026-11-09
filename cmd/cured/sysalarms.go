package main

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
)

// Codes of the conditions only this process can see. core has no way to know the
// panel is missing or the kernel's safety parameter is wrong.
const (
	CodeDisplayMissing = "DISPLAY_MISSING"
	CodeDisplayUnknown = "DISPLAY_UNKNOWN"
	CodePanelRecovered = "PANEL_RECOVERED_AT_BOOT"
	CodePanelStuck     = "PANEL_NOT_UPDATING"
	CodeSafeState      = "UNSAFE_SYSTEM_STATE"
	CodeRestartLoop    = "CONTROLLER_RESTART_LOOP"
	CodeLinesFault     = "ALARM_LINE_FAULT"
	CodeStorage        = "STATE_STORAGE_FAILED"
)

const (
	sysIDPrefix   = "sys-"
	sysSilenceFor = 30 * time.Minute // as core's silence
	sysKeepHist   = 20
	// sysEventCall is how long a past event calls, as core's events do (ADR 0108).
	sysEventCall = 30 * time.Minute
)

// sysAlarms is the table of those conditions.
type sysAlarms struct {
	mu        sync.Mutex
	now       func() time.Time
	wallOK    func() bool
	active    map[string]*api.Alarm
	hist      []api.Alarm
	seq       int
	silenceAt time.Time
	silenced  bool
	logf      func(string, ...any)
	// event marks codes that report something that happened, not a standing fault.
	// They call for sysEventCall or until acknowledged, and an acknowledgement clears
	// them, so an old one never holds the contact for the rest of a cure (ADR 0408).
	// The call is timed in real time: these are events of the process, and in a
	// simulation half an hour of simulated time passes before the contact even arms.
	event     map[string]bool
	eventReal map[string]time.Time
	realNow   func() time.Time // time.Now; a test may replace it
	// record puts raises and clears on core's journal once core exists; until then
	// they wait in pending.
	record  func(code, sev, msg string)
	pending [][3]string
	// path keeps the event alarms and the history across a restart or power cut;
	// conditions are found again by the new process (integration defect 6, re-test).
	// w writes it off the caller's goroutine: the control loop takes mu every tick, so
	// mu is never held while waiting for the disk.
	path string
	w    *latestWriter
	// restored lists the events an earlier process kept: already signalled, so not news
	// to this process's annunciator.
	restored map[string]bool
}

type sysSaved struct {
	Events []api.Alarm          `json:"events"`
	Real   map[string]time.Time `json:"real"`
	Hist   []api.Alarm          `json:"hist"`
	Seq    int                  `json:"seq"`
}

// Persist loads what an earlier process kept at path and keeps it there from now on.
func (s *sysAlarms) Persist(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = path
	s.w = newLatestWriter(path, s.logf)
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var v sysSaved
	if err := json.Unmarshal(b, &v); err != nil {
		s.logf("<4>cured: system alarms file unreadable, starting empty: %v", err)
		return
	}
	for i := range v.Events {
		a := v.Events[i]
		if _, up := s.active[a.Code]; up {
			continue
		}
		s.active[a.Code] = &a
		s.event[a.Code] = true
		s.eventReal[a.Code] = v.Real[a.Code]
		if s.restored == nil {
			s.restored = map[string]bool{}
		}
		s.restored[a.Code] = true
	}
	s.hist = append(s.hist, v.Hist...)
	if v.Seq > s.seq {
		s.seq = v.Seq
	}
}

// save hands the event alarms and history to the writer. Caller holds mu; nothing
// here waits for the disk.
func (s *sysAlarms) save() {
	if s.w == nil {
		return
	}
	v := sysSaved{Real: map[string]time.Time{}, Hist: s.hist, Seq: s.seq}
	for code, a := range s.active {
		if s.event[code] {
			v.Events = append(v.Events, *a)
			v.Real[code] = s.eventReal[code]
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		s.logf("<4>cured: system alarms file: %v", err)
		return
	}
	s.w.Put(b)
}

// Restored says whether code was kept by an earlier process and loaded at start.
func (s *sysAlarms) Restored(code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restored[code]
}

// Flush waits up to d for the saved alarms to reach the disk.
func (s *sysAlarms) Flush(d time.Duration) bool {
	s.mu.Lock()
	w := s.w
	s.mu.Unlock()
	return w == nil || w.Flush(d)
}

// SetRecord connects the journal and writes what waited for it.
func (s *sysAlarms) SetRecord(f func(code, sev, msg string)) {
	s.mu.Lock()
	p := s.pending
	s.record, s.pending = f, nil
	s.mu.Unlock()
	for _, x := range p {
		f(x[0], x[1], x[2])
	}
}

// note records a raise or clear. Caller holds mu.
func (s *sysAlarms) note(code, sev, msg string) {
	if s.record != nil {
		f := s.record
		go f(code, sev, msg) // core's lock must not be taken under this one
		return
	}
	s.pending = append(s.pending, [3]string{code, sev, msg})
}

func newSysAlarms(now func() time.Time, wallOK func() bool, logf func(string, ...any)) *sysAlarms {
	return &sysAlarms{now: now, wallOK: wallOK, active: map[string]*api.Alarm{}, logf: logf, event: map[string]bool{}, eventReal: map[string]time.Time{}, realNow: time.Now}
}

// RaiseEvent is Raise for something that happened, such as a burst of restarts.
func (s *sysAlarms) RaiseEvent(code string, sev api.Severity, title, action string) {
	s.mu.Lock()
	s.event[code] = true
	if _, up := s.active[code]; !up {
		s.eventReal[code] = s.realNow()
	}
	s.mu.Unlock()
	s.Raise(code, sev, title, action)
}

// calls says whether one alarm calls the contact now. Caller holds mu.
func (s *sysAlarms) calls(a *api.Alarm) bool {
	if a.Severity != api.SevCritical {
		return false
	}
	if s.event[a.Code] {
		return !a.Acked && s.realNow().Sub(s.eventReal[a.Code]) < sysEventCall
	}
	return true
}

// CallingCodes lists the codes calling the contact now.
func (s *sysAlarms) CallingCodes() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for code, a := range s.active {
		if s.calls(a) {
			out[code] = true
		}
	}
	return out
}

// Raise makes the condition active. Repeating it only refreshes the text.
func (s *sysAlarms) Raise(code string, sev api.Severity, title, action string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.active[code]; ok {
		a.Title, a.Action, a.Severity = title, action, sev
		return
	}
	s.seq++
	s.active[code] = &api.Alarm{ID: sysIDPrefix + strings.ToLower(code) + "-" + strconv.Itoa(s.seq), Code: code, Severity: sev,
		Title: title, Action: action, Since: s.now(), ClockOK: s.wallOK()}
	s.silenced = false // something new must be heard
	s.logf("<2>cured: system alarm raised %s: %s", code, title)
	s.note(code, string(sev), "raised: "+title)
	s.save()
}

// SetPeak records the worst figure an active alarm has seen.
func (s *sysAlarms) SetPeak(code string, v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.active[code]; ok && v > a.Peak {
		a.Peak = v
		s.save()
	}
}

// Clear ends the condition.
func (s *sysAlarms) Clear(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.active[code]
	if !ok {
		return
	}
	delete(s.active, code)
	delete(s.event, code)
	delete(s.eventReal, code)
	s.note(code, string(a.Severity), "cleared")
	c := *a
	c.Cleared = s.now()
	s.hist = append([]api.Alarm{c}, s.hist...)
	if len(s.hist) > sysKeepHist {
		s.hist = s.hist[:sysKeepHist]
	}
	s.save()
	s.logf("<4>cured: system alarm cleared %s", code)
}

// Calling is true while any critical condition is active. An acknowledgement does
// not end it: the contact shows the fault for as long as the fault exists. Warnings
// are records on the panel and in history; they call no one.
func (s *sysAlarms) Calling() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.active {
		if s.calls(a) {
			return true
		}
	}
	return false
}

// Sounding is true while an unacknowledged condition should sound the buzzer.
func (s *sysAlarms) Sounding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.silenced && s.now().Sub(s.silenceAt) >= sysSilenceFor {
		s.silenced = false
	}
	if s.silenced {
		return false
	}
	for _, a := range s.active {
		if !a.Acked && s.calls(a) {
			return true
		}
	}
	return false
}

// Silenced reports a silence in force with something active to silence.
func (s *sysAlarms) Silenced() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.silenced && s.now().Sub(s.silenceAt) >= sysSilenceFor {
		s.silenced = false
	}
	return s.silenced && len(s.active) > 0
}

// Silence stops the buzzer for a while.
func (s *sysAlarms) Silence() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.silenced, s.silenceAt = true, s.now()
}

// Ack acknowledges one condition by ID, or all with an empty ID.
func (s *sysAlarms) Ack(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	var done []string
	for code, a := range s.active {
		if id == "" || a.ID == id {
			a.Acked = true
			found = true
			if s.event[code] {
				done = append(done, code)
			}
		}
	}
	s.mu.Unlock()
	for _, code := range done {
		s.Clear(code) // an acknowledged event has been seen; it moves to history
	}
	s.mu.Lock()
	return found
}

// Active lists the conditions, oldest first.
func (s *sysAlarms) Active() []api.Alarm {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]api.Alarm, 0, len(s.active))
	for code, a := range s.active {
		x := *a
		x.Event = s.event[code]
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// History lists cleared conditions, newest first.
func (s *sysAlarms) History() []api.Alarm {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]api.Alarm(nil), s.hist...)
}

// Codes lists the active codes.
func (s *sysAlarms) Codes() []string {
	a := s.Active()
	out := make([]string, 0, len(a))
	for _, x := range a {
		out = append(out, x.Code)
	}
	return out
}

// view is the api.Controller the front panel talks to: the core, with this
// process's own conditions merged in, so they are shown like any other alarm.
type view struct {
	c   *core.Core
	sys *sysAlarms
}

// Snapshot implements api.Controller.
func (v *view) Snapshot() api.Snapshot {
	s := v.c.Snapshot()
	sa := v.sys.Active()
	if len(sa) > 0 {
		s.Active = append(append(make([]api.Alarm, 0, len(sa)+len(s.Active)), sa...), s.Active...)
		s.BuzzerSilenced = s.BuzzerSilenced || v.sys.Silenced()
	}
	if h := v.sys.History(); len(h) > 0 {
		s.History = append(append(make([]api.Alarm, 0, len(h)+len(s.History)), h...), s.History...)
	}
	return s
}

// Do implements api.Controller.
func (v *view) Do(c api.Command) error {
	switch c.Kind {
	case api.CmdSilence:
		v.sys.Silence()
	case api.CmdAck:
		if strings.HasPrefix(c.AlarmID, sysIDPrefix) {
			if !v.sys.Ack(c.AlarmID) {
				return errors.New("that alarm has already cleared")
			}
			return nil
		}
		if c.AlarmID == "" {
			v.sys.Ack("")
		}
	}
	return v.c.Do(c)
}

// faultyUI is the panel's controller in --sim, so an injected panel fault reaches
// the panel's data path and nothing else.
type faultyUI struct {
	api.Controller
	hook func()
}

// Snapshot implements api.Controller.
func (f faultyUI) Snapshot() api.Snapshot {
	f.hook()
	return f.Controller.Snapshot()
}
