package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

// bar is a machine-checkable comparison against a stated number from BENCHMARK.
// A reference number is a documented default or an inference, as labelled there.
type bar struct {
	ID       string
	Label    string // basis in BENCHMARK
	What     string
	Ref      float64
	Lower    bool // true: lower is better, and the measure must be at or under Ref
	Scenario string
	Metric   string
	Abs      bool
}

func bars() []bar {
	var b []bar
	steady := []string{"steady_cure_13c_76rh", "steady_early_14c_82rh", "steady_equalize_4c_85rh", "steady_beef_1p5c_82rh", "steady_ferment_24c_90rh", "steady_warm_20c_70rh", "steady_biltong_25c_55rh"}
	for _, s := range steady {
		b = append(b,
			bar{"R1.2", "INFERRED", "probe cycles set..set+2.0 C ignoring overshoot: span", 2.0, true, s, "probe_t_span_xd", false},
			bar{"R1.5", "DOCUMENTED", "stated cabinet swing 3.9 C span (true air)", 3.9, true, s, "t_span_xd", false},
			bar{"R1.5m", "DOCUMENTED", "stated mean offset 1.7 C from set value", 1.7, true, s, "t_mean_err_xd", true},
			bar{"R1.4", "DOCUMENTED", "humidity corrected once more than 5 %RH from set: 15 min mean stays inside", 5, true, s, "rhavg15_max_err_xd", false},
			bar{"R3.2", "DOCUMENTED", "start spacing example: at most 10 starts/h", 10, true, s, "comp_max_starts_any_h", false},
			bar{"R3.1", "DOCUMENTED", "minimum off, shipped 1 to 4 min: observed at least 240 s", 240, false, s, "comp_min_off_s", false},
		)
	}
	for _, s := range []string{"door_30s_13c", "door_2min_13c", "door_10min_13c", "door_30s_4c", "door_2min_4c", "door_2min_24c"} {
		b = append(b,
			bar{"R2.1", "INFERRED", "back in temperature band (2 C) within the 20 min window", 1200, true, s, "rec_t_band2_s", false},
			bar{"R2.1h", "INFERRED", "back in humidity band (8 %RH) within the 20 min window", 1200, true, s, "rec_rh_band8_s", false},
		)
	}
	for _, s := range []string{"defrost_13c", "defrost_4c", "defrost_1p5c"} {
		b = append(b,
			bar{"R2.3", "INFERRED", "back in temperature band (2 C) within 20 min of defrost end", 1200, true, s, "rec_t_band2_s", false},
			bar{"R2.8", "not published", "humidity back in 8 %RH within 20 min of defrost end", 1200, true, s, "rec_rh_band8_s", false},
		)
	}
	b = append(b,
		bar{"R2.5", "DOCUMENTED", "empty cabinet reaches setpoint within 60 min", 3600, true, "pulldown_empty_4c", "pulldown_s", false},
		bar{"R2.6", "DOCUMENTED", "loaded cabinet settles in about 12 h: product within 1 C", 43200, true, "warmload_13c_10kg_20c", "rec_prod_band1_s", false},
		bar{"R2.6", "DOCUMENTED", "loaded cabinet settles in about 12 h: product within 1 C", 43200, true, "warmload_4c_10kg_12c", "rec_prod_band1_s", false},
		bar{"R3.3", "DOCUMENTED", "power-on compressor delay, default 0: observed from start of run", 240, false, "steady_cure_13c_76rh", "comp_first_on_s", false},
	)
	for _, s := range []string{"alarm_later_fault_gets_through"} {
		b = append(b, bar{"alarm", "project rule", "a later fault changes the contact within 60 s with earlier alarms latched", 60, true, s, "signal_change_s", false})
	}
	for _, s := range []string{"fault_outputs_write_error"} {
		b = append(b, bar{"alarm", "project rule", "an output-write fault changes the contact physically within 60 s", 60, true, s, "signal_change_s", false})
	}
	for _, s := range []string{"faultfree_14d_13c", "faultfree_14d_4c", "faultfree_14d_1p5c", "faultfree_14d_24c", "steady_cure_13c_76rh", "steady_equalize_4c_85rh", "steady_beef_1p5c_82rh", "steady_ferment_24c_90rh"} {
		b = append(b,
			bar{"FF", "project rule", "fault-free: seconds a reading was out of service, whole run", 0, true, s, "ff_reading_not_good_s", false},
			bar{"FF", "project rule", "fault-free: false sensor-disagreement alarms, whole run", 0, true, s, "ff_disagree_raises", false},
			bar{"FF", "project rule", "fault-free: largest true air above set, away from outages, doors and defrost", 3.9, true, s, "ff_powered_t_max_err", false},
			bar{"FF", "project rule", "fault-free: largest true air below set, away from outages, doors and defrost", -3.9, false, s, "ff_powered_t_min_err", false},
		)
	}
	b = append(b, bar{"R3.1", "project rule", "off time kept after a guard lease drop: at least 240 s", 240, false, "guard_lease_drop", "comp_min_off_s", false})
	for _, s := range []string{"fault_climate_dead_1p5c", "fault_climate_dead_4c", "fault_climate_dead_13c", "fault_climate_dead_24c", "fault_climate_dead_7d_1p5c", "fault_climate_dead_7d_4c", "fault_climate_dead_7d_13c", "fault_climate_dead_7d_24c"} {
		b = append(b,
			bar{"R3.5", "project rule", "24 h with temperature unknown: true air under 0 C, seconds", 0, true, s, "fault_air_below_0c_s", false},
			bar{"R3.5", "project rule", "24 h with temperature unknown: true product under 0 C, seconds", 0, true, s, "fault_prod_below_0c_s", false},
		)
	}
	for _, s := range []string{"fault_climate_dead_13c", "fault_climate_dead_4c", "fault_climate_dead_1p5c", "fault_climate_dead_24c", "fault_climate_badread"} {
		b = append(b,
			bar{"R4.1", "DOCUMENTED", "loss of temperature reading shown within about 30 s", 30, true, s, "detect_s", false},
			bar{"R4.4", "DOCUMENTED", "sensor fault clears 10 to 30 s after recovery", 30, true, s, "clear_s", false},
			bar{"food", "project rule", "heater never on with bad temperature data, seconds", 0, true, s, "heat_on_bad_t_s", false},
			bar{"food", "project rule", "humidifier never on with bad data, seconds", 0, true, s, "humid_on_bad_s", false},
		)
	}
	for _, s := range []string{"fault_temp_stuck", "fault_rh_stuck", "fault_air_stuck", "fault_scale_stuck", "fault_temp_spikes", "fault_rh_spikes", "fault_temp_implausible", "fault_rh_implausible", "fault_air_spikes", "fault_scale_spikes", "fault_scale_saturated",
		"fault_air_dead", "fault_scale_dead", "fault_fan_stall", "fault_tach_dead", "fault_tach_wire", "fault_duct_blocked", "fault_cool_stuck_on", "fault_cool_stuck_off", "fault_compressor_no_cooling", "fault_heat_stuck_on", "fault_heat_stuck_off_24c", "fault_humid_stuck_on", "fault_humid_empty_4c", "fault_outputs_write_error", "fault_door_switch_dead", "fault_storage_write_fail", "fault_storage_full", "fault_scale_drift_up", "fault_air_implausible", "fault_temp_drift_up_fast_13c", "fault_temp_drift_up_slow_13c", "fault_temp_drift_down_fast_13c", "fault_temp_drift_down_mid_13c", "fault_temp_drift_down_slow_13c", "fault_temp_drift_down_fast_4c", "fault_temp_drift_down_slow_4c", "fault_rh_drift_up_slow_13c", "fault_rh_drift_down_slow_13c", "fault_rh_drift_down_fast_13c", "fault_temp_drift_up_fast_4c", "fault_temp_drift_up_slow_4c", "fault_temp_drift_up_fast_1p5c", "fault_temp_drift_up_slow_1p5c", "fault_temp_drift_down_fast_1p5c", "fault_cool_stuck_on_4c", "fault_cool_stuck_on_1p5c", "fault_door_switch_stuck_open", "guard_lease_drop", "alarm_later_fault_gets_through"} {
		b = append(b, bar{"R4.10", "absent in reference", "fault detected at all (1 = yes)", 1, false, s, "detected", false})
	}
	return b
}

func writeCompare(out string, results []result, base map[string]sim.Metrics) error {
	dir := filepath.Join(out, "compare")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "reference_bar.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintln(f, "ref_id,ref_basis,ref_value,better,judged_on_magnitude,scenario,metric,statement,base_plant_signed,sweep_n,sweep_null,sweep_median_signed,sweep_p95_worstside,sweep_worst_signed,pass_base,pass_p95,pass_worst,evidence_basis")
	type key struct{ sc, metric string }
	vals := map[key][]float64{}
	nulls := map[key]int{}
	for _, r := range results {
		if r.ctl != "core" {
			continue
		}
		for k, v := range r.m {
			if math.IsNaN(v) {
				nulls[key{r.sc, k}]++
			} else {
				vals[key{r.sc, k}] = append(vals[key{r.sc, k}], v)
			}
		}
	}
	for _, br := range bars() {
		k := key{br.Scenario, br.Metric}
		raw := append([]float64(nil), vals[k]...)
		// Means keep their sign in the file. Only the pass test uses the magnitude.
		mag := func(v float64) float64 {
			if br.Abs {
				return math.Abs(v)
			}
			return v
		}
		x := make([]float64, len(raw))
		for i := range raw {
			x[i] = mag(raw[i])
		}
		better := ">="
		if br.Lower {
			better = "<="
		}
		baseV := math.NaN()
		if m, ok := base[br.Scenario]; ok {
			if v, ok := m[br.Metric]; ok {
				baseV = v
			}
		}
		if len(x) == 0 && nulls[k] == 0 {
			continue
		}
		sort.Float64s(x)
		sort.Float64s(raw)
		med, p95, worst, worstSigned := math.NaN(), math.NaN(), math.NaN(), math.NaN()
		if len(x) > 0 {
			med = sim.Percentile(raw, 50)
			if br.Lower {
				p95 = sim.Percentile(x, 95)
				worst = x[len(x)-1]
			} else {
				p95 = sim.Percentile(x, 5)
				worst = x[0]
			}
			worstSigned = worst
			for _, v := range raw {
				if mag(v) == worst {
					worstSigned = v
				}
			}
		}
		pass := func(v float64) bool {
			if math.IsNaN(v) {
				return false
			}
			v = mag(v)
			if br.Lower {
				return v <= br.Ref
			}
			return v >= br.Ref
		}
		n0 := nulls[k]
		fmt.Fprintf(f, "%s,%s,%.3f,%s,%v,%s,%s,%q,%.3f,%d,%d,%.3f,%.3f,%.3f,%v,%v,%v,MEASURED-SIM\n", br.ID, br.Label, br.Ref, better, br.Abs, br.Scenario, br.Metric, br.What, baseV, len(x), n0, med, p95, worstSigned, pass(baseV), pass(p95) && n0 == 0, pass(worst) && n0 == 0)
	}

	// Scorecard: every fault-free run, for its whole length, per plant in the long
	// format and summarised here.
	ff, err := os.Create(filepath.Join(dir, "fault_free_scorecard.csv"))
	if err != nil {
		return err
	}
	defer ff.Close()
	fmt.Fprintln(ff, "scenario,metric,n_plants,n_null,min,median,p95,max,n_plants_nonzero")
	ffm := []string{"ff_t_mean_err", "ff_t_max_err", "ff_t_min_err", "ff_prod_max_err", "ff_prod_min_err", "ff_powered_t_max_err", "ff_powered_t_min_err", "ff_powered_t_p99_abs", "ff_reading_not_good_s", "ff_disagree_raises", "ff_soft_raises", "ff_other_warn_crit", "ff_contact_open_pct"}
	var ffNames []string
	seenFF := map[string]bool{}
	for _, r := range results {
		if _, ok := r.m["ff_t_mean_err"]; ok && r.ctl == "core" && !seenFF[r.sc] {
			seenFF[r.sc] = true
			ffNames = append(ffNames, r.sc)
		}
	}
	sort.Strings(ffNames)
	for _, name := range ffNames {
		for _, mn := range ffm {
			x := append([]float64(nil), vals[key{name, mn}]...)
			if len(x) == 0 {
				fmt.Fprintf(ff, "%s,%s,0,%d,,,,,\n", name, mn, nulls[key{name, mn}])
				continue
			}
			nz := 0
			for _, v := range x {
				if v != 0 {
					nz++
				}
			}
			sort.Float64s(x)
			fmt.Fprintf(ff, "%s,%s,%d,%d,%.3f,%.3f,%.3f,%.3f,%d\n", name, mn, len(x), nulls[key{name, mn}], x[0], sim.Percentile(x, 50), sim.Percentile(x, 95), x[len(x)-1], nz)
		}
	}
	// Detection next to false alarms: neither means anything alone.
	dfp, err := os.Create(filepath.Join(dir, "detection_vs_false_alarms.csv"))
	if err != nil {
		return err
	}
	defer dfp.Close()
	fmt.Fprintln(dfp, "scenario,detected_of_n,n,critical_of_n,detect_median_s,detect_max_s,true_air_err_at_detect_median,true_air_err_at_detect_worst,false_disagree_before_fault_runs,fault_air_max_abs,fault_air_min_abs")
	type dv struct{ det, crit, n, fp int }
	var dfNames []string
	seenD := map[string]*dv{}
	for _, r := range results {
		if r.ctl != "core" || r.kind != "fault" {
			continue
		}
		if _, ok := r.m["detected"]; !ok {
			continue
		}
		d := seenD[r.sc]
		if d == nil {
			d = &dv{}
			seenD[r.sc] = d
			dfNames = append(dfNames, r.sc)
		}
		d.n++
		if r.m["detected"] == 1 {
			d.det++
		}
		if r.m["detect_critical"] == 1 {
			d.crit++
		}
		if r.m["prefault_disagree_raises"] > 0 {
			d.fp++
		}
	}
	sort.Strings(dfNames)
	stat := func(name, mn string) (med, mx, wAbs, mnv float64) {
		x := append([]float64(nil), vals[key{name, mn}]...)
		if len(x) == 0 {
			return math.NaN(), math.NaN(), math.NaN(), math.NaN()
		}
		sort.Float64s(x)
		med, mx, mnv = sim.Percentile(x, 50), x[len(x)-1], x[0]
		wAbs = mx
		if math.Abs(mnv) > math.Abs(mx) {
			wAbs = mnv
		}
		return
	}
	for _, name := range dfNames {
		d := seenD[name]
		m1, m2, _, _ := stat(name, "detect_s")
		e1, _, e2, _ := stat(name, "detect_t_err")
		_, tx, _, _ := stat(name, "fault_t_max_abs")
		_, _, _, tn := stat(name, "fault_t_min_abs")
		fmt.Fprintf(dfp, "%s,%d,%d,%d,%.0f,%.0f,%.2f,%.2f,%d,%.2f,%.2f\n", name, d.det, d.n, d.crit, m1, m2, e1, e2, d.fp, tx, tn)
	}

	// Time for a sensor that has frozen to be noticed. It depends on the actuators
	// moving, so it is a range and not a figure.
	sf, err := os.Create(filepath.Join(dir, "stuck_sensor_detection.csv"))
	if err != nil {
		return err
	}
	defer sf.Close()
	fmt.Fprintln(sf, "scenario,base_plant_s,sweep_n,sweep_undetected,sweep_min_s,sweep_median_s,sweep_p95_s,sweep_max_s")
	for _, name := range []string{"fault_temp_stuck", "fault_rh_stuck", "fault_air_stuck", "fault_scale_stuck"} {
		x := append([]float64(nil), vals[key{name, "detect_s"}]...)
		und := nulls[key{name, "detect_s"}]
		if len(x) == 0 {
			continue
		}
		sort.Float64s(x)
		fmt.Fprintf(sf, "%s,%.0f,%d,%d,%.0f,%.0f,%.0f,%.0f\n", name, base[name]["detect_s"], len(x), und, x[0], sim.Percentile(x, 50), sim.Percentile(x, 95), x[len(x)-1])
	}

	// Same plants under both controllers, paired by plant.
	g, err := os.Create(filepath.Join(dir, "core_vs_reference_emulation.csv"))
	if err != nil {
		return err
	}
	defer g.Close()
	fmt.Fprintln(g, "scenario,metric,controller,n,median_signed,p05_signed,p95_signed,min_signed,max_signed,n_plants_core_better,n_plants_compared")
	type cell struct {
		ctl, plant string
		v          float64
	}
	table := map[string]map[string][]cell{} // base scenario -> metric -> cells
	metrics := []string{"t_mean_err", "t_span_p1p99", "t_mean_err_xd", "t_span_xd", "probe_t_span_xd", "rh_mean_err_xd", "rh_span_xd", "rhavg15_max_err_xd", "tavg15_span", "probe_t_span_p1p99", "prod_mean_err", "prod_span", "rh_mean_err", "rh_span_p1p99", "rhavg15_span", "rhavg15_max_abs_err", "air_mean_err", "air_span_p1p99", "comp_starts_per_h", "comp_max_starts_any_h", "comp_min_off_s", "comp_first_on_s", "heat_cycles_per_day", "humid_cycles_per_day", "energy_wh_per_day", "rec_t_band2_s", "rec_t_band1_s", "rec_rh_band8_s", "rec_rh_band5_s", "rec_prod_band1_s", "dist_t_peak_err", "dist_rh_peak_err", "pulldown_s", "fault_t_max_err", "fault_t_min_err", "fault_t_mean_err", "fault_prod_max_err", "fault_prod_min_err", "fault_t_min_abs", "fault_t_max_abs", "fault_prod_min_abs", "fault_prod_max_abs", "fault_air_below_0c_s", "fault_prod_below_0c_s", "relay_on_pct", "frost_max_g", "ff_t_mean_err", "ff_t_max_err", "ff_t_min_err", "ff_prod_max_err", "ff_prod_min_err"}
	for _, r := range results {
		bs := r.sc
		if i := strings.Index(bs, "__ref"); i > 0 {
			bs = bs[:i]
		}
		if table[bs] == nil {
			table[bs] = map[string][]cell{}
		}
		for _, mn := range metrics {
			if v, ok := r.m[mn]; ok {
				table[bs][mn] = append(table[bs][mn], cell{r.ctl, r.plant, v})
			}
		}
	}
	var scs []string
	for s, t := range table {
		// Only scenarios that have a reference run.
		has := false
		for _, cs := range t {
			for _, c := range cs {
				if c.ctl != "core" {
					has = true
				}
			}
		}
		if has {
			scs = append(scs, s)
		}
	}
	sort.Strings(scs)
	for _, s := range scs {
		for _, mn := range metrics {
			cs := table[s][mn]
			if len(cs) == 0 {
				continue
			}
			core := map[string]float64{}
			by := map[string][]cell{}
			for _, c := range cs {
				by[c.ctl] = append(by[c.ctl], c)
				if c.ctl == "core" {
					core[c.plant] = c.v
				}
			}
			var ctls []string
			for c := range by {
				ctls = append(ctls, c)
			}
			sort.Strings(ctls)
			for _, ctl := range ctls {
				var x []float64
				better, compared := 0, 0
				for _, c := range by[ctl] {
					if !math.IsNaN(c.v) {
						x = append(x, c.v)
					}
					if ctl != "core" {
						cv, ok := core[c.plant]
						if ok && !math.IsNaN(cv) && !math.IsNaN(c.v) {
							compared++
							if lessIsBetter(mn) {
								if math.Abs(cv) < math.Abs(c.v) {
									better++
								}
							} else if cv > c.v {
								better++
							}
						}
					}
				}
				if len(x) == 0 {
					fmt.Fprintf(g, "%s,%s,%s,0,,,,,,%d,%d\n", s, mn, ctl, better, compared)
					continue
				}
				sort.Float64s(x)
				fmt.Fprintf(g, "%s,%s,%s,%d,%.3f,%.3f,%.3f,%.3f,%.3f,%d,%d\n", s, mn, ctl, len(x), sim.Percentile(x, 50), sim.Percentile(x, 5), sim.Percentile(x, 95), x[0], x[len(x)-1], better, compared)
			}
		}
	}
	return nil
}

// cmdBarsMoved compares two reference_bar.csv files row by row.
func cmdBarsMoved(args []string) error {
	fs := newFlags("barsmoved")
	oldP := fs.String("old", "", "earlier reference_bar.csv")
	newP := fs.String("new", "", "later reference_bar.csv")
	out := fs.String("out", "", "output CSV")
	_ = fs.Parse(args)
	read := func(p string) (map[string][]string, []string, error) {
		f, err := os.Open(p)
		if err != nil {
			return nil, nil, err
		}
		defer f.Close()
		recs, err := csv.NewReader(f).ReadAll()
		if err != nil {
			return nil, nil, err
		}
		m := map[string][]string{}
		var order []string
		for _, r := range recs[1:] {
			k := r[0] + "|" + r[5] + "|" + r[6] + "|" + r[2]
			if _, dup := m[k]; !dup {
				order = append(order, k)
			}
			m[k] = r
		}
		return m, order, nil
	}
	o, _, err := read(*oldP)
	if err != nil {
		return err
	}
	n, order, err := read(*newP)
	if err != nil {
		return err
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"ref_id", "scenario", "metric", "ref_value", "better", "old_base", "new_base", "change_base", "old_median", "new_median", "old_p95", "new_p95", "change_p95", "old_worst", "new_worst", "change_worst", "old_pass_base", "new_pass_base", "old_pass_p95", "new_pass_p95", "old_pass_worst", "new_pass_worst", "pass_change"})
	// Each change label comes from the values on its own row: moved by more than 5 %
	// of the bar in the good or bad direction. Signed means are judged on magnitude.
	moved := func(oldS, newS, better, judgedAbs, refS string) string {
		a, e1 := strconv.ParseFloat(oldS, 64)
		b, e2 := strconv.ParseFloat(newS, 64)
		ref, e3 := strconv.ParseFloat(refS, 64)
		switch {
		case e1 != nil || e2 != nil || e3 != nil:
			return "n/a"
		case math.IsNaN(a) && math.IsNaN(b):
			return "same"
		case math.IsNaN(a):
			return "better"
		case math.IsNaN(b):
			return "worse"
		}
		if judgedAbs == "true" {
			a, b = math.Abs(a), math.Abs(b)
		}
		tol := math.Max(math.Abs(ref)*0.05, 1e-3)
		d := b - a
		if better == ">=" {
			d = -d
		}
		switch {
		case d < -tol:
			return "better"
		case d > tol:
			return "worse"
		}
		return "same"
	}
	for _, k := range order {
		r := n[k]
		q, ok := o[k]
		pc := "same"
		if !ok {
			q = make([]string, len(r))
			pc = "new bar"
		} else {
			score := func(x []string) int {
				s := 0
				for _, i := range []int{14, 15, 16} {
					if x[i] == "true" {
						s++
					}
				}
				return s
			}
			switch {
			case score(r) > score(q):
				pc = "better"
			case score(r) < score(q):
				pc = "worse"
			}
		}
		_ = w.Write([]string{r[0], r[5], r[6], r[2], r[3], q[8], r[8], moved(q[8], r[8], r[3], r[4], r[2]), q[11], r[11], q[12], r[12], moved(q[12], r[12], r[3], r[4], r[2]), q[13], r[13], moved(q[13], r[13], r[3], r[4], r[2]), q[14], r[14], q[15], r[15], q[16], r[16], pc})
	}
	return nil
}

func lessIsBetter(m string) bool {
	switch m {
	case "comp_min_off_s", "comp_first_on_s":
		return false
	}
	return true
}
