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

## Amendment: an injection that changes nothing is refused (integration defect 5)
On the rig the integration critic injected `climate drift -4`, `-40`, `+3` and `climate stuck`. Each
returned `{"ok":true}` and showed in the fault list, and the reading still tracked the truth within
0.5 °C for up to 36 virtual hours. The plant modelled drift and stuck only on the `temp` and `rh`
channels; the `climate` device (the DHT22 itself) modelled only dead and bad frames, and accepted
everything else silently. The part A sweep injected drift on `temp`, so its figures in ADR 0109 stand.

Options: map `climate drift` onto temperature (guesses what the operator meant, and leaves humidity out);
or model exactly what a device can do and refuse the rest.

Decision: refuse the rest. `sim.CheckFault` holds the table of what each device models; `simctl fault`
returns an error naming the right device (`climate drift` says to use `temp` or `rh`; `temp dead` says to
use `climate`), and `Plant.Inject` records nothing it does not model, so the fault list never shows an
injection with no effect. `climate stuck` is now modelled: the chip answers on time with the last frame it
delivered, on both readings. `TestDriftAndStuckChangeTheReading` and `TestNoOpInjectionsAreRefused` hold this.

Second amendment, after the re-test: the tool still accepted injections that did nothing or did the wrong thing: a drift,
offset or spike of 0 or with no value, `stuck 5` (the 5 was ignored), NaN or 1e9 as a rate, and `dehumid relay-stuck-on`
on a plant with no dehumidifier. `Plant.CheckInjection` now also checks the value against what the fault means: drift
(non-zero, at most 50 units per virtual hour), offset (non-zero, at most 100), spike (a share above 0 and at most 1),
range (a value), stuck (none; the door switch takes 0 or 1), everything else none; and refuses a dehumidifier or door
switch fault on a plant without one. Refusals name the device and fault and say what would work. The reply names what
was injected, and what it replaced (one fault per device); a repair says what it repaired, or that there was nothing.
`TestInjectionParametersMustMeanSomething` holds this.
