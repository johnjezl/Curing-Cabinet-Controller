package ui

// ask opens a confirmation. With hold > 0 the confirm button must be held,
// which suits destructive steps: a greasy brush cannot trigger it.
func (p *Panel) ask(d dialog) {
	if d.cancel == "" {
		d.cancel = "CANCEL"
	}
	p.dlg = &d
	p.dirty = true
}

func (p *Panel) drawDialog(c *Canvas, _ Status) {
	d := p.dlg
	body := rect(mX, bodyTop, mR, navTop-4)
	c.Round(body, 8, colPanel)
	c.Text(rect(body.Min.X+16, body.Min.Y+8, body.Max.X-16, body.Min.Y+60), d.title, Style{Font: FontBold, Size: 38, Color: colText, MinSize: 24})
	c.Wrap(rect(body.Min.X+16, body.Min.Y+66, body.Max.X-16, body.Max.Y-8), d.body, Style{Size: 28, Color: colText, MinSize: 18, V: Top}, 7)
	cancel := rect(mX, navTop, mX+330, navBot)
	ok := rect(mR-330, navTop, mR, navBot)
	st := btnPrimary
	if d.danger {
		st = btnDanger
	}
	p.button(c, cancel, "dialog.cancel", d.cancel, btnNormal, func() {
		p.dlg = nil
		p.dirty = true
	})
	run := func() {
		fn := d.fn
		p.dlg = nil
		p.dirty = true
		if fn != nil {
			fn()
		}
	}
	if d.hold > 0 {
		p.holdButton(c, ok, "dialog.ok", d.confirm, d.hold, st, run)
	} else {
		p.button(c, ok, "dialog.ok", d.confirm, st, run)
	}
}
