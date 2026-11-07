#!/usr/bin/env bash
# Regenerates raw evidence for the application on the rig under evidence/d/. Raw output
# only. Holds .rig/lock for the whole run. About 50 minutes, 25 of them the compressed cure.
# Real power cuts need the labctl tool, so they are separate: deploy/evidence-d-power-cut.sh.
#   deploy/evidence-d.sh            everything but power cuts
#   deploy/evidence-d.sh --no-cure  skip the compressed cure
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1
RIG="$ROOT/deploy/rig.sh"
OUT=evidence/d
CURE=1; [ "${1:-}" = --no-cure ] && CURE=0
exec 9>"$ROOT/.rig/lock"
flock -w 1800 9 || { echo "no rig lock" >&2; exit 75; }
export RIG_LOCK_HELD=1 GOMAXPROCS=2
mkdir -p "$OUT"
find "$OUT" -mindepth 1 -maxdepth 1 ! -name 'power-cut-*' ! -name cure -exec rm -rf {} +
mkdir -p "$OUT/screens"
cap() { local f="$1"; shift; { echo "\$ $* # $(date -u +%FT%T.%3NZ)"; "$@"; echo "exit=$?"; } >"$OUT/$f" 2>&1; echo "$(date -u +%T) $f"; }
shot() { "$RIG" screenshot "$OUT/screens/$1.png" >/dev/null 2>&1 || echo "screenshot $1 failed" >>"$OUT/errors.txt"; }
wait_up() { for i in $(seq 1 120); do "$RIG" status 2>/dev/null | grep -qE 'curing(-sim)?\.service enabled=enabled active=active' && return 0; sleep 1; done; }

{
  date -u; go version; git rev-parse HEAD 2>/dev/null
  echo "== source hashes"; find cmd internal deploy docs/wiring.md docs/adr go.mod go.sum -type f | LC_ALL=C sort | xargs sha256sum
  echo "== combined"; find cmd internal go.mod go.sum -type f | LC_ALL=C sort | xargs sha256sum | sha256sum
} >"$OUT/00-versions.txt" 2>&1

cap 01-go-test.txt nice -n 19 go test -count=1 -v -p 2 -parallel 2 ./cmd/cured

# --- Real hardware mode, no sensors attached, from a real boot.
cap 10-deploy.txt "$RIG" deploy-cured
cap 11-provision.txt "$RIG" provision
cap 12-provision-again.txt "$RIG" provision
cap 13-hashes.txt "$RIG" hashes
cap 13a-static-binary.txt file .rig/cured.arm64 .rig/curing-outputd
cap 14-unit-config.txt "$RIG" unit-config
# Soft resets have left the panel blank until a power cycle (ADR 0209), and nothing in a script can
# power cycle. So everything that needs the panel runs first, from a service restart, and every
# reboot is at the end. Cold boot timing is in the power cut captures.
cap 15-display-state-at-start.txt "$RIG" boot-timing
cap 15a-restart.txt "$RIG" cured-restart
sleep 3
for t in 60 120 240 300; do sleep 60; [ "$t" = 60 ] || [ "$t" = 120 ] || sleep 60; shot "real-no-sensors-${t}s"; cap "19-status-no-sensors-${t}s.txt" "$RIG" status; done
cap 20-lines-no-sensors.txt "$RIG" lines
cap 21-driver-faults-journal.txt "$RIG" journal 200 curing.service
cap 22-watchdog-state.txt "$RIG" hwstate

# Five rounds of a deliberate stop and start, which is what demo-start and demo-stop do to the app.
for i in 1 2 3 4 5; do "$RIG" cured-restart >>"$OUT/25a-churn.txt" 2>&1; sleep 2; done
cap 25b-status-after-churn.txt "$RIG" status

# --- PANEL STOPPED after SIGKILL: frames from the rig every 200 ms through the kill.
cap 26-sigkill-frames.txt "$RIG" sigkill-capture "$OUT/sigkill-frames"
"$RIG" cured-restart >/dev/null 2>&1; sleep 3


# --- Crash and hang, real mode. A clean restart between runs keeps the loop count out of it.
for sig in KILL ABRT STOP; do
  for i in 1 2 3; do
    cap "30-real-$sig-$i.txt" "$RIG" crash-test-service "$sig"
    "$RIG" cured-restart >/dev/null 2>&1; sleep 3
  done
done
# PANEL STOPPED while the service is really down.
cap 31-service-stop.txt "$RIG" service-stop
sleep 1; shot real-service-stopped
cap 32-lines-stopped.txt "$RIG" lines
cap 33-restart.txt "$RIG" cured-restart
sleep 3; shot real-after-restart
# A crash loop reaches the alarm.
for i in 1 2 3 4; do "$RIG" service-kill KILL >>"$OUT/34-restart-loop-kills.txt" 2>&1; sleep 4; done
cap 35-restart-loop-status.txt "$RIG" status
shot real-restart-loop
cap 36-clean-restart-clears.txt "$RIG" cured-restart
sleep 3; cap 37-status-after-clean-restart.txt "$RIG" status

# --- Simulator gates.
cap 40-gate-real-mode.txt "$RIG" sim-gate-check
cap 41-sim-state-refused-in-real-mode.txt "$RIG" sim-state
cap 42-provision-default.txt "$RIG" provision-default
cap 43-gate-production-marker.txt "$RIG" sim-gate-check
cap 44-provision-back.txt "$RIG" provision

# --- Alarm line through failures, with the simulated plant healthy so the line can go high.
cap 50-provision-sim.txt "$RIG" provision-sim 60 1x
cap 51-sim-reset.txt "$RIG" sim-reset
cap 52-sim-start.txt "$RIG" cured-restart
"$RIG" sim-setpoint 21 60 >/dev/null 2>&1
for i in $(seq 1 90); do
  "$RIG" status 2>/dev/null | grep -q '"alarm_contact_closed":true' && break
  sleep 2
done
cap 53-status-healthy.txt "$RIG" status
cap 54-lines-healthy.txt "$RIG" lines
shot sim-healthy
healthy() { "$RIG" cured-restart >/dev/null 2>&1; sleep 4; for i in $(seq 1 150); do "$RIG" status 2>/dev/null | grep -q '"alarm_contact_closed":true' && return 0; sleep 1; done; echo "contact did not close within 150 s" >>"$OUT/errors.txt"; }
for sig in KILL STOP; do
  for i in 1 2; do cap "55-contact-$sig-$i.txt" "$RIG" crash-test-service "$sig"; healthy; done
done
for inj in panic-control hang-control; do
  for i in 1 2; do cap "56-contact-$inj-$i.txt" "$RIG" sim-crash-test "$inj"; healthy; done
done
# A sensor fault opens the contact with control alive.
cap 57-fault-climate-dead.txt "$RIG" sim-fault climate dead
sleep 30; cap 57a-status.txt "$RIG" status; cap 57b-lines.txt "$RIG" lines; shot sim-climate-dead
cap 57c-repair.txt "$RIG" sim-repair climate

# --- UI failure does not stop control.
healthy
cap 58-before-ui-fault.txt "$RIG" status
cap 58a-panic-ui.txt "$RIG" sim-ui-fault panic-ui
sleep 5; cap 58b-after-panic-ui.txt "$RIG" status; shot sim-after-panic-ui
cap 58c-stall-ui.txt "$RIG" sim-ui-fault stall-ui
sleep 12; cap 58d-during-stall-ui.txt "$RIG" status; shot sim-during-stall-ui
sleep 25; cap 58e-after-stall-ui.txt "$RIG" status; shot sim-after-stall-ui

# --- Missing display: a stand-in for the marker with the panel actually working.
healthy
cap 60-before-display-missing.txt "$RIG" status
cap 60a-lines.txt "$RIG" lines
cap 61-display-missing-on.txt "$RIG" display-missing-test on
sleep 25; cap 62-status.txt "$RIG" status; cap 62a-lines.txt "$RIG" lines; shot sim-display-missing
cap 63-display-missing-off.txt "$RIG" display-missing-test off
sleep 25; cap 64-status.txt "$RIG" status; cap 64a-lines.txt "$RIG" lines

# --- A whole cure in compressed time, real panel, real card.
if [ "$CURE" = 1 ]; then
  echo "$(date -u +%T) compressed cure"
  deploy/cure-compressed.sh --out "$OUT/cure" --speed 700 --recipe test-12d --outage 1x >"$OUT/80-cure-driver.txt" 2>&1
fi

# --- Silence: quiets the buzzer for a while, leaves the contact and the alarm alone.
# At 60x core's 30 min re-sound timer is 30 real s.
"$RIG" cured-restart >/dev/null 2>&1; sleep 25
cap 66-fault-for-silence.txt "$RIG" sim-fault climate dead
sleep 20; cap 66a-status-sounding.txt "$RIG" status
cap 66b-silence.txt "$RIG" sim-silence
sleep 3; cap 66c-status-silenced.txt "$RIG" status; cap 66d-lines-silenced.txt "$RIG" lines
sleep 40; cap 66e-status-after-resound-timer.txt "$RIG" status
cap 66f-repair.txt "$RIG" sim-repair climate

# --- Outputs that cannot be written: no restart churn, contact open, alarm shown.
"$RIG" sim-ack >/dev/null 2>&1
cap 67-unit-before.txt "$RIG" status
cap 67a-write-error.txt "$RIG" sim-fault outputs write-err
sleep 90; cap 67b-status-90s.txt "$RIG" status; cap 67c-lines-90s.txt "$RIG" lines
cap 67d-repair.txt "$RIG" sim-repair outputs
sleep 20; cap 67e-status-after-repair.txt "$RIG" status
cap 67f-journal.txt "$RIG" journal 60 curing-sim.service

# --- Reboots, last. From here the panel may be blank; DISPLAY_MISSING in the status says so.
cap 84-provision-real.txt "$RIG" provision
cap 85-reboot.txt "$RIG" reboot
cap 86-boot-timing.txt "$RIG" boot-timing
cap 86a-hw-journal.txt "$RIG" journal 60 curing-hw.service
shot real-boot
cap 87-status-after-boot.txt "$RIG" status
cap 88-lines-after-boot.txt "$RIG" lines

# --- An undisturbed boot raises nothing about outages or restart loops. From first-boot
# state, one clean stop, then graceful reboots with nothing else touching the service.
cap 23-real-reset.txt "$RIG" real-reset
cap 23a-start.txt "$RIG" cured-restart
sleep 30
cap 23b-clean-stop.txt "$RIG" service-stop
for b in 1 2; do
  cap "24-undisturbed-reboot-$b.txt" "$RIG" reboot
  cap "24a-undisturbed-boot-timing-$b.txt" "$RIG" boot-timing
  cap "24d-undisturbed-hw-journal-$b.txt" "$RIG" journal 60 curing-hw.service
  sleep 120
  cap "24b-undisturbed-status-$b.txt" "$RIG" status
  cap "24c-undisturbed-journal-$b.txt" "$RIG" journal 40 curing.service
  shot "undisturbed-boot-$b"
done
# --- Kernel panic with the application running, on the simulated plant so the contact can be closed first.
cap 69-provision-sim.txt "$RIG" provision-sim 60 1x
# core holds the contact open for unacknowledged critical events, so start from a fresh plant.
cap 69a-sim-reset.txt "$RIG" sim-reset
"$RIG" cured-restart >/dev/null 2>&1; sleep 4; "$RIG" sim-setpoint 21 60 >/dev/null 2>&1
healthy
cap 70-kernel-panic.txt "$RIG" kernel-panic-test
cap 71-boot-timing.txt "$RIG" boot-timing
cap 71a-hw-journal.txt "$RIG" journal 60 curing-hw.service
cap 72-lines.txt "$RIG" lines
shot sim-after-panic
sleep 25; cap 73-status.txt "$RIG" status; cap 73a-lines.txt "$RIG" lines


# --- Back to real hardware mode.
cap 90-provision-real.txt "$RIG" provision
cap 91-status.txt "$RIG" status
cap 92-hashes.txt "$RIG" hashes
# A clean restart last, so no test leaves a restart-loop alarm on the rig.
cap 93-clean-restart.txt "$RIG" cured-restart
sleep 20; cap 94-final-status.txt "$RIG" status
date -u >"$OUT/99-finished.txt"
