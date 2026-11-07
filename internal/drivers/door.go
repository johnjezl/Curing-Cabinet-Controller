package drivers

import (
	"errors"
	"sync"
)

// Door implements hal.Door for a normally closed switch to ground with a
// pull-up: door shut pulls the line low, door open or a cut wire reads high.
type Door struct {
	open chipOpener
	pin  int

	mu   sync.Mutex
	chip gpioChip
	line gpioLine
}

// NewDoor returns a driver for pin, or one that always answers ErrAbsent when
// pin is negative (no switch fitted).
func NewDoor(open chipOpener, pin int) *Door { return &Door{open: open, pin: pin} }

// Open reports whether the door is open. A fitted switch cannot be told apart
// from an open door when its wire is cut, so that case reads as open.
func (d *Door) Open() (bool, error) {
	if d.pin < 0 {
		return false, absent("door: no switch fitted", nil)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.line == nil {
		chip, err := d.open()
		if err != nil {
			return false, err
		}
		line, err := chip.Request("curing-door", d.pin, flagInput|flagPullUp, 0)
		if err != nil {
			chip.Close()
			return false, err
		}
		d.chip, d.line = chip, line
	}
	return d.line.Get()
}

// Close frees the line.
func (d *Door) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.line == nil {
		return nil
	}
	err := errors.Join(d.line.Close(), d.chip.Close())
	d.line, d.chip = nil, nil
	return err
}
