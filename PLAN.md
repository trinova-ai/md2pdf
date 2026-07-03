# trinova/md2pdf — wrapper CLI over the vendored picoloom library

A thin CLI (`main.go`, module `github.com/trinova/md2pdf`) that turns Markdown
into styled PDFs using the vendored [picoloom](https://github.com/alnah/go-md2pdf)
library (`./alnah:picoloom`). Config + frontmatter + library orchestration are
done; this plan covers what remains: the transformer pipeline (Mermaid → SVG),
batch mode, frontmatter gaps, and honest docs.

Rules for every task:

- Keep `go build ./...` and `go test ./...` green in the wrapper **and** in
  `alnah:picoloom` before checking a step off.
- Never edit `alnah:picoloom/` as a side effect of wrapper work. Library changes
  are their own commits in that repo, on top of its patch stack (see ADR-001).
- Reinstall with `go install .` after user-visible changes and smoke-test:
  `md2pdf -c md2pdf.yaml <some.md>` from a directory that has both.

## Reference

Sections below are context, not work. `mdplan next` skips them; tasks pull them
in with `![[#…]]` embeds where needed.

### Current state (2026-07-03)

Implemented, in a single ~500-line `main.go`:

- **Config loader** — `Config` struct mirrors `all-options.yaml`; loaded via
  `-c FILE`. `init` subcommand writes the annotated template.
- **Frontmatter** — `extractFrontmatter()` parses a `---` block of **dotted
  keys** (ADR-002); `applyFrontmatter()` overlays them on config.
  `date: auto` resolves to today. Covered: all `document.*` and `author.*`
  fields. Not covered: `watermark`, validation of field lengths.
- **Orchestration** — `buildInput()` maps config → `md2pdf.Input` (Cover, TOC
  incl. `DisableNumbering` via `toc.numbered: false`, Footer, Signature,
  Watermark, PageBreaks, Page); style CSS resolved through the library's asset
  loader. Single file in, single PDF out; `-o` overrides the output path.
  Config-only invocation: `md2pdf <config>.yaml` resolves the input from
  `input.file` or implicitly `<config-basename>.md` beside the config.
- **Library** — vendored at `alnah:picoloom`, upstream v2.1.2 plus a
  local patch stack (ADR-001).

Not implemented: transformer pipeline, temp workspace, Mermaid rendering,
batch/directory input, `watermark` frontmatter, frontmatter validation,
`examples/`. The manual workaround for diagrams is visible in `testdata/`:
a hand-rendered SVG referenced from the markdown.

### ADR-001: Vendored fork with a patch stack

The original premise — "use upstream untouched, never fork" — is retired.
Upstream renamed to `picoloom` (module `github.com/alnah/picoloom/v2`) and we
carry a patch stack of local commits on the vendored copy, rebased onto
`origin/main` (`git log origin/main..main` there is authoritative):

1. `fix: keep pre-numbered headings from double numbering in TOC`
2. `feat: embed PDF document outline from headings`
3. `feat: add TOC.DisableNumbering to list headings verbatim`
4. `fix: no blank page after cover/TOC when BeforeH1 is set`

Sync procedure: in `alnah:picoloom/` run `git fetch origin && git rebase
origin/main`, resolve conflicts (most likely `internal/pipeline/tocinject.go`),
run its tests, then rebuild the wrapper. Long-term exit: upstream these commits
as PRs; rebase then drops them automatically.

### ADR-002: Dotted frontmatter keys

Frontmatter uses dotted keys that name the config field they override
(`document.title`, `author.name`), not flat Jekyll-style keys (`title`). One
namespace, zero mapping tables: an override is exactly `config path = value`.
Unknown keys are ignored (lenient parse). The cost — unfamiliar to Jekyll/Hugo
users — is documented in the README rather than papered over with aliases.

### Data priority

```
CLI flags  →  frontmatter  →  config file  →  defaults
 (highest)                                    (lowest)
```

Today only `-o` exists at the CLI layer; new flags must slot into this order.

### Transformer contract

```go
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
```

Transformers run sequentially; each receives the previous one's output. An
error aborts the conversion with the transformer's `Name()` in the message.
The library resolves relative image paths via `Input.SourceDir`, so generated
files must either live under `sourceDir` or be referenced by absolute path.

## Phase P1: Transformer pipeline

The wrapper's reason to exist beyond frontmatter: rewrite markdown before the
library sees it. Ship the plumbing first, then Mermaid as the first consumer.

![[#Transformer contract]]

### G1.1: Pipeline and workspace plumbing

A `transform` package: the interface, a sequential pipeline, and a disposable
workspace. No concrete transformers yet — a zero-transformer pipeline must be
a no-op so this lands without behavior change.

#### G1.1.1: Create the transform package

- [ ] Add `transform/transform.go` with the `Transformer` interface and a
      `Pipeline` type (`NewPipeline(...Transformer)`, `Run(content, workDir,
      sourceDir) (string, error)`).
- [ ] Empty pipeline returns input unchanged; first error aborts and is
      wrapped with the failing transformer's name.
- [ ] Add `transform/transform_test.go`: ordering, error propagation,
      zero-transformer pass-through.

#### G1.1.2: Temp workspace

- [ ] Add `transform/workspace.go`: `NewWorkspace()` wrapping `os.MkdirTemp`,
      with `Dir()` and `Cleanup()` (idempotent, usable via `defer`).
- [ ] Tests: directory exists and is writable; `Cleanup()` removes it;
      double-`Cleanup()` is safe.

#### G1.1.3: Wire the pipeline into run()

- [ ] In `run()`: create a workspace per conversion, run the (currently empty)
      pipeline between frontmatter extraction and `buildInput()`.
- [ ] Add `--keep-workspace` flag: skip cleanup and print the path for
      debugging; document it in `--help`.
- [ ] Ensure `Input.SourceDir` still points at the source file's directory and
      generated-file references will resolve (absolute paths from transformers).
- [ ] Smoke-test: converting `testdata/trust-anchor-strategy.md` produces the
      same PDF as before the change.

### G1.2: Mermaid transformer

First concrete transformer: ` ```mermaid ` fenced blocks become SVG images, so
documents like CrunchGate's `diagram.md` render as diagrams instead of code
listings.

#### G1.2.1: Render mermaid blocks to SVG

- [ ] Add `transform/mermaid/mermaid.go` implementing `Transformer`: find
      ` ```mermaid ` fences, write each body to the workspace, render with
      `mmdc` to `mermaid-<n>.svg`, replace the fence with
      `![diagram](<absolute path>)`.
- [ ] Missing `mmdc` → clear error naming the install command
      (`npm install -g @mermaid-js/mermaid-cli`); non-mermaid fences untouched.
- [ ] Tests (skip when `mmdc` absent): one block, three blocks with sequential
      names, zero blocks pass-through, ` ```go ` fence untouched.

#### G1.2.2: Register and prove end-to-end

- [ ] Register the mermaid transformer in `run()`'s pipeline.
- [ ] Add `testdata/mermaid-sample.md` with a small diagram; convert it and
      verify the PDF embeds a rendered SVG (not a code listing).
- [ ] Re-render the CrunchGate `diagram.md` as a real-world check.

## Phase P2: CLI completeness

Close the gaps between what `run()` does and what the README/config promise.

![[#Data priority]]

### G2.1: Batch mode

#### G2.1.1: Accept a directory as input

- [ ] When the input argument is a directory, convert every `*.md` in it
      (non-recursive), each to its own PDF; `-o` names the output directory.
- [ ] Continue on per-file errors, report them at the end, exit non-zero if
      any file failed.
- [ ] Tests: mixed directory (md + other files), empty directory, one bad file
      among good ones.

### G2.2: Frontmatter gaps

#### G2.2.1: Watermark from frontmatter

Follows the dotted-key convention:

![[#ADR-002: Dotted frontmatter keys]]

- [ ] Support `watermark.text`: sets `Watermark.Text` and enables the
      watermark even when the config leaves it off.
- [ ] Document the key in `all-options.yaml`'s frontmatter notes.
- [ ] Test: `watermark.text: DRAFT` in frontmatter produces a watermarked PDF
      with a watermark-free config.

#### G2.2.2: Frontmatter validation

- [ ] Reject absurd values early with clear errors: field length caps and
      non-string scalars where strings are expected.
- [ ] Unknown dotted keys keep being ignored, but `--verbose` (new flag) lists
      them so typos are discoverable.
- [ ] Tests: over-long title rejected; unknown key silent by default, listed
      under `--verbose`.

## Phase P3: Honest documentation

### G3.1: Make README and examples match reality

#### G3.1.1: Fix README claims

The two decisions the README must tell truthfully:

![[#ADR-001: Vendored fork with a patch stack]]

![[#ADR-002: Dotted frontmatter keys]]

- [ ] Correct the module path (`github.com/trinova/md2pdf`, not `trinova-ai`)
      or rename the module — pick one and make install instructions work.
- [ ] Remove or scope upstream-inherited claims (e.g. "parallel batch
      processing") to what the wrapper actually does; add the vendoring story
      and the sync procedure from ADR-001.
- [ ] Document the dotted frontmatter keys with a full example and the
      data-priority chain.

#### G3.1.2: Examples directory

- [ ] Add `examples/company-config.yaml` (org defaults) and
      `examples/report.md` (frontmatter + a mermaid block), internally
      consistent — no duplicated metadata.
- [ ] Verify the README walkthrough works from scratch:
      `md2pdf -c examples/company-config.yaml examples/report.md`.
