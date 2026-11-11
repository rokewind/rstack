package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type guardConfig struct {
	Reserve    float64           `json:"reserve_gib"`
	Buffer     float64           `json:"buffer_gib"`
	Admission  float64           `json:"admission_gib"`
	CacheLimit float64           `json:"cache_limit_gib"`
	Poll       float64           `json:"poll_seconds"`
	Binaries   map[string]string `json:"binaries"`
}
type activeBuild struct {
	PGID int    `json:"pgid"`
	Run  string `json:"run"`
	Tool string `json:"tool"`
}
type guard struct {
	root string
	cfg  guardConfig
	free func() (int64, error)
}

func loadConfig() (guardConfig, error) {
	c := guardConfig{5, 2, 10, 8, .5, map[string]string{}}
	b, err := os.ReadFile(homePath(".config/build-guard.json"))
	if err == nil {
		err = json.Unmarshal(b, &c)
	} else if os.IsNotExist(err) {
		err = nil
	}
	if c.Binaries == nil {
		c.Binaries = map[string]string{}
	}
	if err == nil && (c.Reserve < 5 || c.Buffer < 2 || c.Admission < c.Reserve+c.Buffer || c.CacheLimit <= 0 || c.Poll < .05 || c.Poll > 1) {
		err = errors.New("unsafe guard thresholds")
	}
	return c, err
}
func newGuard() (*guard, error) {
	c, err := loadConfig()
	if err != nil {
		return nil, err
	}
	g := &guard{root: homePath(".local/state/build-guard"), cfg: c}
	for _, p := range []string{"runs", "cache"} {
		if err = os.MkdirAll(filepath.Join(g.root, p), 0700); err != nil {
			return nil, err
		}
	}
	for _, p := range []string{g.root, filepath.Join(g.root, "runs"), filepath.Join(g.root, "cache")} {
		if err = managedDir(p); err != nil {
			return nil, err
		}
	}
	g.free = func() (int64, error) { return freeBytes(g.root) }
	return g, nil
}
func (g *guard) active() (activeBuild, error) {
	var a activeBuild
	b, err := os.ReadFile(filepath.Join(g.root, "active.json"))
	if err != nil {
		return a, err
	}
	err = json.Unmarshal(b, &a)
	if err == nil && a.PGID <= 1 {
		err = errors.New("invalid build process group")
	}
	return a, err
}
func (g *guard) nested() bool {
	if a, err := g.active(); err == nil && a.PGID == syscall.Getpgrp() && a.Run == os.Getenv("BUILD_GUARD_RUN") {
		return true
	}
	fd, err := strconv.Atoi(os.Getenv("BUILD_GUARD_LOCK_FD"))
	if err != nil {
		return false
	}
	var s, t syscall.Stat_t
	return syscall.Fstat(fd, &s) == nil && syscall.Stat(filepath.Join(g.root, "build.lock"), &t) == nil && s.Dev == t.Dev && s.Ino == t.Ino
}
func groupSignal(pgid int, sig syscall.Signal) (bool, error) {
	if pgid <= 1 {
		return false, errors.New("invalid process group")
	}
	err := syscall.Kill(-pgid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if errors.Is(err, syscall.EPERM) {
		// macOS may report EPERM for a process group containing only zombies.
		out, e := exec.Command("/bin/ps", "-axo", "pgid=,stat=").Output()
		if e != nil {
			return false, err
		}
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == strconv.Itoa(pgid) && !strings.HasPrefix(f[1], "Z") {
				return false, err
			}
		}
		return false, nil
	}
	return err == nil, err
}
func clearContents(path string) error {
	if err := managedDir(path); err != nil {
		return err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	after, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return errors.New("managed directory changed during cleanup")
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err = root.RemoveAll(e.Name()); err != nil {
			return err
		}
	}
	return nil
}
func (g *guard) cleanup() error {
	a, err := g.active()
	if err == nil {
		live, e := groupSignal(a.PGID, 0)
		if e != nil {
			return e
		}
		if live {
			return errors.New("previous build group still exists; inspect active.json")
		}
		if err = os.Remove(filepath.Join(g.root, "active.json")); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = clearContents(filepath.Join(g.root, "runs")); err != nil {
		return err
	}
	size, err := diskSize(filepath.Join(g.root, "cache"))
	if err != nil {
		return err
	}
	free, err := g.free()
	if err != nil {
		return err
	}
	if float64(size) > g.cfg.CacheLimit*float64(gib) || float64(free) < g.cfg.Admission*float64(gib) {
		return clearContents(filepath.Join(g.root, "cache"))
	}
	return nil
}
func (g *guard) env(run string) ([]string, error) {
	vars := map[string]string{"GOCACHE": filepath.Join(g.root, "cache/go-build"), "npm_config_cache": filepath.Join(g.root, "cache/npm"), "UV_CACHE_DIR": filepath.Join(g.root, "cache/uv"), "GOMODCACHE": homePath("go/pkg/mod"), "TF_PLUGIN_CACHE_DIR": homePath(".cache/terraform-plugin-cache"), "TMPDIR": filepath.Join(run, "tmp"), "GOTMPDIR": filepath.Join(run, "tmp"), "BUILD_OUT": filepath.Join(run, "out")}
	for _, p := range vars {
		if err := os.MkdirAll(p, 0700); err != nil {
			return nil, err
		}
	}
	vars["BUILD_GUARD_RUN"] = run
	vars["BUILD_GUARD_LOCK_FD"] = "3"
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := vars[key]; !ok {
			env = append(env, entry)
		}
	}
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	return env, nil
}
func (g *guard) run(args []string) (code int, err error) {
	if len(args) == 0 {
		return 2, errors.New("usage: build-run -- COMMAND [ARGS...]")
	}
	if g.nested() {
		return 75, execReplace(args)
	}
	lock, err := acquire(filepath.Join(g.root, "build.lock"), false)
	if err != nil {
		return 75, err
	}
	if lock == nil {
		fmt.Fprintln(os.Stderr, "build-run: waiting for shared build slot")
		lock, err = acquire(filepath.Join(g.root, "build.lock"), true)
		if err != nil {
			return 75, err
		}
	}
	defer lock.Close()
	if err = g.cleanup(); err != nil {
		return 75, err
	}
	free, err := g.free()
	if err != nil {
		return 75, err
	}
	if float64(free) < g.cfg.Admission*float64(gib) {
		return 75, fmt.Errorf("need %.0f GiB free to start", g.cfg.Admission)
	}
	run, err := os.MkdirTemp(filepath.Join(g.root, "runs"), "run-")
	if err != nil {
		return 75, err
	}
	env, err := g.env(run)
	if err != nil {
		os.RemoveAll(run)
		return 75, err
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err = cmd.Start(); err != nil {
		os.RemoveAll(run)
		return 75, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	defer func() {
		_, e := groupSignal(cmd.Process.Pid, syscall.SIGTERM)
		if e == nil && !finished {
			select {
			case <-done:
				finished = true
			case <-time.After(500 * time.Millisecond):
			}
		}
		if e == nil {
			_, e = groupSignal(cmd.Process.Pid, syscall.SIGKILL)
		}
		if e != nil {
			code = 75
			err = e
			return
		} // retain record and scratch if termination fails
		if !finished {
			<-done
		}
		if e = os.Remove(filepath.Join(g.root, "active.json")); e != nil && !os.IsNotExist(e) {
			code = 75
			err = e
			return
		}
		if e = os.RemoveAll(run); e != nil {
			code = 75
			err = e
		}
	}()
	if err = writeJSON(filepath.Join(g.root, "active.json"), activeBuild{cmd.Process.Pid, run, filepath.Base(args[0])}); err != nil {
		return 75, err
	}
	ticker := time.NewTicker(time.Duration(g.cfg.Poll * float64(time.Second)))
	defer ticker.Stop()
	for {
		select {
		case e := <-done:
			finished = true
			if e == nil {
				return 0, nil
			}
			var exit *exec.ExitError
			if errors.As(e, &exit) {
				if s, ok := exit.Sys().(syscall.WaitStatus); ok && s.Signaled() {
					return 128 + int(s.Signal()), nil
				}
				return exit.ExitCode(), nil
			}
			return 75, e
		case sig := <-signals:
			return 128 + int(sig.(syscall.Signal)), nil
		case <-ticker.C:
			free, e := g.free()
			if e != nil {
				return 75, e
			}
			if float64(free) < (g.cfg.Reserve+g.cfg.Buffer)*float64(gib) {
				return 75, errors.New("stopping build to protect disk reserve")
			}
		}
	}
}

var guarded = map[string]string{"go": "build test run install generate get mod vet", "cargo": "build test run check clippy install fetch", "npm": "install i ci rebuild run run-script test exec", "pnpm": "install i add rebuild run test exec dlx build", "terraform": "init validate plan providers"}

func (g *guard) cli(name string, args []string) (int, error) {
	if name != "build-run" {
		real := g.cfg.Binaries[name]
		if real == "" {
			return 75, fmt.Errorf("no executable configured for %s", name)
		}
		sub := ""
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				sub = a
				break
			}
		}
		command := append([]string{real}, args...)
		if sub != "" && strings.Contains(" "+guarded[name]+" ", " "+sub+" ") {
			return g.run(command)
		}
		return 75, execReplace(command)
	}
	if len(args) == 1 && args[0] == "--status" {
		free, err := g.free()
		if err != nil {
			return 75, err
		}
		var active any
		a, e := g.active()
		if e == nil {
			active = a
		} else if !os.IsNotExist(e) {
			return 75, e
		}
		return 0, encode(map[string]any{"free_gib": float64(free) / float64(gib), "reserve_gib": g.cfg.Reserve, "stop_below_gib": g.cfg.Reserve + g.cfg.Buffer, "start_above_gib": g.cfg.Admission, "active": active})
	}
	if len(args) == 1 && args[0] == "--maintain" {
		f, e := acquire(filepath.Join(g.root, "build.lock"), false)
		if e != nil {
			return 75, e
		}
		if f == nil {
			return 0, nil
		}
		defer f.Close()
		return 0, g.cleanup()
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	return g.run(args)
}
