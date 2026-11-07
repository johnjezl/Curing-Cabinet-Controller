# ADR 0113: A suspicion alarms, and may only move the cabinet toward cold

Status: accepted. Replaces the latch in ADR 0109. Amended after round 3 (see the end).

## Context
In round 2, a sensor-disagreement alarm latched the temperature channel as bad and moved the cabinet
to its blind hold until a person made a reference check. With no fault at all, this fired on healthy
sensors: at 2.3 days on a hot-room plant at 4 °C, and in three of four restart runs. True air then sat
8 to 14.7 °C over setpoint for 12.7 days, and 6 to 7 °C under a 13 °C setpoint elsewhere. The steady bars
measure hours 18 to 42 and could not see it.

## Options
1. Keep the latch and raise thresholds. Detection falls, and a latch with no exit stays possible at any rate
   above zero, on an unattended cabinet, where its cost is the whole batch.
2. A limited action on suspicion, such as blocking the heater. Smaller harm, but still a false signal
   changing regulation, and it is a guess which way the sensor is wrong.
3. Suspicion raises an alarm only. A direct reading that passes every direct test keeps control.

## Decision
Option 3. Control changes on a reading only for direct evidence about that reading: absent, out of range,
frozen, or too many rejected samples. The checks of ADR 0109 mark the displayed reading and raise an
alarm, and nothing else. `TestSuspicionNeverChangesRegulation` runs with the checks on and off and compares
every output.

A failed `reference_check` is a person's measurement and is treated the same: it alarms, and the way to act
on it is a calibration. The reasons are the same: the person may enter a wrong number, then leave.

Exits, none needing an operator: a suspicion ends 48 h after its evidence was last seen, or at a passed
check or a calibration. A failed reference check ends after 7 days, or at the same two. Nothing else latches
regulation.

Checks also run only when the cabinet has held within 1.5 °C of setpoint, defrost aside, for 80 % of the last 48 h, because effort says
nothing about a sensor when the plant is at its limit, and that plant's real problem is already alarmed from
the reading. History restarts after an outage of 10 min or more.

## Consequences
A real drifting sensor now keeps controlling until someone acts on the alarm. A low-reading sensor keeps
warming product after the alarm. This is a deliberate trade: the outcome of a real fault with an alarm against
the outcome of a false alarm that takes the batch with it. Option 2 could be revisited with a measured false-positive
rate from real cabinets, which does not exist yet. Detection and false-alarm figures are always shown together, in
`compare/detection_vs_false_alarms.csv` and `compare/fault_free_scorecard.csv`.

## Amendment after round 3
With suspicion alarm-only, a reading drifting low went on heating the cabinet after the controller's
own "reading may be low" alarm: heater duty to 97 %, true air 37.6 °C median and 42.3 °C worst at a
13 °C setpoint. Warm is the food-safety direction.

New rule: a suspicion of either reading may move regulation toward cold only. It blocks the heater
and the humidifier. It never reduces or stops cooling, and never adds heat or moisture. The worst a
false suspicion can now do is leave a heating-regime cabinet to drift toward a colder room, and leave
humidity uncorrected, both alarmed. `TestSuspicionNeverWarms` and `TestSuspicionOnlyEverMovesTowardCold`
check this, and the fault-free sweep counts how often a suspicion fires with nothing wrong.

A reading drifting low with the heater blocked takes the cabinet toward room temperature, not past it.

## Second amendment: forced cooling
"Toward cold" includes forcing cooling on. While a "reading may be low" suspicion stands (or a reference
check found the reading low), the blind pattern of ADR 0111 is a floor under the compressor: the suspect
reading can still ask for more cooling, but it can no longer stop it. Learning of that pattern pauses while
any temperature suspicion stands. A false low suspicion on a healthy cabinet therefore cools it by up to the
blind pattern's error until the suspicion ends (48 h after its last evidence); this is measured in the fault-free
sweep.
