# Defects, system layer, round 1 (largest first)

Paths are relative to evidence/d/ unless stated. "repro" means files under
/tmp/claude-1000/-home-john-projects-Curing-Cabinet-Controller--claude-worktrees-bridge-cse-014Q9arxsNRyixQaUNwLct8J/b4f63687-8409-5247-bdf7-bd66d4506c04/scratchpad/critic/repro/

1. **An output write error drives the controller into an endless watchdog-kill loop. The evidence set routes around it.**
   - cure-run-with-lost-repair/actions.log: "sim-fault outputs write-err" at 13:49:40. The repair command failed with "connection reset by peer", and "sim-state no answer" repeats for 5+ minutes.
   - cure-run-with-lost-repair/final-status.txt: curing-sim NRestarts=94, with CONTROLLER_RESTART_LOOP, OUTPUTS_DROPPED and POWER_OUTAGE active. The journal reads "restart counter is at 94" at 14:14:18, with a watchdog timeout every 13-17 s.
   - Screen d7.388-periodic.png: during the loop, temperature is 18.9 °C against a 13.0 setpoint (+5.9), air 0.00 m/s, and FAN 100 % 0 rpm.
   - Same build in the full cure: power-cut-cure-full/journal.txt has the write-err at 15:52:28 and "Watchdog timeout" at 15:52:38. The process ignored SIGABRT ("stop-watchdog timed out. Killing", SIGKILL at 15:52:41) and was gone at 15:52:44.
   - The accepted cure/ run only "passed" because the repair arrived 9.74 s after the fault (15:27:07.397 to 15:27:17.136), under the 10 s watchdog.
   - In real time, a 13-17 s restart loop resets the 4:00 cooling hold and the 5:00 heat/humid settle on every start, so the cabinet would get no control at all for as long as the fault lasts. (INFERRED from R3.3 behavior)

2. **Card writes stall control.**
   - Clean stops took 22.75 s (power-cut-real-off30s/37-journal.txt, 14:25:28), 11.13 s, 6.69 s and 5.47 s (21-driver-faults-journal.txt), all logging "still flushing state to the card".
   - Goroutine dumps at watchdog kills show `syscall.Fsync` in `journal.flush` (power-cut-cure-full/journal.txt 15:52:40; cure-run-with-lost-repair/journal.txt 14:12:48, 37 Fsync frames).
   - The dumps prove fsync was running when the hung process was killed. They do not prove the control goroutine was the one waiting on it.
   - On an SD card over weeks, slow or stalled writes are the most likely field failure. Nothing shows control isolated from them, and no storage fault was ever injected (R4.9 NOT-SHOWN).

3. **The local alarm goes silent exactly when the controller fails.**
   - Repro (kill-1.txt): GPIO25 (buzzer) drops from high to low at +0.023 s after SIGKILL and returns at +2.70 s.
   - Repro (stop-1.txt): GPIO25 goes low at +2.958 s of a hang and stays low until +12.6 s.
   - The guard cuts the buzzer with the plant outputs. In a hang loop like item 1 the buzzer is low most of the time, and through a power loss nothing sounds.
   - The real-mode captures (30-real-*) all start with gpio25_before=lo, so the evidence never showed this.

4. **Silence does not silence (simulated plant).**
   - cure/actions.log: "sim-silence" at 15:29:44.586 got reply ok.
   - cure/timeline.jsonl: at virtual day 8.306 and 8.342, buzzer_on=true and buzzer_silenced=false.
   - The buzzer stopped only after "sim-ack" (8.350), which removed the latched critical OUTPUTS_DROPPED alarm from the active list. Acknowledging clears a critical latched alarm outright.

5. **A false critical alarm with a false story is showing on the real controller right now.**
   - Repro before.png (16:12:36), real mode: "Outputs were switched off by the hardware guard … The control loop stalled and the guard cut all outputs. Control has resumed."
   - The real controller had not stalled. It started cleanly at 16:06:02 ("clean previous stop true"), and the stall was in the simulated run at 15:52:38.
   - The alarm survives SIGKILL restarts (sigkill/frame-04937ms.png) and crosses the sim/real boundary.
   - The kernel-panic run shows the same text (screens/sim-after-panic.png).

6. **The panel says power was lost when it wasn't.**
   - POWER_OUTAGE is active with outage.detected=false after a plain restart (15-display-state-at-start.txt, 25b, 37-status-after-clean-restart.txt).
   - The "Power was lost" banner appears after a clean restart (screens/real-after-restart.png, 13:24:20) and after SIGKILL (sigkill-frames/frame-00001ms.png).
   - Severity varies for the same code: info (37), warning (power-cut-sim-1x 34-*), critical (70-app-panic.txt, power-cut-cure-full). Operators will learn to ignore it.

7. **The outage record's start time and duration are wrong.**
   - power-cut-real-off30s/32-status-30s.txt: "at" 14:43:19.33Z. But 10-status-before.txt shows the process alive at 14:43:52, and 20-cut-timing.txt shows pings until 1794062643.5 (14:44:03.5). The start is at least 44 s early, and duration 125 s overstates a nominal 30 s cut.
   - Repro (after status): at=16:13:07.93, which is the previous process's start. The hang began 16:13:19.25 and the duration reads 23.4 s against an actual 12.6 s gap.
   - Captured crashes record 6-15 s for 2.2-11.9 s gaps.
   - Unknown temperatures are stored as numbers: "temp_on_back_c":0, "peak_temp_c":0 with temp_known=false.
   - Simulated 1x: duration 7321 s against the simulator's own 837 s of plant time (power-cut-sim-1x-off30s/34-sim-state-60s-more.txt: virtual_s_at_checkpoint 185584, resume 186421).

8. **The clock is not kept across a real outage.**
   - power-cut-real-off30s/30-boot-timing-first.txt: rtc_epoch=90 (RTC reset; no backup), and system time about 2.3 min behind.
   - The new boot's journal (14:43:06) is time-stamped before pre-cut captures (14:43:52), so the log runs backward.
   - The record is correct only once NTP answers (outage "duration resolved 125 s" at 14:46:24). An offline cabinet would never get a trusted outage duration. Not tested.

9. **No real plant was attached, so the core fail-safe claims are unproven on hardware.**
   - The rig had no DHT22, no relays, no fan tach, no buzzer and no alarm relay. Every real-mode status has temp_quality=fault.
   - The external contact never closed in real mode (alarm_armed=false in every real capture), so "healthy → dead → open" was never shown on real hardware. It was shown only with the simulated plant.
   - All output evidence is GPIO register state from pinctrl.
   - The PWM debug view disagrees with sysfs: 22-watchdog-state.txt shows "requested … enabled 16000/40000 ns" but "actual configuration: disabled, 0/0 ns".

10. **Power-on paths differ between the simulated plant and real hardware.**
    - Simulated: the first control step comes 57.7-68.7 s after process start (71-boot-timing.txt process_age=61.089; power-cut-sim-1x 36-boot-timing-final.txt 68.721; power-cut-cure-full boot-timing-after-cut.txt 60.010).
    - Real: 0.94 s.
    - The real board never resumed a program across a cut ("real state wiped", 23-real-reset.txt). Program resumption (R4.5) is demonstrated only in the simulated path, which takes a different start sequence.

11. **The kernel-panic and power-loss evidence is thin and mislabeled.**
    - 70-app-panic.txt is a kernel panic, not an app panic. It ran with the contact already open: errors.txt says "contact did not close within 150 s", and the status shows contact false with OUTPUTS_DROPPED and CURE_FINISHED.
    - "back after 56 s" contradicts the file's own host clock: 1794062002.9 to 1794062120.0 is 117 s, and in 70b 1794062246.1 to 1794062369.6 is 123 s.
    - No line state was captured during any panic or outage.
    - The PID 1 hardware watchdog (RuntimeWatchdogUSec=10s) and a crash of the guard daemon (curing-outputd) were never exercised.

12. **The guard binary is not tied to the audited source.**
    - 13-hashes.txt: curing-outputd b31e08… has mtime 02:10, about 11 h before the run. hwcheck dates from 12:58 and curing from 02:57. Only cured is built in this run.
    - 02-static-binary.txt builds cured.check = 15a99f…, which is not the deployed 2ff50a… (10-deploy.txt). The tie between source hashes (00-versions.txt combined b688bc…) and the deployed binary is asserted, not shown.
    - The tree is untracked against HEAD 05f449e.
    - Positive finding: one cured build (2ff50a…, a293cfd79079cec6) covers every capture from 13:14 to 16:05.

13. **A hang is classified inconsistently.**
    - 30-real-STOP-1.txt: a watchdog kill is recorded as "clean previous stop true" with no outage.
    - STOP-2 and STOP-3 record "false" with a 15 s outage. The repro recorded false with 23 s.

14. **An operator restart erases the restart-loop alarm.**
    - 35-restart-loop-status.txt shows CONTROLLER_RESTART_LOOP critical; 37-status-after-clean-restart.txt shows it gone after one clean restart.
    - The loop needs 4 starts in 600 s to be raised.
    - Repro: my two crash tests plus the frame capture raised it on the rig (starts within 600 s: 4). The only allowed way to clear it is a clean restart, which wipes the evidence.

15. **The panel misdiagnoses a UI stall.**
    - screens/sim-during-stall-ui.png: "CONTROLLER NOT RESPONDING … check power", with all values STALE.
    - At the same time, 58d-during-stall-ui.txt shows control ticking (2040 ticks) and the contact closed.
    - A frozen panel for 13 s or more is not reported externally, and the operator is told the wrong cause.

16. **Display-missing detection is a planted file.**
    - 61-display-missing-on.txt creates /run/curing/display-missing, and detection happens only at process start.
    - The banner contradicts itself: "Front panel did not come up … Recorded state: fb0=yes touch=yes" (screens/sim-display-missing.png).
    - A real disconnected panel was never tested.

17. **The system reads "degraded" permanently.**
    - curing-sim is left "failed, result=resources" after every mode switch (14, 19, 91, and the repro status). 22-watchdog-state.txt shows "system: degraded".
    - power-cut-cure-full/journal.txt 14:18:19: "Failed to load environment files … Failed to spawn 'stop-post' task".
    - Any outside health check on systemd state is blind.

18. **The logs can lose minutes at a power cut.**
    - 22-watchdog-state.txt: journald SyncIntervalSec=5m, plus conflicting drop-ins (Storage=volatile and Storage=persistent).
    - Sensor-alarm transitions never reach the journal at all: 21-driver-faults-journal.txt has no SENSOR_* line for build a293cfd…, and its 200 lines are mostly the older build 840662e87906b08d.
    - Periodic sensor recording by the controller is not shown anywhere: "trend": null in all final-state.json files.

19. **The final status after returning to real mode is stale.**
    - 91-status.txt, after "provision real", shows mode "simulated", pid 848, recipe test-12d and control_last_tick_age_s 4.06.
    - The real process's status was never captured at the end of the main run. The status file can show a dead process as live data.

20. **Restart latency has an unexplained outlier.**
    - 30-real-KILL-1.txt: "Scheduled restart" at 13:21:27.34 but "Starting" at 13:21:32.52, a 7.9 s outage against 2.7 s in every other run.

21. **Capture names don't match content, which undermines the package.**
    - 15-display-state-at-start.txt holds boot-timing.
    - 22-watchdog-state.txt is a full hwstate dump.
    - 19-status-no-sensors-300s.txt was taken at process_age 370 s.
    - The "undisturbed-boot" screens were taken at process_age about 160 s.
    - 80-cure-driver.txt is empty.
    - 70 versus 70b: one invalid test is kept beside its redo.

22. **Open-loop cooling without a sensor (control-layer, but a reason to distrust it with a batch).**
    - The banner on every real screen reads "cooling repeats its recent pattern with no feedback".
    - Over weeks with a dead DHT22 this can freeze or warm product. Only the alarm contact and buzzer would warn, and items 3 and 9 show those are unproven or self-silencing.

Reproduction summary (rig pi-5-3, 16:12-16:14 UTC, same build a293cfd79079cec6):

| Test | Reproduction | Captures |
|---|---|---|
| SIGKILL frames: exit after signal | 88 ms | 81 ms |
| SIGKILL frames: "panel painted stopped" | 1.17 s | 1.17 s |
| SIGKILL frames: restart scheduled | 2.18 s | 2.20 s |
| SIGKILL frames: new frame | 2.24 s | 2.25 s |
| crash-test-service KILL: active again | +2.72 s | 2.71-2.75 s, one 7.94 s outlier |
| crash-test-service STOP: guard drop | +2.96 s | sim 2.98-3.0 s; not visible in real captures because lines were already low |
| crash-test-service STOP: watchdog | +9.92 s | real 9.17-9.25 s, sim 9.93-10.02 s |
| crash-test-service STOP: active again | +12.63 s | 11.87-11.95 s real, 12.2-13.0 s sim |

- Matches within about 0.7 s.
- New in the reproduction: the buzzer line drops on crash and on hang (item 3), the stale OUTPUTS_DROPPED alarm in real mode (item 5), and the outage-start error (item 7).
- Rig left in real mode with curing.service active (NRestarts=3), with CONTROLLER_RESTART_LOOP raised by these tests.
