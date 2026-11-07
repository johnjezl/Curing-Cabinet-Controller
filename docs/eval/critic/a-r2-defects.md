# Defects, largest first

All paths are under `evidence/a/`. Temperatures are true plant state unless marked as reading. "Pre-alarm" means before the first expected alarm raised during the fault.

## Dangerous behavior

1. **A healthy sensor gets overruled and the controller wrecks the cabinet.** No sensor fault injected.
   - `worst/fault_temp_drift_down_slow_4c` (hot-room plant, 4 C set): reading matched truth until `TEMP_READING_DISAGREES` at 198001 s (2.3 days), before injection at 432000 s. The filtered reading then froze at +0.57 C, compressor duty fell from about 0.86 to 0.48, and true air sat at +8 to +14.7 C (12 to 18.7 C) and product at about +9.5 to +10.9 C for 12.7 days. Contact 91 %, so it was signalled, but the regulation change caused the excursion.
   - `sweep/metrics_long.csv`: the 5-day pre-fault `t_mean_err` at 4 C is +9.04 C for c01 and +10.94 C for s20, against +0.44 and +1.22 in the 18 to 42 h steady window.
   - `restart/base_rtc`, `restart/base_nortc`, `restart/small_rtc` `events.log`: same false alarm at 393389, 286937 and 752230 s with no sensor fault. `restart/hot_nortc`: `RH_READING_DISAGREES` at 217855 s.
   - No clear event was found for the disagree alarm in those runs. `restart/*/outages.csv` shows, for outages under 300 s after the latch, true air median 6.4 to 7.3 C below a 13 C set (as low as 9.82 C below), and medians of +2.3 to +4.8 C (max +6.8 C) on a 4 C set. `restart/*/metrics.json` reports none of this directly; it shows only `temp_on_return_recorded` 18/80, 11/80, 30/60.

2. **Fast low-reading drift at 4 C can bring product to room-plus temperature with no fault-time alarm.** `sweep/metrics_long.csv`, `fault_temp_drift_down_fast_4c`: 8 of 36 undetected. Six (c14, s06, s12, s14, s15, s16) reach air 27.2 to 29.0 C and product 23.7 to 27.7 C on a 4 C set, with `detected=0`. No raw run for these six is archived, because `worst/` holds c01 only, so I could not recompute them. Their `signal_change_s=1` is not evidence of a signal for this fault.

3. **An output-write failure is silent externally while product is ruined.** `base/fault_outputs_write_error`, `worst/fault_outputs_write_error`: contact and buzzer stayed at 0 for all 8 h at 5 s sampling, despite `OUTPUT_FAULT` critical at 31 s. Nominal: air +4.45 C, product +4.01 C. On c01: air +13.23 C, product +12.44 C. `sweep/metrics_long.csv`: air max 27.54 C and product 26.42 C (c14); air min -22.93 C and product -21.56 C (s03), product below 0 C for 23150 s. Reported `signal_change_s` is 31 (see mismatches).

4. **Slow drift takes more than a week to reach a warning, or never does.** Warm direction at about 0.5 C/day:
   - `base/fault_temp_drift_down_slow_13c`: warning at 846001 s (9.8 days), air +4.73 C, product +4.34 C; pre-alarm air peak +5.80 C. `sweep`: median alarm 770401 s; 9 of 36 undetected; worst air 23.05 C, product 18.44 C. Product crosses 15.6 C in every plant, minimum peak 15.8 C.
   - `base/fault_temp_drift_down_slow_4c`: warning at 698401 s (8.1 days), air +4.33 C. `sweep`: 13 of 36 undetected. Product 19.2 C on s20 and 16.2 C on c01, both already collapsed before injection; other undetected plants 8.0 to 9.8 C.
   - `worst/fault_temp_drift_down_slow_13c` (c03): 139.65 h air above +2 C and 121.98 h product above +2 C, no in-fault alarm until the operator's reference check at repair.
   - No critical severity in any plant for either slow run (`detect_critical`), and warning contact is only 30 min per 6 h: in-fault contact 0.2 % at 13 C and 1.7 % at 4 C.

5. **Cold direction: never tested where freezing matters.** Reading-high drift exists only at 13 C.
   - `worst/fault_temp_drift_up_fast_13c`: air -12.35 C from set (0.65 C absolute), product -10.33 C at alarm, 39601 s after start.
   - `worst/fault_temp_drift_up_slow_13c`: 153 h product below -2 C, undetected until the reference check. `sweep`: 9 of 36 undetected, air down to 4.39 C.
   - At the same rate on a 1.5 or 4 C set, that error would put air well below 0 C (INFERRED). No run shows it, and the absolute low alarm reads the same wrong sensor.

6. **Lost-reading mode freezes or warms product, and only 24 h was tested.** `sweep/metrics_long.csv`: `fault_climate_dead_1p5c` air -1.10 to 19.07 C across plants; s18 product below 0 C for 23560 s, air below 0 C for 11070 s; c01 reaches 19.07 C air, 16.51 C product. `fault_climate_dead_4c` air to 18.36 C, product 15.88 C. `fault_climate_dead_13c` air 4.46 to 20.46 C. A week-long sensor loss in an unattended cure has no direct run; `restart/` shows the open-loop state several C off set for weeks.

7. **Acknowledge silences a live critical.** `base/alarm_silence_ack`: acknowledge at 61200 s drops contact and buzzer while compressor no-effect is still active. Only a new alarm restored them, 1055 s later. If temperature is already past all thresholds, no new alarm is available to restore them (INFERRED).

8. **Warning alarms stop signalling after 30 min, repeating 30 min per 6 h.** `base/fault_temp_drift_up_slow_13c` contact edges: 612030 on, 613830 off, 633630 on. Same pattern in `fault_rh_drift_up_slow_13c`, `fault_door_switch_stuck_open`, `fault_air_dead`, `fault_storage_full`, and `worst/fault_cool_stuck_off`. Off 92 % of the time with the condition still present.

9. **Compressor dead can go unnamed.** `sweep`: `fault_cool_stuck_off` and `fault_compressor_no_cooling` 4 of 36 undetected in 8 h (c02, c13, s17, s19), with air up to 16.9 C; on the archived c02 run only RH warnings fired. Worst detected: 14980 s, air up to 31.50 C and product 29.92 C. `base`: alarm at 2508 s with pre-alarm air peak +2.61 C.

10. **RH drift goes on for days and is misdiagnosed.** `base/fault_rh_drift_down_slow_13c` alarm at 781201 s (9 days) with true RH about +9 %RH. `base/fault_rh_drift_up_slow_13c` warning at 687601 s with true RH -11.4 %RH. `sweep`: slow low 11 of 36 undetected, slow high 4 of 36 (true RH reaches -33 %RH across plants), fast low 3 of 36. `worst/fault_rh_drift_down_fast_13c` raises a temperature critical first at 39601 s: true air above +2 C for 15.08 h and product above +2 C for 13.25 h, caused by an RH sensor fault. `first_alarm_expected` median is 0 for fast low.

11. **Humidifier faults.** `worst/fault_humid_stuck_on` (c06): true RH reaches 100 % (+24 %RH) and no alarm in 8 h. `sweep` `fault_humid_empty_4c`: 15 of 36 undetected in 12 h, true RH down to -50 %RH. Mold and case-hardening risk respectively (INFERRED).

12. **Stuck compressor relay reaches freezing before or near the alarm.** `sweep` `fault_cool_stuck_on`: air min -7.80 C, product -7.31 C (s05-class), detection up to 5424 s, true air -6.52 C at worst detection. `base` alarm at 1469 s, air -2.81 C. Across all archived event logs the low-temperature alarm fired only twice, both warnings, and an absolute-low alarm never fired, so cold excursions have thin alarm evidence.

13. **Heater faults.** `sweep` `fault_heat_stuck_on`: air up to 23.82 C, 2 of 36 undetected; detection worst 13867 s; air at detection up to +7.50 C. `fault_heat_stuck_off_24c`: 6 of 36 undetected.

14. **Weak plants never recover.** `worst/steady_beef_1p5c_82rh` (s20): air mean +4.20 C, RH span 48 %RH, contact 85 %. `worst/steady_equalize_4c_85rh` (s20): defrost median 5 s, RH 15 min error 28 %RH, frost near 1 kg. `sweep` frost 1047 g. Defrost is cut short rather than completed.

15. **Outage record has large holes.** `restart/base_rtc/metrics.json` `temp_on_return_recorded` 18 of 80; `restart/base_nortc` 11 of 80; `restart/small_rtc` 30 of 60. Unrecorded rows in `outages.csv` read 0.00 C with true air up to 22.11 C; `events.log` shows "recovered=false ... peak 0.0 C". Without a clock, duration is reported in 31 of 140.

16. **Process record understates heat exposure.** Degree-hours above 15.6 C omit outage time: `cure/wholemuscle_base/metrics.json` 0.96 reported, 12.7 true air recomputed from `samples.csv.gz`, 3.7 product. `cure/ferment_small` 404.6 reported, 444.3 true. Cold and humidity have no accumulators.

17. **Long outages and warm returns are alarm-graded unevenly.** `cure/beef_base/event_journal.tsv`: 5797 s outage, 9.3 C on return at a 1.5 C set, is info, then warning 980 s later. `cure/ferment_small`: critical at 16.3 C for a different setpoint. Grading may be reasonable, but no product exposure estimate is stored.

18. **Mechanical wear.** `cure/ferment_hot/metrics.json` 18880 humidifier closures in 52 days (355 / day). `cure/ferment_loaded/metrics.json` 10536 compressor starts and 10663 heater closures in 61 days (171 / day). Starts sit on the 8/h cap across plants. A contact rated for 100000 cycles lasts under a year at that humidifier rate (INFERRED).

19. **Fan restarts on a warm coil.** `base/defrost_13c`, `base/defrost_4c`, `base/defrost_1p5c`: fan back at +89 s with coil 13.6, 6.0 and 4.4 C, before the compressor at +174, +124, +124 s, moving warm wet air into the cabinet.

20. **Calibration reminder is weak.** `cure/*`: one info-level reminder, no contact, in cures up to 72 days, though drift is the largest hazard here.

## False alarms per cure

Warning and critical raises, excluding outage and finished, from `cure/*/event_journal.tsv`:

| Cure | Days to finish | Raises | Per 30 days | Contact on-time, recomputed |
|---|---|---|---|---|
| beef_base | 28.1 | RH_LOW 2 | 2.1 | 0.149 % |
| biltong_base | 5.1 | RH_HIGH 4, RH_NOT_FALLING 2 | 35.0 | 10.417 % |
| ferment_base | 61.1 | none | 0 | 0.068 % |
| ferment_hot | 52.2 | RH_LOW 3, RH_DRIFT_SUSPECT 5 | 4.6 | 3.358 % |
| ferment_loaded | 60.7 | RH_LOW 1 | 0.5 | 0.098 % |
| ferment_small | 66.6 | RH_LOW 2 | 0.9 | 0.126 % |
| wholemuscle_base | 71.1 | none | 0 | 0.059 % |

- `cure/ferment_hot`: `RH_DRIFT_SUSPECT` is false; reading minus true RH is -0.47 %RH around the first raise, and it took 42 h of contact.
- `cure/biltong_base` RH alarms are genuine: true RH runs +10.6 %RH mean and +26.8 at p99, because the plant has no way to reach 55 %RH.
- Contact on-time is flattered by the 30-min-per-6-h pattern in defect 8.
- The far larger false positive is defect 1, which appears in no cure because cures ran on only 7 configurations.

## Reported metric versus recomputation

| File | Metric | Reported | Recomputed | Note |
|---|---|---|---|---|
| `base/fault_outputs_write_error/metrics.json`, `worst/` same | `signal_change_s` | 31 | No change during fault; first edge 28805 s, after repair | Metric definition says contact change during fault. Also 3 to 31 s across plants, most with no raw run |
| `sweep/metrics_long.csv` door, defrost, warmload, room-step | `rec_*_band*_s` | Numbers such as 28675 s, 85042 s | 246 of 2057 values have a 1 h hold running past run end, so null by the stated definition (13 T2, 82 T1, 38 RH8, 108 RH5, 5 product) | Never-settled shows as a number, understating p95 and worst |
| `base/door_*` | `rec_t_band2_s`, `rec_rh_band8_s` | 0 to 30 s | Floor 175 s and 295 s with a full window | Partial windows; "0" means the band was not left |
| `sweep/metrics_long.csv` `fault_door_switch_dead` | `detected`, `detect_s` | 1, 31 s | No warning or critical, no contact or buzzer; info only | Base and worst |
| `worst/steady_beef_1p5c_82rh`, `worst/steady_equalize_4c_85rh` | `t_span_xd`, `rh_span_xd` | 4.453, 4.964, 48.94, 50.66 | 4.272, 4.814, 48.38, 48.45 | Up to 4 %. Only on heavy defrost-abort runs; I suspect my pairing of cut-short defrosts, unresolved |
| `cure/*/metrics.json` | `phase0_*` | Beef RH p1 -14.41, ferment_base -9.65 | -15.6, -13.43 | Beef `phase0_days` 29.07 includes the hold after finish; ferment difference unresolved |
| `cure/ferment_small`, `ferment_loaded`, `ferment_hot`, `wholemuscle_base` | `relay_closures_to_finish` | 4, 3, 84, 2 | 0, 2, 80, 1 | Mine counts powered-to-powered edges only; likely edges across outages |
| `compare/bars_moved.csv` | `change` | R2.3 `defrost_4c` "better"; R2.1 `door_2min_4c` "worse" | Base 4 to 234 s; base 105 to 95, worst 2795 to 2465 s | Label does not track the values |

Matched within rounding: `detect_s`, `detect_t_err`, `detect_prod_err`, fault extremes on all archived base and worst runs apart from the above; nominal steady spans; contact and buzzer on-time to finish on all 7 cures; compressor protection figures; degree-hours against the log.

## Plant and evidence suspicion

- **Steady bars cannot see defect 1.** `base/steady_*/spec.json` measure hours 18 to 42 and the run ends at 151200 s. The distrust collapse starts at 2.3 days or later.
- **Multi-week cures use 7 configurations only** (`cure/`). None uses c01 or s20.
- **Door is gentle.** `plants.csv` `DoorExchangeTauS` 25 s (15 to 50). A 0.35 m3 cabinet with a full door opening likely exchanges air in seconds by buoyancy (INFERRED). Air is further pinned by `UAAirWall` 30 W/K. Result: `base/door_60min_13c` peaks only +2.92 C after an hour open in a 21 C room.
- **Compressor reacts fast.** `CompTauS` 60 s (30 to 120), `CompStopTauS` 20 s and not swept. Real capillary systems take minutes (INFERRED). This favors tight control at 120 s minimum on.
- **Product is fast.** `UAProdPerKg` 0.8 gives about a 1 h time constant at nominal. `base/warmload_13c_10kg_20c` settles in 3110 s against a 12 h bar, which is not a credible margin for whole muscle (INFERRED).
- **No stratification, one air node.** "True air" is a single number, so cabinet-level swing is not measured at all.
- **No evaporator floor.** `sweep` s03 reaches -22.93 C air in a fridge-class box, which looks unphysical in the pessimistic direction.
- **Not swept** (`sweep_ranges.csv` against `plants.csv`): `CompCoilSlope`, `CompAmbSlope`, `CompStopTauS`, `FrostRefG`, `DrainTauS`, `HeatTauS`, `DehumidGPerS` (0 everywhere), `SorbTauS`, `HmAt1`, fan limits. No compressor start failure, no sensor hysteresis at high humidity.
- **Provenance.** `manifest.txt` shows the crash step was rerun alone with `source_sha256_after` bb50f35d, differing from main-run 4121fd10. "Controller source unchanged" is an unverified claim. `git_head` 05f449e predates the code, which is uncommitted, so the head identifies nothing. `crash/` is process kills and `cutmatrix/` is simulated storage; neither is real power loss.
- **Bars moved.** `compare/bars_moved.csv` adds 27 new bars, and `compare/reference_bar.csv` still fails 59 of 137 at worst plant and 52 at p95. 132 of 137 passing at the nominal plant is not the number to read.
