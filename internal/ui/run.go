package ui

import (
	"context"
	"fmt"
	"image"
	"log"
	"sync/atomic"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// Display receives finished frames. It must not block for long.
type Display interface {
	Present(*image.RGBA) error
}

// FrameInfo describes one presented frame, for latency measurement.
type FrameInfo struct {
	Screen     string
	InputAt    time.Time // oldest input since the last frame; zero if none
	RenderedAt time.Time
	PresentAt  time.Time
	Action     string
	Err        error
}

// LoopOptions holds observers for the UI loop. All optional.
type LoopOptions struct {
	OnFrame func(FrameInfo)
	Power   func(Power)
}

const pollEvery = 500 * time.Millisecond

// pollController fetches snapshots on its own goroutine so a hung or panicking
// controller only makes the panel report a stall. It holds nothing the control
// loop needs. If Snapshot hangs, this goroutine waits; nothing piles up.
func pollController(ctx context.Context, ctl api.Controller, out chan api.Snapshot) {
	get := func() (s api.Snapshot, ok bool) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("ui: Snapshot panicked: %v", r)
				ok = false
			}
		}()
		return ctl.Snapshot(), true
	}
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		if s, ok := get(); ok {
			select {
			case <-out: // drop the older one nobody read yet
			default:
			}
			select {
			case out <- s:
			default:
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type cmdResult struct {
	cmd api.Command
	err error
}

// commandWorker calls Do on its own goroutine, one at a time.
func commandWorker(ctx context.Context, ctl api.Controller, in <-chan api.Command, out chan<- cmdResult) {
	do := func(c api.Command) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("controller error: %v", r)
			}
		}()
		return ctl.Do(c)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-in:
			err := do(c)
			select {
			case out <- cmdResult{c, err}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// freezeWatch replaces the screen if frames stop, whatever stopped them. The
// render loop cannot report its own hang, so this runs beside it. It paints the
// notice once on entering the state and then every 5 s while it lasts.
func freezeWatch(ctx context.Context, d NoticeDisplay, after time.Duration, last *atomic.Int64) {
	t := time.NewTicker(after / 5)
	defer t.Stop()
	var painted time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			age := now.Sub(time.Unix(0, last.Load()))
			if age < after {
				painted = time.Time{}
				continue
			}
			if painted.IsZero() || now.Sub(painted) >= 5*time.Second {
				log.Printf("ui: no frame for %s: painting frozen notice", age.Round(time.Second))
				if err := d.PaintNotice(FrozenFrame()); err != nil {
					log.Printf("ui: frozen notice: %v", err)
				}
				painted = now
			}
		}
	}
}

// persister writes panel settings on its own goroutine, newest first, so a slow
// or failing card cannot hold up the loop.
type persister struct {
	path    string
	in      chan Settings
	results chan error
	latest  atomic.Pointer[Settings]
}

func (s *persister) submit(v Settings) {
	s.latest.Store(&v)
	select {
	case <-s.in:
	default:
	}
	select {
	case s.in <- v:
	default:
	}
}

func (s *persister) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case v := <-s.in:
			err := SaveSettings(s.path, v)
			if err != nil {
				log.Printf("ui: save settings: %v", err)
			}
			select {
			case s.results <- err:
			case <-ctx.Done():
				return
			}
		}
	}
}

// RunLoop is the UI loop. It returns only when ctx ends. A panic inside the loop
// is contained here and restarts the loop with a new Panel, which keeps the lock
// state and the code. On return the screen is told the panel has stopped.
func RunLoop(ctx context.Context, ctl api.Controller, cfg Config, disp Display, input <-chan TouchEvent, opts LoopOptions) {
	cfg.defaults()
	snaps := make(chan api.Snapshot, 1)
	cmds := make(chan api.Command, 1)
	results := make(chan cmdResult, 1)
	go pollController(ctx, ctl, snaps)
	go commandWorker(ctx, ctl, cmds, results)
	var store *persister
	if cfg.StatePath != "" {
		s, err := loadBounded(cfg.StatePath, 2*time.Second)
		if err != nil {
			log.Printf("ui: load settings: %v", err)
			cfg.SettingsErr = err.Error()
		}
		cfg.Settings = s
		store = &persister{path: cfg.StatePath, in: make(chan Settings, 1), results: make(chan error, 1)}
		store.latest.Store(&s)
		cfg.Persist = store.submit
		go store.run(ctx)
	}
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	nd, canNotice := disp.(NoticeDisplay)
	if canNotice {
		go freezeWatch(ctx, nd, cfg.FrozenAfter, &last)
	}
	// The loop runs in its own goroutine so a display stuck in Present cannot hold up
	// a service stop. If it is stuck, it is simply abandoned when the process exits.
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		for ctx.Err() == nil {
			if store != nil {
				cfg.Settings = *store.latest.Load()
			}
			runOnce(ctx, cfg, disp, input, snaps, cmds, results, store, &last, opts)
			if ctx.Err() == nil {
				time.Sleep(time.Second)
			}
		}
	}()
	<-ctx.Done()
	select {
	case <-loopDone:
	case <-time.After(500 * time.Millisecond):
		log.Printf("ui: display did not return; stopping anyway")
	}
	if canNotice {
		if err := nd.PaintNotice(StoppedFrame()); err != nil {
			log.Printf("ui: stopped notice: %v", err)
		}
	}
}

func runOnce(ctx context.Context, cfg Config, disp Display, input <-chan TouchEvent, snaps <-chan api.Snapshot, cmds chan<- api.Command, results <-chan cmdResult, store *persister, last *atomic.Int64, opts LoopOptions) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ui: loop panic contained: %v", r)
		}
	}()
	var saved chan error
	if store != nil {
		saved = store.results
	}
	var p *Panel
	send := func(c api.Command) {
		select {
		case cmds <- c:
		default:
			p.CommandDone(c, fmt.Errorf("controller is busy; try again"))
		}
	}
	p = NewPanel(cfg, time.Now, send)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	var inputAt time.Time
	lastPower := PowerOn
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-snaps:
			p.SetSnapshot(s)
		case r := <-results:
			p.CommandDone(r.cmd, r.err)
		case err := <-saved:
			p.SettingsSaved(err)
		case e := <-input:
			if inputAt.IsZero() {
				inputAt = time.Now()
			}
			p.Input(e)
		case <-tick.C:
		}
		p.Tick()
		if pw := p.PowerState(); pw != lastPower {
			lastPower = pw
			if opts.Power != nil {
				opts.Power(pw)
			}
		}
		if p.dirty {
			img := p.Render()
			rendered := time.Now()
			err := disp.Present(img)
			if err != nil {
				log.Printf("ui: present: %v", err)
			} else {
				last.Store(time.Now().UnixNano())
			}
			if opts.OnFrame != nil {
				opts.OnFrame(FrameInfo{Screen: p.Screen(), InputAt: inputAt, RenderedAt: rendered, PresentAt: time.Now(), Action: p.LastAction, Err: err})
			}
			inputAt = time.Time{}
		}
	}
}

// Run is the production entry point: real framebuffer, real touch device. It
// returns only when ctx ends, and it never returns an error to stop control: a
// missing display is logged and retried. Start it with `go ui.Run(...)`.
func Run(ctx context.Context, ctl api.Controller, cfg Config) {
	if cfg.StatePath == "" {
		cfg.StatePath = DefaultStatePath
	}
	touch := make(chan TouchEvent, 64)
	go ReadTouch(ctx, DefaultTouchName, touch)
	for ctx.Err() == nil {
		fb, err := OpenFramebuffer(DefaultFramebuffer)
		if err != nil {
			log.Printf("ui: framebuffer not available: %v", err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			continue
		}
		bl := OpenBacklight()
		bl.Set(PowerOn) // a previous run may have ended dimmed
		RunLoop(ctx, ctl, cfg, fb, touch, LoopOptions{Power: bl.Set})
		bl.Set(PowerOn)
		fb.Close()
		return
	}
}
