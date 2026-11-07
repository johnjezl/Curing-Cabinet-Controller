// Package ui is the touchscreen front panel. It is written as a pure state
// machine (Panel: events in, frame out) wrapped by a thin loop (Run) that owns
// the framebuffer and touch device. It only reads api.Controller; every call to
// the controller happens on a separate goroutine, so nothing here can block or
// crash control. See docs/adr/03xx.
package ui

import (
	"fmt"
	"image"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

type screenID int

const (
	scrHome screenID = iota
	scrSet
	scrCure
	scrRecipe
	scrAlarms
	scrTrend
	scrMore
	scrHealth
	scrKeypad
	scrInstaller
	scrEdit
	scrScale
	scrOutputs
)

var screenNames = map[screenID]string{
	scrHome: "home", scrSet: "setpoints", scrCure: "cure", scrRecipe: "recipe", scrAlarms: "alarms",
	scrTrend: "trend", scrMore: "more", scrHealth: "health", scrKeypad: "keypad",
	scrInstaller: "installer", scrEdit: "edit", scrScale: "scale", scrOutputs: "outputs",
}

// Power is the display power stage.
type Power int

const (
	PowerOn Power = iota
	PowerDim
	PowerSleep
)

func (p Power) String() string { return [...]string{"on", "dim", "sleep"}[p] }

// Config tunes the panel. Zero values get the documented defaults.
type Config struct {
	MenuTimeout time.Duration // back to Home with no touch; default 30 s
	DimAfter    time.Duration // default 10 min
	SleepAfter  time.Duration // default 60 min
	StaleAfter  time.Duration // no fresh snapshot this long means stalled; default 5 s
	UnlockHold  time.Duration // default 3 s
	ConfirmHold time.Duration // hold on destructive confirm; default 2 s
	StartLocked bool
	FrozenAfter time.Duration // no frame this long paints the frozen notice; default 5 s
	StatePath   string        // panel settings file; empty means not kept across restarts
	// Settings is the state loaded at start. Persist, if set, receives every change
	// and must not block. Both are filled in by RunLoop when StatePath is set.
	Settings    Settings
	SettingsErr string
	Persist     func(Settings)
	Location    *time.Location // default time.Local
}

func (c *Config) defaults() {
	if c.MenuTimeout == 0 {
		c.MenuTimeout = 30 * time.Second
	}
	if c.DimAfter == 0 {
		c.DimAfter = 10 * time.Minute
	}
	if c.SleepAfter == 0 {
		c.SleepAfter = 60 * time.Minute
	}
	if c.StaleAfter == 0 {
		c.StaleAfter = 5 * time.Second
	}
	if c.UnlockHold == 0 {
		c.UnlockHold = 3 * time.Second
	}
	if c.ConfirmHold == 0 {
		c.ConfirmHold = 2 * time.Second
	}
	if c.FrozenAfter == 0 {
		c.FrozenAfter = 5 * time.Second
	}
	if c.Location == nil {
		c.Location = time.Local
	}
}

// EventKind is a contact change on the touch surface.
type EventKind int

const (
	TouchDown EventKind = iota
	TouchMove
	TouchUp
	TouchCancel // palm, or a dropped-event gap: abandon the gesture
)

// TouchEvent is one input in screen pixels.
type TouchEvent struct {
	Kind EventKind
	X, Y int
	At   time.Time // when the event was read
}

// Hit is one touch target of the frame on screen.
type Hit struct {
	ID   string
	R    image.Rectangle
	Hold time.Duration
	fn   func()
}

type dialog struct {
	id, title, body string
	confirm         string
	cancel          string
	danger          bool
	hold            time.Duration
	fn              func()
}

type toast struct {
	msg   string
	bad   bool
	until time.Time
}

// Panel is the UI state machine. It is single-threaded by design.
type Panel struct {
	cfg  Config
	now  func() time.Time
	send func(api.Command) // never blocks

	snap      api.Snapshot
	hasSnap   bool
	lastFresh time.Time // wall time of the last snapshot whose Now advanced
	lastNow   time.Time

	screen      screenID
	back        screenID
	dlg         *dialog
	toast       *toast
	locked      bool
	lastTouch   time.Time
	power       Power
	wakeSwallow bool
	manualSleep bool

	// gesture
	hits         []Hit
	pressed      *Hit
	downAt       time.Time
	downPt       image.Point
	holdFired    bool
	upPending    bool // finger lifted during a hold; greasy contacts drop out briefly
	upAt         time.Time
	refractUntil time.Time

	alarmTab      int
	alarmPage     int
	histPage      int
	recPage       int
	recipe        *api.Recipe
	trendSpan     time.Duration
	edit          *editor
	keypad        *keypad
	afterCode     func() // what an installer-gated action does once a new code is set
	installerOpen bool
	badCode       int
	lockoutUntil  time.Time

	pending   string // command in flight, for feedback
	pendingAt time.Time

	settings    Settings
	settingsErr string

	canvas  *Canvas
	audit   *Audit
	frames  uint64
	dirty   bool
	lastSec int64

	renderHook func() // test hook: runs inside render
	panics     int
	LastAction string
}

// NewPanel builds a panel. send must not block; results come back via CommandDone.
func NewPanel(cfg Config, now func() time.Time, send func(api.Command)) *Panel {
	cfg.defaults()
	p := &Panel{cfg: cfg, now: now, send: send, canvas: NewCanvas(), dirty: true, trendSpan: 24 * time.Hour,
		settings: cfg.Settings, settingsErr: cfg.SettingsErr, locked: cfg.Settings.Locked || cfg.StartLocked}
	p.lastTouch = now()
	p.lastFresh = now()
	return p
}

// SetSnapshot gives the panel the newest controller state.
func (p *Panel) SetSnapshot(s api.Snapshot) {
	now := p.now()
	if !p.hasSnap || !s.Now.Equal(p.lastNow) {
		p.lastFresh = now
		p.lastNow = s.Now
	}
	wasActive := len(p.snap.Active)
	p.snap = s
	p.hasSnap = true
	p.dirty = true
	if len(s.Active) > wasActive {
		p.wake()
	}
}

// save hands the panel's own state to whoever keeps it. It never blocks.
func (p *Panel) save() {
	p.settings.Locked = p.locked
	if p.cfg.Persist != nil {
		p.cfg.Persist(p.settings)
	}
}

// SettingsSaved reports the outcome of the last save to the operator.
func (p *Panel) SettingsSaved(err error) {
	if err == nil {
		p.settingsErr = ""
		p.dirty = true
		return
	}
	p.settingsErr = err.Error()
	p.flash("Panel setting not saved: it will be lost on restart. See Health.", true, 8*time.Second)
}

// Stalled reports whether the controller has gone quiet.
func (p *Panel) Stalled() bool {
	return !p.hasSnap || p.now().Sub(p.lastFresh) > p.cfg.StaleAfter
}

func (p *Panel) status() Status {
	return computeStatus(&p.snap, p.Stalled(), p.now().Sub(p.lastFresh))
}

func (p *Panel) wake() {
	if p.power != PowerOn {
		p.dirty = true
	}
	p.power = PowerOn
	p.manualSleep = false
	p.lastTouch = p.now()
}

// Screen names the visible screen, for logs and tests.
func (p *Panel) Screen() string {
	if p.dlg != nil {
		return screenNames[p.screen] + "+dialog:" + p.dlg.id
	}
	return screenNames[p.screen]
}

// PowerState reports the display power stage.
func (p *Panel) PowerState() Power { return p.power }

// Locked reports the keylock state.
func (p *Panel) Locked() bool { return p.locked }

// SetAudit turns on layout auditing; used by tests and evidence tools only.
func (p *Panel) SetAudit(a *Audit) { p.audit = a }

// Hits returns the current touch targets.
func (p *Panel) Hits() []Hit { return p.hits }

// Input feeds one touch event. Returns quickly and never touches the controller.
func (p *Panel) Input(e TouchEvent) {
	now := p.now()
	pt := image.Pt(e.X, e.Y)
	switch e.Kind {
	case TouchDown:
		p.lastTouch = now
		if p.upPending && p.pressed != nil && pt.In(p.pressed.R.Inset(-touchSlop)) {
			p.upPending = false // contact came back: the same hold goes on
			return
		}
		p.upPending = false
		p.pressed = nil
		p.holdFired = false
		p.dirty = true
		if p.power != PowerOn {
			// The touch that wakes the screen must do nothing else.
			p.power = PowerOn
			p.manualSleep = false
			p.wakeSwallow = true
			p.LastAction = "wake"
			return
		}
		p.wakeSwallow = false
		if now.Before(p.refractUntil) {
			return // the bounce of the last tap, not a new intent
		}
		p.pressed = p.hitAt(pt)
		p.downAt = now
		p.downPt = pt
		p.LastAction = ""
		if p.pressed != nil {
			p.LastAction = "press:" + p.pressed.ID
		}
	case TouchMove:
		p.lastTouch = now
		if p.pressed != nil && !pt.In(p.pressed.R.Inset(-touchSlop)) {
			p.pressed = nil
			p.dirty = true
		}
	case TouchUp:
		p.lastTouch = now
		h := p.pressed
		if h != nil && h.Hold > 0 && !p.holdFired && now.Sub(p.downAt) >= holdGrace {
			p.upPending = true
			p.upAt = now
			return
		}
		p.pressed = nil
		p.dirty = true
		if h == nil || p.wakeSwallow || p.holdFired {
			return
		}
		held := now.Sub(p.downAt)
		if held < minPress || held > maxPress {
			p.LastAction = "reject:" + h.ID
			return
		}
		if h.Hold > 0 {
			p.flash(fmt.Sprintf("Keep holding for %.0f s", h.Hold.Seconds()), false, 3*time.Second)
			return
		}
		p.fire(h)
	case TouchCancel:
		p.pressed = nil
		p.upPending = false
		p.dirty = true
	}
}

const holdGrace = 150 * time.Millisecond

const (
	touchSlop = 24 // gloved fingers roll
	minPress  = 25 * time.Millisecond
	maxPress  = 20 * time.Second // a stuck contact (water, grease) is not a tap
	bounce    = 250 * time.Millisecond
)

func (p *Panel) fire(h *Hit) {
	p.LastAction = "tap:" + h.ID
	p.refractUntil = p.now().Add(bounce)
	before, beforeDlg, old := p.screen, p.dlg, p.toast
	if h.fn != nil {
		h.fn()
	}
	// A message belongs to the screen it was about.
	if p.toast == old && (p.screen != before || p.dlg != beforeDlg) {
		p.toast = nil
	}
	p.dirty = true
}

func (p *Panel) hitAt(pt image.Point) *Hit {
	// last registered wins: overlays register later than what they cover
	for i := len(p.hits) - 1; i >= 0; i-- {
		if pt.In(p.hits[i].R) {
			h := p.hits[i] // a copy: the slice is rebuilt on every render
			return &h
		}
	}
	return nil
}

// Tick advances timers; call at least 10 times a second while pressed.
func (p *Panel) Tick() {
	now := p.now()
	if p.upPending && now.Sub(p.upAt) >= holdGrace {
		p.upPending = false
		h := p.pressed
		p.pressed = nil
		p.dirty = true
		if h != nil {
			p.LastAction = "release-early:" + h.ID
			p.flash(fmt.Sprintf("Released early. Hold for the full %.0f s.", h.Hold.Seconds()), false, 3*time.Second)
		}
	}
	if p.pending != "" && now.Sub(p.pendingAt) > 5*time.Second && !p.Stalled() {
		p.pending = ""
		p.flash("No answer from the controller. Check Health; the change may not have applied.", true, 8*time.Second)
	}
	if p.pressed != nil && p.pressed.Hold > 0 && !p.holdFired {
		p.dirty = true
		if now.Sub(p.downAt) >= p.pressed.Hold {
			h := p.pressed
			p.holdFired = true
			p.pressed = nil
			p.fire(h)
		}
	}
	if p.toast != nil && now.After(p.toast.until) {
		p.toast = nil
		p.dirty = true
	}
	idle := now.Sub(p.lastTouch)
	if p.screen != scrHome || p.dlg != nil {
		limit := p.cfg.MenuTimeout
		if p.screen == scrInstaller || (p.screen == scrEdit && p.edit != nil && p.edit.installer) || p.screen == scrScale {
			limit = 2 * limit
		}
		if idle >= limit && p.pressed == nil {
			p.timeoutHome()
		}
	}
	// Power: anything needing attention forces full on; menus stay full on.
	want := PowerOn
	if !attentionNeeded(&p.snap, p.Stalled()) && p.screen == scrHome && p.dlg == nil {
		switch {
		case p.manualSleep || idle >= p.cfg.SleepAfter:
			want = PowerSleep
		case idle >= p.cfg.DimAfter:
			want = PowerDim
		}
	}
	if want != p.power {
		p.power = want
		p.dirty = true
	}
	if sec := now.Unix(); sec != p.lastSec {
		p.lastSec = sec
		p.dirty = true // clock, countdowns, pulse
	}
}
