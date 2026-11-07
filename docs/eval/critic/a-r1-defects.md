# Defects, round 1 (largest first)

Paths are under evidence/a/. Set means setpoint. All of it is simulated plant data.

## Food-safety and batch-loss defects

1. **Temperature sensor drift silently freezes or chills the batch, and the scorecard calls it a pass.**
   - `base/fault_temp_drift_fast`: reading drifts about 1 C/h for 12 h. True air fell from 13 C to 0.6 C (-12.38 C) and product to 2.3 C (-10.7 C). No alarm of any kind fired between 50400 and 93600 s. The first alarm comes 208 s after repair.
   - `sweep/metrics_long.csv`: during the fault, 1 of 36 plants detected it (c01, at 43122 s of 43200 s). Air went below 0 C on 14 plants, worst -0.8 C. Worst air error -13.81 C, product -11.89 C.
   - `compare/reference_bar.csv`, R4.10 fault_temp_drift_fast, shows base=1, p95=1, pass_base=true, pass_p95=true. See mismatch M1.

2. **Slow temperature drift is missed outright on 14 of 36 plants.**
   - `worst/fault_temp_drift_slow` (c01): no alarm over 10 days while air reached -6.32 C and product -5.13 C.
   - `sweep/metrics_long.csv`: 22 of 36 detected, median 615260 s (7.1 days), worst 792149 s. Air reached -7.44 C.
   - `base/fault_temp_drift_slow`: air passed -3 C at 3.5 days, TEMP_DRIFT_SUSPECT came at 7.1 days, and the first drift alarm named the wrong sensor (RH_DRIFT_SUSPECT at 259208 s).

3. **The dangerous drift direction is not tested at all.**
   - Every temperature drift run reads high and pushes the cabinet cold. A reading that drifts low would hide warm product. Both temperature drift scenarios show max air error of at most +3.5 C across plants, consistent with this.
   - RH drift is one direction too (`fault_rh_drift_slow`, reading high, true RH -35.3 %RH). Reading low means wet product and mould.
   - Files: `base/fault_temp_drift_*`, `base/fault_rh_drift_slow`, `scenarios.txt`.

4. **Open-loop cooling on sensor loss freezes or warms product for as long as the sensor stays dead.**
   - `sweep/metrics_long.csv`, fault_climate_dead_4c, 24 h: air below 0 C on 26 of 36 plants, product below 0 C on 24 of 36, worst air -4.4 C absolute and product -3.9 C absolute. In the other direction, air reached 18.3 C and product 15.9 C absolute (c01).
   - Same scenario on the nominal plant, `base/fault_climate_dead_4c`: air -1.06 C and product -0.7 C at set 4 C.
   - fault_climate_dead_13c worst -11.0 C / +5.6 C. fault_climate_dead_24c worst -10.3 C.
   - The alarm is prompt (19 s), but a cabinet unattended for weeks needs a safe hold, and this one is not. 1.5 C was not run at all, and that is where this mode should bite hardest.

5. **Stuck outputs have no independent limit in evidence.**
   - `sweep/metrics_long.csv`, fault_outputs_write_error, 8 h: s12 air +48.67 C (61.7 C absolute), product 55.6 C. s16 air -30.3 C (-17.3 C absolute), product -16.3 C.
   - The alarm fires in at most 31 s and nothing limits the state. Whether a hardware cutout exists is not shown.
   - fault_cool_stuck_on: c03 air -4.6 C absolute and product -4.0 C absolute; RH worst across plants -52.3 %RH.

6. **Cooling failure in a warm room ruins product before the correct alarm matters, and the first alarm blames the wrong part.**
   - `sweep/metrics_long.csv`, fault_cool_stuck_off: air max worst +13.6 C (s23, 26.6 C absolute), product 25.9 C. Detection median 4683 s, worst 21581 s, and none on c02 and s16 (+3.4 C for s16).
   - `base/fault_cool_stuck_off`: first alarm at 4842 s is HEAT_STUCK_ON, critical, with the heater not at fault. The correct COOL_NO_EFFECT comes at 6840 s and TEMP_HIGH only at 11743 s.
   - TEMP_HIGH has warning severity, and fires after 4680 s above +2 C.

7. **The alarm output is on nearly all the time, so it cannot signal anything new.**
   - Recomputed from `cure/*/samples.csv.gz`, up to recipe finish: relay and buzzer on 81.5% (beef), 94.1% (biltong), 85.5% (ferment_base), 83.7% (ferment_hot), 90.8% (ferment_loaded), 90.3% (ferment_small), 88.6% (wholemuscle).
   - Main cause: POWER_OUTAGE stays active for hours to weeks with no acknowledge in the run (`cure/beef_base`, 549 h; `cure/wholemuscle_base`, 1514 h). A 236 s outage held it for 4.7 days (`cure/beef_base`, 448196 to 855209 s).
   - A remote relay already closed gives no notice of a freezing fault later. Cure runs do not include an operator, so a run with prompt acknowledgement is not shown.

8. **False alarms per cure, no fault injected.** Recomputed from `cure/*/events.log`, up to finish, excluding POWER_OUTAGE and CURE_FINISHED:

   | Run | False or nuisance raises | Detail |
   |---|---|---|
   | beef_base | 1 | RH_LOW 0.2 h |
   | biltong_base | 15 | RH_HIGH 4, RH_NOT_FALLING 11; RH was truly off |
   | ferment_base | 6 | TEMP_DRIFT 64.7 h, RH_DRIFT 195.3 h |
   | ferment_hot | 6 | TEMP_DRIFT 213.4 h |
   | ferment_loaded | 8 | RH_DRIFT 478.2 h, TEMP_DRIFT 31.9 h |
   | ferment_small | 1 | RH_LOW 0.1 h |
   | wholemuscle_base | 4 | RH_DRIFT 345.1 h |

   The drift detector cried wolf with sensors accurate (mean error about 0) in 4 of 7 cures before finish, and missed real drift in defects 1 and 2. Treat it as uninformative.

9. **Nominal plant, primary setpoint, false alarm on every start.**
   - `base/steady_cure_13c_76rh`: RH_NOT_FALLING at 15426 s, buzzer and relay on for 7.7 h, true RH mean -0.44 %RH during it.
   - It clears at 43201 s, the exact defrost start, not from any RH change. The same pattern repeats in `cure/biltong_base`.
   - Other false criticals: COOL_STUCK_ON after sensor repair in `base/fault_temp_stuck` (2) and `base/fault_temp_drift_fast` (5), and COOL_STUCK_ON in `worst/fault_heat_stuck_off_24c` at 20997 s.

10. **Biltong setpoint is not achievable, and the recipe runs to finish anyway.**
    - `cure/biltong_base`, set 25 C / 55 %RH: air -1.0 C, product -1.95 C, RH mean +10.6, p99 +26.6, max +36.9 %RH, for 5 days, with RH_NOT_FALLING raised 11 times.
    - `sweep/metrics_long.csv`: RH mean median +10.4, worst +27.0 %RH. Heater saturated at 96%.
    - It finished on time with 16.9% loss.

11. **Silent humidifier failure at a cold setpoint.**
    - `sweep/metrics_long.csv`, fault_humid_empty_4c: detected during the fault on 19 of 36; 16 never, plus c01 only after repair (60454 s). True RH down to -37.7 %RH, and a 12 h fault can sit at -14.7 %RH with no alarm at all (`worst/fault_humid_empty_4c`, c04).
    - Case hardening risk. Runs are only 12 h, so a longer dry-out is not shown.

12. **Heater failures also go unflagged.**
    - fault_heat_stuck_off_24c: 5 of 36 raise none of the expected alarms, worst air -10.3 C, on a ferment setpoint where low temperature stalls fermentation.
    - fault_heat_stuck_on: s14 raises no temperature or heater alarm at +4.11 C (`worst/fault_heat_stuck_on`; RH_LOW only). s02 reached +9.33 C.

13. **RH swing is large and worse than the simple comparison controller.**
    - `compare/core_vs_reference_emulation.csv`, steady_cure_13c_76rh: rh_span_xd core 18.9 %RH, ref1 9.83, ref4 11.9, with core better on only 4 of 36 plants. t_span_xd 1.62 C vs 1.34 / 1.45, better on 12 and 16 of 36.
    - Energy 1445 Wh/day vs 1102 / 998.
    - It does beat them on mean RH (better on 33 and 34 of 36) and start count.
    - RH span worst 40.6 %RH at 1.5 C (`sweep/metrics_long.csv`, s01). Sustained surface wet/dry cycling is a batch risk the mean hides.

14. **Temperature bands fail on spread plants.** `compare/reference_bar.csv`: R1.2 fails p95 and worst at all 7 setpoints. R1.5 worst 5.55 C at 13 C, 8.50 C at 20 C, 9.41 C at 25 C. Mean error at 1.5 C is +3.39 C on c01, which means beef held at about 4.9 C, and c01 at 4 C runs +1.03 C with RH -15.8 %RH. This plant lacks capacity at cold setpoints in a 30 C room.

15. **Frost runs away.**
    - `sweep/metrics_long.csv`: frost_max_g worst 1079 g (s08, defrost_4c), 987 g at 1.5 C.
    - `worst/defrost_4c` (c01): defrost ended on its air temperature limit after 1 to 614 s, and frost reached 592 g with frost_end 592 g, still at its peak at the end of the run. No frost alarm code appears in any run.
    - The plant's frost reference is 150 g, so this is 4 to 7 times beyond it, with no evidence of the plant being credible up there.

16. **Actuator wear looks high for a months-long unattended run.**
    - `cure/ferment_base`: 9622 compressor starts in 61 days (about 158/day), median 7.27 starts/h at the 8/h limit, 120 s minimum on.
    - `cure/ferment_hot`: 26573 humidifier closures. `cure/beef_base`: 13611 in 28 days. Steady-run maximum 656 humidifier closures per day and 675 heater closures per day. Relay and compressor life are not modelled.

17. **Outage duration lost without a trusted clock.** `restart/base_nortc/outages.csv`: reported_outage_s=0 in 73 of 80, including outages up to 83941 s. `restart/hot_nortc`: 52 of 60. Air on return reached 20.99 C and 29.9 C absolute. The alarm stays at warning severity with no product exposure figure. A cut without a clock cannot say how long product sat warm.

18. **Door and door-switch gaps.** No door-open run over 10 min. Regulation stops while open, so a door left open or a switch stuck reading open is unbounded and not shown. `worst/door_10min_13c` (s14, no switch): no door alarm at all. 8 of 36 plants have no door alarm path. `base/fault_door_switch_dead`: only info severity.

19. **No fan delay after defrost.** `base/defrost_*`: fan 25% during defrost, 11 to 14.6% after. Warm wet coil air is blown straight onto product, and RH then takes 839 to 1404 s to stay inside +/-8 %RH on the nominal plant. On c01 it did not settle in the 3600 s checked at 4 C or 1.5 C.

## Plant credibility (results may be too good)

20. **The plant cannot show the failures that matter most in this cabinet.**
    - `plants.csv`, `sweep_ranges.csv`: one air node, so no stratification, and "cabinet true air" is effectively the probe location.
    - Product is one lump with UAProdPerKg 0.8 W/K/kg and ProductCJKgK 3000, neither swept, giving a 1 h time constant. A dense whole muscle piece would be several hours, and product recovery times in R2.6 are likely optimistic by that factor.
    - CompInputW is fixed at 65 W while CompW sweeps 90 to 200 W, so COP spans 1.4 to 3.1 and energy figures are not comparable across plants.
    - No condenser, gasket ageing, or compressor start failure model. Ambient 14 to 30 C only, so no unheated garage or hot summer.
    - RH bias at most 3 %RH and T bias at most 0.5 C: no aging sensor at high humidity except the one drift scenario.
    - Scale noise 15 to 120 counts at 105 counts/g (at most 1.1 g) and creep at most 2 g/day, with no fan or compressor vibration. Weight-based recipe ends rest on this.
    - The figures themselves look physically reasonable: 0.35 m3, 2.2 W/K, 130 W, 0.15 ACH, about 0.6 to 1.5%/day loss.

21. **Reproducibility and durability evidence is narrower than the headlines.** `cutmatrix/cuts.csv`: 400 of 400 clean, but seed count is 1 and it is all a simulated storage layer. `crash/kills.csv`: 30 kills in runs of 225 to 1599 ms. No real storage or real relay is involved anywhere: `manifest.txt` evidence_basis says no hardware.

## Reported vs recomputed

Recomputed from raw samples and events on all 154 base and worst runs. Means, percentile spans, duties, starts, alarm counts, detect_s and fault extremes matched to within rounding. `sweep/summary.csv` medians match `sweep/metrics_long.csv` on all 3357 core rows. `restart/*/metrics.json`, `crash/metrics.json` and `cutmatrix/metrics.json` match their CSVs.

- **M1. `detected` counts alarms after the fault is repaired.** fault_temp_drift_fast reports detected on 35 of 36; during the fault it is 1 of 36. Nominal detect_s=44152 s against a 43200 s fault. `compare/reference_bar.csv` passes this at base and p95. fault_humid_empty_4c has the same issue on c01 (60454 s vs 43200 s).
- **M2. rhavg15_max_abs_err and rhavg15_max_err_xd use partial averaging windows.** The largest value is often the first sample of the window, not a 15 min mean, which contradicts `metric_definitions.tsv`. Reported vs full-window: `base/steady_cure_13c_76rh` xd 7.92 vs 3.27; `base/steady_early_14c_82rh` xd 6.65 vs 1.90; `base/fault_temp_stuck` abs 11.69 vs 6.14; `worst/steady_beef_1p5c_82rh` abs 21.91 vs 12.30. This makes R1.4 look worse than it is on some rows, so its pass and fail flags are unreliable both ways. Sweep values cannot be corrected without raw data.
- **M3. rec_* is first entry into the band, not settled recovery, and the door/defrost zeros mislead.** Trailing means at D1 include pre-event samples. Reported vs stays-in-band for an hour: `worst/door_30s_13c` T 140 s vs 2945 s; `base/warmload_4c_10kg_12c` RH8 0 s vs 3420 s; `base/defrost_4c` RH8 0 s vs 1404 s; `base/door_2min_4c` RH8 0 s vs 625 s.
- **M4. Defrost recovery clock starts at a fixed disturb_to_s, not the real end.** Defrost ends 599 s earlier on the nominal plant at 13 C and 4 C, and up to 1394 s earlier on c01. R2.3 and R2.8 "within 20 min of defrost end" in `compare/reference_bar.csv` therefore get a free 10 to 23 min.
- **M5. Bar shows absolute values for signed means without saying so.** `compare/reference_bar.csv`: t_mean_err_xd for biltong base_plant 0.941 is really -0.941, and median 0.854 is -0.854. At other setpoints, sweep medians shown as about 0.16 are medians of absolute values; signed medians are near 0.01.
- **M6. Small differences, not material.** compressor_starts_total differs by up to 4 (`cure/*`, 9625 from events vs 9622). compressor_run_h differs by up to about 2% (sampled at 120 to 300 s). `worst/fault_climate_dead_4c` t_span_xd 2.58 reported vs 2.345 recomputed; not resolved. Two bar rows have pass_worst=false with a passing worst value because one plant is null (conservative).
