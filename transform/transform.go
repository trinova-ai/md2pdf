// Package transform rewrites markdown content before it is handed to the
// PDF conversion library. Transformers run sequentially; each receives the
// previous one's output.
package transform

import "fmt"

// Transformer processes markdown content before PDF conversion.
type Transformer interface {
	// Name returns a human-readable identifier for logging and errors.
	Name() string

	// Transform rewrites the markdown. workDir is a temp directory for
	// intermediate files (SVGs, …); sourceDir is the source file's directory
	// for resolving relative paths. Returns the content unchanged when
	// nothing matches.
	Transform(content, workDir, sourceDir string) (string, error)
}

// Pipeline runs a fixed sequence of transformers over markdown content.
type Pipeline struct {
	transformers []Transformer
}

// NewPipeline returns a Pipeline that runs the given transformers in order.
func NewPipeline(transformers ...Transformer) *Pipeline {
	return &Pipeline{transformers: transformers}
}

// Run feeds content through each transformer in order, passing every one the
// same workDir and sourceDir. The first error aborts the run and is wrapped
// with the failing transformer's name. A pipeline with no transformers
// returns the content unchanged.
func (p *Pipeline) Run(content, workDir, sourceDir string) (string, error) {
	for _, t := range p.transformers {
		out, err := t.Transform(content, workDir, sourceDir)
		if err != nil {
			return "", fmt.Errorf("transformer %s: %w", t.Name(), err)
		}
		content = out
	}
	return content, nil
}
