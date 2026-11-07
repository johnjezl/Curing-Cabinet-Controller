#!/usr/bin/env bash
# Regenerates all raw evidence for part A (control core and plant simulator)
# under evidence/a/. Results describe the simulated plant, not a real cabinet.
# Usage: deploy/evidence-a.sh [sweep_plants]   Needs Go on PATH.
# Runs on a shared machine, so by default it uses 2 cores at the lowest priority.
# EVIDENCE_CORES changes the cap.
set -euo pipefail
CORES="${EVIDENCE_CORES:-2}"
export GOMAXPROCS="$CORES"
if [ -z "${EVIDENCE_NICED:-}" ]; then
  export EVIDENCE_NICED=1
  exec nice -n 19 ionice -c3 "$0" "$@"
fi
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
SWEEP="${1:-36}"
OUT=evidence/a
JOBS="$CORES"
# baseline/ is the previous round scored with the current metrics, kept for comparison.
mkdir -p "$OUT"
find "$OUT" -mindepth 1 -maxdepth 1 ! -name baseline -exec rm -rf {} +
mkdir -p "$OUT/test"
BIN="$(mktemp -d)"
trap 'rm -rf "$BIN"' EXIT
START=$(date +%s)
srchash() { find internal/core internal/sim internal/api/api.go internal/hal/hal.go cmd/scenario deploy/evidence-a.sh -type f | sort | xargs sha256sum | sha256sum | cut -d' ' -f1; }
SRC0=$(srchash)

{
  echo "date_utc=$(date -u +%FT%TZ)"
  echo "go=$(go version)"
  echo "git_head=$(git rev-parse HEAD 2>/dev/null || echo none) (the code may be uncommitted; source_sha256 is what identifies it)"
  echo "sweep_plants=$SWEEP"
  echo "cores=$CORES nice=$(nice) go_test_flags=-p $CORES -parallel $CORES"
  echo "benchmark_sha256=$(sha256sum BENCHMARK.md | cut -d' ' -f1)"
  echo "source_sha256=$(srchash)"
  echo "evidence_basis=MEASURED-SIM and TEST; no hardware involved"
} > "$OUT/manifest.txt"

go build -p "$CORES" -o "$BIN/scenario" ./cmd/scenario
S="$BIN/scenario"

go vet ./internal/core ./internal/sim ./cmd/scenario 2>&1 | tee "$OUT/test/go_vet.log"
go test -p "$CORES" -parallel "$CORES" -race -count=1 -timeout 1500s -v ./internal/core ./internal/sim > "$OUT/test/go_test_race.log" 2>&1 || { echo "go test failed"; tail -30 "$OUT/test/go_test_race.log"; exit 1; }
grep -E '^(--- |=== RUN|ok |FAIL|PASS)' "$OUT/test/go_test_race.log" | grep -v '=== RUN' > "$OUT/test/go_test_summary.txt" || true

"$S" list > "$OUT/scenarios.txt"
"$S" suite -out "$OUT" -sweep "$SWEEP" -j "$JOBS" 2> "$OUT/suite_timing.log"

# Power cuts at random instants with storage damage. With and without a clock
# that survives, on the base plant and on stressed plants.
"$S" restart -out "$OUT/restart/base_rtc"   -cuts 80 -plant base -rtc yes -seed 11
"$S" restart -out "$OUT/restart/base_nortc" -cuts 80 -plant base -rtc no  -seed 12
"$S" restart -out "$OUT/restart/hot_nortc"  -cuts 60 -plant c01-hot-room-weak-comp -rtc no -seed 13 -sweep "$SWEEP"
"$S" restart -out "$OUT/restart/small_rtc"  -cuts 60 -plant c03-small-big-comp -rtc yes -seed 14 -sweep "$SWEEP"

# A power cut after every storage operation in a run, in turn.
"$S" cutmatrix -out "$OUT/cutmatrix" -ops 400

# SIGKILL of a real process on real files. This does not lose the OS page cache;
# that needs a real power cut on the rig.
"$S" crash -out "$OUT/crash" -n 30

# Whole cures with door openings and power cuts.
"$S" cure -out "$OUT/cure/ferment_base"      -recipe ferment-dry  -plant base -days 120 -outages 6 -sample 120 -seed 21
"$S" cure -out "$OUT/cure/wholemuscle_base"  -recipe whole-muscle -plant base -days 120 -outages 6 -sample 120 -seed 22
"$S" cure -out "$OUT/cure/beef_base"         -recipe beef-age     -plant base -days 40  -outages 3 -sample 120 -seed 23
"$S" cure -out "$OUT/cure/biltong_base"      -recipe biltong      -plant base -days 10  -outages 1 -sample 120 -seed 24
"$S" cure -out "$OUT/cure/ferment_hot"       -recipe ferment-dry  -plant c01-hot-room-weak-comp -days 120 -outages 6 -sample 300 -seed 25 -sweep "$SWEEP"
"$S" cure -out "$OUT/cure/ferment_loaded"    -recipe ferment-dry  -plant c04-large-loaded -days 120 -outages 6 -sample 300 -seed 26 -sweep "$SWEEP"
"$S" cure -out "$OUT/cure/ferment_coldhot"   -recipe whole-muscle -plant c14-hot-summer-room -days 120 -outages 6 -sample 300 -seed 28 -sweep "$SWEEP"
"$S" cure -out "$OUT/cure/ferment_small"     -recipe ferment-dry  -plant c05-small-big-humidifier -days 120 -outages 6 -sample 300 -seed 27 -sweep "$SWEEP"

# Which bars moved since the previous round, both scored with the current metrics.
if [ -f "$OUT/baseline/reference_bar.csv" ]; then
  "$S" barsmoved -old "$OUT/baseline/reference_bar.csv" -new "$OUT/compare/reference_bar.csv" -out "$OUT/compare/bars_moved.csv"
fi

# Recompute every metric from the raw files only and compare.
"$S" verify -dir "$OUT" > "$OUT/verify.log"

# One source for the whole run: if anything changed while it ran, the run does not count.
SRC1=$(srchash)
if [ "$SRC0" != "$SRC1" ]; then
  echo "source changed during the run" | tee -a "$OUT/manifest.txt"
  exit 1
fi
echo "source_sha256_end=$SRC1" >> "$OUT/manifest.txt"
echo "elapsed_s=$(( $(date +%s) - START ))" >> "$OUT/manifest.txt"
du -sh "$OUT" >> "$OUT/manifest.txt"
