# ADR 0108: Alarm output semantics

Status: accepted. Replaces the output rules in ADR 0106.

## Context
In round 1 the contact and buzzer were on 81 to 94 % of every unattended simulated
cure, mainly because a past outage held them until acknowledged. A contact that is
already signalling cannot report a new fault, so an unattended cabinet in effect had
no external alarm. A dead controller also could not signal at all.

## Contact polarity
Options: active-on (simple, silent if control dies); energize-to-run.

Decision: energize-to-run. `OutAlarm` on means healthy with nothing to report; off
means signalling. Control death, panic, a lost guard and lost power all drop it, and
read as an alarm through normally-closed wiring. An orderly stop also drops it, which
is correct: a stopped cabinet is not being controlled. `OutBuzzer` stays active-on.

## What signals
Options:
1. Any active alarm signals until cleared (round 1). Past events masked later faults.
2. Only critical alarms signal. Quiet, but a weeks-long humidity problem never reaches anyone.
3. Warnings for 30 min in every 6 h (round 2). Quiet, but a standing condition was invisible
   92 % of the time, and acknowledging a critical stopped the contact with the fault still there.
4. Separate standing conditions, past events and suspicions, and let only standing conditions
   hold the contact.

Decision: option 4 (round 3).

| Alarm | Contact | Buzzer |
|---|---|---|
| Info | Never | Never |
| Critical condition | Open for as long as it is present. Acknowledge does not change this | Until acknowledged. Comes back 1 h after acknowledge if still present |
| Warning condition | Open until it clears or is acknowledged. Comes back 6 h after acknowledge if still present | 30 min in every 6 h, until acknowledged |
| Suspicion from one signal, with no direct evidence | 30 min in every 6 h, until acknowledged | The same |
| Past event (outage, recipe finished, outputs dropped, weight step, log damage, state reset) | 30 min once, stays listed until acknowledged | The same |

What restores the buzzer after an acknowledge: the hour above, a new alarm, or an escalation.

An outage starts as info because its importance is unknown until the cabinet is read
again. It becomes warning if it returned over the band, lasted an hour or more, or has
unknown length. It becomes critical if it returned over the absolute limit, or over
the 15.6 °C reference with a setpoint under it. A short outage back in band clears
itself and signals nothing.

## When the outputs cannot be written
If relay writes fail, the controller cannot move the contact itself. After 10 s of failed
writes it stops renewing the guard's lease. The guard then opens every relay by its own path,
so the contact signals and the compressor and heater stop. Stuck-on outputs had taken air to
-23 and +62 °C in round 2. With everything off the cabinet goes to room temperature, which is the
smaller harm and is bounded. With no guard fitted (no Watchdog) this cannot work, and only
OUTPUT_FAULT on the panel remains. On hardware, systemd's own watchdog also restarts the
service, which is harmless: restart honours the power-on delay.

## A new alarm while already signalling
Options: a pulse train coded by severity (a remote dialer has to decode it); re-opening
after a brief close; a second contact (no such output).

Decision: re-open after a brief close. When any warning or critical alarm is raised or
escalated while the contact is open, it closes for 15 s and opens again, and the buzzer
stops for the same 15 s and restarts. Dialers and PLCs trigger on the open edge, so
they see a second event. A new alarm also cancels a buzzer silence. The event log has
every change, so `signal_change_s` is measured.

## Consequences
A remote monitor must trigger on each open edge, not on level, and 15 s must be within
its input filter. An operator who never acknowledges anything still gets every new
fault. On a cabinet that cannot hold its setpoint the contact stays open, truthfully, for as
long as that is so; the edge is what carries news then. Silence affects the buzzer only and times out after 30 min. An external alarm
monitor is still required to learn anything when the Pi itself has no power: that is
the normally-closed wiring, and it is the installer's job to fit it.

## Amendment after round 3
- A critical past event (outputs dropped; an outage that came back over the absolute limit or over 15.6 °C
  under a lower setpoint) holds the contact and the buzzer until acknowledged. A warning event still signals
  for 30 min.
- ~~The buzzer is a relay on the same board. When the board cannot be written, or the guard has dropped every
  output, the buzzer cannot sound, and no software can change that.~~ Superseded by ADR 0407: the buzzer is
  energize-to-silence, so a dropped board sounds it. The contact does open, by the guard. A
  buzzer that sounds on a lost lease needs wiring through the guard or a normally-closed driver: a hardware
  decision for parts B and D.
- In the simulator's logs the contact is open whenever the plant is unpowered, which is what an energize-to-run
  relay does.
