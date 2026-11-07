# ADR 0208: Fan state when control is gone

Status: accepted, not verified with a real fan

## Context
The fan's PWM runs in RP1 hardware, so it does not stop when software does. What should happen?

## Options
1. Keep the last duty when control is lost, and start at duty 0 after any reset until commanded.
2. Have the guard force a fixed duty on lease expiry. Requires the guard to own the fan as well.
3. A nonzero duty from boot, before control starts.

## Decision
Option 1. A moving fan cannot overheat or wet anything, so nothing needs to change it within the
few seconds a hung service needs to be restarted. A fan stopped at boot is what happens anyway
during the 20 to 40 s before any software runs, so 3 would not remove that window. Air movement
with no control running would also keep drying product.

Measured from registers, no fan attached: the duty holds through a stuck, frozen or killed test
driver and a killed or frozen guard (`evidence/b/28-...`). After a panic or a power cut the channel
reads duty 0 once bring-up has run (`40-...`, `43-...`, `static-power-cut-*`). Before that the pin
is in its reset state, `no pd`, as logged at the start of `curing-hw-init`. During a panic nothing
observes it. See the fan state table in docs/wiring.md for what the fan should see in each state,
which depends on the buffer and on the fan and is not measured.

## Consequences
`VerifySafeState` checks the PWM channel is enabled at 25 kHz. The application calls it at startup
and shows a failure; the `Fan` driver also repairs a wrong period on the next `SetDuty`.
