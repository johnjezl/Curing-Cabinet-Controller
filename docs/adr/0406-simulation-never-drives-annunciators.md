# ADR 0406: A simulation never drives a real line, and says so across the room

Status: accepted. Supersedes the `--sim-annunciate` part of ADR 0402. Fixes integration defect 10.

## Context
The integration critic ran a compressed cure on the rig in simulation. With `--sim-annunciate` the
simulated alarms drove GPIO 24 (alarm contact) and GPIO 25 (buzzer). With a dialer or contact monitor
wired, a simulated alarm goes out as a real one, and a simulated "healthy" closes a real contact. The
panel marked simulation with small SIM tags and a 6 px hatched frame; at 2 m it looked like a running
cabinet, with no speed factor, and its clock showed simulated time.

## Options
1. Keep `--sim-annunciate` for bench evidence of the healthy-to-failed transition, off by default.
   One wrong unit file and simulated alarms reach a dialer.
2. Let the simulation drive the lines only to the safe side (always "signalling"). Still a line moving
   for simulated reasons.
3. The simulation never opens the output guard. The guard holds every line off, so the energize-to-run
   contact reads "not controlling a cabinet" for as long as a simulation runs, which is true.

## Decision
Option 3. `cured --sim` builds no driver and never connects to the guard; `--sim-annunciate` is refused
with its reason, and `curing-sim.service` no longer passes it. `cured post-stop --sim`, run after the
simulator's unit, does not talk to the guard either. The annunciator still decides the simulated contact
and buzzer, so the panel and the status file show them, and the status file says
`annunciator_lines_driven: false`. In real mode it says `true`.

Panel: the home screen, the across-the-room view, starts with a full-width purple band reading
"SIMULATION ×N · NOT A REAL CABINET · SIMULATED CLOCK" above the header. Every other screen keeps the
"SIMULATED ×N" pill beside its clock. The hatched frame and the SIM tag on each value stay.

## Consequences
- On the rig, GPIO 24 and 25 stay low for the whole of a simulation, whatever it raises. A dialer wired
  to the contact sees one open contact for the whole bench session, never a simulated alarm.
- The healthy (energized) contact state is no longer reachable on a bench with no plant. It is reached
  only by real mode with working sensors, which docs/wiring.md lists as not yet done.
- The band costs 24 px. The temperature and humidity digits keep their 104 px except in one state:
  simulation with an alarm banner and an output hold both showing, where they are 84 px (11 mm), under
  the 14 mm reference. Real mode is unchanged.
- `deploy/local-rig.sh` runs the same binary on a development machine when the rig cannot be reached.
  It puts a recorder in place of the output guard, and `lines` prints every request made to it.
