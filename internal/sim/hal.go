package sim

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// Device names something a fault can be injected into.
type Device int

// Devices.
const (
	DevNone Device = iota
	DevClimate
	DevTemp
	DevRH
	DevAir
	DevScale
	DevFan
	DevTach
	DevDuct
	DevCool
	DevHeat
	DevHumid
	DevDehumid
	DevCompressor
	DevHumidifier
	DevDoorSwitch
	DevOutputs
	devCount
)

// FaultKind is how a device has failed.
type FaultKind int

// Fault kinds.
const (
	FaultNone    FaultKind = iota
	FaultDead              // no response at all
	FaultBadRead           // responds, fails validation
	FaultStuck             // repeats its last value
	FaultSpike             // normal, with frequent wild samples
	FaultDrift             // Param is units per hour
	FaultOffset            // Param is an instant offset
	FaultRange             // Param is an impossible fixed value
	FaultRelayStuckOn
	FaultRelayStuckOff
	FaultStall
	FaultBlocked
	FaultNoCool
	FaultEmpty
	FaultWriteErr
)

// Faults is the current fault table.
type Faults struct {
	k     [devCount]FaultKind
	param [devCount]float64
	since [devCount]float64
}

func (f *Faults) kind(d Device) FaultKind { return f.k[d] }

// implemented lists every fault each device really models. Anything else would be
// accepted and change nothing, which is how a "drift" once passed a test by doing
// nothing (integration defect 5). CheckFault refuses it.
var implemented = map[Device][]FaultKind{
	// One DHT22: dead, bad frames, or a frozen frame on both channels.
	DevClimate: {FaultDead, FaultBadRead, FaultStuck},
	// The two channels of that chip, for faults that hit one reading.
	DevTemp:       {FaultStuck, FaultSpike, FaultDrift, FaultOffset, FaultRange},
	DevRH:         {FaultStuck, FaultSpike, FaultDrift, FaultOffset, FaultRange},
	DevAir:        {FaultDead, FaultBadRead, FaultStuck, FaultSpike, FaultDrift, FaultOffset, FaultRange},
	DevScale:      {FaultDead, FaultStuck, FaultSpike, FaultDrift, FaultOffset, FaultRange},
	DevFan:        {FaultStall},
	DevTach:       {FaultDead, FaultStuck},
	DevDuct:       {FaultBlocked},
	DevCool:       {FaultRelayStuckOn, FaultRelayStuckOff},
	DevHeat:       {FaultRelayStuckOn, FaultRelayStuckOff},
	DevHumid:      {FaultRelayStuckOn, FaultRelayStuckOff},
	DevDehumid:    {FaultRelayStuckOn, FaultRelayStuckOff},
	DevCompressor: {FaultNoCool},
	DevHumidifier: {FaultEmpty},
	DevDoorSwitch: {FaultDead, FaultStuck},
	DevOutputs:    {FaultWriteErr},
}

var deviceName = map[Device]string{
	DevClimate: "climate", DevTemp: "temp", DevRH: "rh", DevAir: "air", DevScale: "scale", DevFan: "fan",
	DevTach: "tach", DevDuct: "duct", DevCool: "cool", DevHeat: "heat", DevHumid: "humid", DevDehumid: "dehumid",
	DevCompressor: "compressor", DevHumidifier: "humidifier", DevDoorSwitch: "door-switch", DevOutputs: "outputs",
}

var faultName = map[FaultKind]string{
	FaultNone: "none", FaultDead: "dead", FaultBadRead: "bad-read", FaultStuck: "stuck", FaultSpike: "spike",
	FaultDrift: "drift", FaultOffset: "offset", FaultRange: "range", FaultRelayStuckOn: "relay-stuck-on",
	FaultRelayStuckOff: "relay-stuck-off", FaultStall: "stall", FaultBlocked: "blocked", FaultNoCool: "no-cool",
	FaultEmpty: "empty", FaultWriteErr: "write-err",
}

func (d Device) String() string {
	if n, ok := deviceName[d]; ok {
		return n
	}
	return fmt.Sprintf("device-%d", int(d))
}

func (k FaultKind) String() string {
	if n, ok := faultName[k]; ok {
		return n
	}
	return fmt.Sprintf("fault-%d", int(k))
}

// FaultOn is the fault currently injected into d.
func (pl *Plant) FaultOn(d Device) FaultKind { return pl.Faults.kind(d) }

// CheckInjection says whether injecting k into d with this parameter (hasParam false
// when none was given) changes this plant in the way the fault describes. A zero
// drift, a stuck value that would be ignored, a rate that is not a number, or a
// dehumidifier fault on a plant with none would all be accepted and change nothing,
// or change the wrong thing (integration defect 5, re-test).
func (pl *Plant) CheckInjection(d Device, k FaultKind, param float64, hasParam bool) error {
	if err := CheckFault(d, k); err != nil {
		return err
	}
	if (d == DevTemp || d == DevRH) && k != FaultNone {
		switch ck := pl.Faults.kind(DevClimate); ck {
		case FaultDead, FaultBadRead, FaultStuck:
			return fmt.Errorf("%s %s: the climate chip is %s, so the fault would not show until climate is repaired", d, k, ck)
		}
	}
	if hasParam && (math.IsNaN(param) || math.IsInf(param, 0)) {
		return fmt.Errorf("%s %s: the value must be a number", d, k)
	}
	need := func(what string, ok bool) error {
		if !hasParam || !ok {
			return fmt.Errorf("%s %s needs %s", d, k, what)
		}
		return nil
	}
	// The smallest value that moves the reading by one sensor step within a day (drift)
	// or at once (offset), and the values a sensor cannot report (range).
	minDrift, minOffset := 0.01, 0.1
	driftUnit, offsetUnit, driftEg, offsetEg := "units per virtual hour", "units", "-0.5", "-2"
	inRange := func(v float64) bool { return false }
	rangeEg := ""
	switch d {
	case DevTemp:
		driftUnit, offsetUnit = "°C per virtual hour", "°C"
		inRange, rangeEg = func(v float64) bool { return v >= -20 && v <= 60 }, "below -20 or above 60 °C, where the controller rejects a reading (e.g. 'temp range 85')"
	case DevRH:
		driftUnit, offsetUnit, driftEg, offsetEg = "%RH per virtual hour", "%RH", "2", "5"
		inRange, rangeEg = func(v float64) bool { return v >= 0.5 && v <= 100 }, "below 0.5 or above 100 %, where the controller rejects a reading (e.g. 'rh range 101')"
	case DevAir:
		minDrift, minOffset = 0.001, 0.01
		driftUnit, offsetUnit, driftEg, offsetEg = "share of the reading per virtual hour", "m/s", "-0.05", "0.2"
		inRange, rangeEg = func(v float64) bool { return v >= -0.1 && v <= 8 }, "below -0.1 or above 8 m/s, where the controller rejects a reading (e.g. 'air range 40')"
	case DevScale:
		minDrift, minOffset = 1, 1
		driftUnit, offsetUnit, driftEg, offsetEg = "g per virtual hour", "g", "-50", "100"
		// The controller takes raw counts up to 8388000 either way as a weight, so a
		// range value must be past that: the HX711 gives 8388607 when saturated.
		inRange, rangeEg = func(v float64) bool { return math.Abs(v) <= 8388000 }, "a raw ADC count past the controller's limit of 8388000 either way, as a saturated HX711 gives (e.g. 'scale range 8388607'); range is in counts, not grams"
	}
	switch {
	case k == FaultDrift:
		return need(fmt.Sprintf("a rate in %s, at least %g and at most 50 either way, or it never shows (e.g. '%s drift %s')", driftUnit, minDrift, d, driftEg), math.Abs(param) >= minDrift && math.Abs(param) <= 50)
	case k == FaultOffset:
		return need(fmt.Sprintf("an offset in %s, at least %g and at most 100 either way (e.g. '%s offset %s')", offsetUnit, minOffset, d, offsetEg), math.Abs(param) >= minOffset && math.Abs(param) <= 100)
	case k == FaultSpike:
		// Fewer spikes than this are filtered out by the controller with no sign at
		// all, which from outside is an injection that did nothing (re-test of defect 5).
		return need(fmt.Sprintf("the share of samples that spike, at least 0.2 and at most 1; fewer are filtered out with no visible sign (e.g. '%s spike 0.3')", d), param >= 0.2 && param <= 1)
	case k == FaultRange:
		return need("a value the controller rejects as impossible: "+rangeEg+"; a value it accepts would only freeze the reading, which is 'stuck'", !inRange(param))
	case k == FaultStuck && d == DevDoorSwitch:
		return need("the state it sticks in, 0 (closed) or 1 (open)", param == 0 || param == 1)
	case hasParam:
		return fmt.Errorf("%s %s takes no value: %s", d, k, map[bool]string{true: "it freezes the reading it has now", false: "remove the number"}[k == FaultStuck])
	}
	if (d == DevDehumid) && pl.P.DehumidGPerS <= 0 {
		return fmt.Errorf("%s %s: this plant has no dehumidifier, so the fault would change nothing", d, k)
	}
	if d == DevDoorSwitch && !pl.P.DoorSwitch {
		return fmt.Errorf("%s %s: this plant has no door switch, so the fault would change nothing", d, k)
	}
	return nil
}

// Latent says why a fault just injected into d changes nothing yet: the part it acts
// on is idle. It returns "" when the fault acts at once. A fault on a part that is
// not in use is a fair test, but the tool must say it has had no effect so far.
func (pl *Plant) Latent(d Device, k FaultKind, param float64) string {
	cmd := map[Device]*bool{DevCool: &pl.CmdCool, DevHeat: &pl.CmdHeat, DevHumid: &pl.CmdHumid, DevDehumid: &pl.CmdDehumid}
	switch {
	case k == FaultRelayStuckOff && cmd[d] != nil && !*cmd[d]:
		return fmt.Sprintf("no effect until the controller turns %s on: it is off now", d)
	case k == FaultRelayStuckOn && cmd[d] != nil && *cmd[d]:
		return fmt.Sprintf("no effect until the controller turns %s off: it is on now", d)
	case d == DevCompressor && k == FaultNoCool && !pl.CmdCool:
		return "no effect until the compressor runs: it is off now"
	case d == DevHumidifier && k == FaultEmpty && !pl.CmdHumid:
		return "no effect until the humidifier is called: it is off now"
	case (d == DevFan && k == FaultStall || d == DevDuct && k == FaultBlocked) && pl.FanDuty <= 0:
		return "no effect until the fan runs: it is stopped now"
	// A fault behind another that already pins what it would change shows nothing until
	// that one is repaired (re-test of defect 5).
	case pl.Faults.kind(DevFan) == FaultStall && (d == DevTach && k == FaultStuck || d == DevDuct && k == FaultBlocked ||
		d == DevAir && (k == FaultStuck || k == FaultDrift)):
		return "no effect while fan:stall is injected: the rpm and the airflow already read 0; repair the fan to see it"
	case d == DevCompressor && k == FaultNoCool && pl.Faults.kind(DevCool) == FaultRelayStuckOff:
		return "no effect while cool:relay-stuck-off is injected: the compressor never runs; repair cool to see it"
	case d == DevHumidifier && k == FaultEmpty && pl.Faults.kind(DevHumid) == FaultRelayStuckOff:
		return "no effect while humid:relay-stuck-off is injected: the humidifier never runs; repair humid to see it"
	case d == DevDoorSwitch && k == FaultStuck && (param == 1) == pl.DoorIsOpen:
		if pl.DoorIsOpen {
			return "no effect until the door is closed: the switch already reads open"
		}
		return "no effect until the door is opened: the switch already reads closed"
	}
	return ""
}

// SameReading says whether replacing the value of fault k on d by another would leave
// the reading exactly where it is: two range values or two offsets that both put it
// beyond what the controller accepts (or both clamp humidity at the same end).
func (pl *Plant) SameReading(d Device, k FaultKind, old, new float64) bool {
	p := &pl.P
	out := func(v float64, lo, hi float64) int {
		switch {
		case v < lo:
			return -1
		case v > hi:
			return 1
		}
		return 0
	}
	switch {
	case k == FaultRange:
		return true // both are values the controller rejects: CheckInjection allows no other
	case k == FaultOffset && d == DevTemp:
		v := pl.sT + p.TBias
		a, b := out(v+old, -20, 60), out(v+new, -20, 60)
		return a != 0 && b != 0
	case k == FaultOffset && d == DevRH:
		v := pl.sRH + p.RHBias
		a, b := out(v+old, 0.5, 99.999), out(v+new, 0.5, 99.999) // clamped at 100, rejected under 0.5
		return a != 0 && a == b
	case k == FaultOffset && d == DevAir:
		v := pl.FanV * (1 + p.AirBiasPct/100)
		a, b := out(v+old, -0.1, 8), out(v+new, -0.1, 8)
		return a != 0 && b != 0
	}
	return false
}

// OffsetActsAsRange says whether an offset puts the reading beyond what the controller
// accepts, so that it behaves as a range fault.
func (pl *Plant) OffsetActsAsRange(d Device, off float64) bool {
	p := &pl.P
	switch d {
	case DevTemp:
		v := pl.sT + p.TBias + off
		return v < -20 || v > 60
	case DevRH:
		v := pl.sRH + p.RHBias + off
		return v < 0.5 || v >= 100
	case DevAir:
		v := pl.FanV*(1+p.AirBiasPct/100) + off
		return v < -0.1 || v > 8
	}
	return false
}

// FaultParam is the value the current fault on d was injected with.
func (pl *Plant) FaultParam(d Device) float64 { return pl.Faults.param[d] }

// CheckFault says whether injecting k into d changes anything. FaultNone (repair)
// always does.
func CheckFault(d Device, k FaultKind) error {
	if k == FaultNone {
		return nil
	}
	for _, ok := range implemented[d] {
		if ok == k {
			return nil
		}
	}
	switch {
	case k == FaultNone:
		return nil
	case d == DevClimate && (k == FaultDrift || k == FaultOffset || k == FaultSpike || k == FaultRange):
		return errors.New("the climate chip has two readings: inject this on temp or rh")
	case (d == DevTemp || d == DevRH) && (k == FaultDead || k == FaultBadRead):
		return errors.New("temperature and humidity come from one chip: inject this on climate")
	}
	var can []string
	for _, ok := range implemented[d] {
		can = append(can, ok.String())
	}
	return fmt.Errorf("%s does not model %s, so the injection would change nothing; it models: %s", d, k, strings.Join(can, ", "))
}

// Inject sets a fault at the plant's current time. Use FaultNone to repair. A
// fault CheckFault refuses is not recorded, so the fault table never lists one
// that has no effect.
func (pl *Plant) Inject(d Device, k FaultKind, param float64) {
	if CheckFault(d, k) != nil {
		return
	}
	pl.Faults.k[d] = k
	pl.Faults.param[d] = param
	pl.Faults.since[d] = pl.T
	switch d {
	case DevTemp:
		pl.haveStuckT = false
	case DevRH:
		pl.haveStuckRH = false
	case DevClimate:
		pl.haveStuckT, pl.haveStuckRH = false, false
	case DevAir:
		pl.haveStuckA = false
	case DevScale:
		pl.haveStuckS = false
	}
}

// Elapsed since a fault was injected.
func (pl *Plant) faultAge(d Device) float64 { return pl.T - pl.Faults.since[d] }

type simClimate struct{ pl *Plant }

func (s simClimate) Read() (float64, float64, float64, error) {
	pl := s.pl
	p := &pl.P
	if !pl.Powered {
		return 0, 0, 0, hal.ErrAbsent
	}
	k := pl.Faults.kind(DevClimate)
	if k == FaultDead {
		return 0, 0, 0, fmt.Errorf("dht22: %w", hal.ErrAbsent)
	}
	if k == FaultBadRead {
		return 0, 0, 0, fmt.Errorf("dht22 checksum: %w", hal.ErrBadRead)
	}
	if pl.T-pl.dhtLastSampleT >= 2 {
		had := pl.dhtHave && !pl.dhtErr
		pl.dhtLastSampleT = pl.T
		pl.dhtHave = true
		pl.dhtErr = pl.rng.Float64() < p.DHTErrProb
		if k == FaultStuck {
			// The chip answers on time with a valid frame that never changes: the
			// last one it delivered.
			if !pl.haveStuckT || !pl.haveStuckRH {
				pl.stuckT, pl.stuckRH = pl.lastTemp, pl.lastRH
				if !had {
					pl.stuckT, pl.stuckRH = pl.sampleTemp(), pl.sampleRH()
				}
				pl.haveStuckT, pl.haveStuckRH = true, true
			}
			pl.dhtErr = false
			pl.lastTemp, pl.lastRH = pl.stuckT, pl.stuckRH
		} else if !pl.dhtErr {
			pl.lastTemp = pl.sampleTemp()
			pl.lastRH = pl.sampleRH()
			// A glitch that passes the checksum is real on these parts.
			if pl.rng.Float64() < p.DHTSpikePerHour*2/3600 {
				if pl.rng.IntN(2) == 0 {
					pl.lastTemp = quant(pl.lastTemp+pl.signed(20), 0.1)
				} else {
					pl.lastRH = math.Max(0, math.Min(100, quant(pl.lastRH+pl.signed(40), 0.1)))
				}
			}
		}
	}
	if pl.dhtErr {
		return 0, 0, 0, fmt.Errorf("dht22 checksum: %w", hal.ErrBadRead)
	}
	return pl.lastTemp, pl.lastRH, pl.T - pl.dhtLastSampleT, nil
}

func (pl *Plant) signed(mag float64) float64 {
	if pl.rng.IntN(2) == 0 {
		return mag
	}
	return -mag
}

func quant(v, q float64) float64 { return math.Round(v/q) * q }

func (pl *Plant) sampleTemp() float64 {
	p := &pl.P
	v := pl.sT + p.TBias + pl.rng.NormFloat64()*p.TNoise
	switch pl.Faults.kind(DevTemp) {
	case FaultStuck:
		if !pl.haveStuckT {
			pl.stuckT, pl.haveStuckT = quant(v, 0.1), true
		}
		return pl.stuckT
	case FaultSpike:
		if pl.rng.Float64() < pl.Faults.param[DevTemp] {
			return quant(v+pl.signed(10+pl.rng.Float64()*30), 0.1)
		}
	case FaultDrift:
		v += pl.Faults.param[DevTemp] * pl.faultAge(DevTemp) / 3600
	case FaultOffset:
		v += pl.Faults.param[DevTemp]
	case FaultRange:
		return pl.Faults.param[DevTemp]
	}
	return quant(v, 0.1)
}

func (pl *Plant) sampleRH() float64 {
	p := &pl.P
	v := pl.sRH + p.RHBias + pl.rng.NormFloat64()*p.RHNoise
	switch pl.Faults.kind(DevRH) {
	case FaultStuck:
		if !pl.haveStuckRH {
			pl.stuckRH, pl.haveStuckRH = quant(v, 0.1), true
		}
		return pl.stuckRH
	case FaultSpike:
		if pl.rng.Float64() < pl.Faults.param[DevRH] {
			v += pl.signed(20 + pl.rng.Float64()*50)
		}
	case FaultDrift:
		v += pl.Faults.param[DevRH] * pl.faultAge(DevRH) / 3600
	case FaultOffset:
		v += pl.Faults.param[DevRH]
	case FaultRange:
		return pl.Faults.param[DevRH]
	}
	return quant(math.Max(0, math.Min(100, v)), 0.1)
}

type simAir struct{ pl *Plant }

// fs3000Table is the FS3000-1005 count to m/s table in the part's data sheet.
var fs3000Counts = []float64{409, 915, 1522, 2066, 2523, 2908, 3256, 3572, 3686}
var fs3000Vel = []float64{0, 1.07, 2.01, 3.00, 3.97, 4.96, 5.98, 6.99, 7.23}

func fs3000Quant(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 7.23 {
		return 7.23
	}
	// To counts, rounded to an integer, and back.
	for i := 1; i < len(fs3000Vel); i++ {
		if v <= fs3000Vel[i] {
			c := fs3000Counts[i-1] + (v-fs3000Vel[i-1])/(fs3000Vel[i]-fs3000Vel[i-1])*(fs3000Counts[i]-fs3000Counts[i-1])
			c = math.Round(c)
			return fs3000Vel[i-1] + (c-fs3000Counts[i-1])/(fs3000Counts[i]-fs3000Counts[i-1])*(fs3000Vel[i]-fs3000Vel[i-1])
		}
	}
	return 7.23
}

func (s simAir) Read() (float64, error) {
	pl := s.pl
	p := &pl.P
	if !pl.Powered {
		return 0, hal.ErrAbsent
	}
	switch pl.Faults.kind(DevAir) {
	case FaultDead:
		return 0, fmt.Errorf("fs3000: %w", hal.ErrAbsent)
	case FaultBadRead:
		return 0, fmt.Errorf("fs3000 checksum: %w", hal.ErrBadRead)
	case FaultRange:
		return pl.Faults.param[DevAir], nil
	}
	v := pl.FanV*(1+p.AirBiasPct/100) + pl.rng.NormFloat64()*p.AirNoise
	if pl.FanV < 0.05 {
		v = 0
	}
	switch pl.Faults.kind(DevAir) {
	case FaultStuck:
		if !pl.haveStuckA {
			pl.stuckA, pl.haveStuckA = fs3000Quant(v), true
		}
		return pl.stuckA, nil
	case FaultSpike:
		if pl.rng.Float64() < pl.Faults.param[DevAir] {
			return 7.23, nil
		}
	case FaultDrift:
		v *= 1 + pl.Faults.param[DevAir]*pl.faultAge(DevAir)/3600
	case FaultOffset:
		v += pl.Faults.param[DevAir]
	}
	return fs3000Quant(v), nil
}

type simScale struct{ pl *Plant }

func (s simScale) ReadRaw() (int32, error) {
	pl := s.pl
	p := &pl.P
	if !pl.Powered {
		return 0, hal.ErrAbsent
	}
	switch pl.Faults.kind(DevScale) {
	case FaultDead:
		return 0, fmt.Errorf("hx711: %w", hal.ErrAbsent)
	case FaultRange:
		return int32(pl.Faults.param[DevScale]), nil
	}
	g := pl.ProdKg*1000 + pl.scaleCreep + p.ScaleTempCoGPerK*(pl.TAir-13)
	noise := p.ScaleNoiseCounts
	if pl.EffCool() {
		noise *= 2 // compressor vibration on the frame
	}
	raw := p.ScaleTareCounts + g*p.ScaleCountsPerG + pl.rng.NormFloat64()*noise
	switch pl.Faults.kind(DevScale) {
	case FaultStuck:
		if !pl.haveStuckS {
			pl.stuckS, pl.haveStuckS = int32(raw), true
		}
		return pl.stuckS, nil
	case FaultSpike:
		if pl.rng.Float64() < pl.Faults.param[DevScale] {
			raw += pl.signed(5000 * p.ScaleCountsPerG)
		}
	case FaultDrift:
		raw += pl.Faults.param[DevScale] * pl.faultAge(DevScale) / 3600 * p.ScaleCountsPerG
	case FaultOffset:
		raw += pl.Faults.param[DevScale] * p.ScaleCountsPerG
	}
	return int32(raw), nil
}

type simFan struct{ pl *Plant }

func (s simFan) SetDuty(pct float64) error {
	if !s.pl.Powered {
		return hal.ErrAbsent
	}
	if s.pl.Faults.kind(DevOutputs) == FaultWriteErr {
		return errors.New("pwm write failed")
	}
	s.pl.FanDuty = pct
	return nil
}

func (s simFan) RPM() (float64, error) {
	pl := s.pl
	if !pl.Powered {
		return 0, hal.ErrAbsent
	}
	switch pl.Faults.kind(DevTach) {
	case FaultDead:
		return 0, fmt.Errorf("tach: %w", hal.ErrAbsent)
	case FaultStuck:
		return 0, nil // a broken tach wire reads as no pulses
	}
	if pl.FanRPM <= 0 {
		return 0, nil
	}
	// Two pulses per revolution counted over one second gives 30 rpm steps.
	return quant(pl.FanRPM+pl.rng.NormFloat64()*10, 30), nil
}

type simOutputs struct{ pl *Plant }

func (s simOutputs) Set(id hal.OutputID, on bool) error {
	pl := s.pl
	if !pl.Powered {
		return hal.ErrAbsent
	}
	if pl.Faults.kind(DevOutputs) == FaultWriteErr {
		return errors.New("relay board not answering")
	}
	switch id {
	case hal.OutCool:
		pl.CmdCool = on
	case hal.OutHeat:
		pl.CmdHeat = on
	case hal.OutHumid:
		pl.CmdHumid = on
	case hal.OutDehumid:
		pl.CmdDehumid = on
	case hal.OutAlarm:
		pl.CmdAlarm = on
	case hal.OutBuzzer:
		pl.CmdBuzzer = on
	default:
		return fmt.Errorf("no output %q", id)
	}
	return nil
}

func (s simOutputs) AllOff() error {
	pl := s.pl
	pl.CmdCool, pl.CmdHeat, pl.CmdHumid, pl.CmdDehumid = false, false, false, false
	pl.CmdAlarm, pl.CmdBuzzer = false, false
	return nil
}

type simDoor struct{ pl *Plant }

func (s simDoor) Open() (bool, error) {
	pl := s.pl
	if !pl.P.DoorSwitch {
		return false, fmt.Errorf("door: %w", hal.ErrAbsent)
	}
	switch pl.Faults.kind(DevDoorSwitch) {
	case FaultDead:
		return false, fmt.Errorf("door: %w", hal.ErrAbsent)
	case FaultStuck:
		return pl.Faults.param[DevDoorSwitch] != 0, nil
	}
	return pl.DoorIsOpen, nil
}

// SimGuard stands in for the hardware output guard: after DropOutputs every output
// is off and the next Pet reports it, as the real guard's lease does. cured --sim
// uses one too, in virtual time, so an output write fault drops the plant's relays
// as the rig's guard would (ADR 0407).
//
// The guard also has its own 3 s lease: when Pet stops arriving it opens every relay
// by its own path, which works even when the relay board refuses the controller.
type SimGuard struct {
	pl      *Plant
	dropped bool
	lastPet float64
	expired bool
}

// NewSimGuard returns a guard whose lease starts now.
func NewSimGuard(pl *Plant) *SimGuard { return &SimGuard{pl: pl, lastPet: pl.T} }

// Tick applies the lease at virtual time t.
func (g *SimGuard) Tick(t float64) { g.tick(t) }

// tick applies the lease.
func (g *SimGuard) tick(t float64) {
	if t-g.lastPet > 3 && !g.expired {
		g.expired = true
		g.DropOutputs()
	}
}

// Pet implements hal.Watchdog.
func (g *SimGuard) Pet() error {
	g.lastPet = g.pl.T
	g.expired = false
	if g.dropped {
		g.dropped = false
		return fmt.Errorf("output guard: %w", hal.ErrOutputsDropped)
	}
	return nil
}

// DropOutputs models a lease expiry: everything off at once.
func (g *SimGuard) DropOutputs() {
	simOutputs{g.pl}.AllOff()
	g.pl.FanDuty = 0
	g.dropped = true
}

// Cabinet returns the plant as a hal.Cabinet.
func (pl *Plant) Cabinet() hal.Cabinet {
	return hal.Cabinet{
		Climate: simClimate{pl},
		Air:     simAir{pl},
		Scale:   simScale{pl},
		Fan:     simFan{pl},
		Outputs: simOutputs{pl},
		Door:    simDoor{pl},
		Close:   func() error { return nil },
	}
}

// PowerOff cuts mains. Relays open and sensors stop answering; the plant keeps
// evolving.
func (pl *Plant) PowerOff() {
	pl.Powered = false
	pl.CmdCool, pl.CmdHeat, pl.CmdHumid, pl.CmdDehumid = false, false, false, false
	pl.CmdAlarm, pl.CmdBuzzer = false, false
	pl.FanDuty = 0
}

// PowerOn restores mains. Outputs start open, as hardware would.
func (pl *Plant) PowerOn() { pl.Powered = true }

// Clock is a virtual clock with a separate wall clock that can be wrong.
type Clock struct {
	mu sync.Mutex
	// VT is total virtual time. Set it directly from one goroutine, or with Set
	// when other goroutines may be reading the clock.
	VT float64
	// BootVT is VT when the current boot started.
	BootVT float64
	// Wall behaviour.
	BaseWall  time.Time
	RTC       bool
	TrustAtVT float64 // when no RTC, the wall clock becomes right at this VT
	OldWall   time.Time
}

// NewClock starts at a fixed wall time so that runs are reproducible.
func NewClock(rtc bool) *Clock {
	return &Clock{BaseWall: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), RTC: rtc}
}

// Set moves virtual time; safe against concurrent readers.
func (c *Clock) Set(vt float64) {
	c.mu.Lock()
	c.VT = vt
	c.mu.Unlock()
}

// Now implements core.Clock.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.RTC || c.VT >= c.TrustAtVT {
		return c.BaseWall.Add(time.Duration(c.VT * float64(time.Second)))
	}
	// A Pi with no battery boots on a saved or epoch time, which keeps running.
	return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration((c.VT - c.BootVT) * float64(time.Second)))
}

// Mono implements core.Clock.
func (c *Clock) Mono() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Duration((c.VT - c.BootVT) * float64(time.Second))
}

// Trusted implements core.Clock.
func (c *Clock) Trusted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.RTC || c.VT >= c.TrustAtVT
}

// Reboot starts a new boot. Without an RTC the clock is wrong until NTP arrives
// ntpDelay seconds later; a negative delay means never in this run.
func (c *Clock) Reboot(ntpDelay float64) {
	c.BootVT = c.VT
	if !c.RTC {
		if ntpDelay < 0 {
			c.TrustAtVT = math.Inf(1)
		} else {
			c.TrustAtVT = c.VT + ntpDelay
		}
	}
}
