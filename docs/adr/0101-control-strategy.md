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
