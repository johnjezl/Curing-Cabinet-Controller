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
