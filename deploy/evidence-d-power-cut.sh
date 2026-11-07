#!/usr/bin/env bash
# Real power cuts. A shell cannot cut power, so this holds .rig/lock, sets the scenario
# up, writes .rig/power-cut-ready, and waits for the rig to return with a new boot ID.
# When that file appears, call the labctl MCP tool power_cycle on pi-5-3 with the delay
# you want. Raw output under evidence/d/power-cut-<scenario>-<label>/.
#
#   deploy/evidence-d-power-cut.sh sim-1x <label>           simulated cure, outage counted at 1x
#   deploy/evidence-d-power-cut.sh sim-compressed <label>   simulated cure, outage at the compressed rate
#   deploy/evidence-d-power-cut.sh real <label>             real hardware mode, no sensors
#
# About 6 minutes each, plus the cut.
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1
SC="${1:?usage: evidence-d-power-cut.sh <sim-1x|sim-compressed|real> <label>}"; LABEL="${2:?label}"
case "$SC" in sim-1x) MODE=1x ;; sim-compressed) MODE=compressed ;; real) MODE="" ;; *) echo "bad scenario" >&2; exit 2 ;; esac
OUT="evidence/d/power-cut-$SC-$LABEL"; rm -rf "$OUT"; mkdir -p "$OUT/screens"
RIG="$ROOT/deploy/rig.sh"; HOST="${RIG_HOST:-192.168.4.142}"
exec 9>"$ROOT/.rig/lock"
flock -w 1800 9 || { echo "no rig lock" >&2; exit 75; }
export RIG_LOCK_HELD=1 GOMAXPROCS=2
SPEED=700
cap() { local f="$1"; shift; { echo "\$ $* # $(date -u +%FT%T.%3NZ)"; "$@"; echo "exit=$?"; } >"$OUT/$f" 2>&1; }
shot() { "$RIG" screenshot "$OUT/screens/$1.png" >/dev/null 2>&1 || echo "screenshot $1 failed" >>"$OUT/errors.txt"; }
{
  date -u; echo "scenario=$SC label=$LABEL speed=$SPEED"
  find cmd/cured internal deploy go.mod go.sum -type f | LC_ALL=C sort | xargs sha256sum
} >"$OUT/00-sources.txt"
cap 01-deploy.txt "$RIG" deploy-cured
if [ -n "$MODE" ]; then
  cap 02-provision.txt "$RIG" provision-sim "$SPEED" "$MODE"
  cap 03-reset.txt "$RIG" sim-reset
  cap 04-start.txt "$RIG" cured-restart
  for i in $(seq 1 60); do "$RIG" sim-state >/dev/null 2>&1 && break; sleep 1; done
  cap 05-start-cure.txt "$RIG" sim-start-cure test-12d
  sleep 240 # about 2 virtual days
else
  cap 02-provision.txt "$RIG" provision
  cap 04-start.txt "$RIG" cured-restart
  sleep 150
fi
cap 06-hashes.txt "$RIG" hashes
cap 10-status-before.txt "$RIG" status
cap 11-lines-before.txt "$RIG" lines
[ -z "$MODE" ] || cap 12-sim-state-before.txt "$RIG" sim-state
shot before
old=$("$RIG" boot-id)
echo "old_boot=$old" >"$OUT/20-cut-timing.txt"
ping -D -i 0.2 -W 1 "$HOST" >"$OUT/21-ping.txt" 2>&1 &
pingpid=$!
touch "$ROOT/.rig/power-cut-ready"
echo "$(date -u +%FT%T.%3NZ) waiting for labctl power_cycle on pi-5-3" >>"$OUT/20-cut-timing.txt"
while :; do
  id=$("$RIG" boot-id 2>/dev/null)
  if [ -n "$id" ] && [ "$id" != "$old" ]; then break; fi
  sleep 1
done
rm -f "$ROOT/.rig/power-cut-ready"
kill "$pingpid" 2>/dev/null
{
  echo "new_boot=$id ssh_answered_host_clock=$(date +%s.%N)"
  awk -F'[][]' '/bytes from/ {t=$2; split($3,a,"icmp_seq="); split(a[2],b," "); s=b[1]; if (ps && s>ps+5) {print "last_ping_before_gap=" pt; print "first_ping_after_gap=" t; exit} pt=t; ps=s}' "$OUT/21-ping.txt"
} >>"$OUT/20-cut-timing.txt"
# The panel as soon as anything can read it.
shot after-0s
cap 30-boot-timing-first.txt "$RIG" boot-timing
cap 31-lines-first.txt "$RIG" lines
sleep 30; shot after-30s
cap 32-status-30s.txt "$RIG" status
if [ -n "$MODE" ]; then
  cap 33-sim-state-30s.txt "$RIG" sim-state
  for t in 60 120 180; do sleep 60; shot "after-${t}s-more"; cap "34-sim-state-${t}s-more.txt" "$RIG" sim-state; done
  cap 35-lines-after.txt "$RIG" lines
  cap 36-boot-timing-final.txt "$RIG" boot-timing
  cap 37-journal.txt "$RIG" journal 400 curing-sim.service
else
  sleep 90; shot after-120s
  cap 33-status-120s.txt "$RIG" status
  cap 35-lines-after.txt "$RIG" lines
  cap 36-boot-timing-final.txt "$RIG" boot-timing
  cap 37-journal.txt "$RIG" journal 200 curing.service
fi
cap 38-hw-journal.txt "$RIG" journal 40 curing-hw.service
cap 39-hwstate.txt "$RIG" hwstate
"$RIG" cured-restart >/dev/null 2>&1 # leave no restart-loop count behind
echo "wrote $OUT"
