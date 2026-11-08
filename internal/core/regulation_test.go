package core_test

import (
	"math"
	"testing"

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
		h0 := r.Plant.HeatCycles()
		r.Advance(ph.days * 86400)
		var n, in1, heat, sumT, sumRH float64
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
		t.Logf("%-9s set %4.1f/%2.0f: mean %.2f °C %.1f %%, %.0f %% of samples within 1 °C, %d of %d hourly means out of band, heater %.0f %% of samples, %.0f closures/day",
			ph.name, ph.t, ph.rh, mT, mRH, 100*in1/n, hoursOut, hours, 100*heat/n, closures)
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
