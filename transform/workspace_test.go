package transform

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestNewWorkspaceCreatesWritableDir(t *testing.T) {
	ws, err := NewWorkspace()
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v, want nil", err)
	}
	defer ws.Cleanup()

	info, err := os.Stat(ws.Dir())
	if err != nil {
		t.Fatalf("Stat(%q) error = %v, want existing directory", ws.Dir(), err)
	}
	if !info.IsDir() {
		t.Fatalf("Dir() = %q, want a directory", ws.Dir())
	}
	probe := filepath.Join(ws.Dir(), "probe.txt")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		t.Errorf("WriteFile(%q) error = %v, want writable workspace", probe, err)
	}
}

func TestCleanupRemovesDir(t *testing.T) {
	ws, err := NewWorkspace()
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v, want nil", err)
	}
	dir := ws.Dir()
	// Put a file inside so Cleanup has to remove contents, not just the
	// empty directory.
	if err := os.WriteFile(filepath.Join(dir, "probe.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	if err := ws.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v, want nil", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(%q) error = %v, want fs.ErrNotExist after Cleanup", dir, err)
	}
}

func TestCleanupTwiceIsSafe(t *testing.T) {
	ws, err := NewWorkspace()
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v, want nil", err)
	}

	if err := ws.Cleanup(); err != nil {
		t.Fatalf("first Cleanup() error = %v, want nil", err)
	}
	if err := ws.Cleanup(); err != nil {
		t.Errorf("second Cleanup() error = %v, want nil", err)
	}
}
