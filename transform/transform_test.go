package transform

import (
	"errors"
	"strings"
	"testing"
)

// fakeTransformer appends its name to the content and records the arguments
// it was called with. A non-nil err makes Transform fail.
type fakeTransformer struct {
	name      string
	err       error
	called    bool
	gotWork   string
	gotSource string
}

func (f *fakeTransformer) Name() string { return f.name }

func (f *fakeTransformer) Transform(content, workDir, sourceDir string) (string, error) {
	f.called = true
	f.gotWork = workDir
	f.gotSource = sourceDir
	if f.err != nil {
		return "", f.err
	}
	return content + "+" + f.name, nil
}

func TestRunEmptyPipelinePassesThrough(t *testing.T) {
	const content = "# Title\n\nBody with ```mermaid``` fence left alone.\n"

	got, err := NewPipeline().Run(content, "/work", "/src")
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got != content {
		t.Errorf("Run() = %q, want input unchanged %q", got, content)
	}
}

func TestRunAppliesTransformersInOrder(t *testing.T) {
	first := &fakeTransformer{name: "first"}
	second := &fakeTransformer{name: "second"}
	third := &fakeTransformer{name: "third"}

	got, err := NewPipeline(first, second, third).Run("start", "/work", "/src")
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if want := "start+first+second+third"; got != want {
		t.Errorf("Run() = %q, want %q", got, want)
	}
	for _, f := range []*fakeTransformer{first, second, third} {
		if f.gotWork != "/work" || f.gotSource != "/src" {
			t.Errorf("transformer %s got (workDir, sourceDir) = (%q, %q), want (%q, %q)",
				f.name, f.gotWork, f.gotSource, "/work", "/src")
		}
	}
}

func TestRunFirstErrorAborts(t *testing.T) {
	cause := errors.New("boom")
	ok := &fakeTransformer{name: "ok"}
	failing := &fakeTransformer{name: "failing", err: cause}
	never := &fakeTransformer{name: "never"}

	got, err := NewPipeline(ok, failing, never).Run("start", "/work", "/src")
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !errors.Is(err, cause) {
		t.Errorf("Run() error = %v, want wrapped %v", err, cause)
	}
	if !strings.Contains(err.Error(), "failing") {
		t.Errorf("Run() error = %q, want it to name the failing transformer", err)
	}
	if got != "" {
		t.Errorf("Run() = %q, want empty string on error", got)
	}
	if never.called {
		t.Error("transformer after the failure was called, want run aborted")
	}
}
