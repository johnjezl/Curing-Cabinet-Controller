# Final report

**Outcome: not ready for a batch.** Only the front panel (C) and the system layer (D) beat the frozen
benchmark in blind comparison. The control core (A) and the drivers (B) stopped on the repeating
"no real hardware" blocker. The final integration critic, after a complete compressed cure with
failures on the rig, said **no**, and a blind judge preferred the reference, moderately.

| Part | Rounds | Result |
|---|---|---|
| A control core and simulator | 3 + safety fix pass | lost all 3, moderately; stopped (ADR 0004) |
| B drivers and provisioning | 2 | lost both, clearly; stopped (ADR 0003) |
| C front panel | 1 + fixes | won, moderately |
| D service, boot, watchdog, power loss | 1 + fixes | won, narrowly |
| Integration, full cure on the rig | 1 | lost, moderately; verdict NO |

Round-by-round detail is in rounds.md. Critic sheets and defect lists are in critic/.

## Blocking defects from the integration run (critic/int-defects.md)
1. A stuck-on heater is not identified, and the panel shows the commanded output state ("HEAT off")
   rather than the measured one.
2. Heating barely runs at the 24 C ferment setpoint (true air averages 20.7 C), and fermentation RH
   sits high with the dehumidifier never used. ADR 0004 recorded the 24 C cold regression, caused by
   the heat/cool no-fight rule.
3. A latched system alarm (restart loop) holds the alarm contact open for the rest of a cure, so later
   alarms make no transition.
4. Under an output-write fault the panel shows commanded outputs, and the buzzer is off during a
   critical alarm.
5. The simulator's drift and stuck sensor faults do nothing on the rig path, so the warm-hazard drift
   detection measured in part A's suite was never exercised end to end.
6. The compressor minimum-off time is enforced during TEMP_HIGH_CRITICAL.
7. Outage and crash records are inconsistent; weight-loss is meaningless after a load change; alarm
   priority lets stale events headline over live criticals; simulation marking is not unmistakable.

## What the evidence does establish (rig, no sensors or relays attached)
- Real-mode boot to first panel frame and control: about 38 s after the kernel starts.
- Service restart: 2.7 to 8 s after a kill, about 12 s after a hang (watchdog).
- Relay lines read low within 3 s of control stalling, and within about 0.3 ms of control being
  killed (guard lease, read from the board's own registers).
- Energize-to-run alarm contact opens on app death, panic or power loss.
- Blank panel after a soft reset happened often in one session (cause: firmware detection). A runtime
  overlay fallback recovered it on 2 of 2 observed occurrences. Not reproduced afterwards.

## Not established
No sensor, relay, fan, sounder or load-cell has ever been connected. All control figures come from
a simulated plant that the same team wrote. docs/wiring.md has the first-power-on checklist, and
docs/validation-first-cure.md has the first real-cabinet validation plan.

## Hardware the software cannot replace (docs/wiring.md)
Independent heater thermal cutout; an independently powered sounder on the alarm contact's
normally-closed side; the Pi 5 RTC battery; solid-state relays for the heater and humidifier
(high cycle counts); and ideally a second temperature probe, the only real fix for slow drift.

## State left behind
The raw evidence (about 325 MB) is kept in Dropbox, not in git: see docs/evidence.md. The rig runs the production service in real mode. The rig still has passwordless
sudo for john (a production install should remove it), and provisioning disabled automatic apt
updates (ADR 0204).
