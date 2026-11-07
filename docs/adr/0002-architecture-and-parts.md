# ADR 0002: Architecture and division of work

Status: accepted

## Options
1. One process per concern (sensors, control, UI) with IPC. Isolates UI crashes,
   adds failure modes and start-order problems.
2. A single static Go binary with the UI and control in goroutines, with the
   control loop kept independent of UI health.
3. Two binaries: a control daemon and a separate UI process. Control survives a
   UI crash, at the cost of one local socket.

## Decision
Option 2 first, with the seam (internal/api) kept narrow enough to split later:
one artifact to deploy, no IPC to fail, and the watchdog is pet only by the
control loop. A panic anywhere restarts the whole service to a known state.

Parts, judged independently:
- A, control core and plant simulator: control, recovery, protection, failure
  logic, alarms, cure programs, persistence. Tightly coupled, one owner.
- B, hardware drivers and provisioning.
- C, front panel.
- D, system on the rig: service, boot, watchdog, power loss. Follows A, B and C.

A, B and C share only internal/hal and internal/api, so they can run in parallel.

## Actuators
CONSTRAINTS.md lists sensors and fan only. The cabinet also needs relays, so the
contract assumes outputs for cooling, heating, humidifying, optional
dehumidifying, an alarm contact and a buzzer. A door switch is optional.
