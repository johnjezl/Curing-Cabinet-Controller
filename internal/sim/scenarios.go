package sim

import (
	"fmt"
	"io"
	"sort"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
)

// Scenario is a scripted run.
type Scenario struct {
	Name        string
	Kind        string
	T, RH       float64
	Air         float64
	Duration    float64
	M0, M1      float64
	D0, D1      float64
	Expect      []string
	Notes       string
	RecoverFrom string
	Ref         int // 0 core, 1 reference with AC 1 min, 4 reference with AC 4 min
	RefDuty     bool
	Mutate      func(*Params)
	Actions     []Action
	Init        func(*Run)
	// SampleSec is the row period; 0 means 5 s. Long runs use more.
	SampleSec float64
}

// Action happens at a virtual time.
type Action struct {
	At float64
	Do func(*Run)
}

const h = 3600.0

func steady(name string, t, rh, air float64) Scenario {
	return Scenario{Name: name, Kind: "steady", T: t, RH: rh, Air: air, Duration: 42 * h, M0: 18 * h, M1: 42 * h}
}

// door builds a door opening. Warm load adds product at the same moment.
func door(name string, t, rh, air, openSec float64) Scenario {
	at := 18.0 * h
	return Scenario{Name: name, Kind: "disturbance", T: t, RH: rh, Air: air, Duration: 26 * h, M0: 12 * h, M1: 17.9 * h, D0: at, D1: at + openSec,
		Actions: []Action{
			{at, func(r *Run) { r.Plant.DoorIsOpen = true; r.Note("door opened") }},
			{at + openSec, func(r *Run) { r.Plant.DoorIsOpen = false; r.Note("door closed") }},
		}}
}

func warmLoad(name string, t, rh, air, kg, tempC float64) Scenario {
	at := 18.0 * h
	return Scenario{Name: name, Kind: "disturbance", T: t, RH: rh, Air: air, Duration: 42 * h, M0: 12 * h, M1: 17.9 * h, D0: at, D1: at + 90,
		Notes: "90 s door opening, then product at the stated temperature",
		Actions: []Action{
			{at, func(r *Run) {
				r.Plant.DoorIsOpen = true
				r.Plant.LoadProduct(kg, tempC)
				r.Note(fmt.Sprintf("door opened, %.0f kg loaded at %.1f C", kg, tempC))
			}},
			{at + 90, func(r *Run) { r.Plant.DoorIsOpen = false; r.Note("door closed") }},
		}}
}

func defrostRun(name string, t, rh, air float64) Scenario {
	// Operator-started so that the disturbance has a known time; the run also
	// contains scheduled ones. D1 is the expected end.
	at := 24.0 * h
	return Scenario{Name: name, Kind: "disturbance", T: t, RH: rh, Air: air, Duration: 48 * h, M0: 12 * h, M1: 23.9 * h, D0: at, D1: at + 30*60, RecoverFrom: "defrost_end",
		Notes: "operator-started defrost at D0; recovery counted from its logged end",
		Actions: []Action{{at, func(r *Run) {
			if r.Core != nil {
				_ = r.Core.Do(cmdDefrost())
			}
			r.Note("defrost requested")
		}}}}
}

func fault(name string, dev Device, kind FaultKind, param float64, expect ...string) Scenario {
	return faultAt(name, 13, 76, 1.2, 8*h, dev, kind, param, expect...)
}

func faultAt(name string, t, rh, air, dur float64, dev Device, kind FaultKind, param float64, expect ...string) Scenario {
	at := 14.0 * h
	return Scenario{Name: name, Kind: "fault", T: t, RH: rh, Air: air, Duration: at + dur + 9*h, M0: 8 * h, M1: 13.9 * h, D0: at, D1: at + dur, Expect: expect,
		Actions: []Action{
			{at, func(r *Run) { r.Plant.Inject(dev, kind, param); r.Note(name + " injected") }},
			{at + dur, func(r *Run) { r.Plant.Inject(dev, FaultNone, 0); r.Note(name + " repaired") }},
		}}
}

// driftFault runs long enough first for the plant-response checks to have a history,
// then a person checks against a reference at repair, which is what clears them.
func driftFault(name string, t, rh, air, days float64, dev Device, ratePerHour float64, expect ...string) Scenario {
	at := 5 * 24 * h
	return Scenario{Name: name, Kind: "fault", T: t, RH: rh, Air: air, Duration: at + days*24*h + 3*h, M0: 2 * 24 * h, M1: at - 1, D0: at, D1: at + days*24*h, Expect: expect, SampleSec: 30,
		Actions: []Action{
			{at, func(r *Run) { r.Plant.Inject(dev, FaultDrift, ratePerHour); r.Note(name + " injected") }},
			{at + days*24*h, func(r *Run) {
				r.Plant.Inject(dev, FaultNone, 0)
				r.Note(name + " repaired; operator checks against a reference")
				r.RefCheck()
			}},
		}}
}

func longRun(name string, t, rh, air float64) Scenario {
	const days = 14
	sc := Scenario{Name: name, Kind: "longrun", T: t, RH: rh, Air: air, Duration: days * 24 * h, M0: 2 * 24 * h, M1: days * 24 * h, SampleSec: 30}
	// A door opening every 3 days, and power cuts of 10 min, 1 h and 6 h.
	for d := 3.0; d < days; d += 3 {
		at := d * 24 * h
		sc.Actions = append(sc.Actions,
			Action{at, func(r *Run) { r.Plant.DoorIsOpen = true; r.Note("door opened") }},
			Action{at + 60, func(r *Run) { r.Plant.DoorIsOpen = false; r.Note("door closed") }})
	}
	for i, c := range []struct{ day, dur float64 }{{4.4, 600}, {7.7, 3600}, {11.2, 6 * 3600}} {
		_ = i
		c := c
		sc.Actions = append(sc.Actions, Action{c.day * 24 * h, func(r *Run) { _ = r.PowerLoss(c.dur, 300) }})
	}
	return sc
}

func sampled(s Scenario, sec float64) Scenario { s.SampleSec = sec; return s }

func withMutate(s Scenario, f func(*Params)) Scenario { s.Mutate = f; return s }

func withRef(s Scenario, ac int, duty bool) Scenario {
	s.Ref = ac
	s.RefDuty = duty
	s.Name += fmt.Sprintf("__ref%d", ac)
	if duty {
		s.Name += "_duty"
	}
	s.Expect = nil
	return s
}

// Scenarios returns all named scenarios for the sweep and the base plant.
func Scenarios() []Scenario {
	const (
		tempS = core.CodeTempSuspect
		rhS   = core.CodeRHSuspect
		clim  = core.CodeClimateFault
	)
	tdis := []string{core.CodeTempDisagree, core.CodeTempDrift, core.CodeDryingShift, core.CodeTempHigh, core.CodeTempHighCrit, core.CodeTempLow, core.CodeTempAbsLow}
	tcold := append(append([]string{}, tdis...), core.CodeCoolStuck)
	rdis := []string{core.CodeRHDisagree, core.CodeRHDrift, core.CodeDryingShift, core.CodeRHHigh, core.CodeRHLow}
	s := []Scenario{
		steady("steady_cure_13c_76rh", 13, 76, 1.2),
		steady("steady_early_14c_82rh", 14, 82, 1.2),
		steady("steady_equalize_4c_85rh", 4, 85, 1.2),
		steady("steady_beef_1p5c_82rh", 1.5, 82, 1.2),
		steady("steady_ferment_24c_90rh", 24, 90, 1.2),
		steady("steady_warm_20c_70rh", 20, 70, 1.2),
		steady("steady_biltong_25c_55rh", 25, 55, 1.5),
		{Name: "pulldown_empty_4c", Kind: "pulldown", T: 4, RH: 85, Air: 1.2, Duration: 6 * h, M0: 3 * h, M1: 6 * h,
			Mutate: func(p *Params) { p.ProductKg = 0 }},
		{Name: "pulldown_empty_13c", Kind: "pulldown", T: 13, RH: 76, Air: 1.2, Duration: 6 * h, M0: 3 * h, M1: 6 * h,
			Mutate: func(p *Params) { p.ProductKg = 0 }},
		door("door_30s_13c", 13, 76, 1.2, 30),
		door("door_2min_13c", 13, 76, 1.2, 120),
		door("door_10min_13c", 13, 76, 1.2, 600),
		door("door_30s_4c", 4, 85, 1.2, 30),
		door("door_2min_4c", 4, 85, 1.2, 120),
		door("door_2min_24c", 24, 90, 1.2, 120),
		warmLoad("warmload_13c_10kg_20c", 13, 76, 1.2, 10, 20),
		warmLoad("warmload_4c_10kg_12c", 4, 85, 1.2, 10, 12),
		defrostRun("defrost_13c", 13, 76, 1.2),
		defrostRun("defrost_4c", 4, 85, 1.2),
		defrostRun("defrost_1p5c", 1.5, 82, 1.2),

		// Sensor faults.
		faultAt("fault_climate_dead_13c", 13, 76, 1.2, 24*h, DevClimate, FaultDead, 0, clim),
		faultAt("fault_climate_dead_4c", 4, 85, 1.2, 24*h, DevClimate, FaultDead, 0, clim),
		faultAt("fault_climate_dead_1p5c", 1.5, 82, 1.2, 24*h, DevClimate, FaultDead, 0, clim),
		faultAt("fault_climate_dead_24c", 24, 90, 1.2, 24*h, DevClimate, FaultDead, 0, clim),
		sampled(faultAt("fault_climate_dead_7d_1p5c", 1.5, 82, 1.2, 7*24*h, DevClimate, FaultDead, 0, clim), 30),
		sampled(faultAt("fault_climate_dead_7d_4c", 4, 85, 1.2, 7*24*h, DevClimate, FaultDead, 0, clim), 30),
		sampled(faultAt("fault_climate_dead_7d_13c", 13, 76, 1.2, 7*24*h, DevClimate, FaultDead, 0, clim), 30),
		sampled(faultAt("fault_climate_dead_7d_24c", 24, 90, 1.2, 7*24*h, DevClimate, FaultDead, 0, clim), 30),
		fault("fault_climate_badread", DevClimate, FaultBadRead, 0, clim),
		fault("fault_temp_stuck", DevTemp, FaultStuck, 0, tempS),
		fault("fault_rh_stuck", DevRH, FaultStuck, 0, rhS),
		fault("fault_temp_spikes", DevTemp, FaultSpike, 0.2, core.CodeTempErratic, tempS),
		fault("fault_rh_spikes", DevRH, FaultSpike, 0.2, core.CodeRHErratic, rhS),
		fault("fault_temp_implausible", DevTemp, FaultRange, 85, tempS),
		fault("fault_rh_implausible", DevRH, FaultRange, 0, rhS),
		driftFault("fault_temp_drift_up_fast_13c", 13, 76, 1.2, 1, DevTemp, 1.0, tdis...),
		driftFault("fault_temp_drift_up_slow_13c", 13, 76, 1.2, 10, DevTemp, 0.02, tdis...),
		driftFault("fault_temp_drift_down_fast_13c", 13, 76, 1.2, 1, DevTemp, -1.0, tdis...),
		driftFault("fault_temp_drift_down_mid_13c", 13, 76, 1.2, 5, DevTemp, -0.1, tdis...),
		driftFault("fault_temp_drift_down_slow_13c", 13, 76, 1.2, 10, DevTemp, -0.02, tdis...),
		driftFault("fault_temp_drift_down_fast_4c", 4, 85, 1.2, 1, DevTemp, -1.0, tdis...),
		driftFault("fault_temp_drift_down_slow_4c", 4, 85, 1.2, 10, DevTemp, -0.02, tdis...),
		driftFault("fault_temp_drift_up_fast_4c", 4, 85, 1.2, 1, DevTemp, 1.0, tcold...),
		driftFault("fault_temp_drift_up_slow_4c", 4, 85, 1.2, 10, DevTemp, 0.02, tcold...),
		driftFault("fault_temp_drift_up_fast_1p5c", 1.5, 82, 1.2, 1, DevTemp, 1.0, tcold...),
		driftFault("fault_temp_drift_up_slow_1p5c", 1.5, 82, 1.2, 10, DevTemp, 0.02, tcold...),
		driftFault("fault_temp_drift_down_fast_1p5c", 1.5, 82, 1.2, 1, DevTemp, -1.0, tdis...),
		driftFault("fault_rh_drift_up_slow_13c", 13, 76, 1.2, 10, DevRH, 0.05, rdis...),
		driftFault("fault_rh_drift_down_slow_13c", 13, 76, 1.2, 10, DevRH, -0.05, rdis...),
		driftFault("fault_rh_drift_down_fast_13c", 13, 76, 1.2, 2, DevRH, -0.5, rdis...),
		fault("fault_air_dead", DevAir, FaultDead, 0, core.CodeAirFault),
		fault("fault_air_stuck", DevAir, FaultStuck, 0, core.CodeAirSuspect),
		fault("fault_air_spikes", DevAir, FaultSpike, 0.3, core.CodeAirSuspect),
		fault("fault_air_implausible", DevAir, FaultRange, 40, core.CodeAirFault, core.CodeAirSuspect),
		fault("fault_scale_dead", DevScale, FaultDead, 0, core.CodeScaleFault),
		fault("fault_scale_stuck", DevScale, FaultStuck, 0, core.CodeScaleSuspect),
		fault("fault_scale_spikes", DevScale, FaultSpike, 0.5, core.CodeScaleSuspect),
		fault("fault_scale_saturated", DevScale, FaultRange, 8388607, core.CodeScaleFault),
		faultAt("fault_scale_drift_up", 13, 76, 1.2, 48*h, DevScale, FaultDrift, 100, core.CodeScaleSuspect),

		// Fan and duct.
		fault("fault_fan_stall", DevFan, FaultStall, 0, core.CodeFanStall),
		fault("fault_tach_dead", DevTach, FaultDead, 0, core.CodeFanTach),
		fault("fault_tach_wire", DevTach, FaultStuck, 0, core.CodeFanTach),
		fault("fault_duct_blocked", DevDuct, FaultBlocked, 0, core.CodeAirBlocked),

		// Actuators.
		fault("fault_cool_stuck_on", DevCool, FaultRelayStuckOn, 0, core.CodeCoolStuck, core.CodeTempLow),
		faultAt("fault_cool_stuck_on_4c", 4, 85, 1.2, 8*h, DevCool, FaultRelayStuckOn, 0, core.CodeCoolStuck, core.CodeTempLow, core.CodeTempAbsLow),
		faultAt("fault_cool_stuck_on_1p5c", 1.5, 82, 1.2, 8*h, DevCool, FaultRelayStuckOn, 0, core.CodeCoolStuck, core.CodeTempLow, core.CodeTempAbsLow),
		fault("fault_cool_stuck_off", DevCool, FaultRelayStuckOff, 0, core.CodeCoolFail, core.CodeTempHigh, core.CodeTempHighCrit),
		fault("fault_compressor_no_cooling", DevCompressor, FaultNoCool, 0, core.CodeCoolFail, core.CodeTempHigh, core.CodeTempHighCrit),
		fault("fault_heat_stuck_on", DevHeat, FaultRelayStuckOn, 0, core.CodeHeatStuck, core.CodeCoolFail, core.CodeTempHigh, core.CodeTempHighCrit),
		faultAt("fault_heat_stuck_off_24c", 24, 90, 1.2, 8*h, DevHeat, FaultRelayStuckOff, 0, core.CodeHeatFail, core.CodeTempLow),
		fault("fault_humid_stuck_on", DevHumid, FaultRelayStuckOn, 0, core.CodeHumidStuck, core.CodeRHHigh),
		faultAt("fault_humid_empty_4c", 4, 85, 1.2, 12*h, DevHumidifier, FaultEmpty, 0, core.CodeHumidFail, core.CodeRHLow),
		fault("fault_outputs_write_error", DevOutputs, FaultWriteErr, 0, core.CodeOutputFault),
		fault("fault_door_switch_dead", DevDoorSwitch, FaultDead, 0, core.CodeDoorSwitch),

		// An earlier critical is still active and unacknowledged, with an outage on the
		// list, and the contact must still show the later fault.
		{Name: "alarm_later_fault_gets_through", Kind: "fault", T: 13, RH: 76, Air: 1.2, Duration: 30 * h, M0: 8 * h, M1: 13.9 * h, D0: 22 * h, D1: 26 * h, Expect: []string{core.CodeClimateFault},
			Actions: []Action{
				{14 * h, func(r *Run) { _ = r.PowerLoss(3600, 60); r.Note("earlier: outage not acknowledged") }},
				{18 * h, func(r *Run) {
					r.Plant.Inject(DevDuct, FaultBlocked, 0)
					r.Note("earlier: duct blocked, critical, not acknowledged")
				}},
				{22 * h, func(r *Run) { r.Plant.Inject(DevClimate, FaultDead, 0); r.Note("later: climate sensor dead") }},
				{26 * h, func(r *Run) {
					r.Plant.Inject(DevClimate, FaultNone, 0)
					r.Plant.Inject(DevDuct, FaultNone, 0)
					r.Note("all repaired")
				}},
			}},
		// The hardware guard's lease expires while the compressor runs. It must not be
		// restarted inside its off time when control comes back.
		{Name: "guard_lease_drop", Kind: "fault", T: 4, RH: 85, Air: 1.2, Duration: 20 * h, M0: 8 * h, M1: 13.9 * h, D0: 14 * h, D1: 14*h + 1800, Expect: []string{core.CodeOutDropped},
			Init: func(r *Run) {},
			Actions: []Action{
				{14 * h, func(r *Run) {
					r.AdvanceUntil(3600, func() bool { return r.Plant.CmdCool })
					r.Guard.DropOutputs()
					r.Note("guard lease expired with compressor running")
				}},
			}},
		door("door_60min_13c", 13, 76, 1.2, 3600),
		{Name: "fault_door_switch_stuck_open", Kind: "fault", T: 13, RH: 76, Air: 1.2, Duration: 24 * h, M0: 8 * h, M1: 13.9 * h, D0: 14 * h, D1: 22 * h, Expect: []string{core.CodeDoorOpen},
			Actions: []Action{
				{14 * h, func(r *Run) {
					r.Plant.Inject(DevDoorSwitch, FaultStuck, 1)
					r.Note("door switch reads open, door is shut")
				}},
				{22 * h, func(r *Run) { r.Plant.Inject(DevDoorSwitch, FaultNone, 0); r.Note("switch repaired") }},
			}},

		// Operator handling of an alarm: silence, its timeout, acknowledge, repair.
		{Name: "alarm_silence_ack", Kind: "fault", T: 13, RH: 76, Air: 1.2, Duration: 24 * h, M0: 8 * h, M1: 13.9 * h, D0: 14 * h, D1: 19 * h, Expect: []string{core.CodeTempHigh, core.CodeCoolFail},
			Actions: []Action{
				{14 * h, func(r *Run) { r.Plant.Inject(DevCompressor, FaultNoCool, 0); r.Note("compressor stops cooling") }},
				{16 * h, func(r *Run) { _ = r.Core.Do(api.Command{Kind: api.CmdSilence}); r.Note("operator silences buzzer") }},
				{17 * h, func(r *Run) { _ = r.Core.Do(api.Command{Kind: api.CmdAck}); r.Note("operator acknowledges all") }},
				{19 * h, func(r *Run) { r.Plant.Inject(DevCompressor, FaultNone, 0); r.Note("compressor repaired") }},
			}},
		// A room that warms by 6 C and stays there. Not a fault; shows the band and
		// whether anything falsely alarms.
		{Name: "room_step_plus6c_13c", Kind: "disturbance", T: 13, RH: 76, Air: 1.2, Duration: 72 * h, M0: 24 * h, M1: 72 * h, D0: 18 * h, D1: 18*h + 1,
			Actions: []Action{{18 * h, func(r *Run) { r.Plant.AmbientOffset = 6; r.Note("room warms 6 C") }}}},

		// Storage.
		{Name: "fault_storage_write_fail", Kind: "fault", T: 13, RH: 76, Air: 1.2, Duration: 22 * h, M0: 8 * h, M1: 13.9 * h, D0: 14 * h, D1: 20 * h, Expect: []string{core.CodeStorage},
			Actions: []Action{
				{14 * h, func(r *Run) { r.FS.FailWrite = true; r.Note("storage writes fail") }},
				{20 * h, func(r *Run) { r.FS.FailWrite = false; r.Note("storage repaired") }},
			}},
		{Name: "fault_storage_full", Kind: "fault", T: 13, RH: 76, Air: 1.2, Duration: 22 * h, M0: 8 * h, M1: 13.9 * h, D0: 14 * h, D1: 20 * h, Expect: []string{core.CodeStorage},
			Actions: []Action{
				{14 * h, func(r *Run) { r.FS.CapacityBytes = 1; r.Note("storage full") }},
				{20 * h, func(r *Run) { r.FS.CapacityBytes = 0; r.Note("storage space restored") }},
			}},
	}
	// Long runs with nothing injected, across every plant: doors, and power cuts with
	// damage to storage. Every sensor-disagreement alarm here is false, and any change in
	// regulation caused by a suspicion would show as an excursion.
	for _, ll := range []struct {
		name       string
		t, rh, air float64
	}{{"13c", 13, 76, 1.2}, {"4c", 4, 85, 1.2}, {"1p5c", 1.5, 82, 1.2}, {"24c", 24, 90, 1.2}} {
		s = append(s, longRun("faultfree_14d_"+ll.name, ll.t, ll.rh, ll.air))
	}
	// Reference comparison runs on the same plants.
	var refs []Scenario
	for _, sc := range s {
		switch sc.Name {
		case "steady_cure_13c_76rh", "steady_early_14c_82rh", "steady_equalize_4c_85rh", "steady_beef_1p5c_82rh", "steady_ferment_24c_90rh", "steady_warm_20c_70rh", "steady_biltong_25c_55rh",
			"door_2min_13c", "door_2min_4c", "warmload_13c_10kg_20c", "warmload_4c_10kg_12c", "defrost_13c", "defrost_4c", "defrost_1p5c", "pulldown_empty_4c":
			refs = append(refs, withRef(sc, 1, false), withRef(sc, 4, false))
		case "fault_climate_dead_13c", "fault_climate_dead_4c", "fault_climate_dead_1p5c", "fault_climate_dead_24c":
			refs = append(refs, withRef(sc, 1, false), withRef(sc, 1, true))
		}
	}
	return append(s, refs...)
}

// Find returns a scenario by name.
func Find(name string) (Scenario, bool) {
	for _, s := range Scenarios() {
		if s.Name == name {
			return s, true
		}
	}
	return Scenario{}, false
}

// Names returns all scenario names.
func Names() []string {
	var out []string
	for _, s := range Scenarios() {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// Output is where a scenario writes raw data. Nil members are not written.
type Output struct {
	CSV    io.Writer
	Events io.Writer
}

// Execute runs a scenario on a plant and returns its spec and metrics, computed
// from the same rows and events that are written out.
func Execute(sc Scenario, plant Params, out Output) (Spec, Metrics, error) {
	if sc.Mutate != nil {
		sc.Mutate(&plant)
	}
	ctl := "core"
	if sc.Ref > 0 {
		ctl = fmt.Sprintf("ref%d", sc.Ref)
		if sc.RefDuty {
			ctl += "_duty"
		}
	}
	if (sc.Name == "fault_door_switch_dead" || sc.Name == "fault_door_switch_stuck_open") && !plant.DoorSwitch {
		sc.Expect = nil // nothing fitted to fail
	}
	sp := Spec{Name: sc.Name, Plant: plant.Name, Ctl: ctl, Kind: sc.Kind, SetT: sc.T, SetRH: sc.RH, SetAir: sc.Air, M0: sc.M0, M1: sc.M1, D0: sc.D0, D1: sc.D1, Expect: sc.Expect, Notes: sc.Notes, RecoverFrom: sc.RecoverFrom}
	r, err := NewRun(Options{Plant: plant, UseRef: sc.Ref > 0, KeepRows: true, SampleSec: sc.SampleSec, CSV: out.CSV, Events: out.Events})
	if err != nil {
		return sp, nil, err
	}
	if sc.Ref > 0 {
		r.Ref.acMin = float64(sc.Ref) * 60
		r.Ref.FaultDuty = sc.RefDuty
	} else {
		r.SetCal()
	}
	r.SetPoint(sc.T, sc.RH, sc.Air)
	if sc.Init != nil {
		sc.Init(r)
	}
	acts := append([]Action(nil), sc.Actions...)
	sort.SliceStable(acts, func(i, j int) bool { return acts[i].At < acts[j].At })
	for _, a := range acts {
		if a.At > r.Clock.VT {
			r.Advance(a.At - r.Clock.VT)
		}
		a.Do(r)
	}
	if sc.Duration > r.Clock.VT {
		r.Advance(sc.Duration - r.Clock.VT)
	}
	r.Flush()
	return sp, Compute(sp, r.Rows, r.Evs), nil
}
