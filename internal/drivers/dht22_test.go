package drivers

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// --- pulse trains for the GPIO backend ---

// dhtFrame builds the 5-byte frame for rh (x10) and temperature (x10).
func dhtFrame(rh10, t10 int) [5]byte {
	var f [5]byte
	f[0], f[1] = byte(rh10>>8), byte(rh10)
	abs := t10
	if abs < 0 {
		abs = -abs
	}
	f[2], f[3] = byte(abs>>8), byte(abs)
	if t10 < 0 {
		f[2] |= 0x80
	}
	f[4] = f[0] + f[1] + f[2] + f[3]
	return f
}

// dhtTrain renders a frame the way the sensor drives it: after release an 80 us
// low and 80 us high response, then for each bit 50 us low and a high whose
// length carries the value.
func dhtTrain(f [5]byte, zero, one time.Duration) []lineEvent {
	ts := uint64(1_000_000)
	var ev []lineEvent
	add := func(rising bool, after time.Duration) {
		ts += uint64(after)
		ev = append(ev, lineEvent{TimestampNS: ts, Rising: rising})
	}
	add(false, 30*time.Microsecond) // sensor pulls low
	add(true, 80*time.Microsecond)
	add(false, 80*time.Microsecond) // preamble high
	for i := 0; i < 40; i++ {
		add(true, 50*time.Microsecond)
		h := zero
		if f[i/8]&(0x80>>(i%8)) != 0 {
			h = one
		}
		add(false, h)
	}
	add(true, 50*time.Microsecond) // release at the end
	return ev
}

const (
	us27 = 27 * time.Microsecond
	us70 = 70 * time.Microsecond
)

func TestDHTDecodesPulseTrains(t *testing.T) {
	cases := []struct{ rh10, t10 int }{
		{652, 351}, {0, 1}, {1000, 800}, {555, -101}, {400, -400}, {751, 0},
	}
	for _, c := range cases {
		temp, rh, err := decodeDHTEdges(dhtTrain(dhtFrame(c.rh10, c.t10), us27, us70))
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if int(rh*10+0.5) != c.rh10 || int(abs(temp)*10+0.5) != abs10(c.t10) || (c.t10 < 0) != (temp < 0) {
			t.Errorf("%+v: got %.1f C %.1f %%", c, temp, rh)
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func abs10(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestDHTToleratesTimingSpreadAndLateArming(t *testing.T) {
	f := dhtFrame(601, 125)
	for _, tm := range []struct{ zero, one time.Duration }{
		{20 * time.Microsecond, 60 * time.Microsecond},
		{35 * time.Microsecond, 80 * time.Microsecond},
	} {
		if _, _, err := decodeDHTEdges(dhtTrain(f, tm.zero, tm.one)); err != nil {
			t.Errorf("%+v: %v", tm, err)
		}
	}
	// Arming edge detection late loses the response preamble, not data.
	train := dhtTrain(f, us27, us70)
	temp, rh, err := decodeDHTEdges(train[3:])
	if err != nil || rh != 60.1 || temp != 12.5 {
		t.Errorf("late arming: %v %v %v", temp, rh, err)
	}
}

func TestDHTBadPulseTrains(t *testing.T) {
	good := dhtTrain(dhtFrame(500, 200), us27, us70)
	badSum := dhtFrame(500, 200)
	badSum[4] ^= 1
	longPulse := append([]lineEvent(nil), good...)
	longPulse[20].TimestampNS += uint64(300 * time.Microsecond)
	for i := 21; i < len(longPulse); i++ {
		longPulse[i].TimestampNS += uint64(300 * time.Microsecond)
	}
	backwards := append([]lineEvent(nil), good...)
	for i := range backwards {
		backwards[i].TimestampNS = uint64(10_000_000 - i*1000)
	}
	cases := map[string][]lineEvent{
		"bad checksum":            dhtTrain(badSum, us27, us70),
		"truncated frame":         good[:40],
		"lost edge":               append(append([]lineEvent(nil), good[:30]...), good[31:]...),
		"over-long high":          longPulse,
		"glitch-short high":       dhtTrain(dhtFrame(500, 200), 2*time.Microsecond, us70),
		"backwards clock":         backwards,
		"one edge":                good[:1],
		"zero frame":              dhtTrain([5]byte{}, us27, us70),
		"humidity above 100":      dhtTrain(dhtFrame(1100, 200), us27, us70),
		"temperature above range": dhtTrain(dhtFrame(500, 900), us27, us70),
	}
	for name, train := range cases {
		if _, _, err := decodeDHTEdges(train); !errors.Is(err, hal.ErrBadRead) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// A driving device on the fake line: a full exchange through dhtGPIO.Measure.
func dhtGPIORig(respond func(l *fakeLine)) (*dhtGPIO, *fakeChip) {
	chip := newFakeChip()
	chip.onLine = func(l *fakeLine) {
		l.onReconfigure = func(flags uint64) {
			if flags&flagEdgeFalling != 0 {
				respond(l)
			}
		}
	}
	return newDHTGPIO(chip.opener(), 4), chip
}

func TestDHTGPIOFullExchange(t *testing.T) {
	g, chip := dhtGPIORig(func(l *fakeLine) { l.push(dhtTrain(dhtFrame(601, -55), us27, us70)...) })
	temp, rh, err := g.Measure()
	if err != nil || temp != -5.5 || rh != 60.1 {
		t.Fatalf("%v %v %v", temp, rh, err)
	}
	h := chip.lines[4].flagHistory
	if len(h) < 3 || h[0]&flagOutput == 0 || h[1]&(flagInput|flagEdgeFalling|flagEdgeRising) != flagInput|flagEdgeFalling|flagEdgeRising || h[1]&flagPullUp == 0 {
		t.Errorf("start sequence wrong: %#x", h)
	}
	if chip.lines[4].buffer < 84 {
		t.Errorf("event buffer %d is smaller than one frame", chip.lines[4].buffer)
	}
}

func TestDHTGPIOUnplugged(t *testing.T) {
	g, _ := dhtGPIORig(func(*fakeLine) {}) // no edges, pull-up holds the line high
	start := time.Now()
	_, _, err := g.Measure()
	if !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
	if el := time.Since(start); el > 100*time.Millisecond {
		t.Errorf("took %v", el)
	}
}

func TestDHTGPIOStuckLow(t *testing.T) {
	g, chip := dhtGPIORig(func(*fakeLine) {})
	_, _, _ = g.Measure() // opens the line
	chip.lines[4].onGet = func() bool { return false }
	if _, _, err := g.Measure(); !errors.Is(err, hal.ErrBadRead) {
		t.Fatalf("err = %v", err)
	}
}

func TestDHTGPIOPartialResponse(t *testing.T) {
	train := dhtTrain(dhtFrame(500, 200), us27, us70)
	g, _ := dhtGPIORig(func(l *fakeLine) { l.push(train[:25]...) })
	if _, _, err := g.Measure(); !errors.Is(err, hal.ErrBadRead) {
		t.Fatalf("err = %v", err)
	}
}

func TestDHTGPIONoChip(t *testing.T) {
	g := newDHTGPIO(func() (gpioChip, error) { return nil, absent("gpio chip", nil) }, 4)
	if _, _, err := g.Measure(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

// --- kernel IIO backend ---

const iioDev = "/sys/bus/iio/devices/iio:device0"

func iioFS(temp, rh string) *fakeFS {
	fs := newFakeFS()
	fs.files[iioDev+"/name"] = "dht11@4\n"
	fs.files[iioDev+"/in_temp_input"] = temp
	fs.files[iioDev+"/in_humidityrelative_input"] = rh
	return fs
}

func TestDHTKernelReads(t *testing.T) {
	temp, rh, err := newDHTKernel(iioFS("12500\n", "75300\n")).Measure()
	if err != nil || temp != 12.5 || rh != 75.3 {
		t.Fatalf("%v %v %v", temp, rh, err)
	}
	temp, _, err = newDHTKernel(iioFS("-3200\n", "75300\n")).Measure()
	if err != nil || temp != -3.2 {
		t.Fatalf("negative: %v %v", temp, err)
	}
}

func TestDHTKernelSkipsOtherIIODevices(t *testing.T) {
	fs := iioFS("12500\n", "75300\n")
	fs.files["/sys/bus/iio/devices/iio:device1/name"] = "rp1-adc\n"
	if _, _, err := newDHTKernel(fs).Measure(); err != nil {
		t.Fatal(err)
	}
	only := newFakeFS()
	only.files["/sys/bus/iio/devices/iio:device0/name"] = "rp1-adc\n"
	if _, _, err := newDHTKernel(only).Measure(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

func TestDHTKernelErrorMapping(t *testing.T) {
	path := iioDev + "/in_temp_input"
	wrap := func(e error) error { return &os.PathError{Op: "read", Path: path, Err: e} }
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"no edges", wrap(syscall.ETIMEDOUT), hal.ErrAbsent},
		{"undecodable", wrap(syscall.EIO), hal.ErrBadRead},
		{"device removed", wrap(syscall.ENODEV), hal.ErrAbsent},
	}
	for _, c := range cases {
		fs := iioFS("0", "0")
		fs.readErr[path] = c.err
		if _, _, err := newDHTKernel(fs).Measure(); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
	for _, v := range [][2]string{{"90000", "50000"}, {"-50000", "50000"}, {"20000", "120000"}, {"garbage", "50000"}} {
		if _, _, err := newDHTKernel(iioFS(v[0], v[1])).Measure(); !errors.Is(err, hal.ErrBadRead) {
			t.Errorf("%v: err = %v", v, err)
		}
	}
}

func TestDHTKernelNoOverlay(t *testing.T) {
	if _, _, err := newDHTKernel(newFakeFS()).Measure(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

// --- caching worker, the hal.Climate contract ---

type scriptedDHT struct {
	mu     sync.Mutex
	t, rh  float64
	err    error
	calls  int
	block  chan struct{}
	closed bool
}

func (s *scriptedDHT) Measure() (float64, float64, error) {
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.t, s.rh, s.err
}

func (s *scriptedDHT) Close() error { s.closed = true; return nil }

func TestClimateReadDoesNotWaitOnASlowSensor(t *testing.T) {
	// The kernel driver takes a second to fail. Read's bound is 500 ms.
	src := &scriptedDHT{block: make(chan struct{})}
	d := NewDHT22(src)
	start := time.Now()
	_, _, _, err := d.Read()
	if el := time.Since(start); el > 500*time.Millisecond {
		t.Errorf("took %v", el)
	}
	if !errors.Is(err, hal.ErrAbsent) {
		t.Errorf("err = %v", err)
	}
	close(src.block)
	if err := d.Close(); err != nil {
		t.Error(err)
	}
}

func TestClimateServesSampleWithAgeThenExpires(t *testing.T) {
	clk := &virtualClock{t: time.Unix(1000, 0)}
	src := &scriptedDHT{t: 12.5, rh: 75}
	d := newDHT22(src, clk.Now, time.Hour, 10*time.Second, 50*time.Millisecond)
	d.measure()

	clk.Sleep(3 * time.Second)
	temp, rh, age, err := d.Read()
	if err != nil || temp != 12.5 || rh != 75 || age != 3 {
		t.Fatalf("%v %v %v %v", temp, rh, age, err)
	}

	// One miss keeps the good sample and its true age.
	src.err = badRead("dht22", nil)
	d.measure()
	clk.Sleep(3 * time.Second)
	_, _, age, err = d.Read()
	if err != nil || age != 6 {
		t.Fatalf("after one miss: age %v err %v", age, err)
	}

	// Past the limit the failure surfaces.
	clk.Sleep(5 * time.Second)
	if _, _, _, err = d.Read(); !errors.Is(err, hal.ErrBadRead) {
		t.Fatalf("after expiry: %v", err)
	}

	// Recovery is immediate.
	src.err = nil
	d.measure()
	if _, _, age, err = d.Read(); err != nil || age != 0 {
		t.Fatalf("after recovery: age %v err %v", age, err)
	}
}

func TestClimateStuckWorkerExpires(t *testing.T) {
	clk := &virtualClock{t: time.Unix(1000, 0)}
	src := &scriptedDHT{t: 20, rh: 50}
	d := newDHT22(src, clk.Now, time.Hour, 10*time.Second, 50*time.Millisecond)
	d.measure()
	clk.Sleep(11 * time.Second) // no measure() completes
	if _, _, _, err := d.Read(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

func TestClimateEnforcesMinimumInterval(t *testing.T) {
	if dhtInterval < 2*time.Second {
		t.Fatalf("interval %v under the sensor's 2 s minimum", dhtInterval)
	}
	src := &scriptedDHT{t: 20, rh: 50}
	d := NewDHT22(src)
	defer d.Close()
	for i := 0; i < 200; i++ {
		d.Read()
		time.Sleep(time.Millisecond)
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.calls > 1 {
		t.Errorf("%d measurements in 200 ms", src.calls)
	}
}
