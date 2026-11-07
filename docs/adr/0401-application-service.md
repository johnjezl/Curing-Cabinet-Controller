# ADR 0401: Application service, modes and restart policy

Status: accepted

## Modes
A real cabinet running a simulation would look alive while controlling nothing. The options for
selecting the simulated plant:
1. A `--sim` flag alone. One stray flag or a copied unit file is enough.
2. A separate binary. The two would drift, and evidence from one would say little about the other.
3. One binary, with the simulator behind several independent gates.

Decision: 3. `cured` with no arguments is real hardware, and `curing.service` passes none. `--sim` also
needs `--speed N`, `--sim-outage`, and `--sim-confirm=not-a-real-cabinet`. Anything missing, or any
stray argument, exits with status 2 and no fallback to either mode. Provisioning a real cabinet
(`provision-default`) writes `/etc/curing/production`, which makes `--sim` and `sim-wipe` refuse whatever
the command line says, and removes the simulator unit. On screen, the panel's simulated markings are
driven by `Health.Simulated`.

The simulator keeps its state in `/var/lib/curing-sim`, a different directory from the real
`/var/lib/curing`, so neither can pick up the other's cure.

## Choosing what starts at boot
A simulated cure has to survive a real power cut, so the simulator must be able to start at boot. That
is persistent system state, so it goes through `provision.sh --bench --sim-boot SPEED OUTAGE`
(`rig.sh provision-sim`). Any other provisioning run switches back to `curing.service`. The two units
conflict.

## Unit settings
- `Type=notify`. READY is sent after the first control tick, so "active" means control has run once.
- `WatchdogSec=10s`, pet only from the control loop's iteration through `core`. Outputs do not wait for
  it: the 3 s lease in ADR 0207 drops them first. 10 s is long against a 1 Hz loop with bounded I/O
  and short against the cabinet's thermal time constants. `TimeoutAbortSec=3s` bounds the goroutine dump
  that the default SIGABRT gives us in the journal.
- `Wants=`, not `Requires=`, on bring-up and the guard. Failed bring-up or a guard restart must not stop
  control and its reporting. The guard refuses unsafe commands on its own.
- `ExecStopPost=cured post-stop`, as the service user. It asks the guard for all-off and paints STOPPED.
  It deliberately does not call `curing-safe-outputs`: that uses pinctrl behind the guard's back, and a
  line changed that way would stop obeying the guard, so the next run's relays would silently not switch.
  With the guard dead there is nothing to do, since the kernel has stopped driving the lines.

## A clean stop has to be allowed to finish
`core.Close` turns outputs off first and then waits for its queue to reach the card. During a reboot
that took 16 and 20 s on the rig, measured, because `fsync` waits behind other shutdown work: the boot
record unit, 13 s in the same stop. With an early bound of 6 s the stop was cut short and core rightly
recorded it as unclean, so every graceful reboot produced a POWER_OUTAGE and counted toward the restart
loop. That was a defect here, and it is why these alarms appeared during panel evidence. The bound is now
45 s, with `TimeoutStopSec=60s`. It costs nothing for safety because outputs are already off.
`evidence/d/24*` shows reboots from first-boot state with neither alarm.

## Crash loop: `StartLimitAction`
ADR 0203 left this open. Options:
1. Stop trying after N failures. The cabinet then has no control at all, for a fault that might pass.
2. Reboot after N failures. A reboot is a soft reset, and soft resets have left the panel blank until a
   power cycle (ADR 0209), so this could take away the display as well. It also fixes little: a bad
   state file or a bug is still there afterwards.
3. Keep restarting every second, and raise the alarm.

Decision: 3, with `StartLimitIntervalSec=0`. The alarm relay cannot pull in until control has run for
15 s (ADR 0402), so a fast loop keeps the contact open with no help from the software in the loop. A
loop slower than that is caught by `CONTROLLER_RESTART_LOOP`: 4 starts in 10 minutes inside one boot,
counted on tmpfs, cleared after 30 minutes of one process running. A deliberate stop resets the count.

## Output write errors restart the service
core withholds the watchdog pet while output writes keep failing. In a simulated cure an injected
write error lasting past the watchdog limit restarted the service that way (`evidence/d/power-cut-cure-full`).
That is core's choice; this unit's part is the 10 s limit and the restart.

## What this does not do
- **A kernel hang freezes the panel with old readings.** Nothing runs to paint a notice, so only the
  moving bar and clock stopping shows it (ADR 0309), until the hardware watchdog resets the board. That
  path is itself not proven (ADR 0203). The alarm contact also holds through this window (ADR 0402).
- A hang that keeps `core.Step` returning is not detected here. The watchdog proves iteration, not sense.
- A panic in a goroutine the panel starts internally, outside its recovers, would take down the process.
  That is the restart path, not a lost cabinet.
