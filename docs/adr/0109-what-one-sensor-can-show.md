# ADR 0109: What one sensor chip can show about itself

Status: accepted. Replaces the drift rules in ADR 0106.

## Context
Temperature and humidity come from one DHT22. There is no second reading to compare with.
A sensor drifting low is the dangerous case: the controller stops cooling, and may heat,
while the display looks normal and product warms. Round 1 tested only drift reading high, and
counted alarms after repair as detection.

## Options
1. Rely on periodic operator checks only. Honest, but nothing between checks.
2. Effort shift alone, as in round 1. Cannot tell a sensor from a change of room, and it
   raised false warnings in 4 of 7 cures.
3. Plant-response cross-checks using signals the sensor does not control, plus an operator
   reference check as the only thing that can settle the question.
4. A second independent probe. The real answer, but not in the parts list.

## Decision
Option 3, with 4 recommended for any real build.

Signals, hourly, kept across restarts:
- Net compressor duty, minus the part cancelling reheat. Reading low means less cooling and then
  heat; reading high means more.
- Drying rate from the scale. It follows the true product temperature and true humidity. A change of
  room moves duty and not drying, since the air stays where it was, and that is what separates them.
- Humidifier and dehumidify effort, for the humidity channel.

Windows: the last 24 h against up to 72 h before; the last 6 h against everything before, and against
the same 6 h yesterday, which cancels the room's daily cycle; and, for slow drift, effort against the
first days since the last check, and drying against 4 days ago, since fresh product only ever dries more
slowly.

Two signals agreeing, or a change too large for a room (about 8 °C in a day), or a heater newly working in
a cabinet that was not using one, raise a critical alarm and latch the channel as suspect. Temperature
then falls back to the blind hold of ADR 0111, so a low reading cannot go on heating the product. Only
`reference_check` or a calibration clears the latch. One signal alone raises a warning after 12 h, and
changes nothing.

## Measured against the simulated sweep (36 plants, `sweep/summary.csv`)
| Injected, 13 °C unless noted | Detected during fault | Critical | Median time | True air error at detection, median (worst) |
|---|---|---|---|---|
| Reads low, 1 °C/h | 36 | 36 | 6 h | +5.3 °C (+13.9) |
| Reads low, 1 °C/h, at 4 °C | 28 | 28 | 5 h | +5.7 °C (+10.5) |
| Reads low, 0.1 °C/h | 36 | 36 | 58 h | +5.3 °C (+9.3) |
| Reads low, 0.02 °C/h, 10 days | 27 | 0 | 9 days | +4.0 °C (+6.0) |
| Reads low, 0.02 °C/h at 4 °C, 10 days | 23 | 0 | 8 days | +3.5 °C (+5.0) |
| Reads high, 1 °C/h | 36 | 36 | 6 h | -6.5 °C (-10.5) |
| Reads high, 0.02 °C/h, 10 days | 27 | 3 | 7 days | -3.2 °C (-5.2) |
| RH reads low, 0.5 %/h | 33 | 14 | 25 h | n/a |
| RH reads low, 0.05 %/h | 25 | 8 | 6 days | n/a |
| RH reads high, 0.05 %/h | 32 | 0 | 7 days | n/a |

## What remains undetectable
- Any constant offset present since the last check. Control works perfectly around a wrong number.
- Temperature drift under about 0.02 °C/h: 9 to 13 of 36 plants saw nothing within 10 days, and those that
  did were 3 to 6 °C out by then. At that rate a 1.5 °C error takes 3 days.
- No product on the scale, or a failed scale: the drying signal is gone, and only large effort changes remain.
- A real room change the same size and direction as the drift, with an empty cabinet.
- All of the above figures are against a simulated plant. Real drying rates and duty noise could make these
  checks better or worse, which `docs/validation-first-cure.md` stage 5 measures.

## The periodic check
These limits set the reminder. With a cure running a reference check is due every 7 days of power, which at
the slowest rate above is bounded to about 3.4 °C and is the only defence below it. Idle, every 30 days. It
is an info reminder: it never trips the contact. Lifecycle: raised when due; acknowledging keeps it listed; a
passed `reference_check` or a calibration resets it; ignored for another period it asks again. A check records
both readings in the journal, and a difference over 1.5 °C or 5 %RH raises a critical alarm and puts the reading
in doubt.

## Consequences
A false critical puts the cabinet on the blind hold until someone checks. The 8 °C-in-a-day line is the balance:
a +6 °C room step raised no such alarm in the sweep's median, but a sharper one could. A reading in doubt
is one case where an alarm changes regulation, which R4.8 does not do, on purpose: here the reading itself is
what is in question.
