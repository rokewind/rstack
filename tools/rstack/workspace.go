package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type workspace struct {
	Path    string   `json:"path"`
	Head    string   `json:"head"`
	Branch  string   `json:"branch"`
	Bytes   int64    `json:"allocated_bytes"`
	Reasons []string `json:"hold_reasons"`
}

func git(repo string, args ...string) ([]byte, error) {
	out, err := exec.Command("git", append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return out, nil
}
func workspaces(repo, base string) ([]workspace, error) {
	if strings.HasPrefix(base, "-") {
		return nil, errors.New("base must be a ref, not an option")
	}
	if _, err := git(repo, "rev-parse", "--verify", base+"^{commit}"); err != nil {
		return nil, err
	}
	data, err := git(repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	result := []workspace{}
	for _, record := range strings.Split(string(data), "\x00\x00") {
		if record == "" {
			continue
		}
		w := workspace{Reasons: []string{}}
		for _, line := range strings.Split(record, "\x00") {
			key, value, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				w.Path = value
			case "HEAD":
				w.Head = value
			case "branch":
				w.Branch = value
			case "locked", "prunable":
				w.Reasons = append(w.Reasons, key)
			}
		}
		if w.Path == "" {
			continue
		}
		if len(result) == 0 {
			w.Reasons = append(w.Reasons, "primary worktree")
		}
		status, e := git(w.Path, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching")
		if e != nil {
			w.Reasons = append(w.Reasons, "cannot inspect status")
		} else if len(status) > 0 {
			w.Reasons = append(w.Reasons, "tracked, untracked or ignored files need attention")
		}
		if _, e = git(repo, "merge-base", "--is-ancestor", w.Head, base); e != nil {
			w.Reasons = append(w.Reasons, "HEAD is not proven merged into base")
		}
		w.Bytes, e = diskSize(w.Path)
		if e != nil {
			w.Reasons = append(w.Reasons, "cannot fully inspect files")
		}
		result = append(result, w)
	}
	return result, nil
}
func workspaceClean(args []string) error {
	f := flag.NewFlagSet("workspace-clean", flag.ContinueOnError)
	repo := f.String("repo", ".", "repository to inspect")
	base := f.String("base", "HEAD", "local ref that must contain the candidate commits")
	remove := f.String("remove", "", "exact worktree path to select")
	apply := f.Bool("apply", false, "remove the selected eligible worktree")
	inactive := f.Bool("confirm-inactive", false, "confirm no thread, editor or background job uses this worktree")
	jsonOut := f.Bool("json", false, "JSON output")
	if err := f.Parse(args); err != nil {
		return err
	}
	if len(f.Args()) > 0 {
		return errors.New("unexpected arguments; use --repo PATH")
	}
	if *apply && (*remove == "" || !*inactive) {
		return errors.New("--apply requires --remove PATH and --confirm-inactive")
	}
	rows, err := workspaces(*repo, *base)
	if err != nil {
		return err
	}
	if *remove != "" {
		target, err := filepath.EvalSymlinks(*remove)
		if err != nil {
			return err
		}
		target, err = filepath.Abs(target)
		if err != nil {
			return err
		}
		var selected *workspace
		for i := range rows {
			p, e := filepath.EvalSymlinks(rows[i].Path)
			if e == nil && p == target {
				selected = &rows[i]
				break
			}
		}
		if selected == nil {
			return errors.New("path is not a registered worktree")
		}
		if *apply {
			if len(selected.Reasons) > 0 {
				return fmt.Errorf("refusing removal: %s", strings.Join(selected.Reasons, ", "))
			}
			// Explicit inactivity confirmation also covers pinned agent sessions that lsof cannot see.
			check := exec.Command("lsof", "-nP", "+D", target)
			out, e := check.CombinedOutput()
			if len(out) > 0 {
				return errors.New("worktree has open files or lsof could not inspect it")
			}
			var exit *exec.ExitError
			if e != nil && (!errors.As(e, &exit) || exit.ExitCode() != 1) {
				return fmt.Errorf("cannot verify open files: %w", e)
			}
			current, e := git(target, "rev-parse", "HEAD")
			if e != nil || strings.TrimSpace(string(current)) != selected.Head {
				return errors.New("HEAD changed during inspection")
			}
			status, e := git(target, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching")
			if e != nil || len(status) > 0 {
				return errors.New("worktree changed during inspection")
			}
			if _, e = git(*repo, "merge-base", "--is-ancestor", selected.Head, *base); e != nil {
				return errors.New("merge status changed during inspection")
			}
			if _, err = git(*repo, "worktree", "remove", target); err != nil {
				return err
			} // no force, no branch deletion
			fmt.Println("Removed", target)
			return nil
		}
		rows = []workspace{*selected}
	}
	if *jsonOut {
		return encode(rows)
	}
	for _, w := range rows {
		status := "review inactivity before removal"
		if len(w.Reasons) > 0 {
			status = "hold: " + strings.Join(w.Reasons, ", ")
		}
		fmt.Printf("%.3f GiB\t%s\t%s\n", float64(w.Bytes)/float64(gib), w.Path, status)
	}
	fmt.Fprintln(os.Stdout, "Dry run. No files or branches removed. Merge checks use local refs; fetch first if needed.")
	return nil
}
