package transform

import (
	"fmt"
	"os"
)

// Workspace is a temporary directory for a pipeline run's intermediate
// files (SVGs, …). Create one with NewWorkspace and remove it with Cleanup,
// typically via defer.
type Workspace struct {
	dir string
}

// NewWorkspace creates a fresh temporary directory under the system temp
// root and returns a Workspace owning it.
func NewWorkspace() (*Workspace, error) {
	dir, err := os.MkdirTemp("", "md2pdf-*")
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	return &Workspace{dir: dir}, nil
}

// Dir returns the path of the workspace directory.
func (w *Workspace) Dir() string {
	return w.dir
}

// Cleanup removes the workspace directory and everything in it. It is
// idempotent — calling it after the directory is already gone is a no-op —
// so it is safe to use via defer.
func (w *Workspace) Cleanup() error {
	if err := os.RemoveAll(w.dir); err != nil {
		return fmt.Errorf("cleanup workspace: %w", err)
	}
	return nil
}
