# ADR 0112: Changing setpoints during a running cure

Status: accepted

## Context
The core refused any setpoint change during a recipe. A batch runs for weeks. If a recipe
is wrong, or the cabinet cannot reach it, the only way out was to stop the recipe and lose
its phase, elapsed time and start weight. Reference products split on this: changing program
discards the old one (DRY AGER), while XLH360 remembers an edit for the next cycle.

## Options
1. Keep refusing. Simple, and it protects the recipe, but forces a stop that loses progress.
2. Edit the recipe in place. Changes what the recipe says, so the record no longer matches.
3. An override for the current phase only, on the record, with the recipe left unchanged.

## Decision
Option 3. `set_setpoint` during an active, paused or finished recipe applies to the current
phase. It has the same range checks as any setpoint, persists across power loss, ends when the
phase ends, and does not change phase time or the start weight. The event journal records old
and new values, and `Program.Override` shows it, so the panel must display that the cabinet is
not following the recipe and must confirm the change. The temporary alarm suppression for a
setpoint change applies, and ends at the first entry to the band. Sensor cross-check history
restarts, because the old behaviour no longer applies.

## Consequences
An operator can move a cure outside the recipe's conditions, and the process record will say so
but will not stop it. Whether that is acceptable belongs to the operator's own process plan.
