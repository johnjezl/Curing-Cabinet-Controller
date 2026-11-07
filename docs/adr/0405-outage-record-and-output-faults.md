# ADR 0405: Outage record, silence, and outputs that cannot be written

Status: accepted. Fixes for the round 1 critic findings on part D.

## Outage record
Findings: the start was stamped at the last durable record, so a 12.6 s stop read as 23.4 s and a real
cut started at least 44 s early. Unknown temperatures were written as 0. With no RTC battery the
clock came back near zero, journal times ran backwards, and the duration was trusted too early.
"Power was lost" also appeared after a clean restart, and its severity varied: info, warning or critical.

Options for the start: (1) a heartbeat file written every few seconds, a new write load on the card;
(2) keep the last log record, now every 60 s, and say how uncertain it is; (3) log more often.

Decision: 2. These are small edits in `internal/core` and `internal/api`, listed below. The record says
the outage began between `at` and `at + start_uncertainty_s`, so `duration` is an upper bound by up to that.
`provisional` stays true until the clock is trusted. Unknown temperatures are JSON `null`. A clean stop
clears any restored POWER_OUTAGE; it stays in history. POWER_OUTAGE now starts as a warning and
becomes critical only if the temperature on return crossed the food-safety limit. A short outage that
came back in band still clears itself. The RTC battery is now listed as needed in docs/wiring.md.

Edits made by part D in shared code:
- `internal/api/api.go`: `Outage` gains `StartUncertaintySec` and `Provisional`, and a `MarshalJSON` that
  writes the two temperatures as null when `TempKnown` is false.
- `internal/core/core.go`: sets those two fields at boot, clears `Provisional` when the clock resolves the
  duration, and clears a restored `CodeOutage` after a clean stop.
- `internal/core/alarms.go`: `CodeOutage` default severity changed from info to warning.

## Silence
The critic saw silence fail in the simulator. Silence was working, but core's 30 minute re-sound timer
runs in virtual time, and at 700x that is 2.6 real seconds. Nothing changed in core. The evidence now
tests silence at 60x, where the timer is 30 real seconds (`evidence/d/66*`). Acknowledging a latched
event alarm clears it by core's design; silence never clears anything.

## Outputs that cannot be written
core stops petting the watchdog when outputs keep failing, so the guard's lease runs out and drops
every relay by its own path. systemd then restarted the service every 10 s for as long as the fault
lasted: 94 restarts in 24 minutes, each one resetting protection timers. Options: (1) keep restarting;
(2) remove the withholding from core, which loses the guard's drop; (3) keep the withholding, and have
cured tell systemd the process is alive while core still steps and reports OUTPUT_FAULT, and write
nothing to the guard meanwhile.

Decision: 3. The lease stays expired, so the outputs stay dropped and the contact open. OUTPUT_FAULT
stays on the panel and nothing restarts. A real hang still stops both the step and the notification,
so the watchdog still works. `evidence/d/67*` shows it.

## Also
- At start cured asks the guard for all-off, so a lease dropped under an earlier process, or under the
  simulator, does not show up as OUTPUTS_DROPPED in this one.
- `post-stop` removes the status file, so a dead process never reads as live.
