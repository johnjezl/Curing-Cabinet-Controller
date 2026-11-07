# Front panel defects, round 1 (largest first)

File paths are relative to evidence/c/ unless marked otherwise. "Fixture caveat" means the state was canned, so the panel's rendering is proven but not that the real core would produce that state.

## Evidence checks
- **Hashes:** the source hash list in 01-rig-section-versions.txt matches 00-versions.txt and matches the working tree as of this audit.
- **Coverage gap:** the lists cover only the UI files, the demo command and the walk script. They omit internal/core, internal/api, the fixtures and the binary on the board.
- **Timestamps:** the clock on each board frame agrees with its file time and its service start in 50-rig-log.txt, to within about 2 s. rig/sleep.png, rig/frozen.png and rig/service-stopped.png have no clock to compare, but their file times fit the log.
- **Frames look real:** all 15 are 800 x 480, the heartbeat bar is at a plausible position in each, and contents match file names and fixture arguments. The two notice frames differ from their rendered counterparts only inside the text area; the cause of that difference was not determined.
- **Tests run in this audit:** `go test ./internal/ui .` exits 1. The `.` target has no Go files, which is a setup failure in the command itself. internal/ui has a real failure (defect 2).

## Defects

1. **A stalled controller still looks live on Health and Set.**
   - The banner says CONTROLLER NOT RESPONDING, yet Health still lists "Temperature 13.2 °C, 2 s old", the same for humidity, airflow and weight, plus "Fan 45 %, 1320 rpm", "Watchdog ok" and "Data log logging".
   - Set says "Temperature now OK", "Humidity now OK", "Airflow now OK".
   - Anyone who goes to these screens to diagnose the fault is told the readings are fresh and fine. This is the exact stale-as-live failure. Home handles the same state correctly, so the protection exists on one screen only.
   - Files: screens/stalled__health.png, screens/stalled__set.png. No stalled Cure or Trend screen was captured.

2. **A UI test fails on the current tree, and the recorded evidence cannot detect this.**
   - TestEveryCoreAlarmDefinitionFits failed on two runs at about 03:06Z and 03:09Z. Titles "ALARM: Temperature reading disagrees with how the cabinet is behaving" and the humidity equivalent are truncated on Home at 19 px, and on Alarms at 14 px when simulated.
   - Recorded 20-go-test.txt says all pass. internal/core changed at 19:05 to 19:07 local, after the last board frame at 18:57, so the panel evidence predates the alarm catalog it must display.
   - The hash lists omit internal/core, so they still "match". Other work may be in progress on that code, so this needs a re-run once it settles. As it stands, a plausibility alarm is the alarm losing its text.

3. **Bad or dangerous conditions render with no alarm and with cooling on.**
   - all-bad: temperature STALE 2 h 1 min, humidity and weight FAULT 2 h 1 min, airflow SUSPECT. The banner is amber, ALARMS has no count, there is no silence button, and COOL ON is shown.
   - bad-numbers: both main readings FAULT, amber, no alarm, COOL ON.
   - stale-temp: 5:12 stale, COOL ON. Nothing says what control is doing without a temperature.
   - extreme-values: -2.5 °C (15.5 below set) and 100 %RH, "No alarm yet", COOL ON.
   - Fixture caveat applies. Even so, the panel offers a red FAULT block with an amber banner and an empty alarm list, and no real-core evidence shows these escalating.
   - Files: screens/all-bad__home.png, screens/bad-numbers__home.png, rig/stale.png, screens/extreme-values__home.png.

4. **A dead panel process may freeze on a green OK frame.**
   - Orderly stop and a simulated display hang paint notices (rig/service-stopped.png, rig/frozen.png; the hang notice took 5 s). That is good, but both need the process alive.
   - There is no evidence for SIGKILL, out-of-memory, kernel hang, or backlight or panel failure. A SIGKILL did occur on the board at 02:55:55, but no frame was taken before the restart.
   - INFERRED: the last frame stays in display memory, reading "OK: NO ALARMS, IN RANGE" with live-looking values. Only a stopped clock and a 5 px bar (0.7 mm, explained only on Health) would give it away, and neither is visible at 2 m.

5. **The state the panel spends weeks in is its least readable.**
   - After 10 min idle, digits peak at 93/255 against 250 normally, at 3.0:1 contrast, with the banner text at 2.3:1. Backlight is also cut to 102/255. INFERRED: about 5 % of full luminance.
   - The brightest thing on the dim screen is the COOL ON and HOME chips, not the temperature.
   - After 60 min idle: 7.2 mm digits at 3.4:1 and backlight 25/255. INFERRED: about 2 % of full luminance. There is no setpoint, no OK color, no clock and no recipe or phase. The position wanders. It says "no alarms", not "in range".
   - A person 2 m away cannot tell a good batch from a drifting one without walking over and touching it. A drifting (no-alarm) state during sleep was never captured.
   - Files: rig/dim.png, rig/sleep.png, walk/34-dim.png, walk/35-sleep.png.

6. **Board evidence is weaker than the folder name suggests.**
   - All frames come from a demo harness ("uidemo" in the journal) under a temporary service unit ("runtime unit from rig.sh, no unit installed"), with canned values.
   - No real sensors, no real core, no finger, no glass photograph. Touch is software injection.
   - Input-to-present of 2.6 to 7.1 ms (median 4.2 ms, 42 frames, 52-rig-walk-latency.txt) excludes the touch controller and the display.
   - Four different binary sizes were deployed across scenes (6103878, 6103910, 6103918, 6103822) with no hash recorded on the board, so no frame is tied to a build.
   - Three scenes needed retries, and PIDs and a boot marker show reboots between scenes with no recorded cause (50-rig-log.txt).
   - No board frame covers Alarms, History, Set, Trend or Health.

7. **Touch targets miss the 12 mm (about 89 px) glove size.**
   - Nav buttons are 80 px tall (10.8 mm), with only 4 px (0.5 mm) between them.
   - SILENCE BUZZER is 72 px tall (9.7 mm), ACK ALL and the Alarms tabs 72 px, recipe rows and trend range buttons about 79 px, and dialog and SAVE buttons 80 px.
   - The most urgent button on the panel is the shortest. Air and weight tiles are 82 px under an alarm banner. The silence button also sits next to the 633 px wide banner target, 6 px away.
   - TestTouchTargets passes anyway, so its threshold is below this requirement.
   - Files: 31-targets-alarm-home.txt, 31-targets-home.txt, 31-targets-set-pending.txt, screens/alarm-temp-high__alarms.png.

8. **A double tap can start a recipe.**
   - START... and the confirming START occupy the same spot: x 494..794, y 394..474 (tapped at 644,434 then 629,434). Confirm is a single tap, while Stop and Next need a 2 s hold.
   - On the board, injected taps 530 ms apart started a recipe (51-rig-walk-journal.txt, 02:57:35.115 and 02:57:35.645). A bounce filter exists, but this gap passes it.
   - The result is not harmless: setpoints jumped from 13 °C / 78 % to 22 °C / 90 % (walk/08-start-confirm.png, rig/walk-end.png), and the dialog does not state those numbers. It also records whatever is on the scale as the start weight.

9. **Setpoints cannot be adjusted during a recipe without destroying the recipe.**
   - The only stated path is "stop the cure first", and Stop "cannot be resumed".
   - During a multi-week cure with a drifting cabinet, the operator must choose between no adjustment and losing the program's position.
   - Files: screens/normal__set.png, screens/normal__cure-stop-confirm.png.

10. **Pause is one unguarded tap directly above the nav bar.**
    - PAUSE is 222 x 85 px, just above HOME and SET, with no confirmation. It stops the program clock.
    - A paused cure shows a calm blue banner, no time since pausing, and no reminder or escalation in any capture. A slip could silently stretch a phase for days.
    - Files: walk/10-paused.png, screens/paused__home.png, 30-walkthrough.tsv steps 23 to 25.

11. **The lock is not an access control, and its wording contradicts it.**
    - Anyone can unlock with a 3 s hold and no code (rig/lock-clear.png). That suits a wipe guard, but the LOCKED chip implies more.
    - A locked idle Home still says "Tap to choose a recipe" (rig/lock-set.png, rig/lock-after-restart.png).
    - The chip is black on green and about 13 x 5 mm, not visible from 2 m.

12. **Toasts hide content, outlive their cause, and let hidden controls fire.**
    - They cover about 722 x 112 px mid-screen and follow the user across screens.
    - "Wrong code." stays over the Installer menu and limits page after the correct code was entered (walk/31-installer.png, walk/32-limits.png).
    - "Released early. Hold for the full 2 s." sits over a fresh alarm's Home, covering air, weight and output chips (walk/15-alarm-home.png).
    - "Setpoints saved." covers the recipe detail and the start dialog body (walk/07-recipe.png, walk/08-start-confirm.png).
    - Key 8 at (550,318) was accepted under the toast (30-walkthrough.tsv step 83, walk/30-wrong-code.png). A gloved user can press what they cannot see, and the walkthrough ran faster than a person would, so real dwell needs checking.

13. **Silence behavior is largely unproven and inconsistently available.**
    - No audio evidence exists. Re-sound behavior, silence duration and silence for a second alarm are not shown.
    - Set, Cure, Trend, More and Health show the alarm with no silence button, so it takes 2 taps there.
    - The INFO filter reminder offers SILENCE BUZZER and lights the same red badge as a critical alarm, which teaches people to ignore red (screens/alarm-info__home.png).

14. **Screens contradict each other.**
    - Weight 4812 g from 5880 sits beside LOSS 35.1 % (should be 18.2 %) (rig/end-of-cure.png).
    - Trend weight "now" is about 5494 g while Home says 4812 g (screens/normal__trend.png).
    - Trend max is 13.4 °C during a 22 min alarm with Home at 16.4 °C (screens/alarm-temp-high__trend.png).
    - "0.94 %/day" fits neither 18.2 % nor 31.4 % over 14 d 3 h (screens/normal__cure.png, screens/open-ended-phase__cure.png).
    - Likely fixture artefacts, but no test catches them, and these are the shots used to judge trust. The real-core trend (realcore/realcore__trend.png) does agree with its Home value, though it has no scale or weight to check.

15. **Defrost can present an out-of-limit temperature as fine.**
    - 16.1 °C, which is +3.1 against a +2.0 limit, shows in white digits under a green banner, with no time limit or time remaining (screens/defrost__home.png).
    - A stuck defrost would keep looking normal until an alarm, which is not shown.

16. **The banner names only one problem.**
    - stale-air-weight names airflow only (screens/stale-air-weight__home.png), all-bad names temperature only, and extreme-values omits humidity at +22.
    - On alarm-many, "Temperature high +4" means four more alarms, but looks like the +3.4 deviation next to it (screens/alarm-many__home.png).

17. **Digit color does not rank severity.**
    - The same amber is used for a no-alarm drift (15.6), a critical alarm (16.4) and an over-absolute-limit move-product-now event (21.3).
    - At 2 m only the banner separates them.
    - Files: screens/drift-no-alarm__home.png, rig/alarm.png, screens/alarm-abs-temp__home.png.

18. **The real-core run shows an unexplained idle output.**
    - Temperature is 2.6 below set with COOL off and HEAT off, and no hold reason is shown (realcore/realcore__home.png). Version reads "--" (realcore/realcore__health.png).
    - It may be legitimate, but the panel gives no reason, while it does explain compressor holds elsewhere.

19. **The maintenance reminder has no lifecycle.**
    - There is no interval, hours count, or "done" action, and no installer screen for it.
    - ACK is the only control shown (screens/alarm-many__alarms-p3.png, screens/normal__installer.png).

20. **Secondary text is unusable at distance.**
    - The running recipe line, loss bar text, output chips and setpoints are about 2 to 2.5 mm tall. "15 d 21 h left" shrinks further under an alarm banner.
    - Long alarm instructions drop to about 14 px (screens/long-text__home.png).
    - "LOSS 18.2 % of 35" reads as a percentage of 35, and its text straddles the bar edge.
    - Tiles shift by 48 px when a banner appears (31-targets-home.txt vs 31-targets-alarm-home.txt).

21. **A hung display wedges service stop.**
    - SIGTERM at 02:54:24 was not honored. SIGKILL came 91 s later at 02:55:55 (51-rig-walk-journal.txt lines 16 to 25).
    - The notice tells the operator to restart the controller, so the wait matters.

22. **Trend is weak for spotting a bad night.**
    - The 7 d view aliases into a solid block (screens/normal__trend-7d.png).
    - Auto-scale makes ±0.4 °C look violent.
    - The setpoint is drawn, but alarm limits are not.

23. **Minor inconsistencies.**
    - Timestamps in history carry no untrusted marker while the header shows "~14:22:05?" (screens/clock-untrusted__history.png).
    - Calibrate and Start Defrost are single-tap, while Tare needs a hold (screens/idle-manual__installer-calweight-confirm.png, screens/normal__defrost-confirm.png).
    - The known-mass keypad header says CODE (screens/idle-manual__installer-calweight.png).
    - Log step 98 records a tap at x=202 (SET's position) as tap:nav.home (30-walkthrough.tsv).

24. **Evidence gaps to close.**
    - Limits page 3 and history page 3.
    - Locked Alarms and Trend, and any alarm while locked.
    - Stalled Cure and Trend.
    - Drift or stall during sleep.
    - Installer session after timeout, and wrong-code lockout.
    - Setpoint edit during pause.
    - 10-render-audit.txt is 0 bytes, which cannot be told apart from "audit did not run". screens/INDEX.tsv omits the two notice screens.

## What held up
- On Home, a bad reading cannot pass as good: STALE, FAULT and SUSPECT replace the digits with hatching and text, confirmed on rig/stale.png and rig/fault.png.
- SIMULATED is unmistakable on every captured screen, including sleep.
- Hold-to-confirm for Stop, Next and End rejected short holds in the walkthrough.
- The panel's own frozen and stopped notices appeared on the board.
- Normal temperature digits at 15.7 mm are large enough for 2 m at full brightness, subject to checking the glass.
