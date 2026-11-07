# ADR 0308: Installer settings behind a code

Status: accepted

## Options
1. A long hold only. No secret to manage, but anyone can find it.
2. A numeric code, with lockout after 5 wrong tries for 60 s.
3. Per-user accounts. Too much for one cabinet.

## Decision
Option 2. Limits, offsets, tare and scale calibration sit behind the code and away
from user screens. Unlike the reference's cryptic codes, parameters use plain words.
Installer menus time out at 60 s, twice the 30 s user timeout, because they take
longer to work through.

The 30 s menu timeout is also longer than the reference's 15 s, because here a menu
carries sentences to read, such as what to do about an alarm.

## The code itself
A fixed code in the source is a shared password. Options were: set it at
provisioning (nothing stops a default being left in place, and it needs SSH for
every change), or change it from the panel. The panel does this: the published
default, 2580, opens the installer only to force a new code of 4 to 8 digits,
entered twice, and it is refused as a new code. CHANGE CODE is on the installer
screen after that.

The code is salted and hashed with SHA-256 in the panel's settings file, written
atomically and synced. The file is DefaultStatePath, /var/lib/curing/panel.json, in
the service's state directory. Saves run on their own goroutine.

Keylock state lives in the same file, so a locked panel comes back locked after a
restart, a power cut or a contained loop panic. A panel with no file or a damaged
file starts unlocked with the default code, and a damaged file is shown on Health.
Failing to save is shown to the operator: the change holds until the next restart.

Recovery from a forgotten code: delete the file over SSH. This also clears the lock.
It is deliberate that this needs access to the machine rather than the panel.

## Consequences
A short numeric code hashed without a slow function can be brute forced by anyone
who can read the file, which is mode 0600. The code keeps honest people out of the
limits; it is not security against someone with root. Damage to the file reopens
the default, which is a small weakening chosen so a panel cannot lock itself out.
