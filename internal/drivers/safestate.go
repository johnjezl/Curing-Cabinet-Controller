package drivers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const persistParam = "/sys/module/pinctrl_rp1/parameters/persist_gpio_outputs"

// ErrUnsafeState means a precondition of the documented fail-safe behavior does
// not hold on this system, so a crash might leave outputs on.
var ErrUnsafeState = errors.New("unsafe system state")

// checkPersistOff verifies that the kernel stops driving a line when its owner
// dies. With the default of Y, relays stay on after a crash. See ADR 0201.
func checkPersistOff(fs fileSystem) error {
	b, err := fs.ReadFile(persistParam)
	if err != nil {
		return fmt.Errorf("%w: cannot read %s: %v", ErrUnsafeState, persistParam, err)
	}
	if v := strings.TrimSpace(string(b)); v != "N" {
		return fmt.Errorf("%w: %s is %q, want N", ErrUnsafeState, persistParam, v)
	}
	return nil
}

// checkFanPWM verifies the fan channel is running at 25 kHz.
func checkFanPWM(fs fileSystem, cfg Config) error {
	f := &Fan{fs: fs, device: cfg.FanPWMDevice, channel: cfg.FanPWMChannel}
	ch, err := f.findChannel()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeState, err)
	}
	read := func(name string) (int, error) {
		b, err := fs.ReadFile(ch + "/" + name)
		if err != nil {
			return 0, err
		}
		return strconv.Atoi(strings.TrimSpace(string(b)))
	}
	period, err := read("period")
	if err != nil {
		return fmt.Errorf("%w: fan pwm: %v", ErrUnsafeState, err)
	}
	enable, err := read("enable")
	if err != nil {
		return fmt.Errorf("%w: fan pwm: %v", ErrUnsafeState, err)
	}
	if period != fanPeriodNS || enable != 1 {
		return fmt.Errorf("%w: fan pwm period=%d enable=%d, want %d and 1", ErrUnsafeState, period, enable, fanPeriodNS)
	}
	return nil
}

// VerifySafeState checks the kernel and PWM state the fail-safe behavior rests on.
// The application calls it at startup and treats an error as a fault to show the
// operator. Independently, curing-outputd refuses to energize outputs while the
// relay precondition is wrong, and rechecks it every second.
func VerifySafeState(cfg Config) error {
	fs := osFS{}
	return errors.Join(checkPersistOff(fs), checkFanPWM(fs, cfg))
}

// Display state written by curing-hw-init at each boot.
const (
	displayStatusFile  = "/run/curing/display-status"
	displayMissingFile = "/run/curing/display-missing"
)

// DisplayMissing reports whether the panel failed to come up on this boot, in which
// case the application should alarm by other means. detail holds the raw status.
// An error means bring-up has not recorded a state, which is itself worth alarming on.
func DisplayMissing() (missing bool, detail string, err error) {
	return displayMissing(osFS{})
}

func displayMissing(fs fileSystem) (bool, string, error) {
	status, err := fs.ReadFile(displayStatusFile)
	if err != nil {
		return false, "", fmt.Errorf("display state not recorded: %w", err)
	}
	detail := strings.Join(strings.Fields(string(status)), " ")
	if _, err := fs.ReadFile(displayMissingFile); err == nil {
		return true, detail, nil
	}
	return false, detail, nil
}
