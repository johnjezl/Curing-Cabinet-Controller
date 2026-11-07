package ui

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

func (p *Panel) alarmTime(t time.Time, clockOK bool) string {
	if t.IsZero() {
		return "--"
	}
	s := t.In(p.cfg.Location).Format("Jan 2 15:04")
	if !clockOK {
		s = "~" + s // from a clock that could not be trusted
	}
	return s
}

// alarmSpan words how long a cleared alarm lasted, trusting the monotonic
// measure the core records over wall clock subtraction.
func alarmSpan(a api.Alarm) string {
	switch {
	case a.DurationSec < 0:
		return "across a restart"
	case a.DurationSec > 0:
		return fmtAge(a.DurationSec)
	}
	return fmtDur(a.Cleared.Sub(a.Since))
}

func (p *Panel) drawAlarms(c *Canvas) {
	s := &p.snap
	tabs := []string{fmt.Sprintf("ACTIVE %d", len(s.Active)), fmt.Sprintf("HISTORY %d", len(s.History))}
	tabTop, tabBot := bodyTop, bodyTop+89
	tw := 190
	for i, t := range tabs {
		r := rect(mX+i*(tw+4), tabTop, mX+i*(tw+4)+tw, tabBot)
		st := btnNormal
		st.size = 24
		if p.alarmTab == i {
			st.bg, st.fg = colOn, colBlack
		}
		n := i
		p.button(c, r, fmt.Sprintf("alarms.tab.%d", i), t, st, func() { p.alarmTab = n })
	}
	right := rect(mX+2*(tw+4)+8, tabTop, mR, tabBot)
	if p.alarmTab == 0 {
		p.drawAlarmActions(c, right)
		p.drawActiveList(c, rect(mX, tabBot+4, mR, bodyBot))
	} else {
		p.drawHistoryList(c, rect(mX, tabBot+4, mR, bodyBot))
	}
}

func (p *Panel) drawAlarmActions(c *Canvas, r image.Rectangle) {
	s := &p.snap
	if p.Stalled() {
		c.Round(r, 8, colPanel2)
		c.Wrap(r.Inset(8), "NOT LIVE: this list stopped updating. Alarms may have changed.", Style{Font: FontBold, Size: 22, Color: colWarn, H: Center, MinSize: 15}, 3)
		return
	}
	if len(s.Active) == 0 {
		c.Text(r, "No active alarms", Style{Font: FontBold, Size: 26, Color: colDim, H: Center})
		return
	}
	half := r
	half.Max.X = r.Min.X + r.Dx()/2 - 4
	if s.BuzzerSilenced {
		c.Round(half, 8, colPanel2)
		c.Wrap(half.Inset(6), "BUZZER SILENCED, alarm still active", Style{Font: FontBold, Size: 19, Color: colText, H: Center, MinSize: 14}, 2)
	} else {
		p.button(c, half, "alarms.silence", "SILENCE BUZZER", btnStyle{colText, colBlack, 22}, func() {
			p.command(api.Command{Kind: api.CmdSilence}, "Buzzer silenced. Alarms remain listed.")
		})
	}
	other := r
	other.Min.X = half.Max.X + 8
	unacked := 0
	for _, a := range s.Active {
		if !a.Acked {
			unacked++
		}
	}
	if p.locked {
		c.Round(other, 8, colPanel2)
		c.Wrap(other.Inset(6), "Unlock to acknowledge", Style{Font: FontBold, Size: 19, Color: colDim, H: Center, MinSize: 14}, 2)
		return
	}
	if unacked > 0 {
		p.button(c, other, "alarms.ackall", fmt.Sprintf("ACK ALL %d", unacked), btnStyle{colBtn, colText, 22}, func() {
			p.command(api.Command{Kind: api.CmdAck}, "Acknowledged. Alarms stay until the condition clears.")
		})
	} else {
		c.Round(other, 8, colPanel2)
		c.Wrap(other.Inset(6), "All acknowledged", Style{Font: FontBold, Size: 19, Color: colDim, H: Center, MinSize: 14}, 2)
	}
}

func sevColors(s api.Severity) (colorBar, textOn color.RGBA) {
	switch s {
	case api.SevCritical:
		return colCrit, colText
	case api.SevWarning:
		return colWarn, colBlack
	}
	return colInfo, colText
}

func (p *Panel) alarmValues(a api.Alarm) string {
	var parts string
	if a.Limit != 0 || a.Peak != 0 {
		if finite(a.Peak) && a.Peak != 0 {
			parts = fmt.Sprintf("peak %s%s", trimFloat(a.Peak), a.Unit)
		}
		if finite(a.Limit) && a.Limit != 0 {
			if parts != "" {
				parts += ", "
			}
			parts += fmt.Sprintf("limit %s%s", trimFloat(a.Limit), a.Unit)
		}
	}
	return parts
}

func trimFloat(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%.0f ", v)
	}
	return fmt.Sprintf("%.1f ", v)
}

func (p *Panel) drawActiveList(c *Canvas, r image.Rectangle) {
	al := sortAlarms(p.snap.Active)
	if len(al) == 0 {
		c.Round(r, 6, colPanel)
		c.Text(r, "Nothing needs action.", Style{Font: FontBold, Size: 30, Color: colDim, H: Center})
		return
	}
	per := 2
	pages := (len(al) + per - 1) / per
	if p.alarmPage >= pages {
		p.alarmPage = pages - 1
	}
	listR := r.Max.X
	if pages > 1 {
		listR -= 94
	}
	h := (r.Dy() - 4) / per
	for i := 0; i < per; i++ {
		ai := p.alarmPage*per + i
		if ai >= len(al) {
			break
		}
		a := al[ai]
		y := r.Min.Y + i*(h+4)
		row := rect(r.Min.X, y, listR, y+h)
		p.alarmRow(c, row, a)
	}
	if pages > 1 {
		p.pager(c, listR+4, r.Min.Y, r.Max.X, r.Max.Y, p.alarmPage, pages, "alarms.page", func(n int) { p.alarmPage = n })
	}
}

func (p *Panel) alarmRow(c *Canvas, row image.Rectangle, a api.Alarm) {
	bar, on := sevColors(a.Severity)
	c.Round(row, 6, colPanel)
	sevW := 118
	sev := rect(row.Min.X, row.Min.Y, row.Min.X+sevW, row.Min.Y+34)
	c.Round(sev, 6, bar)
	c.Text(sev.Inset(2), sevWord(a.Severity), Style{Font: FontBold, Size: 18, Color: on, H: Center, MinSize: 13})
	btnX := row.Max.X
	if !a.Acked && !p.locked && !p.Stalled() {
		ack := rect(row.Max.X-104, row.Min.Y+2, row.Max.X-2, row.Max.Y-2)
		id := a.ID
		p.button(c, ack, "alarms.ack."+a.ID, "ACK", btnStyle{colBtn, colText, 24}, func() {
			p.command(api.Command{Kind: api.CmdAck, AlarmID: id}, "Acknowledged. It stays until the condition clears.")
		})
		btnX = ack.Min.X
	}
	if isRefClearable(a.Code) && !p.locked && !p.Stalled() {
		chk := rect(btnX-104, row.Min.Y+2, btnX-2, row.Max.Y-2)
		p.button(c, chk, "alarms.refcheck."+a.ID, "CHECK…", btnStyle{colBtn, colText, 22}, func() { p.requireInstaller(p.beginRefCheck) })
		btnX = chk.Min.X
	}
	state := "NEW"
	if a.Acked {
		state = "ACKED"
	}
	c.Wrap(rect(sev.Max.X+8, row.Min.Y, btnX-6, sev.Max.Y+4), a.Title+" ("+state+")", Style{Font: FontBold, Size: 25, Color: colText, MinSize: 14}, 2)
	c.Wrap(rect(row.Min.X+8, sev.Max.Y+5, btnX-6, row.Max.Y-21), "Do: "+a.Action, Style{Size: 22, Color: colText, MinSize: 13, V: Top}, 3)
	age := p.snap.Now.Sub(a.Since)
	foot := "since " + p.alarmTime(a.Since, a.ClockOK) + " (" + fmtDur(age) + ")"
	if v := p.alarmValues(a); v != "" {
		foot += ", " + v
	}
	c.Text(rect(row.Min.X+8, row.Max.Y-24, btnX-6, row.Max.Y-2), foot, Style{Size: 18, Color: colDim, MinSize: 12})
}

func (p *Panel) drawHistoryList(c *Canvas, r image.Rectangle) {
	h := p.snap.History
	if len(h) == 0 {
		c.Round(r, 6, colPanel)
		c.Text(r, "No alarms recorded.", Style{Font: FontBold, Size: 30, Color: colDim, H: Center})
		return
	}
	per := 3
	pages := (len(h) + per - 1) / per
	if p.histPage >= pages {
		p.histPage = pages - 1
	}
	listR := r.Max.X
	if pages > 1 {
		listR -= 94
	}
	rowH := (r.Dy() - (per-1)*3) / per
	for i := 0; i < per; i++ {
		ai := p.histPage*per + i
		if ai >= len(h) {
			break
		}
		a := h[ai]
		y := r.Min.Y + i*(rowH+3)
		row := rect(r.Min.X, y, listR, y+rowH)
		c.Round(row, 4, colPanel)
		bar, _ := sevColors(a.Severity)
		c.Fill(rect(row.Min.X, row.Min.Y, row.Min.X+10, row.Max.Y), bar)
		end := "still active"
		if !a.Cleared.IsZero() {
			end = "to " + a.Cleared.In(p.cfg.Location).Format("15:04") + " (" + alarmSpan(a) + ")"
		}
		c.Text(rect(row.Min.X+18, row.Min.Y+1, row.Max.X-110, row.Min.Y+rowH/2+1), a.Title, Style{Font: FontBold, Size: 22, Color: colText, MinSize: 12})
		c.Text(rect(row.Max.X-108, row.Min.Y+1, row.Max.X-6, row.Min.Y+rowH/2+1), sevWord(a.Severity), Style{Font: FontBold, Size: 16, Color: colDim, H: Right, MinSize: 12})
		line := p.alarmTime(a.Since, a.ClockOK) + " " + end
		if v := p.alarmValues(a); v != "" {
			line += ", " + v
		}
		c.Text(rect(row.Min.X+18, row.Min.Y+rowH/2+1, row.Max.X-6, row.Max.Y-1), line, Style{Size: 19, Color: colDim, MinSize: 12})
	}
	if pages > 1 {
		p.pager(c, listR+4, r.Min.Y, r.Max.X, r.Max.Y, p.histPage, pages, "history.page", func(n int) { p.histPage = n })
	}
}
