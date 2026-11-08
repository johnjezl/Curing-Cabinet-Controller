package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// drawHome is the across-the-room view.
func (p *Panel) drawHome(c *Canvas, st Status) {
	s := &p.snap
	big := st.Detail != ""
	_, _, note := blindNote(s, p.Stalled())
	holds := len(s.Outputs.Holds) > 0 || note != "" || p.whyIdle() != ""
	var aTop, aBot, bTop, bBot, cTop, cBot, digit, medDigit int
	// In simulation the across-the-room view starts with a full-width band (ADR 0406).
	// The space comes out of the air and weight row, so the temperature and humidity
	// digits keep their height, except with a banner and a hold both showing.
	off := 0
	if s.Health.Simulated {
		off = simBandH
		p.drawSimBand(c, rect(mX, headTop, mR, headTop+simBandH-4))
	}
	switch {
	case big && off > 0:
		p.drawBigBanner(c, st, off, 98)
		aTop, aBot, digit = 132, 279, 104
		bTop, bBot, cTop, cBot, medDigit = 282, 347, 350, 382, 30
		if holds {
			aBot, digit = 262, 84
			bTop, bBot, cTop, medDigit = 265, 318, 321, 20
		}
	case big:
		p.drawBigBanner(c, st, 0, 98)
		aTop, aBot, digit = 108, 255, 104
		bTop, bBot, cTop, cBot, medDigit = 258, 347, 350, 382, 40
		if holds {
			bBot, cTop, medDigit = 318, 321, 30
		}
	case off > 0:
		p.drawHeaderIn(c, st, "", rect(mX, headTop+off, mR, headBot+off))
		aTop, aBot, digit = 84, 231, 104
		bTop, bBot, cTop, cBot, medDigit = 235, 315, 319, 382, 38
	default:
		p.drawHeader(c, st, "")
		aTop, aBot, digit = 60, 222, 118
		bTop, bBot, cTop, cBot, medDigit = 226, 315, 319, 382, 46
	}
	stale := p.Stalled()
	tSet := "SET " + fmtFloat(s.Setpoint.TempC, 1) + " °C"
	rSet := "SET " + fmtFloat(s.Setpoint.RHPct, 0) + " %"
	if s.Program.Override != nil {
		tSet = "OVERRIDE " + fmtFloat(s.Setpoint.TempC, 1) + " °C"
		rSet = "OVERRIDE " + fmtFloat(s.Setpoint.RHPct, 0) + " %"
	}
	tDev := bandDelta(s.Temp, s.Setpoint.TempC, s.Limits.TempHighDelta, s.Limits.TempLowDelta, 1, false)
	rDev := bandDelta(s.RH, s.Setpoint.RHPct, s.Limits.RHHighDelta, s.Limits.RHLowDelta, 0, false)
	tileW := (mR - mX - 4) / 2
	tR := rect(mX, aTop, mX+tileW, aBot)
	rR := rect(mX+tileW+4, aTop, mR, aBot)
	p.bigTile(c, tR, "TEMPERATURE", tSet, s.Temp, 1, "°C", digit, tDev, stale, "home.temp")
	p.bigTile(c, rR, "HUMIDITY", rSet, s.RH, 0, "%", digit, rDev, stale, "home.rh")
	p.addHit(tR, "home.temp", func() { p.navTo(scrSet) })
	p.addHit(rR, "home.rh", func() { p.navTo(scrSet) })

	aW := 196
	aR := rect(mX, bTop, mX+aW, bBot)
	wR := rect(mX+aW+4, bTop, mX+2*aW+4, bBot)
	cR := rect(mX+2*aW+8, bTop, mR, bBot)
	aSet := "SET " + fmtFloat(s.Setpoint.AirMS, 2)
	if s.Program.Override != nil {
		aSet = "OVR " + fmtFloat(s.Setpoint.AirMS, 2)
	}
	p.medTile(c, aR, "AIR", aSet, s.Air, 2, "m/s", medDigit, stale, "home.air")
	p.medTile(c, wR, "WEIGHT", p.weightSub(), s.Weight, 0, "g", medDigit, stale, "home.weight")
	p.drawCureCard(c, cR)
	// In the tightest layout these are too short to be fair targets, so they are
	// not targets at all; the navigation row reaches the same places.
	if aR.Dy() >= MinTapPx {
		p.addHit(aR, "home.air", func() { p.navTo(scrSet) })
		p.addHit(wR, "home.weight", func() { p.go_(scrTrend) })
		p.addHit(cR, "home.cure", func() { p.go_(scrCure) })
	}
	p.drawOutputs(c, rect(mX, cTop, mR, cBot), holds)
}

// bandDelta returns the signed distance from setpoint text when outside the
// alarm band, else "". A reading that is not good returns nothing: it has its
// own treatment.
func bandDelta(r api.Reading, set, hi, lo float64, dec int, _ bool) string {
	if !readingGood(r) {
		return ""
	}
	d := r.Value - set
	if (hi > 0 && d > hi) || (lo > 0 && -d > lo) {
		return fmt.Sprintf("%+.*f", dec, d)
	}
	return ""
}

func (p *Panel) weightSub() string {
	pr := p.snap.Program
	if pr.State == api.RunIdle || pr.StartWeightG <= 0 || !finite(pr.StartWeightG) || p.snap.Health.Simulated {
		return ""
	}
	return "from " + fmtWeight(pr.StartWeightG)
}

func (p *Panel) drawBigBanner(c *Canvas, st Status, off, height int) {
	bg, fg := levelColors(st.Level)
	r := rect(mX, headTop+off, mR, headTop+off+height)
	c.Round(r, 6, bg)
	s := &p.snap
	const colW = 146
	textRight := r.Max.X - 8
	sounding := false
	for _, a := range s.Active {
		if a.Severity != api.SevInfo { // info alarms never drive the buzzer
			sounding = true
		}
	}
	if st.Alarm != nil && (sounding || s.BuzzerSilenced) {
		b := rect(r.Max.X-colW-3, r.Min.Y+3, r.Max.X-3, r.Max.Y-3)
		if s.BuzzerSilenced {
			c.Round(b, 6, colBG)
			c.Wrap(b.Inset(6), "BUZZER SILENCED", Style{Font: FontBold, Size: 22, Color: colText, H: Center, MinSize: 16}, 2)
		} else {
			p.button(c, b, "home.silence", "SILENCE BUZZER", btnStyle{colText, colBlack, 23}, func() {
				p.command(api.Command{Kind: api.CmdSilence}, "Buzzer silenced. The alarm stays on screen.")
			})
		}
		textRight = b.Min.X - 10
	}
	tagR := p.drawBannerTags(c, rect(r.Min.X+10, r.Min.Y+2, textRight, r.Min.Y+24), fg)
	head := st.Headline
	if st.Extra > 0 {
		head += fmt.Sprintf(" (+%d MORE)", st.Extra)
	}
	// Whatever text the core supplies has to fit: wrap first, then shrink.
	used := c.Wrap(rect(r.Min.X+10, r.Min.Y+2, tagR-6, r.Min.Y+50), head, Style{Font: FontBold, Size: 29, Color: fg, MinSize: 17, V: Top}, 2)
	if used < 36 {
		used = 36
	}
	det := rect(r.Min.X+10, r.Min.Y+2+used, textRight, r.Max.Y-6)
	c.Wrap(det, st.Detail, Style{Size: 24, Color: fg, MinSize: 14, V: Top}, 3)
	if st.Level >= LevelWarn && st.Alarm != nil && !st.Alarm.Acked {
		// A slow pulse while unacknowledged: a thick bar, never the text.
		if p.now().Unix()%2 == 0 {
			c.Fill(rect(r.Min.X, r.Max.Y-5, textRight, r.Max.Y), fg)
		}
	}
	if st.Screen != p.screen && textRight-r.Min.X > MinTapPx {
		p.addHit(rect(r.Min.X, r.Min.Y, textRight, r.Max.Y), "banner", func() { p.go_(st.Screen) })
	}
}

// drawBannerTags draws clock, lock and simulation marker at the right of r and
// returns where they start.
func (p *Panel) drawBannerTags(c *Canvas, r image.Rectangle, fg color.RGBA) int {
	x := r.Max.X
	clock := p.clockText()
	w := getFace(FontBold, 18).measure(clock) + 2
	c.Text(rect(x-w, r.Min.Y, x, r.Max.Y), clock, Style{Font: FontBold, Size: 18, Color: fg, H: Right})
	x -= w + 4
	tag := func(label string, bg color.RGBA) {
		tw := getFace(FontBold, 15).measure(label) + 10
		b := rect(x-tw, r.Min.Y, x, r.Max.Y)
		c.Round(b, 3, bg)
		c.Text(b, label, Style{Font: FontBold, Size: 15, Color: colText, H: Center})
		x -= tw + 4
	}
	if p.locked {
		tag("LOCKED", colBG)
	}
	if p.snap.Health.Simulated {
		tag("SIM", colSim)
	}
	return x
}

// tileChrome draws the label line shared by reading tiles; returns the value area.
func (p *Panel) tileChrome(c *Canvas, r image.Rectangle, label, sub string, lineH, size int, bg color.RGBA) image.Rectangle {
	c.Round(r, 6, bg)
	line := rect(r.Min.X+10, r.Min.Y+3, r.Max.X-8, r.Min.Y+3+lineH)
	if p.snap.Health.Simulated {
		tag := rect(line.Max.X-44, line.Min.Y+3, line.Max.X, line.Max.Y-2)
		c.Round(tag, 3, colSim)
		c.Text(tag, "SIM", Style{Font: FontBold, Size: size - 6, Color: colText, H: Center, MinSize: 11})
		line.Max.X = tag.Min.X - 6
	}
	lw := getFace(FontBold, size).measure(label)
	c.Text(rect(line.Min.X, line.Min.Y, line.Min.X+lw+2, line.Max.Y), label, Style{Font: FontBold, Size: size, Color: colDim, MinSize: size - 3})
	if sub != "" {
		col := colText
		box := rect(line.Min.X+lw+8, line.Min.Y, line.Max.X, line.Max.Y)
		if strings.HasPrefix(sub, "OVR") || strings.HasPrefix(sub, "OVERRIDE") {
			// Not the recipe's value: it must read differently at a glance.
			c.Round(box, 3, colWarn)
			col = colBlack
			box = box.Inset(2)
		}
		c.Text(box, sub, Style{Font: FontBold, Size: size, Color: col, H: Right, MinSize: 13})
	}
	return rect(r.Min.X+8, line.Max.Y, r.Max.X-8, r.Max.Y-4)
}

// bigTile is a temperature or humidity tile. Only a good, fresh reading gets big
// white digits. Everything else is a different picture.
func (p *Panel) bigTile(c *Canvas, r image.Rectangle, label, sub string, rd api.Reading, dec int, unit string, digit int, dev string, stalled bool, id string) {
	bad := !readingGood(rd) || stalled
	bg := colPanel
	if p.isPressed(id) {
		bg = colPanel2
	}
	v := p.tileChrome(c, r, label, sub, 34, 24, bg)
	if bad {
		p.badReading(c, r, v, rd, dec, unit, stalled, true)
		return
	}
	col := colText
	devCol := colWarn
	if dev != "" {
		col = colWarn
		// Digit colour ranks like the alarm: drift is amber, critical is red.
		if p.worstAlarmLevel() == LevelCrit && len(p.snap.Active) > 0 {
			col, devCol = colCritText, colCritText
		}
	}
	numR := rect(v.Min.X, v.Min.Y, v.Max.X-78, v.Max.Y)
	ink := c.Digits(numR, fmtFloat(rd.Value, dec), digit, col, Right, id)
	uR := rect(ink.Max.X+8, ink.Min.Y-2, v.Max.X, ink.Min.Y+40)
	c.Text(uR, unit, Style{Font: FontBold, Size: 36, Color: col, V: Top, MinSize: 24})
	if dev != "" {
		c.Text(rect(ink.Max.X+2, ink.Max.Y-40, v.Max.X, ink.Max.Y+2), dev, Style{Font: FontBold, Size: 30, Color: devCol, H: Center, MinSize: 18})
	}
}

// badReading draws a reading that must not be believed: hatched, with its state
// as the largest word and any last value small.
func (p *Panel) badReading(c *Canvas, tile, v image.Rectangle, rd api.Reading, dec int, unit string, stalled, large bool) {
	q := rd.Quality
	if stalled && q != api.QualityFault && q != api.QualityOff {
		q = api.QualityStale
	}
	if q == api.QualityOK { // non-finite value marked ok
		q = api.QualityFault
	}
	if q == api.QualityOff {
		size := 22
		if large {
			size = 34
		}
		c.Text(v, "NOT FITTED", Style{Font: FontBold, Size: size, Color: colMuted, H: Center, MinSize: 14})
		return
	}
	c.Hatch(v, 16, 6, colHatch)
	word := qualityWord(q)
	wordCol, plate := colWarn, colBlack
	if q == api.QualityFault {
		wordCol, plate = colText, colCrit
	}
	sub := ""
	switch q {
	case api.QualityStale:
		if finite(rd.Value) {
			sub = "last " + fmtFloat(rd.Value, dec) + " " + unit
		}
		age := rd.AgeSec
		if sub != "" {
			sub += ", " + fmtAge(age) + " ago"
		} else {
			sub = fmtAge(age) + " ago"
		}
		if stalled && rd.Quality == api.QualityOK {
			sub = "last " + fmtFloat(rd.Value, dec) + " " + unit + ", not live"
		}
	case api.QualityFault:
		sub = "no valid reading"
		if rd.Note != "" {
			sub = rd.Note
		}
	case api.QualitySuspect:
		sub = "reads " + fmtFloat(rd.Value, dec) + " " + unit + "?"
		if rd.Note != "" {
			sub += " " + rd.Note
		}
	}
	if large {
		wr := rect(v.Min.X+30, v.Min.Y+8, v.Max.X-30, v.Min.Y+v.Dy()*6/10)
		c.Round(wr, 6, plate)
		c.Text(wr, word, Style{Font: FontBold, Size: 54, Color: wordCol, H: Center, MinSize: 28})
		sr := rect(v.Min.X+8, wr.Max.Y+4, v.Max.X-8, v.Max.Y-2)
		c.Round(sr, 4, colBG)
		c.Wrap(sr.Inset(2), sub, Style{Font: FontBold, Size: 24, Color: colText, H: Center, MinSize: 14}, 2)
		return
	}
	if v.Dy() < 36 { // the tightest layout: the state word is what matters
		c.Round(v, 4, plate)
		c.Text(v, word, Style{Font: FontBold, Size: v.Dy() - 4, Color: wordCol, H: Center, MinSize: 14})
		return
	}
	h := v.Dy() * 55 / 100
	wr := rect(v.Min.X, v.Min.Y+1, v.Max.X, v.Min.Y+h)
	c.Round(wr, 4, plate)
	c.Text(wr, word, Style{Font: FontBold, Size: h - 4, Color: wordCol, H: Center, MinSize: 14})
	sr := rect(v.Min.X, wr.Max.Y+1, v.Max.X, v.Max.Y)
	c.Round(sr, 3, colBG)
	c.Text(sr, sub, Style{Size: 16, Color: colText, H: Center, MinSize: 10})
}

func (p *Panel) medTile(c *Canvas, r image.Rectangle, label, sub string, rd api.Reading, dec int, unit string, digit int, stalled bool, id string) {
	bg := colPanel
	if p.isPressed(id) {
		bg = colPanel2
	}
	v := p.tileChrome(c, r, label, sub, 26, 18, bg)
	if !readingGood(rd) || stalled {
		p.badReading(c, r, v, rd, dec, unit, stalled, false)
		return
	}
	uw := 42
	ink := c.Digits(rect(v.Min.X, v.Min.Y, v.Max.X-uw, v.Max.Y), fmtFloat(rd.Value, dec), digit, colText, Right, id)
	c.Text(rect(ink.Max.X+4, ink.Max.Y-22, v.Max.X, ink.Max.Y+3), unit, Style{Font: FontBold, Size: 20, Color: colDim, MinSize: 14})
}

func (p *Panel) drawCureCard(c *Canvas, r image.Rectangle) {
	bg := colPanel
	if p.isPressed("home.cure") {
		bg = colPanel2
	}
	c.Round(r, 6, bg)
	pr := p.snap.Program
	in := r.Inset(8)
	lh, size := 28, 24
	if r.Dy() < 90 {
		lh, size = 22, 20
		in = r.Inset(5)
	}
	switch pr.State {
	case api.RunIdle, "":
		c.Text(rect(in.Min.X, in.Min.Y, in.Max.X, in.Min.Y+lh+4), "MANUAL: no cure running", Style{Font: FontBold, Size: size, Color: colText, MinSize: 16})
		hint := "Tap to choose a recipe"
		if p.locked {
			hint = "Unlock in More to choose a recipe"
		}
		c.Text(rect(in.Min.X, in.Min.Y+lh+6, in.Max.X, in.Min.Y+2*lh+6), hint, Style{Size: size - 3, Color: colDim, MinSize: 15})
		return
	}
	head := fmt.Sprintf("%s  %d/%d %s", pr.RecipeName, pr.PhaseIndex+1, pr.PhaseCount, pr.PhaseName)
	c.Text(rect(in.Min.X, in.Min.Y-2, in.Max.X, in.Min.Y+lh-2), head, Style{Font: FontBold, Size: size - 3, Color: colDim, MinSize: 14})
	var line string
	col := colText
	switch pr.State {
	case api.RunFinished:
		line, col = "ENDED: VERIFY BEFORE USE", colWarn
	case api.RunPaused:
		line, col = "PAUSED: clock stopped", colWarn
	default:
		if pr.PhaseRemaining < 0 {
			line = "until weight target or operator"
		} else {
			line = fmtDur(pr.PhaseRemaining) + " left in phase"
		}
	}
	barFits := r.Dy() >= 76
	if !barFits && readingGood(p.snap.Weight) {
		line += ", loss " + fmtFloat(pr.WeightLossPct, 1) + " %"
	}
	if pr.Override != nil && pr.State != api.RunFinished {
		line = "OVERRIDE this phase, " + line
		col = colWarn
	}
	c.Text(rect(in.Min.X, in.Min.Y+lh-3, in.Max.X, in.Min.Y+2*lh-3), line, Style{Font: FontBold, Size: size, Color: col, MinSize: 14})
	if barFits {
		p.lossBar(c, rect(in.Min.X, in.Min.Y+2*lh-1, in.Max.X, in.Max.Y+1))
	}
}

// lossBar shows weight loss against the target. If the scale is not good the
// figure is labelled as out of date.
func (p *Panel) lossBar(c *Canvas, r image.Rectangle) {
	pr := p.snap.Program
	stalled := p.Stalled()
	good := readingGood(p.snap.Weight) && !stalled
	txt := fmt.Sprintf("LOSS %s %%", fmtFloat(pr.WeightLossPct, 1))
	if pr.TargetLossPct > 0 { // NaN compares false
		txt += ", target " + fmtFloat(pr.TargetLossPct, 0) + " %"
	}
	if !good {
		txt = "LOSS not current: scale " + strings.ToLower(qualityWord(p.snap.Weight.Quality))
		if stalled {
			txt = "LOSS not current: no data"
		}
		c.Round(r, 3, colPanel2)
		c.Hatch(r, 16, 6, colHatch)
		pl := r
		c.Round(pl, 3, colBG)
		c.Text(pl.Inset(2), txt, Style{Font: FontBold, Size: 18, Color: colWarn, H: Center, MinSize: 13})
		return
	}
	c.Round(r, 3, colPanel2)
	if pr.TargetLossPct > 0 && finite(pr.WeightLossPct) && pr.WeightLossPct > 0 {
		f := pr.WeightLossPct / pr.TargetLossPct
		if f > 1 {
			f = 1
		}
		fill := r
		fill.Max.X = r.Min.X + int(float64(r.Dx())*f)
		c.Round(fill, 3, colInfo)
	}
	// Text stays legible where the fill passes under it.
	c.Text(r.Inset(2), txt, Style{Font: FontBold, Size: 19, Color: colText, H: Center, MinSize: 14})
}

// drawOutputs shows what the cabinet is doing right now and any protection hold.
func (p *Panel) drawOutputs(c *Canvas, r image.Rectangle, holds bool) {
	o := p.snap.Outputs
	tempBlind, rhBlind, note := blindNote(&p.snap, p.Stalled())
	if w := p.whyIdle(); w != "" {
		if note != "" {
			note += "  |  "
		}
		note += w
	}
	if p.Stalled() {
		// With no fresh data the panel cannot know what the outputs are doing.
		c.Round(r, 4, colPanel)
		c.Hatch(r, 16, 6, colHatch)
		c.Round(r.Inset(4), 3, colBG)
		c.Text(r.Inset(6), "OUTPUTS UNKNOWN: no fresh data", Style{Font: FontBold, Size: 24, Color: colWarn, H: Center, MinSize: 14})
		return
	}
	chipR := r
	if holds {
		chipR.Max.Y = r.Min.Y + r.Dy()/2 - 1
		p.drawHolds(c, rect(r.Min.X, chipR.Max.Y+2, r.Max.X, r.Max.Y), note)
	}
	type chip struct {
		label string
		on    bool
		hold  bool
		blind bool
	}
	held := func(names ...string) bool {
		for _, h := range o.Holds {
			l := strings.ToLower(h.Output)
			for _, n := range names {
				if strings.Contains(l, n) {
					return true
				}
			}
		}
		return false
	}
	chips := []chip{
		{"COOL", o.Cool, held("cool", "compressor") || p.statusKind("cool") == "hold", tempBlind},
		{"HEAT", o.Heat, held("heat") || p.statusKind("heat") == "hold", tempBlind},
		{"HUMID", o.Humid, held("humid") || p.statusKind("humid") == "hold", rhBlind},
		{"DEHUM", o.Dehumid, held("dehum"), rhBlind},
		{"DEFROST", o.Defrost, held("defrost"), false},
	}
	fw := 168
	w := (r.Dx() - fw - 5*4) / 5
	size := 20
	if chipR.Dy() < 30 {
		size = 16
	}
	for i, ch := range chips {
		x := r.Min.X + i*(w+4)
		b := rect(x, chipR.Min.Y, x+w, chipR.Max.Y)
		switch {
		case !ch.on && p.statusKind(strings.ToLower(ch.label)) == "fault":
			c.Round(b, 4, colCrit)
			c.Text(b.Inset(2), ch.label+" FAULT", Style{Font: FontBold, Size: size, Color: colText, H: Center, MinSize: 12})
		case ch.hold:
			c.Round(b, 4, colWarn)
			c.Text(b.Inset(2), ch.label+" HOLD", Style{Font: FontBold, Size: size, Color: colBlack, H: Center, MinSize: 12})
		case ch.on && ch.blind:
			c.Round(b, 4, colWarn)
			c.Text(b.Inset(2), ch.label+" ON !", Style{Font: FontBold, Size: size, Color: colBlack, H: Center, MinSize: 12})
		case ch.on && p.power == PowerDim:
			// Large light fills would be the brightest thing on a dim screen.
			c.Round(b, 4, colPanel2)
			c.Text(b.Inset(2), ch.label+" ON", Style{Font: FontBold, Size: size, Color: colText, H: Center, MinSize: 12})
		case ch.on:
			c.Round(b, 4, colOn)
			c.Text(b.Inset(2), ch.label+" ON", Style{Font: FontBold, Size: size, Color: colBlack, H: Center, MinSize: 12})
		default:
			c.Round(b, 4, colPanel)
			c.Text(b.Inset(2), ch.label+" off", Style{Size: size, Color: colMuted, H: Center, MinSize: 12})
		}
	}
	x := r.Max.X - fw
	b := rect(x, chipR.Min.Y, r.Max.X, chipR.Max.Y)
	fanOn := o.FanDutyPct > 0
	txt := "FAN off"
	bg, fg := colPanel, colMuted
	if fanOn {
		txt = "FAN " + fmtFloat(o.FanDutyPct, 0) + "% " + fmtFloat(o.FanRPM, 0) + " rpm"
		bg, fg = colOn, colBlack
		if p.power == PowerDim {
			bg, fg = colPanel2, colText
		}
		if o.FanRPM <= 0 {
			bg, fg = colWarn, colBlack
		}
	}
	c.Round(b, 4, bg)
	c.Text(b.Inset(2), txt, Style{Font: FontBold, Size: size - 1, Color: fg, H: Center, MinSize: 12})
}

func (p *Panel) statusKind(out string) string {
	for _, o := range p.snap.Outputs.Status {
		if o.Output == out {
			return o.Kind
		}
	}
	return ""
}

// whyIdle answers the question an operator asks when the reading is off setpoint
// and the output that would fix it is off: the core's reason, in its words. An
// output in hold or fault says so in its chip and in the holds line already.
func (p *Panel) whyIdle() string {
	s := &p.snap
	if p.Stalled() || !readingGood(s.Temp) || len(s.Outputs.Holds) > 0 {
		return ""
	}
	d := s.Temp.Value - s.Setpoint.TempC
	var want string
	switch {
	case d > 1 && !s.Outputs.Cool:
		want = "cool"
	case d < -1 && !s.Outputs.Heat:
		want = "heat"
	default:
		return ""
	}
	for _, o := range s.Outputs.Status {
		if o.Output == want && !o.On {
			return strings.ToUpper(want) + " off: " + o.Reason
		}
	}
	return ""
}

func (p *Panel) drawHolds(c *Canvas, r image.Rectangle, note string) {
	holds := p.snap.Outputs.Holds
	c.Round(r, 4, colBG)
	c.Stroke(r, 4, 2, colWarn, colBG)
	parts := make([]string, 0, len(holds)+1)
	if note != "" {
		parts = append(parts, note)
	}
	// The core may hold several outputs for one reason, and a reason with no time
	// left to run is a state, not a countdown.
	type group struct {
		outs, reason string
		sec          float64
	}
	var groups []group
	for _, h := range holds {
		sec := h.RemainingSec
		if !finite(sec) || sec < 0 {
			sec = 0
		}
		merged := false
		for i := range groups {
			if groups[i].reason == h.Reason && groups[i].sec == sec {
				groups[i].outs += ", " + strings.ToUpper(h.Output)
				merged = true
			}
		}
		if !merged {
			groups = append(groups, group{strings.ToUpper(h.Output), h.Reason, sec})
		}
	}
	for _, g := range groups {
		t := fmt.Sprintf("%s HELD: %s", g.outs, g.reason)
		if g.sec > 0 {
			t += ", " + fmtTimer(g.sec) + " left"
		}
		parts = append(parts, t)
	}
	c.Text(r.Inset(3), strings.Join(parts, "  |  "), Style{Font: FontBold, Size: 21, Color: colWarn, H: Center, MinSize: 12})
}

// drawSleep is the low-level view: dark, slowly moving, still honest and still
// readable. It only appears when nothing needs attention, so it never hides an alarm.
func (p *Panel) drawSleep(c *Canvas) {
	s := &p.snap
	t := p.now().Unix() / 60
	dx := int((t*7)%25) - 12
	dy := int((t*11)%61) - 30
	col := rgb(0xB8, 0xC4, 0xCE)
	txt, rh := "--", "--"
	if readingGood(s.Temp) {
		txt = fmtFloat(s.Temp.Value, 1)
	}
	if readingGood(s.RH) {
		rh = fmtFloat(s.RH.Value, 0)
	}
	if s.Health.Simulated {
		p.drawSimFrame(c)
	}
	top := 120 + dy
	tR := rect(20+dx, top, 440+dx, top+120)
	rR := rect(450+dx, top, 780+dx, top+120)
	ink := c.Digits(rect(tR.Min.X, tR.Min.Y, tR.Max.X-60, tR.Max.Y), txt, 104, col, Right, "sleep.temp")
	c.Text(rect(ink.Max.X+6, ink.Min.Y, tR.Max.X, ink.Min.Y+40), "°C", Style{Font: FontBold, Size: 34, Color: col, V: Top})
	ink = c.Digits(rect(rR.Min.X, rR.Min.Y, rR.Max.X-50, rR.Max.Y), rh, 104, col, Right, "sleep.rh")
	c.Text(rect(ink.Max.X+6, ink.Min.Y, rR.Max.X, ink.Min.Y+40), "%", Style{Font: FontBold, Size: 34, Color: col, V: Top})
	line := fmt.Sprintf("OK: IN RANGE   set %s °C  %s %%", fmtFloat(s.Setpoint.TempC, 1), fmtFloat(s.Setpoint.RHPct, 0))
	c.Text(rect(20+dx, top+128, 780+dx, top+168), line, Style{Font: FontBold, Size: 32, Color: rgb(0x7F, 0xE0, 0xA8), H: Center, MinSize: 20})
	c.Text(rect(20+dx, top+170, 780+dx, top+204), "touch to wake", Style{Size: 26, Color: col, H: Center})
	if s.Health.Simulated {
		c.Text(rect(20+dx, top+206, 780+dx, top+240), "SIMULATED, NOT A REAL CABINET", Style{Font: FontBold, Size: 26, Color: colSimLite, H: Center, MinSize: 16})
	}
}
