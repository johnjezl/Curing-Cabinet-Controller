package sim

import (
	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// RefCtl emulates the documented default behaviour of the reference humidity
// controller (BENCHMARK 1.3, XLH360 defaults) so that the same plant can be run
// under both controllers. Probe fault policy follows the 15/30 duty of BENCHMARK
// R3.5 when FaultDuty is set and all-off otherwise.
//
// It is an emulation from documents, not a product, and has none of a product's
// unpublished refinements.
type RefCtl struct {
	pl  *Plant
	clk *Clock

	Set       api.Setpoints
	FaultDuty bool
	FanFixed  float64 // fixed fan duty; no reference closes the loop

	cool, heat, humid bool
	lastStop          float64
	hasStop           bool
	lastDefrost       float64
	defrostEnd        float64
	inDefrost         bool
	dbT, dbH          float64
	acMin             float64
	badSince          float64
	bad               bool
	temp, rh          float64
	have              bool
	doorSince         float64
	wasDoor           bool
}

// NewRefCtl returns the emulation with its documented defaults.
func NewRefCtl(pl *Plant, clk *Clock) *RefCtl {
	return &RefCtl{pl: pl, clk: clk, dbT: 2.0, dbH: 5.0, acMin: 60, FanFixed: 30, Set: api.Setpoints{TempC: 13, RHPct: 75, AirMS: 1}, lastDefrost: clk.VT}
}

// Restart models power returning; XLH360 od default is effectively none.
func (r *RefCtl) Restart() {
	r.cool, r.heat, r.humid = false, false, false
	r.hasStop = false
	r.inDefrost = false
	r.lastDefrost = r.clk.VT
	r.have = false
}

// Step implements Ctl.
func (r *RefCtl) Step() {
	t := r.clk.VT
	cab := r.pl.Cabinet()
	temp, rh, _, err := cab.Climate.Read()
	if err != nil {
		if !r.bad {
			if r.badSince == 0 {
				r.badSince = t
			}
			if t-r.badSince > 10 {
				r.bad = true
			}
		}
	} else {
		r.badSince = 0
		r.bad = false
		r.temp, r.rh, r.have = temp, rh, true
	}
	// Defrost: every 6 h for 20 min.
	if !r.inDefrost && t-r.lastDefrost >= 6*3600 {
		r.inDefrost = true
		r.defrostEnd = t + 20*60
	}
	if r.inDefrost && t >= r.defrostEnd {
		r.inDefrost = false
		r.lastDefrost = t
	}
	door, _ := cab.Door.Open()
	if door && !r.wasDoor {
		r.doorSince = t
	}
	r.wasDoor = door

	wantCool, wantHeat, wantHum := r.cool, r.heat, r.humid
	switch {
	case r.inDefrost:
		wantCool, wantHeat, wantHum = false, false, false
	case r.bad || !r.have:
		wantHeat, wantHum = false, false
		wantCool = false
		if r.FaultDuty {
			wantCool = int(t)%(45*60) < 15*60
		}
	default:
		// Temperature neutral zone.
		if r.temp >= r.Set.TempC+r.dbT {
			wantCool = true
		} else if r.temp <= r.Set.TempC {
			wantCool = false
		}
		if r.temp <= r.Set.TempC-r.dbT {
			wantHeat = true
		} else if r.temp >= r.Set.TempC {
			wantHeat = false
		}
		// Humidity neutral zone.
		if r.rh <= r.Set.RHPct-r.dbH {
			wantHum = true
		} else if r.rh >= r.Set.RHPct {
			wantHum = false
		}
		// Dehumidify is compressor, with heater joining at set temperature.
		if r.rh >= r.Set.RHPct+r.dbH {
			wantCool = true
			if r.temp < r.Set.TempC {
				wantHeat = true
			}
		}
	}
	if door && t-r.doorSince < 20*60 {
		wantCool, wantHeat, wantHum = false, false, false
	}
	// Anti-short-cycle: AC 1 min.
	if wantCool && !r.cool && r.hasStop && t-r.lastStop < r.acMin {
		wantCool = false
	}
	if r.cool && !wantCool {
		r.lastStop, r.hasStop = t, true
	}
	r.cool, r.heat, r.humid = wantCool, wantHeat, wantHum
	_ = cab.Outputs.Set(hal.OutCool, r.cool)
	_ = cab.Outputs.Set(hal.OutHeat, r.heat)
	_ = cab.Outputs.Set(hal.OutHumid, r.humid)
	duty := r.FanFixed
	if r.pl.FanRPM < 100 && duty > 0 {
		duty = 30 // fans are started with a kick by any fan driver
	}
	if door {
		duty = 0
	}
	_ = cab.Fan.SetDuty(duty)
}

// View implements Ctl.
func (r *RefCtl) View() core.ProbeView {
	q := api.QualityOK
	if r.bad {
		q = api.QualityFault
	}
	return core.ProbeView{Temp: r.temp, RH: r.rh, TempQ: q, RHQ: q, AirQ: api.QualityOff, WeightQ: api.QualityOff, Set: r.Set,
		Cool: r.cool, Heat: r.heat, Humid: r.humid, Defrost: r.inDefrost, Fan: r.pl.FanDuty, RunState: api.RunIdle}
}
