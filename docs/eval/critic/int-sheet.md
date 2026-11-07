# Integration behaviour sheet

Rig: pi-5-3 with only the 800x480 panel attached. Tree: HEAD 05f449e plus uncommitted work, source digest in tree-hash.txt. Deployed /opt/curing/bin/cured sha256 b9ee1d82...66f66a1 (version 2c7ab8862fc578f7). All hashes are in hashes-initial.txt and hashes-final.txt.
Simulated cure: recipe test-12d (3 phases: Ferment 24 C/90 %, Dry early 14 C/82 %, Dry 13 C/76 %), speed 400x, compressed outages. 1 real second is 400 virtual seconds.
Sample log: log/s-<epoch>.json (state) and log/l-<epoch>.txt (GPIO lines), taken about every 7 s. Injection times: injections.txt.
Caveat: at virtual 11-01 11:15-13:30 a probing loop of mine injected dead and then stuck on every simulated device for about 30 real seconds before I repaired them (see injections.txt). Items that depend on that window say so.
Item numbering inside each group is my own breakdown of the group title.

## R1 Setpoint ranges and control bands
- Item R1.1: Recipe and override temperature setpoints of 13, 14, 15 and 24 C were applied and shown. The accepted min/max range was not exercised. (MEASURED-SIM)
- Item R1.2: Humidity setpoints of 76, 78, 82 and 90 % were applied. The range limits were not exercised. (MEASURED-SIM)
- Item R1.3: Airflow setpoint 1.00 m/s read 1.03-1.05 m/s with the fan at 11 % for the whole cure, except during the injected fan stall. (MEASURED-SIM)
- Item R1.4: Ferment at 24 C setpoint: the true air temperature averaged 20.7 C over 2 days (62 samples). Its maximum was 23.6 C, reached only in the first hours. The heater was on in 2 of 62 samples. Before my accidental fault window the air drifted 23.7 -> 22.3 C with the heater off. Afterwards it ran 3.4-5.9 C under setpoint for about 36 h, with TEMP_LOW active and the heater reason "slightly under setpoint; pulses are spaced". (MEASURED-SIM)
- Item R1.5: Drying temperature: setpoint 14 gave a mean of 14.27 C (12.4-15.5 outside faults). Setpoint 13 gave a mean of 13.49 C. Override 15 gave a mean of 13.87 C (11.1-15.8), a steady 1.1 C under setpoint for about 5 virtual days with cooling running "removing moisture". (MEASURED-SIM)
- Item R1.6: Humidity band: Ferment at 90 % averaged 93.9 % and peaked at 97.7 % before any fault, with the dehumidifier never on. Dry early at 82 % swung 73-91 %. Override 78 % averaged 83.9 % (61-94 %), with RH_NOT_FALLING raised 6 times. (MEASURED-SIM)
- Item R1.7: The device reported these alarm bands: temperature +/-2.5 C, RH +/-10 %, deviation delay 15 min, door delay 5 min, absolute 30 C high and -0.5 C low. TEMP_LOW showed limit 21.5 C at setpoint 24. (MEASURED-SIM)
- Item R1.8: An operator setpoint change (15 C/78 %) applied at the next sample and was marked OVERRIDE on temperature, humidity, air and the cure line. It survived a power cut and 4 service restarts. (MEASURED-SIM)
- Item R1.9: Phases advanced Ferment -> Dry early at 2.0 d and Dry early -> Dry at 5.0 d. The cure ended at total_elapsed 12.0 d (1,036,800 s). Phase time excludes outage time. (MEASURED-SIM)

## R2 Recovery
- Item R2.1: Cold boot through a labctl power cycle in real mode: first panel frame at uptime 36.13 s and first control step at 36.56 s, about 49 s (+/-7 s) after power was restored. The watchdog reported OK. (MEASURED-RIG)
- Item R2.2: A graceful reboot came back in 119 s, with the first frame at uptime 29.41 s and control at 29.81 s. (MEASURED-RIG)
- Item R2.3: A labctl power cut mid-cure (off 33 s, rig answering again 97 s after power-on) resumed the same phase with the override intact. In sim mode control started at uptime 85.8 s, because it waits about 58 s for a trusted clock before accounting the gap. (MEASURED-SIM)
- Item R2.4: Outage length: real mode reported 73 s (start uncertainty 60 s) for a real gap of about 56 s. Sim mode reported 52,311 virtual s against the simulator's 52,135 s, with temperature on return 18.7 C and recover_sec 509. (MEASURED-RIG / MEASURED-SIM)
- Item R2.5: Phase elapsed advanced 7,791 s across a span of about 60,000 virtual s, of which 52,135 s was outage. Outage time is not counted as cure time. (MEASURED-SIM)
- Item R2.6: SIGKILL of the service: the process took 5.4 s to exit, the guard confirmed outputs off, and the service was active again in 8.5 s (NRestarts 1). (MEASURED-SIM)
- Item R2.7: SIGSTOP hang: the systemd watchdog killed it at 10 s and the service was back in 19.9 s. The control-loop hang injection was back in 13.1 s, and the control panic in 5.8 s. (MEASURED-SIM)
- Item R2.8: After 5 starts, CONTROLLER_RESTART_LOOP (critical) was raised. It never cleared before the cure ended, about 3.5 virtual days and 15 real minutes later. (MEASURED-SIM)
- Item R2.9: After the compressor was repaired, air went from 25.2 to 14.5 C within about 1 virtual hour. After the climate sensor was repaired, readings were valid at the next sample, with one 4 s TEMP_SUSPECT blip. (MEASURED-SIM)

## R3 Equipment protection
- Item R3.1: After boot the compressor is held: "COOL HELD: power-on delay, 3:12 left" (2:48 in another boot). (MEASURED-RIG)
- Item R3.2: Minimum off time is enforced ("COOL HELD: minimum off time, 3:31 left"). It was enforced even during TEMP_HIGH_CRITICAL at +5 C, where cooling showed "building up demand; runs are spaced to protect the compressor". (MEASURED-SIM)
- Item R3.3: Fan stall: FAN_STALL critical plus AIR_LOW. The heater and humidifier were "locked off: no airflow proven" and the fan was commanded to 100 %. (MEASURED-SIM)
- Item R3.4: With nothing attached, the fan showed 40 % at 0 rpm and FAN_TACH was raised. GPIO12 PWM was active. (MEASURED-RIG)
- Item R3.5: With climate data bad, the heater and humidifier are locked off (HEAT FAULT, HUMID FAULT chips), in both real and sim mode. (MEASURED-RIG)
- Item R3.6: Automatic defrost cycles ran during drying (defrost on, fan 25 %). A forced defrost was not exercised. (MEASURED-SIM)
- Item R3.7: Every stop, crash and hang logged "outputs off confirmed by guard". In real mode the plant relay lines GPIO17, 27, 22 and 23 read low after boot and at the end. GPIO24 (contact) was low, and GPIO25 (buzzer) was high while the sensor fault sounded. (MEASURED-RIG)
- Item R3.8: Heater relay stuck on: never identified as such in about 16 virtual hours. The compressor cycled against it with min-off spacing, and the cabinet sat at 16-18.7 C against a 13 C setpoint. (MEASURED-SIM)

## R4 Failure behaviour
- Item R4.1: Real mode with nothing attached: temperature, RH and air show FAULT and weight shows NOT FITTED. SENSOR_CLIMATE_FAULT (critical), SENSOR_AIR_FAULT and FAN_TACH are raised. Heater and humidifier are locked off. Cooling "repeats its recent pattern with no feedback" and was held by the power-on delay while observed. (MEASURED-RIG)
- Item R4.2: Climate sensor dead: SENSOR_CLIMATE_FAULT critical within one sample (under 1 virtual hour). Blind cooling then held the true air at 12.5-13.8 C in a room raised by 12 C, for about 8 virtual hours. (MEASURED-SIM)
- Item R4.3: The room returned to normal while the sensor was dead: blind cooling overcooled the true air to 7.1-7.5 C against a 13 C setpoint for about 6 virtual hours. Cold side only. (MEASURED-SIM)
- Item R4.4: Sensor drifting low (warm hazard): the simulator accepted the drift fault (-4, then -40), but the reading stayed within +/-0.5 C of the truth for 36 and 8 virtual hours. The controller's drift detection was not demonstrated. (NOT-SHOWN)
- Item R4.5: Sensor drifting high: drift +3 was accepted and had no effect on the reading. (NOT-SHOWN)
- Item R4.6: Climate sensor stuck was accepted and had no effect on the reading. Air and scale "stuck" (accidental window) raised AIR_SUSPECT and SCALE_SUSPECT within about 10 virtual minutes. (NOT-SHOWN for climate, MEASURED-SIM for air/scale)
- Item R4.7: Compressor not cooling (room +12 C): COOL_NO_EFFECT in about 32 virtual minutes and TEMP_HIGH_CRITICAL at 18.2 C. The peak was 25.2 C. The fault was cleared after repair. (MEASURED-SIM)
- Item R4.8: Heater relay stuck on: TEMP_HIGH and TEMP_HIGH_CRITICAL at about +5 C (peak 18.7 C). There was no cause alarm, and the panel kept showing "HEAT off". (MEASURED-SIM)
- Item R4.9: Output write fault: OUTPUT_FAULT, then the guard dropped all outputs. The panel showed COOL ON and HEAT ON while nothing could switch. The true air rose 11.7 -> 15.9 C and TEMP_HIGH_CRITICAL was raised. The buzzer was off throughout. History logged 104 OUTPUTS_DROPPED entries in about 1 virtual minute. (MEASURED-SIM)
- Item R4.10: Door open 10 real s (about 67 virtual min): DOOR_OPEN, the buzzer, and RH_LOW as RH fell to 54 %. RH recovered to 79-89 % within about 1.5 virtual hours. (MEASURED-SIM)
- Item R4.11: A 5 kg warm load at 25 C: WEIGHT_STEP (info). Product went 17.4 -> 13.5 C in about 2.5 virtual hours with no TEMP_HIGH. The weight-loss figure was wrong from then on (-46 % to -36 %). (MEASURED-SIM)

## R5 Alarming
- Item R5.1: Local buzzer is GPIO25. It was high when status reported buzzer_on, which happens for critical alarms and the door, and also in real mode idle with nothing attached. (MEASURED-RIG)
- Item R5.2: The alarm contact is GPIO24. It is high (contact closed) when armed with no unacknowledged alarm, and low on alarm. It went low within 3 s of a crash or hang and stayed low until restart, so it fails safe. (MEASURED-SIM; line levels MEASURED-RIG)
- Item R5.3: Real mode idle with a critical sensor fault: alarm_armed false, contact low, buzzer on. The meaning of "not armed" with the contact open was not explained on the panel. (MEASURED-RIG)
- Item R5.4: SILENCE BUZZER changed to "BUZZER SILENCED". The buzzer sounded again about 4 real s (about 30 virtual min) later while the critical alarm was still active. (MEASURED-SIM)
- Item R5.5: Acknowledging an active warning (TEMP_LOW) closed the contact and silenced the buzzer. Both signalled again about 6 virtual hours (54 real s) later. (MEASURED-SIM)
- Item R5.6: Acknowledging an active critical (TEMP_HIGH_CRITICAL) left the contact open. The buzzer sounded again within about 1 virtual hour. (MEASURED-SIM)
- Item R5.7: Deviation alarms waited for the 15 min delay. Critical temperature alarms were raised in the same sample as the excursion. (MEASURED-SIM)
- Item R5.8: Latched CONTROLLER_RESTART_LOOP (critical, acknowledged) and the associated outage criticals kept the contact open from virtual 11-10 to the end of the cure. The remote channel could not show a new transition for any later alarm. (MEASURED-SIM)
- Item R5.9: False alarm: HEAT_STUCK_ON "Disconnect it" was raised twice when the heater was not stuck (in a room raised by 12 C after a compressor repair, and after the output fault cleared). The heater reason stayed "normal", so regulation was not changed. (MEASURED-SIM)
- Item R5.10: Missed cause alarm: a real heater relay stuck on never raised HEAT_STUCK_ON. Only TEMP_HIGH and TEMP_HIGH_CRITICAL were raised. (MEASURED-SIM)
- Item R5.11: During the output write fault the buzzer stayed off (buzzer_on false, GPIO25 low) through TEMP_HIGH_CRITICAL, while the panel offered "SILENCE BUZZER". (MEASURED-SIM)

## R6 Front panel
- Item R6.1: Temperature and RH digits are about 110 px tall. Values are coloured by deviation (amber for low, red for high) with a signed delta, and the banner is red for an alarm and amber for a warning. Readable at 2 m. (MEASURED-RIG)
- Item R6.2: The banner shows the top alarm with "(+N MORE)", an action sentence and a SILENCE BUZZER button, and ALARMS shows a count. (MEASURED-RIG)
- Item R6.3: Banner priority is wrong in practice. DOOR_OPEN was hidden behind "Temperature low (+2 MORE)". A cleared event ("Outputs were switched off ... Control has resumed") and the stale "Controller keeps restarting" headlined over a live TEMP_HIGH_CRITICAL at +5.9 C and over the cure-ended notice. (MEASURED-SIM)
- Item R6.4: A dead sensor shows a large red FAULT with "no valid reading", the held outputs are named, and the status line reads "NO TEMP OR RH FEEDBACK". (MEASURED-RIG)
- Item R6.5: A stalled or panicked panel data path showed STALE on every value with the last value, "CONTROLLER NOT RESPONDING ... Values shown are not live" and "OUTPUTS UNKNOWN" within 20 s. It recovered by itself. (MEASURED-SIM)
- Item R6.6: Output chips show the commanded state, not the proved state: COOL ON and HEAT ON during the write fault, HEAT off while the heater relay was stuck on. (MEASURED-SIM)
- Item R6.7: Simulation marking: a purple SIM tag on every value and a thin hatched purple border. There is no full-width SIMULATION text and no speed factor on the home screen, and the clock shows virtual time. (MEASURED-SIM)
- Item R6.8: Override is marked with an orange OVERRIDE tag on each setpoint and an "OVERRIDE this phase" line. (MEASURED-SIM)
- Item R6.9: The status line explains held outputs ("COOL HELD: power-on delay, 3:12 left"). It said "HEAT off: slightly under setpoint; pulses are spaced" at -4.7 C. (MEASURED-SIM)
- Item R6.10: The cure line shows phase n/3, name, time left and loss %. At the end it showed "ENDED: VERIFY BEFORE USE, loss -36.0 %". (MEASURED-SIM)

## RF Food-safety support
- Item RF.1: At cure end the record says weight loss "does not show that product is safe" and asks for water activity and pH to be checked with calibrated instruments. Nothing claimed the product was safe. (MEASURED-SIM)
- Item RF.2: Degree-hours above 15.6 C were tracked (333.7) and flagged incomplete, with an outage estimate of 8.18. (MEASURED-SIM)
- Item RF.3: The outage record shows outage_count 5 and outage_known 81,291 s. In the alarm history every outage entry has duration -1, its "since" is the time of return rather than the time of loss, and the 4 service crashes are recorded as "Power was lost". (MEASURED-SIM)
- Item RF.4: The excursion record holds 154 history entries with since, cleared and peak, and the first-day entries are retained. 104 of them are 1-second OUTPUTS_DROPPED repeats. (MEASURED-SIM)
- Item RF.5: CAL_CHECK_DUE (info) "Compare temperature and humidity with a separate reference" was raised at virtual day 7. The scripts do not expose the reference check itself. (NOT-SHOWN)
