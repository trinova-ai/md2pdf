# trinova/md2pdf — wrapper CLI over the vendored picoloom library

A thin CLI (`main.go`, module `github.com/trinova-ai/md2pdf`) that turns Markdown
into styled PDFs using a tagged fork of the
[picoloom](https://github.com/alnah/picoloom) library
(`github.com/trinova-ai/picoloom/v2`; `./picoloom` is the untracked dev
checkout — ADR-001). Delivered so far: the tool (P1–P3), publication under
`trinova-ai` with a clean lint (P4), feature-matrix e2e fixtures (P5), and
the tagged-fork dependency that makes `go install …@latest` work (P6).
New work gets a new phase.

Rules for every task:

- Keep `go build ./...` and `go test ./...` green in the wrapper **and** in
  `picoloom` before checking a step off.
- Never edit `picoloom/` as a side effect of wrapper work. Library changes
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
  fields plus `watermark.text`; known values are validated (string type,
  500-character cap).
- **Orchestration** — `buildInput()` maps config → `md2pdf.Input` (Cover, TOC
  incl. `DisableNumbering` via `toc.numbered: false`, Footer, Signature,
  Watermark, PageBreaks, Page); style CSS resolved through the library's asset
  loader. Single file in, single PDF out; `-o` overrides the output path.
  Config-only invocation: `md2pdf <config>.yaml` resolves the input from
  `input.file` or implicitly `<config-basename>.md` beside the config.
- **Library** — tagged fork dependency `github.com/trinova-ai/picoloom/v2`
  (upstream v2.1.3 plus the patch stack; `./picoloom` is the untracked dev
  checkout — ADR-001).

All phases delivered (P1–P3): transformer pipeline and temp workspace
(`transform/`), Mermaid rendering registered in `run()` (` ```mermaid ` fences
→ SVG via `mmdc`, required at runtime), batch/directory input, `watermark.text`
frontmatter, and frontmatter validation. The example pair lives in `testdata/`
(company-config.yaml + report.md, moved from the retired `examples/` dir) and
is converted for real by `TestConvertReportEndToEnd`;
`testdata/mermaid-sample.md` proves the mermaid path in isolation.

### ADR-001: Vendored fork with a patch stack

The original premise — "use upstream untouched, never fork" — is retired.
Upstream renamed to picoloom (repo `alnah/picoloom`, module
`github.com/alnah/picoloom/v2`); we carry a patch stack on the public fork
`trinova-ai/picoloom` and depend on it as a normal tagged module,
`github.com/trinova-ai/picoloom/v2 vX.Y.Z-trinova.N` (upstream base X.Y.Z,
cut N). No `replace`, no submodule — so `go install
github.com/trinova-ai/md2pdf@latest` works (P6, 2026-07-05; supersedes the
submodule scheme of 2026-07-04).

Triangular layout: remote `upstream` = `alnah/picoloom`, fetch-only; remote
`origin` = `trinova-ai/picoloom`. Branch `main` mirrors upstream,
fast-forward only. Branch `trinova` = the patch stack, kept pure and
upstream-shaped (`git log main..trinova` is authoritative):

1. `fix: keep pre-numbered headings from double numbering in TOC`
2. `feat: embed PDF document outline from headings`
3. `feat: add TOC.DisableNumbering to list headings verbatim`
4. `fix: no blank page after cover/TOC when BeforeH1 is set`
5. `feat: duplex option keeps cover and TOC on their own sheet`

Releases: `scripts/release-picoloom.sh <tag>` stamps a *generated* commit on
a detached head above `trinova` renaming the module path to
`github.com/trinova-ai/picoloom/v2` (go.mod + self-imports), tags it, pushes
the tag, regenerates local branch `dev` (= `trinova` + rename), and bumps
the wrapper's go.mod. The rename never lives on `trinova` — the stack stays
PR-able and the rename cannot conflict, being re-stamped per release.
Published tags are immutable: the Go checksum DB records them permanently —
never re-point one, mint the next `-trinova.N`.

Sync procedure, in `picoloom/`:

```sh
git fetch upstream
git checkout main    && git merge --ff-only upstream/main && git push origin main
git checkout trinova && git rebase main && git push --force-with-lease origin trinova
cd .. && scripts/release-picoloom.sh v2.X.Y-trinova.N   # then commit the go.mod bump
```

Conflicts land most often in `internal/pipeline/tocinject.go`. Dev loop: the
untracked `./picoloom` checkout rests on `dev`; a personal untracked
`go.work` (`use .` + `use ./picoloom`) makes wrapper builds pick up local
library edits, while `GOWORK=off` builds against the pinned tag (what users
get). Decided 2026-07-05: the patches will NOT be upstreamed as PRs — the
stack is a permanent feature branch, carried forward by the sync procedure
above.

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

- [x] Add `transform/transform.go` with the `Transformer` interface and a
      `Pipeline` type (`NewPipeline(...Transformer)`, `Run(content, workDir,
      sourceDir) (string, error)`).
- [x] Empty pipeline returns input unchanged; first error aborts and is
      wrapped with the failing transformer's name.
- [x] Add `transform/transform_test.go`: ordering, error propagation,
      zero-transformer pass-through.

#### G1.1.2: Temp workspace

- [x] Add `transform/workspace.go`: `NewWorkspace()` wrapping `os.MkdirTemp`,
      with `Dir()` and `Cleanup()` (idempotent, usable via `defer`).
- [x] Tests: directory exists and is writable; `Cleanup()` removes it;
      double-`Cleanup()` is safe.

#### G1.1.3: Wire the pipeline into run()

- [x] In `run()`: create a workspace per conversion, run the (currently empty)
      pipeline between frontmatter extraction and `buildInput()`.
- [x] Add `--keep-workspace` flag: skip cleanup and print the path for
      debugging; document it in `--help`.
- [x] Ensure `Input.SourceDir` still points at the source file's directory and
      generated-file references will resolve (absolute paths from transformers).
- [x] Smoke-test: converting `testdata/trust-anchor-strategy.md` produces the
      same PDF as before the change.

### G1.2: Mermaid transformer

First concrete transformer: ` ```mermaid ` fenced blocks become SVG images, so
documents like CrunchGate's `diagram.md` render as diagrams instead of code
listings.

#### G1.2.1: Render mermaid blocks to SVG

- [x] Add `transform/mermaid/mermaid.go` implementing `Transformer`: find
      ` ```mermaid ` fences, write each body to the workspace, render with
      `mmdc` to `mermaid-<n>.svg`, replace the fence with
      `![diagram](<absolute path>)`.
- [x] Missing `mmdc` → clear error naming the install command
      (`npm install -g @mermaid-js/mermaid-cli`); non-mermaid fences untouched.
- [x] Tests (skip when `mmdc` absent): one block, three blocks with sequential
      names, zero blocks pass-through, ` ```go ` fence untouched.

#### G1.2.2: Register and prove end-to-end

- [x] Register the mermaid transformer in `run()`'s pipeline.
- [x] Add `testdata/mermaid-sample.md` with a small diagram; convert it and
      verify the PDF embeds a rendered SVG (not a code listing).
- [x] Re-render the CrunchGate `diagram.md` as a real-world check.

## Phase P2: CLI completeness

Close the gaps between what `run()` does and what the README/config promise.

![[#Data priority]]

### G2.1: Batch mode

#### G2.1.1: Accept a directory as input

- [x] When the input argument is a directory, convert every `*.md` in it
      (non-recursive), each to its own PDF; `-o` names the output directory.
- [x] Continue on per-file errors, report them at the end, exit non-zero if
      any file failed.
- [x] Tests: mixed directory (md + other files), empty directory, one bad file
      among good ones.

### G2.2: Frontmatter gaps

#### G2.2.1: Watermark from frontmatter

Follows the dotted-key convention:

![[#ADR-002: Dotted frontmatter keys]]

- [x] Support `watermark.text`: sets `Watermark.Text` and enables the
      watermark even when the config leaves it off.
- [x] Document the key in `all-options.yaml`'s frontmatter notes.
- [x] Test: `watermark.text: DRAFT` in frontmatter produces a watermarked PDF
      with a watermark-free config.

#### G2.2.2: Frontmatter validation

- [x] Reject absurd values early with clear errors: field length caps and
      non-string scalars where strings are expected.
- [x] Unknown dotted keys keep being ignored, but `--verbose` (new flag) lists
      them so typos are discoverable.
- [x] Tests: over-long title rejected; unknown key silent by default, listed
      under `--verbose`.

## Phase P3: Honest documentation

### G3.1: Make README and examples match reality

#### G3.1.1: Fix README claims

The two decisions the README must tell truthfully:

![[#ADR-001: Vendored fork with a patch stack]]

![[#ADR-002: Dotted frontmatter keys]]

- [x] Correct the module path (`github.com/trinova/md2pdf`, not `trinova-ai`)
      or rename the module — pick one and make install instructions work.
      (Superseded in P4: the module moved to `github.com/trinova-ai/md2pdf`
      when the repo was published under the org.)
- [x] Remove or scope upstream-inherited claims (e.g. "parallel batch
      processing") to what the wrapper actually does; add the vendoring story
      and the sync procedure from ADR-001.
- [x] Document the dotted frontmatter keys with a full example and the
      data-priority chain.

#### G3.1.2: Examples directory

- [x] Add `examples/company-config.yaml` (org defaults) and
      `examples/report.md` (frontmatter + a mermaid block), internally
      consistent — no duplicated metadata.
- [x] Verify the README walkthrough works from scratch:
      `md2pdf -c examples/company-config.yaml examples/report.md`.

## Phase P4: Publication

Everything shipped so far exists only on this machine. This phase backs the
work up and gets the linter to zero. (Upstreaming the library patches was
originally a goal here; dropped 2026-07-05 — see ADR-001.)
Two related loose ends live outside this repo and are deliberately
not tasks here: the CrunchGate vault's uncommitted method doc + `md2pdf.yaml`,
and the out-of-sync `plugins/trinova` copy of the method doc.

### G4.1: Remotes and backup

The wrapper repo (~26 commits) has no remote. The vendored fork's only remote
is upstream `alnah/picoloom` (still fetched via its old `go-md2pdf` URL),
which is not writable, so its 5-commit patch stack is machine-local too. `go.mod` wires the fork in via
`replace … => ./alnah:picoloom`, and that directory is untracked by the
wrapper repo — a fresh clone of the wrapper alone does not build.

Decided 2026-07-04: both repos live under the `trinova-ai` GitHub org (the
org René owns). The module path `github.com/trinova/md2pdf` predates this and
gets renamed along the way.

![[#ADR-001: Vendored fork with a patch stack]]

#### G4.1.1: Push the picoloom fork

Set up the triangular workflow from ADR-001: fork, rewire remotes, split the
patch stack onto `trinova`, push.

- [x] Fork upstream into the org: `gh repo fork alnah/picoloom --org
      trinova-ai` — a real GitHub fork (public by construction) so upstream
      PRs can come from it.
- [x] Rewire remotes in `alnah:picoloom/`: rename `origin` → `upstream`,
      re-point it at `git@github.com:alnah/picoloom.git` (drop the
      `go-md2pdf` redirect), disable its push URL; add `origin` =
      `git@github.com:trinova-ai/picoloom.git`.
- [x] Branch split: create `trinova` at the current 5-patch tip and check it
      out; reset `main` to `upstream/main`; run one full ADR-001 sync cycle.
- [x] Push `main` and `trinova` to `origin` with `-u`; fork and wrapper
      builds/tests green.
- [x] Update the README's Vendored library section (repo link, remote/branch
      layout, sync procedure) to match ADR-001.

#### G4.1.2: Push the wrapper repo

- [x] Create `github.com/trinova-ai/md2pdf` (public — decided 2026-07-04)
      and rename the module from `github.com/trinova/md2pdf` to
      `github.com/trinova-ai/md2pdf` — go.mod, the `transform` import in
      `main.go`, and the README install instructions.
- [x] Decide how a clone obtains the vendored fork — git submodule pinned to
      the patch stack, README bootstrap instructions, or dropping `replace`
      in favor of the pushed fork's module path — and implement it (update
      the README's Vendored library section to match). Decided: submodule at
      `./picoloom` (branch `trinova`), replacing the untracked
      `alnah:picoloom/` working-clone name.
- [x] Tidy the working tree first: ignore or remove the `md2pdf` binary,
      `.DS_Store`, and `solworktext:md2pdf/` (unrelated reference checkout);
      decide whether `testdata/trust-anchor-strategy.*` and `work.yaml` are
      fixtures worth tracking or scratch to drop.
- [x] Create the remote, push `master`, set upstream; verify a fresh clone
      builds and converts `examples/report.md` per the README walkthrough.

### G4.3: Lint zero

#### G4.3.1: Fix errcheck findings

- [x] Handle the unchecked `defer ws.Cleanup()` returns (in `run()` in
      `main.go`, and in `transform/workspace_test.go`) — check the error or
      discard it explicitly with a rationale.
- [x] Lint the wrapper to zero findings, keep `go build ./...` and
      `go test ./...` green, then `go install .` and smoke-test per the plan
      rules.

## Phase P5: Test fixtures

Fixture-driven end-to-end coverage of the feature matrix. Everything here is
synthetic or explicitly cleared for the public repo (`trinova-mark.svg` —
cleared by René 2026-07-04). A real-world representative document (deep
heading hierarchy, pre-numbered headings, big tables, several diagrams) is
still wanted; René is looking for one — when it lands, add a task here to
fixture it. E2E tests follow the `TestConvertReportEndToEnd` conventions:
skip under `-short`, skip mermaid-dependent ones without `mmdc`.

### G5.1: Feature-matrix fixtures

Each task adds a fixture pair plus the test that converts it for real. After
these land, the untested config surface shrinks to: styles other than
corporate/custom, page orientation/size variants, and `--keep-workspace`.

#### G5.1.1: Cover logo in the walkthrough pair

- [x] Vendor `testdata/trinova-mark.svg` from
      https://trinovalabs.ai/assets/trinova-mark.svg.
- [x] Enable `cover.logo: "./trinova-mark.svg"` in
      `testdata/company-config.yaml` so `TestConvertReportEndToEnd` also
      covers config-relative logo resolution; suite green.

#### G5.1.2: Formal-report pair — verbatim TOC, duplex, signature, custom CSS

- [x] Add `testdata/formal.yaml` (`input.file: formal.md`,
      `toc.numbered: false`, `pageBreaks.duplex: true`,
      `signature.enabled: true`, `style: formal` +
      `assets.basePath: testdata`), `testdata/formal.md` (pre-numbered
      `## N.` headings, a table), and a small `testdata/styles/formal.css`.
      Custom styles load as `<basePath>/styles/<name>.css` — the "path to a
      .css file" story in the README/all-options was wrong; corrected as
      part of this task. `basePath` being CWD-relative (unlike
      `input.file`/`cover.logo`) is a known wart.
- [x] Test: config-only invocation (`md2pdf testdata/formal.yaml`) produces a
      PDF — this also exercises the `input.file` resolution path.

#### G5.1.3: Mermaid width-and-scale fixture

- [x] Add `testdata/diagrams.md`: one wide and one narrow diagram plus a bare
      numeric `mermaid.scale` in its frontmatter (the numeric-key path, end
      to end).
- [x] Test: converting it with the walkthrough config produces a PDF; skipped
      without `mmdc`.

#### G5.1.4: Batch conversion end to end

- [x] Test: a temp directory holding two small generated markdown files,
      converted through the real CLI (directory input, `-o` out-dir),
      produces two PDFs — the real-render counterpart to the fake-convert
      batch unit tests.

## Phase P6: Tagged-fork dependency — drop the submodule

Decided 2026-07-05 (supersedes the submodule story in ADR-001): the fork
publishes real tags whose tip is a *generated* module-rename commit
(`github.com/alnah/picoloom/v2` → `github.com/trinova-ai/picoloom/v2`), and
md2pdf requires the fork by tag like any normal dependency — no `replace`,
no submodule. That makes `go install github.com/trinova-ai/md2pdf@latest` a
working install path. `trinova` stays pure (upstream + patches, PR-able);
the rename is stamped fresh at each release so it can never conflict. The
dev loop uses an untracked `go.work` (`use .` + `use ./picoloom`) with the
nested, untracked fork checkout resting on `dev` (= trinova + rename).
Published tags are immutable: never re-point one, always mint the next
`-trinova.N`.

### G6.1: Switch to the tagged fork

#### G6.1.1: Fork release machinery and first tag

- [x] Add `scripts/release-picoloom.sh <tag> [fork-dir]`: from a clean fork
      tree — detach from `trinova`, `go mod edit -module` + rewrite
      self-imports, `GOWORK=off` build+test, commit, tag, push the tag,
      reset `dev` to the release commit and rest there; then `go get` the
      tag in md2pdf. Add an `xc` task wrapping it.
- [x] Cut `v2.1.2-trinova.1` with the script; tag fetchable from GitHub.

#### G6.1.2: Require the tag; remove the submodule

- [x] Swap the root import and `go.mod` to
      `github.com/trinova-ai/picoloom/v2 v2.1.2-trinova.1`; drop the
      `replace` and the alnah require.
- [x] De-submodule: remove the `picoloom` gitlink and `.gitmodules`, clean
      the submodule config; ignore `/picoloom/`, `go.work`, `go.work.sum`;
      write the untracked `go.work`.
- [x] Both build modes green: `GOWORK=off go test ./...` (pinned tag) and
      workspace `go test ./...` (dev loop).

#### G6.1.3: Docs, v0.1.0, end-to-end install proof

- [x] Rewrite README Installation (`go install …@latest`) and Vendored
      library (tagged fork, release script, `dev` branch, go.work loop);
      update ADR-001; no `--recurse-submodules` anywhere.
- [x] Tag md2pdf `v0.1.0` and push it — `@latest` must not resolve to the
      ancient `v0.0.1`.
- [x] Prove it: from a pristine `GOMODCACHE`,
      `go install github.com/trinova-ai/md2pdf@latest` and the binary
      reports `v0.1.0`.
