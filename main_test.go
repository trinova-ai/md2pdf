package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

// TestApplyFrontmatterFooterText: footer.text overrides the footer text per
// document but, unlike watermark.text, must NOT enable the footer — the
// config decides whether a footer exists, the document only what it says.
func TestApplyFrontmatterFooterText(t *testing.T) {
	t.Run("overrides configured text", func(t *testing.T) {
		cfg := Config{Footer: FooterConfig{Enabled: true, Text: "TriNova — Draft for internal use"}}
		applyFrontmatter(map[string]string{"footer.text": "Final"}, &cfg)
		if cfg.Footer.Text != "Final" {
			t.Errorf("Footer.Text = %q, want %q", cfg.Footer.Text, "Final")
		}
		if !cfg.Footer.Enabled {
			t.Error("Footer.Enabled = false, want true (untouched)")
		}
	})

	t.Run("does not enable a disabled footer", func(t *testing.T) {
		var cfg Config
		applyFrontmatter(map[string]string{"footer.text": "Final"}, &cfg)
		if cfg.Footer.Enabled {
			t.Error("Footer.Enabled = true, want false — footer.text must not enable the footer")
		}
		if cfg.Footer.Text != "Final" {
			t.Errorf("Footer.Text = %q, want %q", cfg.Footer.Text, "Final")
		}
	})

	t.Run("empty value is ignored", func(t *testing.T) {
		cfg := Config{Footer: FooterConfig{Enabled: true, Text: "Kept"}}
		applyFrontmatter(map[string]string{"footer.text": ""}, &cfg)
		if cfg.Footer.Text != "Kept" {
			t.Errorf("Footer.Text = %q, want %q", cfg.Footer.Text, "Kept")
		}
	})
}

// TestApplyFrontmatterTOCTitle: toc.title overrides the TOC heading per
// document but, like footer.text, must NOT enable the TOC — the config
// decides whether a TOC exists, the document only what its heading says.
func TestApplyFrontmatterTOCTitle(t *testing.T) {
	t.Run("overrides configured title", func(t *testing.T) {
		cfg := Config{TOC: TOCConfig{Enabled: true, Title: "Contents"}}
		applyFrontmatter(map[string]string{"toc.title": "Inhoud"}, &cfg)
		if cfg.TOC.Title != "Inhoud" {
			t.Errorf("TOC.Title = %q, want %q", cfg.TOC.Title, "Inhoud")
		}
		if !cfg.TOC.Enabled {
			t.Error("TOC.Enabled = false, want true (untouched)")
		}
	})

	t.Run("does not enable a disabled TOC", func(t *testing.T) {
		cfg := Config{TOC: TOCConfig{Enabled: false, Title: "Contents"}}
		applyFrontmatter(map[string]string{"toc.title": "Inhoud"}, &cfg)
		if cfg.TOC.Enabled {
			t.Error("TOC.Enabled = true, want false — toc.title must not enable the TOC")
		}
		if cfg.TOC.Title != "Inhoud" {
			t.Errorf("TOC.Title = %q, want %q", cfg.TOC.Title, "Inhoud")
		}
		// buildInput builds the TOC only when enabled: the renamed-but-disabled
		// TOC must not surface in the input at all.
		if in := buildInput("# Body\n", "doc.md", "", &cfg); in.TOC != nil {
			t.Error("buildInput produced a TOC though toc.enabled is false")
		}
	})

	t.Run("empty value is ignored", func(t *testing.T) {
		cfg := Config{TOC: TOCConfig{Enabled: true, Title: "Kept"}}
		applyFrontmatter(map[string]string{"toc.title": ""}, &cfg)
		if cfg.TOC.Title != "Kept" {
			t.Errorf("TOC.Title = %q, want %q", cfg.TOC.Title, "Kept")
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

// TestFrontmatterValues: the migratable entries of a config are exactly the
// non-empty frontmatterTargets fields plus a positive mermaid.scale, values
// verbatim — "auto" is not resolved, the scale is formatted as a bare number
// string.
func TestFrontmatterValues(t *testing.T) {
	t.Run("non-empty eligible fields only", func(t *testing.T) {
		cfg := &Config{}
		cfg.Document.Title = "Trust Anchor Strategy"
		cfg.Document.Date = "auto"
		cfg.Author.Name = "René Post"
		cfg.Footer.Text = "TriNova — Draft"
		cfg.Mermaid.Scale = 0.62
		cfg.Style = "trinova" // config-level field, never a frontmatter key

		got := frontmatterValues(cfg)
		want := map[string]string{
			"document.title": "Trust Anchor Strategy",
			"document.date":  "auto",
			"author.name":    "René Post",
			"footer.text":    "TriNova — Draft",
			"mermaid.scale":  "0.62",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("frontmatterValues = %v, want %v", got, want)
		}
	})

	t.Run("zero config yields nothing", func(t *testing.T) {
		if got := frontmatterValues(&Config{}); len(got) != 0 {
			t.Errorf("frontmatterValues(zero) = %v, want empty", got)
		}
	})
}

// TestScaffoldFrontmatterValues pins the no-config scaffold to the full
// document.*/author.* subset of frontmatterTargets with empty values. The
// hard-coded want doubles as a drift guard: adding a key to
// frontmatterTargets fails here until the scaffold expectation is revisited.
func TestScaffoldFrontmatterValues(t *testing.T) {
	want := map[string]string{
		"document.title":        "",
		"document.subtitle":     "",
		"document.version":      "",
		"document.date":         "",
		"document.documentID":   "",
		"document.clientName":   "",
		"document.projectName":  "",
		"document.documentType": "",
		"document.description":  "",
		"author.name":           "",
		"author.title":          "",
		"author.organization":   "",
		"author.email":          "",
		"author.phone":          "",
		"author.address":        "",
		"author.department":     "",
	}
	got := scaffoldFrontmatterValues()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scaffoldFrontmatterValues = %v, want %v", got, want)
	}

	// The scaffold must stay a strict subset of the reader's key set.
	known := frontmatterTargets(&Config{})
	for key := range got {
		if _, ok := known[key]; !ok {
			t.Errorf("scaffold key %q is not in frontmatterTargets", key)
		}
	}
}

// TestMergeFrontmatter: the writer/merger core. Creation, partial merge with
// byte-for-byte preservation of existing lines, untouched unknown keys,
// value formatting per the parser's rules, and the error cases.
func TestMergeFrontmatter(t *testing.T) {
	t.Run("creates block when file has none", func(t *testing.T) {
		values := map[string]string{
			"document.title": "Trust Anchor Strategy",
			"document.date":  "auto",
			"author.name":    "René Post",
			"footer.text":    "TriNova — Draft",
			"mermaid.scale":  "0.62",
		}
		merged, added, err := mergeFrontmatter("# Title\n\nBody text.\n", values)
		if err != nil {
			t.Fatalf("mergeFrontmatter: %v", err)
		}
		want := "---\n" +
			"document.date: \"auto\"\n" +
			"document.title: \"Trust Anchor Strategy\"\n" +
			"author.name: \"René Post\"\n" +
			"footer.text: \"TriNova — Draft\"\n" +
			"mermaid.scale: 0.62\n" +
			"---\n\n# Title\n\nBody text.\n"
		if merged != want {
			t.Errorf("merged = %q, want %q", merged, want)
		}
		wantAdded := []string{"document.date", "document.title", "author.name", "footer.text", "mermaid.scale"}
		if !reflect.DeepEqual(added, wantAdded) {
			t.Errorf("added = %v, want %v", added, wantAdded)
		}

		// Round-trip through the reader: every written value parses back
		// verbatim — quoting preserved "auto" as a literal string and the
		// bare scale as the known numeric key.
		_, fm, unknown, err := extractFrontmatter(merged)
		if err != nil {
			t.Fatalf("extractFrontmatter(merged): %v", err)
		}
		if !reflect.DeepEqual(fm, values) {
			t.Errorf("round-trip fm = %v, want %v", fm, values)
		}
		if len(unknown) != 0 {
			t.Errorf("round-trip unknown = %v, want none", unknown)
		}
	})

	t.Run("partial frontmatter keeps existing keys verbatim", func(t *testing.T) {
		content := "---\n" +
			"document.title:     'Spaced  &  quoted'   # keep me\n" +
			"document.date:\n" +
			"---\n# Body\n"
		values := map[string]string{
			"document.title": "New Title", // present: must not be touched
			"document.date":  "auto",      // present as bare scaffold: must not be re-added
			"author.name":    "A",         // missing: appended
		}
		merged, added, err := mergeFrontmatter(content, values)
		if err != nil {
			t.Fatalf("mergeFrontmatter: %v", err)
		}
		want := "---\n" +
			"document.title:     'Spaced  &  quoted'   # keep me\n" +
			"document.date:\n" +
			"author.name: \"A\"\n" +
			"---\n# Body\n"
		if merged != want {
			t.Errorf("merged = %q, want %q", merged, want)
		}
		if wantAdded := []string{"author.name"}; !reflect.DeepEqual(added, wantAdded) {
			t.Errorf("added = %v, want %v", added, wantAdded)
		}
	})

	t.Run("unknown private keys untouched", func(t *testing.T) {
		history := "version-history: \"" + strings.Repeat("h", maxFrontmatterValueLen+100) + "\""
		content := "---\nstatus: Draft\nweight: 2.5\n" + history + "\n---\nbody\n"
		merged, added, err := mergeFrontmatter(content, map[string]string{"document.title": "T"})
		if err != nil {
			t.Fatalf("mergeFrontmatter: %v", err)
		}
		want := "---\nstatus: Draft\nweight: 2.5\n" + history + "\ndocument.title: \"T\"\n---\nbody\n"
		if merged != want {
			t.Errorf("merged = %q, want %q", merged, want)
		}
		if wantAdded := []string{"document.title"}; !reflect.DeepEqual(added, wantAdded) {
			t.Errorf("added = %v, want %v", added, wantAdded)
		}
	})

	t.Run("nothing missing returns content unchanged", func(t *testing.T) {
		content := "---\ndocument.title: \"T\"\n---\nbody\n"
		merged, added, err := mergeFrontmatter(content, map[string]string{"document.title": "Other"})
		if err != nil {
			t.Fatalf("mergeFrontmatter: %v", err)
		}
		if merged != content {
			t.Errorf("merged = %q, want unchanged input", merged)
		}
		if len(added) != 0 {
			t.Errorf("added = %v, want none", added)
		}
	})

	t.Run("scaffold into empty file", func(t *testing.T) {
		merged, added, err := mergeFrontmatter("", scaffoldFrontmatterValues())
		if err != nil {
			t.Fatalf("mergeFrontmatter: %v", err)
		}
		if len(added) != len(scaffoldFrontmatterValues()) {
			t.Errorf("added %d keys, want %d", len(added), len(scaffoldFrontmatterValues()))
		}
		if !strings.HasPrefix(merged, "---\n") || !strings.HasSuffix(merged, "---\n") {
			t.Errorf("merged = %q, want a bare frontmatter block with no trailing blank line", merged)
		}
		if !strings.Contains(merged, "document.title: \"\"\n") {
			t.Errorf("merged = %q, missing empty-value scaffold line for document.title", merged)
		}
		// Empty scaffold values must parse cleanly and stay render-neutral.
		_, fm, unknown, err := extractFrontmatter(merged)
		if err != nil {
			t.Fatalf("extractFrontmatter(merged): %v", err)
		}
		if len(unknown) != 0 {
			t.Errorf("unknown = %v, want none", unknown)
		}
		var cfg Config
		applyFrontmatter(fm, &cfg)
		if !reflect.DeepEqual(cfg, Config{}) {
			t.Errorf("scaffold applied to zero config = %+v, want untouched zero config", cfg)
		}
	})

	t.Run("crlf block gets crlf lines", func(t *testing.T) {
		content := "---\r\ndocument.title: \"T\"\r\n---\r\nbody\r\n"
		merged, _, err := mergeFrontmatter(content, map[string]string{"author.name": "A"})
		if err != nil {
			t.Fatalf("mergeFrontmatter: %v", err)
		}
		want := "---\r\ndocument.title: \"T\"\r\nauthor.name: \"A\"\r\n---\r\nbody\r\n"
		if merged != want {
			t.Errorf("merged = %q, want %q", merged, want)
		}
	})

	t.Run("unclosed frontmatter errors", func(t *testing.T) {
		_, _, err := mergeFrontmatter("---\ndocument.title: \"T\"\nno closing delimiter\n", map[string]string{"author.name": "A"})
		if err == nil || !strings.Contains(err.Error(), "closing") {
			t.Errorf("err = %v, want missing-closing-delimiter error", err)
		}
	})

	t.Run("invalid yaml in existing block errors", func(t *testing.T) {
		_, _, err := mergeFrontmatter("---\n{\n---\nbody\n", map[string]string{"author.name": "A"})
		if err == nil || !strings.Contains(err.Error(), "frontmatter") {
			t.Errorf("err = %v, want frontmatter parse error", err)
		}
	})
}

// TestMigratedConfigKeys pins the strip policy: a config key is safe to
// remove exactly when the merged document carries it WITH a value — whether
// the merge just added it or the document already had its own (possibly
// different) value, frontmatter outranks the config either way. An empty
// scaffold (`key:` or `key: ""`) overrides nothing, so its config key stays.
func TestMigratedConfigKeys(t *testing.T) {
	values := map[string]string{
		"document.title": "Config Title", // doc carries its own different value
		"document.date":  "auto",         // doc carries it only as an empty scaffold
		"author.name":    "Sarah Chen",   // just added by the merge
	}
	merged := "---\n" +
		"document.title: \"Doc Title\"\n" +
		"document.date:\n" +
		"author.name: \"Sarah Chen\"\n" +
		"---\nbody\n"

	got, err := migratedConfigKeys(merged, values)
	if err != nil {
		t.Fatalf("migratedConfigKeys: %v", err)
	}
	want := []string{"document.title", "author.name"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("migratedConfigKeys = %v, want %v", got, want)
	}
}

// TestStripConfigKeys: the yaml.v3 node rewrite behind `frontmatter -c` —
// migrated keys removed, comments and ordering of untouched settings
// preserved, emptied sections dropped, and a no-op returning the input
// byte-for-byte.
func TestStripConfigKeys(t *testing.T) {
	t.Run("removes keys and preserves comments and order", func(t *testing.T) {
		in := "# banner comment\n\n" +
			"style: \"corporate\"\n" +
			"footer:\n" +
			"  enabled: true\n" +
			"  text: \"Draft\"\n" +
			"  position: \"right\"   # keep me\n" +
			"watermark:\n" +
			"  enabled: true\n" +
			"  text: \"DRAFT\"\n"
		out, removed, err := stripConfigKeys([]byte(in), []string{"footer.text", "watermark.text"})
		if err != nil {
			t.Fatalf("stripConfigKeys: %v", err)
		}
		if want := []string{"footer.text", "watermark.text"}; !reflect.DeepEqual(removed, want) {
			t.Errorf("removed = %v, want %v", removed, want)
		}
		got := string(out)
		for _, want := range []string{"# banner comment", "style: \"corporate\"", "enabled: true", "# keep me"} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing %q:\n%s", want, got)
			}
		}
		for _, gone := range []string{"Draft", "DRAFT", "text:"} {
			if strings.Contains(got, gone) {
				t.Errorf("output still contains %q:\n%s", gone, got)
			}
		}
		if fi, wi := strings.Index(got, "footer:"), strings.Index(got, "watermark:"); fi < 0 || wi < 0 || fi > wi {
			t.Errorf("section order not preserved (footer at %d, watermark at %d):\n%s", fi, wi, got)
		}
	})

	t.Run("drops emptied document and author sections", func(t *testing.T) {
		in := "document:\n  title: \"T\"\nauthor:\n  name: \"A\"\n  email: \"a@b.c\"\nstyle: \"formal\"\n"
		out, removed, err := stripConfigKeys([]byte(in), []string{"document.title", "author.name", "author.email"})
		if err != nil {
			t.Fatalf("stripConfigKeys: %v", err)
		}
		if len(removed) != 3 {
			t.Errorf("removed = %v, want 3 keys", removed)
		}
		got := string(out)
		for _, gone := range []string{"document:", "author:"} {
			if strings.Contains(got, gone) {
				t.Errorf("emptied section %q not dropped:\n%s", gone, got)
			}
		}
		if !strings.Contains(got, "style: \"formal\"") {
			t.Errorf("untouched style lost:\n%s", got)
		}
	})

	t.Run("head comment on dropped first section moves to next key", func(t *testing.T) {
		// No blank line: this comment attaches to the `author` key node, not
		// the document node, and would vanish with the section without the
		// transfer in dropMapEntry.
		in := "# shared org defaults\nauthor:\n  name: \"A\"\nstyle: \"formal\"\n"
		out, _, err := stripConfigKeys([]byte(in), []string{"author.name"})
		if err != nil {
			t.Fatalf("stripConfigKeys: %v", err)
		}
		got := string(out)
		if !strings.Contains(got, "# shared org defaults") {
			t.Errorf("banner comment lost:\n%s", got)
		}
		if strings.Contains(got, "author:") {
			t.Errorf("emptied author section not dropped:\n%s", got)
		}
	})

	t.Run("nothing removed returns input byte-for-byte", func(t *testing.T) {
		in := "style:   \"formal\"    # odd spacing survives because no rewrite happens\n"
		out, removed, err := stripConfigKeys([]byte(in), []string{"footer.text", "document.title"})
		if err != nil {
			t.Fatalf("stripConfigKeys: %v", err)
		}
		if len(removed) != 0 {
			t.Errorf("removed = %v, want none", removed)
		}
		if string(out) != in {
			t.Errorf("out = %q, want unchanged input %q", out, in)
		}
	})

	t.Run("empty config passes through", func(t *testing.T) {
		out, removed, err := stripConfigKeys(nil, []string{"document.title"})
		if err != nil {
			t.Fatalf("stripConfigKeys: %v", err)
		}
		if len(out) != 0 || len(removed) != 0 {
			t.Errorf("out = %q, removed = %v, want both empty", out, removed)
		}
	})

	t.Run("invalid yaml errors", func(t *testing.T) {
		_, _, err := stripConfigKeys([]byte("{"), []string{"document.title"})
		if err == nil || !strings.Contains(err.Error(), "parsing config") {
			t.Errorf("err = %v, want parsing error", err)
		}
	})
}

// copyTestFile copies src into dir under the same basename and returns the
// new path.
func copyTestFile(t *testing.T, dir, src string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	dst := filepath.Join(dir, filepath.Base(src))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
	return dst
}

// effectiveConfig computes the config a render would use for the pair:
// config file loaded, then the document's frontmatter applied on top —
// exactly the overlay convertFile performs before building the input.
func effectiveConfig(t *testing.T, configPath, mdPath string) Config {
	t.Helper()
	var cfg Config
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read markdown: %v", err)
	}
	_, fm, _, err := extractFrontmatter(string(md))
	if err != nil {
		t.Fatalf("extract frontmatter: %v", err)
	}
	applyFrontmatter(fm, &cfg)
	return cfg
}

// TestRunFrontmatterCommand drives the subcommand through the real CLI on
// temp copies of the testdata pair: migration, dry-run isolation, and the
// missing-argument error.
func TestRunFrontmatterCommand(t *testing.T) {
	t.Run("migrates the testdata pair", func(t *testing.T) {
		dir := t.TempDir()
		md := copyTestFile(t, dir, "testdata/report.md")
		cfgPath := copyTestFile(t, dir, "testdata/company-config.yaml")

		args := []string{"md2pdf", "frontmatter", "-c", cfgPath, md}
		if err := newApp().Run(context.Background(), args); err != nil {
			t.Fatalf("frontmatter subcommand: %v", err)
		}

		// The document gained the config's author.* keys; its own keys and
		// body are untouched.
		mdOut, err := os.ReadFile(md)
		if err != nil {
			t.Fatalf("read migrated markdown: %v", err)
		}
		for _, want := range []string{
			"author.name: \"Sarah Chen\"",
			"document.title: \"API Gateway Security Review\"", // pre-existing, verbatim
			"watermark.text: \"DRAFT\"",
		} {
			if !strings.Contains(string(mdOut), want) {
				t.Errorf("migrated markdown missing %q", want)
			}
		}

		// The config lost the whole author section but kept everything else,
		// including its banner comment.
		cfgOut, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("read rewritten config: %v", err)
		}
		// The banner comment legitimately mentions the word author; only the
		// section key itself must be gone.
		if strings.Contains(string(cfgOut), "author:") {
			t.Errorf("rewritten config still has an author: section:\n%s", cfgOut)
		}
		for _, want := range []string{"org-wide defaults", "style: \"corporate\"", "footer:", "showPageNumber: true"} {
			if !strings.Contains(string(cfgOut), want) {
				t.Errorf("rewritten config missing %q:\n%s", want, cfgOut)
			}
		}
		var cfg Config
		if err := yaml.Unmarshal(cfgOut, &cfg); err != nil {
			t.Fatalf("rewritten config does not parse: %v", err)
		}
		if cfg.Author != (AuthorConfig{}) || cfg.Document != (DocumentConfig{}) {
			t.Errorf("rewritten config still carries metadata: author %+v, document %+v", cfg.Author, cfg.Document)
		}
	})

	t.Run("dry-run prints both files and touches nothing", func(t *testing.T) {
		dir := t.TempDir()
		md := copyTestFile(t, dir, "testdata/report.md")
		cfgPath := copyTestFile(t, dir, "testdata/company-config.yaml")
		mdBefore, _ := os.ReadFile(md)
		cfgBefore, _ := os.ReadFile(cfgPath)

		// The subcommand prints to os.Stdout (matching the converter's
		// "Created …" convention), so capture it via a pipe.
		orig := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		os.Stdout = w
		runErr := newApp().Run(context.Background(), []string{"md2pdf", "frontmatter", "--dry-run", "-c", cfgPath, md})
		w.Close()
		os.Stdout = orig
		out, _ := io.ReadAll(r)
		if runErr != nil {
			t.Fatalf("frontmatter --dry-run: %v", runErr)
		}

		for _, want := range []string{
			"==> " + md + " <==",
			"==> " + cfgPath + " <==",
			"author.name: \"Sarah Chen\"", // the would-be migrated document
		} {
			if !strings.Contains(string(out), want) {
				t.Errorf("dry-run output missing %q", want)
			}
		}
		if mdAfter, _ := os.ReadFile(md); !bytes.Equal(mdBefore, mdAfter) {
			t.Error("dry-run modified the markdown file")
		}
		if cfgAfter, _ := os.ReadFile(cfgPath); !bytes.Equal(cfgBefore, cfgAfter) {
			t.Error("dry-run modified the config file")
		}
	})

	t.Run("missing input argument errors", func(t *testing.T) {
		err := newApp().Run(context.Background(), []string{"md2pdf", "frontmatter"})
		if err == nil || !strings.Contains(err.Error(), "input.md") {
			t.Errorf("err = %v, want missing <input.md> error", err)
		}
	})
}

// TestFrontmatterMigratesTOCTitle drives the subcommand on a style-only config:
// toc.title moves into the document and is stripped from the config, while
// toc.enabled — the gate that decides whether a TOC exists — is not a
// frontmatter key and stays put, leaving the config still able to switch the
// TOC on for every document it styles.
func TestFrontmatterMigratesTOCTitle(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(mdPath, []byte("# Title\n\nBody.\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	cfgPath := filepath.Join(dir, "style.yaml")
	cfgSrc := "style: \"corporate\"\ntoc:\n  enabled: true\n  title: \"Contents\"\n  maxDepth: 3\n"
	if err := os.WriteFile(cfgPath, []byte(cfgSrc), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := newApp().Run(context.Background(), []string{"md2pdf", "frontmatter", "-c", cfgPath, mdPath}); err != nil {
		t.Fatalf("frontmatter subcommand: %v", err)
	}

	mdOut, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read migrated markdown: %v", err)
	}
	if !strings.Contains(string(mdOut), "toc.title: \"Contents\"") {
		t.Errorf("migrated markdown missing toc.title:\n%s", mdOut)
	}

	cfgOut, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read rewritten config: %v", err)
	}
	if strings.Contains(string(cfgOut), "Contents") {
		t.Errorf("rewritten config still carries toc.title:\n%s", cfgOut)
	}
	var parsed Config
	if err := yaml.Unmarshal(cfgOut, &parsed); err != nil {
		t.Fatalf("rewritten config does not parse: %v", err)
	}
	if parsed.TOC.Title != "" {
		t.Errorf("TOC.Title = %q, want stripped", parsed.TOC.Title)
	}
	if !parsed.TOC.Enabled {
		t.Errorf("TOC.Enabled = false — the gate must survive migration:\n%s", cfgOut)
	}
}

// TestFrontmatterMigrationRenderNeutral is the end-to-end render-neutrality
// check for G7.2: convert the testdata pair before and after `md2pdf
// frontmatter` (on temp copies) and require the same cover/footer metadata.
// Extracting text from the PDFs would be overkill (and byte comparison is
// impossible — PDFs embed timestamps), so the metadata assertion runs at the
// exact seam the renderer consumes: the effective Config after config load +
// frontmatter overlay must be identical before and after migration, and both
// real renders must succeed and produce valid PDFs.
func TestFrontmatterMigrationRenderNeutral(t *testing.T) {
	if testing.Short() {
		t.Skip("full PDF render is slow; skipping in -short mode")
	}
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed; skipping end-to-end conversion test")
	}

	dir := t.TempDir()
	md := copyTestFile(t, dir, "testdata/report.md")
	cfgPath := copyTestFile(t, dir, "testdata/company-config.yaml")
	copyTestFile(t, dir, "testdata/trinova-mark.svg") // cover.logo, config-relative

	render := func(name string) []byte {
		t.Helper()
		out := filepath.Join(dir, name)
		args := []string{"md2pdf", "-c", cfgPath, "-o", out, md}
		if err := newApp().Run(context.Background(), args); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Errorf("%s does not start with %%PDF-", name)
		}
		return data
	}

	before := render("before.pdf")
	wantCfg := effectiveConfig(t, cfgPath, md)

	args := []string{"md2pdf", "frontmatter", "-c", cfgPath, md}
	if err := newApp().Run(context.Background(), args); err != nil {
		t.Fatalf("frontmatter migration: %v", err)
	}

	// The migrated yaml must carry no document.*/author.* values at all.
	var migrated Config
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	if err := yaml.Unmarshal(cfgData, &migrated); err != nil {
		t.Fatalf("parse migrated config: %v", err)
	}
	if migrated.Document != (DocumentConfig{}) || migrated.Author != (AuthorConfig{}) {
		t.Errorf("migrated config still carries metadata: document %+v, author %+v", migrated.Document, migrated.Author)
	}

	gotCfg := effectiveConfig(t, cfgPath, md)
	if !reflect.DeepEqual(wantCfg, gotCfg) {
		t.Errorf("effective config changed by migration:\nbefore: %+v\nafter:  %+v", wantCfg, gotCfg)
	}

	after := render("after.pdf")
	if len(before) < 10_000 || len(after) < 10_000 {
		t.Errorf("suspiciously small PDFs: before %d bytes, after %d bytes", len(before), len(after))
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
