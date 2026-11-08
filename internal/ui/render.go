package ui

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// Layout rows shared by every screen. The 6 px outer margin holds the simulated
// plant frame; it stays empty on a real plant so nothing shifts.
const (
	mX      = 6
	mR      = ScreenW - 6
	headTop = 6
	headBot = 56
	navTop  = 386
	navBot  = 475
	bodyTop = 60
	bodyBot = 382
)

// dimScale is the pixel level in dim mode, out of 256. It keeps white digits at
// 6:1 or better; the backlight and the orbit do the luminance reduction (ADR 0305).
const dimScale = 180

// Render draws the current state into the panel's canvas and returns it. The
// returned image is reused by the next call. A panic while drawing never leaves
// this function: it paints a plain fault frame instead.
func (p *Panel) Render() (img *image.RGBA) {
	defer func() {
		if r := recover(); r != nil {
			p.panics++
			p.hits = p.hits[:0]
			p.pressed = nil
			p.dlg = nil
			p.screen = scrHome
			img = p.renderFault(fmt.Sprint(r))
		}
	}()
	p.dirty = false
	p.frames++
	p.canvas.Audit = p.audit
	c := p.canvas
	c.Fill(c.Img.Bounds(), colBG)
	p.hits = p.hits[:0]
	if p.renderHook != nil {
		p.renderHook()
	}
	if p.power == PowerSleep {
		p.drawSleep(c)
		p.drawHeartbeat(c, rgb(0x5A, 0x66, 0x70))
		return p.finish(c)
	}
	st := p.status()
	switch {
	case p.dlg != nil:
		p.drawHeader(c, st, screenTitle(p.screen))
		p.drawDialog(c, st)
	case p.screen == scrHome:
		p.drawHome(c, st)
		p.drawNav(c)
	default:
		title := screenTitle(p.screen)
		if p.screen == scrKeypad && p.keypad != nil && p.keypad.header != "" {
			title = p.keypad.header
		}
		p.drawHeader(c, st, title)
		switch p.screen {
		case scrSet:
			p.drawSetpoints(c)
		case scrCure:
			p.drawCure(c)
		case scrRecipe:
			p.drawRecipe(c)
		case scrAlarms:
			p.drawAlarms(c)
		case scrTrend:
			p.drawTrend(c)
		case scrMore:
			p.drawMore(c)
		case scrHealth:
			p.drawHealth(c)
		case scrKeypad:
			p.drawKeypad(c)
		case scrInstaller:
			p.drawInstaller(c)
		case scrEdit:
			p.drawEdit(c)
		case scrScale:
			p.drawScale(c)
		case scrOutputs:
			p.drawOutputsScreen(c)
		}
		if p.showNav() {
			p.drawNav(c)
		}
	}
	p.drawToast(c)
	if p.snap.Health.Simulated {
		p.drawSimFrame(c)
	}
	if p.power == PowerDim {
		dimImage(c.Img, dimScale)
		orbit(c.Img, p.now())
		p.drawHeartbeat(c, rgb(0x90, 0x98, 0xA0))
		return p.finish(c)
	}
	p.drawHeartbeat(c, colText)
	return p.finish(c)
}

func (p *Panel) finish(c *Canvas) *image.RGBA {
	// A press survives a redraw only if its target is still on screen.
	if p.pressed != nil {
		found := false
		for i := range p.hits {
			if p.hits[i].ID == p.pressed.ID && p.hits[i].R == p.pressed.R {
				f := p.hits[i]
				p.pressed = &f
				found = true
				break
			}
		}
		if !found {
			p.pressed = nil
		}
	}
	return c.Img
}

// drawHeartbeat moves a bar along the bottom edge once a second on every screen,
// from the panel's own clock. It is the sign a person can see that this frame is
// new: a still bar means the panel has stopped, whatever the numbers say.
func (p *Panel) drawHeartbeat(c *Canvas, col color.RGBA) {
	const w, h, steps = 100, 5, 12
	i := p.now().Unix() % (2 * steps)
	if i >= steps {
		i = 2*steps - i - 1 // sweep back, so the edges are not a jump
	}
	x := int(i) * (ScreenW - w) / (steps - 1)
	c.Fill(rect(x, ScreenH-h, x+w, ScreenH), col)
}

func (p *Panel) renderFault(msg string) *image.RGBA {
	c := p.canvas
	c.Audit = nil
	c.Fill(c.Img.Bounds(), colBG)
	c.Fill(rect(mX, headTop, mR, 150), colCrit)
	c.Text(rect(20, 16, 780, 70), "DISPLAY FAULT", Style{Font: FontBold, Size: 44, Color: colText})
	c.Text(rect(20, 76, 780, 140), "Control keeps running. Do not trust this panel; it restarts itself.", Style{Size: 24, Color: colText, MinSize: 16})
	if len(msg) > 80 {
		msg = msg[:80]
	}
	c.Text(rect(20, 170, 780, 210), msg, Style{Size: 20, Color: colDim, MinSize: 14})
	p.drawHeartbeat(c, colText)
	return c.Img
}

func screenTitle(s screenID) string {
	switch s {
	case scrSet:
		return "SETPOINTS"
	case scrCure, scrRecipe:
		return "CURE"
	case scrAlarms:
		return "ALARMS"
	case scrTrend:
		return "TREND"
	case scrMore:
		return "MORE"
	case scrHealth:
		return "HEALTH"
	case scrOutputs:
		return "OUTPUTS"
	case scrKeypad:
		return "ENTRY"
	case scrInstaller, scrEdit, scrScale:
		return "INSTALLER"
	}
	return ""
}

func levelColors(l Level) (bg, fg color.RGBA) {
	switch l {
	case LevelCrit:
		return colCrit, colText
	case LevelWarn:
		return colWarn, colBlack
	case LevelInfo:
		return colInfo, colText
	}
	return colOK, colText
}

// drawHeader is the one-line banner on every screen but alarm-state Home: the
// overall state never leaves the screen, whatever menu is open.
func (p *Panel) drawHeader(c *Canvas, st Status, title string) {
	p.drawHeaderIn(c, st, title, rect(mX, headTop, mR, headBot))
}

// drawHeaderIn draws the header in r, which the home screen moves down in simulation.
func (p *Panel) drawHeaderIn(c *Canvas, st Status, title string, r image.Rectangle) {
	bg, fg := levelColors(st.Level)
	c.Round(r, 6, bg)
	x := r.Min.X + 10
	if title != "" {
		w := getFace(FontBold, 24).measure(title) + 12
		c.Round(rect(x-4, r.Min.Y+5, x+w, r.Max.Y-5), 4, colBG)
		c.Text(rect(x, r.Min.Y+5, x+w-8, r.Max.Y-5), title, Style{Font: FontBold, Size: 24, Color: colText})
		x += w + 8
	}
	right := p.drawHeaderRight(c, r, bg, fg, st.Alarm != nil)
	textR := rect(x, r.Min.Y, right-6, r.Max.Y)
	size := 28
	if title != "" {
		size = 24
	}
	head := st.Headline
	if st.Extra > 0 {
		head += fmt.Sprintf(" (+%d MORE)", st.Extra)
	}
	c.Wrap(textR, head, Style{Font: FontBold, Size: size, Color: fg, MinSize: 14}, 2)
}

// drawHeaderRight draws clock, lock and simulation marker; returns the left edge.
func (p *Panel) drawHeaderRight(c *Canvas, r image.Rectangle, bg, fg color.RGBA, compact bool) int {
	x := r.Max.X - 8
	if p.snap.Health.Simulated {
		// The clock goes inside the simulation pill: it is simulated time.
		x = p.drawSimPill(c, x, r, compact)
		if p.locked {
			x = p.drawLockPill(c, x, r)
		}
		return x
	}
	clock := p.clockText()
	w := getFace(FontBold, 22).measure(clock) + 2
	c.Text(rect(x-w, r.Min.Y, x, r.Max.Y), clock, Style{Font: FontBold, Size: 22, Color: fg, H: Right})
	x -= w + 8
	if p.locked {
		x = p.drawLockPill(c, x, r)
	}
	return x
}

func (p *Panel) clockText() string {
	if p.snap.Now.IsZero() {
		return "--:--:--"
	}
	s := p.snap.Now.In(p.cfg.Location).Format("15:04:05")
	if !p.snap.Health.ClockTrusted {
		s = "~" + s + "?"
	}
	return s
}

func (p *Panel) drawLockPill(c *Canvas, x int, r image.Rectangle) int {
	w := 96
	box := rect(x-w, r.Min.Y+7, x, r.Max.Y-7)
	c.Round(box, 4, colBG)
	c.Text(box, "LOCKED", Style{Font: FontBold, Size: 20, Color: colText, H: Center, MinSize: 16})
	return x - w - 8
}

func (p *Panel) drawSimPill(c *Canvas, x int, r image.Rectangle, compact bool) int {
	label := "SIMULATED"
	if compact {
		label = "SIM"
	}
	if f := p.snap.Health.SpeedFactor; finite(f) && f > 1 {
		label += fmt.Sprintf(" ×%.0f", f)
	}
	label += " " + p.clockText()
	w := getFace(FontBold, 22).measure(label) + 18
	box := rect(x-w, r.Min.Y+4, x, r.Max.Y-4)
	c.Round(box, 4, colSim)
	c.Text(box, label, Style{Font: FontBold, Size: 22, Color: colText, H: Center})
	return x - w - 8
}

// simBandH is the height the home screen gives the simulation band, gap included.
// The word must be readable at 2 m, not merely visible: 72 px type has capitals of
// about 52 px (7 mm, about 12 arcmin at 2 m on this panel), over twice the 5 arcmin
// limit of normal vision.
const simBandH = 70

// drawSimBand is the full-width simulation marker at the top of the home screen.
// The hatched ends match the frame.
func (p *Panel) drawSimBand(c *Canvas, r image.Rectangle) {
	label := "SIMULATION"
	if f := p.snap.Health.SpeedFactor; finite(f) && f > 1 {
		label += fmt.Sprintf(" ×%.0f", f)
	}
	drawSimBandText(c, r, label, 72)
}

func drawSimBandText(c *Canvas, r image.Rectangle, label string, size int) {
	c.Fill(r, colSim)
	st := Style{Font: FontBold, Size: size, Color: colText, H: Center, MinSize: size * 3 / 4}
	w := getFace(FontBold, size).measure(label) + 24
	if w > r.Dx()-24 {
		w = r.Dx() - 24
	}
	mid := (r.Min.X + r.Max.X) / 2
	c.Hatch(rect(r.Min.X, r.Min.Y, mid-w/2, r.Max.Y), 18, 9, colSimLite)
	c.Hatch(rect(mid+w/2, r.Min.Y, r.Max.X, r.Max.Y), 18, 9, colSimLite)
	c.Text(rect(mid-w/2, r.Min.Y, mid+w/2, r.Max.Y), label, st)
}

func (p *Panel) drawSimFrame(c *Canvas) {
	b := c.Img.Bounds()
	stripe := func(r image.Rectangle) {
		c.Fill(r, colSim)
		c.Hatch(r, 18, 9, colSimLite)
	}
	stripe(rect(0, 0, b.Dx(), 5))
	stripe(rect(0, b.Dy()-5, b.Dx(), b.Dy()))
	stripe(rect(0, 0, 5, b.Dy()))
	stripe(rect(b.Dx()-5, 0, b.Dx(), b.Dy()))
}

func (p *Panel) addHit(r image.Rectangle, id string, fn func()) {
	p.hits = append(p.hits, Hit{ID: id, R: r, fn: fn})
}

func (p *Panel) addHold(r image.Rectangle, id string, d time.Duration, fn func()) {
	p.hits = append(p.hits, Hit{ID: id, R: r, Hold: d, fn: fn})
}

func (p *Panel) isPressed(id string) bool { return p.pressed != nil && p.pressed.ID == id }

type btnStyle struct {
	bg, fg color.RGBA
	size   int
}

var (
	btnNormal  = btnStyle{colBtn, colText, 26}
	btnPrimary = btnStyle{colOn, colBlack, 28}
	btnDanger  = btnStyle{colCrit, colText, 28}
	btnWarn    = btnStyle{colWarn, colBlack, 26}
)

// button draws and registers a tap target.
func (p *Panel) button(c *Canvas, r image.Rectangle, id, label string, st btnStyle, fn func()) {
	bg, fg := st.bg, st.fg
	if p.isPressed(id) {
		bg = colBtnDown
		fg = colText
		if Contrast(fg, bg) < 3 {
			fg = colBlack
		}
	}
	c.Round(r, 8, bg)
	c.Stroke(r, 8, 2, colLine, bg)
	c.Wrap(r.Inset(6), label, Style{Font: FontBold, Size: st.size, Color: fg, H: Center, MinSize: 16}, 2)
	p.addHit(r, id, fn)
}

// holdButton fires only after being held; the fill shows progress.
func (p *Panel) holdButton(c *Canvas, r image.Rectangle, id, label string, d time.Duration, st btnStyle, fn func()) {
	c.Round(r, 8, st.bg)
	c.Stroke(r, 8, 2, colLine, st.bg)
	fg := st.fg
	if p.isPressed(id) {
		f := float64(p.now().Sub(p.downAt)) / float64(d)
		if f > 1 {
			f = 1
		}
		if f > 0 {
			fill := r
			fill.Max.X = r.Min.X + int(float64(r.Dx())*f)
			c.Round(fill, 8, colOn)
		}
		// Text sits on a solid plate so it stays readable over the moving fill.
		tw := getFace(FontBold, st.size).measure(label)
		if tw > r.Dx()-24 {
			tw = r.Dx() - 24
		}
		plate := rect(r.Min.X+(r.Dx()-tw)/2-8, r.Min.Y+r.Dy()/2-22, r.Min.X+(r.Dx()+tw)/2+8, r.Min.Y+r.Dy()/2+22)
		c.Round(plate, 4, colBG)
		fg = colText
	}
	c.Wrap(r.Inset(8), label, Style{Font: FontBold, Size: st.size, Color: fg, H: Center, MinSize: 16}, 2)
	p.addHold(r, id, d, fn)
}

func (p *Panel) go_(s screenID) {
	if s == p.screen {
		return
	}
	p.back = p.screen
	p.screen = s
	p.dlg = nil
	p.alarmPage, p.histPage, p.recPage = 0, 0, 0
	p.dirty = true
}

func (p *Panel) showNav() bool {
	if p.edit != nil && p.edit.changed() {
		return false
	}
	switch p.screen {
	case scrKeypad:
		return false
	case scrRecipe:
		return p.locked
	}
	return true
}

func (p *Panel) drawNav(c *Canvas) {
	type item struct {
		id, label string
		s         screenID
	}
	items := []item{{"nav.home", "HOME", scrHome}, {"nav.set", "SET", scrSet}, {"nav.cure", "CURE", scrCure},
		{"nav.alarms", "ALARMS", scrAlarms}, {"nav.trend", "TREND", scrTrend}, {"nav.more", "MORE", scrMore}}
	w := (mR - mX - 5*6) / 6
	for i, it := range items {
		x := mX + i*(w+6)
		r := rect(x, navTop, x+w, navBot)
		st := btnNormal
		st.size = 25
		active := p.screen == it.s || (it.s == scrCure && p.screen == scrRecipe) ||
			(it.s == scrMore && (p.screen == scrHealth || p.screen == scrOutputs || p.screen == scrInstaller || p.screen == scrEdit || p.screen == scrKeypad || p.screen == scrScale))
		if active && p.power != PowerDim {
			st.bg, st.fg = colOn, colBlack
		}
		label := it.label
		if it.s == scrAlarms && len(p.snap.Active) > 0 && !p.Stalled() {
			label = fmt.Sprintf("ALARMS %d", len(p.snap.Active))
			if !active {
				// The badge ranks like the alarm: red is kept for critical.
				st.bg, st.fg = levelColors(p.worstAlarmLevel())
			}
		}
		s := it.s
		p.button(c, r, it.id, label, st, func() { p.navTo(s) })
	}
}

func (p *Panel) worstAlarmLevel() Level {
	best := LevelInfo
	for _, a := range p.snap.Active {
		switch a.Severity {
		case api.SevCritical:
			return LevelCrit
		case api.SevWarning:
			best = LevelWarn
		}
	}
	return best
}

func (p *Panel) navTo(s screenID) {
	if s == scrSet {
		p.beginSetpoints()
	} else if p.screen == scrSet {
		p.edit = nil
	}
	p.go_(s)
}

func (p *Panel) timeoutHome() {
	lost := p.edit != nil && p.edit.changed()
	p.edit = nil
	p.keypad = nil
	p.dlg = nil
	p.installerOpen = false
	p.recipe = nil
	p.screen = scrHome
	p.dirty = true
	if lost {
		p.flash("Timed out. Change NOT saved; old value kept.", true, 8*time.Second)
	}
}

func (p *Panel) flash(msg string, bad bool, d time.Duration) {
	p.toast = &toast{msg: msg, bad: bad, until: p.now().Add(d)}
	p.dirty = true
}

func (p *Panel) drawToast(c *Canvas) {
	if p.toast == nil {
		return
	}
	r := rect(40, 262, 760, 372)
	if p.screen == scrKeypad { // beside the keys, not over them
		r = rect(mX+6, 160, 294, 284)
	}
	bg, fg := colPanel2, colText
	if p.toast.bad {
		bg, fg = colWarn, colBlack
	}
	c.Round(r.Inset(-4), 10, colBG)
	c.Round(r, 8, bg)
	c.Wrap(r.Inset(8), p.toast.msg, Style{Font: FontBold, Size: 26, Color: fg, H: Center, MinSize: 16}, 4)
	// A control under a message cannot be seen, so it must not fire: a touch here
	// only dismisses the message.
	if p.screen != scrKeypad {
		p.addHit(r, "toast.dismiss", func() { p.toast = nil })
	}
}

// command sends to the controller without waiting. The result arrives later.
func (p *Panel) command(cmd api.Command, what string) {
	// With no fresh data a change cannot be seen to take effect. Silence is still
	// tried: it only stops a noise, and asking cannot make anything worse.
	if p.Stalled() && cmd.Kind != api.CmdSilence {
		p.flash("Not sent: the controller is not responding.", true, 4*time.Second)
		return
	}
	if p.pending != "" && p.now().Sub(p.pendingAt) < 5*time.Second {
		p.flash("Still waiting for the controller.", true, 3*time.Second)
		return
	}
	p.pending = what
	p.pendingAt = p.now()
	p.send(cmd)
}

// CommandDone reports the outcome of a command to the operator.
func (p *Panel) CommandDone(cmd api.Command, err error) {
	what := p.pending
	p.pending = ""
	if err != nil {
		p.flash("Refused: "+err.Error(), true, 8*time.Second)
		// An installer editor would otherwise keep showing the refused values as
		// though they were current. The setpoint screen re-reads the live values.
		if p.screen == scrEdit {
			p.edit = nil
			p.screen = scrInstaller
		}
		return
	}
	if what != "" {
		p.flash(what, false, 2*time.Second)
	}
}

// dimImage scales all pixel levels in place. scale/256 of full level.
func dimImage(img *image.RGBA, scale uint32) {
	pix := img.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		pix[i] = uint8(uint32(pix[i]) * scale >> 8)
		pix[i+1] = uint8(uint32(pix[i+1]) * scale >> 8)
		pix[i+2] = uint8(uint32(pix[i+2]) * scale >> 8)
	}
}

// orbit shifts the frame by up to 3 px on a slow cycle, in dim mode only. It
// reduces image retention on static content. Touch is unaffected: dim mode's
// first touch only wakes.
func orbit(img *image.RGBA, now time.Time) {
	path := [...]image.Point{{0, 0}, {2, 0}, {3, 2}, {2, 3}, {0, 3}, {-2, 3}, {-3, 2}, {-2, 0}}
	d := path[(now.Unix()/60)%int64(len(path))]
	if d == (image.Point{}) {
		return
	}
	shift(img, d)
}

func shift(img *image.RGBA, d image.Point) {
	stride := img.Stride
	h := img.Bounds().Dy()
	w := img.Bounds().Dx()
	row := func(y int) []byte { return img.Pix[y*stride : (y+1)*stride] }
	mv := func(y int) {
		src := row(y)
		if d.X >= 0 {
			copy(src[d.X*4:], src[:(w-d.X)*4])
			for i := 0; i < d.X*4; i += 4 {
				src[i], src[i+1], src[i+2], src[i+3] = 0, 0, 0, 255
			}
		} else {
			copy(src, src[-d.X*4:])
			for i := (w + d.X) * 4; i < w*4; i += 4 {
				src[i], src[i+1], src[i+2], src[i+3] = 0, 0, 0, 255
			}
		}
	}
	for y := h - 1; y >= d.Y; y-- {
		if d.Y > 0 {
			copy(row(y), row(y-d.Y))
		}
		mv(y)
	}
	for y := 0; y < d.Y; y++ {
		r := row(y)
		for i := 0; i < len(r); i += 4 {
			r[i], r[i+1], r[i+2], r[i+3] = 0, 0, 0, 255
		}
	}
}
