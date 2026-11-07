# ADR 0103: Compressor protection and defrost defaults

Status: accepted

## Compressor timers
Options: reference defaults (1 to 4 min off, often no minimum on or spacing), or
longer timers (5 min off, 10 min spacing), or a middle.

Decision: minimum off 240 s, minimum on 120 s, start spacing 480 s (at most 7.5
starts/h, inside the 10 starts/h example in BENCHMARK 3.1), power-on delay 240 s.
Longer timers widened the air band measurably; shorter ones trade compressor life for
a band a reference emulation reaches only at over 20 starts/h. Nothing bypasses the
timers except a stop at an orderly shutdown. Installer settings, in Config.

## Defrost
There is no evaporator probe. Options: fixed clock; compressor-runtime-based; infer ice
from falling cooling effect.

Decision: time-based on the clock, gated by compressor runtime: no defrost until 30 min
of compressor run since the last one, so a heat-only fermentation stage has none.
Interval by setpoint: 6 h below 4 °C, 8 h to 10 °C, 12 h above. Duration 20 min, 30 min
below 4 °C. Below 4 °C the heater warms the air, within set + 4 °C, aborting at set +
5 °C, because air at setpoint cannot melt the coil. Inference of ice was rejected as a
primary method: with no coil probe it cannot be told from a failed compressor, and a
false negative lets ice build.

Humidity control is off in defrost and for 2 min of drip after; alarms suppressed for
20 min after.

## Consequences
Heater-assist assumes the heater is in the air path and the air reaches the coil. Time
gating can defrost when no ice exists, which costs a small disturbance and is measured.

## Round 2 changes
- A defrost cut short by the air limit before half its time no longer counts as done: it
  retries in about 2 h, and three in a row raise DEFROST_CUT_SHORT. There is still no way to
  measure ice, and a hot room with a weak compressor can still build it.
- The fan stops for 90 s after a defrost (R3.7), so warm wet air off the coil is not blown over
  product.
- Humidifier pulses at least 170 s apart, to bound relay closures at about 500 a day.
- Wear is measured, not modelled: `compressor_starts_per_day`, `heater_closures_per_day` and
  `humid_closures_per_day` in each cure's metrics. BENCHMARK 3.1 gives a bar for starts only (10 an
  hour, 240 a day); the loop is limited to 7.5 an hour by its start spacing. It gives no bar for
  relay closures. Rated life of the compressor and relays is not modelled.

## Amendment after round 3: defrost cut short for good
A cabinet already warm from ice cut every defrost after 1 s on the temperature limit, retried every 2 h, and
built 3 to 4.7 kg of ice while it warmed by 10 °C over 14 days, with no fault injected. Now the temperature
limit is the higher of setpoint + limit and the air at defrost start + 3 °C, and applies only after 5 min. The
absolute limit still ends a defrost at once.
Defrost heat now follows the same limit, and is exempt from the rule that stops the heater over
setpoint; that rule had been switching it off in exactly the cabinets that needed it.

Heater-assisted defrost now applies below 6 °C, so 4 °C setpoints get it, with the cold-setpoint interval
(6 h) and length (30 min).
