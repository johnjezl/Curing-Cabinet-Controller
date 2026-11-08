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
