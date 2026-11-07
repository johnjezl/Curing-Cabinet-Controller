package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

func cmdCure(args []string) error {
	fs := newFlags("cure")
	out := fs.String("out", "out/cure", "output directory")
	recipe := fs.String("recipe", "ferment-dry", "recipe id")
	plant := fs.String("plant", "base", "plant")
	days := fs.Float64("days", 120, "maximum virtual days")
	outages := fs.Int("outages", 6, "random power cuts during the cure")
	door := fs.Float64("door-hours", 24*3, "hours between door openings for inspection; 0 for none")
	seed := fs.Uint64("seed", 7, "seed")
	sample := fs.Float64("sample", 60, "CSV period in s")
	sweep := fs.Int("sweep", 36, "sweep size the plant name refers to")
	_ = fs.Parse(args)
	p, err := findPlant(*plant, *sweep)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	open := func(n string) (*os.File, error) { return os.Create(filepath.Join(*out, n)) }
	csF, err := open("samples.csv.gz")
	if err != nil {
		return err
	}
	defer csF.Close()
	cs := gzip.NewWriter(csF)
	defer cs.Close()
	ev, _ := open("events.log")
	defer ev.Close()
	lgF, _ := open("process_log.csv.gz")
	defer lgF.Close()
	lg := gzip.NewWriter(lgF)
	defer lg.Close()
	ej, _ := open("event_journal.tsv")
	defer ej.Close()
	fin, _ := open("final_state.txt")
	defer fin.Close()
	m, err := sim.RunCure(sim.CureOptions{Plant: p, Recipe: *recipe, MaxDays: *days, Outages: *outages, DoorEvery: *door * 3600, Seed: *seed, SampleSec: *sample,
		CSV: cs, Events: ev, Log: lg, EvJournal: ej, Final: fin})
	if err != nil {
		return err
	}
	js, _ := m.JSON()
	return os.WriteFile(filepath.Join(*out, "metrics.json"), js, 0o644)
}

func cmdRestart(args []string) error {
	fs := newFlags("restart")
	out := fs.String("out", "out/restart", "output directory")
	cuts := fs.Int("cuts", 60, "power cuts")
	plant := fs.String("plant", "base", "plant")
	seed := fs.Uint64("seed", 11, "seed")
	rtc := fs.String("rtc", "yes", "wall clock survives power loss: yes or no")
	sweep := fs.Int("sweep", 36, "sweep size the plant name refers to")
	_ = fs.Parse(args)
	p, err := findPlant(*plant, *sweep)
	if err != nil {
		return err
	}
	p.RTC = *rtc == "yes"
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	cs, err := os.Create(filepath.Join(*out, "outages.csv"))
	if err != nil {
		return err
	}
	defer cs.Close()
	ev, _ := os.Create(filepath.Join(*out, "events.log"))
	defer ev.Close()
	res, err := sim.RestartRun(p, *cuts, *seed, cs, ev)
	if err != nil {
		return err
	}
	js, _ := res.Metrics.JSON()
	return os.WriteFile(filepath.Join(*out, "metrics.json"), js, 0o644)
}

func cmdCutMatrix(args []string) error {
	fs := newFlags("cutmatrix")
	out := fs.String("out", "out/cutmatrix", "output directory")
	ops := fs.Int("ops", 300, "storage operations to cut after")
	seed := fs.Uint64("seed", 3, "seed")
	_ = fs.Parse(args)
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	cs, err := os.Create(filepath.Join(*out, "cuts.csv"))
	if err != nil {
		return err
	}
	defer cs.Close()
	m, err := sim.CutMatrix(sim.BasePlant(), *ops, *seed, cs)
	if err != nil {
		return err
	}
	js, _ := m.JSON()
	return os.WriteFile(filepath.Join(*out, "metrics.json"), js, 0o644)
}

// crashClock uses the real wall clock and a virtual monotonic clock.
type crashClock struct{ vt float64 }

func (c *crashClock) Now() time.Time      { return time.Now() }
func (c *crashClock) Mono() time.Duration { return time.Duration(c.vt * float64(time.Second)) }
func (c *crashClock) Trusted() bool       { return true }

// cmdCrashChild runs the controller on real files until it gets killed.
func cmdCrashChild(args []string) error {
	fs := newFlags("crashchild")
	dir := fs.String("dir", "", "state directory")
	_ = fs.Parse(args)
	ofs, err := core.NewOSFS(*dir)
	if err != nil {
		return err
	}
	pl := sim.NewPlant(sim.BasePlant())
	clk := &crashClock{}
	c, err := core.New(core.Options{Cabinet: pl.Cabinet(), Clock: clk, FS: ofs, Simulated: true})
	if err != nil {
		return err
	}
	snap := c.Snapshot()
	if snap.Program.State == api.RunIdle {
		k := api.Calibration{ScaleZero: int32(pl.P.ScaleTareCounts), ScaleCountsPG: pl.P.ScaleCountsPerG}
		_ = c.Do(api.Command{Kind: api.CmdSetCal, Calibration: &k})
		_ = c.Do(api.Command{Kind: api.CmdStart, RecipeID: "whole-muscle"})
	}
	js, _ := json.Marshal(map[string]any{"boot_state": snap.Program.State, "elapsed_s": snap.Program.TotalElapsed.Seconds(), "outage": snap.Health.LastOutage.Detected, "set_t": snap.Setpoint.TempC})
	fmt.Println(string(js))
	os.Stdout.Sync()
	for {
		pl.Step(1)
		clk.vt = pl.T
		c.Step()
	}
}

// cmdCrash kills a real process running on real files at random instants. That
// is a real test of torn process state; it does not lose the OS page cache, which
// only the rig's power-cut test can do.
func cmdCrash(args []string) error {
	fs := newFlags("crash")
	out := fs.String("out", "out/crash", "output directory")
	n := fs.Int("n", 30, "kills")
	seed := fs.Uint64("seed", 5, "seed")
	_ = fs.Parse(args)
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "crashtest")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	self, err := os.Executable()
	if err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(*seed, 9))
	cs, err := os.Create(filepath.Join(*out, "kills.csv"))
	if err != nil {
		return err
	}
	defer cs.Close()
	fmt.Fprintln(cs, "kill,run_ms,boot_state,elapsed_at_boot_s,outage_detected,set_t,log_records,log_bad_lines,log_seq_gaps,log_repeats,event_records,event_bad_lines,event_seq_gaps,state_slots_ok,boots_in_journal")
	last := -1.0
	regress, badState, undetected, notBooted := 0, 0, 0, 0
	for i := 0; i < *n; i++ {
		cmd := exec.Command(self, "crashchild", "-dir", tmp)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		if err := cmd.Start(); err != nil {
			return err
		}
		ms := 150 + rng.IntN(1500)
		time.Sleep(time.Duration(ms) * time.Millisecond)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		var boot struct {
			BootState string  `json:"boot_state"`
			Elapsed   float64 `json:"elapsed_s"`
			Outage    bool    `json:"outage"`
			SetT      float64 `json:"set_t"`
		}
		line := strings.SplitN(buf.String(), "\n", 2)[0]
		// A child killed before it had printed its boot line says nothing about resuming.
		booted := json.Unmarshal([]byte(line), &boot) == nil
		if !booted {
			fmt.Fprintf(cs, "%d,%d,killed_before_boot_line,,,,,,,,,,,,\n", i, ms)
			notBooted++
			continue
		}
		ofs, _ := core.NewOSFS(tmp)
		a, _ := core.AuditStore(ofs)
		if i > 0 && boot.BootState != "active" {
			badState++
		}
		if i > 0 && !boot.Outage {
			undetected++
		}
		if boot.Elapsed < last {
			regress++
		}
		last = math.Max(last, boot.Elapsed)
		fmt.Fprintf(cs, "%d,%d,%s,%.0f,%v,%.1f,%d,%d,%d,%d,%d,%d,%d,%d,%d\n", i, ms, boot.BootState, boot.Elapsed, boot.Outage, boot.SetT, a.LogRecords, a.LogBadLines, a.LogSeqGaps, a.LogRepeats, a.EventRecords, a.EventBadLine, a.EventSeqGaps, a.StateSlotsOK, a.Boots)
	}
	ofs, _ := core.NewOSFS(tmp)
	a, _ := core.AuditStore(ofs)
	m := sim.Metrics{"kills": float64(*n), "killed_before_boot_line": float64(notBooted), "elapsed_regressions": float64(regress), "boots_not_resuming_recipe": float64(badState), "unclean_stop_not_detected": float64(undetected),
		"log_records": float64(a.LogRecords), "log_bad_lines": float64(a.LogBadLines), "log_seq_gaps": float64(a.LogSeqGaps), "event_bad_lines": float64(a.EventBadLine), "event_seq_gaps": float64(a.EventSeqGaps), "final_elapsed_s": last}
	js, _ := m.JSON()
	return os.WriteFile(filepath.Join(*out, "metrics.json"), js, 0o644)
}

// cmdVerify recomputes metrics from raw files only and reports any difference.
func cmdVerify(args []string) error {
	fs := newFlags("verify")
	dir := fs.String("dir", "evidence/a", "evidence directory")
	_ = fs.Parse(args)
	checked, diffs := 0, 0
	err := filepath.Walk(*dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "spec.json" {
			return err
		}
		d := filepath.Dir(path)
		var sp sim.Spec
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &sp); err != nil {
			return err
		}
		cf, err := os.Open(filepath.Join(d, "samples.csv.gz"))
		if err != nil {
			return err
		}
		defer cf.Close()
		gz, err := gzip.NewReader(cf)
		if err != nil {
			return err
		}
		rows, err := sim.ReadRows(gz)
		if err != nil {
			return fmt.Errorf("%s: %w", d, err)
		}
		ef, err := os.Open(filepath.Join(d, "events.log"))
		if err != nil {
			return err
		}
		defer ef.Close()
		evs, err := sim.ReadEvs(ef)
		if err != nil {
			return fmt.Errorf("%s: %w", d, err)
		}
		mb, err := os.ReadFile(filepath.Join(d, "metrics.json"))
		if err != nil {
			return err
		}
		var stored map[string]*float64
		if err := json.Unmarshal(mb, &stored); err != nil {
			return err
		}
		m := sim.Compute(sp, rows, evs)
		checked++
		for k, sv := range stored {
			v, ok := m[k]
			switch {
			case !ok:
				diffs++
				fmt.Printf("DIFF %s %s: missing on recompute\n", d, k)
			case sv == nil:
				if !math.IsNaN(v) {
					diffs++
					fmt.Printf("DIFF %s %s: stored null, recomputed %v\n", d, k, v)
				}
			case math.IsNaN(v) || math.Abs(v-*sv) > tolerance(k, *sv):
				diffs++
				fmt.Printf("DIFF %s %s: stored %v, recomputed %v\n", d, k, *sv, v)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("runs_checked=%d differences=%d\n", checked, diffs)
	if diffs > 0 {
		return fmt.Errorf("%d differences", diffs)
	}
	return nil
}

// tolerance covers the 4 decimal places stored in metrics.json.
func tolerance(k string, v float64) float64 { return 1e-3 }
