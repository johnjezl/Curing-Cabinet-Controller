// Package uitest drives a Panel headless on a fake clock, by screen coordinates.
// Only tests and cmd/uidemo use it. Production binaries do not link it.
package uitest

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui/fixtures"
)

// Harness owns a Panel, a fake controller and the fake clock.
type Harness struct {
	Panel *ui.Panel
	Ctl   *fixtures.Controller
	Now   time.Time
	Img   *image.RGBA
	Audit *ui.Audit
	queue []api.Command

	control api.Controller
	tick    func(time.Duration)
	// Errs records command refusals from a live controller, in order.
	Errs []error
}

// New builds a harness whose installer code is already set to the one in
// installerPath, so walks do not repeat the forced first change. NewFresh starts
// from a first boot instead.
func New(s api.Snapshot, cfg ui.Config) *Harness {
	if !cfg.Settings.HasOwnCode() {
		if err := cfg.Settings.SetCode("2580"); err != nil {
			panic(err)
		}
	}
	return NewFresh(s, cfg)
}

// NewFresh builds a harness with the given settings as they are.
func NewFresh(s api.Snapshot, cfg ui.Config) *Harness {
	h := &Harness{Now: fixtures.T0}
	if !s.Now.IsZero() {
		h.Now = s.Now // a snapshot from a real core keeps its own clock
	}
	cfg.Location = time.UTC
	h.Ctl = fixtures.NewController(s, func() time.Time { return h.Now })
	h.Panel = ui.NewPanel(cfg, func() time.Time { return h.Now }, func(c api.Command) { h.queue = append(h.queue, c) })
	h.Sync()
	h.Frame()
	return h
}

// Sync delivers commands and the newest snapshot, as the poller would.
func (h *Harness) Sync() {
	ctl := h.control
	if ctl == nil {
		ctl = h.Ctl
	}
	for _, c := range h.queue {
		err := ctl.Do(c)
		if err != nil {
			h.Errs = append(h.Errs, err)
		}
		h.Panel.CommandDone(c, err)
	}
	h.queue = nil
	h.Panel.SetSnapshot(ctl.Snapshot())
}

// Frame ticks and draws.
func (h *Harness) Frame() {
	h.Panel.Tick()
	h.Img = h.Panel.Render()
}

// Advance moves the fake clock in steps, as the running loop would.
func (h *Harness) Advance(d time.Duration) {
	const step = 50 * time.Millisecond
	end := h.Now.Add(d)
	for h.Now.Before(end) {
		n := step
		if rem := end.Sub(h.Now); rem < n {
			n = rem
		}
		h.Now = h.Now.Add(n)
		if h.tick != nil {
			h.tick(n)
		}
		h.Sync()
		h.Panel.Tick()
	}
	h.Frame()
}

// TapResult records one tap: action, screens and the real compute time.
type TapResult struct {
	Action         string
	Before, After  string
	FeedbackMicros int64 // down event to pressed frame drawn
	RedrawMicros   int64 // up event to result frame drawn
}

// Press holds a contact at x,y for d of fake time.
func (h *Harness) Press(x, y int, d time.Duration) TapResult {
	r := TapResult{Before: h.Panel.Screen()}
	t0 := time.Now()
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchDown, X: x, Y: y, At: h.Now})
	h.Frame()
	r.FeedbackMicros = time.Since(t0).Microseconds()
	h.Advance(d)
	action := h.Panel.LastAction
	t1 := time.Now()
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchUp, X: x, Y: y, At: h.Now})
	h.Sync()
	h.Frame()
	r.RedrawMicros = time.Since(t1).Microseconds()
	if a := h.Panel.LastAction; a != "" {
		action = a
	}
	r.Action = action
	// Let a hold's lift-off grace run out and the bounce window pass, as real
	// fingers do between taps.
	h.Advance(300 * time.Millisecond)
	r.After = h.Panel.Screen()
	return r
}

// Tap is a normal short touch.
func (h *Harness) Tap(x, y int) TapResult { return h.Press(x, y, 80*time.Millisecond) }

// Find returns the centre of a hit target.
func (h *Harness) Find(id string) (int, int, error) {
	for _, t := range h.Panel.Hits() {
		if t.ID == id {
			c := t.R.Min.Add(t.R.Max).Div(2)
			return c.X, c.Y, nil
		}
	}
	return 0, 0, fmt.Errorf("no target %q on screen %s", id, h.Panel.Screen())
}

// TapID taps a target by name; returns an error if it is not on screen.
func (h *Harness) TapID(id string) (TapResult, error) {
	x, y, err := h.Find(id)
	if err != nil {
		return TapResult{}, err
	}
	return h.Tap(x, y), nil
}

// HoldID holds a target by name for d.
func (h *Harness) HoldID(id string, d time.Duration) (TapResult, error) {
	x, y, err := h.Find(id)
	if err != nil {
		return TapResult{}, err
	}
	return h.Press(x, y, d), nil
}

// SavePNG writes the current frame.
func (h *Harness) SavePNG(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, h.Img)
}

// ForFixture builds a harness in a fixture's state, with a frozen controller
// already past the stall limit when the fixture asks for it.
func ForFixture(f fixtures.Fixture, cfg ui.Config) *Harness {
	h := New(f.Make(), cfg)
	if f.Frozen {
		h.Ctl.Frozen = true
		h.Advance(10 * time.Second)
	}
	return h
}

// View is one screen to capture for a fixture.
type View struct {
	Name string
	Path []string // target ids to tap, in order
}

var navViews = []View{
	{"set", []string{"nav.set"}},
	{"cure", []string{"nav.cure"}},
	{"alarms", []string{"nav.alarms"}},
	{"history", []string{"nav.alarms", "alarms.tab.1"}},
	{"trend", []string{"nav.trend"}},
	{"more", []string{"nav.more"}},
	{"health", []string{"nav.more", "more.health"}},
}

// ViewsFor lists the screens captured for a fixture, beyond Home.
func ViewsFor(name string) []View {
	switch name {
	case "normal":
		return append(append([]View{}, navViews...),
			View{"cure-stop-confirm", []string{"nav.cure", "cure.stop"}},
			View{"cure-next-confirm", []string{"nav.cure", "cure.next"}},
			View{"defrost-confirm", []string{"nav.more", "more.defrost"}},
			View{"outputs", []string{"nav.more", "more.outputs"}},
			View{"set-override-pending", []string{"nav.set", "edit.plus.0"}},
			View{"set-override-confirm", []string{"nav.set", "edit.plus.0", "edit.save"}},
			View{"refcheck-entry", refCheckPath()},
			View{"trend-all", []string{"nav.trend", "trend.span.ALL"}},
			View{"history-p2", []string{"nav.alarms", "alarms.tab.1", "history.page.next"}},
			View{"history-p3", []string{"nav.alarms", "alarms.tab.1", "history.page.next", "history.page.next"}},
			View{"history-p4", []string{"nav.alarms", "alarms.tab.1", "history.page.next", "history.page.next", "history.page.next"}},
			View{"installer-limits-p3", append(installerPath(), "inst.limits", "edit.page.next", "edit.page.next")},
			View{"installer-change-code", append(installerPath(), "inst.code")},
			View{"trend-7d", []string{"nav.trend", "trend.span.7 d"}},
			View{"installer-code", []string{"nav.more", "more.installer", "keypad.2", "keypad.5"}},
			View{"installer", installerPath()},
			View{"installer-limits", append(installerPath(), "inst.limits")},
			View{"installer-limits-p2", append(installerPath(), "inst.limits", "edit.page.next")},
			View{"installer-limits-pending", append(installerPath(), "inst.limits", "edit.plus.0")},
			View{"installer-offsets", append(installerPath(), "inst.cal")},
			View{"installer-scale", append(installerPath(), "inst.scale")},
		)
	case "idle-manual":
		return []View{{"recipes", []string{"nav.cure"}},
			{"recipes-p2", []string{"nav.cure", "recipes.page.next"}},
			{"recipe-detail", []string{"nav.cure", "recipe.0"}},
			{"recipe-start-confirm", []string{"nav.cure", "recipe.0", "recipe.start"}},
			{"set", []string{"nav.set"}},
			{"set-pending", []string{"nav.set", "edit.plus.0", "edit.plus.0", "edit.minus.1"}},
			{"installer-tare-confirm", append(installerPath(), "inst.scale", "scale.tare")},
			{"installer-calweight", append(installerPath(), "inst.scale", "scale.cal", "keypad.5", "keypad.0", "keypad.0")},
			{"installer-calweight-confirm", append(installerPath(), "inst.scale", "scale.cal", "keypad.5", "keypad.0", "keypad.0", "keypad.ok")},
		}
	case "alarm-temp-high":
		return []View{{"alarms", []string{"nav.alarms"}}, {"set", []string{"nav.set"}}, {"trend", []string{"nav.trend"}}, {"cure", []string{"nav.cure"}}, {"more", []string{"nav.more"}}, {"history", []string{"nav.alarms", "alarms.tab.1"}}}
	case "alarm-silenced", "alarm-acked":
		return []View{{"alarms", []string{"nav.alarms"}}}
	case "alarm-many":
		return []View{{"alarms", []string{"nav.alarms"}}, {"alarms-p2", []string{"nav.alarms", "alarms.page.next"}}, {"alarms-p3", []string{"nav.alarms", "alarms.page.next", "alarms.page.next"}}}
	case "long-text", "alarm-sensor-fault", "alarm-door", "alarm-power-back":
		return []View{{"alarms", []string{"nav.alarms"}}, {"health", []string{"nav.more", "more.health"}}}
	case "simulated":
		return append(append([]View{}, navViews...), View{"cure-stop-confirm", []string{"nav.cure", "cure.stop"}},
			View{"installer-code", []string{"nav.more", "more.installer"}}, View{"installer", installerPath()},
			View{"history-p2", []string{"nav.alarms", "alarms.tab.1", "history.page.next"}})
	case "simulated-alarm":
		return []View{{"alarms", []string{"nav.alarms"}}, {"cure", []string{"nav.cure"}}}
	case "end-of-cure", "end-of-cure-alarm":
		return []View{{"cure", []string{"nav.cure"}}, {"end-confirm", []string{"nav.cure", "cure.end"}}}
	case "paused":
		return []View{{"cure", []string{"nav.cure"}}}
	case "open-ended-phase":
		return []View{{"cure", []string{"nav.cure"}}}
	case "stale-temp", "fault-temp", "suspect-rh", "all-bad":
		return []View{{"health", []string{"nav.more", "more.health"}}, {"set", []string{"nav.set"}}, {"alarms", []string{"nav.alarms"}}}
	case "clock-untrusted":
		return []View{{"health", []string{"nav.more", "more.health"}}, {"history", []string{"nav.alarms", "alarms.tab.1"}}}
	case "override", "override-alarm":
		return []View{{"set", []string{"nav.set"}}, {"cure", []string{"nav.cure"}}}
	case "reading-in-doubt", "reference-disagrees":
		return []View{{"alarms", []string{"nav.alarms"}}, {"outputs", []string{"nav.more", "more.outputs"}}, {"more", []string{"nav.more"}}, {"health", []string{"nav.more", "more.health"}}}
	case "ref-check-overdue":
		return []View{{"more", []string{"nav.more"}}, {"health", []string{"nav.more", "more.health"}}, {"alarms", []string{"nav.alarms"}}}
	case "idle-with-reason", "output-holds":
		return []View{{"outputs", []string{"nav.more", "more.outputs"}}}
	case "not-fitted":
		return []View{{"trend", []string{"nav.trend"}}}
	case "trend-gap", "trend-empty":
		return []View{{"trend", []string{"nav.trend"}}}
	case "stalled", "stalled-alarm":
		return []View{{"set", []string{"nav.set"}}, {"cure", []string{"nav.cure"}}, {"alarms", []string{"nav.alarms"}}, {"history", []string{"nav.alarms", "alarms.tab.1"}},
			{"trend", []string{"nav.trend"}}, {"more", []string{"nav.more"}}, {"health", []string{"nav.more", "more.health"}}, {"outputs", []string{"nav.more", "more.outputs"}}}
	case "stalled-idle":
		return []View{{"set", []string{"nav.set"}}, {"recipes", []string{"nav.cure"}}, {"recipe", []string{"nav.cure", "recipe.0"}}, {"health", []string{"nav.more", "more.health"}}}
	case "stalled-end-of-cure":
		return []View{{"cure", []string{"nav.cure"}}}
	case "temp-fault-core-alarm", "no-alarm-long-fault", "long-title":
		return []View{{"alarms", []string{"nav.alarms"}}, {"health", []string{"nav.more", "more.health"}}}
	}
	return nil
}

func refCheckPath() []string {
	return []string{"nav.more", "more.refcheck", "keypad.2", "keypad.5", "keypad.8", "keypad.0", "keypad.ok"}
}

func installerPath() []string {
	return []string{"nav.more", "more.installer", "keypad.2", "keypad.5", "keypad.8", "keypad.0", "keypad.ok"}
}

// ViewsForInstaller is the tap path that opens the installer with the default code.
func ViewsForInstaller() []string { return installerPath() }

// NewLive drives a real controller. tick advances it by a duration of panel time.
func NewLive(ctl api.Controller, tick func(time.Duration), cfg ui.Config) *Harness {
	s := ctl.Snapshot()
	if !cfg.Settings.HasOwnCode() {
		if err := cfg.Settings.SetCode("2580"); err != nil {
			panic(err)
		}
	}
	h := &Harness{Now: s.Now, control: ctl, tick: tick}
	cfg.Location = time.UTC
	h.Panel = ui.NewPanel(cfg, func() time.Time { return h.Now }, func(c api.Command) { h.queue = append(h.queue, c) })
	h.Sync()
	h.Frame()
	return h
}
