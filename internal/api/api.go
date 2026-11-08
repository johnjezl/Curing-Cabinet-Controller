// Package api is the contract between the control core and every front end
// (touchscreen, local socket, local HTTP). Front ends read Snapshots and send
// Commands. They hold no control state of their own.
package api

import (
	"encoding/json"
	"time"
)

// Controller is what the core exposes. Snapshot must be cheap and non-blocking;
// Do validates the command and returns an operator-readable error if refused.
type Controller interface {
	Snapshot() Snapshot
	Do(Command) error
}

// Quality of a sensor reading.
type Quality string

const (
	QualityOK      Quality = "ok"
	QualityStale   Quality = "stale"   // sensor has not updated recently; value is last good
	QualityFault   Quality = "fault"   // absent or repeatedly invalid
	QualitySuspect Quality = "suspect" // plausible-looking but failed a cross-check or stuck test
	QualityOff     Quality = "off"     // not fitted or disabled
)

// Reading is one sensor value with its validity. Display it honestly: a value
// whose Quality is not ok must never look like a live good reading.
type Reading struct {
	Value   float64 `json:"value"`
	Quality Quality `json:"quality"`
	AgeSec  float64 `json:"age_sec"`
	Note    string  `json:"note,omitempty"`
	// Avg is the few-minute average the control loop acts on, 0 if not given. A
	// panel uses it for a "no alarm yet" headline, so a compressor run's passing dip
	// does not raise one, and shows Value.
	Avg float64 `json:"avg,omitempty"`
	// Doubt marks a reading suspected from how the cabinet behaves (ADR 0109). Control
	// still uses it; only heat and humidity are held off (ADR 0113).
	Doubt bool `json:"doubt,omitempty"`
}

type Setpoints struct {
	TempC float64 `json:"temp_c"`
	RHPct float64 `json:"rh_pct"`
	AirMS float64 `json:"air_ms"`
}

// Hold is a protection timer currently stopping an output.
type Hold struct {
	Output       string  `json:"output"`
	Reason       string  `json:"reason"` // e.g. "minimum off time"
	RemainingSec float64 `json:"remaining_sec"`
}

type Outputs struct {
	Cool       bool    `json:"cool"`
	Heat       bool    `json:"heat"`
	Humid      bool    `json:"humid"`
	Dehumid    bool    `json:"dehumid"`
	Defrost    bool    `json:"defrost"`
	FanDutyPct float64 `json:"fan_duty_pct"`
	FanRPM     float64 `json:"fan_rpm"`
	Holds      []Hold  `json:"holds,omitempty"`
	// Status says, for each regulated output, why it is in the state it is in, so an
	// operator can tell a normal wait from a hold or a fault.
	Status []OutputStatus `json:"status,omitempty"`
}

// OutputStatus explains one output.
type OutputStatus struct {
	Output string `json:"output"`
	On     bool   `json:"on"`
	Reason string `json:"reason"`
	// Kind is "normal", "hold" (a timer or interlock is stopping it), "fault" (bad
	// data or a failed part is stopping it), "unknown" (outputs cannot be written, so
	// the real state is not known) or "stuck" (the cabinet's response shows the relay
	// is closed although it is commanded open; On is then true).
	Kind string `json:"kind"`
}

type Severity string

const (
	SevInfo     Severity = "info"
	SevWarning  Severity = "warning"
	SevCritical Severity = "critical"
)

// Alarm is an active or historical alarm.
type Alarm struct {
	ID       string    `json:"id"`   // unique per occurrence
	Code     string    `json:"code"` // stable, e.g. "TEMP_HIGH"
	Severity Severity  `json:"severity"`
	Title    string    `json:"title"`  // short, for a list row
	Action   string    `json:"action"` // what the operator should do
	Since    time.Time `json:"since"`
	Cleared  time.Time `json:"cleared,omitzero"`
	Acked    bool      `json:"acked"`
	Peak     float64   `json:"peak,omitempty"`
	Limit    float64   `json:"limit,omitempty"`
	Unit     string    `json:"unit,omitempty"`
	// ClockOK is false when Since and Cleared came from a clock that could not be
	// trusted. DurationSec is measured on the monotonic clock; -1 means the alarm
	// spanned a restart. Set when cleared.
	ClockOK     bool    `json:"clock_ok"`
	DurationSec float64 `json:"duration_sec,omitempty"`
	// Event is true for something that happened (an outage, a restart, a finished
	// recipe), as opposed to a condition that is still there. A panel ranks live
	// conditions above events.
	Event bool `json:"event,omitempty"`
}

// PhaseSpec is one stage of a cure recipe.
type PhaseSpec struct {
	Name     string        `json:"name"`
	Setpoint Setpoints     `json:"setpoint"`
	Duration time.Duration `json:"duration"` // 0 means until the weight target is met or the operator advances
	// TargetLossPct ends the phase when reached; 0 means not used. It is a yield
	// target, never a statement that product is safe.
	TargetLossPct float64 `json:"target_loss_pct"`
}

type Recipe struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Notes   string      `json:"notes,omitempty"`
	Phases  []PhaseSpec `json:"phases"`
	BuiltIn bool        `json:"built_in"`
}

type RunState string

const (
	RunIdle     RunState = "idle"     // holds manual setpoints
	RunActive   RunState = "active"   // following a recipe
	RunPaused   RunState = "paused"   // recipe clock stopped, holding current phase setpoints
	RunFinished RunState = "finished" // target reached; holding; operator must verify and end
)

type Program struct {
	State          RunState      `json:"state"`
	RecipeID       string        `json:"recipe_id,omitempty"`
	RecipeName     string        `json:"recipe_name,omitempty"`
	PhaseIndex     int           `json:"phase_index"`
	PhaseCount     int           `json:"phase_count"`
	PhaseName      string        `json:"phase_name,omitempty"`
	PhaseElapsed   time.Duration `json:"phase_elapsed"`
	PhaseRemaining time.Duration `json:"phase_remaining"` // <0 if open-ended
	TotalElapsed   time.Duration `json:"total_elapsed"`
	StartWeightG   float64       `json:"start_weight_g"`
	WeightLossPct  float64       `json:"weight_loss_pct"`
	TargetLossPct  float64       `json:"target_loss_pct"`
	LossPctPerDay  float64       `json:"loss_pct_per_day"`
	// Verify is what the operator must confirm at the end of a cure; never empty
	// when State is finished. The system never says product is safe.
	Verify string `json:"verify,omitempty"`
	// Override is an operator setpoint replacing the recipe's for this phase only;
	// nil if the recipe's own setpoint is in force. It ends when the phase ends.
	Override *Setpoints `json:"override,omitempty"`
	// Outage totals for this cure. The cure clock counts powered time only.
	OutageCount   int           `json:"outage_count,omitempty"`
	OutageKnown   time.Duration `json:"outage_known,omitempty"`
	OutageUnknown int           `json:"outage_unknown,omitempty"` // outages of unknown length
	// DegreeHoursAbove156 is a process record against the 15.6 °C reference; it
	// says nothing about pH or about whether a limit was met. Incomplete means
	// data was missing, so the true total could be higher.
	DegreeHoursAbove156   float64 `json:"degree_hours_above_15_6,omitempty"`
	DegreeHoursIncomplete bool    `json:"degree_hours_incomplete,omitempty"`
	// DegreeHoursOutageEst estimates the same for outages of known length, from the
	// temperature before and after; there is no measurement of that time.
	DegreeHoursOutageEst float64 `json:"degree_hours_outage_est,omitempty"`
}

// Sample is one point of trend history.
type Sample struct {
	T       time.Time `json:"t"`
	TempC   float64   `json:"temp_c"` // NaN not allowed; use Valid
	RHPct   float64   `json:"rh_pct"`
	AirMS   float64   `json:"air_ms"`
	WeightG float64   `json:"weight_g"`
	Valid   bool      `json:"valid"`
}

// Limits are operator-visible alarm settings.
type Limits struct {
	TempHighDelta float64       `json:"temp_high_delta"`
	TempLowDelta  float64       `json:"temp_low_delta"`
	RHHighDelta   float64       `json:"rh_high_delta"`
	RHLowDelta    float64       `json:"rh_low_delta"`
	AlarmDelay    time.Duration `json:"alarm_delay"`
	DoorDelay     time.Duration `json:"door_delay"`
	// AbsTempHighC is a food-safety limit independent of the setpoint.
	AbsTempHighC float64 `json:"abs_temp_high_c"`
	// AbsTempLowC is a freeze limit independent of the setpoint.
	AbsTempLowC float64 `json:"abs_temp_low_c"`
}

type Calibration struct {
	TempOffsetC   float64 `json:"temp_offset_c"`
	RHOffsetPct   float64 `json:"rh_offset_pct"`
	ScaleZero     int32   `json:"scale_zero"`
	ScaleCountsPG float64 `json:"scale_counts_per_g"`
}

// Outage describes the most recent power loss seen across a restart.
type Outage struct {
	Detected bool      `json:"detected"`
	At       time.Time `json:"at,omitzero"`
	// Duration is written to JSON as duration_s, in seconds.
	Duration    time.Duration `json:"-"`
	DurationOK  bool          `json:"duration_ok"` // false if the clock could not be trusted
	TempOnBackC float64       `json:"temp_on_back_c"`
	// PeakTempC is the highest reading from return of power until the cabinet was
	// back in band or the recovery window ended. RecoverSec is how long that took,
	// -1 if not yet known.
	PeakTempC  float64 `json:"peak_temp_c"`
	RecoverSec float64 `json:"recover_sec"`
	TempKnown  bool    `json:"temp_known"`
	// StartUncertaintySec: the outage began between At and At plus this, so
	// Duration is an upper bound by up to this much.
	StartUncertaintySec float64 `json:"start_uncertainty_s"`
	// Provisional is true while At and Duration rest on a clock not yet trusted.
	Provisional bool `json:"provisional"`
	// Cause is "power" (a new boot) or "restart" (only the process stopped).
	Cause string `json:"cause,omitempty"`
}

// MarshalJSON writes unknown temperatures as null rather than 0.
func (o Outage) MarshalJSON() ([]byte, error) {
	type plain Outage
	v := struct {
		plain
		DurationSec float64  `json:"duration_s"`
		TempOnBackC *float64 `json:"temp_on_back_c"`
		PeakTempC   *float64 `json:"peak_temp_c"`
	}{plain: plain(o), DurationSec: o.Duration.Seconds()}
	if o.TempKnown {
		v.TempOnBackC, v.PeakTempC = &o.TempOnBackC, &o.PeakTempC
	}
	return json.Marshal(v)
}

// Health is the state of the controller itself.
type Health struct {
	Simulated    bool      `json:"simulated"` // must be unmistakable on the display
	SpeedFactor  float64   `json:"speed_factor"`
	ClockTrusted bool      `json:"clock_trusted"`
	LogOK        bool      `json:"log_ok"`
	StorageFreeM float64   `json:"storage_free_mb"`
	WatchdogOK   bool      `json:"watchdog_ok"`
	StartedAt    time.Time `json:"started_at"`
	LastOutage   Outage    `json:"last_outage"`
	Version      string    `json:"version"`
	// RefCheckDueSec is powered seconds until the next sensor reference check is due;
	// negative when overdue. LastRefCheck is when one last passed, if the clock was good.
	RefCheckDueSec float64   `json:"ref_check_due_sec"`
	LastRefCheck   time.Time `json:"last_ref_check,omitzero"`
}

// Snapshot is the whole visible state at one instant.
type Snapshot struct {
	Now            time.Time   `json:"now"`
	Temp           Reading     `json:"temp"`
	RH             Reading     `json:"rh"`
	Air            Reading     `json:"air"`
	Weight         Reading     `json:"weight"`
	Door           Reading     `json:"door"` // Value 1 = open; Quality off if no switch
	Setpoint       Setpoints   `json:"setpoint"`
	Outputs        Outputs     `json:"outputs"`
	Program        Program     `json:"program"`
	Active         []Alarm     `json:"active"`
	History        []Alarm     `json:"history"` // newest first
	BuzzerSilenced bool        `json:"buzzer_silenced"`
	Trend          []Sample    `json:"trend"` // oldest first; downsampled by core
	Recipes        []Recipe    `json:"recipes"`
	Limits         Limits      `json:"limits"`
	Calibration    Calibration `json:"calibration"`
	Health         Health      `json:"health"`
}

// CommandKind names an operator action.
type CommandKind string

const (
	CmdSetSetpoint  CommandKind = "set_setpoint" // during a recipe: an override for the current phase; UI must confirm
	CmdStart        CommandKind = "start_recipe" // RecipeID, optional StartWeightG
	CmdPause        CommandKind = "pause"
	CmdResume       CommandKind = "resume"
	CmdStop         CommandKind = "stop"       // destructive: UI must confirm
	CmdNextPhase    CommandKind = "next_phase" // UI must confirm
	CmdAck          CommandKind = "ack"        // AlarmID, or empty for all
	CmdSilence      CommandKind = "silence_buzzer"
	CmdSetLimits    CommandKind = "set_limits"
	CmdSetCal       CommandKind = "set_calibration"
	CmdTare         CommandKind = "tare"
	CmdCalWeight    CommandKind = "cal_weight" // KnownWeightG on the scale
	CmdDefrost      CommandKind = "defrost_now"
	CmdSaveRecipe   CommandKind = "save_recipe"
	CmdDeleteRecipe CommandKind = "delete_recipe"
	// CmdRefCheck records a reading from a separate reference instrument and
	// compares it with the sensor. It is what clears a sensor-disagreement alarm.
	CmdRefCheck CommandKind = "reference_check"
)

type Command struct {
	Kind         CommandKind  `json:"kind"`
	Setpoint     *Setpoints   `json:"setpoint,omitempty"`
	RecipeID     string       `json:"recipe_id,omitempty"`
	StartWeightG float64      `json:"start_weight_g,omitempty"`
	AlarmID      string       `json:"alarm_id,omitempty"`
	Limits       *Limits      `json:"limits,omitempty"`
	Calibration  *Calibration `json:"calibration,omitempty"`
	KnownWeightG float64      `json:"known_weight_g,omitempty"`
	Recipe       *Recipe      `json:"recipe,omitempty"`
	Reference    *Reference   `json:"reference,omitempty"`
}

// Reference is what a separate instrument read. Either value may be absent.
type Reference struct {
	TempC *float64 `json:"temp_c,omitempty"`
	RHPct *float64 `json:"rh_pct,omitempty"`
}
