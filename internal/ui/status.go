package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// Level is the overall state shown in the banner on every screen.
type Level int

const (
	LevelOK Level = iota
	LevelInfo
	LevelWarn
	LevelCrit
)

// codeCureFinished is the core's end-of-recipe alarm code.
const codeCureFinished = "CURE_FINISHED"

// Status is the one-glance answer: is it fine, and if not, what to do.
type Status struct {
	Level    Level
	Headline string // short, shouted
	Detail   string // what to do; shown on Home when set
	Alarm    *api.Alarm
	Extra    int // other active alarms
	Screen   screenID
}

// sortAlarms puts first what an operator must act on now: live critical conditions,
// then an open door (usually the cause of whatever else is out), then live warnings,
// then past events of any severity, newest first (the end of a cure is news; an old
// restart burst is not), and standing information last. An old event must never
// headline over a live excursion (integration defect 9).
func sortAlarms(in []api.Alarm) []api.Alarm {
	out := append([]api.Alarm(nil), in...)
	rank := func(a api.Alarm) int {
		switch {
		case a.Event:
			return 3
		case a.Severity == api.SevInfo:
			return 4
		case a.Severity == api.SevCritical:
			return 0
		case a.Code == "DOOR_OPEN":
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if a.Event {
			return a.Since.After(b.Since) // the newest event is the news
		}
		if a.Severity != b.Severity {
			return a.Severity == api.SevCritical
		}
		if a.Acked != b.Acked {
			return !a.Acked
		}
		return a.Since.Before(b.Since)
	})
	return out
}

type namedReading struct {
	name string
	r    api.Reading
}

func readings(s *api.Snapshot) []namedReading {
	return []namedReading{{"Temperature", s.Temp}, {"Humidity", s.RH}, {"Airflow", s.Air}, {"Weight", s.Weight}}
}

// computeStatus is pure: same snapshot, same answer. The word "safe" never
// appears: the panel reports the cabinet, not the product. In a simulation every
// headline says so, and an instruction is marked "If real:", what to do in a real cabinet,
// because it names real parts (ADR 0406).
func computeStatus(s *api.Snapshot, stalled bool, stallFor time.Duration) Status {
	st := cabinetStatus(s, stalled, stallFor)
	if s.Health.Simulated {
		st.Headline = "SIM " + st.Headline
		if st.Detail != "" && (st.Alarm != nil || stalled) {
			st.Detail = "If real: " + st.Detail
		}
	}
	return st
}

func cabinetStatus(s *api.Snapshot, stalled bool, stallFor time.Duration) Status {
	if stalled {
		return Status{Level: LevelCrit, Headline: "CONTROLLER NOT RESPONDING",
			Detail: fmt.Sprintf("No fresh data for %s. Values shown are not live. Check the cabinet with a separate thermometer and check power.", fmtDur(stallFor)),
			Screen: scrHealth}
	}
	if len(s.Active) > 0 {
		al := sortAlarms(s.Active)
		a := al[0]
		st := Status{Headline: a.Title, Detail: a.Action, Alarm: &a, Extra: len(al) - 1, Screen: scrAlarms}
		if a.Code == codeCureFinished {
			st.Screen = scrCure // the screen that holds the verify text and the way to end
		}
		switch a.Severity {
		case api.SevCritical:
			st.Level, st.Headline = LevelCrit, "ALARM: "+st.Headline
		case api.SevWarning:
			st.Level, st.Headline = LevelWarn, "WARNING: "+st.Headline
		default:
			st.Level = LevelInfo
		}
		return st
	}
	if st, ok := badReadingStatus(s); ok {
		return st
	}
	if s.Door.Quality == api.QualityOK && s.Door.Value >= 1 {
		return Status{Level: LevelWarn, Headline: "DOOR OPEN " + fmtAge(s.Door.AgeSec), Detail: "Close the cabinet door.", Screen: scrHome}
	}
	if !s.Health.LogOK || !s.Health.WatchdogOK {
		what := "LOGGING STOPPED"
		if !s.Health.WatchdogOK {
			what = "WATCHDOG FAULT"
		}
		return Status{Level: LevelWarn, Headline: what, Detail: "Open Health. Control is still running but this needs fixing soon.", Screen: scrHealth}
	}
	if s.Program.State == api.RunFinished {
		return Status{Level: LevelInfo, Headline: "RECIPE ENDED: VERIFY BEFORE USE", Detail: "Still holding conditions. Tap to read what you must verify.", Screen: scrCure}
	}
	if m := deviation(s); m != "" {
		return Status{Level: LevelWarn, Headline: m, Detail: "No alarm yet. Check the door and the product load; an alarm follows if it stays out of range.", Screen: scrTrend}
	}
	if s.Program.State == api.RunPaused {
		return Status{Level: LevelWarn, Headline: "CURE PAUSED: PHASE CLOCK STOPPED", Detail: "Holding setpoints. Time is not counting. Resume on the Cure screen.", Screen: scrCure}
	}
	if s.Outputs.Defrost {
		if readingGood(s.Temp) && s.Limits.TempHighDelta > 0 && s.Temp.Value-s.Setpoint.TempC > s.Limits.TempHighDelta {
			return Status{Level: LevelInfo, Headline: fmt.Sprintf("DEFROST: TEMP %.1f °C ABOVE SET", s.Temp.Value-s.Setpoint.TempC)}
		}
		return Status{Level: LevelOK, Headline: "DEFROSTING: TEMP MAY RISE BRIEFLY"}
	}
	if inRegBand(s) {
		return Status{Level: LevelOK, Headline: "OK: NO ALARMS, IN RANGE"}
	}
	return Status{Level: LevelOK, Headline: "OK: NO ALARMS"}
}

// blindAfter is how long a temperature or humidity reading may be unusable with no
// alarm before the panel itself treats that as critical. The core normally alarms
// well before this; this covers a core that does not.
const blindAfter = 5 * time.Minute

// badReadingStatus names every unusable reading. It only runs with no active alarm.
func badReadingStatus(s *api.Snapshot) (Status, bool) {
	var names []string
	first := ""
	critical := false
	for _, n := range readings(s) {
		bad := false
		note := ""
		switch n.r.Quality {
		case api.QualityStale:
			bad, note = true, "has not updated for "+fmtAge(n.r.AgeSec)
		case api.QualityFault:
			bad, note = true, "has no valid reading"
		case api.QualitySuspect:
			bad, note = true, "failed a check"
		case api.QualityOK:
			if !finite(n.r.Value) {
				bad, note = true, "sent an invalid number"
			}
		}
		if !bad {
			continue
		}
		names = append(names, n.name)
		if first == "" {
			first = n.name + " " + note
		}
		if (n.name == "Temperature" || n.name == "Humidity") && (!finite(n.r.AgeSec) || n.r.AgeSec >= blindAfter.Seconds() || !finite(n.r.Value)) {
			critical = true
		}
	}
	if len(names) == 0 {
		return Status{}, false
	}
	head := "CHECK " + strings.ToUpper(names[0]) + " SENSOR"
	if len(names) > 1 {
		head = "CHECK SENSORS: " + strings.ToUpper(strings.Join(names, ", "))
	}
	st := Status{Level: LevelWarn, Headline: head, Detail: first + ". Do not rely on it. Check the sensor and its cable; verify with a separate instrument.", Screen: scrHealth}
	if critical {
		st.Level = LevelCrit
		st.Detail = first + ", and the controller raised no alarm. Use a separate thermometer now. Check the sensor and its cable."
	}
	return st, true
}

// blindNote says what the outputs mean when a reading they follow is unusable.
func blindNote(s *api.Snapshot, stalled bool) (tempBlind, rhBlind bool, note string) {
	if stalled {
		return false, false, ""
	}
	// A reading in doubt still controls the compressor: it is not lost feedback.
	unusable := func(r api.Reading) bool {
		return r.Quality != api.QualityOff && !r.Doubt && !readingGood(r)
	}
	tempBlind, rhBlind = unusable(s.Temp), unusable(s.RH)
	switch {
	case tempBlind && rhBlind:
		note = "NO TEMP OR RH FEEDBACK"
	case tempBlind:
		note = "NO TEMP FEEDBACK"
	case rhBlind:
		note = "NO RH FEEDBACK"
	case s.Temp.Doubt || s.RH.Doubt:
		note = "READING IN DOUBT: HEAT, HUMID HELD"
	}
	return tempBlind, rhBlind, note
}

// deviation words an out-of-band reading that has not yet alarmed.
// deviation names a reading outside its alarm band before the alarm delay is over.
// It judges the few-minute average when the core gives one: a compressor run dips
// humidity by a dozen percent for a few minutes, and that is not a deviation to report
// (integration defect 7, re-test). The digits still show the instant value.
func deviation(s *api.Snapshot) string {
	val := func(r api.Reading) float64 {
		if r.Avg != 0 && finite(r.Avg) {
			return r.Avg
		}
		return r.Value
	}
	var parts []string
	if readingGood(s.Temp) && !s.Outputs.Defrost {
		d := val(s.Temp) - s.Setpoint.TempC
		if s.Limits.TempHighDelta > 0 && d > s.Limits.TempHighDelta {
			parts = append(parts, fmt.Sprintf("TEMP %.1f °C ABOVE SET", d))
		}
		if s.Limits.TempLowDelta > 0 && -d > s.Limits.TempLowDelta {
			parts = append(parts, fmt.Sprintf("TEMP %.1f °C BELOW SET", -d))
		}
	}
	if readingGood(s.RH) {
		d := val(s.RH) - s.Setpoint.RHPct
		if s.Limits.RHHighDelta > 0 && d > s.Limits.RHHighDelta {
			parts = append(parts, fmt.Sprintf("HUMIDITY %.0f %% ABOVE SET", d))
		}
		if s.Limits.RHLowDelta > 0 && -d > s.Limits.RHLowDelta {
			parts = append(parts, fmt.Sprintf("HUMIDITY %.0f %% BELOW SET", -d))
		}
	}
	return strings.Join(parts, ", ")
}

// attentionNeeded is what stops the screen dimming: anything that must be seen.
func attentionNeeded(s *api.Snapshot, stalled bool) bool {
	st := computeStatus(s, stalled, 0)
	return st.Level >= LevelWarn || st.Level == LevelInfo || len(s.Active) > 0 || s.Program.State == api.RunFinished
}
