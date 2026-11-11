package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func testGuard(t *testing.T) *guard {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BUILD_GUARD_RUN", "")
	t.Setenv("BUILD_GUARD_LOCK_FD", "")
	g, e := newGuard()
	must(t, e)
	g.free = func() (int64, error) { return 20 * gib, nil }
	g.cfg.Poll = .02
	return g
}
func capture(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	f, e := os.CreateTemp(t.TempDir(), "output")
	must(t, e)
	old := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = old; f.Close() }()
	err := fn()
	b, e := os.ReadFile(f.Name())
	must(t, e)
	return string(b), err
}

func TestGuardAdmission(t *testing.T) {
	g := testGuard(t)
	g.free = func() (int64, error) { return 6 * gib, nil }
	marker := filepath.Join(t.TempDir(), "never")
	code, e := g.run([]string{"/bin/sh", "-c", `touch "$1"`, "sh", marker})
	if code != 75 || e == nil {
		t.Fatal(code, e)
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("command ran")
	}
}
func TestGuardFailureCleanup(t *testing.T) {
	g := testGuard(t)
	keep := filepath.Join(t.TempDir(), "keep")
	code, e := g.run([]string{"/bin/sh", "-c", `echo temporary > "$BUILD_OUT/tmp"; printf '%s' "$2" > "$1"; exit 23`, "sh", keep, "literal $(no) value"})
	must(t, e)
	if code != 23 {
		t.Fatal(code)
	}
	b, e := os.ReadFile(keep)
	must(t, e)
	if string(b) != "literal $(no) value" {
		t.Fatal(string(b))
	}
	entries, e := os.ReadDir(filepath.Join(g.root, "runs"))
	must(t, e)
	if len(entries) != 0 {
		t.Fatal("scratch retained")
	}
}
func TestGuardLowSpaceKillsDescendants(t *testing.T) {
	g := testGuard(t)
	marker := filepath.Join(t.TempDir(), "heartbeat")
	g.free = func() (int64, error) {
		if _, e := os.Stat(marker); e == nil {
			return 6 * gib, nil
		}
		return 20 * gib, nil
	}
	code, e := g.run([]string{"/bin/sh", "-c", `(trap '' TERM; while :; do echo tick >> "$1"; sleep .02; done) & wait`, "sh", marker})
	if code != 75 || e == nil {
		t.Fatal(code, e)
	}
	first, e := os.ReadFile(marker)
	must(t, e)
	time.Sleep(100 * time.Millisecond)
	last, e := os.ReadFile(marker)
	must(t, e)
	if string(first) != string(last) {
		t.Fatal("descendant survived")
	}
}
func TestGuardSerialization(t *testing.T) {
	g := testGuard(t)
	order := filepath.Join(t.TempDir(), "order")
	var wg sync.WaitGroup
	var failures atomic.Int32
	for _, label := range []string{"a", "b"} {
		wg.Add(1)
		go func(label string) {
			defer wg.Done()
			code, e := g.run([]string{"/bin/sh", "-c", `echo "$2-start" >> "$1"; sleep .15; echo "$2-end" >> "$1"`, "sh", order, label})
			if code != 0 || e != nil {
				failures.Add(1)
			}
		}(label)
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatal("build failed")
	}
	b, e := os.ReadFile(order)
	must(t, e)
	s := string(b)
	if s != "a-start\na-end\nb-start\nb-end\n" && s != "b-start\nb-end\na-start\na-end\n" {
		t.Fatal(s)
	}
}
func TestHelper(t *testing.T) {
	if os.Getenv("RSTACK_TEST_HELPER") != "1" {
		return
	}
	for i, a := range os.Args {
		if a == "--" {
			code, e := dispatch("rstack", os.Args[i+1:])
			if e != nil {
				os.Stderr.WriteString(e.Error())
				if code == 0 {
					code = 1
				}
			}
			os.Exit(code)
		}
	}
	os.Exit(2)
}
func TestGuardNested(t *testing.T) {
	g := testGuard(t)
	t.Setenv("RSTACK_TEST_HELPER", "1")
	code, e := g.run([]string{os.Args[0], "-test.run=^TestHelper$", "--", "build-run", "--", "/bin/sh", "-c", "exit 17"})
	must(t, e)
	if code != 17 {
		t.Fatal(code)
	}
}
func TestMaintenanceSkipsLock(t *testing.T) {
	g := testGuard(t)
	lock, e := acquire(filepath.Join(g.root, "build.lock"), true)
	must(t, e)
	defer lock.Close()
	code, e := g.cli("build-run", []string{"--maintain"})
	must(t, e)
	if code != 0 {
		t.Fatal(code)
	}
}
func TestCleanupRejectsSymlink(t *testing.T) {
	g := testGuard(t)
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep")
	must(t, os.WriteFile(keep, []byte("keep"), 0600))
	path := filepath.Join(g.root, "runs")
	must(t, os.Remove(path))
	must(t, os.Symlink(outside, path))
	if e := g.cleanup(); e == nil {
		t.Fatal("accepted symlink")
	}
	if _, e := os.Stat(keep); e != nil {
		t.Fatal("outside file deleted")
	}
}
func TestDiskGrowthAndSymlinks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	file := filepath.Join(root, "data")
	must(t, os.WriteFile(file, make([]byte, 8192), 0600))
	must(t, os.Symlink(t.TempDir(), filepath.Join(root, "external")))
	_, e := capture(t, func() error { return diskReport([]string{"--json", "--save", root}) })
	must(t, e)
	must(t, os.WriteFile(file, make([]byte, 32768), 0600))
	out, e := capture(t, func() error { return diskReport([]string{"--json", root}) })
	must(t, e)
	var report struct {
		Rows []diskRow `json:"rows"`
	}
	must(t, json.Unmarshal([]byte(out), &report))
	if len(report.Rows) != 1 || report.Rows[0].Change == nil || *report.Rows[0].Change <= 0 {
		t.Fatal(out)
	}
}

func repoFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	must(t, os.Mkdir(repo, 0700))
	_, e := git(repo, "init", "-b", "main")
	must(t, e)
	for _, args := range [][]string{{"config", "user.name", "rstack-test"}, {"config", "user.email", "test@example.invalid"}, {"config", "commit.gpgsign", "false"}} {
		_, e = git(repo, args...)
		must(t, e)
	}
	must(t, os.WriteFile(filepath.Join(repo, "file"), []byte("hello"), 0600))
	_, e = git(repo, "add", "file")
	must(t, e)
	_, e = git(repo, "commit", "-m", "initial")
	must(t, e)
	wt := filepath.Join(dir, "work tree $(touch nope)")
	_, e = git(repo, "worktree", "add", "-b", "topic", wt)
	must(t, e)
	return repo, wt
}
func TestWorkspaceRefusals(t *testing.T) {
	repo, wt := repoFixture(t)
	if e := workspaceClean([]string{"--repo", repo, "--remove", repo, "--apply", "--confirm-inactive"}); e == nil {
		t.Fatal("primary removal allowed")
	}
	if e := workspaceClean([]string{"--repo", repo, "--remove", wt, "--apply"}); e == nil {
		t.Fatal("missing confirmation allowed")
	}
	for _, name := range []string{"untracked", "file"} {
		must(t, os.WriteFile(filepath.Join(wt, name), []byte("work"), 0600))
		if e := workspaceClean([]string{"--repo", repo, "--remove", wt, "--apply", "--confirm-inactive"}); e == nil {
			t.Fatal("dirty removal allowed")
		}
	}
	if _, e := os.Stat(wt); e != nil {
		t.Fatal(e)
	}
}
func TestWorkspaceLockedUnmergedIgnored(t *testing.T) {
	repo, wt := repoFixture(t)
	_, e := git(repo, "worktree", "lock", wt)
	must(t, e)
	rows, e := workspaces(repo, "main")
	must(t, e)
	if !strings.Contains(strings.Join(rows[1].Reasons, " "), "locked") {
		t.Fatal(rows)
	}
	_, e = git(repo, "worktree", "unlock", wt)
	must(t, e)
	must(t, os.WriteFile(filepath.Join(wt, "file"), []byte("new"), 0600))
	_, e = git(wt, "commit", "-am", "new")
	must(t, e)
	rows, e = workspaces(repo, "main")
	must(t, e)
	if !strings.Contains(strings.Join(rows[1].Reasons, " "), "not proven merged") {
		t.Fatal(rows)
	}
	must(t, os.WriteFile(filepath.Join(repo, ".git/info/exclude"), []byte("secret\n"), 0600))
	must(t, os.WriteFile(filepath.Join(wt, "secret"), []byte("keep"), 0600))
	rows, e = workspaces(repo, "topic")
	must(t, e)
	if !strings.Contains(strings.Join(rows[1].Reasons, " "), "ignored") {
		t.Fatal(rows)
	}
}
func TestWorkspaceDisablesGitMonitor(t *testing.T) {
	repo, _ := repoFixture(t)
	marker := filepath.Join(t.TempDir(), "executed")
	hook := filepath.Join(t.TempDir(), "monitor")
	must(t, os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700))
	_, e := git(repo, "config", "core.fsmonitor", hook)
	must(t, e)
	_, e = workspaces(repo, "main")
	must(t, e)
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("repository executable ran")
	}
}
func TestWorkspaceRemovalPreservesBranch(t *testing.T) {
	if _, e := exec.LookPath("lsof"); e != nil {
		t.Skip("lsof not installed")
	}
	repo, wt := repoFixture(t)
	_, e := capture(t, func() error { return workspaceClean([]string{"--repo", repo, "--remove", wt}) })
	must(t, e)
	if _, e = os.Stat(wt); e != nil {
		t.Fatal("dry run deleted worktree")
	}
	must(t, workspaceClean([]string{"--repo", repo, "--remove", wt, "--apply", "--confirm-inactive"}))
	if _, e = os.Stat(wt); !os.IsNotExist(e) {
		t.Fatal("worktree retained")
	}
	_, e = git(repo, "show-ref", "--verify", "refs/heads/topic")
	must(t, e)
}
func TestWorkspaceOpenFileRefused(t *testing.T) {
	if _, e := exec.LookPath("lsof"); e != nil {
		t.Skip("lsof not installed")
	}
	repo, wt := repoFixture(t)
	file, e := os.Open(filepath.Join(wt, "file"))
	must(t, e)
	defer file.Close()
	if e = workspaceClean([]string{"--repo", repo, "--remove", wt, "--apply", "--confirm-inactive"}); e == nil {
		t.Fatal("open file removal allowed")
	}
}

func TestInstaller(t *testing.T) {
	binary := os.Getenv("RSTACK_TEST_BINARY")
	if binary == "" {
		t.Skip("set RSTACK_TEST_BINARY to test a compiled release binary")
	}
	home := t.TempDir()
	bin := filepath.Join(home, ".local/bin")
	must(t, os.MkdirAll(bin, 0700))
	must(t, os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nprintf original-go"), 0755))
	env := append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"))
	run := func(path string, args ...string) string {
		t.Helper()
		cmd := exec.Command(path, args...)
		cmd.Env = env
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("%v: %s", e, out)
		}
		return string(out)
	}
	run(binary, "install", "--shims")
	if out := run(filepath.Join(bin, "go"), "version"); out != "original-go" {
		t.Fatal(out)
	}
	run(filepath.Join(bin, "build-run"), "--", "/usr/bin/true")
	run(filepath.Join(bin, "disk-report"), "--json", "--save", home)
	run(binary, "install", "--shims")
	if out := run(filepath.Join(bin, "go"), "version"); out != "original-go" {
		t.Fatal("reinstall lost original tool:", out)
	}
}
