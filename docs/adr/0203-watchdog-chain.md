# ADR 0203: Watchdog chain

Status: accepted

## Context
hal.Watchdog is pet by the control loop. PID 1 already owns `/dev/watchdog0` (the rig had
`RuntimeWatchdogUSec=1min`), and a device has one opener.

## Options
1. The application opens `/dev/watchdog0` directly. Needs root or a device permission
   change and conflicts with PID 1. If the process exits uncleanly the board resets, so any
   app crash costs a full reboot.
2. A chain: the control loop notifies systemd, systemd watches the unit (`WatchdogSec=`) and
   restarts the service, and PID 1 keeps the hardware watchdog.
3. A separate watchdog daemon. Adds a process with no extra coverage.

## Decision
Option 2. `drivers.SystemdWatchdog` sends `WATCHDOG=1` to `NOTIFY_SOCKET`; without a socket,
`Pet` returns `hal.ErrAbsent`. `RuntimeWatchdogSec=10s`.

1 min was too long and also misleading. wdctl on the rig reported a 60 s timeout and 15 s
left: the BCM2835 hardware counts to about 15 s, so a longer value is emulated by a kernel
worker. 10 s is within the hardware limit. `kernel.panic_on_oops=1` turns an oops into a
panic. `kernel.panic=1` resets the board 1 s after a panic. The hardware watchdog then covers
what never reaches the panic path, such as a hard hang.

This chain is about restarting. Turning outputs off faster than any of it is ADR 0207.

## Consequences
- Part D must set `Type=notify`, `WatchdogSec=` and `Restart=` in its unit.
- `rig.sh crash-test panic` and `crash-test halt` measure both paths. `halt` sets
  `kernel.panic=0` for that boot only, through the provisioned helper `curing-rig-test`, so only
  the hardware watchdog should be able to reset the board. That attribution is an inference: the
  bootloader's `rsts` value reads 0x20 after every soft reset seen here and does not name the
  cause, and the panic text is not captured. Over four runs each, the new kernel started 9.3 to 9.6 s after the last ping on the
  `panic` path, and 16.1, 17.8, 18.7 and 8.2 s on the `halt` path. Three of those fit a longer wait.
  The 8.2 s run does not: it is shorter than a 10 s watchdog should allow unless the timer was
  already well along and the boot was quick, and it is unexplained. So the hardware watchdog path
  is not proven by these runs. `rsts` reads 0x1000 after a power cut, so it does separate that case.
- A service in a restart loop keeps the board up; part D decides on `StartLimitAction`.
