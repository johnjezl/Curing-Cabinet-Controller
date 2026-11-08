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
- **COOL_STUCK_ON** needs the dew point not rising (mist from a humidifier pushes it up) and is not raised by reheat
  against a humidifier at saturation. **HEAT_NO_EFFECT** is not judged while dehumidifying.
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

Across the 36-plant sweep: heater stuck identified on 31/36 (13 °C) and 26/36 (24 °C) plants, median about 100 min;
humidifier 20/36 and 27/36, median 30 to 37 min; compressor 33/36 and 15/36. Wrong accusations remain on weak plants:
HEAT_STUCK_ON on 3/36 in a room raised by 12 °C and on 8/36 recovering slowly from a compressor repair in that room;
COOL_STUCK_ON on 2 to 4 with a stuck humidifier. Sensor drift detection is unchanged from before this ADR.

## Consequences
- The sensor suspicion still fires for a sharp room change (240 to 360 min after +6 to +12 °C on the simulator's plant),
  by ADR 0109's design. Its effect moves the cabinet toward cold, and its text now names both causes.
- Nothing here sees a stuck relay faster than its signature allows: a heater the compressor can beat is found only by
  the duty it costs, after the cabinet has steadied.
