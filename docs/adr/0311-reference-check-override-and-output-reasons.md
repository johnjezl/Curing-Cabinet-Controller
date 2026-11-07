# ADR 0311: Reference check, recipe override and output reasons on the panel

Status: accepted

## Context
The core added three things an operator can reach a dead end without: a reference check
(the only way, besides calibration, to clear a reading in doubt), a recipe override for one
phase (ADR 0112), and a reason for each output's state.

## Reference check: options
1. Pre-fill the sensor's value and adjust it. Quick, but one tap can clear a false reading
   with no instrument involved.
2. A user-level action with a confirmation. A person can dismiss a critical alarm by typing
   something plausible while looking at the sensor's number.
3. Behind the installer code, with blind entry and a held confirmation.

## Decision
Option 3. It sits behind the installer code, on More, the installer menu and the button on
the alarm itself. The reference value is typed into an empty field with the sensor's value
out of sight. Only then does the confirmation show sensor, reference and difference, and say
what recording will do: clear the doubt, or raise a critical alarm. It needs a 2 s hold.

A sensor with no reading is not offered for comparison, because the core would compare
against its last value. If neither sensor reads, the panel says to fix the wiring. It is not
offered while locked or while the controller is not responding.

The panel words the outcome before the core decides it, so it mirrors the core's tolerances,
and a test fails if those change.

## Override: options
Show it like any setpoint change; or confirm it and mark it wherever the setpoint appears.
Chose the second. Each row shows what the recipe asks for. SAVE becomes OVERRIDE, and a held
confirmation states new and recipe values, that it covers this phase only, and that it goes on
the record. While active, the setpoint on Home is an amber OVERRIDE label in every layout,
including under an alarm banner, and the cure card and Cure screen say so too.

## Output reasons
Home has no room for three sentences and a row too short to be a fair touch target. So: a new
OUTPUTS screen lists each output with its state, its kind and the core's own reason, and any
timer. On Home, a fault shows on the chip, and when the reading is over a degree off setpoint
while the output that would correct it is off, the core's reason appears in the holds line.

## Reminder
Health and More show when the check is due and when it last passed. Overdue turns the button
amber. The core's reminder alarm carries a CHECK button that goes straight to the entry.

## Consequences
- The keypad has no minus key, so a reference below 0 °C cannot be entered. In a cabinet near
  freezing that is a real limit; the sensor can still be compared through calibration.
- Someone without the code cannot clear a false critical. That is deliberate, and it means a
  site needs someone who has the code.
- There is no way to end an override early from the panel, because the core has no such command.
  Setting the recipe's values brings the cabinet back, but the record still shows an override.
