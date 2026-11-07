# ADR 0202: Enabling I2C, PWM and DHT without touching /boot/firmware

Status: accepted

## Context
config.txt and cmdline.txt must not change. The rig has no `/dev/i2c-*`, and RP1 PWM0 (the
block that reaches GPIO 12, 13, 18 and 19) is disabled in the device tree. The kernel has
`CONFIG_OF_CONFIGFS=y`, and `dtoverlay` loads overlays through it at runtime.

## Options
1. Runtime overlays from a boot-time systemd oneshot, using stock overlays where they fit.
2. Userspace only: bit-bang I2C and software PWM from GPIO. No stable 25 kHz, and more CPU.
3. Use RP1's PIO. More moving parts for what fixed hardware blocks already do.

## Decision
Option 1, in `curing-hw.service`, which runs `/opt/curing/libexec/curing-hw-init`:
- I2C: stock `i2c1-pi5` (GPIO 2 and 3, 100 kHz) and `modprobe i2c-dev`. Gives `/dev/i2c-1`.
- DHT: stock `dht11 gpiopin=4`.
- PWM: our own `deploy/files/curing-fan.dts`, compiled by provisioning. The stock `pwm` and
  `pwm-2chan` overlays use `brcm,pins` on the BCM283x `pwm` node and have no bcm2712 mapping
  in overlay_map. Decompiled from the rig, not assumed; `rig.sh probe-overlays` shows them.

`pwmchip0` on the rig is RP1 PWM1 and runs the board's own fan header (`cooling_fan` on
channel 3), so it must not be used. PWM0 appears as a later chip, found by device path.

The unit runs in multi-user boot rather than early sysinit, because it needs udev-loaded
drivers including the panel for the fbcon step, and because nothing else depends on it
earlier: relay lines are already safe from hardware reset.

## Evidence
`evidence/b/21-state-after-reboot.txt` and `25-pwm-measured-at-pin.txt`. Every option tried
first worked, so no fallback was needed.

## Consequences
- These devices do not exist until `curing-hw.service` finishes, so the application unit
  orders itself after it.
- A pin change means editing the overlay parameters as well as `drivers.Config`.
- Device tree bring-up depends on the `dtoverlay` and `dtc` packages on the image.
