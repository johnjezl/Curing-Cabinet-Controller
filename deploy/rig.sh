#!/usr/bin/env bash
# Single gateway for SSH access to the test rig. SSH to the rig is authorised by
# the project owner only for this repo's provisioning and deploy tooling, and
# only for the rig named below. Deliberately has no generic "run a command"
# subcommand: state changes must go through deploy/provision.sh.
#
# Every subcommand that touches the rig holds an exclusive flock on .rig/lock,
# including for the whole of a reboot, because several builders share the rig.
set -euo pipefail

RIG_HOST="${RIG_HOST:-192.168.4.142}"
RIG_USER="${RIG_USER:-john}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="$REPO_ROOT/.rig"
LOCK_WAIT="${RIG_LOCK_WAIT:-900}"
mkdir -p "$STATE_DIR"

# Local Go toolchain for fbshot and hwcheck. PATH wins; RIG_GO_DIR or the
# build sandbox's toolchain are fallbacks.
if ! command -v go >/dev/null 2>&1; then
  for d in "${RIG_GO_DIR:-}" /tmp/claude-1000/-home-john-projects-Curing-Cabinet-Controller--claude-worktrees-bridge-cse-014Q9arxsNRyixQaUNwLct8J/b4f63687-8409-5247-bdf7-bd66d4506c04/scratchpad/go/bin; do
    if [ -n "$d" ] && [ -x "$d/go" ]; then PATH="$d:$PATH"; break; fi
  done
fi
export GOCACHE="${GOCACHE:-$STATE_DIR/gocache}" GOPATH="${GOPATH:-$STATE_DIR/gopath}"

lock() {
  # A caller holding the lock for a whole run (evidence-b.sh) sets RIG_LOCK_HELD.
  [ -z "${RIG_LOCK_HELD:-}" ] || return 0
  exec 9>"$STATE_DIR/lock"
  flock -w "$LOCK_WAIT" 9 || { echo "rig.sh: timed out waiting for .rig/lock" >&2; exit 75; }
}

ssh_rig() {
  ssh -o BatchMode=yes -o ConnectTimeout=8 -o ServerAliveInterval=5 -o ServerAliveCountMax=3 -o StrictHostKeyChecking=accept-new \
      -o UserKnownHostsFile="$STATE_DIR/known_hosts" "$RIG_USER@$RIG_HOST" "$@" 9>&-
}

boot_id() { ssh_rig 'cat /proc/sys/kernel/random/boot_id' 2>/dev/null || true; }

wait_new_boot() { # <old boot id> <timeout seconds>
  local old="$1" limit="$2" start now id
  start=$(date +%s)
  while :; do
    now=$(date +%s)
    if [ $((now - start)) -gt "$limit" ]; then
      echo "rig.sh: rig did not come back within ${limit}s" >&2; return 1
    fi
    id=$(boot_id)
    if [ -n "$id" ] && [ "$id" != "$old" ]; then
      ssh_rig 'systemctl is-system-running --wait' >/dev/null 2>&1 || true
      echo "back after $((now - start)) s, boot_id $id"
      return 0
    fi
    sleep 2
  done
}

build_hwcheck() { # prints path of the arm64 binary
  local out="$STATE_DIR/hwcheck.arm64"
  (cd "$REPO_ROOT" && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$out" ./cmd/hwcheck) || exit 1
  echo "$out"
}

push_bin() { # <local> <name>: atomic install into /opt/curing/bin
  local src="$1" name="$2"
  case "$name" in *[!A-Za-z0-9._-]*|"") echo "rig.sh: bad binary name '$name'" >&2; exit 2 ;; esac
  ssh_rig "test -d /opt/curing/bin" || { echo "rig.sh: /opt/curing/bin missing, run: $0 provision" >&2; exit 1; }
  ssh_rig "set -e; t=\$(sudo mktemp /opt/curing/bin/.$name.XXXXXX); sudo tee \"\$t\" >/dev/null; sudo chown root:curing \"\$t\"; sudo chmod 0755 \"\$t\"; sudo mv -f \"\$t\" /opt/curing/bin/$name" <"$src"
}

start_test_driver() { # deliberately holds outputs on; ends at the reset that follows
  echo "DELIBERATE TEST DRIVER: hwcheck test-driver is commanding all outputs on and the fan to 50 %. This is not the control path."
  ssh_rig 'sudo systemctl stop curing-test-driver.service 2>/dev/null; sudo systemctl reset-failed curing-test-driver.service 2>/dev/null; sudo systemd-run --collect --quiet --unit=curing-test-driver --uid=curing --gid=curing --property=SupplementaryGroups=gpio /opt/curing/bin/hwcheck test-driver; sleep 2'
}

print_reset_bracket() { # <host epoch of the event> <ping log or empty>
  local t_event="$1" pinglog="$2" t_last_ping=none host_now up first_up
  if [ -n "$pinglog" ]; then
    t_last_ping=$(sed -n 's/^\[\([0-9.]*\)\].*bytes from.*/\1/p' "$pinglog" | awk -v t="$t_event" '$1 < t + 10 {l=$1} END {print l}')
  fi
  up=$(ssh_rig 'cut -d" " -f1 /proc/uptime'); host_now=$(date +%s.%N)
  ssh_rig 'bash -s' <<'REMOTE' | tee "$STATE_DIR/bracket.txt"
echo "== after the reset, relay lines and fan pin"
sudo -u curing /opt/curing/bin/hwcheck lines
sudo -u curing /opt/curing/bin/hwcheck fan-duty
echo "== test driver running: $(systemctl is-active curing-test-driver.service 2>/dev/null)"
echo "== reset status from the bootloader"; for f in bootloader/rsts power/power_reset; do echo "$f=$(od -An -tx1 /proc/device-tree/chosen/$f | tr -d ' ')"; done
echo "== earliest line state this boot, from curing-hw-init"
sudo journalctl --no-pager -b -u curing-hw.service -o cat | grep -A7 'lines at start of bring-up'
echo "== last entries of the previous boot"; sudo journalctl --no-pager -b -1 -n 4 -o short-precise
REMOTE
  first_up=$(sed -n 's/.*uptime=\([0-9.]*\) lines at start.*/\1/p' "$STATE_DIR/bracket.txt" | head -1)
  awk -v e="$t_event" -v p="$t_last_ping" -v n="$host_now" -v u="$up" -v f="${first_up:-0}" 'BEGIN {
    k = n - u
    if (p == "none" || p == "") p = e
    printf "== timing on the dev host clock, seconds. Pings are 0.2 s apart; nothing on the rig observes the lines between these points\n"
    printf "command_sent=%.3f\nlast_sign_of_old_kernel=%.3f\nnew_kernel_started=%.3f\nfirst_line_read_this_boot=%.3f\n", e, p, k, k + f
    printf "old_kernel_alive_after_command_s=%.3f\nnew_kernel_started_after_last_sign_s=%.3f\nfirst_line_read_after_last_sign_s=%.3f\n", p - e, k - p, k + f - p
  }'
  rm -f "${STATE_DIR:?}/bracket.txt"
}

# The two application units. The shadow is what service-restart below leaves for the
# panel evidence; an application subcommand must never run underneath it.
stop_app() { ssh_rig 'sudo systemctl stop curing.service curing-sim.service 2>/dev/null || true'; }

clear_shadow() {
  ssh_rig 'if [ -e /run/systemd/system/curing.service ]; then sudo systemctl stop curing.service 2>/dev/null; sudo rm -f /run/systemd/system/curing.service; sudo systemctl daemon-reload; fi'
}

build_cured() { # prints path of the arm64 binary
  local out="$STATE_DIR/cured.arm64" v
  v=$(cd "$REPO_ROOT" && find cmd internal go.mod go.sum -type f -name '*' ! -name '*_test.go' | LC_ALL=C sort | xargs sha256sum | sha256sum | cut -c1-16)
  (cd "$REPO_ROOT" && GOMAXPROCS=2 GOOS=linux GOARCH=arm64 CGO_ENABLED=0 nice -n 19 go build -p 2 -trimpath -ldflags "-X main.version=$v" -o "$out" ./cmd/cured) || exit 1
  echo "$out"
}

simctl() { # <args validated by the caller's case>: only a --sim process has this socket
  local a
  for a in "$@"; do case "$a" in *[!A-Za-z0-9._+-]*|"") echo "rig.sh: bad argument '$a'" >&2; exit 2 ;; esac; done
  lock
  ssh_rig "sudo -u curing /opt/curing/bin/cured simctl $*"
}

# Prints a timeline on the rig's clock after a trigger, until the service is back up.
crash_timeline() { # <trigger command run on the rig>
  ssh_rig "sudo bash -s" <<REMOTE
TRIGGER='$1'
REMOTE_SCRIPT=1
$(cat <<'INNER'
unit=curing.service; systemctl is-active --quiet curing-sim.service && unit=curing-sim.service
oldpid=$(systemctl show -p MainPID --value $unit); nr0=$(systemctl show -p NRestarts --value $unit)
line() { pinctrl get "$1" 2>/dev/null | grep -oE '\b(hi|lo)\b' | head -1; }
echo "unit=$unit oldpid=$oldpid NRestarts_before=$nr0 gpio24_before=$(line 24) gpio25_before=$(line 25)"
t0=$(date +%s.%N)
eval "$TRIGGER"
echo "trigger_sent_at=$t0"
rel() { awk -v a="$t0" -v b="$(date +%s.%N)" 'BEGIN{printf "%.3f", b-a}'; }
prev=""; deadline=$(( $(date +%s) + 60 )); newpid=""; ready_seen=""
while [ "$(date +%s)" -lt "$deadline" ]; do
  st=$(systemctl show -p ActiveState -p SubState -p MainPID --value $unit | tr '
' ' ')
  l="$st gpio24=$(line 24) gpio25=$(line 25)"
  if [ "$l" != "$prev" ]; then echo "t=+$(rel)s $l"; prev="$l"; fi
  pid=$(systemctl show -p MainPID --value $unit)
  if [ "$pid" != "$oldpid" ] && [ "$pid" != 0 ] && [ "$(systemctl show -p ActiveState --value $unit)" = active ]; then
    [ -n "$newpid" ] || { newpid=$pid; echo "t=+$(rel)s service active again, main pid $pid"; }
    if python3 -c "import json;d=json.load(open('/run/curing-cured/status.json'));import sys;sys.exit(0 if d['pid']==$pid and d['first_frame_uptime']>0 else 1)" 2>/dev/null; then
      echo "t=+$(rel)s status shows a panel frame from the new process"; break
    fi
  fi
  sleep 0.05
done
systemctl show -p NRestarts -p Result -p ActiveState $unit
echo "== status"; cat /run/curing-cured/status.json
echo "== journal since trigger"; journalctl --no-pager -u $unit --since "@${t0%.*}" -o short-precise | grep -E ' cured: | cured post-stop|systemd\[1\]|panic:|SIGABRT: abort' | cut -c1-200 | head -40
INNER
)
REMOTE
}

usage() {
  cat >&2 <<USAGE
usage: $0 <subcommand>
  probe                  read-only inventory of the rig
  probe-hw               read-only kernel, pin, watchdog and storage facts
  probe-overlays         read-only decompile of the stock overlays used
  provision              run deploy/provision.sh --bench on this bench rig (idempotent)
  provision-default      run deploy/provision.sh as a real cabinet would: no test helper
  screenshot <out.png>   grab /dev/fb0 and convert to PNG locally
  deploy <binary> [name] install a local arm64 binary as /opt/curing/bin/<name> (default curing)
  service-restart        run /opt/curing/bin/curing (panel evidence) under the name curing.service,
                         shadowing the real unit until a reboot or cured-restart
  service-kill <TERM|KILL> signal the service's main process (test aid)
  probe-unit             read-only state of curing.service
  service-stop           stop curing.service
  service-status         show curing.service status
  journal <n> [unit]     last n journal lines (default unit curing.service, 'all' for everything)
  hwcheck <sub> [args]   build, install and run cmd/hwcheck as the service user
                         (driver-calls, pwm, lines, time-to-safe, safe-state, try-on,
                         hx711-timing; bring-up: read-sensors, relay-click, fan-sweep)
  hwstate                read-only provisioned device and pin state (raw)
  hashes                 sha256 of the binaries and files installed on the rig
  probe-panel            read-only: what the firmware applied for the panel
  panel-overlay <args>   experiment: load a stock panel overlay at runtime on this boot
  bench-helper-state     read-only: whether the bench test helper is installed
  sudo-check             whether passwordless sudo works
  test-driver-stop       end a test driver left holding outputs on
  records [n]            crash record index, and the newest n records' headers
  reboot                 graceful reboot, holding the lock until the rig is back
  crash-test <panic|halt> test driver holds outputs on, then panic the kernel. panic: reset
                         by kernel.panic=1. halt: kernel.panic=0, only the hardware
                         watchdog can reset. Prints the time bracket and reset status.
  safety-param-test      set persist_gpio_outputs=Y, show outputs are refused, restore
  power-cut-before       test driver holds outputs on and fan at 50 % before a labctl cut
  power-cut-after <t>    state after that cut; t is the host epoch of the cut
  lock-hold <seconds>    hold the rig lock locally (around a labctl power cycle)

 the application (cmd/cured, units curing.service and curing-sim.service):
  deploy-cured           build cmd/cured for arm64 and install it as /opt/curing/bin/cured
  cured-restart          restart whichever application unit is enabled
  provision-sim <speed> <1x|compressed>  provision --bench with the simulator starting at boot
  status                 /run/curing-cured/status.json, the unit states and NRestarts
  lines                  the six relay lines and the fan pin, read from the RP1
  boot-timing            this boot's timing lines, systemd-analyze, and the unit timestamps
  crash-test-service <KILL|ABRT|STOP>  signal the app's main process and print the timeline
  display-missing-test <on|off>  stand-in for a blank-panel boot (tmpfs), then restart the app
  demo-start <name>      run /opt/curing/bin/<name> as curing-demo.service, stopping the app and
                         remembering whether it ran (unit under /run, gone after a reboot)
  demo-stop              stop the demo; restart the app only if it was running before
  boot-id                this boot's ID, empty if the rig does not answer
  unit-config            what systemd has for the application units, read-only
  kernel-panic-test      kernel panic with the application running, then wait for the rig
  real-reset             bench only: wipe real-mode state back to first boot
  sigkill-capture <dir>  SIGKILL the app and save framebuffer frames every 200 ms as PNGs
  sim-gate-check         run cured with incomplete --sim arguments and print each refusal

 simulator control, only answered while cured runs with --sim:
  sim-state              JSON snapshot, plant ground truth, outages
  sim-reset              stop the simulator and wipe its state (never the real state)
  sim-start-cure <recipe> start a recipe; test-12d is a 12 day three-phase cure
  sim-door <open|close>  sim-load <kg> <C>  sim-ambient <offset C>  sim-setpoint <C> <%RH>
  sim-fault <device> <kind> [param]  sim-repair <device|all>
  sim-silence  sim-ack  sim-pause  sim-resume  sim-stop-cure  sim-defrost
  sim-crash-test <panic-control|hang-control>  inject and print the timeline
  sim-ui-fault <panic-ui|stall-ui>  fail the panel's data path, control unaffected
USAGE
}

cmd="${1:-}"; shift || true
case "$cmd" in
  probe) # Read-only inventory of what the rig offers.
    lock
    ssh_rig 'bash -s' <<'PROBE'
echo "== id/os"; id; uname -a; head -3 /etc/os-release; uptime
echo "== disk"; df -h / /boot/firmware | sed 1d
echo "== devices"; ls -l /dev/fb* /dev/dri/* /dev/input/event* /dev/gpiochip* /dev/i2c* /dev/watchdog* /dev/rtc* /dev/spidev* 2>&1
ls /sys/class/pwm 2>&1
echo "== fb"; for f in /sys/class/graphics/fb0/{name,virtual_size,bits_per_pixel,stride}; do echo "$f: $(cat $f 2>&1)"; done
echo "== drm"; for c in /sys/class/drm/card*-*; do echo "$c $(cat $c/status 2>/dev/null) $(head -1 $c/modes 2>/dev/null)"; done
echo "== input"; grep -E '^(N|H):' /proc/bus/input/devices
echo "== overlays"; which dtoverlay dtparam pinctrl raspi-gpio i2cdetect 2>&1; ls /sys/kernel/config/device-tree 2>&1; dtoverlay -l 2>&1 | head
echo "== config.txt (read only)"; grep -vE '^\s*(#|$)' /boot/firmware/config.txt
echo "== cmdline"; cat /boot/firmware/cmdline.txt
echo "== services"; systemctl is-system-running; systemctl get-default; systemctl list-units --type=service --state=running --no-legend --plain | awk '{print $1}' | tr '\n' ' '; echo
echo "== getty on tty1"; systemctl is-active getty@tty1
echo "== clock"; timedatectl 2>&1 | head -6
echo "== watchdog"; systemctl show -p RuntimeWatchdogUSec -p RebootWatchdogUSec
echo "== sudo"; sudo -n true && echo passwordless-sudo-ok || echo NO-passwordless-sudo
PROBE
    ;;
  probe-hw) # Read-only: kernel and overlay facts the drivers depend on.
    lock
    ssh_rig 'sudo bash -s' <<'PROBE'
echo "== overlays dir"; ls /boot/firmware/overlays | grep -E 'pwm|i2c1|dht|overlay_map'
echo "== tools"; which dtc dtoverlay dtparam fdtdump gpiodetect i2cdetect wdctl fdtoverlay
echo "== modules"; lsmod | grep -E 'i2c|dht|pwm|industrialio|rp1'
echo "== param"; ls /sys/module/pinctrl_rp1/parameters 2>&1; grep . /sys/module/pinctrl_rp1/parameters/* 2>&1
echo "== symbols"; for s in i2c1 i2c_arm rp1_pwm0 rp1_pwm1 rp1_gpio gpio; do [ -e /proc/device-tree/__symbols__/$s ] && echo "$s -> $(tr -d '\0' < /proc/device-tree/__symbols__/$s)"; done
echo "== rp1 pwm pin node"; n=$(tr -d '\0' < /proc/device-tree/__symbols__/rp1_pwm1_gpio45); ls /proc/device-tree$n; for f in function pins; do tr '\0' ' ' < /proc/device-tree$n/$f; echo; done; for g in 12 13 18 19; do pinctrl funcs $g 2>&1; done
echo "== pwm"; for c in /sys/class/pwm/*; do echo "$c $(readlink -f $c) npwm=$(cat $c/npwm)"; done; cat /sys/kernel/debug/pwm
echo "== pinctrl"; pinctrl get 0-27
echo "== gpiomem"; ls -l /dev/gpiomem* /dev/mem 2>&1
echo "== wd"; wdctl 2>&1; grep . /sys/class/watchdog/watchdog0/{identity,timeout,max_timeout,min_timeout,nowayout} 2>&1
echo "== sysctl"; sysctl kernel.panic kernel.panic_on_oops kernel.sysrq vm.swappiness
echo "== storage"; swapon --show; findmnt -no OPTIONS /; findmnt /tmp; journalctl --disk-usage; ls /etc/systemd/journald.conf.d /var/log/journal 2>&1
echo "== vt"; for v in /sys/class/vtconsole/*; do echo "$v bind=$(cat $v/bind) $(cat $v/name)"; done; cat /proc/consoles
echo "== reset cause candidates"; for f in /proc/device-tree/chosen/bootloader/* /proc/device-tree/chosen/power/*; do printf '%s = ' "$f"; od -An -tx1 "$f" | tr -d '\n' | cut -c1-60; echo; done
echo "== pstore"; mount | grep -i pstore; ls -la /sys/fs/pstore; grep -E 'PSTORE|RAMOOPS' /boot/config-$(uname -r) | head
echo "== rtc"; cat /sys/class/rtc/rtc0/name /sys/class/rtc/rtc0/date /sys/class/rtc/rtc0/time; ls /sys/class/rtc/rtc0; dmesg | grep -iE 'rtc|setting system clock' | head -5; dmesg | grep -iE 'recover|EXT4-fs.*(error|orphan)|mmc0|timed out' | head -8
PROBE
    ;;
  probe-overlays) # Read-only: decompile the stock overlays the provisioning relies on.
    lock
    ssh_rig 'bash -s' <<'PROBE'
for o in pwm i2c1-pi5 dht11; do echo "=== $o"; dtc -q -I dtb -O dts /boot/firmware/overlays/$o.dtbo; done
echo "=== dtoverlay help"; dtoverlay -h dht11 2>&1 | head; dtoverlay -h i2c1-pi5 2>&1 | head -20
PROBE
    ;;
  probe-display) # Read-only: display bring-up on this boot.
    lock
    ssh_rig 'bash -s' <<'PROBE'
uptime; ls -l /dev/fb* /dev/dri/ 2>&1; for c in /sys/class/drm/card*-*; do echo "$c $(cat $c/status 2>/dev/null)"; done
for v in /sys/class/vtconsole/*; do echo "$v bind=$(cat $v/bind) $(cat $v/name)"; done
lsmod | grep -E 'rp1_dsi|panel|tc358762|vc4|rpi_panel'
sudo dmesg | grep -iE 'dsi|panel|fb0|drm|frame buffer|attiny|tc358|deferred' | cut -c1-140 | tail -30
echo "== kernel lines only in the boot without a panel (-3 vs -4), times stripped"
strip() { sudo journalctl --no-pager -b "$1" -k -o cat | sed -E 's/\[ *[0-9.]+\]//; s/[0-9]+\.[0-9]+//g' | sort -u; }
echo "-- only in -3"; comm -23 <(strip -3) <(strip -4) | cut -c1-150 | head -40
echo "-- only in -4"; comm -13 <(strip -3) <(strip -4) | cut -c1-150 | head -40
echo "== last 12 lines of boot -4"; sudo journalctl --no-pager -b -4 -n 12 -o short-monotonic | cut -c1-130
echo "== curing.service in boot -4"; sudo journalctl --no-pager -b -4 -u curing.service -n 6 -o short-monotonic | cut -c1-130
echo "== recent boots"; sudo journalctl --list-boots --no-pager | tail -9
for b in -1 -2 -3 -4 -5 -6 -7 -8; do
  echo "=== boot $b"; sudo journalctl --no-pager -b $b -k -o short-monotonic 2>/dev/null | grep -E 'fb0:|rp1dsi_bind|drm-rp1-dsi.*(fail|error|-[0-9]+)|Initialized drm-rp1-dsi|i2c.*timeout|tc358762.*(fail|error)|attiny.*(fail|error)|edt_ft5x06.*(fail|error)' | cut -c1-130 | head -6
  sudo journalctl --no-pager -b $b -o short-monotonic -u curing-hw.service 2>/dev/null | grep -E 'Finished|uptime=' | cut -c1-110
  sudo journalctl --no-pager -b $b -k -o short-monotonic 2>/dev/null | grep -c . 
done
PROBE
    ;;
  probe-panel) # Read-only: what the firmware applied for the panel, on this boot.
    lock
    ssh_rig 'sudo bash -s' <<'PROBE'
echo "== boot"; cat /proc/sys/kernel/random/boot_id; cut -d" " -f1 /proc/uptime
echo "panel_state: fb0=$([ -e /dev/fb0 ] && echo yes || echo no) touch=$(grep -c ft5x06 /proc/bus/input/devices) rsts=$(od -An -tx1 /proc/device-tree/chosen/bootloader/rsts | tr -d ' ') firmware_panel_overlay=$(vclog --msg 2>/dev/null | grep -c "Loaded overlay .vc4-kms-dsi-7inch") $(tr '\n' ' ' < /run/curing/display-status 2>/dev/null) missing_marker=$([ -e /run/curing/display-missing ] && echo yes || echo no)"
echo "== firmware log"; vclog --msg 2>&1 | grep -iE 'dtoverlay|overlay|dsi|display|panel|i2c|Loaded|dtparam|auto.?detect' | cut -c1-150 | tail -30
echo "== config as seen by firmware"; vcgencmd get_config str 2>&1 | head -10; vcgencmd get_config display_auto_detect 2>&1
echo "== live dt"; D=/proc/device-tree/axi/pcie@1000120000/rp1/dsi@110000; for f in status compatible; do printf "dsi0 $f="; tr '\0' ' ' < $D/$f; echo; done; ls $D | tr '\n' ' '; echo
for n in $D/bridge@0 /proc/device-tree/panel_disp@0 /proc/device-tree/panel_disp1@0; do [ -e "$n" ] && { printf "$n: "; tr '\0' ' ' < $n/compatible; echo; }; done
for s in dsi0 dsi1; do [ -e /proc/device-tree/__symbols__/$s ] && echo "$s -> $(tr -d '\0' < /proc/device-tree/__symbols__/$s)"; done
echo "== i2c10 nodes"; ls /proc/device-tree/axi/pcie@1000120000/rp1/i2c@88000/ | tr '\n' ' '; echo
for f in /proc/device-tree/axi/pcie@1000120000/rp1/i2c@88000/*/compatible; do echo "$f: $(tr '\0' ' ' < $f)"; done
echo "== panel overlays available"; ls /boot/firmware/overlays | grep -E 'dsi|7inch|panel' | tr '\n' ' '; echo
dtoverlay -h vc4-kms-dsi-7inch 2>&1 | head -24
dtoverlay -l
PROBE
    ;;
  panel-overlay) # Experiment: load a stock panel overlay at runtime on this boot.
    case "$*" in *[!A-Za-z0-9._=\ -]*|"") echo "rig.sh: bad overlay arguments" >&2; exit 2 ;; esac
    lock
    ssh_rig "sudo /opt/curing/libexec/curing-rig-test panel-overlay $*; sleep 2; ls -l /dev/fb0 2>&1; grep -c ft5x06 /proc/bus/input/devices; sudo dmesg | grep -iE 'dsi|fb0|ft5x06|attiny|tc358' | tail -12 | cut -c1-140"
    ;;
  probe-sudo) # Read-only, no sudo: why sudo behaves as it does after a reboot.
    lock
    ssh_rig 'bash -s' <<'PROBE'
id; uptime; sudo -n true; echo "sudo rc=$?"; sudo -n -l 2>&1 | tail -5
ls -la /etc/sudoers /etc/sudoers.d 2>&1; ls -la /opt/curing /opt/curing/bin 2>&1
findmnt / ; ls -la --time-style=full-iso /etc/passwd /etc/group /etc/shadow /usr/bin/sudo
grep -E '^(john|sudo|curing)' /etc/group; getent passwd john curing
journalctl --no-pager -b -n 25 -t sudo 2>&1 | tail -25
journalctl --no-pager -b 2>/dev/null | grep -iE 'sudoers|cloud-init.*(sudo|user)|users-groups' | tail -20
echo "== earlier boots"; journalctl --list-boots --no-pager 2>&1 | tail -6
echo "== first sudo uses and logins, previous boot"; journalctl --no-pager -b -1 -t sudo 2>&1 | grep -vE 'pam_unix|session (opened|closed)' | head -12
journalctl --no-pager -b -1 2>&1 | grep -iE 'sudoers|visudo|session-.*(login|opened)|Accepted' | head -12
echo "== sudo config readable"; cat /etc/sudoers 2>&1 | head -2; ls -la /run/sudo /run/sudo/ts /run/sudoers.d 2>&1; cat /etc/sudo.conf 2>/dev/null | grep -vE '^(#|$)'
echo "== journal files"; cat /etc/machine-id; ls -la /var/log/journal /var/log/journal/* 2>&1 | head -20; journalctl --header 2>/dev/null | grep -E 'File path|Head timestamp|Tail timestamp' | head -12; systemd-analyze cat-config systemd/journald.conf 2>/dev/null | grep -vE '^(#|$)'; journalctl --no-pager -b -u systemd-journald 2>&1 | tail -6
echo "== hw bring-up"; systemctl is-active curing-hw.service; systemctl is-system-running; journalctl --no-pager -b -u curing-hw.service 2>&1 | tail -15
ls -l /dev/i2c-1; pinctrl get 12 2>&1; dtoverlay -l 2>&1; systemctl show -p RuntimeWatchdogUSec; systemctl is-active getty@tty1; for v in /sys/class/vtconsole/*; do echo "$v bind=$(cat $v/bind)"; done
PROBE
    ;;
  provision|provision-default)
    # This rig is a bench, so provision adds the test helper. provision-default is what a
    # real cabinet gets: no bench flag, and the helper removed if it was there.
    flag=--bench; [ "$cmd" = provision ] || flag=""
    lock
    (cd "$REPO_ROOT" && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$STATE_DIR/curing-outputd" ./cmd/curing-outputd) || exit 1
    tar -cz -C "$REPO_ROOT/deploy" provision.sh files curing.service curing-sim.service -C "$STATE_DIR" curing-outputd | ssh_rig '
      set -e; d=$(mktemp -d /tmp/curing-provision.XXXXXX); trap "rm -rf \"$d\"" EXIT
      tar -C "$d" -xz; sudo bash "$d/provision.sh" "$d" '"$flag"
    ;;
  provision-sim)
    speed="${1:?usage: rig.sh provision-sim <speed> <1x|compressed>}"; outage="${2:?usage: rig.sh provision-sim <speed> <1x|compressed>}"
    case "$speed" in *[!0-9]*) echo "rig.sh: bad speed" >&2; exit 2 ;; esac
    case "$outage" in 1x|compressed) ;; *) echo "rig.sh: bad outage mode" >&2; exit 2 ;; esac
    lock
    clear_shadow
    (cd "$REPO_ROOT" && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$STATE_DIR/curing-outputd" ./cmd/curing-outputd) || exit 1
    tar -cz -C "$REPO_ROOT/deploy" provision.sh files curing.service curing-sim.service -C "$STATE_DIR" curing-outputd | ssh_rig '
      set -e; d=$(mktemp -d /tmp/curing-provision.XXXXXX); trap "rm -rf \"$d\"" EXIT
      tar -C "$d" -xz; sudo bash "$d/provision.sh" "$d" --bench --sim-boot '"$speed $outage"
    ;;
  deploy-cured)
    bin=$(build_cured)
    lock
    push_bin "$bin" cured
    ssh_rig "ls -l /opt/curing/bin/cured; /opt/curing/bin/cured version"
    sha256sum "$bin"
    ;;
  cured-restart)
    lock
    clear_shadow
    ssh_rig 'u=curing.service; systemctl is-enabled --quiet curing-sim.service 2>/dev/null && u=curing-sim.service; sudo systemctl reset-failed $u 2>/dev/null || true; sudo systemctl restart $u; echo "$u $(systemctl is-active $u)"'
    ;;
  status)
    lock
    ssh_rig 'for u in curing-hw curing-outputd curing curing-sim; do echo "$u.service enabled=$(systemctl is-enabled $u.service 2>/dev/null) active=$(systemctl is-active $u.service 2>/dev/null) NRestarts=$(systemctl show -p NRestarts --value $u.service 2>/dev/null) result=$(systemctl show -p Result --value $u.service 2>/dev/null)"; done; cat /run/curing-cured/status.json 2>/dev/null || echo "no status file"'
    ;;
  lines)
    lock
    ssh_rig 'for p in 17 27 22 23 24 25 12; do sudo pinctrl get $p; done'
    ;;
  boot-timing)
    lock
    ssh_rig 'sudo bash -s' <<'REMOTE'
echo "boot_id=$(cat /proc/sys/kernel/random/boot_id) uptime_now=$(cut -d" " -f1 /proc/uptime)"
echo "rsts=$(od -An -tx1 /proc/device-tree/chosen/bootloader/rsts | tr -d " ")"
systemd-analyze 2>&1 | head -3
for u in curing-hw curing-outputd curing curing-sim; do echo "$u.service ActiveEnterMonotonic_us=$(systemctl show -p ActiveEnterTimestampMonotonic --value $u.service) active=$(systemctl is-active $u.service)"; done
journalctl --no-pager -b -o short-monotonic -u curing.service -u curing-sim.service | grep -E 'timing|start mode|outage accounted|Started' | cut -c1-260 | head -20
echo "== status"; cat /run/curing-cured/status.json 2>/dev/null
timedatectl show -p NTPSynchronized; echo "rtc_epoch=$(cat /sys/class/rtc/rtc0/since_epoch) system_epoch=$(date +%s)"
ls -l /run/curing/; grep -H . /run/curing/display-status
REMOTE
    ;;
  crash-test-service)
    sig="${1:?usage: rig.sh crash-test-service <KILL|ABRT|STOP>}"
    case "$sig" in KILL|ABRT|STOP) ;; *) echo "rig.sh: signal must be KILL, ABRT or STOP" >&2; exit 2 ;; esac
    lock
    crash_timeline "systemctl kill --kill-whom=main -s $sig \$unit"
    ;;
  sim-crash-test)
    inj="${1:?usage: rig.sh sim-crash-test <panic-control|hang-control>}"
    case "$inj" in panic-control|hang-control) ;; *) echo "rig.sh: bad injection" >&2; exit 2 ;; esac
    lock
    crash_timeline "sudo -u curing /opt/curing/bin/cured simctl inject $inj"
    ;;
  display-missing-test)
    what="${1:?usage: rig.sh display-missing-test <on|off>}"
    case "$what" in on|off) ;; *) exit 2 ;; esac
    lock
    clear_shadow
    ssh_rig "sudo /opt/curing/libexec/curing-rig-test display-missing-$what; u=curing.service; systemctl is-enabled --quiet curing-sim.service 2>/dev/null && u=curing-sim.service; sudo systemctl restart \$u; ls -l /run/curing/"
    ;;
  demo-start)
    # Runs a panel demo binary without touching the application's binary or unit.
    # Everything is under /run, so a reboot removes it.
    name="${1:?usage: rig.sh demo-start <name in /opt/curing/bin>}"
    case "$name" in *[!A-Za-z0-9._-]*) echo "rig.sh: bad binary name" >&2; exit 2 ;; esac
    case "$name" in cured|curing-outputd|hwcheck) echo "rig.sh: $name is not a demo" >&2; exit 2 ;; esac
    lock
    clear_shadow
    ssh_rig "bash -s $name" <<'REMOTE'
set -e
name="$1"
[ -x "/opt/curing/bin/$name" ] || { echo "no /opt/curing/bin/$name; run rig.sh deploy first" >&2; exit 1; }
# Remember what was running only on the first start, so a second demo-start in a row
# does not overwrite it with "nothing".
if [ ! -e /run/curing-demo.was-active ]; then
  was=""
  for u in curing.service curing-sim.service; do systemctl is-active --quiet "$u" && was="$u"; done
  echo "$was" | sudo tee /run/curing-demo.was-active >/dev/null
fi
sudo systemctl stop curing.service curing-sim.service 2>/dev/null || true
printf '%s\n' '[Unit]' 'Description=curing panel demo (from rig.sh demo-start, not the application)' \
  'After=curing-hw.service curing-outputd.service' 'Conflicts=curing.service curing-sim.service' '' \
  '[Service]' 'User=curing' 'Group=curing' 'SupplementaryGroups=gpio i2c video input render tty' \
  'StateDirectory=curing' 'StateDirectoryMode=0750' 'WorkingDirectory=/var/lib/curing' "ExecStart=/opt/curing/bin/$name" \
  | sudo tee /run/systemd/system/curing-demo.service >/dev/null
sudo systemctl daemon-reload
sudo systemctl reset-failed curing-demo.service 2>/dev/null || true
sudo systemctl restart curing-demo.service
sleep 1
systemctl --no-pager --lines=0 status curing-demo.service || true
REMOTE
    ;;
  demo-stop)
    lock
    ssh_rig 'bash -s' <<'REMOTE'
sudo systemctl stop curing-demo.service 2>/dev/null || true
sudo rm -f /run/systemd/system/curing-demo.service
sudo systemctl daemon-reload
was=$(cat /run/curing-demo.was-active 2>/dev/null || true)
sudo rm -f /run/curing-demo.was-active
if [ -n "$was" ]; then sudo systemctl restart "$was"; echo "restarted $was: $(systemctl is-active "$was")"; else echo "nothing to restart"; fi
REMOTE
    ;;
  boot-id) # Read-only. Empty output if the rig is not answering.
    lock
    boot_id
    ;;
  unit-config) # Read-only: what systemd has for the application units.
    lock
    ssh_rig 'bash -s' <<'REMOTE'
for u in curing.service curing-sim.service curing-outputd.service; do
  echo "== $u"; systemctl show -p Type -p NotifyAccess -p WatchdogUSec -p TimeoutAbortUSec -p Restart -p RestartUSec -p StartLimitIntervalUSec -p StartLimitBurst -p After -p Wants -p Conflicts -p UnitFileState -p FragmentPath "$u"
done
echo "== PID 1 hardware watchdog"; systemctl show -p RuntimeWatchdogUSec -p RebootWatchdogUSec; wdctl 2>&1 | head -8; sysctl kernel.panic kernel.panic_on_oops 2>/dev/null || /usr/sbin/sysctl kernel.panic kernel.panic_on_oops
echo "== production marker and simulator settings"; ls -l /etc/curing/ 2>&1; cat /etc/curing/sim.env 2>/dev/null
REMOTE
    ;;
  kernel-panic-test) # Kernel panic (not an application panic) with the application running.
    lock
    old=$(boot_id)
    echo "boot_id_before=$old"
    ssh_rig 'for p in 24 25; do sudo pinctrl get $p; done; cat /run/curing-cured/status.json; sync'
    t_cmd=$(date +%s.%N)
    echo "panic_command_at=$t_cmd"
    ssh_rig "sudo /opt/curing/libexec/curing-rig-test panic" || true
    wait_new_boot "$old" 300 | sed 's/^back after/wait loop ended after/'
    t_back=$(date +%s.%N)
    echo "host_clock_when_back=$t_back"
    awk -v a="$t_cmd" -v b="$t_back" 'BEGIN{printf "panic_command_to_ssh_answering_s=%.1f\n", b-a}'
    ;;
  real-reset) # Bench only: real-mode state back to first boot, so a test starts from nothing.
    lock
    clear_shadow
    ssh_rig 'sudo systemctl stop curing.service curing-sim.service 2>/dev/null || true; sudo /opt/curing/libexec/curing-rig-test wipe-real-state; echo "real state wiped"'
    ;;
  sigkill-capture) # SIGKILL the app, grabbing the framebuffer on the rig every 200 ms meanwhile.
    out="${1:?usage: rig.sh sigkill-capture <out dir>}"
    mkdir -p "$out"
    (cd "$REPO_ROOT" && go build -o "$STATE_DIR/fbshot" ./cmd/fbshot)
    lock
    ssh_rig 'sudo bash -s' <<'REMOTE' | tar -xz -C "$out"
d=$(mktemp -d /dev/shm/curing-frames.XXXXXX); trap 'rm -rf "$d"' EXIT
unit=curing.service; systemctl is-active --quiet curing-sim.service && unit=curing-sim.service
systemctl kill --kill-whom=main -s KILL $unit
t0=$(date +%s%N)
for i in $(seq 1 25); do
  ms=$(( ($(date +%s%N) - t0) / 1000000 ))
  dd if=/dev/fb0 bs=3200 count=480 of="$d/$(printf 'frame-%05dms.raw' "$ms")" 2>/dev/null
  sleep 0.2
done
journalctl --no-pager -u $unit -n 25 -o short-precise | grep -E 'SIGKILL|Main process exited|post-stop|Scheduled|first_panel_frame' > "$d/journal.txt"
tar -cz -C "$d" . >&1
REMOTE
    for f in "$out"/frame-*.raw; do "$STATE_DIR/fbshot" -in "$f" -out "${f%.raw}.png" >/dev/null; rm -f "$f"; done
    ls "$out"
    ;;
  sim-gate-check)
    lock
    ssh_rig 'bash -s' <<'REMOTE'
try() { echo "\$ cured $*"; sudo -u curing /opt/curing/bin/cured "$@" 2>&1 | cut -c1-200; echo "exit=${PIPESTATUS[0]}"; }
try --sim
try --speed 400
try --sim --speed 400
try --sim --speed 400 --sim-outage 1x
try --sim --speed 400 --sim-outage 1x --sim-confirm=yes
try stray
echo "\$ cured simctl state  (real mode has no socket)"; sudo -u curing /opt/curing/bin/cured simctl state 2>&1 | cut -c1-200; echo "exit=${PIPESTATUS[0]}"
ls -l /etc/curing/ /run/curing-cured/ 2>&1
REMOTE
    ;;
  sim-state) simctl state ;;
  sim-door) case "${1:-}" in open|close) simctl door "$1" ;; *) echo "usage: rig.sh sim-door <open|close>" >&2; exit 2 ;; esac ;;
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
  sim-ui-fault) case "${1:-}" in panic-ui|stall-ui) simctl inject "$1" ;; *) echo "usage: rig.sh sim-ui-fault <panic-ui|stall-ui>" >&2; exit 2 ;; esac ;;
  sim-reset)
    lock
    clear_shadow
    ssh_rig "sudo systemctl stop curing-sim.service 2>/dev/null || true; sudo -u curing /opt/curing/bin/cured sim-wipe not-a-real-cabinet"
    ;;
  screenshot)
    out="${1:?usage: rig.sh screenshot <out.png>}"
    # /dev/fb0 is 800x480, 32 bpp, stride 3200 on the rig (see probe).
    raw=$(mktemp "$STATE_DIR/fb.XXXXXX")
    trap 'rm -f "$raw"' EXIT
    ( lock; ssh_rig 'dd if=/dev/fb0 bs=3200 count=480 2>/dev/null' >"$raw" )
    (cd "$REPO_ROOT" && go build -o "$STATE_DIR/fbshot" ./cmd/fbshot)
    "$STATE_DIR/fbshot" -in "$raw" -out "$out"
    echo "$out"
    ;;
  deploy)
    bin="${1:?usage: rig.sh deploy <local-binary> [name]}"; name="${2:-curing}"
    [ -f "$bin" ] || { echo "rig.sh: no such file $bin" >&2; exit 1; }
    lock
    push_bin "$bin" "$name"
    ssh_rig "ls -l /opt/curing/bin/$name"
    ;;
  service-restart)
    lock
    ssh_rig 'bash -s' <<'REMOTE'
set -e
RUNTIME=/run/systemd/system/curing.service
reload=no
# curing-hw.service names curing.service in Before=, which keeps a transient unit
# loaded after it stops, and systemd-run then refuses the name. So there is no
# systemd-run here. Clear what older versions of this script left behind.
if [ -e /run/systemd/transient/curing.service ]; then
  sudo systemctl stop curing.service 2>/dev/null || true
  sudo rm -f /run/systemd/transient/curing.service
  reload=yes
fi
# This subcommand runs /opt/curing/bin/curing, the binary the panel evidence deploys,
# not the application. A unit file under /run shadows /etc/systemd/system/curing.service,
# has the same identity and environment, and is gone after a reboot. The application's
# own subcommands (cured-restart) remove it. systemctl restart works from any state.
sudo systemctl stop curing-sim.service 2>/dev/null || true
{
  [ -x /opt/curing/bin/curing ] || { echo "no /opt/curing/bin/curing; run rig.sh deploy first" >&2; exit 1; }
  unit='[Unit]
Description=curing (runtime unit from rig.sh, no unit installed)
After=curing-hw.service curing-outputd.service

[Service]
User=curing
Group=curing
SupplementaryGroups=gpio i2c video input render tty
StateDirectory=curing
StateDirectoryMode=0750
WorkingDirectory=/var/lib/curing
ExecStart=/opt/curing/bin/curing
'
  if [ "$(cat "$RUNTIME" 2>/dev/null)" != "$(printf %s "$unit")" ]; then
    printf %s "$unit" | sudo tee "$RUNTIME" >/dev/null
    reload=yes
  fi
}
[ "$reload" = no ] || sudo systemctl daemon-reload
sudo systemctl reset-failed curing.service 2>/dev/null || true
sudo systemctl restart curing.service
sleep 1
systemctl --no-pager --lines=0 status curing.service || true
REMOTE
    ;;
  service-kill) # Test aid: signal the service's main process, to prove restart after a crash or exit.
    sig="${1:?usage: rig.sh service-kill <TERM|KILL>}"
    case "$sig" in TERM|KILL) ;; *) echo "rig.sh: signal must be TERM or KILL" >&2; exit 2 ;; esac
    lock
    ssh_rig "sudo systemctl kill --kill-whom=main -s $sig curing.service; sleep 1; systemctl show -p ActiveState -p SubState -p Result curing.service"
    ;;
  probe-unit) # Read-only: what keeps curing.service loaded.
    lock
    ssh_rig 'bash -s' <<'PROBE'
cat /proc/sys/kernel/random/boot_id
systemctl show -p LoadState -p ActiveState -p SubState -p Result -p FragmentPath -p CollectMode -p Transient -p After -p Before -p RequiredBy -p WantedBy curing.service
echo "== who references it"; systemctl show -p Before curing-hw.service
ls -la /run/systemd/transient/ /etc/systemd/system/curing.service /run/systemd/system/ 2>&1 | grep -E 'curing|cannot|^/'
PROBE
    ;;
  service-stop)
    lock
    ssh_rig 'sudo systemctl stop curing.service curing-sim.service 2>/dev/null || true; systemctl is-active curing.service curing-sim.service || true'
    ;;
  service-status)
    lock
    ssh_rig 'systemctl --no-pager --lines=0 status curing.service || true'
    ;;
  journal)
    n="${1:?usage: rig.sh journal <n> [unit|all]}"; unit="${2:-curing.service}"
    case "$n" in *[!0-9]*) echo "rig.sh: n must be a number" >&2; exit 2 ;; esac
    case "$unit" in *[!A-Za-z0-9@._-]*) echo "rig.sh: bad unit name" >&2; exit 2 ;; esac
    lock
    if [ "$unit" = all ]; then
      ssh_rig "sudo journalctl --no-pager -n $n"
    else
      ssh_rig "sudo journalctl --no-pager -n $n -u $unit"
    fi
    ;;
  hwcheck)
    sub="${1:?usage: rig.sh hwcheck <subcommand> [args]; see rig.sh help}"; shift
    case "$sub" in driver-calls|pwm|lines|time-to-safe|safe-state|try-on|hx711-timing|read-sensors|relay-click|fan-sweep) ;; *) echo "rig.sh: unknown hwcheck $sub" >&2; exit 2 ;; esac
    for a in "$@"; do case "$a" in *[!A-Za-z0-9._=-]*) echo "rig.sh: bad argument" >&2; exit 2 ;; esac; done
    hw=$(build_hwcheck)
    lock
    push_bin "$hw" hwcheck
    # Anything that drives or times the relay lines needs them to itself.
    case "$sub" in time-to-safe|try-on|relay-click|fan-sweep|hx711-timing|read-sensors|driver-calls|pwm) stop_app ;; esac
    ssh_rig "sudo -u curing /opt/curing/bin/hwcheck $sub $*"
    ;;
  hwstate) # Read-only raw state; used by the evidence script.
    lock
    ssh_rig 'sudo bash -s' <<'REMOTE'
echo "== boot"; cat /proc/sys/kernel/random/boot_id; uptime -s; date -u
echo "== /boot/firmware files (never modified by provisioning)"; ls -l --time-style=full-iso /boot/firmware/config.txt /boot/firmware/cmdline.txt; sha256sum /boot/firmware/config.txt /boot/firmware/cmdline.txt
echo "== units"; echo "system: $(systemctl is-system-running)"
for u in curing-hw curing-outputd curing-boot-record curing; do echo "$u.service: enabled=$(systemctl is-enabled $u.service 2>/dev/null) active=$(systemctl is-active $u.service 2>/dev/null) result=$(systemctl show -p Result --value $u.service 2>/dev/null) since=$(systemctl show -p ActiveEnterTimestamp --value $u.service 2>/dev/null)"; done
echo "guard restarts this boot: $(systemctl show -p NRestarts --value curing-outputd.service)"
echo "== reset status from the bootloader"; for f in bootloader/rsts power/power_reset; do echo "$f=$(od -An -tx1 /proc/device-tree/chosen/$f | tr -d ' ')"; done
echo "== clock"; timedatectl show -p NTPSynchronized -p NTP -p TimeUSec; echo "rtc_epoch=$(cat /sys/class/rtc/rtc0/since_epoch) system_epoch=$(date +%s)"; dmesg | grep -i 'setting system clock'
echo "== overlays"; dtoverlay -l
echo "== device nodes"; ls -l /dev/i2c-* /dev/gpiochip0 /dev/fb0 /dev/gpiomem0 /dev/watchdog0 2>&1
echo "== i2c adapters"; for a in /sys/bus/i2c/devices/i2c-*; do echo "$a $(cat $a/name) $(readlink -f $a)"; done
echo "== iio"; for d in /sys/bus/iio/devices/iio:device*; do echo "$d name=$(cat $d/name)"; done
echo "== pwm"; for c in /sys/class/pwm/pwmchip*; do echo "$c $(readlink -f $c)"; done
for ch in /sys/class/pwm/pwmchip*/pwm*; do [ -d "$ch" ] || continue; echo "$ch"; for f in period duty_cycle enable polarity; do echo "  $f=$(cat $ch/$f 2>&1)"; done; ls -l "$ch/duty_cycle" "$ch/enable"; done
cat /sys/kernel/debug/pwm
echo "== pinctrl"; pinctrl get 2-6; pinctrl get 12; pinctrl get 17; pinctrl get 22-27
echo "== rp1 param"; grep -H . /sys/module/pinctrl_rp1/parameters/persist_gpio_outputs
echo "== watchdog"; sysctl kernel.panic kernel.panic_on_oops; systemctl show -p RuntimeWatchdogUSec -p RebootWatchdogUSec; wdctl; dmesg | grep -iE 'watchdog|bcm2835-wdt' | tail -5
echo "== tty1"; echo "getty@tty1: enabled=$(systemctl is-enabled getty@tty1 2>&1) active=$(systemctl is-active getty@tty1 2>&1)"; for v in /sys/class/vtconsole/*; do echo "$v bind=$(cat $v/bind) $(cat $v/name)"; done
echo "== display marker"; ls -l /run/curing/; grep -H . /run/curing/display-status
echo "== user"; id curing; ls -ld /opt/curing /opt/curing/bin /var/lib/curing
echo "== journald"; journalctl --disk-usage; systemd-analyze cat-config systemd/journald.conf | grep -vE '^(#|$)'
echo "== timers"; systemctl list-timers --all --no-pager --no-legend | awk '{print $(NF-1), $NF}'
REMOTE
    ;;
  reboot)
    lock
    old=$(boot_id)
    ssh_rig 'sudo systemctl reboot' || true
    wait_new_boot "$old" 240
    ;;
  crash-test)
    mode="${1:?usage: rig.sh crash-test <panic|halt>}"
    case "$mode" in panic) action=panic ;; halt) action=panic-halt ;; *) echo "rig.sh: mode must be panic or halt" >&2; exit 2 ;; esac
    hw=$(build_hwcheck)
    lock
    push_bin "$hw" hwcheck
    stop_app
    old=$(boot_id)
    echo "boot_id_before=$old"
    trap 'ssh_rig "sudo systemctl stop curing-test-driver.service" 2>/dev/null || true' ERR
    start_test_driver
    echo "== before the panic, test driver holds outputs on and fan at 50 %"
    ssh_rig 'sudo -u curing /opt/curing/bin/hwcheck lines; sudo -u curing /opt/curing/bin/hwcheck fan-duty; /usr/sbin/sysctl kernel.panic'
    # sync first: flushing the SD card can take seconds, and the kernel is alive for
    # all of it. The last ping reply, not the time of this command, marks the panic.
    ssh_rig sync
    pinglog=$(mktemp "$STATE_DIR/ping.XXXXXX")
    ping -D -i 0.2 -W 1 "$RIG_HOST" >"$pinglog" 2>&1 &
    pingpid=$!
    sleep 1
    t_cmd=$(date +%s.%N)
    rc=0
    ssh_rig "sudo /opt/curing/libexec/curing-rig-test $action" || rc=$?
    echo "== ssh ended rc=$rc (the panic cuts the connection), host clock $(date +%s.%N)"
    wait_new_boot "$old" 300
    kill "$pingpid" 2>/dev/null || true; wait "$pingpid" 2>/dev/null || true
    print_reset_bracket "$t_cmd" "$pinglog"
    rm -f "${pinglog:?}"
    ;;
  power-cut-before)
    hw=$(build_hwcheck)
    lock
    push_bin "$hw" hwcheck
    stop_app
    echo "boot_id_before=$(boot_id)"
    start_test_driver
    ssh_rig 'sudo -u curing /opt/curing/bin/hwcheck lines; sudo -u curing /opt/curing/bin/hwcheck fan-duty'
    ;;
  power-cut-after)
    lock
    t_cmd="${1:?usage: rig.sh power-cut-after <host epoch of the cut>}"
    case "$t_cmd" in *[!0-9.]*) exit 2 ;; esac
    print_reset_bracket "$t_cmd" ""
    ;;
  safety-param-test)
    hw=$(build_hwcheck)
    lock
    push_bin "$hw" hwcheck
    stop_app
    ssh_rig 'bash -s' <<'REMOTE'
try() { sudo -u curing /opt/curing/bin/hwcheck try-on; }
echo "== with persist_gpio_outputs=N"; try
echo "== setting Y"; sudo /opt/curing/libexec/curing-rig-test param-y; sleep 1.5
sudo -u curing /opt/curing/bin/hwcheck safe-state
try
sudo -u curing /opt/curing/bin/hwcheck lines
echo "== restoring N"; sudo /opt/curing/libexec/curing-rig-test param-n; sleep 1.5
sudo -u curing /opt/curing/bin/hwcheck safe-state
try
echo "== guard log"; sudo journalctl --no-pager -u curing-outputd.service -n 6 -o cat
REMOTE
    ;;
  bench-helper-state) # Read-only: whether the test helper is installed.
    lock
    ssh_rig 'if sudo test -e /opt/curing/libexec/curing-rig-test; then echo "curing-rig-test: present"; else echo "curing-rig-test: absent"; fi; sudo ls -l /opt/curing/libexec/'
    ;;
  hashes)
    lock
    for f in curing-outputd cured.arm64 hwcheck.arm64; do [ -f "$STATE_DIR/$f" ] && (cd "$STATE_DIR" && sha256sum "$f" | sed 's/^/local build: /'); done
    ssh_rig 'sudo bash -c "sha256sum /opt/curing/bin/* /opt/curing/libexec/* /opt/curing/overlays/* /etc/systemd/system/curing*.service /etc/systemd/journald.conf.d/50-curing.conf /etc/systemd/system.conf.d/50-curing.conf /etc/sysctl.d/50-curing.conf; ls -l --time-style=full-iso /opt/curing/bin"'
    ;;
  test-driver-stop) # Ends a test driver left holding outputs on by an interrupted test.
    lock
    ssh_rig 'sudo systemctl stop curing-test-driver.service 2>/dev/null; echo "curing-test-driver.service: $(systemctl is-active curing-test-driver.service 2>/dev/null)"'
    ;;
  sudo-check)
    lock
    ssh_rig 'sudo -n true; echo "sudo -n true rc=$?"'
    ;;
  records)
    n="${1:-0}"; case "$n" in *[!0-9]*) exit 2 ;; esac
    lock
    ssh_rig "bash -s $n" <<'REMOTE'
D=/var/lib/curing-records
echo "== index"; sudo cat $D/index.log
echo "== files"; sudo ls -l $D; echo "total_bytes=$(sudo du -sb $D | cut -f1)"
for f in $(sudo ls -1 $D | grep -E '^[0-9]+-' | tail -n "$1"); do echo "== $f"; sudo zcat "$D/$f" | sed -n '1,/^== previous boot, last entries/p' | tail -n 30; done
echo "== journal"; journalctl --disk-usage; sudo ls -l /var/log/journal/*/ | tail -n 20
REMOTE
    ;;
  lock-hold)
    secs="${1:?usage: rig.sh lock-hold <seconds>}"
    case "$secs" in *[!0-9]*) exit 2 ;; esac
    lock
    echo "holding rig lock for $secs s"; sleep "$secs"
    ;;
  ""|-h|--help|help) usage; exit 2 ;;
  *) usage; exit 2 ;;
esac
