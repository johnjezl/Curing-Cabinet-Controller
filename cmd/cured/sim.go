package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
	"github.com/johnjezl/curing-cabinet-controller/internal/drivers"
	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

// simConfirmToken must be passed verbatim for the simulator to start.
const simConfirmToken = "not-a-real-cabinet"

// productionMarker is written by provisioning for a real cabinet. Its presence
// makes --sim impossible on that machine, whatever the command line says.
const productionMarker = "/etc/curing/production"

const (
	simStateDir = "/var/lib/curing-sim"
	simMaxSpeed = 5000
	maxVirtGap  = 30 * 24 * 3600.0
	ckptEvery   = 500 * time.Millisecond
	syncWaitMax = 60 * time.Second
	simTick     = 20 * time.Millisecond
	simBurstMax = 4000
	simPubEvery = 250 * time.Millisecond
	petEvery    = 250 * time.Millisecond
	simRecipeID = "test-12d"
)

// outage accounting modes. See ADR 0404.
const (
	outage1x         = "1x"
	outageCompressed = "compressed"
)

type simOptions struct {
	sim      bool
	speed    float64
	speedSet bool
	outage   string
	confirm  string
	// annunciate is the removed --sim-annunciate. It is parsed only to refuse it
	// with a reason: a simulated alarm must never reach a real line (ADR 0406).
	annunciate bool
}

func parseSimArgs(args []string) (simOptions, error) {
	var o simOptions
	fs := flag.NewFlagSet("cured", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.sim, "sim", false, "")
	fs.Float64Var(&o.speed, "speed", 0, "")
	fs.StringVar(&o.outage, "sim-outage", "", "")
	fs.StringVar(&o.confirm, "sim-confirm", "", "")
	fs.BoolVar(&o.annunciate, "sim-annunciate", false, "")
	if err := fs.Parse(args); err != nil {
		return o, fmt.Errorf("bad arguments: %v (no arguments means real hardware)", err)
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "speed" {
			o.speedSet = true
		}
	})
	return o, nil
}

// simGate is what stands between a command line and a simulated cabinet. A real
// cabinet running a simulation would look alive while controlling nothing, so
// every part must be deliberate.
func simGate(o simOptions, exists func(string) bool) error {
	var errs []error
	if !o.sim {
		errs = append(errs, errors.New("arguments given without --sim; real hardware takes no arguments"))
	}
	if !o.speedSet || o.speed < 1 || o.speed > simMaxSpeed {
		errs = append(errs, fmt.Errorf("--sim needs --speed between 1 and %d", simMaxSpeed))
	}
	if o.outage != outage1x && o.outage != outageCompressed {
		errs = append(errs, fmt.Errorf("--sim needs --sim-outage %s or %s", outage1x, outageCompressed))
	}
	if o.confirm != simConfirmToken {
		errs = append(errs, fmt.Errorf("--sim needs --sim-confirm=%s", simConfirmToken))
	}
	if o.annunciate {
		errs = append(errs, errors.New("--sim-annunciate was removed: a simulation never drives the real alarm contact or buzzer (ADR 0406)"))
	}
	if exists(productionMarker) {
		errs = append(errs, fmt.Errorf("%s exists: this machine is provisioned as a real cabinet", productionMarker))
	}
	return errors.Join(errs...)
}

// checkpoint is the plant's durable state, next to core's own files.
type checkpoint struct {
	Plant    sim.PlantState
	RealWall time.Time
	Uptime   float64
	BootID   string
	Clean    bool
	Seq      uint64
}

func bootID() string {
	b, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return string(b)
}

// outageRecord is one line of outages.jsonl, written before control starts.
type outageRecord struct {
	Seq           int     `json:"seq"`
	Mode          string  `json:"mode"`
	Speed         float64 `json:"speed"`
	NewBoot       bool    `json:"new_boot"`
	UptimeAtStart float64 `json:"uptime_at_start"`
	CkptAgeS      float64 `json:"real_gap_s"`
	GapSource     string  `json:"gap_source"`
	VirtGapS      float64 `json:"virtual_gap_s"`
	StaleBoundVS  float64 `json:"virtual_stale_bound_s"`
	Capped        bool    `json:"capped"`
	VirtAtCkpt    float64 `json:"virtual_s_at_checkpoint"`
	VirtAtResume  float64 `json:"virtual_s_at_resume"`
	TAirBefore    float64 `json:"t_air_before"`
	TAirAfter     float64 `json:"t_air_after"`
	TProdBefore   float64 `json:"t_prod_before"`
	TProdAfter    float64 `json:"t_prod_after"`
	RHBefore      float64 `json:"rh_before"`
	RHAfter       float64 `json:"rh_after"`
}

// virtualGap turns a real gap into virtual seconds. Pure, so it is unit tested.
func virtualGap(mode string, speed, realGap float64) (gap, stale float64, capped bool) {
	if realGap < 0 {
		realGap = 0
	}
	gap = realGap
	if mode == outageCompressed {
		gap = realGap * speed
	}
	if gap > maxVirtGap {
		gap, capped = maxVirtGap, true
	}
	return gap, speed * 2 * ckptEvery.Seconds(), capped
}

// resolveGap finds how long ago the checkpoint was written, by the real clock. A
// Pi with no battery has no idea of the time after a power cut until NTP answers,
// so wait for that, bounded. Failing that, uptime is a lower bound and says so.
func resolveGap(ctx context.Context, ck *checkpoint) (float64, string) {
	if ck.BootID == bootID() {
		// Same boot: uptime is continuous, and better than any wall clock.
		return uptimeNow() - ck.Uptime, "uptime-same-boot"
	}
	deadline := time.Now().Add(syncWaitMax)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if statExists("/run/systemd/timesync/synchronized") {
			return time.Since(ck.RealWall).Seconds(), "wall-clock-ntp"
		}
		sleepCtx(ctx, 250*time.Millisecond)
	}
	return uptimeNow(), "uptime-lower-bound-no-ntp"
}

// simState is the running simulator.
type simState struct {
	o      simOptions
	r      *runtime
	pl     *sim.Plant
	clk    *sim.Clock
	f      *lineFilter
	inbox  chan func()
	plFS   *core.OSFS
	seq    uint64
	saving atomic.Bool

	pub      atomic.Pointer[simPub]
	outages  []outageRecord
	outageMu sync.Mutex

	injectPanic atomic.Bool
	injectHang  atomic.Bool
	uiFault     atomic.Int32
	realStart   time.Time
}

// simPub is ground truth from the plant, published so it stays readable if the
// simulation loop is hung.
type simPub struct {
	VirtualS   float64  `json:"virtual_s"`
	VirtualDay float64  `json:"virtual_day"`
	RealS      float64  `json:"real_s"`
	Speed      float64  `json:"speed_set"`
	Achieved   float64  `json:"speed_achieved"`
	LagVS      float64  `json:"lag_virtual_s"`
	TAir       float64  `json:"true_air_c"`
	TProd      float64  `json:"true_product_c"`
	TWall      float64  `json:"true_wall_c"`
	RH         float64  `json:"true_rh"`
	ProdKg     float64  `json:"true_product_kg"`
	LossPct    float64  `json:"true_water_loss_pct"`
	Door       bool     `json:"door_open"`
	Powered    bool     `json:"powered"`
	Cool       bool     `json:"cool_cmd"`
	Heat       bool     `json:"heat_cmd"`
	Humid      bool     `json:"humid_cmd"`
	Dehum      bool     `json:"dehumid_cmd"`
	EnergyWh   float64  `json:"energy_wh"`
	Faults     []string `json:"faults"`
	OutageMode string   `json:"outage_mode"`
}

var devNames = map[string]sim.Device{
	"climate": sim.DevClimate, "temp": sim.DevTemp, "rh": sim.DevRH, "air": sim.DevAir, "scale": sim.DevScale,
	"fan": sim.DevFan, "tach": sim.DevTach, "duct": sim.DevDuct, "cool": sim.DevCool, "heat": sim.DevHeat,
	"humid": sim.DevHumid, "dehumid": sim.DevDehumid, "compressor": sim.DevCompressor, "humidifier": sim.DevHumidifier,
	"door-switch": sim.DevDoorSwitch, "outputs": sim.DevOutputs,
}

var faultNames = map[string]sim.FaultKind{
	"dead": sim.FaultDead, "bad-read": sim.FaultBadRead, "stuck": sim.FaultStuck, "spike": sim.FaultSpike,
	"drift": sim.FaultDrift, "offset": sim.FaultOffset, "range": sim.FaultRange,
	"relay-stuck-on": sim.FaultRelayStuckOn, "relay-stuck-off": sim.FaultRelayStuckOff, "stall": sim.FaultStall,
	"blocked": sim.FaultBlocked, "no-cool": sim.FaultNoCool, "empty": sim.FaultEmpty, "write-err": sim.FaultWriteErr,
}

// activeFaults names what is injected, by reading the saved state's table.
func activeFaults(s sim.PlantState) []string {
	var out []string
	for dn, d := range devNames {
		k := s.FaultKinds[d]
		if k == sim.FaultNone {
			continue
		}
		name := fmt.Sprintf("%d", int(k))
		for fn, f := range faultNames {
			if f == k {
				name = fn
			}
		}
		out = append(out, dn+":"+name)
	}
	sort.Strings(out)
	return out
}

// rateWD limits how often the virtual-time control loop reaches the real watchdog.
type rateWD struct {
	inner hal.Watchdog
	last  time.Time
	err   error
}

// Pet implements hal.Watchdog.
func (w *rateWD) Pet() error {
	if now := time.Now(); now.Sub(w.last) >= petEvery {
		w.last = now
		w.err = w.inner.Pet()
	}
	return w.err
}

func (s *simState) saveCheckpoint(clean bool) {
	st := s.pl.SaveState()
	s.seq++
	ck := checkpoint{Plant: st, RealWall: time.Now(), Uptime: uptimeNow(), BootID: bootID(), Clean: clean, Seq: s.seq}
	write := func() {
		b, err := json.Marshal(ck)
		if err == nil {
			err = atomicWrite(filepath.Join(s.plFS.Dir, "plant.json"), b)
		}
		if err != nil {
			logfJ("<4>cured: plant checkpoint: %v", err)
		}
	}
	if clean {
		write()
		return
	}
	if s.saving.CompareAndSwap(false, true) {
		go func() { defer s.saving.Store(false); write() }()
	}
}

func (s *simState) publish(done float64) {
	pl := s.pl
	real := time.Since(s.realStart).Seconds()
	st := pl.SaveState()
	p := &simPub{VirtualS: pl.T, VirtualDay: pl.T / 86400, RealS: real, Speed: s.o.speed, LagVS: real*s.o.speed - done,
		TAir: pl.TAir, TProd: pl.TProd, TWall: pl.TWall, RH: pl.RH(), ProdKg: pl.ProdKg, LossPct: pl.WaterLossPct(),
		Door: pl.DoorIsOpen, Powered: pl.Powered, Cool: pl.CmdCool, Heat: pl.CmdHeat, Humid: pl.CmdHumid, Dehum: pl.CmdDehumid,
		EnergyWh: pl.EnergyWh(), Faults: activeFaults(st), OutageMode: s.o.outage}
	if real > 0 {
		p.Achieved = done / real
	}
	s.pub.Store(p)
}

// atomicWrite replaces path so that a reader sees the old or the new content,
// never a torn file, whatever kills the process or the power. core's ReplaceSync
// writes in place, which suits its two alternating slots but not a single file.
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// loadOrNewPlant restores the plant and accounts for the time it was not running.
func (s *simState) loadOrNewPlant(ctx context.Context) (fresh bool, err error) {
	data, rerr := s.plFS.ReadFile("plant.json")
	if rerr != nil {
		s.pl = sim.NewPlant(sim.BasePlant())
		logfJ("<5>cured: sim: new plant at %.1f C", s.pl.TAir)
		return true, nil
	}
	var ck checkpoint
	if jerr := json.Unmarshal(data, &ck); jerr != nil {
		logfJ("<2>cured: sim: plant checkpoint unusable (%v): starting a new plant, so its history is lost", jerr)
		s.pl = sim.NewPlant(sim.BasePlant())
		return true, nil
	}
	s.pl = sim.RestorePlant(ck.Plant, ck.Plant.P.Seed+uint64(ck.Plant.T)+1)
	s.seq = ck.Seq
	s.pl.Powered = true
	s.pl.PowerOff() // whatever ended the last run, the relays are open now
	if ck.Clean {
		s.pl.PowerOn()
		logfJ("<5>cured: sim: clean resume at virtual %.0f s, no gap", s.pl.T)
		return false, nil
	}
	gapReal, source := resolveGap(ctx, &ck)
	gap, stale, capped := virtualGap(s.o.outage, s.o.speed, gapReal)
	rec := outageRecord{Mode: s.o.outage, Speed: s.o.speed, NewBoot: ck.BootID != bootID(), UptimeAtStart: uptimeNow(),
		CkptAgeS: gapReal, GapSource: source, VirtGapS: gap, StaleBoundVS: stale, Capped: capped, VirtAtCkpt: s.pl.T,
		TAirBefore: s.pl.TAir, TProdBefore: s.pl.TProd, RHBefore: s.pl.RH()}
	s.pl.Step(gap + stale)
	s.pl.PowerOn()
	rec.VirtAtResume, rec.TAirAfter, rec.TProdAfter, rec.RHAfter = s.pl.T, s.pl.TAir, s.pl.TProd, s.pl.RH()
	s.appendOutage(rec)
	return false, nil
}

func (s *simState) appendOutage(rec outageRecord) {
	s.loadOutages()
	s.outageMu.Lock()
	defer s.outageMu.Unlock()
	rec.Seq = len(s.outages) + 1
	s.outages = append(s.outages, rec)
	b, _ := json.Marshal(rec)
	logfJ("<4>cured: sim: outage accounted %s", b)
	f, err := s.plFS.OpenAppend("outages.jsonl")
	if err != nil {
		logfJ("<4>cured: sim: outages log: %v", err)
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
	_ = f.Sync()
}

func (s *simState) loadOutages() {
	s.outageMu.Lock()
	defer s.outageMu.Unlock()
	if s.outages != nil {
		return
	}
	s.outages = []outageRecord{}
	data, err := s.plFS.ReadFile("outages.jsonl")
	if err != nil {
		return
	}
	dec := json.NewDecoder(bytesReader(data))
	for {
		var r outageRecord
		if dec.Decode(&r) != nil {
			return
		}
		s.outages = append(s.outages, r)
	}
}

func (s *simState) outagesCopy() []outageRecord {
	s.loadOutages()
	s.outageMu.Lock()
	defer s.outageMu.Unlock()
	return append([]outageRecord{}, s.outages...)
}

// runSim runs the controller against the simulated plant in compressed time. It
// never opens the output guard, so no simulated state can reach any real line:
// not a plant relay, and not the alarm contact or the buzzer. The guard holds
// every line off, so the energize-to-run contact reads "not controlling a
// cabinet" for as long as the simulator runs, which is true. See ADR 0406.
func runSim(ctx context.Context, o simOptions) error {
	start := time.Now()
	logfJ("<4>cured: start mode=SIMULATED speed=%.0f outage=%s lines=never-driven version=%s uptime=%.3f", o.speed, o.outage, version, uptimeNow())
	r := &runtime{mode: "simulated", started: start, upStart: uptimeNow(), safeCfg: drivers.DefaultConfig()}
	r.lastTick.Store(start.UnixNano())
	plFS, err := core.NewOSFS(filepath.Join(simStateDir, "plant"))
	if err != nil {
		return err
	}
	s := &simState{o: o, r: r, plFS: plFS, inbox: make(chan func(), 32), realStart: start}
	fresh, err := s.loadOrNewPlant(ctx)
	if err != nil {
		return err
	}
	s.clk = sim.NewClock(true)
	s.clk.VT, s.clk.BootVT = s.pl.T, s.pl.T
	r.sys = newSysAlarms(s.clk.Now, s.clk.Trusted, logfJ)
	r.sys.Persist(filepath.Join(simStateDir, "system-alarms.json"))
	r.checkSafe()
	r.noteStart()

	// systemd's watchdog, and a simulated guard on the simulated plant's relays in
	// virtual time. The real guard is never asked for anything (ADR 0406).
	wd := drivers.NewWatchdog()
	guard := sim.NewSimGuard(s.pl)
	sysWD := wd
	cab := s.pl.Cabinet()
	s.f = &lineFilter{next: cab.Outputs, forward: true}
	cab.Outputs = s.f
	r.ann = newAnnunciator(nil, s.f, r.sys, start) // decides the simulated contact and buzzer; drives nothing
	r.linesDriven = false

	osfs, err := core.NewOSFS(filepath.Join(simStateDir, "core"))
	if err != nil {
		return err
	}
	coreFS := &groupSyncFS{OSFS: osfs, every: simSyncEvery}
	var lastPet atomic.Int64
	lastPet.Store(start.UnixNano())
	hold := &outputHold{lastPet: &lastPet}
	wdw := watchedWD{inner: simWatchdogs{guard: guard, sys: &rateWD{inner: wd}}, last: &lastPet, on: func(e error) {
		if errors.Is(e, hal.ErrOutputsDropped) {
			r.ann.Dropped()
		}
	}}
	c, err := core.New(core.Options{Cabinet: cab, Clock: s.clk, FS: coreFS, Simulated: true, Speed: o.speed, Version: version, AsyncDepth: 1024, Watchdog: wdw, Trace: traceBoot,
		OSBootID: strings.TrimSpace(bootID())})
	if err != nil {
		return err
	}
	r.c = c
	r.v = &view{c: c, sys: r.sys}
	r.ann.coreEdges = c.EdgeCount
	r.sys.SetRecord(c.RecordSystem)
	r.wireOpenings(filepath.Join(simStateDir, "contact"))
	r.uiCtl = faultyUI{Controller: r.v, hook: s.uiPanicHook}
	r.statusX = func() any { return s.pub.Load() }
	if fresh {
		k := api.Calibration{ScaleZero: int32(s.pl.P.ScaleTareCounts), ScaleCountsPG: s.pl.P.ScaleCountsPerG}
		if err := c.Do(api.Command{Kind: api.CmdSetCal, Calibration: &k}); err != nil {
			logfJ("<3>cured: sim: calibration: %v", err)
		}
	}
	s.publish(0)

	if err := s.serve(ctx); err != nil {
		logfJ("<3>cured: sim control socket: %v", err)
	}
	uiCtx, uiStop := context.WithCancel(context.Background())
	uiDone := make(chan struct{})
	go func() { defer close(uiDone); r.runUI(uiCtx, filepath.Join(simStateDir, "panel.json")) }()
	supCtx, supStop := context.WithCancel(context.Background())
	defer supStop()
	go r.supervise(supCtx)

	// One virtual second is one control tick, as on real hardware. If the Pi cannot
	// keep up, virtual time falls behind and lag_virtual_s says by how much.
	t := time.NewTicker(simTick)
	defer t.Stop()
	begin := time.Now()
	done := 0.0
	var lastPub, lastCk time.Time
	first := true
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			continue
		case <-t.C:
		}
		if s.injectPanic.Load() {
			panic("injected control panic, simulator only")
		}
		if s.injectHang.Load() {
			logfJ("<2>cured: sim: injected control hang: loop stops here")
			select {} //nolint: intended
		}
		for drained := false; !drained; {
			select {
			case f := <-s.inbox:
				f()
			default:
				drained = true
			}
		}
		target := time.Since(begin).Seconds() * o.speed
		n := 0
		for done+1 <= target && n < simBurstMax {
			s.pl.Step(1)
			s.clk.Set(s.pl.T)
			c.Step()
			guard.Tick(s.pl.T)
			done++
			n++
		}
		now := time.Now()
		r.ticks.Add(uint64(n))
		r.lastTick.Store(now.UnixNano())
		if now.Sub(lastPub) >= simPubEvery {
			lastPub = now
			q := api.Quality("")
			if armed, _, _ := r.ann.State(); !armed {
				q = c.Snapshot().Temp.Quality
			}
			held := hold.Check(now, func() bool { return hasActive(c.Snapshot(), "OUTPUT_FAULT") })
			r.ann.tick(now, q, held)
			s.publish(done)
		}
		if now.Sub(lastCk) >= ckptEvery {
			lastCk = now
			s.saveCheckpoint(false)
		}
		if first && n > 0 {
			first = false
			r.noteFirstStep()
			if err := sysWD.Ready(); err != nil {
				logfJ("<4>cured: ready: %v", err)
			}
			r.noteReady()
		}
	}
	_ = sysWD.Stopping()
	logfJ("<5>cured: stopping")
	force := time.AfterFunc(stopBound, shutdownStuck)
	defer force.Stop()
	for s.saving.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	s.saveCheckpoint(true)
	stopAt := time.Now()
	err = errors.Join(c.Close(), coreFS.Flush())
	logfJ("<5>cured: core closed in %.2f s", time.Since(stopAt).Seconds())
	uiStop()
	select {
	case <-uiDone:
	case <-time.After(2 * time.Second):
	}
	return err
}

// simWatchdogs pets the simulated guard on every virtual step, which is what its
// virtual-time lease needs, and systemd at most every petEvery of real time.
type simWatchdogs struct {
	guard *sim.SimGuard
	sys   *rateWD
}

// Pet implements hal.Watchdog.
func (w simWatchdogs) Pet() error { return errors.Join(w.guard.Pet(), w.sys.Pet()) }

// simSyncEvery limits fsyncs of core's append-only files in compressed time.
const simSyncEvery = time.Second

// stateSyncEvery limits how often core's state slots really reach the card.
const stateSyncEvery = 5 * time.Second

// groupSyncFS makes appended data durable at most once per real interval. core logs
// a line a virtual minute and syncs each one. That is nothing at 1x, and about 12
// fsyncs a second at 700x, more than the rig's card does, so without this the queue
// backs up and the store reports a storage fault that only compression causes. The
// price: a real power cut in the simulator can lose up to this much real time of
// log lines, which is more virtual time than the real system would ever lose. Torn
// tails are left for core's recovery to handle, as on real hardware. See ADR 0404.
type groupSyncFS struct {
	*core.OSFS
	every time.Duration

	mu      sync.Mutex
	last    time.Time
	pending map[string][]byte
}

// ReplaceSync implements core.FS. core's state checkpoints come every virtual ten
// minutes, about once a second at 700x, and each costs several fsyncs. In between
// real intervals the newest per name waits, and Flush writes what is left. core's log
// carries progress across whatever a real power cut loses from here.
func (g *groupSyncFS) ReplaceSync(name string, data []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if time.Since(g.last) < stateSyncEvery {
		if g.pending == nil {
			g.pending = map[string][]byte{}
		}
		g.pending[name] = append([]byte(nil), data...)
		return nil
	}
	g.last = time.Now()
	delete(g.pending, name)
	return g.OSFS.ReplaceSync(name, data)
}

// Flush writes every state file still waiting. Run it after core.Close.
func (g *groupSyncFS) Flush() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	var errs []error
	for name, data := range g.pending {
		errs = append(errs, g.OSFS.ReplaceSync(name, data))
	}
	g.pending = nil
	return errors.Join(errs...)
}

// OpenAppend implements core.FS.
func (g *groupSyncFS) OpenAppend(name string) (core.File, error) {
	f, err := g.OSFS.OpenAppend(name)
	if err != nil {
		return nil, err
	}
	return &groupSyncFile{File: f, every: g.every}, nil
}

type groupSyncFile struct {
	core.File
	every time.Duration
	last  time.Time
}

// Sync implements core.File.
func (f *groupSyncFile) Sync() error {
	if now := time.Now(); now.Sub(f.last) >= f.every {
		f.last = now
		return f.File.Sync()
	}
	return nil
}

// Close implements core.File: what was deferred becomes durable now.
func (f *groupSyncFile) Close() error {
	_ = f.File.Sync()
	return f.File.Close()
}

func (s *simState) uiPanicHook() {
	switch s.uiFault.Load() {
	case 1:
		s.uiFault.Store(0)
		panic("injected panel snapshot panic, simulator only")
	case 2:
		time.Sleep(30 * time.Second)
		s.uiFault.CompareAndSwap(2, 0)
	}
}

// testRecipe is a short full cure with phase changes, for the compressed run.
func testRecipe() *api.Recipe {
	d := 24 * time.Hour
	return &api.Recipe{ID: simRecipeID, Name: "Test cure 12 d (simulator)", Notes: "Evidence run only.",
		Phases: []api.PhaseSpec{
			{Name: "Ferment", Setpoint: api.Setpoints{TempC: 24, RHPct: 90, AirMS: 1.0}, Duration: 2 * d},
			{Name: "Dry early", Setpoint: api.Setpoints{TempC: 14, RHPct: 82, AirMS: 1.0}, Duration: 3 * d},
			{Name: "Dry", Setpoint: api.Setpoints{TempC: 13, RHPct: 76, AirMS: 1.0}, Duration: 7 * d},
		}}
}
