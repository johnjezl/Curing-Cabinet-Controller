# ADR 0209: Panel missing after some soft resets

Status: accepted. Runtime fallback in place, proven on 2 blank boots; not yet proven after a
watchdog-only reset.

## What happens
On some soft resets the firmware's `display_auto_detect` misses the panel. Those boots have no
`Loaded overlay 'vc4-kms-dsi-7inch'` in the firmware log (`vclog --msg`), `dsi@110000` (DSI0) has
`status=disabled`, and there is no DSI bridge, panel node or touch controller. Nothing in the kernel log
reports an error. The panel stays blank until a power cycle, which has always restored it. Why
detection misses is unknown.

## Data
- Builder D saw it on most graceful reboots on one day. That capture is D's.
- In my runs (all evidence under `evidence/b/`):
  - 3 consecutive blank boots in an evidence run.
  - 0 of 10 in a first bounded experiment (`static-panel-experiment.txt`).
  - 2 of 20 in a second (`static-panel-experiment-2.txt`): attempt 6 of 6 graceful reboots, then 1 of 7
    graceful reboots and 0 of 7 watchdog-only resets after the fallback was installed.
- The rate varies a lot between sessions. No firmware log line explains a miss.

## What a good boot applies
The firmware log shows `Loaded overlay 'vc4-kms-dsi-7inch'`. The live tree puts the bridge
(`toshiba,tc358762`) under `dsi@110000`, which the tree's own symbols call `dsi0`, with the touch
controller and panel regulator on `i2c@88000`. So the runtime equivalent is
`dtoverlay vc4-kms-dsi-7inch dsi0`.

## Options
1. Set the overlay in config.txt, so detection is not needed. Forbidden by CONSTRAINTS.md.
2. Load the same overlay at runtime on a boot where the firmware did not.
3. Alarm only.

## Decision
2, with 3 kept.

`curing-hw-init` checks the device tree, not a timer. If DSI0 is `disabled` and no panel overlay is
loaded, it logs a warning, runs `dtoverlay vc4-kms-dsi-7inch dsi0`, and records `/run/curing-panel-overlay`.
It then waits up to 20 s for `/dev/fb0` and the touch device, and unbinds fbcon as usual.
`/run/curing/display-status` gains a `panel_source=firmware|runtime-overlay` line.
`/run/curing/display-missing` is still written whenever the framebuffer or touch is absent after that wait.

## Proof
- **By hand, on a blank boot (attempt 6):** `rig.sh panel-overlay vc4-kms-dsi-7inch dsi0` brought up
  `/dev/fb0` and the ft5x06 touch device within about a second. The running application found the
  framebuffer on its next 5 s retry and painted without a restart
  (`static-panel-after-runtime-overlay.png`). It still showed the DISPLAY_MISSING alarm, because the
  marker had been written before the overlay was loaded. With the fallback in `curing-hw-init` the
  marker is written after it.
- **Automatically, on the next blank boot (attempt 3 after installing it):** the firmware log had no
  panel overlay, `panel_source=runtime-overlay`, `fb0=yes touch=yes`, and no missing marker.
- **Not proven:** a third blank boot, and any blank boot after a watchdog-only reset. The 20-reboot
  budget ran out with none occurring after watchdog resets (0 of 7).

## Consequences
- The fallback depends on the stock `vc4-kms-dsi-7inch` overlay and on this panel's wiring to DSI0.
  A different panel or port needs a different overlay.
- If the runtime overlay fails, the panel stays blank, the missing marker is written, and a power
  cycle is still the only known remedy.
- `panel_source=runtime-overlay` lets the application note that firmware detection failed on this boot.
