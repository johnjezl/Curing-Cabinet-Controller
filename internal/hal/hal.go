// Package hal is the contract between control logic and the physical cabinet.
// Real drivers (internal/drivers) and the simulated plant (internal/sim) both
// implement it. Control logic never touches hardware any other way.
//
// Rules for every implementation:
//   - Never block longer than the documented bound; a dead device returns an
//     error, it does not hang the control loop.
//   - Errors wrap ErrAbsent when the device does not answer at all, so a missing
//     sensor can be told apart from a bad reading.
package hal

import "errors"

// ErrAbsent means the device did not respond at all (unplugged, no such bus).
var ErrAbsent = errors.New("device absent")

// ErrBadRead means the device responded with data that failed validation
// (checksum, framing, out of physical range).
var ErrBadRead = errors.New("bad read")

// ErrOutputsDropped comes from Watchdog.Pet when the hardware layer turned every
// output off on its own because commands stopped arriving. The outputs stay off
// until they are commanded again, so a caller that tracks applied state or
// equipment off-timers must treat them as off from this point.
var ErrOutputsDropped = errors.New("outputs dropped by lease expiry")

// Climate is the DHT22/AM2302. Read returns the latest validated measurement.
// Implementations enforce the sensor's minimum 2 s sample interval internally
// and may return the previous sample with its original age. Bound: 500 ms.
type Climate interface {
	Read() (tempC, rhPct float64, ageSec float64, err error)
}

// AirVelocity is the FS3000-1005 in the manifold (0 to 7.23 m/s). Bound: 100 ms.
type AirVelocity interface {
	Read() (metersPerSec float64, err error)
}

// Scale is the HX711 and load cell. RawCounts is the signed 24-bit reading;
// calibration lives in core so that it persists with the rest of the settings.
// Bound: 200 ms; an unready chip returns ErrAbsent after that.
type Scale interface {
	ReadRaw() (counts int32, err error)
}

// Fan is the ARCTIC P12 Pro on 25 kHz PWM with tach feedback. SetDuty is 0..100.
// The P12 Pro's own minimum-speed behaviour is handled in core, not here.
// RPM returns 0 with nil error for a stopped fan, an error if tach is unusable.
type Fan interface {
	SetDuty(pct float64) error
	RPM() (float64, error)
}

// OutputID names a mains-side relay output.
type OutputID string

const (
	OutCool    OutputID = "cool"    // compressor
	OutHeat    OutputID = "heat"    // heater (also reheat during dehumidify)
	OutHumid   OutputID = "humid"   // ultrasonic humidifier
	OutDehumid OutputID = "dehumid" // dedicated dehumidifier, if fitted
	OutAlarm   OutputID = "alarm"   // dry contact, energize-to-run: on = healthy with nothing to report, off = alarm (also when control or power dies)
	OutBuzzer  OutputID = "buzzer"  // local sounder
)

// Outputs drives relays. Set must be idempotent. AllOff is the fail-safe path
// and must attempt every output even if one fails.
type Outputs interface {
	Set(id OutputID, on bool) error
	AllOff() error
}

// Door is an optional door switch. Implementations for an unfitted switch
// return ErrAbsent. Open is true when the door is open.
type Door interface {
	Open() (bool, error)
}

// Watchdog is pet by the control loop on every healthy iteration. A nil
// Watchdog is allowed in tests and in simulation.
type Watchdog interface {
	Pet() error
}

// Cabinet bundles the hardware. Close returns outputs to the fail-safe state.
type Cabinet struct {
	Climate Climate
	Air     AirVelocity
	Scale   Scale
	Fan     Fan
	Outputs Outputs
	Door    Door
	Close   func() error
}
