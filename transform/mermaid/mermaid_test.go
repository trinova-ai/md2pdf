package mermaid

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubTransformer returns a Transformer whose render func only records the
// (input, output) pairs it was called with, so fence detection, numbering,
// and replacement can be tested without mmdc installed.
func stubTransformer(t *testing.T) (*Transformer, *[][2]string) {
	t.Helper()
	var calls [][2]string
	tr := NewTransformer()
	tr.render = func(input, output string) error {
		calls = append(calls, [2]string{input, output})
		return nil
	}
	return tr, &calls
}

func TestName(t *testing.T) {
	if got := NewTransformer().Name(); got != "mermaid" {
		t.Errorf("Name() = %q, want %q", got, "mermaid")
	}
}

func TestTransformZeroBlocksPassesThrough(t *testing.T) {
	const content = "# Title\n\nPlain text with `inline code` and no fences.\n"
	tr, calls := stubTransformer(t)

	got, err := tr.Transform(content, t.TempDir(), ".")
	if err != nil {
		t.Fatalf("Transform() error = %v, want nil", err)
	}
	if got != content {
		t.Errorf("Transform() = %q, want input unchanged %q", got, content)
	}
	if len(*calls) != 0 {
		t.Errorf("render called %d times, want 0", len(*calls))
	}
}

func TestTransformLeavesGoFenceUntouched(t *testing.T) {
	const content = "Before\n\n```go\nfunc main() {}\n```\n\nAfter\n"
	tr, calls := stubTransformer(t)

	got, err := tr.Transform(content, t.TempDir(), ".")
	if err != nil {
		t.Fatalf("Transform() error = %v, want nil", err)
	}
	if got != content {
		t.Errorf("Transform() = %q, want input unchanged %q", got, content)
	}
	if len(*calls) != 0 {
		t.Errorf("render called %d times, want 0", len(*calls))
	}
}

func TestTransformIgnoresMermaidFenceInsideOtherFence(t *testing.T) {
	const content = "````markdown\n```mermaid\ngraph TD\n```\n````\n"
	tr, calls := stubTransformer(t)

	got, err := tr.Transform(content, t.TempDir(), ".")
	if err != nil {
		t.Fatalf("Transform() error = %v, want nil", err)
	}
	if len(*calls) != 0 {
		t.Errorf("render called %d times, want 0", len(*calls))
	}
	if !strings.Contains(got, "```mermaid") {
		t.Errorf("Transform() = %q, want nested mermaid fence preserved", got)
	}
}

func TestTransformIgnoresIndentedFence(t *testing.T) {
	const content = "Example:\n\n  ```mermaid\n  graph TD\n  ```\n"
	tr, calls := stubTransformer(t)

	got, err := tr.Transform(content, t.TempDir(), ".")
	if err != nil {
		t.Fatalf("Transform() error = %v, want nil", err)
	}
	if got != content {
		t.Errorf("Transform() = %q, want input unchanged %q", got, content)
	}
	if len(*calls) != 0 {
		t.Errorf("render called %d times, want 0", len(*calls))
	}
}

func TestTransformOneBlock(t *testing.T) {
	workDir := t.TempDir()
	content := "# Doc\n\n```mermaid\ngraph TD\n  A --> B\n```\n\nAfter.\n"
	tr, calls := stubTransformer(t)

	got, err := tr.Transform(content, workDir, ".")
	if err != nil {
		t.Fatalf("Transform() error = %v, want nil", err)
	}
	svg := filepath.Join(workDir, "mermaid-1.svg")
	want := fmt.Sprintf("# Doc\n\n![diagram](%s)\n\nAfter.\n", svg)
	if got != want {
		t.Errorf("Transform() = %q, want %q", got, want)
	}
	if len(*calls) != 1 {
		t.Fatalf("render called %d times, want 1", len(*calls))
	}
	if (*calls)[0][1] != svg {
		t.Errorf("render output = %q, want %q", (*calls)[0][1], svg)
	}
	source, err := os.ReadFile((*calls)[0][0])
	if err != nil {
		t.Fatalf("read diagram source: %v", err)
	}
	if want := "graph TD\n  A --> B\n"; string(source) != want {
		t.Errorf("diagram source = %q, want %q", source, want)
	}
}

func TestTransformThreeBlocksSequentialNames(t *testing.T) {
	workDir := t.TempDir()
	content := "```mermaid\ngraph TD\n```\ntext\n```mermaid\npie\n```\n" +
		"```go\nfmt.Println()\n```\n```mermaid\nsequenceDiagram\n```\n"
	tr, calls := stubTransformer(t)

	got, err := tr.Transform(content, workDir, ".")
	if err != nil {
		t.Fatalf("Transform() error = %v, want nil", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("render called %d times, want 3", len(*calls))
	}
	for i, call := range *calls {
		wantIn := filepath.Join(workDir, fmt.Sprintf("mermaid-%d.mmd", i+1))
		wantOut := filepath.Join(workDir, fmt.Sprintf("mermaid-%d.svg", i+1))
		if call[0] != wantIn || call[1] != wantOut {
			t.Errorf("render call %d = (%q, %q), want (%q, %q)",
				i+1, call[0], call[1], wantIn, wantOut)
		}
	}
	for n := 1; n <= 3; n++ {
		ref := fmt.Sprintf("![diagram](%s)", filepath.Join(workDir, fmt.Sprintf("mermaid-%d.svg", n)))
		if !strings.Contains(got, ref) {
			t.Errorf("Transform() missing %q in %q", ref, got)
		}
	}
	if !strings.Contains(got, "```go\nfmt.Println()\n```") {
		t.Errorf("Transform() = %q, want go fence preserved", got)
	}
	if strings.Contains(got, "```mermaid") {
		t.Errorf("Transform() = %q, want all mermaid fences replaced", got)
	}
}

func TestTransformUnterminatedFenceErrors(t *testing.T) {
	const content = "text\n```mermaid\ngraph TD\n"
	tr, _ := stubTransformer(t)

	_, err := tr.Transform(content, t.TempDir(), ".")
	if err == nil {
		t.Fatal("Transform() error = nil, want unterminated-fence error")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("Transform() error = %q, want it to mention the unterminated fence", err)
	}
}

func TestTransformMissingMMDCError(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: mmdc cannot be found

	const content = "```mermaid\ngraph TD\n```\n"
	_, err := NewTransformer().Transform(content, t.TempDir(), ".")
	if err == nil {
		t.Fatal("Transform() error = nil, want missing-mmdc error")
	}
	for _, want := range []string{"mermaid CLI (mmdc) not found", "npm install -g @mermaid-js/mermaid-cli"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Transform() error = %q, want it to contain %q", err, want)
		}
	}
}

func TestTransformRendersWithRealMMDC(t *testing.T) {
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed; skipping real render test")
	}
	workDir := t.TempDir()
	const content = "```mermaid\ngraph TD\n  A --> B\n```\n"

	got, err := NewTransformer().Transform(content, workDir, ".")
	if err != nil {
		t.Fatalf("Transform() error = %v, want nil", err)
	}
	svg := filepath.Join(workDir, "mermaid-1.svg")
	if want := fmt.Sprintf("![diagram](%s)\n", svg); got != want {
		t.Errorf("Transform() = %q, want %q", got, want)
	}
	data, err := os.ReadFile(svg)
	if err != nil {
		t.Fatalf("read rendered SVG: %v", err)
	}
	if !strings.Contains(string(data), "<svg") {
		t.Errorf("rendered file does not look like an SVG (%d bytes)", len(data))
	}
	if strings.Contains(string(data), `width="100%"`) {
		t.Errorf("rendered SVG still has width=\"100%%\"; intrinsic size was not pinned")
	}
}

func TestSetExplicitSize(t *testing.T) {
	t.Parallel()

	mmdcRoot := `<svg id="my-svg" width="100%" xmlns="http://www.w3.org/2000/svg" class="flowchart" style="max-width: 204.5px; background-color: white;" viewBox="0 0 204.5 70"><g>body</g></svg>`

	tests := []struct {
		name            string
		svg             string
		scale           float64
		wantContains    []string
		wantNotContains []string
		wantErr         bool
	}{
		{
			name:  "pins width and height from viewBox at scale 1",
			svg:   mmdcRoot,
			scale: 1,
			wantContains: []string{
				`<svg width="204.5px" height="70px"`,
				`viewBox="0 0 204.5 70"`,
			},
			wantNotContains: []string{`width="100%"`, `max-width:`},
		},
		{
			name:  "scale multiplies dimensions",
			svg:   mmdcRoot,
			scale: 0.5,
			wantContains: []string{
				`width="102.25px"`,
				`height="35px"`,
			},
		},
		{
			name:         "no viewBox returns svg unchanged",
			svg:          `<svg width="100%"><g/></svg>`,
			scale:        1,
			wantContains: []string{`<svg width="100%">`},
		},
		{
			name:  "only root tag is rewritten",
			svg:   `<svg width="100%" viewBox="0 0 10 20"><svg width="100%" viewBox="0 0 1 1"/></svg>`,
			scale: 1,
			wantContains: []string{
				`<svg width="10px" height="20px" viewBox="0 0 10 20">`,
				`<svg width="100%" viewBox="0 0 1 1"/>`,
			},
		},
		{
			name:    "not an svg errors",
			svg:     `<html>nope</html>`,
			scale:   1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := setExplicitSize([]byte(tt.svg), tt.scale)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("setExplicitSize() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("setExplicitSize() error = %v, want nil", err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(string(got), want) {
					t.Errorf("setExplicitSize() missing %q\nGot:\n%s", want, got)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(got), notWant) {
					t.Errorf("setExplicitSize() should not contain %q\nGot:\n%s", notWant, got)
				}
			}
		})
	}
}
