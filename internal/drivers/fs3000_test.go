package drivers

import (
	"errors"
	"math"
	"syscall"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// frame builds a valid FS3000 frame for a raw reading.
func frame(raw int, r1, r2 byte) []byte {
	f := []byte{0, byte(raw >> 8), byte(raw), r1, r2}
	var sum byte
	for _, b := range f[1:] {
		sum += b
	}
	f[0] = -sum
	return f
}

func newFS(b *fakeBus) *FS3000 {
	return NewFS3000(func() (i2cBus, error) { return b, nil })
}

func TestFS3000TransferCurve(t *testing.T) {
	cases := []struct {
		raw  int
		want float64
	}{
		{0, 0}, {408, 0}, {409, 0},
		{915, 1.07}, {1522, 2.01}, {2066, 3.00}, {2523, 3.97},
		{2908, 4.96}, {3256, 5.98}, {3572, 6.99}, {3686, 7.23},
		{3687, 7.23}, {4095, 7.23},
		{662, 0.535}, // halfway between the first two table points
		{3629, 7.11}, // halfway between the last two
	}
	for _, c := range cases {
		if got := fs3000Velocity(c.raw); math.Abs(got-c.want) > 0.001 {
			t.Errorf("raw %d: got %.4f want %.4f", c.raw, got, c.want)
		}
	}
	prev := -1.0
	for raw := 0; raw <= 4095; raw++ {
		v := fs3000Velocity(raw)
		if v < prev {
			t.Fatalf("curve not monotonic at raw %d", raw)
		}
		prev = v
	}
}

func TestFS3000ReadsFiveBytesFromAddress28(t *testing.T) {
	b := &fakeBus{frame: frame(1522, 0xAB, 0xCD)}
	v, err := newFS(b).Read()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(v-2.01) > 0.001 {
		t.Errorf("velocity %.3f", v)
	}
	if b.addrs[0] != 0x28 || b.sizes[0] != 5 {
		t.Errorf("addr %#x size %d", b.addrs[0], b.sizes[0])
	}
}

func TestFS3000MasksReservedHighNibble(t *testing.T) {
	f := frame(915, 0, 0)
	f[1] |= 0xF0
	f[0] -= 0xF0
	b := &fakeBus{frame: f}
	v, err := newFS(b).Read()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(v-1.07) > 0.001 {
		t.Errorf("velocity %.3f", v)
	}
}

func TestFS3000BadChecksum(t *testing.T) {
	for i := 0; i < 5; i++ {
		f := frame(2066, 1, 2)
		f[i] ^= 0x10
		_, err := newFS(&fakeBus{frame: f}).Read()
		if !errors.Is(err, hal.ErrBadRead) {
			t.Errorf("byte %d flipped: err = %v", i, err)
		}
	}
}

func TestFS3000RejectsBusRailFrames(t *testing.T) {
	// All zeros passes the sum, so it needs its own rule. All ones fails the sum.
	for _, f := range [][]byte{{0, 0, 0, 0, 0}, {0xFF, 0xFF, 0xFF, 0xFF, 0xFF}} {
		if _, err := newFS(&fakeBus{frame: f}).Read(); !errors.Is(err, hal.ErrBadRead) {
			t.Errorf("frame % x: err = %v", f, err)
		}
	}
}

func TestFS3000Unplugged(t *testing.T) {
	for _, e := range []error{syscall.EREMOTEIO, syscall.ENXIO, syscall.ETIMEDOUT, syscall.EIO} {
		if _, err := newFS(&fakeBus{err: e}).Read(); !errors.Is(err, hal.ErrAbsent) {
			t.Errorf("%v: err = %v", e, err)
		}
	}
}

func TestFS3000NoBusNode(t *testing.T) {
	s := NewFS3000(func() (i2cBus, error) { return nil, syscall.ENOENT })
	if _, err := s.Read(); !errors.Is(err, hal.ErrAbsent) {
		t.Errorf("err = %v", err)
	}
}

func TestFS3000HungBusReturnsWithinBound(t *testing.T) {
	release := make(chan struct{})
	b := &fakeBus{frame: frame(915, 0, 0), block: release}
	s := newFS(b)
	start := time.Now()
	_, err := s.Read()
	if !errors.Is(err, hal.ErrAbsent) {
		t.Errorf("err = %v", err)
	}
	if el := time.Since(start); el > 100*time.Millisecond {
		t.Errorf("took %v, bound is 100 ms", el)
	}
	// A second call must not stack up behind the stuck one.
	start = time.Now()
	if _, err := s.Read(); !errors.Is(err, hal.ErrAbsent) {
		t.Errorf("second read err = %v", err)
	}
	if el := time.Since(start); el > 10*time.Millisecond {
		t.Errorf("second read took %v", el)
	}
	if err := s.Close(); err == nil {
		t.Error("close during a stuck call should report it")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		v, err := s.Read()
		if err == nil {
			if math.Abs(v-1.07) > 0.001 {
				t.Errorf("recovered value %v", v)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no recovery: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}
