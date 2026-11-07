package sim

import (
	"math/rand/v2"
)

// PlantState is everything a plant needs to carry on after the process running
// it dies, so a simulated cabinet can survive a real power cut on the rig. It is
// additive: only cmd/cured uses it. The random stream is not kept; a restored
// plant reseeds, so a resumed run is not bit-for-bit the run it would have been.
type PlantState struct {
	P Params

	T                                                float64
	TAir, TWall, TProd, TCoil, QComp, QHeat          float64
	WAir, Sorb, Film, Frost, Drained, HumidIn, EvapG float64
	ProdKg, ProdWater0, ProdDry, ProdWater           float64
	Powered, CmdCool, CmdHeat, CmdHumid, CmdDehumid  bool
	CmdAlarm, CmdBuzzer, DoorIsOpen                  bool
	HumLag, FanDuty, FanRPM, FanV                    float64
	FanRunning                                       bool
	EnergyWh                                         float64
	Starts, HeatCycles, HumCycles                    int
	LastEffCool, LastEffHeat, LastEffHum             bool
	ST, SRH, DHTLastSampleT, LastTemp, LastRH        float64
	DHTHave, DHTErr                                  bool
	AirLast, ScaleCreep, TachLast                    float64
	StuckT, StuckRH, StuckA                          float64
	StuckS                                           int32
	HaveStuckT, HaveStuckRH, HaveStuckA, HaveStuckS  bool
	LastSpikeT, AmbientOffset                        float64
	FaultKinds                                       [devCount]FaultKind
	FaultParams, FaultSince                          [devCount]float64
}

// SaveState copies the plant's state.
func (pl *Plant) SaveState() PlantState {
	return PlantState{
		P: pl.P, T: pl.T,
		TAir: pl.TAir, TWall: pl.TWall, TProd: pl.TProd, TCoil: pl.TCoil, QComp: pl.QComp, QHeat: pl.QHeat,
		WAir: pl.WAir, Sorb: pl.Sorb, Film: pl.Film, Frost: pl.Frost, Drained: pl.Drained, HumidIn: pl.HumidIn, EvapG: pl.EvapG,
		ProdKg: pl.ProdKg, ProdWater0: pl.ProdWater0, ProdDry: pl.ProdDry, ProdWater: pl.ProdWater,
		Powered: pl.Powered, CmdCool: pl.CmdCool, CmdHeat: pl.CmdHeat, CmdHumid: pl.CmdHumid, CmdDehumid: pl.CmdDehumid,
		CmdAlarm: pl.CmdAlarm, CmdBuzzer: pl.CmdBuzzer, DoorIsOpen: pl.DoorIsOpen,
		HumLag: pl.humLag, FanDuty: pl.FanDuty, FanRPM: pl.FanRPM, FanV: pl.FanV, FanRunning: pl.fanRunning,
		EnergyWh: pl.energyWh, Starts: pl.starts, HeatCycles: pl.heatCycles, HumCycles: pl.humCycles,
		LastEffCool: pl.lastEffCool, LastEffHeat: pl.lastEffHeat, LastEffHum: pl.lastEffHum,
		ST: pl.sT, SRH: pl.sRH, DHTLastSampleT: pl.dhtLastSampleT, LastTemp: pl.lastTemp, LastRH: pl.lastRH,
		DHTHave: pl.dhtHave, DHTErr: pl.dhtErr, AirLast: pl.airLast, ScaleCreep: pl.scaleCreep, TachLast: pl.tachLast,
		StuckT: pl.stuckT, StuckRH: pl.stuckRH, StuckA: pl.stuckA, StuckS: pl.stuckS,
		HaveStuckT: pl.haveStuckT, HaveStuckRH: pl.haveStuckRH, HaveStuckA: pl.haveStuckA, HaveStuckS: pl.haveStuckS,
		LastSpikeT: pl.lastSpikeT, AmbientOffset: pl.AmbientOffset,
		FaultKinds: pl.Faults.k, FaultParams: pl.Faults.param, FaultSince: pl.Faults.since,
	}
}

// RestorePlant rebuilds a plant from saved state. seed picks the new random stream.
func RestorePlant(s PlantState, seed uint64) *Plant {
	pl := &Plant{P: s.P, rng: rand.New(rand.NewPCG(seed, 0xcab2)), T: s.T,
		TAir: s.TAir, TWall: s.TWall, TProd: s.TProd, TCoil: s.TCoil, QComp: s.QComp, QHeat: s.QHeat,
		WAir: s.WAir, Sorb: s.Sorb, Film: s.Film, Frost: s.Frost, Drained: s.Drained, HumidIn: s.HumidIn, EvapG: s.EvapG,
		ProdKg: s.ProdKg, ProdWater0: s.ProdWater0, ProdDry: s.ProdDry, ProdWater: s.ProdWater,
		Powered: s.Powered, CmdCool: s.CmdCool, CmdHeat: s.CmdHeat, CmdHumid: s.CmdHumid, CmdDehumid: s.CmdDehumid,
		CmdAlarm: s.CmdAlarm, CmdBuzzer: s.CmdBuzzer, DoorIsOpen: s.DoorIsOpen,
		humLag: s.HumLag, FanDuty: s.FanDuty, FanRPM: s.FanRPM, FanV: s.FanV, fanRunning: s.FanRunning,
		energyWh: s.EnergyWh, starts: s.Starts, heatCycles: s.HeatCycles, humCycles: s.HumCycles,
		lastEffCool: s.LastEffCool, lastEffHeat: s.LastEffHeat, lastEffHum: s.LastEffHum,
		sT: s.ST, sRH: s.SRH, dhtLastSampleT: s.DHTLastSampleT, lastTemp: s.LastTemp, lastRH: s.LastRH,
		dhtHave: s.DHTHave, dhtErr: s.DHTErr, airLast: s.AirLast, scaleCreep: s.ScaleCreep, tachLast: s.TachLast,
		stuckT: s.StuckT, stuckRH: s.StuckRH, stuckA: s.StuckA, stuckS: s.StuckS,
		haveStuckT: s.HaveStuckT, haveStuckRH: s.HaveStuckRH, haveStuckA: s.HaveStuckA, haveStuckS: s.HaveStuckS,
		lastSpikeT: s.LastSpikeT, AmbientOffset: s.AmbientOffset,
	}
	pl.Faults.k, pl.Faults.param, pl.Faults.since = s.FaultKinds, s.FaultParams, s.FaultSince
	return pl
}
