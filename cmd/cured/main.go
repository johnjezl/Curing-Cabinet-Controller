// Command cured is the curing cabinet controller: control core, front panel and
// supervision in one static binary.
//
//	cured              real hardware. This is what curing.service runs.
//	cured --sim ...    simulated plant. Refuses to start without every one of its
//	                   explicit arguments; see simGate and docs/adr/0401.
//	cured post-stop    run by systemd after the service ends, however it ended.
//	                   With --sim (the simulator's unit) it leaves the guard alone.
//	cured simctl ...   client of the control socket that exists only in --sim.
//	cured version      prints the version.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/drivers"
	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui"
)

// version is set at link time.
var version = "dev"

const (
	runDir       = "/run/curing-cured"
	statusFile   = runDir + "/status.json"
	startsFile   = runDir + "/starts"
	simSock      = runDir + "/sim.sock"
	realStateDir = "/var/lib/curing/core"
	stopBound    = 45 * time.Second // outputs are off first; a slow card only delays the clean flag
)

func logfJ(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }

func main() {
	log.SetFlags(log.Lmicroseconds)
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Println(version)
			return
		case "post-stop":
			postStop(len(args) > 1 && args[1] == "--sim")
			return
		case "simctl":
			os.Exit(simctlMain(args[1:]))
		case "sim-wipe":
			os.Exit(simWipeMain(args[1:]))
		}
	}
	simMode, opts, err := parseArgs(args)
	if err != nil {
		logfJ("<3>cured: refusing to start: %v", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if simMode {
		err = runSim(ctx, opts)
	} else {
		err = runReal(ctx)
	}
	if err != nil {
		logfJ("<2>cured: %v", err)
		os.Exit(1)
	}
	// A deliberate stop is not part of a crash loop.
	_ = os.Remove(startsFile)
}

// parseArgs accepts no arguments (real hardware) or the complete simulator set.
func parseArgs(args []string) (bool, simOptions, error) {
	if len(args) == 0 {
		return false, simOptions{}, nil
	}
	o, err := parseSimArgs(args)
	if err != nil {
		return false, o, err
	}
	if err := simGate(o, statExists); err != nil {
		return false, o, err
	}
	return true, o, nil
}

func statExists(p string) bool { _, err := os.Stat(p); return err == nil }

// uptimeNow is seconds since the kernel started, or -1.
func uptimeNow() float64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return -1
	}
	v, err := strconv.ParseFloat(strings.Fields(string(b))[0], 64)
	if err != nil {
		return -1
	}
	return v
}

// postStop runs after the service ended, however it ended. It must not assume
// anything about what state the process left behind. It does not release the
// lines with pinctrl: the guard owns them and may be alive, and changing a line it
// holds behind its back would stop it driving that line later. After a simulator
// it does not talk to the guard at all: the simulator never asked it for anything
// (ADR 0406), and the guard has kept every line off throughout.
func postStop(sim bool) {
	t := time.AfterFunc(stopBound, func() { logfJ("<3>cured post-stop: gave up"); os.Exit(3) })
	defer t.Stop()
	logfJ("<5>cured post-stop: result=%s exit_code=%s exit_status=%s sim=%v uptime=%.2f", os.Getenv("SERVICE_RESULT"), os.Getenv("EXIT_CODE"), os.Getenv("EXIT_STATUS"), sim, uptimeNow())
	var cab hal.Cabinet
	var err error
	if !sim {
		cab, err = drivers.New(drivers.DefaultConfig())
	}
	switch {
	case sim:
	case err != nil:
		logfJ("<3>cured post-stop: drivers: %v", err)
	default:
		if err := cab.Outputs.AllOff(); err != nil {
			// No guard means nothing owns the lines and the kernel has stopped driving them.
			logfJ("<4>cured post-stop: all off: %v", err)
		} else {
			logfJ("<5>cured post-stop: outputs off confirmed by guard")
		}
		_ = cab.Close()
	}
	// A dead process's status must not read as live.
	_ = os.Remove(statusFile)
	if err := ui.PaintStopped(ui.DefaultFramebuffer); err != nil {
		logfJ("<4>cured post-stop: panel: %v", err)
	} else {
		logfJ("<5>cured post-stop: panel painted stopped uptime=%.2f", uptimeNow())
	}
}
