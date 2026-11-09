package ui_test

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui/fixtures"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui/uitest"
)

// scene is a fixture on one screen, with its audit.
type scene struct {
	name string
	h    *uitest.Harness
	a    *ui.Audit
}

var (
	scenesOnce sync.Once
	scenesAll  []scene
	scenesErr  string
)

// allScenes renders every fixture on every captured screen once; the read-only
// tests share the result.
func allScenes(t *testing.T) []scene {
	t.Helper()
	scenesOnce.Do(func() { scenesAll, scenesErr = buildScenes() })
	if scenesErr != "" {
		t.Fatal(scenesErr)
	}
	return scenesAll
}

func buildScenes() ([]scene, string) {
	var out []scene
	for _, f := range fixtures.All() {
		views := append([]uitest.View{{Name: "home"}}, uitest.ViewsFor(f.Name)...)
		for _, v := range views {
			h := uitest.ForFixture(f, ui.Config{})
			for _, id := range v.Path {
				if _, err := h.TapID(id); err != nil {
					return nil, fmt.Sprintf("%s/%s: %v", f.Name, v.Name, err)
				}
			}
			a := &ui.Audit{}
			h.Panel.SetAudit(a)
			h.Frame()
			out = append(out, scene{f.Name + "/" + v.Name, h, a})
		}
	}
	return out, ""
}

func allText(a *ui.Audit) string {
	var b strings.Builder
	for _, t := range a.Texts {
		b.WriteString(t.S)
		b.WriteByte('\n')
	}
	return b.String()
}

func mustContain(t *testing.T, a *ui.Audit, want string) {
	t.Helper()
	if !strings.Contains(strings.Join(strings.Fields(allText(a)), " "), want) {
		t.Fatalf("screen lacks %q; it has:\n%s", want, allText(a))
	}
}

func mustNotContain(t *testing.T, a *ui.Audit, bad string) {
	t.Helper()
	if strings.Contains(strings.Join(strings.Fields(allText(a)), " "), bad) {
		t.Fatalf("screen shows %q; it has:\n%s", bad, allText(a))
	}
}

func auditHome(h *uitest.Harness) *ui.Audit {
	a := &ui.Audit{}
	h.Panel.SetAudit(a)
	h.Frame()
	return a
}

func TestEveryFrameIsExactly800x480(t *testing.T) {
	for _, s := range allScenes(t) {
		if b := s.h.Img.Bounds(); b.Dx() != 800 || b.Dy() != 480 {
			t.Errorf("%s: %v", s.name, b)
		}
	}
}

func TestNoClippingOverlapOrMissingGlyphs(t *testing.T) {
	for _, s := range allScenes(t) {
		for _, p := range s.a.Problems {
			t.Errorf("%s: %s", s.name, p)
		}
	}
}

func TestTextContrast(t *testing.T) {
	for _, s := range allScenes(t) {
		for _, x := range s.a.Texts {
			min := 4.5
			if x.Size >= 24 {
				min = 3
			}
			if x.Contrast < min {
				t.Errorf("%s: %q size %d contrast %.2f", s.name, x.S, x.Size, x.Contrast)
			}
		}
	}
}

func TestTouchTargets(t *testing.T) {
	screen := image.Rect(0, 0, 800, 480)
	for _, s := range allScenes(t) {
		hits := s.h.Panel.Hits()
		for i, a := range hits {
			if a.R.Dx() < ui.MinTapPx || a.R.Dy() < ui.MinTapPx {
				t.Errorf("%s: %s is %dx%d, under %d px", s.name, a.ID, a.R.Dx(), a.R.Dy(), ui.MinTapPx)
			}
			if !a.R.In(screen) {
				t.Errorf("%s: %s off screen %v", s.name, a.ID, a.R)
			}
			for _, b := range hits[i+1:] {
				if a.ID == "toast.dismiss" || b.ID == "toast.dismiss" {
					continue // deliberately covers what it hides
				}
				if a.R.Overlaps(b.R) {
					t.Errorf("%s: %s %v overlaps %s %v", s.name, a.ID, a.R, b.ID, b.R)
				}
			}
		}
	}
}

// Reference: temperature digits of 14 to 30 mm; 14 mm is 104 px on this panel.
// A simulation is exempt: its band takes the room, and it is meant not to read
// like a real cabinet from across the room (ADR 0406).
func TestDigitHeightOnHome(t *testing.T) {
	for _, f := range fixtures.All() {
		if f.Make().Health.Simulated {
			continue
		}
		h := uitest.ForFixture(f, ui.Config{})
		a := auditHome(h)
		for _, k := range []string{"home.temp", "home.rh"} {
			px, ok := a.Digits[k]
			if !ok {
				continue // bad reading: no live digits, checked elsewhere
			}
			if px < 104 {
				t.Errorf("%s: %s digits %d px (%.1f mm)", f.Name, k, px, float64(px)/ui.PxPerMM)
			}
		}
	}
}

func TestBadReadingsNeverLookLive(t *testing.T) {
	cases := []struct {
		fx, key, word string
	}{
		{"stale-temp", "home.temp", "STALE"},
		{"fault-temp", "home.temp", "FAULT"},
		{"suspect-rh", "home.rh", "SUSPECT"},
		{"stale-air-weight", "home.air", "STALE"},
		{"stalled-alarm", "home.temp", "STALE"},
		{"stale-air-weight", "home.weight", "STALE"},
		{"suspect-weight", "home.weight", "SUSPECT"},
		{"not-fitted", "home.air", "NOT FITTED"},
		{"bad-numbers", "home.temp", "FAULT"},
		{"bad-numbers", "home.rh", "FAULT"},
		{"all-bad", "home.temp", "FAULT"},
		{"all-bad", "home.rh", "FAULT"},
		{"stalled", "home.temp", "STALE"},
	}
	for _, c := range cases {
		f, _ := fixtures.Find(c.fx)
		h := uitest.ForFixture(f, ui.Config{})
		a := auditHome(h)
		if _, live := a.Digits[c.key]; live {
			t.Errorf("%s: %s still has live digits", c.fx, c.key)
		}
		mustContain(t, a, c.word)
	}
	f, _ := fixtures.Find("fault-temp")
	a := auditHome(uitest.ForFixture(f, ui.Config{}))
	if strings.Contains(allText(a), "NaN") || strings.Contains(allText(a), "Inf") {
		t.Fatal("non-number on screen")
	}
}

func TestStaleBannerAndUnknownOutputsWhenControllerStalls(t *testing.T) {
	f, _ := fixtures.Find("stalled")
	h := uitest.ForFixture(f, ui.Config{})
	a := auditHome(h)
	mustContain(t, a, "CONTROLLER NOT RESPONDING")
	mustContain(t, a, "OUTPUTS UNKNOWN")
}

// With a stalled controller no screen may present a value or a state as current.
func TestStalledControllerNothingLooksLiveOnAnyScreen(t *testing.T) {
	banned := []string{" s old", "now OK", "now 13", "now 78", "now 0.4", ": logging", "trusted", "COOL ON", "ACK ALL", "SILENCE BUZZER"}
	screens := map[string]bool{}
	for _, s := range allScenes(t) {
		if !strings.HasPrefix(s.name, "stalled") {
			continue
		}
		screens[s.name] = true
		text := allText(s.a)
		if !strings.Contains(text, "NOT RESPONDING") {
			t.Errorf("%s: no stall banner:\n%s", s.name, text)
		}
		for _, w := range banned {
			if strings.Contains(text, w) {
				t.Errorf("%s: still shows %q:\n%s", s.name, w, text)
			}
		}
		for _, id := range []string{"edit.plus.0", "cure.pause", "cure.next", "cure.stop", "cure.end", "alarms.ackall", "more.defrost"} {
			if _, _, err := s.h.Find(id); err == nil {
				t.Errorf("%s: %s is offered with no controller", s.name, id)
			}
		}
	}
	for _, want := range []string{"stalled/home", "stalled/set", "stalled/cure", "stalled/alarms", "stalled/history", "stalled/trend", "stalled/more", "stalled/health", "stalled-idle/recipes", "stalled-alarm/alarms", "stalled-end-of-cure/cure"} {
		if !screens[want] {
			t.Errorf("no capture of %s", want)
		}
	}
	for _, w := range []string{"stalled/set", "stalled/cure", "stalled/alarms", "stalled/trend", "stalled/health"} {
		found := false
		for _, s := range allScenes(t) {
			if s.name == w && strings.Contains(allText(s.a), "NOT LIVE") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s does not say NOT LIVE", w)
		}
	}
}

func TestStalledControllerRefusesCommands(t *testing.T) {
	f, _ := fixtures.Find("stalled-idle")
	h := uitest.ForFixture(f, ui.Config{})
	tap(t, h, "nav.cure")
	tap(t, h, "recipe.0")
	tap(t, h, "recipe.start")
	h.HoldID("dialog.ok", 2100*time.Millisecond)
	if n := len(h.Ctl.Commands); n != 0 {
		t.Fatalf("%d commands sent to a stalled controller", n)
	}
	mustContain(t, auditHome(h), "not responding")
}

// Fixtures must tell one story, or the screens judged for trust contradict.
func TestFixturesAreSelfConsistent(t *testing.T) {
	for _, f := range fixtures.All() {
		s := f.Make()
		p := s.Program
		if s.Weight.Quality == api.QualityOK && p.StartWeightG > 0 && p.State != api.RunIdle {
			got := (p.StartWeightG - s.Weight.Value) / p.StartWeightG * 100
			if d := got - p.WeightLossPct; d > 0.5 || d < -0.5 {
				t.Errorf("%s: %.0f g from %.0f g is %.1f %%, shown %.1f %%", f.Name, s.Weight.Value, p.StartWeightG, got, p.WeightLossPct)
			}
		}
		if p.State == api.RunActive && p.TotalElapsed > 0 && p.LossPctPerDay != 0 && p.WeightLossPct > 0 {
			avg := p.WeightLossPct / p.TotalElapsed.Hours() * 24
			if f.Name == "normal" && (avg-p.LossPctPerDay > 0.1 || p.LossPctPerDay-avg > 0.1) {
				t.Errorf("%s: %.2f %%/day shown, %.2f by arithmetic", f.Name, p.LossPctPerDay, avg)
			}
		}
		if n := len(s.Trend); n > 0 && s.Trend[n-1].Valid {
			last := s.Trend[n-1]
			check := func(what string, r api.Reading, v, tol float64) {
				if r.Quality == api.QualityOK && r.Value == r.Value && r.Value-v > tol || r.Quality == api.QualityOK && v-r.Value > tol {
					t.Errorf("%s: trend ends %s %.2f, reading %.2f", f.Name, what, v, r.Value)
				}
			}
			check("temp", s.Temp, last.TempC, 0.5)
			check("rh", s.RH, last.RHPct, 2)
			check("weight", s.Weight, last.WeightG, 5)
		}
	}
}

func TestBadReadingShowsWhatControlIsDoing(t *testing.T) {
	f, _ := fixtures.Find("temp-fault-core-alarm")
	a := auditHome(uitest.ForFixture(f, ui.Config{}))
	mustContain(t, a, "cooling repeats its recent pattern")
	mustContain(t, a, "COOL ON !")
	mustContain(t, a, "NO TEMP OR RH FEEDBACK")
	mustContain(t, a, "HEAT, HUMID HELD: temperature data is bad")
	if strings.Contains(allText(a), "0:00") {
		t.Error("a state is shown as a countdown at zero")
	}
}

func TestLongUnusableReadingWithoutAlarmIsCritical(t *testing.T) {
	f, _ := fixtures.Find("no-alarm-long-fault")
	a := auditHome(uitest.ForFixture(f, ui.Config{}))
	mustContain(t, a, "raised no alarm")
	f, _ = fixtures.Find("stale-temp") // seconds old: amber is right, the core has not decided yet
	a = auditHome(uitest.ForFixture(f, ui.Config{}))
	if strings.Contains(allText(a), "raised no alarm") {
		t.Error("a fresh loss was escalated")
	}
}

func TestStartNeedsHoldAndStatesTheNumbers(t *testing.T) {
	h := idleManual(t)
	tap(t, h, "nav.cure")
	tap(t, h, "recipe.0")
	x, y, _ := h.Find("recipe.start")
	h.Tap(x, y)
	mustContain(t, auditHome(h), "22.0 °C, 90 %")
	// A double tap in the same place must not start anything.
	h.Tap(x, y)
	h.Advance(400 * time.Millisecond)
	if len(h.Ctl.Commands) != 0 {
		t.Fatalf("%+v", h.Ctl.Commands)
	}
}

func TestPauseNeedsHoldAndIsNotCalm(t *testing.T) {
	h := normal(t)
	tap(t, h, "nav.cure")
	tap(t, h, "cure.pause")
	if len(h.Ctl.Commands) != 0 {
		t.Fatal("a tap paused the cure")
	}
	h.Advance(3200 * time.Millisecond)
	h.HoldID("cure.pause", 1100*time.Millisecond)
	h.Sync()
	if len(h.Ctl.Commands) != 1 {
		t.Fatalf("%+v", h.Ctl.Commands)
	}
	f, _ := fixtures.Find("paused")
	h2 := uitest.ForFixture(f, ui.Config{})
	mustContain(t, auditHome(h2), "PHASE CLOCK STOPPED")
	h2.Advance(2 * time.Hour)
	if h2.Panel.PowerState() != ui.PowerOn {
		t.Error("a paused cure dimmed")
	}
}

func TestToastBlocksWhatItHidesAndEndsWithItsScreen(t *testing.T) {
	h := idleManual(t)
	h.Ctl.Refuse = "no"
	tap(t, h, "nav.set")
	tap(t, h, "edit.plus.1")
	x, y, _ := h.Find("edit.plus.2") // this control sits under where the message lands
	tap(t, h, "edit.save")
	h.Sync()
	h.Frame()
	mustContain(t, auditHome(h), "Refused: no")
	r := h.Tap(x, y)
	if r.Action != "tap:toast.dismiss" {
		t.Fatalf("a control under a message fired: %s", r.Action)
	}
	// On the keypad a message sits beside the keys and blocks nothing.
	h3 := idleManual(t)
	tap(t, h3, "nav.more")
	tap(t, h3, "more.installer")
	tap(t, h3, "keypad.1")
	tap(t, h3, "keypad.ok")
	mustContain(t, auditHome(h3), "Wrong code")
	x, y, _ = h3.Find("keypad.8")
	if r := h3.Tap(x, y); r.Action != "tap:keypad.8" {
		t.Fatalf("keypad blocked by a message: %s", r.Action)
	}
	h2 := idleManual(t)
	tap(t, h2, "nav.more")
	tap(t, h2, "more.lock")
	mustContain(t, auditHome(h2), "Keep holding")
	tap(t, h2, "nav.home")
	if strings.Contains(allText(auditHome(h2)), "Keep holding") {
		t.Error("a message followed the user to another screen")
	}
}

func TestInfoAlarmDoesNotOfferSilenceOrRed(t *testing.T) {
	f, _ := fixtures.Find("alarm-info")
	h := uitest.ForFixture(f, ui.Config{})
	if _, _, err := h.Find("home.silence"); err == nil {
		t.Error("silence offered for an alarm that never sounds")
	}
}

func TestDefrostOverBandIsNotGreenOrWhite(t *testing.T) {
	f, _ := fixtures.Find("defrost")
	a := auditHome(uitest.ForFixture(f, ui.Config{}))
	mustContain(t, a, "DEFROST: TEMP 3.1 °C ABOVE SET")
	mustContain(t, a, "+3.1")
}

func TestFinishedWithoutScaleDoesNotClaimALoss(t *testing.T) {
	s, _ := fixtures.Find("end-of-cure")
	snap := s.Make()
	snap.Weight = api.Reading{Quality: api.QualityOff}
	h := uitest.New(snap, ui.Config{})
	tap(t, h, "nav.cure")
	a := auditHome(h)
	mustContain(t, a, "Weight loss not known")
	if strings.Contains(allText(a), "Loss 35") {
		t.Error("a loss figure shown with no scale")
	}
}

func TestBannerNamesEveryBadSensor(t *testing.T) {
	f, _ := fixtures.Find("stale-air-weight")
	a := auditHome(uitest.ForFixture(f, ui.Config{}))
	mustContain(t, a, "AIRFLOW, WEIGHT")
}

// Dim must stay readable: the digits measured from the rendered pixels.
func TestDimDigitContrastFromPixels(t *testing.T) {
	h := normal(t)
	h.Advance(10*time.Minute + time.Second)
	if h.Panel.PowerState() != ui.PowerDim {
		t.Fatal(h.Panel.PowerState())
	}
	h.Frame()
	img := h.Img
	var bright, chipBright uint8
	for y := 100; y < 215; y++ {
		for x := 20; x < 380; x++ {
			if v := img.RGBAAt(x, y).G; v > bright {
				bright = v
			}
		}
	}
	for y := 325; y < 375; y++ {
		for x := 10; x < 120; x++ {
			if c := img.RGBAAt(x, y); c.G > chipBright && c.R > 100 {
				// a light fill would dominate; count only large areas
				if img.RGBAAt(x+1, y).G == c.G && img.RGBAAt(x, y+1).G == c.G && img.RGBAAt(x+1, y+1).G == c.G {
					chipBright = c.G
				}
			}
		}
	}
	bg := img.RGBAAt(30, 75)
	fg := color.RGBA{bright, bright, bright, 255}
	if c := ui.Contrast(fg, bg); c < 7 {
		t.Errorf("dim digit contrast %.1f:1 (fg %d)", c, bright)
	}
	_ = chipBright
}

// Fit must not depend on the wording of the day.
func TestAnyReasonableAlarmTextFits(t *testing.T) {
	title := strings.Repeat("Reading disagrees with the cabinet ", 3)[:75]
	action := strings.Repeat("Compare with a separate thermometer and check the wiring. ", 5)[:160]
	for _, sim := range []bool{false, true} {
		for _, locked := range []bool{false, true} {
			s := fixtures.Base()
			s.Health.Simulated, s.Health.SpeedFactor = sim, 60
			s.Active = []api.Alarm{{ID: "x", Code: "X", Severity: api.SevCritical, Title: title, Action: action, Since: fixtures.T0, ClockOK: true}}
			s.History = append(s.Active[:0:0], api.Alarm{ID: "y", Code: "X", Severity: api.SevCritical, Title: title, Action: action, Since: fixtures.T0, Cleared: fixtures.T0.Add(time.Hour), ClockOK: true})
			h := uitest.New(s, ui.Config{StartLocked: locked})
			for _, p := range auditHome(h).Problems {
				t.Errorf("home sim=%v locked=%v: %s", sim, locked, p)
			}
			tap(t, h, "nav.alarms")
			for _, p := range auditHome(h).Problems {
				t.Errorf("alarms sim=%v locked=%v: %s", sim, locked, p)
			}
			tap(t, h, "alarms.tab.1")
			for _, p := range auditHome(h).Problems {
				t.Errorf("history sim=%v locked=%v: %s", sim, locked, p)
			}
		}
	}
}

func TestSimulatedOnEveryScreen(t *testing.T) {
	for _, s := range allScenes(t) {
		if !strings.HasPrefix(s.name, "simulated") {
			continue
		}
		mustContain(t, s.a, "SIM")
		if s.name == "simulated/home" {
			mustContain(t, s.a, "SIMULATED")
		}
		c := s.h.Img.RGBAAt(0, 0)
		found := false
		for x := 0; x < 18; x++ { // the striped frame
			c = s.h.Img.RGBAAt(x, 1)
			if c.R == 0x8A && c.B == 0xD1 {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no simulated frame", s.name)
		}
	}
}

func TestHoldShowsReasonAndTime(t *testing.T) {
	f, _ := fixtures.Find("hold-compressor")
	a := auditHome(uitest.ForFixture(f, ui.Config{}))
	mustContain(t, a, "compressor minimum off time")
	mustContain(t, a, "2:35 left")
	mustContain(t, a, "COOL HOLD")
	f, _ = fixtures.Find("hold-with-alarm")
	a = auditHome(uitest.ForFixture(f, ui.Config{}))
	mustContain(t, a, "1:35 left")
	mustContain(t, a, "ALARM: Temperature high")
}

func normal(t *testing.T) *uitest.Harness {
	t.Helper()
	f, _ := fixtures.Find("normal")
	return uitest.ForFixture(f, ui.Config{})
}

func tap(t *testing.T, h *uitest.Harness, id string) {
	t.Helper()
	if _, err := h.TapID(id); err != nil {
		t.Fatal(err)
	}
}

func idleManual(t *testing.T) *uitest.Harness {
	t.Helper()
	f, _ := fixtures.Find("idle-manual")
	return uitest.ForFixture(f, ui.Config{})
}

func TestSetpointDuringRecipeIsAConfirmedOverride(t *testing.T) {
	h := normal(t)
	tap(t, h, "nav.set")
	a := auditHome(h)
	mustContain(t, a, "recipe 13.0")
	tap(t, h, "edit.plus.0")
	mustContain(t, auditHome(h), "This phase only")
	if len(h.Ctl.Commands) != 0 {
		t.Fatal("command before save")
	}
	tap(t, h, "edit.save")
	if h.Panel.Screen() != "setpoints+dialog:override" {
		t.Fatalf("no confirmation: %s", h.Panel.Screen())
	}
	a = auditHome(h)
	mustContain(t, a, "for this phase only")
	mustContain(t, a, "13.5 °C")
	mustContain(t, a, "13.0 °C")
	mustContain(t, a, "written to the record")
	tap(t, h, "dialog.ok") // a tap is not enough
	if len(h.Ctl.Commands) != 0 {
		t.Fatal("a tap confirmed an override")
	}
	h.Advance(3200 * time.Millisecond)
	h.HoldID("dialog.ok", 2100*time.Millisecond)
	h.Sync()
	if len(h.Ctl.Commands) != 1 || h.Ctl.Commands[0].Kind != api.CmdSetSetpoint || h.Ctl.Commands[0].Setpoint.TempC != 13.5 {
		t.Fatalf("%+v", h.Ctl.Commands)
	}
	h.Advance(2500 * time.Millisecond)
	tap(t, h, "nav.home")
	a = auditHome(h)
	mustContain(t, a, "OVERRIDE 13.5 °C")
	mustContain(t, a, "OVERRIDE this phase")
}

func TestOverrideIsVisibleEvenUnderAnAlarm(t *testing.T) {
	f, _ := fixtures.Find("override-alarm")
	h := uitest.ForFixture(f, ui.Config{})
	a := auditHome(h)
	mustContain(t, a, "OVERRIDE 14.0 °C")
	mustContain(t, a, "ALARM")
	tap(t, h, "nav.cure")
	mustContain(t, auditHome(h), "OVERRIDE 14.0 °C 76 % (recipe 13.0, 78)")
}

func enterReading(t *testing.T, h *uitest.Harness, v string) {
	t.Helper()
	for _, d := range v {
		id := "keypad." + string(d)
		if d == '.' {
			id = "keypad.dot"
		}
		tap(t, h, id)
	}
	tap(t, h, "keypad.ok")
}

func TestReferenceCheckNeedsCodeBlindEntryAndHold(t *testing.T) {
	f, _ := fixtures.Find("reading-in-doubt")
	h := uitest.ForFixture(f, ui.Config{})
	tap(t, h, "nav.alarms")
	tap(t, h, "alarms.refcheck.temp_reading_disagrees-1")
	if h.Panel.Screen() != "keypad" {
		t.Fatal(h.Panel.Screen())
	}
	enterCode(t, h, "2580")
	// Nothing pre-filled, and the sensor's value is out of sight during entry.
	a := auditHome(h)
	mustContain(t, a, "Separate thermometer reads")
	if strings.Contains(allText(a), "14.8") {
		t.Fatalf("sensor value shown during entry:\n%s", allText(a))
	}
	enterReading(t, h, "14.2")
	mustContain(t, auditHome(h), "Separate hygrometer reads")
	tap(t, h, "keypad.ok") // none available
	if h.Panel.Screen() != "alarms+dialog:refcheck" {
		t.Fatal(h.Panel.Screen())
	}
	a = auditHome(h)
	mustContain(t, a, "sensor 14.8, reference 14.2, difference 0.6")
	mustContain(t, a, "clears a reading in doubt")
	tap(t, h, "dialog.ok")
	if len(h.Ctl.Commands) != 0 {
		t.Fatal("a tap recorded a check")
	}
	h.Advance(3200 * time.Millisecond)
	h.HoldID("dialog.ok", 2100*time.Millisecond)
	h.Sync()
	c := h.Ctl.Commands
	if len(c) != 1 || c[0].Kind != api.CmdRefCheck || *c[0].Reference.TempC != 14.2 || c[0].Reference.RHPct != nil {
		t.Fatalf("%+v", c)
	}
	h.Advance(2500 * time.Millisecond)
	if s := h.Ctl.Snapshot(); len(s.Active) != 0 {
		t.Fatalf("%+v", s.Active)
	}
}

func TestReferenceCheckWrongCodeBlocksAndFarValueWarns(t *testing.T) {
	f, _ := fixtures.Find("reading-in-doubt")
	h := uitest.ForFixture(f, ui.Config{})
	tap(t, h, "nav.more")
	tap(t, h, "more.refcheck")
	enterCode(t, h, "1111")
	mustContain(t, auditHome(h), "Wrong code")
	if strings.Contains(allText(auditHome(h)), "thermometer") {
		t.Fatal("wrong code reached the entry")
	}
	tap(t, h, "keypad.back")
	h.Advance(3200 * time.Millisecond)
	tap(t, h, "more.refcheck")
	enterCode(t, h, "2580")
	enterReading(t, h, "20")
	tap(t, h, "keypad.ok")
	a := auditHome(h)
	mustContain(t, a, "differs too much")
	mustContain(t, a, "raise a critical alarm")
}

func TestReferenceCheckNotOfferedWhenLockedOrStalled(t *testing.T) {
	f, _ := fixtures.Find("reading-in-doubt")
	h := uitest.ForFixture(f, ui.Config{StartLocked: true})
	tap(t, h, "nav.alarms")
	if _, _, err := h.Find("alarms.refcheck.temp_reading_disagrees-1"); err == nil {
		t.Error("offered while locked")
	}
	tap(t, h, "nav.more")
	if _, _, err := h.Find("more.refcheck"); err == nil {
		t.Error("offered while locked")
	}
	st, _ := fixtures.Find("stalled-alarm")
	h = uitest.ForFixture(st, ui.Config{})
	tap(t, h, "nav.more")
	if _, _, err := h.Find("more.refcheck"); err == nil {
		t.Error("offered while stalled")
	}
}

// The panel words the outcome before the core decides it, so it must use the
// core's own tolerances.
func TestRefCheckTolerancesMatchCore(t *testing.T) {
	src, err := os.ReadFile("../core/crosscheck.go")
	if err != nil {
		t.Skip(err)
	}
	if !regexp.MustCompile(`tolT, tolRH = 1\.5, 5\.0`).Match(src) {
		t.Fatal("core tolerances changed: update refTolTempC and refTolRHPct")
	}
}

func TestRefCheckDueIsShown(t *testing.T) {
	h := normal(t)
	tap(t, h, "nav.more")
	a := auditHome(h)
	mustContain(t, a, "SENSOR CHECK in 4 d 3 h")
	tap(t, h, "more.health")
	a = auditHome(h)
	mustContain(t, a, "due in 4 d 3 h")
	f, _ := fixtures.Find("ref-check-overdue")
	h = uitest.ForFixture(f, ui.Config{})
	a = auditHome(h)
	mustContain(t, a, "Compare temperature and humidity")
	tap(t, h, "nav.more")
	a = auditHome(h)
	mustContain(t, a, "SENSOR CHECK OVERDUE")
	tap(t, h, "more.health")
	mustContain(t, auditHome(h), "OVERDUE by 9 h")
}

func TestOutputReasonsAreShown(t *testing.T) {
	f, _ := fixtures.Find("idle-with-reason")
	h := uitest.ForFixture(f, ui.Config{})
	mustContain(t, auditHome(h), "COOL off: building up demand")
	tap(t, h, "nav.more")
	tap(t, h, "more.outputs")
	a := auditHome(h)
	mustContain(t, a, "runs are spaced to protect the compressor")
	mustContain(t, a, "waiting to see the last pulse settle")
	f, _ = fixtures.Find("output-holds")
	h = uitest.ForFixture(f, ui.Config{})
	tap(t, h, "nav.more")
	tap(t, h, "more.outputs")
	a = auditHome(h)
	mustContain(t, a, "OFF HOLD")
	mustContain(t, a, "minimum off time, 2:35 left")
	// A reading in doubt still runs the compressor; heat and humidity are held, and the
	// panel does not call that lost feedback (integration defect 1, re-test).
	f, _ = fixtures.Find("reading-in-doubt")
	h = uitest.ForFixture(f, ui.Config{})
	a = auditHome(h)
	mustContain(t, a, "HEAT HOLD")
	mustContain(t, a, "READING IN DOUBT")
	mustNotContain(t, a, "NO TEMP FEEDBACK")
	tap(t, h, "nav.more")
	tap(t, h, "more.outputs")
	mustContain(t, auditHome(h), "locked off while a reading is in doubt")
}

// Re-test of defect 1: during a compressor failure the COOL chip read a plain "COOL ON".
func TestFailedCompressorChipSaysSo(t *testing.T) {
	f, _ := fixtures.Find("compressor-failed")
	h := uitest.ForFixture(f, ui.Config{})
	a := auditHome(h)
	mustContain(t, a, "COOL FAILED")
	mustNotContain(t, a, "COOL ON")
}

func TestSetpointNeedsSaveAndCancelDiscards(t *testing.T) {
	h := idleManual(t)
	tap(t, h, "home.temp")
	tap(t, h, "edit.plus.0")
	tap(t, h, "edit.plus.0")
	if len(h.Ctl.Commands) != 0 {
		t.Fatal("command before SAVE")
	}
	if _, err := h.TapID("nav.home"); err == nil {
		t.Fatal("navigation must be replaced while a change is pending")
	}
	tap(t, h, "edit.cancel")
	if len(h.Ctl.Commands) != 0 {
		t.Fatal("cancel sent a command")
	}
	tap(t, h, "edit.plus.0")
	tap(t, h, "edit.save")
	h.Sync()
	if len(h.Ctl.Commands) != 1 || h.Ctl.Commands[0].Setpoint.TempC != 13.5 {
		t.Fatalf("%+v", h.Ctl.Commands)
	}
}

func TestSetpointTakesFewTaps(t *testing.T) {
	h := idleManual(t)
	tap(t, h, "home.temp")   // 1
	tap(t, h, "edit.plus.0") // 2
	tap(t, h, "edit.save")   // 3
	h.Sync()
	if len(h.Ctl.Commands) != 1 {
		t.Fatal("three taps should commit one step")
	}
}

func TestPendingEditTimesOutUncommitted(t *testing.T) {
	h := idleManual(t)
	tap(t, h, "nav.set")
	tap(t, h, "edit.plus.0")
	h.Advance(31 * time.Second)
	if h.Panel.Screen() != "home" {
		t.Fatalf("screen %s", h.Panel.Screen())
	}
	if len(h.Ctl.Commands) != 0 {
		t.Fatal("timeout committed a change")
	}
	mustContain(t, auditHome(h), "NOT saved")
}

func TestRefusedCommandIsShown(t *testing.T) {
	h := idleManual(t)
	h.Ctl.Refuse = "outside the allowed range"
	tap(t, h, "nav.set")
	tap(t, h, "edit.plus.0")
	tap(t, h, "edit.save")
	h.Sync()
	mustContain(t, auditHome(h), "outside the allowed range")
}

func TestMenuTimeoutEveryScreen(t *testing.T) {
	for _, id := range []string{"nav.set", "nav.cure", "nav.alarms", "nav.trend", "nav.more"} {
		h := normal(t)
		tap(t, h, id)
		h.Advance(31 * time.Second)
		if h.Panel.Screen() != "home" {
			t.Errorf("%s: still on %s", id, h.Panel.Screen())
		}
	}
}

func lock(t *testing.T, h *uitest.Harness) {
	t.Helper()
	tap(t, h, "nav.more")
	if _, err := h.HoldID("more.lock", 2100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !h.Panel.Locked() {
		t.Fatal("did not lock")
	}
}

func TestKeylock(t *testing.T) {
	h := normal(t)
	tap(t, h, "nav.more")
	tap(t, h, "more.lock") // a tap is not a hold
	if h.Panel.Locked() {
		t.Fatal("a tap locked the panel")
	}
	lock(t, h)
	tap(t, h, "nav.set")
	a := auditHome(h)
	mustContain(t, a, "13.0 °C")
	mustContain(t, a, "78 %")
	mustContain(t, a, "0.40 m/s")
	tap(t, h, "nav.cure")
	for _, id := range []string{"cure.stop", "cure.pause", "cure.next"} {
		if _, _, err := h.Find(id); err == nil {
			t.Errorf("%s reachable while locked", id)
		}
	}
	tap(t, h, "nav.more")
	if _, _, err := h.Find("more.installer"); err == nil {
		t.Error("installer reachable while locked")
	}
	// A short hold does not unlock.
	h.HoldID("lock.unlock", time.Second)
	if !h.Panel.Locked() {
		t.Fatal("short hold unlocked")
	}
	h.HoldID("lock.unlock", 3100*time.Millisecond)
	if h.Panel.Locked() {
		t.Fatal("full hold did not unlock")
	}
}

func TestLockedManualSetpointsViewOnly(t *testing.T) {
	h := idleManual(t)
	lock(t, h)
	tap(t, h, "nav.set")
	a := auditHome(h)
	mustContain(t, a, "13.0 °C")
	mustContain(t, a, "0.40 m/s")
	for _, id := range []string{"edit.plus.0", "edit.minus.0", "edit.save"} {
		if _, _, err := h.Find(id); err == nil {
			t.Errorf("%s reachable while locked", id)
		}
	}
}

func TestSilenceWorksWhileLockedAndHidesNothing(t *testing.T) {
	f, _ := fixtures.Find("alarm-temp-high")
	h := uitest.ForFixture(f, ui.Config{StartLocked: true})
	tap(t, h, "home.silence")
	h.Sync()
	h.Frame()
	if len(h.Ctl.Commands) != 1 || h.Ctl.Commands[0].Kind != api.CmdSilence {
		t.Fatalf("%+v", h.Ctl.Commands)
	}
	a := auditHome(h)
	mustContain(t, a, "ALARM: Temperature high")
	mustContain(t, a, "SILENCED")
	mustContain(t, a, "Keep the door shut")
	tap(t, h, "nav.alarms")
	a = auditHome(h)
	mustContain(t, a, "Temperature high")
	mustContain(t, a, "still active")
}

// Every alarm the real core can raise must fit the banner and the list.
func TestEveryCoreAlarmDefinitionFits(t *testing.T) {
	src, err := os.ReadFile("../core/alarms.go")
	if err != nil {
		t.Skip("core not present: ", err)
	}
	re := regexp.MustCompile(`\{api\.Sev(\w+), "([^"]+)", "([^"]+)", "([^"]*)"\}`)
	ms := re.FindAllStringSubmatch(string(src), -1)
	if len(ms) < 20 {
		t.Fatalf("parsed only %d definitions", len(ms))
	}
	for _, m := range ms {
		s := fixtures.Base()
		s.Active = []api.Alarm{{ID: "x", Code: "X", Severity: api.Severity(strings.ToLower(m[1])), Title: m[2], Action: m[3], Since: fixtures.T0.Add(-time.Hour), ClockOK: true, Peak: 123.4, Limit: 120, Unit: m[4]}}
		for _, sim := range []bool{false, true} {
			s.Health.Simulated, s.Health.SpeedFactor = sim, 60
			h := uitest.New(s, ui.Config{})
			a := auditHome(h)
			for _, p := range a.Problems {
				t.Errorf("home %q sim=%v: %s", m[2], sim, p)
			}
			tap(t, h, "nav.alarms")
			a = auditHome(h)
			for _, p := range a.Problems {
				t.Errorf("alarms %q sim=%v: %s", m[2], sim, p)
			}
			mustContain(t, a, m[2])
			past := s
			past.History, past.Active = append([]api.Alarm(nil), s.Active...), nil
			past.History[0].Cleared = fixtures.T0
			h = uitest.New(past, ui.Config{})
			tap(t, h, "nav.alarms")
			tap(t, h, "alarms.tab.1")
			for _, p := range auditHome(h).Problems {
				t.Errorf("history %q sim=%v: %s", m[2], sim, p)
			}
		}
	}
}

func TestAckKeepsAlarmVisible(t *testing.T) {
	f, _ := fixtures.Find("alarm-temp-high")
	h := uitest.ForFixture(f, ui.Config{})
	tap(t, h, "nav.alarms")
	tap(t, h, "alarms.ack.temp_high-1")
	h.Sync()
	a := auditHome(h)
	mustContain(t, a, "ACKED")
	mustContain(t, a, "ALARM: Temperature high")
}

func TestEveryScreenShowsActiveAlarm(t *testing.T) {
	f, _ := fixtures.Find("alarm-temp-high")
	for _, id := range []string{"nav.set", "nav.cure", "nav.alarms", "nav.trend", "nav.more"} {
		h := uitest.ForFixture(f, ui.Config{})
		tap(t, h, id)
		mustContain(t, auditHome(h), "ALARM: Temperature high")
	}
}

func TestHistoryHasAllTypesWithTimes(t *testing.T) {
	h := normal(t)
	tap(t, h, "nav.alarms")
	tap(t, h, "alarms.tab.1")
	seen := ""
	for {
		a := auditHome(h)
		seen += allText(a)
		if _, _, err := h.Find("history.page.next"); err != nil {
			break
		}
		tap(t, h, "history.page.next")
	}
	s := fixtures.Base()
	for _, al := range s.History {
		if !strings.Contains(seen, al.Title) {
			t.Errorf("history lacks %q", al.Title)
		}
		if !strings.Contains(seen, al.Since.Format("Jan 2 15:04")) {
			t.Errorf("history lacks the time of %q", al.Title)
		}
	}
}

func TestDestructiveStepsNeedHold(t *testing.T) {
	h := normal(t)
	tap(t, h, "nav.cure")
	tap(t, h, "cure.stop")
	if h.Panel.Screen() != "cure+dialog:stop" {
		t.Fatalf("%s", h.Panel.Screen())
	}
	tap(t, h, "dialog.ok") // a tap is refused
	if len(h.Ctl.Commands) != 0 || h.Panel.Screen() != "cure+dialog:stop" {
		t.Fatalf("tap confirmed a stop: %+v", h.Ctl.Commands)
	}
	h.HoldID("dialog.ok", 1000*time.Millisecond)
	if len(h.Ctl.Commands) != 0 {
		t.Fatal("short hold confirmed")
	}
	tap(t, h, "dialog.cancel")
	if h.Panel.Screen() != "cure" || len(h.Ctl.Commands) != 0 {
		t.Fatal("cancel")
	}
	tap(t, h, "cure.stop")
	h.HoldID("dialog.ok", 2100*time.Millisecond)
	h.Sync()
	if len(h.Ctl.Commands) != 1 || h.Ctl.Commands[0].Kind != api.CmdStop {
		t.Fatalf("%+v", h.Ctl.Commands)
	}
}

func TestHoldSurvivesBriefContactLoss(t *testing.T) {
	h := normal(t)
	tap(t, h, "nav.cure")
	tap(t, h, "cure.next")
	x, y, _ := h.Find("dialog.ok")
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchDown, X: x, Y: y})
	h.Advance(1000 * time.Millisecond)
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchUp, X: x, Y: y})
	h.Advance(80 * time.Millisecond)
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchDown, X: x, Y: y})
	h.Advance(1100 * time.Millisecond)
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchUp, X: x, Y: y})
	h.Advance(300 * time.Millisecond)
	if len(h.Ctl.Commands) != 1 || h.Ctl.Commands[0].Kind != api.CmdNextPhase {
		t.Fatalf("%+v", h.Ctl.Commands)
	}
}

func TestSlidingOffAButtonCancels(t *testing.T) {
	h := normal(t)
	tap(t, h, "nav.more")
	x, y, _ := h.Find("more.defrost")
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchDown, X: x, Y: y})
	h.Advance(40 * time.Millisecond)
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchMove, X: x, Y: 300 - 280})
	h.Advance(40 * time.Millisecond)
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchUp, X: x, Y: 20})
	h.Advance(300 * time.Millisecond)
	if h.Panel.Screen() != "more" {
		t.Fatalf("%s", h.Panel.Screen())
	}
}

func TestVeryShortAndStuckTouchesIgnored(t *testing.T) {
	h := normal(t)
	x, y, _ := h.Find("nav.set")
	h.Press(x, y, 5*time.Millisecond) // ghost
	if h.Panel.Screen() != "home" {
		t.Fatal("ghost touch fired")
	}
	h.Press(x, y, 25*time.Second) // water drop lifted late
	if h.Panel.Screen() != "home" {
		t.Fatal("stuck touch fired")
	}
}

func TestBounceAfterTapIsIgnored(t *testing.T) {
	h := idleManual(t)
	tap(t, h, "nav.set")
	x, y, _ := h.Find("edit.plus.0")
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchDown, X: x, Y: y})
	h.Advance(50 * time.Millisecond)
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchUp, X: x, Y: y})
	h.Sync()
	h.Frame()
	h.Advance(50 * time.Millisecond)
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchDown, X: x, Y: y}) // 50 ms later: a bounce
	h.Advance(50 * time.Millisecond)
	h.Panel.Input(ui.TouchEvent{Kind: ui.TouchUp, X: x, Y: y})
	h.Advance(300 * time.Millisecond)
	tap(t, h, "edit.save")
	h.Sync()
	if len(h.Ctl.Commands) != 1 || h.Ctl.Commands[0].Setpoint.TempC != 13.5 {
		t.Fatalf("%+v", h.Ctl.Commands)
	}
}

func TestInstallerGate(t *testing.T) {
	h := normal(t)
	tap(t, h, "nav.more")
	tap(t, h, "more.installer")
	for i := 0; i < 5; i++ {
		tap(t, h, "keypad.1")
		tap(t, h, "keypad.ok")
	}
	if h.Panel.Screen() != "more" {
		t.Fatalf("after 5 wrong codes: %s", h.Panel.Screen())
	}
	tap(t, h, "more.installer")
	if h.Panel.Screen() != "more" {
		t.Fatal("lockout did not hold")
	}
	h.Advance(61 * time.Second)
	if h.Panel.Screen() != "home" {
		t.Fatalf("timeout: %s", h.Panel.Screen())
	}
	h2 := normal(t)
	if _, _, err := h2.Find("inst.limits"); err == nil {
		t.Fatal("installer on home")
	}
	for _, id := range uitest.ViewsForInstaller() {
		tap(t, h2, id)
	}
	if h2.Panel.Screen() != "installer" {
		t.Fatalf("%s", h2.Panel.Screen())
	}
	tap(t, h2, "inst.limits")
	tap(t, h2, "edit.plus.0")
	tap(t, h2, "edit.save")
	h2.Sync()
	if len(h2.Ctl.Commands) != 1 || h2.Ctl.Commands[0].Limits == nil {
		t.Fatalf("%+v", h2.Ctl.Commands)
	}
}

func TestEndOfCureShowsVerifyAndClaimsNothing(t *testing.T) {
	f, _ := fixtures.Find("end-of-cure")
	h := uitest.ForFixture(f, ui.Config{})
	a := auditHome(h)
	mustContain(t, a, "VERIFY")
	tap(t, h, "banner")
	a = auditHome(h)
	got := strings.Join(strings.Fields(allText(a)), " ")
	if !strings.Contains(got, fixtures.VerifyText) {
		t.Fatalf("verify text not shown verbatim:\n%s", got)
	}
	mustContain(t, a, "VERIFY BEFORE USE")
	// With the core's end-of-recipe alarm the banner leads to the same place.
	f, _ = fixtures.Find("end-of-cure-alarm")
	h = uitest.ForFixture(f, ui.Config{})
	tap(t, h, "banner")
	if h.Panel.Screen() != "cure" {
		t.Fatalf("banner went to %s", h.Panel.Screen())
	}
}

func TestNoScreenClaimsProductIsSafeOrDone(t *testing.T) {
	bad := []string{"safe", "ready to eat", "ready", "done", "cured"}
	verify := strings.ToLower(fixtures.VerifyText)
	coreAction := "the controller does not determine whether product is safe."
	for _, s := range allScenes(t) {
		text := strings.Join(strings.Fields(strings.ToLower(allText(s.a))), " ")
		// The core's own wording is the one place the word appears, and it is a denial.
		text = strings.ReplaceAll(text, verify, "")
		text = strings.ReplaceAll(text, coreAction, "")
		text = strings.ReplaceAll(text, "do: "+coreAction, "")
		for _, w := range bad {
			if strings.Contains(text, w) {
				t.Errorf("%s: contains %q:\n%s", s.name, w, text)
			}
		}
	}
}

func TestDimAndSleepAndWake(t *testing.T) {
	h := normal(t)
	h.Advance(10*time.Minute + time.Second)
	if h.Panel.PowerState() != ui.PowerDim {
		t.Fatalf("%v", h.Panel.PowerState())
	}
	h.Advance(50 * time.Minute)
	if h.Panel.PowerState() != ui.PowerSleep {
		t.Fatalf("%v", h.Panel.PowerState())
	}
	// The waking touch lands on where SET is; it must only wake.
	h.Tap(200, 430)
	if h.Panel.PowerState() != ui.PowerOn || h.Panel.Screen() != "home" {
		t.Fatalf("%v %s", h.Panel.PowerState(), h.Panel.Screen())
	}
}

func TestAlarmNeverDimsAndWakesSleep(t *testing.T) {
	f, _ := fixtures.Find("alarm-temp-high")
	h := uitest.ForFixture(f, ui.Config{})
	h.Advance(2 * time.Hour)
	if h.Panel.PowerState() != ui.PowerOn {
		t.Fatalf("alarm dimmed: %v", h.Panel.PowerState())
	}
	mustContain(t, auditHome(h), "ALARM: Temperature high")
	// Silenced and acknowledged, still never.
	f, _ = fixtures.Find("alarm-acked")
	h = uitest.ForFixture(f, ui.Config{})
	h.Advance(2 * time.Hour)
	if h.Panel.PowerState() != ui.PowerOn {
		t.Fatalf("acked alarm dimmed: %v", h.Panel.PowerState())
	}
	// An alarm arriving during sleep wakes the screen without a touch.
	h = normal(t)
	h.Advance(2 * time.Hour)
	if h.Panel.PowerState() != ui.PowerSleep {
		t.Fatal("not asleep")
	}
	al, _ := fixtures.Find("alarm-temp-high")
	h.Ctl.SetSnapshot(al.Make())
	h.Advance(100 * time.Millisecond)
	if h.Panel.PowerState() != ui.PowerOn {
		t.Fatal("alarm did not wake")
	}
	mustContain(t, auditHome(h), "ALARM: Temperature high")
}

func TestBadReadingAndEndOfCureDoNotSleep(t *testing.T) {
	for _, name := range []string{"stale-temp", "fault-temp", "end-of-cure", "stalled"} {
		f, _ := fixtures.Find(name)
		h := uitest.ForFixture(f, ui.Config{})
		h.Advance(2 * time.Hour)
		if h.Panel.PowerState() != ui.PowerOn {
			t.Errorf("%s: %v", name, h.Panel.PowerState())
		}
	}
}

func TestRandomTapsNeverPanicOverAllFixtures(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	all := fixtures.All()
	for run := 0; run < 25; run++ {
		f := all[r.Intn(len(all))]
		h := uitest.ForFixture(f, ui.Config{})
		for i := 0; i < 150; i++ {
			if i%40 == 0 {
				h.Ctl.SetSnapshot(all[r.Intn(len(all))].Make())
			}
			switch r.Intn(10) {
			case 0:
				h.Press(r.Intn(800), r.Intn(480), time.Duration(r.Intn(3500))*time.Millisecond)
			default:
				h.Tap(r.Intn(800), r.Intn(480))
			}
		}
	}
}

func TestGarbageSnapshotsDoNotPanic(t *testing.T) {
	nan := math.NaN()
	bad := api.Snapshot{
		Temp: api.Reading{Value: nan, Quality: "weird"}, RH: api.Reading{Value: math.Inf(1)},
		Setpoint: api.Setpoints{TempC: nan, RHPct: nan, AirMS: math.Inf(-1)},
		Outputs:  api.Outputs{FanDutyPct: nan, FanRPM: math.Inf(1), Holds: []api.Hold{{RemainingSec: nan}}},
		Program:  api.Program{State: api.RunActive, PhaseRemaining: -5, WeightLossPct: nan, TargetLossPct: nan, StartWeightG: nan, PhaseIndex: 99},
		Active:   []api.Alarm{{}},
		History:  []api.Alarm{{}},
		Trend:    []api.Sample{{Valid: true, TempC: nan}, {Valid: true, TempC: math.Inf(1)}},
		Recipes:  []api.Recipe{{}},
		Health:   api.Health{SpeedFactor: nan, StorageFreeM: nan},
	}
	for _, id := range [][]string{{}, {"nav.set"}, {"nav.cure"}, {"nav.alarms"}, {"nav.trend"}, {"nav.more", "more.health"}} {
		h := uitest.New(bad, ui.Config{})
		for _, step := range id {
			tap(t, h, step)
		}
		for _, a := range []string{"NaN", "Inf"} {
			audit := auditHome(h)
			if strings.Contains(allText(audit), a) {
				t.Errorf("%v shows %s:\n%s", id, a, allText(audit))
			}
		}
	}
}

type nullDisplay struct{ frames atomic.Int64 }

func (d *nullDisplay) Present(*image.RGBA) error { d.frames.Add(1); return nil }

// hungController blocks forever in both methods, like a deadlocked core.
type hungController struct{ block chan struct{} }

func (c hungController) Snapshot() api.Snapshot { <-c.block; return api.Snapshot{} }
func (c hungController) Do(api.Command) error   { <-c.block; return nil }

// The loop must keep drawing whatever the controller does.
func TestLoopSurvivesHungController(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &nullDisplay{}
	in := make(chan ui.TouchEvent, 8)
	block := make(chan struct{})
	defer close(block)
	go ui.RunLoop(ctx, hungController{block}, ui.Config{StaleAfter: 100 * time.Millisecond}, d, in, ui.LoopOptions{})
	time.Sleep(2500 * time.Millisecond)
	if n := d.frames.Load(); n < 2 {
		t.Fatalf("frames: %d", n)
	}
}

type panicController struct{}

func (panicController) Snapshot() api.Snapshot { panic("snapshot boom") }
func (panicController) Do(api.Command) error   { panic("do boom") }

func TestLoopSurvivesPanickingController(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &nullDisplay{}
	go ui.RunLoop(ctx, panicController{}, ui.Config{}, d, make(chan ui.TouchEvent), ui.LoopOptions{})
	time.Sleep(1500 * time.Millisecond)
	if d.frames.Load() < 1 {
		t.Fatal("no frames")
	}
}

// A control loop running beside a stalled display must see no delay at all: the
// UI holds no lock and the controller is only ever called from UI goroutines.
type stuckDisplay struct{ entered chan struct{} }

func (d stuckDisplay) Present(*image.RGBA) error {
	select {
	case d.entered <- struct{}{}:
	default:
	}
	time.Sleep(time.Hour)
	return nil
}

func TestStuckDisplayDoesNotBlockController(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctl := fixtures.NewController(fixtures.Base(), time.Now)
	d := stuckDisplay{make(chan struct{}, 1)}
	go ui.RunLoop(ctx, ctl, ui.Config{}, d, make(chan ui.TouchEvent), ui.LoopOptions{})
	select {
	case <-d.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("display never called")
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			ctl.Snapshot()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("controller blocked by the UI")
	}
}

// The production package must not link the demo tooling.
func TestProductionDepsExcludeDemoTooling(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	for _, line := range strings.Fields(string(out)) {
		if strings.HasSuffix(line, "/ui/fixtures") || strings.HasSuffix(line, "/ui/uitest") || strings.Contains(line, "uidemo") {
			t.Errorf("production ui links %s", line)
		}
	}
}

// Every frame must differ from the frame one second earlier, on every screen and
// power stage, or a person has no way to tell a live panel from a frozen one.
func TestEveryScreenChangesEverySecond(t *testing.T) {
	check := func(name string, h *uitest.Harness) {
		t.Helper()
		a := append([]byte(nil), h.Img.Pix...)
		h.Advance(time.Second)
		if string(a) == string(h.Img.Pix) {
			t.Errorf("%s: frame did not change in 1 s", name)
		}
	}
	for _, s := range allScenes(t) {
		if strings.HasPrefix(s.name, "normal/") || strings.HasPrefix(s.name, "stalled/") || strings.HasPrefix(s.name, "alarm-temp-high/") {
			check(s.name, s.h)
		}
	}
	h := normal(t)
	h.Advance(11 * time.Minute)
	check("dim", h)
	h.Advance(time.Hour)
	if h.Panel.PowerState() != ui.PowerSleep {
		t.Fatal("not asleep")
	}
	check("sleep", h)
}

type noticeDisplay struct {
	entered chan struct{}
	notices atomic.Int64
	hang    bool
}

func (d *noticeDisplay) Present(*image.RGBA) error {
	if d.hang {
		select {
		case d.entered <- struct{}{}:
		default:
		}
		select {}
	}
	return nil
}

func (d *noticeDisplay) PaintNotice(*image.RGBA) error {
	d.notices.Add(1)
	return nil
}

func TestHungDisplayGetsFrozenNotice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctl := fixtures.NewController(fixtures.Base(), time.Now)
	d := &noticeDisplay{entered: make(chan struct{}, 1), hang: true}
	go ui.RunLoop(ctx, ctl, ui.Config{FrozenAfter: 300 * time.Millisecond}, d, make(chan ui.TouchEvent), ui.LoopOptions{})
	deadline := time.Now().Add(3 * time.Second)
	for d.notices.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no frozen notice")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestHealthyLoopPaintsNoNoticeThenStoppedOnExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ctl := fixtures.NewController(fixtures.Base(), time.Now)
	d := &noticeDisplay{}
	done := make(chan struct{})
	go func() {
		ui.RunLoop(ctx, ctl, ui.Config{FrozenAfter: 1500 * time.Millisecond}, d, make(chan ui.TouchEvent), ui.LoopOptions{})
		close(done)
	}()
	time.Sleep(3 * time.Second)
	if n := d.notices.Load(); n != 0 {
		t.Fatalf("healthy loop painted %d notices", n)
	}
	cancel()
	<-done
	if d.notices.Load() != 1 {
		t.Fatalf("stopped notice count %d", d.notices.Load())
	}
}

func TestSettingsRoundTripAndDamage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.json")
	if s, err := ui.LoadSettings(path); err != nil || s.Locked || s.HasOwnCode() {
		t.Fatalf("missing file: %+v %v", s, err)
	}
	var s ui.Settings
	s.Locked = true
	if err := s.SetCode("7391"); err != nil {
		t.Fatal(err)
	}
	if err := ui.SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "7391") {
		t.Fatal("code stored in the clear")
	}
	got, err := ui.LoadSettings(path)
	if err != nil || !got.Locked || got != s {
		t.Fatalf("%+v %v", got, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("left temporary files: %v", entries)
	}
	os.WriteFile(path, []byte("{broken"), 0o600)
	if _, err := ui.LoadSettings(path); err == nil {
		t.Fatal("damage not reported")
	}
}

func enterCode(t *testing.T, h *uitest.Harness, code string) {
	t.Helper()
	for _, d := range code {
		tap(t, h, "keypad."+string(d))
	}
	tap(t, h, "keypad.ok")
}

func TestDefaultCodeForcesItsOwnReplacement(t *testing.T) {
	var saved ui.Settings
	cfg := ui.Config{Persist: func(s ui.Settings) { saved = s }}
	h := uitest.NewFresh(fixtures.Base(), cfg)
	tap(t, h, "nav.more")
	tap(t, h, "more.installer")
	enterCode(t, h, "2580")
	if h.Panel.Screen() != "keypad" {
		t.Fatalf("default code opened %s", h.Panel.Screen())
	}
	tap(t, h, "keypad.back")
	if h.Panel.Screen() != "more" {
		t.Fatalf("cancel went to %s", h.Panel.Screen())
	}
	h.Advance(4200 * time.Millisecond)
	tap(t, h, "more.installer")
	enterCode(t, h, "2580")
	enterCode(t, h, "12") // too short
	if h.Panel.Screen() != "keypad" {
		t.Fatal(h.Panel.Screen())
	}
	tap(t, h, "keypad.del")
	tap(t, h, "keypad.del")
	enterCode(t, h, "2580") // the default again is refused
	enterCode(t, h, "7391")
	enterCode(t, h, "7392") // mismatch starts again
	enterCode(t, h, "7391")
	enterCode(t, h, "7391")
	if h.Panel.Screen() != "installer" {
		t.Fatalf("%s", h.Panel.Screen())
	}
	if !saved.HasOwnCode() {
		t.Fatal("code not handed over for saving")
	}
	// A restart uses the saved code, and the default no longer works.
	h2 := uitest.NewFresh(fixtures.Base(), ui.Config{Settings: saved})
	tap(t, h2, "nav.more")
	tap(t, h2, "more.installer")
	enterCode(t, h2, "2580")
	if h2.Panel.Screen() != "keypad" {
		t.Fatalf("default still works: %s", h2.Panel.Screen())
	}
	enterCode(t, h2, "7391")
	if h2.Panel.Screen() != "installer" {
		t.Fatalf("%s", h2.Panel.Screen())
	}
	// And the installer can change it again.
	tap(t, h2, "inst.code")
	enterCode(t, h2, "4455")
	enterCode(t, h2, "4455")
	if h2.Panel.Screen() != "installer" {
		t.Fatalf("%s", h2.Panel.Screen())
	}
}

func TestLockSurvivesRestartThroughTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.json")
	var s ui.Settings
	s.Locked = true
	if err := ui.SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	ctl := fixtures.NewController(fixtures.Base(), time.Now)
	frames := make(chan string, 256)
	in := make(chan ui.TouchEvent, 8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ui.RunLoop(ctx, ctl, ui.Config{StatePath: path}, &nullDisplay{}, in, ui.LoopOptions{OnFrame: func(fi ui.FrameInfo) {
			select {
			case frames <- fi.Screen:
			default:
			}
		}})
		close(done)
	}()
	// Unlock is only reachable through a real hold on the More screen.
	time.Sleep(700 * time.Millisecond)
	tapReal := func(x, y int, d time.Duration) {
		in <- ui.TouchEvent{Kind: ui.TouchDown, X: x, Y: y}
		time.Sleep(d)
		in <- ui.TouchEvent{Kind: ui.TouchUp, X: x, Y: y}
		time.Sleep(400 * time.Millisecond)
	}
	tapReal(730, 434, 80*time.Millisecond)
	tapReal(698, 138, 3200*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done
	got, err := ui.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Locked {
		t.Fatal("unlock did not reach the file")
	}
}

func TestDamagedSettingsAreShownOnHealth(t *testing.T) {
	h := uitest.New(fixtures.Base(), ui.Config{SettingsErr: "panel.json is damaged"})
	tap(t, h, "nav.more")
	tap(t, h, "more.health")
	mustContain(t, auditHome(h), "panel.json is damaged")
}

// Samples from a sensor that is not fitted are placeholders. A flat line at zero
// would look like a measurement.
func TestTrendDoesNotPlotSensorNotFitted(t *testing.T) {
	f, _ := fixtures.Find("not-fitted")
	h := uitest.ForFixture(f, ui.Config{})
	tap(t, h, "nav.trend")
	a := auditHome(h)
	mustContain(t, a, "NOT FITTED")
	if strings.Contains(allText(a), "WEIGHT min") {
		t.Fatalf("weight trend plotted:\n%s", allText(a))
	}
}

func BenchmarkHomeFrame(b *testing.B) {
	h := uitest.New(fixtures.Base(), ui.Config{})
	for i := 0; i < b.N; i++ {
		h.Panel.Render()
	}
	_ = fmt.Sprint(h)
}
