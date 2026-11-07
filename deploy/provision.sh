#!/bin/bash
# The only way system state on the Pi changes. Idempotent: run through
# deploy/rig.sh provision as often as you like; a second run reports changed=0.
# Never touches /boot/firmware.
set -euo pipefail
# --bench adds curing-rig-test, which can panic the kernel and flip the safety
# parameter. It is only for a test bench. Without the flag it is removed.
# --sim-boot SPEED OUTAGE (bench only) makes the simulated plant what starts at boot, so
# a simulated cure survives a real power cut. Any other run makes real hardware start.
SRC="${1:?usage: provision.sh <unpacked deploy dir> [--bench [--sim-boot SPEED 1x|compressed]]}"
BENCH="${2:-}"
SIMBOOT="${3:-}"; SIM_SPEED="${4:-}"; SIM_OUTAGE="${5:-}"
case "$BENCH" in ""|--bench) ;; *) echo "unknown option $BENCH" >&2; exit 2 ;; esac
case "$SIMBOOT" in
  "") ;;
  --sim-boot)
    [ "$BENCH" = --bench ] || { echo "--sim-boot needs --bench" >&2; exit 2; }
    case "$SIM_SPEED" in ""|*[!0-9]*) echo "bad speed" >&2; exit 2 ;; esac
    case "$SIM_OUTAGE" in 1x|compressed) ;; *) echo "bad outage mode" >&2; exit 2 ;; esac ;;
  *) echo "unknown option $SIMBOOT" >&2; exit 2 ;;
esac
FILES="$SRC/files"
changed=0
note() { echo "changed: $*"; changed=$((changed + 1)); }

[ "$(id -u)" = 0 ] || { echo "must run as root" >&2; exit 1; }
BOOT_BEFORE=$(find /boot/firmware -maxdepth 1 -type f \( -name config.txt -o -name cmdline.txt \) -exec sha256sum {} + | sort)

put() { # <source> <destination> <mode> [owner:group]
  local src="$1" dst="$2" mode="$3" own="${4:-root:root}"
  if ! cmp -s "$src" "$dst" 2>/dev/null; then
    install -D -m "$mode" -o "${own%:*}" -g "${own#*:}" "$src" "$dst"; note "$dst"
    return 0
  fi
  if [ "$(stat -c '%a %U:%G' "$dst")" != "$mode $own" ]; then
    chmod "$mode" "$dst"; chown "$own" "$dst"; note "$dst metadata"
  fi
}

ensure_dir() { # <path> <mode> <owner:group>
  local p="$1" mode="$2" own="$3"
  if [ ! -d "$p" ] || [ "$(stat -c '%a %U:%G' "$p")" != "$mode $own" ]; then
    install -d -m "$mode" -o "${own%:*}" -g "${own#*:}" "$p"; note "dir $p"
  fi
}

# Service user. Device access is by group only; the service never runs as root.
GROUPS_WANTED="gpio,i2c,video,input,render,tty"
if ! id curing >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/curing --no-create-home --shell /usr/sbin/nologin --groups "$GROUPS_WANTED" curing
  note "user curing"
fi
have=$(id -nG curing | tr ' ' '\n' | sort | tr '\n' ',' )
want=$(echo "curing,$GROUPS_WANTED" | tr ',' '\n' | sort | tr '\n' ',')
if [ "$have" != "$want" ]; then usermod --groups "$GROUPS_WANTED" curing; note "groups of curing"; fi

# Directories. /opt/curing is root-owned so the service cannot rewrite its own
# code; /opt/curing/bin holds the binary from rig.sh deploy; the state directory
# is the only place the service writes.
ensure_dir /opt/curing 755 root:root
ensure_dir /opt/curing/bin 755 root:curing
ensure_dir /opt/curing/libexec 755 root:root
ensure_dir /opt/curing/overlays 755 root:root
ensure_dir /var/lib/curing 750 curing:curing

# Hardware bring-up.
hw_before=$changed
put "$FILES/curing-hw-init" /opt/curing/libexec/curing-hw-init 755
put "$FILES/curing-safe-outputs" /opt/curing/libexec/curing-safe-outputs 755
put "$FILES/curing-display-status" /opt/curing/libexec/curing-display-status 755
put "$FILES/curing-hw.service" /etc/systemd/system/curing-hw.service 644
tmp_dtbo=$(mktemp)
dtc -q -@ -I dts -O dtb -o "$tmp_dtbo" "$FILES/curing-fan.dts"
put "$tmp_dtbo" /opt/curing/overlays/curing-fan.dtbo 644
rm -f "$tmp_dtbo"
hw_changed=$((changed - hw_before))

# Output guard. It owns the relay lines. The binary is built by deploy/rig.sh.
guard_before=$changed
put "$SRC/curing-outputd" /opt/curing/bin/curing-outputd 755 root:curing
put "$FILES/curing-outputd.service" /etc/systemd/system/curing-outputd.service 644
guard_changed=$((changed - guard_before))

# Crash records, kept outside the journal, and the bench test helper.
ensure_dir /var/lib/curing-records 750 root:adm
record_before=$changed
put "$FILES/curing-boot-record" /opt/curing/libexec/curing-boot-record 755
put "$FILES/curing-boot-record.service" /etc/systemd/system/curing-boot-record.service 644
record_changed=$((changed - record_before))
if [ "$BENCH" = --bench ]; then
  put "$FILES/curing-rig-test" /opt/curing/libexec/curing-rig-test 750
elif [ -e /opt/curing/libexec/curing-rig-test ]; then
  rm -f /opt/curing/libexec/curing-rig-test; note "removed bench helper curing-rig-test"
fi

# Watchdog, journal, sysctl.
system_conf_before=$(systemctl show -p RuntimeWatchdogUSec --value)
put "$FILES/system-curing.conf" /etc/systemd/system.conf.d/50-curing.conf 644
put "$FILES/journald-curing.conf" /etc/systemd/journald.conf.d/50-curing.conf 644
put "$FILES/sysctl-curing.conf" /etc/sysctl.d/50-curing.conf 644
sysctl --quiet --load=/etc/sysctl.d/50-curing.conf

# Everything above is on disk; make the running system match.
systemctl daemon-reload
if [ "$(systemctl show -p RuntimeWatchdogUSec --value)" != 10s ]; then
  systemctl daemon-reexec; note "daemon-reexec (was RuntimeWatchdogUSec=$system_conf_before)"
fi
JCONF=/etc/systemd/journald.conf.d/50-curing.conf
# The stamp is on disk: journald reads this file at every boot, so a reboot alone
# must not make the next provisioning run restart it.
JSTAMP=/etc/curing-journald.applied
if ! cmp -s "$JCONF" "$JSTAMP" 2>/dev/null; then
  systemctl restart systemd-journald; cp "$JCONF" "$JSTAMP"; note "journald restarted"
fi

# SD wear: stop periodic jobs that write for no benefit on a sealed appliance.
for u in apt-daily.timer apt-daily-upgrade.timer man-db.timer dpkg-db-backup.timer e2scrub_all.timer; do
  if systemctl is-enabled --quiet "$u" 2>/dev/null; then systemctl disable --now "$u" 2>&1; note "disabled $u"; fi
done
# Unused radio stack; not needed by the controller.
if systemctl is-enabled --quiet bluetooth.service 2>/dev/null; then systemctl disable --now bluetooth.service 2>&1; note "disabled bluetooth"; fi

# Free tty1 for the panel.
if [ "$(systemctl is-enabled getty@tty1.service 2>/dev/null || true)" != masked ]; then
  systemctl disable --now getty@tty1.service 2>&1 || true
  systemctl mask getty@tty1.service 2>&1
  note "masked getty@tty1"
fi

# Enable and run the bring-up now, so no reboot is needed to see the result.
if [ ! -e /etc/systemd/system/multi-user.target.wants/curing-hw.service ]; then
  systemctl disable curing-hw.service 2>&1; systemctl enable curing-hw.service 2>&1; note "enabled curing-hw"
fi
# A re-run resets the fan and relay lines, so the application must not be running.
if [ "$hw_changed" != 0 ] && systemctl is-active --quiet curing-hw.service; then
  systemctl stop curing.service curing-sim.service 2>/dev/null || true
  systemctl stop curing-outputd.service 2>/dev/null || true
  systemctl restart curing-hw.service; note "restarted curing-hw"
elif ! systemctl is-active --quiet curing-hw.service; then
  systemctl start curing-hw.service; note "started curing-hw"
fi

# The guard needs bring-up done first and owns the relay lines from here on.
for u in curing-outputd.service curing-boot-record.service; do
  if ! systemctl is-enabled --quiet "$u"; then systemctl enable "$u" 2>&1; note "enabled $u"; fi
done
if [ "$record_changed" != 0 ] && systemctl is-active --quiet curing-boot-record.service; then
  systemctl restart curing-boot-record.service; note "restarted curing-boot-record"
elif ! systemctl is-active --quiet curing-boot-record.service; then
  systemctl start curing-boot-record.service; note "started curing-boot-record"
fi
if [ "$guard_changed" != 0 ] && systemctl is-active --quiet curing-outputd.service; then
  systemctl restart curing-outputd.service; note "restarted curing-outputd"
elif ! systemctl is-active --quiet curing-outputd.service; then
  systemctl start curing-outputd.service; note "started curing-outputd"
fi

# The application. One of two units starts at boot; they conflict with each other.
# The simulator unit and its settings exist only on a bench, and the production
# marker makes cured refuse --sim on a real cabinet whatever the command line.
ensure_dir /etc/curing 755 root:root
ensure_dir /var/lib/curing-sim 750 curing:curing
app_before=$changed
put "$SRC/curing.service" /etc/systemd/system/curing.service 644
if [ "$BENCH" = --bench ]; then
  put "$SRC/curing-sim.service" /etc/systemd/system/curing-sim.service 644
  if [ -e /etc/curing/production ]; then rm -f /etc/curing/production; note "removed production marker"; fi
else
  if [ -e /etc/systemd/system/curing-sim.service ]; then
    systemctl disable --now curing-sim.service 2>&1 || true
    rm -f /etc/systemd/system/curing-sim.service /etc/curing/sim.env; note "removed simulator unit"
  fi
  if [ ! -e /etc/curing/production ]; then
    echo "real cabinet: cured --sim is refused while this file exists" > /etc/curing/production; note "wrote production marker"
  fi
fi
if [ "$SIMBOOT" = --sim-boot ]; then
  env_tmp=$(mktemp); printf 'SIM_SPEED=%s\nSIM_OUTAGE=%s\n' "$SIM_SPEED" "$SIM_OUTAGE" > "$env_tmp"
  put "$env_tmp" /etc/curing/sim.env 644; rm -f "$env_tmp"
elif [ -e /etc/curing/sim.env ]; then
  rm -f /etc/curing/sim.env; note "removed simulator settings"
fi
app_changed=$((changed - app_before))
systemctl daemon-reload
if [ "$SIMBOOT" = --sim-boot ]; then
  want=curing-sim.service; other=curing.service
else
  want=curing.service; other=curing-sim.service
fi
if systemctl cat "$other" >/dev/null 2>&1 && systemctl is-enabled --quiet "$other" 2>/dev/null; then
  systemctl disable --now "$other" 2>&1; note "disabled $other"
fi
# A disabled unit must not leave the system reading degraded.
systemctl reset-failed "$other" 2>/dev/null || true
if ! systemctl is-enabled --quiet "$want"; then systemctl enable "$want" 2>&1; note "enabled $want"; fi
# A run that changed nothing leaves a stopped application stopped, so a test that
# stopped it on purpose keeps it stopped.
if [ "$app_changed" != 0 ] && [ -x /opt/curing/bin/cured ]; then
  systemctl restart "$want"; note "restarted $want"
fi
BOOT_AFTER=$(find /boot/firmware -maxdepth 1 -type f \( -name config.txt -o -name cmdline.txt \) -exec sha256sum {} + | sort)
[ "$BOOT_BEFORE" = "$BOOT_AFTER" ] || { echo "BUG: /boot/firmware changed" >&2; exit 1; }
echo "provision: done, changed=$changed"
