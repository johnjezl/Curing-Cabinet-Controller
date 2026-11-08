package sim

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/core"
)

// Row is one logged sample: ground truth from the plant alongside what the
// controller believed and did. The two are never mixed, so a reader can see
// where a sensor misled the controller.
type Row struct {
	T                                       float64
	Powered                                 bool
	SetT, SetRH, SetAir                     float64
	TrueT, TrueWall, TrueProd, TrueCoil     float64
	TrueRH, TrueAirMS, TrueMassG, Ambient   float64
	CtlT, CtlRH, CtlAir, CtlW               float64
	QT, QRH, QAir, QW                       string
	CoolCmd, CoolEff, HeatCmd, HeatEff      bool
	HumCmd, HumEff, DehumCmd, Defrost, Door bool
	FanDuty, RPM                            float64
	AlarmRelay, Buzzer                      bool
	NActive                                 int
	FrostG, EnergyWh, LossTruePct, LossCtl  float64
	Run                                     string
	Phase                                   int
	TotalElapsed                            float64
}

// CSVHeader is the column list of Row.
const CSVHeader = "t_s,powered,set_t,set_rh,set_air,true_air_t,true_wall_t,true_prod_t,true_coil_t,true_rh,true_air_ms,true_mass_g,ambient_c," +
	"ctl_t,ctl_rh,ctl_air,ctl_w,q_t,q_rh,q_air,q_w,cool_cmd,cool_eff,heat_cmd,heat_eff,humid_cmd,humid_eff,dehumid_cmd,defrost,door,fan_duty,fan_rpm," +
	"alarm_contact_open,buzzer,n_active,frost_g,energy_wh,loss_true_pct,loss_ctl_pct,run_state,phase,total_elapsed_s"

func b(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func f(v float64, p int) string { return strconv.FormatFloat(v, 'f', p, 64) }

// CSV renders a row. It keeps enough digits to recompute every metric.
func (r *Row) CSV() string {
	return strings.Join([]string{
		f(r.T, 0), b(r.Powered), f(r.SetT, 2), f(r.SetRH, 2), f(r.SetAir, 2),
		f(r.TrueT, 3), f(r.TrueWall, 3), f(r.TrueProd, 3), f(r.TrueCoil, 2), f(r.TrueRH, 2), f(r.TrueAirMS, 3), f(r.TrueMassG, 1), f(r.Ambient, 2),
		f(r.CtlT, 3), f(r.CtlRH, 2), f(r.CtlAir, 3), f(r.CtlW, 1), r.QT, r.QRH, r.QAir, r.QW,
		b(r.CoolCmd), b(r.CoolEff), b(r.HeatCmd), b(r.HeatEff), b(r.HumCmd), b(r.HumEff), b(r.DehumCmd), b(r.Defrost), b(r.Door),
		f(r.FanDuty, 1), f(r.RPM, 0), b(r.AlarmRelay), b(r.Buzzer), strconv.Itoa(r.NActive), f(r.FrostG, 1), f(r.EnergyWh, 1),
		f(r.LossTruePct, 3), f(r.LossCtl, 3), r.Run, strconv.Itoa(r.Phase), f(r.TotalElapsed, 0),
	}, ",")
}

// ParseRow reads a CSV line; used to recompute metrics from files alone.
func ParseRow(line string) (Row, error) {
	p := strings.Split(line, ",")
	if len(p) != 42 {
		return Row{}, fmt.Errorf("want 42 columns, got %d", len(p))
	}
	var e error
	fl := func(i int) float64 {
		v, err := strconv.ParseFloat(p[i], 64)
		if err != nil {
			e = err
		}
		return v
	}
	bo := func(i int) bool { return p[i] == "1" }
	r := Row{T: fl(0), Powered: bo(1), SetT: fl(2), SetRH: fl(3), SetAir: fl(4),
		TrueT: fl(5), TrueWall: fl(6), TrueProd: fl(7), TrueCoil: fl(8), TrueRH: fl(9), TrueAirMS: fl(10), TrueMassG: fl(11), Ambient: fl(12),
		CtlT: fl(13), CtlRH: fl(14), CtlAir: fl(15), CtlW: fl(16), QT: p[17], QRH: p[18], QAir: p[19], QW: p[20],
		CoolCmd: bo(21), CoolEff: bo(22), HeatCmd: bo(23), HeatEff: bo(24), HumCmd: bo(25), HumEff: bo(26), DehumCmd: bo(27), Defrost: bo(28), Door: bo(29),
		FanDuty: fl(30), RPM: fl(31), AlarmRelay: bo(32), Buzzer: bo(33), NActive: int(fl(34)), FrostG: fl(35), EnergyWh: fl(36),
		LossTruePct: fl(37), LossCtl: fl(38), Run: p[39], Phase: int(fl(40)), TotalElapsed: fl(41)}
	return r, e
}

// Ev is one line of the event log.
type Ev struct {
	T    float64
	Kind string
	Code string
	Sev  string
	Msg  string
}

// String renders an event line.
func (e Ev) String() string {
	return fmt.Sprintf("%.0f\t%s\t%s\t%s\t%s", e.T, e.Kind, e.Code, e.Sev, strings.ReplaceAll(e.Msg, "\t", " "))
}

// ParseEv reads an event line back.
func ParseEv(line string) (Ev, error) {
	p := strings.SplitN(line, "\t", 5)
	if len(p) != 5 {
		return Ev{}, fmt.Errorf("bad event line %q", line)
	}
	t, err := strconv.ParseFloat(p[0], 64)
	return Ev{T: t, Kind: p[1], Code: p[2], Sev: p[3], Msg: p[4]}, err
}

// Ctl is what a Run drives. Core is one; RefCtl, an emulation of the reference
// controller defaults, is the other.
type Ctl interface {
	Step()
	View() core.ProbeView
}

type coreCtl struct{ c *core.Core }

func (c coreCtl) Step()                { c.c.Step() }
func (c coreCtl) View() core.ProbeView { return c.c.Probe() }

// Options choose how a Run is set up.
type Options struct {
	Plant     Params
	UseRef    bool // drive RefCtl instead of core
	Config    *core.Config
	SampleSec float64 // CSV period; 0 means 5 s
	KeepRows  bool
	// Sinks for raw output. Nil means not written.
	CSV    io.Writer
	Events io.Writer
	NTP    float64 // seconds after each boot until the clock is right, no RTC only
	Seed   uint64
	// OSBootID is passed to core at each boot; the same ID across a PowerLoss makes it
	// a process restart rather than a power cut, as on a real machine.
	OSBootID string
}

// Run is one simulation.
type Run struct {
	O     Options
	Plant *Plant
	Clock *Clock
	FS    *core.MemFS
	Core  *core.Core
	Ref   *RefCtl
	ctl   Ctl
	rng   *rand.Rand

	csv      *bufio.Writer
	ev       *bufio.Writer
	Rows     []Row
	Evs      []Ev
	lastRow  float64
	last     core.ProbeView
	haveL    bool
	Boots    int
	Guard    *SimGuard
	lastPhys bool
	dead     bool
	// HealthOutages collects outage records seen by the controller.
	Outages []api.Outage
}

// NewRun creates plant, clock, storage and controller.
func NewRun(o Options) (*Run, error) {
	if o.SampleSec == 0 {
		o.SampleSec = 5
	}
	r := &Run{O: o, Plant: NewPlant(o.Plant), Clock: NewClock(o.Plant.RTC), FS: core.NewMemFS(),
		rng: rand.New(rand.NewPCG(o.Seed+o.Plant.Seed, 0xabc))}
	if o.CSV != nil {
		r.csv = bufio.NewWriterSize(o.CSV, 1<<16)
		fmt.Fprintln(r.csv, CSVHeader)
	}
	if o.Events != nil {
		r.ev = bufio.NewWriterSize(o.Events, 1<<14)
		fmt.Fprintln(r.ev, "t_s\tkind\tcode\tseverity\tmessage")
	}
	if !o.Plant.RTC {
		ntp := o.NTP
		if ntp == 0 {
			ntp = 600
		}
		r.Clock.Reboot(ntp)
	}
	if err := r.boot(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Run) boot() error {
	r.Boots++
	if r.O.UseRef {
		if r.Ref == nil {
			r.Ref = NewRefCtl(r.Plant, r.Clock)
		} else {
			r.Ref.Restart()
		}
		r.ctl = r.Ref
		return nil
	}
	if r.Guard == nil {
		r.Guard = &SimGuard{pl: r.Plant}
	}
	c, err := core.New(core.Options{Cabinet: r.Plant.Cabinet(), Clock: r.Clock, FS: r.FS, Config: r.O.Config, Simulated: true, Speed: 0, Watchdog: r.Guard, OSBootID: r.O.OSBootID,
		Trace: func(e core.Event) {
			kind := e.Type
			r.logEv(Ev{T: r.Clock.VT, Kind: kind, Code: e.Code, Sev: e.Sev, Msg: strings.TrimSpace(e.Title + " " + e.Msg)})
		}})
	if err != nil {
		return err
	}
	r.Core = c
	r.ctl = coreCtl{c}
	return nil
}

func (r *Run) logEv(e Ev) {
	r.Evs = append(r.Evs, e)
	if r.ev != nil {
		fmt.Fprintln(r.ev, e.String())
	}
}

// Note adds a scenario marker to the event log.
func (r *Run) Note(msg string) { r.logEv(Ev{T: r.Clock.VT, Kind: "scenario", Msg: msg}) }

// Flush writes buffered output.
func (r *Run) Flush() {
	if r.csv != nil {
		r.csv.Flush()
	}
	if r.ev != nil {
		r.ev.Flush()
	}
}

// Tick advances one second.
func (r *Run) Tick() {
	r.Plant.Step(1)
	r.Clock.VT = r.Plant.T
	if !r.dead {
		r.ctl.Step()
		if r.Guard != nil {
			r.Guard.tick(r.Plant.T)
		}
	}
	r.observe()
}

// Advance runs for sec seconds.
func (r *Run) Advance(sec float64) {
	end := r.Clock.VT + sec
	for r.Clock.VT < end-1e-9 {
		r.Tick()
	}
}

// AdvanceUntil runs until cond is true or maxSec passes, and reports which.
func (r *Run) AdvanceUntil(maxSec float64, cond func() bool) bool {
	end := r.Clock.VT + maxSec
	for r.Clock.VT < end-1e-9 {
		r.Tick()
		if cond() {
			return true
		}
	}
	return false
}

func (r *Run) observe() {
	var v core.ProbeView
	if !r.dead {
		v = r.ctl.View()
	}
	if r.dead && r.haveL {
		// The contact opens the instant power goes; log it like any other change.
		phys := true
		if phys != r.lastPhys {
			r.logEv(Ev{T: r.Clock.VT, Kind: "output", Code: "alarm_signal", Msg: "on"})
			r.lastPhys = phys
		}
	}
	t := r.Clock.VT
	pl := r.Plant
	// Output transitions are logged exactly: protection metrics are computed
	// from these, not from decimated samples.
	if r.haveL {
		trans := func(name string, was, is bool) {
			if was != is {
				s := "off"
				if is {
					s = "on"
				}
				r.logEv(Ev{T: t, Kind: "output", Code: name, Msg: s})
			}
		}
		trans("cool", r.last.Cool, v.Cool)
		trans("heat", r.last.Heat, v.Heat)
		trans("humid", r.last.Humid, v.Humid)
		trans("dehumid", r.last.Dehumid, v.Dehumid)
		trans("defrost", r.last.Defrost, v.Defrost)
		// The relay as it really is, not as commanded: a failed write is visible here.
		phys := !pl.Powered || !pl.CmdAlarm // unpowered, the energize-to-run contact is open
		trans("alarm_signal", r.lastPhys, phys)
		r.lastPhys = phys
		trans("buzzer", r.last.Buzzer, v.Buzzer)
	}
	r.last, r.haveL = v, true

	if t-r.lastRow < r.O.SampleSec-1e-9 {
		return
	}
	r.lastRow = t
	row := Row{T: t, Powered: pl.Powered, SetT: v.Set.TempC, SetRH: v.Set.RHPct, SetAir: v.Set.AirMS,
		TrueT: pl.TAir, TrueWall: pl.TWall, TrueProd: pl.TProd, TrueCoil: pl.TCoil, TrueRH: pl.RH(), TrueAirMS: pl.FanV, TrueMassG: pl.ProdKg * 1000, Ambient: pl.Ambient(),
		CtlT: v.Temp, CtlRH: v.RH, CtlAir: v.Air, CtlW: v.Weight, QT: string(v.TempQ), QRH: string(v.RHQ), QAir: string(v.AirQ), QW: string(v.WeightQ),
		CoolCmd: v.Cool, CoolEff: pl.EffCool(), HeatCmd: v.Heat, HeatEff: pl.EffHeat(), HumCmd: v.Humid, HumEff: pl.EffHumid(), DehumCmd: v.Dehumid, Defrost: v.Defrost, Door: pl.DoorIsOpen,
		FanDuty: v.Fan, RPM: v.RPM, AlarmRelay: !pl.Powered || !pl.CmdAlarm, Buzzer: pl.Powered && !pl.CmdBuzzer, NActive: v.ActiveWarn, FrostG: pl.Frost, EnergyWh: pl.EnergyWh(), LossTruePct: pl.WaterLossPct(), LossCtl: v.LossPct,
		Run: string(v.RunState), Phase: v.Phase, TotalElapsed: v.TotalElapsed}
	if row.QT == "" {
		row.QT, row.QRH, row.QAir, row.QW = "none", "none", "none", "none"
	}
	if row.Run == "" {
		row.Run = "none"
	}
	// Metrics are computed from exactly what the CSV holds, rounding included, so
	// that anyone recomputing from the file gets the same numbers.
	line := row.CSV()
	if r.O.KeepRows {
		if parsed, err := ParseRow(line); err == nil {
			row = parsed
		}
		r.Rows = append(r.Rows, row)
	}
	if r.csv != nil {
		fmt.Fprintln(r.csv, line)
	}
}

// PowerLoss cuts power now, with storage damage appropriate to the cut, for sec
// seconds of plant time, and then boots a new controller. If ntp is negative the
// wall clock stays wrong for the rest of the run.
func (r *Run) PowerLoss(sec float64, ntp float64) error {
	r.logEv(Ev{T: r.Clock.VT, Kind: "scenario", Msg: fmt.Sprintf("power lost for %.0f s", sec)})
	r.Plant.PowerOff()
	r.FS.PowerCut(r.rng)
	r.dead = true
	r.Advance(sec)
	r.Plant.PowerOn()
	r.dead = false
	r.Clock.Reboot(ntp)
	r.haveL = false
	if r.Guard != nil {
		r.Guard.lastPet = r.Plant.T
	}
	r.logEv(Ev{T: r.Clock.VT, Kind: "scenario", Msg: "power back"})
	return r.boot()
}

// SetCal puts the scale in calibration without going through a tare.
func (r *Run) SetCal(extra ...func(*api.Calibration)) {
	if r.Core == nil {
		return
	}
	k := api.Calibration{ScaleZero: int32(r.Plant.P.ScaleTareCounts), ScaleCountsPG: r.Plant.P.ScaleCountsPerG}
	for _, f := range extra {
		f(&k)
	}
	_ = r.Core.Do(api.Command{Kind: api.CmdSetCal, Calibration: &k})
}

// SetPoint sets manual setpoints.
func (r *Run) SetPoint(t, rh, air float64) {
	if r.O.UseRef {
		r.Ref.Set = api.Setpoints{TempC: t, RHPct: rh, AirMS: air}
		// A reference controller has no airflow loop. An installer would pick one
		// fixed speed; this gives it the speed that meets the target on this plant
		// at the start, which is generous to it.
		p := &r.Plant.P
		rpm := air / p.VMax * p.FanMaxRPM
		r.Ref.FanFixed = p.FanMinDuty + (rpm-p.FanMinRPM)/(p.FanMaxRPM-p.FanMinRPM)*(100-p.FanMinDuty)
		if r.Ref.FanFixed < p.FanMinDuty {
			r.Ref.FanFixed = p.FanMinDuty
		}
		return
	}
	_ = r.Core.Do(api.Command{Kind: api.CmdSetSetpoint, Setpoint: &api.Setpoints{TempC: t, RHPct: rh, AirMS: air}})
}

// Percentile of a slice; the slice is sorted in place.
func Percentile(x []float64, p float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	sort.Float64s(x)
	i := p / 100 * float64(len(x)-1)
	lo := int(i)
	if lo+1 >= len(x) {
		return x[len(x)-1]
	}
	return x[lo] + (i-float64(lo))*(x[lo+1]-x[lo])
}

func cmdDefrost() api.Command { return api.Command{Kind: api.CmdDefrost} }

// RefCheck is a person comparing the sensors with a separate instrument that reads
// plant truth.
func (r *Run) RefCheck() {
	if r.Core == nil {
		return
	}
	tv, rh := r.Plant.TAir, r.Plant.RH()
	_ = r.Core.Do(api.Command{Kind: api.CmdRefCheck, Reference: &api.Reference{TempC: &tv, RHPct: &rh}})
}
