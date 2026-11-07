package core

import (
	"fmt"
	"math"
	"strings"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// CrossPt is one hour of what the cabinet did. These are kept across restarts.
type CrossPt struct {
	W    float64 `json:"w"`
	WOK  bool    `json:"k"`
	Net  float64 `json:"n"`
	Hum  float64 `json:"h"`
	De   float64 `json:"d"`
	Door bool    `json:"o,omitempty"`
	// Band is false when the air was away from setpoint: effort then says nothing.
	Band bool `json:"b,omitempty"`
}

// crossAcc builds the current hour.
type crossAcc struct {
	start        float64
	n            float64
	net, hum, de float64
	w, wn        float64
	door         bool
	band, bandN  float64
}

const crossMax = 120

// One chip gives both temperature and humidity, so nothing can compare it directly
// with a second reading. What the sensor cannot hide is how the plant responds:
// holding the air colder than the reading says takes more cooling and slows
// drying, holding it warmer takes less cooling and speeds drying. A change of room
// moves the cooling and not the drying, because the air stays where it was, and that
// is what tells them apart. See docs/adr/0109.
func (c *Core) crossTick(dt float64) {
	if c.cfg.CrossCheckOff {
		return
	}
	x := &c.cross
	t := c.t
	if x.start == 0 {
		x.start = t
	}
	ctl := &c.ctl
	if !ctl.defrost {
		x.n += dt
		if ctl.comp.on {
			x.net += dt
		}
		if ctl.heatOn {
			x.net -= reheatRatio * dt
		}
		if ctl.humOn {
			x.hum += dt
		}
		if ctl.heatOn && ctl.comp.on {
			x.de += dt
		}
	}
	if c.weightUsable() {
		x.w += c.weightF.v * dt
		x.wn += dt
	}
	if c.doorOpen {
		x.door = true
	}
	// Defrost and its recovery leave the band on purpose and do not count against it.
	if !ctl.defrost && (ctl.defrostEnd == 0 || c.t-ctl.defrostEnd > 2400) {
		x.bandN += dt
		if c.chT.quality == api.QualityOK && math.Abs(c.tS.v-c.setpoint().TempC) < 1.5 {
			x.band += dt
		}
	}
	if t-x.start < 3600 {
		return
	}
	pt := CrossPt{Door: x.door, Band: x.bandN > 0 && x.band/x.bandN >= 0.8}
	if x.n > 0 {
		pt.Net, pt.Hum, pt.De = x.net/x.n, x.hum/x.n, x.de/x.n
	}
	if x.wn > 1800 {
		pt.W, pt.WOK = x.w/x.wn, true
	}
	*x = crossAcc{start: t}
	// Only control running on good readings teaches anything.
	if c.chT.quality != api.QualityOK || c.chRH.quality != api.QualityOK {
		c.st.Cross = c.st.Cross[:0]
		return
	}
	c.st.Cross = append(c.st.Cross, pt)
	if len(c.st.Cross) > crossMax {
		c.st.Cross = append(c.st.Cross[:0], c.st.Cross[len(c.st.Cross)-crossMax:]...)
	}
	c.crossEval()
}

func (c *Core) crossReset() {
	c.st.Cross = c.st.Cross[:0]
	c.st.CrossBase = nil
	c.cross = crossAcc{}
}

// lossPerDay is the least-squares weight loss in percent per day; ok is false if a
// point is missing.
func lossPerDay(p []CrossPt) (float64, bool) {
	var sx, sy, sxx, sxy, n float64
	for i := range p {
		if !p[i].WOK {
			continue
		}
		x := float64(i)
		sx += x
		sy += p[i].W
		sxx += x * x
		sxy += x * p[i].W
		n++
	}
	if n < 5 || n < float64(len(p))*0.8 || sy <= 0 {
		return 0, false
	}
	den := n*sxx - sx*sx
	if den <= 0 {
		return 0, false
	}
	return -(n*sxy - sx*sy) / den * 24 / (sy / n) * 100, true
}

func meanNet(p []CrossPt) (net, hum, de float64) {
	for i := range p {
		net += p[i].Net
		hum += p[i].Hum
		de += p[i].De
	}
	n := float64(len(p))
	return net / n, hum / n, de / n
}

func (c *Core) crossEval() {
	h := c.st.Cross
	n := len(h)
	// The first day of a setpoint is transition.
	if n < 72 {
		return
	}
	// A cabinet that cannot hold its setpoint has effort that says nothing about the
	// sensor. Its problem is already alarmed, directly, from the reading.
	inBand := 0
	lookback := 48
	for i := n - lookback; i < n; i++ {
		if h[i].Band {
			inBand++
		}
	}
	if float64(inBand) < 0.8*float64(lookback) {
		c.crossTempSoft, c.crossRHSoft, c.crossRateSoft = false, false, false
		return
	}
	// Base: up to 72 h ending a day ago. Now: the last 24 h, and a fast look at the last 6 h
	// against everything before them.
	baseEnd := n - 24
	baseStart := baseEnd - 72
	if baseStart < 12 {
		baseStart = 12
	}
	base := h[baseStart:baseEnd]
	now := h[n-24:]
	fast := h[n-6:]
	fast12 := h[n-12:]
	fastBase12 := h[maxInt(12, n-84) : n-12]
	bNet, bHum, bDe := meanNet(base)
	nNet, nHum, nDe := meanNet(now)
	fNet, _, _ := meanNet(fast)
	dN, dH, dD := nNet-bNet, nHum-bHum, nDe-bDe

	lb, okB := lossPerDay(base)
	ln, okN := lossPerDay(now)
	// Six hours of drying are too noisy on a small or slow load; twelve are not.
	lfb, okFB := lossPerDay(fastBase12)
	lf, okF := lossPerDay(fast12)
	var r, rf float64 = 1, 1
	haveR := okB && okN && lb >= 0.15
	if haveR {
		r = ln / lb
	}
	haveRF := okFB && okF && lfb >= 0.15
	if haveRF {
		rf = lf / lfb
	}

	// Slow drift keeps inside any moving baseline, so effort is also compared with
	// what this setpoint took in its first days since the last check, and drying
	// with four days ago. Fresh product dries more slowly each day, so a rate that
	// holds or climbs is itself the anomaly.
	if c.st.CrossBase == nil && n >= 48 {
		bn, bh, bd := meanNet(h[24:48])
		c.st.CrossBase = &CrossPt{Net: bn, Hum: bh, De: bd}
	}
	rl := 1.0
	haveRL := false
	if n >= 120 {
		if l0, ok0 := lossPerDay(h[n-120 : n-96]); ok0 && okN && l0 >= 0.15 {
			rl, haveRL = ln/l0, true
		}
	}
	var fzN, fzH, fzD float64
	if b := c.st.CrossBase; b != nil && n >= 72 {
		fzN, fzH, fzD = nNet-b.Net, nHum-b.Hum, nDe-b.De
	}

	// The same six hours yesterday cancels the room's daily cycle, which is what makes
	// a fast look usable. A room can move by this much in a day only rarely (about 8 °C).
	var dY float64
	doorNear := false
	for i := n - 6; i < n; i++ {
		doorNear = doorNear || h[i].Door
	}
	yNet, _, _ := meanNet(h[n-30 : n-24])
	dY = fNet - yNet
	// How much that comparison moves on this cabinet with nothing wrong. Frost and defrost
	// shift it far more on a cold cabinet than on a warm one, so a fixed line cannot do.
	var sum2 float64
	cnt := 0.0
	for i := 30; i <= n-12; i += 3 {
		a, _, _ := meanNet(h[i-6 : i])
		b, _, _ := meanNet(h[i-30 : i-24])
		sum2 += (a - b) * (a - b)
		cnt++
	}
	sigma := 0.0
	if cnt > 0 {
		sigma = math.Sqrt(sum2 / cnt)
	}
	alone := math.Max(0.11, 4*sigma)
	both := math.Max(0.04, 2.5*sigma)
	fastLow := !doorNear && (dY <= -alone || (fNet <= -0.05 && yNet >= -0.01))
	fastHigh := !doorNear && dY >= alone

	note := fmt.Sprintf("net duty now %.3f base %.3f, last 6 h %.3f same hours yesterday %.3f sigma %.3f, drying ratio %.2f fast %.2f", nNet, bNet, fNet, yNet, sigma, r, rf)
	// Temperature reading low: warm product hidden. Both signals, or the heater running in a cabinet that needed cooling.
	last3, _, _ := meanNet(h[n-3:])
	before3, _, _ := meanNet(h[maxInt(12, n-51) : n-3])
	switch {
	case haveR && r >= 1.3 && dN <= -0.03,
		!doorNear && haveRF && rf >= 1.3 && dY <= -both,
		haveRL && rl >= 1.1 && fzN <= -0.03,
		before3 >= 0.08 && last3 <= -0.01,
		fastLow:
		c.latchSensor(true, "reading may be low: "+note)
	case haveR && r <= 0.7 && dN >= 0.03,
		!doorNear && haveRF && rf <= 0.75 && dY >= both,
		haveRL && rl <= 0.6 && fzN >= 0.03,
		fastHigh:
		c.latchSensor(true, "reading may be high: "+note)
	case haveR && r <= 0.7 && (dH >= 0.01 || dD <= -0.03),
		haveRL && rl <= 0.55 && (fzH >= 0.01 || fzD <= -0.03):
		c.latchSensor(false, "reading may be low: "+note)
	case haveR && r >= 1.3 && (dD >= 0.03 || dH <= -0.01),
		haveRL && rl >= 1.2 && (fzD >= 0.03 || fzH <= -0.01):
		c.latchSensor(false, "reading may be high: "+note)
	}

	// One signal alone: a warning that asks for a check, and changes nothing.
	c.crossTempSoft = math.Abs(dN) >= 0.10 || math.Abs(fzN) >= 0.07
	c.crossRHSoft = dD >= 0.06 || dH >= 0.03 || fzD >= 0.07 || fzH >= 0.03
	c.crossRateSoft = haveR && (r >= 1.4 || r <= 0.6)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// doubtHold is how long a suspicion stays up after its evidence was last seen. It ends
// by itself, so nothing here waits for an operator who may be away.
const doubtHold = 48 * 3600.0

// latchSensor records a suspicion. It raises an alarm and marks the displayed reading.
// It does not touch regulation: see docs/adr/0113. A direct reading that passes every
// direct test keeps control, because a false suspicion taking control away did far more
// harm in round 2 than a missed one (a healthy cabinet held 8 to 14 °C off setpoint for
// days), and the false-positive rate of these checks cannot be known before real cures.
func (c *Core) latchSensor(temp bool, why string) {
	if temp && strings.HasPrefix(why, "reading may be low") {
		c.st.TDoubtLow = true
	}
	until := c.st.PoweredSec + doubtHold
	if temp {
		if c.st.TDoubtUntil < until {
			c.st.TDoubtUntil = until
		}
		if _, up := c.al.active[CodeTempDisagree]; !up {
			c.raise(CodeTempDisagree, c.tS.v, 0)
			c.event(Event{Type: EvPhase, Code: CodeTempDisagree, Msg: why})
		}
	} else {
		if c.st.RHDoubtUntil < until {
			c.st.RHDoubtUntil = until
		}
		if _, up := c.al.active[CodeRHDisagree]; !up {
			c.raise(CodeRHDisagree, c.rhS.v, 0)
			c.event(Event{Type: EvPhase, Code: CodeRHDisagree, Msg: why})
		}
	}
}

// doubtExpire ends suspicions whose evidence has gone.
func (c *Core) doubtExpire() {
	p := c.st.PoweredSec
	if c.st.TDoubtUntil != 0 && p >= c.st.TDoubtUntil {
		c.st.TDoubtUntil = 0
		c.st.TDoubtLow = false
		c.clear(CodeTempDisagree)
	}
	if c.st.RHDoubtUntil != 0 && p >= c.st.RHDoubtUntil {
		c.st.RHDoubtUntil = 0
		c.clear(CodeRHDisagree)
	}
}

// referenceCheck compares a separate instrument with the sensors and records it.
func (c *Core) referenceCheck(r *api.Reference) error {
	if r == nil || (r.TempC == nil && r.RHPct == nil) {
		return fmt.Errorf("enter what the reference instrument reads")
	}
	const tolT, tolRH = 1.5, 5.0 // BENCHMARK 7.1: air thermometers ±1.5 °C
	msg := ""
	bad := false
	if r.TempC != nil {
		diff := c.chT.val - *r.TempC
		msg += fmt.Sprintf("temperature sensor %.1f reference %.1f difference %.1f C. ", c.chT.val, *r.TempC, diff)
		if math.Abs(diff) > tolT {
			bad = true
			// A person's measurement: up for a week, or until a check passes. It
			// still does not take control away. The way to act on it is a calibration.
			c.st.TDoubtUntil = c.st.PoweredSec + 7*24*3600
			c.st.TDoubtLow = diff < 0
			c.raise(CodeTempDisagree, c.chT.val, *r.TempC)
		} else {
			c.st.TDoubtUntil = 0
			c.st.TDoubtLow = false
			c.clear(CodeTempDisagree)
		}
	}
	if r.RHPct != nil {
		diff := c.chRH.val - *r.RHPct
		msg += fmt.Sprintf("humidity sensor %.1f reference %.1f difference %.1f %%RH. ", c.chRH.val, *r.RHPct, diff)
		if math.Abs(diff) > tolRH {
			bad = true
			c.st.RHDoubtUntil = c.st.PoweredSec + 7*24*3600
			c.raise(CodeRHDisagree, c.chRH.val, *r.RHPct)
		} else {
			c.st.RHDoubtUntil = 0
			c.clear(CodeRHDisagree)
		}
	}
	if bad {
		c.raise(CodeRefDisagree, 0, 0)
	} else {
		c.clear(CodeRefDisagree)
		c.st.LastCal = c.st.PoweredSec
		if c.clockOK {
			c.st.LastRefWall = c.clock.Now()
		}
	}
	c.crossReset()
	c.event(Event{Type: EvRefCheck, Msg: msg})
	c.saveSoon()
	return nil
}
