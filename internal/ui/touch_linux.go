package ui

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// DefaultTouchName is the stable part of the panel's touch controller name. The
// kernel name ends in the chip's id, "(00)" on one boot and "(79)" on another, so
// the match is on this part only.
const DefaultTouchName = "generic ft5x06"

const (
	eviocGrab  = 0x40044590
	inputEvent = 24 // sizeof(struct input_event) on 64-bit
)

func eviocgname(n uintptr) uintptr   { return 0x80004506 | n<<16 }
func eviocgabs(code uintptr) uintptr { return 0x80184540 + code }

// deviceName asks an event device for its name.
func deviceName(f *os.File) (string, error) {
	var buf [128]byte
	if err := ioctl(f.Fd(), eviocgname(uintptr(len(buf))), unsafe.Pointer(&buf)); err != nil {
		return "", err
	}
	return strings.TrimRight(string(buf[:]), "\x00"), nil
}

// FindTouch opens the event device whose name contains name. Numbers can move.
func FindTouch(name string) (*os.File, error) {
	paths, _ := filepath.Glob("/dev/input/event*")
	sort.Strings(paths)
	var firstErr error
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if n, err := deviceName(f); err == nil && strings.Contains(n, name) {
			return f, nil
		}
		f.Close()
	}
	if firstErr != nil {
		return nil, fmt.Errorf("touch device %q not found (%w)", name, firstErr)
	}
	return nil, fmt.Errorf("touch device %q not found", name)
}

func absRange(f *os.File, code uintptr) (min, max int32, err error) {
	var info [6]int32 // value, minimum, maximum, fuzz, flat, resolution
	if err := ioctl(f.Fd(), eviocgabs(code), unsafe.Pointer(&info)); err != nil {
		return 0, 0, err
	}
	return info[1], info[2], nil
}

// DescribeTouch reports what was found, for a start-up log line.
func DescribeTouch(name string) (string, error) {
	f, err := FindTouch(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	minX, maxX, _ := absRange(f, absMTPositionX)
	minY, maxY, _ := absRange(f, absMTPositionY)
	n, _ := deviceName(f)
	return fmt.Sprintf("%s name=%q x=%d..%d y=%d..%d", f.Name(), n, minX, maxX, minY, maxY), nil
}

// ReadTouch decodes the named device into out until ctx ends, reopening it if it
// goes away. A full channel drops moves but always delivers ups and cancels.
func ReadTouch(ctx context.Context, name string, out chan<- TouchEvent) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := readTouchOnce(ctx, name, out)
		if err != nil && ctx.Err() == nil {
			log.Printf("ui: touch: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}

func readTouchOnce(ctx context.Context, name string, out chan<- TouchEvent) error {
	f, err := FindTouch(name)
	if err != nil {
		return err
	}
	defer f.Close()
	stop := context.AfterFunc(ctx, func() { f.Close() })
	defer stop()
	minX, maxX, err := absRange(f, absMTPositionX)
	if err != nil {
		return fmt.Errorf("EVIOCGABS: %w", err)
	}
	minY, maxY, err := absRange(f, absMTPositionY)
	if err != nil {
		return fmt.Errorf("EVIOCGABS: %w", err)
	}
	// Exclusive: a console or another reader must not also see these touches.
	one := 1
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocGrab, uintptr(one)); e != 0 {
		log.Printf("ui: touch grab: %v", e)
	}
	dec := NewDecoder(minX, maxX, minY, maxY, ScreenW, ScreenH)
	buf := make([]byte, inputEvent*64)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				return nil
			}
			return err
		}
		at := time.Now()
		for off := 0; off+inputEvent <= n; off += inputEvent {
			b := buf[off : off+inputEvent]
			ev := RawEvent{
				Type:  binary.LittleEndian.Uint16(b[16:]),
				Code:  binary.LittleEndian.Uint16(b[18:]),
				Value: int32(binary.LittleEndian.Uint32(b[20:])),
			}
			for _, te := range dec.Feed(ev) {
				te.At = at
				if te.Kind == TouchMove {
					select {
					case out <- te:
					default:
					}
					continue
				}
				select {
				case out <- te:
				case <-ctx.Done():
					return nil
				}
			}
		}
	}
}
