# ADR 0404: Simulated time, real outages

Status: accepted

## Context
A compressed cure on the rig runs on the real panel, real storage and real power cuts. Virtual time runs
hundreds of times faster than real time. When the rig really loses power, how much virtual time passed?
Nothing about that question has one right answer, so an answer that flatters recovery has to be ruled out.

`sim.Live` could not do this: it builds a new plant on every start, so a restart would bring back a warm
empty cabinet under a running cure. `cured` runs its own loop on `sim.Plant`, with a small additive
`PlantState` in `internal/sim` so the plant survives the process.

## Options for an outage's virtual length
1. Freeze the plant across the outage. Recovery looks perfect, and the test proves nothing about the plant.
2. Real outage at 1x: virtual gap = real gap. A 90 s real cut is a 90 s cabinet outage. Honest about the
   length, and in plant terms almost nothing happens. It tests persistence and restart, not recovery.
3. Real outage at the compressed rate: virtual gap = real gap x speed. A 90 s cut at 700x is 17.5 h. The
   plant really warms up and recovery is really tested, at a length no one chose.
4. A chosen virtual length per run. Flexible, and invites choosing a kind one.

## Decision
Both 2 and 3, selected per run by `--sim-outage 1x|compressed`. Each resume writes one line to
`/var/lib/curing-sim/plant/outages.jsonl`, which `sim-state` returns: real gap, its source, virtual gap,
and true air, product temperature and humidity before and after.

Mechanics:
- The plant is stepped for the gap with mains off (`PowerOff`), then `PowerOn`, before `core.New`. core
  then sees the virtual clock jump and handles it as any outage: same code as on real hardware.
- The cure clock does not count the outage (ADR 0107), in either variant.
- The gap is measured from the last plant checkpoint. Same boot: by uptime. New boot: by the wall clock
  after NTP, waiting up to 60 s, because the Pi's RTC returns near zero after a cut. With no NTP in time,
  uptime is used, labelled `uptime-lower-bound-no-ntp`. It includes boot time, since a real cabinet's
  controller is also absent while it boots.
- A clean stop is not an outage: the plant resumes where it was.
- Capped at 30 virtual days, flagged `capped`.
- The plant checkpoint is written to a temporary file, synced and renamed, so a kill or a cut leaves the
  old one or the new one. An earlier version wrote it in place; a watchdog kill left it empty and the
  simulator started a fresh warm plant under a running cure. It now says so loudly if it ever happens.

## Known distortions
- **Stale state.** The checkpoint and log reach disk about every 0.5 to 1 s of real time, which is up to
  700 virtual seconds at 700x. `virtual_stale_bound_s` is added to the gap so virtual time never runs
  backwards against what core wrote. The plant spends that time unpowered though it was in fact running,
  so a real cut reads up to about 12 virtual minutes longer than its true length at 700x.
- **Group sync.** At 700x core would ask for 12 fsyncs a second, more than the rig's card does, and
  queue-full storage faults appear that compression alone causes. In --sim, appended files sync at most
  once a real second and core's state slots reach the card at most every 5 real seconds (`groupSyncFS`).
  Real mode syncs each line and each checkpoint. So on a real cut the simulator loses more
  virtual time of log than a real cabinet would, and evidence from it shows resume works from a torn
  tail, not how fresh the data would be at 1x.
- **Measured loss on real cuts at 700x.** core's outage length came out about 1,500 to 7,000 virtual seconds
  longer than the accounted gap, because that much log had not reached the card. That is about 2 to 10 s of
  real time. The cure clock falls back by the same amount, so a phase runs longer rather than shorter.
- **Real clock path.** The simulator's virtual clock is always trusted, so clock trust after a cut is only
  exercised by the real-hardware power cut.
- **Random stream.** A restored plant reseeds. A resumed run is not the run that would have happened.
- Everything about the plant is the model in ADR 0105, not a cabinet.
