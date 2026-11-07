# ADR 0102: Persistence under power loss

Status: accepted

## Context
Power can fail at any instant. A torn write must not lose earlier data or stop
control. An SD card fsync can stall for seconds. Storage can fill or die.

## Options
1. SQLite. Solid, but a cgo or large dependency, opaque files, and a stall inside the
   library can only be handled from outside it.
2. Write-temp-then-rename for everything. Rewrites whole files; history grows; relies
   on rename ordering across file systems.
3. Two A/B state slots with sequence and CRC, plus append-only text journals with a
   CRC per line, on a small file-system interface that a test double can tear.

## Decision
Option 3. State slots alternate, so a torn write can only hit the slot being written.
Journal lines are `crc json\n`, so a torn tail fails its CRC and is skipped; the file
is only ever appended, and a torn tail is terminated, not truncated, because it is a
record. Lines stay in RAM until fsync succeeds, so failure repeats a line (readers
dedupe by sequence) instead of losing one. RAM holds about a day of lines while storage
is down; overflow drops the oldest and is counted. Process log every 60 s with min,
max and mean per interval, so an excursion between lines is still visible. No retention
delete and no erase command: the record is only appended. If both state slots are bad,
setpoints and recipe progress are rebuilt from the log and a critical alarm is raised.

Control never blocks on storage: on real hardware AsyncStore writes on its own goroutine
with a bounded queue. Simulation uses the synchronous store to stay deterministic.

## Consequences
Up to 60 s of cure time can be lost per outage (phases get longer, never shorter). The
power-loss model is in MemFS and tested at every storage operation; `kill -9` on real
files is tested too, but a real page-cache loss can only be tested on the rig.
