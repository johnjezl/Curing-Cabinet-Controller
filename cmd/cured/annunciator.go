package main

import (
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// armDelay is how long control must run before the alarm relay may pull in. A
// service in a crash loop therefore never looks healthy. See ADR 0402.
const armDelay = 15 * time.Second

// reassertEvery re-sends the line state, so a guard restart cannot leave it stale.
const reassertEvery = 5 * time.Second

// lineFilter sits between core and the outputs. It passes the plant outputs on and
// keeps the two annunciator outputs for the annunciator, because only this process
// knows everything that decides them. Both of core's annunciator lines are
// energize-to-run: OutAlarm on means core has nothing calling, OutBuzzer on means
// silent (ADR 0407). With forward set they also reach next, which the simulated
// plant uses to record them.
type lineFilter struct {
	next        hal.Outputs
	forward     bool
	coreHealthy atomic.Bool
	coreBuzzer  atomic.Bool // core wants the buzzer sounding
}

// Set implements hal.Outputs.
func (f *lineFilter) Set(id hal.OutputID, on bool) error {
	switch id {
	case hal.OutAlarm:
		f.coreHealthy.Store(on)
	case hal.OutBuzzer:
		f.coreBuzzer.Store(!on) // energized line is a silent buzzer
	default:
		return f.next.Set(id, on)
	}
	if f.forward {
		return f.next.Set(id, on)
	}
	return nil
}

// AllOff implements hal.Outputs. It is the fail-safe path, so nothing is held back:
// every line low, which is an open contact and a sounding buzzer.
func (f *lineFilter) AllOff() error {
	f.coreHealthy.Store(false)
	f.coreBuzzer.Store(true)
	return f.next.AllOff()
}

// annunciator decides the alarm contact and the buzzer. Tick runs on the control
// goroutine, so if control stops, nothing renews the relays' lease and the lines
// fall, which opens the contact.
type annunciator struct {
	lines hal.Outputs // nil when no physical line is driven
	f     *lineFilter
	sys   *sysAlarms

	mu                sync.Mutex
	start             time.Time
	armed, sawTemp    bool
	haveLast          bool
	lastAlarm, lastBz bool
	lastSend          time.Time
	errs              int
	contactClosed     bool
	buzzerOn          bool

	dropped atomic.Bool
}

func newAnnunciator(lines hal.Outputs, f *lineFilter, sys *sysAlarms, now time.Time) *annunciator {
	return &annunciator{lines: lines, f: f, sys: sys, start: now}
}

// Dropped records that the guard turned the outputs off because the lease ran out.
// Control stalled, so the relay must prove health again before it pulls in.
func (a *annunciator) Dropped() { a.dropped.Store(true) }

// Tick applies the current decision to the lines.
func (a *annunciator) Tick(now time.Time, temp api.Quality) { a.tick(now, temp, false) }

// tick with hold set writes nothing: a write would renew the relay lease that core
// is deliberately letting run out. The guard has then dropped every line, so the
// contact is open and the buzzer, energize-to-silence, sounds.
func (a *annunciator) tick(now time.Time, temp api.Quality, hold bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dropped.Swap(false) {
		a.start, a.armed, a.sawTemp, a.haveLast = now, false, false, false
	}
	if temp == api.QualityOK {
		a.sawTemp = true
	}
	if !a.armed && a.sawTemp && now.Sub(a.start) >= armDelay {
		a.armed = true
	}
	closed := a.armed && a.f.coreHealthy.Load() && !a.sys.Calling()
	buzz := a.f.coreBuzzer.Load() || a.sys.Sounding()
	if hold {
		closed, buzz = false, true
	}
	a.contactClosed, a.buzzerOn = closed, buzz
	if a.lines == nil || hold {
		a.haveLast = false
		return
	}
	if a.haveLast && closed == a.lastAlarm && buzz == a.lastBz && now.Sub(a.lastSend) < reassertEvery {
		return
	}
	e1 := a.lines.Set(hal.OutAlarm, closed)
	e2 := a.lines.Set(hal.OutBuzzer, !buzz) // energized is silent (ADR 0407)
	if e1 != nil || e2 != nil {
		a.haveLast = false
		a.errs++
		if a.errs == 3 {
			a.sys.Raise(CodeLinesFault, api.SevCritical, "Alarm relay lines not answering",
				"The output guard is not accepting commands, so the alarm contact stays open and no relay can switch. Check the journal for curing-outputd.")
		}
		return
	}
	if a.errs >= 3 {
		a.sys.Clear(CodeLinesFault)
	}
	a.errs = 0
	a.haveLast, a.lastAlarm, a.lastBz, a.lastSend = true, closed, buzz, now
}

// State returns what the lines were last told.
func (a *annunciator) State() (armed, contactClosed, buzzer bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.armed, a.contactClosed, a.buzzerOn
}

// watchedWD wraps the hardware layer's watchdog to learn about a lease drop, and
// records when core last asked for a pet.
type watchedWD struct {
	inner hal.Watchdog
	on    func(error)
	last  *atomic.Int64
}

// Pet implements hal.Watchdog.
func (w watchedWD) Pet() error {
	if w.last != nil {
		w.last.Store(time.Now().UnixNano())
	}
	err := w.inner.Pet()
	if err != nil && w.on != nil {
		w.on(err)
	}
	return err
}

// outputHold handles core withholding its pet because outputs cannot be written.
// core does that on purpose, so the guard's lease runs out and drops every relay by
// its own path. Without help systemd would then restart the service every 10 s, for
// as long as the fault lasts, and every restart resets the protection timers; the
// evidence showed 94 restarts in 24 minutes. So while core is still stepping and
// says OUTPUT_FAULT, this tells systemd the process is alive, and nothing renews the
// lease, so the outputs stay dropped and the contact stays open. See ADR 0405.
type outputHold struct {
	lastPet *atomic.Int64
	active  bool
}

// Check runs on the control goroutine after a step. It returns true while holding.
func (h *outputHold) Check(now time.Time, outputFault func() bool) bool {
	stale := now.Sub(time.Unix(0, h.lastPet.Load())) > 3*time.Second
	hold := stale && outputFault()
	if hold != h.active {
		h.active = hold
		if hold {
			logfJ("<2>cured: outputs cannot be written: core stopped renewing the relay lease, so the guard has dropped them. Staying up instead of restarting.")
		} else {
			logfJ("<4>cured: outputs writable again")
		}
	}
	if hold {
		if err := sdNotify("WATCHDOG=1"); err != nil {
			logfJ("<4>cured: watchdog notify: %v", err)
		}
	}
	return hold
}

// sdNotify sends one message to systemd's notify socket.
func sdNotify(msg string) error {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return errors.New("no NOTIFY_SOCKET")
	}
	if addr[0] == '@' {
		addr = "\x00" + addr[1:]
	}
	c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	_, err = c.Write([]byte(msg))
	return err
}

func hasActive(s api.Snapshot, code string) bool {
	for _, a := range s.Active {
		if a.Code == code {
			return true
		}
	}
	return false
}
