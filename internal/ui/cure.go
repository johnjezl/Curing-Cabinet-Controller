package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

func recipeSummary(r api.Recipe) string {
	var total time.Duration
	open := false
	for _, ph := range r.Phases {
		if ph.Duration == 0 {
			open = true
		}
		total += ph.Duration
	}
	s := fmt.Sprintf("%d phases", len(r.Phases))
	if len(r.Phases) == 1 {
		s = "1 phase"
	}
	if total > 0 {
		s += ", " + fmtDur(total)
	}
	if open {
		s += " + weight target"
	}
	return s
}

func (p *Panel) drawCure(c *Canvas) {
	pr := p.snap.Program
	switch pr.State {
	case api.RunActive, api.RunPaused:
		p.drawCureRunning(c)
	case api.RunFinished:
		p.drawCureFinished(c)
	default:
		p.drawRecipeList(c)
	}
}

func (p *Panel) drawRecipeList(c *Canvas) {
	rs := p.snap.Recipes
	if len(rs) == 0 {
		c.Text(rect(mX, bodyTop, mR, bodyBot), "No recipes stored.", Style{Font: FontBold, Size: 34, Color: colDim, H: Center})
		return
	}
	per := 3
	pages := (len(rs) + per - 1) / per
	if p.recPage >= pages {
		p.recPage = pages - 1
	}
	listR := mR
	if pages > 1 {
		listR = mR - 94
	}
	h := (bodyBot - bodyTop - 2*4) / per
	for i := 0; i < per; i++ {
		ri := p.recPage*per + i
		if ri >= len(rs) {
			break
		}
		y := bodyTop + i*(h+4)
		rc := rs[ri]
		r := rect(mX, y, listR, y+h)
		id := fmt.Sprintf("recipe.%d", ri)
		bg := colPanel
		if p.isPressed(id) {
			bg = colPanel2
		}
		c.Round(r, 6, bg)
		c.Stroke(r, 6, 2, colLine, bg)
		c.Text(rect(r.Min.X+14, r.Min.Y+4, r.Max.X-14, r.Min.Y+44), rc.Name, Style{Font: FontBold, Size: 30, Color: colText, MinSize: 18})
		sum := recipeSummary(rc)
		if !rc.BuiltIn {
			sum = "yours, " + sum
		}
		c.Text(rect(r.Min.X+14, r.Min.Y+44, r.Max.X-14, r.Max.Y-4), sum, Style{Size: 23, Color: colDim, MinSize: 15})
		rec := rc
		p.addHit(r, id, func() {
			p.recipe = &rec
			p.go_(scrRecipe)
		})
	}
	if pages > 1 {
		p.pager(c, listR+4, bodyTop, mR, bodyBot, p.recPage, pages, "recipes.page", func(n int) { p.recPage = n })
	}
}

// phaseTable draws one row per phase; cur is highlighted (-1 for none).
func (p *Panel) phaseTable(c *Canvas, x0, y0, x1, y1 int, phases []api.PhaseSpec, cur int) {
	cols := []int{0, 300, 400, 490, 590}
	w := x1 - x0
	scale := func(i int) int { return x0 + cols[i]*w/780 }
	hdr := []string{"PHASE", "°C", "%RH", "m/s", "TIME / TARGET"}
	for i, t := range hdr {
		e := x1 - 4
		if i+1 < len(cols) {
			e = scale(i+1) - 6
		}
		c.Text(rect(scale(i)+6, y0, e, y0+22), t, Style{Font: FontBold, Size: 18, Color: colMuted, MinSize: 13})
	}
	rowH := 27
	maxRows := (y1 - y0 - 22) / rowH
	start := 0
	if cur+1 >= maxRows { // keep the next phase in view too
		start = cur + 2 - maxRows
	}
	if start > len(phases)-maxRows {
		start = len(phases) - maxRows
	}
	if start < 0 {
		start = 0
	}
	for ri := 0; ri < maxRows && start+ri < len(phases); ri++ {
		i := start + ri
		ph := phases[i]
		y := y0 + 22 + ri*rowH
		fg := colText
		style := FontRegular
		if i == cur {
			c.Round(rect(x0, y, x1, y+rowH-2), 4, colOn)
			fg = colBlack
			style = FontBold
		}
		end := fmtDur(ph.Duration)
		if ph.Duration == 0 {
			end = "until target"
		}
		if ph.TargetLossPct > 0 {
			end += fmt.Sprintf(", %.0f%% loss", ph.TargetLossPct)
		}
		vals := []string{fmt.Sprintf("%d %s", i+1, ph.Name), fmtFloat(ph.Setpoint.TempC, 1), fmtFloat(ph.Setpoint.RHPct, 0), fmtFloat(ph.Setpoint.AirMS, 2), end}
		for j, t := range vals {
			e := x1 - 4
			if j+1 < len(cols) {
				e = scale(j+1) - 6
			}
			c.Text(rect(scale(j)+6, y, e, y+rowH-2), t, Style{Font: style, Size: 21, Color: fg, MinSize: 14})
		}
	}
	if start+maxRows < len(phases) {
		c.Text(rect(x0, y1-20, x1-4, y1), fmt.Sprintf("+%d more", len(phases)-start-maxRows), Style{Size: 16, Color: colDim, H: Right})
	}
}

// drawRecipe shows what a recipe will do before it is started. No black boxes.
func (p *Panel) drawRecipe(c *Canvas) {
	rc := p.recipe
	if rc == nil {
		p.go_(scrCure)
		return
	}
	bot := bodyBot
	if p.locked {
		bot = bodyBot - 101
	}
	c.Round(rect(mX, bodyTop, mR, bot), 6, colPanel)
	c.Text(rect(mX+14, bodyTop+4, mR-14, bodyTop+42), rc.Name, Style{Font: FontBold, Size: 30, Color: colText, MinSize: 18})
	y := bodyTop + 42
	if rc.Notes != "" {
		c.Text(rect(mX+14, y, mR-14, y+28), rc.Notes, Style{Size: 20, Color: colDim, MinSize: 14})
		y += 28
	}
	p.phaseTable(c, mX+8, y, mR-8, bot-6, rc.Phases, -1)
	if p.locked {
		p.lockedNotice(c, "Locked: starting a cure is disabled.")
		return
	}
	b := rect(mX, navTop, mX+300, navBot)
	st := rect(mR-300, navTop, mR, navBot)
	p.button(c, b, "recipe.back", "BACK", btnNormal, func() {
		p.recipe = nil
		p.go_(scrCure)
	})
	p.button(c, st, "recipe.start", "START…", btnPrimary, func() { p.askStart(*rc) })
}

func (p *Panel) askStart(rc api.Recipe) {
	w := p.snap.Weight
	body := "The cabinet setpoints will follow this recipe from phase 1."
	if len(rc.Phases) > 0 {
		sp := rc.Phases[0].Setpoint
		body = fmt.Sprintf("The cabinet moves to phase 1, %s: %s °C, %s %%, %s m/s. It is at %s °C, %s %% now.", rc.Phases[0].Name, fmtFloat(sp.TempC, 1), fmtFloat(sp.RHPct, 0), fmtFloat(sp.AirMS, 2), fmtFloat(p.snap.Setpoint.TempC, 1), fmtFloat(p.snap.Setpoint.RHPct, 0))
	}
	cmd := api.Command{Kind: api.CmdStart, RecipeID: rc.ID}
	if readingGood(w) && !p.Stalled() {
		cmd.StartWeightG = w.Value
		body += " Start weight is recorded as " + fmtWeight(w.Value) + " g."
	} else {
		body += " The scale reading is " + strings.ToLower(qualityWord(w.Quality)) + ", so weight loss will not be tracked until it is fixed."
	}
	p.ask(dialog{id: "start", title: "Start " + rc.Name + "?", body: body, confirm: "HOLD 2 s: START", hold: p.cfg.ConfirmHold, fn: func() {
		p.recipe = nil
		p.screen = scrCure
		p.command(cmd, "Cure started.")
	}})
}

func (p *Panel) drawCureRunning(c *Canvas) {
	s := &p.snap
	pr := s.Program
	top := rect(mX, bodyTop, mR, bodyTop+92)
	c.Round(top, 6, colPanel)
	c.Text(rect(top.Min.X+12, top.Min.Y+2, top.Max.X-12, top.Min.Y+34), fmt.Sprintf("%s: phase %d of %d, %s", pr.RecipeName, pr.PhaseIndex+1, pr.PhaseCount, pr.PhaseName), Style{Font: FontBold, Size: 25, Color: colText, MinSize: 16})
	stalled := p.Stalled()
	var line string
	if pr.PhaseRemaining < 0 {
		line = "Phase ends at weight target or by you"
	} else {
		line = fmtDur(pr.PhaseRemaining) + " left in phase"
	}
	col := colText
	if pr.State == api.RunPaused {
		line = "PAUSED, clock stopped. " + line
		col = colWarn
	}
	if stalled {
		line = "NOT LIVE. Last known: " + line
		col = colWarn
	}
	c.Text(rect(top.Min.X+12, top.Min.Y+33, top.Max.X-12, top.Min.Y+62), line, Style{Font: FontBold, Size: 24, Color: col, MinSize: 15})
	rate := ""
	if finite(pr.LossPctPerDay) && pr.LossPctPerDay != 0 {
		rate = fmt.Sprintf(", %.2f %%/day", pr.LossPctPerDay)
	}
	sub := "Elapsed " + fmtDur(pr.TotalElapsed) + rate
	if stalled {
		sub = "Last known, not counting"
	}
	if ov := pr.Override; ov != nil {
		rs := p.recipeSetpoint()
		sub = fmt.Sprintf("OVERRIDE %s °C %s %%", fmtFloat(ov.TempC, 1), fmtFloat(ov.RHPct, 0))
		if rs != nil {
			sub += fmt.Sprintf(" (recipe %s, %s)", fmtFloat(rs.TempC, 1), fmtFloat(rs.RHPct, 0))
		}
	}
	if pr.StartWeightG <= 0 {
		sub = "Elapsed " + fmtDur(pr.TotalElapsed) + ", no start weight"
	}
	barR := rect(top.Min.X+400, top.Min.Y+63, top.Max.X-12, top.Max.Y-4)
	subCol, subFont := colDim, FontRegular
	if pr.Override != nil {
		subCol, subFont = colWarn, FontBold
	}
	c.Text(rect(top.Min.X+12, top.Min.Y+63, barR.Min.X-8, top.Max.Y-4), sub, Style{Font: subFont, Size: 20, Color: subCol, MinSize: 13})
	p.lossBar(c, barR)

	const btnTop = bodyBot - 89
	tbl := rect(mX, top.Max.Y+4, mR, btnTop-4)
	c.Round(tbl, 6, colPanel)
	if phases := p.currentPhases(); len(phases) > 0 {
		p.phaseTable(c, tbl.Min.X+8, tbl.Min.Y+1, tbl.Max.X-8, tbl.Max.Y-1, phases, pr.PhaseIndex)
	} else {
		c.Text(tbl, "Phase table not available.", Style{Size: 22, Color: colDim, H: Center})
	}
	if stalled {
		r := rect(mX, btnTop, mR, bodyBot)
		c.Round(r, 6, colPanel2)
		c.Wrap(r.Inset(8), "Cure controls are off: the controller is not responding.", Style{Font: FontBold, Size: 26, Color: colText, H: Center, MinSize: 16}, 2)
		return
	}
	if p.locked {
		p.lockedNotice(c, "Locked: cure controls are disabled.")
		return
	}
	bw := (mR - mX - 2*60) / 3
	pause := rect(mX, btnTop, mX+bw, bodyBot)
	next := rect(pause.Max.X+60, btnTop, pause.Max.X+60+bw, bodyBot)
	stop := rect(mR-bw, btnTop, mR, bodyBot)
	if pr.State == api.RunPaused {
		p.button(c, pause, "cure.resume", "RESUME", btnPrimary, func() { p.command(api.Command{Kind: api.CmdResume}, "Cure resumed.") })
	} else {
		p.holdButton(c, pause, "cure.pause", "HOLD 1 s: PAUSE", time.Second, btnNormal, func() { p.command(api.Command{Kind: api.CmdPause}, "Cure paused. The phase clock has stopped.") })
	}
	p.button(c, next, "cure.next", "NEXT PHASE…", btnNormal, p.askNext)
	p.button(c, stop, "cure.stop", "STOP…", btnDanger, p.askStop)
}

func (p *Panel) currentPhases() []api.PhaseSpec {
	for _, r := range p.snap.Recipes {
		if r.ID == p.snap.Program.RecipeID {
			return r.Phases
		}
	}
	return nil
}

func (p *Panel) askNext() {
	pr := p.snap.Program
	nextName := "the next phase"
	ph := p.currentPhases()
	if pr.PhaseIndex+1 < len(ph) {
		nextName = fmt.Sprintf("phase %d, %s (%s °C, %s %%)", pr.PhaseIndex+2, ph[pr.PhaseIndex+1].Name, fmtFloat(ph[pr.PhaseIndex+1].Setpoint.TempC, 1), fmtFloat(ph[pr.PhaseIndex+1].Setpoint.RHPct, 0))
	}
	p.ask(dialog{id: "next", title: "Skip to next phase?", body: "This ends " + pr.PhaseName + " early with " + remainingWords(pr) + ". The cabinet moves to " + nextName + ". You cannot go back.",
		confirm: "HOLD 2 s: NEXT PHASE", hold: p.cfg.ConfirmHold, danger: true, fn: func() {
			p.command(api.Command{Kind: api.CmdNextPhase}, "Moved to next phase.")
		}})
}

func remainingWords(pr api.Program) string {
	if pr.PhaseRemaining < 0 {
		return "no fixed time left"
	}
	return fmtDur(pr.PhaseRemaining) + " left"
}

func (p *Panel) askStop() {
	pr := p.snap.Program
	p.ask(dialog{id: "stop", title: "Stop this cure?", body: "This ends " + pr.RecipeName + " after " + fmtDur(pr.TotalElapsed) + ". The recipe cannot be resumed. The cabinet holds the current setpoints in manual until you change them.",
		confirm: "HOLD 2 s: STOP CURE", cancel: "KEEP RUNNING", hold: p.cfg.ConfirmHold, danger: true, fn: func() {
			p.command(api.Command{Kind: api.CmdStop}, "Cure stopped.")
		}})
}

// drawCureFinished shows the Verify text from the core verbatim. The panel adds
// no claim about the product.
func (p *Panel) drawCureFinished(c *Canvas) {
	pr := p.snap.Program
	bot := bodyBot - 101 // room for the end button or the unlock notice
	r := rect(mX, bodyTop, mR, bot)
	c.Round(r, 6, colPanel)
	c.Text(rect(r.Min.X+14, r.Min.Y+4, r.Max.X-14, r.Min.Y+44), pr.RecipeName+": ended, not yet verified", Style{Font: FontBold, Size: 30, Color: colWarn, MinSize: 17})
	sum := fmt.Sprintf("Loss %s %%, %s elapsed. Still holding conditions.", fmtFloat(pr.WeightLossPct, 1), fmtDur(pr.TotalElapsed))
	if !readingGood(p.snap.Weight) {
		sum = "Weight loss not known: scale " + strings.ToLower(qualityWord(p.snap.Weight.Quality)) + ". " + fmtDur(pr.TotalElapsed) + " elapsed. Still holding conditions."
	}
	c.Text(rect(r.Min.X+14, r.Min.Y+40, r.Max.X-14, r.Min.Y+68), sum, Style{Size: 22, Color: colDim, MinSize: 15})
	vr := rect(r.Min.X+14, r.Min.Y+72, r.Max.X-14, r.Max.Y-4)
	c.Text(rect(vr.Min.X, vr.Min.Y, vr.Max.X, vr.Min.Y+30), "VERIFY BEFORE USE", Style{Font: FontBold, Size: 24, Color: colText})
	verify := pr.Verify
	if verify == "" {
		verify = "Verify the product by your own validated process."
	}
	c.Wrap(rect(vr.Min.X, vr.Min.Y+30, vr.Max.X, vr.Max.Y), verify, Style{Size: 24, Color: colText, MinSize: 15, V: Top}, 6)
	if p.Stalled() {
		c.Wrap(rect(mX, bot+4, mR, bodyBot), "NOT LIVE: last known state. The controller is not responding.", Style{Font: FontBold, Size: 26, Color: colWarn, H: Center, MinSize: 16}, 2)
		return
	}
	if p.locked {
		p.lockedNotice(c, "Locked: ending the cure is disabled.")
		return
	}
	end := rect(mR-400, bot+4, mR, bodyBot)
	p.button(c, end, "cure.end", "END RECIPE…", btnNormal, func() {
		p.ask(dialog{id: "end", title: "End this cure?", body: "The cabinet stops following " + pr.RecipeName + " and keeps holding the current setpoints by hand. The panel does not judge the product: only your verification does.",
			confirm: "HOLD 2 s: END CURE", cancel: "KEEP HOLDING", hold: p.cfg.ConfirmHold, danger: true, fn: func() {
				p.command(api.Command{Kind: api.CmdStop}, "Cure ended.")
			}})
	})
}
