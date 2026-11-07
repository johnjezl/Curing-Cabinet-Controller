# ADR 0111: Holding a cabinet with no temperature reading

Status: accepted. Replaces the fixed choice in ADR 0104.

## Context
Round 1 repeated the learned net compressor duty. At 4 °C that took air below 0 °C on 26
of 36 plants within 24 h, and in a hot room warm by 14 °C. With 150 W of cooling against
about 2.2 W/K of leak, 1 % of duty error is about 0.7 °C at equilibrium. No open-loop duty
can be exact. There is no second temperature signal in the hardware.

## Options
1. All off. No freezing possible; warms to room with a time constant of hours.
2. Fixed duty 15/30. Worst of both across plants (measured in `compare/`).
3. Learned net duty, unscaled (round 1).
4. Learned net duty scaled down, in short runs.

## Decision
Option 4, with scale 0.8. The learned figure contains the latent load of humidifying and of
condensing, and most of that stops when humidity control stops, so the plain figure
overcools. Runs are as short as protection allows. Heater and humidifier stay off. Measured
bounds per setpoint, across the sweep, are in `compare/core_vs_reference_emulation.csv`
(`fault_t_min_abs`, `fault_t_max_abs`, `fault_prod_*_abs`, `fault_*_below_0c_s`).

## What this does not solve
It is a bound on a 24 h window with no feedback, not control. A plant whose room changes
during the fault drifts with it. The critical alarm is what actually protects the product.
A second independent probe would solve this and is the top hardware recommendation.

## Near freezing
At setpoints under 6 °C there is little room on the cold side, so the scale falls further,
linearly to 0.6 at 2 °C and below. The price is warmer air in the window; that is a choice
of which way to err when both ways do harm, and at 1.5 °C freezing the batch is the harm
that cannot be undone within a day.
