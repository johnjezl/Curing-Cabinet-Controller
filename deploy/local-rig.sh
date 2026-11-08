#!/usr/bin/env bash
# Bench stand-in for deploy/rig.sh, for when the rig cannot be reached. It runs the
# real cmd/cured binary, built for this machine, under the user's systemd with the
# settings of deploy/curing-sim.service (Type=notify, WatchdogSec=10s, Restart=always,
# RestartSec=1s, cured post-stop after every end). A bubblewrap sandbox puts the
# files where cured expects them:
#
#   /run/curing-cured    <- $L/run     status, start count, sim socket; wiped by power-cut
#   /var/lib/curing-sim  <- $L/sim     plant and core state; survives restarts and cuts
#   /dev/fb0             <- $L/fb0     a plain file; screenshot turns it into a PNG
#   /run/curing          <- $L/hwrun   display-status, as curing-hw-init writes it at boot
#   /run/curing-outputs  <- $L/guard   a recorder in place of curing-outputd
#   /sys                 <- $L/sys     only the relay and fan PWM facts the rig is provisioned with
#   boot_id              <- $L/boot_id power-cut writes a new one, as a real boot would
#
# What it is not: there is no GPIO, relay, sensor, panel or touch here, and the
# binary is amd64, not the rig's arm64. "lines" prints every request any process
# made to the output guard socket. On the rig such a request is the only way a
# relay line moves, so an empty log means nothing asked for one.
#
#   deploy/local-rig.sh <subcommand>   (the sim-* subcommands are those of rig.sh)
#
# cure-compressed.sh drives it with RIG=deploy/local-rig.sh. LOCAL_RIG_DIR (default
# .rig-local) and LOCAL_RIG_UNIT (default cured-local) keep separate benches apart.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
L="${LOCAL_RIG_DIR:-$REPO_ROOT/.rig-local}"
UNIT="${LOCAL_RIG_UNIT:-cured-local}"   # several benches can run side by side with
GUARD_UNIT="$UNIT-guard"                # their own LOCAL_RIG_DIR and LOCAL_RIG_UNIT
mkdir -p "$L"
case "$L" in *[[:space:]]*) echo "local-rig.sh: LOCAL_RIG_DIR must not contain spaces" >&2; exit 2 ;; esac
export GOCACHE="${GOCACHE:-$L/gocache}" GOPATH="${GOPATH:-$L/gopath}"

lock() {
  [ -z "${RIG_LOCK_HELD:-}" ] || return 0
  exec 9>"$L/lock"
  flock -w 900 9 || { echo "local-rig.sh: timed out waiting for $L/lock" >&2; exit 75; }
}

# The facts the rig's provisioning establishes and cured checks (ADR 0201, 0208).
make_tree() {
  mkdir -p "$L/run" "$L/sim" "$L/hwrun" "$L/guard" "$L/sync" "$L/bin"
  local chip="$L/sys/devices/platform/axi/1000120000.pcie/1f00098000.pwm/pwm/pwmchip0"
  mkdir -p "$chip/pwm0" "$L/sys/class/pwm" "$L/sys/module/pinctrl_rp1/parameters"
  echo 40000 >"$chip/pwm0/period"; echo 1 >"$chip/pwm0/enable"; echo 0 >"$chip/pwm0/duty_cycle"
  ln -sfn ../../devices/platform/axi/1000120000.pcie/1f00098000.pwm/pwm/pwmchip0 "$L/sys/class/pwm/pwmchip0"
  echo N >"$L/sys/module/pinctrl_rp1/parameters/persist_gpio_outputs"
  : >"$L/sync/synchronized"
  [ -s "$L/boot_id" ] || cat /proc/sys/kernel/random/uuid >"$L/boot_id"
  [ -s "$L/hwrun/display-status" ] || echo "panel=present panel_source=firmware fb=/dev/fb0 bench=local-rig" >"$L/hwrun/display-status"
  if [ "$(stat -c %s "$L/fb0" 2>/dev/null || echo 0)" != 1536000 ]; then rm -f "$L/fb0"; truncate -s 1536000 "$L/fb0"; fi
}

# Arguments for bwrap. Paths contain no spaces (checked above), so a flat word list
# also works inside a systemd property.
sandbox() {
  local uid; uid=$(id -u)
  echo /usr/bin/bwrap --die-with-parent --ro-bind / / --dev /dev --proc /proc --tmpfs /tmp \
    --tmpfs /run --bind "/run/user/$uid" "/run/user/$uid" \
    --bind "$L/run" /run/curing-cured --bind "$L/hwrun" /run/curing --bind "$L/guard" /run/curing-outputs \
    --ro-bind "$L/sync" /run/systemd/timesync \
    --tmpfs /var/lib --bind "$L/sim" /var/lib/curing-sim \
    --ro-bind "$L/sys" /sys --bind "$L/fb0" /dev/fb0 \
    --ro-bind "$L/boot_id" /proc/sys/kernel/random/boot_id \
    --chdir /var/lib/curing-sim
}

guard_up() {
  systemctl --user is-active --quiet "$GUARD_UNIT" && return 0
  systemctl --user reset-failed "$GUARD_UNIT" 2>/dev/null || true
  rm -f "$L/guard/guard.sock"
  systemd-run --user --quiet --unit="$GUARD_UNIT" --collect -p Restart=always \
    /usr/bin/python3 -I "$REPO_ROOT/deploy/files/local-guard-recorder.py" "$L/guard/guard.sock" "$L/guard.log"
  for _ in $(seq 1 50); do [ -S "$L/guard/guard.sock" ] && return 0; sleep 0.1; done
  echo "local-rig.sh: guard recorder did not start" >&2; return 1
}

speed_outage() {
  # shellcheck disable=SC1091
  [ -f "$L/sim.env" ] && . "$L/sim.env"
  echo "${SIM_SPEED:-700} ${SIM_OUTAGE:-1x}"
}

start_unit() {
  make_tree
  guard_up
  local so sp out sb
  so=$(speed_outage); sp=${so% *}; out=${so#* }
  sb=$(sandbox)
  systemctl --user reset-failed "$UNIT" 2>/dev/null || true
  # shellcheck disable=SC2086
  systemd-run --user --quiet --unit="$UNIT" \
    -p Type=notify -p NotifyAccess=all -p WatchdogSec=10s -p TimeoutAbortSec=3s -p TimeoutStartSec=120s \
    -p TimeoutStopSec=60s -p Restart=always -p RestartSec=1s -p StartLimitIntervalSec=0 \
    -p "ExecStopPost=-$sb $L/bin/cured post-stop --sim" \
    $sb "$L/bin/cured" --sim --speed "$sp" --sim-outage "$out" --sim-confirm=not-a-real-cabinet
  echo "$UNIT.service $(systemctl --user is-active $UNIT)"
}

stop_unit() {
  systemctl --user stop "$UNIT" 2>/dev/null || true
  systemctl --user reset-failed "$UNIT" 2>/dev/null || true
}

simctl() {
  local a
  for a in "$@"; do case "$a" in *[!A-Za-z0-9._+-]*|"") echo "local-rig.sh: bad argument '$a'" >&2; exit 2 ;; esac; done
  # shellcheck disable=SC2046
  $(sandbox) "$L/bin/cured" simctl "$@"
}

mainpid() { systemctl --user show -p MainPID --value "$UNIT"; }
curedpid() { python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["pid"])' "$L/run/status.json" 2>/dev/null || echo 0; }

# Prints a timeline after a trigger until a new process has drawn a frame.
timeline() { # <trigger...>
  local oldpid nr0 t0 prev st pid
  oldpid=$(curedpid); nr0=$(systemctl --user show -p NRestarts --value "$UNIT")
  echo "unit=$UNIT oldpid=$oldpid NRestarts_before=$nr0"
  t0=$(date +%s.%N)
  "$@"
  echo "trigger_sent_at=$t0"
  rel() { awk -v a="$t0" -v b="$(date +%s.%N)" 'BEGIN{printf "%.3f", b-a}'; }
  prev=""
  for _ in $(seq 1 1200); do
    st=$(systemctl --user show -p ActiveState -p SubState -p MainPID --value "$UNIT" | tr '\n' ' ')
    [ "$st" = "$prev" ] || { echo "t=+$(rel)s $st"; prev="$st"; }
    pid=$(curedpid)
    if [ "$pid" != 0 ] && [ "$pid" != "$oldpid" ] && python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if d["first_frame_uptime"]>0 else 1)' "$L/run/status.json" 2>/dev/null; then
      echo "t=+$(rel)s status shows a panel frame from new process $pid"; break
    fi
    sleep 0.05
  done
  systemctl --user show -p NRestarts -p Result -p ActiveState "$UNIT"
  echo "== journal since trigger"
  journalctl --user --no-pager -u "$UNIT" --since "@${t0%.*}" -o short-precise | grep -E 'cured|panic:|systemd' | cut -c1-220 | head -40
}

usage() {
  sed -n '2,21p' "$0" >&2
  cat >&2 <<'USAGE'
subcommands:
  deploy-cured                 build cmd/cured and cmd/fbshot for this machine into $L/bin
  provision-sim <speed> <1x|compressed>   record the simulator's speed and outage mode
  cured-restart                (re)start the simulator unit
  stop                         stop the simulator unit (orderly)
  sim-reset                    stop the unit and wipe the simulator's state
  status                       unit state, NRestarts and /run/curing-cured/status.json
  lines                        what any process asked of the output guard (recorder log)
  journal <n>                  last n journal lines of the unit
  screenshot <out.png>         the panel framebuffer as a PNG
  boot-id                      the sandbox's boot ID
  power-cut <seconds>          SIGKILL everything, wipe /run, new boot ID, wait, start
  crash-test-service <KILL|ABRT|STOP>   signal cured and print the timeline
  sim-crash-test <panic-control|hang-control>
  hashes                       sha256 of the binaries in use
  sim-state sim-door sim-load sim-ambient sim-setpoint sim-fault sim-repair sim-start-cure
  sim-silence sim-ack sim-pause sim-resume sim-stop-cure sim-defrost sim-ui-fault   as rig.sh
USAGE
}

cmd="${1:-}"; shift || true
case "$cmd" in
  deploy-cured)
    command -v go >/dev/null 2>&1 || { echo "local-rig.sh: go not on PATH" >&2; exit 1; }
    lock; make_tree
    v=$(cd "$REPO_ROOT" && find cmd internal go.mod go.sum -type f ! -name '*_test.go' | LC_ALL=C sort | xargs sha256sum | sha256sum | cut -c1-16)
    (cd "$REPO_ROOT" && CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$v" -o "$L/bin/cured.new" ./cmd/cured && go build -o "$L/bin/fbshot" ./cmd/fbshot)
    mv -f "$L/bin/cured.new" "$L/bin/cured"
    echo "version $("$L/bin/cured" version)"; sha256sum "$L/bin/cured"
    ;;
  provision-sim)
    speed="${1:?usage: local-rig.sh provision-sim <speed> <1x|compressed>}"; outage="${2:?}"
    case "$speed" in *[!0-9]*) echo "bad speed" >&2; exit 2 ;; esac
    case "$outage" in 1x|compressed) ;; *) echo "bad outage mode" >&2; exit 2 ;; esac
    lock; make_tree
    printf 'SIM_SPEED=%s\nSIM_OUTAGE=%s\n' "$speed" "$outage" >"$L/sim.env"
    cat "$L/sim.env"
    ;;
  cured-restart) lock; stop_unit; start_unit ;;
  stop) lock; stop_unit; systemctl --user stop "$GUARD_UNIT" 2>/dev/null || true ;;
  sim-reset)
    lock; stop_unit
    rm -rf "${L:?}/sim" "${L:?}/run"; : >"$L/guard.log"; make_tree
    echo "simulator state wiped"
    ;;
  status)
    echo "$UNIT.service active=$(systemctl --user is-active $UNIT) NRestarts=$(systemctl --user show -p NRestarts --value $UNIT) result=$(systemctl --user show -p Result --value $UNIT)"
    cat "$L/run/status.json" 2>/dev/null || echo "no status file"
    ;;
  lines)
    echo "No GPIO on this bench. Requests made to the output guard (the only way a relay line moves on the rig):"
    if [ -s "$L/guard.log" ]; then grep -v 'recorder listening' "$L/guard.log" | tail -n "${1:-40}" || true; fi
    n=$(grep -c ' asked: ' "$L/guard.log" 2>/dev/null || true); echo "requests_total=${n:-0}"
    ;;
  journal) journalctl --user --no-pager -u "$UNIT" -n "${1:-200}" -o short-precise ;;
  screenshot)
    out="${1:?usage: local-rig.sh screenshot <out.png>}"
    raw=$(mktemp "$L/fb.XXXXXX"); trap 'rm -f "$raw"' EXIT
    cp "$L/fb0" "$raw"
    "$L/bin/fbshot" -in "$raw" -out "$out"; echo "$out"
    ;;
  boot-id) cat "$L/boot_id" 2>/dev/null || true ;;
  power-cut)
    secs="${1:?usage: local-rig.sh power-cut <seconds off>}"
    lock
    systemctl --user kill -s KILL "$UNIT" 2>/dev/null || true
    systemctl --user stop "$UNIT" 2>/dev/null || true
    systemctl --user reset-failed "$UNIT" 2>/dev/null || true
    rm -rf "${L:?}/run"; truncate -s 0 "$L/fb0"; truncate -s 1536000 "$L/fb0"
    cat /proc/sys/kernel/random/uuid >"$L/boot_id"
    echo "power off at $(date -u +%FT%T.%3NZ) for $secs s"; sleep "$secs"
    start_unit
    ;;
  crash-test-service)
    sig="${1:?usage: crash-test-service <KILL|ABRT|STOP>}"
    case "$sig" in KILL|ABRT|STOP) ;; *) exit 2 ;; esac
    lock
    timeline kill "-$sig" "$(curedpid)"
    ;;
  sim-crash-test)
    inj="${1:?usage: sim-crash-test <panic-control|hang-control>}"
    case "$inj" in panic-control|hang-control) ;; *) exit 2 ;; esac
    lock
    timeline simctl inject "$inj"
    ;;
  hashes) sha256sum "$L/bin/cured" "$L/bin/fbshot" 2>/dev/null; "$L/bin/cured" version ;;
  sim-state) simctl state ;;
  sim-door) case "${1:-}" in open|close) simctl door "$1" ;; *) exit 2 ;; esac ;;
  sim-load) simctl load "${1:?kg}" "${2:?temperature}" ;;
  sim-ambient) simctl ambient "${1:?offset}" ;;
  sim-setpoint) simctl setpoint "${1:?temperature}" "${2:?rh}" ;;
  sim-fault) simctl fault "${1:?device}" "${2:?kind}" ${3:+"$3"} ;;
  sim-repair) simctl repair "${1:?device or all}" ;;
  sim-start-cure) simctl start "${1:?recipe}" ;;
  sim-silence) simctl silence ;;
  sim-ack) simctl ack ;;
  sim-pause) simctl pause ;;
  sim-resume) simctl resume ;;
  sim-stop-cure) simctl stop ;;
  sim-defrost) simctl defrost ;;
  sim-ui-fault) case "${1:-}" in panic-ui|stall-ui) simctl inject "$1" ;; *) exit 2 ;; esac ;;
  *) usage; exit 2 ;;
esac
