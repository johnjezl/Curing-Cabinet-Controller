package core_test

import (
	"math"
	"sort"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

// Integration defects 2 and 7: at a 24 °C ferment the heater barely ran (true air
// 20.7 °C, RH 94 %), and drying sat about 1 °C under setpoint. This runs the
// simulator's own plant through the test cure's setpoints and an override, and
// holds the regulation the integration critic asked for: about ±1 °C and ±5 %RH.
// Air and humidity cycle with every compressor run, so the band is held on hourly
// means, and the instantaneous air on most of the time; ADR 0101 has the figures.
func TestHoldsEveryRecipeSetpoint(t *testing.T) {
	r, err := sim.NewRun(sim.Options{Plant: sim.BasePlant(), KeepRows: true, SampleSec: 30})
	if err != nil {
		t.Fatal(err)
	}
	r.SetCal()
	for _, ph := range []struct {
		name        string
		t, rh, days float64
	}{{"ferment", 24, 90, 2}, {"dry early", 14, 82, 3}, {"dry", 13, 76, 3}, {"override", 15, 78, 2}} {
		r.SetPoint(ph.t, ph.rh, 1.0)
		r.Rows = nil
		h0, u0 := r.Plant.HeatCycles(), r.Plant.HumidCycles()
		r.Advance(ph.days * 86400)
		var n, in1, in5, heat, sumT, sumRH float64
		var hourT, hourRH, hourN float64
		var hoursOut, hours int
		t0 := r.Rows[0].T
		for _, x := range r.Rows {
			if x.T-t0 < 6*3600 {
				continue // settling after the setpoint change
			}
			n++
			sumT += x.TrueT
			sumRH += x.TrueRH
			if math.Abs(x.TrueT-ph.t) <= 1 {
				in1++
			}
			if math.Abs(x.TrueRH-ph.rh) <= 5 {
				in5++
			}
			if x.HeatEff {
				heat++
			}
			hourT, hourRH, hourN = hourT+x.TrueT, hourRH+x.TrueRH, hourN+1
			if hourN == 120 {
				hours++
				if math.Abs(hourT/hourN-ph.t) > 1 || math.Abs(hourRH/hourN-ph.rh) > 5 {
					hoursOut++
				}
				hourT, hourRH, hourN = 0, 0, 0
			}
		}
		mT, mRH := sumT/n, sumRH/n
		closures := float64(r.Plant.HeatCycles()-h0) / ph.days
		humClosures := float64(r.Plant.HumidCycles()-u0) / ph.days
		t.Logf("%-9s set %4.1f/%2.0f: mean %.2f °C %.1f %%, %.0f %% of samples within 1 °C, %.0f %% within 5 %%RH, %d of %d hourly means out of band, heater %.0f %% of samples, %.0f closures/day, humidifier %.0f closures/day",
			ph.name, ph.t, ph.rh, mT, mRH, 100*in1/n, 100*in5/n, hoursOut, hours, 100*heat/n, closures, humClosures)
		if math.Abs(mT-ph.t) > 0.3 || math.Abs(mRH-ph.rh) > 1.5 {
			t.Errorf("%s: mean %.2f °C / %.1f %% against %.1f / %.0f", ph.name, mT, mRH, ph.t, ph.rh)
		}
		if hoursOut > 0 {
			t.Errorf("%s: %d hourly means outside ±1 °C / ±5 %%RH", ph.name, hoursOut)
		}
		if in1/n < 0.9 {
			t.Errorf("%s: true air within 1 °C only %.0f %% of the time", ph.name, 100*in1/n)
		}
		if ph.t == 24 && heat/n < 0.05 {
			t.Errorf("ferment: heater on in only %.1f %% of samples; this cabinet needs heat at 24 °C", 100*heat/n)
		}
	}
}

// Integration defect 8: after 5 kg was added the panel showed LOSS -46 %, and -36 % at
// the end of the cure. The change is added to the start weight, so loss stays the mass
// lost over all the mass that went in.
func TestWeightLossRebasedOnALoadChange(t *testing.T) {
	r := newRun(t)
	sp := api.Setpoints{TempC: 13, RHPct: 76, AirMS: 1.0}
	rc := api.Recipe{ID: "t", Name: "t", Phases: []api.PhaseSpec{{Name: "dry", Setpoint: sp, Duration: 10 * 24 * time.Hour}}}
	if err := r.Core.Do(api.Command{Kind: api.CmdSaveRecipe, Recipe: &rc}); err != nil {
		t.Fatal(err)
	}
	if err := r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: "t"}); err != nil {
		t.Fatal(err)
	}
	r.Advance(3 * 86400)
	before := r.Core.Snapshot().Program.WeightLossPct
	if before <= 0.5 {
		t.Fatalf("no loss to speak of before the load change: %.2f %%", before)
	}
	r.Plant.DoorIsOpen = true
	r.Advance(60)
	r.Plant.LoadProduct(5, 25)
	r.Plant.DoorIsOpen = false
	r.Advance(3600)
	after := r.Core.Snapshot().Program.WeightLossPct
	if after < 0 || after > before {
		t.Fatalf("loss %.2f %% before 5 kg went in, %.2f %% after: want a figure between 0 and the old one", before, after)
	}
	r.Advance(2 * 86400)
	if l := r.Core.Snapshot().Program.WeightLossPct; l <= after {
		t.Fatalf("loss must keep growing after the change: %.2f %% then %.2f %%", after, l)
	}
}

// Integration defect 6: the outage alarm was dated at the return, not the loss, its
// history duration read -1, and service crashes were recorded as "Power was lost".
func TestOutageRecordedFromTheLossWithItsCause(t *testing.T) {
	for _, c := range []struct {
		name, nextBoot, want string
	}{{"process restart", "boot-a", "CONTROLLER_RESTARTED"}, {"power cut", "boot-b", "POWER_OUTAGE"}} {
		t.Run(c.name, func(t *testing.T) {
			p := sim.BasePlant()
			r, err := sim.NewRun(sim.Options{Plant: p, KeepRows: true, SampleSec: 60, OSBootID: "boot-a"})
			if err != nil {
				t.Fatal(err)
			}
			r.SetCal()
			r.SetPoint(13, 76, 1.0)
			r.Advance(6 * 3600)
			lost := r.Core.Snapshot().Now
			r.O.OSBootID = c.nextBoot
			if err := r.PowerLoss(2*3600, 60); err != nil {
				t.Fatal(err)
			}
			r.Advance(4 * 3600)
			s := r.Core.Snapshot()
			var got *api.Alarm
			for _, list := range [][]api.Alarm{s.Active, s.History} {
				for i := range list {
					if list[i].Code == "POWER_OUTAGE" || list[i].Code == "CONTROLLER_RESTARTED" {
						got = &list[i]
					}
				}
			}
			if got == nil || got.Code != c.want {
				t.Fatalf("want %s, got %+v", c.want, got)
			}
			if d := got.Since.Sub(lost); d < -2*time.Minute || d > 2*time.Minute {
				t.Errorf("dated %v, %v from the loss; want the loss", got.Since, d)
			}
			if !got.Cleared.IsZero() && (got.DurationSec < 7000 || got.DurationSec > 7600) {
				t.Errorf("history duration %.0f s for a 7200 s outage", got.DurationSec)
			}
			if s.Health.LastOutage.Cause == "" {
				t.Error("outage record has no cause")
			}
		})
	}
}

// The re-test of defect 6: an outage still unacknowledged at the next one took that
// one's duration, and a later restart re-dated it to its return.
func TestEachOutageKeepsItsOwnDateAndLengthAcrossLaterOnes(t *testing.T) {
	r, err := sim.NewRun(sim.Options{Plant: sim.BasePlant(), KeepRows: true, SampleSec: 60, OSBootID: "boot-a"})
	if err != nil {
		t.Fatal(err)
	}
	r.SetCal()
	r.SetPoint(13, 76, 1.0)
	r.Advance(6 * 3600)
	type cut struct {
		boot string
		dur  float64
		lost time.Time
	}
	cuts := []cut{{"boot-b", 3 * 3600, time.Time{}}, {"boot-b", 1800, time.Time{}}, {"boot-b", 600, time.Time{}}}
	for i := range cuts {
		cuts[i].lost = r.Core.Snapshot().Now
		r.O.OSBootID = cuts[i].boot
		if err := r.PowerLoss(cuts[i].dur, 60); err != nil {
			t.Fatal(err)
		}
		r.Advance(3 * 3600) // nobody acknowledges anything
	}
	s := r.Core.Snapshot()
	var got []api.Alarm
	for _, list := range [][]api.Alarm{s.History, s.Active} {
		for _, a := range list {
			if a.Code == "POWER_OUTAGE" || a.Code == "CONTROLLER_RESTARTED" {
				got = append(got, a)
			}
		}
	}
	if len(got) != len(cuts) {
		t.Fatalf("%d outage alarms for %d outages: %+v", len(got), len(cuts), got)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Since.Before(got[j].Since) })
	want := []string{"POWER_OUTAGE", "CONTROLLER_RESTARTED", "CONTROLLER_RESTARTED"}
	for i, a := range got {
		if a.Code != want[i] {
			t.Errorf("outage %d is %s, want %s", i, a.Code, want[i])
		}
		if d := a.Since.Sub(cuts[i].lost); d < -2*time.Minute || d > 2*time.Minute {
			t.Errorf("outage %d dated %v from its loss", i, d)
		}
		if !a.Cleared.IsZero() && math.Abs(a.DurationSec-cuts[i].dur) > 300 {
			t.Errorf("outage %d of %.0f s recorded as %.0f s", i, cuts[i].dur, a.DurationSec)
		}
	}
}

// The re-test of defect 8: taking 3 kg out made loss jump from 5.6 % to 7.0 %, crediting
// the remaining product with water the removed pieces had lost.
func TestWeightLossDoesNotJumpWhenProductIsRemoved(t *testing.T) {
	r := newRun(t)
	sp := api.Setpoints{TempC: 13, RHPct: 76, AirMS: 1.0}
	rc := api.Recipe{ID: "t", Name: "t", Phases: []api.PhaseSpec{{Name: "dry", Setpoint: sp, Duration: 10 * 24 * time.Hour}}}
	if err := r.Core.Do(api.Command{Kind: api.CmdSaveRecipe, Recipe: &rc}); err != nil {
		t.Fatal(err)
	}
	if err := r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: "t"}); err != nil {
		t.Fatal(err)
	}
	r.Advance(4 * 86400)
	before := r.Core.Snapshot().Program.WeightLossPct
	r.Plant.DoorIsOpen = true
	r.Advance(60)
	r.Plant.ProdKg *= 0.7 // a third of the pieces taken out
	r.Plant.DoorIsOpen = false
	r.Advance(1800)
	after := r.Core.Snapshot().Program.WeightLossPct
	if math.Abs(after-before) > 0.3 {
		t.Fatalf("loss %.2f %% before removing product, %.2f %% after: it must not jump", before, after)
	}
}

// Re-test of defect 6, follow-ups: a gap that comes before the last outage's record closed
// must not lose that outage's temperature, and an orderly restart does not take an
// unacknowledged outage off the list.
func TestOutagePeakKeptAcrossAQuickSecondGapAndAnOrderlyRestart(t *testing.T) {
	r, err := sim.NewRun(sim.Options{Plant: sim.BasePlant(), KeepRows: true, SampleSec: 60, OSBootID: "boot-a"})
	if err != nil {
		t.Fatal(err)
	}
	r.SetCal()
	r.SetPoint(13, 76, 1.0)
	r.Advance(6 * 3600)
	r.O.OSBootID = "boot-b"
	// Long enough to warm, short enough to stay under the critical line (15.6 °C), so
	// nothing but the return reading carries the peak.
	if err := r.PowerLoss(1200, 60); err != nil {
		t.Fatal(err)
	}
	r.Advance(60) // the first reading is in; the record is still open
	r.O.OSBootID = "boot-c"
	if err := r.PowerLoss(120, 60); err != nil {
		t.Fatal(err)
	}
	r.Advance(3600)
	s := r.Core.Snapshot()
	var first *api.Alarm
	for i, a := range s.History {
		if a.Code == "POWER_OUTAGE" && (first == nil || a.Since.Before(first.Since)) {
			first = &s.History[i]
		}
	}
	if first == nil {
		t.Fatal("the first outage is not in history")
	}
	if first.Peak < 13.2 || first.Severity == api.SevCritical {
		t.Errorf("first outage: %s, peak %.1f °C; want a warning with the temperature on return", first.Severity, first.Peak)
	}
	listed := func() bool {
		for _, a := range r.Core.Snapshot().Active {
			if a.Code == "POWER_OUTAGE" {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("the second outage is not listed")
	}
	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	r.Advance(600)
	if !listed() {
		t.Error("an orderly restart took an unacknowledged outage off the list")
	}
}
