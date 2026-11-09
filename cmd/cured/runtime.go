package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
	"github.com/johnjezl/curing-cabinet-controller/internal/drivers"
	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui"
)

// floor is a date before this build. An RTC reading below it has lost its time.
var trustFloor = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// realClock is core's clock on the real machine. Trust comes from NTP having
// synchronised or from an RTC that kept its time; a Pi 5 with no battery comes back
// from a power cut with an RTC near zero, measured in evidence/b.
type realClock struct {
	start   time.Time
	trusted atomic.Bool
}

func (c *realClock) Now() time.Time      { return time.Now() }
func (c *realClock) Mono() time.Duration { return time.Since(c.start) }
func (c *realClock) Trusted() bool       { return c.trusted.Load() }

func clockSynced() bool {
	if statExists("/run/systemd/timesync/synchronized") {
		return true
	}
	var t syscall.Timex
	st, err := syscall.Adjtimex(&t)
	return err == nil && st != 5 && t.Status&0x40 == 0 // not TIME_ERROR, not STA_UNSYNC
}

func rtcGood() bool {
	b, err := os.ReadFile("/sys/class/rtc/rtc0/since_epoch")
	if err != nil {
		return false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return false
	}
	rtc := time.Unix(v, 0)
	d := time.Since(rtc)
	return rtc.After(trustFloor) && d < 5*time.Minute && d > -5*time.Minute
}

func (c *realClock) refresh() { c.trusted.Store(clockSynced() || rtcGood()) }

// runtime is what the real and simulated modes share.
type runtime struct {
	mode    string
	started time.Time
	upStart float64
	sys     *sysAlarms
	ann     *annunciator
	// openingsW saves the contact's opening count off the control loop.
	openingsW *latestWriter
	v         *view
	uiCtl     api.Controller // what the panel talks to; r.v unless a test wraps it
	c         *core.Core
	safeCfg   drivers.Config
	statusX   func() any
	// linesDriven is false in simulation: the contact and buzzer states in the
	// status are the simulated decision, and no real line follows them.
	linesDriven bool

	ticks      atomic.Uint64
	lastTick   atomic.Int64
	frames     atomic.Uint64
	lastFrame  atomic.Int64
	upFirstStp atomic.Uint64 // float bits as millis
	upReady    atomic.Uint64
	upFirstFrm atomic.Uint64
	panelBad   atomic.Bool
}

func millis(v float64) uint64 { return uint64(v*1000 + 0.5) }

func (r *runtime) noteFirstStep() {
	u := uptimeNow()
	r.upFirstStp.Store(millis(u))
	logfJ("<5>cured: timing control_first_step uptime=%.3f process_age=%.3f", u, time.Since(r.started).Seconds())
}

func (r *runtime) noteReady() {
	u := uptimeNow()
	r.upReady.Store(millis(u))
	logfJ("<5>cured: timing ready_sent uptime=%.3f process_age=%.3f", u, time.Since(r.started).Seconds())
}

func (r *runtime) onFrame(fi ui.FrameInfo) {
	if fi.Err != nil {
		return
	}
	r.frames.Add(1)
	r.lastFrame.Store(time.Now().UnixNano())
	if r.upFirstFrm.Load() == 0 {
		u := uptimeNow()
		r.upFirstFrm.Store(millis(u))
		logfJ("<5>cured: timing first_panel_frame uptime=%.3f process_age=%.3f screen=%s", u, time.Since(r.started).Seconds(), fi.Screen)
	}
}

// runUI runs the panel with everything it needs on real devices, as ui.Run does,
// with a frame observer added. Any panic escaping it is contained and the panel
// started again: a UI failure must not stop control (ADR 0306).
func (r *runtime) runUI(ctx context.Context, statePath string) {
	touch := make(chan ui.TouchEvent, 64)
	go func() {
		for ctx.Err() == nil {
			func() {
				defer func() {
					if p := recover(); p != nil {
						logfJ("<3>cured: touch reader panic contained: %v", p)
					}
				}()
				ui.ReadTouch(ctx, ui.DefaultTouchName, touch)
			}()
			sleepCtx(ctx, 5*time.Second)
		}
	}()
	for ctx.Err() == nil {
		func() {
			defer func() {
				if p := recover(); p != nil {
					logfJ("<3>cured: panel panic contained: %v", p)
				}
			}()
			fb, err := ui.OpenFramebuffer(ui.DefaultFramebuffer)
			if err != nil {
				log.Printf("ui: framebuffer not available: %v", err)
				return
			}
			defer fb.Close()
			bl := ui.OpenBacklight()
			bl.Set(ui.PowerOn)
			defer bl.Set(ui.PowerOn)
			ui.RunLoop(ctx, r.uiCtl, ui.Config{StatePath: statePath, Simulated: !r.linesDriven}, fb, touch, ui.LoopOptions{Power: bl.Set, OnFrame: r.onFrame})
		}()
		sleepCtx(ctx, 5*time.Second)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// checkSafe calls VerifySafeState and keeps the alarm table in step.
func (r *runtime) checkSafe() {
	if err := drivers.VerifySafeState(r.safeCfg); err != nil {
		r.sys.Raise(CodeSafeState, api.SevCritical, "System not in its fail-safe state",
			"Relays might stay on after a crash, or the fan PWM is not set up. Detail: "+err.Error()+". Re-run provisioning and restart.")
		return
	}
	r.sys.Clear(CodeSafeState)
}

// checkDisplay reads the marker bring-up leaves at boot. A missing panel has to be
// reported some other way, since nothing on the panel can be seen. Bring-up loads
// the panel overlay itself when the firmware missed it (ADR 0209); that is recorded
// as panel_source=runtime-overlay. See ADR 0403.
func (r *runtime) checkDisplay() (missing bool) {
	m, detail, err := drivers.DisplayMissing()
	recovered := strings.Contains(detail, "panel_source=runtime-overlay")
	switch {
	case err != nil:
		r.sys.Clear(CodeDisplayMissing)
		r.sys.Clear(CodePanelRecovered)
		r.sys.Raise(CodeDisplayUnknown, api.SevCritical, "Panel state not recorded at boot",
			"Bring-up did not record whether the panel came up, so it may also have failed: "+err.Error())
	case m:
		r.sys.Clear(CodeDisplayUnknown)
		r.sys.Clear(CodePanelRecovered)
		how := "Bring-up's automatic recovery did not bring it up either."
		if recovered {
			how = "Bring-up loaded the panel overlay itself and the panel still did not come up."
		}
		r.sys.Raise(CodeDisplayMissing, api.SevCritical, "Front panel did not come up",
			"Control is still running. "+how+" A power cycle is the only remedy known to work then. Recorded state: "+detail)
	case recovered:
		r.sys.Clear(CodeDisplayMissing)
		r.sys.Clear(CodeDisplayUnknown)
		r.sys.Raise(CodePanelRecovered, api.SevWarning, "Panel needed recovery at boot",
			"The firmware did not enable the panel on this boot; bring-up enabled it at runtime and it is working. Nothing to do. Recorded state: "+detail)
	default:
		r.sys.Clear(CodeDisplayMissing)
		r.sys.Clear(CodeDisplayUnknown)
		r.sys.Clear(CodePanelRecovered)
	}
	return m
}

const (
	panelStuckAfter  = 60 * time.Second
	restartWindow    = 600.0 // seconds of uptime
	restartThreshold = 4
)

// wireOpenings keeps the contact's opening count across restarts. A process that
// dies with the contact closed opens it by dying,
// and that opening is counted at the next start (ADR 0408).
//
// The file is in the state directory, so the count survives a power cut, and the
// opening the cut itself made is counted (integration defect 12, second re-test).
func (r *runtime) wireOpenings(contactFile string) {
	var n uint64
	closed := false
	if b, err := os.ReadFile(contactFile); err == nil {
		var c int
		if _, err := fmt.Sscan(string(b), &n, &c); err == nil && c == 1 {
			closed = true
		}
	}
	if closed {
		n++
	}
	// The annunciator calls this on the control loop, holding its lock: it must not
	// wait for the disk (regression from 42d54c3).
	w := newLatestWriter(contactFile, logfJ)
	r.openingsW = w
	write := func(n uint64, closed bool) {
		c := 0
		if closed {
			c = 1
		}
		w.Put([]byte(fmt.Sprintf("%d %d\n", n, c)))
	}
	write(n, false)
	r.ann.openings, r.ann.persist = n, write
}

// statusEvery is how often the status file is rewritten.
const statusEvery = 2 * time.Second

// lastAliveFromStatus is when a process last stepped control, from the status file it
// rewrote every couple of seconds. The age of the last control tick is subtracted, so a
// hung loop is not counted as alive. Real mode only: a simulation's control runs on
// simulated time.
func lastAliveFromStatus(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	var v struct {
		Age  *float64 `json:"control_last_tick_age_s"`
		Mode string   `json:"mode"`
	}
	if json.Unmarshal(b, &v) != nil || v.Age == nil || v.Mode != "real" || *v.Age < 0 {
		return time.Time{}
	}
	return fi.ModTime().Add(-time.Duration(*v.Age * float64(time.Second)))
}

// lastAliveRecorded is what post-stop kept for this start. The file is on tmpfs, so it
// exists only after a restart within one boot: a crash, a kill or a hang the watchdog
// ended. core uses it only if it is later than its own last record.
func lastAliveRecorded() time.Time {
	b, err := os.ReadFile(lastAliveFile)
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	if err != nil {
		return time.Time{}
	}
	return t
}

// flushSaves gives cured's own state files a moment to reach the disk at an orderly
// stop. They are written off the control loop; what does not make it in time is the
// newest change or so, as at a power cut.
func (r *runtime) flushSaves() {
	const wait = 3 * time.Second
	if r.openingsW != nil && !r.openingsW.Flush(wait) {
		logfJ("<4>cured: stopping before the contact count was saved")
	}
	if r.sys != nil && !r.sys.Flush(wait) {
		logfJ("<4>cured: stopping before the system alarms were saved")
	}
}

// noteStart records this start and counts recent ones. The file is on tmpfs, so a
// reboot resets the count; restarts inside one boot are what it is for.
func (r *runtime) noteStart() {
	now := uptimeNow()
	prev, _ := os.ReadFile(startsFile)
	var keep []string
	for _, l := range strings.Fields(string(prev)) {
		if v, err := strconv.ParseFloat(l, 64); err == nil && now-v <= restartWindow {
			keep = append(keep, l)
		}
	}
	keep = append(keep, strconv.FormatFloat(now, 'f', 2, 64))
	if err := os.WriteFile(startsFile, []byte(strings.Join(keep, "\n")+"\n"), 0o640); err != nil {
		logfJ("<4>cured: restart count: %v", err)
	}
	logfJ("<5>cured: starts within %.0f s: %d", restartWindow, len(keep))
	if len(keep) >= restartThreshold {
		// A burst of restarts is something that happened: it calls for 30 minutes or
		// until acknowledged, and never holds the contact for the rest of a cure. A
		// reboot restarts the count, so a burst already on record keeps its larger
		// figure.
		n := len(keep)
		for _, a := range r.sys.Active() {
			if a.Code == CodeRestartLoop && int(a.Peak) > n {
				n = int(a.Peak)
			}
		}
		r.sys.RaiseEvent(CodeRestartLoop, api.SevCritical, "Controller keeps restarting",
			fmt.Sprintf("The control service has started %d times in 10 minutes. Outputs drop on every restart. Check the journal.", n))
		r.sys.SetPeak(CodeRestartLoop, float64(n))
	}
}

// supervise rechecks system conditions and publishes status. It runs off the
// control goroutine and changes only the alarm table.
func (r *runtime) supervise(ctx context.Context) {
	t := time.NewTicker(statusEvery)
	defer t.Stop()
	n := 0
	r.lastFrame.Store(time.Now().UnixNano())
	missing := r.checkDisplay()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		n++
		if n%30 == 0 {
			r.checkSafe()
			missing = r.checkDisplay()
		}
		if age := time.Since(time.Unix(0, r.lastFrame.Load())); !missing && age > panelStuckAfter {
			r.sys.Raise(CodePanelStuck, api.SevCritical, "Front panel not updating",
				fmt.Sprintf("No frame reached the panel for %.0f s. Control is still running. Whatever the panel shows may be old.", age.Seconds()))
		} else if age <= panelStuckAfter {
			r.sys.Clear(CodePanelStuck)
		}
		r.writeStatus()
	}
}

func (r *runtime) statusMap() map[string]any {
	armed, closed, buzz := false, false, false
	if r.ann != nil {
		armed, closed, buzz = r.ann.State()
	}
	m := map[string]any{
		"mode": r.mode, "version": version, "pid": os.Getpid(), "uptime": uptimeNow(), "process_age_s": time.Since(r.started).Seconds(),
		"process_start_uptime":      r.upStart,
		"control_first_step_uptime": float64(r.upFirstStp.Load()) / 1000, "ready_uptime": float64(r.upReady.Load()) / 1000,
		"first_frame_uptime": float64(r.upFirstFrm.Load()) / 1000, "control_ticks": r.ticks.Load(),
		"control_last_tick_age_s": time.Since(time.Unix(0, r.lastTick.Load())).Seconds(), "frames": r.frames.Load(),
		"last_frame_age_s": time.Since(time.Unix(0, r.lastFrame.Load())).Seconds(),
		"system_alarms":    r.sys.Codes(), "annunciator_lines_driven": r.linesDriven,
	}
	var openings uint64
	if r.ann != nil {
		openings = r.ann.Openings()
	}
	if r.linesDriven {
		m["alarm_armed"], m["alarm_contact_closed"], m["buzzer_on"] = armed, closed, buzz
		m["contact_openings"] = openings
	} else {
		m["simulated_contact_openings"] = openings
		m["alarm_armed"], m["simulated_alarm_armed"] = false, armed
		// No line follows the decision in a simulation: the guard holds every line off,
		// so the real contact is open and a wired sounder sounds, both energize-to-run
		// (ADR 0406, 0407). The simulated decision is reported under its own names.
		m["alarm_contact_closed"], m["buzzer_on"] = false, true
		m["real_lines"] = "not driven by a simulation: contact open, buzzer line low (a wired sounder sounds)"
		m["simulated_contact_closed"], m["simulated_buzzer_on"] = closed, buzz
	}
	if r.statusX != nil {
		m["sim"] = r.statusX()
	}
	if r.v != nil {
		// A view for evidence and the journal only; the panel shows the same state.
		snap := r.v.Snapshot()
		type al struct {
			Code     string       `json:"code"`
			Severity api.Severity `json:"severity"`
			Acked    bool         `json:"acked"`
		}
		active := make([]al, 0, len(snap.Active))
		for _, a := range snap.Active {
			active = append(active, al{a.Code, a.Severity, a.Acked})
		}
		m["active_alarms"] = active
		m["temp_quality"], m["rh_quality"], m["air_quality"] = snap.Temp.Quality, snap.RH.Quality, snap.Air.Quality
		m["run_state"], m["recipe"], m["phase"] = snap.Program.State, snap.Program.RecipeID, snap.Program.PhaseName
		m["total_elapsed_s"] = snap.Program.TotalElapsed.Seconds()
		m["outage"] = snap.Health.LastOutage
		m["clock_trusted"], m["watchdog_ok"], m["log_ok"] = snap.Health.ClockTrusted, snap.Health.WatchdogOK, snap.Health.LogOK
	}
	return m
}

func (r *runtime) writeStatus() {
	// Nice to have; it must never take control down with it.
	defer func() {
		if p := recover(); p != nil {
			logfJ("<3>cured: status panic contained: %v", p)
		}
	}()
	b, err := json.Marshal(r.statusMap())
	if err != nil {
		return
	}
	tmp := statusFile + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err == nil {
		_ = os.Rename(tmp, statusFile)
	}
}

// shutdownStuck ends a stop that did not finish. Unclean is safe: outputs drop anyway.
func shutdownStuck() {
	logfJ("<3>cured: shutdown stuck, goroutines follow")
	_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
	_ = os.Remove(startsFile) // it was still a deliberate stop, not a crash
	os.Exit(1)
}

// traceBoot puts core's boot and outage records in the journal, which is the only
// place outside core's own files to see whether the previous stop counted as clean.
func traceBoot(e core.Event) {
	// Silences are also put in the system journal: an operator or a test reading the
	// journal could not see when one began or ended (re-test of defect 12).
	if e.Type == core.EvBoot || e.Type == core.EvOutage || e.Type == core.EvSilence {
		logfJ("<5>cured: core event %s: %s", e.Type, e.Msg)
	}
}

// runReal runs against the real cabinet.
func runReal(ctx context.Context) error {
	start := time.Now()
	r := &runtime{mode: "real", started: start, upStart: uptimeNow(), safeCfg: drivers.DefaultConfig(), linesDriven: true}
	r.lastTick.Store(start.UnixNano())
	logfJ("<5>cured: start mode=real version=%s uptime=%.3f", version, r.upStart)
	_ = os.Remove(simSock) // a socket left by a simulator has no business here

	clk := &realClock{start: start}
	clk.refresh()
	go func() {
		for ctx.Err() == nil {
			sleepCtx(ctx, 10*time.Second)
			clk.refresh()
		}
	}()
	r.sys = newSysAlarms(clk.Now, clk.Trusted, logfJ)
	r.sys.Persist("/var/lib/curing/system-alarms.json")
	r.checkSafe()
	r.noteStart()

	cab, wd, err := drivers.Open(r.safeCfg)
	if err != nil {
		return fmt.Errorf("invalid pin map: %w", err)
	}
	// Start from a known guard state. A lease dropped under an earlier process, or
	// under the simulator, must not appear as this process's OUTPUTS_DROPPED.
	if err := cab.Outputs.AllOff(); err != nil {
		logfJ("<4>cured: guard all-off at start: %v", err)
	}
	var lastPet atomic.Int64
	lastPet.Store(start.UnixNano())
	hold := &outputHold{lastPet: &lastPet}
	f := &lineFilter{next: cab.Outputs}
	coreCab := cab
	coreCab.Outputs = f
	r.ann = newAnnunciator(cab.Outputs, f, r.sys, start)

	var fs core.FS
	if err := os.MkdirAll(realStateDir, 0o750); err != nil {
		logfJ("<2>cured: state dir: %v", err)
	}
	osfs, err := core.NewOSFS(realStateDir)
	if err != nil {
		r.sys.Raise(CodeStorage, api.SevCritical, "Cure state cannot be saved",
			"The state directory could not be opened, so a restart loses the cure: "+err.Error())
		fs = core.NewMemFS()
	} else {
		fs = osfs
	}
	lastAlive := lastAliveRecorded()
	c, err := core.New(core.Options{Cabinet: coreCab, Clock: clk, FS: fs, Speed: 1, Version: version, AsyncDepth: 256, Trace: traceBoot,
		OSBootID: strings.TrimSpace(bootID()), LastAlive: lastAlive, LastAliveUncertSec: statusEvery.Seconds(),
		Watchdog: watchedWD{inner: wd, last: &lastPet, on: func(e error) {
			if errors.Is(e, hal.ErrOutputsDropped) {
				r.ann.Dropped()
			}
		}}})
	if err != nil {
		return err
	}
	r.c, r.v = c, &view{c: c, sys: r.sys}
	r.uiCtl = r.v
	r.ann.coreEdges = c.EdgeCount
	r.sys.SetRecord(c.RecordSystem)
	r.wireOpenings("/var/lib/curing/contact")

	uiCtx, uiStop := context.WithCancel(context.Background())
	uiDone := make(chan struct{})
	go func() { defer close(uiDone); r.runUI(uiCtx, ui.DefaultStatePath) }()
	supCtx, supStop := context.WithCancel(context.Background())
	defer supStop()
	go r.supervise(supCtx)

	// The control loop runs on this goroutine. A panic here is not recovered: the
	// process dies, the guard drops the lines, systemd restarts to a known state.
	t := time.NewTicker(time.Second)
	defer t.Stop()
	first := true
	for ctx.Err() == nil {
		c.Step()
		r.ticks.Add(1)
		r.lastTick.Store(time.Now().UnixNano())
		q := api.Quality("")
		if armed, _, _ := r.ann.State(); !armed {
			q = c.Snapshot().Temp.Quality
		}
		held := hold.Check(time.Now(), func() bool { return hasActive(c.Snapshot(), "OUTPUT_FAULT") })
		r.ann.tick(time.Now(), q, held)
		if first {
			first = false
			r.noteFirstStep()
			if err := wd.Ready(); err != nil {
				logfJ("<4>cured: ready: %v", err)
			}
			r.noteReady()
		}
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
	_ = wd.Stopping()
	logfJ("<5>cured: stopping")
	force := time.AfterFunc(stopBound, shutdownStuck)
	defer force.Stop()
	r.flushSaves()
	stopAt := time.Now()
	slow := time.AfterFunc(5*time.Second, func() {
		logfJ("<4>cured: stop is slow: still flushing state to the card")
	})
	defer slow.Stop()
	err = c.Close()
	logfJ("<5>cured: core closed in %.2f s", time.Since(stopAt).Seconds())
	uiStop()
	select {
	case <-uiDone:
	case <-time.After(2 * time.Second):
	}
	return errors.Join(err, cab.Close())
}
