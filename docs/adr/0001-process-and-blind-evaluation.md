# ADR 0001: Build and evaluation process

Status: accepted

## Context
The bar is BENCHMARK.md, frozen after writing (hash in docs/BENCHMARK.sha256).
Critics must judge real evidence, blind, against reference behavior described
the same way. Reference behavior exists only as documentation, not as logs or
screens, so raw evidence and a reference description cannot be shown side by
side without revealing which is which.

## Options
1. One critic reads raw evidence and the reference together. Simple, not blind.
2. One critic reads only sheets written by the builder. Blind, but judges a
   builder's summary, which is forbidden.
3. Two fresh agents per round. An auditor runs the evidence commands, inspects
   raw output and screenshots, and writes a behavior sheet in the reference's
   style. A separate judge gets two unlabeled sheets in random order and decides
   which to trust with the batch.

## Decision
Option 3. Neither agent sees source code or builder reasoning. Each statement
keeps an evidence-basis tag (DOCUMENTED, MEASURED-RIG, MEASURED-SIM, TEST,
INFERRED), which is never stripped. A simulator measurement must not pass as a
hardware one, even though these tags can weaken blinding. Honest evidence takes
priority over perfect blinding.

## Consequences
The judge can sometimes guess which sheet is ours from the tags. Results from
the simulated plant show controller behavior against our model, not a real
cabinet, and the final report has to say so.
