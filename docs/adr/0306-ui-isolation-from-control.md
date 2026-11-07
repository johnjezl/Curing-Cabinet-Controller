# ADR 0306: The UI cannot block or crash control

Status: accepted

## Options
1. Call Snapshot and Do from the draw loop. Simple, but a hung core freezes the
   panel with old numbers still looking live.
2. A separate process. Strongest isolation, but ADR 0002 chose one binary.
3. In-process, with the controller called only from separate goroutines, and the
   draw loop holding no lock the core needs.

## Decision
Option 3. Panel is a pure state machine (events in, frame out). Run wraps it:
- A poller calls Snapshot every 500 ms and passes the newest over a channel.
- A worker calls Do one at a time. A command with no answer in 5 s is reported.
- Panics in those goroutines, in a render and in the loop are all recovered. A
  render panic paints a fault frame. A loop panic restarts the loop with a new Panel.
- A missing framebuffer is logged and retried. A missing touch device is retried
  with backoff. Neither returns an error to the caller.

This narrows ADR 0002's "a panic anywhere restarts the service": a UI panic does not
restart control. The UI is not worth restarting a running cabinet for.

The loop itself runs in its own goroutine, so a display stuck in Present cannot hold
up a service stop. An earlier version waited on it, and a stop took 91 s and a SIGKILL.

## Consequences
Goroutine isolation cannot protect against out-of-memory or a runtime crash, which
take control down too. A hang in the display path stops the panel only; ADR 0309
covers how that hang is made visible.
