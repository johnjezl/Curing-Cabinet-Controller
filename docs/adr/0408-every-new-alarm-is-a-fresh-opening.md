# ADR 0408: Every new alarm is a fresh opening of the contact

Status: accepted. Fixes integration defects 3 and 12 (contact items). Amends ADR 0108 (warning acknowledged) and ADR
0402 (system alarms holding the contact).

## Context
After a burst of restarts, CONTROLLER_RESTART_LOOP (critical, cured's own) stayed active for the rest of the cure, even
acknowledged, and held the contact open. core's brief close for a new alarm (ADR 0108) never reached the line, because
cured opened the contact whenever any system alarm called. A dialer saw nothing for days. Separately, acknowledging a
standing warning (TEMP_LOW) closed the contact while the excursion continued, for 6 h.

## Decision
- cured makes the brief close itself, in real time (15 s), whenever core starts one (core now counts them,
  `EdgeCount`) or a new system alarm starts calling while the contact is already open. A remote monitor triggering on
  open edges sees every new alarm, whoever holds the contact. The status file counts openings
  (`contact_openings`, or `simulated_contact_openings`), so the edges can be checked at any sampling rate.
- A burst of restarts is a past event, as ADR 0108's table already says for events: it calls for 30 min or until
  acknowledged, stays listed until acknowledged, and an acknowledgement moves it to history. It still clears by itself
  after 30 min of stable running.
- A condition holds the contact for as long as it is present, warning or critical. An acknowledgement stops the buzzer
  and says someone has seen it; it no longer closes the contact. A warning's buzzer reminder after 6 h stays.
- The buzzer no longer goes silent during a crash or hang: it is energize-to-silence (ADR 0407).

## Consequences
- A standing warning (say, humidity that cannot be reached) keeps the contact open until it clears. New alarms still
  show as a close and reopen; a dialer must trigger on each open edge (ADR 0108 already requires it).
- In a simulation the brief close lasts 15 real seconds, which is hours of simulated time at high speed; the opening
  count is the record to read.

## Revision after the first re-test (defect 12)
The re-test passed defect 3 and failed 12: the brief close also stopped the buzzer for 15 s during a standing critical
alarm, and came for every alarm event including reminders; the buzzer stopped for 15 s after every restart; the
restart-burst event called for 30 simulated minutes, over before the contact armed in a compressed run; a silence's end
was not recorded, and the panel showed it silenced after it had ended; the opening count started again at every restart.
- The buzzer no longer stops for the brief close, in core or in cured; only the contact makes the transition.
- Only a new or escalated alarm makes a brief close. A reminder (an acknowledged alarm coming back) sounds again but is
  not news, and makes none. cured makes no brief close on its first tick.
- A system event (the restart burst) calls for 30 minutes of real time, the process's own time, in every mode.
- The end of a silence is journalled, and cured's own silence ends at 30 minutes as core's does and is shown so at once.
- The opening count is kept in /run/curing-cured/contact, so it survives restarts within a boot, and a process that dies
  with the contact closed has that opening counted at the next start.
- cured's system alarms are written to core's event journal (type "system"), so the record of a cure has them all.

## Revision after the second re-test (defects 3 and 12)
The re-test passed the buzzer, acknowledgement, silence and restart parts, and failed both defects on transitions.
- core counted a new alarm for cured's brief close only when core itself was signalling. With only the restart burst
  (a cured system alarm) holding the contact, a new SENSOR_CLIMATE_FAULT made no transition in 26 of 27 onsets, and
  FAN_STALL, DOOR_OPEN and others none either. core now counts every new or escalated alarm; cured's annunciator decides
  whether a brief close is needed (the contact is open and armed).
- Started with the climate sensor dead, the relay never armed (it waited for a good temperature reading), so no new
  alarm could make a transition for as long as the sensor stayed dead. It now also arms 60 s after start without one:
  by then core has raised the sensor fault, which holds the contact open by itself. Before 60 s the rule is unchanged,
  so a start with an unread sensor cannot close the contact. No brief close starts while unarmed.
- A power cut set the opening count to 0 and did not count the opening the cut made. The count moves from /run to the
  state directory (/var/lib/curing/contact, or the simulator's), written atomically.
- The restart-burst alarm went away unacknowledged 30 minutes after the last start, with no record. As an event it now
  stays listed until acknowledged; it calls the contact for 30 minutes, as before, and new alarms make transitions
  over it.
- A silence ended early by a new alarm is journalled ("silence ended early: <code> sounded").
`TestDeadSensorAtStartStillGivesFreshTransitions`, `TestOpeningCountSurvivesAPowerCut` and
`TestNewAlarmCountedAndSilenceEndRecordedAndRecoveredSensorNotStuck` hold this.
