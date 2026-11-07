# ADR 0104: Fail directions

Status: accepted

## Rule
Failing cold is the safe direction. Heater and humidifier must not run on bad data.
A fermented or cured product must not be warmed when temperature data is missing.

## Temperature reading lost, stuck or implausible
Options, each with a real cost:
1. All off (several reference products). Cannot freeze or cook. Below-room setpoints
   drift up to room with no response until someone hears the buzzer.
2. Fixed 15 min on, 30 min off (one reference default). Runs blind and is sized for
   cold rooms: at a warm setpoint it can freeze product; at a cold one it can starve.
3. Repeat the net cooling duty this cabinet had just before the fault, in short runs.

Decision: option 3. Net duty is compressor time minus the part spent cancelling reheat
(assumed heater/compressor ratio 0.7), averaged over 24 h at this setpoint. A heating
regime has a net duty near zero, so the cabinet drifts to room, which is the cold
direction there. With under 2 h of history: 20 % below 12 °C, 10 % otherwise. Heater,
humidifier and dehumidify are off until the reading is good.

Known weakness: no feedback for as long as it lasts. Air can end up several degrees
under setpoint, and the ratio is an assumption. The evidence records how far on each
plant. Critical alarm raised; the action text says what is happening.

## Others
- Humidity reading bad: humidifier and dehumidify off; temperature continues.
- Airflow reading bad, tach good: hold last duty that met target; heat allowed, since the
  tach shows the fan turning.
- Fan stalled, or rotor turning with no air measured: no proven airflow, so heater and
  humidifier off. A blocked duct cannot be told apart from an airflow sensor stuck at
  zero, so the cautious reading applies. Compressor limited to 15 min in each 30.
- Scale bad: weight-based phase change is paused; everything else continues. A phase
  ends by weight only on a good reading held for 2 h. Failing to advance is harmless.
- Storage bad: alarm, control continues.
- Output write error: critical alarm. Software cannot do more; a stuck relay needs the
  independent hardware cut-out, which this project does not specify (gap).
- Alarms do not change regulation (R4.8).

## Never
Nothing says product is safe. Weight loss is a yield target. Recipe end shows a request
to verify by water activity and pH with instruments, under the operator's process plan.
Degree-hours are a process record and carry an incomplete flag if data was missing.
