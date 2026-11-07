# ADR 0305: Dim, sleep and image retention

Status: accepted

## Options
1. Always full brightness. Simple, but static content for weeks on an IPS panel
   risks image retention, and it lights a dim room.
2. Blank after a timeout. Hides the state from anyone glancing at the cabinet.
3. Stages: dim, then a dark moving summary, with attention always forcing full on.

## Decision
Option 3. After 10 min on Home, pixel levels scale to about 70 %, large light fills
go dark, the backlight goes to 40 %, and the frame orbits by up to 3 px each minute.
After 60 min a dark view shows temperature and humidity at 14 mm digits, the setpoints
and OK in range, moving slowly, with the backlight at 15 %. A simulated plant stays
marked in both.

The first version dimmed pixels to 38 % as well as the backlight. Measured contrast
of the digits fell to 3.0:1, in the state the panel spends weeks in, and the output
chips became the brightest thing on the screen. So the luminance reduction now comes
from the backlight, which lowers output without lowering contrast, and pixel levels
only drop enough to take the edge off. Wear is reduced by the backlight, by the orbit,
and by removing the large static light fills. A test measures digit contrast from the
rendered pixels and requires 7:1.

An active alarm of any severity, acknowledged or silenced included, a sensor that
is not good, a stalled controller, an out-of-band reading, a paused cure, or a recipe
awaiting verification all force full on and block dim and sleep. A panel with a
backlight this dim has an unmeasured brightness; no one has looked at it. A new alarm wakes the
screen with no touch. Menus time out to Home before any of this happens.

## Consequences
The alarm state itself is static and bright for as long as it lasts, with only a
pulse bar moving. That is the right trade. No burn-in measurement exists: orbit and
dim are mitigations whose effect here is unmeasured.
