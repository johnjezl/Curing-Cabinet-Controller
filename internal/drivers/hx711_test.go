package drivers

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

const (
	testDOUT = 5
	testSCK  = 13
)

// hxDevice models the chip on the far end of the two lines: DOUT low means data
// ready, each SCK rising edge shifts out a bit, and pulse 25 forces DOUT high.
type hxDevice struct {
	mu        sync.Mutex
	value     uint32
	ready     bool
	dout      bool
	pulses    int
	maxPulses int
	frames    int
	stuckLow  bool
}

func (d *hxDevice) load(v int32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.value = uint32(v) & 0xFFFFFF
	d.ready = true
	d.pulses = 0
	d.dout = false
}

func (d *hxDevice) sck(high bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !high {
		return
	}
	d.pulses++
	if d.pulses > d.maxPulses {
		d.maxPulses = d.pulses
	}
	switch {
	case d.pulses <= 24:
		d.dout = d.value&(1<<(24-d.pulses)) != 0
	default:
		d.dout = true
		d.ready = false
		d.frames++
	}
}

func (d *hxDevice) doutLevel() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stuckLow {
		return false
	}
	if !d.ready && d.pulses == 0 {
		return true
	}
	return d.dout
}

// rig wires a device to a fake chip and an HX711 driver with a virtual clock.
func hxRig(dev *hxDevice) (*HX711, *fakeChip, *virtualClock) {
	chip := newFakeChip()
	chip.onLine = func(l *fakeLine) {
		switch l.pin {
		case testDOUT:
			l.onGet = dev.doutLevel
		case testSCK:
			l.onSet = dev.sck
		}
	}
	h := NewHX711(chip.opener(), testDOUT, testSCK)
	clk := &virtualClock{t: time.Unix(1000, 0)}
	h.now = clk.Now
	h.sleep = clk.Sleep
	return h, chip, clk
}

type virtualClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *virtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *virtualClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestHX711ClocksOutSignedValues(t *testing.T) {
	for _, want := range []int32{0, 1, -1, 123456, -123456, 0x7FFFFE, -0x7FFFFF} {
		dev := &hxDevice{}
		dev.load(want)
		h, chip, _ := hxRig(dev)
		got, err := h.ReadRaw()
		if err != nil {
			t.Fatalf("%d: %v", want, err)
		}
		if got != want {
			t.Errorf("got %d want %d", got, want)
		}
		if dev.maxPulses != 25 {
			t.Errorf("%d: %d clock pulses, gain 128 needs exactly 25", want, dev.maxPulses)
		}
		sck := chip.lines[testSCK]
		if sck.sets[len(sck.sets)-1] {
			t.Error("SCK left high: more than 60 us of that powers the chip down")
		}
		if got := chip.lines[testDOUT].flags; got&flagPullUp == 0 || got&flagInput == 0 {
			t.Errorf("DOUT flags %#x: needs input with pull-up", got)
		}
		if got := sck.flags; got&flagOutput == 0 {
			t.Errorf("SCK flags %#x", got)
		}
	}
}

func TestHX711AbsentChipTimesOutWithErrAbsent(t *testing.T) {
	dev := &hxDevice{} // never ready, DOUT held high by the pull-up
	h, _, clk := hxRig(dev)
	start := clk.Now()
	_, err := h.ReadRaw()
	if !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
	if el := clk.Now().Sub(start); el > 200*time.Millisecond || el < 100*time.Millisecond {
		t.Errorf("gave up after %v of virtual time", el)
	}
	if dev.maxPulses != 0 {
		t.Error("clocked a chip that never signalled ready")
	}
}

func TestHX711AbsentInRealTime(t *testing.T) {
	dev := &hxDevice{}
	h, _, _ := hxRig(dev)
	h.now, h.sleep = time.Now, time.Sleep
	start := time.Now()
	if _, err := h.ReadRaw(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
	if el := time.Since(start); el > 200*time.Millisecond {
		t.Errorf("took %v, bound is 200 ms", el)
	}
}

func TestHX711DOUTStuckLowIsBadRead(t *testing.T) {
	dev := &hxDevice{stuckLow: true}
	h, _, _ := hxRig(dev)
	if _, err := h.ReadRaw(); !errors.Is(err, hal.ErrBadRead) {
		t.Fatalf("err = %v", err)
	}
}

func TestHX711SaturatedIsBadRead(t *testing.T) {
	for _, v := range []int32{0x7FFFFF, -0x800000} {
		dev := &hxDevice{}
		dev.load(v)
		h, _, _ := hxRig(dev)
		if _, err := h.ReadRaw(); !errors.Is(err, hal.ErrBadRead) {
			t.Errorf("%d: err = %v", v, err)
		}
	}
}

func TestHX711WaitsForReady(t *testing.T) {
	dev := &hxDevice{}
	h, _, clk := hxRig(dev)
	h.sleep = func(d time.Duration) {
		clk.Sleep(d)
		if clk.Now().Sub(time.Unix(1000, 0)) >= 90*time.Millisecond && !dev.ready && dev.frames == 0 {
			dev.load(4242)
		}
	}
	got, err := h.ReadRaw()
	if err != nil || got != 4242 {
		t.Fatalf("got %d, %v", got, err)
	}
}

// A preempted pulse must discard the frame, keep the chip in step by finishing
// the 25 pulses, and take the next conversion cleanly.
func TestHX711SlowPulseDiscardsFrameAndRetries(t *testing.T) {
	dev := &hxDevice{}
	dev.load(1111)
	h, _, clk := hxRig(dev)
	pulse := 0
	sck := func(high bool) {
		dev.sck(high)
		if high {
			pulse++
			if pulse == 7 {
				clk.Sleep(70 * time.Microsecond) // exceeds the SCK-high limit
			}
		}
	}
	if err := h.hookSCKForTest(sck); err != nil {
		t.Fatal(err)
	}
	h.sleep = func(d time.Duration) {
		clk.Sleep(d)
		if dev.frames == 1 && !dev.ready {
			dev.mu.Lock()
			dev.pulses = 0
			dev.mu.Unlock()
			dev.load(2222)
		}
	}
	got, err := h.ReadRaw()
	if err != nil {
		t.Fatal(err)
	}
	if got != 2222 {
		t.Errorf("got %d: the disturbed frame's value must not be returned", got)
	}
	if dev.frames != 2 {
		t.Errorf("frames %d", dev.frames)
	}
}

func TestHX711SlowPulseEveryTimeIsBadRead(t *testing.T) {
	dev := &hxDevice{}
	dev.load(1111)
	h, _, clk := hxRig(dev)
	if err := h.hookSCKForTest(func(high bool) {
		dev.sck(high)
		if high {
			clk.Sleep(70 * time.Microsecond)
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.sleep = func(d time.Duration) {
		clk.Sleep(d)
		if !dev.ready {
			dev.mu.Lock()
			dev.pulses = 0
			dev.mu.Unlock()
			dev.load(1111)
		}
	}
	if _, err := h.ReadRaw(); !errors.Is(err, hal.ErrBadRead) {
		t.Fatalf("err = %v", err)
	}
}

func TestHX711NoChipDevice(t *testing.T) {
	h := NewHX711(func() (gpioChip, error) { return nil, absent("gpio chip", nil) }, testDOUT, testSCK)
	if _, err := h.ReadRaw(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

// hookSCKForTest opens the lines and replaces the device's SCK hook.
func (h *HX711) hookSCKForTest(sck func(bool)) error {
	if err := h.ensureOpen(); err != nil {
		return err
	}
	h.clock.(*fakeLine).onSet = sck
	return nil
}
