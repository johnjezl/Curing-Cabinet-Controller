package drivers

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// DHT22 timing. The sensor needs at least 2 s between measurements. The kernel
// driver can take a full second to time out on a dead sensor, longer than the
// 500 ms hal bound, so measuring happens on a worker and Read returns its result.
const (
	dhtInterval  = 2500 * time.Millisecond
	dhtReadWait  = 450 * time.Millisecond // Read's wait for the very first result
	dhtStaleness = 10 * time.Second       // a sample older than this is no longer served
)

// dhtSource takes one measurement. It may block for about a second.
type dhtSource interface {
	Measure() (tempC, rhPct float64, err error)
	Close() error
}

// DHT22 implements hal.Climate.
type DHT22 struct {
	src  dhtSource
	now  func() time.Time
	stop chan struct{}
	done chan struct{}

	interval time.Duration
	stale    time.Duration
	wait     time.Duration

	mu        sync.Mutex
	first     chan struct{} // closed after the first attempt finishes
	firstOnce sync.Once
	started   time.Time
	hasSample bool
	t, rh     float64
	sampledAt time.Time
	lastErr   error
	closeOnce sync.Once
}

// NewDHT22 starts measuring from src on a background goroutine.
func NewDHT22(src dhtSource) *DHT22 {
	d := newDHT22(src, time.Now, dhtInterval, dhtStaleness, dhtReadWait)
	go d.loop()
	return d
}

func newDHT22(src dhtSource, now func() time.Time, interval, stale, wait time.Duration) *DHT22 {
	return &DHT22{
		src: src, now: now, interval: interval, stale: stale, wait: wait,
		stop: make(chan struct{}), done: make(chan struct{}),
		first: make(chan struct{}), started: now(),
	}
}

func (d *DHT22) loop() {
	defer close(d.done)
	for {
		d.measure()
		select {
		case <-d.stop:
			return
		case <-time.After(d.interval):
		}
	}
}

func (d *DHT22) measure() {
	t, rh, err := d.src.Measure()
	d.mu.Lock()
	if err == nil {
		d.hasSample, d.t, d.rh, d.sampledAt, d.lastErr = true, t, rh, d.now(), nil
	} else {
		d.lastErr = err
	}
	d.mu.Unlock()
	d.firstOnce.Do(func() { close(d.first) })
}

// Read returns the newest good sample and its age. One failed measurement does
// not hide a good recent sample, since a DHT22 misses reads now and then. Once
// the newest sample is older than the staleness limit, or none exists, the last
// error is returned. The call blocks only until the first attempt has finished,
// and then for at most 450 ms.
func (d *DHT22) Read() (tempC, rhPct, ageSec float64, err error) {
	select {
	case <-d.first:
	case <-time.After(d.wait):
		return 0, 0, 0, absent("dht22: no reading yet", nil)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.hasSample {
		age := d.now().Sub(d.sampledAt)
		if age <= d.stale {
			return d.t, d.rh, age.Seconds(), nil
		}
	}
	if d.lastErr != nil {
		return 0, 0, 0, d.lastErr
	}
	// The worker is stuck inside a kernel read or has not finished another one.
	return 0, 0, 0, absent("dht22: no fresh reading", nil)
}

// Close stops the worker. It does not wait for a measurement stuck in the kernel.
func (d *DHT22) Close() error {
	d.closeOnce.Do(func() { close(d.stop) })
	select {
	case <-d.done:
	case <-time.After(2 * time.Second):
		return fmt.Errorf("dht22: worker still in a kernel read at close")
	}
	return d.src.Close()
}

func validateClimate(t, rh float64) error {
	// AM2302 datasheet ranges: -40 to 80 C, 0 to 100 %RH.
	if math.IsNaN(t) || math.IsNaN(rh) || t < -40 || t > 80 || rh < 0 || rh > 100 {
		return badRead("dht22", fmt.Errorf("out of range: %.1f C, %.1f %%RH", t, rh))
	}
	return nil
}

// dhtKernel reads the kernel dht11 IIO driver, which also handles the DHT22.
type dhtKernel struct{ fs fileSystem }

func newDHTKernel(fs fileSystem) *dhtKernel { return &dhtKernel{fs: fs} }

func (k *dhtKernel) Close() error { return nil }

func (k *dhtKernel) findDevice() (string, error) {
	devs, err := k.fs.Glob("/sys/bus/iio/devices/iio:device*")
	if err != nil {
		return "", err
	}
	for _, d := range devs {
		if name, err := k.fs.ReadFile(d + "/name"); err == nil && strings.HasPrefix(strings.TrimSpace(string(name)), "dht11") {
			return d, nil
		}
	}
	return "", absent("dht22: no dht11 iio device, overlay not loaded", nil)
}

func (k *dhtKernel) readMilli(path string) (float64, error) {
	b, err := k.fs.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, badRead("dht22", err)
	}
	return float64(v) / 1000, nil
}

// classifyDHTKernel maps the dht11 driver's errors. It returns ETIMEDOUT when
// too few edges arrive (no sensor, or a broken line), and EIO when edges arrived
// but do not decode or fail the checksum.
func classifyDHTKernel(err error) error {
	switch {
	case errors.Is(err, hal.ErrBadRead), errors.Is(err, hal.ErrAbsent):
		return err
	case errors.Is(err, syscall.ETIMEDOUT), errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ENODEV):
		return absent("dht22", err)
	}
	return badRead("dht22", err)
}

func (k *dhtKernel) Measure() (float64, float64, error) {
	dev, err := k.findDevice()
	if err != nil {
		return 0, 0, err
	}
	t, err := k.readMilli(dev + "/in_temp_input")
	if err != nil {
		return 0, 0, classifyDHTKernel(err)
	}
	rh, err := k.readMilli(dev + "/in_humidityrelative_input")
	if err != nil {
		return 0, 0, classifyDHTKernel(err)
	}
	if err := validateClimate(t, rh); err != nil {
		return 0, 0, err
	}
	return t, rh, nil
}

// dhtGPIO is the user-space alternative: it drives the start pulse and decodes
// the kernel-timestamped edges of the response. See ADR 0205 for why it is not
// the default.
type dhtGPIO struct {
	open chipOpener
	pin  int
	chip gpioChip
	line gpioLine
}

func newDHTGPIO(open chipOpener, pin int) *dhtGPIO { return &dhtGPIO{open: open, pin: pin} }

func (g *dhtGPIO) Close() error {
	var err error
	if g.line != nil {
		err = g.line.Close()
		g.line = nil
	}
	if g.chip != nil {
		err = errors.Join(err, g.chip.Close())
		g.chip = nil
	}
	return err
}

const (
	dhtStartLow     = 2 * time.Millisecond
	dhtEdgeWindow   = 15 * time.Millisecond // a whole frame takes under 6 ms
	dhtEdgeQuiet    = 2 * time.Millisecond  // silence after the last edge
	dhtEventsBuffer = 256
)

func (g *dhtGPIO) Measure() (float64, float64, error) {
	if g.line == nil {
		chip, err := g.open()
		if err != nil {
			return 0, 0, err
		}
		line, err := chip.Request("curing-dht22", g.pin, flagInput|flagPullUp, dhtEventsBuffer)
		if err != nil {
			chip.Close()
			return 0, 0, err
		}
		g.chip, g.line = chip, line
	}
	if err := g.line.Reconfigure(flagOutput); err != nil {
		return 0, 0, err
	}
	time.Sleep(dhtStartLow)
	if err := g.line.Reconfigure(flagInput | flagPullUp | flagEdgeRising | flagEdgeFalling); err != nil {
		return 0, 0, err
	}
	defer g.line.Reconfigure(flagInput | flagPullUp)

	var edges []lineEvent
	deadline := time.Now().Add(dhtEdgeWindow)
	for {
		wait := time.Until(deadline)
		if len(edges) > 0 && wait > dhtEdgeQuiet {
			wait = dhtEdgeQuiet
		}
		if wait <= 0 {
			break
		}
		ev, err := g.line.ReadEvent(wait)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			break
		}
		if err != nil {
			return 0, 0, err
		}
		edges = append(edges, ev)
	}
	if len(edges) == 0 {
		// No response. A low line means a short or a sensor stuck in reset, not
		// an empty connector.
		if high, err := g.line.Get(); err == nil && !high {
			return 0, 0, badRead("dht22", errors.New("data line stuck low"))
		}
		return 0, 0, absent("dht22: no response", nil)
	}
	return decodeDHTEdges(edges)
}

// DHT22 bit timing from the AM2302 datasheet: each bit is about 50 us low, then
// high for 26 to 28 us (0) or 70 us (1). The limits allow the sensor's spread
// and interrupt-timestamp jitter.
const (
	dhtBitHighMin   = 8 * time.Microsecond
	dhtBitThreshold = 48 * time.Microsecond
	dhtBitHighMax   = 110 * time.Microsecond
)

// decodeDHTEdges turns the edge list into a reading. It uses the last 40 complete
// high pulses, so a missed preamble edge from arming late does no harm.
func decodeDHTEdges(edges []lineEvent) (float64, float64, error) {
	var highs []time.Duration
	for i := 0; i+1 < len(edges); i++ {
		if edges[i].Rising && !edges[i+1].Rising {
			if edges[i+1].TimestampNS < edges[i].TimestampNS {
				return 0, 0, badRead("dht22", errors.New("edge timestamps go backwards"))
			}
			highs = append(highs, time.Duration(edges[i+1].TimestampNS-edges[i].TimestampNS))
		}
	}
	if len(highs) < 40 {
		return 0, 0, badRead("dht22", fmt.Errorf("only %d of 40 bits", len(highs)))
	}
	highs = highs[len(highs)-40:]
	var frame [5]byte
	for i, h := range highs {
		if h < dhtBitHighMin || h > dhtBitHighMax {
			return 0, 0, badRead("dht22", fmt.Errorf("bit %d high for %v", i, h))
		}
		if h > dhtBitThreshold {
			frame[i/8] |= 0x80 >> (i % 8)
		}
	}
	return decodeDHTFrame(frame)
}

// decodeDHTFrame parses the 5-byte frame: RH x10, temperature x10 with a sign
// bit (not two's complement), then the 8-bit sum.
func decodeDHTFrame(f [5]byte) (float64, float64, error) {
	if byte(f[0]+f[1]+f[2]+f[3]) != f[4] {
		return 0, 0, badRead("dht22", fmt.Errorf("checksum: frame % x", f[:]))
	}
	if f == [5]byte{} {
		return 0, 0, badRead("dht22", errors.New("all-zero frame"))
	}
	rh := float64(uint16(f[0])<<8|uint16(f[1])) / 10
	raw := uint16(f[2]&0x7F)<<8 | uint16(f[3])
	t := float64(raw) / 10
	if f[2]&0x80 != 0 {
		t = -t
	}
	if err := validateClimate(t, rh); err != nil {
		return 0, 0, err
	}
	return t, rh, nil
}
