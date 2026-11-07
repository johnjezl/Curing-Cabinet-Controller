// Package simtest runs the panel on the real control core and the simulated plant,
// not on fixtures: taps go through the panel to the core, and what comes back is
// the core's own text and state. It is separate so a change in the core can only
// fail this package.
package simtest

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui/uitest"
)

type rig struct {
	t   *testing.T
	run *sim.Run
	h   *uitest.Harness
	acc time.Duration
	dir string
	log *os.File
}

func newRig(t *testing.T) *rig {
	t.Helper()
	p := sim.BasePlant()
	run, err := sim.NewRun(sim.Options{Plant: p, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	run.Advance(2 * 3600)
	r := &rig{t: t, run: run, dir: os.Getenv("UI_SIM_PNG_DIR")}
	r.h = uitest.NewLive(run.Core, func(d time.Duration) {
		r.acc += d
		for r.acc >= time.Second {
			r.acc -= time.Second
			run.Tick()
		}
	}, ui.Config{})
	if r.dir != "" {
		f, err := os.OpenFile(filepath.Join(r.dir, "states.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		r.log = f
		t.Cleanup(func() { f.Close() })
	}
	return r
}

func q(n string, x api.Reading) string {
	return fmt.Sprintf("%s=%.2f/%s/%.0fs", n, x.Value, x.Quality, x.AgeSec)
}

// dump records the core's own state for a stage. Raw: no interpretation.
func (r *rig) dump(stage string) api.Snapshot {
	s := r.run.Core.Snapshot()
	var al []string
	for _, a := range s.Active {
		al = append(al, fmt.Sprintf("%s/%s/acked=%v", a.Code, a.Severity, a.Acked))
	}
	var holds []string
	for _, h := range s.Outputs.Holds {
		holds = append(holds, fmt.Sprintf("%s:%s:%.0fs", h.Output, h.Reason, h.RemainingSec))
	}
	if r.log != nil {
		o := s.Outputs
		fmt.Fprintf(r.log, "%s\t%s %s %s %s\tset=%.1f/%.0f/%.2f\tcool=%v heat=%v humid=%v dehum=%v defrost=%v fan=%.0f%%\tbuzzer_silenced=%v\tactive=%v\tholds=%v\thistory=%d\tscreen=%s\n",
			stage, q("t", s.Temp), q("rh", s.RH), q("air", s.Air), q("w", s.Weight), s.Setpoint.TempC, s.Setpoint.RHPct, s.Setpoint.AirMS,
			o.Cool, o.Heat, o.Humid, o.Dehumid, o.Defrost, o.FanDutyPct, s.BuzzerSilenced, al, holds, len(s.History), r.h.Panel.Screen())
	}
	return s
}

func (r *rig) shot(name string) {
	r.t.Helper()
	a := &ui.Audit{}
	r.h.Panel.SetAudit(a)
	r.h.Sync()
	r.h.Frame()
	for _, p := range a.Problems {
		r.t.Errorf("%s: %s", name, p)
	}
	r.h.Panel.SetAudit(nil)
	if r.dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(r.dir, "realcore__"+name+".png"))
	if err != nil {
		r.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, r.h.Img); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) tap(id string) {
	r.t.Helper()
	if _, err := r.h.TapID(id); err != nil {
		r.t.Fatal(err)
	}
	r.h.Sync()
	r.h.Frame()
}

func (r *rig) hold(id string, d time.Duration) {
	r.t.Helper()
	if _, err := r.h.HoldID(id, d); err != nil {
		r.t.Fatal(err)
	}
	r.h.Sync()
	r.h.Frame()
}

func (r *rig) until(max float64, what string, cond func(api.Snapshot) bool) {
	r.t.Helper()
	ok := r.run.AdvanceUntil(max, func() bool { return cond(r.run.Core.Snapshot()) })
	if !ok {
		r.t.Fatalf("%s did not happen in %.0f simulated s", what, max)
	}
	r.h.Sync()
	r.h.Frame()
}

func hasAlarm(s api.Snapshot, code string) *api.Alarm {
	for i := range s.Active {
		if s.Active[i].Code == code {
			return &s.Active[i]
		}
	}
	return nil
}

// The whole alarm path, through the panel's own targets, on the real core.
func TestAlarmPathOnRealCore(t *testing.T) {
	r := newRig(t)
	r.dump("start")
	r.shot("home")
	r.tap("nav.set")
	r.shot("set-manual")
	r.tap("nav.more")
	r.tap("more.health")
	r.shot("health")
	r.tap("nav.cure")
	r.shot("recipes")
	r.tap("recipe.0")
	r.tap("recipe.start")
	r.shot("start-confirm")
	r.hold("dialog.ok", 2100*time.Millisecond)
	r.h.Advance(2500 * time.Millisecond)
	if st := r.dump("started").Program.State; st != api.RunActive {
		t.Fatalf("state %s, errs %v", st, r.h.Errs)
	}
	r.shot("cure-running")
	r.run.Advance(3600)
	r.h.Sync()
	r.tap("nav.trend")
	r.shot("trend")
	r.tap("nav.home")

	// A sensor fails. The alarm is the core's, with its own words.
	r.run.Plant.Inject(sim.DevClimate, sim.FaultDead, 0)
	r.until(3600, "climate alarm", func(s api.Snapshot) bool { return hasAlarm(s, core.CodeClimateFault) != nil })
	s := r.dump("climate-sensor-dead")
	al := hasAlarm(s, core.CodeClimateFault)
	r.shot("alarm-home")
	if _, _, err := r.h.Find("home.silence"); err != nil {
		t.Fatal(err)
	}

	// Silence: the core says silenced, and the alarm stays.
	r.tap("home.silence")
	r.h.Advance(2500 * time.Millisecond)
	s = r.dump("after-silence")
	if !s.BuzzerSilenced {
		t.Fatalf("core not silenced: errs %v", r.h.Errs)
	}
	if hasAlarm(s, core.CodeClimateFault) == nil {
		t.Fatal("silence removed the alarm")
	}
	r.shot("silenced-home")

	// Acknowledge: the core records it, and the alarm stays.
	r.tap("nav.alarms")
	r.shot("alarms-before-ack")
	r.tap("alarms.ack." + al.ID)
	r.h.Advance(2500 * time.Millisecond)
	s = r.dump("after-ack")
	if a := hasAlarm(s, core.CodeClimateFault); a == nil || !a.Acked {
		t.Fatalf("ack not recorded: %+v errs %v", a, r.h.Errs)
	}
	r.shot("alarms-acked")

	// Repair: it clears by itself and lands in the core's history, with times.
	r.run.Plant.Inject(sim.DevClimate, sim.FaultNone, 0)
	r.until(3600, "climate clear", func(s api.Snapshot) bool { return hasAlarm(s, core.CodeClimateFault) == nil })
	r.run.Advance(600)
	s = r.dump("after-repair")
	found := false
	for _, a := range s.History {
		if a.Code == core.CodeClimateFault && !a.Cleared.IsZero() && !a.Since.IsZero() {
			found = true
		}
	}
	if !found {
		t.Fatalf("not in history: %+v", s.History)
	}
	r.h.Sync()
	r.tap("alarms.tab.1")
	r.shot("history")
	r.tap("nav.home")

	// End of cure: the verify text is the core's, verbatim.
	r.tap("nav.cure")
	for i := 0; i < 8 && r.run.Core.Snapshot().Program.State != api.RunFinished; i++ {
		r.tap("cure.next")
		r.hold("dialog.ok", 2100*time.Millisecond)
		r.h.Advance(2500 * time.Millisecond) // the confirmation message clears
	}
	s = r.dump("finished")
	if s.Program.State != api.RunFinished {
		t.Fatalf("state %s errs %v", s.Program.State, r.h.Errs)
	}
	if s.Program.Verify != core.VerifyText {
		t.Fatalf("verify differs from core text")
	}
	a := &ui.Audit{}
	r.h.Panel.SetAudit(a)
	r.h.Frame()
	var all []string
	for _, x := range a.Texts {
		all = append(all, x.S)
	}
	r.h.Panel.SetAudit(nil)
	if !strings.Contains(strings.Join(strings.Fields(strings.Join(all, " ")), " "), core.VerifyText) {
		t.Fatalf("verify text not on screen:\n%v", all)
	}
	r.shot("verify")
}

func enter(r *rig, s string) {
	r.t.Helper()
	for _, d := range s {
		id := "keypad." + string(d)
		if d == '.' {
			id = "keypad.dot"
		}
		r.tap(id)
	}
	r.tap("keypad.ok")
}

// An override and a reference check, through the panel's targets, on the real core.
func TestOverrideAndReferenceCheckOnRealCore(t *testing.T) {
	r := newRig(t)
	r.tap("nav.cure")
	r.tap("recipe.0")
	r.tap("recipe.start")
	r.hold("dialog.ok", 2100*time.Millisecond)
	r.h.Advance(2500 * time.Millisecond)
	r.run.Advance(1800)
	r.h.Sync()
	before := r.run.Core.Snapshot().Setpoint
	r.tap("nav.set")
	r.tap("edit.minus.0")
	r.shot("override-pending")
	r.tap("edit.save")
	r.shot("override-confirm")
	r.hold("dialog.ok", 2100*time.Millisecond)
	r.h.Advance(2500 * time.Millisecond)
	s := r.dump("override")
	if s.Program.Override == nil || s.Setpoint.TempC != before.TempC-0.5 {
		t.Fatalf("override not applied: %+v errs %v", s.Program.Override, r.h.Errs)
	}
	r.tap("nav.home")
	r.shot("override-home")
	r.tap("nav.cure")
	r.shot("override-cure")
	r.tap("nav.more")
	r.tap("more.outputs")
	r.shot("outputs")

	// A reference far from the sensor raises the core's alarm and latches.
	r2 := newRig(t)
	sensor := r2.run.Core.Snapshot().Temp.Value
	r2.tap("nav.more")
	r2.shot("more")
	r2.tap("more.refcheck")
	enter(r2, "2580")
	r2.shot("refcheck-entry")
	enter(r2, fmt.Sprintf("%.1f", sensor+6))
	r2.tap("keypad.ok")
	r2.shot("refcheck-confirm-far")
	r2.hold("dialog.ok", 2100*time.Millisecond)
	r2.h.Advance(2500 * time.Millisecond)
	s = r2.dump("refcheck-far")
	if hasAlarm(s, core.CodeRefDisagree) == nil {
		t.Fatalf("no disagreement alarm: errs %v, %+v", r2.h.Errs, s.Active)
	}
	r2.tap("nav.alarms")
	r2.shot("refcheck-alarm")

	// A close reference, entered from that alarm's own button, clears it.
	sensor = r2.run.Core.Snapshot().Temp.Value
	r2.tap("alarms.refcheck." + hasAlarm(s, core.CodeRefDisagree).ID)
	enter(r2, "2580")
	enter(r2, fmt.Sprintf("%.1f", sensor+0.3))
	r2.tap("keypad.ok")
	r2.shot("refcheck-confirm-close")
	r2.hold("dialog.ok", 2100*time.Millisecond)
	r2.h.Advance(2500 * time.Millisecond)
	s = r2.dump("refcheck-close")
	if hasAlarm(s, core.CodeRefDisagree) != nil || hasAlarm(s, core.CodeTempDisagree) != nil {
		t.Fatalf("not cleared: errs %v, %+v", r2.h.Errs, s.Active)
	}
	if s.Health.RefCheckDueSec <= 0 {
		t.Errorf("due not reset: %v", s.Health.RefCheckDueSec)
	}
	r2.tap("nav.home")
	r2.shot("refcheck-cleared")
}

// What the core really produces for bad sensors. Recorded raw for the states the
// panel's fixtures claim to represent.
func TestBadReadingStatesFromRealCore(t *testing.T) {
	cases := []struct {
		name string
		do   func(r *rig)
	}{
		{"climate-dead", func(r *rig) { r.run.Plant.Inject(sim.DevClimate, sim.FaultDead, 0) }},
		{"temp-stuck", func(r *rig) { r.run.Plant.Inject(sim.DevTemp, sim.FaultStuck, 0) }},
		{"rh-stuck", func(r *rig) { r.run.Plant.Inject(sim.DevRH, sim.FaultStuck, 0) }},
		{"temp-impossible", func(r *rig) { r.run.Plant.Inject(sim.DevTemp, sim.FaultRange, 150) }},
		{"all-dead", func(r *rig) {
			r.run.Plant.Inject(sim.DevClimate, sim.FaultDead, 0)
			r.run.Plant.Inject(sim.DevAir, sim.FaultDead, 0)
			r.run.Plant.Inject(sim.DevScale, sim.FaultDead, 0)
		}},
		{"compressor-dead", func(r *rig) { r.run.Plant.Inject(sim.DevCompressor, sim.FaultNoCool, 0) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.dump(c.name + " +0")
			c.do(r)
			for _, step := range []float64{30, 90, 180, 300, 600, 1800, 3600} {
				r.run.Advance(step)
				s := r.dump(fmt.Sprintf("%s after+%.0fs", c.name, step))
				_ = s
			}
			r.h.Sync()
			r.shot("state-" + c.name)
			r.tap("nav.alarms")
			r.shot("state-" + c.name + "-alarms")
			r.tap("nav.more")
			r.tap("more.health")
			r.shot("state-" + c.name + "-health")
		})
	}
}
