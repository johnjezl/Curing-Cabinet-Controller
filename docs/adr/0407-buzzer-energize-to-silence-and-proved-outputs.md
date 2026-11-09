# ADR 0407: The buzzer is energize-to-silence, and output chips show what is proved

Status: accepted. Fixes integration defect 4 and the panel half of defect 1. Supersedes the buzzer rules in
ADR 0108 (amendment after round 3) and ADR 0402 ("the local buzzer cannot sound when the Pi is dead").

## Context
With the relay board refusing writes (`outputs:write-err`), the integration critic saw the panel show COOL ON and
HEAT ON while nothing could switch, the true air rise from 11.7 to 15.9 °C, and TEMP_HIGH_CRITICAL raised with the
buzzer silent (GPIO 25 low) while the panel offered SILENCE BUZZER. With a heater relay stuck closed the HEAT chip
read "HEAT off" for 16 virtual hours. The buzzer also went silent for 3 to 17 s during every crash or hang
(defect 12). ADR 0108 had said a buzzer on the same board cannot sound when the board cannot be written, and left
it to hardware.

## Options for the buzzer
1. Keep it energize-to-sound. It is silent whenever its line drops: on a write fault, a crash, a hang, a guard
   drop. No software fixes that.
2. A second, independent driver for the buzzer. No such output exists in the parts list.
3. Energize-to-silence, as the alarm contact already is (ADR 0402): the controller holds the buzzer relay
   energized to keep it quiet, and the sounder is wired to the relay's normally closed terminal. Every way the
   line can drop sounds it.

## Decision
Option 3. core writes `OutBuzzer` energized for silent; cured's annunciator does the same; the line filter and
annunciator read it that way. When core withholds the guard's lease because outputs cannot be written, the guard
drops every line, so the contact opens and the buzzer sounds; the annunciator reports exactly that while it holds
off. In `cured --sim` a simulated guard (`sim.SimGuard`, virtual time, 3 s lease) now sits on the simulated plant's
relays, so a write fault drops them as the rig's guard would; before, the simulated relays stayed wherever they last were.

Output chips: core's output status gains two kinds. `unknown` while any output write is failing: nothing commanded
is proved, so the chips read COOL UNKNOWN, HEAT UNKNOWN and so on, hatched. `stuck` when a cross-check proves a relay
closed against its command (COOL_STUCK_ON, HEAT_STUCK_ON): the chip reads HEAT STUCK ON in red. Otherwise the chip
shows the command, which a successful write and no contrary evidence is the best proof available: there is no current
sensor. While outputs cannot be written the banner shows "OUTPUTS DOWN: CANNOT SILENCE" instead of a SILENCE button,
because no command can reach the buzzer then.

## Consequences
- The sounder sounds from power-on until control's first step (about 40 s on the rig), during every restart, after an
  orderly stop until the service is back, and in real mode whenever a critical alarm is unacknowledged. A planned stop
  sounds it: that is what an uncontrolled cabinet should do.
- In a simulation no line is driven (ADR 0406), so on a bench with a sounder wired it sounds for the whole session.
  The status file says so (`buzzer_on: true`, `real_lines`). The simulated decision is `simulated_buzzer_on`.
- Wiring changes: the sounder goes on the GPIO 25 relay's **normally closed** terminal (docs/wiring.md). Nothing has
  been wired yet, so no installed cabinet changes.
- A sounder powered from the Pi's supply is still silent on a power cut. The independently powered sounder on the alarm
  contact's normally closed side (docs/wiring.md) remains the one that covers that.
- A stuck relay is only shown once its cross-check alarm is raised; how fast and how accurately is defect 1's other
  half (ADR 0110 and its successor).

## Amendment after the first re-test
The re-test found the write fault itself right (chips UNKNOWN, buzzer on, CANNOT SILENCE, one record), and its end
wrong. On repair core met the guard's drop it had caused itself and raised OUTPUTS_DROPPED, "The control loop stalled",
which it had not. The new alarm then started a brief close while the relay was still proving health, and that build
also took the buzzer off during a brief close: buzzer off for 18.5 s with TEMP_HIGH_CRITICAL standing. During the
fault COOL_NO_EFFECT blamed a compressor that could not be switched, and FAN_STALL a fan the guard had stopped.

Changes, each removing a judgment made on something that did not happen:
- A drop that ends core's own hold is recorded as an event ("outputs restored after the output fault") and is not
  OUTPUTS_DROPPED: OUTPUT_FAULT already said it. A drop core did not cause still raises OUTPUTS_DROPPED.
- The annunciator starts no brief close until it is armed again: before that the contact is open and may not close.
  The buzzer follows the alarms throughout (it has not been gated by the brief close since ADR 0408's second revision).
- Effect checks (compressor, heater, humidifier) do not judge a run that overlaps an output fault or the 30 min after
  it, and a long run's window starts again when it cannot be judged. FAN_STALL is not raised while the guard holds the
  outputs or for 60 s after.
- A TEMP_HIGH_CRITICAL record carries the limit that made it critical (15.6 °C, or the band after 2 h), so its peak is
  never below its own limit.

`TestOutputWriteFaultOpensContactThroughGuard` (an hour's fault in a room 12 °C warmer) and
`TestAfterADropNoBriefCloseAndTheBuzzerSounds` hold this.

## Second amendment (next fix round)
AIR_LOW, like FAN_STALL, is not raised while the guard holds the outputs off or for 5 min after: the guard stopped the
fan. The COOL chip's proved state now includes COOL_NO_EFFECT ("running, but the cabinet is not cooling"), and a closed
compressor relay is named and kept named as ADR 0114's amendment describes, so the chip no longer returns to "COOL off"
while the relay is still closed.
