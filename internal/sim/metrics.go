package sim

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// Spec is what the metric code needs to know about a run. It is written beside
// the raw files, so metrics can be recomputed from the files alone.
type Spec struct {
	Name   string   `json:"name"`
	Plant  string   `json:"plant"`
	Ctl    string   `json:"controller"` // core, ref1 or ref4
	Kind   string   `json:"kind"`       // steady, disturbance, fault, pulldown
	SetT   float64  `json:"set_t"`
	SetRH  float64  `json:"set_rh"`
	SetAir float64  `json:"set_air"`
	M0     float64  `json:"measure_from_s"`
	M1     float64  `json:"measure_to_s"`
	D0     float64  `json:"disturb_from_s"` // door open, fault injected
	D1     float64  `json:"disturb_to_s"`   // door closed, fault repaired
	Expect []string `json:"expect_alarms,omitempty"`
	// ExcludeProtect is set for scenarios where a relay is stuck, so relay timing
	// is not under the controller's control. Commanded timing is still measured.
	Notes string `json:"notes,omitempty"`
	// RecoverFrom "defrost_end" counts recovery from the logged end of the defrost.
	RecoverFrom string `json:"recover_from,omitempty"`
}

// Metrics is a flat name to value map. NaN means not applicable or not reached.
type Metrics map[string]float64

// Names returns sorted metric names.
func (m Metrics) Names() []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// JSON renders metrics with NaN as null.
func (m Metrics) JSON() ([]byte, error) {
	o := map[string]any{}
	for k, v := range m {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			o[k] = nil
		} else {
			o[k] = math.Round(v*1e4) / 1e4
		}
	}
	return json.MarshalIndent(o, "", " ")
}

// MetricDefs documents each metric name, for the evidence directory.
var MetricDefs = [][2]string{
	{"t_mean_err", "mean(true air T - set) over the measure window, °C"},
	{"t_span_p1p99", "p99-p1 of true air T, °C"},
	{"t_p1_err", "p1 of true air T minus set"},
	{"t_p99_err", "p99 of true air T minus set"},
	{"t_span_xd", "p99-p1 of true air T excluding defrost and the 40 min after it, times from the event log, °C"},
	{"t_mean_err_xd", "mean(true air T - set) excluding defrost and the 40 min after it"},
	{"probe_t_span_xd", "p99-p1 of the filtered reading excluding defrost and the 40 min after it"},
	{"rh_span_xd", "p99-p1 of true RH excluding defrost and the 40 min after it"},
	{"rh_mean_err_xd", "mean(true RH - set) excluding defrost and the 40 min after it"},
	{"rhavg15_max_err_xd", "largest |15 min mean true RH - set|, full windows only, with the whole window clear of defrost and the 40 min after it"},
	{"tavg15_span", "p99-p1 of the 15 min rolling mean of true air T"},
	{"probe_t_span_p1p99", "the same for the filtered reading the controller used"},
	{"probe_t_mean_err", "mean(reading - set)"},
	{"prod_mean_err", "mean(true product T - set)"},
	{"prod_span", "max-min of true product T"},
	{"rh_mean_err", "mean(true RH - set), %RH"},
	{"rh_span_p1p99", "p99-p1 of true RH"},
	{"rh_p1_err", "p1 of true RH minus set"},
	{"rh_p99_err", "p99 of true RH minus set"},
	{"rhavg15_span", "p99-p1 of the 15 min rolling mean of true RH"},
	{"rhavg15_max_abs_err", "largest |15 min mean true RH - set|, full windows only"},
	{"probe_rh_span_p1p99", "the same for the filtered reading"},
	{"air_mean_err", "mean(true manifold velocity - set), m/s"},
	{"air_span_p1p99", "p99-p1 of true manifold velocity"},
	{"comp_starts_per_h", "compressor starts per hour in the window, from the event log"},
	{"comp_max_starts_any_h", "most compressor starts in any rolling hour of the run"},
	{"comp_duty_pct", "percent of window with compressor contactor closed"},
	{"comp_min_on_s", "shortest commanded compressor run in the run"},
	{"comp_min_off_s", "shortest commanded compressor stop in the run"},
	{"comp_min_spacing_s", "shortest start to start time in the run"},
	{"comp_first_on_s", "seconds from the run start to the first compressor start"},
	{"heat_cycles_per_day", "heater relay closures per day in the window"},
	{"humid_cycles_per_day", "humidifier relay closures per day in the window"},
	{"heat_duty_pct", "percent of window with heater commanded on"},
	{"humid_duty_pct", "percent of window with humidifier commanded on"},
	{"energy_wh_per_day", "modelled electrical energy per day in the window"},
	{"loss_pct_per_day", "true product weight loss rate in the window"},
	{"frost_end_g", "frost on the coil at the end of the run"},
	{"frost_max_g", "largest frost on the coil in the run"},
	{"alarms_warn_crit_window", "warning and critical alarms raised inside the measure window"},
	{"alarms_warn_crit_total", "warning and critical alarms raised at any time"},
	{"heat_on_bad_t_s", "seconds heater commanded on while the temperature reading was not ok or stale"},
	{"humid_on_bad_s", "seconds humidifier commanded on while the T or RH reading was not ok or stale"},
	{"rec_t_band2_s", "seconds after D1 (or logged defrost end) until the 3 min mean of true air T, using only samples after that time, is within ±2.0 °C of set and stays there for 1 h; null if it never does"},
	{"rec_t_band1_s", "the same, ±1.0 °C"},
	{"rec_rh_band8_s", "the same for the 5 min mean of true RH, ±8 %RH"},
	{"rec_rh_band5_s", "the same within ±5 %RH"},
	{"rec_prod_band1_s", "the same for true product T, ±1.0 °C"},
	{"dist_t_peak_err", "largest true air T minus set from D0 to the end of the run"},
	{"dist_prod_peak_err", "largest true product T minus set from D0 to the end"},
	{"dist_rh_peak_err", "largest true RH minus set from D0 to the end"},
	{"pulldown_s", "seconds from run start until true air T reached within 1.0 °C of set"},
	{"detect_s", "seconds from D0 to the first alarm in the expected list raised while the fault was present; null if none"},
	{"detected", "1 if an expected alarm of warning or critical severity was raised while the fault was present; an alarm after repair, or an info message, does not count"},
	{"info_only_s", "seconds from D0 to an expected message of info severity, which signals nothing"},
	{"prefault_disagree_raises", "sensor-disagreement alarms raised before the fault was injected: all of them are false"},
	{"prefault_any_warn_crit", "warning or critical alarms raised before the fault was injected, after the first 12 h"},
	{"ff_from_s", "whole-run fault-free measures start here: 12 h, after the first pull-down"},
	{"ff_t_mean_err", "mean true air minus set, whole run from ff_from_s, including outages"},
	{"ff_t_max_err", "largest true air minus set, whole run from ff_from_s, including outages"},
	{"ff_t_min_err", "smallest true air minus set, same window"},
	{"ff_prod_max_err", "largest true product minus set, same window"},
	{"ff_prod_min_err", "smallest true product minus set, same window"},
	{"ff_powered_t_max_err", "largest true air minus set more than 2 h after any outage or door opening, with power on"},
	{"ff_powered_t_min_err", "smallest true air minus set, same window"},
	{"ff_powered_t_p99_abs", "p99 of |true air minus set|, same window"},
	{"ff_reading_not_good_s", "seconds the temperature or humidity reading was not ok or stale, from 60 s after each boot"},
	{"ff_heat_or_cool_on_blind_s", "seconds compressor ran on the blind pattern"},
	{"ff_disagree_raises", "sensor-disagreement critical alarms raised in a fault-free run: every one is false"},
	{"ff_soft_raises", "one-signal drift or drying warnings raised in a fault-free run: every one is false"},
	{"ff_other_warn_crit", "all other warning and critical alarms in a fault-free run; real out-of-band conditions and nuisance both"},
	{"ff_contact_open_pct", "percent of powered time from ff_from_s with the contact signalling"},
	{"alarm_after_repair_s", "seconds after repair of the first expected alarm, when none came during the fault"},
	{"detect_t_err", "true air T minus set at the moment of detection"},
	{"detect_prod_err", "true product T minus set at the moment of detection"},
	{"detect_critical", "1 if an expected alarm of critical severity was raised during the fault"},
	{"critical_s", "seconds from D0 to the first expected critical alarm"},
	{"first_alarm_expected", "1 if the first warning or critical alarm during the fault was on the expected list, 0 if it named something else"},
	{"fault_t_min_abs", "lowest true air T from D0 to D1, absolute °C"},
	{"fault_t_max_abs", "highest true air T from D0 to D1, absolute °C"},
	{"fault_prod_min_abs", "lowest true product T from D0 to D1, absolute °C"},
	{"fault_prod_max_abs", "highest true product T from D0 to D1, absolute °C"},
	{"fault_air_below_0c_s", "seconds true air was below 0 °C from D0 to D1"},
	{"fault_prod_below_0c_s", "seconds true product was below 0 °C from D0 to D1"},
	{"signal_change_s", "seconds from D0 to the first physical change of the alarm contact during the fault, as the relay really moved; null if none. A close-and-open counts"},
	{"relay_on_pct", "percent of powered samples with the alarm contact open (signalling; energize-to-run), whole run. Unpowered samples also log the contact open, which is what it physically does, and are not in this figure"},
	{"buzzer_on_pct", "percent of powered samples with the buzzer on, whole run"},
	{"relay_close_edges", "times the contact was seen to go to signalling, at the sample period"},
	{"defrost_length_s", "logged defrost length"},
	{"recover_from_s", "the time recovery was counted from: the logged defrost end"},
	{"clear_s", "seconds from D1 until the expected alarm cleared; null if it did not"},
	{"fault_t_max_err", "largest true air T minus set from D0 to D1"},
	{"fault_t_min_err", "smallest true air T minus set from D0 to D1"},
	{"fault_t_mean_err", "mean true air T minus set from D0 to D1"},
	{"fault_prod_max_err", "largest true product T minus set from D0 to D1"},
	{"fault_prod_min_err", "smallest true product T minus set from D0 to D1"},
	{"fault_rh_max_err", "largest true RH minus set from D0 to D1"},
	{"fault_rh_min_err", "smallest true RH minus set from D0 to D1"},
	{"unexpected_alarms", "warning and critical alarms from D0 on that were not expected and not a consequence listed in the scenario"},
}

func pct(x []float64, p float64) float64 { return Percentile(x, p) }

func mean(x []float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

// rolling returns the trailing mean over w seconds of regular samples. A sample
// with less than a full window behind it gets NaN, so that a short average can
// never be reported as a 15 min mean. Gaps longer than 3 samples restart the window.
func rolling(t, x []float64, w float64) []float64 {
	out := make([]float64, len(x))
	s := 0.0
	j := 0
	start := 0
	for i := range x {
		if i > 0 && t[i]-t[i-1] > 3*(t[1]-t[0]) {
			s, j, start = 0, i, i
		}
		s += x[i]
		for t[i]-t[j] > w {
			s -= x[j]
			j++
		}
		if t[i]-t[start] >= w {
			out[i] = s / float64(i-j+1)
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}

// noNaN drops NaN values.
func noNaN(x []float64) []float64 {
	out := make([]float64, 0, len(x))
	for _, v := range x {
		if !math.IsNaN(v) {
			out = append(out, v)
		}
	}
	return out
}

func good(q string) bool { return q == "ok" || q == "stale" || q == "off" }

// Compute derives every metric from rows and events only.
func Compute(sp Spec, rows []Row, evs []Ev) Metrics {
	m := Metrics{}
	var ts, T, prod, RH, air, pT, pRH []float64
	cool, heat, hum := 0.0, 0.0, 0.0
	var e0, e1, l0, l1 float64
	haveE := false
	dt := 5.0
	if len(rows) > 1 {
		dt = rows[1].T - rows[0].T
	}
	for i := range rows {
		r := &rows[i]
		if r.T < sp.M0 || r.T > sp.M1 || !r.Powered {
			continue
		}
		ts = append(ts, r.T)
		T = append(T, r.TrueT)
		prod = append(prod, r.TrueProd)
		RH = append(RH, r.TrueRH)
		air = append(air, r.TrueAirMS)
		pT = append(pT, r.CtlT)
		pRH = append(pRH, r.CtlRH)
		if r.CoolEff {
			cool++
		}
		if r.HeatCmd {
			heat++
		}
		if r.HumCmd {
			hum++
		}
		if !haveE {
			e0, l0, haveE = r.EnergyWh, r.LossTruePct, true
		}
		e1, l1 = r.EnergyWh, r.LossTruePct
	}
	hours := (sp.M1 - sp.M0) / 3600
	if len(T) > 0 {
		n := float64(len(T))
		m["t_mean_err"] = mean(T) - sp.SetT
		m["t_p1_err"] = pct(append([]float64(nil), T...), 1) - sp.SetT
		m["t_p99_err"] = pct(append([]float64(nil), T...), 99) - sp.SetT
		m["t_span_p1p99"] = m["t_p99_err"] - m["t_p1_err"]
		tavg := noNaN(rolling(ts, T, 900))
		m["tavg15_span"] = pct(append([]float64(nil), tavg...), 99) - pct(tavg, 1)
		m["probe_t_mean_err"] = mean(pT) - sp.SetT
		m["probe_t_span_p1p99"] = pct(append([]float64(nil), pT...), 99) - pct(pT, 1)
		m["prod_mean_err"] = mean(prod) - sp.SetT
		sort.Float64s(prod)
		m["prod_span"] = prod[len(prod)-1] - prod[0]
		m["rh_mean_err"] = mean(RH) - sp.SetRH
		m["rh_p1_err"] = pct(append([]float64(nil), RH...), 1) - sp.SetRH
		m["rh_p99_err"] = pct(append([]float64(nil), RH...), 99) - sp.SetRH
		m["rh_span_p1p99"] = m["rh_p99_err"] - m["rh_p1_err"]
		ravg := noNaN(rolling(ts, RH, 900))
		m["rhavg15_span"] = pct(append([]float64(nil), ravg...), 99) - pct(append([]float64(nil), ravg...), 1)
		mx := 0.0
		for _, v := range ravg {
			mx = math.Max(mx, math.Abs(v-sp.SetRH))
		}
		m["rhavg15_max_abs_err"] = mx
		m["probe_rh_span_p1p99"] = pct(append([]float64(nil), pRH...), 99) - pct(pRH, 1)
		m["air_mean_err"] = mean(air) - sp.SetAir
		m["air_span_p1p99"] = pct(append([]float64(nil), air...), 99) - pct(air, 1)
		m["comp_duty_pct"] = cool / n * 100
		m["heat_duty_pct"] = heat / n * 100
		m["humid_duty_pct"] = hum / n * 100
		if hours > 0 {
			m["energy_wh_per_day"] = (e1 - e0) / hours * 24
			m["loss_pct_per_day"] = (l1 - l0) / hours * 24
		}
	}

	// The same, leaving out defrost and the 40 min after it, because reference
	// controllers switch humidity control off in defrost and exclude that time.
	var xT, xpT, xRH, xs []float64
	// Defrost times come from the event log, since a defrost cut short in a few
	// seconds can fall between samples.
	var defOn, defOff []float64
	for _, e := range evs {
		if e.Kind == "output" && e.Code == "defrost" {
			if e.Msg == "on" {
				defOn = append(defOn, e.T)
			} else {
				defOff = append(defOff, e.T)
			}
		}
	}
	lastDef := math.Inf(-1)
	di := 0
	var xAvgIn []bool
	var xLastOut []float64
	lastOut := math.Inf(-1)
	for i := range rows {
		r := &rows[i]
		for di < len(defOff) && defOff[di] <= r.T {
			lastDef = defOff[di]
			di++
		}
		inDefrost := di < len(defOn) && defOn[di] <= r.T
		if inDefrost {
			lastDef = r.T
		}
		if r.T < sp.M0 || r.T > sp.M1 || !r.Powered {
			continue
		}
		in := r.T-lastDef > 2400
		xAvgIn = append(xAvgIn, in)
		if !in {
			lastOut = r.T
		}
		xLastOut = append(xLastOut, lastOut)
		xs = append(xs, r.T)
		xRH = append(xRH, r.TrueRH)
		if in {
			xT = append(xT, r.TrueT)
			xpT = append(xpT, r.CtlT)
		}
	}
	if len(xT) > 0 {
		m["t_mean_err_xd"] = mean(xT) - sp.SetT
		m["t_span_xd"] = pct(append([]float64(nil), xT...), 99) - pct(append([]float64(nil), xT...), 1)
		m["probe_t_span_xd"] = pct(append([]float64(nil), xpT...), 99) - pct(append([]float64(nil), xpT...), 1)
		rav := rolling(xs, xRH, 900)
		var inRH []float64
		mxx := 0.0
		for i := range xRH {
			if xAvgIn[i] {
				inRH = append(inRH, xRH[i])
				// The whole 15 min window must also be clear of the exclusion.
				if !math.IsNaN(rav[i]) && xs[i]-xLastOut[i] > 900 {
					mxx = math.Max(mxx, math.Abs(rav[i]-sp.SetRH))
				}
			}
		}
		m["rh_mean_err_xd"] = mean(inRH) - sp.SetRH
		m["rh_span_xd"] = pct(append([]float64(nil), inRH...), 99) - pct(append([]float64(nil), inRH...), 1)
		m["rhavg15_max_err_xd"] = mxx
	}

	// Protection, exact, from the output events.
	var coolOn []float64
	lastOn, lastOff := math.NaN(), math.NaN()
	minOn, minOff, minSp := math.Inf(1), math.Inf(1), math.Inf(1)
	heatC, humC := 0, 0
	warnW, warnT := 0, 0
	for _, e := range evs {
		if e.Kind == "scenario" && strings.HasPrefix(e.Msg, "power") {
			lastOn, lastOff = math.NaN(), math.NaN()
		}
		if e.Kind == "output" {
			switch e.Code {
			case "cool":
				if e.Msg == "on" {
					coolOn = append(coolOn, e.T)
					if !math.IsNaN(lastOff) {
						minOff = math.Min(minOff, e.T-lastOff)
					}
					if !math.IsNaN(lastOn) {
						minSp = math.Min(minSp, e.T-lastOn)
					}
					lastOn = e.T
				} else {
					if !math.IsNaN(lastOn) {
						minOn = math.Min(minOn, e.T-lastOn)
					}
					lastOff = e.T
				}
			case "heat":
				if e.Msg == "on" && e.T >= sp.M0 && e.T <= sp.M1 {
					heatC++
				}
			case "humid":
				if e.Msg == "on" && e.T >= sp.M0 && e.T <= sp.M1 {
					humC++
				}
			}
		}
		if e.Kind == "alarm" && (e.Sev == "warning" || e.Sev == "critical") {
			warnT++
			if e.T >= sp.M0 && e.T <= sp.M1 {
				warnW++
			}
		}
	}
	m["comp_min_on_s"] = infNaN(minOn)
	m["comp_min_off_s"] = infNaN(minOff)
	m["comp_min_spacing_s"] = infNaN(minSp)
	m["alarms_warn_crit_window"] = float64(warnW)
	m["alarms_warn_crit_total"] = float64(warnT)
	if hours > 0 {
		c := 0
		for _, t := range coolOn {
			if t >= sp.M0 && t <= sp.M1 {
				c++
			}
		}
		m["comp_starts_per_h"] = float64(c) / hours
		m["heat_cycles_per_day"] = float64(heatC) / hours * 24
		m["humid_cycles_per_day"] = float64(humC) / hours * 24
	}
	mxh := 0
	j := 0
	for i := range coolOn {
		for coolOn[i]-coolOn[j] >= 3600 {
			j++
		}
		if i-j+1 > mxh {
			mxh = i - j + 1
		}
	}
	m["comp_max_starts_any_h"] = float64(mxh)
	if len(coolOn) > 0 {
		m["comp_first_on_s"] = coolOn[0]
	} else {
		m["comp_first_on_s"] = math.NaN()
	}

	// Alarm outputs over the whole run.
	relayOn, buzzOn, nAll, relayEdges := 0.0, 0.0, 0.0, 0.0
	prevRelay := false
	for i := range rows {
		r := &rows[i]
		if !r.Powered {
			continue
		}
		nAll++
		if r.AlarmRelay {
			relayOn++
			if !prevRelay {
				relayEdges++
			}
		}
		if r.Buzzer {
			buzzOn++
		}
		prevRelay = r.AlarmRelay
	}
	if nAll > 0 {
		m["relay_on_pct"] = relayOn / nAll * 100
		m["buzzer_on_pct"] = buzzOn / nAll * 100
		m["relay_close_edges"] = relayEdges
	}

	// Whole-run safety measures.
	hb, ub := 0.0, 0.0
	frostMax := 0.0
	frostEnd := 0.0
	for i := range rows {
		r := &rows[i]
		if r.HeatCmd && !good(r.QT) && r.T > 30 {
			hb += dt
		}
		if r.HumCmd && (!good(r.QT) || !good(r.QRH)) && r.T > 30 {
			ub += dt
		}
		frostMax = math.Max(frostMax, r.FrostG)
		frostEnd = r.FrostG
	}
	m["heat_on_bad_t_s"] = hb
	m["humid_on_bad_s"] = ub
	m["frost_max_g"] = frostMax
	m["frost_end_g"] = frostEnd

	faultFree(sp, rows, evs, m)
	if sp.Kind == "pulldown" {
		m["pulldown_s"] = math.NaN()
		for i := range rows {
			if math.Abs(rows[i].TrueT-sp.SetT) <= 1 {
				m["pulldown_s"] = rows[i].T
				break
			}
		}
	}
	if sp.Kind == "disturbance" || sp.Kind == "fault" {
		disturbance(sp, rows, evs, m)
	}
	return m
}

func infNaN(v float64) float64 {
	if math.IsInf(v, 0) {
		return math.NaN()
	}
	return v
}

// settled returns seconds after from until value stays inside band for a full hold.
// Means are trailing, use only samples after from, and need a full avg window, so the
// smallest possible answer is avg seconds. If the run ends before a full hold has been
// seen, the answer is NaN: it did not show that it settled.
func settled(ts, x []float64, from, set, band, avg, hold float64) float64 {
	start := -1
	for i := range ts {
		if ts[i] >= from {
			start = i
			break
		}
	}
	if start < 0 {
		return math.NaN()
	}
	s := 0.0
	j := start
	mean := make([]float64, len(ts))
	for i := start; i < len(ts); i++ {
		s += x[i]
		for ts[i]-ts[j] > avg {
			s -= x[j]
			j++
		}
		mean[i] = s / float64(i-j+1)
		if ts[i]-ts[start] < avg {
			mean[i] = math.NaN()
		}
	}
	end := ts[len(ts)-1]
	nextOut := math.Inf(1)
	next := make([]float64, len(ts))
	for i := len(ts) - 1; i >= start; i-- {
		if !math.IsNaN(mean[i]) && math.Abs(mean[i]-set) > band {
			nextOut = ts[i]
		}
		next[i] = nextOut
	}
	for i := start; i < len(ts); i++ {
		if !math.IsNaN(mean[i]) && math.Abs(mean[i]-set) <= band && next[i] > ts[i]+hold && end-ts[i] >= hold {
			return ts[i] - from
		}
	}
	return math.NaN()
}

func disturbance(sp Spec, rows []Row, evs []Ev, m Metrics) {
	var ts, T, RH, P []float64
	for i := range rows {
		ts = append(ts, rows[i].T)
		T = append(T, rows[i].TrueT)
		RH = append(RH, rows[i].TrueRH)
		P = append(P, rows[i].TrueProd)
	}
	// A defrost's recovery starts when it really ended, read from the event log.
	d1 := sp.D1
	if sp.RecoverFrom == "defrost_end" {
		d1 = math.NaN()
		for _, e := range evs {
			if e.Kind == "output" && e.Code == "defrost" && e.Msg == "off" && e.T >= sp.D0 {
				d1 = e.T
				break
			}
		}
		m["defrost_length_s"] = d1 - sp.D0
		if !math.IsNaN(d1) {
			// Drip follows: nothing can recover until it is over, and it is counted.
			m["recover_from_s"] = d1
		}
	}
	if sp.D1 > 0 {
		if math.IsNaN(d1) {
			for _, k := range []string{"rec_t_band2_s", "rec_t_band1_s", "rec_rh_band8_s", "rec_rh_band5_s", "rec_prod_band1_s"} {
				m[k] = math.NaN()
			}
		} else {
			m["rec_t_band2_s"] = settled(ts, T, d1, sp.SetT, 2, 180, 3600)
			m["rec_t_band1_s"] = settled(ts, T, d1, sp.SetT, 1, 180, 3600)
			m["rec_rh_band8_s"] = settled(ts, RH, d1, sp.SetRH, 8, 300, 3600)
			m["rec_rh_band5_s"] = settled(ts, RH, d1, sp.SetRH, 5, 300, 3600)
			m["rec_prod_band1_s"] = settled(ts, P, d1, sp.SetT, 1, 0, 3600)
		}
	}
	mt, mp, mr := math.Inf(-1), math.Inf(-1), math.Inf(-1)
	for i := range rows {
		if ts[i] >= sp.D0 {
			mt = math.Max(mt, T[i]-sp.SetT)
			mp = math.Max(mp, P[i]-sp.SetT)
			mr = math.Max(mr, RH[i]-sp.SetRH)
		}
	}
	m["dist_t_peak_err"], m["dist_prod_peak_err"], m["dist_rh_peak_err"] = infNaN(mt), infNaN(mp), infNaN(mr)

	// Fault-specific.
	if sp.Kind != "fault" {
		return
	}
	end := sp.D1
	if end == 0 {
		end = sp.M1
	}
	fmxT, fmnT, fmxP, fmnP, fmxR, fmnR := math.Inf(-1), math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1), math.Inf(1)
	sum, n := 0.0, 0.0
	dtRow := 5.0
	if len(ts) > 1 {
		dtRow = ts[1] - ts[0]
	}
	var airBelow0, prodBelow0 float64
	for i := range rows {
		if ts[i] >= sp.D0 && ts[i] <= end {
			d := T[i] - sp.SetT
			fmxT, fmnT = math.Max(fmxT, d), math.Min(fmnT, d)
			p := P[i] - sp.SetT
			fmxP, fmnP = math.Max(fmxP, p), math.Min(fmnP, p)
			r := RH[i] - sp.SetRH
			fmxR, fmnR = math.Max(fmxR, r), math.Min(fmnR, r)
			sum += d
			n++
			if T[i] < 0 {
				airBelow0 += dtRow
			}
			if P[i] < 0 {
				prodBelow0 += dtRow
			}
		}
	}
	m["fault_t_max_err"], m["fault_t_min_err"] = infNaN(fmxT), infNaN(fmnT)
	m["fault_prod_max_err"], m["fault_prod_min_err"] = infNaN(fmxP), infNaN(fmnP)
	m["fault_rh_max_err"], m["fault_rh_min_err"] = infNaN(fmxR), infNaN(fmnR)
	m["fault_t_min_abs"], m["fault_t_max_abs"] = infNaN(fmnT)+sp.SetT, infNaN(fmxT)+sp.SetT
	m["fault_prod_min_abs"], m["fault_prod_max_abs"] = infNaN(fmnP)+sp.SetT, infNaN(fmxP)+sp.SetT
	m["fault_air_below_0c_s"], m["fault_prod_below_0c_s"] = airBelow0, prodBelow0
	if n > 0 {
		m["fault_t_mean_err"] = sum / n
	}
	if len(sp.Expect) == 0 {
		return
	}
	isExpect := func(c string) bool {
		for _, x := range sp.Expect {
			if x == c {
				return true
			}
		}
		return false
	}
	// Detected means raised while the fault is present. An alarm after repair is
	// reported separately and does not count.
	m["detected"] = 0
	m["detect_s"] = math.NaN()
	m["detect_t_err"] = math.NaN()
	m["detect_prod_err"] = math.NaN()
	m["detect_critical"] = 0
	m["critical_s"] = math.NaN()
	m["first_alarm_expected"] = math.NaN()
	m["alarm_after_repair_s"] = math.NaN()
	m["clear_s"] = math.NaN()
	un := 0
	raisedCode := ""
	firstSeen := false
	for _, e := range evs {
		if e.T < sp.D0 {
			continue
		}
		// Strictly before repair: a check made at the repair is not the controller
		// detecting anything.
		active := sp.D1 == 0 || e.T < sp.D1
		if e.Kind == "alarm" && active && (e.Sev == "warning" || e.Sev == "critical") && !firstSeen && !isIncidental(e.Code) {
			firstSeen = true
			if isExpect(e.Code) {
				m["first_alarm_expected"] = 1
			} else {
				m["first_alarm_expected"] = 0
			}
		}
		if e.Kind == "alarm" {
			if isExpect(e.Code) && e.Sev == "info" {
				if active {
					m["info_only_s"] = e.T - sp.D0
				}
			} else if isExpect(e.Code) {
				if active {
					if m["detected"] == 0 {
						m["detected"] = 1
						m["detect_s"] = e.T - sp.D0
						raisedCode = e.Code
						m["detect_t_err"], m["detect_prod_err"] = trueAt(rows, e.T, sp)
					}
					if e.Sev == "critical" && m["detect_critical"] == 0 {
						m["detect_critical"] = 1
						m["critical_s"] = e.T - sp.D0
					}
				} else if math.IsNaN(m["alarm_after_repair_s"]) && m["detected"] == 0 {
					m["alarm_after_repair_s"] = e.T - sp.D1
				}
			} else if (e.Sev == "warning" || e.Sev == "critical") && active {
				un++
			}
		}
		if e.Kind == "clear" && sp.D1 > 0 && e.T >= sp.D1 && e.Code == raisedCode && math.IsNaN(m["clear_s"]) {
			m["clear_s"] = e.T - sp.D1
		}
	}
	m["unexpected_alarms"] = float64(un)
	// How soon the contact visibly changed after the fault began.
	m["signal_change_s"] = math.NaN()
	for _, e := range evs {
		if e.T >= sp.D0 && e.Kind == "output" && e.Code == "alarm_signal" && (sp.D1 == 0 || e.T < sp.D1) {
			m["signal_change_s"] = e.T - sp.D0
			break
		}
	}
}

// faultFree measures a whole run with nothing injected, for its whole length, and,
// for fault runs, what happened before the injection.
func faultFree(sp Spec, rows []Row, evs []Ev, m Metrics) {
	const from = 12 * 3600.0
	if sp.Kind == "fault" {
		dis, any := 0.0, 0.0
		for _, e := range evs {
			if e.T >= sp.D0 || e.Kind != "alarm" || e.Sev == "info" {
				continue
			}
			if e.Code == "TEMP_READING_DISAGREES" || e.Code == "RH_READING_DISAGREES" {
				dis++
			}
			if e.T >= from {
				any++
			}
		}
		m["prefault_disagree_raises"], m["prefault_any_warn_crit"] = dis, any
		return
	}
	if sp.Kind != "steady" && sp.Kind != "longrun" && sp.Kind != "pulldown" {
		return
	}
	var quietUntil float64
	var marks []float64
	for _, e := range evs {
		if e.Kind == "scenario" && (strings.HasPrefix(e.Msg, "power back") || strings.HasPrefix(e.Msg, "door closed")) {
			marks = append(marks, e.T)
		}
		if e.Kind == "output" && e.Code == "defrost" && e.Msg == "off" {
			marks = append(marks, e.T)
		}
	}
	sort.Float64s(marks)
	mk := 0
	var sumT, nT, onN, openN, notGood float64
	mxT, mnT, mxP, mnP := math.Inf(-1), math.Inf(1), math.Inf(-1), math.Inf(1)
	pMx, pMn := math.Inf(-1), math.Inf(1)
	var abs []float64
	dt := 5.0
	if len(rows) > 1 {
		dt = rows[1].T - rows[0].T
	}
	lastOff := -1e18
	for i := range rows {
		r := &rows[i]
		if !r.Powered {
			lastOff = r.T
		}
		for mk < len(marks) && marks[mk] <= r.T {
			quietUntil = marks[mk] + 2*3600
			mk++
		}
		if r.T < from {
			continue
		}
		d, p := r.TrueT-r.SetT, r.TrueProd-r.SetT
		if !r.Powered {
			// The set value is not known with power off: use the last one known.
			if i > 0 {
				d, p = r.TrueT-rows[i-1].SetT, r.TrueProd-rows[i-1].SetT
				r.SetT = rows[i-1].SetT
			}
		}
		sumT += d
		nT++
		mxT, mnT, mxP, mnP = math.Max(mxT, d), math.Min(mnT, d), math.Max(mxP, p), math.Min(mnP, p)
		if !r.Powered {
			continue
		}
		onN++
		if r.AlarmRelay {
			openN++
		}
		if r.T-lastOff > 60 && (!good(r.QT) || !good(r.QRH)) {
			notGood += dt
		}
		if r.T >= quietUntil && r.T-lastOff > 2*3600 && !r.Door {
			pMx, pMn = math.Max(pMx, d), math.Min(pMn, d)
			abs = append(abs, math.Abs(d))
		}
	}
	if nT == 0 {
		return
	}
	m["ff_from_s"] = from
	m["ff_t_mean_err"] = sumT / nT
	m["ff_t_max_err"], m["ff_t_min_err"], m["ff_prod_max_err"], m["ff_prod_min_err"] = mxT, mnT, mxP, mnP
	m["ff_powered_t_max_err"], m["ff_powered_t_min_err"] = infNaN(pMx), infNaN(pMn)
	if len(abs) > 0 {
		m["ff_powered_t_p99_abs"] = pct(abs, 99)
	}
	m["ff_reading_not_good_s"] = notGood
	if onN > 0 {
		m["ff_contact_open_pct"] = openN / onN * 100
	}
	dis, soft, other := 0.0, 0.0, 0.0
	for _, e := range evs {
		if e.Kind != "alarm" || e.Sev == "info" || e.T < from {
			continue
		}
		switch e.Code {
		case "TEMP_READING_DISAGREES", "RH_READING_DISAGREES":
			dis++
		case "TEMP_DRIFT_SUSPECT", "RH_DRIFT_SUSPECT", "DRYING_RATE_SHIFT":
			soft++
		default:
			other++
		}
	}
	m["ff_disagree_raises"], m["ff_soft_raises"], m["ff_other_warn_crit"] = dis, soft, other
}

// isIncidental names alarms that say nothing about which part failed.
func isIncidental(code string) bool {
	switch code {
	case "CLOCK_UNTRUSTED", "CAL_CHECK_DUE", "POWER_OUTAGE":
		return true
	}
	return false
}

func trueAt(rows []Row, t float64, sp Spec) (float64, float64) {
	for i := range rows {
		if rows[i].T >= t {
			return rows[i].TrueT - sp.SetT, rows[i].TrueProd - sp.SetT
		}
	}
	return math.NaN(), math.NaN()
}

// ReadRows parses a samples CSV.
func ReadRows(r io.Reader) ([]Row, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var rows []Row
	first := true
	for sc.Scan() {
		if first {
			first = false
			if sc.Text() != CSVHeader {
				return nil, fmt.Errorf("unexpected CSV header")
			}
			continue
		}
		row, err := ParseRow(sc.Text())
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, sc.Err()
}

// ReadEvs parses an event log.
func ReadEvs(r io.Reader) ([]Ev, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var evs []Ev
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		e, err := ParseEv(sc.Text())
		if err != nil {
			return nil, err
		}
		evs = append(evs, e)
	}
	return evs, sc.Err()
}
