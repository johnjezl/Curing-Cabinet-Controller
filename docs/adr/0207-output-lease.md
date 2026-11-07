# ADR 0207: Outputs drop on their own when commands stop

Status: accepted

## Context
A relay line stays on as long as the process holding it is alive. A control loop that hangs in
a live process would hold the heater on, and the systemd watchdog would be its only way out,
at a timeout part D chooses, by restarting a whole service. Nothing at this layer protected
against it. The rule needed: an output stays on only while something proves control is alive,
and the proof must not depend on the thing being checked.

The rig has no relays, no probe and no labctl serial console for this board.

## Options
1. A timer goroutine in the application. Catches a stuck control goroutine, but freezes with
   the process (SIGSTOP, a stopped-the-world runtime, D-state I/O in some cases).
2. A separate guard process that owns the lines and needs a renewed lease. Catches all of those,
   and works however the application fails. Costs one small process and a local socket, and
   departs from ADR 0002's single binary for this one job.
3. A dead-man program on RP1 PIO. It would keep working through a kernel panic, which is the one
   case 2 cannot cover. It needs consecutive pins, an undocumented ioctl interface from Go and
   firmware behaviour nothing here could verify, with no way to observe the result in a panic.
4. External hardware, a retriggerable one-shot gating the relay supply. The strongest answer, and
   impossible to build or test on a bench with no hardware. Recommended as an option in
   docs/wiring.md.

## Decision
Option 2: `cmd/curing-outputd`, its own service, a separate process, and the only owner of the
relay lines. The application's `hal.Outputs` is a client of it.

- **Lease: 3 s.** Set, AllOff and renew requests extend it. A state query does not.
- **Renewal comes from `Watchdog.Pet`**, which `core` calls at the end of each healthy iteration.
  `core` only calls `Set` on a change, so `Set` alone could not be the signal. A background
  renewer in the application is deliberately not provided: it would keep running when the loop hung.
- **Expiry turns every output off**, logged at critical priority so journald syncs it. Outputs
  stay off until commanded again. Renewal alone does not restore them, since re-energizing a
  compressor on a recovering loop could short-cycle it. `Pet` returns `hal.ErrOutputsDropped`.
- **A controlling client's socket closing** drops at once, without waiting for the lease.
- **A hung guard** stops petting systemd, which kills it with `WatchdogSec=2s` and
  `WatchdogSignal=SIGKILL`. Its death makes the kernel stop driving the lines (ADR 0201).
- **Failing to turn outputs off** makes the guard exit, for the same reason.
- **Failed bring-up** stops the guard from starting (`Requires=curing-hw.service`), so nothing
  owns the lines. The unit dependency is configuration; this failure has not been exercised.

3 s is short next to any cabinet's thermal time constant and long next to a loop that runs about
once a second. Whether real loop stalls ever reach it is unknown until the system runs for real;
a false drop is visible through `ErrOutputsDropped` and the guard's journal line.

## What this does not do
- **It does not judge validity.** A live loop commanding the heater on with no valid temperature
  is still obeyed. That decision is `core`'s; this layer has no sensor knowledge. The guard also does
  not forbid combinations, because `core` legitimately runs heat with cool during dehumidify.
- **It does not help in a kernel panic or hang.** Nothing on the board can run then. The board
  reset is the only release, so `kernel.panic=1` makes it as early as possible, with the hardware
  watchdog behind it. The independent thermal cutout covers this window.

## Measurement
`rig.sh hwcheck time-to-safe` runs a labelled test driver, then a separate process samples the RP1
status registers in a tight loop (`max_gap_us` is in each row) and times each line's pad going low.
These numbers come from registers on the same board with no relay attached.

The panic numbers are a bracket on the dev host's clock. The last ping reply marks the old kernel's
end. The new kernel's start comes from its uptime. No instrument sees the lines between those points,
so the moment they drop is not measured; `first_line_read` is the earliest software proof.
Exact values are in `evidence/b/28-...`, `40-...` and `43-...`. Typical values, all with a test
driver holding all six lines on and no relay attached:

| Failure | All lines low after |
|---|---|
| Control loop stuck, process alive | about 2.8 s (3 s lease less up to 0.2 s since the last renewal) |
| Control process frozen by SIGSTOP | about 2.8 s |
| Control process killed | 0.25 ms typically, 3.6 ms the slowest seen |
| Guard frozen by SIGSTOP | 2.1 to 2.2 s |
| Guard killed | about 8 ms |
| Kernel panic, `kernel.panic=1` | new kernel started 9.3 to 9.6 s after the last ping; lines first read low 43 to 51 s after it |
| Panic with `kernel.panic=0`, meant to leave only the watchdog | new kernel started 8.2 to 18.7 s after the last ping (ADR 0203 on the spread); lines first read low 46 to 54 s after it |
| Power cut, 3 s off | new kernel started about 17.4 s after the last ping |
| Power cut, 60 s off | new kernel started about 73.6 s after the last ping |

The panic rows are upper bounds, not times to safe. The lines drop somewhere before the new kernel
starts if the reset also resets RP1, which is inferred, and proven only by the first read.

The panic tests use `curing-rig-test`, which can panic the kernel and flip the safety parameter.
`provision.sh` installs it only with `--bench` and otherwise removes it. `rig.sh provision` passes
that flag, for this bench rig; `rig.sh provision-default` is the real-cabinet path.

## Consequences
- Part D must build with `drivers.Open` and hand its Watchdog to `core`. With no Watchdog, outputs
  drop 3 s after the last command, which is safe but obvious.
- `core` should treat `hal.ErrOutputsDropped` as outputs off: clear its applied map and restart its
  off-timers. Until then its 30 s reassert restores outputs, which could be sooner than a
  compressor's minimum off time.
- The application cannot switch relays while the guard is restarting (under a second, measured
  as `guard=` in the captures); `Set` returns `ErrAbsent` and outputs are off.
