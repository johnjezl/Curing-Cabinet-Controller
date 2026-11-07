# Defects: hardware-driver and provisioning layer

File references are to evidence/b/ from the 00:31-00:36Z run unless marked prev-run. prev-run is the complete earlier run (00:04:22Z finish), copied to the critic/prev-run/ directory beside this file; the coordinator told me to treat it as partial, but it finished with exit 0 and failed=0, so I only use it where noted.

Verdict: I would not trust a batch to this. The evidence shows a bare board in a few failure paths. It does not show one real sensor reading, one real relay switching, or the control path acting on a fault.

## Blocking

1. **No real sensor has ever been read.** Every climate result is value=0.0C/0.0%/age0.0s with an error (24:4-8, 32:4-8). Accuracy, resolution, kernel-driver compatibility with the fitted sensor and real read failure rate are all unknown. 63 passing tests use fake devices (01:129-130).

2. **Sensor loss and recovery were never exercised.** The sensor is absent from boot, so a good-to-lost transition never happens on the board, and neither does recovery. The test names show nothing about temperature or humidity recovery (01:3-34). Both 24 and 32 only show the permanently absent case.

3. **Outputs stay on with no valid temperature, and the driver allows every output on at once.** 26:12-17 and 30:6-11 show heat, cool, humidify, dehumidify, alarm and buzzer all out=1, oe=1 with no sensor attached. No capture shows outputs dropping because a reading was lost. If that interlock exists elsewhere, it is not shown at this layer.

4. **No installed control service, no restart, no process watchdog visible.** 30a:73 shows the only application start: `systemd-run --unit=... '--description=... (transient, no unit installed)'`, with no Restart=, WatchdogSec= or Type=notify on the command line. After a power cut, nothing in the evidence shows control resuming. An unattended cabinet would sit with everything off, and no capture shows any alert for it.

5. **A hung control process is untested, and the hardware watchdog would not obviously catch it.** The only crash cases are SIGKILL and a kernel panic. 24:25 and 32:25 show the process watchdog called only with no socket, returning absent. The hardware watchdog is driven by PID 1 (21:97-110). A live, stuck process holding heat on is the worst case and has no evidence at all.

6. **Fan output is missing from every crash and reset path.** 26 and 30 cover relay lines only. The fan PWM line is unpulled (21:84, `pn`) and nothing measures it during boot, or across kill, panic or power cut. Fan behavior with its input floating or at last duty is unknown.

7. **Real relays were never attached, so "off" is a register state.** After SIGKILL the lines read oe=0, pad_in=0 while out=1 stays latched (26:20-25). With the line floating on a 0x56 pad and a weak internal pull-down, an external module's input bias decides what the relay does, and module polarity is unproven. An active-low or pull-up input module could energize. The latched out=1 also needs proof that the next claim cannot glitch high.

## Serious

8. **Time to safe on panic is not measured.** 30:21 gives the panic time and the first line read is about 00:36:06 (30:28, 30:35-46), so the bound is about 70 s. Between those points nothing observes the lines, and no external probe or serial trace exists. Off-at-next-read is being presented next to a 10 s watchdog figure, but the measured number is 70 s.

9. **File name 30 claims a watchdog reset the content does not show.** "kernel-panic-watchdog-reset" has no recorded reset cause: BOOT-STATUS is 0 for all flags (31:103-106) and there is no panic text (30:30-33 end at the sudo line). kernel.panic = 0 (31:95) makes the watchdog the likely cause, but only by inference. Kernel start lands about 24 s after the panic, against a 10 s timeout, and the difference is unexplained.

10. **Panic timing is inconsistent between runs.** In prev-run, the panic was due at about 00:02:23 (prev-run/30:21), yet kernel messages are stored at 00:02:24 and 00:02:28 for that boot (prev-run/30:32-33). The kernel was alive at least 5 s after the stated panic moment, so any time derived from the "panic in 1 s" line is unreliable by at least that much.

11. **The rig was not quiescent during evidence capture.** Something uploaded a new application binary at 00:32:51 and ran stop and then start of the application service at 00:32:55 (20a:48-62, 30a:71-73), after 10-service-stop reported inactive (10:3). None of the listed step commands is an application deploy. The same pattern recurs at 00:05:09 (20a:84-85), 47 s after prev-run's script had ended (20a:95-97 show the same origin and key). 24 captured at 00:33:02 ran at 00:33:22, a 20 s unexplained delay. In prev-run kernel sensor errors arrived during the crash step (prev-run/30:32-33), so something was polling the sensor. Whether the application was running during 24-32 cannot be determined, and neither step 21 nor 31 names it.

12. **Code under test cannot be identified.** 00:5 pins HEAD 05f449e, while 00:6-9 show all of deploy/, internal/drivers/, cmd/hwcheck and cmd/fbshot untracked. That commit contains none of the tested code. No capture hashes source or deployed binaries. Step 03 builds only the drivers and the check tool to /dev/null, and the deployed application binary (20a:35) has no build record here.

13. **Reset-path evidence for power loss is stale and thin.** Power loss is the most likely real event, and it has one 3 s cut (static-after-power-cycle.txt:1) that the script cannot regenerate. It predates this run by about 40 min, and the binary directory mtime differs (31:120 vs static:121), so it may not be the same binaries. The capture does not show outputs on before the cut, so it says little about any transition. No long outage, repeated cuts or brownout.

14. **The safe crash result rests on a single kernel parameter, whose failure path is untested.** static-control-persist-Y.txt:20-25 shows all outputs stay ON after SIGKILL with Y. Lines 21:93 and 31:93 show N only after bring-up has succeeded (21:16-18). Default value before bring-up, the case where bring-up fails, and a kernel update changing it are all not exercised. Every capture of the bring-up unit shows only Result=success.

15. **Clock is wrong after power loss, and time is never checked.** static-after-power-cycle.txt:6 reports boot at 23:50:09, while line 19 shows a unit active at 23:49:21, so the clock stepped at least 48 s. No capture holds clock-source, sync-status or RTC data (grep for these across evidence/b/ returns nothing). Fault timestamps early in a boot after an outage cannot be trusted, and a long or offline outage is unmeasured.

16. **Fault history is destroyed by the faults it should record.** Each panic renames the journal as corrupted (30a:140). That costs an 8 MiB file out of the 48 MiB cap: 30a:106-110 no longer has the 23:49 file present at 20a:115, and prev-run/20a:115-116 had 23:37 and 23:44, both now gone. The oldest record is now from 23:55, under 1 h old. Roughly 4 unclean resets evict everything older. SyncIntervalSec=5m (20a:138) can also lose up to 5 min of info-level logs. The panicked boot lost its panic text entirely, and no persistent crash store appears.

17. **Plausible-but-wrong readings are undetected at this layer, as far as evidence goes.** Tests cover only obvious corruption (01:7-8, 01:53-54, 01:69-72, 01:87-88). A single sensor provides temperature and humidity, with no second source, no stuck-value or drift test name, and no real-device data. A frozen or drifted value would likely be believed. Its failure is also common-mode for both quantities.

18. **"device absent" hides the fault type.** The kernel timeout is reported as absent (24:5-8), and test names suggest other mappings but only with fakes. On a real sensor, an intermittent timeout may be indistinguishable from an unplugged one. Reads after the first return in 0.003-0.016 ms (32:5-8), so those are cached repeats, not 4 independent results. age0.0s on an error tells the reader nothing about how old the failure is.

## Claims not matched by content

19. **"provision-first-run" (11) is not a first run.** The rig was already provisioned: /opt/curing dates from Nov 6 23:18 (20a:25). No capture provisions a fresh OS image.

20. **Provisioning is idempotent only back to back.** 12 shows changed=0 right after 11, yet 11:3-4 shows changed=2 in both runs: the state directory is changed and journald restarted (also prev-run/11:3-4). Something undoes state between runs, and journald restarts as a side effect.

21. **"pwm-measured-at-pin" (25) is self-measured with no load.** It reads the SoC's own register at about 1.03 MHz, with oe_fraction 1.000. It has no external instrument and no fan, and 100 % duty is not tested. The debug interface also says actual configuration disabled, 0/0 ns (21:68) while enable=1, so that interface cannot be used as a check either.

22. **"absent-with-no-sensors" (24, 32) cannot fail for a present device, and proves no sensors only by assertion.** Nothing independently confirms the wiring state. bound_ms and within_bound are self-declared by the tool. Exit 0 here means tool completion, not sensor health.

23. **"hx711-clock-high-time" (27) was measured on a pin with nothing attached.** max=10.0 us here vs 7.8 us in prev-run (prev-run/27:3), with over_50us=0 over 25000 pulses on one quiet short run. That is no evidence for weeks under load, and it never produced a load reading.

24. **"failed=0" (99) counts step exit codes only.** 20a:99-102 and 30a:90-93 contain permission and not-found errors under exit=0. deploy/rig.sh probe-hw also prints "Too many arguments" and exits 0 (my own run). The all-green summary carries little weight.

25. **Unlabeled state values.** 21:13-15 and 31:13-15 print running/enabled/active without naming a unit. 20a:166 prints a bare `inactive` the same way. The timestamp at 21:18 matches the bring-up unit, so the application service's state is not reported by any capture.

## Lesser

26. **Slow cold-path boot, with watchdog handover late.** systemd takes over the hardware watchdog at 19.77 s after a panic reset (31:110) and at 19.73 s after a power cut (static:111), against 2.68 s after graceful reboot (21:109). That 17 s is unexplained. Bring-up finishes about 35 s after kernel start after a panic (31:5, 31:18).

27. **Fan stopped and fan absent are conflated for a while.** RPM at 50 % duty returns 0 with no error at t=0 (24:20), and only the 5.5 s sample reports absent (24:21). The real detection time between those is unmeasured, and no real fan was tested.

28. **Passwordless root and writable SD root on the unattended unit.** NOPASSWD: ALL (20a:8-10), /etc/sudoers.d/nopasswd added Nov 6 23:31 (20a:21), and root mounted rw on SD (20a:37-38). No fsck, wear or read-only protection evidence, on a unit expected to take repeated unclean resets.

29. **Test output is not auditable.** 01 prints only names and PASS with no thresholds. 63 RUN, 63 PASS, 0 FAIL, 0 SKIP (under -race, 2.261 s) is clean but says nothing about values. Only drivers are tested, and no test covers the bring-up script or provisioning.

## Fresh and consistent

For balance, and so these are not re-audited: all 20 regenerated captures carry timestamps from 00:31:27Z to 00:36:09Z. Boot IDs change across both resets and match between 20, 21 and 30. /boot/firmware hashes match across every capture. Both static files are byte-identical before and after the rerun and are older one-offs (15:52 and 15:34 local), not regenerated. Relay-line values matched between the two runs.
