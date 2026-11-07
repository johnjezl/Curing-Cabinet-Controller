package sim

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
)

// RestartHeader is the per-outage CSV header of RestartRun.
const RestartHeader = "idx,plant,rtc,t_cut_s,true_outage_s,detected,duration_ok,reported_outage_s,duration_err_s,temp_known,reported_on_return_c,true_air_on_return_c,reported_peak_c,true_air_peak_after_c,recover_s,first_comp_on_after_boot_s,setpoint_ok,limits_ok,cal_ok,phase_before,phase_after,elapsed_before_s,elapsed_after_s,elapsed_lost_s,program_active,history_before,history_after,journal_alarms,traced_alarms,clock_trusted_at_boot"

// RestartResult summarises a run of random power cuts.
type RestartResult struct {
	Metrics Metrics
	Audit   core.Audit
}

// RestartRun runs a cure and cuts power at random instants for random lengths,
// with storage damage from the power-loss model on every cut. Per-outage rows go
// to w. All figures compare what the controller reports against plant truth.
func RestartRun(plant Params, cuts int, seed uint64, w io.Writer, events io.Writer) (RestartResult, error) {
	rng := rand.New(rand.NewPCG(seed, 0xdead))
	r, err := NewRun(Options{Plant: plant, KeepRows: false, SampleSec: 60, Events: events, Seed: seed})
	if err != nil {
		return RestartResult{}, err
	}
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	fmt.Fprintln(bw, RestartHeader)
	r.SetCal()
	lim := core.DefaultLimits()
	lim.TempHighDelta = 3.1 // a non-default value, to see it survive
	_ = r.Core.Do(api.Command{Kind: api.CmdSetLimits, Limits: &lim})
	cal := api.Calibration{TempOffsetC: 0.2, RHOffsetPct: -0.5, ScaleZero: int32(plant.ScaleTareCounts), ScaleCountsPG: plant.ScaleCountsPerG}
	_ = r.Core.Do(api.Command{Kind: api.CmdSetCal, Calibration: &cal})
	if err := r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: "whole-muscle"}); err != nil {
		return RestartResult{}, err
	}
	r.Advance(12 * h)
	type agg struct {
		n, det, durOK, setOK, limOK, calOK, tempKnown, delayBad int
		lostMax, lostSum, durErrMax                             float64
		minFirstOn                                              float64
		peakErrMax, returnErrMax                                float64
	}
	var ag agg
	ag.minFirstOn = math.Inf(1)
	trueRun := 0.0
	lastElapsed := r.Core.Probe().TotalElapsed
	_ = trueRun
	for i := 0; i < cuts; i++ {
		// Random instant: run a random time first.
		r.Advance(math.Round(300 + rng.Float64()*rng.Float64()*12*h))
		snap := r.Core.Snapshot()
		before := r.Core.Probe()
		histBefore := len(snap.History)
		tracedAlarms := 0
		for _, e := range r.Evs {
			if e.Kind == "alarm" {
				tracedAlarms++
			}
		}
		// Log-uniform outage, 5 s to 24 h.
		dur := math.Round(math.Exp(rng.Float64()*(math.Log(24*h)-math.Log(5)) + math.Log(5)))
		ntp := -1.0
		if rng.IntN(2) == 0 {
			ntp = 30 + rng.Float64()*h
		}
		tCut := r.Clock.VT
		if err := r.PowerLoss(dur, ntp); err != nil {
			return RestartResult{}, err
		}
		trustBoot := r.Clock.Trusted()
		bootT := r.Clock.VT
		// Run until the outage record is complete or for up to 2 h.
		var trueOnReturn = r.Plant.TAir
		truePeak := r.Plant.TAir
		firstOn := math.NaN()
		snapAfter := r.Core.Snapshot()
		for j := 0; j < int(2*h); j++ {
			r.Tick()
			if r.Plant.TAir > truePeak && snapAfter.Health.LastOutage.RecoverSec < 0 {
				truePeak = r.Plant.TAir
			}
			if math.IsNaN(firstOn) && r.Plant.CmdCool {
				firstOn = r.Clock.VT - bootT
			}
			if j%10 == 0 || j == int(2*h)-1 {
				snapAfter = r.Core.Snapshot()
				if snapAfter.Health.LastOutage.RecoverSec >= 0 && !math.IsNaN(firstOn) {
					break
				}
			}
		}
		o := snapAfter.Health.LastOutage
		afterP := r.Core.Probe()
		bad := func(cond bool) int {
			if cond {
				return 1
			}
			return 0
		}
		setOK := snapAfter.Setpoint == before.Set || afterP.Phase != before.Phase
		limOK := snapAfter.Limits == lim
		calOK := snapAfter.Calibration == cal
		// The progress lost is what the record has at reboot, compared with before
		// the cut. Elapsed after the check run is corrected by powered time since boot.
		elapsedAfter := afterP.TotalElapsed - (r.Clock.VT - bootT)
		lost := before.TotalElapsed - elapsedAfter
		a, _ := core.AuditStore(r.FS)
		durOK := o.Detected && o.DurationOK
		rep := o.Duration.Seconds()
		durErr := math.NaN()
		if durOK {
			durErr = rep - dur
		}
		fmt.Fprintf(bw, "%d,%s,%v,%.0f,%.0f,%v,%v,%.0f,%.1f,%v,%.2f,%.2f,%.2f,%.2f,%.0f,%.0f,%v,%v,%v,%d,%d,%.0f,%.0f,%.0f,%v,%d,%d,%d,%d,%v\n",
			i, plant.Name, plant.RTC, tCut, dur, o.Detected, durOK, rep, durErr, o.TempKnown, o.TempOnBackC, trueOnReturn, o.PeakTempC, truePeak, o.RecoverSec, firstOn,
			setOK, limOK, calOK, before.Phase, afterP.Phase, before.TotalElapsed, elapsedAfter, lost, afterP.RunState != api.RunIdle, histBefore, len(snapAfter.History), a.Alarms, tracedAlarms, trustBoot)
		ag.n++
		ag.det += bad(o.Detected)
		ag.durOK += bad(durOK)
		ag.setOK += bad(setOK)
		ag.limOK += bad(limOK)
		ag.calOK += bad(calOK)
		ag.tempKnown += bad(o.TempKnown)
		if !math.IsNaN(firstOn) {
			ag.minFirstOn = math.Min(ag.minFirstOn, firstOn)
		}
		ag.lostMax = math.Max(ag.lostMax, lost)
		ag.lostSum += lost
		if durOK {
			ag.durErrMax = math.Max(ag.durErrMax, math.Abs(durErr))
		}
		if o.TempKnown {
			ag.peakErrMax = math.Max(ag.peakErrMax, math.Abs(o.PeakTempC-truePeak))
			ag.returnErrMax = math.Max(ag.returnErrMax, math.Abs(o.TempOnBackC-trueOnReturn))
		}
		lastElapsed = afterP.TotalElapsed
	}
	_ = lastElapsed
	r.Flush()
	aud, err := core.AuditStore(r.FS)
	m := Metrics{
		"cuts":                    float64(ag.n),
		"outages_detected":        float64(ag.det),
		"duration_reported":       float64(ag.durOK),
		"setpoint_survived":       float64(ag.setOK),
		"limits_survived":         float64(ag.limOK),
		"calibration_survived":    float64(ag.calOK),
		"temp_on_return_recorded": float64(ag.tempKnown),
		"min_first_comp_on_s":     infNaN(ag.minFirstOn),
		"progress_lost_max_s":     ag.lostMax,
		"progress_lost_total_s":   ag.lostSum,
		"duration_err_max_s":      ag.durErrMax,
		"peak_err_max_c":          ag.peakErrMax,
		"return_temp_err_max_c":   ag.returnErrMax,
		"log_records":             float64(aud.LogRecords),
		"log_bad_lines":           float64(aud.LogBadLines),
		"log_seq_gaps":            float64(aud.LogSeqGaps),
		"event_records":           float64(aud.EventRecords),
		"event_bad_lines":         float64(aud.EventBadLine),
		"event_seq_gaps":          float64(aud.EventSeqGaps),
		"boots":                   float64(aud.Boots),
	}
	return RestartResult{Metrics: m, Audit: aud}, err
}

// CutMatrixHeader is the per-cut CSV header of CutMatrix.
const CutMatrixHeader = "op,seed,fired,durable_log_lines,recovered_log_lines,durable_event_lines,recovered_event_lines,state_recovered,setpoint_recovered,boot_ok,control_ran,comp_held_at_boot,comp_later_ran"

// CutMatrix cuts power after every storage operation in a short run, in turn,
// and for each cut checks what survived. Durable counts are read just before the
// cut is applied, so "recovered >= durable" is a direct check of the promise.
func CutMatrix(plant Params, ops int, seed uint64, w io.Writer) (Metrics, error) {
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	fmt.Fprintln(bw, CutMatrixHeader)
	m := Metrics{"cuts": 0, "log_lines_lost": 0, "event_lines_lost": 0, "state_missing_after_first_save": 0, "boot_failed": 0, "control_failed": 0, "comp_not_held": 0, "setpoint_wrong": 0}
	for op := 1; op <= ops; op++ {
		rng := rand.New(rand.NewPCG(seed, uint64(op)))
		r, err := NewRun(Options{Plant: plant, SampleSec: 60, Seed: seed})
		if err != nil {
			return m, err
		}
		r.FS.ArmCut(op)
		r.SetCal()
		sp1 := api.Setpoints{TempC: 12, RHPct: 74, AirMS: 1.1}
		sp2 := api.Setpoints{TempC: 11, RHPct: 73, AirMS: 1.3}
		_ = r.Core.Do(api.Command{Kind: api.CmdSetSetpoint, Setpoint: &sp1})
		for i := 0; i < 3*3600 && !r.FS.Cut(); i++ {
			r.Tick()
			if i == 1800 {
				_ = r.Core.Do(api.Command{Kind: api.CmdSetSetpoint, Setpoint: &sp2})
			}
		}
		fired := r.FS.Cut()
		durLog, durEv := 0, 0
		for _, n := range r.FS.FileNames() {
			g, _ := core.CountGoodLines(r.FS.DurableFile(n))
			if strings.HasPrefix(n, "process-") {
				durLog += g
			}
			if strings.HasPrefix(n, "events-") {
				durEv += g
			}
		}
		slotDurable := core.DecodeSlotOK(r.FS.DurableFile("state.a")) || core.DecodeSlotOK(r.FS.DurableFile("state.b"))
		r.Plant.PowerOff()
		r.FS.PowerCut(rng)
		r.dead = true
		r.Advance(60)
		r.Plant.PowerOn()
		r.dead = false
		r.Clock.Reboot(-1)
		r.haveL = false
		bootErr := r.boot()
		bootOK := bootErr == nil
		a, _ := core.AuditStore(r.FS)
		// Count what came back, from before this boot wrote anything new: reboot
		// adds one event and one or two state writes and no log line.
		recLog, recEv := a.LogRecords, a.EventRecords-1
		if !bootOK {
			m["boot_failed"]++
		}
		ran := false
		held := true
		compLater := false
		sn := r.Core.Snapshot().Setpoint
		for i := 0; i < 1200; i++ {
			r.Tick()
			if r.Plant.CmdCool {
				compLater = true
				if float64(i) < 239 {
					held = false
				}
			}
			if r.Plant.FanDuty > 0 {
				ran = true
			}
		}
		spOK := sn == sp1 || sn == sp2 || sn == (api.Setpoints{TempC: 13, RHPct: 75, AirMS: 1.0})
		fmt.Fprintf(bw, "%d,%d,%v,%d,%d,%d,%d,%v,%v,%v,%v,%v,%v\n", op, seed, fired, durLog, recLog, durEv, recEv, a.HaveState, spOK, bootOK, ran, held, compLater)
		m["cuts"]++
		if recLog < durLog {
			m["log_lines_lost"] += float64(durLog - recLog)
		}
		if recEv < durEv {
			m["event_lines_lost"] += float64(durEv - recEv)
		}
		if slotDurable && !a.HaveState {
			m["state_missing_after_first_save"]++
		}
		if !ran {
			m["control_failed"]++
		}
		if !held {
			m["comp_not_held"]++
		}
		if !spOK {
			m["setpoint_wrong"]++
		}
	}
	return m, nil
}

// CureOptions configure a whole cure.
type CureOptions struct {
	Plant     Params
	Recipe    string
	MaxDays   float64
	Outages   int     // random power cuts during the cure
	DoorEvery float64 // seconds between door openings for inspection; 0 means none
	Seed      uint64
	SampleSec float64
	CSV       io.Writer
	Events    io.Writer
	Log       io.Writer // process log export
	EvJournal io.Writer // event journal export
	Final     io.Writer // final snapshot text
}

// RunCure runs a recipe to its end or to MaxDays.
func RunCure(o CureOptions) (Metrics, error) {
	if o.SampleSec == 0 {
		o.SampleSec = 60
	}
	r, err := NewRun(Options{Plant: o.Plant, KeepRows: true, SampleSec: o.SampleSec, CSV: o.CSV, Events: o.Events, Seed: o.Seed})
	if err != nil {
		return nil, err
	}
	r.SetCal()
	if err := r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: o.Recipe}); err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewPCG(o.Seed, 0xc0de))
	maxT := o.MaxDays * 86400
	cuts := make([]float64, o.Outages)
	for i := range cuts {
		cuts[i] = rng.Float64() * maxT * 0.5
	}
	nextDoor := o.DoorEvery
	nextCut := func() float64 {
		best := math.Inf(1)
		for _, c := range cuts {
			if c > r.Clock.VT && c < best {
				best = c
			}
		}
		return best
	}
	cutT := nextCut()
	finishedAt := math.NaN()
	var doorUntil float64
	for r.Clock.VT < maxT {
		r.Tick()
		t := r.Clock.VT
		if o.DoorEvery > 0 && t >= nextDoor {
			r.Plant.DoorIsOpen = true
			doorUntil = t + 45
			nextDoor = t + o.DoorEvery
			r.Note("door opened for inspection")
		}
		if r.Plant.DoorIsOpen && t >= doorUntil {
			r.Plant.DoorIsOpen = false
			r.Note("door closed")
		}
		if t >= cutT {
			dur := math.Exp(rng.Float64()*(math.Log(12*h)-math.Log(30)) + math.Log(30))
			if err := r.PowerLoss(math.Round(dur), 600); err != nil {
				return nil, err
			}
			cutT = nextCut()
		}
		if math.IsNaN(finishedAt) && r.last.RunState == api.RunFinished {
			finishedAt = r.Clock.VT
			// Hold a further day to show it keeps holding conditions.
			maxT = math.Min(maxT, r.Clock.VT+86400)
		}
	}
	r.Flush()
	snap := r.Core.Snapshot()
	m := Metrics{}
	m["virtual_days"] = r.Clock.VT / 86400
	m["finished_at_days"] = finishedAt / 86400
	m["loss_true_pct"] = r.Plant.WaterLossPct()
	m["loss_controller_pct"] = snap.Program.WeightLossPct
	m["loss_err_pct"] = snap.Program.WeightLossPct - r.Plant.WaterLossPct()
	m["outages_seen"] = float64(snap.Program.OutageCount)
	m["boots"] = float64(r.Boots)
	m["degree_hours_above_15_6"] = snap.Program.DegreeHoursAbove156
	if snap.Program.Verify != "" {
		m["verify_text_present"] = 1
	} else {
		m["verify_text_present"] = 0
	}
	starts, runS := r.Core.CounterSnapshot()
	m["compressor_starts_total"] = float64(starts)
	m["compressor_run_h"] = runS / 3600
	m["heater_relay_closures"] = float64(r.Plant.HeatCycles())
	m["humid_relay_closures"] = float64(r.Plant.HumidCycles())
	m["energy_kwh"] = r.Plant.EnergyWh() / 1000
	days := r.Clock.VT / 86400
	m["compressor_starts_per_day"] = float64(starts) / days
	m["heater_closures_per_day"] = float64(r.Plant.HeatCycles()) / days
	m["humid_closures_per_day"] = float64(r.Plant.HumidCycles()) / days
	// Per-phase statistics from the rows.
	maxPhase := 0
	for i := range r.Rows {
		if r.Rows[i].Phase > maxPhase {
			maxPhase = r.Rows[i].Phase
		}
	}
	alarms := map[string]int{}
	for _, e := range r.Evs {
		if e.Kind == "alarm" {
			alarms[e.Code]++
		}
	}
	for code, n := range alarms {
		m["alarm_count_"+code] = float64(n)
	}
	// Alarm contact up to the recipe end, with nobody attending, exactly, from the event
	// log of physical contact changes, so that short signals between samples count.
	endT := r.Clock.VT
	if !math.IsNaN(finishedAt) {
		endT = finishedAt
	}
	openS, opens := 0.0, 0.0
	wasOpen := false
	lastT := 0.0
	for _, e := range r.Evs {
		if e.T > endT {
			break
		}
		switch {
		case e.Kind == "scenario" && strings.HasPrefix(e.Msg, "power lost"):
			if wasOpen {
				openS += e.T - lastT
			}
			wasOpen, lastT = false, e.T
		case e.Kind == "output" && e.Code == "alarm_signal":
			if wasOpen {
				openS += e.T - lastT
			}
			wasOpen = e.Msg == "on"
			if wasOpen {
				opens++
			}
			lastT = e.T
		}
	}
	if wasOpen {
		openS += endT - lastT
	}
	if endT > 0 {
		m["relay_on_pct_to_finish"] = openS / endT * 100
		m["relay_open_events_to_finish"] = opens
	}
	frost := 0.0
	for ph := 0; ph <= maxPhase; ph++ {
		var T, RH, P, A []float64
		var setT, setRH, setA float64
		first := math.NaN()
		last := 0.0
		for i := range r.Rows {
			x := &r.Rows[i]
			frost = math.Max(frost, x.FrostG)
			// Up to the recipe's end only; the day of holding after it is not a phase.
			if x.Phase != ph || !x.Powered || x.Run == "none" || (!math.IsNaN(finishedAt) && x.T > finishedAt) {
				continue
			}
			if math.IsNaN(first) {
				first = x.T
			}
			last = x.T
			// Skip the first 12 h of a phase, which is transition.
			if x.T-first < 12*h {
				continue
			}
			T = append(T, x.TrueT)
			RH = append(RH, x.TrueRH)
			P = append(P, x.TrueProd)
			A = append(A, x.TrueAirMS)
			setT, setRH, setA = x.SetT, x.SetRH, x.SetAir
		}
		k := fmt.Sprintf("phase%d_", ph)
		m[k+"days"] = (last - first) / 86400
		if len(T) > 0 {
			m[k+"t_mean_err"] = mean(T) - setT
			m[k+"t_p1_err"] = Percentile(T, 1) - setT
			m[k+"t_p99_err"] = Percentile(T, 99) - setT
			m[k+"prod_mean_err"] = mean(P) - setT
			m[k+"prod_p1_err"] = Percentile(P, 1) - setT
			m[k+"prod_p99_err"] = Percentile(P, 99) - setT
			m[k+"rh_mean_err"] = mean(RH) - setRH
			m[k+"rh_p1_err"] = Percentile(RH, 1) - setRH
			m[k+"rh_p99_err"] = Percentile(RH, 99) - setRH
			m[k+"air_mean_err"] = mean(A) - setA
		}
	}
	m["frost_max_g"] = frost
	aud, _ := core.AuditStore(r.FS)
	m["log_records"] = float64(aud.LogRecords)
	m["log_bad_lines"] = float64(aud.LogBadLines)
	m["log_seq_gaps"] = float64(aud.LogSeqGaps)
	m["event_records"] = float64(aud.EventRecords)
	if o.Log != nil {
		_ = core.ExportProcessLog(r.FS, o.Log)
	}
	if o.EvJournal != nil {
		_ = core.ExportEvents(r.FS, o.EvJournal)
	}
	if o.Final != nil {
		fmt.Fprintf(o.Final, "state=%s recipe=%s phase=%s total_elapsed=%s weight_loss_pct=%.2f target=%.1f\nverify=%q\n",
			snap.Program.State, snap.Program.RecipeID, snap.Program.PhaseName, snap.Program.TotalElapsed.Round(time.Second), snap.Program.WeightLossPct, snap.Program.TargetLossPct, snap.Program.Verify)
		for _, a := range snap.Active {
			fmt.Fprintf(o.Final, "active: %s %s %s\n", a.Code, a.Severity, a.Title)
		}
	}
	return m, nil
}
