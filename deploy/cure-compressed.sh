#!/usr/bin/env bash
# A whole cure on the rig's real panel and real storage, on the simulated plant, in
# compressed time, with disturbances and faults injected on a schedule in virtual time.
#
#   deploy/cure-compressed.sh --out DIR [--speed 700] [--recipe test-12d] [--outage 1x|compressed]
#                             [--cut-at-day D] [--no-schedule]
#
# --cut-at-day D stops at virtual day D, writes .rig/power-cut-ready and waits for the
# rig to return with a new boot ID. A shell cannot cut power: call the labctl MCP tool
# power_cycle on pi-5-3 when that file appears. Takes about 25 minutes at 700x for 12 days.
# Holds .rig/lock throughout, unless the caller already does (RIG_LOCK_HELD).
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1
RIG="$ROOT/deploy/rig.sh"
SPEED=700 RECIPE=test-12d OUTAGE=1x OUT="" CUT="" SCHEDULE=1 LIMIT_DAYS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    --speed) SPEED="$2"; shift 2 ;;
    --recipe) RECIPE="$2"; shift 2 ;;
    --outage) OUTAGE="$2"; shift 2 ;;
    --cut-at-day) CUT="$2"; shift 2 ;;
    --stop-at-day) LIMIT_DAYS="$2"; shift 2 ;;
    --no-schedule) SCHEDULE=0; shift ;;
    *) echo "unknown argument $1" >&2; exit 2 ;;
  esac
done
[ -n "$OUT" ] || { echo "--out is required" >&2; exit 2; }
if [ -z "${RIG_LOCK_HELD:-}" ]; then
  exec 9>"$ROOT/.rig/lock"
  flock -w 1800 9 || { echo "no rig lock" >&2; exit 75; }
  export RIG_LOCK_HELD=1
fi
export GOMAXPROCS=2
rm -rf "$OUT"; mkdir -p "$OUT/screens"
T="$OUT/timeline.jsonl"; A="$OUT/actions.log"; : >"$T"; : >"$A"
stamp() { date -u +%FT%T.%3NZ; }

# virtual_day action [args]; relative to the cure's start. A repair ends the fault above it.
SCHED=(
  "0.60 sim-door open" "0.62 sim-door close"
  "1.00 sim-door open" "1.003 sim-load 5 25" "1.006 sim-door close"
  "1.50 sim-fault climate dead" "1.65 sim-repair climate"
  "2.05 sim-ambient 10" "2.45 sim-ambient 0"
  "2.50 sim-fault rh stuck" "2.95 sim-repair rh"
  "3.05 sim-fault air dead" "3.15 sim-repair air"
  "3.30 sim-fault scale dead" "3.40 sim-repair scale"
  "3.55 sim-fault fan stall" "3.70 sim-repair fan"
  "3.85 sim-fault climate bad-read" "3.95 sim-repair climate"
  "4.20 sim-fault temp drift 1" "4.70 sim-repair temp"
  "5.20 sim-fault cool relay-stuck-on" "5.50 sim-repair cool"
  "5.80 sim-fault heat relay-stuck-on" "6.00 sim-repair heat"
  "6.30 sim-fault humidifier empty" "6.70 sim-repair humidifier"
  "7.00 sim-fault outputs write-err" "7.05 sim-repair outputs"
  "7.40 sim-ambient -10" "7.90 sim-ambient 0"
  "8.30 sim-silence" "8.31 sim-ack"
  "9.00 sim-fault compressor no-cool" "9.50 sim-repair compressor"
)
[ "$SCHEDULE" = 1 ] || SCHED=()

{
  echo "start $(stamp) speed=$SPEED recipe=$RECIPE outage=$OUTAGE cut_at_day=$CUT"
  "$RIG" deploy-cured
  "$RIG" provision-sim "$SPEED" "$OUTAGE"
  "$RIG" sim-reset
  "$RIG" cured-restart
} >"$OUT/setup.log" 2>&1

for i in $(seq 1 60); do "$RIG" sim-state >/dev/null 2>&1 && break; sleep 1; done
"$RIG" sim-start-cure "$RECIPE" >>"$OUT/setup.log" 2>&1

field() { python3 -c "
import json,sys
try:
    d=json.loads(sys.argv[1]); s=d['status']; print(s['sim']['virtual_day'], s.get('run_state',''))
except Exception: print('-1 unknown')
" "$1"; }

day0=$(field "$("$RIG" sim-state)"); day0=${day0%% *}
echo "cure_started_at_virtual_day=$day0 $(stamp)" >>"$A"
next=0
last_shot=0
n=0
cut_done=0
start=$(date +%s)
shot() { "$RIG" screenshot "$OUT/screens/$1.png" >/dev/null 2>&1 || echo "screenshot $1 failed" >>"$A"; }
shot start
while :; do
  js=$("$RIG" sim-state 2>/dev/null)
  if [ -z "$js" ]; then echo "$(stamp) sim-state no answer" >>"$A"; sleep 2; continue; fi
  echo "$js" >>"$T"
  set -- $(field "$js"); day=$1; run=$2
  rel=$(awk -v a="$day" -v b="$day0" 'BEGIN{printf "%.3f", a-b}')
  # Actions due.
  while [ "$next" -lt "${#SCHED[@]}" ]; do
    item=${SCHED[$next]}; due=${item%% *}; act=${item#* }
    [ "$(awk -v r="$rel" -v d="$due" 'BEGIN{print (r>=d)?1:0}')" = 1 ] || break
    # A fault can restart the service, so an action is retried until it is accepted.
    for try in $(seq 1 60); do
      reply=$("$RIG" $act 2>&1)
      case "$reply" in *'"ok":true'*) break ;; esac
      sleep 1
    done
    reply="$reply tries=$try"
    echo "$(stamp) virtual_day=$rel due=$due action=$act reply=$reply" >>"$A"
    case "$act" in
      sim-fault*|sim-door\ open|sim-ambient*) sleep 3; "$RIG" sim-state >>"$T" 2>/dev/null; shot "d${rel}-$(echo "$act" | tr ' ' '_')"; "$RIG" lines >>"$A" 2>&1 ;;
    esac
    next=$((next + 1))
  done
  # Periodic capture.
  now=$(date +%s)
  if [ $((now - last_shot)) -ge 120 ]; then last_shot=$now; shot "d${rel}-periodic"; fi
  # The real power cut.
  if [ -n "$CUT" ] && [ "$cut_done" = 0 ] && [ "$(awk -v r="$rel" -v c="$CUT" 'BEGIN{print (r>=c)?1:0}')" = 1 ]; then
    cut_done=1
    shot "d${rel}-before-cut"
    old=$("$RIG" boot-id)
    echo "$(stamp) waiting for a real power cut; old boot $old" >>"$A"
    touch "$ROOT/.rig/power-cut-ready"
    while :; do id=$("$RIG" boot-id 2>/dev/null); [ -n "$id" ] && [ "$id" != "$old" ] && break; sleep 1; done
    rm -f "$ROOT/.rig/power-cut-ready"
    echo "$(stamp) back, new boot $id" >>"$A"
    for i in $(seq 1 120); do "$RIG" sim-state >/dev/null 2>&1 && break; sleep 1; done
    "$RIG" boot-timing >"$OUT/boot-timing-after-cut.txt" 2>&1
    "$RIG" sim-state >"$OUT/state-after-cut.json" 2>&1
    shot "d${rel}-after-cut"
  fi
  [ "$run" = finished ] && break
  if [ -n "$LIMIT_DAYS" ] && [ "$(awk -v r="$rel" -v c="$LIMIT_DAYS" 'BEGIN{print (r>=c)?1:0}')" = 1 ]; then break; fi
  # Well past the expected end: stop rather than run forever.
  [ $(( $(date +%s) - start )) -lt 7200 ] || { echo "$(stamp) time limit" >>"$A"; break; }
  sleep 4
done
echo "$(stamp) loop ended run_state=$run virtual_day=$rel real_s=$(( $(date +%s) - start ))" >>"$A"
sleep 5
shot end-home
"$RIG" sim-state >"$OUT/final-state.json" 2>&1
"$RIG" status >"$OUT/final-status.txt" 2>&1
"$RIG" journal 3000 curing-sim.service >"$OUT/journal.txt" 2>&1
echo "finished $(stamp)" >>"$OUT/setup.log"
