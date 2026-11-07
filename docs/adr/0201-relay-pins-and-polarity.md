# ADR 0201: Relay pins, polarity and crash behavior

Status: accepted

## Context
Relays switch a heater, a compressor and a humidifier. They must de-energize through
a crash or reboot. The rig probe shows that at reset GPIO 0 to 8 pull up and GPIO 9 and
above pull down.

## Options
1. Active low, any pin. Common cheap modules. Pull-up pins would hold them off at reset,
   but the safe state then depends on the module's behavior when the line floats.
2. Active high on GPIO 9 and above. The reset pull-down and the driven-off state are the
   same level, so reset, crash and unplugged-cable behaviour agree.
3. A configurable polarity. Flexible, and one wrong setting energizes the heater at boot.

## Decision
Option 2, with no polarity setting. `Config.Validate` rejects a relay below GPIO 9.
docs/wiring.md requires a high-level trigger module.

## The crash case
A released RP1 line can keep driving: the kernel module `pinctrl_rp1` defaults to
`persist_gpio_outputs=Y`. Options:
1. Depend on systemd `ExecStopPost=` to reset pins. Runs only if systemd is healthy and
   only after the stop sequence.
2. Set `persist_gpio_outputs=N` at boot, so the kernel stops driving as soon as the file
   descriptor closes, with no helper process.
3. A separate supervisor process holding the lines. A new failure mode for no gain.

Chosen: 2, set by `curing-hw-init` before anything claims a line. `curing-outputd`, which owns
the lines (ADR 0207), also verifies the setting at startup and every second. While it reads
anything but N, or cannot be read, the guard turns everything off and refuses to turn anything
on. `curing-safe-outputs` is `ExecStopPost=` on the guard, as a second layer. Lines are requested
with a pull-down and start low.

Evidence: the guard-killed rows of `evidence/b/28-time-to-safe-relays-and-fan-test-driver.txt`,
`29-unsafe-kernel-param-refused.txt`, and the one-off control run
`static-control-persist-Y.txt`, from an earlier build, with the setting reversed.

## Consequences
- Relay modules are limited to high-level trigger types.
- A kernel panic or hang leaves lines as they were until the hardware watchdog resets the
  board (ADR 0203). The heater's independent thermal cutout covers that window.
- Each output is its own line request, so one bad line cannot stop `AllOff` from turning off
  the rest.
