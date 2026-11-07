package drivers

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// HX711 facts from the Avia datasheet. DOUT low means a conversion is ready.
// Each rising SCK edge shifts out one bit, MSB first; 24 pulses read the data
// and a 25th selects channel A at gain 128 for the next conversion. SCK high for
// more than 60 us puts the chip into power-down. Conversions arrive at 10 SPS or
// 80 SPS depending on the board's RATE pin.
const (
	hx711DataPulses  = 24
	hx711GainPulses  = 1                      // channel A, gain 128
	hx711Bound       = 190 * time.Millisecond // hal bound is 200 ms
	hx711PollEvery   = 2 * time.Millisecond
	hx711MaxClockHi  = 50 * time.Microsecond // margin under the 60 us power-down limit
	hx711SaturatedHi = 0x7FFFFF
	hx711SaturatedLo = -0x800000
)

// HX711 implements hal.Scale by bit-banging over the GPIO character device.
type HX711 struct {
	open     chipOpener
	dataPin  int
	clockPin int
	now      func() time.Time
	sleep    func(time.Duration)
	limit    time.Duration

	mu    sync.Mutex
	chip  gpioChip
	data  gpioLine
	clock gpioLine
}

// NewHX711 returns a driver that claims its pins on first use.
func NewHX711(open chipOpener, dataPin, clockPin int) *HX711 {
	return &HX711{open: open, dataPin: dataPin, clockPin: clockPin, now: time.Now, sleep: time.Sleep, limit: hx711Bound}
}

func (h *HX711) ensureOpen() error {
	if h.chip != nil {
		return nil
	}
	chip, err := h.open()
	if err != nil {
		return err
	}
	// Pull-up on DOUT: an empty connector reads "not ready", never a stream of
	// zeros that would look like a valid conversion.
	data, err := chip.Request("curing-hx711-dout", h.dataPin, flagInput|flagPullUp, 0)
	if err != nil {
		chip.Close()
		return err
	}
	clock, err := chip.Request("curing-hx711-sck", h.clockPin, flagOutput|flagPullDown, 0)
	if err != nil {
		data.Close()
		chip.Close()
		return err
	}
	h.chip, h.data, h.clock = chip, data, clock
	return nil
}

// ReadRaw waits for a conversion and clocks it out, returning within about
// 190 ms. A chip that never signals ready is hal.ErrAbsent. A frame disturbed
// by scheduling (SCK held high too long) is retried while time remains.
func (h *HX711) ReadRaw() (int32, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	deadline := h.now().Add(h.limit)
	if err := h.ensureOpen(); err != nil {
		return 0, err
	}
	var lastBad error
	for {
		high, err := h.data.Get()
		if err != nil {
			return 0, err
		}
		if !high {
			v, err := h.clockOut()
			if err == nil {
				return v, nil
			}
			if !errors.Is(err, errHX711Retry) {
				return 0, err
			}
			lastBad = err
		}
		if h.now().After(deadline) {
			if lastBad != nil {
				return 0, badRead("hx711", lastBad)
			}
			return 0, absent("hx711: not ready within bound", nil)
		}
		h.sleep(hx711PollEvery)
	}
}

var errHX711Retry = errors.New("clock held high too long")

// clockOut reads one frame. It keeps the goroutine on its OS thread to reduce
// the chance that the 60 us SCK-high limit is broken, and measures the high time
// to detect when it was anyway.
func (h *HX711) clockOut() (int32, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var raw uint32
	for i := 0; i < hx711DataPulses+hx711GainPulses; i++ {
		start := h.now()
		if err := h.clock.Set(true); err != nil {
			return 0, err
		}
		if err := h.clock.Set(false); err != nil {
			return 0, err
		}
		if h.now().Sub(start) > hx711MaxClockHi {
			// The chip may or may not have powered down. Finish the frame so a
			// chip that did not stays in step; the next read starts only when
			// DOUT shows a fresh conversion.
			for j := i + 1; j < hx711DataPulses+hx711GainPulses; j++ {
				h.clock.Set(true)
				h.clock.Set(false)
			}
			return 0, fmt.Errorf("%w: pulse %d", errHX711Retry, i)
		}
		bit, err := h.data.Get()
		if err != nil {
			return 0, err
		}
		if i < hx711DataPulses {
			raw <<= 1
			if bit {
				raw |= 1
			}
		} else if !bit {
			// The 25th pulse must force DOUT high.
			return 0, badRead("hx711", errors.New("DOUT low after final pulse: stuck line or wrong clock count"))
		}
	}
	v := int32(raw<<8) >> 8
	if v == hx711SaturatedHi || v == hx711SaturatedLo {
		return 0, badRead("hx711", fmt.Errorf("saturated at %d: bridge wiring open or shorted", v))
	}
	return v, nil
}

// Close leaves SCK low and frees the lines.
func (h *HX711) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.chip == nil {
		return nil
	}
	err := errors.Join(h.clock.Set(false), h.clock.Close(), h.data.Close(), h.chip.Close())
	h.chip, h.data, h.clock = nil, nil, nil
	return err
}

// MeasureHX711Clock claims only the clock pin of a HX711 and reports how long
// each high pulse took through the kernel GPIO path. It is a rig diagnostic for
// the 60 us power-down limit and works with nothing attached.
func MeasureHX711Clock(cfg Config, pulses int) ([]time.Duration, error) {
	chip, err := openChipByLabel(cfg.GPIOChipLabel)
	if err != nil {
		return nil, err
	}
	defer chip.Close()
	clock, err := chip.Request("curing-hx711-sck-test", cfg.HX711ClockPin, flagOutput|flagPullDown, 0)
	if err != nil {
		return nil, err
	}
	defer clock.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	out := make([]time.Duration, 0, pulses)
	for i := 0; i < pulses; i++ {
		start := time.Now()
		if err := clock.Set(true); err != nil {
			return out, err
		}
		if err := clock.Set(false); err != nil {
			return out, err
		}
		out = append(out, time.Since(start))
		if i%25 == 24 {
			time.Sleep(12 * time.Millisecond) // the rhythm of real frames at 80 SPS
		}
	}
	return out, nil
}
