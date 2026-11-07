# ADR 0004: Stop judging part A after round 3, with one safety fix pass

Status: accepted

## Context
Part A lost three blind rounds, moderately each time. In rounds 2 and 3 the
judge named "no run on real equipment" as the largest gap, and said it cannot be
closed without real equipment. That meets the stop rule that the same blocker
repeats. Round 3 also left a food-safety hazard, introduced by the orchestrator's
own round 3 rule (a suspicion never changes regulation): the controller keeps
heating against a reading it has flagged critical as possibly low.

## Options
1. Keep looping. Each round brings measurable gains, but the deciding item will
   not change without hardware.
2. Stop now and ship the known warm-side hazard.
3. Stop judged rounds, do one fix pass limited to defects that make a batch
   unsafe or the alarm path fail, and let the integration critic check them.

## Decision
Option 3. Suspicion may only push toward the cold side: it can block heating and
humidifying, but never reduce cooling. Part A has not beaten the benchmark.
