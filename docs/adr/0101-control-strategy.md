# ADR 0101: Temperature and humidity control strategy

Status: accepted

## Context
One relay-driven compressor with protection timers, one heater, one humidifier.
A small cabinet: a few grams of water vapour in the air, a coil that holds water
and gives it back. The plant is not known in advance; the same binary must work on
whatever cabinet is built.

## Options
1. On/off with differential, as the reference controllers. Simple. The average sits
   half a differential off setpoint and moves with load, and a tight differential
   short-cycles the compressor.
2. Model-based predictive control. Needs a model that does not exist for a real
   build; fragile on a wrong one.
3. PI on a slow-filtered temperature gives a demand 0..1, and sigma-delta turns the
   demand into compressor runs: demand accrues as run-time credit, a run starts when
   credit pays for a minimum run, and ends when it is spent.

## Decision
Option 3. Long-run duty equals demand at any cycle length the timers allow, so the
mean goes to setpoint with no plant model. Humidity joins through the same credit:
if humidity asks for more compressor than temperature needs, temperature falls, the
PI output goes negative and the heater answers. That is reheat, with no separate
mode. Reheat is placed on the compressor run it cancels, 45 s in, which both flattens the
air and needs one relay closure per run; a fixed 60 s to 240 s window was measured first and
gave a wider band at more closures. Temperature has priority: humidity demand is cut when air is 0.7 °C under.

Humidifier: measured pulses. Pulse size comes from the RH rise the last clean pulse
produced and from a learned dead time, capped so one pulse cannot move RH more than
a few percent. A standing error makes pulses longer and more frequent.

Fan: velocity-form I loop from FS3000 to PWM with a floor above the fan's minimum.

## Consequences
Air at the probe still cycles with each compressor run, by physics of a single-speed
compressor: this is reported as measured. Product temperature cycles far less. Mean
error is small. Humidity swing within a compressor cycle remains large, and is
reported; the slow average is what the loop can control. Relay counts are recorded;
the heater and humidifier relays can see about ten thousand closures in a long cure, so
a solid-state relay suits those outputs. A dehumidify pulse in a heating regime still dips
the air by more than in a cooling regime, because reheat has little credit at that moment.

## Amendment after round 3: heating and cooling must not fight
On a small cabinet with a slow compressor, a large heater and a room near setpoint, the heater and the
compressor took turns: 9.3 to 18.0 °C at a 13 °C setpoint, against 3.5 °C for a plain thermostat on the
same plant. Now, outside dehumidify reheat, the heater runs only once the compressor has been off for its
minimum off time with no cooling demand building, and only 0.3 °C or more under setpoint; and the compressor
does not start within 5 min of the heater unless the air is 1 °C or more over setpoint.

## Second amendment, after the integration critic: the round 3 rule is removed
Status: supersedes the round 3 amendment above, and the placement of reheat on the compressor run.

The integration critic ran the test cure on the rig: at a 24 °C ferment the true air averaged 20.7 °C with the
heater on in 2 of 62 samples, RH sat at 94 % with no dehumidifying, and drying sat a steady 1.1 °C under setpoint.
The same plant offline (`TestHoldsEveryRecipeSetpoint`, before this change): ferment 21.06 °C with the heater never
on, RH 96.4 %; drying 1.0 to 1.1 °C under. Two of the rule's own conditions caused it:
- The heater ran only with the compressor "rested", which included no cooling credit left. Credit drains only while
  the compressor runs, and a leftover credit under the start threshold never runs it, so a heating-regime cabinet
  could block its own heater for good. With the air cold, "temperature first" also cut dehumidifying, so RH rose.
- In dehumidifying, the heater was barred for the compressor's 240 s minimum off time after every run, exactly while
  the coil pulls the air down. Reheat could not keep up, and "temperature first" settled the air near -0.7 °C.

Options: patch the gates (another special case on a special case); or remove the rule and find what the round 3 plant
actually needs.

Decision: remove the rule entirely: no rested-compressor gate, no 0.3 °C gap, no 5 min compressor lockout. The
heater's demand is the PI output's negative part, `clamp(-u, 0, 1)`, as in the original decision. Reheat placement on
the compressor run is also removed; reheat goes through the same heater window as all heat. With both gone, the round 3
limit-cycle plant holds 12.3 to 13.7 °C (p5 to p95) instead of swinging, so its fight came from the placement interacting
with the rule, not from the absence of a rule. The heater window is now 60 s with a 5 s minimum pulse, down from 120 s
and 10 s: a 40 s pulse of a 100 W heater lifted the air of the simulator's plant by 1.4 °C, and at 13 °C a degree is
about 6.5 %RH.

Measured on the simulator's plant over the test cure's setpoints plus an override (after 6 h settling):

| Phase | Mean air | Within ±1 °C | Mean RH | Hourly means outside ±1 °C / ±5 %RH |
|---|---|---|---|---|
| Ferment 24 °C / 90 % | 23.99 °C | 94 % of samples | 90.1 % | 0 of 42 |
| Dry early 14 / 82 | 14.00 | 99 % | 82.1 | 0 of 66 |
| Dry 13 / 76 | 13.00 | 98 % | 76.0 | 0 of 66 |
| Override 15 / 78 | 14.99 | 99 % | 78.0 | 0 of 42 |

Across the 36-plant sweep, plants with more than 10 % of the time outside ±1 °C or 20 % outside ±5 %RH fell from 31 to 17
at 24 °C and from 35 to 32 at 13 °C; 4 °C is unchanged (36, mostly defrost and humidity). No plant's mean moved away from
setpoint except where its sensor bias (±1 °C in the sweep) now shows, because the controller holds its own reading on setpoint.

## Consequences of the second amendment
- RH still swings with every compressor run: on the simulator's plant in drying, p5 to p95 is about 69 to 82 % at a
  76 % setpoint, and 20 to 32 % of instantaneous samples are outside ±5 %RH, while every hourly mean is inside. The
  coil dries the air during a run and gives water back after it; runs are already the shortest the protection timers
  allow at the duty dehumidifying needs. A smaller swing needs a separate dehumidifier (`DehumidFitted`) or a
  variable-speed compressor.
- The heater relay closes about 1,400 times a day, about 17,000 over the 12-day test cure. A solid-state relay is required
  on the heater (docs/wiring.md already asks for one).
- A frozen temperature reading is now caught after about 22 min at a 24 °C ferment, by 6 min of heater actuation with no
  change, where the old heater, which never ran there, left only the 2 h idle limit.
