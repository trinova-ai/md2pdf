package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
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

// TestApplyFrontmatterWatermark: watermark.text sets the text AND enables the
// watermark on a config that leaves it off; an absent or empty key must not
// enable it.
func TestApplyFrontmatterWatermark(t *testing.T) {
	t.Run("watermark.text enables and sets", func(t *testing.T) {
		var cfg Config
		applyFrontmatter(map[string]string{"watermark.text": "DRAFT"}, &cfg)
		if !cfg.Watermark.Enabled {
			t.Error("Watermark.Enabled = false, want true")
		}
		if cfg.Watermark.Text != "DRAFT" {
			t.Errorf("Watermark.Text = %q, want %q", cfg.Watermark.Text, "DRAFT")
		}
	})

	t.Run("absent key leaves watermark disabled", func(t *testing.T) {
		var cfg Config
		applyFrontmatter(map[string]string{"document.title": "T"}, &cfg)
		if cfg.Watermark.Enabled {
			t.Error("Watermark.Enabled = true, want false")
		}
		if cfg.Watermark.Text != "" {
			t.Errorf("Watermark.Text = %q, want empty", cfg.Watermark.Text)
		}
	})

	t.Run("empty value leaves watermark disabled", func(t *testing.T) {
		var cfg Config
		applyFrontmatter(map[string]string{"watermark.text": ""}, &cfg)
		if cfg.Watermark.Enabled {
			t.Error("Watermark.Enabled = true, want false")
		}
	})

	t.Run("overrides configured text", func(t *testing.T) {
		cfg := Config{Watermark: WatermarkConfig{Enabled: true, Text: "CONFIDENTIAL"}}
		applyFrontmatter(map[string]string{"watermark.text": "DRAFT"}, &cfg)
		if cfg.Watermark.Text != "DRAFT" {
			t.Errorf("Watermark.Text = %q, want %q", cfg.Watermark.Text, "DRAFT")
		}
		if !cfg.Watermark.Enabled {
			t.Error("Watermark.Enabled = false, want true")
		}
	})
}

// TestParseFrontmatterOverlongKnownValue: a known key whose value exceeds the
// cap is rejected, naming the key; a value exactly at the cap passes.
func TestParseFrontmatterOverlongKnownValue(t *testing.T) {
	long := strings.Repeat("x", maxFrontmatterValueLen+1)
	_, _, err := parseFrontmatter("document.title: " + long)
	if err == nil {
		t.Fatal("over-long document.title: got nil, want error")
	}
	for _, want := range []string{"document.title", "exceeds 500 characters"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, missing %q", err, want)
		}
	}

	atCap := strings.Repeat("x", maxFrontmatterValueLen)
	fm, _, err := parseFrontmatter("document.title: " + atCap)
	if err != nil {
		t.Fatalf("value at cap: %v", err)
	}
	if fm["document.title"] != atCap {
		t.Error("value at cap was not applied")
	}
}

// TestParseFrontmatterUnknownKey: unknown keys never error — they are ignored
// (not applied) and reported in the sorted unknown list for --verbose.
func TestParseFrontmatterUnknownKey(t *testing.T) {
	fm, unknown, err := parseFrontmatter("status: Draft\nowner: TriNova\ndocument.title: T")
	if err != nil {
		t.Fatalf("parseFrontmatter: %v", err)
	}
	if want := []string{"owner", "status"}; !reflect.DeepEqual(unknown, want) {
		t.Errorf("unknown = %v, want %v (sorted)", unknown, want)
	}
	if _, ok := fm["status"]; ok {
		t.Error("unknown key leaked into the applied map")
	}
	if fm["document.title"] != "T" {
		t.Errorf("document.title = %q, want %q", fm["document.title"], "T")
	}
}

// TestParseFrontmatterNonStringKnownScalar: an unquoted number for a known key
// must not be silently dropped (the old behavior) — it errors with a hint to
// quote the value.
func TestParseFrontmatterNonStringKnownScalar(t *testing.T) {
	_, _, err := parseFrontmatter("document.version: 2.5")
	if err == nil {
		t.Fatal("non-string document.version: got nil, want error")
	}
	for _, want := range []string{"document.version", "must be a string", `quote it: "2.5"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, missing %q", err, want)
		}
	}
}

// TestParseFrontmatterMermaidScale: the one numeric frontmatter key accepts
// bare YAML numbers and quoted strings, and rejects non-positive or
// non-numeric values.
func TestParseFrontmatterMermaidScale(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string // expected fm["mermaid.scale"]; "" = absent
		wantErr string // substring of the error; "" = no error
	}{
		{name: "bare float", raw: "mermaid.scale: 0.62", want: "0.62"},
		{name: "bare int", raw: "mermaid.scale: 2", want: "2"},
		{name: "quoted number", raw: `mermaid.scale: "0.75"`, want: "0.75"},
		{name: "null is absent", raw: "mermaid.scale:", want: ""},
		{name: "zero rejected", raw: "mermaid.scale: 0", wantErr: "positive number"},
		{name: "negative rejected", raw: "mermaid.scale: -1.5", wantErr: "positive number"},
		{name: "non-numeric rejected", raw: "mermaid.scale: big", wantErr: "positive number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, unknown, err := parseFrontmatter(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFrontmatter() error = %v, want nil", err)
			}
			if got := fm["mermaid.scale"]; got != tt.want {
				t.Errorf("fm[mermaid.scale] = %q, want %q", got, tt.want)
			}
			if len(unknown) != 0 {
				t.Errorf("unknown = %v, want none (mermaid.scale is a known key)", unknown)
			}
		})
	}
}

// TestApplyFrontmatterMermaidScale: the parsed value lands on cfg.Mermaid.Scale.
func TestApplyFrontmatterMermaidScale(t *testing.T) {
	cfg := &Config{}
	applyFrontmatter(map[string]string{"mermaid.scale": "0.62"}, cfg)
	if cfg.Mermaid.Scale != 0.62 {
		t.Errorf("Mermaid.Scale = %v, want 0.62", cfg.Mermaid.Scale)
	}

	cfg = &Config{Mermaid: MermaidConfig{Scale: 1.5}}
	applyFrontmatter(map[string]string{}, cfg)
	if cfg.Mermaid.Scale != 1.5 {
		t.Errorf("Mermaid.Scale = %v after empty frontmatter, want 1.5 preserved", cfg.Mermaid.Scale)
	}
}

// TestParseFrontmatterUnknownKeysExempt: unknown keys escape validation
// entirely — non-string scalars and values beyond the cap are fine, because
// they are never applied. Guards real documents carrying long private
// metadata (e.g. CrunchGate's ~600-char version-history).
func TestParseFrontmatterUnknownKeysExempt(t *testing.T) {
	raw := "weight: 2.5\nversion-history: \"" + strings.Repeat("h", maxFrontmatterValueLen+100) + "\""
	fm, unknown, err := parseFrontmatter(raw)
	if err != nil {
		t.Fatalf("unknown keys must be exempt from validation, got: %v", err)
	}
	if want := []string{"version-history", "weight"}; !reflect.DeepEqual(unknown, want) {
		t.Errorf("unknown = %v, want %v", unknown, want)
	}
	if len(fm) != 0 {
		t.Errorf("fm = %v, want empty", fm)
	}
}

// TestParseFrontmatterNullKnownKey: a bare `key:` (YAML null) is treated as an
// absent key, not a type error.
func TestParseFrontmatterNullKnownKey(t *testing.T) {
	fm, _, err := parseFrontmatter("document.title:")
	if err != nil {
		t.Fatalf("null known key: %v", err)
	}
	if _, ok := fm["document.title"]; ok {
		t.Error("null value must not land in the applied map")
	}
}

// TestExtractFrontmatterEndToEnd: body split, applied map, unknown list, and
// error propagation through the extraction wrapper.
func TestExtractFrontmatterEndToEnd(t *testing.T) {
	t.Run("valid frontmatter", func(t *testing.T) {
		body, fm, unknown, err := extractFrontmatter("---\ndocument.title: \"T\"\nowner: X\n---\n# body\n")
		if err != nil {
			t.Fatalf("extractFrontmatter: %v", err)
		}
		if body != "# body" {
			t.Errorf("body = %q, want %q", body, "# body")
		}
		if fm["document.title"] != "T" {
			t.Errorf("document.title = %q, want %q", fm["document.title"], "T")
		}
		if want := []string{"owner"}; !reflect.DeepEqual(unknown, want) {
			t.Errorf("unknown = %v, want %v", unknown, want)
		}
	})

	t.Run("no frontmatter passes through", func(t *testing.T) {
		content := "# just a doc\n"
		body, fm, unknown, err := extractFrontmatter(content)
		if err != nil {
			t.Fatalf("extractFrontmatter: %v", err)
		}
		if body != content {
			t.Errorf("body = %q, want unchanged input", body)
		}
		if len(fm) != 0 || len(unknown) != 0 {
			t.Errorf("fm = %v, unknown = %v, want both empty", fm, unknown)
		}
	})

	t.Run("validation error propagates", func(t *testing.T) {
		_, _, _, err := extractFrontmatter("---\ndocument.version: 2.5\n---\nbody\n")
		if err == nil || !strings.Contains(err.Error(), "document.version") {
			t.Errorf("err = %v, want document.version validation error", err)
		}
	})
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

func TestResolveVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.2.3", "v1.2.3"},
		{"v0.0.2", "v0.0.2"},
		{"v0.0.1-3-gabc1234", "v0.0.1-3-gabc1234"},
	}
	for _, c := range cases {
		if got := resolveVersion(c.in); got != c.want {
			t.Errorf("resolveVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// "dev" falls through to the embedded module version; a test binary has
	// none, so it stays "dev".
	if got := resolveVersion("dev"); got != "dev" {
		t.Errorf("resolveVersion(%q) = %q, want %q", "dev", got, "dev")
	}
}

// TestConvertReportEndToEnd drives the real CLI over the testdata pair:
// company-config.yaml (org defaults: cover, TOC, footer) + report.md
// (frontmatter metadata, DRAFT watermark, a mermaid diagram). It exercises
// the full pipeline — config, frontmatter overlay, mermaid transform,
// headless-Chrome render.
func TestConvertReportEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("full PDF render is slow; skipping in -short mode")
	}
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed; skipping end-to-end conversion test")
	}

	outPath := filepath.Join(t.TempDir(), "report.pdf")
	args := []string{"md2pdf", "-c", "testdata/company-config.yaml", "-o", outPath, "testdata/report.md"}
	if err := newApp().Run(context.Background(), args); err != nil {
		t.Fatalf("convert testdata/report.md: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Errorf("output does not start with %%PDF- (got %q)", data[:min(8, len(data))])
	}
	if len(data) < 10_000 {
		t.Errorf("output suspiciously small: %d bytes; cover, TOC, and a rendered diagram expected", len(data))
	}
}

// TestConvertFormalConfigOnly drives the config-only invocation path
// (`md2pdf testdata/formal.yaml`): input.file resolution, verbatim TOC
// (toc.numbered: false), duplex page breaks, the signature block, and a
// custom style loaded via assets.basePath. No mermaid — mmdc not required.
func TestConvertFormalConfigOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("full PDF render is slow; skipping in -short mode")
	}

	outPath := filepath.Join(t.TempDir(), "formal.pdf")
	args := []string{"md2pdf", "-o", outPath, "testdata/formal.yaml"}
	if err := newApp().Run(context.Background(), args); err != nil {
		t.Fatalf("config-only convert of testdata/formal.yaml: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Errorf("output does not start with %%PDF- (got %q)", data[:min(8, len(data))])
	}
	if len(data) < 10_000 {
		t.Errorf("output suspiciously small: %d bytes; cover, TOC, signature expected", len(data))
	}
}

// TestConvertDiagramsEndToEnd converts testdata/diagrams.md (one wide and
// one narrow mermaid diagram) with the walkthrough config, exercising the
// bare numeric mermaid.scale frontmatter key end to end.
func TestConvertDiagramsEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("full PDF render is slow; skipping in -short mode")
	}
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed; skipping end-to-end conversion test")
	}

	outPath := filepath.Join(t.TempDir(), "diagrams.pdf")
	args := []string{"md2pdf", "-c", "testdata/company-config.yaml", "-o", outPath, "testdata/diagrams.md"}
	if err := newApp().Run(context.Background(), args); err != nil {
		t.Fatalf("convert testdata/diagrams.md: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Errorf("output does not start with %%PDF- (got %q)", data[:min(8, len(data))])
	}
	if len(data) < 10_000 {
		t.Errorf("output suspiciously small: %d bytes; two rendered diagrams expected", len(data))
	}
}

// TestConvertBatchEndToEnd is the real-render counterpart to the
// fake-convert batch unit tests: a directory of two small markdown files
// through the actual CLI, each producing its own PDF in the -o directory.
func TestConvertBatchEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("full PDF render is slow; skipping in -short mode")
	}

	inDir := t.TempDir()
	outDir := t.TempDir()
	docs := map[string]string{
		"alpha.md": "# Alpha\n\nFirst of two batch documents.\n",
		"beta.md":  "# Beta\n\nSecond of two batch documents.\n",
	}
	for name, content := range docs {
		if err := os.WriteFile(filepath.Join(inDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	args := []string{"md2pdf", "-o", outDir, inDir}
	if err := newApp().Run(context.Background(), args); err != nil {
		t.Fatalf("batch convert: %v", err)
	}

	for _, name := range []string{"alpha.pdf", "beta.pdf"} {
		data, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Errorf("%s does not start with %%PDF-", name)
		}
	}
}
