package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListMarkdownFilesMixedDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.md", "a.md", "notes.txt", "image.png", "UPPER.MD"} {
		writeTestFile(t, filepath.Join(dir, name))
	}
	// Non-recursive: markdown inside subdirectories must be ignored, and a
	// directory whose own name ends in .md must not be listed either.
	sub := filepath.Join(dir, "nested.md")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(sub, "inner.md"))

	files, err := listMarkdownFiles(dir)
	if err != nil {
		t.Fatalf("listMarkdownFiles: %v", err)
	}
	want := []string{
		filepath.Join(dir, "UPPER.MD"),
		filepath.Join(dir, "a.md"),
		filepath.Join(dir, "b.md"),
	}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("listMarkdownFiles = %v, want %v", files, want)
	}
}

func TestListMarkdownFilesEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := listMarkdownFiles(dir)
	if err == nil || !strings.Contains(err.Error(), "no .md files in "+dir) {
		t.Errorf("listMarkdownFiles on empty dir: got %v, want 'no .md files in %s'", err, dir)
	}
}

func TestListMarkdownFilesNoMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "readme.txt"))
	if _, err := listMarkdownFiles(dir); err == nil || !strings.Contains(err.Error(), "no .md files") {
		t.Errorf("listMarkdownFiles without .md files: got %v, want 'no .md files' error", err)
	}
}

func TestListMarkdownFilesMissingDirectory(t *testing.T) {
	if _, err := listMarkdownFiles(filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Error("listMarkdownFiles on missing dir: got nil, want error")
	}
}

func TestBatchOutputDir(t *testing.T) {
	tests := []struct {
		name                       string
		flag, cfgDefault, inputDir string
		want                       string
	}{
		{"flag wins", "out", "cfgdir", "in", "out"},
		{"config default when no flag", "", "cfgdir", "in", "cfgdir"},
		{"input dir as last resort", "", "", "in", "in"},
	}
	for _, tt := range tests {
		if got := batchOutputDir(tt.flag, tt.cfgDefault, tt.inputDir); got != tt.want {
			t.Errorf("%s: batchOutputDir(%q, %q, %q) = %q, want %q",
				tt.name, tt.flag, tt.cfgDefault, tt.inputDir, got, tt.want)
		}
	}
}

func TestPdfBaseName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"docs/report.md", "report.pdf"},
		{"plain.md", "plain.pdf"},
		{"noext", "noext.pdf"},
		{"double.tar.md", "double.tar.pdf"},
	}
	for _, tt := range tests {
		if got := pdfBaseName(tt.in); got != tt.want {
			t.Errorf("pdfBaseName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestConvertBatchOutputNaming checks that every file is routed to
// <outDir>/<basename>.pdf and that an all-green batch returns nil.
func TestConvertBatchOutputNaming(t *testing.T) {
	files := []string{"in/a.md", "in/b.md"}
	var got [][2]string
	var stderr bytes.Buffer

	err := convertBatch(files, "out", func(in, out string) error {
		got = append(got, [2]string{in, out})
		return nil
	}, &stderr)

	if err != nil {
		t.Fatalf("convertBatch: %v", err)
	}
	want := [][2]string{
		{"in/a.md", filepath.Join("out", "a.pdf")},
		{"in/b.md", filepath.Join("out", "b.pdf")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("conversions = %v, want %v", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// TestConvertBatchContinuesOnError checks that one bad file among good ones
// does not stop the batch: all files are attempted, the failure is reported
// as a "file: error" line, and a summary error signals the non-zero exit.
func TestConvertBatchContinuesOnError(t *testing.T) {
	files := []string{"in/a.md", "in/bad.md", "in/c.md"}
	var attempted []string
	var stderr bytes.Buffer

	err := convertBatch(files, "out", func(in, out string) error {
		attempted = append(attempted, in)
		if strings.Contains(in, "bad") {
			return errors.New("boom")
		}
		return nil
	}, &stderr)

	if !reflect.DeepEqual(attempted, files) {
		t.Errorf("attempted = %v, want all of %v", attempted, files)
	}
	if err == nil || !strings.Contains(err.Error(), "1 of 3 files failed") {
		t.Errorf("err = %v, want '1 of 3 files failed'", err)
	}
	if want := "in/bad.md: boom\n"; stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

// TestConvertBatchAllFail: every failure is reported and counted.
func TestConvertBatchAllFail(t *testing.T) {
	files := []string{"x.md", "y.md"}
	var stderr bytes.Buffer

	err := convertBatch(files, "out", func(in, out string) error {
		return errors.New("nope")
	}, &stderr)

	if err == nil || !strings.Contains(err.Error(), "2 of 2 files failed") {
		t.Errorf("err = %v, want '2 of 2 files failed'", err)
	}
	for _, line := range []string{"x.md: nope", "y.md: nope"} {
		if !strings.Contains(stderr.String(), line) {
			t.Errorf("stderr %q missing %q", stderr.String(), line)
		}
	}
}
