package core

import (
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// Clock is injected so that a cure of many weeks can run in virtual time.
type Clock interface {
	// Now is the wall clock. It can be wrong: a Pi with no battery boots with a
	// stale or epoch time. Nothing in control depends on it.
	Now() time.Time
	// Mono is a monotonic time since this process started.
	Mono() time.Duration
	// Trusted says whether the platform believes the wall clock (RTC valid or
	// NTP synchronised).
	Trusted() bool
}

// Config holds installer-level settings. Defaults are in DefaultConfig and the
// reasoning for each is in docs/adr/0102 and 0103.
type Config struct {
	// Compressor protection, seconds.
	CompMinOff       float64
	CompMinOn        float64
	CompStartSpacing float64
	PowerOnDelay     float64

	// Defrost.
	DefrostInterval     float64 // s between defrosts
	DefrostDuration     float64 // s, hard cap, no evaporator probe exists
	DefrostMinRuntime   float64 // s compressor run needed since the last defrost
	DefrostDrip         float64 // s after defrost with compressor held off
	DefrostFanDuty      float64 // % fan during defrost
	DefrostHeatBelowC   float64 // setpoints under this use heater-assist
	DefrostAbortDeltaC  float64 // ends defrost early when air is this far over set
	DefrostAbortCold    float64 // the same at cold setpoints, where heater assist is used
	DefrostDurationCold float64
	DefrostIntervalCold float64
	DefrostIntervalWarm float64

	// Suppression windows, seconds.
	SuppressDoor    float64
	SuppressDefrost float64
	SuppressPowerOn float64
	SuppressChange  float64

	// Clearing differentials.
	TempClearDiff float64
	RHClearDiff   float64

	// Silence timeout: a silenced buzzer comes back for unacknowledged alarms.
	SilenceSec float64

	// Log and checkpoint periods, seconds.
	LogInterval   float64
	CheckpointSec float64

	// Dedicated dehumidifier fitted. If false, dehumidify is compressor + reheat.
	DehumidFitted bool

	// DoorHold is how long cooling stays stopped with the door open.
	DoorHold float64

	// CalReminderSec raises a reminder to check sensors against a reference while a
	// cure runs; CalReminderIdleSec applies with no cure running.
	CalReminderSec     float64
	CalReminderIdleSec float64

	// Alarm output timing, seconds. See docs/adr/0108.
	NotifySec   float64 // a warning or a past event signals this long
	RenotifySec float64 // a warning still present signals again this often
	EdgeSec     float64 // the contact closes this long to mark a new alarm
	AckRearmSec float64 // an acknowledged critical still present sounds again
	// CrossCheckOff disables the plant-response checks; a test uses it to show they
	// have no effect on regulation.
	CrossCheckOff bool

	// FallbackScale multiplies the learned net duty when temperature is unknown.
	FallbackScale float64
	// FallbackColdCut is taken off the scale at setpoints near freezing.
	FallbackColdCut float64
	// FanDelay is how long the fan stays stopped after a defrost.
	FanDelay float64
}

// DefaultConfig returns defaults. See docs/adr/0103 for the reasoning.
func DefaultConfig() Config {
	return Config{
		CompMinOff:          240,
		CompMinOn:           120,
		CompStartSpacing:    480,
		PowerOnDelay:        240,
		DefrostInterval:     8 * 3600,
		DefrostDuration:     20 * 60,
		DefrostMinRuntime:   30 * 60,
		DefrostDrip:         120,
		DefrostFanDuty:      25,
		DefrostHeatBelowC:   6,
		DefrostAbortDeltaC:  2.5,
		DefrostAbortCold:    5,
		DefrostDurationCold: 30 * 60,
		DefrostIntervalCold: 6 * 3600,
		DefrostIntervalWarm: 12 * 3600,
		SuppressDoor:        20 * 60,
		SuppressDefrost:     20 * 60,
		SuppressPowerOn:     90 * 60,
		SuppressChange:      4 * 3600,
		TempClearDiff:       1.0,
		RHClearDiff:         2.0,
		SilenceSec:          30 * 60,
		LogInterval:         60,
		CheckpointSec:       600,
		DoorHold:            300,
		CalReminderSec:      7 * 24 * 3600,
		CalReminderIdleSec:  30 * 24 * 3600,
		NotifySec:           30 * 60,
		RenotifySec:         6 * 3600,
		EdgeSec:             15,
		AckRearmSec:         3600,
		FallbackScale:       0.8,
		FallbackColdCut:     0.2,
		FanDelay:            180,
	}
}

// Stable alarm codes.
const (
	CodeTempHigh     = "TEMP_HIGH"
	CodeTempLow      = "TEMP_LOW"
	CodeTempAbsHigh  = "TEMP_ABS_HIGH"
	CodeTempAbsLow   = "TEMP_ABS_LOW"
	CodeRHHigh       = "RH_HIGH"
	CodeRHLow        = "RH_LOW"
	CodeAirLow       = "AIR_LOW"
	CodeAirHigh      = "AIR_HIGH"
	CodeDoorOpen     = "DOOR_OPEN"
	CodeOutage       = "POWER_OUTAGE"
	CodeRestarted    = "CONTROLLER_RESTARTED"
	CodeClimateFault = "SENSOR_CLIMATE_FAULT"
	CodeTempSuspect  = "TEMP_SUSPECT"
	CodeRHSuspect    = "RH_SUSPECT"
	CodeTempErratic  = "TEMP_ERRATIC"
	CodeRHErratic    = "RH_ERRATIC"
	CodeTempDrift    = "TEMP_DRIFT_SUSPECT"
	CodeRHDrift      = "RH_DRIFT_SUSPECT"
	CodeAirFault     = "SENSOR_AIR_FAULT"
	CodeAirSuspect   = "AIR_SUSPECT"
	CodeScaleFault   = "SENSOR_SCALE_FAULT"
	CodeScaleSuspect = "SCALE_SUSPECT"
	CodeWeightStep   = "WEIGHT_STEP"
	CodeFanStall     = "FAN_STALL"
	CodeFanTach      = "FAN_TACH"
	CodeAirBlocked   = "AIRFLOW_BLOCKED"
	CodeCoolFail     = "COOL_NO_EFFECT"
	CodeCoolStuck    = "COOL_STUCK_ON"
	CodeHeatFail     = "HEAT_NO_EFFECT"
	CodeHeatStuck    = "HEAT_STUCK_ON"
	CodeHumidFail    = "HUMID_NO_EFFECT"
	CodeHumidStuck   = "RH_NOT_FALLING"
	CodeHumidOnStuck = "HUMID_STUCK_ON"
	CodeOutputFault  = "OUTPUT_FAULT"
	CodeStorage      = "STORAGE_FAULT"
	CodeStateLost    = "STATE_RESET"
	CodeClock        = "CLOCK_UNTRUSTED"
	CodeCalReminder  = "CAL_CHECK_DUE"
	CodeCureFinished = "CURE_FINISHED"
	CodeLogDamage    = "LOG_DAMAGE"
	CodeDoorSwitch   = "DOOR_SWITCH_FAULT"
	CodeTempHighCrit = "TEMP_HIGH_CRITICAL"
	CodeTempDisagree = "TEMP_READING_DISAGREES"
	CodeRHDisagree   = "RH_READING_DISAGREES"
	CodeDryingShift  = "DRYING_RATE_SHIFT"
	CodeRefDisagree  = "REFERENCE_DISAGREES"
	CodeOutDropped   = "OUTPUTS_DROPPED"
	CodeDefrostShort = "DEFROST_CUT_SHORT"
)

// ProgramState is the persisted form of a running cure. Times are seconds of
// powered, unpaused running, so they cannot depend on the wall clock.
type ProgramState struct {
	State        api.RunState   `json:"state"`
	Recipe       *api.Recipe    `json:"recipe,omitempty"`
	PhaseIndex   int            `json:"phase_index"`
	PhaseElapsed float64        `json:"phase_elapsed_s"`
	TotalElapsed float64        `json:"total_elapsed_s"`
	StartWeightG float64        `json:"start_weight_g"`
	DegreeHours  float64        `json:"degree_hours_156"`
	DHIncomplete bool           `json:"dh_incomplete"`
	DHOutageEst  float64        `json:"dh_outage_est,omitempty"`
	Outages      int            `json:"outages"`
	OutageKnownS float64        `json:"outage_known_s"`
	OutageUnk    int            `json:"outage_unknown"`
	ManualSet    api.Setpoints  `json:"manual_set"`
	Override     *api.Setpoints `json:"override,omitempty"`
}

// State is everything that must survive a restart other than the journals.
type State struct {
	Version   int    `json:"version"`
	Clean     bool   `json:"clean"` // set only by an orderly Close
	BootCount uint64 `json:"boot_count"`
	// OSBootID is the operating system's boot ID at the last start. The same ID at
	// the next start means only the process stopped, not the power (ADR 0409).
	OSBootID      string          `json:"os_boot_id,omitempty"`
	Manual        api.Setpoints   `json:"manual"`
	Limits        api.Limits      `json:"limits"`
	Cal           api.Calibration `json:"cal"`
	Program       ProgramState    `json:"program"`
	UserRecipes   []api.Recipe    `json:"user_recipes,omitempty"`
	LastWall      time.Time       `json:"last_wall,omitzero"`
	WallHighWater time.Time       `json:"wall_high_water,omitzero"`
	LastWallOK    bool            `json:"last_wall_ok"`
	CompStarts    uint64          `json:"comp_starts"`
	CompRunSec    float64         `json:"comp_run_s"`
	HeatCycles    uint64          `json:"heat_cycles"`
	HumidCycles   uint64          `json:"humid_cycles"`
	PoweredSec    float64         `json:"powered_s"`
	SinceDefrost  float64         `json:"since_defrost_s"`
	RunSinceDef   float64         `json:"run_since_defrost_s"`
	CoolDuty      float64         `json:"cool_duty"`
	CoolDutyAge   float64         `json:"cool_duty_age_s"`
	CoolDutySet   float64         `json:"cool_duty_set_c"`
	FanDuty       float64         `json:"fan_duty"`
	HumidGain     float64         `json:"humid_gain"`
	LastCal       float64         `json:"last_cal_powered_s"`
	LastRefWall   time.Time       `json:"last_ref_wall,omitzero"`
	BadLinesSeen  uint64          `json:"bad_lines_seen"`
	Cross         []CrossPt       `json:"cross,omitempty"`
	CrossBase     *CrossPt        `json:"cross_base,omitempty"`
	TDoubtUntil   float64         `json:"t_doubt_until,omitempty"`
	RHDoubtUntil  float64         `json:"rh_doubt_until,omitempty"`
	TDoubtLow     bool            `json:"t_doubt_low,omitempty"`
}

func (s State) clone() State {
	if s.Program.Recipe != nil {
		r := *s.Program.Recipe
		r.Phases = append([]api.PhaseSpec(nil), r.Phases...)
		s.Program.Recipe = &r
	}
	s.UserRecipes = append([]api.Recipe(nil), s.UserRecipes...)
	s.Cross = append([]CrossPt(nil), s.Cross...)
	return s
}

// LogRecord is one process-log line. Values are interval statistics over
// LogInterval, so a short excursion between lines is still visible. Short keys
// because it is written every minute for weeks.
type LogRecord struct {
	Seq     uint64    `json:"s"`
	Boot    uint64    `json:"b"`
	Mono    float64   `json:"m"`  // seconds since this boot
	Powered float64   `json:"pw"` // seconds powered across all boots
	Wall    time.Time `json:"w"`
	ClkOK   bool      `json:"ck"` // wall clock trusted when written
	T       float64   `json:"t"`  // mean °C of valid samples this interval
	TMin    float64   `json:"tn"`
	TMax    float64   `json:"tx"`
	TQ      string    `json:"tq"`
	RH      float64   `json:"h"`
	RHMin   float64   `json:"hn"`
	RHMax   float64   `json:"hx"`
	RHQ     string    `json:"hq"`
	Air     float64   `json:"a"`
	AirQ    string    `json:"aq"`
	Wt      float64   `json:"g"`
	WtQ     string    `json:"gq"`
	SetT    float64   `json:"st"`
	SetRH   float64   `json:"sh"`
	SetAir  float64   `json:"sa"`
	Cool    float64   `json:"oc"` // fraction of the interval on
	Heat    float64   `json:"oh"`
	Humid   float64   `json:"ou"`
	Dehum   float64   `json:"od"`
	Def     float64   `json:"of"`
	Fan     float64   `json:"fd"`
	RPM     float64   `json:"fr"`
	Door    float64   `json:"dr"` // fraction of the interval open
	AlarmN  int       `json:"an"` // active warning or critical count
	Run     string    `json:"rs,omitempty"`
	Recipe  string    `json:"ri,omitempty"`
	Phase   int       `json:"pi"`
	PhEl    float64   `json:"pe"`
	TotEl   float64   `json:"te"`
	Start   float64   `json:"sw,omitempty"`
	DH      float64   `json:"dh,omitempty"`
}

// Event is a journal entry: alarms, operator commands, restarts, phase changes.
type Event struct {
	Seq    uint64    `json:"s"`
	Boot   uint64    `json:"b"`
	Mono   float64   `json:"m"`
	Wall   time.Time `json:"w"`
	ClkOK  bool      `json:"ck"`
	Type   string    `json:"e"`
	Code   string    `json:"c,omitempty"`
	ID     string    `json:"id,omitempty"`
	Sev    string    `json:"sv,omitempty"`
	Title  string    `json:"ti,omitempty"`
	Action string    `json:"ac,omitempty"`
	Peak   float64   `json:"p,omitempty"`
	Limit  float64   `json:"l,omitempty"`
	Unit   string    `json:"u,omitempty"`
	Msg    string    `json:"x,omitempty"`
	Dur    float64   `json:"d,omitempty"`
	// Since, when set, is when the alarm's subject began, earlier than the event:
	// an outage is raised on return but began at the loss.
	Since time.Time `json:"sn,omitzero"`
}

// Event types.
const (
	EvBoot      = "boot"
	EvShutdown  = "shutdown"
	EvOutage    = "outage"
	EvOutageEnd = "outage_end"
	EvAlarm     = "alarm"
	EvClear     = "clear"
	EvAck       = "ack"
	EvSilence   = "silence"
	EvSystem    = "system" // an alarm of the host process, outside core, kept on the same record
	EvCmd       = "command"
	EvPhase     = "phase"
	EvDefrost   = "defrost"
	EvClockOK   = "clock_trusted"
	EvClockJump = "clock_jump"
	EvStorage   = "storage"
	EvRefCheck  = "reference_check"
	EvDropped   = "outputs_dropped"
)
