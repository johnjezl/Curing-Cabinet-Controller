package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// Tolerances the core applies to a reference check (crosscheck.go); a test compares.
const (
	refTolTempC = 1.5
	refTolRHPct = 5.0
)

// isRefClearable says a reference check is the way out of this alarm.
func isRefClearable(code string) bool {
	switch code {
	case "TEMP_READING_DISAGREES", "RH_READING_DISAGREES", "REFERENCE_DISAGREES", "CAL_CHECK_DUE":
		return true
	}
	return false
}

// beginRefCheck starts the entry. The reference readings are typed in with the
// sensor's value out of sight, and nothing is filled in, so that "looks about
// right" cannot clear a reading in doubt. See ADR 0311.
func (p *Panel) beginRefCheck() {
	s := &p.snap
	usable := func(r api.Reading) bool {
		return (r.Quality == api.QualityOK || r.Quality == api.QualitySuspect) && finite(r.Value)
	}
	tempOK, rhOK := usable(s.Temp), usable(s.RH)
	if !tempOK && !rhOK {
		p.keypad = nil
		p.screen = p.refReturnScreen()
		p.flash("No sensor is reading, so there is nothing to compare. Fix the wiring first.", true, 6*time.Second)
		return
	}
	back := func() {
		p.keypad = nil
		p.screen = p.refReturnScreen()
	}
	askRH := func(t *float64) {
		if !rhOK {
			p.confirmRefCheck(t, nil)
			return
		}
		p.keypad = &keypad{title: "Separate hygrometer reads, %RH. Leave empty if none.", header: "SENSOR CHECK", maxLen: 5, dot: true, okLabel: "NEXT", onBack: back, onOK: func(d string) {
			if d == "" {
				if t == nil {
					p.flash("Enter at least one reference reading.", true, 4*time.Second)
					return
				}
				p.confirmRefCheck(t, nil)
				return
			}
			v, err := strconv.ParseFloat(d, 64)
			if err != nil || v < 0 || v > 100 {
				p.keypad.digits = ""
				p.flash("Enter a humidity from 0 to 100.", true, 4*time.Second)
				return
			}
			p.confirmRefCheck(t, &v)
		}}
		p.screen = scrKeypad
	}
	if !tempOK {
		askRH(nil)
		return
	}
	p.keypad = &keypad{title: "Separate thermometer reads, °C. Leave empty if none.", header: "SENSOR CHECK", maxLen: 5, dot: true, okLabel: "NEXT", onBack: back, onOK: func(d string) {
		if d == "" {
			if !rhOK {
				p.flash("Enter the reference reading.", true, 4*time.Second)
				return
			}
			askRH(nil)
			return
		}
		v, err := strconv.ParseFloat(d, 64)
		if err != nil || v < 0 || v > 60 {
			p.keypad.digits = ""
			p.flash("Enter a temperature from 0 to 60.", true, 4*time.Second)
			return
		}
		askRH(&v)
	}}
	p.screen = scrKeypad
	p.dirty = true
}

func (p *Panel) refReturnScreen() screenID {
	if p.installerOpen {
		return scrInstaller
	}
	if len(p.snap.Active) > 0 {
		return scrAlarms
	}
	return scrMore
}

// confirmRefCheck shows sensor beside reference and says what recording it will do.
func (p *Panel) confirmRefCheck(t, rh *float64) {
	p.keypad = nil
	p.screen = p.refReturnScreen()
	var lines, after []string
	pass := true
	if t != nil {
		d := p.snap.Temp.Value - *t
		lines = append(lines, fmt.Sprintf("Temperature: sensor %s, reference %s, difference %s °C.", fmtFloat(p.snap.Temp.Value, 1), fmtFloat(*t, 1), fmtFloat(math.Abs(d), 1)))
		pass = pass && math.Abs(d) <= refTolTempC
		after = append(after, fmtFloat(math.Abs(d), 1)+" °C")
	}
	if rh != nil {
		d := p.snap.RH.Value - *rh
		lines = append(lines, fmt.Sprintf("Humidity: sensor %s, reference %s, difference %s %%.", fmtFloat(p.snap.RH.Value, 0), fmtFloat(*rh, 0), fmtFloat(math.Abs(d), 0)))
		pass = pass && math.Abs(d) <= refTolRHPct
		after = append(after, fmtFloat(math.Abs(d), 0)+" %")
	}
	outcome := fmt.Sprintf("Within %.1f °C and %.0f %% this clears a reading in doubt and control trusts the sensor again.", refTolTempC, refTolRHPct)
	if !pass {
		outcome = "This differs too much: it will be recorded and raise a critical alarm, and the sensor stays in doubt."
	}
	cmd := api.Command{Kind: api.CmdRefCheck, Reference: &api.Reference{TempC: t, RHPct: rh}}
	done := "Check recorded, difference " + strings.Join(after, " and ") + ". See Alarms for the result."
	p.ask(dialog{id: "refcheck", title: "Record this check?", body: strings.Join(lines, " ") + " " + outcome + " Only enter what a separate instrument in the cabinet really reads.",
		confirm: "HOLD 2 s: RECORD", hold: p.cfg.ConfirmHold, danger: !pass, fn: func() { p.command(cmd, done) }})
}
