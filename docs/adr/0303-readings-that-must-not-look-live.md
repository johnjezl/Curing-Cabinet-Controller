# ADR 0303: Readings that must not look live

Status: accepted

## Options
1. Keep the digit and change its colour. Fails for colour blindness and at a distance.
2. Keep the digit and add a small icon. A distant reader sees a number.
3. Remove the big digit. The state word (STALE, FAULT, SUSPECT) becomes the largest
   thing in the tile, on a hatched background, with any last value in small type.

## Decision
Option 3. A value only gets big digits if its quality is ok, it is finite, and the
controller is responding. If the snapshot stops advancing for 5 s every reading
turns STALE, outputs turn to "OUTPUTS UNKNOWN", and the banner says the controller
is not responding. Weight loss is labelled not current when the scale is not good.

A simulated plant gets a striped frame around every screen, a pill in the banner,
and a SIM tag in every reading tile.

A stalled controller is handled on every screen, not only Home, because the screens
people open to diagnose a fault are the worst place to be told a reading is fresh.
Health, Set, Cure, Alarms and Trend say NOT LIVE, values the controller reports about
itself become "unknown", and controls that need the controller are removed. A test
covers every stalled screen. Commands other than silence are refused while stalled.

## Consequences
The last value is harder to read from afar during a fault, which is intended.
Non-finite values marked ok by the core are treated as a fault.
