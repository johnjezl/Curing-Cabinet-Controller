package core

import "github.com/johnjezl/curing-cabinet-controller/internal/api"

// ProbeView is a cheap copy of the values a test harness logs on every tick. It
// holds nothing the operator does not also see through Snapshot.
type ProbeView struct {
	Temp, RH, Air, Weight               float64
	TempQ, RHQ, AirQ, WeightQ           api.Quality
	Set                                 api.Setpoints
	Cool, Heat, Humid, Dehumid, Defrost bool
	Alarm, Buzzer                       bool
	Fan, RPM                            float64
	ActiveWarn                          int
	RunState                            api.RunState
	Phase                               int
	TotalElapsed, PhaseElapsed          float64
	LossPct                             float64
}

// Probe returns a ProbeView.
func (c *Core) Probe() ProbeView {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := ProbeView{
		Temp: c.tF.v, RH: c.rhF.v, Air: c.airF.v, Weight: c.weightF.v,
		TempQ: c.chT.quality, RHQ: c.chRH.quality, AirQ: c.chAir.quality, WeightQ: c.chScale.quality,
		Set:  c.setpoint(),
		Cool: c.ctl.comp.on, Heat: c.ctl.heatOn, Humid: c.ctl.humOn, Dehumid: c.ctl.dehumOut, Defrost: c.ctl.defrost,
		Alarm: c.alarmOut, Buzzer: c.buzzerOut, Fan: c.fanOut, RPM: c.rpm,
		RunState: c.st.Program.State, Phase: c.st.Program.PhaseIndex,
		TotalElapsed: c.st.Program.TotalElapsed, PhaseElapsed: c.st.Program.PhaseElapsed,
	}
	if !c.scaleCal() {
		v.WeightQ = api.QualityOff
	} else if c.scaleImplausible {
		v.WeightQ = api.QualitySuspect
	}
	if c.weightUsable() {
		v.LossPct = c.lossPct()
	}
	for _, a := range c.al.active {
		if a.a.Severity != api.SevInfo {
			v.ActiveWarn++
		}
	}
	return v
}
