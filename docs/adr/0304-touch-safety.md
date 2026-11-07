# ADR 0304: Touch handling and accidental input

Status: accepted

## Context
The reference panels use a 2 to 5 s hold to enter edit and commit automatically
after about 10 s. Fingers here may be greasy or gloved. A cabinet door can brush
the panel, and water or grease can create contacts.

## Options for changing a setpoint
1. As the reference: hold to edit, auto-commit on timeout. A slip can commit.
2. Edit in place with +/-; nothing is sent until SAVE; a timeout discards.
3. Numeric keypad with confirm. More taps and more error modes.

## Decision
Option 2: three taps for one step. While a change is pending, the navigation row is
replaced with CANCEL and SAVE at opposite ends, and old and new values show side by
side. A timeout discards and says so. This departs from the reference on purpose:
an unattended, half-made change must not take effect.

While a recipe runs the core refuses manual setpoints, so the screen is view only
and says why, instead of inviting a change that will be refused.

Destructive steps (stop, next phase, end recipe, tare) need a held confirm of 2 s.
Keylock takes a 2 s hold and unlock a 3 s hold.

Recognition rules:
- Tap fires on release, on the same target, between 25 ms and 20 s of contact.
- 24 px of finger roll is allowed before a press is abandoned.
- 250 ms after a tap further touches are ignored, as bounce.
- Three or more contacts cancel until the surface is clear, as a palm or wet cloth.
- A hold survives a contact loss under 150 ms, as greasy contact drops out.
- A press is dropped if its target leaves the screen.
- The touch that wakes the screen does nothing else.

Every target is at least 89 px (12 mm) in both directions, the gloved-finger size,
and no two overlap. A test checks this on every screen and state. Controls too short
to meet it, such as the one-line banner, are not targets. The cost: three recipes and
three history rows per page instead of four, and trend ranges cut from four to three.

Start, next phase, stop, end, tare, calibrate and defrost all need a 2 s hold, and
pause a 1 s hold. A double tap can therefore not start a recipe, which matters because
START... and its confirmation fall in the same place. The start dialog states the
setpoints it will move to.

A message covering controls blocks them: a touch there only dismisses it, because a
control a person cannot see must not fire. Messages end when their screen does. On the
keypad, messages sit beside the keys so a wrong code does not eat the next digit.

Silence stays available while locked: it cannot hide anything, and a locked panel
must not leave a buzzer sounding.

## Consequences
No auto-repeat on +/-, so large changes take several taps. The filters are untested
with real gloves or grease, as no one has touched this panel during development.
