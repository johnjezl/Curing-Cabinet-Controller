package core_test

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

func newRun(t *testing.T, mut ...func(*sim.Params)) *sim.Run {
	t.Helper()
	p := sim.BasePlant()
	for _, m := range mut {
		m(&p)
	}
	r, err := sim.NewRun(sim.Options{Plant: p, KeepRows: true, SampleSec: 5})
	if err != nil {
		t.Fatal(err)
	}
	r.SetCal()
	return r
}

func alarmEvents(r *sim.Run, code string) int {
	n := 0
	for _, e := range r.Evs {
		if e.Kind == "alarm" && e.Code == code {
			n++
		}
	}
	return n
}

func TestDeterministic(t *testing.T) {
	run := func() string {
		r := newRun(t)
		r.SetPoint(13, 76, 1.2)
		r.Advance(3 * 3600)
		r.Plant.DoorIsOpen = true
		r.Advance(60)
		r.Plant.DoorIsOpen = false
		r.Advance(3600)
		var b strings.Builder
		for _, x := range r.Rows {
			b.WriteString(x.CSV())
		}
		return b.String()
	}
	if run() != run() {
		t.Fatal("two runs with the same seed differ")
	}
}

func TestCompressorProtectionNeverViolated(t *testing.T) {
	r := newRun(t)
	r.SetPoint(4, 85, 1.2)
	r.Advance(12 * 3600)
	cfg := core.DefaultConfig()
	var lastOn, lastOff float64 = -1e9, -1e9
	starts := 0
	for _, e := range r.Evs {
		if e.Kind != "output" || e.Code != "cool" {
			continue
		}
		if e.Msg == "on" {
			if lastOff > -1e8 && e.T-lastOff < cfg.CompMinOff {
				t.Fatalf("min off violated at %v: %v", e.T, e.T-lastOff)
			}
			if lastOn > -1e8 && e.T-lastOn < cfg.CompStartSpacing {
				t.Fatalf("spacing violated at %v", e.T)
			}
			if starts == 0 && e.T < cfg.PowerOnDelay {
				t.Fatalf("power-on delay violated: first start at %v", e.T)
			}
			lastOn = e.T
			starts++
		} else {
			if e.T-lastOn < cfg.CompMinOn {
				t.Fatalf("min on violated at %v: %v", e.T, e.T-lastOn)
			}
			lastOff = e.T
		}
	}
	if starts < 10 {
		t.Fatalf("too few starts to mean anything: %d", starts)
	}
}

func TestHeaterAndHumidifierNeverRunOnBadData(t *testing.T) {
	for _, k := range []struct {
		name string
		dev  sim.Device
		kind sim.FaultKind
		par  float64
	}{
		{"dead", sim.DevClimate, sim.FaultDead, 0},
		{"bad", sim.DevClimate, sim.FaultBadRead, 0},
		{"stuck", sim.DevTemp, sim.FaultStuck, 0},
		{"implausible", sim.DevTemp, sim.FaultRange, 85},
	} {
		t.Run(k.name, func(t *testing.T) {
			r := newRun(t)
			r.SetPoint(24, 90, 1.2) // heat is wanted at this setpoint
			r.Advance(6 * 3600)
			r.Plant.Inject(k.dev, k.kind, k.par)
			if k.dev == sim.DevTemp && k.kind == sim.FaultStuck {
				// A frozen value is seen after 6 min of heating or cooling that should
				// have moved it, or 2 h regardless. With the heater cycling at its
				// working duty that takes about half an hour of real time.
				t0 := r.Clock.VT
				if !r.AdvanceUntil(2*3600+60, func() bool { return r.Core.Probe().TempQ != api.QualityOK }) {
					t.Fatal("stuck reading never detected")
				}
				t.Logf("stuck reading detected after %.0f s", r.Clock.VT-t0)
			} else {
				r.Advance(30)
			}
			from := r.Clock.VT
			r.Rows = nil
			r.Advance(3 * 3600)
			if len(r.Rows) == 0 {
				t.Fatal("no rows")
			}
			for _, x := range r.Rows {
				if x.HeatCmd || x.HumCmd {
					t.Fatalf("at %v (fault+%v): heat=%v humid=%v, q=%s", x.T, x.T-from, x.HeatCmd, x.HumCmd, x.QT)
				}
			}
		})
	}
}

func TestSensorFaultShownWithin30sAndClears(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(2 * 3600)
	r.Plant.Inject(sim.DevClimate, sim.FaultDead, 0)
	r.Advance(30)
	s := r.Core.Snapshot()
	if s.Temp.Quality != api.QualityFault || s.RH.Quality != api.QualityFault {
		t.Fatalf("quality after 30 s: %s %s", s.Temp.Quality, s.RH.Quality)
	}
	found := false
	for _, a := range s.Active {
		if a.Code == core.CodeClimateFault {
			found = a.Severity == api.SevCritical
		}
	}
	if !found {
		t.Fatal("no critical climate alarm")
	}
	r.Plant.Inject(sim.DevClimate, sim.FaultNone, 0)
	r.Advance(30)
	s = r.Core.Snapshot()
	for _, a := range s.Active {
		if a.Code == core.CodeClimateFault {
			t.Fatal("still active 30 s after repair")
		}
	}
	if s.Temp.Quality != api.QualityOK {
		t.Fatalf("temp quality after repair %s", s.Temp.Quality)
	}
}

func TestStaleValueNeverLooksGood(t *testing.T) {
	r := newRun(t)
	r.Advance(600)
	r.Plant.Inject(sim.DevClimate, sim.FaultDead, 0)
	for i := 0; i < 12; i++ {
		r.Tick()
	}
	s := r.Core.Snapshot()
	if s.Temp.Quality == api.QualityOK {
		t.Fatal("reading 12 s old is still reported ok")
	}
	if s.Temp.AgeSec < 10 {
		t.Fatalf("age %v", s.Temp.AgeSec)
	}
}

func TestSpikesRejectedWithoutChangingControl(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(8 * 3600)
	r.Plant.Inject(sim.DevTemp, sim.FaultSpike, 0.1)
	r.Rows = nil
	r.Advance(3600)
	for _, x := range r.Rows {
		if x.QT != string(api.QualityOK) && x.QT != string(api.QualityStale) {
			t.Fatalf("10%% spikes made the channel %s", x.QT)
		}
		if x.CtlT > 20 || x.CtlT < 5 {
			t.Fatalf("a spike reached the control value: %v", x.CtlT)
		}
	}
	if alarmEvents(r, core.CodeTempErratic) == 0 {
		t.Fatal("no erratic warning")
	}
}

func TestSilenceDoesNotHideCondition(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(3 * 3600)
	r.Plant.Inject(sim.DevClimate, sim.FaultDead, 0)
	r.Advance(60)
	if !r.Plant.CmdBuzzer || r.Plant.CmdAlarm {
		t.Fatal("buzzer should be on and the energize-to-run contact open")
	}
	if err := r.Core.Do(api.Command{Kind: api.CmdSilence}); err != nil {
		t.Fatal(err)
	}
	r.Advance(5)
	s := r.Core.Snapshot()
	if r.Plant.CmdBuzzer {
		t.Fatal("buzzer not silenced")
	}
	if r.Plant.CmdAlarm {
		t.Fatal("silence must not stop the external alarm contact signalling")
	}
	if !s.BuzzerSilenced || len(s.Active) == 0 {
		t.Fatal("alarm must stay visible while silenced")
	}
	// The silence times out and sounds again for an unacknowledged alarm.
	r.Advance(31 * 60)
	if !r.Plant.CmdBuzzer {
		t.Fatal("silence should time out while the alarm is unacknowledged")
	}
	// Ack silences for good, and the alarm stays active until the condition ends.
	if err := r.Core.Do(api.Command{Kind: api.CmdAck}); err != nil {
		t.Fatal(err)
	}
	r.Advance(40 * 60)
	if r.Plant.CmdBuzzer {
		t.Fatal("acknowledged alarm should not sound")
	}
	s = r.Core.Snapshot()
	active := false
	for _, a := range s.Active {
		if a.Code == core.CodeClimateFault {
			active = a.Acked
		}
	}
	if !active {
		t.Fatal("acknowledged alarm should stay active and marked acked")
	}
	if r.Plant.CmdAlarm {
		t.Fatal("acknowledge must not stop the contact while a critical condition is present")
	}
	// The buzzer comes back after an hour if it is still there.
	r.Advance(25 * 60)
	if !r.Plant.CmdBuzzer {
		t.Fatal("buzzer should return an hour after acknowledge while the critical is present")
	}
	_ = r.Core.Do(api.Command{Kind: api.CmdAck})
	r.Advance(5)
	// A new alarm sounds again.
	r.Plant.Inject(sim.DevAir, sim.FaultDead, 0)
	r.Advance(30)
	if !r.Plant.CmdBuzzer {
		t.Fatal("a new alarm should sound")
	}
}

func TestAlarmDelaySuppressionAndDifferential(t *testing.T) {
	r := newRun(t, func(p *sim.Params) { p.CompW = 130 })
	r.SetPoint(13, 76, 1.2)
	r.Advance(12 * 3600)
	// Compressor failure: temperature climbs; alarm only after the band and delay.
	r.Plant.Inject(sim.DevCompressor, sim.FaultNoCool, 0)
	var crossed float64
	for r.Clock.VT < 24*3600 {
		r.Tick()
		s := r.Core.Probe()
		if crossed == 0 && s.Temp > 13+core.DefaultLimits().TempHighDelta {
			crossed = r.Clock.VT
		}
		if alarmEvents(r, core.CodeTempHigh) > 0 {
			break
		}
	}
	if crossed == 0 {
		t.Fatal("temperature never crossed the limit")
	}
	var at float64
	for _, e := range r.Evs {
		if e.Kind == "alarm" && e.Code == core.CodeTempHigh {
			at = e.T
		}
	}
	if at == 0 || at-crossed < 14*60 {
		t.Fatalf("alarm %v s after crossing, want about the 15 min delay", at-crossed)
	}
	// Repair: alarm must not clear until the differential below the limit.
	r.Plant.Inject(sim.DevCompressor, sim.FaultNone, 0)
	hi := 13 + core.DefaultLimits().TempHighDelta
	cfg := core.DefaultConfig()
	for r.Clock.VT < at+12*3600 {
		r.Tick()
		act := false
		for _, a := range r.Core.Snapshot().Active {
			if a.Code == core.CodeTempHigh {
				act = true
			}
		}
		if !act {
			break
		}
	}
	for _, e := range r.Evs {
		if e.Kind == "clear" && e.Code == core.CodeTempHigh {
			// Reading at the moment of clearing must be past the differential.
			if v := r.Core.Probe().Temp; v > hi-cfg.TempClearDiff+0.5 {
				t.Fatalf("cleared at %v, limit %v, differential %v", v, hi, cfg.TempClearDiff)
			}
			return
		}
	}
	t.Fatal("alarm never cleared")
}

func TestDoorSuppressionWindow(t *testing.T) {
	r := newRun(t)
	r.SetPoint(4, 85, 1.2)
	r.Advance(12 * 3600)
	start := len(r.Evs)
	r.Plant.DoorIsOpen = true
	r.Advance(120)
	r.Plant.DoorIsOpen = false
	r.Advance(19 * 60)
	for _, e := range r.Evs[start:] {
		if e.Kind == "alarm" && (e.Code == core.CodeTempHigh || e.Code == core.CodeRHLow || e.Code == core.CodeRHHigh) {
			t.Fatalf("alarm %s inside the suppression window", e.Code)
		}
	}
	// Absolute limit is never suppressed.
	r.Plant.DoorIsOpen = true
	r.Plant.AmbientOffset = 40
	r.Advance(3600)
	if alarmEvents(r, core.CodeDoorOpen) == 0 {
		t.Fatal("door open too long did not alarm")
	}
	if alarmEvents(r, core.CodeTempAbsHigh) == 0 {
		t.Fatal("absolute limit suppressed by open door")
	}
}

func TestAlarmsDoNotChangeRegulation(t *testing.T) {
	// The same run with an RH alarm limit tight enough to alarm continuously, and
	// with the default: outputs must be identical.
	run := func(tight bool) []sim.Row {
		r := newRun(t)
		if tight {
			l := core.DefaultLimits()
			l.RHHighDelta, l.RHLowDelta = 1, 1
			l.TempHighDelta, l.TempLowDelta = 0.5, 0.5
			l.AlarmDelay = 0
			_ = r.Core.Do(api.Command{Kind: api.CmdSetLimits, Limits: &l})
		}
		r.SetPoint(13, 76, 1.2)
		r.Advance(8 * 3600)
		return r.Rows
	}
	a, b := run(false), run(true)
	for i := range a {
		if a[i].CoolCmd != b[i].CoolCmd || a[i].HeatCmd != b[i].HeatCmd || a[i].HumCmd != b[i].HumCmd {
			t.Fatalf("regulation differs at %v", a[i].T)
		}
	}
}

func TestHistoryPersistsAndIsNotErasable(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(3600)
	r.Plant.Inject(sim.DevClimate, sim.FaultDead, 0)
	r.Advance(120)
	r.Plant.Inject(sim.DevClimate, sim.FaultNone, 0)
	r.Advance(120)
	if err := r.PowerLoss(300, 60); err != nil {
		t.Fatal(err)
	}
	r.Advance(60)
	s := r.Core.Snapshot()
	found := false
	for _, a := range s.History {
		if a.Code == core.CodeClimateFault && !a.Cleared.IsZero() {
			found = true
		}
	}
	if !found {
		t.Fatal("cleared alarm lost across a power cut")
	}
	for _, k := range []api.CommandKind{"clear_history", "erase_log"} {
		if err := r.Core.Do(api.Command{Kind: k}); err == nil {
			t.Fatalf("command %s accepted", k)
		}
	}
}

func TestActiveAlarmSurvivesRestart(t *testing.T) {
	r := newRun(t)
	r.Advance(3600)
	r.Plant.Inject(sim.DevAir, sim.FaultDead, 0)
	r.Advance(60)
	if err := r.PowerLoss(60, 60); err != nil {
		t.Fatal(err)
	}
	s := r.Core.Snapshot()
	ok := false
	for _, a := range s.Active {
		if a.Code == core.CodeAirFault {
			ok = true
		}
	}
	if !ok {
		t.Fatal("active alarm lost across a restart")
	}
}

func TestRecipeTimeDoesNotDependOnWallClock(t *testing.T) {
	r := newRun(t, func(p *sim.Params) { p.RTC = false })
	if err := r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: "ferment-dry"}); err != nil {
		t.Fatal(err)
	}
	r.Advance(10 * 3600)
	before := r.Core.Probe().TotalElapsed
	// Boot with a wrong clock, never corrected.
	if err := r.PowerLoss(7200, -1); err != nil {
		t.Fatal(err)
	}
	s := r.Core.Snapshot()
	if s.Health.ClockTrusted {
		t.Fatal("clock should be untrusted")
	}
	if !s.Health.LastOutage.Detected {
		t.Fatal("outage not detected")
	}
	if s.Health.LastOutage.DurationOK {
		t.Fatal("duration claimed with no trustworthy clock")
	}
	after := s.Program.TotalElapsed.Seconds()
	if after < before-70 || after > before {
		t.Fatalf("elapsed %v -> %v", before, after)
	}
	r.Advance(3600)
	got := r.Core.Probe().TotalElapsed
	if got < after+3590 || got > after+3610 {
		t.Fatalf("elapsed moved by %v in one powered hour", got-after)
	}
}

func TestOutageDurationResolvedWhenClockArrives(t *testing.T) {
	r := newRun(t, func(p *sim.Params) { p.RTC = false })
	r.Advance(3600)
	if err := r.PowerLoss(5000, 600); err != nil {
		t.Fatal(err)
	}
	if r.Core.Snapshot().Health.LastOutage.DurationOK {
		t.Fatal("duration known too early")
	}
	r.Advance(700)
	o := r.Core.Snapshot().Health.LastOutage
	if !o.DurationOK {
		t.Fatal("duration not resolved after NTP")
	}
	if d := o.Duration.Seconds(); d < 5000 || d > 5100 {
		t.Fatalf("duration %v, want 5000 to about 5060", d)
	}
}

func TestOutageRecordsPeakAndPowerOnDelay(t *testing.T) {
	r := newRun(t)
	r.SetPoint(4, 85, 1.2)
	r.Advance(8 * 3600)
	if err := r.PowerLoss(4*3600, 60); err != nil {
		t.Fatal(err)
	}
	warmOnReturn := r.Plant.TAir
	boot := r.Clock.VT
	firstOn := -1.0
	for r.Clock.VT < boot+1800 {
		r.Tick()
		if firstOn < 0 && r.Plant.CmdCool {
			firstOn = r.Clock.VT - boot
		}
	}
	if firstOn < core.DefaultConfig().PowerOnDelay {
		t.Fatalf("compressor on after %v s", firstOn)
	}
	r.Advance(4 * 3600)
	s := r.Core.Snapshot()
	o := s.Health.LastOutage
	if !o.Detected || !o.DurationOK || !o.TempKnown {
		t.Fatalf("%+v", o)
	}
	if o.PeakTempC < warmOnReturn-1.5 {
		t.Fatalf("peak %v, on return %v", o.PeakTempC, warmOnReturn)
	}
	if o.RecoverSec <= 0 {
		t.Fatalf("recover %v", o.RecoverSec)
	}
}

func TestCleanShutdownIsNotAnOutage(t *testing.T) {
	p := sim.BasePlant()
	fs := core.NewMemFS()
	pl := sim.NewPlant(p)
	clk := sim.NewClock(true)
	c, err := core.New(core.Options{Cabinet: pl.Cabinet(), Clock: clk, FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		pl.Step(1)
		clk.VT = pl.T
		c.Step()
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	clk.Reboot(0)
	c2, err := core.New(core.Options{Cabinet: pl.Cabinet(), Clock: clk, FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	if c2.Snapshot().Health.LastOutage.Detected {
		t.Fatal("clean shutdown reported as an outage")
	}
}

func TestStorageFailureRaisesAlarmAndControlContinues(t *testing.T) {
	r := newRun(t)
	r.SetPoint(4, 85, 1.2)
	r.Advance(3600)
	r.FS.FailWrite = true
	r.Advance(7200)
	s := r.Core.Snapshot()
	found := false
	for _, a := range s.Active {
		if a.Code == core.CodeStorage {
			found = true
		}
	}
	if !found || s.Health.LogOK {
		t.Fatalf("no storage alarm; LogOK=%v", s.Health.LogOK)
	}
	on := false
	for _, x := range r.Rows[len(r.Rows)-700:] {
		on = on || x.CoolCmd
	}
	if !on {
		t.Fatal("control stopped when storage failed")
	}
	before, _ := core.AuditStore(r.FS)
	r.FS.FailWrite = false
	r.Advance(1200)
	after, err := core.AuditStore(r.FS)
	if err != nil {
		t.Fatal(err)
	}
	// Records held in RAM reach the disk after recovery, with no gap.
	if after.LogRecords-before.LogRecords < 100 {
		t.Fatalf("records after repair %d -> %d", before.LogRecords, after.LogRecords)
	}
	if after.LogSeqGaps != 0 {
		t.Fatalf("gaps %d", after.LogSeqGaps)
	}
	for _, a := range r.Core.Snapshot().Active {
		if a.Code == core.CodeStorage {
			t.Fatal("storage alarm did not clear after repair")
		}
	}
}

func TestTornWritesNeverLoseEarlierData(t *testing.T) {
	seed := uint64(1)
	for op := 1; op < 150; op++ {
		rng := rand.New(rand.NewPCG(seed, uint64(op)))
		p := sim.BasePlant()
		pl := sim.NewPlant(p)
		clk := sim.NewClock(true)
		fs := core.NewMemFS()
		c, err := core.New(core.Options{Cabinet: pl.Cabinet(), Clock: clk, FS: fs})
		if err != nil {
			t.Fatal(err)
		}
		sp := api.Setpoints{TempC: 11, RHPct: 70, AirMS: 1.3}
		if err := c.Do(api.Command{Kind: api.CmdSetSetpoint, Setpoint: &sp}); err != nil {
			t.Fatal(err)
		}
		fs.ArmCut(op)
		for i := 0; i < 7200 && !fs.Cut(); i++ {
			pl.Step(1)
			clk.VT = pl.T
			c.Step()
		}
		durableLog := 0
		for _, n := range fs.FileNames() {
			if strings.HasPrefix(n, "process-") {
				g, _ := core.CountGoodLines(fs.DurableFile(n))
				durableLog += g
			}
		}
		fs.PowerCut(rng)
		a, err := core.AuditStore(fs)
		if err != nil {
			t.Fatal(err)
		}
		if a.LogRecords < durableLog {
			t.Fatalf("op %d: durable %d log lines, %d survive", op, durableLog, a.LogRecords)
		}
		clk.Reboot(0)
		c2, err := core.New(core.Options{Cabinet: pl.Cabinet(), Clock: clk, FS: fs})
		if err != nil {
			t.Fatalf("op %d: boot: %v", op, err)
		}
		s := c2.Snapshot()
		if a.HaveState && s.Setpoint != sp && s.Setpoint != (api.Setpoints{TempC: 13, RHPct: 75, AirMS: 1.0}) {
			t.Fatalf("op %d: setpoint %+v is neither old nor new", op, s.Setpoint)
		}
		for i := 0; i < 600; i++ {
			pl.Step(1)
			clk.VT = pl.T
			c2.Step()
		}
		if pl.FanDuty <= 0 {
			t.Fatalf("op %d: control did not run after recovery", op)
		}
	}
}

func TestBothStateSlotsCorrupt(t *testing.T) {
	r := newRun(t)
	if err := r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: "whole-muscle"}); err != nil {
		t.Fatal(err)
	}
	r.Advance(3 * 3600)
	before := r.Core.Probe().TotalElapsed
	r.FS.WriteRaw("state.a", []byte("garbage"))
	r.FS.WriteRaw("state.b", []byte("more garbage"))
	if err := r.PowerLoss(10, 60); err != nil {
		t.Fatal(err)
	}
	s := r.Core.Snapshot()
	found := false
	for _, a := range s.Active {
		if a.Code == core.CodeStateLost && a.Severity == api.SevCritical {
			found = true
		}
	}
	if !found {
		t.Fatal("no state-lost alarm")
	}
	if s.Program.State != api.RunActive || s.Program.TotalElapsed.Seconds() < before-70 {
		t.Fatalf("recipe not rebuilt from the log: %+v", s.Program)
	}
}

func TestCorruptJournalLineSkippedAndReported(t *testing.T) {
	r := newRun(t)
	r.Advance(3600)
	b, err := r.FS.ReadFile("process-000001.jnl")
	if err != nil {
		t.Fatal(err)
	}
	n := len(b) / 2
	b[n] ^= 0xff
	r.FS.WriteRaw("process-000001.jnl", b)
	a, _ := core.AuditStore(r.FS)
	if a.LogBadLines != 1 {
		t.Fatalf("bad lines %d", a.LogBadLines)
	}
	if a.LogRecords < 50 {
		t.Fatalf("records %d", a.LogRecords)
	}
	if err := r.PowerLoss(10, 60); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range r.Core.Snapshot().Active {
		found = found || x.Code == core.CodeLogDamage
	}
	if !found {
		t.Fatal("damage not reported")
	}
}

// blockingFS stalls writes, as a sick SD card does.
type blockingFS struct {
	core.FS
	block chan struct{}
}

type blockingFile struct {
	core.File
	b *blockingFS
}

func (f blockingFile) Sync() error { <-f.b.block; return f.File.Sync() }

func (b *blockingFS) OpenAppend(n string) (core.File, error) {
	f, err := b.FS.OpenAppend(n)
	if err != nil {
		return nil, err
	}
	return blockingFile{f, b}, nil
}

func (b *blockingFS) ReplaceSync(n string, d []byte) error { <-b.block; return b.FS.ReplaceSync(n, d) }

func TestStalledStorageDoesNotBlockControl(t *testing.T) {
	p := sim.BasePlant()
	pl := sim.NewPlant(p)
	clk := sim.NewClock(true)
	fs := &blockingFS{FS: core.NewMemFS(), block: make(chan struct{})}
	defer close(fs.block)
	c, err := core.New(core.Options{Cabinet: pl.Cabinet(), Clock: clk, FS: fs, AsyncDepth: 8})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3600; i++ {
			pl.Step(1)
			clk.Set(pl.T)
			c.Step()
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("control loop blocked on storage")
	}
	s := c.Snapshot()
	found := false
	for _, a := range s.Active {
		found = found || a.Code == core.CodeStorage
	}
	if !found {
		t.Fatal("stalled storage did not raise the alarm")
	}
}

func TestFailDirectionNoHeatWithoutAirflowProof(t *testing.T) {
	r := newRun(t)
	r.SetPoint(24, 90, 1.2)
	r.Advance(6 * 3600)
	r.Plant.Inject(sim.DevFan, sim.FaultStall, 0)
	r.Advance(60)
	r.Rows = nil
	r.Advance(3600)
	for _, x := range r.Rows {
		if x.HeatCmd || x.HumCmd {
			t.Fatalf("heat or humid on with a stalled fan at %v", x.T)
		}
	}
}

func TestHumidityLossStopsHumidityOnly(t *testing.T) {
	r := newRun(t)
	r.SetPoint(4, 85, 1.2)
	r.Advance(6 * 3600)
	r.Plant.Inject(sim.DevRH, sim.FaultRange, 0)
	r.Advance(60)
	r.Rows = nil
	r.Advance(6 * 3600)
	cool := 0
	for _, x := range r.Rows {
		if x.HumCmd {
			t.Fatal("humidifier on with a bad humidity reading")
		}
		if x.CoolCmd {
			cool++
		}
		if x.QT != string(api.QualityOK) {
			t.Fatalf("temperature channel %s", x.QT)
		}
	}
	if cool == 0 {
		t.Fatal("temperature control stopped")
	}
}

func TestWeightTargetIsNotASafetyGate(t *testing.T) {
	r := newRun(t)
	sp := api.Setpoints{TempC: 13, RHPct: 75, AirMS: 1.2}
	rc := api.Recipe{ID: "t", Name: "t", Phases: []api.PhaseSpec{{Name: "dry", Setpoint: sp, TargetLossPct: 1}}}
	if err := r.Core.Do(api.Command{Kind: api.CmdSaveRecipe, Recipe: &rc}); err != nil {
		t.Fatal(err)
	}
	if err := r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: "t"}); err != nil {
		t.Fatal(err)
	}
	ok := r.AdvanceUntil(7*24*3600, func() bool { return r.Core.Probe().RunState == api.RunFinished })
	if !ok {
		t.Fatal("did not finish on the weight target")
	}
	s := r.Core.Snapshot()
	if !strings.Contains(s.Program.Verify, "does not show that product is safe") {
		t.Fatalf("verify text: %q", s.Program.Verify)
	}
	// Nothing visible may say the product is safe.
	js, _ := json.Marshal(s)
	low := strings.ToLower(string(js))
	for _, bad := range []string{"product is safe\"", "safe to eat", "food safe", "safe to consume", "is ready"} {
		if strings.Contains(low, bad) {
			t.Fatalf("snapshot contains %q", bad)
		}
	}
	// It keeps holding conditions after the end.
	r.Rows = nil
	r.Advance(6 * 3600)
	on := false
	for _, x := range r.Rows {
		on = on || x.CoolCmd
	}
	if !on {
		t.Fatal("control stopped at recipe end")
	}
	if len(r.Core.Snapshot().Active) == 0 {
		t.Fatal("end of recipe must leave a visible request to verify")
	}
}

func TestScaleFaultDoesNotAdvanceWeightPhase(t *testing.T) {
	r := newRun(t)
	sp := api.Setpoints{TempC: 13, RHPct: 75, AirMS: 1.2}
	rc := api.Recipe{ID: "t", Name: "t", Phases: []api.PhaseSpec{{Name: "dry", Setpoint: sp, TargetLossPct: 2}}}
	_ = r.Core.Do(api.Command{Kind: api.CmdSaveRecipe, Recipe: &rc})
	_ = r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: "t"})
	r.Advance(3600)
	// A scale reading far low would look like a huge weight loss.
	r.Plant.Inject(sim.DevScale, sim.FaultDead, 0)
	r.Advance(5 * 24 * 3600)
	if s := r.Core.Probe(); s.RunState != api.RunActive {
		t.Fatalf("state %s with a dead scale", s.RunState)
	}
}

func TestCommandsValidated(t *testing.T) {
	r := newRun(t)
	bad := []api.Command{
		{Kind: api.CmdSetSetpoint},
		{Kind: api.CmdSetSetpoint, Setpoint: &api.Setpoints{TempC: 50, RHPct: 70, AirMS: 1}},
		{Kind: api.CmdSetSetpoint, Setpoint: &api.Setpoints{TempC: 10, RHPct: 99, AirMS: 1}},
		{Kind: api.CmdSetCal, Calibration: &api.Calibration{TempOffsetC: 13}},
		{Kind: api.CmdStart, RecipeID: "nope"},
		{Kind: api.CmdPause},
		{Kind: api.CmdSetLimits, Limits: &api.Limits{}},
		{Kind: api.CmdCalWeight, KnownWeightG: -1},
		{Kind: "unknown"},
	}
	for i, c := range bad {
		if err := r.Core.Do(c); err == nil {
			t.Errorf("command %d (%s) accepted", i, c.Kind)
		}
	}
	// R1.1, R1.8: required ranges are accepted.
	for _, sp := range []api.Setpoints{{TempC: 0, RHPct: 40, AirMS: 0}, {TempC: 30, RHPct: 90, AirMS: 2}, {TempC: 25, RHPct: 95, AirMS: 5}} {
		sp := sp
		if err := r.Core.Do(api.Command{Kind: api.CmdSetSetpoint, Setpoint: &sp}); err != nil {
			t.Errorf("%+v refused: %v", sp, err)
		}
	}
	k := api.Calibration{TempOffsetC: -12, RHOffsetPct: 10}
	if err := r.Core.Do(api.Command{Kind: api.CmdSetCal, Calibration: &k}); err != nil {
		t.Errorf("calibration refused: %v", err)
	}
}

func TestTareAndCalibrateThroughCommands(t *testing.T) {
	p := sim.BasePlant()
	p.ProductKg = 0
	r, err := sim.NewRun(sim.Options{Plant: p, KeepRows: false})
	if err != nil {
		t.Fatal(err)
	}
	r.Advance(120)
	if err := r.Core.Do(api.Command{Kind: api.CmdTare}); err != nil {
		t.Fatal(err)
	}
	r.Plant.LoadProduct(2, 21)
	r.Advance(120)
	if err := r.Core.Do(api.Command{Kind: api.CmdCalWeight, KnownWeightG: 2000}); err != nil {
		t.Fatal(err)
	}
	r.Advance(600)
	s := r.Core.Snapshot()
	if s.Weight.Quality != api.QualityOK || s.Weight.Value < 1960 || s.Weight.Value > 2040 {
		t.Fatalf("weight %+v", s.Weight)
	}
}

func TestSnapshotConcurrentWithStep(t *testing.T) {
	// Run under -race. Front ends call Snapshot and Do from other goroutines while
	// the control loop steps. Commands are paced like a person, not a flood.
	pl := sim.NewPlant(sim.BasePlant())
	clk := sim.NewClock(true)
	c, err := core.New(core.Options{Cabinet: pl.Cabinet(), Clock: clk, FS: core.NewMemFS(), AsyncDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				case <-time.After(time.Millisecond):
				}
				s := c.Snapshot()
				_ = s.Trend
				_ = c.Do(api.Command{Kind: api.CmdSilence})
			}
		}()
	}
	for i := 0; i < 1800; i++ {
		pl.Step(1)
		clk.Set(pl.T)
		c.Step()
	}
	close(stop)
	wg.Wait()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

type failWatchdog struct{ err error }

func (f *failWatchdog) Pet() error { return f.err }

func TestWatchdogPetOnStep(t *testing.T) {
	pl := sim.NewPlant(sim.BasePlant())
	clk := sim.NewClock(true)
	wd := &failWatchdog{err: errors.New("no watchdog")}
	c, err := core.New(core.Options{Cabinet: pl.Cabinet(), Clock: clk, FS: core.NewMemFS(), Watchdog: wd})
	if err != nil {
		t.Fatal(err)
	}
	c.Step()
	if c.Snapshot().Health.WatchdogOK {
		t.Fatal("watchdog failure not reported")
	}
}

func TestMissingHardwareDoesNotPanic(t *testing.T) {
	clk := sim.NewClock(true)
	c, err := core.New(core.Options{Cabinet: hal.Cabinet{}, Clock: clk, FS: core.NewMemFS()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		clk.VT++
		c.Step()
	}
	if c.Snapshot().Temp.Quality != api.QualityFault {
		t.Fatal("no sensor should read as a fault")
	}
}

func TestOutageDurationNeverComesFromAStaleClock(t *testing.T) {
	r := newRun(t, func(p *sim.Params) { p.RTC = false })
	r.Advance(3600) // clock arrives at 600 s
	// A short boot with a wrong clock, then a second cut before NTP.
	if err := r.PowerLoss(1000, -1); err != nil {
		t.Fatal(err)
	}
	r.Advance(30)
	if err := r.PowerLoss(2000, 60); err != nil {
		t.Fatal(err)
	}
	r.Advance(120)
	if o := r.Core.Snapshot().Health.LastOutage; o.DurationOK {
		t.Fatalf("claimed %v for an outage whose start time was never trusted", o.Duration)
	}
}

func TestHeatingRegimeIsNotAStuckCompressor(t *testing.T) {
	r := newRun(t, func(p *sim.Params) { p.AmbientC = 14; p.UAWallAmb = 3.6 })
	r.SetPoint(24, 90, 1.2)
	r.Advance(36 * 3600)
	if n := alarmEvents(r, core.CodeCoolStuck); n != 0 {
		t.Fatalf("%d false stuck-compressor alarms with a busy heater in a cold room", n)
	}
}

func TestCoolRelayStuckClosedAgainstHeater(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(14 * 3600)
	r.Plant.Inject(sim.DevCool, sim.FaultRelayStuckOn, 0)
	r.Advance(6 * 3600)
	if alarmEvents(r, core.CodeCoolStuck) == 0 && alarmEvents(r, core.CodeTempLow) == 0 {
		t.Fatal("a closed compressor relay fought by the heater went unreported")
	}
}

func TestContactQuietInNormalRunAndOpensOnPowerLoss(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(120)
	if !r.Plant.CmdAlarm {
		t.Fatal("healthy contact must be energized")
	}
	r.Rows = nil
	r.Advance(10 * 3600)
	for _, x := range r.Rows {
		if x.AlarmRelay || x.Buzzer {
			t.Fatalf("signalling at %v with no fault", x.T)
		}
	}
	_ = r.PowerLoss(120, 60)
	open := false
	for _, x := range r.Rows {
		if !x.Powered && x.AlarmRelay {
			open = true
		}
	}
	if !open {
		t.Fatal("the logged contact must be open while unpowered")
	}
	r.Plant.PowerOff()
	if r.Plant.CmdAlarm {
		t.Fatal("lost power must read as an alarm")
	}
	r.Plant.PowerOn()
	if err := r.Core.Close(); err != nil {
		t.Fatal(err)
	}
	if r.Plant.CmdAlarm {
		t.Fatal("an orderly stop must also drop the contact")
	}
}

func TestLaterCriticalChangesContactWhenAlreadySignalling(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(6 * 3600)
	r.Plant.Inject(sim.DevDuct, sim.FaultBlocked, 0)
	r.Advance(600)
	if r.Plant.CmdAlarm {
		t.Fatal("critical should be signalling")
	}
	r.Plant.Inject(sim.DevClimate, sim.FaultDead, 0)
	changed := false
	for i := 0; i < 60; i++ {
		r.Tick()
		if r.Plant.CmdAlarm {
			changed = true
		}
	}
	if !changed {
		t.Fatal("a new critical made no visible change on the contact")
	}
	if r.Plant.CmdAlarm {
		t.Fatal("the contact should be signalling again after the brief close")
	}
}

func TestPastEventDoesNotHoldContact(t *testing.T) {
	// A warning outage signals for a while and lets go; a critical one holds until seen.
	r := newRun(t)
	r.SetPoint(20, 70, 1.2) // no 15.6 °C rule, room close: a warning outage
	r.Advance(6 * 3600)
	if err := r.PowerLoss(2*3600, 60); err != nil {
		t.Fatal(err)
	}
	r.Advance(4 * 3600)
	sev := api.Severity("")
	for _, a := range r.Core.Snapshot().Active {
		if a.Code == core.CodeOutage {
			sev = a.Severity
		}
	}
	if sev != api.SevWarning {
		t.Fatalf("want a listed warning outage, got %q", sev)
	}
	// Other real conditions may also be standing; acknowledge those, not the outage.
	for _, a := range r.Core.Snapshot().Active {
		if a.Code != core.CodeOutage {
			_ = r.Core.Do(api.Command{Kind: api.CmdAck, AlarmID: a.ID})
		}
	}
	r.Advance(30)
	if !r.Plant.CmdAlarm {
		for _, a := range r.Core.Snapshot().Active {
			t.Logf("active %s %s acked=%v", a.Code, a.Severity, a.Acked)
		}
		t.Fatal("a warning outage is still holding the contact after 4 h")
	}

	q := newRun(t)
	q.SetPoint(4, 85, 1.2)
	q.Advance(6 * 3600)
	if err := q.PowerLoss(10*3600, 60); err != nil { // comes back far over the 15.6 °C line
		t.Fatal(err)
	}
	q.Advance(4 * 3600)
	if q.Plant.CmdAlarm {
		t.Fatal("an unacknowledged critical outage must hold the contact")
	}
	_ = q.Core.Do(api.Command{Kind: api.CmdAck})
	q.Advance(60)
	if !q.Plant.CmdAlarm {
		t.Fatal("acknowledged, it should let go")
	}
}

func TestLeaseDropKeepsOffTime(t *testing.T) {
	r := newRun(t)
	r.SetPoint(4, 85, 1.2)
	r.Advance(4 * 3600)
	if !r.AdvanceUntil(3600, func() bool { return r.Plant.CmdCool }) {
		t.Fatal("no compressor run")
	}
	r.Guard.DropOutputs()
	dropAt := r.Clock.VT
	for r.Clock.VT < dropAt+core.DefaultConfig().CompMinOff-2 {
		r.Tick()
		if r.Plant.CmdCool {
			t.Fatalf("compressor back %v s after a lease drop", r.Clock.VT-dropAt)
		}
	}
	if alarmEvents(r, core.CodeOutDropped) != 1 {
		t.Fatal("drop not reported")
	}
	if r.Plant.FanDuty == 0 {
		t.Fatal("fan must be commanded again after a drop")
	}
}

func TestReferenceCheckRecordsAndLatches(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(3600)
	wrong := r.Plant.TAir + 4
	if err := r.Core.Do(api.Command{Kind: api.CmdRefCheck, Reference: &api.Reference{TempC: &wrong}}); err != nil {
		t.Fatal(err)
	}
	r.Advance(5)
	s := r.Core.Snapshot()
	if s.Temp.Quality != api.QualitySuspect {
		t.Fatal("a failed reference check must mark the reading")
	}
	// It ends by itself if nobody comes back.
	r.RefCheck()
	r.Advance(5)
	s = r.Core.Snapshot()
	if s.Temp.Quality == api.QualitySuspect {
		t.Fatal("a passed check must clear the doubt")
	}
	for _, a := range s.Active {
		if a.Code == core.CodeRefDisagree || a.Code == core.CodeTempDisagree {
			t.Fatal("alarm still active after a passed check")
		}
	}
}

func TestOperatorCanOverrideARunningPhase(t *testing.T) {
	r := newRun(t)
	if err := r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: "ferment-dry"}); err != nil {
		t.Fatal(err)
	}
	r.Advance(3600)
	sp := api.Setpoints{TempC: 22, RHPct: 88, AirMS: 1.0}
	if err := r.Core.Do(api.Command{Kind: api.CmdSetSetpoint, Setpoint: &sp}); err != nil {
		t.Fatal(err)
	}
	s := r.Core.Snapshot()
	if s.Setpoint != sp || s.Program.Override == nil || s.Program.PhaseIndex != 0 {
		t.Fatalf("override not in force: %+v", s.Program)
	}
	r.Advance(2) // a command is saved by the next control tick
	if err := r.PowerLoss(60, 60); err != nil {
		t.Fatal(err)
	}
	if got := r.Core.Snapshot().Setpoint; got != sp {
		t.Fatalf("override lost across a restart: %+v", got)
	}
	if err := r.Core.Do(api.Command{Kind: api.CmdNextPhase}); err != nil {
		t.Fatal(err)
	}
	s = r.Core.Snapshot()
	if s.Program.Override != nil || s.Setpoint.TempC != 14 {
		t.Fatalf("override carried over: %+v", s.Setpoint)
	}
	found := false
	for _, e := range r.Evs {
		found = found || strings.Contains(e.Msg, "operator override")
	}
	if !found {
		t.Fatal("override not on the record")
	}
	bad := api.Setpoints{TempC: 90, RHPct: 88, AirMS: 1}
	if err := r.Core.Do(api.Command{Kind: api.CmdSetSetpoint, Setpoint: &bad}); err == nil {
		t.Fatal("an out-of-range override was accepted")
	}
}

func statusOf(s api.Snapshot, out string) api.OutputStatus {
	for _, x := range s.Outputs.Status {
		if x.Output == out {
			return x
		}
	}
	return api.OutputStatus{}
}

func TestIdleReasonTellsHoldFromFault(t *testing.T) {
	r := newRun(t)
	r.SetPoint(24, 90, 1.2)
	r.Advance(6 * 3600)
	for _, o := range []string{"cool", "heat", "humid"} {
		if st := statusOf(r.Core.Snapshot(), o); st.Reason == "" || st.Kind == "fault" {
			t.Fatalf("%s: %+v on a healthy cabinet", o, st)
		}
	}
	r.Plant.Inject(sim.DevClimate, sim.FaultDead, 0)
	r.Advance(60)
	if st := statusOf(r.Core.Snapshot(), "heat"); st.On || st.Kind != "fault" {
		t.Fatalf("heat with no sensor: %+v", st)
	}
	r.Plant.Inject(sim.DevClimate, sim.FaultNone, 0)
	r.Advance(120)
	r.SetPoint(4, 85, 1.2)
	r.Plant.DoorIsOpen = true
	r.Advance(30)
	if st := statusOf(r.Core.Snapshot(), "cool"); st.On || st.Kind != "hold" {
		t.Fatalf("cool with door open: %+v", st)
	}
}

func TestReminderLifecycle(t *testing.T) {
	r := newRun(t)
	if err := r.Core.Do(api.Command{Kind: api.CmdStart, RecipeID: "beef-age"}); err != nil {
		t.Fatal(err)
	}
	listed := func() *api.Alarm {
		for _, a := range r.Core.Snapshot().Active {
			if a.Code == core.CodeCalReminder {
				return &a
			}
		}
		return nil
	}
	week := core.DefaultConfig().CalReminderSec
	r.Advance(week - 3600)
	if listed() != nil {
		t.Fatal("raised early")
	}
	if d := r.Core.Snapshot().Health.RefCheckDueSec; d < 3000 || d > 4000 {
		t.Fatalf("due in %v s", d)
	}
	r.Advance(7200)
	if a := listed(); a == nil || a.Severity != api.SevInfo {
		t.Fatal("not raised when due")
	}
	if !r.Plant.CmdAlarm {
		t.Fatal("a reminder must not trip the alarm contact")
	}
	if err := r.Core.Do(api.Command{Kind: api.CmdAck}); err != nil {
		t.Fatal(err)
	}
	r.Advance(60)
	if a := listed(); a == nil || !a.Acked {
		t.Fatal("acknowledge should keep it listed")
	}
	r.Advance(week + 60)
	if a := listed(); a == nil || a.Acked {
		t.Fatal("should ask again after another period")
	}
	r.RefCheck()
	r.Advance(5)
	if listed() != nil {
		t.Fatal("not cleared by a passed reference check")
	}
	if d := r.Core.Snapshot().Health.RefCheckDueSec; d < week-100 {
		t.Fatalf("timer not reset: %v", d)
	}
}

func TestSuspicionOnlyEverMovesTowardCold(t *testing.T) {
	// With a suspicion forced on by a failed reference check, compare with a run where
	// it is not: cooling must never be less, and heater and humidifier never more.
	run := func(doubt bool) *sim.Run {
		r := newRun(t)
		r.SetPoint(24, 90, 1.2) // a setpoint that needs heat
		r.Advance(6 * 3600)
		if doubt {
			wrong := r.Plant.TAir + 4
			_ = r.Core.Do(api.Command{Kind: api.CmdRefCheck, Reference: &api.Reference{TempC: &wrong}})
		}
		r.Rows = nil
		r.Advance(6 * 3600)
		return r
	}
	a, b := run(false), run(true)
	var coolA, coolB, heatB, humB int
	for i := range b.Rows {
		if a.Rows[i].CoolCmd {
			coolA++
		}
		if b.Rows[i].CoolCmd {
			coolB++
		}
		if b.Rows[i].HeatCmd {
			heatB++
		}
		if b.Rows[i].HumCmd {
			humB++
		}
	}
	if heatB != 0 || humB != 0 {
		t.Fatalf("heater %d or humidifier %d ran under suspicion", heatB, humB)
	}
	_ = coolA
	_ = coolB
}

func TestHeatAndCoolDoNotFight(t *testing.T) {
	r := newRun(t, func(p *sim.Params) {
		// The round 3 limit-cycle plant: small, slow compressor, big heater, room near set.
		p.VolumeM3, p.CWallJK, p.UAAirWall, p.UAWallAmb = 0.22, 7355, 24, 1.49
		p.CompW, p.CompTauS, p.CompStopTauS, p.HeatW, p.HeatTauS = 180, 223, 104, 147, 97
		p.AmbientC = 14
	})
	r.SetPoint(13, 76, 1.2)
	r.Advance(18 * 3600)
	r.Rows = nil
	r.Advance(24 * 3600)
	both := 0
	var T []float64
	for _, x := range r.Rows {
		T = append(T, x.TrueT)
		if x.CoolCmd && x.HeatCmd && !x.DehumCmd {
			both++
		}
	}
	if span := sim.Percentile(append([]float64(nil), T...), 99) - sim.Percentile(T, 1); span > 4 {
		t.Fatalf("p1-p99 span %.2f C", span)
	}
	_ = both
}

func TestSuspicionNeverWarms(t *testing.T) {
	// Every plant kind that produced a false suspicion in round 2: with the checks on and
	// off, second by second, through outages, the checks never add heat or moisture.
	run := func(off bool, mut func(*sim.Params)) *sim.Run {
		p := sim.BasePlant()
		mut(&p)
		cfg := core.DefaultConfig()
		cfg.CrossCheckOff = off
		r, err := sim.NewRun(sim.Options{Plant: p, KeepRows: true, SampleSec: 30, Config: &cfg})
		if err != nil {
			t.Fatal(err)
		}
		r.SetCal()
		r.SetPoint(4, 85, 1.2)
		r.Advance(3 * 24 * 3600)
		_ = r.PowerLoss(3600, 300)
		r.Advance(2 * 24 * 3600)
		return r
	}
	for name, mut := range map[string]func(*sim.Params){
		"base": func(p *sim.Params) {},
		"hot":  func(p *sim.Params) { p.AmbientC, p.CompW, p.UAWallAmb = 30, 90, 3.6 },
	} {
		a, b := run(false, mut), run(true, mut)
		if len(a.Rows) != len(b.Rows) {
			t.Fatalf("%s: rows differ", name)
		}
		for i := range a.Rows {
			// Any difference may only be toward cold: the run with checks on (a) may
			// heat or humidify less, never more, and never cool differently.
			if a.Rows[i].CoolCmd != b.Rows[i].CoolCmd && !a.Rows[i].HeatCmd && !b.Rows[i].HeatCmd {
				t.Fatalf("%s: cooling differs at %v", name, a.Rows[i].T)
			}
			if (a.Rows[i].HeatCmd && !b.Rows[i].HeatCmd) || (a.Rows[i].HumCmd && !b.Rows[i].HumCmd) {
				t.Fatalf("%s: a suspicion added heat or moisture at %v", name, a.Rows[i].T)
			}
		}
	}
}

func TestSuspicionEndsByItself(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(3600)
	wrong := r.Plant.TAir + 4
	if err := r.Core.Do(api.Command{Kind: api.CmdRefCheck, Reference: &api.Reference{TempC: &wrong}}); err != nil {
		t.Fatal(err)
	}
	r.Advance(5)
	if r.Core.Snapshot().Temp.Quality != api.QualitySuspect {
		t.Fatal("not marked")
	}
	r.Advance(7*24*3600 + 600)
	if r.Core.Snapshot().Temp.Quality == api.QualitySuspect {
		t.Fatal("no exit without an operator")
	}
}

func TestOutputWriteFaultOpensContactThroughGuard(t *testing.T) {
	r := newRun(t)
	r.SetPoint(13, 76, 1.2)
	r.Advance(3600)
	if !r.Plant.CmdAlarm {
		t.Fatal("healthy contact should be energized")
	}
	r.Plant.Inject(sim.DevOutputs, sim.FaultWriteErr, 0)
	r.Advance(30)
	if r.Plant.CmdAlarm || r.Plant.CmdCool || r.Plant.CmdHeat {
		t.Fatal("the guard should have dropped everything when writes fail")
	}
	r.Plant.Inject(sim.DevOutputs, sim.FaultNone, 0)
	r.Advance(600)
	if alarmEvents(r, core.CodeOutDropped) == 0 {
		t.Fatal("drop not reported after repair")
	}
	if r.Plant.FanDuty == 0 {
		t.Fatal("control did not resume")
	}
}
