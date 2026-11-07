package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Audit is the integrity result of reading the journals.
type Audit struct {
	LogRecords   int
	LogBadLines  int
	LogSeqGaps   int
	LogRepeats   int
	EventRecords int
	EventBadLine int
	EventSeqGaps int
	FirstMono    float64
	LastPowered  float64
	Boots        int
	Alarms       int
	Clears       int
	StateSlotsOK int
	StateLatest  State
	HaveState    bool
}

// AuditStore reads back everything on storage and reports its integrity.
func AuditStore(fs FS) (Audit, error) {
	var a Audit
	var last uint64
	var firstErr error
	bad, err := readJournal(fs, prefixLog, func(js []byte) {
		var l LogRecord
		if json.Unmarshal(js, &l) != nil {
			a.LogBadLines++
			return
		}
		a.LogRecords++
		switch {
		case last == 0 || l.Seq == last+1:
		case l.Seq > last+1:
			a.LogSeqGaps++
		default:
			a.LogRepeats++
		}
		if l.Seq > last {
			last = l.Seq
		}
		a.LastPowered = l.Powered
	})
	a.LogBadLines += bad
	if err != nil {
		firstErr = err
	}
	last = 0
	boots := map[uint64]bool{}
	bad, err = readJournal(fs, prefixEv, func(js []byte) {
		var e Event
		if json.Unmarshal(js, &e) != nil {
			a.EventBadLine++
			return
		}
		a.EventRecords++
		if last != 0 && e.Seq > last+1 {
			a.EventSeqGaps++
		}
		if e.Seq > last {
			last = e.Seq
		}
		boots[e.Boot] = true
		switch e.Type {
		case EvAlarm:
			a.Alarms++
		case EvClear:
			a.Clears++
		}
	})
	a.EventBadLine += bad
	if err != nil && firstErr == nil {
		firstErr = err
	}
	a.Boots = len(boots)
	var bestSeq uint64
	for _, name := range []string{slotA, slotB} {
		b, err := fs.ReadFile(name)
		if err != nil {
			continue
		}
		seq, js, ok := decodeSlot(b)
		if !ok {
			continue
		}
		var st State
		if json.Unmarshal(js, &st) != nil {
			continue
		}
		a.StateSlotsOK++
		if !a.HaveState || seq > bestSeq {
			bestSeq, a.StateLatest, a.HaveState = seq, st, true
		}
	}
	return a, firstErr
}

// ExportProcessLog writes the process log as CSV: the record of what the cabinet
// did. Every row carries whether its time could be trusted and its boot number,
// so a gap or a wrong clock is visible in the record itself. Bad lines are
// skipped and counted in the trailing comment line.
func ExportProcessLog(fs FS, w io.Writer) error {
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	fmt.Fprintln(bw, "seq,boot,mono_s,powered_s,wall_time,clock_trusted,temp_c,temp_min,temp_max,temp_q,rh_pct,rh_min,rh_max,rh_q,air_ms,air_q,weight_g,weight_q,set_t,set_rh,set_air,cool_frac,heat_frac,humid_frac,dehum_frac,defrost_frac,door_frac,fan_pct,fan_rpm,active_alarms,run_state,recipe,phase,phase_elapsed_s,total_elapsed_s,start_weight_g,degree_hours_above_15_6")
	var last uint64
	bad, err := readJournal(fs, prefixLog, func(js []byte) {
		var l LogRecord
		if json.Unmarshal(js, &l) != nil || l.Seq <= last {
			return
		}
		last = l.Seq
		fmt.Fprintf(bw, "%d,%d,%.0f,%.0f,%s,%v,%.2f,%.2f,%.2f,%s,%.2f,%.2f,%.2f,%s,%.3f,%s,%.1f,%s,%.2f,%.2f,%.2f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.1f,%.0f,%d,%s,%s,%d,%.0f,%.0f,%.1f,%.2f\n",
			l.Seq, l.Boot, l.Mono, l.Powered, l.Wall.UTC().Format("2006-01-02T15:04:05Z"), l.ClkOK, l.T, l.TMin, l.TMax, l.TQ, l.RH, l.RHMin, l.RHMax, l.RHQ, l.Air, l.AirQ, l.Wt, l.WtQ,
			l.SetT, l.SetRH, l.SetAir, l.Cool, l.Heat, l.Humid, l.Dehum, l.Def, l.Door, l.Fan, l.RPM, l.AlarmN, l.Run, l.Recipe, l.Phase, l.PhEl, l.TotEl, l.Start, l.DH)
	})
	if bad > 0 {
		fmt.Fprintf(bw, "# %d damaged lines skipped\n", bad)
	}
	return err
}

// ExportEvents writes the event journal as tab-separated text.
func ExportEvents(fs FS, w io.Writer) error {
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	fmt.Fprintln(bw, "seq\tboot\tmono_s\twall_time\tclock_trusted\ttype\tcode\tseverity\tpeak\tlimit\tunit\tduration_s\tmessage")
	var last uint64
	bad, err := readJournal(fs, prefixEv, func(js []byte) {
		var e Event
		if json.Unmarshal(js, &e) != nil || e.Seq <= last {
			return
		}
		last = e.Seq
		msg := strings.TrimSpace(e.Title + " " + e.Msg)
		fmt.Fprintf(bw, "%d\t%d\t%.0f\t%s\t%v\t%s\t%s\t%s\t%.2f\t%.2f\t%s\t%.0f\t%s\n", e.Seq, e.Boot, e.Mono, e.Wall.UTC().Format("2006-01-02T15:04:05Z"), e.ClkOK, e.Type, e.Code, e.Sev, e.Peak, e.Limit, e.Unit, e.Dur, msg)
	})
	if bad > 0 {
		fmt.Fprintf(bw, "# %d damaged lines skipped\n", bad)
	}
	return err
}
