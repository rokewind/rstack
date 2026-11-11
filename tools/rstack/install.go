package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

func install(args []string) error {
	f := flag.NewFlagSet("install", flag.ContinueOnError)
	shims := f.Bool("shims", false, "also wrap installed build commands")
	maintain := f.Bool("maintenance", false, "schedule idle cache cleanup on macOS")
	if err := f.Parse(args); err != nil {
		return err
	}
	if len(f.Args()) > 0 {
		return fmt.Errorf("unexpected installer arguments")
	}
	if *maintain && runtime.GOOS != "darwin" {
		return fmt.Errorf("--maintenance requires macOS")
	}
	src, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	dest := homePath(".local/share/rstack/rstack")
	bin := homePath(".local/bin")
	backup := homePath(".local/share/rstack/backups", strconv.FormatInt(time.Now().UnixNano(), 10))
	if err = os.MkdirAll(bin, 0700); err != nil {
		return err
	}
	if err = os.MkdirAll(backup, 0700); err != nil {
		return err
	}
	preserve := func(path string) error {
		info, e := os.Lstat(path)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		saved := filepath.Join(backup, filepath.Base(path))
		if info.Mode()&os.ModeSymlink != 0 {
			link, e := os.Readlink(path)
			if e != nil {
				return e
			}
			return os.Symlink(link, saved)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(saved, b, info.Mode().Perm())
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	commands := []string{"build-run", "disk-report", "workspace-clean"}
	if *shims {
		for name := range guarded {
			if old := cfg.Binaries[name]; old != "" {
				if _, e := os.Stat(old); e == nil {
					commands = append(commands, name)
					continue
				}
			}
			path, e := exec.LookPath(name)
			if e != nil {
				continue
			}
			resolved, e := filepath.EvalSymlinks(path)
			if e != nil {
				return e
			}
			if resolved == dest {
				return fmt.Errorf("missing original executable for %s", name)
			}
			if path == filepath.Join(bin, name) {
				saved := filepath.Join(backup, "original", name)
				b, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				if e = atomicWrite(saved, b, 0755); e != nil {
					return e
				}
				path = saved
			}
			cfg.Binaries[name] = path
			commands = append(commands, name)
		}
	}
	if err = preserve(homePath(".config/build-guard.json")); err != nil {
		return err
	}
	if err = writeJSON(homePath(".config/build-guard.json"), cfg); err != nil {
		return err
	}
	if _, e := os.Stat(dest); e == nil {
		b, e := os.ReadFile(dest)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(backup, "rstack.previous"), b, 0755); e != nil {
			return e
		}
	}
	if err = atomicWrite(dest, data, 0755); err != nil {
		return err
	}
	for _, name := range append([]string{"rstack"}, commands...) {
		path := filepath.Join(bin, name)
		if err = preserve(path); err != nil {
			return err
		}
		tmp := filepath.Join(bin, ".rstack-install-"+name)
		if err = os.Symlink(dest, tmp); err != nil {
			return err
		}
		if err = os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if *maintain {
		label := "dev.rstack.build-guard"
		path := homePath("Library/LaunchAgents", label+".plist")
		if err = preserve(path); err != nil {
			return err
		}
		// Use the stable home-relative shell expansion, not an interpolated XML path.
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>/bin/sh</string><string>-c</string><string>exec "$HOME/.local/bin/build-run" --maintain</string></array><key>RunAtLoad</key><true/><key>StartInterval</key><integer>60</integer></dict></plist>`, label)
		if err = atomicWrite(path, []byte(plist), 0600); err != nil {
			return err
		}
		domain := "gui/" + strconv.Itoa(os.Getuid())
		exec.Command("launchctl", "bootout", domain+"/"+label).Run()
		if out, e := exec.Command("launchctl", "bootstrap", domain, path).CombinedOutput(); e != nil {
			return fmt.Errorf("launchctl: %s: %w", out, e)
		}
	}
	fmt.Println("Installed native tools in", bin, "\nBackups:", backup)
	return nil
}
