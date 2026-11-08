package core

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// alarmDef is the fixed description of a code. Titles state what is known, and
// actions are what a person standing at the cabinet can do.
type alarmDef struct {
	Sev    api.Severity
	Title  string
	Action string
	Unit   string
}

var alarmDefs = map[string]alarmDef{
	CodeTempHigh:     {api.SevWarning, "Temperature high", "Check the door and the room. Check the compressor is running.", "°C"},
	CodeTempLow:      {api.SevWarning, "Temperature low", "Check the heater, the room temperature and whether the compressor is stuck on.", "°C"},
	CodeTempAbsHigh:  {api.SevCritical, "Temperature over absolute limit", "Inspect product and judge it by your own process plan. Find the cause before continuing.", "°C"},
	CodeTempAbsLow:   {api.SevCritical, "Temperature near freezing", "Check whether the compressor relay is stuck on. Product may freeze.", "°C"},
	CodeRHHigh:       {api.SevWarning, "Humidity high", "Check the door, the humidifier and for a wet load. Check the drain.", "%RH"},
	CodeRHLow:        {api.SevWarning, "Humidity low", "Check the door seal and that the humidifier has water and runs. Heat running when it is not needed also dries the air.", "%RH"},
	CodeAirLow:       {api.SevWarning, "Airflow low", "Check the manifold and filter for blockage. Check the fan.", "m/s"},
	CodeAirHigh:      {api.SevInfo, "Airflow above target", "The fan cannot go slower. Lower resistance or accept the higher flow.", "m/s"},
	CodeDoorOpen:     {api.SevWarning, "Door open too long", "Close the door.", "s"},
	CodeOutage:       {api.SevWarning, "Power was lost", "Read the outage record. Inspect product and judge it by your own process plan.", "°C"},
	CodeRestarted:    {api.SevWarning, "Controller stopped and restarted", "Control stopped without an orderly shutdown, with power on, and restarted; outputs were off meanwhile. Read the outage record and the system log.", "°C"},
	CodeClimateFault: {api.SevCritical, "Temperature and humidity sensor not reading", "Check the DHT22 wiring. Heater and humidifier are off; cooling repeats its recent pattern with no feedback.", ""},
	CodeTempSuspect:  {api.SevCritical, "Temperature reading looks wrong", "Check against a separate thermometer. Heater and humidifier are off; cooling repeats its recent pattern with no feedback.", "°C"},
	CodeRHSuspect:    {api.SevCritical, "Humidity reading looks wrong", "Check the sensor. Humidity control is off; temperature control continues.", "%RH"},
	CodeTempErratic:  {api.SevWarning, "Temperature sensor giving bad samples", "Check the wiring. Bad samples are being rejected.", ""},
	CodeRHErratic:    {api.SevWarning, "Humidity sensor giving bad samples", "Check the wiring. Bad samples are being rejected.", ""},
	CodeTempDrift:    {api.SevWarning, "Temperature sensor may have drifted", "Cooling effort shifted with no change in the reading. Check against a separate thermometer. A change of room temperature also causes this.", ""},
	CodeRHDrift:      {api.SevWarning, "Humidity sensor may have drifted", "Humidifying effort shifted with no change in the reading. Check against a separate hygrometer.", ""},
	CodeAirFault:     {api.SevWarning, "Airflow sensor not reading", "Check the FS3000 cable. The fan holds its last good speed.", ""},
	CodeAirSuspect:   {api.SevWarning, "Airflow reading looks wrong", "Check the FS3000. The fan holds its last good speed.", "m/s"},
	CodeScaleFault:   {api.SevWarning, "Scale not reading", "Check the HX711 wiring. Weight-based phase changes are paused; temperature and humidity control continue.", ""},
	CodeScaleSuspect: {api.SevWarning, "Weight reading looks wrong", "Check nothing touches the load cell. Weight-based phase changes are paused.", "g"},
	CodeWeightStep:   {api.SevInfo, "Weight changed suddenly", "Weight loss now covers the new load: added product counts from no loss, removed product as having lost what the rest had. If nothing was added or removed, check the scale.", "g"},
	CodeFanStall:     {api.SevCritical, "Fan stopped", "Check the fan and its cable. Heater and humidifier are off, and cooling is restricted.", ""},
	CodeFanTach:      {api.SevWarning, "Fan speed signal lost", "Check the tach wire. Air is still measured as moving.", ""},
	CodeAirBlocked:   {api.SevCritical, "Fan turning but no air measured", "Check for a blocked manifold or a failed airflow sensor. Heater and humidifier are off.", "m/s"},
	CodeCoolFail:     {api.SevCritical, "Compressor called for but the cabinet is not cooling", "Check the compressor and its relay, and for ice on the coil. A heater stuck on looks the same.", "°C"},
	CodeCoolStuck:    {api.SevCritical, "Cabinet is cooling with the compressor off", "The compressor relay may be stuck closed. Disconnect it if the temperature keeps falling.", "°C"},
	CodeHeatFail:     {api.SevWarning, "Heater on but cabinet is not warming", "Check the heater and its relay.", "°C"},
	CodeHeatStuck:    {api.SevCritical, "Heater appears stuck on", "The cabinet needs far more cooling than usual with the heater off. Feel the heater: if it is warm, disconnect it. If it is cold, the room has become much warmer.", "°C"},
	CodeHumidOnStuck: {api.SevCritical, "Humidifier appears stuck on", "Humidity is near saturation with the humidifier off while the cabinet is drying the air. Check whether the humidifier is running: if it is, disconnect it. If not, look for standing water.", "%RH"},
	CodeHumidFail:    {api.SevWarning, "Humidifier on but humidity is not rising", "Refill the humidifier. Check the relay.", "%RH"},
	CodeHumidStuck:   {api.SevWarning, "Humidity cannot be brought down to setpoint", "Check for a humidifier relay stuck closed, or a very wet load. Check the drain. If the setpoint is beyond this cabinet, change it.", "%RH"},
	CodeOutputFault:  {api.SevCritical, "Output could not be switched", "Check the relay board and its connection.", ""},
	CodeStorage:      {api.SevWarning, "Storage is failing", "Control continues, but records are held in memory only. Check or replace the storage card.", ""},
	CodeStateLost:    {api.SevCritical, "Saved settings could not be read", "Check setpoints, limits, calibration and the recipe. Check the storage card.", ""},
	CodeClock:        {api.SevInfo, "Clock not set", "Times in the record are marked unverified until the clock is set. Cure timing is not affected.", ""},
	CodeCalReminder:  {api.SevInfo, "Sensor check due", "Compare temperature and humidity with a separate reference and adjust the calibration.", ""},
	CodeCureFinished: {api.SevWarning, "Recipe finished: operator must verify", "The controller does not determine whether product is safe. Verify by your own process plan.", ""},
	CodeLogDamage:    {api.SevInfo, "Damaged log lines were skipped", "Expected once after a power cut. If it repeats, check the storage card.", ""},
	CodeDoorSwitch:   {api.SevWarning, "Door switch not responding", "Door alarms are unavailable. Check the switch.", ""},
	CodeTempHighCrit: {api.SevCritical, "Temperature far above setpoint", "Find the cause now. Inspect product and judge it by your own process plan.", "°C"},
	CodeTempDisagree: {api.SevCritical, "Cabinet effort changed with no change in the reading", "The reading may have drifted, or the room changed sharply. Check with a thermometer and enter it. Heat and humidity held off; clears after a check or 48 h.", "°C"},
	CodeRHDisagree:   {api.SevCritical, "Humidity reading disagrees with how the cabinet is behaving", "Check with a separate hygrometer and enter the result. Heating and humidifying are held off. Clears after a passed check or 48 h.", "%RH"},
	CodeDryingShift:  {api.SevWarning, "Drying rate changed with no change of setpoint", "Check temperature and humidity against separate instruments, and check nothing touches the scale.", ""},
	CodeRefDisagree:  {api.SevCritical, "Sensor differs from the reference instrument", "Replace or calibrate the sensor, then check again.", ""},
	CodeOutDropped:   {api.SevCritical, "Outputs were switched off by the hardware guard", "Control stopped renewing the guard (a stalled loop, or outputs that could not be written) and it cut all outputs. Control has resumed. Check the system log.", ""},
	CodeDefrostShort: {api.SevWarning, "Defrost keeps being cut short", "The air reaches its limit before the coil is clear. Check for ice on the coil and the room temperature.", ""},
}

// activeAlarm is an alarm plus what is needed to clear it.
type activeAlarm struct {
	a        api.Alarm
	mono     float64
	restored bool
	warnBeep bool
	// subjectDur is how long an outage alarm's outage lasted, -1 if not known. It is
	// the alarm's own, so the next outage cannot overwrite it.
	subjectDur float64
	// Signalling window; see updateBuzzer.
	callUntil, nextCall, ackT float64
	buzzUntil, nextBuzz       float64
}

// condition tracks the delay for a condition alarm.
type condition struct {
	since float64
}

type alarms struct {
	active    map[string]*activeAlarm
	order     []string // active codes in raise order, for stable output
	history   []api.Alarm
	conds     map[string]*condition
	seq       uint64
	silenced  bool
	silenceT  float64
	edgeUntil float64
	edges     uint64 // brief closes started for a new alarm, for the annunciator
}

func newAlarms() alarms {
	return alarms{active: map[string]*activeAlarm{}, conds: map[string]*condition{}}
}

// raise creates an alarm if not already active.
func (c *Core) raise(code string, peak, limit float64) {
	c.raiseSince(code, peak, limit, time.Time{}, false, -1)
}

// raiseSince raises an alarm whose subject began at since (when known, sinceOK),
// before the moment it could be raised.
func (c *Core) raiseSince(code string, peak, limit float64, since time.Time, sinceOK bool, subjectDur float64) {
	if a, ok := c.al.active[code]; ok {
		c.updatePeak(a, peak)
		return
	}
	def := alarmDefs[code]
	c.al.seq++
	now := c.wallNow()
	al := api.Alarm{
		ID:       fmt.Sprintf("B%d-%d-%s", c.st.BootCount, c.al.seq, code),
		Code:     code,
		Severity: def.Sev,
		Title:    def.Title,
		Action:   def.Action,
		Since:    now,
		Peak:     peak,
		Limit:    limit,
		Unit:     def.Unit,
		ClockOK:  c.clockOK,
	}
	var evSince time.Time
	if sinceOK && !since.IsZero() {
		al.Since, evSince = since, since
	}
	aa := &activeAlarm{a: al, mono: c.t, subjectDur: subjectDur}
	c.al.active[code] = aa
	c.al.order = append(c.al.order, code)
	if def.Sev != api.SevInfo {
		c.announce(aa)
	}
	c.event(Event{Type: EvAlarm, Code: code, ID: al.ID, Sev: string(def.Sev), Title: def.Title, Action: def.Action, Peak: peak, Limit: limit, Unit: def.Unit, Since: evSince, Dur: subjectDur})
}

func (c *Core) updatePeak(a *activeAlarm, v float64) {
	if a.a.Limit == 0 && v == 0 {
		return
	}
	if a.a.Code == CodeTempLow || a.a.Code == CodeRHLow || a.a.Code == CodeTempAbsLow || a.a.Code == CodeAirLow {
		if v < a.a.Peak {
			a.a.Peak = v
		}
		return
	}
	if v > a.a.Peak {
		a.a.Peak = v
	}
}

// clear ends an active alarm and files it in history.
func (c *Core) clear(code string) {
	a, ok := c.al.active[code]
	if !ok {
		return
	}
	a.a.Cleared = c.wallNow()
	a.a.DurationSec = c.t - a.mono
	if a.restored {
		a.a.DurationSec = -1 // spans a restart; duration on one mono clock is meaningless
	}
	if code == CodeOutage || code == CodeRestarted {
		// What matters about an outage is how long control was away, not how long the
		// alarm stayed listed: -1 only when that is not known (integration defect 6).
		a.a.DurationSec = a.subjectDur
	}
	c.al.history = append(c.al.history, a.a)
	const histCap = 5000
	if len(c.al.history) > histCap {
		c.al.history = append(c.al.history[:0], c.al.history[len(c.al.history)-histCap:]...)
	}
	delete(c.al.active, code)
	for i, o := range c.al.order {
		if o == code {
			c.al.order = append(c.al.order[:i], c.al.order[i+1:]...)
			break
		}
	}
	c.event(Event{Type: EvClear, Code: code, ID: a.a.ID, Peak: a.a.Peak, Limit: a.a.Limit, Unit: a.a.Unit, Dur: a.a.DurationSec})
}

// timed runs the delay. raise=true when the condition has held for delay;
// clearOK means it has gone past the clearing differential.
func (c *Core) timed(code string, cond bool, clearOK bool, delay float64, peak, limit float64) {
	st := c.al.conds[code]
	if st == nil {
		st = &condition{since: -1}
		c.al.conds[code] = st
	}
	if cond {
		if st.since < 0 {
			st.since = c.t
		}
		if c.t-st.since >= delay {
			c.raise(code, peak, limit)
		}
		if a, ok := c.al.active[code]; ok {
			c.updatePeak(a, peak)
		}
	} else {
		st.since = -1
		if clearOK {
			c.clear(code)
		}
	}
}

// evalAlarms runs every alarm rule. Alarms never change regulation.
func (c *Core) evalAlarms(dt float64) {
	t := c.t
	lim := &c.st.Limits
	set := c.setpoint()
	cfg := &c.cfg
	ctl := &c.ctl

	// Temperature and humidity bands. Judged on filtered values so that a normal
	// compressor cycle does not reset the delay.
	// A door left open, or a switch stuck reading open, cannot hide a real excursion.
	expected := (c.doorOpen && t-c.doorOpenT < lim.DoorDelay.Seconds()) || ctl.defrost
	suppressedT := expected || t < c.suppressUntil
	suppressedRH := expected || t < c.suppressRHUntil
	tOK := c.chT.quality == api.QualityOK
	rhOK := c.chRH.quality == api.QualityOK
	ts := c.tS.v
	rs := c.rhS.v
	delay := lim.AlarmDelay.Seconds()
	inBandT := tOK && ts < set.TempC+lim.TempHighDelta && ts > set.TempC-lim.TempLowDelta
	inBandRH := rhOK && rs < set.RHPct+lim.RHHighDelta && rs > set.RHPct-lim.RHLowDelta
	// A power-on or setpoint-change window ends the moment that reading first
	// reaches its band: it exists for the pull-down, not to hide later trouble.
	if c.openT && tOK && inBandT {
		c.openT = false
		c.suppressUntil = math.Max(c.suppressOther, t)
	}
	if c.openRH && rhOK && inBandRH {
		c.openRH = false
		c.suppressRHUntil = math.Max(c.suppressOther, t)
	}
	if tOK {
		hi := set.TempC + lim.TempHighDelta
		lo := set.TempC - lim.TempLowDelta
		c.timed(CodeTempHigh, ts > hi && !suppressedT, ts < hi-cfg.TempClearDiff, delay, ts, hi)
		// Far over setpoint, over the 15.6 °C reference for a cabinet set under it, or
		// over the band for 2 h, is critical. See docs/adr/0110.
		if ts > hi && !suppressedT {
			if c.aboveBandT == 0 {
				c.aboveBandT = t
			}
		} else if ts < hi-cfg.TempClearDiff {
			c.aboveBandT = 0
		}
		// The record carries the limit that made it critical, so its peak is never under it.
		critLim := set.TempC + 2*lim.TempHighDelta
		switch {
		case ts > critLim:
		case set.TempC < 15.6 && ts > 15.6 && ts > hi:
			critLim = math.Max(15.6, hi)
		default:
			critLim = hi // over the band for 2 h
		}
		crit := !suppressedT && (ts > set.TempC+2*lim.TempHighDelta || (set.TempC < 15.6 && ts > 15.6 && ts > hi) || (c.aboveBandT != 0 && t-c.aboveBandT >= 7200))
		if a := c.al.active[CodeTempHighCrit]; a != nil {
			critLim = a.a.Limit // one record, one limit
		}
		c.timed(CodeTempHighCrit, crit, ts < hi-cfg.TempClearDiff, delay, ts, critLim)
		c.timed(CodeTempLow, ts < lo && !suppressedT, ts > lo+cfg.TempClearDiff, delay, ts, lo)
		// Absolute limits are never suppressed.
		c.timed(CodeTempAbsHigh, ts > lim.AbsTempHighC, ts < lim.AbsTempHighC-cfg.TempClearDiff, 300, ts, lim.AbsTempHighC)
		c.timed(CodeTempAbsLow, ts < lim.AbsTempLowC, ts > lim.AbsTempLowC+cfg.TempClearDiff, 300, ts, lim.AbsTempLowC)
	}
	if rhOK {
		hi := set.RHPct + lim.RHHighDelta
		lo := set.RHPct - lim.RHLowDelta
		c.timed(CodeRHHigh, rs > hi && !suppressedRH, rs < hi-cfg.RHClearDiff, delay, rs, hi)
		c.timed(CodeRHLow, rs < lo && !suppressedRH, rs > lo+cfg.RHClearDiff, delay, rs, lo)
	}

	// Door.
	if c.doorFitted {
		c.timed(CodeDoorOpen, c.doorOpen, !c.doorOpen, lim.DoorDelay.Seconds(), t-c.doorOpenT, lim.DoorDelay.Seconds())
	}
	// Door switch that used to work has stopped answering.
	c.timed(CodeDoorSwitch, c.doorFittedEver && !c.doorFitted, c.doorFitted, 30, 0, 0)

	// Climate sensor: same chip, so the fault is reported once when both fail.
	grace := t-c.bootT < 30
	both := !grace && c.chT.quality == api.QualityFault && c.chRH.quality == api.QualityFault
	c.timed(CodeClimateFault, both, !both, 0, 0, 0)
	// One channel alone can also fail validation.
	tBad := !grace && (c.chT.quality == api.QualitySuspect || (c.chT.quality == api.QualityFault && !both))
	rBad := !grace && (c.chRH.quality == api.QualitySuspect || (c.chRH.quality == api.QualityFault && !both))
	c.timed(CodeTempSuspect, tBad, !tBad, 0, c.chT.val, 0)
	c.timed(CodeRHSuspect, rBad, !rBad, 0, c.chRH.val, 0)
	c.timed(CodeTempErratic, c.chT.erratic, !c.chT.erratic, 0, 0, 0)
	c.timed(CodeRHErratic, c.chRH.erratic, !c.chRH.erratic, 0, 0, 0)

	// Airflow sensor.
	airFault := !grace && c.chAir.quality == api.QualityFault
	c.timed(CodeAirFault, airFault, !airFault, 0, 0, 0)
	airSus := c.chAir.quality == api.QualitySuspect || c.chAir.erratic
	c.timed(CodeAirSuspect, airSus, !airSus, 0, c.chAir.val, 0)

	// Fan cross-checks: tach against airflow.
	// While outputs are held off, or just back, the guard has stopped the fan: that is the
	// output fault, already alarmed, not the fan (integration defect 4).
	if c.outputErr != "" || c.leaseHeld {
		c.lastOutErrT = t
	}
	outputsBack := c.lastOutErrT == 0 || t-c.lastOutErrT > 60
	c.timed(CodeFanStall, ctl.stall && outputsBack, !ctl.stall, 0, 0, 0)
	tachBad := !c.rpmOK || (c.rpm < 150 && c.chAir.ok() && c.airF.v >= 0.3)
	c.timed(CodeFanTach, tachBad, !tachBad, 60, 0, 0)
	// Rotor turning with no air: duct blocked or a sensor reading zero. These
	// cannot be told apart, so the safe assumption applies: no proven airflow.
	blocked := c.rpmOK && c.rpm >= 300 && c.chAir.ok() && c.airF.v < 0.12
	c.airBlocked = blocked
	c.timed(CodeAirBlocked, blocked, !blocked, 30, c.airF.v, 0.12)

	// Airflow against target, not during expected interruptions.
	airUse := c.chAir.quality == api.QualityOK && !ctl.defrost && t >= ctl.dripUntil && !c.doorOpen && t-c.bootT > 120
	target := set.AirMS
	if airUse && target > 0 {
		c.timed(CodeAirLow, c.airF.v < target*0.6, c.airF.v > target*0.75, 300, c.airF.v, target*0.6)
		// Above target is only reported when the fan floor is the cause.
		c.timed(CodeAirHigh, ctl.fanFloorHit && c.airF.v > target*1.4, !ctl.fanFloorHit || c.airF.v < target*1.2, 600, c.airF.v, target*1.4)
	}

	// Scale.
	sf := t-c.bootT >= 90 && c.chScale.quality == api.QualityFault && c.scaleCal()
	c.timed(CodeScaleFault, sf, !sf, 0, 0, 0)
	ss := c.chScale.quality == api.QualitySuspect || c.scaleImplausible
	c.timed(CodeScaleSuspect, ss && c.scaleCal(), !ss, 0, c.weightF.v, 0)

	// Actuator effect checks.
	c.evalActuators(set, dt)

	// Output errors.
	c.timed(CodeOutputFault, c.outputErr != "", c.outputErr == "", 0, 0, 0)

	// Storage.
	if t-c.storeCheckT >= 10 {
		c.storeCheckT = t
		s := c.store.Status()
		c.storeStatus = s
	}
	sBad := !c.storeStatus.OK || c.storeStatus.Dropped != c.storeDroppedAck
	c.timed(CodeStorage, sBad, c.storeStatus.OK, 0, 0, 0)
	if c.storeStatus.OK {
		c.storeDroppedAck = c.storeStatus.Dropped
	}

	// Clock.
	c.timed(CodeClock, !c.clockOK, c.clockOK, 0, 0, 0)

	// Calibration reminder, which is the only defence against slow drift that
	// the cross-checks cannot see. Ack does not clear it; a calibration does.
	dueAfter := cfg.CalReminderIdleSec
	if c.st.Program.State == api.RunActive || c.st.Program.State == api.RunPaused {
		dueAfter = cfg.CalReminderSec
	}
	// Lifecycle: raised when due; acknowledging keeps it listed; only a passed reference
	// check or a calibration resets it; ignored for a whole further period, it asks again.
	due := c.st.PoweredSec-c.st.LastCal >= dueAfter
	if a := c.al.active[CodeCalReminder]; a != nil && a.a.Acked && t-a.ackT >= dueAfter {
		a.a.Acked = false
	}
	c.timed(CodeCalReminder, due, !due, 0, 0, 0)

	c.updateBuzzer()
}

// evalActuators detects actuators that do not do what they are told, by the
// response of the reading to the command.
func (c *Core) evalActuators(set api.Setpoints, dt float64) {
	ctl := &c.ctl
	t := c.t
	const coolWindow = 1200.0
	tOK := c.chT.quality == api.QualityOK
	// Across an output fault nothing followed its command, so no run says anything about
	// the part (integration defect 4).
	quiet := !c.doorOpen && !ctl.defrost && t >= ctl.dripUntil && (ctl.defrostEnd == 0 || t-ctl.defrostEnd > 1800) && t-c.lastTBadT > 3600 &&
		(c.lastOutErrT == 0 || t-c.lastOutErrT > 1800)
	if c.chT.quality != api.QualityOK {
		c.lastTBadT = t
	}
	ts := c.tS.v

	// Does a compressor run pull the air down? Healthy runs drop the air by much
	// more than the threshold within two minutes of stopping, even on weak plants.
	// A heater running in the same window hides this, so such runs do not count.
	// The dew point is the second witness: a cold coil condenses water and pulls it
	// down, while heat alone leaves it where it is. So a run that lowers the dew point
	// proves the compressor works even when a heater stuck on outruns it.
	dew := math.NaN()
	if c.chRH.ok() && tOK {
		dew = dewPoint(c.tF.v, c.rhF.v)
	}
	if ctl.comp.on && !c.runActive {
		c.runActive, c.runT0, c.runMin, c.runClean, c.runContT, c.runContT0 = true, c.tF.v, c.tF.v, true, t, c.tF.v
		c.runDew0, c.runDewMin, c.runContDew0 = dew, dew, dew
	}
	if c.runActive {
		if c.tF.v < c.runMin {
			c.runMin = c.tF.v
		}
		if dew < c.runDewMin || (math.IsNaN(c.runDewMin) && !math.IsNaN(dew)) {
			c.runDewMin = dew
		}
		if !tOK || !quiet || ctl.heatOn || ctl.stall {
			c.runClean = false
		}
		if ctl.comp.on {
			c.runEnd = t
			// A long run with no fall counts as failure on its own, heater or not:
			// a heater that wins against the compressor is also not cooling. The window
			// starts again whenever the run cannot be judged.
			if !tOK || !quiet {
				c.runContT, c.runContT0, c.runContDew0 = t, c.tF.v, dew
			} else if t-c.runContT >= 900 {
				condensing := !math.IsNaN(dew) && !math.IsNaN(c.runContDew0) && dew <= c.runContDew0-0.5
				// A heater already shown stuck on explains a compressor that cannot win.
				if c.tF.v > c.runContT0-0.2 && c.tF.v > set.TempC+0.3 && !condensing && !c.heatStuck {
					c.coolFailing = true
				}
				c.runContT, c.runContT0, c.runContDew0 = t, c.tF.v, dew
			}
		} else if t-c.runEnd >= 120 {
			c.runActive = false
			drop := c.runT0 - c.runMin
			c.lastRunDewDrop = 0
			if !math.IsNaN(c.runDew0) && !math.IsNaN(c.runDewMin) {
				c.lastRunDewDrop = c.runDew0 - c.runDewMin
			}
			if c.lastRunDewDrop >= 0.5 {
				drop = math.Max(drop, 0.5) // the coil works, whatever the air did
			}
			if c.runClean {
				c.lastRunDrop = drop
				if drop < 0.2 && c.runT0 > set.TempC-0.5 {
					c.badRuns++
				} else {
					c.badRuns = 0
				}
				if c.badRuns >= 4 {
					c.coolFailing = true
				}
			}
			if drop >= 0.5 {
				c.coolFailing = false
				c.badRuns = 0
			}
		}
	}
	if tOK && ts < set.TempC-0.5 {
		c.coolFailing = false
	}
	c.timed(CodeCoolFail, c.coolFailing, !c.coolFailing, 0, ts, set.TempC)

	// Compressor off and air keeps falling fast. In a cabinet that normally needs
	// cooling the room is warmer than the air, so it takes a closed relay. In a
	// heating regime a cold room can do this alone, so the rule does not apply.
	if !ctl.comp.on && tOK && quiet && (c.st.CoolDuty > 0.03 || c.coolStuck) {
		off := t - ctl.comp.lastStop
		if !ctl.comp.hasStop {
			off = t - c.bootT
		}
		if off >= 300 && c.coolOffT0 == 0 {
			c.coolOffT0 = ts
			c.coolOffStart = t
			c.coolOffDew0 = dew
		}
		if c.coolOffT0 != 0 {
			if ts > c.coolOffT0 {
				c.coolOffT0 = ts
				c.coolOffStart = t
				c.coolOffDew0 = dew
			}
			// A closed compressor relay condenses water and pulls the dew point down about
			// as fast as the air. Mist from a humidifier stuck on also cools the air but
			// pushes the dew point up, and a room turned cold cools the air far faster than
			// its leak of dry air lowers the dew point.
			condensing := math.IsNaN(dew) || math.IsNaN(c.coolOffDew0) || c.coolOffDew0-dew >= 0.5*(c.coolOffT0-ts)
			if c.coolOffT0-ts >= 1.0 && t-c.coolOffStart < 1500 && condensing {
				if !c.coolStuck {
					c.coolStuckMin = ts
				}
				c.coolStuck = true
			}
			if c.coolStuck {
				if ts < c.coolStuckMin {
					c.coolStuckMin = ts
				}
				if ts > c.coolStuckMin+0.5 {
					c.coolStuck = false
				}
			}
			if t-c.coolOffStart > 1500 {
				c.coolOffT0 = ts
				c.coolOffStart = t
			}
		}
	} else if ctl.comp.on {
		c.coolOffT0 = 0
	}
	// The heater may hold the temperature up against a closed compressor relay, so
	// a falling reading is not the only sign: the heater has suddenly become busy
	// while the compressor is commanded off. Only in a cabinet that normally needs
	// cooling: in a heating regime a busy heater is ordinary.
	// Only while the busy heater holds the air near setpoint: a closed compressor relay
	// condenses water against a heater that keeps up. A room turned cold with a heater
	// that does not work lets the air fall well under setpoint, and that is
	// HEAT_NO_EFFECT; its cold walls pull the dew point down too.
	dew30 := c.dewRing[len(c.dewRing)-1]
	dewFalling := (math.IsNaN(dew) || math.IsNaN(dew30) || dew30-dew >= 0.3) && ts > set.TempC-1
	if tOK && quiet && !ctl.comp.on && ctl.driftAge >= 6*3600 && c.st.CoolDuty > 0.03 && ctl.dutyFast.v < c.st.CoolDuty-0.2 && ctl.dutyFast.v < -0.1 &&
		!c.humOnStuck && c.humSatSince == 0 && dewFalling { // reheat against a humidifier's mist is not this
		c.coolStuck = true
		c.coolStuckMin = ts
	} else if c.coolStuck && ctl.dutyFast.v > -0.05 && ts > set.TempC-1 {
		c.coolStuck = false
	}
	c.timed(CodeCoolStuck, c.coolStuck, !c.coolStuck, 0, ts, set.TempC)

	// A heater stuck closed adds its full power to the cabinet. The compressor then runs
	// nearly all the time and the air still does not come down. Recovering from a warm
	// excursion (a repaired compressor, outputs back after a fault, a warm load) also
	// runs the compressor hard, but the air comes down; a compressor that does not cool
	// is COOL_NO_EFFECT, not this. Only with the heater commanded off for half an hour.
	// It latches until the cabinet is back to normal for half an hour, so it cannot flap.
	if ctl.heatOn {
		c.lastHeatOn = t
	}
	if ctl.humOn {
		c.lastHumOn = t
	}
	on := 0.0
	if ctl.comp.on {
		on = 1
	}
	c.compRecent.add(on, dt)
	if t >= c.tRingT {
		c.tRingT = t + 300
		copy(c.tRing[1:], c.tRing[:len(c.tRing)-1])
		c.tRing[0] = ts
		copy(c.dewRing[1:], c.dewRing[:len(c.dewRing)-1])
		c.dewRing[0] = dew
	}
	t30 := c.tRing[len(c.tRing)-1] // the reading about 30 min ago
	noDoor := c.lastDoorT == 0 || t-c.lastDoorT > 3600
	coilWorks := c.lastRunDrop >= 0.3 || c.lastRunDewDrop >= 0.5
	held := true // in band for the last half hour, so not recovering from an excursion
	for _, v := range c.tRing {
		held = held && math.Abs(v-set.TempC) < 1
	}
	base := tOK && quiet && t-c.lastHeatOn >= 1800 && t-c.bootT >= 2400 && coilWorks && !c.coolFailing
	// Either the compressor cannot hold the air down, or it holds it only by running far
	// more than this cabinet's own history at this setpoint (24 h, ADR 0111's figure):
	// 100 W of heater is most of a small compressor, a room change is a fraction of it.
	// Flat or rising: a cabinet recovering from an excursion comes down, however slowly.
	losing := noDoor && c.compRecent.v >= 0.6 && ts >= set.TempC+1 && ts-t30 > -0.05
	working := (c.lastDoorT == 0 || t-c.lastDoorT > 6*3600) && held && c.st.CoolDutyAge >= 6*3600 && c.compRecent.v-c.st.CoolDuty >= 0.45
	heatEvidence := base && (losing || working)
	switch {
	case heatEvidence:
		c.heatStuck, c.stuckClearT = true, 0
	case c.heatStuck && tOK && ts < set.TempC+0.5 && c.compRecent.v < 0.5:
		if c.stuckClearT == 0 {
			c.stuckClearT = t
		}
		if t-c.stuckClearT >= 1800 {
			c.heatStuck = false
		}
	default:
		c.stuckClearT = 0
	}
	c.timed(CodeHeatStuck, c.heatStuck, !c.heatStuck, 0, ts, set.TempC)

	// A humidifier stuck closed drives the air to saturation and holds it there. With the
	// humidifier commanded off for half an hour and the cabinet dehumidifying as it does,
	// a wet load does not keep the air at saturation for most of 20 minutes (each
	// compressor run dips it briefly, so this is an average, not an unbroken spell). It
	// latches until humidity has stayed normal for 2 h, because the coil pulls it down
	// between peaks.
	rhOK := c.chRH.ok()
	sat := math.Max(97, set.RHPct+5)
	if !ctl.defrost {
		// A cabinet gone cold also saturates its air, with the water it already had: the
		// dew point stays where the setpoint puts it and the air is well under setpoint.
		// Mist adds water and lifts the dew point toward the setpoint temperature, with
		// the air held near setpoint.
		setDew := dewPoint(set.TempC, set.RHPct)
		wet := !math.IsNaN(dew) && dew >= setDew+0.3*(set.TempC-setDew) && ts >= set.TempC-1.5
		at := 0.0
		if rhOK && c.rhS.v >= sat && wet && t-c.lastHumOn >= 1800 && noDoor {
			at = 1
		}
		c.humSat.add(at, dt)
	}
	c.humSatSince = 0
	if c.humSat.v >= 0.6 {
		c.humSatSince = t // read by COOL_STUCK_ON: reheat against mist is not a stuck compressor
	}
	// A compressor that does not cool also lets the air reach saturation, but warm: that
	// is COOL_NO_EFFECT. So the coil must be shown working, or the air held at setpoint
	// (mist cools it; a dead compressor lets it rise).
	humEvidence := tOK && rhOK && t-c.bootT >= 2400 && c.humSat.v >= 0.6 &&
		(coilWorks || ts <= set.TempC+0.5) && !c.coolFailing
	switch {
	case humEvidence:
		c.humOnStuck, c.humStuckClearT = true, 0
	case c.humOnStuck && rhOK && c.rhS.v < math.Min(95, set.RHPct+5):
		if c.humStuckClearT == 0 {
			c.humStuckClearT = t
		}
		if t-c.humStuckClearT >= 7200 {
			c.humOnStuck = false
		}
	case c.humOnStuck && c.rhS.v >= sat:
		c.humStuckClearT = 0
	}
	c.timed(CodeHumidOnStuck, c.humOnStuck, !c.humOnStuck, 0, c.rhS.v, set.RHPct)

	// Heater working hard with no result. Not against air saturated by mist: evaporation
	// takes the heat, and that is HUMID_STUCK_ON's case, not a dead heater.
	if tOK && quiet && ts < set.TempC-0.7 && !ctl.comp.on && !c.humOnStuck && c.humSat.v < 0.3 {
		if ctl.heatOn {
			c.heatEffort += dt
		}
		if c.heatEvalT == 0 {
			c.heatEvalT = t
			c.heatEvalT0 = ts
			c.heatEffort = 0
		}
		if t-c.heatEvalT >= 1800 {
			if c.heatEffort >= 600 && ts < c.heatEvalT0+0.3 {
				c.heatFailing = true
			} else if ts >= c.heatEvalT0+0.5 {
				c.heatFailing = false
			}
			c.heatEvalT = t
			c.heatEvalT0 = ts
			c.heatEffort = 0
		}
	} else {
		c.heatEvalT = 0
		if tOK && ts > set.TempC-0.3 {
			c.heatFailing = false
		}
	}
	c.timed(CodeHeatFail, c.heatFailing, !c.heatFailing, 0, ts, set.TempC)

	// Humidifier pulses with no response.
	humFail := ctl.humNoRise >= 5
	c.timed(CodeHumidFail, humFail, !humFail, 0, c.rhS.v, set.RHPct)

	// Dehumidifying for a long time and humidity stays high.
	if c.chRH.ok() && c.rhS.v > set.RHPct+4 && !c.doorOpen {
		if c.rhHighSince == 0 {
			c.rhHighSince = t
		}
	} else if !c.chRH.ok() || c.rhS.v < set.RHPct+2 {
		c.rhHighSince = 0
	}
	humHigh := c.rhHighSince != 0 && t-c.rhHighSince >= 6*3600
	c.timed(CodeHumidStuck, humHigh, c.rhHighSince == 0, 0, c.rhS.v, set.RHPct)

	// Single-signal doubts: see crosscheck.go. These must hold 12 h, because a
	// room's daily cycle also moves them, and they ask for a check and change nothing.
	c.timed(CodeTempDrift, c.crossTempSoft && c.st.TDoubtUntil == 0, !c.crossTempSoft, 12*3600, 0, 0)
	c.timed(CodeRHDrift, c.crossRHSoft && c.st.RHDoubtUntil == 0, !c.crossRHSoft, 12*3600, 0, 0)
	c.timed(CodeDryingShift, c.crossRateSoft && c.st.TDoubtUntil == 0 && c.st.RHDoubtUntil == 0, !c.crossRateSoft, 12*3600, 0, 0)
	c.timed(CodeDefrostShort, c.defrostShortN >= 3, c.defrostShortN == 0, 0, float64(c.defrostShortN), 3)
}

// dewPoint is the Magnus approximation, good to a few tenths of a degree here.
func dewPoint(tC, rh float64) float64 {
	if rh <= 0 {
		return math.NaN()
	}
	const b, cc = 17.62, 243.12
	g := math.Log(math.Min(rh, 100)/100) + b*tC/(cc+tC)
	return cc * g / (b - g)
}

// announce starts a new or escalated alarm's signalling. If the contact is already
// signalling something, it closes briefly and opens again, so a new alarm is always
// a visible change. The buzzer does not stop for that close: it must sound while a
// critical alarm stands (integration defect 12). A new alarm always sounds,
// whatever has been silenced.
//
// Every new alarm is counted for cured's annunciator, whatever core itself was
// signalling: the contact may be held open by something core does not know of (a
// system alarm), and a new alarm must still be a fresh transition (integration
// defect 3, second re-test).
func (c *Core) announce(a *activeAlarm) {
	c.realert(a)
	c.al.edges++
	if c.signalling {
		c.al.edgeUntil = c.t + c.cfg.EdgeSec
	}
}

// realert sounds an alarm that is already known again (an acknowledged one coming
// back as a reminder). It is not news, so it makes no brief close of the contact.
func (c *Core) realert(a *activeAlarm) {
	a.callUntil = c.t + c.cfg.NotifySec
	a.nextCall = c.t + c.cfg.RenotifySec
	a.buzzUntil = c.t + c.cfg.NotifySec
	a.nextBuzz = c.t + c.cfg.RenotifySec
	if c.al.silenced {
		c.al.silenced = false
		c.event(Event{Type: EvSilence, Msg: "silence ended early: " + a.a.Code + " sounded"}) // both ends are on record
	}
}

// escalate raises an active alarm's severity and announces it again.
func (c *Core) escalate(a *activeAlarm, sev api.Severity) {
	if a.a.Severity == sev {
		return
	}
	a.a.Severity = sev
	a.a.Acked = false
	c.announce(a)
	c.event(Event{Type: EvAlarm, Code: a.a.Code, ID: a.a.ID, Sev: string(sev), Title: a.a.Title, Action: a.a.Action, Peak: a.a.Peak, Limit: a.a.Limit, Unit: a.a.Unit, Since: a.a.Since})
}

// isEventAlarm names alarms that report something that happened, not something
// that is still happening. They signal once and stay listed until acknowledged.
func isEventAlarm(code string) bool {
	switch code {
	case CodeOutage, CodeRestarted, CodeCureFinished, CodeWeightStep, CodeLogDamage, CodeStateLost, CodeOutDropped:
		return true
	}
	return false
}

// isSuspicion names one-signal doubts. There is no direct evidence behind them, so they
// tell someone periodically and do not stand on the contact.
func isSuspicion(code string) bool {
	switch code {
	case CodeTempDrift, CodeRHDrift, CodeDryingShift:
		return true
	}
	return false
}

// contactCalling and buzzerCalling are the output rules. See docs/adr/0108.
func (c *Core) contactCalling(a *activeAlarm) bool {
	t := c.t
	switch {
	case a.a.Severity == api.SevInfo:
		return false
	case isEventAlarm(a.a.Code) && a.a.Severity == api.SevCritical:
		return !a.a.Acked // a critical event stays on the contact until someone has seen it
	case isEventAlarm(a.a.Code):
		return !a.a.Acked && t < a.callUntil
	case isSuspicion(a.a.Code):
		if a.a.Acked {
			return false
		}
		if t >= a.nextCall {
			a.callUntil = t + c.cfg.NotifySec
			a.nextCall = t + c.cfg.RenotifySec
		}
		return t < a.callUntil
	}
	// A condition, warning or critical, holds the contact for as long as it is there.
	// An acknowledgement says someone has seen it, not that it has gone, and a remote
	// monitor must not read it as cleared (integration defect 12). New alarms still
	// show as a brief close and reopen (announce), so a standing one blinds nothing.
	return true
}

func (c *Core) buzzerCalling(a *activeAlarm) bool {
	t := c.t
	switch {
	case a.a.Severity == api.SevInfo:
		return false
	case isEventAlarm(a.a.Code) && a.a.Severity != api.SevCritical:
		return !a.a.Acked && t < a.callUntil
	}
	if a.a.Severity == api.SevCritical {
		// An acknowledged critical still there sounds again after an hour.
		if a.a.Acked && t-a.ackT >= c.cfg.AckRearmSec {
			a.a.Acked = false
			c.realert(a)
		}
		return !a.a.Acked
	}
	if a.a.Acked && t-a.ackT >= c.cfg.RenotifySec {
		// An acknowledged warning still there is a reminder again after the re-notify time.
		a.a.Acked = false
		c.realert(a)
	}
	if a.a.Acked {
		return false
	}
	if t >= a.nextBuzz {
		a.buzzUntil = t + c.cfg.NotifySec
		a.nextBuzz = t + c.cfg.RenotifySec
	}
	return t < a.buzzUntil
}

func (c *Core) updateBuzzer() {
	if c.al.silenced && c.t-c.al.silenceT >= c.cfg.SilenceSec {
		c.al.silenced = false
		c.event(Event{Type: EvSilence, Msg: "silence ended"}) // both ends are on record
	}
	contact, buzz := false, false
	for _, code := range c.al.order {
		a := c.al.active[code]
		if c.contactCalling(a) {
			contact = true
		}
		if c.buzzerCalling(a) {
			buzz = true
		}
	}
	c.signalling = contact
	edge := c.t < c.al.edgeUntil
	c.alarmOut = contact && !edge // true means the contact is open: signalling
	c.buzzerOut = buzz && !c.al.silenced
}

func (c *Core) activeList() []api.Alarm {
	out := make([]api.Alarm, 0, len(c.al.order))
	for _, code := range c.al.order {
		a := c.al.active[code].a
		a.Event = isEventAlarm(code)
		out = append(out, a)
	}
	// Most severe first, then oldest.
	rank := func(s api.Severity) int {
		switch s {
		case api.SevCritical:
			return 0
		case api.SevWarning:
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Severity) < rank(out[j].Severity) })
	return out
}

// wallNow is the wall clock as recorded, whatever its quality.
func (c *Core) wallNow() time.Time { return c.clock.Now() }

// replayEvents rebuilds alarm history and active alarms from the journal.
func (c *Core) replayEvents(evs []Event) {
	byID := map[string]*api.Alarm{}
	subject := map[string]float64{} // outage alarms: how long the outage lasted
	lastOutageID := ""
	var ids []string
	for i := range evs {
		e := &evs[i]
		switch e.Type {
		case EvAlarm:
			if a, dup := byID[e.ID]; dup {
				// An escalation: same alarm, new severity. Its date stays the first one.
				a.Severity, a.Title, a.Action = api.Severity(e.Sev), e.Title, e.Action
				if e.Peak != 0 {
					a.Peak = e.Peak
				}
				continue
			}
			a := &api.Alarm{ID: e.ID, Code: e.Code, Severity: api.Severity(e.Sev), Title: e.Title, Action: e.Action,
				Since: e.Wall, Peak: e.Peak, Limit: e.Limit, Unit: e.Unit, ClockOK: e.ClkOK}
			if !e.Since.IsZero() {
				a.Since = e.Since
			}
			ids = append(ids, e.ID)
			byID[e.ID] = a
			if e.Code == CodeOutage || e.Code == CodeRestarted {
				lastOutageID = e.ID
				subject[e.ID] = -1
				if e.Dur > 0 {
					subject[e.ID] = e.Dur
				}
			}
		case EvOutage:
			// Only a duration resolved later belongs to the alarm already raised; the
			// boot's own outage event comes before its alarm.
			if lastOutageID != "" && e.Dur > 0 && strings.HasPrefix(e.Msg, "duration resolved") {
				subject[lastOutageID] = e.Dur
			}
			// The temperature on return is the least the peak was. If the next gap
			// comes before the record closes, it is all the record has.
			if a := byID[lastOutageID]; a != nil && a.Cleared.IsZero() && e.Peak != 0 && (a.Peak == 0 || e.Peak > a.Peak) {
				a.Peak = e.Peak
			}
		case EvOutageEnd:
			if a := byID[lastOutageID]; a != nil && a.Cleared.IsZero() {
				a.Peak = e.Peak
			}
		case EvClear:
			if a := byID[e.ID]; a != nil {
				a.Cleared = e.Wall
				a.Peak = e.Peak
				a.DurationSec = e.Dur
			}
		case EvAck:
			if a := byID[e.ID]; a != nil {
				a.Acked = true
			}
		}
	}
	for _, id := range ids {
		a := byID[id]
		if a.Cleared.IsZero() {
			// Still active when power failed.
			if _, ok := c.al.active[a.Code]; ok {
				continue
			}
			sd, ok := subject[id]
			if !ok {
				sd = -1
			}
			c.al.active[a.Code] = &activeAlarm{a: *a, mono: c.t, restored: true, subjectDur: sd}
			c.al.order = append(c.al.order, a.Code)
		} else {
			c.al.history = append(c.al.history, *a)
		}
	}
	if n := len(c.al.history); n > 5000 {
		c.al.history = c.al.history[n-5000:]
	}
}
