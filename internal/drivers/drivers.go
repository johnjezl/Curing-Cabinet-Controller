// Package drivers implements internal/hal against real Raspberry Pi 5 hardware
// through kernel interfaces only: the GPIO character device, /dev/i2c-N, IIO and
// PWM sysfs, and the systemd notify socket. It needs the runtime overlays and
// permissions set up by deploy/provision.sh.
//
// Every driver talks to a small interface (gpioChip, i2cBus, fileSystem) so the
// protocol logic can be tested without a Pi. A device that is missing is never a
// construction error: New always returns a usable Cabinet and the missing device
// answers hal.ErrAbsent, so the controller can start and report the fault.
package drivers

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// DHTBackend selects how the DHT22 is read.
type DHTBackend string

const (
	// DHTKernel uses the kernel dht11 IIO driver (the default; see ADR 0205).
	DHTKernel DHTBackend = "kernel"
	// DHTGPIO decodes edge timestamps in user space from the GPIO character
	// device. It cannot be used while the dht11 overlay holds the pin.
	DHTGPIO DHTBackend = "gpio"
)

// Config is the pin map and device selection. docs/wiring.md is the matching
// wiring table. Pins are BCM numbers on the RP1 40-pin header.
type Config struct {
	GPIOChipLabel string // label of the header's GPIO chip; default pinctrl-rp1
	I2CBus        int    // /dev/i2c-N for the FS3000; default 1 (GPIO 2 and 3)

	DHTBackend DHTBackend
	DHTPin     int // data line; used by DHTGPIO, and must match the overlay for DHTKernel

	HX711DataPin  int
	HX711ClockPin int

	FanPWMDevice  string // substring of the pwmchip's sysfs path; default RP1 PWM0
	FanPWMChannel int    // 0 for GPIO 12
	FanInvert     bool   // true when an inverting driver sits between pin and fan
	FanTachPin    int

	// Relays maps each output to its line. All must be GPIO 9 or above. The lines
	// belong to the curing-outputd process; see ADR 0207.
	Relays map[hal.OutputID]int

	// GuardSocket is curing-outputd's socket.
	GuardSocket string

	// DoorPin is the door switch line, or -1 when no switch is fitted.
	DoorPin int
}

// DefaultConfig is the pin map documented in docs/wiring.md.
func DefaultConfig() Config {
	return Config{
		GPIOChipLabel: "pinctrl-rp1",
		I2CBus:        1,
		DHTBackend:    DHTKernel,
		DHTPin:        4,
		HX711DataPin:  5,
		HX711ClockPin: 13,
		FanPWMDevice:  "1f00098000.pwm",
		FanPWMChannel: 0,
		FanTachPin:    6,
		Relays: map[hal.OutputID]int{
			hal.OutCool:    17,
			hal.OutHeat:    27,
			hal.OutHumid:   22,
			hal.OutDehumid: 23,
			hal.OutAlarm:   24,
			hal.OutBuzzer:  25,
		},
		GuardSocket: "/run/curing-outputs/guard.sock",
		DoorPin:     -1,
	}
}

// minRelayPin is the lowest line whose reset state is pull-down on the Pi 5, so
// that a high-level-trigger relay is off from power-on until software drives it.
const minRelayPin = 9

// Validate rejects a pin map that would break a safety property or collide.
func (c Config) Validate() error {
	used := map[int]string{2: "i2c sda", 3: "i2c scl", 0: "id eeprom", 1: "id eeprom", 12: "fan pwm"}
	claim := func(pin int, who string) error {
		if pin < 0 || pin > 27 {
			return fmt.Errorf("drivers: %s pin %d is not a header GPIO", who, pin)
		}
		if other, ok := used[pin]; ok {
			return fmt.Errorf("drivers: %s pin %d is already used by %s", who, pin, other)
		}
		used[pin] = who
		return nil
	}
	ids := make([]string, 0, len(c.Relays))
	for id := range c.Relays {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	for _, id := range ids {
		pin := c.Relays[hal.OutputID(id)]
		if pin < minRelayPin {
			return fmt.Errorf("drivers: relay %s on GPIO %d: below GPIO %d the reset pull is up, which would energize a relay at boot", id, pin, minRelayPin)
		}
		if err := claim(pin, "relay "+id); err != nil {
			return err
		}
	}
	if c.DHTBackend != DHTKernel && c.DHTBackend != DHTGPIO {
		return fmt.Errorf("drivers: unknown DHT backend %q", c.DHTBackend)
	}
	for _, p := range []struct {
		pin int
		who string
	}{{c.DHTPin, "dht"}, {c.HX711DataPin, "hx711 data"}, {c.HX711ClockPin, "hx711 clock"}, {c.FanTachPin, "fan tach"}} {
		if err := claim(p.pin, p.who); err != nil {
			return err
		}
	}
	if c.DoorPin >= 0 {
		if err := claim(c.DoorPin, "door"); err != nil {
			return err
		}
	}
	return nil
}

// New builds a Cabinet from cfg. It fails only on an invalid pin map.
func New(cfg Config) (hal.Cabinet, error) {
	if err := cfg.Validate(); err != nil {
		return hal.Cabinet{}, err
	}
	chip := func() (gpioChip, error) { return openChipByLabel(cfg.GPIOChipLabel) }
	fs := osFS{}

	var climate *DHT22
	switch cfg.DHTBackend {
	case DHTGPIO:
		climate = NewDHT22(newDHTGPIO(chip, cfg.DHTPin))
	default:
		climate = NewDHT22(newDHTKernel(fs))
	}
	air := NewFS3000(func() (i2cBus, error) { return openI2C(cfg.I2CBus) })
	scale := NewHX711(chip, cfg.HX711DataPin, cfg.HX711ClockPin)
	fan := NewFan(fs, chip, cfg.FanPWMDevice, cfg.FanPWMChannel, cfg.FanInvert, cfg.FanTachPin)
	outputs := newGuardClient(cfg.GuardSocket)
	door := NewDoor(chip, cfg.DoorPin)

	return hal.Cabinet{
		Climate: climate,
		Air:     air,
		Scale:   scale,
		Fan:     fan,
		Outputs: outputs,
		Door:    door,
		Close: func() error {
			// Outputs first: nothing below may stop the fail-safe path.
			return errors.Join(outputs.Close(), door.Close(), fan.Close(), scale.Close(), air.Close(), climate.Close())
		},
	}, nil
}

// Open builds the Cabinet and the Watchdog that goes with it. The application
// must pass this Watchdog to the control loop: each Pet renews the output lease,
// so without it the outputs drop one lease after the last command.
func Open(cfg Config) (hal.Cabinet, *SystemdWatchdog, error) {
	cab, err := New(cfg)
	if err != nil {
		return hal.Cabinet{}, nil, err
	}
	wd := NewWatchdog()
	wd.lease = cab.Outputs.(*guardClient)
	return cab, wd, nil
}

// absent wraps cause so errors.Is(err, hal.ErrAbsent) is true.
func absent(what string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%s: %w", what, hal.ErrAbsent)
	}
	return fmt.Errorf("%s: %w: %v", what, hal.ErrAbsent, cause)
}

// badRead wraps cause so errors.Is(err, hal.ErrBadRead) is true.
func badRead(what string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%s: %w", what, hal.ErrBadRead)
	}
	return fmt.Errorf("%s: %w: %v", what, hal.ErrBadRead, cause)
}

// errHung reports a kernel call that outlived its bound.
var errHung = errors.New("kernel call did not return in time")

// bounded runs f in a goroutine and waits at most d. A call still stuck after
// the deadline is left to finish on its own, and gate stops another from piling
// up behind it. It keeps a wedged bus or filesystem from blocking the caller.
type bounded struct {
	busy chan struct{}
}

func newBounded() *bounded { return &bounded{busy: make(chan struct{}, 1)} }

func (b *bounded) run(d time.Duration, f func() error) error {
	select {
	case b.busy <- struct{}{}:
	default:
		return errHung // the previous call is still stuck
	}
	done := make(chan error, 1)
	go func() {
		err := f()
		<-b.busy
		done <- err
	}()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case err := <-done:
		return err
	case <-t.C:
		return errHung
	}
}
