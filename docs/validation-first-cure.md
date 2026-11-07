# First-cure validation plan

Everything measured so far ran against a simulated plant. No number in `evidence/a` is
a statement about a real cabinet. This plan is how the first real cabinet replaces those
estimates. It validates the controller and cabinet. It does not validate any food process:
that stays with the operator's own plan, with water activity and pH measured by calibrated
instruments.

## Rules
- The first run uses sacrificial product or a water and salt-brine load, not food for eating.
- The independent loggers are not connected to the controller and are not read by it.
- Results are logged as they are, including the bad ones. A test that fails stops the plan
  until the cause is understood.

## Instruments, independent of the controller
| Instrument | Use | Requirement |
|---|---|---|
| 3 temperature loggers (top, middle, bottom) | Air, stratification | ±0.5 °C or better, calibrated in ice water before and after |
| 1 probe logger inside a sacrificial product piece | Product core | ±0.5 °C |
| 2 RH loggers (one beside the DHT22) | Humidity | ±2 %RH, checked over saturated salts (NaCl 75 %, MgCl2 33 %) before and after |
| Mains clamp logger on compressor and heater | Real starts, power | 1 s or better |
| Reference scale | Weigh product by hand against the HX711 | ±1 g |
| Manifold reference anemometer | FS3000 check | Spot checks |
| A second alarm receiver on the normally-closed contact | Records every edge | Timestamped |

## Stage 1: bench, no food (2 days)
1. Sensor comparison in a stable room for 12 h: DHT22 against loggers.
2. Trip each output by hand. Confirm relay polarity, and that the alarm contact opens on
   power loss, on killing the control process, and on removing the guard.
3. Disconnect each sensor in turn. Record time to alarm, and what the outputs do.
4. Compressor timers: from the clamp logger, shortest on, off and start spacing.

## Stage 2: empty cabinet (3 days)
Fit real parameters: air and wall time constants, cooling capacity from a pull-down, RH
response to one humidifier pulse, coil film rebound. Replace the estimates in
`internal/sim/params.go` with a stated "measured on cabinet X" plant and rerun the suite on it.
The least certain parameters, which most need replacing, are `FilmMaxG`, `SorbG`, `CoilContact`
and `UAAirWall`. Note whether the probe location reads what the product zone reads; the plant
has one air node and cannot predict this.

## Stage 3: loaded with sacrificial product (at least 14 days)
Run a real recipe. Pass or fail against these, from the independent loggers only:

| Check | Pass |
|---|---|
| Mean air error against setpoint, per phase | Within ±0.5 °C after sensor calibration |
| Controller reading against logger at the probe | Within ±1.5 °C at all times |
| Product core band | Recorded; compare with simulated `prod_span` |
| RH 15 min mean against setpoint | Recorded; compare with simulated values |
| Compressor starts | Under 10 per hour, always |
| Weight: HX711 against reference scale, weekly | Within 1 % of the reading |
| Frost | Inspect the coil at each door opening; photograph it |
| False alarms | Every one logged with its cause |

If simulated and measured differ by more than the sweep range covers, the plant model is
wrong and the sweep results cannot be used for that cabinet.

## Stage 4: fault tests on the loaded cabinet
Each with the second receiver recording the contact: unplug the DHT22 for 4 h; power cut of
1 min, 1 h and overnight, with and without network; open the door 2 and 30 min; stop the
compressor at its relay; hold the humidifier off; block the manifold; fill the storage; a
reference check with a deliberately wrong entry. A later fault must produce a new edge with an
earlier one still unacknowledged.

Real power cuts here also test what the simulation cannot: whether real storage loses what
the OS reported as synced.

## Stage 5: drift check
Offset the DHT22 physically where possible (a small heater beside it raises the reading; a
shielded cold pack lowers it) and hold for 48 h. Record whether the plant-response checks fire,
how long they take, and how far the logger shows the true air moved. This gives the first real
figures for ADR 0109.

## Exit
A written record of stages 1 to 5 with raw logger files, a measured plant in the sweep, and
every difference from simulation explained. Only then is any number claimed for that cabinet.
