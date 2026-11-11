package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type pdfResult struct {
	Input  string   `json:"input"`
	Output string   `json:"output"`
	Bytes  int64    `json:"bytes"`
	Pages  int      `json:"pages,omitempty"`
	Images []string `json:"images,omitempty"`
	Chrome string   `json:"chrome"`
}

func pdfRender(args []string) error {
	f := flag.NewFlagSet("pdf-render", flag.ContinueOnError)
	out := f.String("o", "", "output PDF (default: input with .pdf extension)")
	png := f.String("png", "", "also write page images to this directory (needs pdftoppm)")
	dpi := f.Int("dpi", 110, "page image resolution")
	timeout := f.Duration("timeout", 60*time.Second, "render time limit")
	jsonOut := f.Bool("json", false, "JSON output")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 1 {
		return fmt.Errorf("usage: pdf-render [-o out.pdf] [--png dir] [--json] input.html")
	}
	if *dpi < 30 || *dpi > 600 {
		return fmt.Errorf("--dpi must be between 30 and 600")
	}
	in, err := filepath.Abs(f.Arg(0))
	if err != nil {
		return err
	}
	if info, err := os.Stat(in); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", in)
	}
	dest := *out
	if dest == "" {
		dest = strings.TrimSuffix(in, filepath.Ext(in)) + ".pdf"
	}
	if dest, err = filepath.Abs(dest); err != nil {
		return err
	}
	if dest == in {
		return fmt.Errorf("output would overwrite the input")
	}
	chrome, err := findChrome()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	if err = printPDF(chrome, in, dest, *timeout); err != nil {
		return err
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return err
	}
	res := pdfResult{Input: in, Output: dest, Bytes: int64(len(data)), Pages: pdfPageCount(data), Chrome: chrome}
	if *png != "" {
		if res.Images, err = rasterise(dest, *png, *dpi); err != nil {
			return err
		}
	}
	if *jsonOut {
		return encode(res)
	}
	pages := "unknown"
	if res.Pages > 0 {
		pages = strconv.Itoa(res.Pages)
	}
	fmt.Printf("%s (%s pages, %d KB)\n", res.Output, pages, (res.Bytes+1023)/1024)
	for _, img := range res.Images {
		fmt.Println(img)
	}
	return nil
}

// findChrome prefers PDF_RENDER_CHROME, then installed Chrome-family browsers.
func findChrome() (string, error) {
	if p := os.Getenv("PDF_RENDER_CHROME"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("PDF_RENDER_CHROME: %w", err)
		}
		return p, nil
	}
	var candidates []string
	if runtime.GOOS == "darwin" {
		for _, app := range []string{"Google Chrome", "Chromium", "Google Chrome for Testing", "Microsoft Edge", "Brave Browser"} {
			for _, root := range []string{"/Applications", homePath("Applications")} {
				candidates = append(candidates, filepath.Join(root, app+".app/Contents/MacOS", app))
			}
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no Chrome or Chromium found; install one or set PDF_RENDER_CHROME")
}

// printPDF renders into a temporary file beside dest so a failed run never
// leaves a partial PDF at the destination.
func printPDF(chrome, in, dest string, timeout time.Duration) error {
	profile, err := os.MkdirTemp("", "pdf-render-profile-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".pdf-render-*.pdf")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	os.Remove(tmp.Name()) // Chrome must create it; a stale empty file would look like output.

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome,
		"--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--disable-extensions", "--no-pdf-header-footer",
		"--user-data-dir="+profile,
		"--virtual-time-budget=10000",
		"--print-to-pdf="+tmp.Name(),
		(&url.URL{Scheme: "file", Path: in}).String())
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return fmt.Errorf("render timed out after %s", timeout)
	}
	data, readErr := os.ReadFile(tmp.Name())
	if runErr != nil || readErr != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return fmt.Errorf("chrome produced no PDF (%v)\n%s", errors.Join(runErr, readErr), tail(stderr.String(), 20))
	}
	return os.Rename(tmp.Name(), dest)
}

var pagesCount = regexp.MustCompile(`/Type\s*/Pages\b[^>]*?/Count\s+(\d+)|/Count\s+(\d+)[^>]*?/Type\s*/Pages\b`)

// pdfPageCount reads the largest page-tree count, which is the root's. It
// returns 0 when the tree is compressed and cannot be read without a parser.
func pdfPageCount(data []byte) int {
	n := 0
	for _, m := range pagesCount.FindAllSubmatch(data, -1) {
		s := m[1]
		if len(s) == 0 {
			s = m[2]
		}
		if v, err := strconv.Atoi(string(s)); err == nil && v > n {
			n = v
		}
	}
	return n
}

func rasterise(pdf, dir string, dpi int) ([]string, error) {
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		return nil, errors.New("--png needs pdftoppm (poppler); the PDF was still written")
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	prefix := filepath.Join(dir, strings.TrimSuffix(filepath.Base(pdf), ".pdf")+"-page")
	old, _ := filepath.Glob(prefix + "-*.png")
	for _, p := range old {
		os.Remove(p) // stale pages from a longer earlier render would mislead review
	}
	if out, err := exec.Command(tool, "-r", strconv.Itoa(dpi), "-png", pdf, prefix).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %v\n%s", err, out)
	}
	imgs, err := filepath.Glob(prefix + "-*.png")
	sort.Strings(imgs)
	return imgs, err
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimSpace(s), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}
