package drivers

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

func relayRig() (*Relays, *fakeChip, map[hal.OutputID]int) {
	chip := newFakeChip()
	pins := DefaultConfig().Relays
	return NewRelays(chip.opener(), pins), chip, pins
}

func TestRelayIsActiveHighAndClaimsLow(t *testing.T) {
	r, chip, pins := relayRig()
	if err := r.Set(hal.OutHeat, false); err != nil {
		t.Fatal(err)
	}
	l := chip.lines[pins[hal.OutHeat]]
	if l.flags&flagOutput == 0 || l.flags&flagPullDown == 0 {
		t.Errorf("flags %#x: needs output with pull-down", l.flags)
	}
	if l.level {
		t.Error("claiming a line must leave it low")
	}
	if err := r.Set(hal.OutHeat, true); err != nil {
		t.Fatal(err)
	}
	if !l.level {
		t.Error("on must drive high")
	}
}

func TestRelaySetIsIdempotent(t *testing.T) {
	r, chip, pins := relayRig()
	for i := 0; i < 3; i++ {
		if err := r.Set(hal.OutCool, true); err != nil {
			t.Fatal(err)
		}
	}
	l := chip.lines[pins[hal.OutCool]]
	if !l.level || len(chip.lines) != 1 {
		t.Errorf("level %v lines %d", l.level, len(chip.lines))
	}
}

func TestRelayAllOffAttemptsEveryOutputDespiteFailures(t *testing.T) {
	r, chip, pins := relayRig()
	for id := range pins {
		if err := r.Set(id, true); err != nil {
			t.Fatal(err)
		}
	}
	bad := errors.New("line write failed")
	chip.lines[pins[hal.OutHeat]].setErr = bad
	err := r.AllOff()
	if !errors.Is(err, bad) {
		t.Fatalf("err = %v", err)
	}
	for id, pin := range pins {
		if id == hal.OutHeat {
			continue
		}
		if chip.lines[pin].level {
			t.Errorf("%s still on after AllOff", id)
		}
	}
}

func TestRelayAllOffContinuesPastUnclaimableLine(t *testing.T) {
	r, chip, pins := relayRig()
	chip.failPins[pins[hal.OutCool]] = errors.New("device or resource busy")
	if err := r.Set(hal.OutHeat, true); err != nil {
		t.Fatal(err)
	}
	if err := r.AllOff(); err == nil || !strings.Contains(err.Error(), "cool") {
		t.Fatalf("err = %v", err)
	}
	if chip.lines[pins[hal.OutHeat]].level {
		t.Error("heat still on")
	}
}

func TestRelayUnknownOutput(t *testing.T) {
	r, _, _ := relayRig()
	if err := r.Set("freezer", true); err == nil {
		t.Fatal("accepted an unknown output")
	}
}

func TestRelayNoChipIsAbsent(t *testing.T) {
	r := NewRelays(func() (gpioChip, error) { return nil, absent("gpio chip", nil) }, DefaultConfig().Relays)
	if err := r.Set(hal.OutHeat, false); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
	if err := r.AllOff(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

func TestRelayCloseDrivesLowThenReleases(t *testing.T) {
	r, chip, pins := relayRig()
	if err := r.Set(hal.OutHeat, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	l := chip.lines[pins[hal.OutHeat]]
	if l.level || !l.closed {
		t.Errorf("level %v closed %v", l.level, l.closed)
	}
}

func TestDoor(t *testing.T) {
	chip := newFakeChip()
	d := NewDoor(chip.opener(), 26)
	defer d.Close()
	if open, err := d.Open(); err != nil || !open {
		t.Fatalf("pull-up with nothing closing the contact reads open: %v %v", open, err)
	}
	chip.lines[26].level = false
	if open, err := d.Open(); err != nil || open {
		t.Fatalf("shut: %v %v", open, err)
	}
	if chip.lines[26].flags&flagPullUp == 0 {
		t.Error("door line needs a pull-up")
	}
}

func TestDoorNotFitted(t *testing.T) {
	d := NewDoor(func() (gpioChip, error) { t.Fatal("must not touch GPIO"); return nil, nil }, -1)
	if _, err := d.Open(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	mutate := map[string]func(*Config){
		"relay on a pull-up pin": func(c *Config) { c.Relays[hal.OutHeat] = 7 },
		"pin conflict":           func(c *Config) { c.FanTachPin = c.Relays[hal.OutCool] },
		"i2c pin reused":         func(c *Config) { c.HX711DataPin = 2 },
		"pwm pin reused":         func(c *Config) { c.FanTachPin = 12 },
		"off the header":         func(c *Config) { c.DHTPin = 40 },
		"door conflict":          func(c *Config) { c.DoorPin = c.HX711ClockPin },
		"unknown dht backend":    func(c *Config) { c.DHTBackend = "x" },
	}
	for name, m := range mutate {
		c := DefaultConfig()
		m(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
		if _, err := New(c); err == nil {
			t.Errorf("New with %s accepted", name)
		}
	}
}

// Every default relay pin must be a pull-down at reset per the rig probe.
func TestDefaultRelayPinsPullDownAtReset(t *testing.T) {
	for id, pin := range DefaultConfig().Relays {
		if pin < 9 {
			t.Errorf("%s on GPIO %d", id, pin)
		}
	}
}

func TestWatchdogNotifiesSystemd(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "notify")
	l, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("NOTIFY_SOCKET", sock)
	var _ hal.Watchdog = NewWatchdog()
	w := NewWatchdog()
	read := func() string {
		buf := make([]byte, 64)
		l.SetReadDeadline(time.Now().Add(time.Second))
		n, err := l.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		return string(buf[:n])
	}
	if err := w.Ready(); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "READY=1" {
		t.Errorf("got %q", got)
	}
	if err := w.Pet(); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "WATCHDOG=1" {
		t.Errorf("got %q", got)
	}
}

func TestWatchdogNoSocket(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := NewWatchdog().Pet(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

func TestWatchdogSocketGone(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", filepath.Join(t.TempDir(), "missing"))
	if err := NewWatchdog().Pet(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

func TestWatchdogFullQueueDoesNotBlock(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "notify")
	l, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("NOTIFY_SOCKET", sock)
	w := NewWatchdog()
	for i := 0; i < 2000; i++ {
		start := time.Now()
		_ = w.Pet() // nobody reads: errors are fine, blocking is not
		if el := time.Since(start); el > 300*time.Millisecond {
			t.Fatalf("Pet took %v", el)
		}
		if i > 400 {
			return
		}
	}
}

func TestNewBuildsEveryDevice(t *testing.T) {
	c, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if c.Climate == nil || c.Air == nil || c.Scale == nil || c.Fan == nil || c.Outputs == nil || c.Door == nil || c.Close == nil {
		t.Fatalf("%+v", c)
	}
	if _, err := c.Door.Open(); !errors.Is(err, hal.ErrAbsent) {
		t.Errorf("door: %v", err)
	}
	if err := c.Outputs.Set(hal.OutHeat, true); !errors.Is(err, hal.ErrAbsent) {
		t.Errorf("no guard running: %v", err)
	}
	c.Close()
}
