# Defects: cabinet control logic, round 3 evidence (evidence/a)

The defects are ranked by harm to a batch. A warm excursion is a food-safety hazard and ranks above a cold one, which is a quality loss. All paths are under evidence/a/. "Air" and "product" mean the true simulated state, not the reading. I recomputed from raw samples and events unless a line says "sweep", meaning sweep/metrics_long.csv.

## A. Warm excursions (food safety)

1. **The controller heats the cabinet to 37-42 C when the probe reads low, and keeps heating after its own alarm.**
   - The fault is a temperature reading drifting low at about 1 C/h for 24 h, at a 13 C setpoint.
   - On the nominal plant (base/fault_temp_drift_down_fast_13c), a critical TEMP_READING_DISAGREES ("reading may be low") is raised at 450 001 s, 5 h in, with air at 18.0 C.
   - After that alarm the heater duty rises from 0.50 to 0.97 and air climbs about 1 C/h. It reaches 35.95 C at 142 h (mean of that 2 h window), with product at 34.2 C.
   - Across 36 plants the peak true air during the fault is 37.6 C median and 42.3 C worst. Peak product is 34.7 C median and 35.9 C worst (sweep).
   - Three plants never get an expected alarm during the fault:
     - kept/fault_temp_drift_down_fast_13c__s03: air peak 42.297 C, product 33.839 C, detected 0.
     - …__s10: air 38.794 C, product 34.822 C, detected 0.
     - …__s20: air 36.756 C, product 31.845 C, detected 0.
   - On those plants the contact only fired, 3.4-14.6 h in, for unrelated codes such as COOL_STUCK_ON and AIR_SUSPECT.
   - At 4 C the median peak air is 29.0 C and product 26.7 C. At 1.5 C the median peak product is 24.1 C, and 4/36 plants are undetected (kept/fault_temp_drift_down_fast_1p5c__s07: product 23.66 C, never detected).
   - Slow drift at 13 C: 10/36 undetected, median detection 8.6 days, with product +3.7 C at detection.
   - No independent backstop exists. TEMP_ABS_HIGH (30 C) is judged on the same drifting probe, so it fires only after an operator reference check.
   - Unit test TestSuspicionNeverChangesRegulation makes this behaviour intended.

2. **Fault-free cabinets warm up for weeks because frost is never cleared (defrost is ended after 1 s).**
   - In worst/faultfree_14d_1p5c (plant s16, room 31.6 C), 112 of 126 defrosts log "end: temperature limit" 1 s after starting and are retried every 7 201 s.
   - Coil frost grows to 3 665 g. Daily mean air rises from 1.9 C to 19.6 C.
   - Over 14 days, ff_t_mean_err is +10.21 C. Product exceeds setpoint +5 C (above 6.5 C) for 66.8 % of the run, with a maximum of 25.05 C.
   - The same happens on s16 at 4 C (worst/faultfree_14d_4c): mean +10.17 C, product above 9 C for 72.9 % of the time, maximum 25.8 C.
   - kept/faultfree_14d_1p5c__s07: mean +4.82 C, product maximum 15.2 C.
   - Sweep frost_end above 2.9 kg on c04, s05, s07, s16, s17 and s19 at 1.5 C, and on c01 and s16 at 4 C.
   - All of these runs had no injected fault. The contact was signalling 99.7 % of the time, but mostly with cycling COOL_NO_EFFECT plus a false critical TEMP_READING_DISAGREES that blames the sensor. That sends the operator the wrong way.

3. **Losing the climate sensor lets the cabinet run warm for days.**
   - The alarm itself is immediate: critical at 19 s on 36/36 plants.
   - Sweep fault_climate_dead_7d_13c on s01: fault_t_mean_err +11.96 C for 7 days, product +12.53 C (about 25.5 C).
   - fault_climate_dead_7d_1p5c: s12 mean +11.0 C, product maximum +12.96 C. c01 mean +13.3 C.
   - Medians at 1.5 C: air peak +6.75 C, product peak +4.86 C.
   - fault_climate_dead_13c (24 h) on s01: mean +8.0 C, peak +11.8 C.
   - The duty pattern used while blind is not adjusted for plant or season.

4. **A heater stuck on is caught late on some plants.**
   - Detected on 35/36 plants, at median 1 612 s but worst 18 856 s (5.2 h). True air at detection is +2.8 C median and +7.7 C worst.
   - Worst-case air reaches 30.8 C and product 27.3 C (compare/detection_vs_false_alarms.csv, sweep).
   - Compressor failure, or the cooling output stuck off, is detected on 34/36 plants at median 2 886 s and worst 7 044 s. Air at detection is up to +4.8 C. Air reaches 27.5 C and product 26.4 C on the worst plant during the 5 h fault.

5. **The buzzer is silent for the whole of an output write fault.**
   - In base/fault_outputs_write_error, the critical OUTPUT_FAULT is raised at +6 s, followed by FAN_STALL, COOL_NO_EFFECT and TEMP_HIGH_CRITICAL.
   - The buzzer stays at 0 for the entire 28 800 s fault and sounds only after repair (+28 820 s). Sweep buzzer_on_pct is about 1.6 % on every plant.
   - The contact does open at 18 s.
   - Air reaches +4.4 C on the nominal plant and 27.3 C on the worst (fault_air_max_abs).

6. **Unacknowledged critical alarms stop signalling after 30 min.**
   - base/guard_lease_drop: critical OUTPUTS_DROPPED. The contact and buzzer run from +195 s to +1 995 s and then go quiet. The alarm is never cleared (clear_s null) or acknowledged.
   - base/faultfree_14d_13c: after a 6 h outage the cabinet returns at 19.1 C with a 13 C setpoint, and a critical POWER_OUTAGE is raised at 989 289 s. The contact signals only from 989 310 s to 991 080 s (1 800 s), and the alarm is never cleared.
   - An owner who is away for more than 30 min learns nothing about a 6 h warm event from the contact.

7. **The alarm contact reads "not signalling" during power loss.**
   - All 860 unpowered samples in base/faultfree_14d_13c have alarm_contact_open = 0, and the same holds in every outage.
   - The design claims the contact is energize-to-run (test name TestContactQuietInNormalRunAndOpensOnPowerLoss). The evidence does not show the contact opening on power loss.
   - Either the plant logs it wrongly or the contact is not fail-safe. Power loss is the most likely unattended failure, so this has to be shown.

8. **A new fault arriving while the contact is already signalling produces only a 15 s release.**
   - On plants that can't hold setpoint, the contact signals for days (sweep ff_contact_open_pct up to 99.9 %; relay_on_pct 91-97 % in the c01 and s16 drift runs).
   - A later, more dangerous fault shows up only as a 15 s release of the contact (for example kept/fault_temp_drift_up_fast_4c__s16 events: alarm_signal off at 496 928 s, on at 496 943 s).
   - Any external monitor that polls slower than 15 s misses it. The 30 s samples in long runs miss it as well.

## B. Cold excursions (quality loss)

9. **When the probe reads high, the cabinet freezes the product.**
   - Sweep fault_temp_drift_up_fast_13c: median air minimum -11.3 C, product -9.7 C. Air is below 0 C for 39 090 s (median); detection takes a median of 6 h with air already at -6.0 C. kept/…__s03, s10 and s20 are never detected, with product reaching -7.0 to -9.3 C.
   - At 1.5 C: air down to -20.5 C, product -19.2 C. At 4 C: air -19.8 C.
   - Slow drift at 1.5 C on s03 (kept): 403 260 s below 0 C, never detected.
   - These numbers depend on the plant being able to reach -20 C, which is doubtful for a converted fridge (see D).

10. **Cooling stuck on, and blind cooling, freeze the product.**
    - fault_cool_stuck_on_4c: air down to -15.2 C and product to -9.4 C (worst). At 1.5 C, air is below 0 C for 28 275 s on the median plant.
    - fault_climate_dead_7d_1p5c on s14: air below 0 C for 489 360 s and product for 584 100 s.
    - fault_climate_dead_24c on c13: mean -16.1 C for 24 h because the heater is locked out while blind.

11. **When the RH probe reads low, true RH reaches 100 % for days.**
    - fault_rh_drift_down_fast_13c: fault_rh_max_err +24 (100 %RH) on the median plant. Detection takes a median of 21 h; 4/36 plants are undetected (kept s03, s10 and s13 at 100 %RH for 48 h).
    - Slow drift: median 6.6 days to detection, 10/36 undetected.
    - This means surface slime and mould. The quality loss is large, with a possible safety tail.

12. **An empty humidifier at 4 C goes unnoticed.**
    - 19/36 plants undetected (kept c02, c04, c09). True RH falls 20-40 %RH below setpoint (sweep fault_rh_min_err median -20.1).
    - This causes case hardening.

## C. Regulation and nuisance

13. **The temperature loop oscillates on a small cabinet with a large heater and compressor (s03).**
    - Steady 13 C: air runs 9.3-18.0 C (p1/p99) with heater and compressor on together (kept/fault_temp_drift_down_mid_13c__s03 pre-fault).
    - steady_cure t_span_xd is 8.68 C, against 3.49 C worst for a simple thermostat on the same plant set (compare/core_vs_reference_emulation.csv). faultfree_14d_13c s03 has ff_powered_t_p99_abs 5.03 C.
    - No alarm is raised.

14. **The biltong and equalize presets are unattainable, and the alarm signals constantly.**
    - base/steady_biltong_25c_55rh: RH mean +11.26 %RH, heater duty 96 % (saturated), air -0.94 C.
    - The contact is open 99.96 % of the time on the nominal plant and median 99.96 % across the sweep. That conditions owners to ignore it.
    - Equalize 4 C / 85 %: rh_mean_err_xd worst -29.3. Beef: -19.2.

15. **False critical sensor-blame alarms appear in fault-free runs.**
    - cure/ferment_coldhot: RH_READING_DISAGREES (critical) at 432 001 s lasts 176 400 s (49 h), plus 3 RH_DRIFT_SUSPECT warnings. The recomputed RH reading error was at most 0.6 %RH; the real cause was a 39 C room.
    - TEMP_READING_DISAGREES (critical) fires in fault-free worst/faultfree_14d_1p5c (s16) and kept/…__s07.
    - prefault_disagree_raises is 1 in 9 drift runs (s07, s16 and s17 at 1.5 C).

16. **Nuisance alarms recur before faults.**
    - 319 of 2 160 fault runs have warning or critical alarms before the fault is injected (sweep prefault_any_warn_crit). They concentrate on c11 (55), s01 (55), s16 (52) and s20 (49).
    - On c11, s01 and s20 an AIR_SUSPECT warning follows every defrost, caused by a 100 % fan kick giving 6.5 m/s (worst/fault_duct_blocked: 44 587-44 644 s).

17. **Without a clock, outage durations are unknown.**
    - Duration reported for only 7/80 outages (restart/base_nortc) and 8/60 (restart/hot_nortc). Food-safety time-at-temperature after an outage cannot be reconstructed.

18. **Defrost runs where there is no frost, and relays cycle heavily.**
    - Defrost runs for 1 200 s every 12.3 h at 13 and 24 C with frost at 0 g, costing +1.5 C median air (defrost_13c dist_t_peak_err).
    - The humidifier relay cycles 355 times/day (cure/beef_base: 10 312 in 28 days), up to 428/day in the sweep. That is relay-life wear.

## D. Plant plausibility (the simulator is part of the evidence)

19. **The coil can freeze air like a freezer.**
    - CoilFloorC is -28 C on all 37 plants and is not swept. Cooling capacity is 90-200 W at a coefficient of performance (COP) of 1.4-2.6, which is about 130-520 W into 0.2-0.6 m3.
    - True air reaches -20.5 C (fault_temp_drift_up_fast_1p5c) and -15.2 C (fault_cool_stuck_on_4c). A converted refrigerator cannot do that.
    - Cold-side harm is overstated. Pull-down (R2.5), recovery and cooling-failure detection are likely flattered.

20. **The door model is gentle.**
    - With a 60 min door opening (base/door_60min_13c, room 19 C), true air peaks at 15.8 C and sits around 14.6 C, with the compressor at 36 % duty.
    - Product never leaves ±1 C in door runs on the median plant (rec_prod_band1_s = 0).
    - Door recovery results (median 180 s) are probably too good.

21. **The frost model allows 3.7 kg of ice on a fridge coil.** A real coil would block airflow long before that. The direction of the effect is right, but the timescale is suspect.

## E. Reported against recomputed

- About 1 200 values recomputed from raw match within rounding: detection, critical time, fault extremes, pre-fault counts (1 134 checks), all fault-free ff_* metrics, steady spans and RH metrics, and restart, cut-matrix and crash counts. The source hash is identical at start and end of the run.
- **Recovery times are floored by the averaging window.** rec_t_band2_s and rec_t_band1_s are reported as 180/184 s and rec_rh_band8_s as 300/304 s where raw data is back in band in 4-90 s. Examples: base/door_2min_13c (5 s), base/door_30s_4c (15 s and 80 s), base/room_step_plus6c_13c (4 s). This is a reporting convention, but it makes medians look like a measured 3 min.
- **rec_rh_band5_s does not reproduce.**
  - base/defrost_4c: reported 4 869 s, recomputed 33 944 s.
  - base/defrost_13c: 8 589 vs 11 469 s.
  - base/room_step_plus6c_13c: 304 vs 794 s.
  - In each case the 5 min mean RH exceeds ±5 %RH by up to 0.13 %RH inside the claimed hour, so the result depends on how the window edge is handled. RH sits on the edge of the band.
- **signal_change_s.** It first looked wrong in 13/189 fault runs where the contact was already signalling, because 30 s samples never show the change. The events.log alarm_signal records confirm a real 15 s release, so this is not a mismatch. It does mean the only "change" is a 15 s release (defect 8).
- **Contact during outages.** Unpowered samples show the contact not signalling, against the claim in the unit test name (defect 7).
