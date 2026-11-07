// uidemo renders the panel from scripted fixtures, headless or on real hardware.
//
//	uidemo render -out DIR            every state and screen as an 800x480 PNG
//	uidemo walk -script F -out DIR    run a tap script headless, log timings
//	uidemo hits -fixture N -nav A,B   list touch targets on a screen
//	uidemo panel [-fixture N]         run on /dev/fb0 and the touch device
//
// With the demoinject build tag only, panel also accepts -inject FIFO, a
// tap-by-coordinates path that does not exist otherwise.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui/fixtures"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui/uitest"
)

// defaultArgs, set at link time, replaces the command line ('|' separated). The
// rig's service runs a fixed binary with no arguments, so each rig scene is a build.
var defaultArgs string

func main() {
	log.SetFlags(log.Lmicroseconds)
	if defaultArgs != "" {
		os.Args = append(os.Args[:1], strings.Split(defaultArgs, "|")...)
	}
	if len(os.Args) < 2 {
		runPanel(nil)
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "render":
		runRender(args)
	case "walk":
		runWalk(args)
	case "hits":
		runHits(args)
	case "panel":
		runPanel(args)
	case "stopped":
		if err := ui.PaintStopped(ui.DefaultFramebuffer); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown mode %q", cmd)
	}
}

func newHarness(f fixtures.Fixture, cfg ui.Config) *uitest.Harness {
	return uitest.ForFixture(f, cfg)
}

func runRender(args []string) {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	out := fs.String("out", "screens", "output directory")
	fs.Parse(args)
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	idx, err := os.Create(filepath.Join(*out, "INDEX.tsv"))
	if err != nil {
		log.Fatal(err)
	}
	defer idx.Close()
	fmt.Fprintln(idx, "file\tfixture\tview\tscreen\tpower\twidth\theight\taudit_problems")
	saved, problems := 0, 0
	defer func() { fmt.Printf("render finished: %d frames audited, %d problems\n", saved, problems) }()
	save := func(fx, view string, h *uitest.Harness, audit *ui.Audit) {
		name := fx + "__" + view + ".png"
		if err := h.SavePNG(filepath.Join(*out, name)); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(idx, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\n", name, fx, view, h.Panel.Screen(), h.Panel.PowerState(), h.Img.Bounds().Dx(), h.Img.Bounds().Dy(), len(audit.Problems))
		saved++
		problems += len(audit.Problems)
		for _, p := range audit.Problems {
			fmt.Printf("AUDIT %s: %s\n", name, p)
		}
	}
	for _, f := range fixtures.All() {
		audit := &ui.Audit{}
		h := newHarness(f, ui.Config{})
		h.Panel.SetAudit(audit)
		h.Sync()
		h.Frame()
		save(f.Name, "home", h, audit)
		for _, v := range uitest.ViewsFor(f.Name) {
			a := &ui.Audit{}
			h := newHarness(f, ui.Config{})
			h.Panel.SetAudit(a)
			h.Sync()
			h.Frame()
			for _, id := range v.Path {
				if _, err := h.TapID(id); err != nil {
					log.Fatalf("%s/%s: %v", f.Name, v.Name, err)
				}
			}
			a.Problems, a.Texts = nil, nil
			h.Frame()
			save(f.Name, v.Name, h, a)
		}
	}
	// States that need time to pass or a gesture in progress.
	base, _ := fixtures.Find("normal")
	special := func(name string, setup func(h *uitest.Harness)) {
		a := &ui.Audit{}
		h := newHarness(base, ui.Config{})
		h.Panel.SetAudit(a)
		setup(h)
		a.Problems = nil
		h.Frame()
		save("state", name, h, a)
	}
	special("dim", func(h *uitest.Harness) { h.Advance(10*time.Minute + time.Second) })
	special("sleep", func(h *uitest.Harness) { h.Advance(60*time.Minute + time.Second) })
	idle, _ := fixtures.Find("idle-manual")
	specialIdle := func(name string, setup func(h *uitest.Harness)) {
		a := &ui.Audit{}
		h := newHarness(idle, ui.Config{})
		h.Panel.SetAudit(a)
		setup(h)
		a.Problems = nil
		h.Frame()
		save("state", name, h, a)
	}
	specialIdle("timeout-toast", func(h *uitest.Harness) {
		h.TapID("nav.set")
		h.TapID("edit.plus.0")
		h.Advance(31 * time.Second)
	})
	specialIdle("setpoint-refused", func(h *uitest.Harness) {
		h.Ctl.Refuse = "setpoint outside the allowed range"
		h.TapID("nav.set")
		h.TapID("edit.plus.0")
		h.TapID("edit.save")
		h.Sync()
	})
	lockSetup := func(h *uitest.Harness) {
		h.TapID("nav.more")
		h.HoldID("more.lock", 2100*time.Millisecond)
		h.Advance(5 * time.Second)
	}
	special("locked-home", func(h *uitest.Harness) { lockSetup(h); h.TapID("nav.home") })
	special("locked-set", func(h *uitest.Harness) { lockSetup(h); h.TapID("nav.set") })
	special("locked-cure", func(h *uitest.Harness) { lockSetup(h); h.TapID("nav.cure") })
	special("locked-more", func(h *uitest.Harness) { lockSetup(h) })
	special("locked-alarms", func(h *uitest.Harness) { lockSetup(h); h.TapID("nav.alarms") })
	special("locked-trend", func(h *uitest.Harness) { lockSetup(h); h.TapID("nav.trend") })
	special("locked-then-alarm-home", func(h *uitest.Harness) {
		lockSetup(h)
		h.TapID("nav.home")
		f, _ := fixtures.Find("alarm-temp-high")
		h.Ctl.SetSnapshot(f.Make())
		h.Advance(time.Second)
	})
	special("wrong-code-lockout", func(h *uitest.Harness) {
		h.TapID("nav.more")
		h.TapID("more.installer")
		for i := 0; i < 5; i++ {
			h.TapID("keypad.1")
			h.TapID("keypad.ok")
			h.Advance(3200 * time.Millisecond)
		}
	})
	special("installer-timed-out", func(h *uitest.Harness) {
		for _, id := range uitest.ViewsForInstaller() {
			h.TapID(id)
		}
		h.Advance(61 * time.Second)
	})
	special("drift-while-asleep-wakes", func(h *uitest.Harness) {
		h.Advance(61 * time.Minute)
		f, _ := fixtures.Find("drift-no-alarm")
		h.Ctl.SetSnapshot(f.Make())
		h.Advance(time.Second)
	})
	special("stall-while-asleep-wakes", func(h *uitest.Harness) {
		h.Advance(61 * time.Minute)
		h.Ctl.Frozen = true
		h.Advance(6 * time.Second)
	})
	refEntry := func(name string, temp string, steps ...string) {
		special(name, func(h *uitest.Harness) {
			for _, id := range []string{"nav.more", "more.refcheck", "keypad.2", "keypad.5", "keypad.8", "keypad.0", "keypad.ok"} {
				h.TapID(id)
			}
			for _, d := range temp {
				id := "keypad." + string(d)
				if d == '.' {
					id = "keypad.dot"
				}
				h.TapID(id)
			}
			for _, id := range steps {
				h.TapID(id)
			}
		})
	}
	refEntry("refcheck-temp-typed", "13.5")
	refEntry("refcheck-rh-entry", "13.5", "keypad.ok")
	refEntry("refcheck-confirm-pass", "13.5", "keypad.ok", "keypad.ok")
	refEntry("refcheck-confirm-fail", "19.5", "keypad.ok", "keypad.ok")
	freshH := uitest.NewFresh(fixtures.Base(), ui.Config{})
	freshA := &ui.Audit{}
	freshH.Panel.SetAudit(freshA)
	for _, id := range []string{"nav.more", "more.installer", "keypad.2", "keypad.5", "keypad.8", "keypad.0", "keypad.ok"} {
		if _, err := freshH.TapID(id); err != nil {
			log.Fatal(err)
		}
	}
	freshA.Problems = nil
	freshH.Frame()
	save("state", "first-entry-forces-new-code", freshH, freshA)
	special("hold-progress", func(h *uitest.Harness) {
		h.TapID("nav.cure")
		h.TapID("cure.stop")
		x, y, _ := h.Find("dialog.ok")
		h.Panel.Input(ui.TouchEvent{Kind: ui.TouchDown, X: x, Y: y, At: h.Now})
		h.Advance(1200 * time.Millisecond)
	})
	writePNG := func(name string, img *image.RGBA) {
		f, err := os.Create(filepath.Join(*out, name))
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		fmt.Fprintf(idx, "%s\tstate\tnotice\tnotice\t-\t%d\t%d\t0\n", name, img.Bounds().Dx(), img.Bounds().Dy())
		if err := png.Encode(f, img); err != nil {
			log.Fatal(err)
		}
	}
	writePNG("state__panel-frozen-notice.png", ui.FrozenFrame())
	writePNG("state__panel-stopped-notice.png", ui.StoppedFrame())
	simFix, _ := fixtures.Find("simulated")
	a := &ui.Audit{}
	h := newHarness(simFix, ui.Config{})
	h.Panel.SetAudit(a)
	h.Advance(61 * time.Minute)
	a.Problems = nil
	h.Frame()
	save("state", "simulated-sleep", h, a)
	almFix, _ := fixtures.Find("alarm-temp-high")
	a = &ui.Audit{}
	h = newHarness(almFix, ui.Config{})
	h.Panel.SetAudit(a)
	h.Advance(61 * time.Minute)
	a.Problems = nil
	h.Frame()
	save("state", "alarm-after-61min-idle", h, a)
}

func runHits(args []string) {
	fs := flag.NewFlagSet("hits", flag.ExitOnError)
	fx := fs.String("fixture", "normal", "fixture")
	nav := fs.String("nav", "", "comma separated target ids to tap first")
	fs.Parse(args)
	f, ok := fixtures.Find(*fx)
	if !ok {
		log.Fatalf("no fixture %q", *fx)
	}
	h := newHarness(f, ui.Config{})
	if *nav != "" {
		for _, id := range strings.Split(*nav, ",") {
			if _, err := h.TapID(id); err != nil {
				log.Fatal(err)
			}
		}
	}
	fmt.Printf("screen=%s\n", h.Panel.Screen())
	for _, t := range h.Panel.Hits() {
		fmt.Printf("%-24s x=%d..%d y=%d..%d size=%dx%d hold_ms=%d\n", t.ID, t.R.Min.X, t.R.Max.X, t.R.Min.Y, t.R.Max.Y, t.R.Dx(), t.R.Dy(), t.Hold.Milliseconds())
	}
}

// runWalk executes a script of coordinate taps against the headless panel:
//
//	fixture NAME        switch the controller to a fixture's state
//	tap X Y             touch and release at a coordinate
//	hold X Y MS         touch for MS milliseconds of panel time
//	tapid ID            tap a target by name; the log shows its coordinates
//	holdid ID MS
//	wait MS             let panel time pass
//	shot NAME           save the current frame
func runWalk(args []string) {
	fs := flag.NewFlagSet("walk", flag.ExitOnError)
	script := fs.String("script", "", "script file, or - for stdin")
	out := fs.String("out", "walk", "output directory for frames")
	fx := fs.String("fixture", "normal", "starting fixture")
	fs.Parse(args)
	f, ok := fixtures.Find(*fx)
	if !ok {
		log.Fatalf("no fixture %q", *fx)
	}
	src := os.Stdin
	if *script != "" && *script != "-" {
		var err error
		if src, err = os.Open(*script); err != nil {
			log.Fatal(err)
		}
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	h := newHarness(f, ui.Config{})
	seen := 0
	fmt.Println("step\tpanel_ms\top\tx\ty\tresult\tbefore\tafter\tfeedback_us\tredraw_us\tcommands")
	sc := bufio.NewScanner(src)
	step := 0
	logTap := func(op string, x, y int, r uitest.TapResult) {
		cmds := ""
		for _, c := range h.Ctl.Commands[seen:] {
			cmds += describe(c) + ";"
		}
		seen = len(h.Ctl.Commands)
		fmt.Printf("%d\t%d\t%s\t%d\t%d\t%s\t%s\t%s\t%d\t%d\t%s\n", step, h.Now.Sub(fixtures.T0).Milliseconds(), op, x, y, r.Action, r.Before, r.After, r.FeedbackMicros, r.RedrawMicros, cmds)
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		step++
		w := strings.Fields(line)
		num := func(i int) int {
			if i >= len(w) {
				log.Fatalf("line %d: missing argument: %s", step, line)
			}
			n, err := strconv.Atoi(w[i])
			if err != nil {
				log.Fatalf("line %d: %v", step, err)
			}
			return n
		}
		switch w[0] {
		case "fixture":
			nf, ok := fixtures.Find(w[1])
			if !ok {
				log.Fatalf("no fixture %q", w[1])
			}
			h.Ctl.SetSnapshot(nf.Make())
			h.Advance(600 * time.Millisecond)
			fmt.Printf("%d\t%d\tfixture\t\t\t%s\t\t%s\t\t\t\n", step, h.Now.Sub(fixtures.T0).Milliseconds(), w[1], h.Panel.Screen())
		case "tap":
			x, y := num(1), num(2)
			logTap("tap", x, y, h.Tap(x, y))
		case "hold":
			x, y := num(1), num(2)
			logTap("hold", x, y, h.Press(x, y, time.Duration(num(3))*time.Millisecond))
		case "tapid":
			x, y, err := h.Find(w[1])
			if err != nil {
				fmt.Printf("%d\t%d\ttapid\t\t\tERROR %v\t\t\t\t\t\n", step, h.Now.Sub(fixtures.T0).Milliseconds(), err)
				continue
			}
			logTap("tap", x, y, h.Tap(x, y))
		case "holdid":
			x, y, err := h.Find(w[1])
			if err != nil {
				fmt.Printf("%d\t%d\tholdid\t\t\tERROR %v\t\t\t\t\t\n", step, h.Now.Sub(fixtures.T0).Milliseconds(), err)
				continue
			}
			logTap("hold", x, y, h.Press(x, y, time.Duration(num(2))*time.Millisecond))
		case "wait":
			h.Advance(time.Duration(num(1)) * time.Millisecond)
			fmt.Printf("%d\t%d\twait\t\t\t\t\t%s\t\t\t\n", step, h.Now.Sub(fixtures.T0).Milliseconds(), h.Panel.Screen())
		case "shot":
			if err := h.SavePNG(filepath.Join(*out, w[1]+".png")); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("%d\t%d\tshot\t\t\t%s.png\t\t%s\t\t\t\n", step, h.Now.Sub(fixtures.T0).Milliseconds(), w[1], h.Panel.Screen())
		default:
			log.Fatalf("line %d: unknown op %q", step, w[0])
		}
	}
}

// hangingDisplay stops returning from Present at a chosen time, like a stuck
// driver, while PaintNotice still works. Demo only.
type hangingDisplay struct {
	*ui.Framebuffer
	at time.Time
}

func (d *hangingDisplay) Present(img *image.RGBA) error {
	if time.Now().After(d.at) {
		log.Printf("uidemo: display hanging now, as asked")
		select {}
	}
	return d.Framebuffer.Present(img)
}

// shiftTimes moves a fixture's timestamps to the present for a live run.
func shiftTimes(s *api.Snapshot, d time.Duration) {
	for i := range s.Trend {
		s.Trend[i].T = s.Trend[i].T.Add(d)
	}
	for _, l := range [][]api.Alarm{s.Active, s.History} {
		for i := range l {
			l[i].Since = l[i].Since.Add(d)
			if !l[i].Cleared.IsZero() {
				l[i].Cleared = l[i].Cleared.Add(d)
			}
		}
	}
	s.Health.StartedAt = s.Health.StartedAt.Add(d)
	if !s.Health.LastOutage.At.IsZero() {
		s.Health.LastOutage.At = s.Health.LastOutage.At.Add(d)
	}
}

func describe(c api.Command) string {
	s := string(c.Kind)
	if c.Setpoint != nil {
		s += fmt.Sprintf("(temp=%.1f rh=%.0f air=%.2f)", c.Setpoint.TempC, c.Setpoint.RHPct, c.Setpoint.AirMS)
	}
	if c.AlarmID != "" {
		s += "(" + c.AlarmID + ")"
	}
	if c.RecipeID != "" {
		s += fmt.Sprintf("(%s start_g=%.0f)", c.RecipeID, c.StartWeightG)
	}
	return s
}

func runPanel(args []string) {
	fs := flag.NewFlagSet("panel", flag.ExitOnError)
	fx := fs.String("fixture", "normal", "fixture to show")
	cycle := fs.Duration("cycle", 0, "move through fixtures at this interval")
	fbPath := fs.String("fb", ui.DefaultFramebuffer, "framebuffer device")
	inject := addInjectFlag(fs)
	autotap := addAutotapFlag(fs)
	realcoreFlags := addRealCoreFlags(fs)
	simulated := fs.Bool("simulated", false, "mark the fixture as a simulated plant")
	statePath := fs.String("state", "", "panel settings file (lock state, installer code)")
	freezeAfter := fs.Duration("freeze-after", 0, "demo: hang the display after this long, to show the frozen notice")
	exitAfter := fs.Duration("exit-after", 0, "demo: end the panel cleanly after this long")
	presetCode := fs.String("preset-code", "", "demo: start with this installer code set, not the default")
	dimAfter := fs.Duration("dim-after", 0, "override the dim delay")
	sleepAfter := fs.Duration("sleep-after", 0, "override the sleep delay")
	fs.Parse(args)
	f, ok := fixtures.Find(*fx)
	if !ok {
		log.Fatalf("no fixture %q", *fx)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if *exitAfter > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *exitAfter)
		defer cancel()
	}
	first := f.Make()
	first.Now = time.Now()
	first.Health.Simulated = first.Health.Simulated || *simulated
	shiftTimes(&first, time.Since(fixtures.T0))
	fx2 := fixtures.NewController(first, time.Now)
	fx2.Frozen = f.Frozen
	var ctl api.Controller = fx2
	if c := startRealCore(ctx, realcoreFlags); c != nil {
		ctl = c
	}
	fb, err := ui.OpenFramebuffer(*fbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer fb.Close()
	log.Printf("uidemo: framebuffer %s", fb.Geometry())
	if d, err := ui.DescribeTouch(ui.DefaultTouchName); err != nil {
		log.Printf("uidemo: touch: %v", err)
	} else {
		log.Printf("uidemo: touch %s", d)
	}
	bl := ui.OpenBacklight()
	log.Printf("uidemo: backlight %s", bl.Describe())
	bl.Set(ui.PowerOn)
	defer bl.Set(ui.PowerOn)
	input := make(chan ui.TouchEvent, 64)
	go ui.ReadTouch(ctx, ui.DefaultTouchName, input)
	startInject(ctx, inject, input)
	startAutotap(ctx, autotap, input)
	if *cycle > 0 {
		go func() {
			all := fixtures.All()
			for i := 0; ctx.Err() == nil; i++ {
				select {
				case <-ctx.Done():
				case <-time.After(*cycle):
				}
				nf := all[i%len(all)]
				log.Printf("uidemo: fixture %s", nf.Name)
				fx2.Frozen = nf.Frozen
				fx2.SetSnapshot(nf.Make())
			}
		}()
	}
	var disp ui.Display = fb
	if *freezeAfter > 0 {
		disp = &hangingDisplay{Framebuffer: fb, at: time.Now().Add(*freezeAfter)}
	}
	cfg := ui.Config{Location: time.UTC, DimAfter: *dimAfter, SleepAfter: *sleepAfter, StatePath: *statePath}
	if *presetCode != "" {
		if err := cfg.Settings.SetCode(*presetCode); err != nil {
			log.Fatal(err)
		}
	}
	ui.RunLoop(ctx, ctl, cfg, disp, input, ui.LoopOptions{Power: func(pw ui.Power) {
		bl.Set(pw)
		log.Printf("power %s backlight_level=%d", pw, bl.Level())
	}, OnFrame: func(fi ui.FrameInfo) {
		if !fi.InputAt.IsZero() {
			log.Printf("frame screen=%s action=%s input_to_present_us=%d render_to_present_us=%d", fi.Screen, fi.Action, fi.PresentAt.Sub(fi.InputAt).Microseconds(), fi.PresentAt.Sub(fi.RenderedAt).Microseconds())
		}
	}})
}
