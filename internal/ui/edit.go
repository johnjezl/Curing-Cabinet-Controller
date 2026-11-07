package ui

import (
	"fmt"
	"image"
	"math"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// field is one adjustable number in an editor.
type field struct {
	label, unit string
	step        float64
	min, max    float64
	dec         int
}

// editor holds pending values. Nothing reaches the controller until SAVE.
type editor struct {
	title     string
	fields    []field
	vals      []float64
	orig      []float64
	page      int
	perPage   int
	installer bool
	commit    func(vals []float64) (api.Command, string)
}

func (e *editor) changed() bool {
	for i := range e.vals {
		if e.vals[i] != e.orig[i] {
			return true
		}
	}
	return false
}

func newEditor(title string, fields []field, vals []float64, perPage int, installer bool, commit func([]float64) (api.Command, string)) *editor {
	clean := make([]float64, len(vals))
	for i, v := range vals {
		if !finite(v) {
			v = fields[i].min
		}
		clean[i] = v
	}
	return &editor{title: title, fields: fields, vals: clean, orig: append([]float64(nil), clean...), perPage: perPage, installer: installer, commit: commit}
}

func (e *editor) bump(i int, dir float64) {
	f := e.fields[i]
	v := e.vals[i] + dir*f.step
	v = math.Round(v/f.step) * f.step // land on the step grid
	v = math.Round(v*1000) / 1000
	if v < f.min {
		v = f.min
	}
	if v > f.max {
		v = f.max
	}
	e.vals[i] = v
}

// Setpoint adjust ranges. The core validates as well; these keep a slip small.
var setpointFields = []field{
	{"Temperature", "°C", 0.5, 0, 35, 1},
	{"Humidity", "%", 1, 40, 95, 0},
	{"Airflow", "m/s", 0.05, 0, 5, 2},
}

func (p *Panel) beginSetpoints() {
	sp := p.snap.Setpoint
	p.edit = newEditor("SETPOINTS", setpointFields, []float64{sp.TempC, sp.RHPct, sp.AirMS}, 3, false,
		func(v []float64) (api.Command, string) {
			return api.Command{Kind: api.CmdSetSetpoint, Setpoint: &api.Setpoints{TempC: v[0], RHPct: v[1], AirMS: v[2]}}, "Setpoints saved."
		})
}

func (p *Panel) drawSetpoints(c *Canvas) {
	if p.edit == nil {
		p.beginSetpoints()
	}
	e := p.edit
	if !e.changed() { // follow the live setpoint (a recipe may move it)
		sp := p.snap.Setpoint
		if finite(sp.TempC) && finite(sp.RHPct) && finite(sp.AirMS) {
			e.vals = []float64{sp.TempC, sp.RHPct, sp.AirMS}
			e.orig = append([]float64(nil), e.vals...)
		}
	}
	act := []string{p.liveText(p.snap.Temp, 1, "°C"), p.liveText(p.snap.RH, 0, "%"), p.liveText(p.snap.Air, 2, "m/s")}
	stalled := p.Stalled()
	bot := bodyBot
	if p.locked || stalled {
		bot = bodyBot - 101 // every setpoint stays in view above the notice
	}
	// During a recipe a change is an override for this phase only (ADR 0112): each
	// row says what the recipe asks for, and SAVE goes through a confirmation.
	if rs := p.recipeSetpoint(); rs != nil {
		for i, v := range []float64{rs.TempC, rs.RHPct, rs.AirMS} {
			act[i] += ", recipe " + fmtFloat(v, setpointFields[i].dec)
		}
	}
	h := (bot - bodyTop - 2*4) / 3
	for i := range e.fields {
		y := bodyTop + i*(h+4)
		p.drawFieldRow(c, rect(mX, y, mR, y+h), e, i, act[i], 54, !stalled)
	}
	if e.changed() {
		p.drawSaveBar(c, e)
	}
	if p.locked {
		// Locked view keeps every value visible; only the unlock gesture is live.
		p.lockedNotice(c, "Setpoints are view only while locked.")
	} else if stalled {
		msg := "NOT LIVE: these are the last setpoints received. Changes cannot be sent."
		r := rect(mX, bodyBot-97, mR, bodyBot)
		c.Round(r, 6, colPanel2)
		c.Wrap(r.Inset(8), msg, Style{Font: FontBold, Size: 24, Color: colText, H: Center, MinSize: 16}, 2)
	}
}

func (p *Panel) liveText(r api.Reading, dec int, unit string) string {
	if p.Stalled() {
		return "now NOT LIVE"
	}
	if readingGood(r) {
		return "now " + fmtFloat(r.Value, dec) + " " + unit
	}
	if r.Quality == api.QualityOK {
		return "now " + qualityWord(api.QualityFault) // a non-finite value marked ok
	}
	return "now " + qualityWord(r.Quality)
}

// drawFieldRow: label, minus, value, plus. The buttons sit at opposite ends so a
// slip lands on neither commit nor the opposite direction.
func (p *Panel) drawFieldRow(c *Canvas, r image.Rectangle, e *editor, i int, sub string, digit int, editable bool) {
	c.Round(r, 6, colPanel)
	f := e.fields[i]
	bw := 130
	labelW := 208
	in := r.Inset(3)
	lab := rect(r.Min.X+10, r.Min.Y+2, r.Min.X+labelW, r.Max.Y-2)
	if sub != "" {
		c.Text(rect(lab.Min.X, lab.Min.Y+6, lab.Max.X, lab.Min.Y+lab.Dy()/2+2), f.label, Style{Font: FontBold, Size: 28, Color: colText, MinSize: 16})
		c.Text(rect(lab.Min.X, lab.Min.Y+lab.Dy()/2+2, lab.Max.X, lab.Max.Y-6), sub, Style{Size: 23, Color: colDim, MinSize: 15})
	} else {
		c.Wrap(lab, f.label, Style{Font: FontBold, Size: 24, Color: colText, MinSize: 15}, 2)
	}
	if r.Dy() < 80 {
		// Compact view-only row: same layout, smaller digits, no buttons.
		digit = digit * 3 / 4
	}
	minus := rect(r.Min.X+labelW+4, in.Min.Y, r.Min.X+labelW+4+bw, in.Max.Y)
	plus := rect(in.Max.X-bw, in.Min.Y, in.Max.X, in.Max.Y)
	val := rect(minus.Max.X+6, in.Min.Y, plus.Min.X-6, in.Max.Y)
	col := colText
	changed := e.vals[i] != e.orig[i]
	if changed {
		c.Round(val, 6, colOn)
		col = colBlack
		ink := c.Digits(rect(val.Min.X+4, val.Min.Y, val.Max.X-4, val.Min.Y+val.Dy()*2/3), fmtFloat(e.vals[i], f.dec)+" "+f.unit, digit*2/3, col, Center, "")
		_ = ink
		c.Text(rect(val.Min.X+4, val.Min.Y+val.Dy()*2/3, val.Max.X-4, val.Max.Y-2), "was "+fmtFloat(e.orig[i], f.dec)+" (not saved)", Style{Font: FontBold, Size: 19, Color: colBlack, H: Center, MinSize: 13})
	} else {
		c.Digits(val.Inset(2), fmtFloat(e.vals[i], f.dec)+" "+f.unit, digit*2/3, col, Center, "")
	}
	if p.locked || !editable {
		return
	}
	ii := i
	st := btnStyle{colBtn, colText, 54}
	p.button(c, minus, fmt.Sprintf("edit.minus.%d", i), "−", st, func() { e.bump(ii, -1) })
	p.button(c, plus, fmt.Sprintf("edit.plus.%d", i), "+", st, func() { e.bump(ii, 1) })
}

// drawSaveBar replaces the navigation while a change is pending, so the only
// ways out are explicit. CANCEL and SAVE are far apart.
func (p *Panel) drawSaveBar(c *Canvas, e *editor) {
	cancel := rect(mX, navTop, mX+300, navBot)
	save := rect(mR-300, navTop, mR, navBot)
	p.button(c, cancel, "edit.cancel", "CANCEL", btnNormal, func() {
		copy(e.vals, e.orig)
		p.flash("Cancelled. Nothing changed.", false, 3*time.Second)
	})
	overriding := !e.installer && p.recipeSetpoint() != nil
	saveLabel := "SAVE"
	if overriding {
		saveLabel = "OVERRIDE…"
	}
	p.button(c, save, "edit.save", saveLabel, btnPrimary, func() {
		cmd, msg := e.commit(e.vals)
		if overriding {
			p.askOverride(e, cmd)
			return
		}
		p.command(cmd, msg)
		copy(e.orig, e.vals)
	})
	mid := rect(cancel.Max.X+8, navTop, save.Min.X-8, navBot)
	idle := p.now().Sub(p.lastTouch)
	left := p.cfg.MenuTimeout - idle
	if e.installer {
		left = 2*p.cfg.MenuTimeout - idle
	}
	note := "Not saved yet"
	if overriding {
		note = "This phase only"
	}
	c.Wrap(mid, fmt.Sprintf("%s\nDiscarded in %.0f s", note, math.Max(0, left.Seconds())), Style{Font: FontBold, Size: 19, Color: colWarn, H: Center, MinSize: 14}, 2)
}

// recipeSetpoint returns what the current phase asks for while a recipe owns the
// setpoints, nil otherwise.
func (p *Panel) recipeSetpoint() *api.Setpoints {
	pr := p.snap.Program
	switch pr.State {
	case api.RunActive, api.RunPaused, api.RunFinished:
	default:
		return nil
	}
	ph := p.currentPhases()
	if pr.PhaseIndex < 0 || pr.PhaseIndex >= len(ph) {
		return nil
	}
	sp := ph[pr.PhaseIndex].Setpoint
	return &sp
}

func (p *Panel) askOverride(e *editor, cmd api.Command) {
	rs := p.recipeSetpoint()
	sp := cmd.Setpoint
	body := fmt.Sprintf("The cabinet will stop following the recipe for this phase only: %s °C, %s %%, %s m/s instead of the recipe's %s °C, %s %%, %s m/s. Phase time and start weight do not change. This is written to the record, and it ends when the phase ends.",
		fmtFloat(sp.TempC, 1), fmtFloat(sp.RHPct, 0), fmtFloat(sp.AirMS, 2), fmtFloat(rs.TempC, 1), fmtFloat(rs.RHPct, 0), fmtFloat(rs.AirMS, 2))
	p.ask(dialog{id: "override", title: "Override the recipe?", body: body, confirm: "HOLD 2 s: OVERRIDE", cancel: "KEEP EDITING", hold: p.cfg.ConfirmHold, danger: true, fn: func() {
		p.command(cmd, "Override on: not following the recipe this phase.")
		copy(e.orig, e.vals)
	}})
}

func (p *Panel) lockedNotice(c *Canvas, msg string) {
	r := rect(mX, bodyBot-97, mR, bodyBot)
	c.Round(r, 6, colPanel2)
	c.Wrap(rect(r.Min.X+10, r.Min.Y+4, r.Max.X-300, r.Max.Y-4), msg, Style{Font: FontBold, Size: 24, Color: colText, MinSize: 15}, 2)
	p.holdButton(c, rect(r.Max.X-290, r.Min.Y+4, r.Max.X-4, r.Max.Y-4), "lock.unlock", "HOLD 3 s TO UNLOCK", p.cfg.UnlockHold, btnStyle{colOn, colBlack, 24}, p.unlock)
}

func (p *Panel) unlock() {
	p.locked = false
	p.save()
	p.flash("Panel unlocked.", false, 3*time.Second)
}

func (p *Panel) lockPanel() {
	p.locked = true
	p.edit = nil
	p.save()
	p.flash("Panel locked. Readings and setpoints stay visible; silence stays available.", false, 4*time.Second)
}

// drawEdit is the installer's multi-page editor.
func (p *Panel) drawEdit(c *Canvas) {
	e := p.edit
	if e == nil {
		p.go_(scrInstaller)
		return
	}
	pages := (len(e.fields) + e.perPage - 1) / e.perPage
	h := (bodyBot - bodyTop - (e.perPage-1)*4) / e.perPage
	listR := mR
	if pages > 1 {
		listR = mR - 94
	}
	for i := 0; i < e.perPage; i++ {
		fi := e.page*e.perPage + i
		if fi >= len(e.fields) {
			break
		}
		y := bodyTop + i*(h+4)
		p.drawFieldRow(c, rect(mX, y, listR, y+h), e, fi, "", 54, true)
	}
	if pages > 1 {
		p.pager(c, listR+4, bodyTop, mR, bodyBot, e.page, pages, "edit.page", func(n int) { e.page = n })
	}
	if e.changed() {
		p.drawSaveBar(c, e)
	}
}

// pager draws previous and next page buttons in a column.
func (p *Panel) pager(c *Canvas, x0, y0, x1, y1, page, pages int, id string, set func(int)) {
	mid := (y0 + y1) / 2
	label := fmt.Sprintf("%d/%d", page+1, pages)
	up := rect(x0, y0, x1, mid-14)
	dn := rect(x0, mid+14, x1, y1)
	c.Text(rect(x0, mid-14, x1, mid+14), label, Style{Font: FontBold, Size: 22, Color: colText, H: Center})
	st := btnStyle{colBtn, colText, 34}
	if page > 0 {
		p.button(c, up, id+".prev", "▲", st, func() { set(page - 1) })
	}
	if page < pages-1 {
		p.button(c, dn, id+".next", "▼", st, func() { set(page + 1) })
	}
}
