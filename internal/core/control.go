package core

import (
	"math"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// compressor applies every protection timer. Nothing bypasses it except an
// orderly Close, which is a stop, and stops only wait for minimum on.
type compressor struct {
	cfg        *Config
	on         bool
	lastStart  float64
	lastStop   float64
	hasStart   bool
	hasStop    bool
	powerUntil float64
	holdReason string
	holdUntil  float64
}

func (k *compressor) update(t float64, want bool) {
	k.holdReason, k.holdUntil = "", 0
	if k.on {
		if want {
			return
		}
		if t-k.lastStart < k.cfg.CompMinOn {
			k.holdReason, k.holdUntil = "minimum on time", k.lastStart+k.cfg.CompMinOn
			return
		}
		k.on, k.lastStop, k.hasStop = false, t, true
		return
	}
	if !want {
		return
	}
	if t < k.powerUntil {
		k.holdReason, k.holdUntil = "power-on delay", k.powerUntil
		return
	}
	if k.hasStop && t-k.lastStop < k.cfg.CompMinOff {
		k.holdReason, k.holdUntil = "minimum off time", k.lastStop+k.cfg.CompMinOff
		return
	}
	if k.hasStart && t-k.lastStart < k.cfg.CompStartSpacing {
		k.holdReason, k.holdUntil = "start spacing", k.lastStart+k.cfg.CompStartSpacing
		return
	}
	k.on, k.lastStart, k.hasStart = true, t, true
}

// reheatRatio converts heater time into the compressor time it cancels: a 100 W
// heater against about 150 W of real cooling.
const reheatRatio = 0.7

// control is the state of the regulation loops.
type control struct {
	comp compressor

	coolWant   bool
	everCooled bool

	heatWinT    float64
	heatOnUntil float64
	heatOn      bool
	heatDemand  float64 // last heater duty asked for, 0..1, for the panel's reason

	dehum      bool
	rhI        float64
	tI         float64
	credit     float64
	dehumSince float64
	rhHighT    float64

	humOn       bool
	humOnStart  float64
	humOnUntil  float64
	humNext     float64
	humMeasure  bool
	humStartRH  float64
	humPeakRH   float64
	humClean    bool // no compressor during the pulse and its settling
	humSecs     float64
	humPulses   int
	humNoRise   int
	humDead     float64
	humPreSlope float64
	rhRing      [7]float64
	rhIdx       int
	rhRingT     float64
	humI        float64
	humLastT    float64

	dehumOut bool

	defrost      bool
	defrostStart float64
	defrostEnd   float64
	dripUntil    float64
	defrostForce bool
	defrostT0    float64
	defrostReasn string

	fbCycleT float64 // sensor-fault cooling cycle start

	// fan
	fanDuty       float64
	fanKickTo     float64
	fanLearn      float64
	fanFloorHit   bool
	stall         bool
	stallT        float64
	nextKick      float64
	airflowProven bool

	// energy of outputs, for the drift cross-check
	dutyShort, dutyBase    ema
	dutyFast               ema
	dehumEffort, dehumBase ema
	humidShort, humidBase  ema
	driftAge               float64
	driftSet               float64
	driftSetRH             float64
}

// dutyAtFailure picks the sensor-fault cooling pattern. Reference products run a
// fixed 15 on/30 off written for cold rooms (a 33% duty whatever the load, so it
// can freeze or starve a cabinet). This one repeats what this cabinet was
// actually doing just before the sensor died, with a small bias to the cold
// side, and does nothing at all if the cabinet had not been cooling.
func (c *Core) fallbackCool() bool {
	cfg := &c.cfg
	duty := c.st.CoolDuty
	if c.st.CoolDutyAge < 2*3600 {
		// Not enough history. Cold setpoints are assumed to need some cooling;
		// warm ones are left to drift, because there is no way to know the room.
		if c.setpoint().TempC < 12 {
			duty = 0.20
		} else {
			duty = 0.10
		}
	}
	// Learned with the humidifier and product latent load in it, most of which
	// stops with humidity control off, so the plain figure overcools. See docs/adr/0111.
	// The nearer the setpoint is to freezing, the less room there is on the cold
	// side, so the bias grows as the setpoint falls from 6 °C to 2 °C.
	scale := cfg.FallbackScale
	if sp := c.setpoint().TempC; sp < 6 {
		scale -= clamp((6-sp)/4, 0, 1) * cfg.FallbackColdCut
	}
	duty = clamp(duty*scale, 0, 0.6)
	if duty < 0.02 {
		return false
	}
	// As short a cycle as protection allows: a long blind run swings the air far
	// further than the same duty in short runs.
	period := clamp(cfg.CompMinOn/duty, cfg.CompStartSpacing, 1800)
	on := math.Max(duty*period, cfg.CompMinOn)
	if c.t-c.ctl.fbCycleT >= period {
		c.ctl.fbCycleT = c.t
	}
	return c.t-c.ctl.fbCycleT < on
}

// controlClimate decides compressor, heater, humidifier and dehumidifier.
func (c *Core) controlClimate(dt float64) {
	ctl := &c.ctl
	cfg := &c.cfg
	set := c.setpoint()
	t := c.t
	tOK := c.chT.ok()
	rhOK := c.chRH.ok()
	tf := c.tF.v

	c.defrostStep(set, tOK)

	wantCool := false
	heatDuty := 0.0
	wantDehum := false

	doorHoldCool := c.doorOpen && t-c.doorOpenT < cfg.DoorHold
	// Time-limited for everything: a door left open or a switch stuck reading open
	// must not stop regulation for good.
	doorHoldOther := doorHoldCool

	// Humidity demand. Only with both readings good: dehumidify runs the
	// compressor, and that cannot be judged with temperature unknown. It is judged
	// on the slow average, because a compressor cycle swings RH by several percent
	// all by itself and that swing is not what needs correcting.
	uR := 0.0
	if rhOK && tOK && !ctl.defrost && t >= ctl.dripUntil && t-c.bootT > 300 && !doorHoldOther {
		e := c.rhS.v - set.RHPct
		if cfg.DehumidFitted {
			if e > 2 {
				ctl.dehum = true
			} else if e < 0.3 {
				ctl.dehum = false
			}
		} else {
			const kp, ti = 0.02, 2400.0
			ctl.rhI = clamp(ctl.rhI+kp*e*dt/ti, 0, 0.85)
			uR = clamp(kp*(e-0.5)+ctl.rhI, 0, 0.95)
			was := ctl.dehum
			ctl.dehum = uR > 0.03
			if ctl.dehum && !was {
				ctl.dehumSince = t
			}
		}
	} else {
		ctl.dehum = false
	}

	switch {
	case ctl.defrost || t < ctl.dripUntil:
		// Defrost has no probe: compressor held off for its fixed time.
		// At a cold setpoint the air alone cannot melt the coil, so the heater
		// warms the air for the defrost, within the abort limit.
		// The same limit as the abort, so that a cabinet already warm from ice still
		// gets the heat that clears it.
		if ctl.defrost && set.TempC < cfg.DefrostHeatBelowC && tOK && tf < math.Max(set.TempC+cfg.DefrostAbortCold, ctl.defrostT0+3)-1 {
			heatDuty = 0.8
		}
	case !tOK:
		// Fail direction, see docs/adr/0104: no heat, no humidity, cooling at the
		// duty the cabinet had just before the fault.
		wantCool = c.fallbackCool()
	default:
		wantCool, heatDuty = c.tempLoop(set, uR, doorHoldCool, dt)
		// A reading suspected of reading low must not be what stops the compressor:
		// the blind pattern used for a lost reading is a floor under cooling, so true
		// air is bounded the way it is with a dead sensor. Only ever toward cold.
		if c.st.TDoubtLow && c.st.TDoubtUntil != 0 && !doorHoldCool && c.fallbackCool() {
			wantCool = true
		}
		if ctl.dehum && cfg.DehumidFitted {
			wantDehum = true
		}
	}
	if doorHoldCool {
		wantCool = false
	}
	if doorHoldOther {
		heatDuty = 0
		wantDehum = false
	}

	// A stalled fan means the coil will ice and the probe sees stagnant air, so
	// the compressor may only run in a restricted cycle.
	if ctl.stall && wantCool {
		if math.Mod(t, 1800) > 900 {
			wantCool = false
		}
	}

	ctl.coolWant = wantCool
	ctl.comp.update(t, wantCool)
	if ctl.comp.on {
		ctl.everCooled = true
	}

	// Heater: never on bad temperature data, never without proven airflow. A suspicion
	// about either reading also blocks it: a suspicion may only move the cabinet toward
	// cold (docs/adr/0113). It never reduces or stops cooling.
	suspect := c.st.TDoubtUntil != 0 || c.st.RHDoubtUntil != 0
	heatAllowed := tOK && ctl.airflowProven && !doorHoldOther && t-c.bootT > 30 && !suspect
	ctl.heatDemand = heatDuty
	switch {
	case !heatAllowed || heatDuty <= 0:
		ctl.heatOnUntil = 0
	default:
		const win = 60.0
		if t-ctl.heatWinT >= win {
			ctl.heatWinT = t
			on := heatDuty * win
			if on < 5 {
				on = 0 // too short to bother a relay
			}
			ctl.heatOnUntil = t + on
		}
	}
	// Hard stops that act immediately, not at the end of a window.
	if !heatAllowed || (tOK && tf > set.TempC+0.8 && !ctl.dehum && !ctl.defrost) {
		ctl.heatOnUntil = 0
	}
	ctl.heatOn = t < ctl.heatOnUntil

	// Humidifier.
	c.humidLoop(set, rhOK && tOK && ctl.airflowProven && !doorHoldOther && !ctl.defrost && t >= ctl.dripUntil && t-c.bootT > 300 && !ctl.dehum && !suspect)

	ctl.dehumOut = wantDehum && rhOK && tOK && !ctl.defrost

	// Long averages used for the fallback and the drift cross-check. Net duty is
	// compressor time minus the part spent cancelling reheat, which is what the
	// cabinet would need with no heater running. The heater and compressor sizes
	// are not known, so a fixed ratio is assumed; see docs/adr/0104.
	// Learning stops while the reading is in doubt: the pattern above depends on it.
	if tOK && !ctl.defrost && !c.doorOpen && c.st.TDoubtUntil == 0 {
		x := 0.0
		if ctl.comp.on {
			x = 1
		}
		if ctl.heatOn {
			x -= reheatRatio
		}
		if math.Abs(c.st.CoolDutySet-set.TempC) > 0.5 {
			c.st.CoolDutySet = set.TempC
			c.st.CoolDutyAge = 0
			c.st.CoolDuty = 0
		}
		// Time-weighted average that converges fast at first and to a 24 h
		// memory later, so a short history is not trusted as a long one.
		c.st.CoolDutyAge += dt
		tau := math.Min(c.st.CoolDutyAge, 24*3600)
		c.st.CoolDuty += (x - c.st.CoolDuty) * dt / (tau + dt)
		h := 0.0
		if ctl.humOn {
			h = 1
		}
		if ctl.heatOn && ctl.comp.on {
			ctl.dehumEffort.add(1, dt)
		} else {
			ctl.dehumEffort.add(0, dt)
		}
		if math.Abs(ctl.driftSet-set.TempC) > 0.01 || math.Abs(ctl.driftSetRH-set.RHPct) > 0.01 {
			ctl.driftSet, ctl.driftSetRH = set.TempC, set.RHPct
			ctl.driftAge = 0
			ctl.dutyShort.reset()
			ctl.dutyBase.reset()
			ctl.humidShort.reset()
			ctl.humidBase.reset()
			ctl.dehumEffort.reset()
			ctl.dehumBase.reset()
			ctl.dutyFast.reset()
		}
		ctl.driftAge += dt
		ctl.dutyShort.add(x, dt)
		ctl.dutyFast.add(x, dt)
		ctl.humidShort.add(h, dt)
		if ctl.driftAge > 12*3600 {
			ctl.dutyBase.add(ctl.dutyShort.v, dt)
			ctl.humidBase.add(ctl.humidShort.v, dt)
			ctl.dehumBase.add(ctl.dehumEffort.v, dt)
		}
	}
}

// tempLoop computes a continuous demand and turns it into compressor runs. The
// compressor is one relay with protection timers, so it can only be cycled. A
// thermostat cycles it whenever a threshold is crossed, which puts the average
// wherever the plant's asymmetry leaves it (reference controllers sit about half
// a differential above setpoint). Here a PI loop on the slow temperature gives a
// demand from 0 to 1, and the compressor runs by sigma-delta modulation: demand
// accumulates as run-time credit, a run starts once the credit pays for a minimum
// run, and a run ends when the credit is spent. The long-run duty therefore
// equals the demand at any cycle length the protection timers allow, and no model
// of the plant is needed. Humidity demand joins through the same credit; if it
// asks for more than temperature needs, temperature falls, the demand goes
// negative, and the heater takes up the difference. That is reheat.
func (c *Core) tempLoop(set api.Setpoints, uR float64, door bool, dt float64) (cool bool, heat float64) {
	ctl := &c.ctl
	cfg := &c.cfg
	e := c.tS.v - set.TempC
	const kp, ti = 0.25, 2400.0
	if math.Abs(e) < 2.5 && !door {
		ctl.tI = clamp(ctl.tI+kp*e*dt/ti, -1, 1)
	}
	uT := kp*e + ctl.tI
	// Temperature comes first. If reheat cannot keep up, the humidity demand is
	// cut back rather than letting dehumidifying pull the air away from setpoint.
	if e < -0.7 {
		uR *= clamp(1+(e+0.7)/0.5, 0, 1)
	}
	u := clamp(math.Max(uT, uR), 0, 1)
	if u < 0.01 {
		u = 0
	}
	if !door {
		ctl.credit = clamp(ctl.credit+u*dt, -300, 1200)
	}
	if ctl.comp.on {
		ctl.credit -= dt
		cool = ctl.credit > 0
	} else {
		// Demand keeps accumulating during a run, so this start threshold gives a
		// run of minimum length at low demand, the shortest cycle that is allowed.
		cool = ctl.credit >= cfg.CompMinOn*(1-u)
	}
	// Guard: never drive the air far below setpoint, whatever is asking.
	if c.tF.v < set.TempC-2.5 {
		cool = false
	}

	heat = clamp(-uT, 0, 1)
	if heat < 0.05 {
		heat = 0
	}
	return cool, heat
}

// humidLoop runs the humidifier as short measured pulses. A small cabinet holds
// only a few grams of water vapour, so a humidifier can move it several percent
// per second and a thermostat-style loop would hunt. The size of each pulse
// comes from the RH rise the previous pulse actually produced, so it adapts to
// the cabinet volume and humidifier size without configuration.
func (c *Core) humidLoop(set api.Setpoints, allowed bool) {
	ctl := &c.ctl
	t := c.t
	rf := c.rhF.v
	if t >= ctl.rhRingT {
		ctl.rhRingT = t + 10
		ctl.rhIdx = (ctl.rhIdx + 1) % len(ctl.rhRing)
		ctl.rhRing[ctl.rhIdx] = c.chRH.val
	}
	if ctl.comp.on {
		ctl.humClean = false
	}
	if ctl.humOn {
		if !allowed || t >= ctl.humOnUntil {
			ctl.humOn = false
		}
	}
	if ctl.humMeasure {
		if c.chRH.val > ctl.humPeakRH {
			ctl.humPeakRH = c.chRH.val
		}
		if t >= ctl.humOnStart+ctl.humSecs+50 {
			ctl.humMeasure = false
			// What RH was already doing is not the humidifier's work: it may be
			// coming back up after a compressor run.
			rise := ctl.humPeakRH - ctl.humStartRH - math.Max(ctl.humPreSlope, 0)*(ctl.humSecs+50)
			if ctl.humClean && ctl.humSecs > 0 {
				if rise > 0.3 {
					eff := math.Max(ctl.humSecs-ctl.humDead, 2)
					g := rise / eff
					c.st.HumidGain = clamp(c.st.HumidGain+0.4*(g-c.st.HumidGain), 0.02, 3)
					ctl.humNoRise = 0
					// Creep the dead time back down so it tracks the humidifier.
					ctl.humDead = math.Max(ctl.humDead-0.25, 2)
				} else {
					ctl.humDead = math.Min(ctl.humDead+3, 45)
					if ctl.humSecs >= 45 {
						ctl.humNoRise++
					}
				}
			}
		}
	}
	if !allowed || ctl.humOn || t < ctl.humNext {
		ctl.humLastT = t
		return
	}
	err := set.RHPct - c.rhS.v
	// Slow integral: if the average stays low, pulses come sooner, so the long-run
	// humidity settles on setpoint whatever the coil and the room are taking out.
	ctl.humI = clamp(ctl.humI+err*(t-ctl.humLastT)/3600*3, 0, 10)
	ctl.humLastT = t
	if err < 0.5 || rf > set.RHPct {
		return
	}
	gain := c.st.HumidGain
	if gain <= 0 {
		gain = 0.3
	}
	// Mist takes a few seconds to start, so a pulse that produced nothing is
	// lengthened until one does. That also finds a humidifier's real dead time.
	// No single pulse is allowed to move RH more than a few percent; a bigger
	// need is met by more frequent pulses, not longer ones.
	on := clamp(ctl.humDead+clamp(0.7*err/gain, 1.5, 5/gain)*(1+ctl.humI/3), 4, 60)
	ctl.humOn = true
	ctl.humOnStart = t
	ctl.humOnUntil = t + on
	ctl.humSecs = on
	ctl.humNext = t + on + clamp(240-15*ctl.humI, 170, 240)
	ctl.humMeasure = true
	ctl.humStartRH = c.chRH.val
	ctl.humPreSlope = (c.chRH.val - ctl.rhRing[(ctl.rhIdx+1)%len(ctl.rhRing)]) / (10 * float64(len(ctl.rhRing)-1))
	ctl.humPeakRH = c.chRH.val
	ctl.humClean = !ctl.comp.on
	ctl.humPulses++
}

// defrostStep schedules and ends defrost. There is no evaporator probe, so it
// runs on time only, and only if the compressor has actually run enough since
// the last one to have made ice: a fermentation stage on the heater needs none.
func (c *Core) defrostStep(set api.Setpoints, tOK bool) {
	ctl := &c.ctl
	cfg := &c.cfg
	t := c.t
	interval := cfg.DefrostInterval
	switch {
	case set.TempC < cfg.DefrostHeatBelowC:
		interval = cfg.DefrostIntervalCold
	case set.TempC >= 10:
		interval = cfg.DefrostIntervalWarm
	}
	if ctl.defrost {
		limit := cfg.DefrostAbortDeltaC
		dur := cfg.DefrostDuration
		if set.TempC < cfg.DefrostHeatBelowC {
			limit = cfg.DefrostAbortCold
			dur = cfg.DefrostDurationCold
		}
		// The limit is over setpoint or over the air at defrost start, whichever is
		// higher, and only after 5 min: in round 3 a cabinet that was already warm cut
		// every defrost after 1 s, the ice was never cleared and it warmed for weeks.
		lim := math.Max(set.TempC+limit, ctl.defrostT0+3)
		over := tOK && t-ctl.defrostStart >= 300 && c.tF.v > lim
		if tOK && c.tF.v > c.st.Limits.AbsTempHighC {
			over = true // never past the absolute limit
		}
		if t-ctl.defrostStart >= dur || over {
			ctl.defrost = false
			ctl.defrostEnd = t
			ctl.dripUntil = t + cfg.DefrostDrip
			reason := "time"
			if over {
				reason = "temperature limit"
			}
			c.event(Event{Type: EvDefrost, Msg: "end: " + reason, Dur: t - ctl.defrostStart})
			if over && t-ctl.defrostStart < dur/2 {
				// Cut short: the ice is probably still there, so try again sooner,
				// and say so if it keeps happening.
				c.defrostShortN++
				c.st.SinceDefrost = interval - 2*3600
				if c.st.SinceDefrost < 0 {
					c.st.SinceDefrost = 0
				}
				return
			}
			c.defrostShortN = 0
			c.st.SinceDefrost = 0
			c.st.RunSinceDef = 0
		}
		return
	}
	// With too little compressor time there is nothing to melt: keep waiting.
	due := c.st.SinceDefrost >= interval && c.st.RunSinceDef >= cfg.DefrostMinRuntime
	if ctl.defrostForce || due {
		if c.doorOpen {
			return
		}
		ctl.defrost = true
		ctl.defrostStart = t
		ctl.defrostT0 = c.tF.v
		ctl.dehum = false
		reason := "schedule"
		if ctl.defrostForce {
			reason = "operator"
		}
		ctl.defrostForce = false
		c.event(Event{Type: EvDefrost, Msg: "start: " + reason})
	}
}

// fanCurve keeps the fan turning. The P12 Pro holds about 600 rpm at low duty
// and some samples stop below a few percent, and a stopped fan may need a kick
// to restart.
const (
	fanFloorDuty = 11.0
	fanKickDuty  = 45.0
	fanKickSec   = 5.0
)

// controlFan closes the loop from the FS3000 to fan PWM and decides whether
// airflow is proven, which is what lets the heater and humidifier run.
func (c *Core) controlFan(dt float64) {
	ctl := &c.ctl
	t := c.t
	set := c.setpoint()
	airOK := c.chAir.ok()
	af := c.airF.v
	target := set.AirMS

	rpmOK := c.rpmOK
	rpm := c.rpm

	// Airflow is proven by either independent signal showing motion. Both have
	// to fail or read still before heat and humidity are locked out. A tach
	// reading proves the rotor spins, not that air moves, so a reading of no
	// flow from a good air sensor beats it.
	moving := false
	if airOK && af >= 0.15 {
		moving = true
	} else if !airOK && rpmOK && rpm >= 300 {
		moving = true
	}
	ctl.airflowProven = moving

	// Stall: told to run and the tach reads nothing, with no air either.
	bothStill := rpmOK && rpm < 150 && (!airOK || af < 0.2)
	// Not while it is meant to be still, nor just after it was told to start.
	if t-c.bootT < 15 || (ctl.defrostEnd != 0 && t < ctl.defrostEnd+c.cfg.FanDelay+20) {
		bothStill = false
	}
	if bothStill {
		if ctl.stallT == 0 {
			ctl.stallT = t
		}
		if t-ctl.stallT > 10 {
			ctl.stall = true
		}
	} else {
		ctl.stallT = 0
		if ctl.stall && ((rpmOK && rpm >= 300) || (airOK && af >= 0.3)) {
			ctl.stall = false
		}
	}

	duty := ctl.fanDuty
	switch {
	case c.doorOpen && t-c.doorOpenT < c.cfg.DoorHold:
		duty = fanFloorDuty
	case ctl.defrost:
		duty = c.cfg.DefrostFanDuty
	case t < ctl.dripUntil:
		duty = fanFloorDuty
	case airOK && !c.chAir.stuck:
		if !c.airBlocked {
			// Velocity-form PI with a deadband. FS3000 noise is about 0.03 m/s,
			// and the mass-airflow plant is close to static, so integral action
			// alone is well damped. Rate limited: the loop only needs to follow
			// a slow drift such as a clogging filter.
			e := target - af
			if math.Abs(e) < 0.03 {
				e = 0
			}
			step := clamp(e*12*dt/8, -1.5*dt, 1.5*dt)
			duty += step
			ctl.fanFloorHit = duty <= fanFloorDuty && e < -0.05
		}
		if af > 0.1 && math.Abs(target-af) < 0.08 {
			c.st.FanDuty += (duty - c.st.FanDuty) * dt / 600
		}
	default:
		// No usable airflow reading: hold the last duty that met the target.
		if c.st.FanDuty >= fanFloorDuty {
			duty = c.st.FanDuty
		} else {
			duty = 40
		}
	}
	duty = clamp(duty, fanFloorDuty, 100)
	ctl.fanDuty = duty
	// After a defrost the fan waits, so the warm wet air off the coil is not blown
	// straight over product.
	fanHeld := ctl.defrostEnd != 0 && t < ctl.defrostEnd+c.cfg.FanDelay
	if fanHeld {
		c.fanOut = 0
		ctl.fanKickTo = t + fanKickSec + 1
		return
	}

	// A kick after a stall or at start.
	out := duty
	if t < ctl.fanKickTo {
		out = math.Max(out, fanKickDuty)
	} else if (ctl.stall || t-c.bootT < 3) && t >= ctl.nextKick {
		ctl.fanKickTo = t + fanKickSec
		ctl.nextKick = t + 60
		out = math.Max(out, fanKickDuty)
	}
	c.fanOut = out
}
