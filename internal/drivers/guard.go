package drivers

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// The output guard is a separate process, curing-outputd, that owns the relay
// lines. The application asks it to switch them and has to keep asking: any output
// stays on only while the lease is being renewed. A control loop that hangs, a
// process that is frozen and a process that dies all lose the lease, and the guard
// turns everything off with no help from them. See ADR 0207.
const (
	// OutputLease is how long the outputs stay allowed on after the last command
	// or renewal. The control loop renews about once a second.
	OutputLease = 3 * time.Second

	guardTick       = 50 * time.Millisecond
	guardCheckEvery = time.Second
	guardClientWait = 100 * time.Millisecond
)

// journald priorities, as the "<n>" line prefix.
const (
	prioCrit    = 2
	prioWarning = 4
	prioInfo    = 6
)

// guardCore is the guard's logic, free of sockets and clocks.
type guardCore struct {
	mu     sync.Mutex
	out    hal.Outputs
	ids    []hal.OutputID
	on     map[hal.OutputID]bool
	lease  time.Duration
	now    func() time.Time
	check  func() error
	logf   func(prio int, format string, args ...any)
	owner  any
	expiry time.Time

	dropped   bool
	unsafe    error
	lastCheck time.Time
}

func newGuardCore(out hal.Outputs, ids []hal.OutputID, lease time.Duration, now func() time.Time, check func() error, logf func(int, string, ...any)) *guardCore {
	sorted := append([]hal.OutputID(nil), ids...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	g := &guardCore{out: out, ids: sorted, on: map[hal.OutputID]bool{}, lease: lease, now: now, check: check, logf: logf}
	g.recheck()
	return g
}

func (g *guardCore) anyOn() bool {
	for _, on := range g.on {
		if on {
			return true
		}
	}
	return false
}

func (g *guardCore) onList() string {
	var l []string
	for _, id := range g.ids {
		if g.on[id] {
			l = append(l, string(id))
		}
	}
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ",")
}

// dropAll is the one path that turns everything off on the guard's own decision.
func (g *guardCore) dropAll(reason string) error {
	was := g.onList()
	g.on = map[hal.OutputID]bool{}
	g.dropped = true
	g.owner = nil
	g.logf(prioCrit, "all outputs off: %s (were on: %s)", reason, was)
	return g.out.AllOff()
}

// recheck verifies the safety preconditions. Caller holds mu or is constructing.
func (g *guardCore) recheck() error {
	g.lastCheck = g.now()
	err := g.check()
	switch {
	case err != nil && g.unsafe == nil:
		g.logf(prioCrit, "refusing to energize outputs: %v", err)
	case err == nil && g.unsafe != nil:
		g.logf(prioWarning, "safety precondition restored")
	}
	g.unsafe = err
	return err
}

// Tick enforces the lease and rechecks the preconditions. An error means an
// output may still be on and the guard could not turn it off; the caller must
// exit, which makes the kernel stop driving the lines.
func (g *guardCore) Tick() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if now.Sub(g.lastCheck) >= guardCheckEvery {
		if err := g.recheck(); err != nil && g.anyOn() {
			return g.dropAll("safety precondition failed: " + err.Error())
		}
	}
	if g.anyOn() && now.After(g.expiry) {
		return g.dropAll(fmt.Sprintf("no command or renewal for %v", g.lease))
	}
	return nil
}

// Disconnected reports a client's connection closing. If that client issued the
// last command, control is gone and there is no reason to wait out the lease.
func (g *guardCore) Disconnected(client any) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.owner == client && g.anyOn() {
		return g.dropAll("controlling client disconnected")
	}
	return nil
}

// Handle processes one request and returns the reply line.
func (g *guardCore) Handle(client any, req string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := strings.Fields(req)
	if len(f) == 0 {
		return "err empty request"
	}
	switch f[0] {
	case "renew":
		if g.unsafe != nil {
			return "err " + g.unsafe.Error()
		}
		g.expiry = g.now().Add(g.lease)
		g.owner = client
	case "alloff":
		g.on = map[hal.OutputID]bool{}
		g.dropped = false
		g.owner = client
		if err := g.out.AllOff(); err != nil {
			return "err " + err.Error()
		}
	case "set":
		if len(f) != 3 || (f[2] != "0" && f[2] != "1") {
			return "err bad set request"
		}
		id, on := hal.OutputID(f[1]), f[2] == "1"
		if on && g.unsafe != nil {
			return "err " + g.unsafe.Error()
		}
		if err := g.out.Set(id, on); err != nil {
			return "err " + err.Error()
		}
		g.on[id] = on
		g.dropped = false
		g.owner = client
		g.expiry = g.now().Add(g.lease)
	case "state":
	default:
		return "err unknown request " + f[0]
	}
	d := 0
	if g.dropped {
		d = 1
	}
	return fmt.Sprintf("ok dropped=%d on=%s", d, g.onList())
}

// guardClient implements hal.Outputs by asking curing-outputd.
type guardClient struct {
	path string

	mu   sync.Mutex
	conn net.Conn
}

func newGuardClient(path string) *guardClient { return &guardClient{path: path} }

func (c *guardClient) dropConn() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *guardClient) ask(req string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	deadline := time.Now().Add(guardClientWait)
	if c.conn == nil {
		d := net.Dialer{Deadline: deadline}
		conn, err := d.Dial("unixpacket", c.path)
		if err != nil {
			return "", absent("output guard", err)
		}
		c.conn = conn
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		c.dropConn()
		return "", absent("output guard", err)
	}
	if _, err := c.conn.Write([]byte(req)); err != nil {
		c.dropConn()
		return "", absent("output guard", err)
	}
	buf := make([]byte, 512)
	n, err := c.conn.Read(buf)
	if err != nil {
		c.dropConn()
		return "", absent("output guard", err)
	}
	reply := string(buf[:n])
	if rest, ok := strings.CutPrefix(reply, "err "); ok {
		return "", fmt.Errorf("output guard: %s", rest)
	}
	return reply, nil
}

// Set switches one output and renews the lease. It is idempotent.
func (c *guardClient) Set(id hal.OutputID, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	_, err := c.ask(fmt.Sprintf("set %s %s", id, v))
	return err
}

// AllOff asks the guard to turn everything off. If the guard cannot be reached
// the error says so; its own watchdog then removes it, and with it the lines.
func (c *guardClient) AllOff() error {
	_, err := c.ask("alloff")
	return err
}

// Renew extends the lease. It wraps hal.ErrOutputsDropped after a lease drop.
func (c *guardClient) Renew() error {
	reply, err := c.ask("renew")
	if err != nil {
		return err
	}
	if strings.Contains(reply, "dropped=1") {
		return fmt.Errorf("output guard: %w", hal.ErrOutputsDropped)
	}
	return nil
}

// State returns the guard's own view, for diagnostics.
func (c *guardClient) State() (string, error) { return c.ask("state") }

// Close turns everything off and disconnects.
func (c *guardClient) Close() error {
	_, err := c.ask("alloff")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropConn()
	if errors.Is(err, hal.ErrAbsent) {
		return nil // no guard: nothing of ours is on
	}
	return err
}

// GuardState asks a running guard for its state.
func GuardState(cfg Config) (string, error) {
	c := newGuardClient(cfg.GuardSocket)
	defer func() { c.mu.Lock(); c.dropConn(); c.mu.Unlock() }()
	return c.State()
}

// RunOutputGuard is curing-outputd's main. It returns only on SIGTERM or a failure
// that requires the process to exit so the kernel releases the lines.
func RunOutputGuard(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	logf := func(prio int, format string, args ...any) {
		fmt.Printf("<%d>%s\n", prio, fmt.Sprintf(format, args...))
	}
	relays := NewRelays(func() (gpioChip, error) { return openChipByLabel(cfg.GPIOChipLabel) }, cfg.Relays)
	ids := make([]hal.OutputID, 0, len(cfg.Relays))
	for id := range cfg.Relays {
		ids = append(ids, id)
	}
	fs := osFS{}
	core := newGuardCore(relays, ids, OutputLease, time.Now, func() error { return checkPersistOff(fs) }, logf)
	// Claiming every line now, low, proves they are available at startup.
	if err := relays.AllOff(); err != nil {
		return fmt.Errorf("cannot claim relay lines: %w", err)
	}

	os.Remove(cfg.GuardSocket)
	ln, err := net.Listen("unixpacket", cfg.GuardSocket)
	if err != nil {
		return err
	}
	defer ln.Close()
	if err := os.Chmod(cfg.GuardSocket, 0o660); err != nil {
		return err
	}

	fatal := make(chan error, 1)
	report := func(err error) {
		if err != nil {
			select {
			case fatal <- err:
			default:
			}
		}
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 128)
				for {
					n, err := conn.Read(buf)
					if err != nil || n == 0 {
						report(core.Disconnected(conn))
						return
					}
					if _, err := conn.Write([]byte(core.Handle(conn, string(buf[:n])))); err != nil {
						report(core.Disconnected(conn))
						return
					}
				}
			}(conn)
		}
	}()

	sd := NewWatchdog()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	sd.Ready()
	logf(prioInfo, "output guard ready, lease %v, %d outputs off", OutputLease, len(ids))

	// The loop that enforces the lease also pets systemd, so if it stops, systemd
	// kills this process and the kernel stops driving the lines.
	runtime.LockOSThread()
	t := time.NewTicker(guardTick)
	defer t.Stop()
	for {
		select {
		case <-sig:
			sd.Stopping()
			logf(prioInfo, "output guard stopping")
			return relays.Close()
		case err := <-fatal:
			return fmt.Errorf("could not turn outputs off: %w", err)
		case <-t.C:
			if err := core.Tick(); err != nil {
				return fmt.Errorf("could not turn outputs off: %w", err)
			}
			sd.Pet()
		}
	}
}
