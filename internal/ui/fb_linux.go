package ui

import (
	"encoding/binary"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// DefaultFramebuffer is the DSI panel's fbdev node.
const DefaultFramebuffer = "/dev/fb0"

const (
	fbioGetVScreenInfo = 0x4600
	fbioGetFScreenInfo = 0x4602
)

// Framebuffer writes frames to an fbdev device through a shadow copy, so only
// changed rows reach the panel. It repaints in full every few seconds so a
// stray write by anything else cannot persist.
type Framebuffer struct {
	f                *os.File
	mem              []byte
	shadow           []byte
	row              []byte
	w, h, stride     int
	rOff, gOff, bOff int
	aOff             int // -1 when the format has no alpha (XRGB)
	lastFull         time.Time
	forceFull        atomic.Bool
	Name             string
}

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if e != 0 {
		return e
	}
	return nil
}

// OpenFramebuffer maps the device and validates its pixel format.
func OpenFramebuffer(path string) (*Framebuffer, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	var vs [160]byte
	if err := ioctl(f.Fd(), fbioGetVScreenInfo, unsafe.Pointer(&vs)); err != nil {
		f.Close()
		return nil, fmt.Errorf("FBIOGET_VSCREENINFO: %w", err)
	}
	var fs [80]byte
	if err := ioctl(f.Fd(), fbioGetFScreenInfo, unsafe.Pointer(&fs)); err != nil {
		f.Close()
		return nil, fmt.Errorf("FBIOGET_FSCREENINFO: %w", err)
	}
	le := binary.LittleEndian
	u32 := func(b []byte, off int) int { return int(le.Uint32(b[off:])) }
	w, h, bpp := u32(vs[:], 0), u32(vs[:], 4), u32(vs[:], 24)
	if bpp != 32 {
		f.Close()
		return nil, fmt.Errorf("%s is %d bpp; only 32 is supported", path, bpp)
	}
	stride := u32(fs[:], 48)
	smem := u32(fs[:], 24)
	size := stride * h
	if smem > 0 && smem < size {
		size = smem
	}
	mem, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("mmap: %w", err)
	}
	aOff := -1 // XRGB reports transp offset 0, length 0: that byte is not alpha
	if u32(vs[:], 72) > 0 {
		aOff = u32(vs[:], 68) / 8
	}
	return &Framebuffer{
		f: f, mem: mem, shadow: make([]byte, size), row: make([]byte, stride),
		w: w, h: h, stride: stride,
		rOff: u32(vs[:], 32) / 8, gOff: u32(vs[:], 44) / 8, bOff: u32(vs[:], 56) / 8, aOff: aOff,
		Name: strings.TrimRight(string(fs[:16]), "\x00"),
	}, nil
}

// Geometry reports the mapped size and pixel layout, for logs.
func (fb *Framebuffer) Geometry() string {
	return fmt.Sprintf("name=%q %dx%d stride=%d r=%d g=%d b=%d a=%d", fb.Name, fb.w, fb.h, fb.stride, fb.rOff, fb.gOff, fb.bOff, fb.aOff)
}

// Present copies the changed rows to the device.
func (fb *Framebuffer) Present(img *image.RGBA) error {
	force := time.Since(fb.lastFull) > 5*time.Second || fb.forceFull.Swap(false)
	if force {
		fb.lastFull = time.Now()
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w > fb.w {
		w = fb.w
	}
	if h > fb.h {
		h = fb.h
	}
	for y := 0; y < h; y++ {
		src := img.Pix[y*img.Stride : y*img.Stride+w*4]
		dst := fb.row
		for x := 0; x < w; x++ {
			s := src[x*4 : x*4+3 : x*4+3]
			d := dst[x*4 : x*4+4 : x*4+4]
			d[fb.rOff], d[fb.gOff], d[fb.bOff] = s[0], s[1], s[2]
			if fb.aOff >= 0 {
				d[fb.aOff] = 255
			}
		}
		off := y * fb.stride
		sh := fb.shadow[off : off+w*4]
		if force || string(sh) != string(dst[:w*4]) {
			copy(sh, dst[:w*4])
			copy(fb.mem[off:off+w*4], dst[:w*4])
		}
	}
	return nil
}

// PaintNotice writes a whole frame directly to the device, bypassing the shadow,
// from whatever goroutine notices trouble. The next Present repaints in full.
func (fb *Framebuffer) PaintNotice(img *image.RGBA) error {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w > fb.w {
		w = fb.w
	}
	if h > fb.h {
		h = fb.h
	}
	for y := 0; y < h; y++ {
		src := img.Pix[y*img.Stride : y*img.Stride+w*4]
		dst := fb.mem[y*fb.stride : y*fb.stride+w*4]
		for x := 0; x < w; x++ {
			s := src[x*4 : x*4+3 : x*4+3]
			d := dst[x*4 : x*4+4 : x*4+4]
			d[fb.rOff], d[fb.gOff], d[fb.bOff] = s[0], s[1], s[2]
			if fb.aOff >= 0 {
				d[fb.aOff] = 255
			}
		}
	}
	fb.forceFull.Store(true)
	return nil
}

// PaintStopped opens the framebuffer and paints the stopped notice. A service
// manager can run it after the process has died, however it died.
func PaintStopped(path string) error {
	fb, err := OpenFramebuffer(path)
	if err != nil {
		return err
	}
	defer fb.Close()
	return fb.PaintNotice(StoppedFrame())
}

// Close unmaps the device.
func (fb *Framebuffer) Close() error {
	syscall.Munmap(fb.mem)
	return fb.f.Close()
}

// Backlight drives /sys/class/backlight when the panel has one. Many DSI panels
// do not; then dimming is pixel-level only. Nil-safe.
type Backlight struct {
	path string
	max  int
}

// OpenBacklight returns the first writable backlight, or nil.
func OpenBacklight() *Backlight {
	dirs, _ := filepath.Glob("/sys/class/backlight/*")
	for _, d := range dirs {
		b, err := os.ReadFile(filepath.Join(d, "max_brightness"))
		if err != nil {
			continue
		}
		m, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil || m <= 0 {
			continue
		}
		f, err := os.OpenFile(filepath.Join(d, "brightness"), os.O_WRONLY, 0)
		if err != nil {
			continue
		}
		f.Close()
		return &Backlight{path: filepath.Join(d, "brightness"), max: m}
	}
	return nil
}

// Set applies the power stage. Best effort: failure leaves pixel dimming.
func (b *Backlight) Set(pw Power) { b.set(pw) }

// Level reads the brightness back from the driver, for logs. -1 if unavailable.
func (b *Backlight) Level() int {
	if b == nil {
		return -1
	}
	v, err := os.ReadFile(b.path)
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(v)))
	if err != nil {
		return -1
	}
	return n
}

// Describe names the device, for a start-up log line.
func (b *Backlight) Describe() string {
	if b == nil {
		return "none"
	}
	return fmt.Sprintf("%s max=%d", b.path, b.max)
}

func (b *Backlight) set(pw Power) {
	if b == nil {
		return
	}
	level := b.max
	switch pw {
	case PowerDim:
		level = b.max * 40 / 100
	case PowerSleep:
		level = b.max * 15 / 100
	}
	os.WriteFile(b.path, []byte(strconv.Itoa(level)), 0)
}
