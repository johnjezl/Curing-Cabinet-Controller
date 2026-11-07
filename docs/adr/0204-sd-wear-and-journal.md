# ADR 0204: Journal and SD card wear

Status: accepted

## Context
The image ships `Storage=volatile`: no logs survive a reboot. For a controller that may hang
or lose power, the logs just before that event are the ones that matter. SD cards wear out,
and a power loss can damage what is being written.

## Options
1. Keep volatile. No wear, no post-mortem.
2. Persistent and unbounded. Forensics and needless wear.
3. Persistent, capped and rarely synced. Forensics for most failures and small write volume.
4. Log to a USB or network target. Not in the parts list.

## Decision
Option 3: `SystemMaxUse=128M`, 8 MB files, `SplitMode=none`, `SyncIntervalSec=5m`,
`MaxLevelStore=info`, `MaxRetentionSec=1month`. Messages at critical level and above still
sync immediately (journald's normal behavior).

Also disabled: the `apt-daily`, `apt-daily-upgrade`, `man-db`, `dpkg-db-backup` and
`e2scrub_all` timers and the `bluetooth` service. `/` is already `noatime` and swap is zram,
so neither changes. `fstrim.timer` and `logrotate.timer` stay.

## Crash history
Each unclean reset makes journald rename its open file as corrupted. That file still counts
toward the cap, so resets push older logs out, exactly when history matters most. A bigger cap
only delays this.

Options:
1. Sync the journal constantly. More wear and still evicted.
2. pstore/ramoops for the panic text. The kernel has `CONFIG_PSTORE_RAM`, but it needs memory
   reserved at boot, which needs config.txt or cmdline.txt. A runtime overlay cannot reserve
   memory the kernel may already be using.
3. A small record per boot in its own directory.

Chosen: 3. `curing-boot-record` writes one compressed file in `/var/lib/curing-records` per
boot: whether the previous shutdown was clean, reset status, clock state, and the previous boot's
warnings and last 200 journal lines. It keeps the newest 200 unclean and 50 clean records
separately, so clean reboots never evict an unclean one, and size stays bounded. Tests run the
script itself.

## Consequences
- **Panic text is not kept.** With no pstore, and no serial console assigned to this board in
  labctl, the last journal lines are the only record, and they end before the panic.
- The application's own fault log in its state directory is part A's.
- Up to 5 min of ordinary logs can be lost in a power cut.
- Security updates no longer download automatically. That suits a sealed appliance and is the
  owner's decision to reverse.
- Application data in the state directory is part A's and D's to manage.
