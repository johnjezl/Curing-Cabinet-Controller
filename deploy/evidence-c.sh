#!/usr/bin/env bash
# Regenerates raw evidence for the front panel under evidence/c/. Raw output only.
#   deploy/evidence-c.sh          everything, including frames from the rig
#   deploy/evidence-c.sh --no-rig off-rig evidence only
#   deploy/evidence-c.sh --rig-only keep off-rig evidence, redo only the rig section
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
OUT=evidence/c
RIG=1
OFFRIG=1
[ "${1:-}" = "--no-rig" ] && RIG=0
[ "${1:-}" = "--rig-only" ] && OFFRIG=0
STAMP() { date -u +%FT%TZ; }
# Everything a frame or a test result depends on, including the core it displays.
tree_hashes() {
  find internal/ui internal/api internal/core internal/sim internal/hal cmd/uidemo -type f -name '*.go' | LC_ALL=C sort | xargs sha256sum
  sha256sum go.mod go.sum deploy/walk-c.txt deploy/evidence-c.sh
}
export GOMAXPROCS="${GOMAXPROCS:-2}" GOFLAGS="${GOFLAGS:--p=2}"
NICE="nice -n 19"
if [ "$OFFRIG" = 0 ]; then
  rm -rf "$OUT/rig" "$OUT"/.rig-* "$OUT/.uidemo" "$OUT"/5*-rig-*; mkdir -p "$OUT/rig"
  { echo "rig section regenerated $(STAMP)"; tree_hashes; } > "$OUT/01-rig-section-versions.txt"
else
rm -rf "$OUT"; mkdir -p "$OUT/screens" "$OUT/walk" "$OUT/rig" "$OUT/realcore"

{
  echo "generated $(STAMP)"
  echo "go: $(go version)"
  git rev-parse HEAD 2>/dev/null | sed 's/^/git HEAD /'
  tree_hashes
} > "$OUT/00-versions.txt"

$NICE go build -o "$OUT/.uidemo" ./cmd/uidemo || exit 1

# Every state and screen as an 800x480 PNG.
"$OUT/.uidemo" render -out "$OUT/screens" > "$OUT/10-render-audit.txt" 2>&1
python3 - "$OUT/screens" > "$OUT/11-png-sizes.txt" 2>&1 <<'PY' || file "$OUT"/screens/*.png > "$OUT/11-png-sizes.txt"
import struct,sys,glob,os
for f in sorted(glob.glob(sys.argv[1]+'/*.png')):
    with open(f,'rb') as h:
        b=h.read(24)
    print(os.path.basename(f),*struct.unpack('>II',b[16:24]))
PY

# Real control core on the simulated plant, not fixtures.
UI_SIM_PNG_DIR="$ROOT/$OUT/realcore" $NICE go test -count=1 -v -p 2 -parallel 2 ./internal/ui/simtest > "$OUT/12-realcore-test.txt" 2>&1
echo "exit=$?" >> "$OUT/12-realcore-test.txt"
mv "$OUT/realcore/states.txt" "$OUT/13-realcore-states.tsv"

# Tests.
$NICE go test -count=1 -v -p 2 -parallel 2 ./internal/ui > "$OUT/20-go-test.txt" 2>&1
echo "exit=$?" >> "$OUT/20-go-test.txt"
CGO_ENABLED=1 $NICE go test -count=1 -race -p 2 -parallel 2 ./internal/ui -run 'Loop|Stuck|Render|Decoder|Framebuffer' > "$OUT/21-go-test-race.txt" 2>&1
echo "exit=$?" >> "$OUT/21-go-test-race.txt"
$NICE go test -run '^$' -p 2 -bench HomeFrame -benchtime 200x ./internal/ui > "$OUT/22-bench-amd64.txt" 2>&1

# Tap walkthrough with timing, headless.
"$OUT/.uidemo" walk -fixture idle-manual -script deploy/walk-c.txt -out "$OUT/walk" > "$OUT/30-walkthrough.tsv" 2>&1
"$OUT/.uidemo" hits -fixture normal > "$OUT/31-targets-home.txt" 2>&1
"$OUT/.uidemo" hits -fixture idle-manual -nav nav.set,edit.plus.0 > "$OUT/31-targets-set-pending.txt" 2>&1
"$OUT/.uidemo" hits -fixture alarm-temp-high > "$OUT/31-targets-alarm-home.txt" 2>&1

# The injection path and the production build.
{
  echo "== go list -deps ./internal/ui | grep module packages"
  go list -deps ./internal/ui | grep curing-cabinet-controller
  echo "== symbols in default uidemo matching inject"
  go tool nm "$OUT/.uidemo" | grep -iE 'main\.(injectLine|startInject|startAutotap)' 
  echo "== default build: -inject flag"
  "$OUT/.uidemo" panel -inject /tmp/x 2>&1 | head -3
  echo "== default build: -autotap flag"
  "$OUT/.uidemo" panel -autotap 'tap 1 1' 2>&1 | head -3
  $NICE go build -tags demoinject -o "$OUT/.uidemo-tag" ./cmd/uidemo
  echo "== symbols in -tags demoinject build"
  go tool nm "$OUT/.uidemo-tag" | grep -E 'main\.(injectLine)$'
  rm -f "$OUT/.uidemo-tag"
} > "$OUT/40-injection-scope.txt" 2>&1

fi # OFFRIG

# Frames from the real display. The demo runs as its own unit under its own name, so
# the application binary is untouched and restored to running by demo-stop. The lock
# is held for the whole section; the service and the frames cannot interleave with
# another agent's run.
if [ "$RIG" = 1 ]; then
  exec 8>.rig/lock
  flock -w 3600 8 || { echo "could not take .rig/lock in 1 h" > "$OUT/50-rig-log.txt"; exit 75; }
  export RIG_LOCK_HELD=1
  DEMO=uidemo-c
  build_rig() { # name args
    GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $NICE go build -tags demoinject -ldflags "-X 'main.defaultArgs=$2'" -o "$OUT/.rig-$1" ./cmd/uidemo
  }
  # A frame only counts if this scene started a new process, its binary is the one
  # just built, and that process is still running after the capture. Otherwise the
  # PNG is removed and the scene is logged NOT CAPTURED.
  LASTPID=""
  demo_pid() { deploy/rig.sh journal 400 curing-demo.service 2>/dev/null | sed -n 's/.*\[\([0-9]*\)\]: .*uidemo: framebuffer.*/\1/p' | tail -1; }
  # Only what the journal says after the newest start counts.
  alive() { ! deploy/rig.sh journal 400 curing-demo.service 2>/dev/null | awk '/Started curing-demo/{buf=""} {buf=buf $0 "\n"} END{printf "%s", buf}' | grep -qE 'Deactivated|Main process exited|Failed with|Stopped '; }
  scene() { # name args settle-seconds
    build_rig "$1" "$2" || { echo "scene $1 build failed"; return; }
    local sha ok=0
    sha=$(sha256sum "$OUT/.rig-$1" | cut -d' ' -f1)
    for try in 1 2 3; do
      echo "== scene $1 try=$try args=$2 local_sha256=$sha $(STAMP)"
      deploy/rig.sh deploy "$OUT/.rig-$1" "$DEMO"
      deploy/rig.sh demo-start "$DEMO" | grep -E 'Main PID|Active:|Failed'
      deploy/rig.sh hashes 2>/dev/null | grep "bin/$DEMO"
      sleep 1
      pid1=$(demo_pid)
      sleep "$3"
      deploy/rig.sh screenshot "$OUT/rig/$1.png"
      pid2=$(demo_pid)
      echo "last=[$LASTPID] before=[$pid1] after=[$pid2]"
      if [ -n "$pid1" ] && [ "$pid1" = "$pid2" ] && [ "$pid1" != "$LASTPID" ] && alive; then
        ok=1; LASTPID="$pid1"; break
      fi
      sleep 15
    done
    [ "$ok" = 1 ] || { rm -f "$OUT/rig/$1.png"; echo "scene $1 NOT CAPTURED"; }
    rm -f "$OUT/.rig-$1"
  }
  # Layout: nav row y=430; HOME x=69 SET x=201 CURE x=333 ALARMS x=465 TREND x=597 MORE x=729.
  {
    deploy/rig.sh status
    scene normal 'panel|-fixture|normal' 3
    scene alarm 'panel|-fixture|alarm-temp-high' 3
    scene stale 'panel|-fixture|stale-temp' 3
    scene fault 'panel|-fixture|temp-fault-core-alarm' 3
    scene hold 'panel|-fixture|hold-compressor' 3
    scene end-of-cure 'panel|-fixture|end-of-cure-alarm' 3
    scene simulated 'panel|-fixture|simulated' 3
    scene dim 'panel|-fixture|normal|-dim-after|2s|-sleep-after|900s' 6
    deploy/rig.sh journal 8 curing-demo.service
    scene sleep 'panel|-fixture|normal|-dim-after|1s|-sleep-after|2s' 6
    deploy/rig.sh journal 8 curing-demo.service
    scene frozen 'panel|-fixture|normal|-freeze-after|3s' 11
    deploy/rig.sh journal 8 curing-demo.service
    # Screens other than Home, by injected taps through the normal input path.
    scene s-set 'panel|-fixture|idle-manual|-autotap|sleep 1200;tap 201 430' 4
    scene s-alarms 'panel|-fixture|alarm-many|-autotap|sleep 1200;tap 465 430' 4
    scene s-history 'panel|-fixture|normal|-autotap|sleep 1200;tap 465 430;tap 295 104' 5
    scene s-outputs 'panel|-fixture|output-holds|-autotap|sleep 1200;tap 729 430;tap 300 138' 5
    scene s-override 'panel|-fixture|override-alarm' 3
    scene s-more 'panel|-fixture|ref-check-overdue|-autotap|sleep 1200;tap 729 430' 4
    scene s-trend 'panel|-fixture|normal|-autotap|sleep 1200;tap 597 430' 4
    scene s-health 'panel|-fixture|normal|-autotap|sleep 1200;tap 729 430;tap 101 138' 5
    # Keylock across a restart, in the demo's own state file.
    STATE=/var/lib/curing/uidemo-panel.json
    scene lock-set "panel|-fixture|idle-manual|-state|$STATE|-autotap|sleep 1500;tap 729 430;hold 698 138 2300;sleep 500;tap 69 430" 7
    scene lock-after-restart "panel|-fixture|idle-manual|-state|$STATE" 3
    scene lock-clear "panel|-fixture|idle-manual|-state|$STATE|-autotap|sleep 1500;tap 729 430;hold 698 138 3300;sleep 500;tap 69 430" 8
    # A tap walk on fixtures, for latency.
    WALK='sleep 2000;tap 201 430;tap 726 113;tap 726 113;tap 644 430;sleep 2500;tap 333 430;tap 300 100;tap 644 430;hold 630 430 2200;sleep 2500;tap 465 430;tap 597 430;tap 729 430;tap 101 138;sleep 1500;tap 729 430;tap 597 430;tap 333 430'
    scene walk-end "panel|-fixture|idle-manual|-autotap|$WALK" 22
    deploy/rig.sh journal 120 curing-demo.service > "$OUT/51-rig-walk-journal.txt" 2>&1

    # The alarm path on the real core and simulated plant, on this board. Each stage
    # is timed by -autotap; a frame is taken inside each stage's window.
    # silence at (718,55); ALARMS tab; ack-all (698,104); history tab (295,104).
    PATHTAPS='sleep 26000;tap 718 55;sleep 9000;tap 465 430;sleep 9000;tap 698 104;sleep 12000;tap 295 104'
    build_rig corepath "panel|-realcore|-sim-rate|30|-fault-at|8s|-repair-at|64s|-autotap|$PATHTAPS"
    sha=$(sha256sum "$OUT/.rig-corepath" | cut -d' ' -f1)
    echo "== scene corepath local_sha256=$sha $(STAMP)"
    deploy/rig.sh deploy "$OUT/.rig-corepath" "$DEMO"
    deploy/rig.sh demo-start "$DEMO" | grep -E 'Main PID|Active:|Failed'
    deploy/rig.sh hashes 2>/dev/null | grep "bin/$DEMO"
    t0=$(date +%s)
    at() { while [ $(( $(date +%s) - t0 )) -lt "$1" ]; do sleep 1; done; deploy/rig.sh screenshot "$OUT/rig/$2.png"; echo "$2 at +$(( $(date +%s) - t0 ))s"; }
    at 5 corepath-1-before-fault
    at 20 corepath-2-alarm
    at 31 corepath-3-silenced
    at 41 corepath-4-alarm-list
    at 53 corepath-5-acked
    at 78 corepath-6-history
    deploy/rig.sh journal 200 curing-demo.service > "$OUT/53-rig-realcore-journal.txt" 2>&1
    rm -f "$OUT/.rig-corepath"

    # The reference check on the real core: more, sensor check, code 2580, a far
    # temperature, no humidity, a held confirmation; then a close one. Keypad: x 387 550
    # 713, y 110 214 318 422; OK at (713,422). The dialog's confirm is at (629,430).
    # The sim holds near 13 °C manual in this run; 25 is far from it, 13 close to it.
    KEYS25='tap 550 110;tap 550 214;tap 713 422'
    KEYS13='tap 387 110;tap 713 110;tap 713 422'
    CODE='tap 550 110;tap 550 214;tap 550 318;tap 550 422;tap 713 422'
    REFTAPS="sleep 5000;tap 729 430;tap 499 138;$CODE;$KEYS25;tap 713 422;sleep 1500;hold 629 430 2200;sleep 3000;tap 465 430;sleep 8000;tap 729 430;tap 499 138;$CODE;$KEYS13;tap 713 422;sleep 1500;hold 629 430 2200"
    build_rig refpath "panel|-realcore|-sim-rate|5|-preset-code|2580|-autotap|$REFTAPS"
    sha=$(sha256sum "$OUT/.rig-refpath" | cut -d' ' -f1)
    echo "== scene refpath local_sha256=$sha $(STAMP)"
    deploy/rig.sh deploy "$OUT/.rig-refpath" "$DEMO"
    deploy/rig.sh demo-start "$DEMO" | grep -E 'Main PID|Active:|Failed'
    t0=$(date +%s)
    at 12 refpath-1-confirm-far
    at 21 refpath-2-reference-alarm
    at 42 refpath-3-cleared
    deploy/rig.sh journal 120 curing-demo.service > "$OUT/54-rig-refcheck-journal.txt" 2>&1
    rm -f "$OUT/.rig-refpath"

    # An orderly end paints the notice. The process ends itself so the frame can be
    # taken before demo-stop lets the application repaint.
    build_rig stopped 'panel|-fixture|normal|-exit-after|3s'
    deploy/rig.sh deploy "$OUT/.rig-stopped" "$DEMO"
    deploy/rig.sh demo-start "$DEMO" | grep -E 'Main PID|Active:|Failed'
    sleep 8
    deploy/rig.sh screenshot "$OUT/rig/service-stopped.png"
    deploy/rig.sh journal 6 curing-demo.service
    rm -f "$OUT/.rig-stopped"
    deploy/rig.sh demo-stop
    deploy/rig.sh status
  } > "$OUT/50-rig-log.txt" 2>&1
  grep -E 'framebuffer|touch |backlight|frame screen|inject|autotap' "$OUT/51-rig-walk-journal.txt" > "$OUT/52-rig-walk-latency.txt"
  flock -u 8
fi
{ echo "tree at end $(STAMP)"; tree_hashes; } > "$OUT/02-versions-at-end.txt"
diff <(grep -v '^generated\|^go:\|^git HEAD' "$OUT/00-versions.txt") <(tail -n +2 "$OUT/02-versions-at-end.txt") > "$OUT/03-tree-changed-during-run.txt" 2>&1
echo "diff_exit=$?" >> "$OUT/03-tree-changed-during-run.txt"
rm -f "$OUT/.uidemo"
ls -R "$OUT" | wc -l
