# ADR 0409: The outage record says when and why; weight loss survives a load change; live conditions headline

Status: accepted. Fixes integration defects 6, 8 and 9. Amends ADR 0107 and ADR 0405 (outage record) and ADR 0302
(banner).

## Context
- Defect 6: a 14.5-hour power cut appeared in the alarm history dated at the return, not the loss, with duration -1;
  four service crashes and hangs appeared as "Power was lost", also with duration -1. The real figures existed only in
  status fields, and the status file wrote the duration in nanoseconds.
- Defect 8: after 5 kg of product went in, the panel showed LOSS -46 %, and -36 % on the end-of-cure line.
- Defect 9: an open door was hidden behind "Temperature low (+2 MORE)", and old events (OUTPUTS_DROPPED, the restart
  loop) headlined over a live critical excursion and over the end of the cure.

## Decision
- **When:** the outage alarm is dated from the last durable record before the loss (the same uncertainty as ADR 0405
  states), when the clock can be trusted; otherwise from the return, flagged as before. The journal carries the date, so
  the history keeps it across restarts.
- **How long:** the history entry's duration is the outage's length when known, -1 only when it is not.
- **Why:** core remembers the operating system's boot ID. An unclean stop within the same boot was the process (a
  crash, a kill, a hang caught by the watchdog), recorded as CONTROLLER_RESTARTED, "Controller stopped and restarted";
  a new boot is POWER_OUTAGE. Both are events, counted as outages for the cure (control was away either way), and
  `Outage.Cause` says which. The status file writes `duration_s` in seconds.
- **Load change:** when the scale accepts a step (three agreeing samples at a new level), the step is added to the start
  weight. Loss then stays the mass lost over all the mass that went in: it neither goes negative when product is added
  nor jumps when some is taken out. Taking everything out clears the start weight, and the next good reading starts it
  again. The event log records the change and the loss before it. WEIGHT_STEP's action says so.
- **Headline:** live critical conditions first, then an open door (usually the cause of whatever else is out), then
  live warnings, then past events of any severity newest first (so the end of a cure beats an old restart burst), then
  standing information. Alarms now carry `event` so the panel can tell.

## Consequences
- A crash is no longer reported as a power cut, so a power log can be trusted. A crash with the clock untrusted is still
  dated at the return.
- Re-basing assumes the added product starts at no loss. For removed product it assumes the pieces taken out had lost
  what the batch had; the figure is a batch yield, not a per-piece one, as before.

## Revision after the first re-test (defect 6; defects 8 and 9 passed)
- An outage alarm still unacknowledged at the next outage took the next one's duration, because its duration was read
  from the current outage record when it was cleared. Each outage alarm now carries its own outage length, journalled
  with it, and replay restores it; a length resolved later, when the clock arrives, is journalled too.
- A correctly dated POWER_OUTAGE was re-dated to its return at the next restart: its escalation was journalled as a
  second raise, and replay took the second date. Replay now keeps the first raise's date for an escalation, and the
  escalation carries it as well. The peak recorded when the outage closes is restored with the alarm.
- cured's own event alarms (the restart burst) and its alarm history are kept in the state directory
  (`system-alarms.json`), so an unacknowledged one is not lost at a power cut. Conditions are found again by the new
  process and are not kept.
- Weight loss: removing product scaled the start weight by the change instead of in proportion, crediting the remaining
  product with water the removed pieces had lost (5.6 % jumped to 7.0 %, the unsafe direction). A removal now scales the
  start weight by the share of the load that stays, so the figure does not jump; an addition still adds.

## Revision after the second re-test (defects 6 and 8 passed)
- Headline: an unacknowledged critical past event (a power outage, the restart burst) now comes before other past
  events, which stay newest first. The re-test saw "Recipe finished" and "Controller stopped and restarted" headline
  over an unacknowledged critical "Power was lost" and "Controller keeps restarting", hidden behind "+1 MORE". Integration
  defect 3's case, an acknowledged old burst over the end of the cure, still goes the other way.
- An outage's peak survives a second gap before its record closes: the temperature on return, already journalled, is
  restored as the peak, the least it can have been. Before, the history entry had none.
- An orderly restart no longer takes an unacknowledged outage alarm off the list: like every event, it stays until
  acknowledged.
- The load-change event gives the loss before and after the change from the weights on either side of the step
  ("0.7 % before the change, 0.5 % after"), instead of the filtered weight, which had already moved (it read -49.4 %).
- The restart-burst alarm keeps the largest count it has reported: a reboot restarts the count, and the record had
  dropped from 8 to 4.

