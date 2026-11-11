package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeChrome writes a script that behaves like Chrome's --print-to-pdf flag.
func fakeChrome(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "chrome")
	script := "#!/bin/sh\nfor a; do case $a in --print-to-pdf=*) out=${a#--print-to-pdf=};; esac; done\n" + body
	must(t, os.WriteFile(p, []byte(script), 0755))
	t.Setenv("PDF_RENDER_CHROME", p)
}

func htmlInput(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc $(x).html")
	must(t, os.WriteFile(p, []byte("<p>hi</p>"), 0644))
	return p
}

func TestPDFPageCount(t *testing.T) {
	for in, want := range map[string]int{
		"<</Type /Pages\n/Count 12\n/Kids [1 0 R]>> <</Type /Pages /Count 3>>": 12,
		"<</Count 4 /Kids [] /Type/Pages>>":                                    4,
		"<</Type /Outlines /Count 9>>":                                         0,
	} {
		if got := pdfPageCount([]byte(in)); got != want {
			t.Errorf("%q: got %d want %d", in, got, want)
		}
	}
}

func TestPDFRenderWritesOutput(t *testing.T) {
	fakeChrome(t, `printf '%%PDF-1.4\n<</Type /Pages /Count 2>>' > "$out"`)
	in := htmlInput(t)
	out := filepath.Join(t.TempDir(), "nested", "out.pdf")
	s, err := capture(t, func() error { return pdfRender([]string{"-o", out, in}) })
	must(t, err)
	if !strings.Contains(s, "(2 pages") {
		t.Fatal(s)
	}
	if b, _ := os.ReadFile(out); !strings.HasPrefix(string(b), "%PDF-") {
		t.Fatal("missing output")
	}
}

func TestPDFRenderFailureKeepsExistingOutput(t *testing.T) {
	fakeChrome(t, `printf 'not a pdf' > "$out"; echo boom >&2`)
	in := htmlInput(t)
	out := filepath.Join(t.TempDir(), "out.pdf")
	must(t, os.WriteFile(out, []byte("previous"), 0644))
	err := pdfRender([]string{"-o", out, in})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "previous" {
		t.Fatal("existing output replaced")
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(out), ".pdf-render-*")); len(left) != 0 {
		t.Fatal("temporary file left", left)
	}
}

func TestPDFRenderTimeoutKillsProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	fakeChrome(t, `(sleep 2; touch "`+marker+`") & wait`)
	start := time.Now()
	err := pdfRender([]string{"--timeout", "300ms", htmlInput(t)})
	if err == nil || !strings.Contains(err.Error(), "timed out") || time.Since(start) > 2*time.Second {
		t.Fatal(err, time.Since(start))
	}
	time.Sleep(2500 * time.Millisecond)
	if _, e := os.Stat(marker); e == nil {
		t.Fatal("child process survived timeout")
	}
}

func TestPDFRenderRejectsOverwritingInput(t *testing.T) {
	fakeChrome(t, "")
	in := htmlInput(t)
	if err := pdfRender([]string{"-o", in, in}); err == nil {
		t.Fatal("accepted output equal to input")
	}
}

// Uses a real browser when one is installed; skipped otherwise.
func TestPDFRenderRealChrome(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	t.Setenv("PDF_RENDER_CHROME", "")
	if _, err := findChrome(); err != nil {
		t.Skip(err)
	}
	in := filepath.Join(t.TempDir(), "report #1 50% draft.html")
	must(t, os.WriteFile(in, []byte(`<style>@page{size:A4;@bottom-right{content:counter(page) " / " counter(pages)}}</style><p>one</p><p style="break-before:page">two</p>`), 0644))
	s, err := capture(t, func() error { return pdfRender([]string{in}) })
	must(t, err)
	if !strings.Contains(s, "(2 pages") {
		t.Fatal(s)
	}
}
