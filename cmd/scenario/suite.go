package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/sim"
)

func cmdList() {
	for _, n := range sim.Names() {
		fmt.Println(n)
	}
}

const sweepSeed = 20261106

func findPlant(name string, n int) (sim.Params, error) {
	if name == "base" {
		return sim.BasePlant(), nil
	}
	for _, p := range sim.Sweep(n, sweepSeed) {
		if p.Name == name {
			return p, nil
		}
	}
	return sim.Params{}, fmt.Errorf("no plant %q in a sweep of %d", name, n)
}

// writeRun runs a scenario with raw output into dir.
func writeRun(sc sim.Scenario, p sim.Params, dir string) (sim.Metrics, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	csvF, err := os.Create(filepath.Join(dir, "samples.csv.gz"))
	if err != nil {
		return nil, err
	}
	defer csvF.Close()
	gz := gzip.NewWriter(csvF)
	evF, err := os.Create(filepath.Join(dir, "events.log"))
	if err != nil {
		return nil, err
	}
	defer evF.Close()
	sp, m, err := sim.Execute(sc, p, sim.Output{CSV: gz, Events: evF})
	if err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	js, _ := json.MarshalIndent(sp, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "spec.json"), js, 0o644); err != nil {
		return nil, err
	}
	js, err = m.JSON()
	if err != nil {
		return nil, err
	}
	return m, os.WriteFile(filepath.Join(dir, "metrics.json"), js, 0o644)
}

func cmdRun(args []string) error {
	fs := newFlags("run")
	name := fs.String("name", "", "scenario name")
	plant := fs.String("plant", "base", "plant name")
	sweep := fs.Int("sweep", 36, "sweep size the plant name refers to")
	out := fs.String("out", "out", "output directory")
	_ = fs.Parse(args)
	sc, ok := sim.Find(*name)
	if !ok {
		return fmt.Errorf("no scenario %q", *name)
	}
	p, err := findPlant(*plant, *sweep)
	if err != nil {
		return err
	}
	m, err := writeRun(sc, p, *out)
	if err != nil {
		return err
	}
	for _, k := range m.Names() {
		fmt.Printf("%s=%.4f\n", k, m[k])
	}
	return nil
}

// result is one sweep cell.
type result struct {
	sc, plant string
	ctl, kind string
	m         sim.Metrics
	err       error
}

// keyMetric picks the metric whose worst case decides which run has its raw data
// kept. Larger is worse.
func keyMetric(sc sim.Scenario, m sim.Metrics) float64 {
	v := func(k string) float64 {
		x, ok := m[k]
		if !ok || math.IsNaN(x) {
			return math.Inf(1)
		}
		return x
	}
	switch sc.Kind {
	case "longrun":
		return math.Max(math.Abs(v("ff_t_max_err")), math.Abs(v("ff_t_min_err")))
	case "steady":
		return math.Max(v("t_span_p1p99"), v("rh_span_p1p99"))
	case "pulldown":
		return v("pulldown_s")
	case "disturbance":
		return v("rec_t_band2_s") + v("rec_rh_band8_s")
	case "fault":
		if v, ok := m["detected"]; ok && v == 0 {
			return math.Inf(1)
		}
		return v("detect_s")
	}
	return 0
}

func cmdSuite(args []string) error {
	fs := newFlags("suite")
	out := fs.String("out", "evidence/a", "output directory")
	nSweep := fs.Int("sweep", 36, "plants in the sweep")
	j := fs.Int("j", runtime.GOMAXPROCS(0), "parallel runs; defaults to GOMAXPROCS")
	only := fs.String("only", "", "only scenarios whose name contains this")
	skip := fs.String("skip", "", "leave out scenarios whose name contains any of these, comma-separated")
	_ = fs.Parse(args)
	start := time.Now()
	scs := sim.Scenarios()
	if *only != "" {
		var keep []sim.Scenario
		for _, s := range scs {
			if strings.Contains(s.Name, *only) {
				keep = append(keep, s)
			}
		}
		scs = keep
	}
	if *skip != "" {
		var keep []sim.Scenario
	next:
		for _, s := range scs {
			for _, x := range strings.Split(*skip, ",") {
				if strings.Contains(s.Name, x) {
					continue next
				}
			}
			keep = append(keep, s)
		}
		scs = keep
	}
	plants := sim.Sweep(*nSweep, sweepSeed)
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := writePlants(*out, plants); err != nil {
		return err
	}
	if err := writeDefs(*out); err != nil {
		return err
	}

	// Base plant, raw.
	base := sim.BasePlant()
	type job struct {
		sc sim.Scenario
		p  sim.Params
		i  int
	}
	sem := make(chan struct{}, *j)
	var wg sync.WaitGroup
	var mu sync.Mutex
	baseRes := map[string]sim.Metrics{}
	var firstErr error
	for _, sc := range scs {
		sc := sc
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			m, err := writeRun(sc, base, filepath.Join(*out, "base", sc.Name))
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", sc.Name, err)
			}
			baseRes[sc.Name] = m
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	fmt.Fprintf(os.Stderr, "base done in %s\n", time.Since(start).Round(time.Second))

	// Sweep, metrics in memory, raw only for the worst plant of each scenario.
	var results []result
	for _, sc := range scs {
		for _, p := range plants {
			sc, p := sc, p
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				sp, m, err := sim.Execute(sc, p, sim.Output{})
				mu.Lock()
				defer mu.Unlock()
				results = append(results, result{sc: sc.Name, plant: p.Name, ctl: sp.Ctl, kind: sc.Kind, m: m, err: err})
			}()
		}
	}
	wg.Wait()
	for _, r := range results {
		if r.err != nil {
			return fmt.Errorf("%s on %s: %w", r.sc, r.plant, r.err)
		}
	}
	sort.Slice(results, func(a, b int) bool {
		if results[a].sc != results[b].sc {
			return results[a].sc < results[b].sc
		}
		return results[a].plant < results[b].plant
	})
	fmt.Fprintf(os.Stderr, "sweep done in %s\n", time.Since(start).Round(time.Second))
	if err := writeSweep(*out, results); err != nil {
		return err
	}
	// Worst plant of each scenario, raw.
	worst := map[string]string{}
	worstV := map[string]float64{}
	byName := map[string]sim.Scenario{}
	for _, s := range scs {
		byName[s.Name] = s
	}
	for _, r := range results {
		v := keyMetric(byName[r.sc], r.m)
		if _, ok := worst[r.sc]; !ok || v > worstV[r.sc] {
			worst[r.sc], worstV[r.sc] = r.plant, v
		}
	}
	plantBy := map[string]sim.Params{}
	for _, p := range plants {
		plantBy[p.Name] = p
	}
	var worstNames []string
	for name := range worst {
		if byName[name].Ref == 0 {
			worstNames = append(worstNames, name)
		}
	}
	sort.Strings(worstNames)
	wf, err := os.Create(filepath.Join(*out, "worst_plants.csv"))
	if err != nil {
		return err
	}
	defer wf.Close()
	fmt.Fprintln(wf, "scenario,plant,key_metric_value")
	for _, name := range worstNames {
		pn := worst[name]
		fmt.Fprintf(wf, "%s,%s,%.3f\n", name, pn, worstV[name])
		sc, pp := byName[name], plantBy[pn]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := writeRun(sc, pp, filepath.Join(*out, "worst", sc.Name)); err != nil {
				mu.Lock()
				firstErr = err
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	// Raw data for runs that matter most: up to 3 plants per fault scenario where it went
	// undetected, and any run with a false sensor alarm, so nobody has to take a metric on trust.
	keepN := map[string]int{}
	var keep [][2]string
	for _, r := range results {
		if r.ctl != "core" || keepN[r.sc] >= 3 {
			continue
		}
		missed := r.m["detected"] == 0 && !math.IsNaN(r.m["detected"]) && r.kind == "fault"
		if _, has := r.m["detected"]; !has {
			missed = false
		}
		falseAl := r.m["ff_disagree_raises"] > 0 || r.m["prefault_disagree_raises"] > 0
		if missed || falseAl {
			keepN[r.sc]++
			keep = append(keep, [2]string{r.sc, r.plant})
		}
	}
	kf, err := os.Create(filepath.Join(*out, "kept_runs.csv"))
	if err != nil {
		return err
	}
	defer kf.Close()
	fmt.Fprintln(kf, "scenario,plant,directory")
	for _, k := range keep {
		k := k
		dir := filepath.Join("kept", k[0]+"__"+k[1])
		fmt.Fprintf(kf, "%s,%s,%s\n", k[0], k[1], dir)
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := writeRun(byName[k[0]], plantBy[k[1]], filepath.Join(*out, dir)); err != nil {
				mu.Lock()
				firstErr = err
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if err := writeCompare(*out, results, baseRes); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "suite done in %s\n", time.Since(start).Round(time.Second))
	return nil
}

func writePlants(out string, plants []sim.Params) error {
	f, err := os.Create(filepath.Join(out, "plants.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	t := reflect.TypeOf(sim.Params{})
	var names []string
	for i := 0; i < t.NumField(); i++ {
		names = append(names, t.Field(i).Name)
	}
	fmt.Fprintln(f, strings.Join(names, ","))
	all := append([]sim.Params{sim.BasePlant()}, plants...)
	for _, p := range all {
		v := reflect.ValueOf(p)
		var cols []string
		for i := 0; i < v.NumField(); i++ {
			cols = append(cols, fmt.Sprint(v.Field(i).Interface()))
		}
		fmt.Fprintln(f, strings.Join(cols, ","))
	}
	g, err := os.Create(filepath.Join(out, "sweep_ranges.csv"))
	if err != nil {
		return err
	}
	defer g.Close()
	fmt.Fprintln(g, "parameter,base,low,high")
	b := reflect.ValueOf(sim.BasePlant())
	for _, r := range sim.SweepRangeList() {
		fmt.Fprintf(g, "%s,%v,%v,%v\n", r[0], b.FieldByName(r[0]).Interface(), r[1], r[2])
	}
	return nil
}

func writeDefs(out string) error {
	f, err := os.Create(filepath.Join(out, "metric_definitions.tsv"))
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintln(f, "metric\tdefinition")
	for _, d := range sim.MetricDefs {
		fmt.Fprintf(f, "%s\t%s\n", d[0], d[1])
	}
	return nil
}

func writeSweep(out string, results []result) error {
	dir := filepath.Join(out, "sweep")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "metrics_long.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintln(f, "scenario,plant,controller,metric,value")
	type key struct{ sc, ctl, metric string }
	vals := map[key][]float64{}
	nanC := map[key]int{}
	for _, r := range results {
		for _, k := range r.m.Names() {
			v := r.m[k]
			if math.IsNaN(v) {
				fmt.Fprintf(f, "%s,%s,%s,%s,\n", r.sc, r.plant, r.ctl, k)
				nanC[key{r.sc, r.ctl, k}]++
				continue
			}
			fmt.Fprintf(f, "%s,%s,%s,%s,%.4f\n", r.sc, r.plant, r.ctl, k, v)
			vals[key{r.sc, r.ctl, k}] = append(vals[key{r.sc, r.ctl, k}], v)
		}
	}
	g, err := os.Create(filepath.Join(dir, "summary.csv"))
	if err != nil {
		return err
	}
	defer g.Close()
	fmt.Fprintln(g, "scenario,controller,metric,n,n_null,min,p05,median,p95,max")
	var keys []key
	seen := map[key]bool{}
	for k := range vals {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range nanC {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].sc != keys[b].sc {
			return keys[a].sc < keys[b].sc
		}
		return keys[a].metric < keys[b].metric
	})
	for _, k := range keys {
		x := vals[k]
		if len(x) == 0 {
			fmt.Fprintf(g, "%s,%s,%s,0,%d,,,,,\n", k.sc, k.ctl, k.metric, nanC[k])
			continue
		}
		sort.Float64s(x)
		fmt.Fprintf(g, "%s,%s,%s,%d,%d,%.3f,%.3f,%.3f,%.3f,%.3f\n", k.sc, k.ctl, k.metric, len(x), nanC[k], x[0], sim.Percentile(x, 5), sim.Percentile(x, 50), sim.Percentile(x, 95), x[len(x)-1])
	}
	return nil
}
