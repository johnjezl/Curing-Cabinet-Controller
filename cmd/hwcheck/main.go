// Command hwcheck is a rig diagnostic, run through deploy/rig.sh. It prints raw
// measurements of the real drivers and pin state and has no pass or fail logic.
// Where it turns outputs on, it says so: it is a test driver, not the control path.
//
// Registers come from a read-only mapping of /dev/gpiomem0, which is the RP1's
// IO_BANK0, RIO and PADS_BANK0: status at 8*n, ctrl at 8*n+4, pad at
// 0x20000+4+4*n. In the status word, bit 9 is the level the peripheral drives
// to the pad, bit 13 its output enable and bit 17 the level at the pad.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/johnjezl/curing-cabinet-controller/internal/drivers"
	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("usage: hwcheck driver-calls|pwm|fan-duty|lines|test-driver|time-to-safe|safe-state|try-on|hx711-timing|read-sensors|relay-click <output>|fan-sweep"))
	}
	switch os.Args[1] {
	case "driver-calls":
		driverCalls()
	case "pwm":
		pwm()
	case "fan-duty":
		fanDuty()
	case "lines":
		lines()
	case "test-driver":
		testDriver()
	case "time-to-safe":
		timeToSafe()
	case "safe-state":
		safeState()
	case "try-on":
		tryOn()
	case "hx711-timing":
		hxTiming()
	case "read-sensors":
		readSensors()
	case "relay-click":
		if len(os.Args) != 3 {
			fatal(errors.New("usage: hwcheck relay-click <output>"))
		}
		relayClick(hal.OutputID(os.Args[2]))
	case "fan-sweep":
		fanSweep()
	default:
		fatal(fmt.Errorf("unknown subcommand %s", os.Args[1]))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "hwcheck:", err)
	os.Exit(1)
}

func cabinet() (hal.Cabinet, *drivers.SystemdWatchdog) {
	c, wd, err := drivers.Open(drivers.DefaultConfig())
	if err != nil {
		fatal(err)
	}
	return c, wd
}

// testDriverLabel marks every capture in which outputs are on.
const testDriverLabel = "DELIBERATE TEST DRIVER: hwcheck is commanding these outputs on. This is not the control path."

// call runs f and prints one fixed-format result line.
func call(driver, op string, boundMS int, f func() (string, error)) {
	start := time.Now()
	val, err := f()
	el := time.Since(start)
	msg := "nil"
	if err != nil {
		msg = err.Error()
	}
	fmt.Printf("driver=%-8s op=%-14s elapsed_ms=%8.3f bound_ms=%-4d within_bound=%-5v is_ErrAbsent=%-5v is_ErrBadRead=%-5v value=%s err=%q\n",
		driver, op, float64(el.Microseconds())/1000, boundMS, boundMS == 0 || el <= time.Duration(boundMS)*time.Millisecond,
		errors.Is(err, hal.ErrAbsent), errors.Is(err, hal.ErrBadRead), val, msg)
}

// driverCalls calls each driver and prints what it returned. Whether anything is
// wired is a fact about the bench, not something this tool checks.
func driverCalls() {
	fmt.Printf("# hwcheck driver-calls uid=%d time=%s\n", os.Getuid(), time.Now().UTC().Format(time.RFC3339))
	c, wd := cabinet()
	defer c.Close()
	climate := func() (string, error) {
		t, rh, age, err := c.Climate.Read()
		return fmt.Sprintf("%.1fC/%.1f%%/age%.1fs", t, rh, age), err
	}
	call("dht22", "Read(first)", 500, climate)
	for i := 0; i < 4; i++ {
		time.Sleep(3 * time.Second)
		call("dht22", "Read", 500, climate)
	}
	for i := 0; i < 5; i++ {
		call("fs3000", "Read", 100, func() (string, error) {
			v, err := c.Air.Read()
			return fmt.Sprintf("%.3f", v), err
		})
	}
	for i := 0; i < 3; i++ {
		call("hx711", "ReadRaw", 200, func() (string, error) {
			v, err := c.Scale.ReadRaw()
			return strconv.Itoa(int(v)), err
		})
	}
	rpm := func() (string, error) {
		v, err := c.Fan.RPM()
		return fmt.Sprintf("%.0f", v), err
	}
	call("fan", "SetDuty(0)", 100, func() (string, error) { return "", c.Fan.SetDuty(0) })
	call("fan", "RPM(duty=0)", 100, rpm)
	call("fan", "SetDuty(50)", 100, func() (string, error) { return "", c.Fan.SetDuty(50) })
	call("fan", "RPM(t=0s)", 100, rpm)
	time.Sleep(5500 * time.Millisecond)
	call("fan", "RPM(t=5.5s)", 100, rpm)
	call("fan", "SetDuty(0)", 100, func() (string, error) { return "", c.Fan.SetDuty(0) })
	call("door", "Open", 100, func() (string, error) {
		v, err := c.Door.Open()
		return strconv.FormatBool(v), err
	})
	call("relays", "AllOff", 100, func() (string, error) { return "", c.Outputs.AllOff() })
	call("watchdog", "Pet(no socket)", 100, func() (string, error) { return "", wd.Pet() })
	call("cabinet", "Close", 3000, func() (string, error) { return "", c.Close() })
}

// regs maps the RP1 GPIO registers read-only.
func regs() []byte {
	f, err := os.Open("/dev/gpiomem0")
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	m, err := syscall.Mmap(int(f.Fd()), 0, 0x30000, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		fatal(fmt.Errorf("mmap gpiomem0: %w", err))
	}
	return m
}

func reg32(m []byte, off int) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(&m[off])))
}

const (
	statusOutToPad = 1 << 9
	statusOEToPad  = 1 << 13
	statusInPad    = 1 << 17
)

func lines() {
	cfg := drivers.DefaultConfig()
	m := regs()
	ids := make([]string, 0, len(cfg.Relays))
	for id := range cfg.Relays {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := cfg.Relays[hal.OutputID(id)]
		st, ctrl, pad := reg32(m, 8*n), reg32(m, 8*n+4), reg32(m, 0x20000+4+4*n)
		fmt.Printf("output=%-8s gpio=%-2d out=%d oe=%d pad_in=%d funcsel=%d pull_down=%d pull_up=%d status=0x%08x ctrl=0x%08x pad=0x%08x\n",
			id, n, b(st&statusOutToPad), b(st&statusOEToPad), b(st&statusInPad), ctrl&0x1f, b(pad&(1<<2)), b(pad&(1<<3)), st, ctrl, pad)
	}
}

func b(v uint32) int {
	if v != 0 {
		return 1
	}
	return 0
}

// pwm sets each duty through the real driver and measures GPIO 12's output
// signal by sampling the RP1 status register: edges per second and high fraction.
func pwm() {
	c, _ := cabinet()
	defer c.Close()
	m := regs()
	for _, duty := range []float64{10, 25, 50, 75, 90, 100, 0} {
		if err := c.Fan.SetDuty(duty); err != nil {
			fmt.Printf("set_duty=%.0f err=%q\n", duty, err.Error())
			continue
		}
		time.Sleep(50 * time.Millisecond)
		fmt.Printf("set_duty=%-3.0f %s\n", duty, sampleFan(m, 2000000))
	}
}

const fanGPIO = 12

// sampleFan measures GPIO 12's output signal from the RP1 status register. This
// is the SoC reporting on itself: no external instrument and no fan.
func sampleFan(m []byte, samples int) string {
	off := 8 * fanGPIO
	var high, rising, oe uint64
	prev := reg32(m, off) & statusOutToPad
	start := time.Now()
	for i := 0; i < samples; i++ {
		st := reg32(m, off)
		v := st & statusOutToPad
		if v != 0 {
			high++
			if prev == 0 {
				rising++
			}
		}
		if st&statusOEToPad != 0 {
			oe++
		}
		prev = v
	}
	el := time.Since(start).Seconds()
	return fmt.Sprintf("samples=%d sample_rate_hz=%.0f oe_fraction=%.3f rising_edges=%d register_hz=%.1f register_duty_pct=%.2f funcsel=%d",
		samples, float64(samples)/el, float64(oe)/float64(samples), rising, float64(rising)/el, 100*float64(high)/float64(samples), reg32(m, off+4)&0x1f)
}

func fanDuty() { fmt.Println("fan_pin", sampleFan(regs(), 500000)) }

var allOutputs = []hal.OutputID{hal.OutCool, hal.OutHeat, hal.OutHumid, hal.OutDehumid, hal.OutAlarm, hal.OutBuzzer}

// testDriver stands in for a control loop: it turns every output on and the fan to
// 50 %, then renews the lease every 200 ms. SIGUSR1 makes it stop renewing while
// staying alive, as a stuck control goroutine would.
func testDriver() {
	fmt.Println(testDriverLabel)
	c, wd := cabinet()
	for _, id := range allOutputs {
		if err := c.Outputs.Set(id, true); err != nil {
			fatal(err)
		}
	}
	if err := c.Fan.SetDuty(50); err != nil {
		fmt.Println("fan:", err)
	}
	stuck := make(chan os.Signal, 1)
	signal.Notify(stuck, syscall.SIGUSR1)
	fmt.Println("TEST DRIVER READY")
	t := time.NewTicker(200 * time.Millisecond)
	for {
		select {
		case <-stuck:
			fmt.Println("test driver stopped renewing, process still alive")
			select {}
		case <-t.C:
			wd.Pet()
		}
	}
}

type outLine struct {
	id  string
	off int
}

func outLines() []outLine {
	cfg := drivers.DefaultConfig()
	var l []outLine
	for _, id := range allOutputs {
		l = append(l, outLine{string(id), 8 * cfg.Relays[id]})
	}
	return l
}

func guardPID() int {
	out, err := exec.Command("systemctl", "show", "-p", "MainPID", "--value", "curing-outputd.service").Output()
	if err != nil {
		fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid == 0 {
		fatal(fmt.Errorf("output guard not running: %q", out))
	}
	return pid
}

func waitGuard() error {
	cfg := drivers.DefaultConfig()
	var err error
	for i := 0; i < 300; i++ {
		var st string
		if st, err = drivers.GuardState(cfg); err == nil && strings.Contains(st, "on=none") {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("guard not ready: %v", err)
}

// timeToSafe measures, for each way control can stop, how long until every relay
// line reads low at the pad, while a tight loop samples the RP1 status registers
// from this separate process. It also watches for a high glitch after that, through
// the guard's restart and new claim of the lines.
func timeToSafe() {
	fmt.Println("# " + testDriverLabel)
	fmt.Println("# Measured by this process from RP1 registers on the same board, with no relays attached.")
	self, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	m := regs()
	lease := drivers.OutputLease.Seconds()
	type scenario struct {
		name   string
		expect string
		act    func(child *os.Process) error
	}
	sig := func(p func(*os.Process) int, s syscall.Signal) func(*os.Process) error {
		return func(child *os.Process) error { return syscall.Kill(p(child), s) }
	}
	child := func(c *os.Process) int { return c.Pid }
	guard := func(*os.Process) int { return guardPID() }
	scenarios := []scenario{
		{"control_loop_stuck_process_alive", fmt.Sprintf("lease %.0f s", lease), sig(child, syscall.SIGUSR1)},
		{"control_process_frozen_SIGSTOP", fmt.Sprintf("lease %.0f s", lease), sig(child, syscall.SIGSTOP)},
		{"control_process_killed_SIGKILL", "socket close", sig(child, syscall.SIGKILL)},
		{"guard_frozen_SIGSTOP", "systemd WatchdogSec 2 s, then kernel release", sig(guard, syscall.SIGSTOP)},
		{"guard_killed_SIGKILL", "kernel release", sig(guard, syscall.SIGKILL)},
	}
	for _, sc := range scenarios {
		for rep := 0; rep < 3; rep++ {
			measureScenario(m, self, sc.name, sc.expect, rep, sc.act)
		}
	}
}

func measureScenario(m []byte, self, name, expect string, rep int, act func(*os.Process) error) {
	cmd := exec.Command(self, "test-driver")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fatal(err)
	}
	buf := make([]byte, 512)
	for got := ""; !strings.Contains(got, "TEST DRIVER READY"); {
		n, err := stdout.Read(buf)
		if err != nil {
			fatal(fmt.Errorf("test driver: %v, output %q", err, got))
		}
		got += string(buf[:n])
	}
	go func() {
		drain := make([]byte, 512)
		for {
			if _, err := stdout.Read(drain); err != nil {
				return
			}
		}
	}()
	time.Sleep(time.Second)
	lns := outLines()
	for _, l := range lns {
		if reg32(m, l.off)&statusInPad == 0 {
			fatal(fmt.Errorf("%s: %s not high before the event", name, l.id))
		}
	}
	fanBefore := sampleFan(m, 200000)

	runtime.LockOSThread()
	safeAt := make([]time.Duration, len(lns))
	remaining := len(lns)
	t0 := time.Now()
	if err := act(cmd.Process); err != nil {
		fatal(err)
	}
	var samples, maxGap int64
	last := time.Since(t0)
	for remaining > 0 && last < 60*time.Second {
		for i, l := range lns {
			if safeAt[i] == 0 && reg32(m, l.off)&statusInPad == 0 {
				safeAt[i] = time.Since(t0)
				remaining--
			}
		}
		samples++
		now := time.Since(t0)
		if g := int64(now - last); g > maxGap {
			maxGap = g
		}
		last = now
	}
	var slowest time.Duration
	for _, d := range safeAt {
		if d > slowest {
			slowest = d
		}
	}
	// Keep watching for any line going high again, through a restart of the guard.
	var glitches int64
	watchEnd := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(watchEnd) {
		for _, l := range lns {
			if reg32(m, l.off)&statusInPad != 0 {
				glitches++
			}
		}
	}
	runtime.UnlockOSThread()
	guardBack, guardErr := drivers.GuardState(drivers.DefaultConfig())
	if guardErr != nil {
		guardBack = "not answering yet"
	}
	fanAfter := sampleFan(m, 200000)
	fmt.Printf("scenario=%s run=%d expected_path=%q all_lines_low_after_ms=%.3f still_high=%d sample_loops=%d max_gap_us=%.1f high_samples_in_next_1500ms=%d guard=%q\n",
		name, rep, expect, float64(slowest.Microseconds())/1000, remaining, samples, float64(maxGap)/1000, glitches, guardBack)
	fmt.Printf("  fan_before %s\n  fan_after  %s\n", fanBefore, fanAfter)
	syscall.Kill(cmd.Process.Pid, syscall.SIGCONT)
	cmd.Process.Kill()
	cmd.Wait()
	if err := waitGuard(); err != nil {
		fatal(err)
	}
}

func safeState() {
	err := drivers.VerifySafeState(drivers.DefaultConfig())
	fmt.Printf("VerifySafeState is_ErrUnsafeState=%v err=%v\n", errors.Is(err, drivers.ErrUnsafeState), err)
	missing, detail, err := drivers.DisplayMissing()
	fmt.Printf("DisplayMissing missing=%v detail=%q err=%v\n", missing, detail, err)
}

// tryOn asks for one output on and off again and prints what the guard said.
func tryOn() {
	fmt.Println("# " + testDriverLabel)
	c, _ := cabinet()
	defer c.Close()
	call("relays", "Set(heat,on)", 100, func() (string, error) { return "", c.Outputs.Set(hal.OutHeat, true) })
	cfg := drivers.DefaultConfig()
	m := regs()
	fmt.Printf("heat_pad_level=%d\n", b(reg32(m, 8*cfg.Relays[hal.OutHeat])&statusInPad))
	call("relays", "Set(heat,off)", 100, func() (string, error) { return "", c.Outputs.Set(hal.OutHeat, false) })
	fmt.Printf("heat_pad_level=%d\n", b(reg32(m, 8*cfg.Relays[hal.OutHeat])&statusInPad))
}

// The next three are for first power-on with real hardware; docs/wiring.md says
// what passing looks like.

func readSensors() {
	c, _ := cabinet()
	defer c.Close()
	fmt.Println("# reading each sensor for 30 s; see docs/wiring.md, bring-up checklist")
	for i := 0; i < 10; i++ {
		t, rh, age, err := c.Climate.Read()
		fmt.Printf("dht22 temp_c=%.1f rh_pct=%.1f age_s=%.1f err=%v\n", t, rh, age, err)
		v, err := c.Air.Read()
		fmt.Printf("fs3000 m_per_s=%.2f err=%v\n", v, err)
		raw, err := c.Scale.ReadRaw()
		fmt.Printf("hx711 raw=%d err=%v\n", raw, err)
		time.Sleep(3 * time.Second)
	}
}

func relayClick(id hal.OutputID) {
	fmt.Println("# " + testDriverLabel)
	c, wd := cabinet()
	defer c.Close()
	fmt.Printf("%s ON for 3 s\n", id)
	if err := c.Outputs.Set(id, true); err != nil {
		fatal(err)
	}
	for i := 0; i < 6; i++ {
		time.Sleep(500 * time.Millisecond)
		wd.Pet()
	}
	if err := c.Outputs.Set(id, false); err != nil {
		fatal(err)
	}
	fmt.Printf("%s OFF\n", id)
}

func fanSweep() {
	c, _ := cabinet()
	defer c.Close()
	for _, duty := range []float64{0, 30, 60, 100, 0} {
		if err := c.Fan.SetDuty(duty); err != nil {
			fatal(err)
		}
		time.Sleep(8 * time.Second)
		rpm, err := c.Fan.RPM()
		fmt.Printf("duty_pct=%.0f rpm=%.0f err=%v\n", duty, rpm, err)
	}
}

func hxTiming() {
	const pulses = 25000
	cfg := drivers.DefaultConfig()
	d, err := drivers.MeasureHX711Clock(cfg, pulses)
	if err != nil {
		fatal(err)
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	over := 0
	for _, v := range d {
		if v > 50*time.Microsecond {
			over++
		}
	}
	fmt.Printf("gpio=%d pulses=%d high_time_us min=%.1f p50=%.1f p99=%.1f p999=%.1f max=%.1f over_50us=%d\n",
		cfg.HX711ClockPin, len(d), us(d[0]), us(d[len(d)/2]), us(d[len(d)*99/100]), us(d[len(d)*999/1000]), us(d[len(d)-1]), over)
}

func us(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1000 }
