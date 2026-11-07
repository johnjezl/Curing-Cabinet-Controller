# ADR 0402: Alarm contact is energize-to-run

Status: accepted. Measured on GPIO lines only. No relay has been connected.

## Context
The alarm and buzzer lines fall with everything else when control dies (ADR 0207), so a contact that
closes on alarm goes silent in the failure that most needs an alarm.

## Options
1. Keep energize-on-alarm. Simple, and silent when control, the Pi or the power is gone.
2. Energize-to-run. Everything that drops the line signals, with no code involved.
3. A separate supervisor process driving the alarm. It dies with the Pi, and a live supervisor could
   report a dead controller as healthy if its own check is wrong.
4. An external watchdog relay kept alive by a pulse train. Stronger: it would also cover a kernel hang
   sooner. It needs hardware that does not exist here. Left as the option in docs/wiring.md.

## Decision
Option 2. The relay on GPIO 24 is energized only while control runs and nothing calls for attention.

Who decides: `core` already drives `OutAlarm` this way (`on` = healthy, builder A). `cured` keeps that
as an input and adds what only it knows, so core's request never reaches the line directly. The line is
high only when all of these hold:
- core says healthy,
- no critical system alarm is active (`DISPLAY_MISSING`, `UNSAFE_SYSTEM_STATE`, `CONTROLLER_RESTART_LOOP`, and so on; warnings such as `PANEL_RECOVERED_AT_BOOT` are records only),
- control has run for 15 s since start or since a lease drop (`armDelay`),
- a valid temperature has been seen since then.

The last two stop the contact flickering healthy during start-up, or through a crash loop.

The line is set from the control goroutine, and its lease is renewed only by a control iteration. So a
hung loop, a dead process, a dead guard, a failed safety precondition, a panic after reset and a power
loss all drop it, by the same mechanisms measured in ADR 0207.

## What it can and cannot do
Pulling the contact open needs no working software. These cases were measured on the line with no relay
attached; the timings are in `evidence/d`.

**A kernel panic or hard hang is the exception.** While the kernel is stopped, nothing changes the
lines, so the relay stays energized and the contact keeps saying healthy until the board resets: about
10 s for a panic by the bracket in ADR 0207, and the hardware watchdog path is not proven (ADR 0203).

**The local buzzer cannot sound when the Pi is dead, and no software can change that.** It runs from a
relay the Pi holds energized, so anything that stops the Pi also stops it: a dead process leaves it off,
and a power cut removes it. What can sound is a sounder powered independently and wired through the
alarm relay's normally closed contact, which is the wiring in docs/wiring.md. If mains goes too, that
sounder needs its own battery. The independent thermometer with its own alarm in docs/wiring.md is
still required: an alarm contact says the controller has failed, not that food is cold.

## Consequences
- The contact reads as alarm during every boot and restart, 15 s or more after control starts. A remote
  dialer needs a delay of about a minute, or it will call on every power blip. That is the honest reading.
- The relay spends its life energized. It must be rated for continuous duty.
- A welded contact shows healthy forever. Testing it periodically is the owner's job, in the checklist.
- On a bench with no plant, `--sim-annunciate` lets the simulator drive these two lines, and only these,
  so the healthy-to-failed transition can be measured. It is on in the bench unit only. Real hardware
  mode with no sensors never gets to healthy, by design.
