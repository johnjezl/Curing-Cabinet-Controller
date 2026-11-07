package sim

import (
	"context"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
)

// LiveOptions set up core plus plant running against the real clock at a speed
// factor. This is what a daemon uses to drive the display with no hardware.
type LiveOptions struct {
	Plant Params
	Speed float64 // virtual seconds per real second; 0 means 1
	// FS holds the state. Nil means memory only.
	FS core.FS
	// Setpoint at first start. Stored settings win if they exist.
	Version string
}

// Live is a simulated cabinet running continuously.
type Live struct {
	opts  LiveOptions
	plant *Plant
	clock *Clock
	core  *core.Core
	inbox chan func(*Plant)
}

// NewLive builds the simulated system. Run starts it.
func NewLive(o LiveOptions) (*Live, error) {
	if o.Speed <= 0 {
		o.Speed = 1
	}
	if o.FS == nil {
		o.FS = core.NewMemFS()
	}
	pl := NewPlant(o.Plant)
	clk := NewClock(true)
	c, err := core.New(core.Options{Cabinet: pl.Cabinet(), Clock: clk, FS: o.FS, Simulated: true, Speed: o.Speed, Version: o.Version})
	if err != nil {
		return nil, err
	}
	k := api.Calibration{ScaleZero: int32(pl.P.ScaleTareCounts), ScaleCountsPG: pl.P.ScaleCountsPerG}
	_ = c.Do(api.Command{Kind: api.CmdSetCal, Calibration: &k})
	return &Live{opts: o, plant: pl, clock: clk, core: c, inbox: make(chan func(*Plant), 16)}, nil
}

// Controller is what the front ends talk to. Its Health says Simulated.
func (l *Live) Controller() api.Controller { return l.core }

// Core returns the controller itself.
func (l *Live) Core() *core.Core { return l.core }

// Disturb runs f on the plant between ticks, so that a demo can open the door,
// load product or inject a fault without racing the simulation.
func (l *Live) Disturb(f func(*Plant)) {
	select {
	case l.inbox <- f:
	default:
	}
}

// Run steps plant and core until ctx ends. Each second of virtual time is one
// control tick, as on real hardware. If the machine cannot keep up, virtual time
// falls behind rather than skipping ticks.
func (l *Live) Run(ctx context.Context) error {
	tick := time.NewTicker(time.Duration(float64(time.Second) / l.opts.Speed))
	if l.opts.Speed > 200 {
		tick.Reset(5 * time.Millisecond)
	}
	defer tick.Stop()
	start := time.Now()
	done := 0.0
	for {
		select {
		case <-ctx.Done():
			return l.core.Close()
		case <-tick.C:
		}
		for {
			select {
			case f := <-l.inbox:
				f(l.plant)
				continue
			default:
			}
			break
		}
		target := time.Since(start).Seconds() * l.opts.Speed
		for done+1 <= target {
			l.plant.Step(1)
			l.clock.Set(l.plant.T)
			l.core.Step()
			done++
			if ctx.Err() != nil {
				break
			}
		}
	}
}
