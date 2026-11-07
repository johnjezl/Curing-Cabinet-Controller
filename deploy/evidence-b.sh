#!/usr/bin/env bash
# Regenerates the raw evidence for part B under evidence/b/. Raw output only.
# Holds .rig/lock for the whole run, so nothing else can touch the rig. Reboots it
# three times: gracefully, by kernel panic, and by kernel panic with only the
# hardware watchdog able to reset. About 10 min, plus any wait for the lock.
#
# Each capture ends with "exit=N" and "errors_in_output=N". A capture fails on a
# non-zero exit or on an error line in its output. Lines beginning driver= are driver
# results, where errors are the thing being recorded, and are not scanned.
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/evidence/b"
RIG="$ROOT/deploy/rig.sh"
cd "$ROOT" || exit 1
mkdir -p "$OUT" "$ROOT/.rig"
rm -f "${OUT:?}"/[0-9]*.txt
SP=/tmp/claude-1000/-home-john-projects-Curing-Cabinet-Controller--claude-worktrees-bridge-cse-014Q9arxsNRyixQaUNwLct8J/b4f63687-8409-5247-bdf7-bd66d4506c04/scratchpad
if ! command -v go >/dev/null 2>&1 && [ -n "${RIG_GO_DIR:-}" ]; then PATH="$RIG_GO_DIR:$PATH"; fi
if ! command -v go >/dev/null 2>&1 && [ -x "$SP/go/bin/go" ]; then PATH="$SP/go/bin:$PATH"; fi
export GOCACHE="${GOCACHE:-$ROOT/.rig/gocache}" GOPATH="${GOPATH:-$ROOT/.rig/gopath}"

exec 9>"$ROOT/.rig/lock"
echo "waiting for .rig/lock"
flock -w 1800 9 || { echo "could not get .rig/lock"; exit 75; }
export RIG_LOCK_HELD=1
echo "holding .rig/lock"

PATTERN='permission denied|not found|no such file|too many arguments|command not found|failed to|cannot |error:|panic: '
failed=0
cap() { # <file> <command...>
  local f="$OUT/$1"; shift
  ( echo "\$ $*"; echo "# captured $(date -u +%Y-%m-%dT%H:%M:%SZ)"; "$@" 2>&1; echo "exit=$?" ) >"$f" 2>&1
  local rc errs
  rc=$(sed -n 's/^exit=//p' "$f" | tail -1)
  errs=0
  if [ "${NOSCAN:-}" != 1 ]; then
    errs=$(grep -v '^driver=' "$f" | grep -v '^\$ ' | grep -ciE "$PATTERN")
  fi
  echo "errors_in_output=$errs" >>"$f"
  if [ "$rc" != 0 ] || [ "$errs" != 0 ]; then failed=$((failed + 1)); fi
  echo "$(basename "$f") exit=$rc errors_in_output=$errs"
}

source_hashes() {
  echo "== source files under test"
  find internal/drivers internal/hal cmd/hwcheck cmd/curing-outputd cmd/fbshot deploy -type f | LC_ALL=C sort | xargs sha256sum
  echo "== combined source hash"
  find internal/drivers internal/hal cmd/hwcheck cmd/curing-outputd cmd/fbshot deploy -type f | LC_ALL=C sort | xargs sha256sum | sha256sum
}
binary_hashes() { # local builds, then what is installed on the rig
  for c in hwcheck curing-outputd; do
    GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$ROOT/.rig/check.$c" "./cmd/$c" && echo "local_build $c $(sha256sum "$ROOT/.rig/check.$c" | cut -d' ' -f1)"
  done
  echo "== installed on the rig"
  "$RIG" hashes
}
start=$(date +%s)

NOSCAN=1 cap 00-versions-and-source-hashes.txt sh -c 'date -u; go version; echo "git HEAD (tested code may be uncommitted, the hashes below identify it)"; git rev-parse HEAD; git status --short deploy internal/drivers internal/hal cmd/hwcheck cmd/curing-outputd cmd/fbshot; true'
NOSCAN=1 cap 00a-source-hashes.txt source_hashes
NOSCAN=1 cap 01-go-test-race.txt go test -race -v -count=1 ./internal/drivers
cap 02-go-vet.txt go vet ./internal/drivers ./cmd/hwcheck ./cmd/curing-outputd ./cmd/fbshot
cap 03-cross-compile.txt env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o /dev/null ./internal/drivers ./cmd/hwcheck ./cmd/curing-outputd
cap 10-stop-application.txt "$RIG" service-stop
cap 11-provision-rerun-on-provisioned-rig.txt "$RIG" provision
cap 12-provision-again-immediately.txt "$RIG" provision
cap 13-binary-hashes.txt binary_hashes
cap 14-default-provision-removes-bench-helper.txt sh -c '"$0" provision-default && "$0" bench-helper-state' "$RIG"
cap 15-bench-provision-restores-helper-for-tests.txt sh -c '"$0" provision && "$0" bench-helper-state' "$RIG"

cap 20-graceful-reboot.txt "$RIG" reboot
cap 21-sudo-check.txt "$RIG" sudo-check
cap 22-state-after-graceful-reboot.txt "$RIG" hwstate
cap 23-provision-after-reboot-changes-nothing.txt "$RIG" provision
cap 24-bringup-journal.txt "$RIG" journal 30 curing-hw.service
cap 25-relay-lines-after-reboot.txt "$RIG" hwcheck lines
cap 26-driver-calls-nothing-wired-on-this-bench.txt "$RIG" hwcheck driver-calls
cap 27-pwm-from-rp1-registers-no-fan-no-instrument.txt "$RIG" hwcheck pwm
cap 28-time-to-safe-relays-and-fan-test-driver.txt "$RIG" hwcheck time-to-safe
cap 29-unsafe-kernel-param-refused.txt "$RIG" safety-param-test
cap 30-hx711-sck-high-time-nothing-attached.txt "$RIG" hwcheck hx711-timing

cap 40-panic-reset-by-kernel-panic-1.txt "$RIG" crash-test panic
cap 41-sudo-check.txt "$RIG" sudo-check
cap 42-state-after-panic.txt "$RIG" hwstate
cap 43-panic-only-hw-watchdog-can-reset.txt "$RIG" crash-test halt
cap 44-state-after-panic-halt.txt "$RIG" hwstate
cap 45-driver-calls-after-unclean-resets.txt "$RIG" hwcheck driver-calls

NOSCAN=1 cap 50-crash-records.txt "$RIG" records 3
cap 51-application-state-at-end.txt "$RIG" service-status
cap 90-binary-hashes-at-end.txt binary_hashes

echo "elapsed_s=$(( $(date +%s) - start )) failed=$failed" | tee "$OUT/99-summary.txt"
[ "$failed" = 0 ]
