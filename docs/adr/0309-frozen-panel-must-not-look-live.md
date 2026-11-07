# ADR 0309: A frozen panel must not look live

Status: accepted

## Context
The framebuffer keeps its last image when nothing draws to it. A hung loop, a stuck
driver or a dead process would leave a good-looking reading on the wall indefinitely.
The stalled-controller banner cannot help, because the loop that would draw it may
be the part that is stuck.

## Options
1. A visible heartbeat only. Works in every failure, needs a person to know it.
2. A watchdog inside the process that replaces the screen. Active and unmistakable,
   but cannot run if the process or kernel has died.
3. A separate supervisor process. Covers a dead main process, at the cost of a
   second thing to deploy and keep alive, against ADR 0002's single binary.
4. A hardware dead-man on the backlight. Would cover everything, but needs hardware
   this project does not have.

## Decision
Layers of 1 and 2, plus a hook for the service manager.
- Every frame carries a bar along the bottom edge that moves once a second, and the
  clock shows seconds. Both come from the render, so they stop when it stops. A test
  requires every screen and power stage to change within one second. Health says
  what the bar means.
- A goroutine beside the loop checks that a frame finished in the last 5 s. If not,
  it paints FROZEN straight to the framebuffer, removing the old readings, and repeats
  every 5 s. Frames resuming repaint the screen in full.
- When the loop ends normally the panel paints STOPPED.
- ui.PaintStopped paints the same notice from a new process. The service unit should
  call it through ExecStopPost, which also runs after a crash or SIGKILL. This wiring
  belongs to part D and does not exist yet.

## Consequences
Remaining cases, and what covers them:
- Process dead, before ExecStopPost is wired: only the still bar and clock show it,
  until the service restarts.
- Kernel hang: only the still bar and clock show it, until the hardware watchdog
  reboots (ADR 0203).
- A stuck write to framebuffer memory, if that can happen, may also block the notice.
- The bar is 13.5 x 0.7 mm. It only helps a person who looks for it and knows what it
  means, so it is the weakest layer and the last line, not the main defence.
