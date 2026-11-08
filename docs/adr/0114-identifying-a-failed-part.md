# ADR 0114: Identifying a failed part from what only that failure does

Status: accepted. Replaces the HEAT_STUCK_ON rule of ADR 0110 and narrows ADR 0106's cross-checks and ADR 0109's
sensor suspicion. Fixes integration defect 1 (diagnosis half).

## Context
On the rig a heater relay stuck on was never identified in 16 virtual hours, while a healthy heater was accused twice
("Disconnect it"): once while the cabinet recovered after a compressor repair in a room raised by 12 °C, once after an
output fault cleared. The old rule read "cooling demand jumped with the heater off" as a stuck heater, and recovery
from any warm excursion makes the same jump. Offline, a humidifier stuck on had no alarm of its own; instead it raised
COOL_STUCK_ON (mist cools the air with the compressor off), HEAT_NO_EFFECT (reheat losing to dehumidifying) and, from
the sensor cross-check, TEMP_READING_DISAGREES, whose forced cooling then took the cabinet below 0 °C. A compressor that
does not cool also raised the sensor suspicion, and could raise HUMID or HEAT accusations before COOL_NO_EFFECT.

## Options
1. Tune thresholds on effort. Every failure and every room change moves effort; no threshold separates them.
2. Probe actively (switch parts and watch). A stuck relay cannot be switched, and probing changes regulation.
3. Look for what only each failure does, and let a known failure explain the effort it causes.

## Decision
Option 3:
- **Coil works**, proved by the dew point: a run that lowers it by 0.5 °C condenses water, which only a cold coil does.
  Heat alone leaves the dew point where it is, so this holds even when a stuck heater outruns the compressor.
  COOL_NO_EFFECT is not raised by a run that condenses, nor while a heater is already known stuck.
- **HEAT_STUCK_ON**: heater commanded off for 30 min, coil shown working, and either the compressor runs at least 60 %
  of the last half hour while the air sits 1 °C or more over setpoint, flat or rising (a recovering cabinet comes down),
  or the air has held in band for half an hour only by running 0.45 of duty above its own 24 h figure, with no door or
  load for 6 h. It latches until the cabinet has been back to normal for 30 min. The action asks the operator to feel
  the heater before disconnecting it, because a much warmer room can look the same on a weak cabinet.
- **HUMID_STUCK_ON** (new, critical): humidity at saturation (97 %, or setpoint + 5) for most (60 %) of 20 min with the humidifier
  commanded off for 30 min and the coil shown working. It latches until humidity has been normal for 2 h.
- **COOL_STUCK_ON** needs condensation: the dew point falling at least half as fast as the air (mist from a humidifier
  pushes it up; a room turned cold cools the air far faster than its leak lowers the dew point). Its "heater suddenly
  busy" form needs the air held within 1 °C of setpoint, as a working heater against a closed compressor relay holds it.
  **HEAT_NO_EFFECT** is not judged against air saturated by mist.
- **HUMID_STUCK_ON** also needs the dew point most of the way from the setpoint's toward saturation at setpoint, with the
  air within 1.5 °C of setpoint: a cabinet gone cold saturates its air with the water it already had (the first re-test
  found it raised for a heater stuck off in a cold room).
- **Sensor suspicion** (ADR 0109, 0113) is not evaluated while any equipment fault alarm is active or was in the last 6 h.
  Its triggers are otherwise unchanged: removing the effort-only ones took detection of a reading drifting low at 1 °C/h
  from 5 h (true air +4.9 °C) to 21 h (+19 °C, the heater warming product to 35 °C), and that is the worse failure. Its
  alarm now says the room may have changed instead of naming the sensor alone: "Cabinet effort changed with no change in
  the reading".
- Output chips show HEAT STUCK ON and HUMID STUCK ON from these alarms (ADR 0407).

## Measured (simulator's plant, 2 days at setpoint then the event, 12 h observed)
| Event | 13 °C | 24 °C | 4 °C | Other equipment alarms |
|---|---|---|---|---|
| Heater stuck on | HEAT_STUCK_ON 75 min | 165 min | 18 min | none |
| Humidifier stuck on | HUMID_STUCK_ON 37 min | 28 min | | none |
| Compressor does not cool | COOL_NO_EFFECT 39 min | | | none |
| Room +12 °C, room -10 °C, 5 kg warm load, recovery after compressor repair or output fault | none | none | none | none |

Across the 36-plant sweep (2 days at setpoint, 12 h observed): heater stuck identified on 31/36 plants at 13 °C,
26/36 at 24 °C and 29/36 at 4 °C, median 28 to 110 min; humidifier on 24, 29 and 31 of 36, median 28 to 47 min;
compressor on 33, 15 and 29 of 36. No healthy plant, cold room or warm load raised an equipment alarm, except one
COOL_STUCK_ON in a cold room and one HEAT_STUCK_ON after a warm load, both at 4 °C. Wrong accusations remain on weak
plants: HEAT_STUCK_ON on 1 to 4 of 36 in a room raised by 12 °C and on 4 to 8 of 36 recovering slowly from a compressor
repair in that room; COOL_STUCK_ON on 3 to 5 with a stuck humidifier. Sensor drift detection is unchanged from before
this ADR.

## Consequences
- The sensor suspicion still fires for a sharp room change (240 to 360 min after +6 to +12 °C on the simulator's plant),
  by ADR 0109's design. Its effect moves the cabinet toward cold, and its text now names both causes.
- Nothing here sees a stuck relay faster than its signature allows: a heater the compressor can beat is found only by
  the duty it costs, after the cabinet has steadied.

## Amendment after the first re-test
Identification passed every trial. The failure was the sensor check: 238 min after a compressor was repaired in a room
12 °C warmer, TEMP_READING_DISAGREES (critical, "reading may be low") marked the healthy sensor in doubt and held heat
and humidity off for 48 h, the cabinet at 11.7 °C mean against 13 °C. The 6 h gate above was updated only when the
check ran, and it does not run in a setpoint's first 72 h or with the cabinet out of band, which is exactly when a
compressor fault happens. Measured afterwards on the simulator with 4 days of history, any warmer room of 6 to 12 °C
raised the critical within 4 to 6 h by itself, from the fast comparison with the same hours yesterday.

Changes:
- **History restarts while a part is known to misbehave**, judged every step, and while a suspicion holds heat and
  humidity off. A repaired or failing part makes a different cabinet from the one the history describes, and so does a
  held-off heater. This replaces the 6 h gate. Checks resume 72 h after, as after a new setpoint.
- **More cooling than yesterday, on effort alone, is a warning** (TEMP_DRIFT_SUSPECT, which names a room change as a
  cause), not a critical: a warmer room does exactly this, and a reading high is the cold, food-safe side. Less cooling
  than yesterday, the sign of a reading drifting low, keeps the critical: without it a reading falling 1 °C/h is caught
  at 17 h instead of 5 h, and the product reaches +22 °C instead of +4 °C. A room 10 °C colder still raises it, within
  2 h; the alarm says the room may have changed, and its action moves the cabinet toward cold.
- The panel shows a reading in doubt as "READING IN DOUBT: HEAT, HUMID HELD", not "NO TEMP FEEDBACK": the compressor
  still follows it.
- Wrong-cause text: RH_LOW no longer says "Refill the humidifier" alone (heat also dries the air), a defrost cut short
  while the heater is shown stuck on is not counted as ice, and a STUCK chip's reason is in the past tense, since it
  stays latched after the evidence ends.

Measured (base plant, 13 °C, 4 days of history, 4 days observed): room +6 or +12 °C for 10 or 30 h, a compressor
repaired in that room, an output fault of 1 h: no sensor critical (TEMP_DRIFT_SUSPECT once, 16 h after a +12 °C room).
Room -10 °C for 12 h: TEMP_READING_DISAGREES at 2 h. Reading drifting at 1 °C/h: low caught at 5 h (13 °C) and 6 h (4 °C),
unchanged; high now critical at 19 h instead of 6 h, warned at 18 h. Slow drifts unchanged. Detection of a slow drift
already took days (-0.1 °C/h: 66 h), and this does not improve it. `TestNoSensorAccusedAfterARoomChangeOrARepair`
holds the cases above.

