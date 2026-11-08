package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

func TestDecoderTapMoveUp(t *testing.T) {
	d := NewDecoder(0, 799, 0, 479, 800, 480)
	feed := func(ev ...RawEvent) []TouchEvent {
		var out []TouchEvent
		for _, e := range ev {
			out = append(out, d.Feed(e)...)
		}
		return out
	}
	got := feed(RawEvent{evAbs, absMTSlot, 0}, RawEvent{evAbs, absMTTrackingID, 5}, RawEvent{evAbs, absMTPositionX, 100}, RawEvent{evAbs, absMTPositionY, 200}, RawEvent{evSyn, synReport, 0})
	if len(got) != 1 || got[0].Kind != TouchDown || got[0].X != 100 || got[0].Y != 200 {
		t.Fatalf("down: %+v", got)
	}
	got = feed(RawEvent{evAbs, absMTPositionX, 110}, RawEvent{evSyn, synReport, 0})
	if len(got) != 1 || got[0].Kind != TouchMove || got[0].X != 110 {
		t.Fatalf("move: %+v", got)
	}
	got = feed(RawEvent{evAbs, absMTTrackingID, -1}, RawEvent{evSyn, synReport, 0})
	if len(got) != 1 || got[0].Kind != TouchUp || got[0].X != 110 {
		t.Fatalf("up: %+v", got)
	}
}

func TestDecoderScalesDeviceRange(t *testing.T) {
	d := NewDecoder(0, 4095, 0, 4095, 800, 480)
	x, y := d.scale(4095, 0)
	if x != 799 || y != 0 {
		t.Fatalf("scale: %d %d", x, y)
	}
}

func TestDecoderPalmCancelsUntilClear(t *testing.T) {
	d := NewDecoder(0, 799, 0, 479, 800, 480)
	touch := func(s, id int32) []TouchEvent {
		var o []TouchEvent
		for _, e := range []RawEvent{{evAbs, absMTSlot, s}, {evAbs, absMTTrackingID, id}, {evAbs, absMTPositionX, 50 + s}, {evAbs, absMTPositionY, 50}, {evSyn, synReport, 0}} {
			o = append(o, d.Feed(e)...)
		}
		return o
	}
	if g := touch(0, 1); len(g) != 1 || g[0].Kind != TouchDown {
		t.Fatalf("down %+v", g)
	}
	touch(1, 2)
	if g := touch(2, 3); len(g) != 1 || g[0].Kind != TouchCancel {
		t.Fatalf("third contact should cancel: %+v", g)
	}
	// Lifting one contact of the palm must not start a new tap.
	if g := touch(2, -1); len(g) != 0 {
		t.Fatalf("after cancel: %+v", g)
	}
	touch(1, -1)
	touch(0, -1)
	if g := touch(0, 9); len(g) != 1 || g[0].Kind != TouchDown {
		t.Fatalf("clear surface should allow a new down: %+v", g)
	}
}

func TestDecoderDroppedEventsCancel(t *testing.T) {
	d := NewDecoder(0, 799, 0, 479, 800, 480)
	for _, e := range []RawEvent{{evAbs, absMTTrackingID, 1}, {evAbs, absMTPositionX, 5}, {evAbs, absMTPositionY, 5}, {evSyn, synReport, 0}} {
		d.Feed(e)
	}
	g := d.Feed(RawEvent{evSyn, synDropped, 0})
	if len(g) != 1 || g[0].Kind != TouchCancel {
		t.Fatalf("dropped: %+v", g)
	}
}

// A panic while drawing must stay inside Render and yield a fault frame.
func TestRenderPanicContained(t *testing.T) {
	now := time.Unix(1000, 0)
	p := NewPanel(Config{}, func() time.Time { return now }, func(api.Command) {})
	p.SetSnapshot(api.Snapshot{Now: now})
	p.renderHook = func() { panic("boom") }
	img := p.Render()
	if img == nil || img.Bounds().Dx() != ScreenW {
		t.Fatalf("no frame: %v", img)
	}
	if p.panics != 1 {
		t.Fatalf("panics = %d", p.panics)
	}
	if len(p.Hits()) != 0 {
		t.Fatalf("fault frame must have no targets")
	}
	p.renderHook = nil
	p.Render() // recovers on the next frame
}

// A press whose target has gone from the screen must not fire later.
func TestPressCancelledWhenTargetGoes(t *testing.T) {
	now := time.Unix(1000, 0)
	var sent []api.Command
	p := NewPanel(Config{}, func() time.Time { return now }, func(c api.Command) { sent = append(sent, c) })
	p.SetSnapshot(api.Snapshot{Now: now, Active: []api.Alarm{{ID: "a", Severity: api.SevCritical, Title: "x", Action: "y"}}})
	p.Render()
	var h *Hit
	for i := range p.hits {
		if p.hits[i].ID == "home.silence" {
			h = &p.hits[i]
		}
	}
	if h == nil {
		t.Fatal("no silence button")
	}
	pt := h.R.Min.Add(image.Pt(10, 10))
	p.Input(TouchEvent{Kind: TouchDown, X: pt.X, Y: pt.Y})
	// Buzzer becomes silenced from elsewhere: the button disappears.
	p.SetSnapshot(api.Snapshot{Now: now, BuzzerSilenced: true, Active: []api.Alarm{{ID: "a", Severity: api.SevCritical, Title: "x", Action: "y"}}})
	p.Render()
	now = now.Add(100 * time.Millisecond)
	p.Input(TouchEvent{Kind: TouchUp, X: pt.X, Y: pt.Y})
	if len(sent) != 0 {
		t.Fatalf("stale press fired: %+v", sent)
	}
}

func TestFramebufferPixelOrderAndRowDiff(t *testing.T) {
	mem := make([]byte, 4*4*2)
	fb := &Framebuffer{mem: mem, shadow: make([]byte, len(mem)), row: make([]byte, 16), w: 4, h: 2, stride: 16, rOff: 2, gOff: 1, bOff: 0, aOff: 3, lastFull: time.Now()}
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.SetRGBA(1, 0, rgb(10, 20, 30))
	if err := fb.Present(img); err != nil {
		t.Fatal(err)
	}
	if mem[4] != 30 || mem[5] != 20 || mem[6] != 10 || mem[7] != 255 {
		t.Fatalf("pixel bytes %v", mem[4:8])
	}
	mem[16] = 99 // a stray write to a row that has not changed in the shadow
	if err := fb.Present(img); err != nil {
		t.Fatal(err)
	}
	if mem[16] != 99 {
		t.Fatalf("unchanged row rewritten")
	}
	fb.lastFull = time.Now().Add(-time.Minute)
	if err := fb.Present(img); err != nil {
		t.Fatal(err)
	}
	if mem[16] == 99 {
		t.Fatalf("periodic full repaint did not heal a stray write")
	}
}

// XRGB, as on the rig: transp has offset 0 and length 0. Alpha must not be written
// over the blue byte. This was a real bug seen only on the rig's framebuffer.
func TestFramebufferXRGBKeepsBlue(t *testing.T) {
	mem := make([]byte, 4)
	fb := &Framebuffer{mem: mem, shadow: make([]byte, 4), row: make([]byte, 4), w: 1, h: 1, stride: 4, rOff: 2, gOff: 1, bOff: 0, aOff: -1, lastFull: time.Now()}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, rgb(10, 20, 30))
	if err := fb.Present(img); err != nil {
		t.Fatal(err)
	}
	if mem[0] != 30 || mem[1] != 20 || mem[2] != 10 {
		t.Fatalf("pixel bytes %v", mem)
	}
}

func TestStatusWordsNeverClaimProduct(t *testing.T) {
	s := api.Snapshot{Program: api.Program{State: api.RunFinished, Verify: "v"}, Health: api.Health{LogOK: true, WatchdogOK: true}}
	st := computeStatus(&s, false, 0)
	if strings.Contains(strings.ToLower(st.Headline+st.Detail), "safe") {
		t.Fatalf("%+v", st)
	}
}

// Integration defect 9: the door was hidden behind a temperature warning, and old
// events headlined over a live critical excursion and over the end of the cure.
func TestHeadlineRanksLiveConditionsOverEvents(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	al := func(code string, sev api.Severity, event bool, at time.Duration) api.Alarm {
		return api.Alarm{ID: code, Code: code, Severity: sev, Event: event, Since: t0.Add(at)}
	}
	seen := al("OUTPUTS_DROPPED", api.SevCritical, true, 0)
	seen.Acked = true
	got := sortAlarms([]api.Alarm{
		seen,
		al("CONTROLLER_RESTART_LOOP", api.SevCritical, true, time.Hour),
		al("TEMP_LOW", api.SevWarning, false, 2*time.Hour),
		al("DOOR_OPEN", api.SevWarning, false, 3*time.Hour),
		al("TEMP_HIGH_CRITICAL", api.SevCritical, false, 4*time.Hour),
		al("CURE_FINISHED", api.SevInfo, true, 5*time.Hour),
	})
	var order []string
	for _, a := range got {
		order = append(order, a.Code)
	}
	// An unacknowledged critical event comes before a newer, lesser one; one already
	// acknowledged does not beat the end of the cure.
	want := []string{"TEMP_HIGH_CRITICAL", "DOOR_OPEN", "TEMP_LOW", "CONTROLLER_RESTART_LOOP", "CURE_FINISHED", "OUTPUTS_DROPPED"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v, want %v", order, want)
	}
}
