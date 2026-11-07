package main

import (
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
	sys.Raise(CodeDisplayMissing, api.SevCritical, "t", "a")
	a.Tick(*clock, "")
	if l.alarm || !l.buzzer {
		t.Fatalf("a system alarm must open the contact and sound: %+v", l)
	}
	sys.Ack("")
	a.Tick(*clock, "")
	if l.alarm || l.buzzer {
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
	*clock = clock.Add(time.Hour)
	a.Tick(*clock, api.QualityFault)
	if l.alarm {
		t.Fatal("no good reading yet")
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
	if next.sets != 0 || !f.coreHealthy.Load() || !f.coreBuzzer.Load() {
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

func TestAlarmOnlyRefusesPlantOutputs(t *testing.T) {
	r := &recLines{}
	a := alarmOnly{r}
	for _, id := range []hal.OutputID{hal.OutCool, hal.OutHeat, hal.OutHumid, hal.OutDehumid} {
		if a.Set(id, true) == nil {
			t.Fatalf("simulator reached %s", id)
		}
	}
	if err := a.Set(hal.OutAlarm, true); err != nil {
		t.Fatal(err)
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
	if !l.alarm || l.buzzer {
		t.Fatalf("a warning must not open the contact or sound: %+v", l)
	}
	if len(sys.Active()) != 1 {
		t.Fatal("the warning must stay on record")
	}
}
