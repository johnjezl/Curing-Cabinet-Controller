package core_test

import (
	"fmt"
	"math"
	"sort"
	"os"
	"strings"
	"testing"

	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

// Exploration only (EXPLORE=1).
func TestExploreDiagnosis(t *testing.T) {
	if os.Getenv("EXPLORE") == "" {
		t.Skip()
	}
	type sc struct {
		name   string
		setT   float64
		setRH  float64
		inject func(r *sim.Run)
	}
	cases := []sc{
		{"heat stuck on @13", 13, 76, func(r *sim.Run) { r.Plant.Inject(sim.DevHeat, sim.FaultRelayStuckOn, 0) }},
		{"heat stuck on @24", 24, 90, func(r *sim.Run) { r.Plant.Inject(sim.DevHeat, sim.FaultRelayStuckOn, 0) }},
		{"humid stuck on @13", 13, 76, func(r *sim.Run) { r.Plant.Inject(sim.DevHumid, sim.FaultRelayStuckOn, 0) }},
		{"humid stuck on @24", 24, 90, func(r *sim.Run) { r.Plant.Inject(sim.DevHumid, sim.FaultRelayStuckOn, 0) }},
		{"compressor no-cool @13", 13, 76, func(r *sim.Run) { r.Plant.Inject(sim.DevCompressor, sim.FaultNoCool, 0) }},
		{"cool relay stuck off @13", 13, 76, func(r *sim.Run) { r.Plant.Inject(sim.DevCool, sim.FaultRelayStuckOff, 0) }},
		{"room +12 @13", 13, 76, func(r *sim.Run) { r.Plant.AmbientOffset = 12 }},
		{"room +6 @13", 13, 76, func(r *sim.Run) { r.Plant.AmbientOffset = 6 }},
		{"room -10 @13", 13, 76, func(r *sim.Run) { r.Plant.AmbientOffset = -10 }},
		{"room +12 @24", 24, 90, func(r *sim.Run) { r.Plant.AmbientOffset = 12 }},
		{"warm load 5kg@25 @13", 13, 76, func(r *sim.Run) { r.Plant.LoadProduct(5, 25) }},
		{"no fault @13", 13, 76, func(r *sim.Run) {}},
		{"no fault @24", 24, 90, func(r *sim.Run) {}},
	}
	only := os.Getenv("CASE")
	for _, c := range cases {
		if only != "" && !strings.Contains(c.name, only) {
			continue
		}
		r, _ := sim.NewRun(sim.Options{Plant: sim.BasePlant(), KeepRows: true, SampleSec: 60})
		r.SetCal()
		r.SetPoint(c.setT, c.setRH, 1.0)
		r.Advance(3 * 86400)
		r.Evs = nil
		r.Rows = nil
		t0 := r.Clock.VT
		c.inject(r)
		r.Advance(16 * 3600)
		var line []string
		for _, e := range r.Evs {
			if e.Kind == "alarm" || e.Kind == "clear" || e.Kind == "escalate" {
				line = append(line, fmt.Sprintf("%s %s@%.0fm", e.Kind, e.Code, (e.T-t0)/60))
			}
		}
		maxT := -100.0
		for _, x := range r.Rows {
			if x.TrueT > maxT {
				maxT = x.TrueT
			}
		}
		fmt.Printf("== %-24s max air %.1f\n   %s\n", c.name, maxT, strings.Join(line, ", "))
	}
}

func TestExploreStuckHeatTrace(t *testing.T) {
	if os.Getenv("EXPLORE") == "" {
		t.Skip()
	}
	r, _ := sim.NewRun(sim.Options{Plant: sim.BasePlant(), KeepRows: true, SampleSec: 60})
	r.SetCal()
	r.SetPoint(13, 76, 1.0)
	r.Advance(3 * 86400)
	r.Plant.Inject(sim.DevHeat, sim.FaultRelayStuckOn, 0)
	t0 := r.Clock.VT
	for i := 0; i < 8*3600; i++ {
		r.Tick()
		if i%300 == 0 {
			v := r.Core.Probe()
			s := r.Core.Snapshot()
			var act []string
			for _, a := range s.Active {
				act = append(act, a.Code)
			}
			why := ""
			for _, o := range s.Outputs.Status {
				if o.Output == "cool" {
					why = o.Kind + ": " + o.Reason
				}
			}
			fmt.Printf("%5.0fm air %5.2f read %5.2f cool %v heatcmd %v | %s | %s\n", (r.Clock.VT-t0)/60, r.Plant.TAir, v.Temp, v.Cool, v.Heat, why, strings.Join(act, ","))
		}
	}
}

// Air rise rate (reading, °C/min over 120 s) while compressor and heater have both
// been off for at least 240 s.
func idleRiseRates(r *sim.Run, secs int) (rates []float64) {
	var offC, offH float64
	ring := make([]float64, 121)
	for i := 0; i < secs; i++ {
		r.Tick()
		v := r.Core.Probe()
		if v.Cool {
			offC = 0
		} else {
			offC++
		}
		if v.Heat {
			offH = 0
		} else {
			offH++
		}
		ring[i%121] = v.Temp
		if offC >= 240 && offH >= 240 && i >= 120 && !v.Defrost && v.TempQ == "ok" && i%30 == 0 {
			rates = append(rates, (v.Temp-ring[(i+1)%121])/2)
		}
	}
	return
}

func TestExploreHeatSignature(t *testing.T) {
	if os.Getenv("EXPLORE") == "" {
		t.Skip()
	}
	for _, sp := range []struct{ t, rh float64 }{{13, 76}, {24, 90}, {4, 85}} {
		var normMax, hotMax, stuckMed, stuckMin []float64
		for i, p := range sim.Sweep(36, 1) {
			_ = i
			run := func(f func(r *sim.Run)) []float64 {
				r, _ := sim.NewRun(sim.Options{Plant: p, SampleSec: 600})
				r.SetCal()
				r.SetPoint(sp.t, sp.rh, 1.0)
				r.Advance(86400)
				f(r)
				return idleRiseRates(r, 12*3600)
			}
			n := run(func(r *sim.Run) {})
			h := run(func(r *sim.Run) { r.Plant.AmbientOffset = 14 })
			s := run(func(r *sim.Run) { r.Plant.Inject(sim.DevHeat, sim.FaultRelayStuckOn, 0) })
			mx := func(v []float64) float64 {
				m := -9.0
				for _, x := range v {
					if x > m {
						m = x
					}
				}
				return m
			}
			normMax = append(normMax, mx(n))
			hotMax = append(hotMax, mx(h))
			if len(s) > 0 {
				stuckMed = append(stuckMed, pct(s, 50))
				stuckMin = append(stuckMin, pct(s, 10))
			} else {
				stuckMed = append(stuckMed, -9)
				stuckMin = append(stuckMin, -9)
			}
		}
		fmt.Printf("== %.0f C: normal max rise/min: max over plants %.3f p90 %.3f | hot room +14 max %.3f p90 %.3f | stuck heater median-rate min over plants %.3f p10 %.3f, p10-rate min %.3f (plants with no idle window: %d)\n",
			sp.t, pct(normMax, 100), pct(normMax, 90), pct(hotMax, 100), pct(hotMax, 90), pct(stuckMed, 0), pct(stuckMed, 10), pct(stuckMin, 0), countNeg(stuckMed))
	}
}

func countNeg(v []float64) int {
	n := 0
	for _, x := range v {
		if x <= -9 {
			n++
		}
	}
	return n
}

func pct(v []float64, p float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s) == 0 {
		return math.NaN()
	}
	return s[int(p/100*float64(len(s)-1))]
}
