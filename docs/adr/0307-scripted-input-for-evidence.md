# ADR 0307: Scripted taps for evidence

Status: accepted

## Options
1. A debug socket in the production UI, disabled by a setting. It exists in the
   binary, and one misconfiguration exposes control.
2. uinput virtual device on the rig. Tests the most, but changes kernel state on the
   rig outside provisioning.
3. Injection only in cmd/uidemo, behind a build tag and a flag.

## Decision
Option 3. Headless, `uidemo walk` drives taps by coordinate against a fake clock and
a fake controller. On the rig, `-inject FIFO` and `-autotap` exist only with
`-tags demoinject` and only act if the flag is set. The production entry point
ui.Run takes no input other than the evdev reader, and internal/ui does not import
the fixtures or the harness: a test checks the dependency list.

## Consequences
Injected taps enter after the evdev decoder, so the rig's latency figures exclude
the kernel and the decoder. The decoder is tested with raw event sequences. Real
finger input through the real device has not been exercised.
