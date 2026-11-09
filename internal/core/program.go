package core

import (
	"fmt"
	"math"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// VerifyText is shown at the end of every cure. This wording is deliberate: the
// controller does not measure water activity or pH and makes no safety claim.
const VerifyText = "This recipe has ended. Weight loss is a yield figure and does not show that product is safe. " +
	"Verify water activity and pH with calibrated instruments, and judge the product by your own validated process plan. " +
	"The controller keeps holding conditions until you stop it."

const recipeNote = "Example starting point, not a validated process. "

// BuiltInRecipes returns the built-in recipes. Conditions are drawn from
// BENCHMARK section 7; the operator owns whether they suit a formulation.
func BuiltInRecipes() []api.Recipe {
	h := time.Hour
	d := 24 * time.Hour
	return []api.Recipe{
		{ID: "ferment-dry", Name: "Fermented sausage", BuiltIn: true,
			Notes: recipeNote + "Needs a starter culture and a process plan with pH and degree-hour control.",
			Phases: []api.PhaseSpec{
				{Name: "Ferment", Setpoint: api.Setpoints{TempC: 24, RHPct: 90, AirMS: 1.0}, Duration: 48 * h},
				{Name: "Dry early", Setpoint: api.Setpoints{TempC: 14, RHPct: 82, AirMS: 1.0}, Duration: 7 * d},
				{Name: "Dry", Setpoint: api.Setpoints{TempC: 13, RHPct: 76, AirMS: 1.0}, TargetLossPct: 32},
			}},
		{ID: "whole-muscle", Name: "Whole muscle (coppa, bresaola)", BuiltIn: true,
			Notes: recipeNote + "Salt-cured product. Cure at refrigeration temperature before drying.",
			Phases: []api.PhaseSpec{
				{Name: "Equalize", Setpoint: api.Setpoints{TempC: 4, RHPct: 85, AirMS: 1.0}, Duration: 7 * d},
				{Name: "Dry", Setpoint: api.Setpoints{TempC: 13, RHPct: 75, AirMS: 1.0}, TargetLossPct: 35},
			}},
		{ID: "beef-age", Name: "Dry-age beef", BuiltIn: true,
			Notes: recipeNote + "Refrigeration-range ageing. Trim the crust before use.",
			Phases: []api.PhaseSpec{
				{Name: "Age", Setpoint: api.Setpoints{TempC: 1.5, RHPct: 82, AirMS: 1.0}, Duration: 28 * d},
			}},
		{ID: "biltong", Name: "Biltong style", BuiltIn: true,
			Notes: recipeNote + "Warm air drying. Keep to a validated recipe; the process depends on it.",
			Phases: []api.PhaseSpec{
				{Name: "Dry", Setpoint: api.Setpoints{TempC: 25, RHPct: 55, AirMS: 1.5}, Duration: 5 * d},
			}},
	}
}

func (c *Core) allRecipes() []api.Recipe {
	out := BuiltInRecipes()
	return append(out, c.st.UserRecipes...)
}

func (c *Core) findRecipe(id string) *api.Recipe {
	for _, r := range c.allRecipes() {
		if r.ID == id {
			r := r
			return &r
		}
	}
	return nil
}

// setpoint returns the setpoints in force now.
func (c *Core) setpoint() api.Setpoints {
	p := &c.st.Program
	if p.Recipe != nil && p.State != api.RunIdle && p.PhaseIndex < len(p.Recipe.Phases) {
		if p.Override != nil {
			return *p.Override
		}
		return p.Recipe.Phases[p.PhaseIndex].Setpoint
	}
	return c.st.Manual
}

// updateProgram advances cure time. Time is counted from the monotonic clock,
// powered and unpaused only, so a wrong or jumping wall clock cannot change it.
// The outage itself does not count: a phase never gets shorter because of a
// power cut, whether its length is known or not.
func (c *Core) updateProgram(dt float64) {
	p := &c.st.Program
	if p.State == api.RunActive || p.State == api.RunPaused {
		// Degree-hours above the 15.6 °C reference are recorded for the process
		// record whatever the state. Missing data makes the total a lower bound.
		if c.chT.quality == api.QualityOK {
			if ex := c.tF.v - 15.6; ex > 0 {
				p.DegreeHours += ex * dt / 3600
			}
		} else {
			p.DHIncomplete = true
		}
	}
	if p.State != api.RunActive || p.Recipe == nil {
		return
	}
	p.PhaseElapsed += dt
	p.TotalElapsed += dt

	// Weight is locked in as the start once it can be trusted.
	if p.StartWeightG <= 0 && c.weightUsable() {
		p.StartWeightG = c.weightF.v
		c.event(Event{Type: EvPhase, Msg: fmt.Sprintf("start weight %.0f g", p.StartWeightG)})
		c.saveSoon()
	}

	ph := p.Recipe.Phases[p.PhaseIndex]
	done := ""
	if ph.Duration > 0 && p.PhaseElapsed >= ph.Duration.Seconds() {
		done = "time"
	}
	if ph.TargetLossPct > 0 && c.weightUsable() && p.StartWeightG > 0 {
		if c.lossPct() >= ph.TargetLossPct {
			if c.lossHoldT == 0 {
				c.lossHoldT = c.t
			}
			// Must hold for two hours so that a bump of the scale does not end a phase.
			if c.t-c.lossHoldT >= 2*3600 {
				done = "weight target"
			}
		} else {
			c.lossHoldT = 0
		}
	} else {
		c.lossHoldT = 0
	}
	if done != "" {
		c.advancePhase(done)
	}
}

func (c *Core) advancePhase(why string) {
	p := &c.st.Program
	c.lossHoldT = 0
	if p.PhaseIndex+1 >= len(p.Recipe.Phases) {
		p.State = api.RunFinished
		c.event(Event{Type: EvPhase, Msg: "recipe finished: " + why})
		c.raise(CodeCureFinished, 0, 0)
		c.saveSoon()
		return
	}
	p.PhaseIndex++
	p.PhaseElapsed = 0
	p.Override = nil
	c.event(Event{Type: EvPhase, Msg: fmt.Sprintf("phase %d: %s (%s)", p.PhaseIndex, p.Recipe.Phases[p.PhaseIndex].Name, why)})
	c.setpointChanged()
	c.saveSoon()
}

// rebaseLoss carries weight loss across a load change, so it does not read -46 % after
// 5 kg of fresh product goes in (integration defect 8). Added product starts with no
// loss, so its weight joins the start weight. Removed product is assumed to have lost
// what the batch had, so the start weight shrinks in proportion and the figure does not
// jump: crediting the remaining product with water the removed pieces lost would make
// it look drier than it is, the unsafe direction. If the load is gone, the start weight
// is cleared and the next good reading becomes the new start.
func (c *Core) rebaseLoss(before, after float64) {
	p := &c.st.Program
	if p.State == api.RunIdle || p.StartWeightG <= 0 {
		return
	}
	// What the figure was before the step, from the weight before it: the filtered
	// weight has already moved, and against the old start weight it reads as nonsense.
	lossOf := func(start, w float64) float64 {
		if start <= 0 {
			return 0
		}
		return (start - w) / start * 100
	}
	lossBefore := lossOf(p.StartWeightG, before)
	delta := after - before
	if delta >= 0 || before <= 0 {
		p.StartWeightG += delta
	} else {
		p.StartWeightG *= math.Max(after, 0) / before
	}
	if p.StartWeightG < 100 {
		p.StartWeightG = 0
	}
	c.event(Event{Type: EvPhase, Msg: fmt.Sprintf("load changed by %+.0f g: weight loss re-based on the new load (%.1f %% before the change, %.1f %% after; start weight now %.0f g)", delta, lossBefore, lossOf(p.StartWeightG, after), p.StartWeightG)})
	c.saveSoon()
}

// stepConfirm is how long a new weight level must hold before it is taken as a load
// change.
const stepConfirm = 600.0

func (c *Core) lossPct() float64 {
	p := &c.st.Program
	if p.StartWeightG <= 0 || !c.weightF.init {
		return 0
	}
	w := c.weightF.v
	if c.rebasePending {
		w = c.weightBefore // the figure before the step, until the step is confirmed
	}
	return (p.StartWeightG - w) / p.StartWeightG * 100
}

func (c *Core) scaleCal() bool { return math.Abs(c.st.Cal.ScaleCountsPG) > 1e-6 }

func (c *Core) weightUsable() bool {
	return c.scaleCal() && c.chScale.quality == api.QualityOK && !c.scaleImplausible && c.weightF.init
}

// weightTrack keeps a ring of filtered weight to estimate the current rate and
// catch impossible movement. Product on a load cell can only lose mass quickly,
// so a sustained rise means the reading is wrong or something changed.
type weightTrack struct {
	t, w     [288]float64
	n, idx   int
	nextT    float64
	rate     float64
	baseline float64
	lastStep float64
}

func (c *Core) updateWeight(t float64) {
	if !c.scaleCal() {
		return
	}
	if c.chScale.quality != api.QualityOK {
		return
	}
	wt := &c.wt
	v := c.weightF.v
	if t < wt.nextT {
		return
	}
	wt.nextT = t + 300
	wt.t[wt.idx] = t
	wt.w[wt.idx] = v
	wt.idx = (wt.idx + 1) % len(wt.t)
	if wt.n < len(wt.t) {
		wt.n++
	}
	if wt.n < 12 {
		return
	}
	// Least squares over the window (24 h when full).
	var st, sw, stt, stw float64
	n := float64(wt.n)
	for i := 0; i < wt.n; i++ {
		x := wt.t[i] / 86400
		y := wt.w[i]
		st += x
		sw += y
		stt += x * x
		stw += x * y
	}
	den := n*stt - st*st
	if den > 0 {
		wt.rate = (n*stw - st*sw) / den // g per day, over the whole window
		// Plausibility looks at the last 6 h only, so that it also clears.
		var a, b, c2, d, m float64
		for i := 0; i < wt.n; i++ {
			if t-wt.t[i] > 6*3600 {
				continue
			}
			x := wt.t[i] / 86400
			y := wt.w[i]
			a += x
			b += y
			c2 += x * x
			d += x * y
			m++
		}
		slope := wt.rate
		if m >= 12 && m*c2-a*a > 0 {
			slope = (m*d - a*b) / (m*c2 - a*a)
		}
		base := c.st.Program.StartWeightG
		if base <= 0 {
			base = math.Abs(v)
		}
		if base > 100 {
			// Rising by more than 0.5 %/day over a full day cannot be evaporation.
			// Losing more than 8 %/day is far above any drying rate.
			span := (wt.t[(wt.idx-1+wt.n)%wt.n] - wt.t[(wt.idx-wt.n+len(wt.t))%len(wt.t)])
			if span >= 6*3600 {
				pct := slope / base * 100
				was := c.scaleImplausible
				c.scaleImplausible = pct > 0.5 || pct < -8
				if was != c.scaleImplausible {
					c.event(Event{Type: EvPhase, Msg: fmt.Sprintf("weight trend %.2f %%/day flagged=%v", pct, c.scaleImplausible)})
				}
			}
		}
	}
}

func (c *Core) programView() api.Program {
	p := &c.st.Program
	v := api.Program{State: p.State, PhaseRemaining: -1}
	if v.State == "" {
		v.State = api.RunIdle
	}
	if p.Recipe == nil {
		return v
	}
	v.RecipeID, v.RecipeName = p.Recipe.ID, p.Recipe.Name
	v.PhaseIndex = p.PhaseIndex
	v.PhaseCount = len(p.Recipe.Phases)
	if p.PhaseIndex < len(p.Recipe.Phases) {
		ph := p.Recipe.Phases[p.PhaseIndex]
		v.PhaseName = ph.Name
		v.TargetLossPct = ph.TargetLossPct
		if ph.Duration > 0 {
			rem := ph.Duration - time.Duration(p.PhaseElapsed*float64(time.Second))
			if rem < 0 {
				rem = 0
			}
			v.PhaseRemaining = rem
		}
	}
	if p.Override != nil {
		o := *p.Override
		v.Override = &o
	}
	v.PhaseElapsed = time.Duration(p.PhaseElapsed * float64(time.Second))
	v.TotalElapsed = time.Duration(p.TotalElapsed * float64(time.Second))
	v.StartWeightG = p.StartWeightG
	if c.weightUsable() {
		v.WeightLossPct = c.lossPct()
		if p.StartWeightG > 0 {
			v.LossPctPerDay = -c.wt.rate / p.StartWeightG * 100
		}
	}
	v.OutageCount = p.Outages
	v.OutageKnown = time.Duration(p.OutageKnownS * float64(time.Second))
	v.OutageUnknown = p.OutageUnk
	v.DegreeHoursAbove156 = p.DegreeHours
	v.DegreeHoursIncomplete = p.DHIncomplete
	v.DegreeHoursOutageEst = p.DHOutageEst
	if p.State == api.RunFinished {
		v.Verify = VerifyText
	}
	return v
}
