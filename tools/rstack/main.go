package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const gib = int64(1 << 30)

func main() {
	code, err := dispatch(filepath.Base(os.Args[0]), os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "rstack:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func dispatch(name string, args []string) (int, error) {
	if name == "rstack" || name == "rstack.test" {
		if len(args) == 0 || args[0] == "--help" {
			fmt.Println("rstack: build-run | disk-report | workspace-clean | pdf-render | install")
			return 0, nil
		}
		name, args = args[0], args[1:]
	}
	switch name {
	case "disk-report":
		return 0, diskReport(args)
	case "workspace-clean":
		return 0, workspaceClean(args)
	case "pdf-render":
		return 0, pdfRender(args)
	case "install":
		return 0, install(args)
	case "build-run", "go", "cargo", "npm", "pnpm", "terraform":
		g, err := newGuard()
		if err != nil {
			return 75, err
		}
		return g.cli(name, args)
	default:
		return 2, fmt.Errorf("unknown command %q", name)
	}
}

func homePath(parts ...string) string {
	h, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	return filepath.Join(append([]string{h}, parts...)...)
}
func encode(v any) error { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
func atomicWrite(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, b, 0600)
}
func managedDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing non-directory or symlink: %s", path)
	}
	if info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("unsafe ownership or permissions: %s", path)
	}
	return nil
}
func freeBytes(path string) (int64, error) {
	var s syscall.Statfs_t
	err := syscall.Statfs(path, &s)
	return int64(s.Bavail) * int64(s.Bsize), err
}

// Allocated bytes, without following symlinks or counting hard links twice.
func diskSize(path string) (int64, error) {
	var total int64
	seen := map[[2]uint64]bool{}
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		s := info.Sys().(*syscall.Stat_t)
		key := [2]uint64{uint64(s.Dev), uint64(s.Ino)}
		if !seen[key] {
			total += s.Blocks * 512
			seen[key] = true
		}
		return nil
	})
	return total, err
}
func execReplace(args []string) error {
	path, err := exec.LookPath(args[0])
	if err != nil {
		return err
	}
	return syscall.Exec(path, args, os.Environ())
}
func acquire(path string, wait bool) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	mode := syscall.LOCK_EX
	if !wait {
		mode |= syscall.LOCK_NB
	}
	if err = syscall.Flock(int(f.Fd()), mode); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, err
	}
	return f, nil
}
