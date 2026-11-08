# ADR 0106: Sensor fault detection and alarms

Status: accepted

## Sensor conditioning
Options: trust the driver's checksum (the reference, per R4.10, describes nothing more);
a plain low-pass; or per-channel validation.

Decision: per-channel validation: range check, median of 5, jump gate that believes a jump
only when 3 samples agree, stale at 10 s and fault at 20 s for DHT22, recovery after 5 good
samples. Stuck: identical raw value through 6 min (T) or 10 min (RH) of actuation that
should move it, or 2 h regardless. Erratic and heavy rejection rates are separate: a median
of 5 rides through 20 % spikes with a warning; half the samples rejected counts as bad data.

## Cross-checks
Fan: tach against FS3000, both directions. Compressor: 20 min run with no fall while above
set. Relay stuck on: fast fall with compressor off. A closed relay fought by the heater: the heater suddenly busy with the compressor
commanded off. Heater stuck on: cooling demand jumps with heater commanded off. Humidifier:
long pulses in quiet periods with no rise beyond what RH was already doing. A rule based on
RH staying low was tried and dropped: it fired on healthy cabinets with a weak humidifier. Scale: last 6 h trend rising over 0.5 %/day or falling over 8 %/day.

Slow drift: there is one chip and no second reference, so only an effort shift can show
it. A warning fires when net cooling or dehumidifying effort moves from its long baseline
with the reading unchanged. It must hold 12 h and stays up a day, because a daily room cycle moves the same averages,
and a change in the room causes the same signal. Drift slower than
this is not detectable; a sensor-check reminder every 30 days of power is the fallback.

## Alarms
Delay 15 min on slow-filtered values, so a compressor cycle does not reset it. Clearing
differential 1.0 °C, 2 %RH. Suppression: door + 20 min, defrost + 20 min, power-on up to
90 min and setpoint change up to 4 h, the last two closing at first entry to the band, and
per reading. Absolute limits are never suppressed. Severity info, warning, critical.

Silence stops the buzzer for 30 min, then it sounds again for unacknowledged alarms. A new
alarm always sounds. The dry contact follows active warnings and criticals regardless of
silence or ack. Ack stops the sound for that alarm; it stays listed until its condition
ends. Outage and cure-finished are latched until the operator deals with them.

History: every occurrence, every type, journalled with peak, limit and duration, restored
at boot, with no erase command. Times carry a clock-trusted flag.

## Consequences
The dry contact is on = alarm, so a dead controller cannot signal: an external watchdog or
normally-closed wiring is needed (gap). Warm load with no door switch may raise a warning.

## Amendment (integration defect 12, second re-test)
Every recovery of the climate sensor raised TEMP_SUSPECT (critical) for a few seconds, and with it a brief contact
close. The median window still held samples from before the fault, so after a long one the first true readings were
rejected as jumps, temperature recovered a few samples after humidity, and one channel in fault reads as a channel
failing validation. The stuck test also counted the fault's duration as time without change. While a channel is in
fault both now start again: the window empties and the stuck timer restarts, so samples after the gap are judged only
against each other.
