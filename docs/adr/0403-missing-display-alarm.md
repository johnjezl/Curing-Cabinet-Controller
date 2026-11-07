# ADR 0403: Alarm when the panel is missing

Status: accepted. Not exercised on a real blank-panel boot: none occurred.

## Context
Blank-panel boots were seen on 3 consecutive soft resets, cause unknown, not reproduced in 10 further
attempts, and only a power cycle is known to fix them (ADR 0209). The panel cannot report its own absence.

## Options
1. Log only. No one reads a journal on a cabinet with no screen.
2. Reboot to try again. It could make things worse, and soft resets are the suspect.
3. Raise an alarm through the contact and buzzer. Does not fix anything, and does not claim to.

## Decision
3. `cured` reads `drivers.DisplayMissing` at start and every minute. True raises `DISPLAY_MISSING`,
critical: the contact opens and the buzzer sounds. With no panel it cannot be silenced, except by the
power cycle that is also the known remedy, and that is intended. If the state was never recorded,
`DISPLAY_UNKNOWN` (warning) does the same.

Update: bring-up now loads the panel overlay at runtime when the firmware left DSI0 disabled (builder B,
`curing-hw-init`), and records `panel_source=runtime-overlay`. So `DISPLAY_MISSING` now means that
recovery did not work either, and only then does its text say a power cycle is the remedy. A recovered
panel raises `PANEL_RECOVERED_AT_BOOT`, a warning: on the panel and in history, with no contact or buzzer,
since nothing needs doing. Only critical system alarms open the contact or sound the buzzer;
`DISPLAY_UNKNOWN` is critical for that reason.

Observed on the rig before that fix: after almost every graceful reboot in one day the panel was missing,
the firmware log showed no panel overlay and `dsi0` disabled, and a power cycle restored it each time
(`evidence/d/real-blank-panel/`, captured before any power cycle).

`PANEL_NOT_UPDATING` covers a panel that was present and stopped: no frame for 60 s, when the display is
not already reported missing.

The alarms are also merged into what the panel shows, for the case where only touch is missing.

## Limits
- The test uses a stand-in: `curing-rig-test display-missing-on` writes the marker on tmpfs with the panel
  actually working. The reaction is real; the trigger is not.
- A panel that shows nothing while the framebuffer exists and frames are being written would not be
  noticed by any of this.
