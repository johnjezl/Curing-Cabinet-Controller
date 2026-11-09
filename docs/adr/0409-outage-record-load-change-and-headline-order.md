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

- The system-alarms file is written off the caller's goroutine, never while holding the alarm table's lock (ADR 0408,
  "Regression found on the rig").

## Revision in the next fix round (defects 6 and 9)
- A crash's outage was dated from the last per-minute journal record, so a 2.7 s kill on the rig read as 45 s, and in
  compressed simulation crashes started up to 3.5 virtual hours early. core now takes a later sign of life from outside
  its journal, used only if it is after its own last record, within an hour of it and not in the future: in real mode,
  post-stop keeps when the stopped process last stepped control (the status file's write time less the age of its
  last control tick, so a hang does not count as alive) in /run/curing-cured/last-alive, which exists only within one
  boot; in simulation, the plant checkpoint's simulated time (written every half second). The stated start uncertainty
  becomes 2 s, or the checkpoint interval in simulated time. A power cut keeps the per-minute bound in real mode.
- Headline: within a rank, unacknowledged alarms come first, newest first (was oldest first). On the rig an older live
  critical ("humidity reading looks wrong") kept the headline over HEAT_STUCK_ON, outputs down and a power outage; the
  newest is usually the cause named after the excursion it explains, or a new fault.
`TestCrashDatedFromTheLastSignOfLife`, `TestLastAliveFromStatus` and `TestHeadlineRanksLiveConditionsOverEvents` hold this.
- Load change: a weight step was re-based a few seconds after three agreeing samples, so a scale glitch that held
  briefly was taken as a load and corrupted the cure's loss for good (the defect 5 re-test saw a glitch read as 69 kg,
  and spikes driving the loss to -190 %); the glitch's end was then a second step. A step is now taken as a load change
  only once the new level has held 10 min, is cancelled if the weight comes back within 100 g of its level before, and
  is not re-based while the scale is faulted; with a door switch fitted, a step with the door shut throughout (no open
  or close within 10 min before it) is never a load change, and the event says to check the scale. Until a step is
  confirmed the loss figure stays at its value before the step. Without a door switch a glitch that holds more than
  10 min at a new level is still taken as a load. `TestScaleGlitchDoesNotRebaseTheLoss` holds this.
