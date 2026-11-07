package sim

import (
	"errors"
	"fmt"
	"math"
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

// Inject sets a fault at the plant's current time. Use FaultNone to repair.
func (pl *Plant) Inject(d Device, k FaultKind, param float64) {
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
		pl.dhtLastSampleT = pl.T
		pl.dhtHave = true
		pl.dhtErr = pl.rng.Float64() < p.DHTErrProb
		if !pl.dhtErr {
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
// is off and the next Pet reports it, as the real guard's lease does.
//
// The guard also has its own 3 s lease: when Pet stops arriving it opens every relay
// by its own path, which works even when the relay board refuses the controller.
type SimGuard struct {
	pl      *Plant
	dropped bool
	lastPet float64
	expired bool
}

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
