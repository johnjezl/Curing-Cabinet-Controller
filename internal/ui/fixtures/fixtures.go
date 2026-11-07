// Package fixtures holds scripted snapshots and a fake controller for the panel.
// It is used by tests and cmd/uidemo only; production code never imports it.
package fixtures

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// T0 is the fixed instant all fixtures start from.
var T0 = time.Date(2026, 3, 14, 14, 22, 5, 0, time.UTC)

// VerifyText mirrors the core's end-of-cure wording.
const VerifyText = "This recipe has ended. Weight loss is a yield figure and does not show that product is safe. " +
	"Verify water activity and pH with calibrated instruments, and judge the product by your own validated process plan. " +
	"The controller keeps holding conditions until you stop it."

func recipes() []api.Recipe {
	d := 24 * time.Hour
	return []api.Recipe{
		{ID: "salami", Name: "Salami, 60 mm casing", Notes: "Whole-muscle free. Check casing for hardening weekly.", BuiltIn: true, Phases: []api.PhaseSpec{
			{Name: "Fermentation", Setpoint: api.Setpoints{TempC: 22, RHPct: 90, AirMS: 0.3}, Duration: 2 * d},
			{Name: "Drying 1", Setpoint: api.Setpoints{TempC: 15, RHPct: 82, AirMS: 0.5}, Duration: 7 * d},
			{Name: "Drying 2", Setpoint: api.Setpoints{TempC: 13, RHPct: 78, AirMS: 0.4}, Duration: 21 * d},
			{Name: "Finish", Setpoint: api.Setpoints{TempC: 13, RHPct: 75, AirMS: 0.3}, TargetLossPct: 35},
		}},
		{ID: "bresaola", Name: "Bresaola", BuiltIn: true, Phases: []api.PhaseSpec{
			{Name: "Equalise", Setpoint: api.Setpoints{TempC: 4, RHPct: 85, AirMS: 0.2}, Duration: 3 * d},
			{Name: "Dry", Setpoint: api.Setpoints{TempC: 13, RHPct: 75, AirMS: 0.4}, TargetLossPct: 30},
		}},
		{ID: "coppa", Name: "Coppa", BuiltIn: true, Phases: []api.PhaseSpec{
			{Name: "Dry", Setpoint: api.Setpoints{TempC: 12, RHPct: 78, AirMS: 0.4}, TargetLossPct: 30},
		}},
		{ID: "pancetta", Name: "Pancetta, rolled", BuiltIn: true, Phases: []api.PhaseSpec{
			{Name: "Dry", Setpoint: api.Setpoints{TempC: 12, RHPct: 72, AirMS: 0.3}, Duration: 21 * d},
		}},
		{ID: "lomo", Name: "Lomo", BuiltIn: true, Phases: []api.PhaseSpec{
			{Name: "Dry", Setpoint: api.Setpoints{TempC: 13, RHPct: 78, AirMS: 0.4}, TargetLossPct: 35},
		}},
		{ID: "user1", Name: "My short test cycle", Phases: []api.PhaseSpec{
			{Name: "Hold", Setpoint: api.Setpoints{TempC: 14, RHPct: 75, AirMS: 0.4}, Duration: 12 * time.Hour},
		}},
	}
}

func trend(now time.Time, hours int, every time.Duration, gapFrom, gapTo time.Duration) []api.Sample {
	var out []api.Sample
	start := now.Add(-time.Duration(hours) * time.Hour)
	for t := start; !t.After(now); t = t.Add(every) {
		h := t.Sub(start).Hours()
		ago := now.Sub(t)
		s := api.Sample{T: t, Valid: true,
			TempC:   13 + 0.35*math.Sin(h*2*math.Pi/0.7) + 0.1*math.Sin(h*0.37),
			RHPct:   78 + 1.6*math.Sin(h*2*math.Pi/1.1+1),
			AirMS:   0.40 + 0.02*math.Sin(h*0.9),
			WeightG: 4812 + ago.Hours()*3.16, // agrees with Base: 18.2 % of 5880 g at 1.29 %/day
		}
		if ago <= gapFrom && ago >= gapTo {
			s.Valid = false
			s.TempC, s.RHPct, s.AirMS, s.WeightG = 0, 0, 0, 0
		}
		out = append(out, s)
	}
	return out
}

func good(v float64) api.Reading { return api.Reading{Value: v, Quality: api.QualityOK, AgeSec: 1.5} }

func history(now time.Time) []api.Alarm {
	type h struct {
		code, title string
		sev         api.Severity
		start, dur  time.Duration
		peak, limit float64
		unit        string
	}
	list := []h{
		{"DOOR_OPEN", "Door open too long", api.SevWarning, 3 * time.Hour, 7 * time.Minute, 0, 0, ""},
		{"TEMP_HIGH", "Temperature high", api.SevCritical, 26 * time.Hour, 48 * time.Minute, 16.4, 15, "°C"},
		{"RH_LOW", "Humidity low", api.SevWarning, 31 * time.Hour, 22 * time.Minute, 69, 72, "%"},
		{"RH_HIGH", "Humidity high", api.SevWarning, 40 * time.Hour, 95 * time.Minute, 88, 84, "%"},
		{"TEMP_LOW", "Temperature low", api.SevCritical, 49 * time.Hour, 18 * time.Minute, 9.8, 11, "°C"},
		{"SENSOR_FAULT", "Temp/RH sensor fault", api.SevCritical, 60 * time.Hour, 3 * time.Minute, 0, 0, ""},
		{"POWER_LOSS", "Power was lost", api.SevWarning, 72 * time.Hour, 2 * time.Minute, 0, 0, ""},
		{"AIR_LOW", "Airflow low", api.SevWarning, 80 * time.Hour, 41 * time.Minute, 0.08, 0.2, "m/s"},
		{"DEFROST_LONG", "Defrost too long", api.SevWarning, 96 * time.Hour, 12 * time.Minute, 0, 0, ""},
		{"LOG_FAIL", "Log write failed", api.SevInfo, 120 * time.Hour, 1 * time.Minute, 0, 0, ""},
	}
	out := make([]api.Alarm, 0, len(list))
	for i, e := range list {
		s := now.Add(-e.start)
		out = append(out, api.Alarm{ID: fmt.Sprintf("h%d", i), Code: e.code, Severity: e.sev, Title: e.title,
			Action: "Cleared.", Since: s, Cleared: s.Add(e.dur), Acked: true, ClockOK: true, DurationSec: e.dur.Seconds(), Peak: e.peak, Limit: e.limit, Unit: e.unit})
	}
	return out
}

// Base is a healthy cure in progress.
func Base() api.Snapshot {
	now := T0
	rs := recipes()
	return api.Snapshot{
		Now: now, Temp: good(13.2), RH: good(78.4), Air: good(0.41), Weight: good(4812),
		Door:     api.Reading{Value: 0, Quality: api.QualityOK, AgeSec: 0.2},
		Setpoint: api.Setpoints{TempC: 13, RHPct: 78, AirMS: 0.40},
		Outputs: api.Outputs{Cool: true, Humid: false, FanDutyPct: 45, FanRPM: 1320, Status: []api.OutputStatus{
			{Output: "cool", On: true, Reason: "cooling", Kind: "normal"},
			{Output: "heat", Reason: "at or over setpoint", Kind: "normal"},
			{Output: "humid", Reason: "at or over setpoint", Kind: "normal"},
		}},
		Program: api.Program{State: api.RunActive, RecipeID: "salami", RecipeName: "Salami, 60 mm casing", PhaseIndex: 2, PhaseCount: 4,
			PhaseName: "Drying 2", PhaseElapsed: 5*24*time.Hour + 3*time.Hour, PhaseRemaining: 15*24*time.Hour + 21*time.Hour,
			TotalElapsed: 14*24*time.Hour + 3*time.Hour, StartWeightG: 5880, WeightLossPct: 18.2, TargetLossPct: 35, LossPctPerDay: 1.29},
		History:     history(now),
		Trend:       trend(now, 7*24, 5*time.Minute, 0, 0),
		Recipes:     rs,
		Limits:      api.Limits{TempHighDelta: 2, TempLowDelta: 2, RHHighDelta: 6, RHLowDelta: 6, AlarmDelay: 15 * time.Minute, DoorDelay: 5 * time.Minute, AbsTempHighC: 20, AbsTempLowC: 0.5},
		Calibration: api.Calibration{TempOffsetC: -0.2, RHOffsetPct: 1, ScaleZero: 84213, ScaleCountsPG: 412.5},
		Health: api.Health{SpeedFactor: 1, ClockTrusted: true, LogOK: true, StorageFreeM: 4120, WatchdogOK: true,
			StartedAt: now.Add(-9*24*time.Hour - 4*time.Hour), Version: "0.1.0 (a1b2c3d)",
			RefCheckDueSec: 4*86400 + 3*3600, LastRefCheck: now.Add(-3 * 24 * time.Hour)},
	}
}

// settleTrend makes the end of the trend agree with the readings a fixture shows:
// the last rampMin minutes run from the normal band to the current value.
func settleTrend(s *api.Snapshot, rampMin float64) {
	n := len(s.Trend)
	for i := n - 1; i >= 0; i-- {
		ago := s.Now.Sub(s.Trend[i].T).Minutes()
		if ago > rampMin {
			break
		}
		f := 1 - ago/rampMin
		sm := &s.Trend[i]
		if s.Temp.Quality == api.QualityOK && !math.IsNaN(s.Temp.Value) {
			sm.TempC += (s.Temp.Value - sm.TempC) * f
		}
		if s.RH.Quality == api.QualityOK && !math.IsInf(s.RH.Value, 0) {
			sm.RHPct += (s.RH.Value - sm.RHPct) * f
		}
		if s.Air.Quality == api.QualityOK {
			sm.AirMS += (s.Air.Value - sm.AirMS) * f
		}
		if s.Weight.Quality == api.QualityOK {
			sm.WeightG += (s.Weight.Value - sm.WeightG) * f
		}
	}
}

// lossOf keeps the weight a fixture shows in step with its loss figure.
func lossOf(s *api.Snapshot, pct float64) {
	s.Program.WeightLossPct = pct
	s.Weight = good(s.Program.StartWeightG * (1 - pct/100))
}

func alarm(code string, sev api.Severity, title, action string, ago time.Duration, peak, limit float64, unit string) api.Alarm {
	return api.Alarm{ID: strings.ToLower(code) + "-1", Code: code, Severity: sev, Title: title, Action: action,
		Since: T0.Add(-ago), ClockOK: true, Peak: peak, Limit: limit, Unit: unit}
}

// Fixture is a named state.
type Fixture struct {
	Name string
	Make func() api.Snapshot
	// Frozen means the controller stops updating: the panel must report a stall.
	Frozen bool
}

func with(f func(*api.Snapshot)) func() api.Snapshot {
	return func() api.Snapshot {
		s := Base()
		f(&s)
		settleTrend(&s, 40)
		return s
	}
}

var alarmTempHigh = alarm("TEMP_HIGH", api.SevCritical, "Temperature high", "Keep the door shut. Check that the cooling runs. Move product if it stays high.", 22*time.Minute, 16.4, 15, "°C")

// All lists every state the panel must render well.
func All() []Fixture {
	return []Fixture{
		{Name: "normal", Make: Base},
		{Name: "idle-manual", Make: with(func(s *api.Snapshot) { s.Program = api.Program{State: api.RunIdle} })},
		{Name: "alarm-temp-high", Make: with(func(s *api.Snapshot) {
			s.Temp = good(16.4)
			s.Active = []api.Alarm{alarmTempHigh}
		})},
		{Name: "alarm-temp-low", Make: with(func(s *api.Snapshot) {
			s.Temp = good(9.8)
			s.Active = []api.Alarm{alarm("TEMP_LOW", api.SevCritical, "Temperature low", "Check the heater and that cooling is not stuck on. Product may freeze below 0 °C.", 17*time.Minute, 9.8, 11, "°C")}
		})},
		{Name: "alarm-abs-temp", Make: with(func(s *api.Snapshot) {
			s.Temp = good(21.3)
			s.Active = []api.Alarm{alarm("TEMP_ABS_HIGH", api.SevCritical, "Over absolute temperature limit", "Move product to a cold store now. Record how long it was above the limit.", 9*time.Minute, 21.3, 20, "°C")}
		})},
		{Name: "alarm-rh-high", Make: with(func(s *api.Snapshot) {
			s.RH = good(88)
			s.Active = []api.Alarm{alarm("RH_HIGH", api.SevWarning, "Humidity high", "Check the humidifier is not stuck on. Inspect product for slime or mould.", 31*time.Minute, 88, 84, "%")}
		})},
		{Name: "alarm-rh-low", Make: with(func(s *api.Snapshot) {
			s.RH = good(66)
			s.Active = []api.Alarm{alarm("RH_LOW", api.SevWarning, "Humidity low", "Refill the humidifier. Case hardening risk if this persists.", 26*time.Minute, 66, 72, "%")}
		})},
		{Name: "alarm-door", Make: with(func(s *api.Snapshot) {
			s.Door = api.Reading{Value: 1, Quality: api.QualityOK, AgeSec: 372}
			s.Active = []api.Alarm{alarm("DOOR_OPEN", api.SevWarning, "Door open too long", "Close the cabinet door.", 72*time.Second, 0, 0, "")}
		})},
		{Name: "alarm-sensor-fault", Make: with(func(s *api.Snapshot) {
			s.Temp = api.Reading{Quality: api.QualityFault, AgeSec: 840, Note: "no reply from sensor"}
			s.RH = api.Reading{Quality: api.QualityFault, AgeSec: 840, Note: "no reply from sensor"}
			s.Outputs.Cool = false
			s.Active = []api.Alarm{alarm("SENSOR_FAULT", api.SevCritical, "Temp/RH sensor fault", "Outputs are held off. Check the sensor cable. Use a separate thermometer.", 14*time.Minute, 0, 0, "")}
		})},
		{Name: "alarm-air-low", Make: with(func(s *api.Snapshot) {
			s.Air = good(0.08)
			s.Active = []api.Alarm{alarm("AIR_LOW", api.SevWarning, "Airflow low", "Check the fan and the manifold for blockage.", 11*time.Minute, 0.08, 0.2, "m/s")}
		})},
		{Name: "alarm-fan-fail", Make: with(func(s *api.Snapshot) {
			s.Outputs.FanRPM = 0
			s.Air = good(0.02)
			s.Active = []api.Alarm{alarm("FAN_FAIL", api.SevCritical, "Fan not turning", "Check the fan and its cable. Humidity will rise fast without air movement.", 4*time.Minute, 0, 0, "")}
		})},
		{Name: "alarm-scale", Make: with(func(s *api.Snapshot) {
			s.Weight = api.Reading{Quality: api.QualityFault, AgeSec: 300, Note: "HX711 no data"}
			s.Active = []api.Alarm{alarm("SCALE_FAULT", api.SevWarning, "Scale fault", "Check the load cell cable. The weight target will not end the phase meanwhile.", 5*time.Minute, 0, 0, "")}
		})},
		{Name: "alarm-power-back", Make: with(func(s *api.Snapshot) {
			s.Health.LastOutage = api.Outage{Detected: true, At: T0.Add(-12 * time.Minute), Duration: 47 * time.Minute, DurationOK: true, TempOnBackC: 17.8}
			s.Temp = good(15.9)
			s.Active = []api.Alarm{alarm("POWER_LOSS", api.SevWarning, "Power was lost for 47 min", "Temperature on return was 17.8 °C. Review the time above your limit and record it.", 12*time.Minute, 17.8, 15, "°C")}
		})},
		{Name: "alarm-storage", Make: with(func(s *api.Snapshot) {
			s.Health.LogOK = false
			s.Health.StorageFreeM = 3
			s.Active = []api.Alarm{alarm("LOG_FAIL", api.SevWarning, "Log write failed", "Storage is nearly full. Control continues but no records are being kept.", 8*time.Minute, 0, 0, "")}
		})},
		{Name: "alarm-info", Make: with(func(s *api.Snapshot) {
			s.Active = []api.Alarm{alarm("MAINT_FILTER", api.SevInfo, "Filter change due", "Replace the air filter at your next stop.", 6*time.Hour, 0, 0, "")}
		})},
		{Name: "alarm-silenced", Make: with(func(s *api.Snapshot) {
			s.Temp = good(16.4)
			s.Active = []api.Alarm{alarmTempHigh}
			s.BuzzerSilenced = true
		})},
		{Name: "alarm-acked", Make: with(func(s *api.Snapshot) {
			s.Temp = good(16.4)
			a := alarmTempHigh
			a.Acked = true
			s.Active = []api.Alarm{a}
			s.BuzzerSilenced = true
		})},
		{Name: "alarm-many", Make: with(func(s *api.Snapshot) {
			s.Temp = good(16.4)
			s.RH = good(88)
			s.Active = []api.Alarm{
				alarm("RH_HIGH", api.SevWarning, "Humidity high", "Check the humidifier is not stuck on. Inspect product.", 31*time.Minute, 88, 84, "%"),
				alarmTempHigh,
				alarm("DOOR_OPEN", api.SevWarning, "Door open too long", "Close the cabinet door.", 72*time.Second, 0, 0, ""),
				alarm("AIR_LOW", api.SevWarning, "Airflow low", "Check the fan and the manifold for blockage.", 11*time.Minute, 0.08, 0.2, "m/s"),
				alarm("MAINT_FILTER", api.SevInfo, "Filter change due", "Replace the air filter at your next stop.", 6*time.Hour, 0, 0, ""),
			}
		})},
		// Readings just gone bad, before the core's alarm: seconds, on the real core.
		{Name: "stale-temp", Make: with(func(s *api.Snapshot) {
			s.Temp = api.Reading{Value: 13.2, Quality: api.QualityStale, AgeSec: 22}
		})},
		{Name: "fault-temp", Make: with(func(s *api.Snapshot) {
			s.Temp = api.Reading{Quality: api.QualityFault, AgeSec: 25, Note: "checksum errors"}
		})},
		// What the real core raises for the same sensor, with its own words.
		{Name: "temp-fault-core-alarm", Make: with(func(s *api.Snapshot) {
			s.Temp = api.Reading{Value: 13.2, Quality: api.QualityFault, AgeSec: 301}
			s.RH = api.Reading{Value: 78, Quality: api.QualityFault, AgeSec: 301}
			s.Outputs.Heat, s.Outputs.Humid = false, false
			s.Outputs.Holds = []api.Hold{{Output: "heat", Reason: "temperature data is bad"}, {Output: "humid", Reason: "temperature data is bad"}}
			s.Active = []api.Alarm{alarm("SENSOR_CLIMATE_FAULT", api.SevCritical, "Temperature and humidity sensor not reading", "Check the DHT22 wiring. Heater and humidifier are off; cooling repeats its recent pattern with no feedback.", 5*time.Minute, 0, 0, "")}
		})},
		// A defence, not a state the real core was seen to produce: readings long
		// unusable with no alarm at all.
		{Name: "no-alarm-long-fault", Make: with(func(s *api.Snapshot) {
			s.Temp = api.Reading{Value: 13.2, Quality: api.QualityStale, AgeSec: 7300}
		})},
		{Name: "suspect-rh", Make: with(func(s *api.Snapshot) {
			s.RH = api.Reading{Value: 99.9, Quality: api.QualitySuspect, AgeSec: 1, Note: "stuck for 2 h"}
		})},
		{Name: "stale-air-weight", Make: with(func(s *api.Snapshot) {
			s.Air = api.Reading{Value: 0.41, Quality: api.QualityStale, AgeSec: 188}
			s.Weight = api.Reading{Value: 4812, Quality: api.QualityStale, AgeSec: 640}
		})},
		{Name: "suspect-weight", Make: with(func(s *api.Snapshot) {
			s.Weight = api.Reading{Value: 7120, Quality: api.QualitySuspect, AgeSec: 2, Note: "jumped"}
		})},
		{Name: "all-bad", Make: with(func(s *api.Snapshot) {
			s.Temp = api.Reading{Value: 13.2, Quality: api.QualityFault, AgeSec: 7300}
			s.RH = api.Reading{Quality: api.QualityFault, AgeSec: 7300}
			s.Air = api.Reading{Value: 0.2, Quality: api.QualityFault, AgeSec: 7300}
			s.Weight = api.Reading{Quality: api.QualityFault, AgeSec: 7300}
			s.Outputs.Heat, s.Outputs.Humid = false, false
			s.Outputs.Holds = []api.Hold{{Output: "heat", Reason: "temperature data is bad"}, {Output: "humid", Reason: "temperature data is bad"}}
			s.Active = []api.Alarm{
				alarm("SENSOR_CLIMATE_FAULT", api.SevCritical, "Temperature and humidity sensor not reading", "Check the DHT22 wiring. Heater and humidifier are off; cooling repeats its recent pattern with no feedback.", 2*time.Hour, 0, 0, ""),
				alarm("SENSOR_AIR_FAULT", api.SevWarning, "Airflow sensor not reading", "Check the FS3000 cable. The fan holds its last good speed.", 2*time.Hour, 0, 0, ""),
				alarm("SCALE_FAULT", api.SevWarning, "Scale not reading", "Check the HX711 wiring. Weight-based phase changes are paused; temperature and humidity control continue.", 2*time.Hour, 0, 0, ""),
			}
		})},
		{Name: "not-fitted", Make: with(func(s *api.Snapshot) {
			s.Air = api.Reading{Quality: api.QualityOff}
			s.Weight = api.Reading{Quality: api.QualityOff}
			s.Door = api.Reading{Quality: api.QualityOff}
		})},
		{Name: "drift-no-alarm", Make: with(func(s *api.Snapshot) { s.Temp = good(15.6) })},
		{Name: "hold-compressor", Make: with(func(s *api.Snapshot) {
			s.Temp = good(14.2)
			s.Outputs.Cool = false
			s.Outputs.Holds = []api.Hold{{Output: "cool", Reason: "compressor minimum off time", RemainingSec: 155}}
		})},
		{Name: "hold-two", Make: with(func(s *api.Snapshot) {
			s.Outputs.Cool = false
			s.Outputs.Humid = false
			s.Outputs.Holds = []api.Hold{{Output: "cool", Reason: "minimum off time", RemainingSec: 155}, {Output: "humid", Reason: "post-defrost", RemainingSec: 640}}
		})},
		{Name: "hold-with-alarm", Make: with(func(s *api.Snapshot) {
			s.Temp = good(16.4)
			s.Outputs.Cool = false
			s.Outputs.Holds = []api.Hold{{Output: "cool", Reason: "compressor minimum off time", RemainingSec: 95}}
			s.Active = []api.Alarm{alarmTempHigh}
		})},
		{Name: "defrost", Make: with(func(s *api.Snapshot) {
			s.Temp = good(16.1)
			s.Outputs.Cool, s.Outputs.Defrost = false, true
		})},
		{Name: "paused", Make: with(func(s *api.Snapshot) { s.Program.State = api.RunPaused })},
		{Name: "open-ended-phase", Make: with(func(s *api.Snapshot) {
			s.Program.PhaseIndex, s.Program.PhaseName, s.Program.PhaseRemaining = 3, "Finish", -1
			lossOf(s, 31.4)
		})},
		{Name: "end-of-cure", Make: with(func(s *api.Snapshot) {
			s.Program.State = api.RunFinished
			s.Program.PhaseIndex, s.Program.PhaseName, s.Program.PhaseRemaining = 3, "Finish", -1
			lossOf(s, 35.1)
			s.Program.TotalElapsed = 34 * 24 * time.Hour
			s.Program.Verify = VerifyText
		})},
		{Name: "end-of-cure-alarm", Make: with(func(s *api.Snapshot) {
			s.Program.State = api.RunFinished
			s.Program.PhaseIndex, s.Program.PhaseName, s.Program.PhaseRemaining = 3, "Finish", -1
			lossOf(s, 35.1)
			s.Program.TotalElapsed = 34 * 24 * time.Hour
			s.Program.Verify = VerifyText
			s.Active = []api.Alarm{alarm("CURE_FINISHED", api.SevWarning, "Recipe finished: operator must verify", "The controller does not determine whether product is safe. Verify by your own process plan.", 40*time.Minute, 0, 0, "")}
		})},
		{Name: "simulated", Make: with(func(s *api.Snapshot) { s.Health.Simulated, s.Health.SpeedFactor = true, 60 })},
		{Name: "simulated-alarm", Make: with(func(s *api.Snapshot) {
			s.Health.Simulated, s.Health.SpeedFactor = true, 60
			s.Temp = good(16.4)
			s.Active = []api.Alarm{alarmTempHigh}
		})},
		{Name: "clock-untrusted", Make: with(func(s *api.Snapshot) {
			s.Health.ClockTrusted = false
			for i := range s.History {
				s.History[i].ClockOK = false
			}
		})},
		{Name: "trend-gap", Make: with(func(s *api.Snapshot) { s.Trend = trend(T0, 7*24, 5*time.Minute, 9*time.Hour, 5*time.Hour) })},
		{Name: "trend-empty", Make: with(func(s *api.Snapshot) { s.Trend = nil })},
		{Name: "extreme-values", Make: with(func(s *api.Snapshot) {
			s.Temp = good(-2.5)
			s.RH = good(100)
			s.Outputs.Cool = false
			s.Active = []api.Alarm{alarm("TEMP_ABS_LOW", api.SevCritical, "Temperature near freezing", "Check whether the compressor relay is stuck on. Product may freeze.", 3*time.Minute, -2.5, 0.5, "°C")}
			s.Air = good(12.34)
			s.Program.StartWeightG = 150970
			s.Weight = good(123456)
			s.Outputs.FanDutyPct, s.Outputs.FanRPM = 100, 3000
		})},
		{Name: "bad-numbers", Make: with(func(s *api.Snapshot) {
			s.Temp = api.Reading{Value: math.NaN(), Quality: api.QualityOK}
			s.RH = api.Reading{Value: math.Inf(1), Quality: api.QualityOK}
		})},
		{Name: "long-text", Make: with(func(s *api.Snapshot) {
			s.Active = []api.Alarm{alarm("LONG", api.SevCritical, "Temp sensor value implausible",
				"Open the door only if you must. Check the sensor, its cable and connector, then compare with a separate calibrated thermometer before moving any product out of the cabinet.", 3*time.Hour, 0, 0, "")}
		})},
		{Name: "stalled", Make: Base, Frozen: true},
		{Name: "stalled-idle", Make: with(func(s *api.Snapshot) { s.Program = api.Program{State: api.RunIdle} }), Frozen: true},
		{Name: "stalled-alarm", Make: with(func(s *api.Snapshot) {
			s.Temp = good(16.4)
			s.Active = []api.Alarm{alarmTempHigh}
		}), Frozen: true},
		{Name: "stalled-end-of-cure", Make: with(func(s *api.Snapshot) {
			s.Program.State = api.RunFinished
			s.Program.Verify = VerifyText
			lossOf(s, 35.1)
		}), Frozen: true},
		{Name: "override", Make: with(func(s *api.Snapshot) {
			s.Setpoint = api.Setpoints{TempC: 14, RHPct: 76, AirMS: 0.4}
			s.Temp = good(14.1)
			s.Program.Override = &api.Setpoints{TempC: 14, RHPct: 76, AirMS: 0.4}
		})},
		{Name: "override-alarm", Make: with(func(s *api.Snapshot) {
			s.Setpoint = api.Setpoints{TempC: 14, RHPct: 76, AirMS: 0.4}
			s.Temp = good(17.2)
			s.Program.Override = &api.Setpoints{TempC: 14, RHPct: 76, AirMS: 0.4}
			s.Active = []api.Alarm{alarm("TEMP_HIGH", api.SevCritical, "Temperature far above setpoint", "Find the cause now. Inspect product and judge it by your own process plan.", 18*time.Minute, 17.2, 16, "°C")}
		})},
		{Name: "reading-in-doubt", Make: with(func(s *api.Snapshot) {
			s.Temp = api.Reading{Value: 14.8, Quality: api.QualitySuspect, AgeSec: 1, Note: "disagrees with cabinet"}
			s.Outputs.Heat, s.Outputs.Humid = false, false
			s.Outputs.Holds = []api.Hold{{Output: "heat", Reason: "temperature data is bad"}, {Output: "humid", Reason: "temperature data is bad"}}
			s.Outputs.Status = []api.OutputStatus{
				{Output: "cool", On: true, Reason: "running a blind pattern: temperature data is bad", Kind: "fault"},
				{Output: "heat", Reason: "locked off: temperature data is bad", Kind: "fault"},
				{Output: "humid", Reason: "locked off: temperature or humidity data is bad", Kind: "fault"},
			}
			s.Active = []api.Alarm{alarm("TEMP_READING_DISAGREES", api.SevCritical, "Temperature reading disagrees with how the cabinet is behaving", "Compare with a separate thermometer and enter the result. Product may be warmer or colder than shown.", 25*time.Minute, 0, 0, "")}
		})},
		{Name: "reference-disagrees", Make: with(func(s *api.Snapshot) {
			s.Temp = api.Reading{Value: 14.8, Quality: api.QualitySuspect, AgeSec: 1}
			s.Active = []api.Alarm{alarm("REFERENCE_DISAGREES", api.SevCritical, "Sensor differs from the reference instrument", "Replace or calibrate the sensor, then check again.", 2*time.Minute, 0, 0, "")}
		})},
		{Name: "ref-check-overdue", Make: with(func(s *api.Snapshot) {
			s.Health.RefCheckDueSec = -9 * 3600
			s.Active = []api.Alarm{alarm("CAL_CHECK_DUE", api.SevInfo, "Sensor check due", "Compare temperature and humidity with a separate reference and adjust the calibration.", 9*time.Hour, 0, 0, "")}
		})},
		{Name: "idle-with-reason", Make: with(func(s *api.Snapshot) {
			s.Temp = good(14.4)
			s.Outputs.Cool = false
			s.Outputs.Status = []api.OutputStatus{
				{Output: "cool", Reason: "building up demand for the next run; runs are spaced to protect the compressor", Kind: "normal"},
				{Output: "heat", Reason: "at or over setpoint", Kind: "normal"},
				{Output: "humid", Reason: "waiting to see the last pulse settle", Kind: "normal"},
			}
		})},
		{Name: "output-holds", Make: with(func(s *api.Snapshot) {
			s.Outputs.Cool = false
			s.Outputs.Holds = []api.Hold{{Output: "cool", Reason: "minimum off time", RemainingSec: 155}}
			s.Outputs.Status = []api.OutputStatus{
				{Output: "cool", Reason: "minimum off time", Kind: "hold"},
				{Output: "heat", Reason: "at or over setpoint", Kind: "normal"},
				{Output: "humid", Reason: "door open", Kind: "hold"},
			}
		})},
		{Name: "long-title", Make: with(func(s *api.Snapshot) {
			s.Active = []api.Alarm{alarm("LONG2", api.SevCritical, "Temperature reading disagrees with how the cabinet is behaving", "Compare with a separate thermometer. Heater and humidifier are off; cooling repeats its recent pattern with no feedback until this is resolved.", 3*time.Minute, 0, 0, "")}
		})},
	}
}

// Find returns a fixture by name.
func Find(name string) (Fixture, bool) {
	for _, f := range All() {
		if f.Name == name {
			return f, true
		}
	}
	return Fixture{}, false
}

// Controller is a fake api.Controller. It applies commands to its snapshot so a
// walkthrough shows real state changes, and records every command it receives.
type Controller struct {
	mu       sync.Mutex
	snap     api.Snapshot
	clock    func() time.Time
	Frozen   bool
	Commands []api.Command
	Refuse   string // when set, Do refuses with this message
}

// NewController wraps a fixture snapshot. clock supplies Snapshot.Now.
func NewController(s api.Snapshot, clock func() time.Time) *Controller {
	return &Controller{snap: s, clock: clock}
}

// SetSnapshot replaces the state, as a scripted scene change.
func (c *Controller) SetSnapshot(s api.Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snap = s
}

// Snapshot implements api.Controller.
func (c *Controller) Snapshot() api.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snap
	if !c.Frozen && c.clock != nil {
		s.Now = c.clock()
	}
	return s
}

// Do implements api.Controller.
func (c *Controller) Do(cmd api.Command) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Commands = append(c.Commands, cmd)
	if c.Refuse != "" {
		return fmt.Errorf("%s", c.Refuse)
	}
	s := &c.snap
	switch cmd.Kind {
	case api.CmdSetSetpoint:
		if s.Program.State == api.RunActive || s.Program.State == api.RunPaused || s.Program.State == api.RunFinished {
			ov := *cmd.Setpoint
			s.Program.Override = &ov
		}
		s.Setpoint = *cmd.Setpoint
	case api.CmdRefCheck:
		// As the core: within tolerance clears a reading in doubt, outside raises.
		bad := false
		if r := cmd.Reference; r != nil {
			if r.TempC != nil && math.Abs(s.Temp.Value-*r.TempC) > 1.5 {
				bad = true
			}
			if r.RHPct != nil && math.Abs(s.RH.Value-*r.RHPct) > 5 {
				bad = true
			}
		}
		kept := s.Active[:0:0]
		for _, a := range s.Active {
			if a.Code != "TEMP_READING_DISAGREES" && a.Code != "RH_READING_DISAGREES" && a.Code != "REFERENCE_DISAGREES" && a.Code != "CAL_CHECK_DUE" {
				kept = append(kept, a)
			}
		}
		if bad {
			kept = append(kept, alarm("REFERENCE_DISAGREES", api.SevCritical, "Sensor differs from the reference instrument", "Replace or calibrate the sensor, then check again.", 0, 0, 0, ""))
		} else {
			s.Temp.Quality, s.Health.RefCheckDueSec = api.QualityOK, 7*86400
		}
		s.Active = kept
	case api.CmdSilence:
		s.BuzzerSilenced = true
	case api.CmdAck:
		for i := range s.Active {
			if cmd.AlarmID == "" || s.Active[i].ID == cmd.AlarmID {
				s.Active[i].Acked = true
			}
		}
	case api.CmdPause:
		s.Program.State = api.RunPaused
	case api.CmdResume:
		s.Program.State = api.RunActive
	case api.CmdStop:
		s.Program = api.Program{State: api.RunIdle}
	case api.CmdNextPhase:
		s.Program.Override = nil
		s.Program.PhaseIndex++
		if s.Program.PhaseIndex >= s.Program.PhaseCount {
			s.Program.State = api.RunFinished
			s.Program.PhaseIndex = s.Program.PhaseCount - 1
			s.Program.Verify = VerifyText
		}
		for _, r := range s.Recipes {
			if r.ID == s.Program.RecipeID {
				ph := r.Phases[s.Program.PhaseIndex]
				s.Program.PhaseName = ph.Name
				s.Setpoint = ph.Setpoint
				s.Program.PhaseRemaining = ph.Duration
				if ph.Duration == 0 {
					s.Program.PhaseRemaining = -1
				}
			}
		}
	case api.CmdStart:
		for _, r := range s.Recipes {
			if r.ID == cmd.RecipeID {
				ph := r.Phases[0]
				s.Program = api.Program{State: api.RunActive, RecipeID: r.ID, RecipeName: r.Name, PhaseCount: len(r.Phases),
					PhaseName: ph.Name, PhaseRemaining: ph.Duration, StartWeightG: cmd.StartWeightG, TargetLossPct: r.Phases[len(r.Phases)-1].TargetLossPct}
				s.Setpoint = ph.Setpoint
			}
		}
	case api.CmdSetLimits:
		s.Limits = *cmd.Limits
	case api.CmdSetCal:
		s.Calibration = *cmd.Calibration
	}
	return nil
}
