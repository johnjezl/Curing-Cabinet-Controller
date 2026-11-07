package drivers

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

type guardLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *guardLog) logf(prio int, f string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, f)
}

type guardRig struct {
	g    *guardCore
	r    *Relays
	chip *fakeChip
	pins map[hal.OutputID]int
	clk  *virtualClock
	log  *guardLog
	bad  error
}

func newGuardRig(t *testing.T) *guardRig {
	t.Helper()
	r, chip, pins := relayRig()
	rig := &guardRig{r: r, chip: chip, pins: pins, clk: &virtualClock{t: time.Unix(1000, 0)}, log: &guardLog{}}
	ids := []hal.OutputID{}
	for id := range pins {
		ids = append(ids, id)
	}
	rig.g = newGuardCore(r, ids, 3*time.Second, rig.clk.Now, func() error { return rig.bad }, rig.log.logf)
	return rig
}

func (r *guardRig) level(id hal.OutputID) bool {
	l := r.chip.lines[r.pins[id]]
	return l != nil && l.level
}

func mustOK(t *testing.T, reply string) {
	t.Helper()
	if !strings.HasPrefix(reply, "ok") {
		t.Fatalf("reply %q", reply)
	}
}

func TestLeaseExpiryDropsEveryOutput(t *testing.T) {
	r := newGuardRig(t)
	for id := range r.pins {
		mustOK(t, r.g.Handle("c", "set "+string(id)+" 1"))
	}
	r.clk.Sleep(2900 * time.Millisecond)
	if err := r.g.Tick(); err != nil {
		t.Fatal(err)
	}
	if !r.level(hal.OutHeat) {
		t.Fatal("dropped before the lease ended")
	}
	r.clk.Sleep(200 * time.Millisecond)
	if err := r.g.Tick(); err != nil {
		t.Fatal(err)
	}
	for id := range r.pins {
		if r.level(id) {
			t.Errorf("%s still on after the lease ended", id)
		}
	}
	if got := r.g.Handle("c", "state"); !strings.Contains(got, "dropped=1 on=none") {
		t.Errorf("state %q", got)
	}
}

func TestRenewKeepsOutputsOnButDoesNotRestoreThem(t *testing.T) {
	r := newGuardRig(t)
	mustOK(t, r.g.Handle("c", "set heat 1"))
	for i := 0; i < 10; i++ {
		r.clk.Sleep(2 * time.Second)
		mustOK(t, r.g.Handle("c", "renew"))
		r.g.Tick()
		if !r.level(hal.OutHeat) {
			t.Fatalf("dropped despite renewal %d", i)
		}
	}
	r.clk.Sleep(4 * time.Second)
	r.g.Tick()
	if r.level(hal.OutHeat) {
		t.Fatal("not dropped")
	}
	// A loop that comes back must not silently re-energize: that could short-cycle a compressor.
	got := r.g.Handle("c", "renew")
	if !strings.Contains(got, "dropped=1") || r.level(hal.OutHeat) {
		t.Fatalf("renew after drop: %q, heat %v", got, r.level(hal.OutHeat))
	}
	mustOK(t, r.g.Handle("c", "set heat 1"))
	if !r.level(hal.OutHeat) {
		t.Fatal("an explicit command must restore the output")
	}
	if got := r.g.Handle("c", "state"); !strings.Contains(got, "dropped=0") {
		t.Errorf("state %q", got)
	}
}

func TestDisconnectOfControllingClientDropsAtOnce(t *testing.T) {
	r := newGuardRig(t)
	mustOK(t, r.g.Handle("a", "set cool 1"))
	if err := r.g.Disconnected("someone else"); err != nil {
		t.Fatal(err)
	}
	if !r.level(hal.OutCool) {
		t.Fatal("a diagnostic client leaving must not drop the outputs")
	}
	if err := r.g.Disconnected("a"); err != nil {
		t.Fatal(err)
	}
	if r.level(hal.OutCool) {
		t.Fatal("still on")
	}
}

func TestStateRequestDoesNotExtendLease(t *testing.T) {
	r := newGuardRig(t)
	mustOK(t, r.g.Handle("c", "set heat 1"))
	for i := 0; i < 5; i++ {
		r.clk.Sleep(time.Second)
		r.g.Handle("diag", "state")
		r.g.Tick()
	}
	if r.level(hal.OutHeat) {
		t.Fatal("a state query kept the output on")
	}
}

func TestUnsafeParamRefusesOnAndDropsWhatIsOn(t *testing.T) {
	r := newGuardRig(t)
	mustOK(t, r.g.Handle("c", "set heat 1"))
	r.bad = ErrUnsafeState
	r.clk.Sleep(time.Second + time.Millisecond)
	mustOK(t, r.g.Handle("c", "renew"))
	if err := r.g.Tick(); err != nil {
		t.Fatal(err)
	}
	if r.level(hal.OutHeat) {
		t.Fatal("output left on with the precondition failed")
	}
	if got := r.g.Handle("c", "set heat 1"); !strings.HasPrefix(got, "err") || r.level(hal.OutHeat) {
		t.Fatalf("set on while unsafe: %q", got)
	}
	if got := r.g.Handle("c", "renew"); !strings.HasPrefix(got, "err") {
		t.Fatalf("renew while unsafe: %q", got)
	}
	mustOK(t, r.g.Handle("c", "set heat 0"))
	r.bad = nil
	r.clk.Sleep(time.Second + time.Millisecond)
	r.g.Tick()
	mustOK(t, r.g.Handle("c", "set heat 1"))
	if !r.level(hal.OutHeat) {
		t.Fatal("not restored after the precondition recovered")
	}
}

func TestDropFailureIsReportedSoTheProcessExits(t *testing.T) {
	r := newGuardRig(t)
	mustOK(t, r.g.Handle("c", "set heat 1"))
	bad := errors.New("line write failed")
	r.chip.lines[r.pins[hal.OutHeat]].setErr = bad
	r.clk.Sleep(4 * time.Second)
	if err := r.g.Tick(); !errors.Is(err, bad) {
		t.Fatalf("err = %v", err)
	}
}

func TestGuardBadRequests(t *testing.T) {
	r := newGuardRig(t)
	for _, q := range []string{"", "set", "set heat", "set heat 2", "set freezer 1", "launch"} {
		if got := r.g.Handle("c", q); !strings.HasPrefix(got, "err") {
			t.Errorf("%q: %q", q, got)
		}
	}
}

// serveGuard runs the request loop over a real socket with a real clock.
func serveGuard(t *testing.T, r *guardRig) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "g.sock")
	ln, err := net.Listen("unixpacket", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
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
					if err != nil {
						r.g.Disconnected(conn)
						return
					}
					conn.Write([]byte(r.g.Handle(conn, string(buf[:n]))))
				}
			}(conn)
		}
	}()
	return sock
}

func TestClientOverSocket(t *testing.T) {
	r := newGuardRig(t)
	c := newGuardClient(serveGuard(t, r))
	var _ hal.Outputs = c
	if err := c.Set(hal.OutHeat, true); err != nil {
		t.Fatal(err)
	}
	if !r.level(hal.OutHeat) {
		t.Fatal("not on")
	}
	if err := c.Renew(); err != nil {
		t.Fatal(err)
	}
	r.clk.Sleep(4 * time.Second)
	r.g.Tick()
	if err := c.Renew(); !errors.Is(err, hal.ErrOutputsDropped) {
		t.Fatalf("renew after drop: %v", err)
	}
	if err := c.Set(hal.OutHeat, true); err != nil {
		t.Fatal(err)
	}
	if err := c.AllOff(); err != nil {
		t.Fatal(err)
	}
	if r.level(hal.OutHeat) {
		t.Fatal("still on")
	}
	if err := c.Set("freezer", true); err == nil || errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("a guard refusal must not read as absent: %v", err)
	}
	if err := c.Set(hal.OutHeat, true); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if r.level(hal.OutHeat) {
		t.Fatal("Close left an output on")
	}
}

func TestClientWithNoGuardIsAbsent(t *testing.T) {
	c := newGuardClient(filepath.Join(t.TempDir(), "none"))
	if err := c.Set(hal.OutHeat, false); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close with no guard: %v", err)
	}
}

func TestClientHungGuardReturnsWithinBound(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "g.sock")
	ln, err := net.Listen("unixpacket", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() // accepts nothing and never answers
	c := newGuardClient(sock)
	start := time.Now()
	if err := c.Set(hal.OutHeat, true); !errors.Is(err, hal.ErrAbsent) {
		t.Fatalf("err = %v", err)
	}
	if el := time.Since(start); el > 200*time.Millisecond {
		t.Errorf("took %v", el)
	}
}

func TestOpenWatchdogRenewsLease(t *testing.T) {
	r := newGuardRig(t)
	cfg := DefaultConfig()
	cfg.GuardSocket = serveGuard(t, r)
	t.Setenv("NOTIFY_SOCKET", "")
	cab, wd, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer cab.Close()
	if err := cab.Outputs.Set(hal.OutHeat, true); err != nil {
		t.Fatal(err)
	}
	r.clk.Sleep(2 * time.Second)
	if err := wd.Pet(); !errors.Is(err, hal.ErrAbsent) { // no systemd socket here
		t.Fatalf("pet: %v", err)
	}
	r.clk.Sleep(2 * time.Second)
	r.g.Tick()
	if !r.level(hal.OutHeat) {
		t.Fatal("Pet did not renew the lease")
	}
	r.clk.Sleep(4 * time.Second)
	r.g.Tick()
	if err := wd.Pet(); !errors.Is(err, hal.ErrOutputsDropped) {
		t.Fatalf("pet after drop: %v", err)
	}
}

func TestCheckPersistOff(t *testing.T) {
	fs := newFakeFS()
	if err := checkPersistOff(fs); !errors.Is(err, ErrUnsafeState) {
		t.Errorf("missing parameter (kernel changed): %v", err)
	}
	fs.files[persistParam] = "Y\n"
	if err := checkPersistOff(fs); !errors.Is(err, ErrUnsafeState) {
		t.Errorf("Y: %v", err)
	}
	fs.files[persistParam] = "N\n"
	if err := checkPersistOff(fs); err != nil {
		t.Errorf("N: %v", err)
	}
}

func TestCheckFanPWM(t *testing.T) {
	cfg := DefaultConfig()
	if err := checkFanPWM(pwmFS(), cfg); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*fakeFS){
		"wrong period": func(f *fakeFS) { f.files[testCh+"/period"] = "1000000\n" },
		"disabled":     func(f *fakeFS) { f.files[testCh+"/enable"] = "0\n" },
		"not exported": func(f *fakeFS) { delete(f.files, testCh+"/period") },
	} {
		fs := pwmFS()
		mutate(fs)
		if err := checkFanPWM(fs, cfg); !errors.Is(err, ErrUnsafeState) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDisplayMissing(t *testing.T) {
	fs := newFakeFS()
	if _, _, err := displayMissing(fs); err == nil {
		t.Error("no status file must be an error, not a healthy display")
	}
	fs.files[displayStatusFile] = "fb0=yes\ntouch=yes\nuptime=30.1\n"
	if missing, detail, err := displayMissing(fs); err != nil || missing || !strings.Contains(detail, "fb0=yes") {
		t.Errorf("up: %v %q %v", missing, detail, err)
	}
	fs.files[displayStatusFile] = "fb0=no\ntouch=no\nuptime=55\n"
	fs.files[displayMissingFile] = "fb0=no\ntouch=no\nuptime=55\n"
	if missing, detail, err := displayMissing(fs); err != nil || !missing || !strings.Contains(detail, "fb0=no") {
		t.Errorf("missing: %v %q %v", missing, detail, err)
	}
}
