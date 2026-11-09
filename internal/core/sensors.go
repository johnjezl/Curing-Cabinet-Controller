package core

import (
	"math"
	"sort"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// channel conditions one scalar measurement. It separates four questions that
// a simple controller blurs together: did a sample arrive, is it physically
// possible, does it agree with its neighbours, and is the sensor actually alive.
type channel struct {
	name        string
	lo, hi      float64 // physically possible range
	jumpMax     float64 // largest believable change between two samples
	staleAfter  float64
	faultAfter  float64
	recoverN    int     // consecutive good samples needed to clear a fault
	stuckActive float64 // seconds of actuation with an identical raw value
	stuckIdle   float64 // seconds with an identical raw value regardless

	win    [5]float64
	n, idx int

	raw       float64
	val       float64 // median of the window
	have      bool
	lastGoodT float64
	everGood  bool
	okRun     int
	quality   api.Quality
	note      string

	cand             float64
	candN            int
	rejects          [64]float64 // ring of reject times
	rejIdx           int
	erratic          bool
	heavyBad         bool
	errN, heavyN     int
	errWin, heavyWin float64

	stuckRaw    float64
	haveStuck   bool
	lastChange  float64
	activeSince float64
	stuck       bool
	changesLeft int
	stuckSince  float64
}

func newChannel(name string, lo, hi, jump, stale, fault float64, recoverN int, stuckActive, stuckIdle float64) *channel {
	return &channel{name: name, lo: lo, hi: hi, jumpMax: jump, staleAfter: stale, faultAfter: fault,
		recoverN: recoverN, stuckActive: stuckActive, stuckIdle: stuckIdle, quality: api.QualityFault, note: "no reading yet"}
}

func (c *channel) setReject(errN int, errWin float64, heavyN int, heavyWin float64) {
	c.errN, c.errWin, c.heavyN, c.heavyWin = errN, errWin, heavyN, heavyWin
}

func (c *channel) median() float64 {
	var tmp [5]float64
	copy(tmp[:], c.win[:c.n])
	s := tmp[:c.n]
	sort.Float64s(s)
	return s[c.n/2]
}

func (c *channel) reject(t float64) {
	c.rejects[c.rejIdx%len(c.rejects)] = t
	c.rejIdx++
}

// rejectRate returns rejected samples within window seconds. It tops out at the
// ring size, which is above every threshold used.
func (c *channel) rejectCount(t, window float64) int {
	n := 0
	for i := range c.rejects {
		if c.rejects[i] > 0 && t-c.rejects[i] <= window {
			n++
		}
	}
	return n
}

// feed takes one new sample. ok is false when the driver reported an error.
func (c *channel) feed(t, raw float64, ok bool) {
	if !ok {
		c.okRun = 0
		return
	}
	if math.IsNaN(raw) || raw < c.lo || raw > c.hi {
		c.reject(t)
		c.okRun = 0
		return
	}
	// Stuck detector works on the raw value even when the sample is rejected
	// later, because a frozen sensor can only be seen in the raw stream.
	if !c.haveStuck || raw != c.stuckRaw {
		c.haveStuck = true
		c.stuckRaw = raw
		c.lastChange = t
		c.activeSince = 0
		if c.stuck {
			c.changesLeft--
			if c.changesLeft <= 0 {
				c.stuck = false
			}
		}
	}
	if c.n >= 3 {
		med := c.median()
		if math.Abs(raw-med) > c.jumpMax {
			// A jump is believed only if the next samples agree with it.
			if c.candN > 0 && math.Abs(raw-c.cand) <= c.jumpMax/2 {
				c.candN++
			} else {
				c.cand = raw
				c.candN = 1
			}
			c.reject(t)
			if c.candN < 3 {
				return
			}
			// Three agreeing samples: a real change. Restart the window on it.
			c.n, c.idx = 0, 0
		}
	}
	c.candN = 0
	c.win[c.idx] = raw
	c.idx = (c.idx + 1) % len(c.win)
	if c.n < len(c.win) {
		c.n++
	}
	c.raw = raw
	c.val = c.median()
	c.have = true
	c.lastGoodT = t
	c.okRun++
	c.everGood = true
}

// update recomputes quality for time t. active is true when an actuator that
// should visibly move this reading is running.
func (c *channel) update(t, dt float64, active bool) {
	if active {
		c.activeSince += dt
	}
	if c.haveStuck && !c.stuck {
		idle := t - c.lastChange
		if (c.stuckActive > 0 && c.activeSince >= c.stuckActive) || (c.stuckIdle > 0 && idle >= c.stuckIdle) {
			c.stuck = true
			c.changesLeft = 3
			c.stuckSince = t
		}
	}
	// Goes up on the count in the window, comes down quickly on a clean run, so a
	// repaired sensor clears in about the same time as any other fault.
	if c.rejectCount(t, c.errWin) >= c.errN {
		c.erratic = true
	} else if c.rejectCount(t, 30) == 0 {
		c.erratic = false
	}
	if c.rejectCount(t, c.heavyWin) >= c.heavyN {
		c.heavyBad = true
	} else if c.rejectCount(t, 30) == 0 {
		c.heavyBad = false
	}
	age := math.Inf(1)
	if c.everGood {
		age = t - c.lastGoodT
	}
	switch {
	case age >= c.faultAfter:
		c.quality, c.note = api.QualityFault, "no valid reading"
		// A reading that is not arriving cannot be "not changing", and the samples from
		// before the gap cannot judge a jump after it: both start again when samples
		// return. Otherwise the first true readings after a long fault are rejected as
		// jumps, and the chip's two channels recover apart, which reads as a one-channel
		// fault (integration defect 12, second re-test: TEMP_SUSPECT on every recovery).
		c.stuck, c.lastChange, c.activeSince = false, t, 0
		c.n, c.idx, c.candN = 0, 0, 0
		// okRun is rebuilt only by samples that arrive after the fault began.
		if c.okRun > 0 && age >= c.faultAfter {
			c.okRun = 0
		}
	case c.quality == api.QualityFault && c.okRun < c.recoverN:
		// Still recovering: stay in fault until enough good samples in a row.
	case c.stuck:
		c.quality, c.note = api.QualitySuspect, "reading is not changing"
	case c.heavyBad:
		c.quality, c.note = api.QualitySuspect, "too many rejected readings"
	case age >= c.staleAfter:
		c.quality, c.note = api.QualityStale, "reading is not updating"
	default:
		c.quality, c.note = api.QualityOK, ""
	}
}

func (c *channel) ok() bool { return c.quality == api.QualityOK || c.quality == api.QualityStale }

// ema is a first-order filter with a time constant.
type ema struct {
	v    float64
	init bool
	tau  float64
}

func (e *ema) add(x, dt float64) float64 {
	if !e.init {
		e.v, e.init = x, true
		return e.v
	}
	a := dt / (e.tau + dt)
	e.v += a * (x - e.v)
	if math.Abs(e.v) < 1e-9 && x == 0 {
		e.v = 0 // decayed to nothing: zero, not a denormal like 1E-323 in the record
	}
	return e.v
}

func (e *ema) reset() { e.init = false }

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
