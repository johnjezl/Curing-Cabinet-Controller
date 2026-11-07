package drivers

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// Relays implements hal.Outputs. Polarity is fixed active-high: GPIO high means
// energized. There is deliberately no active-low setting, because a low-level
// trigger module would energize on the pull-down lines at reset and after any
// crash. See docs/adr/0201 and docs/wiring.md.
//
// Each output holds its own line request so one failure cannot prevent the
// others from being switched off, and lines are requested with a pull-down so
// that a released line stays low.
type Relays struct {
	open chipOpener
	pins map[hal.OutputID]int

	mu    sync.Mutex
	chip  gpioChip
	lines map[hal.OutputID]gpioLine
}

// NewRelays returns a driver that claims each line on first use.
func NewRelays(open chipOpener, pins map[hal.OutputID]int) *Relays {
	return &Relays{open: open, pins: pins, lines: map[hal.OutputID]gpioLine{}}
}

func (r *Relays) line(id hal.OutputID) (gpioLine, error) {
	if l, ok := r.lines[id]; ok {
		return l, nil
	}
	pin, ok := r.pins[id]
	if !ok {
		return nil, fmt.Errorf("relays: no pin for output %q", id)
	}
	if r.chip == nil {
		chip, err := r.open()
		if err != nil {
			return nil, err
		}
		r.chip = chip
	}
	// A new output request starts low, so claiming a line never energizes it.
	l, err := r.chip.Request("curing-relay-"+string(id), pin, flagOutput|flagPullDown, 0)
	if err != nil {
		return nil, fmt.Errorf("relays: %s: %w", id, err)
	}
	r.lines[id] = l
	return l, nil
}

// Set switches one output. It is idempotent.
func (r *Relays) Set(id hal.OutputID, on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, err := r.line(id)
	if err != nil {
		return err
	}
	if err := l.Set(on); err != nil {
		return fmt.Errorf("relays: %s: %w", id, err)
	}
	return nil
}

// AllOff attempts every configured output and joins any failures.
func (r *Relays) AllOff() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.allOff()
}

func (r *Relays) allOff() error {
	ids := make([]string, 0, len(r.pins))
	for id := range r.pins {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	var errs []error
	for _, s := range ids {
		id := hal.OutputID(s)
		l, err := r.line(id)
		if err == nil {
			err = l.Set(false)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("relays: %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// Close drives every claimed output off and releases it to its pull-down. An
// output never claimed has never been driven and is already off.
func (r *Relays) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var err error
	for id, l := range r.lines {
		err = errors.Join(err, l.Set(false), l.Close())
		delete(r.lines, id)
	}
	if r.chip != nil {
		err = errors.Join(err, r.chip.Close())
		r.chip = nil
	}
	return err
}
