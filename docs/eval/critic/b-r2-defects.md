# Defects: hardware-driver and provisioning layer, largest first

Run audited: evidence/b/ at 02:41:37Z-02:52:58Z, 681 s, exit 0. Paths below are relative to evidence/b/. Rig-probe output was taken after the run with the permitted read-only subcommands.

1. **Nothing real was ever attached, so no reading path has run on real hardware.**
   - 26 and 45: every temperature/humidity, airflow and load-cell line is is_ErrAbsent=true. `grep is_ErrBadRead=true` over all files gives 0 hits.
   - 27's own name says "no-fan-no-instrument". 28 line 4 says "no relays attached". 30 says "nothing-attached".
   - Decode, scaling, resolution, accuracy, relay polarity, fan response and tach have never met a real part. Sensor overlay and module are `dht11` (22 line "1: dht11 gpiopin=4") while the hardware list names a different part. Relay polarity is an unverified assumption: if a real board is active-low, every "safe low" figure here means energized.
   - The 81 passing tests (01, 0 FAIL, 0 SKIP) use fakes and print only names and PASS, with no thresholds or values.

2. **No control application comes back after any reset.**
   - 22, 42, 44: `curing.service: enabled=not-found`. static-power-cut-3s and -60s: same.
   - 51 ("application-state-at-end"): "Unit curing.service could not be found".
   - The binary is installed (13) with nothing to start it. After the run the application shows `enabled=static` with start time 02:56:54, about 4 min after the run ended and while another agent's script was running, not at boot.
   - After a power blip this evidence shows a board with lines low, fan at 0 and nothing regulating, for an unbounded time. If autostart lives in another layer, this layer's evidence never shows it.

3. **Alarm and buzzer lines fall with everything else when control fails.**
   - 28: all_lines_low on all 15 runs includes `alarm` and `buzzer`. 01: TestLeaseExpiryDropsEveryOutput. 40, 43 and both power-cut files: alarm out=0 after reset.
   - The failure that needs annunciation silences the annunciator. No capture shows any alert that survives control death: no energize-to-run alarm, independent alarm path or remote notice.

4. **The fan is outside the safety mechanism, in both directions.**
   - 28: fan_after is about 25 kHz, 50 % in 15 of 15 runs, including both guard-death scenarios. Nothing in the captures shows duty ever dropping after the process dies, though only a short window after each event is sampled.
   - After any reset the fan is at duty 0 and stays there (22 PWM duty_cycle=0; 40 and 43 rising_edges=0), so airflow stops with no recovery shown.
   - Fan stop detection takes more than 5 s: 26 RPM(t=0s) value=0 err=nil at 50 % duty, then "no tach pulses" only at t=5.5 s.

5. **Line state during every kernel-level failure is unobserved, and the file names imply otherwise.**
   - 40 and 43 state "nothing on the rig observes the lines between these points".
   - First looks: 43.4 s and 41.4 s after last sign (40, 43); 54.6 s and 107.6 s after the last ping (static-power-cut files).
   - Lines were high before each event. How long they stayed high, and whether they glitch at bring-up (first read at uptime 33-37 s, 24 and 40), is not shown. In 43 the kernel may have been dead with heat commanded on for about 10 s.
   - No external instrument exists anywhere. All figures in 28 come from the same board reading its own registers.

6. **Capture 43's name is not shown by its content.**
   - Named "panic-only-hw-watchdog-can-reset", yet line 13 prints `kernel.panic = 1`, identical to 40.
   - rsts=00000020 is the same as for panic and graceful reboot (40; records 2-9). Watchdog BOOT-STATUS reads 0 (probe-hw, 22).
   - old_kernel_alive_after_command_s=9.708: the old kernel answered pings for 9.7 s after the panic command, which contradicts an immediate panic. New kernel came 8.2 s after last sign, shorter than the 10 s watchdog timeout counted from last sign.
   - Total 17.9 s from command fits a watchdog reset only if the panic happened at the command, which the pings contradict. No record says the watchdog fired. It is unproven that the hardware watchdog has ever reset this board.

7. **Clock is lost on power cut, with no battery shown, and the system then lies about time.**
   - static-power-cut-3s/-60s: `setting system clock to 1970-01-01T00:00:12`. Records 10, 27, 28, 32, 33: rtc=45-50.
   - Record 32 is stamped 02:34:47 and record 33 is stamped 02:37:15, while the cuts were at 02:35:07 and 02:37:30 (host clock). Stamps are 20 s and 15 s before the outage itself, and 76 s and 124 s behind real time when written.
   - Service `since` times precede kernel start (3 s capture: since=02:34:48, boot 02:35:25).
   - Backward step in 43's last entries: 02:50:22.79 followed by 02:50:17.42 "Initial clock synchronization".
   - Network time was available in all runs. A cut with no network leaves the clock behind by the outage, permanently; that is INFERRED and untested. Any durations, dwell or weight logs across an outage are unreliable.

8. **Crash records do not capture the crash.**
   - 50 lines 113, 144, 175, and a separate `rig.sh records 1`: section "previous boot, last entries" is empty in every record shown.
   - No panic text appears: grep finds no "panic", "Oops" or "sysrq" anywhere in 50. `/sys/fs/pstore` is empty after a panic (probe-hw) although pstore is configured.
   - Records show a class and uptime only, with cause not distinguishable. rsts=0x20 covers reboot, panic and watchdog. rsts=0x1000 is sticky into the next graceful reboot (22 rsts=00001020; records 11, 29, 34).
   - ntp=no in 35 of 36 records because they are written before sync, and they are never corrected.

9. **Log durability is weak on this filesystem.**
   - 40: last journal entry is 9 s before command; 43: 13 s; static 3 s: 9.7 s. Static 60 s: last entry stamped 113 s before the cut, and the capture's own session at 02:37:13 (17 s before the cut) is absent. hwstate shows SyncIntervalSec=5m.
   - 50: 127.5 MB against a 128 MB cap, with the oldest file dated 01:35, about 77 min of retained logs. 14 of 18 journal files are `.journal~`, renamed after corruption or unclean shutdown. Each unclean boot retires a whole 8 MB file.
   - Each failed sensor read logs a kernel line (`Only 0 signal edges`, 50). Retention over weeks with a dead sensor is not shown.

10. **Storage integrity after unclean resets is not checked.**
    - Root is `rw,noatime` on a 7.29 GiB SD card (probe-hw); 22 unclean boots so far (50).
    - probe-hw: `EXT4-fs (mmcblk0p2): orphan cleanup on readonly fs` at 17.9 s. Unclean boots start the init system about 17 s later than clean ones (19-20 s vs 2.68 s, 22 vs 42/44).
    - No fsck output, write-endurance figure or corruption test exists, and no capture re-reads configuration or data after a cut. Hashes are compared around the panics (13 vs 90) but no post-cut hash is captured in the static power-cut files.

11. **Evidence pass/fail flags are close to meaningless.**
    - 51 prints "could not be found" with `errors_in_output=0`. 40 and 43 print "Timeout, server ... not responding" and rc=255 with `errors_in_output=0`. 26 and 45 are full of `err=` with 0.
    - 99 `failed=0` only means commands exited 0. No capture asserts a threshold.

12. **Power-cut captures have a false timing field and thin sample.**
    - static-power-cut-3s/-60s: `command_sent` equals `last_ping_before_gap` to the millisecond (1794018907.700, 1794019050.472), so `old_kernel_alive_after_command_s=0.000` is an artifact, not a measurement.
    - The 3 s and 60 s labels are not verifiable from content: new kernel at 17.4 s and 73.6 s after last ping. Compared with about 9 s from reset to kernel in 40, the 3 s cut looks like about 8 s off.
    - n=1 each, not regenerated by the script. Clean cuts only; no brownout, bounce or cut during a write.

13. **Time-to-safe figures are small-sample and start from an undefined zero.**
    - 28: 2808-2848 ms against an expected "lease 3 s", so the clock apparently starts about 150-190 ms after the last renewal (INFERRED) and the worst case from the last renewal is not stated.
    - n=3 per scenario on an idle board. No CPU, I/O or memory stress. guard_frozen holds lines high for 2.1-2.2 s.
    - Guard restart limit not shown: 29 reaches restart counter 6, equal to the 6 injected faults, and stops. Behavior on continued crash looping is unknown.
    - Scenarios are test-driver-driven, per the file header. The real control process was never the client.

14. **Rig tests ran on bench provisioning, not the deliverable state.**
    - 14 removes the helper once; 15 reinstalls it immediately; all reboots, panics and power cuts ran with `curing-rig-test` present. It is still present afterward (bench-helper-state, 90).
    - Default provisioning has no idempotence run and no reboot run. Provisioning changed=0 is only shown after a graceful reboot (23), not after panic or power loss.
    - 21 and 41 `sudo -n true rc=0` show passwordless root for the login user on the system being rated. kernel.sysrq=438 (probe-hw).

15. **Guard may be bypassable by the unprivileged user (INFERRED).**
    - 22: the service user is in group `gpio`. /dev/gpiomem0 and gpiochip0 are group-writable, and the PWM sysfs files are `rw-rw-r-- root gpio`.
    - 26: hwcheck runs as uid=999 and reads RP1 registers directly. Nothing shows a write being refused. A control-process bug could drive lines without the lease.

16. **No duration evidence.**
    - The longest uptime in the run is under 6 min; the largest in the records is 1152 s (record 1, uptime when written).
    - Weeks unattended is not exercised: no soak, wear, drift, memory growth or log-fill measurement.

17. **Tight bounds and misleading values.**
    - 26: first climate read 450.7 of 500 ms, HX711 190.6 of 200 ms on an idle board.
    - 26: returns numeric zeros with errors (`value=0.0C/0.0%/age0.0s`, RPM 0). Only the caller's error check keeps 0 C from being used.
    - TestClimateReadDoesNotWaitOnASlowSensor (0.45 s) sits beside a first read that blocks 450 ms on the board.

18. **Display check cannot detect later loss.**
    - 29: DisplayMissing detail carries `uptime=16.85` 3 min after boot, so it is a boot-time marker and not a live check.
    - probe-hw dmesg shows `[drm] Cannot find any crtc or sizes` at 27.99 s with the marker saying fb0=yes touch=yes.
    - static-panel-experiment's repeated "no bad boot in this sequence" implies a known intermittent fault hunted over 10 attempts and not reproduced. Those attempts did not include a power cut.

19. **PWM at 100 % is not DC.**
    - 27: set_duty=100 shows rising_edges=987, register_hz=511, duty 99.95. Inferred as a brief low pulse each period seen by sampling. Effect on a real fan is unknown.

20. **Graceful reboot gap unexplained.**
    - 24: service stop at 02:42:23, then new kernel 02:43:11 (22), 48 s, against about 9 s firmware-to-kernel after a panic. Line state in that window is unobserved.

21. **Tested code is not committed.**
    - 00: HEAD 05f449e, with cmd/, deploy/, internal/ all `??`. Only hashes identify the code under test.

## Static files
Hashes are unchanged by this run.
- `static-control-persist-Y.txt` 8b6424b9...: 15:34 PST, predates the guard binary (02:10Z, 13). No source hash. Before-state oe=0 differs from this run's oe=1, so it is a different system configuration. Does not match this run.
- `static-panel-experiment.txt` d366ccd4...: ends 02:08Z, predates the 02:10Z display-status and hw-init install. Uses touch=1 where this run uses touch=yes. No source hash. Does not match this run.
- `static-power-cut-3s.txt` a0787913... and `static-power-cut-60s.txt` 7827676e...: combined source hash a737b298... matches 00a. Boot ids match records 32 and 33. Installed hashes match 13 except the application binary (c402958c... vs ca7da600..., not under test here). hwcheck mtime differs, content hash same. Matches this run.

## Rig interference
- None seen in the captures.
- Boot ids run 33 -> 34 -> 35 -> 36, exactly the three injected reboots. Guard restart counter 6 equals 6 injected faults. Binary hashes in 13 and 90 match.
- Another agent's evidence script was blocked on the rig lock (`flock -w 900`) for the whole run, observed by process listing at 02:52Z. It ran after the lock was released: the application service appeared at 02:56:54, after this run ended.
- The application binary was redeployed at 02:41:29, 8 s before this run started (13).
