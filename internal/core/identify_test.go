package core_test

import (
	"testing"

	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

// Integration defect 1, diagnosis half (ADR 0114): each failed part is named, within a
// stated time, and a healthy part is not accused in a hot room or while recovering.
func TestEachFailedPartIsNamedAndNoneAccusedFalsely(t *testing.T) {
	equip := []string{"HEAT_STUCK_ON", "HUMID_STUCK_ON", "COOL_NO_EFFECT", "COOL_STUCK_ON", "HEAT_NO_EFFECT", "HUMID_NO_EFFECT",
		"RH_NOT_FALLING"} // it points at a humidifier relay stuck closed
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
		// Next fix round: a closed compressor relay against a heater that cannot keep up
		// was never named, and the heater was accused; at a heating setpoint too.
		{"compressor relay stuck closed at 13", 13, 76, func(r *sim.Run) { r.Plant.Inject(sim.DevCool, sim.FaultRelayStuckOn, 0) }, "COOL_STUCK_ON", 60},
		{"compressor relay stuck closed at 24", 24, 90, func(r *sim.Run) { r.Plant.Inject(sim.DevCool, sim.FaultRelayStuckOn, 0) }, "COOL_STUCK_ON", 60},
		{"humidifier empty in a cold room at 24", 24, 90, func(r *sim.Run) {
			r.Plant.AmbientOffset = -10
			r.Plant.Inject(sim.DevHumidifier, sim.FaultEmpty, 0)
		}, "", 0},
		// A cabinet gone cold saturates its air too; that is not a humidifier.
		{"heater stuck off in a cold room", 13, 76, func(r *sim.Run) {
			r.Plant.AmbientOffset = -10
			r.Plant.Inject(sim.DevHeat, sim.FaultRelayStuckOff, 0)
		}, "HEAT_NO_EFFECT", 720},
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
			// The part is still broken at the end: the alarm must still stand.
			if c.want != "" && found {
				standing := false
				for _, a := range r.Core.Snapshot().Active {
					standing = standing || a.Code == c.want
				}
				if !standing {
					t.Errorf("%s cleared while the fault was still there", c.want)
				}
			}
		})
	}
}

// Integration defect 1, re-test: once the sensor cross-check has its three days of
// history, a warmer room, a compressor repaired in a hot room, or an output fault must not
// accuse the healthy sensor. A reading drifting low, the warm direction, is still caught
// within hours (ADR 0109, 0113).
func TestNoSensorAccusedAfterARoomChangeOrARepair(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(r *sim.Run)
	}{
		{"room 12 warmer for 10 h", func(r *sim.Run) {
			r.Plant.AmbientOffset = 12
			r.Advance(10 * 3600)
			r.Plant.AmbientOffset = 0
		}},
		{"room 6 warmer for 10 h", func(r *sim.Run) {
			r.Plant.AmbientOffset = 6
			r.Advance(10 * 3600)
			r.Plant.AmbientOffset = 0
		}},
		{"compressor repaired in a hot room", func(r *sim.Run) {
			r.Plant.AmbientOffset = 12
			r.Advance(4.8 * 3600)
			r.Plant.Inject(sim.DevCompressor, sim.FaultNoCool, 0)
			r.Advance(6 * 3600)
			r.Plant.Inject(sim.DevCompressor, sim.FaultNone, 0)
			r.Advance(10 * 3600)
			r.Plant.AmbientOffset = 0
		}},
		{"output fault for an hour", func(r *sim.Run) {
			r.Plant.Inject(sim.DevOutputs, sim.FaultWriteErr, 0)
			r.Advance(3600)
			r.Plant.Inject(sim.DevOutputs, sim.FaultNone, 0)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRun(t)
			r.SetPoint(13, 76, 1.0)
			r.Advance(4 * 86400)
			r.Evs = nil
			t0 := r.Clock.VT
			c.run(r)
			r.Advance(4 * 86400)
			for _, e := range r.Evs {
				if e.Kind == "alarm" && (e.Code == "TEMP_READING_DISAGREES" || e.Code == "RH_READING_DISAGREES") {
					t.Errorf("healthy sensor accused: %s at %.1f h", e.Code, (e.T-t0)/3600)
				}
			}
		})
	}
	t.Run("reading drifting low is still caught", func(t *testing.T) {
		r := newRun(t)
		r.SetPoint(13, 76, 1.2)
		r.Advance(5 * 86400)
		r.Evs = nil
		t0 := r.Clock.VT
		r.Plant.Inject(sim.DevTemp, sim.FaultDrift, -1)
		r.Advance(12 * 3600)
		for _, e := range r.Evs {
			if e.Kind == "alarm" && e.Code == "TEMP_READING_DISAGREES" {
				if h := (e.T - t0) / 3600; h > 8 {
					t.Errorf("caught after %.1f h, want within 8", h)
				}
				return
			}
		}
		t.Error("a reading drifting low at 1 °C/h was not caught in 12 h")
	})
}
