//go:build demoinject

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

type realCoreFlags struct {
	on       *bool
	rate     *float64
	faultAt  *time.Duration
	repairAt *time.Duration
}

func addRealCoreFlags(fs *flag.FlagSet) *realCoreFlags {
	return &realCoreFlags{
		on:       fs.Bool("realcore", false, "drive the panel from the real control core on the simulated plant"),
		rate:     fs.Float64("sim-rate", 30, "simulated seconds per real second"),
		faultAt:  fs.Duration("fault-at", 0, "real time after start to kill the simulated temperature sensor"),
		repairAt: fs.Duration("repair-at", 0, "real time after start to repair it"),
	}
}

// liveCore serialises the panel's calls with the simulation stepping the core.
type liveCore struct {
	mu  sync.Mutex
	run *sim.Run
}

func (l *liveCore) Snapshot() api.Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.run.Core.Snapshot()
}

func (l *liveCore) Do(c api.Command) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.run.Core.Do(c)
	log.Printf("realcore do %s err=%v", c.Kind, err)
	return err
}

// startRealCore runs a real core against the simulated plant, in memory. Its
// snapshots say Simulated, and the panel marks them. Every alarm and the buzzer
// state are logged raw as they change.
func startRealCore(ctx context.Context, f *realCoreFlags) api.Controller {
	if f == nil || !*f.on {
		return nil
	}
	run, err := sim.NewRun(sim.Options{Plant: sim.BasePlant(), Seed: 1})
	if err != nil {
		log.Fatalf("realcore: %v", err)
	}
	run.Advance(2 * 3600)
	lc := &liveCore{run: run}
	start := time.Now()
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		var acc float64
		faulted, repaired := false, false
		last := ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			acc += *f.rate / 10
			lc.mu.Lock()
			for ; acc >= 1; acc-- {
				run.Tick()
			}
			el := time.Since(start)
			if *f.faultAt > 0 && !faulted && el >= *f.faultAt {
				run.Plant.Inject(sim.DevClimate, sim.FaultDead, 0)
				faulted = true
				log.Printf("realcore inject climate sensor dead")
			}
			if *f.repairAt > 0 && !repaired && el >= *f.repairAt {
				run.Plant.Inject(sim.DevClimate, sim.FaultNone, 0)
				repaired = true
				log.Printf("realcore repair climate sensor")
			}
			s := run.Core.Snapshot()
			lc.mu.Unlock()
			state := fmt.Sprintf("temp=%s rh=%s cool=%v heat=%v humid=%v silenced=%v history=%d active=", s.Temp.Quality, s.RH.Quality, s.Outputs.Cool, s.Outputs.Heat, s.Outputs.Humid, s.BuzzerSilenced, len(s.History))
			for _, a := range s.Active {
				state += fmt.Sprintf("%s/%s/acked=%v,", a.Code, a.Severity, a.Acked)
			}
			if state != last {
				log.Printf("realcore state %s", state)
				last = state
			}
		}
	}()
	return lc
}
