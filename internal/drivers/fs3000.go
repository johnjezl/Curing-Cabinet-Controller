package drivers

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
)

// FS3000-1005 facts from the Renesas datasheet and SparkFun's library:
// address 0x28, 5-byte read frame [checksum, data high, data low, reserved,
// reserved], 12-bit reading in the low nibble of byte 1 and all of byte 2, and
// bytes summing to 0 modulo 256.
const (
	fs3000Addr      = 0x28
	fs3000FrameSize = 5
	fs3000Bound     = 90 * time.Millisecond // hal bound is 100 ms
)

// FS3000-1005 transfer curve, datasheet table, linearly interpolated.
var (
	fs3000Raw = [...]int{409, 915, 1522, 2066, 2523, 2908, 3256, 3572, 3686}
	fs3000MPS = [...]float64{0, 1.07, 2.01, 3.00, 3.97, 4.96, 5.98, 6.99, 7.23}
)

// fs3000Velocity converts a raw 12-bit reading to m/s, clamped to the sensor's
// 0 to 7.23 m/s range as the datasheet does at both ends.
func fs3000Velocity(raw int) float64 {
	if raw <= fs3000Raw[0] {
		return 0
	}
	last := len(fs3000Raw) - 1
	if raw >= fs3000Raw[last] {
		return fs3000MPS[last]
	}
	for i := 1; i <= last; i++ {
		if raw <= fs3000Raw[i] {
			f := float64(raw-fs3000Raw[i-1]) / float64(fs3000Raw[i]-fs3000Raw[i-1])
			return fs3000MPS[i-1] + f*(fs3000MPS[i]-fs3000MPS[i-1])
		}
	}
	return fs3000MPS[last]
}

// decodeFS3000 validates a frame and returns the raw 12-bit reading.
func decodeFS3000(f [fs3000FrameSize]byte) (int, error) {
	var sum byte
	for _, b := range f {
		sum += b
	}
	if sum != 0 {
		return 0, badRead("fs3000", fmt.Errorf("checksum: frame % x", f[:]))
	}
	// An all-zero frame passes the sum but cannot be a measurement: a powered
	// sensor reads about 409 at still air. It indicates a held-low bus.
	if f == [fs3000FrameSize]byte{} {
		return 0, badRead("fs3000", errors.New("all-zero frame"))
	}
	return int(f[1]&0x0F)<<8 | int(f[2]), nil
}

// FS3000 implements hal.AirVelocity.
type FS3000 struct {
	open  func() (i2cBus, error)
	gate  *bounded
	limit time.Duration

	mu  sync.Mutex // guards bus; held by the call goroutine
	bus i2cBus
}

// NewFS3000 returns a driver that opens the bus on first use and after errors.
func NewFS3000(open func() (i2cBus, error)) *FS3000 {
	return &FS3000{open: open, gate: newBounded(), limit: fs3000Bound}
}

// Read does one bus transaction, in at most about 90 ms however the bus behaves.
func (s *FS3000) Read() (float64, error) {
	var frame [fs3000FrameSize]byte
	err := s.gate.run(s.limit, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.bus == nil {
			bus, err := s.open()
			if err != nil {
				return err
			}
			s.bus = bus
		}
		var buf [fs3000FrameSize]byte
		if err := s.bus.ReadFrom(fs3000Addr, buf[:]); err != nil {
			return err
		}
		frame = buf
		return nil
	})
	if err != nil {
		return 0, classifyFS3000(err)
	}
	// err == nil means the closure finished, so frame is safe to read here. After
	// errHung it may still be running and frame is left alone.
	raw, err := decodeFS3000(frame)
	if err != nil {
		return 0, err
	}
	return fs3000Velocity(raw), nil
}

func classifyFS3000(err error) error {
	switch {
	case errors.Is(err, errHung):
		return absent("fs3000: bus hung", err)
	case errors.Is(err, syscall.ENXIO), // address NACK on some adapters
		errors.Is(err, syscall.EREMOTEIO), // address NACK on i2c-designware
		errors.Is(err, syscall.ENOENT),    // no /dev/i2c-N
		errors.Is(err, syscall.ENODEV),
		errors.Is(err, syscall.ETIMEDOUT), // bus stuck or stretching forever
		errors.Is(err, syscall.EIO):
		return absent("fs3000", err)
	}
	return fmt.Errorf("fs3000: %w", err)
}

// Close releases the bus, unless a hung call still holds it.
func (s *FS3000) Close() error {
	if !s.mu.TryLock() {
		return fmt.Errorf("fs3000: bus call still stuck at close")
	}
	defer s.mu.Unlock()
	if s.bus == nil {
		return nil
	}
	err := s.bus.Close()
	s.bus = nil
	return err
}
