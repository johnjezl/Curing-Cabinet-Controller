# ADR 0105: Plant model, sweep and reference emulation

Status: accepted

## Options
1. One tuned first-order plant. Fast, flatters any controller.
2. A physical multi-node model with parameters stated as estimates, varied widely.
3. Only hardware. Weeks per run, and no fault injection.

## Decision
Option 2. Nodes: air, walls, product, coil with frost and water film, sorption buffer,
vapour mass, product water. Compressor lag, heater lag, humidifier dead time. Sensors at
real quantization and sample rate with bias and noise. Parameters and reasons are in
`internal/sim/params.go`; ranges in `SweepRanges`. Water balance is a unit test.

Sweep: 12 stress corners chosen to be bad for a controller, plus a Latin hypercube over 35
parameters. Some corners are beyond what the hardware can do (a 90 W compressor in a 30 °C
room at 1.5 °C); those are kept and will not meet any bar. The controller uses one fixed
configuration across all plants.

The reference exists only as documents, so the suite also runs an emulation of its
documented defaults (`RefCtl`: neutral zones 2 °C and 5 %RH, AC 1 min or 4 min, 6 h defrost,
fixed fan, all-off or 15/30 on probe fault) on the same plants. It is an emulation from
documents, not a product, and lacks a product's unpublished refinements.

## Consequences
All numbers are MEASURED-SIM. They show behavior against this model. Film, sorption and
coil contact are the least certain parameters, and they drive the size of the RH swing.
The rig has to replace these estimates before any number is claimed for a real cabinet.
Metrics are recomputed from raw files by `scenario verify`.
