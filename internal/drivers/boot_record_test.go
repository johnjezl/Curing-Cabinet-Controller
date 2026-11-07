package drivers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These run deploy/files/curing-boot-record with a stub journalctl.
func bootRecordEnv(t *testing.T) (run func(arg, bootID string), dir string) {
	t.Helper()
	tmp := t.TempDir()
	dir = filepath.Join(tmp, "records")
	bin := filepath.Join(tmp, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\necho 'stub journal line'\n"
	if err := os.WriteFile(filepath.Join(bin, "journalctl"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	idFile := filepath.Join(tmp, "boot_id")
	script, err := filepath.Abs("../../deploy/files/curing-boot-record")
	if err != nil {
		t.Fatal(err)
	}
	run = func(arg, bootID string) {
		t.Helper()
		if err := os.WriteFile(idFile, []byte(bootID+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", script, arg)
		cmd.Env = append(os.Environ(),
			"PATH="+bin+":"+os.Getenv("PATH"),
			"CURING_RECORD_DIR="+dir,
			"CURING_DT_DIR="+filepath.Join(tmp, "no-dt"),
			"CURING_BOOT_ID_FILE="+idFile,
			"CURING_KEEP_UNCLEAN=5",
			"CURING_KEEP_CLEAN=3")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", arg, err, out)
		}
	}
	return run, dir
}

func recordsOf(t *testing.T, dir, class string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "*-"+class+"-*.txt.gz"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBootRecordClassifiesShutdowns(t *testing.T) {
	run, dir := bootRecordEnv(t)
	run("start", "a")
	run("start", "a") // same boot: no second record
	run("mark-clean", "a")
	run("start", "b") // a was clean
	run("start", "c") // b never marked clean
	idx, err := os.ReadFile(filepath.Join(dir, "index.log"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(string(idx)), "\n") {
		got = append(got, strings.Fields(l)[2])
	}
	want := "class=first class=clean class=unclean"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v want %s", got, want)
	}
}

func TestCleanBootsDoNotEvictUncleanRecords(t *testing.T) {
	run, dir := bootRecordEnv(t)
	run("start", "boot0")
	for i := 1; i <= 4; i++ { // four unclean resets
		run("start", fmt.Sprintf("u%d", i))
	}
	first := recordsOf(t, dir, "unclean")
	if len(first) != 4 {
		t.Fatalf("%d unclean records", len(first))
	}
	prev := "u4"
	for i := 0; i < 8; i++ { // many clean reboots
		run("mark-clean", prev)
		prev = fmt.Sprintf("c%d", i)
		run("start", prev)
	}
	if got := recordsOf(t, dir, "unclean"); strings.Join(got, ",") != strings.Join(first, ",") {
		t.Errorf("clean reboots changed unclean records: %v", got)
	}
	if got := recordsOf(t, dir, "clean"); len(got) > 3 {
		t.Errorf("%d clean records kept, limit 3", len(got))
	}
}

func TestUncleanRecordsAreCappedKeepingNewest(t *testing.T) {
	run, dir := bootRecordEnv(t)
	run("start", "boot0")
	for i := 1; i <= 9; i++ {
		run("start", fmt.Sprintf("u%d", i))
	}
	got := recordsOf(t, dir, "unclean")
	if len(got) != 5 {
		t.Fatalf("%d unclean records, limit 5", len(got))
	}
	if !strings.Contains(filepath.Base(got[len(got)-1]), "000010-") {
		t.Errorf("newest missing: %v", got)
	}
}

// These run deploy/files/curing-display-status against files standing in for the
// system, including the missing-panel case, which cannot be produced on demand on the rig.
func TestDisplayStatusScript(t *testing.T) {
	tmp := t.TempDir()
	run := filepath.Join(tmp, "run")
	fb := filepath.Join(tmp, "fb0")
	input := filepath.Join(tmp, "input")
	uptime := filepath.Join(tmp, "uptime")
	if err := os.WriteFile(uptime, []byte("31.50 100.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../deploy/files/curing-display-status")
	if err != nil {
		t.Fatal(err)
	}
	exec1 := func() string {
		t.Helper()
		cmd := exec.Command("bash", script)
		cmd.Env = append(os.Environ(), "CURING_RUN_DIR="+run, "CURING_FB="+fb, "CURING_INPUT_DEVICES="+input, "CURING_UPTIME="+uptime, "CURING_PANEL_MARK="+filepath.Join(tmp, "panel-mark"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return string(out)
	}
	missing := func() bool {
		_, err := os.Stat(filepath.Join(run, "display-missing"))
		return err == nil
	}
	if out := exec1(); !missing() || !strings.Contains(out, "<2>") {
		t.Errorf("no panel: missing=%v output %q", missing(), out)
	}
	if b, _ := os.ReadFile(filepath.Join(run, "display-status")); string(b) != "fb0=no\ntouch=no\npanel_source=firmware\nuptime=31.50\n" {
		t.Errorf("status %q", b)
	}
	if err := os.WriteFile(fb, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if exec1(); !missing() {
		t.Error("framebuffer without touch must still count as missing")
	}
	if err := os.WriteFile(input, []byte("N: Name=\"10-0038 generic ft5x06 (79)\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if exec1(); missing() {
		t.Error("marker must clear when the panel is up")
	}
}
