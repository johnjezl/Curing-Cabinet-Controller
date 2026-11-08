package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

func noFiles(string) bool { return false }

func TestNoArgumentsIsRealHardware(t *testing.T) {
	isSim, _, err := parseArgs(nil)
	if err != nil || isSim {
		t.Fatalf("no arguments must be real hardware, got sim=%v err=%v", isSim, err)
	}
}

func TestSimGateRefusesAnythingIncomplete(t *testing.T) {
	good := []string{"--sim", "--speed", "400", "--sim-outage", "1x", "--sim-confirm=" + simConfirmToken}
	o, err := parseSimArgs(good)
	if err != nil {
		t.Fatal(err)
	}
	if err := simGate(o, noFiles); err != nil {
		t.Fatalf("complete set refused: %v", err)
	}
	for i := range good {
		// Remove one element, including a flag's value.
		var a []string
		a = append(a, good[:i]...)
		a = append(a, good[i+1:]...)
		o, err := parseSimArgs(a)
		if err != nil {
			continue
		}
		if simGate(o, noFiles) == nil {
			t.Errorf("accepted %v", a)
		}
	}
	for _, a := range [][]string{{"--speed", "400"}, {"--sim"}, {"--sim", "--speed", "0", "--sim-outage", "1x", "--sim-confirm=" + simConfirmToken},
		{"--sim", "--speed", "400", "--sim-outage", "1x", "--sim-confirm=yes"}, {"--sim", "--speed", "400", "--sim-outage", "other", "--sim-confirm=" + simConfirmToken}} {
		o, err := parseSimArgs(a)
		if err == nil && simGate(o, noFiles) == nil {
			t.Errorf("accepted %v", a)
		}
	}
	// A real cabinet's marker wins over a perfect command line.
	if err := simGate(o, func(p string) bool { return p == productionMarker }); err == nil || !strings.Contains(err.Error(), "real cabinet") {
		t.Fatalf("production marker ignored: %v", err)
	}
	if _, _, err := parseArgs([]string{"stray"}); err == nil {
		t.Fatal("stray argument accepted")
	}
}

func TestVirtualGap(t *testing.T) {
	g, st, c := virtualGap(outage1x, 400, 90)
	if g != 90 || c || st != 400 {
		t.Fatalf("1x: %v %v %v", g, st, c)
	}
	g, _, _ = virtualGap(outageCompressed, 400, 90)
	if g != 36000 {
		t.Fatalf("compressed: %v", g)
	}
	g, _, c = virtualGap(outageCompressed, 5000, 100000)
	if !c || g != maxVirtGap {
		t.Fatalf("cap: %v %v", g, c)
	}
	g, _, _ = virtualGap(outage1x, 400, -5)
	if g != 0 {
		t.Fatalf("negative gap: %v", g)
	}
}

type recLines struct {
	alarm, buzzer bool
	sets          int
	err           error
	offs          int
}

func (r *recLines) Set(id hal.OutputID, on bool) error {
	r.sets++
	if r.err != nil {
		return r.err
	}
	switch id {
	case hal.OutAlarm:
		r.alarm = on
	case hal.OutBuzzer:
		r.buzzer = on
	}
	return nil
}
func (r *recLines) AllOff() error { r.offs++; r.alarm, r.buzzer = false, false; return nil }

func testAnn() (*annunciator, *lineFilter, *recLines, *sysAlarms, *time.Time) {
	now := time.Unix(1000, 0)
	clock := &now
	sys := newSysAlarms(func() time.Time { return *clock }, func() bool { return true }, func(string, ...any) {})
	l := &recLines{}
	f := &lineFilter{next: &recLines{}}
	return newAnnunciator(l, f, sys, now), f, l, sys, clock
}

func TestContactEnergizedOnlyWhenHealthyAndArmed(t *testing.T) {
	a, f, l, sys, clock := testAnn()
	_ = f.Set(hal.OutAlarm, true)
	a.Tick(*clock, api.QualityOK)
	if l.alarm {
		t.Fatal("contact closed before the arm delay")
	}
	*clock = clock.Add(armDelay)
	a.Tick(*clock, api.QualityOK)
	if !l.alarm {
		t.Fatal("contact should be closed when armed and healthy")
	}
	_ = f.Set(hal.OutAlarm, false)
	a.Tick(*clock, "")
	if l.alarm {
		t.Fatal("core calling must open the contact")
	}
	_ = f.Set(hal.OutAlarm, true)
	a.Tick(*clock, "") // healthy again first, so the system alarm is a plain opening
	sys.Raise(CodeDisplayMissing, api.SevCritical, "t", "a")
	a.Tick(*clock, "")
	if l.alarm || l.buzzer { // both energize-to-run: low is open and sounding
		t.Fatalf("a system alarm must open the contact and sound: %+v", l)
	}
	sys.Ack("")
	a.Tick(*clock, "")
	if l.alarm || !l.buzzer {
		t.Fatalf("ack stops the buzzer, never the contact: %+v", l)
	}
	sys.Clear(CodeDisplayMissing)
	a.Tick(*clock, "")
	if !l.alarm {
		t.Fatal("contact should close again")
	}
}

func TestContactNeedsAGoodTemperatureFirst(t *testing.T) {
	a, f, l, _, clock := testAnn()
	_ = f.Set(hal.OutAlarm, true)
	*clock = clock.Add(armBlind - time.Second)
	a.Tick(*clock, api.QualityFault)
	if l.alarm {
		t.Fatal("no good reading yet")
	}
}

// Integration defect 12, second re-test: started with the climate sensor dead, the
// relay never armed, so no later alarm could make a fresh transition. It arms once core
// has had time to raise the sensor fault, which keeps the contact open by itself.
func TestDeadSensorAtStartStillGivesFreshTransitions(t *testing.T) {
	a, f, l, _, clock := testAnn()
	var edges uint64
	a.coreEdges = func() uint64 { return edges }
	_ = f.Set(hal.OutAlarm, false) // core calls the sensor fault
	for i := 0; i < int(armBlind/time.Second); i++ {
		*clock = clock.Add(time.Second)
		a.Tick(*clock, api.QualityFault)
		if l.alarm {
			t.Fatalf("closed %d s after start with the sensor dead", i+1)
		}
	}
	if armed, _, _ := a.State(); !armed {
		t.Fatal("not armed a minute after start with the sensor dead")
	}
	edges++ // a new critical alarm
	*clock = clock.Add(time.Second)
	a.Tick(*clock, api.QualityFault)
	if !l.alarm {
		t.Fatal("a new alarm made no fresh transition")
	}
	*clock = clock.Add(edgeFor)
	a.Tick(*clock, api.QualityFault)
	if l.alarm {
		t.Fatal("and the contact must open again")
	}
}

func TestDropRearms(t *testing.T) {
	a, f, l, _, clock := testAnn()
	_ = f.Set(hal.OutAlarm, true)
	*clock = clock.Add(armDelay)
	a.Tick(*clock, api.QualityOK)
	if !l.alarm {
		t.Fatal("setup")
	}
	a.Dropped()
	*clock = clock.Add(time.Second)
	a.Tick(*clock, api.QualityOK)
	if l.alarm {
		t.Fatal("after a lease drop the contact stays open until control proves itself")
	}
}

func TestLineFaultRaises(t *testing.T) {
	a, _, l, sys, clock := testAnn()
	l.err = errors.New("no guard")
	for i := 0; i < 3; i++ {
		*clock = clock.Add(time.Second)
		a.Tick(*clock, api.QualityOK)
	}
	if len(sys.Codes()) != 1 || sys.Codes()[0] != CodeLinesFault {
		t.Fatalf("codes %v", sys.Codes())
	}
	l.err = nil
	*clock = clock.Add(time.Second)
	a.Tick(*clock, api.QualityOK)
	if len(sys.Codes()) != 0 {
		t.Fatalf("not cleared: %v", sys.Codes())
	}
}

func TestLineFilterKeepsAnnunciatorLinesFromHardware(t *testing.T) {
	next := &recLines{}
	f := &lineFilter{next: next}
	_ = f.Set(hal.OutAlarm, true)
	_ = f.Set(hal.OutBuzzer, true)
	if next.sets != 0 || !f.coreHealthy.Load() || f.coreBuzzer.Load() { // energized buzzer line: silent
		t.Fatalf("filter leaked: %+v", next)
	}
	_ = f.Set(hal.OutCool, true)
	if next.sets != 1 {
		t.Fatal("plant output must pass")
	}
	_ = f.AllOff()
	if next.offs != 1 || f.coreHealthy.Load() {
		t.Fatal("alloff must pass and clear")
	}
}

func TestSimulationRefusesToDriveAnnunciators(t *testing.T) {
	a := []string{"--sim", "--speed", "400", "--sim-outage", "1x", "--sim-confirm=" + simConfirmToken, "--sim-annunciate"}
	o, err := parseSimArgs(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := simGate(o, noFiles); err == nil || !strings.Contains(err.Error(), "never drives") {
		t.Fatalf("--sim-annunciate must be refused with its reason, got %v", err)
	}
}

func TestSysAlarmSilenceReturnsOnNewAlarm(t *testing.T) {
	now := time.Unix(1000, 0)
	s := newSysAlarms(func() time.Time { return now }, func() bool { return true }, func(string, ...any) {})
	s.Raise("A", api.SevCritical, "a", "x")
	s.Silence()
	if s.Sounding() {
		t.Fatal("silenced")
	}
	now = now.Add(sysSilenceFor)
	if !s.Sounding() {
		t.Fatal("silence must time out")
	}
	s.Silence()
	s.Raise("B", api.SevCritical, "b", "x")
	if !s.Sounding() {
		t.Fatal("a new alarm must be heard")
	}
}

func TestPlantStateRoundTrip(t *testing.T) {
	p := sim.NewPlant(sim.BasePlant())
	for i := 0; i < 3600; i++ {
		p.CmdCool = true
		p.FanDuty = 40
		p.Step(1)
	}
	p.Inject(sim.DevTemp, sim.FaultOffset, 3)
	s := p.SaveState()
	q := sim.RestorePlant(s, 7)
	if q.SaveState() != s {
		t.Fatalf("round trip differs\n%+v\n%+v", q.SaveState(), s)
	}
	if got := activeFaults(s); len(got) != 1 || got[0] != "temp:offset" {
		t.Fatalf("faults %v", got)
	}
	q.Step(10) // must run
}

func TestAtomicWriteReplaces(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plant.json")
	if err := atomicWrite(p, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(p, []byte("two")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "two" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}

func TestSystemWarningIsARecordOnly(t *testing.T) {
	a, f, l, sys, clock := testAnn()
	_ = f.Set(hal.OutAlarm, true)
	*clock = clock.Add(armDelay)
	sys.Raise(CodePanelRecovered, api.SevWarning, "t", "a")
	a.Tick(*clock, api.QualityOK)
	if !l.alarm || !l.buzzer {
		t.Fatalf("a warning must not open the contact or sound: %+v", l)
	}
	if len(sys.Active()) != 1 {
		t.Fatal("the warning must stay on record")
	}
}

// Integration defect 3: a latched system alarm held the contact open for the rest of a
// cure, so later alarms made no transition. Every new alarm is now a brief close and a
// fresh opening, whoever holds the contact (ADR 0408).
func TestNewAlarmIsAFreshOpeningWhileContactHeld(t *testing.T) {
	a, f, l, sys, clock := testAnn()
	var edges uint64
	a.coreEdges = func() uint64 { return edges }
	_ = f.Set(hal.OutAlarm, true)
	*clock = clock.Add(armDelay)
	a.Tick(*clock, api.QualityOK)
	if !l.alarm {
		t.Fatal("not armed")
	}
	sys.Raise(CodeDisplayMissing, api.SevCritical, "t", "a")
	a.Tick(*clock, "")
	if l.alarm || a.Openings() != 1 {
		t.Fatalf("system alarm must open the contact: %+v openings %d", l, a.Openings())
	}
	// core raises a new alarm while the system alarm holds the contact open.
	edges++
	*clock = clock.Add(time.Second)
	a.Tick(*clock, "")
	if !l.alarm {
		t.Fatal("a new core alarm must close the contact briefly")
	}
	*clock = clock.Add(edgeFor)
	a.Tick(*clock, "")
	if l.alarm || a.Openings() != 2 {
		t.Fatalf("and open it again: %+v openings %d", l, a.Openings())
	}
	// A second system alarm while the contact is open does the same.
	sys.Raise(CodeSafeState, api.SevCritical, "t", "a")
	a.Tick(*clock, "")
	if !l.alarm {
		t.Fatal("a new system alarm must close the contact briefly")
	}
	*clock = clock.Add(edgeFor)
	a.Tick(*clock, "")
	if l.alarm || a.Openings() != 3 {
		t.Fatalf("and open it again: %+v openings %d", l, a.Openings())
	}
}

func TestRestartLoopIsAnEventNotALatch(t *testing.T) {
	now := time.Unix(1000, 0)
	s := newSysAlarms(func() time.Time { return now }, func() bool { return true }, func(string, ...any) {})
	s.realNow = func() time.Time { return now }
	s.RaiseEvent(CodeRestartLoop, api.SevCritical, "t", "a")
	if !s.Calling() || !s.Sounding() {
		t.Fatal("a fresh restart burst must call")
	}
	now = now.Add(sysEventCall)
	if s.Calling() || s.Sounding() {
		t.Fatal("an old restart burst must not hold the contact")
	}
	if len(s.Active()) != 1 {
		t.Fatal("it stays listed until acknowledged")
	}
	s.Ack("")
	if len(s.Active()) != 0 || len(s.History()) != 1 {
		t.Fatal("acknowledged, it moves to history")
	}
}

// The re-test of defect 6: an unacknowledged restart-burst alarm vanished at a power cut
// and never reached the history. Events and history now survive a new process.
func TestSystemEventSurvivesANewProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "system-alarms.json")
	now := time.Unix(1000, 0)
	s := newSysAlarms(func() time.Time { return now }, func() bool { return true }, func(string, ...any) {})
	s.Persist(path)
	s.RaiseEvent(CodeRestartLoop, api.SevCritical, "t", "a")
	s.Raise(CodeSafeState, api.SevCritical, "t", "a") // a condition: found again, not kept
	s2 := newSysAlarms(func() time.Time { return now }, func() bool { return true }, func(string, ...any) {})
	s2.Persist(path)
	if codes := s2.Codes(); len(codes) != 1 || codes[0] != CodeRestartLoop {
		t.Fatalf("after a new process: %v", codes)
	}
	s2.Ack("")
	s3 := newSysAlarms(func() time.Time { return now }, func() bool { return true }, func(string, ...any) {})
	s3.Persist(path)
	if len(s3.Active()) != 0 || len(s3.History()) != 1 || s3.History()[0].Code != CodeRestartLoop {
		t.Fatalf("acknowledged, it must be in the kept history: active %v history %v", s3.Codes(), s3.History())
	}
}

// Integration defect 4, re-test: after the guard drop that ends an output fault, the
// contact stays open until control has proved itself again, with no brief close for the
// alarms that arrive meanwhile, and the buzzer sounds for a critical alarm throughout.
func TestAfterADropNoBriefCloseAndTheBuzzerSounds(t *testing.T) {
	a, f, l, _, clock := testAnn()
	var edges uint64
	a.coreEdges = func() uint64 { return edges }
	_ = f.Set(hal.OutAlarm, true)
	_ = f.Set(hal.OutBuzzer, true) // silent
	*clock = clock.Add(armDelay)
	a.Tick(*clock, api.QualityOK)
	if !l.alarm {
		t.Fatal("setup: not armed")
	}
	// A critical alarm stands: core opens the contact and sounds the buzzer.
	_ = f.Set(hal.OutAlarm, false)
	_ = f.Set(hal.OutBuzzer, false)
	a.Dropped()
	for i := 0; i < int(armDelay/time.Second); i++ {
		if i == 1 {
			edges++ // a new alarm while the relay must prove health again
		}
		*clock = clock.Add(time.Second)
		a.Tick(*clock, api.QualityOK)
		armed, closed, buzz := a.State()
		if closed {
			t.Fatalf("contact closed %d s after a drop (armed %v)", i+1, armed)
		}
		if !buzz {
			t.Fatalf("buzzer silent %d s after a drop with a critical alarm standing", i+1)
		}
	}
	for i := 0; i < 30; i++ {
		*clock = clock.Add(time.Second)
		a.Tick(*clock, api.QualityOK)
		if _, closed, buzz := a.State(); closed || !buzz {
			t.Fatalf("after re-arming, with core still calling: closed %v buzzer %v", closed, buzz)
		}
	}
}

// Integration defect 5, second re-test: the simulator's control tool says what happened,
// in words: a re-sent fault restarts, a text value is not a number, an unknown name lists
// what would work, and repairing everything lists what was repaired.
func TestSimctlRepliesSayWhatHappened(t *testing.T) {
	s := &simState{pl: sim.NewPlant(sim.BasePlant()), inbox: make(chan func(), 1)}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case f := <-s.inbox:
				f()
			case <-stop:
				return
			}
		}
	}()
	call := func(args ...string) string {
		v, err := s.handle(args)
		if err != nil {
			return "error: " + err.Error()
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"fault", "temp", "drift", "-0.5"}, `"injected":"temp:drift"`},
		{[]string{"fault", "temp", "drift", "-0.5"}, `"restarted"`},
		{[]string{"fault", "temp", "drift", "abc"}, `"abc" is not a number`},
		{[]string{"fault", "thermo", "drift", "1"}, "devices: "},
		{[]string{"fault", "temp", "melt"}, "faults: "},
		{[]string{"fault", "temp", "range", "20"}, "refused"},
		{[]string{"fault", "heat", "relay-stuck-on"}, `"injected":"heat:relay-stuck-on"`},
		{[]string{"repair", "all"}, `"repaired":["heat:relay-stuck-on","temp:drift"]`},
		{[]string{"repair", "all"}, "no fault was injected"},
	} {
		if got := call(c.args...); !strings.Contains(got, c.want) {
			t.Errorf("%v: got %s, want %q in it", c.args, got, c.want)
		}
	}
}

// Integration defect 12, second re-test: a power cut reset the opening count to 0, and
// the opening the cut made was not counted. The count is kept in the state directory.
func TestOpeningCountSurvivesAPowerCut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contact")
	r1 := &runtime{ann: newAnnunciator(nil, &lineFilter{next: &recLines{}}, newSysAlarms(time.Now, func() bool { return true }, func(string, ...any) {}), time.Now())}
	r1.wireOpenings(path)
	r1.ann.persist(43, true) // 43 openings so far, contact closed when power went
	r2 := &runtime{ann: newAnnunciator(nil, &lineFilter{next: &recLines{}}, newSysAlarms(time.Now, func() bool { return true }, func(string, ...any) {}), time.Now())}
	r2.wireOpenings(path)
	if got := r2.ann.Openings(); got != 44 {
		t.Fatalf("openings after the cut %d, want 44 (43 and the cut's own)", got)
	}
}
