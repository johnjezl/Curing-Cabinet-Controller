package core_test

import (
	"testing"

	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

// Integration defect 1, diagnosis half (ADR 0114): each failed part is named, within a
// stated time, and a healthy part is not accused in a hot room or while recovering.
func TestEachFailedPartIsNamedAndNoneAccusedFalsely(t *testing.T) {
	equip := []string{"HEAT_STUCK_ON", "HUMID_STUCK_ON", "COOL_NO_EFFECT", "COOL_STUCK_ON", "HEAT_NO_EFFECT", "HUMID_NO_EFFECT"}
	for _, c := range []struct {
		name   string
		setT   float64
		setRH  float64
		inject func(r *sim.Run)
		want   string
		within float64 // minutes
	}{
		{"heater stuck at 13", 13, 76, func(r *sim.Run) { r.Plant.Inject(sim.DevHeat, sim.FaultRelayStuckOn, 0) }, "HEAT_STUCK_ON", 180},
		// At a heating setpoint the compressor beats the heater, so only the duty it costs
		// shows, once the cabinet has steadied.
		{"heater stuck at 24", 24, 90, func(r *sim.Run) { r.Plant.Inject(sim.DevHeat, sim.FaultRelayStuckOn, 0) }, "HEAT_STUCK_ON", 360},
		{"humidifier stuck at 13", 13, 76, func(r *sim.Run) { r.Plant.Inject(sim.DevHumid, sim.FaultRelayStuckOn, 0) }, "HUMID_STUCK_ON", 120},
		{"humidifier stuck at 24", 24, 90, func(r *sim.Run) { r.Plant.Inject(sim.DevHumid, sim.FaultRelayStuckOn, 0) }, "HUMID_STUCK_ON", 120},
		{"compressor does not cool", 13, 76, func(r *sim.Run) { r.Plant.Inject(sim.DevCompressor, sim.FaultNoCool, 0) }, "COOL_NO_EFFECT", 90},
		{"room 12 warmer", 13, 76, func(r *sim.Run) { r.Plant.AmbientOffset = 12 }, "", 0},
		{"room 10 colder", 13, 76, func(r *sim.Run) { r.Plant.AmbientOffset = -10 }, "", 0},
		{"recovering after a compressor repair in a hot room", 13, 76, func(r *sim.Run) {
			r.Plant.AmbientOffset = 12
			r.Plant.Inject(sim.DevCompressor, sim.FaultNoCool, 0)
			r.Advance(6 * 3600)
			r.Plant.Inject(sim.DevCompressor, sim.FaultNone, 0)
			r.Evs = nil
		}, "", 0},
		{"recovering after an output write fault", 13, 76, func(r *sim.Run) {
			r.Plant.Inject(sim.DevOutputs, sim.FaultWriteErr, 0)
			r.Advance(2 * 3600)
			r.Plant.Inject(sim.DevOutputs, sim.FaultNone, 0)
			r.Evs = nil
		}, "", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRun(t)
			r.SetPoint(c.setT, c.setRH, 1.0)
			r.Advance(2 * 86400)
			r.Evs = nil
			t0 := r.Clock.VT
			c.inject(r)
			r.Advance(12 * 3600)
			found := false
			for _, e := range r.Evs {
				if e.Kind != "alarm" {
					continue
				}
				if e.Code == c.want {
					if (e.T-t0)/60 > c.within {
						t.Errorf("%s after %.0f min, want within %.0f", e.Code, (e.T-t0)/60, c.within)
					}
					found = true
					continue
				}
				for _, w := range equip {
					if e.Code == w {
						t.Errorf("wrong part accused: %s at %.0f min", e.Code, (e.T-t0)/60)
					}
				}
			}
			if c.want != "" && !found {
				t.Errorf("%s never raised", c.want)
			}
		})
	}
}
