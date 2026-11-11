package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type diskRow struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"allocated_bytes"`
	Change *int64 `json:"change_bytes,omitempty"`
	Error  string `json:"error,omitempty"`
}
type diskSnapshot struct {
	At    time.Time        `json:"at"`
	Sizes map[string]int64 `json:"sizes"`
}

func diskReport(args []string) error {
	flags := flag.NewFlagSet("disk-report", flag.ContinueOnError)
	jsonOut := flags.Bool("json", false, "JSON output")
	save := flags.Bool("save", false, "save a complete scan as the next comparison baseline")
	top := flags.Int("top", 20, "rows to display (0 means all)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *top < 0 {
		return fmt.Errorf("--top must be nonnegative")
	}
	roots := flags.Args()
	if len(roots) == 0 {
		roots = []string{homePath()}
	}
	for i, p := range roots {
		a, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		roots[i] = a
	}
	sort.Strings(roots)
	encodedRoots, _ := json.Marshal(roots)
	key := fmt.Sprintf("%x", sha256.Sum256(encodedRoots))
	state := homePath(".local/state/rstack/disk-report")
	if err := os.MkdirAll(state, 0700); err != nil {
		return err
	}
	lock, err := acquire(filepath.Join(state, key+".lock"), true)
	if err != nil {
		return err
	}
	defer lock.Close()
	prior := diskSnapshot{}
	baseline := filepath.Join(state, key+".json")
	if data, e := os.ReadFile(baseline); e == nil {
		if err = json.Unmarshal(data, &prior); err != nil {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	next := diskSnapshot{At: time.Now().UTC(), Sizes: map[string]int64{}}
	rows := []diskRow{}
	seen := map[string]bool{}
	partial := false
	for _, root := range roots {
		entries, e := os.ReadDir(root)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			path := filepath.Join(root, entry.Name())
			if seen[path] || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			seen[path] = true
			size, e := diskSize(path)
			row := diskRow{Path: path, Bytes: size}
			if e != nil {
				row.Error = e.Error()
				partial = true
			} else {
				next.Sizes[path] = size
				if old, ok := prior.Sizes[path]; ok {
					delta := size - old
					row.Change = &delta
				}
			}
			rows = append(rows, row)
		}
	}
	// A disappeared directory is a real decrease, not an omitted row.
	for path, old := range prior.Sizes {
		if !seen[path] {
			if _, e := os.Lstat(path); os.IsNotExist(e) {
				delta := -old
				rows = append(rows, diskRow{Path: path, Change: &delta})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Bytes > rows[j].Bytes })
	if *save && !partial {
		if err = writeJSON(baseline, next); err != nil {
			return err
		}
	}
	visible := rows
	if *top > 0 && len(visible) > *top {
		visible = visible[:*top]
	}
	if *jsonOut {
		err = encode(map[string]any{"at": next.At, "baseline_at": prior.At, "partial": partial, "saved": *save && !partial, "rows": visible})
	} else {
		fmt.Println("GiB\tCHANGE GiB\tPATH")
		for _, r := range visible {
			delta := "-"
			if r.Change != nil {
				delta = fmt.Sprintf("%+.3f", float64(*r.Change)/float64(gib))
			}
			fmt.Printf("%.3f\t%s\t%s", float64(r.Bytes)/float64(gib), delta, r.Path)
			if r.Error != "" {
				fmt.Printf(" [INCOMPLETE: %s]", r.Error)
			}
			fmt.Println()
		}
		if !prior.At.IsZero() {
			fmt.Println("Compared with", prior.At.Format(time.RFC3339))
		}
		if *save && !partial {
			fmt.Println("Baseline saved.")
		}
	}
	if err != nil {
		return err
	}
	if partial {
		return fmt.Errorf("scan incomplete; baseline not updated")
	}
	return nil
}
