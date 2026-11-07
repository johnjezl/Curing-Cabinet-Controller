package sim

import (
	"math"
	"math/rand/v2"
)

// psat is saturation vapor pressure (Pa), Magnus form, over water.
func psat(tC float64) float64 { return 610.94 * math.Exp(17.625*tC/(tC+243.04)) }

// vaporDensity is saturation vapor density (g/m3) at tC.
func vaporDensity(tC float64) float64 { return psat(tC) / (461.5 * (tC + 273.15)) * 1000 }

const (
	latentJPerG = 2500.0
	fusionJPerG = 334.0
	airRho      = 1.2
	airCp       = 1005.0
)

// Plant is the simulated cabinet. Step advances it. It has a single state with
// no goroutines, so one seed gives one result.
type Plant struct {
	P   Params
	rng *rand.Rand

	T float64 // virtual seconds since the plant was created

	// Thermal state.
	TAir, TWall, TProd, TCoil float64
	QComp                     float64 // current cooling capacity W
	QHeat                     float64 // element output W

	// Moisture state.
	WAir    float64 // g vapour in the cabinet air
	Sorb    float64 // sorption buffer fraction 0..1
	Film    float64 // g liquid on the coil
	Frost   float64 // g ice on the coil
	Drained float64 // g sent down the drain
	HumidIn float64 // g humidifier output, cumulative
	EvapG   float64 // g evaporated from product, cumulative

	// Product.
	ProdKg     float64 // current mass
	ProdWater0 float64 // g of water at start (per the initial load)
	ProdDry    float64 // g of dry matter
	ProdWater  float64 // g

	// Commanded and effective outputs.
	Powered                                bool
	CmdCool, CmdHeat, CmdHumid, CmdDehumid bool
	CmdAlarm, CmdBuzzer                    bool
	DoorIsOpen                             bool
	humLag                                 float64
	FanDuty, FanRPM, FanV                  float64
	fanRunning                             bool
	energyWh                               float64
	starts, heatCycles, humCycles          int
	lastEffCool, lastEffHeat, lastEffHum   bool

	// Sensor state.
	sT, sRH                 float64 // sensor lagged state
	dhtLastSampleT          float64
	dhtHave                 bool
	lastTemp, lastRH        float64
	dhtErr                  bool
	airLast                 float64
	scaleCreep              float64
	tachLast                float64
	stuckT, stuckRH, stuckA float64
	stuckS                  int32
	haveStuckT, haveStuckRH bool
	haveStuckA, haveStuckS  bool
	lastSpikeT              float64

	Faults Faults

	AmbientOffset float64
}

// NewPlant builds a plant at room temperature with its load in equilibrium.
func NewPlant(p Params) *Plant {
	pl := &Plant{P: p, rng: rand.New(rand.NewPCG(p.Seed, 0xcab1)), Powered: true}
	amb := p.AmbientC
	pl.TAir, pl.TWall, pl.TProd, pl.TCoil = amb, amb, amb, amb
	pl.WAir = p.AmbientRH / 100 * vaporDensity(amb) * p.VolumeM3
	pl.Sorb = p.AmbientRH / 100
	pl.setProduct(p.ProductKg)
	pl.sT, pl.sRH = amb, p.AmbientRH
	pl.dhtLastSampleT = -10
	return pl
}

func (pl *Plant) setProduct(kg float64) {
	pl.ProdKg = kg
	pl.ProdWater = kg * 1000 * pl.P.WaterFrac
	pl.ProdWater0 = pl.ProdWater
	pl.ProdDry = kg*1000 - pl.ProdWater
}

// SetInitial puts the cabinet and its contents at a given condition.
func (pl *Plant) SetInitial(tC, rh float64) {
	pl.TAir, pl.TWall, pl.TProd, pl.TCoil = tC, tC, tC, tC
	pl.WAir = rh / 100 * vaporDensity(tC) * pl.P.VolumeM3
	pl.Sorb = rh / 100
	pl.sT, pl.sRH = tC, rh
}

// LoadProduct adds fresh product at tempC. It models opening a door separately.
func (pl *Plant) LoadProduct(kg, tempC float64) {
	c0 := pl.ProdKg * pl.P.ProductCJKgK
	c1 := kg * pl.P.ProductCJKgK
	if c0+c1 > 0 {
		pl.TProd = (pl.TProd*c0 + tempC*c1) / (c0 + c1)
	}
	water := kg * 1000 * pl.P.WaterFrac
	pl.ProdWater += water
	pl.ProdWater0 += water
	pl.ProdDry += kg*1000 - water
	pl.ProdKg += kg
}

// LossFraction is the fraction of initial water lost.
func (pl *Plant) lossFraction() float64 {
	if pl.ProdWater0 <= 0 {
		return 0
	}
	return clamp01(1 - pl.ProdWater/pl.ProdWater0)
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// Ambient returns room temperature at time T.
func (pl *Plant) Ambient() float64 {
	return pl.P.AmbientC + pl.AmbientOffset + pl.P.AmbientSwingC*math.Sin(2*math.Pi*pl.T/86400)
}

// RH returns the true relative humidity.
func (pl *Plant) RH() float64 {
	return pl.WAir / (vaporDensity(pl.TAir) * pl.P.VolumeM3) * 100
}

// EffCool says whether the compressor contactor is really closed.
func (pl *Plant) EffCool() bool { return pl.effective(DevCool, pl.CmdCool) }

// EffHeat says whether the heater contactor is really closed.
func (pl *Plant) EffHeat() bool { return pl.effective(DevHeat, pl.CmdHeat) }

// EffHumid says whether the humidifier contactor is really closed.
func (pl *Plant) EffHumid() bool { return pl.effective(DevHumid, pl.CmdHumid) }

func (pl *Plant) effective(d Device, cmd bool) bool {
	if !pl.Powered {
		return false
	}
	switch pl.Faults.kind(d) {
	case FaultRelayStuckOn:
		return true
	case FaultRelayStuckOff:
		return false
	}
	return cmd
}

// Step advances the plant by dt seconds, sub-stepping for stability.
func (pl *Plant) Step(dt float64) {
	for dt > 0 {
		h := math.Min(dt, 1.0)
		pl.step(h)
		dt -= h
	}
}

func (pl *Plant) step(h float64) {
	p := &pl.P
	pl.T += h
	amb := pl.Ambient()

	// --- Fan ---
	fanFault := pl.Faults.kind(DevFan)
	duty := pl.FanDuty
	if !pl.Powered {
		duty = 0
	}
	target := 0.0
	stalled := fanFault == FaultStall
	switch {
	case stalled || duty <= 0 || duty < p.FanStopDuty:
		pl.fanRunning = false
	case pl.fanRunning || duty >= p.FanStartDuty:
		pl.fanRunning = true
		if duty <= p.FanMinDuty {
			target = p.FanMinRPM
		} else {
			target = p.FanMinRPM + (duty-p.FanMinDuty)/(100-p.FanMinDuty)*(p.FanMaxRPM-p.FanMinRPM)
		}
	}
	pl.FanRPM += (target - pl.FanRPM) * h / (p.FanTauS + h)
	if pl.FanRPM < 1 {
		pl.FanRPM = 0
	}
	vmax := p.VMax
	if pl.Faults.kind(DevDuct) == FaultBlocked {
		vmax *= 0.01
	}
	pl.FanV = pl.FanRPM / p.FanMaxRPM * vmax
	// Velocity scaling floor: natural convection when the fan is stopped.
	vEff := math.Max(pl.FanV, 0.05)
	s := math.Sqrt(vEff)

	// --- Compressor ---
	coolOn := pl.EffCool()
	qTarget := 0.0
	if coolOn && pl.Faults.kind(DevCompressor) != FaultNoCool {
		qTarget = p.CompW * (1 + p.CompCoilSlope*(pl.TCoil+10)) * (1 + p.CompAmbSlope*(amb-25))
		if qTarget < 0 || pl.TCoil < p.CoilFloorC {
			qTarget = 0
		}
		pl.QComp += (qTarget - pl.QComp) * h / (p.CompTauS + h)
	} else {
		pl.QComp += (0 - pl.QComp) * h / (p.CompStopTauS + h)
	}
	if coolOn && !pl.lastEffCool {
		pl.starts++
	}
	pl.lastEffCool = coolOn
	if coolOn {
		pl.energyWh += p.CompW / p.CompCOP * h / 3600
	}

	// --- Heater ---
	heatOn := pl.EffHeat()
	heatTarget := 0.0
	if heatOn {
		heatTarget = p.HeatW
		pl.energyWh += p.HeatW * h / 3600
	}
	pl.QHeat += (heatTarget - pl.QHeat) * h / (p.HeatTauS + h)
	if heatOn && !pl.lastEffHeat {
		pl.heatCycles++
	}
	pl.lastEffHeat = heatOn

	// --- Humidifier ---
	humOn := pl.EffHumid() && pl.Faults.kind(DevHumidifier) != FaultEmpty
	if humOn {
		pl.humLag += h
	} else {
		pl.humLag = 0
	}
	if pl.EffHumid() && !pl.lastEffHum {
		pl.humCycles++
	}
	pl.lastEffHum = pl.EffHumid()
	humG := 0.0
	if humOn && pl.humLag >= p.HumidLagS {
		humG = p.HumidGPerS * h
	}
	pl.HumidIn += humG

	// --- Coil ---
	ua := p.UACoil * s
	if pl.FanV <= 0.05 {
		ua = p.UACoil * 0.22
	}
	frostFactor := 1 / (1 + pl.Frost/p.FrostRefG)
	ua *= frostFactor
	qCoilAir := ua * (pl.TAir - pl.TCoil) // W into the coil from the air

	// Moisture on the coil.
	airFlow := math.Max(pl.FanV, 0.02) * p.ManifoldAreaM2 // m3/s
	rhoV := pl.WAir / p.VolumeM3
	rhoCoil := vaporDensity(pl.TCoil)
	condense := airFlow * p.CoilContact * (rhoV - rhoCoil) * frostFactor // g/s; negative evaporates
	var coilLatent float64
	maxCond := pl.WAir * 0.5 / h
	if condense > maxCond {
		condense = maxCond
	}
	if condense > 0 {
		dm := condense * h
		pl.WAir -= dm
		if pl.TCoil < 0 {
			pl.Frost += dm
			coilLatent = (latentJPerG + fusionJPerG) * condense
		} else {
			pl.Film += dm
			coilLatent = latentJPerG * condense
		}
	} else if condense < 0 && pl.Film > 0 {
		dm := math.Min(-condense*h, pl.Film)
		pl.Film -= dm
		pl.WAir += dm
		coilLatent = -latentJPerG * dm / h
	}
	if pl.Film > p.FilmMaxG {
		pl.Drained += pl.Film - p.FilmMaxG
		pl.Film = p.FilmMaxG
	}
	if pl.Film > 0 && pl.TCoil > 0 {
		dm := pl.Film * h / p.DrainTauS
		pl.Film -= dm
		pl.Drained += dm
	}
	// Melt frost when the coil is above zero. The melt holds the coil at 0.
	if pl.Frost > 0 && pl.TCoil > 0 {
		melt := math.Min(pl.Frost, pl.TCoil*p.CCoilJK/fusionJPerG)
		pl.Frost -= melt
		pl.Film += melt
		pl.TCoil -= melt * fusionJPerG / p.CCoilJK
	}
	pl.TCoil += (qCoilAir + coilLatent - pl.QComp) * h / p.CCoilJK

	// --- Air ---
	cAir := p.VolumeM3 * airRho * airCp
	uaWall := p.UAAirWall * s
	if pl.FanV <= 0.05 {
		uaWall = p.UAAirWall * 0.25
	}
	uaProd := 0.0
	if pl.ProdKg > 0 {
		uaProd = p.UAProdPerKg * pl.ProdKg * s
		if pl.FanV <= 0.05 {
			uaProd = p.UAProdPerKg * pl.ProdKg * 0.25
		}
	}
	qWall := uaWall * (pl.TWall - pl.TAir)
	qProd := uaProd * (pl.TProd - pl.TAir)
	qFan := p.FanHeatW * math.Pow(pl.FanRPM/p.FanMaxRPM, 3)
	// Air exchange with the room.
	ach := p.InfilACH / 3600
	if pl.DoorIsOpen {
		ach = 1 / p.DoorExchangeTauS
	}
	qInfil := ach * cAir * (amb - pl.TAir)
	if humG > 0 {
		// Ultrasonic mist evaporates using heat from the air.
		qFan -= latentJPerG * humG / h
	}
	pl.TAir += (qWall + qProd - qCoilAir + pl.QHeat + qFan + qInfil) * h / cAir
	if pl.FanRPM > 0 {
		pl.energyWh += 3 * h / 3600
	}

	// --- Wall ---
	// Door open: room air also washes the liner.
	uaWallAmb := p.UAWallAmb
	pl.TWall += (-qWall + uaWallAmb*(amb-pl.TWall)) * h / p.CWallJK

	// --- Product evaporation ---
	evap := 0.0
	if pl.ProdKg > 0 {
		f := pl.lossFraction()
		aw := 0.97 - 0.18*f
		area := 0.1 * pl.ProdKg
		hm := p.HmAt1 * s
		rExt := 1 / (hm * area)
		rInt := p.RInt * (10 / math.Max(pl.ProdKg, 0.5)) * (1 + 4*f)
		rhoA := pl.WAir / p.VolumeM3
		evap = (aw*vaporDensity(pl.TProd) - rhoA) / (rExt + rInt)
		if evap > 0 {
			evap = math.Min(evap, pl.ProdWater/h)
		} else {
			evap *= 0.25 // uptake is much slower than loss
		}
		dm := evap * h
		pl.ProdWater -= dm
		pl.ProdKg = (pl.ProdWater + pl.ProdDry) / 1000
		pl.WAir += dm
		pl.EvapG += dm
		cProd := pl.ProdKg * p.ProductCJKgK
		pl.TProd += (-qProd - latentJPerG*evap) * h / cProd
	}

	// --- Moisture balance ---
	if humG > 0 {
		pl.WAir += humG
	}
	sat := vaporDensity(pl.TAir) * p.VolumeM3
	ambV := p.AmbientRH / 100 * vaporDensity(amb)
	pl.WAir += ach * (ambV*p.VolumeM3 - pl.WAir) * h
	// Sorption buffer exchanges with the air.
	rh := clamp01(pl.WAir / sat)
	sorbFlux := p.SorbG / p.SorbTauS * (rh - pl.Sorb) // g/s into the buffer
	pl.Sorb += sorbFlux * h / p.SorbG
	pl.WAir -= sorbFlux * h
	// Dedicated dehumidifier if fitted.
	if pl.effective(DevDehumid, pl.CmdDehumid) && p.DehumidGPerS > 0 {
		dm := math.Min(p.DehumidGPerS*h, pl.WAir*0.2)
		pl.WAir -= dm
		pl.Drained += dm
	}
	// Anything over saturation condenses on walls (and comes back through the
	// buffer later). Fog does not stay in the air.
	sat = vaporDensity(pl.TAir) * p.VolumeM3
	if pl.WAir > sat {
		ex := pl.WAir - sat
		pl.WAir = sat
		room := (1 - pl.Sorb) * p.SorbG
		took := math.Min(ex, room)
		pl.Sorb += took / p.SorbG
		// What the surfaces cannot hold runs down the walls to the drain.
		pl.Drained += ex - took
	}
	if pl.WAir < 0 {
		pl.WAir = 0
	}

	// --- Sensor lag ---
	pl.sT += (pl.TAir - pl.sT) * h / (p.TTauS + h)
	pl.sRH += (pl.RH() - pl.sRH) * h / (p.RHTauS + h)
	pl.scaleCreep += p.ScaleCreepGPerDay * h / 86400
}

// EnergyWh returns electrical energy consumed so far.
func (pl *Plant) EnergyWh() float64 { return pl.energyWh }

// Starts returns compressor physical starts so far.
func (pl *Plant) Starts() int { return pl.starts }

// HeatCycles returns heater relay closures so far.
func (pl *Plant) HeatCycles() int { return pl.heatCycles }

// HumidCycles returns humidifier relay closures so far.
func (pl *Plant) HumidCycles() int { return pl.humCycles }

// WaterLossPct is the true product weight loss.
func (pl *Plant) WaterLossPct() float64 {
	m0 := (pl.ProdWater0 + pl.ProdDry) / 1000
	if m0 <= 0 {
		return 0
	}
	return (m0 - pl.ProdKg) / m0 * 100
}
