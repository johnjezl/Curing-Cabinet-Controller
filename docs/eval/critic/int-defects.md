# Integration defects, largest first

All paths are relative to this directory. Log samples are log/s-<host epoch>.json; injection times are in injections.txt.

1. **A stuck-on heater is not identified, and the panel says the heater is off.** I injected a stuck-on heater relay at 1794073210. The cabinet sat at 16-18.7 C against a 13 C setpoint for about 16 virtual hours. Only TEMP_HIGH and TEMP_HIGH_CRITICAL were raised, never HEAT_STUCK_ON. The HEAT chip read "HEAT off" the whole time. The headline was a cleared event ("Outputs were switched off by the hardware guard ... Control has resumed"), not the live +5.9 C excursion or the action an operator needs (cut the heater). The compressor meanwhile kept its minimum-off spacing ("runs are spaced to protect the compressor") during a critical warm excursion. The same cause alarm fired falsely twice when the heater was fine, each time saying "Disconnect it" (virtual 11-06 00:03 in a room raised by 12 C, and 11-07 15:32). The diagnosis is backwards both ways. Evidence: sim-10-heat-stuck.png, sim-11-heat-stuck-later.png, sim-12-heat-stuck-after-ack.png, log/s-1794073259.json, final-history.txt.

2. **Heating does not regulate. The cabinet runs well under setpoint while the panel calls the gap slight.** In Ferment at a 24 C setpoint, the true air averaged 20.7 C over 2 days. The heater was on in 2 of 62 samples. TEMP_LOW was active for about 36 virtual hours (peak 18.1 C). The status line read "HEAT off: slightly under setpoint; pulses are spaced" at -3.4 and -4.7 C. Under a 15 C override, drying averaged 13.87 C (a steady -1.1 C) for about 5 days with cooling "removing moisture". Caveat: my accidental all-device fault window (virtual 11-01 11:15-13:30) came before the largest deficit. Before it, the air was already sliding 23.7 -> 22.3 C with the heater off (log/s-1794071782..1794071831.json). A slow, cool ferment means slow acidification, which is a food-safety issue for fermented sausage, not just a quality one. Evidence: sim-01-templow-heater-off.png, sim-02-door-open.png, sim-13-setpoint.png, the per-phase statistics in the verdict notes, and log/.

3. **A latched system alarm holds the remote alarm contact open for the rest of the cure.** CONTROLLER_RESTART_LOOP (critical, "started 5 times in 10 minutes") stayed active from virtual 11-10 until the cure ended at 11-14, about 15 real minutes after the restarts stopped, even after it was acknowledged. GPIO24 stayed low throughout (cont False in every log sample after 1794073942). A dialer or contact monitor therefore sees no new transition for any later excursion, and the alarm channel stops carrying information for the rest of the batch. The stale alarm also took the panel headline over "ENDED: VERIFY BEFORE USE". Evidence: sim-19-finished.png, pair-ui.png, log/s-17940745*.json, status-final.txt.

4. **The output write fault makes the panel lie and silences the buzzer.** With outputs:write-err, the panel showed COOL ON and HEAT ON while nothing could switch, and the true air rose 11.7 -> 15.9 C. TEMP_HIGH_CRITICAL was active while buzzer_on was false and GPIO25 low, yet the panel still offered "SILENCE BUZZER". The guard drop wrote 104 one-second OUTPUTS_DROPPED alarms into the history in about one virtual minute, flooding the record. alarm_armed read false for about 14 real seconds. Evidence: sim-09-write-err.png, log/s-1794073126..1794073189.json, final-history.txt, simulator journal line "outputs cannot be written ... Staying up instead of restarting".

5. **The warm-hazard sensor failures cannot be demonstrated: simulator drift and stuck faults do nothing.** `sim-fault climate drift -4`, `-40`, `+3` and `sim-fault climate stuck` all returned {"ok":true} and appeared in the simulator's fault list. The reported temperature still tracked the plant truth within +/-0.5 C for up to 36 virtual hours. A sensor reading low while the product warms is the main way a curing chamber spoils meat. This build gives no evidence that it is caught, and the test tool reports success for an injection that has no effect. Evidence: log/s-1794072274..1794072700.json, log/s-1794072871..1794072950.json, log/s-1794073379..1794073440.json (diff.py prints reading minus truth).

6. **The outage and crash record is inconsistent.** The labctl power cut (14.5 virtual hours) appears in the alarm history as POWER_OUTAGE with since = the time of return (11-09 17:58), not the loss (11-09 03:26), and duration -1. The four service crashes and hangs are also recorded as "Power was lost" with duration -1. The real durations exist only in the status/program fields (outage_known 81,291 s, last_outage 52,311 s). An operator reading the alarm history gets wrong times and wrong causes. Evidence: final-history.txt, sim-after-powercut.json, sim-14-after-powercut.png.

7. **Humidity is held high and loose.** In Ferment at a 90 % setpoint, RH sat at 94-97.7 % for about 2 days before any fault, with the dehumidifier never on. In drying it swung 61-94 % around 76-82 %, with RH_NOT_FALLING raised 6 times. RH up to +9 % shows in white (no deviation colour) until the 10 % alarm band is crossed. Prolonged high RH at 24 C in ferment favours surface mould and slime. Evidence: log/s-1794071789..1794071831.json, sim-13-setpoint.png.

8. **The weight-loss figure is nonsense after a load change and is printed on the end-of-cure line.** After a 5 kg addition the panel showed "LOSS -46.0 %", then "-36.0 %" at "ENDED: VERIFY BEFORE USE". WEIGHT_STEP was info-only and cleared after 1 virtual hour, but loss was never re-based. Evidence: sim-06-nocool.png, sim-19-finished.png, sim-final.json.

9. **Alarm priority hides the actionable item.** The door being open was hidden behind "Temperature low (+2 MORE)". Stale or cleared events (OUTPUTS_DROPPED, CONTROLLER_RESTART_LOOP) headlined over live critical temperature. From 2 m the operator reads the wrong problem. Evidence: sim-02-door-open.png, sim-11-heat-stuck-later.png, sim-19-finished.png.

10. **Simulation marking is not unmistakable, and simulation drives the real annunciators.** Sim mode is marked by small purple SIM tags and a thin hatched border. There is no full-width SIMULATION banner, no speed factor, and the clock shows virtual time. At 2 m it looks like a running cabinet. In sim mode the real GPIO24 (alarm contact) and GPIO25 (buzzer) are driven by simulated alarms, so with a real alarm dialer wired, simulated alarms go out as real ones. Evidence: sim-00-idle.png, log/l-*.txt (GPIO24/25 follow sim alarms).

11. **Blind cooling with a dead sensor overcools when the room changes.** With the climate sensor dead and the room dropping back by 12 C, "repeat recent pattern" cooling drove the true air to 7.1-7.5 C against a 13 C setpoint for about 6 virtual hours. This is the cold side, so it does not spoil product, but it shows blind cooling is not bounded. Evidence: log/s-1794073035..1794073096.json, sim-08-climate-dead.png.

12. **Alarm signalling drops briefly in several paths.**
    - Acknowledging an active TEMP_LOW warning closed the remote contact while the excursion continued, until it re-signalled about 6 virtual hours later.
    - A silence lasted about 30 virtual minutes (4 real s at 400x). This is fine, but it is not visible in any record.
    - The buzzer (GPIO25) went low for 3-17 s during each crash or hang. The contact was correctly low.
    - SIGKILL took 5.4 s to take effect.

    Evidence: crash-kill.txt, crash-stop.txt, simcrash-*.txt, log/s-1794071958.json.

13. **Real mode with nothing attached reports alarm_armed false while a critical sensor fault is active.** The contact was open (GPIO24 low) and the buzzer on, so a fail-safe monitor would still alarm, but nothing on the panel says whether the remote alarm is armed. Evidence: status-real-5min.json, real-01-boot.png, real-lines-boot.txt.

Not defects, for balance: the following were observed working.
- Sensor-dead lockouts.
- Fan-stall interlocks.
- Compressor delays.
- COOL_NO_EFFECT.
- Watchdog restart (10 s).
- Panic containment.
- Stale-panel overlay within 20 s.
- Override marking that persists across power loss.
- Outage time excluded from phase time.
- Degree-hours flagged incomplete.
- An end-of-cure statement that makes no safety claim.
