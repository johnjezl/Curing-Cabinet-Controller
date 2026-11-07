package ui

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

func (p *Panel) drawMore(c *Canvas) {
	bw := (mR - mX - 3*8) / 4
	bh := (bodyBot - bodyTop - 8) / 2
	cell := func(i, j int) image.Rectangle {
		x := mX + i*(bw+8)
		y := bodyTop + j*(bh+8)
		return rect(x, y, x+bw, y+bh)
	}
	st := btnStyle{colBtn, colText, 28}
	off := func(r image.Rectangle, msg string) {
		c.Round(r, 8, colPanel)
		c.Wrap(r.Inset(10), msg, Style{Size: 24, Color: colDim, H: Center, MinSize: 16}, 4)
	}
	p.button(c, cell(0, 0), "more.health", "HEALTH", st, func() { p.go_(scrHealth) })
	p.button(c, cell(1, 0), "more.outputs", "OUTPUTS", st, func() { p.go_(scrOutputs) })
	checkLabel := "SENSOR CHECK " + p.refDueShort()
	checkSt := st
	checkSt.size = 24
	if d := p.snap.Health.RefCheckDueSec; finite(d) && d <= 0 && !p.Stalled() {
		checkSt.bg, checkSt.fg = colWarn, colBlack
	}
	switch {
	case p.locked:
		off(cell(2, 0), "Sensor check "+p.refDueShort()+": unlock first")
		p.holdButton(c, cell(3, 0), "lock.unlock", "HOLD 3 s TO UNLOCK", p.cfg.UnlockHold, btnPrimary, p.unlock)
		off(cell(0, 1), "Defrost: unlock first")
		off(cell(1, 1), "Installer: unlock first")
	default:
		p.holdButton(c, cell(3, 0), "more.lock", "HOLD 2 s TO LOCK", 2*time.Second, st, p.lockPanel)
		if p.Stalled() {
			off(cell(2, 0), "Sensor check: controller not responding")
			off(cell(0, 1), "Defrost: controller not responding")
		} else {
			p.button(c, cell(2, 0), "more.refcheck", checkLabel, checkSt, func() { p.requireInstaller(p.beginRefCheck) })
			p.button(c, cell(0, 1), "more.defrost", "DEFROST NOW…", st, func() {
				p.ask(dialog{id: "defrost", title: "Defrost now?", body: "Cooling stops while the coil defrosts. The cabinet temperature may rise for a short time. Protection timers still apply and may delay it.",
					confirm: "HOLD 2 s: DEFROST", hold: p.cfg.ConfirmHold, fn: func() { p.command(api.Command{Kind: api.CmdDefrost}, "Defrost requested.") }})
			})
		}
		p.button(c, cell(1, 1), "more.installer", "INSTALLER", st, p.openInstallerGate)
	}
	p.button(c, cell(2, 1), "more.sleep", "SCREEN OFF", st, func() {
		if attentionNeeded(&p.snap, p.Stalled()) {
			p.flash("Screen stays on: something needs attention.", true, 4*time.Second)
			return
		}
		p.screen = scrHome
		p.manualSleep = true
	})
}

// refDueShort words the time to the next reference check, for a button.
func (p *Panel) refDueShort() string {
	d := p.snap.Health.RefCheckDueSec
	if !finite(d) {
		return ""
	}
	if d <= 0 {
		return "OVERDUE"
	}
	return "in " + fmtDur(time.Duration(d*float64(time.Second)))
}

func orDash(s string) string {
	if s == "" {
		return "--"
	}
	return s
}

func yesNo(b bool, yes, no string) (string, bool) {
	if b {
		return yes, true
	}
	return no, false
}

// drawHealth states facts about the controller itself. Problems are amber text
// with a leading "!" so they do not rely on colour.
func (p *Panel) drawHealth(c *Canvas) {
	s := &p.snap
	h := s.Health
	left := rect(mX, bodyTop, (mX+mR)/2-2, bodyBot)
	right := rect((mX+mR)/2+2, bodyTop, mR, bodyBot)
	c.Round(left, 6, colPanel)
	c.Round(right, 6, colPanel)
	type row struct {
		k, v string
		ok   bool
	}
	plant := "real hardware"
	if h.Simulated {
		plant = "SIMULATED"
		if h.SpeedFactor > 1 {
			plant += fmt.Sprintf(" ×%.0f", h.SpeedFactor)
		}
	}
	clock, clockOK := yesNo(h.ClockTrusted, "trusted", "NOT trusted, times are ~")
	logT, logOK := yesNo(h.LogOK, "logging", "NOT logging")
	wd, wdOK := yesNo(h.WatchdogOK, "ok", "FAULT")
	out := "none seen"
	outOK := true
	if h.LastOutage.Detected {
		outOK = false
		out = p.alarmTime(h.LastOutage.At, h.LastOutage.DurationOK)
		if h.LastOutage.DurationOK {
			out += ", lasted " + fmtDur(h.LastOutage.Duration)
		} else {
			out += ", length unknown"
		}
	}
	up := "--"
	if !h.StartedAt.IsZero() {
		up = fmtDur(s.Now.Sub(h.StartedAt))
	}
	stalled := p.Stalled()
	unknown := "unknown: no fresh data"
	if stalled {
		clock, clockOK = unknown, false
		logT, logOK = unknown, false
		wd, wdOK = unknown, false
	}
	storage := fmtFloat(h.StorageFreeM, 0) + " MB"
	storageOK := h.StorageFreeM >= 100
	if stalled {
		storage, storageOK = unknown, false
	}
	rows := []row{
		{"Plant", plant, !h.Simulated},
		{"Version", orDash(h.Version), true},
		{"Running for", up, true},
		{"Clock", clock, clockOK},
		{"Data log", logT, logOK},
		{"Storage free", storage, storageOK},
		{"Watchdog", wd, wdOK},
		{"Last outage", out, outOK},
	}
	if h.LastOutage.Detected && finite(h.LastOutage.TempOnBackC) {
		rows = append(rows, row{"Temp on return", fmtFloat(h.LastOutage.TempOnBackC, 1) + " °C", true})
	}
	if p.Stalled() {
		rows = append(rows, row{"Controller", "not responding", false})
	}
	if p.settingsErr != "" {
		rows = append(rows, row{"Panel settings", p.settingsErr, false})
	}
	if !stalled {
		rows = append(rows, row{"Sensor check", p.refCheckWords(), finite(h.RefCheckDueSec) && h.RefCheckDueSec > 0})
	}
	rows = append(rows, row{"Panel live", "bottom bar moves each second", true})
	drawRows := func(r image.Rectangle, keyW int, rs []row) {
		rh := r.Dy() / maxInt(len(rs), 1)
		if rh > 41 {
			rh = 41
		}
		for i, ro := range rs {
			y := r.Min.Y + 4 + i*rh
			c.Text(rect(r.Min.X+10, y, r.Min.X+keyW, y+rh), ro.k, Style{Size: 21, Color: colDim, MinSize: 14})
			col := colText
			txt := ro.v
			if !ro.ok {
				col = colWarn
				txt = "! " + txt
			}
			c.Wrap(rect(r.Min.X+keyW+4, y, r.Max.X-8, y+rh), txt, Style{Font: FontBold, Size: 21, Color: col, MinSize: 14}, 2)
		}
	}
	drawRows(left, 148, rows)
	sens := []row{}
	type sr struct {
		n    string
		r    api.Reading
		dec  int
		unit string
	}
	for _, e := range []sr{{"Temperature", s.Temp, 1, "°C"}, {"Humidity", s.RH, 0, "%"}, {"Airflow", s.Air, 2, "m/s"}, {"Weight", s.Weight, 0, "g"}, {"Door", s.Door, 0, ""}} {
		v := qualityWord(e.r.Quality)
		ok := e.r.Quality == api.QualityOK || e.r.Quality == api.QualityOff
		if stalled {
			v, ok = "NOT LIVE", false
		} else if e.r.Quality == api.QualityOK {
			if e.n == "Door" {
				v = "shut"
				if e.r.Value >= 1 {
					v = "OPEN"
				}
			} else {
				v = fmtFloat(e.r.Value, e.dec) + " " + e.unit
			}
			v += ", " + fmtFloat(math.Max(0, e.r.AgeSec), 0) + " s old"
		} else if e.r.Quality != api.QualityOff {
			v += " " + fmtAge(e.r.AgeSec)
			if e.r.Note != "" {
				v += ": " + e.r.Note
			}
		}
		sens = append(sens, row{e.n, v, ok})
	}
	fan := "off"
	if s.Outputs.FanDutyPct > 0 {
		fan = fmtFloat(s.Outputs.FanDutyPct, 0) + " %, " + fmtFloat(s.Outputs.FanRPM, 0) + " rpm"
	}
	fanOK := true
	if stalled {
		fan, fanOK = "NOT LIVE", false
	}
	sens = append(sens, row{"Fan", fan, fanOK})
	lim := s.Limits
	sens = append(sens, row{"Temp alarms", fmt.Sprintf("+%s / -%s °C, abs %s", fmtFloat(lim.TempHighDelta, 1), fmtFloat(lim.TempLowDelta, 1), fmtFloat(lim.AbsTempHighC, 1)), true})
	sens = append(sens, row{"RH alarms", fmt.Sprintf("+%s / -%s %%", fmtFloat(lim.RHHighDelta, 0), fmtFloat(lim.RHLowDelta, 0)), true})
	drawRows(right, 128, sens)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// keypad is a numeric entry pad, used for the installer code and known weight.
type keypad struct {
	title   string
	header  string
	mask    bool
	digits  string
	maxLen  int
	dot     bool
	onOK    func(string)
	onBack  func()
	okLabel string
}

func (p *Panel) openInstallerGate() {
	p.requireInstaller(func() {
		p.installerOpen = true
		p.screen = scrInstaller
	})
}

// requireInstaller asks for the installer code, then runs then.
func (p *Panel) requireInstaller(then func()) {
	if p.now().Before(p.lockoutUntil) {
		p.flash(fmt.Sprintf("Too many wrong codes. Wait %.0f s.", p.lockoutUntil.Sub(p.now()).Seconds()), true, 4*time.Second)
		return
	}
	cancelTo := p.screen
	p.keypad = &keypad{title: "Installer code", mask: true, maxLen: 8, okLabel: "ENTER", onOK: func(code string) {
		if p.settings.checkCode(code) {
			p.badCode = 0
			p.keypad = nil
			if !p.settings.HasOwnCode() {
				// The published default opens the door once, only to replace itself.
				p.afterCode = then
				p.beginNewCode(true)
				return
			}
			then()
			return
		}
		p.badCode++
		p.keypad.digits = ""
		if p.badCode >= 5 {
			p.badCode = 0
			p.lockoutUntil = p.now().Add(60 * time.Second)
			p.keypad = nil
			p.screen = cancelTo
			p.flash("Too many wrong codes. Wait 60 s.", true, 5*time.Second)
			return
		}
		p.flash("Wrong code.", true, 3*time.Second)
	}, onBack: func() { p.keypad = nil; p.screen = cancelTo }}
	p.go_(scrKeypad)
}

func (p *Panel) drawKeypad(c *Canvas) {
	k := p.keypad
	if k == nil {
		p.screen = scrMore
		return
	}
	left := rect(mX, bodyTop, 300, bodyBot)
	c.Round(left, 6, colPanel)
	c.Wrap(rect(left.Min.X+12, left.Min.Y+8, left.Max.X-12, left.Min.Y+80), k.title, Style{Font: FontBold, Size: 28, Color: colText, V: Top, MinSize: 18}, 2)
	shown := k.digits
	if k.mask {
		shown = strings.Repeat("●", len(k.digits))
	}
	field := rect(left.Min.X+12, left.Min.Y+90, left.Max.X-12, left.Min.Y+160)
	c.Round(field, 6, colBG)
	if shown != "" {
		c.Text(field.Inset(4), shown, Style{Font: FontBold, Size: 44, Color: colText, H: Center, MinSize: 20})
	}
	backR := rect(left.Min.X+12, left.Max.Y-99, left.Max.X-12, left.Max.Y-8)
	if k.dot {
		delR := backR
		delR.Min.X = backR.Min.X + backR.Dx()/2 + 4
		backR.Max.X = delR.Min.X - 8
		p.button(c, delR, "keypad.del", "DEL", btnStyle{colBtn, colText, 24}, func() {
			if len(k.digits) > 0 {
				k.digits = k.digits[:len(k.digits)-1]
			}
		})
	}
	p.button(c, backR, "keypad.back", "CANCEL", btnStyle{colBtn, colText, 24}, func() {
		if k.onBack != nil {
			k.onBack()
		}
	})
	keys := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "⌫", "0", "OK"}
	if k.dot {
		keys[9] = "."
	}
	gridBot := navBot
	x0, gap := 308, 4
	kw := (mR - x0 - 2*gap) / 3
	kh := (gridBot - bodyTop - 3*gap) / 4
	for i, key := range keys {
		r := rect(x0+(i%3)*(kw+gap), bodyTop+(i/3)*(kh+gap), x0+(i%3)*(kw+gap)+kw, bodyTop+(i/3)*(kh+gap)+kh)
		st := btnStyle{colBtn, colText, 44}
		key := key
		id := "keypad." + key
		switch key {
		case ".":
			p.button(c, r, "keypad.dot", ".", st, func() {
				if len(k.digits) < k.maxLen && !strings.Contains(k.digits, ".") {
					k.digits += "."
				}
			})
			continue
		case "OK":
			st = btnStyle{colOn, colBlack, 34}
			id = "keypad.ok"
			label := k.okLabel
			p.button(c, r, id, label, st, func() {
				if k.onOK != nil {
					k.onOK(k.digits)
				}
			})
			continue
		case "⌫":
			st.size = 26
			p.button(c, r, "keypad.del", "DEL", st, func() {
				if len(k.digits) > 0 {
					k.digits = k.digits[:len(k.digits)-1]
				}
			})
			continue
		}
		p.button(c, r, id, key, st, func() {
			if len(k.digits) < k.maxLen {
				k.digits += key
			}
		})
	}
}

// beginNewCode asks for a new installer code twice. With forced set, this is the
// first entry with the default code, and the installer stays shut until it is done.
func (p *Panel) beginNewCode(forced bool) {
	back := func() {
		p.keypad = nil
		if forced {
			p.afterCode = nil
			p.screen = scrMore
			p.flash("The installer stays shut until its code is changed.", true, 4*time.Second)
			return
		}
		p.screen = scrInstaller
	}
	title := "Set a new installer code, 4 to 8 digits"
	if forced {
		title = "Default code: set your own, 4 to 8 digits"
	}
	p.keypad = &keypad{title: title, mask: true, maxLen: 8, okLabel: "NEXT", onBack: back, onOK: func(first string) {
		if len(first) < 4 {
			p.flash("Use at least 4 digits.", true, 3*time.Second)
			return
		}
		if first == defaultInstallerCode {
			p.keypad.digits = ""
			p.flash("That is the published default. Choose another.", true, 4*time.Second)
			return
		}
		p.keypad = &keypad{title: "Repeat the new code", mask: true, maxLen: 8, okLabel: "SAVE", onBack: back, onOK: func(second string) {
			if second != first {
				p.flash("Codes differ. Nothing changed; start again.", true, 4*time.Second)
				p.beginNewCode(forced)
				return
			}
			if err := p.settings.SetCode(first); err != nil {
				p.flash("Code not changed: "+err.Error(), true, 6*time.Second)
				back()
				return
			}
			p.save()
			p.keypad = nil
			next := p.afterCode
			p.afterCode = nil
			if next != nil {
				next()
			} else {
				p.installerOpen = true
				p.screen = scrInstaller
			}
			p.flash("Installer code changed.", false, 3*time.Second)
		}}
	}}
	p.screen = scrKeypad
	p.dirty = true
}

func (p *Panel) drawInstaller(c *Canvas) {
	if !p.installerOpen {
		p.screen = scrMore
		return
	}
	bh := (bodyBot - bodyTop - 8) / 2
	bw := (mR - mX - 2*8) / 3
	cell := func(i, j int) image.Rectangle {
		x := mX + i*(bw+8)
		y := bodyTop + j*(bh+8)
		return rect(x, y, x+bw, y+bh)
	}
	st := btnStyle{colBtn, colText, 28}
	p.button(c, cell(0, 0), "inst.limits", "ALARM LIMITS", st, p.beginLimits)
	p.button(c, cell(1, 0), "inst.cal", "SENSOR OFFSETS", st, p.beginCal)
	p.button(c, cell(2, 0), "inst.scale", "SCALE", st, func() { p.go_(scrScale) })
	p.button(c, cell(0, 1), "inst.code", "CHANGE CODE", st, func() { p.beginNewCode(false) })
	p.button(c, cell(1, 1), "inst.refcheck", "SENSOR CHECK "+p.refDueShort(), btnStyle{colBtn, colText, 24}, p.beginRefCheck)
	p.button(c, cell(2, 1), "inst.exit", "CLOSE INSTALLER", st, func() {
		p.installerOpen = false
		p.go_(scrMore)
	})
}

func (p *Panel) beginLimits() {
	l := p.snap.Limits
	fields := []field{
		{"Temp high, above set", "°C", 0.5, 0.5, 20, 1},
		{"Temp low, below set", "°C", 0.5, 0.5, 20, 1},
		{"Humidity high, above set", "%", 1, 1, 40, 0},
		{"Humidity low, below set", "%", 1, 1, 40, 0},
		{"Alarm delay", "min", 1, 0, 240, 0},
		{"Door alarm delay", "min", 1, 1, 240, 1},
		{"Absolute temp high", "°C", 0.5, 5, 60, 1},
		{"Absolute temp low", "°C", 0.5, -10, 5, 1},
	}
	vals := []float64{l.TempHighDelta, l.TempLowDelta, l.RHHighDelta, l.RHLowDelta, l.AlarmDelay.Minutes(), l.DoorDelay.Minutes(), l.AbsTempHighC, l.AbsTempLowC}
	p.edit = newEditor("LIMITS", fields, vals, 3, true, func(v []float64) (api.Command, string) {
		nl := l // keep anything this screen does not show
		nl.TempHighDelta, nl.TempLowDelta, nl.RHHighDelta, nl.RHLowDelta = v[0], v[1], v[2], v[3]
		nl.AlarmDelay = time.Duration(v[4] * float64(time.Minute))
		nl.DoorDelay = time.Duration(v[5] * float64(time.Minute))
		nl.AbsTempHighC, nl.AbsTempLowC = v[6], v[7]
		return api.Command{Kind: api.CmdSetLimits, Limits: &nl}, "Alarm limits saved."
	})
	p.go_(scrEdit)
}

func (p *Panel) beginCal() {
	cal := p.snap.Calibration
	fields := []field{
		{"Temperature offset", "°C", 0.1, -12, 12, 1},
		{"Humidity offset", "%", 1, -15, 15, 0},
	}
	p.edit = newEditor("OFFSETS", fields, []float64{cal.TempOffsetC, cal.RHOffsetPct}, 3, true, func(v []float64) (api.Command, string) {
		nc := cal
		nc.TempOffsetC, nc.RHOffsetPct = v[0], v[1]
		return api.Command{Kind: api.CmdSetCal, Calibration: &nc}, "Offsets saved."
	})
	p.go_(scrEdit)
}

func (p *Panel) drawScale(c *Canvas) {
	s := &p.snap
	info := rect(mX, bodyTop, mR, bodyTop+150)
	c.Round(info, 6, colPanel)
	w := "reading " + qualityWord(s.Weight.Quality)
	if readingGood(s.Weight) {
		w = fmtWeight(s.Weight.Value) + " g"
	}
	c.Text(rect(info.Min.X+12, info.Min.Y+4, info.Max.X-12, info.Min.Y+64), "Scale now: "+w, Style{Font: FontBold, Size: 40, Color: colText, MinSize: 22})
	c.Text(rect(info.Min.X+12, info.Min.Y+70, info.Max.X-12, info.Min.Y+104), fmt.Sprintf("Zero %d counts, %s counts per gram", s.Calibration.ScaleZero, fmtFloat(s.Calibration.ScaleCountsPG, 2)), Style{Size: 24, Color: colDim, MinSize: 15})
	c.Text(rect(info.Min.X+12, info.Min.Y+108, info.Max.X-12, info.Max.Y-4), "Tare with the empty rack. Calibrate with a known mass on it.", Style{Size: 22, Color: colDim, MinSize: 15})
	y := bodyTop + 158
	if pr := s.Program.State; pr == api.RunActive || pr == api.RunPaused {
		// The core refuses both during a cure: it would invalidate the weight loss.
		r := rect(mX, y, mR, bodyBot)
		c.Round(r, 6, colPanel2)
		c.Wrap(r.Inset(12), "Tare and calibrate are off during a cure: they would invalidate the weight loss. Stop the cure first.", Style{Font: FontBold, Size: 28, Color: colText, H: Center, MinSize: 18}, 3)
		return
	}
	bw := (mR - mX - 8) / 2
	st := btnStyle{colBtn, colText, 28}
	p.button(c, rect(mX, y, mX+bw, bodyBot), "scale.tare", "TARE…", st, func() {
		p.ask(dialog{id: "tare", title: "Tare the scale?", body: "The current load becomes zero. Only tare an empty rack.",
			confirm: "HOLD 2 s: TARE", hold: p.cfg.ConfirmHold, danger: true, fn: func() { p.command(api.Command{Kind: api.CmdTare}, "Scale zeroed.") }})
	})
	p.button(c, rect(mX+bw+8, y, mR, bodyBot), "scale.cal", "CALIBRATE…", st, func() {
		p.keypad = &keypad{title: "Known mass on scale, grams", header: "SCALE", maxLen: 6, okLabel: "NEXT", onBack: func() { p.keypad = nil; p.screen = scrScale }, onOK: func(d string) {
			g, err := strconv.ParseFloat(d, 64)
			if err != nil || g <= 0 {
				p.flash("Enter the mass in grams.", true, 3*time.Second)
				return
			}
			p.keypad = nil
			p.screen = scrScale
			p.ask(dialog{id: "calweight", title: "Calibrate to " + d + " g?", body: "This sets the scale factor from the load now on the rack. Confirm that exactly " + d + " g is on the scale.",
				confirm: "HOLD 2 s: CALIBRATE", hold: p.cfg.ConfirmHold, fn: func() { p.command(api.Command{Kind: api.CmdCalWeight, KnownWeightG: g}, "Scale calibrated.") }})
		}}
		p.go_(scrKeypad)
	})
}

// refCheckWords states the seven-day reference check for Health.
func (p *Panel) refCheckWords() string {
	h := p.snap.Health
	last := ""
	if !h.LastRefCheck.IsZero() {
		last = ", last " + p.alarmTime(h.LastRefCheck, true)
	}
	d := h.RefCheckDueSec
	switch {
	case !finite(d):
		return "due time unknown" + last
	case d <= 0:
		return "OVERDUE by " + fmtDur(time.Duration(-d*float64(time.Second))) + last
	}
	return "due in " + fmtDur(time.Duration(d*float64(time.Second))) + last
}

// drawOutputsScreen says why each output is doing what it is doing, in the core's
// words, so a wait is not mistaken for a fault or for nothing happening.
func (p *Panel) drawOutputsScreen(c *Canvas) {
	s := &p.snap
	stalled := p.Stalled()
	sts := s.Outputs.Status
	type row struct {
		name, state, kind, why string
	}
	var rows []row
	timer := func(out string) string {
		for _, h := range s.Outputs.Holds {
			if strings.EqualFold(h.Output, out) && finite(h.RemainingSec) && h.RemainingSec > 0 {
				return ", " + fmtTimer(h.RemainingSec) + " left"
			}
		}
		return ""
	}
	for _, o := range sts {
		state := "OFF"
		if o.On {
			state = "ON"
		}
		rows = append(rows, row{strings.ToUpper(o.Output), state, o.Kind, o.Reason + timer(o.Output)})
	}
	if len(sts) == 0 {
		onoff := func(n string, b bool) {
			state := "OFF"
			if b {
				state = "ON"
			}
			rows = append(rows, row{n, state, "", "no reason reported"})
		}
		onoff("COOL", s.Outputs.Cool)
		onoff("HEAT", s.Outputs.Heat)
		onoff("HUMID", s.Outputs.Humid)
	}
	fan := row{"FAN", "OFF", "", "not running"}
	if s.Outputs.FanDutyPct > 0 {
		fan = row{"FAN", "ON", "", fmtFloat(s.Outputs.FanDutyPct, 0) + " % duty, " + fmtFloat(s.Outputs.FanRPM, 0) + " rpm"}
	}
	rows = append(rows, fan)
	defrost := row{"DEFROST", "OFF", "", "not running"}
	if s.Outputs.Defrost {
		defrost = row{"DEFROST", "ON", "", "running; cabinet temperature may rise"}
	}
	rows = append(rows, defrost)
	h := (bodyBot - bodyTop - (len(rows)-1)*4) / len(rows)
	for i, ro := range rows {
		y := bodyTop + i*(h+4)
		r := rect(mX, y, mR, y+h)
		c.Round(r, 6, colPanel)
		c.Text(rect(r.Min.X+10, r.Min.Y, r.Min.X+130, r.Max.Y), ro.name, Style{Font: FontBold, Size: 24, Color: colText, MinSize: 16})
		pill := rect(r.Min.X+132, r.Min.Y+8, r.Min.X+262, r.Max.Y-8)
		switch {
		case stalled:
			c.Round(pill, 4, colPanel2)
			c.Text(pill, "NOT LIVE", Style{Font: FontBold, Size: 20, Color: colWarn, H: Center, MinSize: 14})
			ro.why = "last known: " + ro.why
		case ro.kind == "fault":
			c.Round(pill, 4, colCrit)
			c.Text(pill, ro.state+" FAULT", Style{Font: FontBold, Size: 20, Color: colText, H: Center, MinSize: 14})
		case ro.kind == "hold":
			c.Round(pill, 4, colWarn)
			c.Text(pill, ro.state+" HOLD", Style{Font: FontBold, Size: 20, Color: colBlack, H: Center, MinSize: 14})
		case ro.state == "ON":
			c.Round(pill, 4, colOn)
			c.Text(pill, "ON", Style{Font: FontBold, Size: 20, Color: colBlack, H: Center})
		default:
			c.Round(pill, 4, colPanel2)
			c.Text(pill, "OFF", Style{Font: FontBold, Size: 20, Color: colDim, H: Center})
		}
		c.Wrap(rect(r.Min.X+270, r.Min.Y+2, r.Max.X-8, r.Max.Y-2), ro.why, Style{Size: 22, Color: colText, MinSize: 14}, 2)
	}
}
