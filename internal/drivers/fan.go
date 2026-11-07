package drivers

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	fanPeriodNS = 40000 // 25 kHz, Intel 4-wire fan specification
	fanBound    = 100 * time.Millisecond

	tachPulsesPerRev = 2
	tachWindow       = time.Second
	// The P12 Pro tops out at 3000 RPM, or 100 pulses/s. Anything closer than
	// 3 ms is noise, usually PWM crosstalk on the tach wire.
	tachMinSpacing = 3 * time.Millisecond
	// Above this the signal cannot be a real P12 Pro.
	tachMaxRPM = 4000
	// With at least tachExpectDuty commanded for tachGrace, silence on the tach
	// line means no fan or an unusable line, not a stopped fan.
	tachExpectDuty = 30.0
	tachGrace      = 5 * time.Second
)

// Fan implements hal.Fan: sysfs PWM for speed, an edge-detecting GPIO for tach.
type Fan struct {
	fs      fileSystem
	open    chipOpener
	device  string
	channel int
	invert  bool
	tachPin int
	now     func() time.Time
	gate    *bounded

	cfgMu  sync.Mutex // guards chPath; a hung sysfs call may still hold it
	chPath string

	mu      sync.Mutex
	duty    float64
	spunUp  time.Time // when duty first reached tachExpectDuty; zero if below
	pulses  []pulse
	tachErr error
	chip    gpioChip
	tach    gpioLine
	stop    chan struct{}
	stopped chan struct{}
}

type pulse struct {
	arrived time.Time
	stampNS uint64
}

// NewFan returns a driver that finds its PWM channel and tach line on first use.
func NewFan(fs fileSystem, open chipOpener, device string, channel int, invert bool, tachPin int) *Fan {
	return &Fan{fs: fs, open: open, device: device, channel: channel, invert: invert, tachPin: tachPin, now: time.Now, gate: newBounded()}
}

// findChannel locates the channel directory of the configured chip. pwmchip
// numbers depend on probe order, so it matches on the device path.
func (f *Fan) findChannel() (string, error) {
	chips, err := f.fs.Glob("/sys/class/pwm/pwmchip*")
	if err != nil {
		return "", err
	}
	for _, c := range chips {
		real, err := f.fs.EvalSymlinks(c)
		if err != nil || !strings.Contains(real, f.device) {
			continue
		}
		ch := filepath.Join(c, fmt.Sprintf("pwm%d", f.channel))
		if _, err := f.fs.ReadFile(ch + "/period"); err != nil {
			return "", absent("fan: pwm channel not exported, curing-hw.service has not run", err)
		}
		return ch, nil
	}
	return "", absent("fan: pwm chip "+f.device+" not present, overlay not loaded", nil)
}

func (f *Fan) writeInt(name string, v int64) error {
	return f.fs.WriteFile(f.chPath+"/"+name, []byte(strconv.FormatInt(v, 10)))
}

func (f *Fan) ensureConfigured() error {
	if f.chPath == "" {
		ch, err := f.findChannel()
		if err != nil {
			return err
		}
		f.chPath = ch
	}
	period, err := f.fs.ReadFile(f.chPath + "/period")
	if err != nil {
		f.chPath = ""
		return absent("fan: pwm channel vanished", err)
	}
	if strings.TrimSpace(string(period)) != strconv.Itoa(fanPeriodNS) {
		// The kernel rejects a period below the duty cycle, so clear duty first.
		if err := f.writeInt("duty_cycle", 0); err != nil {
			return err
		}
		if err := f.writeInt("period", fanPeriodNS); err != nil {
			return err
		}
	}
	enable, err := f.fs.ReadFile(f.chPath + "/enable")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(enable)) != "1" {
		return f.writeInt("enable", 1)
	}
	return nil
}

// SetDuty sets the PWM duty in percent, 0 to 100.
func (f *Fan) SetDuty(pct float64) error {
	if math.IsNaN(pct) || pct < 0 || pct > 100 {
		return fmt.Errorf("fan: duty %v outside 0 to 100", pct)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startTach()
	err := f.gate.run(fanBound, func() error {
		f.cfgMu.Lock()
		defer f.cfgMu.Unlock()
		if err := f.ensureConfigured(); err != nil {
			return err
		}
		active := pct
		if f.invert {
			active = 100 - pct
		}
		return f.writeInt("duty_cycle", int64(math.Round(active/100*fanPeriodNS)))
	})
	if err != nil {
		if errors.Is(err, errHung) {
			return absent("fan: pwm sysfs hung", err)
		}
		return err
	}
	switch {
	case pct < tachExpectDuty:
		f.spunUp = time.Time{}
	case f.spunUp.IsZero():
		f.spunUp = f.now()
	}
	f.duty = pct
	return nil
}

// startTach claims the tach line and starts counting. Caller holds mu. A failure
// is remembered and retried on the next call.
func (f *Fan) startTach() {
	if f.tach != nil {
		return
	}
	chip, err := f.open()
	if err != nil {
		f.tachErr = err
		return
	}
	// External pull-up per docs/wiring.md; the internal one also holds an unwired
	// line at a defined level. Falling edges only: the fan's open collector makes
	// those the clean ones.
	line, err := chip.Request("curing-fan-tach", f.tachPin, flagInput|flagPullUp|flagEdgeFalling, 64)
	if err != nil {
		chip.Close()
		f.tachErr = err
		return
	}
	f.chip, f.tach, f.tachErr = chip, line, nil
	f.stop, f.stopped = make(chan struct{}), make(chan struct{})
	go f.tachLoop(line, f.stop, f.stopped)
}

func (f *Fan) tachLoop(line gpioLine, stop, stopped chan struct{}) {
	defer close(stopped)
	for {
		select {
		case <-stop:
			return
		default:
		}
		ev, err := line.ReadEvent(250 * time.Millisecond)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			continue
		}
		if err != nil {
			f.mu.Lock()
			f.tachErr = err
			f.mu.Unlock()
			return
		}
		now := f.now()
		f.mu.Lock()
		if n := len(f.pulses); n == 0 || time.Duration(ev.TimestampNS-f.pulses[n-1].stampNS) >= tachMinSpacing {
			f.pulses = append(f.pulses, pulse{arrived: now, stampNS: ev.TimestampNS})
		}
		f.prunePulses(now)
		f.mu.Unlock()
	}
}

func (f *Fan) prunePulses(now time.Time) {
	i := 0
	for i < len(f.pulses) && now.Sub(f.pulses[i].arrived) > tachWindow {
		i++
	}
	f.pulses = f.pulses[i:]
}

// RPM returns the speed measured over the last second. A stopped fan with a low
// duty command is 0 with no error; a tach line that cannot be read, or silence
// after tachGrace at a duty that must spin the fan, is hal.ErrAbsent, because
// the two look identical on the wire.
func (f *Fan) RPM() (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startTach()
	if f.tach == nil {
		return 0, absent("fan: tach line unavailable", f.tachErr)
	}
	if f.tachErr != nil {
		return 0, absent("fan: tach read failed", f.tachErr)
	}
	now := f.now()
	f.prunePulses(now)
	if len(f.pulses) < 2 {
		if !f.spunUp.IsZero() && now.Sub(f.spunUp) >= tachGrace {
			return 0, absent(fmt.Sprintf("fan: no tach pulses at %.0f%% duty", f.duty), nil)
		}
		return 0, nil
	}
	first, last := f.pulses[0], f.pulses[len(f.pulses)-1]
	span := time.Duration(last.stampNS - first.stampNS)
	if span <= 0 {
		return 0, badRead("fan", errors.New("tach timestamps not increasing"))
	}
	revs := float64(len(f.pulses)-1) / tachPulsesPerRev
	rpm := revs / span.Minutes()
	if rpm > tachMaxRPM {
		return 0, badRead("fan", fmt.Errorf("tach reads %.0f RPM, above anything this fan can do", rpm))
	}
	return rpm, nil
}

// Close frees the tach line. The PWM keeps its last setting: leaving the fan
// running is the safe direction, and curing-hw.service resets it at boot.
func (f *Fan) Close() error {
	f.mu.Lock()
	line, chip, stop, stopped := f.tach, f.chip, f.stop, f.stopped
	f.tach, f.chip = nil, nil
	f.mu.Unlock()
	if line == nil {
		return nil
	}
	close(stop)
	<-stopped
	return errors.Join(line.Close(), chip.Close())
}
