package drivers

import (
	"errors"
	"net"
	"os"
	"sync"
	"time"
)

// SystemdWatchdog implements hal.Watchdog over the systemd notify socket. Each
// Pet tells systemd the control loop is healthy. If they stop, systemd (with
// WatchdogSec= on the unit) kills and restarts the service. PID 1 owns the
// hardware watchdog in turn and resets the board if the kernel or PID 1 dies, so
// the chain is control loop -> systemd -> hardware. See docs/adr/0203.
//
// A Watchdog from Open also renews the output lease on each Pet, which is what
// keeps the relays allowed on (ADR 0207). A Watchdog from NewWatchdog does not.
type SystemdWatchdog struct {
	mu    sync.Mutex
	addr  string
	conn  *net.UnixConn
	lease *guardClient
}

// NewWatchdog reads NOTIFY_SOCKET. Pet returns hal.ErrAbsent when the process
// was not started by systemd with a notify socket.
func NewWatchdog() *SystemdWatchdog {
	return &SystemdWatchdog{addr: os.Getenv("NOTIFY_SOCKET")}
}

const notifyBound = 100 * time.Millisecond

func (w *SystemdWatchdog) send(msg string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.addr == "" {
		return absent("watchdog: no NOTIFY_SOCKET", nil)
	}
	if w.conn == nil {
		c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: w.addr, Net: "unixgram"})
		if err != nil {
			return absent("watchdog", err)
		}
		w.conn = c
	}
	// A full socket queue must not stall the control loop.
	if err := w.conn.SetWriteDeadline(time.Now().Add(notifyBound)); err != nil {
		return err
	}
	if _, err := w.conn.Write([]byte(msg)); err != nil {
		w.conn.Close()
		w.conn = nil
		return absent("watchdog", err)
	}
	return nil
}

// Pet tells systemd and the output guard that the control loop is alive. Both are
// always attempted. The error wraps hal.ErrOutputsDropped after a lease drop.
func (w *SystemdWatchdog) Pet() error {
	var renewErr error
	if w.lease != nil {
		renewErr = w.lease.Renew()
	}
	return errors.Join(renewErr, w.send("WATCHDOG=1"))
}

// Ready tells systemd startup has finished, for a Type=notify unit.
func (w *SystemdWatchdog) Ready() error { return w.send("READY=1") }

// Stopping tells systemd a clean stop has begun.
func (w *SystemdWatchdog) Stopping() error { return w.send("STOPPING=1") }
