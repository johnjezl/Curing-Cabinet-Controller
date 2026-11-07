# ADR 0107: Time, cure progress and outage records

Status: accepted

## Context
The Pi may boot with no valid clock. Cure timing must not depend on wall-clock continuity.

## Options for cure time
1. Store start time; elapsed = now - start. Wrong by any clock error.
2. Count powered time from the monotonic clock, and persist it.
3. As 2, and add the outage length when known.

Decision: option 2. Phases count powered, unpaused seconds. Adding outages (3) would shorten
a phase by a span in which conditions were not held, and would differ depending on whether a
clock was present. A phase is never shortened by a power cut. Outage counts and known lengths
are kept with the cure.

## Outage detection
A clean-shutdown flag is set only by an orderly stop, so any other stop is an outage. Duration
comes from last-log wall time to now only if both are trusted. If the clock was trusted before
and is not yet now, the duration resolves when the clock arrives, by subtracting the time since
boot; otherwise it is reported as unknown, not guessed. It reads up to one log interval (60 s)
long or short. Temperature on return and the peak are recorded until the cabinet is in band,
past the power-on delay, or the 90 min window ends. A short outage in band clears itself.

Trust: the platform's word (RTC, NTP) and a local check that time has not gone back past what
was recorded. Every log line and alarm carries a trusted flag and boot number, so a record stays
ordered by sequence with no clock at all.

## Consequences
The daemon must supply Trusted() from a real signal (timesyncd state or RTC validity).
