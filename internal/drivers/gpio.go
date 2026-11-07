package drivers

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Line flags, the kernel's gpio_v2_line_flag values.
const (
	flagInput       uint64 = 1 << 2
	flagOutput      uint64 = 1 << 3
	flagEdgeRising  uint64 = 1 << 4
	flagEdgeFalling uint64 = 1 << 5
	flagPullUp      uint64 = 1 << 8
	flagPullDown    uint64 = 1 << 9
)

// Edge values in lineEvent.Rising come from the kernel's event id.
type lineEvent struct {
	TimestampNS uint64 // CLOCK_MONOTONIC, taken in the kernel's interrupt handler
	Rising      bool
}

// gpioChip opens lines. Production: the character device. Tests: a fake.
type gpioChip interface {
	// Request claims one line. A new output is driven low.
	Request(consumer string, offset int, flags uint64, eventBuffer int) (gpioLine, error)
	Close() error
}

// gpioLine is one claimed line.
type gpioLine interface {
	Set(high bool) error
	Get() (high bool, err error)
	// Reconfigure changes flags without releasing the line. A line changed to
	// output is driven low.
	Reconfigure(flags uint64) error
	// ReadEvent returns the next edge or os.ErrDeadlineExceeded after timeout.
	ReadEvent(timeout time.Duration) (lineEvent, error)
	Close() error
}

// chipOpener opens the chip on demand, so a chip that appears later is picked
// up and a missing one reports hal.ErrAbsent at use rather than at startup.
type chipOpener func() (gpioChip, error)

// Kernel uapi, linux/gpio.h.
const (
	ioctlChipInfo  = 0x8044B401
	ioctlGetLine   = 0xC250B407
	ioctlSetConfig = 0xC110B40D
	ioctlGetValues = 0xC010B40E
	ioctlSetValues = 0xC010B40F
	eventIDRising  = 1
	lineEventSize  = 48
	maxEventBuffer = 1024
)

type gpioChipInfo struct {
	Name, Label [32]byte
	Lines       uint32
}

type gpioV2ConfigAttr struct {
	ID, Padding uint32
	Value, Mask uint64
}

type gpioV2LineConfig struct {
	Flags    uint64
	NumAttrs uint32
	Padding  [5]uint32
	Attrs    [10]gpioV2ConfigAttr
}

type gpioV2LineRequest struct {
	Offsets         [64]uint32
	Consumer        [32]byte
	Config          gpioV2LineConfig
	NumLines        uint32
	EventBufferSize uint32
	Padding         [5]uint32
	Fd              int32
}

type gpioV2LineValues struct {
	Bits, Mask uint64
}

// Compile-time size checks against the kernel's struct sizes.
var (
	_ [68 - unsafe.Sizeof(gpioChipInfo{})]byte
	_ [unsafe.Sizeof(gpioChipInfo{}) - 68]byte
	_ [272 - unsafe.Sizeof(gpioV2LineConfig{})]byte
	_ [unsafe.Sizeof(gpioV2LineConfig{}) - 272]byte
	_ [592 - unsafe.Sizeof(gpioV2LineRequest{})]byte
	_ [unsafe.Sizeof(gpioV2LineRequest{}) - 592]byte
)

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
		if e == syscall.EINTR {
			continue
		}
		if e != 0 {
			return e
		}
		return nil
	}
}

type cdevChip struct{ f *os.File }

// openChipByLabel finds the GPIO chip with the given label. The RP1 is
// gpiochip0 on this kernel but Raspberry Pi has renumbered it before.
func openChipByLabel(label string) (gpioChip, error) {
	paths, _ := filepath.Glob("/dev/gpiochip*")
	seen := map[string]bool{}
	for _, p := range paths {
		real, err := filepath.EvalSymlinks(p)
		if err != nil || seen[real] {
			continue
		}
		seen[real] = true
		f, err := os.OpenFile(real, os.O_RDWR|syscall.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		var info gpioChipInfo
		if err := ioctl(f.Fd(), ioctlChipInfo, unsafe.Pointer(&info)); err == nil && cstr(info.Label[:]) == label {
			return &cdevChip{f: f}, nil
		}
		f.Close()
	}
	return nil, absent("gpio chip "+label, nil)
}

func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func (c *cdevChip) Close() error { return c.f.Close() }

func (c *cdevChip) Request(consumer string, offset int, flags uint64, eventBuffer int) (gpioLine, error) {
	if eventBuffer > maxEventBuffer {
		eventBuffer = maxEventBuffer
	}
	var r gpioV2LineRequest
	r.Offsets[0] = uint32(offset)
	copy(r.Consumer[:31], consumer)
	r.NumLines = 1
	r.Config.Flags = flags
	r.EventBufferSize = uint32(eventBuffer)
	if err := ioctl(c.f.Fd(), ioctlGetLine, unsafe.Pointer(&r)); err != nil {
		return nil, fmt.Errorf("request GPIO %d: %w", offset, err)
	}
	// The poller gives ReadEvent a deadline that also unblocks on Close.
	if err := syscall.SetNonblock(int(r.Fd), true); err != nil {
		syscall.Close(int(r.Fd))
		return nil, err
	}
	return &cdevLine{f: os.NewFile(uintptr(r.Fd), fmt.Sprintf("gpio%d", offset))}, nil
}

type cdevLine struct{ f *os.File }

func (l *cdevLine) rawFd() (uintptr, error) {
	sc, err := l.f.SyscallConn()
	if err != nil {
		return 0, err
	}
	var fd uintptr
	if err := sc.Control(func(f uintptr) { fd = f }); err != nil {
		return 0, err
	}
	return fd, nil
}

func (l *cdevLine) Set(high bool) error {
	fd, err := l.rawFd()
	if err != nil {
		return err
	}
	v := gpioV2LineValues{Mask: 1}
	if high {
		v.Bits = 1
	}
	return ioctl(fd, ioctlSetValues, unsafe.Pointer(&v))
}

func (l *cdevLine) Get() (bool, error) {
	fd, err := l.rawFd()
	if err != nil {
		return false, err
	}
	v := gpioV2LineValues{Mask: 1}
	if err := ioctl(fd, ioctlGetValues, unsafe.Pointer(&v)); err != nil {
		return false, err
	}
	return v.Bits&1 != 0, nil
}

func (l *cdevLine) Reconfigure(flags uint64) error {
	fd, err := l.rawFd()
	if err != nil {
		return err
	}
	var c gpioV2LineConfig
	c.Flags = flags
	return ioctl(fd, ioctlSetConfig, unsafe.Pointer(&c))
}

func (l *cdevLine) ReadEvent(timeout time.Duration) (lineEvent, error) {
	if err := l.f.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return lineEvent{}, err
	}
	var b [lineEventSize]byte
	if _, err := io.ReadFull(l.f, b[:]); err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return lineEvent{}, os.ErrDeadlineExceeded
		}
		return lineEvent{}, err
	}
	ev := (*struct {
		TS uint64
		ID uint32
	})(unsafe.Pointer(&b[0]))
	return lineEvent{TimestampNS: ev.TS, Rising: ev.ID == eventIDRising}, nil
}

func (l *cdevLine) Close() error { return l.f.Close() }
