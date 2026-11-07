package drivers

import (
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

const (
	testChip = "/sys/class/pwm/pwmchip1"
	testCh   = testChip + "/pwm0"
	testTach = 6
)

func pwmFS() *fakeFS {
	fs := newFakeFS()
	fs.files["/sys/class/pwm/pwmchip0/npwm"] = "4"
	fs.links["/sys/class/pwm/pwmchip0"] = "/sys/devices/platform/axi/1f0009c000.pwm/pwm/pwmchip0"
	fs.files[testChip+"/npwm"] = "4"
	fs.links[testChip] = "/sys/devices/platform/1f00098000.pwm/pwm/pwmchip1"
	fs.files[testCh+"/period"] = "40000\n"
	fs.files[testCh+"/duty_cycle"] = "0\n"
	fs.files[testCh+"/enable"] = "1\n"
	return fs
}

func fanRig(fs *fakeFS) (*Fan, *fakeChip, *virtualClock) {
	chip := newFakeChip()
	f := NewFan(fs, chip.opener(), "1f00098000.pwm", 0, false, testTach)
	clk := &virtualClock{t: time.Unix(1000, 0)}
	f.now = clk.Now
	return f, chip, clk
}

func TestFanDutyMapsToNanoseconds(t *testing.T) {
	fs := pwmFS()
	f, _, _ := fanRig(fs)
	defer f.Close()
	for _, c := range []struct {
		pct  float64
		want string
	}{{0, "0"}, {25, "10000"}, {50, "20000"}, {100, "40000"}, {33.3, "13320"}} {
		if err := f.SetDuty(c.pct); err != nil {
			t.Fatal(err)
		}
		if got := fs.files[testCh+"/duty_cycle"]; got != c.want {
			t.Errorf("%v%%: %s want %s", c.pct, got, c.want)
		}
	}
}

func TestFanInvertedDriver(t *testing.T) {
	fs := pwmFS()
	chip := newFakeChip()
	f := NewFan(fs, chip.opener(), "1f00098000.pwm", 0, true, testTach)
	defer f.Close()
	if err := f.SetDuty(25); err != nil {
		t.Fatal(err)
	}
	if got := fs.files[testCh+"/duty_cycle"]; got != "30000" {
		t.Errorf("duty %s", got)
	}
}

func TestFanRejectsBadDutyWithoutWriting(t *testing.T) {
	fs := pwmFS()
	f, _, _ := fanRig(fs)
	defer f.Close()
	for _, v := range []float64{-1, 100.5, math.NaN(), math.Inf(1)} {
		if err := f.SetDuty(v); err == nil {
			t.Errorf("%v accepted", v)
		}
	}
	if len(fs.writes) != 0 {
		t.Errorf("wrote %v", fs.writes)
	}
}

func TestFanRepairsWrongPeriodInSafeOrder(t *testing.T) {
	fs := pwmFS()
	fs.files[testCh+"/period"] = "1000000\n"
	fs.files[testCh+"/duty_cycle"] = "500000\n"
	fs.files[testCh+"/enable"] = "0\n"
	f, _, _ := fanRig(fs)
	defer f.Close()
	if err := f.SetDuty(50); err != nil {
		t.Fatal(err)
	}
	want := []string{"duty_cycle=0", "period=40000", "enable=1", "duty_cycle=20000"}
	if len(fs.writes) != len(want) {
		t.Fatalf("writes %v", fs.writes)
	}
	for i := range want {
		if fs.writes[i] != want[i] {
			t.Fatalf("writes %v want %v", fs.writes, want)
		}
	}
}

func TestFanIgnoresTheBoardFanChip(t *testing.T) {
	fs := newFakeFS()
	fs.files["/sys/class/pwm/pwmchip0/pwm0/period"] = "40000"
	fs.links["/sys/class/pwm/pwmchip0"] = "/sys/devices/platform/axi/1f0009c000.pwm/pwm/pwmchip0"
	f, _, _ := fanRig(fs)
	defer f.Close()
	if err := f.SetDuty(50); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
	if len(fs.writes) != 0 {
		t.Errorf("wrote to the wrong chip: %v", fs.writes)
	}
}

func TestFanChannelNotExportedIsAbsent(t *testing.T) {
	fs := pwmFS()
	delete(fs.files, testCh+"/period")
	f, _, _ := fanRig(fs)
	defer f.Close()
	if err := f.SetDuty(50); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
}

func TestFanHungSysfsReturnsWithinBound(t *testing.T) {
	fs := pwmFS()
	block := make(chan struct{})
	fs.block = block
	f, _, _ := fanRig(fs)
	start := time.Now()
	err := f.SetDuty(50)
	if el := time.Since(start); el > 200*time.Millisecond {
		t.Errorf("took %v", el)
	}
	if !errors.Is(err, hal.ErrAbsent) {
		t.Errorf("err = %v", err)
	}
	close(block)
	f.Close()
}

// tachPulses pushes n falling edges spaced apart and waits for them to be taken.
func tachPulses(t *testing.T, f *Fan, l *fakeLine, startNS uint64, n int, spacing time.Duration) uint64 {
	t.Helper()
	ts := startNS
	for i := 0; i < n; i++ {
		l.push(lineEvent{TimestampNS: ts})
		ts += uint64(spacing)
	}
	deadline := time.Now().Add(time.Second)
	for {
		l.mu.Lock()
		left := len(l.events)
		l.mu.Unlock()
		if left == 0 {
			time.Sleep(time.Millisecond) // let the loop publish the last one
			return ts
		}
		if time.Now().After(deadline) {
			t.Fatal("tach loop did not drain events")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTachRPM(t *testing.T) {
	// 2 pulses per revolution: 1500 RPM is 50 pulses/s, a 20 ms spacing.
	f, chip, _ := fanRig(pwmFS())
	defer f.Close()
	if err := f.SetDuty(50); err != nil {
		t.Fatal(err)
	}
	l := chip.lines[testTach]
	if l.flags&flagEdgeFalling == 0 || l.flags&flagPullUp == 0 {
		t.Errorf("tach flags %#x", l.flags)
	}
	tachPulses(t, f, l, 1_000_000, 40, 20*time.Millisecond)
	rpm, err := f.RPM()
	if err != nil || math.Abs(rpm-1500) > 1 {
		t.Fatalf("rpm %v err %v", rpm, err)
	}
}

func TestTachRejectsCrosstalkGlitches(t *testing.T) {
	f, chip, _ := fanRig(pwmFS())
	defer f.Close()
	f.SetDuty(50)
	l := chip.lines[testTach]
	// Real pulses at 20 ms, each followed by 40 us ringing at the PWM rate.
	ts := uint64(1_000_000)
	for i := 0; i < 40; i++ {
		l.push(lineEvent{TimestampNS: ts}, lineEvent{TimestampNS: ts + 40_000}, lineEvent{TimestampNS: ts + 80_000})
		ts += uint64(20 * time.Millisecond)
	}
	tachPulses(t, f, l, ts, 0, 0)
	time.Sleep(5 * time.Millisecond)
	rpm, err := f.RPM()
	if err != nil || math.Abs(rpm-1500) > 1 {
		t.Fatalf("rpm %v err %v", rpm, err)
	}
}

func TestTachImplausibleSpeedIsBadRead(t *testing.T) {
	f, chip, _ := fanRig(pwmFS())
	defer f.Close()
	f.SetDuty(50)
	tachPulses(t, f, chip.lines[testTach], 1_000_000, 40, 4*time.Millisecond) // 7500 RPM
	if _, err := f.RPM(); !errors.Is(err, hal.ErrBadRead) {
		t.Fatalf("err = %v", err)
	}
}

func TestTachStoppedVersusAbsent(t *testing.T) {
	f, chip, clk := fanRig(pwmFS())
	defer f.Close()
	if err := f.SetDuty(0); err != nil {
		t.Fatal(err)
	}
	clk.Sleep(time.Minute)
	if rpm, err := f.RPM(); err != nil || rpm != 0 {
		t.Fatalf("stopped by command: %v %v", rpm, err)
	}

	// A duty that must spin the fan, within the spin-up grace: still 0, no error.
	if err := f.SetDuty(60); err != nil {
		t.Fatal(err)
	}
	clk.Sleep(tachGrace - time.Second)
	if rpm, err := f.RPM(); err != nil || rpm != 0 {
		t.Fatalf("during grace: %v %v", rpm, err)
	}
	// After it, silence means no fan or a dead tach.
	clk.Sleep(2 * time.Second)
	if _, err := f.RPM(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("after grace: %v", err)
	}
	// Pulses clear it.
	tachPulses(t, f, chip.lines[testTach], 1_000_000, 20, 20*time.Millisecond)
	if rpm, err := f.RPM(); err != nil || math.Abs(rpm-1500) > 1 {
		t.Fatalf("recovered: %v %v", rpm, err)
	}
	// Dropping below the expected duty clears the expectation.
	if err := f.SetDuty(10); err != nil {
		t.Fatal(err)
	}
	clk.Sleep(time.Minute)
	if rpm, err := f.RPM(); err != nil || rpm != 0 {
		t.Fatalf("low duty: %v %v", rpm, err)
	}
}

func TestTachWindowExpiresToZero(t *testing.T) {
	f, chip, clk := fanRig(pwmFS())
	defer f.Close()
	f.SetDuty(10)
	tachPulses(t, f, chip.lines[testTach], 1_000_000, 20, 20*time.Millisecond)
	clk.Sleep(2 * time.Second)
	if rpm, err := f.RPM(); err != nil || rpm != 0 {
		t.Fatalf("%v %v", rpm, err)
	}
}

func TestTachReadFailureIsAbsent(t *testing.T) {
	f, chip, _ := fanRig(pwmFS())
	defer f.Close()
	f.SetDuty(0)
	l := chip.lines[testTach]
	l.mu.Lock()
	l.eventErr = os.ErrClosed
	l.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for {
		_, err := f.RPM()
		if errors.Is(err, hal.ErrAbsent) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("err = %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTachLineBusyOrChipMissing(t *testing.T) {
	chip := newFakeChip()
	chip.failPins[testTach] = errors.New("device or resource busy")
	f := NewFan(pwmFS(), chip.opener(), "1f00098000.pwm", 0, false, testTach)
	if _, err := f.RPM(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("busy: %v", err)
	}
	f2 := NewFan(pwmFS(), func() (gpioChip, error) { return nil, absent("gpio chip", nil) }, "1f00098000.pwm", 0, false, testTach)
	if _, err := f2.RPM(); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("no chip: %v", err)
	}
}
