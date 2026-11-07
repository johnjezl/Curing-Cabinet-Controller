// Package sim is a plant model of a converted-refrigerator curing cabinet that
// implements internal/hal, and a harness that runs internal/core against it in
// virtual time. It is a model: results describe behavior against this model, not
// a real cabinet. Every parameter is a stated physical estimate, listed with its
// reasoning in Params, and is varied in Sweep so that a controller cannot look
// good on one lucky plant.
package sim

import (
	"fmt"
	"math/rand/v2"
	"reflect"
)

// Params are the plant's physical estimates. Comments give the basis. None of
// these were chosen by looking at controller results; the sweep ranges are wide
// for the same reason.
type Params struct {
	Name string

	// --- Thermal ---
	VolumeM3 float64 // 0.35 m3: a 350 L single-door display or under-counter fridge.
	// CWallJK: 12 kJ/K. Liner, shelves and racks: ~15 kg steel at 0.5 kJ/kgK plus
	// ~3 kg plastic at 1.5 kJ/kgK. Foam insulation adds little on these timescales.
	CWallJK float64
	// UAAirWall: 30 W/K at 1 m/s manifold flow. ~5 m2 of surface, h about 6 W/m2K
	// (surface air speed is well under the manifold's). Scales with sqrt(velocity).
	UAAirWall float64
	// UAWallAmb: 2.2 W/K. 3.5 m2 of 40 mm PU foam at 0.025 W/mK, gaskets included.
	UAWallAmb float64
	InfilACH  float64 // 0.15 air changes/h door closed: a worn but sound gasket.
	// DoorExchangeTauS: 25 s. Door-open air exchange time constant: a warm-air
	// plume exchanges most of a small cabinet in under a minute.
	DoorExchangeTauS float64

	// --- Compressor and coil ---
	// CompW: 130 W net cooling at -10 °C coil and 25 °C room. Small R600a unit;
	// 60 W input at COP about 2.
	CompW         float64
	CompCoilSlope float64 // +2.5 %/K of coil temperature: suction density effect.
	CompAmbSlope  float64 // -1.2 %/K of room temperature: condensing pressure effect.
	CompTauS      float64 // 60 s first-order capacity lag after start.
	CompStopTauS  float64 // 20 s capacity decay after stop.
	// CoilFloorC: -28 °C. A small fridge system has no useful capacity under this.
	CoilFloorC float64
	CompCOP    float64 // 2.0 net cooling per electrical W; energy figures use CompW/COP.
	// CCoilJK: 1.5 kJ/K: ~1 kg aluminium and copper plus refrigerant charge.
	CCoilJK float64
	// UACoil: 9 W/K at 1 m/s, sqrt scaling, 2 W/K with no fan.
	UACoil float64
	// CoilContact: 0.55 fraction of manifold air that reaches coil temperature;
	// sets condensation rate.
	CoilContact float64
	// ManifoldAreaM2: 0.0113 m2: 120 mm fan circle.
	ManifoldAreaM2 float64
	// FrostRefG: 150 g of frost halves coil conductance.
	FrostRefG float64
	DrainTauS float64 // 300 s for liquid on the coil to reach the drain.
	FilmMaxG  float64 // 15 g clings to about 0.5 m2 of fin (30 g/m2) and can re-evaporate.

	// --- Heater, humidifier, optional dehumidifier ---
	HeatW        float64 // 100 W: small PTC or cartridge element.
	HeatTauS     float64 // 40 s element lag.
	HumidGPerS   float64 // 0.020 g/s = 72 mL/h: small ultrasonic fogger.
	HumidLagS    float64 // 8 s from relay on to mist reaching the air.
	DehumidGPerS float64 // 0 if not fitted. 0.05 g/s for a small Peltier unit.

	// --- Moisture buffering ---
	// SorbG: 40 g per unit of RH. Racks, wood, casings' surface biofilm, gaskets.
	SorbG    float64
	SorbTauS float64 // 1200 s.

	// --- Product ---
	ProductKg    float64 // 10 kg: DRY AGER states about 10 kg as optimum for sausage.
	ProductCJKgK float64 // 3000 J/kgK for lean meat.
	// UAProdPerKg: 0.8 W/K per kg at 1 m/s (0.1 m2/kg, h 8 W/m2K).
	// A dense piece is far lower: 0.15 W/K/kg is a time constant of several hours.
	UAProdPerKg float64
	WaterFrac   float64 // 0.60 initial water fraction.
	// RInt: 800 s/m3 at 10 kg. Internal drying resistance; gives about 1.8 %/day
	// fresh at 13 °C/78 %RH, which matches hobby and FSIS-cited rates.
	RInt  float64
	HmAt1 float64 // 0.004 m/s mass-transfer coefficient at 1 m/s manifold flow.

	// --- Room ---
	AmbientC      float64 // 21 °C.
	AmbientSwingC float64 // ±2 °C daily swing.
	AmbientRH     float64 // 50 %.

	// --- Fan and duct ---
	FanMinRPM    float64 // 600 rpm floor (P12 Pro spec).
	FanMaxRPM    float64 // 3000 rpm.
	FanStopDuty  float64 // PWM below this stops the fan. 0 means it holds minimum.
	FanMinDuty   float64 // 10 %: below this the fan holds its minimum speed.
	FanStartDuty float64 // 20 %: needed to start from standstill.
	FanTauS      float64 // 3 s rotor lag.
	VMax         float64 // 5 m/s in the manifold at 3000 rpm (P12 about 3.5 m3/min through 113 cm2 less duct loss).
	FanHeatW     float64 // 1.6 W at full speed.

	// --- Sensors ---
	TBias, RHBias     float64 // Unit-to-unit error. DHT22: ±0.5 °C, ±2 to 5 %RH.
	TNoise, RHNoise   float64 // Sample noise: 0.05 °C, 0.15 %RH.
	TTauS, RHTauS     float64 // DHT22 lag in moving air: 30 s, 12 s.
	DHTErrProb        float64 // 0.01: real DHT22 buses lose about 1 % of frames.
	DHTSpikePerHour   float64 // 0.1 per hour glitch passing the checksum.
	AirBiasPct        float64 // FS3000 gain error: ±5 % of reading class.
	AirNoise          float64 // 0.03 m/s turbulence and noise.
	ScaleCountsPerG   float64 // 105 counts/g: 20 kg cell, 2 mV/V, gain 128, 5 V excitation.
	ScaleNoiseCounts  float64 // 40 counts rms.
	ScaleTempCoGPerK  float64 // 0.4 g/K, typical cell plus frame.
	ScaleCreepGPerDay float64 // 0.5 g/day.
	ScaleTareCounts   float64 // Rack and hook tare in counts.

	// DoorSwitch says whether a door switch is fitted.
	DoorSwitch bool
	// RTC says whether the wall clock survives power loss.
	RTC bool

	Seed uint64
}

// BasePlant is the central estimate.
func BasePlant() Params {
	return Params{
		Name:              "base",
		VolumeM3:          0.35,
		CWallJK:           12000,
		UAAirWall:         30,
		UAWallAmb:         2.2,
		InfilACH:          0.15,
		DoorExchangeTauS:  25,
		CompW:             130,
		CompCoilSlope:     0.025,
		CompAmbSlope:      -0.012,
		CompTauS:          60,
		CompStopTauS:      20,
		CompCOP:           2.0,
		CoilFloorC:        -28,
		CCoilJK:           1500,
		UACoil:            9,
		CoilContact:       0.55,
		ManifoldAreaM2:    0.0113,
		FrostRefG:         150,
		DrainTauS:         300,
		FilmMaxG:          15,
		HeatW:             100,
		HeatTauS:          40,
		HumidGPerS:        0.020,
		HumidLagS:         8,
		SorbG:             40,
		SorbTauS:          1200,
		ProductKg:         10,
		ProductCJKgK:      3000,
		UAProdPerKg:       0.8,
		WaterFrac:         0.60,
		RInt:              800,
		HmAt1:             0.004,
		AmbientC:          21,
		AmbientSwingC:     2,
		AmbientRH:         50,
		FanMinRPM:         600,
		FanMaxRPM:         3000,
		FanMinDuty:        10,
		FanStartDuty:      20,
		FanTauS:           3,
		VMax:              5,
		FanHeatW:          1.6,
		TNoise:            0.05,
		RHNoise:           0.15,
		TTauS:             30,
		RHTauS:            12,
		DHTErrProb:        0.01,
		DHTSpikePerHour:   0.1,
		AirNoise:          0.03,
		ScaleCountsPerG:   105,
		ScaleNoiseCounts:  40,
		ScaleTempCoGPerK:  0.4,
		ScaleCreepGPerDay: 0.5,
		ScaleTareCounts:   50000,
		DoorSwitch:        true,
		RTC:               true,
		Seed:              1,
	}
}

// sweepRange is one varied parameter. The range is a judgement of what a real
// build could plausibly be, deliberately including poor ones.
type sweepRange struct {
	field string
	lo    float64
	hi    float64
}

// SweepRanges lists what the sweep varies. Exported so that it can be printed
// into the evidence.
var SweepRanges = []sweepRange{
	{"VolumeM3", 0.20, 0.60},
	{"CWallJK", 7000, 22000},
	{"UAAirWall", 18, 45},
	{"UAWallAmb", 1.4, 3.6},
	{"InfilACH", 0.05, 0.6},
	{"DoorExchangeTauS", 5, 50},
	{"CompW", 90, 200},
	{"CompTauS", 30, 240},
	{"CompStopTauS", 15, 120},
	{"CoilFloorC", -28, -12},
	{"FrostRefG", 80, 300},
	{"DrainTauS", 120, 900},
	{"HeatTauS", 15, 120},
	{"SorbTauS", 400, 3600},
	{"HmAt1", 0.002, 0.008},
	{"CCoilJK", 800, 2500},
	{"UACoil", 6, 14},
	{"CoilContact", 0.35, 0.75},
	{"FilmMaxG", 8, 40},
	{"HeatW", 60, 180},
	{"HumidGPerS", 0.010, 0.045},
	{"HumidLagS", 3, 20},
	{"SorbG", 12, 90},
	{"ProductKg", 2, 22},
	{"RInt", 500, 1400},
	{"AmbientC", 10, 34},
	{"AmbientSwingC", 0, 4},
	{"AmbientRH", 30, 70},
	{"VMax", 3.5, 7},
	{"UAProdPerKg", 0.15, 1.0},
	{"ProductCJKgK", 2500, 3400},
	{"CompCOP", 1.4, 2.6},
	{"TBias", -1.0, 1.0},
	{"RHBias", -5, 5},
	{"TNoise", 0.02, 0.12},
	{"RHNoise", 0.05, 0.4},
	{"TTauS", 15, 60},
	{"RHTauS", 6, 30},
	{"DHTErrProb", 0, 0.05},
	{"DHTSpikePerHour", 0, 1},
	{"AirBiasPct", -5, 5},
	{"AirNoise", 0.01, 0.08},
	{"ScaleNoiseCounts", 15, 120},
	{"ScaleTempCoGPerK", 0, 1.5},
	{"ScaleCreepGPerDay", 0, 2},
}

func setField(p *Params, field string, v float64) {
	f := reflect.ValueOf(p).Elem().FieldByName(field)
	f.SetFloat(v)
}

// Sweep returns n plants. The first ones are stress corners chosen to be bad
// for a controller: cabinets and rooms at their extremes. The rest are a Latin
// hypercube over SweepRanges, so every parameter's whole range gets covered with
// everything else varying at the same time. Everything comes from the seed.
func Sweep(n int, seed uint64) []Params {
	rng := rand.New(rand.NewPCG(seed, 0x5eed))
	out := []Params{}
	corner := func(name string, kv map[string]float64, noSwitch bool) {
		p := BasePlant()
		p.Name = name
		for k, v := range kv {
			setField(&p, k, v)
		}
		if noSwitch {
			p.DoorSwitch = false
		}
		out = append(out, p)
	}
	corner("c01-hot-room-weak-comp", map[string]float64{"AmbientC": 30, "CompW": 90, "UAWallAmb": 3.6}, false)
	corner("c02-cold-room", map[string]float64{"AmbientC": 14, "UAWallAmb": 3.6}, false)
	corner("c03-small-big-comp", map[string]float64{"VolumeM3": 0.2, "CWallJK": 7000, "CompW": 200, "ProductKg": 2}, false)
	corner("c04-large-loaded", map[string]float64{"VolumeM3": 0.6, "CWallJK": 22000, "ProductKg": 22}, false)
	corner("c05-small-big-humidifier", map[string]float64{"VolumeM3": 0.2, "HumidGPerS": 0.045, "SorbG": 12, "ProductKg": 2}, false)
	corner("c06-weak-humidifier-dry-room", map[string]float64{"HumidGPerS": 0.010, "AmbientRH": 30, "InfilACH": 0.6}, false)
	corner("c07-no-door-switch", nil, true)
	corner("c08-noisy-sensors", map[string]float64{"TNoise": 0.12, "RHNoise": 0.4, "DHTErrProb": 0.05, "DHTSpikePerHour": 1, "AirNoise": 0.08, "ScaleNoiseCounts": 120}, false)
	corner("c09-biased-sensors", map[string]float64{"TBias": 0.5, "RHBias": -3, "AirBiasPct": 5}, false)
	corner("c10-leaky-humid-room", map[string]float64{"InfilACH": 0.6, "AmbientRH": 70, "AmbientC": 26}, false)
	corner("c11-free-duct", map[string]float64{"VMax": 7}, false)
	corner("c12-tight-duct", map[string]float64{"VMax": 3.5}, false)
	corner("c13-unheated-garage-cold", map[string]float64{"AmbientC": 5, "UAWallAmb": 3.0}, false)
	corner("c14-hot-summer-room", map[string]float64{"AmbientC": 36, "AmbientSwingC": 4}, false)
	corner("c15-dense-whole-muscle", map[string]float64{"UAProdPerKg": 0.15, "ProductCJKgK": 3400, "ProductKg": 15}, false)
	// Converted domestic fridges: a small compressor that cannot pull a coil much below
	// -15 °C, a weaker coil, and a door that exchanges the air in a few seconds.
	corner("c17-fridge-class", map[string]float64{"CompW": 70, "CompCOP": 1.2, "CoilFloorC": -15, "UACoil": 6, "DoorExchangeTauS": 5, "UAAirWall": 20, "FrostRefG": 80}, false)
	corner("c18-fridge-class-warm-room", map[string]float64{"CompW": 70, "CompCOP": 1.2, "CoilFloorC": -15, "UACoil": 6, "DoorExchangeTauS": 5, "UAAirWall": 20, "FrostRefG": 80, "AmbientC": 28}, false)
	corner("c16-aged-sensors", map[string]float64{"TBias": -1.0, "RHBias": 5}, false)
	if n < len(out) {
		return out[:n]
	}
	m := n - len(out)
	// Latin hypercube: each range is cut into m strata, and each plant takes a
	// different stratum for each parameter.
	perms := make([][]int, len(SweepRanges))
	for i := range perms {
		perms[i] = rng.Perm(m)
	}
	for j := 0; j < m; j++ {
		p := BasePlant()
		p.Name = fmt.Sprintf("s%02d", j+1)
		p.Seed = uint64(j + 100)
		for i, r := range SweepRanges {
			u := (float64(perms[i][j]) + rng.Float64()) / float64(m)
			setField(&p, r.field, r.lo+u*(r.hi-r.lo))
		}
		p.DoorSwitch = rng.Float64() < 0.75
		p.RTC = rng.Float64() < 0.5
		if rng.Float64() < 0.25 {
			p.FanStopDuty = 5
		}
		out = append(out, p)
	}
	return out
}

// SweepRangeList returns the sweep ranges as printable rows.
func SweepRangeList() [][3]string {
	out := make([][3]string, 0, len(SweepRanges))
	for _, r := range SweepRanges {
		out = append(out, [3]string{r.field, fmt.Sprint(r.lo), fmt.Sprint(r.hi)})
	}
	return out
}
