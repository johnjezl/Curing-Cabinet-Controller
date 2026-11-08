// Package core is the control logic of the curing cabinet. It implements
// api.Controller over hal.Cabinet as a deterministic step function: every timer
// runs on an injected monotonic clock, nothing reads the system time, and no
// goroutine is started unless the caller asks for the asynchronous store. The
// same code runs on the Pi in real time and in a simulator in virtual time.
package core

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// Options configure a Core.
type Options struct {
	Cabinet hal.Cabinet
	Clock   Clock
	FS      FS // required

	Config    *Config // nil means DefaultConfig
	Watchdog  hal.Watchdog
	Simulated bool
	Speed     float64
	Version   string
	// AsyncDepth above 0 writes on a background goroutine with a queue this deep.
	// Use on real storage. Leave 0 in simulation: determinism needs it.
	AsyncDepth int
	// Trace, if set, is called with every event as it is created.
	Trace func(Event)
}

// DefaultLimits are the alarm limits a new cabinet starts with.
func DefaultLimits() api.Limits {
	return api.Limits{
		TempHighDelta: 2.5,
		TempLowDelta:  2.5,
		RHHighDelta:   10,
		RHLowDelta:    10,
		AlarmDelay:    15 * time.Minute,
		DoorDelay:     5 * time.Minute,
		AbsTempHighC:  30,
		AbsTempLowC:   -0.5,
	}
}

// Core is the controller.
type Core struct {
	mu    sync.Mutex
	opts  Options
	cfg   Config
	cab   hal.Cabinet
	clock Clock
	store Store
	wd    hal.Watchdog

	st State

	t, bootT float64
	started  bool

	chT, chRH, chAir, chScale *channel
	tF, tS, tMean, rhF, rhS   ema
	airF, rpmF, scaleRawF     ema
	weightF                   ema
	lastDHT                   float64
	haveDHT                   bool
	lastScaleRead             float64
	rpm                       float64
	rpmOK                     bool
	wasFaultT, wasFaultRH     bool

	doorOpen       bool
	doorOpenT      float64
	doorFitted     bool
	doorFittedEver bool

	ctl control
	al  alarms
	wt  weightTrack

	suppressUntil, suppressRHUntil, suppressOther float64
	openT, openRH                                 bool
	prevDefrost                                   bool
	airBlocked                                    bool
	scaleImplausible                              bool
	lossHoldT                                     float64
	weightStepT                                   float64
	logDamageUntil                                float64

	coolRunStart, coolRunT0, coolBest  float64
	coolFailing                        bool
	coolOffT0, coolOffStart            float64
	coolStuck                          bool
	coolStuckMin, lastDoorT            float64
	runActive, runClean                bool
	runT0, runMin, runEnd              float64
	runContT, runContT0, lastRunDrop   float64
	runDew0, runDewMin, runContDew0    float64 // dew point at run start, lowest in the run, at the 15 min mark
	lastRunDewDrop                     float64
	badRuns                            int
	aboveBandT, lastTBadT, rhHighSince float64
	signalling                         bool
	defrostShortN                      int
	cross                              crossAcc
	crossTempSoft, crossRHSoft         bool
	crossRateSoft                      bool
	dropped                            bool
	outErrSince                        float64
	driftClearT, rhDriftClearT         float64
	lastHeatOn, lastHumOn              float64
	heatStuck, humOnStuck              bool
	compRecent                         ema // compressor on-time over the last half hour or so
	humSat                             ema // share of the last 20 min or so at saturation with the humidifier off
	tRing                              [7]float64
	tRingT                             float64
	stuckClearT, humStuckClearT        float64
	humSatSince, coolOffDew0           float64
	lastEquipFaultT                    float64
	heatEffort, heatEvalT, heatEvalT0  float64
	heatFailing                        bool

	// outputs
	fanOut, lastFanOut  float64
	alarmOut, buzzerOut bool
	applied             map[hal.OutputID]bool
	reassertT           float64
	outputErr           string
	wdOK                bool
	fanCmdSent          bool

	// clock
	clockOK              bool
	wasTrusted           bool
	lastWall             time.Time
	lastMonoForWall      float64
	startedWall          time.Time
	outage               api.Outage
	outagePending        bool
	lastAliveT           float64
	outageResolvePending bool
	outagePrevWall       time.Time
	storeStatus          StoreStatus
	storeCheckT          float64
	storeDroppedAck      uint64

	// log accumulation
	acc      logAcc
	nextLogT float64
	nextCkpt float64
	saveReq  bool

	pendingEv []Event

	// trend
	fine, coarse         []api.Sample
	nextFine, nextCoarse float64
	logBuf               []LogRecord
}

type logAcc struct {
	n, tn, hn                     float64
	sumT, minT, maxT              float64
	sumH, minH, maxH              float64
	sumA, an, sumG, gn            float64
	cool, heat, humid, dehum, def float64
	door, dur                     float64
	fan, rpm                      float64
}

func (a *logAcc) reset() {
	*a = logAcc{minT: math.Inf(1), maxT: math.Inf(-1), minH: math.Inf(1), maxH: math.Inf(-1)}
}

// New opens the store, recovers what survived, and detects an outage. It always
// returns a usable Core: storage trouble raises an alarm and control runs anyway.
func New(o Options) (*Core, error) {
	if o.Clock == nil {
		return nil, errors.New("core: clock required")
	}
	if o.FS == nil {
		return nil, errors.New("core: storage required")
	}
	c := &Core{opts: o, cab: o.Cabinet, clock: o.Clock, wd: o.Watchdog, wdOK: true,
		applied: map[hal.OutputID]bool{}}
	c.cfg = DefaultConfig()
	if o.Config != nil {
		c.cfg = *o.Config
	}
	c.al = newAlarms()
	c.chT = newChannel("temp", -20, 60, 2.5, 10, 20, 5, 360, 7200)
	c.chRH = newChannel("rh", 0.5, 100, 25, 10, 20, 5, 600, 7200)
	c.chAir = newChannel("air", -0.1, 8, 3, 3, 5, 5, 0, 300)
	c.chScale = newChannel("scale", -8388000, 8388000, 1e9, 30, 60, 3, 0, 600)
	// Erratic: about 3 % of samples rejected, which is more than a healthy part
	// does. Heavy: half of them, more than a median of five can be trusted to fix.
	c.chT.setReject(5, 60, 30, 120)
	c.chRH.setReject(5, 60, 30, 120)
	c.chAir.setReject(5, 60, 60, 120)
	c.chScale.setReject(3, 300, 12, 120)
	c.tF.tau, c.tS.tau, c.tMean.tau = 20, 180, 1800
	c.rhF.tau, c.rhS.tau = 45, 300
	c.airF.tau, c.rpmF.tau = 6, 3
	c.scaleRawF.tau = 60
	c.weightF.tau = 1 // follows scaleRawF, which already carries the time constant
	c.ctl.comp.cfg = &c.cfg
	c.ctl.humDead = 5
	c.ctl.dutyShort.tau, c.ctl.humidShort.tau = 12*3600, 12*3600
	c.ctl.dutyBase.tau, c.ctl.humidBase.tau = 21*24*3600, 21*24*3600
	c.ctl.dutyFast.tau = 3600
	c.compRecent.tau = 1800
	c.humSat.tau = 1200
	c.ctl.dehumEffort.tau, c.ctl.dehumBase.tau = 12*3600, 21*24*3600
	c.acc.reset()

	fsStore, rec := OpenFileStore(o.FS)
	var store Store = fsStore
	if o.AsyncDepth > 0 {
		store = NewAsyncStore(fsStore, o.AsyncDepth)
	}
	c.store = store
	c.storeStatus = StoreStatus{OK: true}
	c.t = c.clock.Mono().Seconds()
	c.bootT = c.t
	c.boot(rec)
	return c, nil
}

// boot works out what the previous run left behind.
func (c *Core) boot(rec Recovered) {
	now := c.clock.Now()
	c.clockOK = c.clock.Trusted()
	c.startedWall = now

	first := !rec.HaveState && !rec.HaveLog && rec.SlotDamage == 0
	if rec.HaveState {
		c.st = rec.State
	} else {
		c.st = State{Version: 1}
		c.st.Manual = api.Setpoints{TempC: 13, RHPct: 75, AirMS: 1.0}
		c.st.Limits = DefaultLimits()
		c.st.Cal = api.Calibration{}
	}
	if c.st.Limits.AlarmDelay == 0 {
		c.st.Limits = DefaultLimits()
	}
	if c.st.Program.State == "" {
		c.st.Program.State = api.RunIdle
	}
	wasClean := rec.HaveState && c.st.Clean

	// Newest progress is in the process log, written every minute. State is
	// only checkpointed every ten minutes, to spare the card.
	lastWall, lastOK := c.st.LastWall, c.st.LastWallOK
	if rec.HaveLog {
		l := rec.LastLog
		if l.Powered > c.st.PoweredSec {
			c.st.PoweredSec = l.Powered
			lastWall, lastOK = l.Wall, l.ClkOK
		}
		p := &c.st.Program
		if p.State == api.RunActive || p.State == api.RunPaused {
			if l.Recipe != "" && p.Recipe != nil && l.Recipe == p.Recipe.ID && l.TotEl >= p.TotalElapsed && l.Phase == p.PhaseIndex {
				p.PhaseElapsed = l.PhEl
				p.TotalElapsed = l.TotEl
				if l.DH > p.DegreeHours {
					p.DegreeHours = l.DH
				}
			}
		}
		if !rec.HaveState {
			// Both state slots are gone. Rebuild what can be from the log.
			c.st.Manual = api.Setpoints{TempC: l.SetT, RHPct: l.SetRH, AirMS: l.SetAir}
			c.st.BootCount = l.Boot
			if l.Recipe != "" && (api.RunState(l.Run) == api.RunActive || api.RunState(l.Run) == api.RunPaused) {
				if r := c.findRecipe(l.Recipe); r != nil && l.Phase < len(r.Phases) {
					c.st.Program = ProgramState{State: api.RunState(l.Run), Recipe: r, PhaseIndex: l.Phase,
						PhaseElapsed: l.PhEl, TotalElapsed: l.TotEl, StartWeightG: l.Start, DegreeHours: l.DH}
				}
			}
		}
	}
	c.outagePrevWall = lastWall
	if rec.HaveLog {
		c.lastAliveT = rec.LastLog.T
	}
	// From here the stored time describes this boot, so a later outage cannot take
	// a stale trusted time from before this one as its start.
	c.st.LastWallOK = c.clockOK
	if c.clockOK {
		c.st.LastWall = now
	}

	c.st.BootCount++
	c.st.Clean = false
	c.al.seq = 0
	c.replayEvents(rec.Events)
	c.event(Event{Type: EvBoot, Msg: fmt.Sprintf("boot %d, clean previous stop %v, clock trusted %v", c.st.BootCount, wasClean, c.clockOK)})

	c.ctl.comp.powerUntil = c.t + c.cfg.PowerOnDelay
	if c.st.HumidGain <= 0 {
		c.st.HumidGain = 0.3
	}
	c.ctl.fanDuty = c.st.FanDuty
	if c.ctl.fanDuty < fanFloorDuty {
		c.ctl.fanDuty = 30
	}
	// Power-on suppression window. It closes at the first entry to the band.
	c.suppressUntil = c.t + c.cfg.SuppressPowerOn
	c.suppressRHUntil = c.t + c.cfg.SuppressPowerOn
	c.openT, c.openRH = true, true
	c.nextLogT = c.t + c.cfg.LogInterval
	c.nextCkpt = c.t + c.cfg.CheckpointSec
	c.restoreTrend(rec.LogTail)

	if rec.ReadErr != "" {
		c.storeStatus = StoreStatus{OK: false, Err: rec.ReadErr}
	}
	if !first && !rec.HaveState {
		c.raise(CodeStateLost, 0, 0)
	}
	if rec.SlotDamage > 0 || rec.BadEvents+rec.BadLog > int(c.st.BadLinesSeen) {
		if rec.BadEvents+rec.BadLog > int(c.st.BadLinesSeen) {
			c.raise(CodeLogDamage, float64(rec.BadEvents+rec.BadLog), 0)
			c.logDamageUntil = c.t + 3600
		}
		c.st.BadLinesSeen = uint64(rec.BadEvents + rec.BadLog)
	}

	if !first && !wasClean {
		// Power failed or the process died.
		// The stop happened after the last durable record, at most one log interval
		// later. Duration is measured from that record, so it is an upper bound.
		c.outage = api.Outage{Detected: true, At: lastWall, RecoverSec: -1, StartUncertaintySec: c.cfg.LogInterval, Provisional: true}
		c.outagePending = true
		if lastOK && c.clockOK && now.After(lastWall) {
			c.outage.Duration = now.Sub(lastWall)
			c.outage.DurationOK = true
			c.outage.Provisional = false
		} else if lastOK {
			c.outageResolvePending = true // resolved when the clock comes up
		}
		p := &c.st.Program
		if p.State == api.RunActive || p.State == api.RunPaused {
			p.Outages++
			p.DHIncomplete = true
			if c.outage.DurationOK {
				p.OutageKnownS += c.outage.Duration.Seconds()
			}
		}
		if c.outage.Duration >= 10*time.Minute || !c.outage.DurationOK {
			// The first hours after a long stop are not like the days before.
			c.crossReset()
		}
		msg := "duration unknown"
		if c.outage.DurationOK {
			msg = fmt.Sprintf("duration %.0f s", c.outage.Duration.Seconds())
		}
		dur := 0.0
		if c.outage.DurationOK {
			dur = c.outage.Duration.Seconds()
		}
		c.event(Event{Type: EvOutage, Msg: msg, Dur: dur})
		// A restored alarm from before the outage is closed; this is a new one.
		c.clear(CodeOutage)
		c.raise(CodeOutage, math.Inf(-1), 0)
		if a := c.al.active[CodeOutage]; a != nil {
			a.a.Peak = 0
		}
		// Whether this matters is known only once the cabinet is seen again, so it
		// starts silent and updateOutageRecord decides.
	} else if c.outage.Detected == false && rec.HaveState {
		c.outage = api.Outage{}
	}
	if wasClean {
		// A clean stop is not an outage. A record restored from an earlier outage
		// stays in history but is no longer an active alarm.
		c.clear(CodeOutage)
	}
	c.saveReq = true
	c.flush()
}

func (c *Core) event(e Event) {
	e.Boot = c.st.BootCount
	e.Mono = c.t
	e.Wall = c.clock.Now()
	e.ClkOK = c.clockOK
	if math.IsInf(e.Peak, 0) || math.IsNaN(e.Peak) {
		e.Peak = 0
	}
	c.pendingEv = append(c.pendingEv, e)
	if c.opts.Trace != nil {
		c.opts.Trace(e)
	}
}

func (c *Core) saveSoon() { c.saveReq = true }

func (c *Core) setpointChanged() {
	c.suppressUntil = math.Max(c.suppressUntil, c.t+c.cfg.SuppressChange)
	c.suppressRHUntil = math.Max(c.suppressRHUntil, c.t+c.cfg.SuppressChange)
	c.openT, c.openRH = true, true
	c.ctl.credit = 0
	c.ctl.rhI = 0
	c.crossReset()
	c.ctl.humI = 0
	c.ctl.humNoRise = 0
	c.tMean.reset()
	c.lossHoldT = 0
}

type inputs struct {
	temp, rh, age float64
	climateErr    error
	air           float64
	airErr        error
	scale         int32
	scaleErr      error
	scaleRead     bool
	rpm           float64
	rpmErr        error
	door          bool
	doorErr       error
	doorRead      bool
}

func (c *Core) read(t float64) inputs {
	var in inputs
	if c.cab.Climate != nil {
		in.temp, in.rh, in.age, in.climateErr = c.cab.Climate.Read()
	} else {
		in.climateErr = hal.ErrAbsent
	}
	if c.cab.Air != nil {
		in.air, in.airErr = c.cab.Air.Read()
	} else {
		in.airErr = hal.ErrAbsent
	}
	if c.cab.Fan != nil {
		in.rpm, in.rpmErr = c.cab.Fan.RPM()
	} else {
		in.rpmErr = hal.ErrAbsent
	}
	if c.cab.Scale != nil && t-c.lastScaleRead >= 5 {
		c.lastScaleRead = t
		in.scaleRead = true
		in.scale, in.scaleErr = c.cab.Scale.ReadRaw()
	}
	if c.cab.Door != nil {
		in.doorRead = true
		in.door, in.doorErr = c.cab.Door.Open()
	}
	return in
}

// Step runs one control tick. Call it about once a second from one goroutine.
// Sensors are read outside the lock so that a slow bus cannot stall Snapshot.
func (c *Core) Step() {
	mono := c.clock.Mono().Seconds()
	in := c.read(mono)

	c.mu.Lock()
	dt := mono - c.t
	if !c.started {
		c.started = true
		dt = 0
	}
	if dt < 0 {
		dt = 0
	}
	// A long stall still counts as powered time, but must not kick filters.
	fdt := math.Min(dt, 5)
	c.t = mono
	c.st.PoweredSec += dt

	c.updateClock(dt)
	c.ingest(in, fdt)
	c.updateProgram(dt)
	c.updateDefrostCounters(dt)
	c.controlFan(fdt)
	c.controlClimate(fdt)
	c.afterControl(dt)
	c.crossTick(fdt)
	c.doubtExpire()
	c.evalAlarms(fdt)
	c.updateOutageRecord()
	c.accumulate(dt)
	c.trendTick()

	want := c.wantOutputs()
	fan := c.fanOut
	c.mu.Unlock()

	c.apply(want, fan)
	c.flush()

	if c.wd != nil {
		// If outputs cannot be written, software has lost the cabinet, and the contact
		// cannot be trusted to change either. Stop renewing the lease and let the guard
		// open every relay by its own path. Everything off, contact signalling.
		c.mu.Lock()
		if c.outputErr == "" {
			c.outErrSince = 0
		} else if c.outErrSince == 0 {
			c.outErrSince = c.t
		}
		withhold := c.outErrSince != 0 && c.t-c.outErrSince >= 10
		c.mu.Unlock()
		if !withhold {
			err := c.wd.Pet()
			c.mu.Lock()
			c.wdOK = err == nil
			if errors.Is(err, hal.ErrOutputsDropped) {
				c.outputsDropped()
			}
			c.mu.Unlock()
		} else {
			c.mu.Lock()
			c.wdOK = false
			c.mu.Unlock()
		}
	}
}

// outputsDropped handles the hardware guard having cut everything. All outputs are
// off from this moment, whatever was last commanded, so the off-timers start now.
// Otherwise the next re-assert could restart a compressor that has only just
// stopped. Caller holds the lock.
func (c *Core) outputsDropped() {
	for k := range c.applied {
		delete(c.applied, k)
	}
	c.fanCmdSent = false
	if c.ctl.comp.on {
		c.ctl.comp.on = false
	}
	c.ctl.comp.lastStop, c.ctl.comp.hasStop = c.t, true
	c.ctl.heatOn = false
	c.ctl.heatOnUntil = 0
	c.ctl.humOn = false
	c.runActive = false
	c.reassertT = c.t
	if a := c.al.active[CodeOutDropped]; a != nil {
		c.clear(CodeOutDropped)
	}
	c.raise(CodeOutDropped, 0, 0)
	c.event(Event{Type: EvDropped, Msg: "guard lease expired; outputs treated as off"})
	c.saveSoon()
}

type wanted struct{ cool, heat, humid, dehum, alarm, buzzer bool }

func (c *Core) wantOutputs() wanted {
	return wanted{cool: c.ctl.comp.on, heat: c.ctl.heatOn, humid: c.ctl.humOn, dehum: c.ctl.dehumOut,
		alarm: c.alarmOut, buzzer: c.buzzerOut}
}

func (c *Core) apply(w wanted, fan float64) {
	var firstErr error
	force := c.t-c.reassertT >= 5
	if force {
		c.reassertT = c.t
	}
	set := func(id hal.OutputID, on bool) {
		if c.cab.Outputs == nil {
			return
		}
		if prev, ok := c.applied[id]; ok && prev == on && !force {
			return
		}
		if err := c.cab.Outputs.Set(id, on); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", id, err)
			}
			delete(c.applied, id)
			return
		}
		c.applied[id] = on
	}
	set(hal.OutCool, w.cool)
	set(hal.OutHeat, w.heat)
	set(hal.OutHumid, w.humid)
	set(hal.OutDehumid, w.dehum)
	// Energize-to-run: closed means healthy, so that a dead controller, a dead
	// guard and lost power all read as an alarm through normally-closed wiring.
	// The buzzer is the same: energized means silent, so a dropped line sounds it,
	// including when the guard drops every output because they cannot be written
	// (ADR 0407).
	set(hal.OutAlarm, !w.alarm)
	set(hal.OutBuzzer, !w.buzzer)
	if c.cab.Fan != nil && (math.Abs(fan-c.lastFanOut) >= 0.1 || !c.fanCmdSent || force) {
		if err := c.cab.Fan.SetDuty(fan); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("fan: %w", err)
			}
		} else {
			c.lastFanOut = fan
			c.fanCmdSent = true
		}
	}
	msg := ""
	if firstErr != nil {
		msg = firstErr.Error()
	}
	c.mu.Lock()
	c.outputErr = msg
	c.mu.Unlock()
}

func (c *Core) updateClock(dt float64) {
	now := c.clock.Now()
	trusted := c.clock.Trusted()
	// A clock that runs backwards past what was recorded cannot be believed.
	if trusted && now.Before(c.st.WallHighWater.Add(-5*time.Minute)) {
		trusted = false
	}
	if !c.lastWall.IsZero() {
		jump := now.Sub(c.lastWall).Seconds() - (c.t - c.lastMonoForWall)
		if math.Abs(jump) > 60 {
			c.event(Event{Type: EvClockJump, Msg: fmt.Sprintf("wall clock moved %.0f s against monotonic time", jump)})
		}
	}
	c.lastWall, c.lastMonoForWall = now, c.t
	if trusted && !c.wasTrusted && c.started {
		c.event(Event{Type: EvClockOK, Msg: fmt.Sprintf("clock trusted at mono %.1f s", c.t)})
		c.startedWall = now.Add(-time.Duration((c.t - c.bootT) * float64(time.Second)))
		if c.outageResolvePending {
			c.outageResolvePending = false
			back := c.startedWall
			if back.After(c.outagePrevWall) {
				c.outage.Duration = back.Sub(c.outagePrevWall)
				c.outage.DurationOK = true
				c.outage.Provisional = false
				p := &c.st.Program
				if p.State == api.RunActive || p.State == api.RunPaused {
					p.OutageKnownS += c.outage.Duration.Seconds()
				}
				c.event(Event{Type: EvOutage, Msg: fmt.Sprintf("duration resolved %.0f s", c.outage.Duration.Seconds()), Dur: c.outage.Duration.Seconds()})
			}
		}
	}
	c.wasTrusted = trusted
	c.clockOK = trusted
	c.st.LastWallOK = trusted
	if trusted {
		c.st.LastWall = now
		c.st.WallHighWater = now
	}
}

func (c *Core) ingest(in inputs, dt float64) {
	t := c.t
	cal := c.st.Cal

	// Climate: a sample is new only when the driver's age says so.
	if in.climateErr == nil {
		sampleT := t - in.age
		if !c.haveDHT || sampleT > c.lastDHT+0.5 {
			c.haveDHT = true
			c.lastDHT = sampleT
			c.chT.feed(t, in.temp+cal.TempOffsetC, true)
			c.chRH.feed(t, in.rh+cal.RHOffsetPct, true)
		}
	} else {
		c.chT.feed(t, 0, false)
		c.chRH.feed(t, 0, false)
	}
	tActive := c.ctl.comp.on || c.ctl.heatOn
	hActive := c.ctl.comp.on || c.ctl.humOn
	c.chT.update(t, dt, tActive)
	c.chRH.update(t, dt, hActive)
	if c.chT.quality == api.QualityFault || c.chT.quality == api.QualitySuspect {
		c.wasFaultT = true
	} else if c.wasFaultT && c.chT.ok() {
		c.wasFaultT = false
		c.tF.reset()
		c.tS.reset()
		c.tMean.reset()
	}
	if c.chRH.quality == api.QualityFault || c.chRH.quality == api.QualitySuspect {
		c.wasFaultRH = true
	} else if c.wasFaultRH && c.chRH.ok() {
		c.wasFaultRH = false
		c.rhF.reset()
		c.rhS.reset()
	}
	if c.chT.ok() {
		c.tF.add(c.chT.val, dt)
		c.tS.add(c.chT.val, dt)
		c.tMean.add(c.chT.val, dt)
	}
	if c.chRH.ok() {
		c.rhF.add(c.chRH.val, dt)
		c.rhS.add(c.chRH.val, dt)
	}

	// Airflow. Exactly zero is a legitimate reading of a stopped fan.
	if in.airErr == nil {
		c.chAir.feed(t, in.air, true)
		if in.air == 0 {
			c.chAir.lastChange = t
			c.chAir.stuck = false
		}
	} else {
		c.chAir.feed(t, 0, false)
	}
	c.chAir.update(t, dt, false)
	if c.chAir.ok() {
		c.airF.add(c.chAir.val, dt)
	}

	c.rpmOK = in.rpmErr == nil
	if c.rpmOK {
		c.rpm = c.rpmF.add(in.rpm, dt)
	}

	// Scale.
	if in.scaleRead {
		if c.scaleCal() {
			c.chScale.jumpMax = math.Abs(cal.ScaleCountsPG) * 100 // 100 g
		}
		before := c.chScale.n
		c.chScale.feed(t, float64(in.scale), in.scaleErr == nil)
		if before >= 3 && c.chScale.n == 1 {
			// Three agreeing samples at a new level: something really changed.
			c.weightStepT = t
			c.crossReset()
			c.scaleRawF.reset()
			c.wt = weightTrack{}
			if c.scaleCal() {
				c.event(Event{Type: EvPhase, Msg: "weight step accepted"})
			}
		}
	}
	c.chScale.update(t, dt, false)
	if c.chScale.ok() && c.chScale.have {
		raw := c.scaleRawF.add(c.chScale.val, dt)
		if c.scaleCal() {
			g := (raw - float64(cal.ScaleZero)) / cal.ScaleCountsPG
			c.weightF.v, c.weightF.init = g, true
		}
	}
	c.updateWeight(t)

	// Door.
	if in.doorRead && in.doorErr == nil {
		c.doorFitted, c.doorFittedEver = true, true
		if in.door != c.doorOpen {
			c.doorOpen = in.door
			c.lastDoorT = t
			if in.door {
				c.doorOpenT = t
				c.event(Event{Type: EvPhase, Msg: "door open"})
			} else {
				c.suppressOther = t + c.cfg.SuppressDoor
				c.suppressUntil = math.Max(c.suppressUntil, c.suppressOther)
				c.suppressRHUntil = math.Max(c.suppressRHUntil, c.suppressOther)
				c.event(Event{Type: EvPhase, Msg: fmt.Sprintf("door closed after %.0f s", t-c.doorOpenT)})
			}
		}
	} else if in.doorRead {
		c.doorFitted = false
		c.doorOpen = false
	}
}

func (c *Core) updateDefrostCounters(dt float64) {
	if !c.ctl.defrost {
		c.st.SinceDefrost += dt
	}
	if c.ctl.comp.on {
		c.st.CompRunSec += dt
		c.st.RunSinceDef += dt
	}
}

func (c *Core) afterControl(dt float64) {
	if c.prevDefrost && !c.ctl.defrost {
		c.suppressOther = c.t + c.cfg.SuppressDefrost
		c.suppressUntil = math.Max(c.suppressUntil, c.suppressOther)
		c.suppressRHUntil = math.Max(c.suppressRHUntil, c.suppressOther)
	}
	c.prevDefrost = c.ctl.defrost
	if c.ctl.comp.on && c.ctl.comp.lastStart == c.t {
		c.st.CompStarts++
	}
	if c.logDamageUntil != 0 && c.t >= c.logDamageUntil {
		c.clear(CodeLogDamage)
		c.logDamageUntil = 0
	}
	if c.weightStepT != 0 {
		on := c.t-c.weightStepT < 3600
		if on {
			c.raise(CodeWeightStep, c.weightF.v, 0)
		} else {
			c.clear(CodeWeightStep)
			c.weightStepT = 0
		}
	}
}

// updateOutageRecord fills in the outage after power returned: first valid
// temperature, the highest reading, and how long it took to get back in band.
func (c *Core) updateOutageRecord() {
	if !c.outagePending {
		return
	}
	t := c.t
	set := c.setpoint()
	lim := &c.st.Limits
	if c.chT.quality == api.QualityOK {
		v := c.chT.val
		if !c.outage.TempKnown {
			c.outage.TempKnown = true
			c.outage.TempOnBackC = v
			c.outage.PeakTempC = v
			c.event(Event{Type: EvOutage, Msg: fmt.Sprintf("temperature on return %.1f C", v), Peak: v})
			// An estimate for the process record: the mean of before and after, over the
			// known length. The control log has nothing for this time.
			if p := &c.st.Program; c.outage.DurationOK && (p.State == api.RunActive || p.State == api.RunPaused) {
				if ex := (c.lastAliveT+v)/2 - 15.6; ex > 0 {
					p.DHOutageEst += ex * c.outage.Duration.Seconds() / 3600
				}
			}
		}
		if v > c.outage.PeakTempC {
			c.outage.PeakTempC = v
		}
		if a := c.al.active[CodeOutage]; a != nil {
			a.a.Peak = c.outage.PeakTempC
			set := c.setpoint()
			if c.outage.PeakTempC > lim.AbsTempHighC || (set.TempC < 15.6 && c.outage.PeakTempC > 15.6) {
				c.escalate(a, api.SevCritical)
			}
		}
	}
	inBand := c.chT.quality == api.QualityOK && c.tS.v < set.TempC+lim.TempHighDelta && c.tS.v > set.TempC-lim.TempLowDelta
	// The cabinet keeps warming through the power-on delay, so the record stays
	// open past it even if the first reading is in band.
	settled := inBand && t-c.bootT >= c.cfg.PowerOnDelay+180
	window := t-c.bootT >= c.cfg.SuppressPowerOn
	if settled || window {
		c.outagePending = false
		if settled {
			c.outage.RecoverSec = t - c.bootT
		}
		a := c.al.active[CodeOutage]
		peakTxt := "peak unknown: no valid reading"
		if c.outage.TempKnown {
			peakTxt = fmt.Sprintf("peak %.1f C", c.outage.PeakTempC)
		}
		c.event(Event{Type: EvOutageEnd, Msg: fmt.Sprintf("recovered=%v after %.0f s, %s", settled, t-c.bootT, peakTxt), Peak: c.outage.PeakTempC, Dur: t - c.bootT})
		short := c.outage.DurationOK && c.outage.Duration < time.Minute
		peakOK := c.outage.PeakTempC <= set.TempC+lim.TempHighDelta
		switch {
		case a == nil:
		case a.a.Acked || (short && peakOK):
			c.clear(CodeOutage)
		case !peakOK || !c.outage.DurationOK || c.outage.Duration >= time.Hour:
			// Over the band on return, unknown length or long: worth telling someone.
			c.escalate(a, maxSev(a.a.Severity, api.SevWarning))
		}
	}
}

func maxSev(a, b api.Severity) api.Severity {
	if a == api.SevCritical || b == api.SevCritical {
		return api.SevCritical
	}
	if a == api.SevWarning || b == api.SevWarning {
		return api.SevWarning
	}
	return api.SevInfo
}

func (c *Core) accumulate(dt float64) {
	a := &c.acc
	a.dur += dt
	a.n++
	if c.chT.quality == api.QualityOK {
		v := c.chT.val
		a.sumT += v
		a.tn++
		a.minT = math.Min(a.minT, v)
		a.maxT = math.Max(a.maxT, v)
	}
	if c.chRH.quality == api.QualityOK {
		v := c.chRH.val
		a.sumH += v
		a.hn++
		a.minH = math.Min(a.minH, v)
		a.maxH = math.Max(a.maxH, v)
	}
	if c.chAir.quality == api.QualityOK {
		a.sumA += c.airF.v
		a.an++
	}
	if c.weightUsable() {
		a.sumG += c.weightF.v
		a.gn++
	}
	if c.ctl.comp.on {
		a.cool += dt
	}
	if c.ctl.heatOn {
		a.heat += dt
	}
	if c.ctl.humOn {
		a.humid += dt
	}
	if c.ctl.dehumOut {
		a.dehum += dt
	}
	if c.ctl.defrost {
		a.def += dt
	}
	if c.doorOpen {
		a.door += dt
	}
	a.fan += c.fanOut
	a.rpm += c.rpm
	if c.t >= c.nextLogT {
		c.writeLog()
		c.nextLogT = c.t + c.cfg.LogInterval
	}
	if c.t >= c.nextCkpt {
		c.saveReq = true
		c.nextCkpt = c.t + c.cfg.CheckpointSec
	}
}

func (c *Core) writeLog() {
	a := &c.acc
	set := c.setpoint()
	p := &c.st.Program
	alarmsN := 0
	for _, x := range c.al.active {
		if x.a.Severity != api.SevInfo {
			alarmsN++
		}
	}
	l := LogRecord{
		Boot: c.st.BootCount, Mono: c.t, Powered: c.st.PoweredSec, Wall: c.clock.Now(), ClkOK: c.clockOK,
		TQ: string(c.chT.quality), RHQ: string(c.chRH.quality), AirQ: string(c.chAir.quality),
		SetT: set.TempC, SetRH: set.RHPct, SetAir: set.AirMS,
		AlarmN: alarmsN, Phase: p.PhaseIndex, PhEl: p.PhaseElapsed, TotEl: p.TotalElapsed, Start: p.StartWeightG, DH: p.DegreeHours,
		Run: string(p.State),
	}
	if p.Recipe != nil {
		l.Recipe = p.Recipe.ID
	}
	if a.tn > 0 {
		l.T, l.TMin, l.TMax = a.sumT/a.tn, a.minT, a.maxT
	}
	if a.hn > 0 {
		l.RH, l.RHMin, l.RHMax = a.sumH/a.hn, a.minH, a.maxH
	}
	if a.an > 0 {
		l.Air = a.sumA / a.an
	}
	if a.gn > 0 {
		l.Wt = a.sumG / a.gn
		l.WtQ = string(api.QualityOK)
	} else if c.scaleCal() {
		l.WtQ = string(c.chScale.quality)
	} else {
		l.WtQ = string(api.QualityOff)
	}
	if a.dur > 0 {
		l.Cool, l.Heat, l.Humid, l.Dehum = a.cool/a.dur, a.heat/a.dur, a.humid/a.dur, a.dehum/a.dur
		l.Def, l.Door = a.def/a.dur, a.door/a.dur
	}
	if a.n > 0 {
		l.Fan, l.RPM = a.fan/a.n, a.rpm/a.n
	}
	a.reset()
	c.logBuf = append(c.logBuf, l)
}

// flush performs all storage work outside the lock.
func (c *Core) flush() {
	c.mu.Lock()
	evs := c.pendingEv
	c.pendingEv = nil
	logs := c.logBuf
	c.logBuf = nil
	var st *State
	if c.saveReq {
		s := c.st.clone()
		st = &s
		c.saveReq = false
	}
	c.mu.Unlock()
	failed := false
	for i := range evs {
		if err := c.store.AppendEvent(evs[i]); err != nil {
			failed = true
		}
	}
	for i := range logs {
		if err := c.store.AppendLog(logs[i]); err != nil {
			failed = true
		}
	}
	if st != nil {
		if err := c.store.SaveState(*st); err != nil {
			failed = true
			c.mu.Lock()
			c.saveReq = false // the next checkpoint or change will retry
			c.mu.Unlock()
		}
	}
	if len(evs)+len(logs) > 0 || st != nil {
		c.mu.Lock()
		s := c.store.Status()
		c.storeStatus = s
		if failed {
			c.storeStatus.OK = false
		}
		c.mu.Unlock()
	}
}

func (c *Core) sample() api.Sample {
	s := api.Sample{T: c.clock.Now(), Valid: c.chT.quality == api.QualityOK && c.chRH.quality == api.QualityOK}
	if c.chT.quality == api.QualityOK {
		s.TempC = c.tF.v
	}
	if c.chRH.quality == api.QualityOK {
		s.RHPct = c.rhF.v
	}
	if c.chAir.quality == api.QualityOK {
		s.AirMS = c.airF.v
	}
	if c.weightUsable() {
		s.WeightG = c.weightF.v
	}
	return s
}

const (
	trendFineSec   = 120
	trendCoarseSec = 3600
	trendFineN     = 720 // 24 h
	trendCoarseN   = 720 // 30 days
)

func (c *Core) trendTick() {
	if c.t < c.nextFine {
		return
	}
	c.nextFine = c.t + trendFineSec
	s := c.sample()
	c.fine = append(c.fine, s)
	if len(c.fine) > 2*trendFineN {
		c.fine = append(c.fine[:0], c.fine[len(c.fine)-trendFineN:]...)
	}
	if c.t >= c.nextCoarse {
		c.nextCoarse = c.t + trendCoarseSec
		c.coarse = append(c.coarse, s)
		if len(c.coarse) > 2*trendCoarseN {
			c.coarse = append(c.coarse[:0], c.coarse[len(c.coarse)-trendCoarseN:]...)
		}
	}
}

func (c *Core) restoreTrend(tail []LogRecord) {
	var lastF, lastC float64 = -1e18, -1e18
	for i := range tail {
		l := &tail[i]
		s := api.Sample{T: l.Wall, TempC: l.T, RHPct: l.RH, AirMS: l.Air, WeightG: l.Wt,
			Valid: l.TQ == string(api.QualityOK) && l.RHQ == string(api.QualityOK)}
		if l.Powered-lastC >= trendCoarseSec {
			lastC = l.Powered
			c.coarse = append(c.coarse, s)
		}
		if l.Powered-lastF >= trendFineSec {
			lastF = l.Powered
			c.fine = append(c.fine, s)
		}
	}
	if len(c.fine) > trendFineN {
		c.fine = c.fine[len(c.fine)-trendFineN:]
	}
	if len(c.coarse) > trendCoarseN {
		c.coarse = c.coarse[len(c.coarse)-trendCoarseN:]
	}
}

func reading(ch *channel, v float64, t float64) api.Reading {
	age := 0.0
	if ch.everGood {
		age = t - ch.lastGoodT
	}
	r := api.Reading{Value: v, Quality: ch.quality, AgeSec: age, Note: ch.note}
	if !ch.everGood {
		r.Value = 0
		r.AgeSec = -1
	}
	return r
}

// Snapshot implements api.Controller.
func (c *Core) Snapshot() api.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.t
	s := api.Snapshot{Now: c.clock.Now()}
	tv := c.tF.v
	if !c.tF.init {
		tv = c.chT.val
	}
	rv := c.rhF.v
	if !c.rhF.init {
		rv = c.chRH.val
	}
	s.Temp = reading(c.chT, tv, t)
	s.RH = reading(c.chRH, rv, t)
	// A suspicion shows on the reading. Control goes on using it.
	if c.st.TDoubtUntil != 0 && s.Temp.Quality == api.QualityOK {
		s.Temp.Quality, s.Temp.Note = api.QualitySuspect, "disagrees with how the cabinet is behaving; check with a separate thermometer"
	}
	if c.st.RHDoubtUntil != 0 && s.RH.Quality == api.QualityOK {
		s.RH.Quality, s.RH.Note = api.QualitySuspect, "disagrees with how the cabinet is behaving; check with a separate hygrometer"
	}
	s.Air = reading(c.chAir, c.airF.v, t)
	if c.scaleCal() {
		s.Weight = reading(c.chScale, c.weightF.v, t)
		if c.scaleImplausible {
			s.Weight.Quality = api.QualitySuspect
			s.Weight.Note = "weight trend is not physically plausible"
		}
	} else {
		s.Weight = api.Reading{Quality: api.QualityOff, Note: "scale not calibrated"}
	}
	switch {
	case c.doorFitted:
		v := 0.0
		if c.doorOpen {
			v = 1
		}
		s.Door = api.Reading{Value: v, Quality: api.QualityOK}
	case c.doorFittedEver:
		s.Door = api.Reading{Quality: api.QualityFault, Note: "door switch not responding"}
	default:
		s.Door = api.Reading{Quality: api.QualityOff, Note: "no door switch"}
	}
	s.Setpoint = c.setpoint()
	s.Outputs = api.Outputs{Cool: c.ctl.comp.on, Heat: c.ctl.heatOn, Humid: c.ctl.humOn, Dehumid: c.ctl.dehumOut,
		Defrost: c.ctl.defrost, FanDutyPct: c.fanOut, FanRPM: c.rpm, Holds: c.holds(), Status: c.outputStatus()}
	s.Program = c.programView()
	s.Active = c.activeList()
	n := len(c.al.history)
	const histShow = 200
	start := 0
	if n > histShow {
		start = n - histShow
	}
	s.History = make([]api.Alarm, 0, n-start)
	for i := n - 1; i >= start; i-- {
		s.History = append(s.History, c.al.history[i])
	}
	s.BuzzerSilenced = c.al.silenced
	s.Trend = c.trendView()
	s.Recipes = c.allRecipes()
	s.Limits = c.st.Limits
	s.Calibration = c.st.Cal
	s.Health = api.Health{Simulated: c.opts.Simulated, SpeedFactor: c.opts.Speed, ClockTrusted: c.clockOK,
		LogOK: c.storeStatus.OK, StorageFreeM: c.storeStatus.FreeMB, WatchdogOK: c.wdOK, StartedAt: c.startedWall,
		LastOutage: c.outage, Version: c.opts.Version, RefCheckDueSec: c.refDue() - (c.st.PoweredSec - c.st.LastCal), LastRefCheck: c.st.LastRefWall}
	return s
}

func (c *Core) trendView() []api.Sample {
	fine := c.fine
	if len(fine) > trendFineN {
		fine = fine[len(fine)-trendFineN:]
	}
	coarse := c.coarse
	if len(coarse) > trendCoarseN {
		coarse = coarse[len(coarse)-trendCoarseN:]
	}
	out := make([]api.Sample, 0, len(fine)+len(coarse))
	var first time.Time
	if len(fine) > 0 {
		first = fine[0].T
	}
	for _, s := range coarse {
		if len(fine) == 0 || s.T.Before(first) {
			out = append(out, s)
		}
	}
	return append(out, fine...)
}

// refDue is how many powered seconds a reference check lasts for right now.
func (c *Core) refDue() float64 {
	if c.st.Program.State == api.RunActive || c.st.Program.State == api.RunPaused {
		return c.cfg.CalReminderSec
	}
	return c.cfg.CalReminderIdleSec
}

// EdgeCount is how many brief closes of the contact core has started, one for each
// alarm raised or escalated while the contact was already signalling. cured turns
// each new one into its own brief close, so a new alarm is a fresh transition even
// while one of its own system alarms holds the contact (ADR 0408).
func (c *Core) EdgeCount() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.al.edges
}

// outputStatus explains the regulated outputs. It is computed on demand only.
func (c *Core) outputStatus() []api.OutputStatus {
	ctl := &c.ctl
	t := c.t
	set := c.setpoint()
	door := c.doorOpen && t-c.doorOpenT < c.cfg.DoorHold
	tOK, rhOK := c.chT.ok(), c.chRH.ok()
	st := func(out string, on bool, kind, why string) api.OutputStatus {
		return api.OutputStatus{Output: out, On: on, Kind: kind, Reason: why}
	}
	var cool api.OutputStatus
	const co = "cool"
	switch {
	case ctl.comp.on && !tOK:
		cool = st(co, true, "fault", "running a blind pattern: temperature data is bad")
	case ctl.comp.on && ctl.dehum:
		cool = st(co, true, "normal", "removing moisture")
	case ctl.comp.on:
		cool = st(co, true, "normal", "cooling")
	case ctl.defrost:
		cool = st(co, false, "hold", "defrost")
	case t < ctl.dripUntil:
		cool = st(co, false, "hold", "draining after defrost")
	case door:
		cool = st(co, false, "hold", "door open")
	case ctl.comp.holdReason != "":
		cool = st(co, false, "hold", ctl.comp.holdReason)
	case !tOK:
		cool = st(co, false, "fault", "temperature data is bad; between blind runs")
	case ctl.coolWant:
		cool = st(co, false, "hold", "starting")
	case c.tS.v > set.TempC:
		cool = st(co, false, "normal", fmt.Sprintf("%.1f °C over setpoint: building up demand for the next run", c.tS.v-set.TempC))
	default:
		cool = st(co, false, "normal", "at or under setpoint")
	}
	var heat api.OutputStatus
	const he = "heat"
	switch {
	case ctl.heatOn && ctl.defrost:
		heat = st(he, true, "normal", "warming air for defrost")
	case ctl.heatOn && ctl.dehum:
		heat = st(he, true, "normal", "reheat while removing moisture")
	case ctl.heatOn:
		heat = st(he, true, "normal", "heating")
	case !tOK:
		heat = st(he, false, "fault", "locked off: temperature data is bad")
	case door:
		heat = st(he, false, "hold", "door open")
	case !ctl.airflowProven:
		heat = st(he, false, "fault", "locked off: no airflow proven")
	case c.st.TDoubtUntil != 0 || c.st.RHDoubtUntil != 0:
		heat = st(he, false, "hold", "locked off while a reading is in doubt: only cooling is allowed")
	case t-c.bootT <= 30:
		heat = st(he, false, "hold", "settling after power-on")
	case c.tS.v >= set.TempC:
		heat = st(he, false, "normal", "at or over setpoint")
	case ctl.heatDemand > 0:
		heat = st(he, false, "normal", fmt.Sprintf("%.1f °C under setpoint: on %.0f %% of each minute, off between pulses", set.TempC-c.tS.v, 100*ctl.heatDemand))
	default:
		heat = st(he, false, "normal", fmt.Sprintf("%.1f °C under setpoint: this cabinet needs cooling overall, so the room brings it back", set.TempC-c.tS.v))
	}
	var hum api.OutputStatus
	const hu = "humid"
	switch {
	case ctl.humOn:
		hum = st(hu, true, "normal", "adding moisture")
	case !tOK || !rhOK:
		hum = st(hu, false, "fault", "locked off: temperature or humidity data is bad")
	case !ctl.airflowProven:
		hum = st(hu, false, "fault", "locked off: no airflow proven")
	case ctl.defrost || t < ctl.dripUntil:
		hum = st(hu, false, "hold", "defrost")
	case door:
		hum = st(hu, false, "hold", "door open")
	case t-c.bootT <= 300:
		hum = st(hu, false, "hold", "settling after power-on")
	case c.rhS.v >= set.RHPct:
		hum = st(hu, false, "normal", "at or over setpoint")
	case t < ctl.humNext:
		hum = st(hu, false, "normal", "waiting to see the last pulse settle")
	default:
		hum = st(hu, false, "normal", "under setpoint, within the band left for compressor cycling")
	}
	// What the cabinet's response proves overrides what was commanded.
	if c.al.active[CodeCoolStuck] != nil {
		cool = st(co, true, "stuck", "relay appears stuck closed: the air falls with the compressor commanded off")
	}
	if c.al.active[CodeHeatStuck] != nil {
		heat = st(he, true, "stuck", "appears stuck on: the compressor runs nearly all the time with the heater off and the air does not come down")
	}
	if c.al.active[CodeHumidOnStuck] != nil {
		hum = st(hu, true, "stuck", "appears stuck on: humidity is near saturation with the humidifier off")
	}
	// A failed write leaves every relay where it was, or wherever the guard put it:
	// nothing here can say which (ADR 0407).
	if c.outputErr != "" {
		why := "outputs cannot be written, so the real state is not known: " + c.outputErr
		cool, heat, hum = st(co, false, "unknown", why), st(he, false, "unknown", why), st(hu, false, "unknown", why)
	}
	return []api.OutputStatus{cool, heat, hum}
}

// holds says what is holding each output off, so that a wait never looks like a
// fault or like nothing happening.
func (c *Core) holds() []api.Hold {
	var hs []api.Hold
	t := c.t
	add := func(out, why string, until float64) {
		rem := until - t
		if rem < 0 {
			rem = 0
		}
		hs = append(hs, api.Hold{Output: out, Reason: why, RemainingSec: rem})
	}
	ctl := &c.ctl
	if ctl.comp.holdReason != "" {
		add(string(hal.OutCool), ctl.comp.holdReason, ctl.comp.holdUntil)
	}
	if ctl.defrost {
		add(string(hal.OutCool), "defrost", ctl.defrostStart+c.cfg.DefrostDuration)
	} else if t < ctl.dripUntil {
		add(string(hal.OutCool), "drip after defrost", ctl.dripUntil)
	}
	if c.doorOpen && t-c.doorOpenT < c.cfg.DoorHold {
		add(string(hal.OutCool), "door open", c.doorOpenT+c.cfg.DoorHold)
	}
	tOK := c.chT.ok()
	if !tOK {
		add(string(hal.OutHeat), "temperature data is bad", t)
		add(string(hal.OutHumid), "temperature data is bad", t)
	} else if !ctl.airflowProven {
		add(string(hal.OutHeat), "airflow not proven", t)
		add(string(hal.OutHumid), "airflow not proven", t)
	} else if !c.chRH.ok() {
		add(string(hal.OutHumid), "humidity data is bad", t)
	}
	if t-c.bootT < 300 {
		add(string(hal.OutHumid), "settling after power-on", c.bootT+300)
	}
	return hs
}

// Do implements api.Controller.
func (c *Core) Do(cmd api.Command) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.do(cmd)
	msg := string(cmd.Kind)
	if err != nil {
		msg += " refused: " + err.Error()
	} else {
		msg += describe(cmd)
	}
	c.event(Event{Type: EvCmd, Msg: msg})
	return err
}

func describe(cmd api.Command) string {
	switch {
	case cmd.Setpoint != nil:
		return fmt.Sprintf(" T=%.1f RH=%.1f air=%.2f", cmd.Setpoint.TempC, cmd.Setpoint.RHPct, cmd.Setpoint.AirMS)
	case cmd.RecipeID != "":
		return " " + cmd.RecipeID
	case cmd.AlarmID != "":
		return " " + cmd.AlarmID
	case cmd.KnownWeightG != 0:
		return fmt.Sprintf(" %.1f g", cmd.KnownWeightG)
	case cmd.Limits != nil:
		return fmt.Sprintf(" %+v", *cmd.Limits)
	case cmd.Calibration != nil:
		return fmt.Sprintf(" %+v", *cmd.Calibration)
	case cmd.Recipe != nil:
		return " " + cmd.Recipe.ID
	}
	return ""
}

func validateSetpoints(s api.Setpoints) error {
	if math.IsNaN(s.TempC) || s.TempC < 0 || s.TempC > 35 {
		return errors.New("temperature setpoint must be from 0 to 35 °C")
	}
	if math.IsNaN(s.RHPct) || s.RHPct < 40 || s.RHPct > 95 {
		return errors.New("humidity setpoint must be from 40 to 95 %RH")
	}
	if math.IsNaN(s.AirMS) || s.AirMS < 0 || s.AirMS > 5 {
		return errors.New("airflow setpoint must be from 0 to 5 m/s")
	}
	return nil
}

func (c *Core) do(cmd api.Command) error {
	p := &c.st.Program
	switch cmd.Kind {
	case api.CmdSetSetpoint:
		if cmd.Setpoint == nil {
			return errors.New("no setpoint given")
		}
		if err := validateSetpoints(*cmd.Setpoint); err != nil {
			return err
		}
		if p.State == api.RunActive || p.State == api.RunPaused || p.State == api.RunFinished {
			// Applies to this phase only and is on the record; see docs/adr/0112.
			sp := *cmd.Setpoint
			was := c.setpoint()
			p.Override = &sp
			c.event(Event{Type: EvPhase, Msg: fmt.Sprintf("operator override of phase %d: T %.1f>%.1f RH %.1f>%.1f air %.2f>%.2f", p.PhaseIndex, was.TempC, sp.TempC, was.RHPct, sp.RHPct, was.AirMS, sp.AirMS)})
			c.setpointChanged()
			c.saveSoon()
			return nil
		}
		c.st.Manual = *cmd.Setpoint
		c.setpointChanged()
		c.saveSoon()
	case api.CmdStart:
		if p.State == api.RunActive || p.State == api.RunPaused {
			return errors.New("a recipe is already running; stop it first")
		}
		r := c.findRecipe(cmd.RecipeID)
		if r == nil {
			return fmt.Errorf("no recipe named %q", cmd.RecipeID)
		}
		if err := validateRecipe(r); err != nil {
			return err
		}
		if cmd.StartWeightG < 0 {
			return errors.New("start weight cannot be negative")
		}
		*p = ProgramState{State: api.RunActive, Recipe: r, StartWeightG: cmd.StartWeightG}
		c.clear(CodeCureFinished)
		c.setpointChanged()
		c.event(Event{Type: EvPhase, Msg: fmt.Sprintf("recipe %s started, phase 0: %s", r.ID, r.Phases[0].Name)})
		c.saveSoon()
	case api.CmdPause:
		if p.State != api.RunActive {
			return errors.New("no recipe is running")
		}
		p.State = api.RunPaused
		c.saveSoon()
	case api.CmdResume:
		if p.State != api.RunPaused {
			return errors.New("the recipe is not paused")
		}
		p.State = api.RunActive
		c.saveSoon()
	case api.CmdStop:
		if p.State == api.RunIdle {
			return errors.New("no recipe to stop")
		}
		// Keep holding the phase setpoints as manual, rather than jumping
		// somewhere else with product still inside.
		c.st.Manual = c.setpoint()
		*p = ProgramState{State: api.RunIdle}
		c.clear(CodeCureFinished)
		c.saveSoon()
	case api.CmdNextPhase:
		if p.State != api.RunActive && p.State != api.RunPaused {
			return errors.New("no recipe is running")
		}
		was := p.State
		c.advancePhase("operator")
		if was == api.RunPaused && p.State == api.RunActive {
			p.State = was
		}
	case api.CmdAck:
		found := false
		for _, code := range append([]string(nil), c.al.order...) {
			a := c.al.active[code]
			if cmd.AlarmID == "" || a.a.ID == cmd.AlarmID {
				found = true
				if !a.a.Acked {
					a.a.Acked = true
					a.ackT = c.t
					c.event(Event{Type: EvAck, Code: code, ID: a.a.ID})
				}
				if code == CodeOutage && !c.outagePending {
					c.clear(code)
				}
				if code == CodeWeightStep {
					c.weightStepT = 0
					c.clear(code)
				}
				if code == CodeCureFinished || code == CodeOutDropped || code == CodeLogDamage {
					// Cure finished stays until the recipe is stopped; the others
					// are done once somebody has seen them.
					if code != CodeCureFinished {
						c.clear(code)
					}
				}
			}
		}
		if !found && cmd.AlarmID != "" {
			return errors.New("that alarm is no longer active")
		}
		c.updateBuzzer()
	case api.CmdSilence:
		c.al.silenced = true
		c.al.silenceT = c.t
		c.event(Event{Type: EvSilence})
		c.updateBuzzer()
	case api.CmdSetLimits:
		if cmd.Limits == nil {
			return errors.New("no limits given")
		}
		if err := validateLimits(*cmd.Limits); err != nil {
			return err
		}
		c.st.Limits = *cmd.Limits
		c.saveSoon()
	case api.CmdSetCal:
		if cmd.Calibration == nil {
			return errors.New("no calibration given")
		}
		k := *cmd.Calibration
		if math.Abs(k.TempOffsetC) > 12 {
			return errors.New("temperature offset must be within ±12 °C")
		}
		if math.Abs(k.RHOffsetPct) > 15 {
			return errors.New("humidity offset must be within ±15 %RH")
		}
		// A changed offset is a person acting on a check, so doubts about the
		// reading end here. The scale fields alone do not count.
		if k.TempOffsetC != c.st.Cal.TempOffsetC || k.RHOffsetPct != c.st.Cal.RHOffsetPct {
			c.st.LastCal = c.st.PoweredSec
			c.st.TDoubtUntil, c.st.RHDoubtUntil = 0, 0
			c.st.TDoubtLow = false
			c.clear(CodeTempDisagree)
			c.clear(CodeRHDisagree)
			c.clear(CodeRefDisagree)
			c.crossReset()
		}
		c.st.Cal = k
		c.saveSoon()
	case api.CmdTare:
		if p.State == api.RunActive || p.State == api.RunPaused {
			return errors.New("taring during a cure would invalidate the weight loss; stop the recipe first")
		}
		if !c.chScale.have || c.chScale.quality != api.QualityOK {
			return errors.New("the scale is not reading")
		}
		c.st.Cal.ScaleZero = int32(c.chScale.val)
		c.scaleRawF.reset()
		c.wt = weightTrack{}
		c.saveSoon()
	case api.CmdCalWeight:
		if cmd.KnownWeightG <= 0 {
			return errors.New("enter the known weight in grams")
		}
		if p.State == api.RunActive || p.State == api.RunPaused {
			return errors.New("calibrating during a cure would invalidate the weight loss; stop the recipe first")
		}
		if !c.chScale.have || c.chScale.quality != api.QualityOK {
			return errors.New("the scale is not reading")
		}
		cpg := (c.chScale.val - float64(c.st.Cal.ScaleZero)) / cmd.KnownWeightG
		if math.Abs(cpg) < 1 {
			return errors.New("too little change on the scale: is the weight on it, and was it tared first?")
		}
		c.st.Cal.ScaleCountsPG = cpg
		c.scaleRawF.reset()
		c.wt = weightTrack{}
		c.saveSoon()
	case api.CmdRefCheck:
		return c.referenceCheck(cmd.Reference)
	case api.CmdDefrost:
		if c.ctl.defrost {
			return errors.New("a defrost is already running")
		}
		c.ctl.defrostForce = true
	case api.CmdSaveRecipe:
		if cmd.Recipe == nil {
			return errors.New("no recipe given")
		}
		r := *cmd.Recipe
		r.BuiltIn = false
		r.Phases = append([]api.PhaseSpec(nil), r.Phases...)
		if err := validateRecipe(&r); err != nil {
			return err
		}
		for _, b := range BuiltInRecipes() {
			if b.ID == r.ID {
				return errors.New("a built-in recipe has that ID; choose another")
			}
		}
		replaced := false
		for i := range c.st.UserRecipes {
			if c.st.UserRecipes[i].ID == r.ID {
				c.st.UserRecipes[i] = r
				replaced = true
			}
		}
		if !replaced {
			c.st.UserRecipes = append(c.st.UserRecipes, r)
		}
		c.saveSoon()
	case api.CmdDeleteRecipe:
		for _, b := range BuiltInRecipes() {
			if b.ID == cmd.RecipeID {
				return errors.New("built-in recipes cannot be deleted")
			}
		}
		if p.Recipe != nil && p.Recipe.ID == cmd.RecipeID && p.State != api.RunIdle {
			return errors.New("that recipe is running")
		}
		for i := range c.st.UserRecipes {
			if c.st.UserRecipes[i].ID == cmd.RecipeID {
				c.st.UserRecipes = append(c.st.UserRecipes[:i], c.st.UserRecipes[i+1:]...)
				c.saveSoon()
				return nil
			}
		}
		return errors.New("no such recipe")
	default:
		return fmt.Errorf("unknown command %q", cmd.Kind)
	}
	return nil
}

func validateRecipe(r *api.Recipe) error {
	if r.ID == "" || r.Name == "" {
		return errors.New("a recipe needs an ID and a name")
	}
	if len(r.Phases) == 0 {
		return errors.New("a recipe needs at least one phase")
	}
	for i, ph := range r.Phases {
		if err := validateSetpoints(ph.Setpoint); err != nil {
			return fmt.Errorf("phase %d: %w", i+1, err)
		}
		if ph.Duration < 0 {
			return fmt.Errorf("phase %d: duration cannot be negative", i+1)
		}
		if ph.TargetLossPct < 0 || ph.TargetLossPct > 80 {
			return fmt.Errorf("phase %d: weight-loss target must be from 0 to 80 %%", i+1)
		}
	}
	return nil
}

func validateLimits(l api.Limits) error {
	switch {
	case l.TempHighDelta < 0.5 || l.TempHighDelta > 20:
		return errors.New("high temperature band must be 0.5 to 20 °C")
	case l.TempLowDelta < 0.5 || l.TempLowDelta > 20:
		return errors.New("low temperature band must be 0.5 to 20 °C")
	case l.RHHighDelta < 1 || l.RHHighDelta > 40:
		return errors.New("high humidity band must be 1 to 40 %RH")
	case l.RHLowDelta < 1 || l.RHLowDelta > 40:
		return errors.New("low humidity band must be 1 to 40 %RH")
	case l.AlarmDelay < 0 || l.AlarmDelay > 4*time.Hour:
		return errors.New("alarm delay must be up to 4 hours")
	case l.DoorDelay < 30*time.Second || l.DoorDelay > 4*time.Hour:
		return errors.New("door delay must be from 30 s to 4 hours")
	case l.AbsTempHighC < 5 || l.AbsTempHighC > 60:
		return errors.New("absolute high limit must be 5 to 60 °C")
	case l.AbsTempLowC < -10 || l.AbsTempLowC > 5:
		return errors.New("absolute low limit must be -10 to 5 °C")
	}
	return nil
}

// Close is an orderly stop: outputs off, then a state marked clean. Without this
// marker the next boot treats the stop as a power loss.
func (c *Core) Close() error {
	if c.cab.Outputs != nil {
		_ = c.cab.Outputs.AllOff()
	}
	if c.cab.Fan != nil {
		_ = c.cab.Fan.SetDuty(0)
	}
	c.mu.Lock()
	c.writeLog()
	c.event(Event{Type: EvShutdown})
	c.st.Clean = true
	c.saveReq = true
	c.mu.Unlock()
	c.flush()
	return c.store.Close()
}

// Abandon drops the Core with no shutdown work. It models power loss in a
// simulation: nothing more is written.
func (c *Core) Abandon() {}

// Powered returns seconds of powered time across all boots.
func (c *Core) Powered() float64 { c.mu.Lock(); defer c.mu.Unlock(); return c.st.PoweredSec }

// Status is a compact view for tests and scenario logs.
func (c *Core) CounterSnapshot() (starts uint64, runSec float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st.CompStarts, c.st.CompRunSec
}

var _ api.Controller = (*Core)(nil)
