package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// serve opens the control socket. It exists only in --sim: runReal has no path to
// this code, so a real cabinet has no way to inject anything.
func (s *simState) serve(ctx context.Context) error {
	_ = os.Remove(simSock)
	l, err := net.Listen("unix", simSock)
	if err != nil {
		return err
	}
	if err := os.Chmod(simSock, 0o660); err != nil {
		l.Close()
		return err
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.handleConn(c)
		}
	}()
	return nil
}

func (s *simState) handleConn(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 4096), 1<<20)
	if !sc.Scan() {
		return
	}
	var req []string
	var resp any
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil || len(req) == 0 {
		resp = map[string]any{"error": "bad request"}
	} else {
		v, err := s.handle(req)
		if err != nil {
			resp = map[string]any{"error": err.Error()}
		} else {
			resp = v
		}
	}
	b, err := json.Marshal(resp)
	if err != nil {
		b, _ = json.Marshal(map[string]any{"error": err.Error()})
	}
	_, _ = c.Write(append(b, '\n'))
}

// onPlant runs f between ticks and waits briefly for it, so a hung loop reports an
// error rather than blocking the client.
func (s *simState) onPlant(f func()) error {
	done := make(chan struct{})
	select {
	case s.inbox <- func() { f(); close(done) }:
	default:
		return errors.New("simulation is busy")
	}
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("simulation loop did not answer")
	}
}

func argFloat(a []string, i int) (float64, error) {
	if i >= len(a) {
		return 0, fmt.Errorf("missing argument %d", i)
	}
	v, err := strconv.ParseFloat(a[i], 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number; give a plain number such as -0.5", a[i])
	}
	return v, nil
}

// num writes a value as typed, without exponents for ordinary sizes.
func num(v float64) string {
	if math.Abs(v) < 1e9 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// names lists a table's keys, for a refusal that says what would work.
func names[V any](m map[string]V) string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func (s *simState) handle(a []string) (any, error) {
	ok := map[string]any{"ok": true}
	do := func(c api.Command) (any, error) {
		if err := s.r.v.Do(c); err != nil {
			return nil, err
		}
		return ok, nil
	}
	cmd, a := a[0], a[1:]
	switch cmd {
	case "state":
		snap := s.r.v.Snapshot()
		snap.Trend, snap.Recipes = nil, nil
		return map[string]any{"status": s.r.statusMap(), "snapshot": snap, "outages": s.outagesCopy()}, nil
	case "door":
		if len(a) != 1 || (a[0] != "open" && a[0] != "close") {
			return nil, errors.New("door open|close")
		}
		return ok, s.onPlant(func() { s.pl.DoorIsOpen = a[0] == "open" })
	case "load": // warm product: kg, temperature
		kg, err := argFloat(a, 0)
		if err != nil {
			return nil, err
		}
		tc, err := argFloat(a, 1)
		if err != nil {
			return nil, err
		}
		return ok, s.onPlant(func() { s.pl.LoadProduct(kg, tc) })
	case "ambient": // offset in degrees
		v, err := argFloat(a, 0)
		if err != nil {
			return nil, err
		}
		return ok, s.onPlant(func() { s.pl.AmbientOffset = v })
	case "fault":
		if len(a) < 2 {
			return nil, errors.New("fault <device> <kind> [param]")
		}
		d, okD := devNames[a[0]]
		k, okK := faultNames[a[1]]
		if !okD {
			return nil, fmt.Errorf("unknown device %q; devices: %s", a[0], names(devNames))
		}
		if !okK {
			return nil, fmt.Errorf("unknown fault %q; faults: %s", a[1], names(faultNames))
		}
		p := 0.0
		if len(a) > 3 {
			return nil, errors.New("fault <device> <kind> [value]: one value at most")
		}
		if len(a) > 2 {
			v, err := argFloat(a, 2)
			if err != nil {
				return nil, err
			}
			p = v
		}
		reply := map[string]any{"ok": true, "injected": d.String() + ":" + k.String()}
		if len(a) > 2 {
			reply["value"] = p // what was applied, as parsed
		}
		var refused error
		err := s.onPlant(func() {
			if refused = s.pl.CheckInjection(d, k, p, len(a) > 2); refused != nil {
				return
			}
			switch prev := s.pl.FaultOn(d); {
			case prev == k && k == sim.FaultDrift:
				reply["restarted"] = d.String() + ":drift (what it had built up starts again from zero)"
			case prev == k && s.pl.FaultParam(d) == p:
				// Re-freezing a stuck reading, or an offset it already has, changes nothing.
				refused = fmt.Errorf("%s %s is already injected with this value, so this would change nothing; repair %s first to start it again", d, k, d)
				return
			case prev == k && s.pl.SameReading(d, k, s.pl.FaultParam(d), p):
				refused = fmt.Errorf("%s %s %s would leave the reading exactly where %s %s %s already puts it, beyond what the controller accepts; repair %s first to start again",
					d, k, num(p), d, k, num(s.pl.FaultParam(d)), d)
				return
			case prev == k:
				reply["changed"] = fmt.Sprintf("%s:%s %s -> %s", d, k, num(s.pl.FaultParam(d)), num(p))
			case prev != sim.FaultNone:
				reply["replaced"] = d.String() + ":" + prev.String() // one fault per device
			}
			if d == sim.DevClimate { // a chip fault hides what is injected on its readings
				var masked []string
				for _, r := range []sim.Device{sim.DevTemp, sim.DevRH} {
					if f := s.pl.FaultOn(r); f != sim.FaultNone {
						masked = append(masked, r.String()+":"+f.String())
					}
				}
				if len(masked) > 0 {
					reply["masks"] = strings.Join(masked, ", ") + " until climate is repaired"
				}
			}
			s.pl.Inject(d, k, p)
			if why := s.pl.Latent(d, k, p); why != "" {
				reply["latent"] = why
			}
			if k == sim.FaultOffset && s.pl.OffsetActsAsRange(d, p) {
				reply["note"] = "this puts the reading beyond what the controller accepts, so it acts as a range fault"
			}
			s.saveCheckpoint(false)
		})
		if refused != nil {
			return nil, fmt.Errorf("refused: %v", refused)
		}
		if err == nil {
			s.savedNote(reply)
		}
		return reply, err
	case "repair":
		if len(a) != 1 {
			return nil, errors.New("repair <device>|all")
		}
		if a[0] == "all" {
			var done []string
			err := s.onPlant(func() {
				for _, d := range devNames {
					if prev := s.pl.FaultOn(d); prev != sim.FaultNone {
						done = append(done, d.String()+":"+prev.String())
					}
					s.pl.Inject(d, sim.FaultNone, 0)
				}
				s.saveCheckpoint(false)
			})
			sort.Strings(done)
			reply := map[string]any{"ok": true, "repaired": done}
			if len(done) == 0 {
				reply = map[string]any{"ok": true, "note": "no fault was injected"}
			}
			if err == nil {
				s.savedNote(reply)
			}
			return reply, err
		}
		d, okD := devNames[a[0]]
		if !okD {
			return nil, fmt.Errorf("unknown device %q; devices: %s", a[0], names(devNames))
		}
		reply := map[string]any{"ok": true}
		err := s.onPlant(func() {
			if prev := s.pl.FaultOn(d); prev == sim.FaultNone {
				reply["note"] = "no fault was injected on " + d.String()
			} else {
				reply["repaired"] = d.String() + ":" + prev.String()
			}
			s.pl.Inject(d, sim.FaultNone, 0)
			s.saveCheckpoint(false)
		})
		if err == nil {
			s.savedNote(reply)
		}
		return reply, err
	case "start":
		if len(a) != 1 {
			return nil, errors.New("start <recipe id>")
		}
		if a[0] == simRecipeID {
			if err := s.r.v.Do(api.Command{Kind: api.CmdSaveRecipe, Recipe: testRecipe()}); err != nil {
				return nil, err
			}
		}
		return do(api.Command{Kind: api.CmdStart, RecipeID: a[0]})
	case "setpoint":
		t, err := argFloat(a, 0)
		if err != nil {
			return nil, err
		}
		h, err := argFloat(a, 1)
		if err != nil {
			return nil, err
		}
		return do(api.Command{Kind: api.CmdSetSetpoint, Setpoint: &api.Setpoints{TempC: t, RHPct: h, AirMS: 1.0}})
	case "silence":
		return do(api.Command{Kind: api.CmdSilence})
	case "ack":
		return do(api.Command{Kind: api.CmdAck})
	case "pause":
		return do(api.Command{Kind: api.CmdPause})
	case "resume":
		return do(api.Command{Kind: api.CmdResume})
	case "stop":
		return do(api.Command{Kind: api.CmdStop})
	case "defrost":
		return do(api.Command{Kind: api.CmdDefrost})
	case "inject":
		if len(a) != 1 {
			return nil, errors.New("inject panic-control|hang-control|panic-ui|stall-ui")
		}
		switch a[0] {
		case "panic-control":
			s.injectPanic.Store(true)
		case "hang-control":
			s.injectHang.Store(true)
		case "panic-ui":
			s.uiFault.Store(1)
		case "stall-ui":
			s.uiFault.Store(2)
		default:
			return nil, fmt.Errorf("unknown injection %q", a[0])
		}
		return ok, nil
	}
	return nil, fmt.Errorf("unknown command %q", cmd)
}

// simctlMain is the client. With no simulator running there is no socket, and that
// is the error it reports.
func simctlMain(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: cured simctl <state|door|load|ambient|fault|repair|start|setpoint|silence|ack|pause|resume|stop|defrost|inject> [args]")
		return 2
	}
	c, err := net.DialTimeout("unix", simSock, 3*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cured simctl: no simulator control socket (it exists only in --sim): %v\n", err)
		return 3
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(25 * time.Second))
	b, _ := json.Marshal(args)
	if _, err := c.Write(append(b, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 65536), 8<<20)
	if !sc.Scan() {
		fmt.Fprintln(os.Stderr, "cured simctl: no reply", sc.Err())
		return 3
	}
	fmt.Println(sc.Text())
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(sc.Bytes(), &e) == nil && e.Error != "" {
		return 1
	}
	return 0
}

// simWipeMain removes the simulator's state, never the real cabinet's. The caller
// has stopped the service.
func simWipeMain(args []string) int {
	if len(args) != 1 || args[0] != simConfirmToken {
		fmt.Fprintln(os.Stderr, "usage: cured sim-wipe "+simConfirmToken)
		return 2
	}
	if statExists(productionMarker) {
		fmt.Fprintln(os.Stderr, "cured sim-wipe: this machine is a real cabinet")
		return 2
	}
	entries, err := os.ReadDir(simStateDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(simStateDir, e.Name())); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	fmt.Println("sim state wiped")
	return 0
}
