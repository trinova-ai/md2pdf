// Package mermaid renders ```mermaid fenced code blocks to SVG images via
// the Mermaid CLI (mmdc), replacing each fence with an image reference so
// documents show diagrams instead of code listings.
package mermaid

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// installCmd is the command that installs the Mermaid CLI.
const installCmd = "npm install -g @mermaid-js/mermaid-cli"

// Transformer implements transform.Transformer. It finds ```mermaid fenced
// blocks, renders each one to workDir/mermaid-<n>.svg with mmdc, and replaces
// the fence with `![diagram](<absolute path to the SVG>)`.
//
// Fence matching is deliberately narrow: a block opens with a column-0 line
// whose trailing-whitespace-trimmed form is "```mermaid" and closes at the
// next column-0 line that trims to "```". Fences for other languages, inline
// code, mermaid fences nested inside other column-0 fences, and indented
// fences all pass through untouched.
type Transformer struct {
	// render turns one diagram source file (input) into an SVG (output).
	// It defaults to invoking mmdc; tests may replace it to exercise fence
	// detection and replacement without the Mermaid CLI installed.
	render func(input, output string) error
}

// NewTransformer returns a Transformer that renders diagrams with mmdc.
func NewTransformer() *Transformer {
	return &Transformer{render: renderWithMMDC}
}

// Name returns the transformer's identifier for logging and errors.
func (t *Transformer) Name() string { return "mermaid" }

// Transform rewrites content, replacing every ```mermaid fence with an image
// reference to an SVG rendered into workDir. Diagrams are numbered in order
// of appearance: mermaid-1.svg, mermaid-2.svg, … When the content has no
// mermaid fences it is returned unchanged. sourceDir is unused; generated
// SVGs are referenced by absolute path.
func (t *Transformer) Transform(content, workDir, sourceDir string) (string, error) {
	if !strings.Contains(content, "```mermaid") {
		return content, nil
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("resolve workDir: %w", err)
	}

	const (
		text      = iota // outside any fence
		inMermaid        // inside a ```mermaid fence
		inOther          // inside some other column-0 fence
	)
	lines := strings.Split(content, "\n")
	var (
		out       []string
		body      []string
		state     = text
		openLine  int // 1-based line number of the current mermaid opener
		diagrams  int
		unchanged = true
	)
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		switch state {
		case text:
			switch {
			case trimmed == "```mermaid":
				state = inMermaid
				openLine = i + 1
				body = body[:0]
			case strings.HasPrefix(line, "```"):
				state = inOther
				out = append(out, line)
			default:
				out = append(out, line)
			}
		case inMermaid:
			if trimmed == "```" {
				diagrams++
				svg, err := t.renderDiagram(absWork, diagrams, body)
				if err != nil {
					return "", err
				}
				out = append(out, fmt.Sprintf("![diagram](%s)", svg))
				unchanged = false
				state = text
			} else {
				body = append(body, line)
			}
		case inOther:
			out = append(out, line)
			if trimmed == "```" {
				state = text
			}
		}
	}
	if state == inMermaid {
		return "", fmt.Errorf("unterminated ```mermaid fence opened at line %d", openLine)
	}
	if unchanged {
		return content, nil
	}
	return strings.Join(out, "\n"), nil
}

// renderDiagram writes the diagram source to workDir/mermaid-<n>.mmd, renders
// it to workDir/mermaid-<n>.svg, and returns the SVG's absolute path.
func (t *Transformer) renderDiagram(workDir string, n int, body []string) (string, error) {
	input := filepath.Join(workDir, fmt.Sprintf("mermaid-%d.mmd", n))
	output := filepath.Join(workDir, fmt.Sprintf("mermaid-%d.svg", n))
	source := strings.Join(body, "\n") + "\n"
	if err := os.WriteFile(input, []byte(source), 0o644); err != nil {
		return "", fmt.Errorf("diagram %d: write source: %w", n, err)
	}
	if err := t.render(input, output); err != nil {
		return "", fmt.Errorf("diagram %d: %w", n, err)
	}
	return output, nil
}

// renderWithMMDC renders input to output using the Mermaid CLI.
func renderWithMMDC(input, output string) error {
	mmdc, err := exec.LookPath("mmdc")
	if err != nil {
		return fmt.Errorf("mermaid CLI (mmdc) not found: install with `%s`", installCmd)
	}
	cmd := exec.Command(mmdc, "-i", input, "-o", output)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mmdc: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
