# ADR 0003: Stop iterating part B on the no-hardware blocker

Status: accepted

## Context
Two blind rounds of part B both lost for the same reason: no real sensor, fan or
relay has ever been attached, so the judge has no reading to weigh against the
reference's documented figures. In round 2 the judge said the gap cannot be
closed without connecting real sensors and loads. The rig has only the display,
and the project owner has said that missing sensors are expected on it.

## Options
1. Keep looping. More fake-device tests cannot change the deciding item.
2. Build a hardware emulator on the Pi's pins. No wiring can be added to the rig,
   and it would still not be the real parts.
3. Stop part B under the "same blocker repeats" rule, and report that it has not
   beaten the benchmark.

## Decision
Option 3. Part B has not beaten the benchmark. Round 2 did bring measured gains:
outputs drop on their own within 3 s of control stopping, on 15 of 15 runs, read
from the Pi's registers with no relays attached.

## Consequences
docs/wiring.md holds a first-power-on checklist that the owner has to run. Until
then, all drivers are unproven against real parts.
