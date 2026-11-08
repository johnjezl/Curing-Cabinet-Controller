# ADR 0406: A simulation never drives a real line, and says so across the room

Status: accepted. Supersedes the `--sim-annunciate` part of ADR 0402. Fixes integration defect 10.
Revised after its first re-test: the band's 14 px lettering could not be read at 2 m, the stopped screen of a
simulation was unmarked, simulated alarms read like real ones, and the status file reported the simulated contact
under the real contact's name.

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
"SIMULATION ×N · NOT A REAL CABINET" in 32 px capitals (about 3.1 mm, over 5 arcmin at 2 m). The clock sits
inside the purple simulation pill on every screen ("SIMULATED ×N 18:13:12"), because it shows simulated time.
Alarm headlines read "SIM ALARM:" and "SIM WARNING:", since their action text names real parts. The frozen and
stopped notices of a simulation are purple, carry the band top and bottom, and say no cabinet was being controlled,
instead of asking for a thermometer check. The hatched frame and the SIM tag on each value stay.

## Consequences
- On the rig, GPIO 24 and 25 stay low for the whole of a simulation, whatever it raises. A dialer wired
  to the contact sees one open contact for the whole bench session, never a simulated alarm.
- The healthy (energized) contact state is no longer reachable on a bench with no plant. It is reached
  only by real mode with working sensors, which docs/wiring.md lists as not yet done.
- The band costs 44 px. Without an alarm banner the temperature and humidity digits keep their 104 px; with a
  banner they are 80 px in simulation, and 62 px with an output hold showing too. That is under the 14 mm reference,
  on purpose: a simulation should not read like a real cabinet from across the room. Real mode is unchanged.
- The status file's `alarm_contact_closed` and `buzzer_on` always describe the real lines. In a simulation they are
  false (the guard holds both off), and the simulated decision is in `simulated_contact_closed` and
  `simulated_buzzer_on`.
- `cured post-stop` accepts no argument or `--sim`, and refuses anything else.
- `deploy/local-rig.sh` runs the same binary on a development machine when the rig cannot be reached.
  It puts a recorder in place of the output guard, and `lines` prints every request made to it.
