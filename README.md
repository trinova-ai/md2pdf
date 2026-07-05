# md2pdf

Convert Markdown to styled PDFs from the command line. A thin Go CLI
(`github.com/trinova-ai/md2pdf`) around a tagged fork of
[picoloom](https://github.com/alnah/picoloom), which renders through headless
Chrome (Chromium is auto-downloaded by the library on first run).

- Cover page, table of contents (auto-numbered, or verbatim with
  `toc.numbered: false`), footers with page numbers, signature block,
  watermarks, PDF document outline
- 8 built-in CSS themes (academic, corporate, creative, default, invoice,
  legal, manuscript, technical) or custom styles via `assets.basePath`
  (loaded as `<basePath>/styles/<name>.css`)
- YAML config plus per-document frontmatter overrides (dotted keys, see below)
- Batch mode: a directory as input converts every `.md` in it, sequentially,
  continuing past per-file failures
- Duplex-printing support (`pageBreaks.duplex`)

## Installation

```sh
go install github.com/trinova-ai/md2pdf@latest
```

Requires Go 1.25+. The picoloom dependency is a regular tagged module
(`github.com/trinova-ai/picoloom/v2`, see
[Vendored library](#vendored-library-adr-001)) — no submodules, no `replace`
directives. From a checkout, `go install .` works too (`xc install` for a
tag-stamped binary).

## Usage

Single file:

```sh
md2pdf document.md                        # → document.pdf next to the input
md2pdf -c config.yaml document.md         # apply a config
md2pdf -c config.yaml -o out.pdf document.md
```

Config-only invocation — a lone YAML argument is a config that names its own
input, explicitly via `input.file` (relative to the config file) or implicitly
as `<config-basename>.md` next to it:

```sh
md2pdf report.yaml    # converts report.md (or input.file) using report.yaml
```

Batch mode — a directory as input converts every `*.md` directly inside it
(non-recursive), each to its own PDF. `-o` names the output **directory**
(default: `output.defaultDir` from the config, else the input directory).
Conversion continues past per-file failures; they are listed at the end and
the exit code is non-zero if any file failed:

```sh
md2pdf -c config.yaml -o out/ docs/
```

Flags and subcommands:

| | |
|---|---|
| `-c FILE` | YAML config file |
| `-o PATH` | output PDF file; output directory in batch mode |
| `--verbose` | list ignored unknown frontmatter keys (typo discovery) |
| `--keep-workspace` | keep the transformer workspace directory and print its path |
| `init [filename]` | write an exhaustively commented config template |
| `frontmatter [-c FILE] [--dry-run] <input.md>` | move document metadata from the config into the .md frontmatter ([details](#moving-metadata-into-the-document-md2pdf-frontmatter)) |

## Configuration

`md2pdf init` writes a template documenting every option (the same content as
`all-options.yaml` in this repository). Highlights:

- `style` — a theme name, or a custom style name resolved through
  `assets.basePath` as `<basePath>/styles/<name>.css` (names may not contain
  `/` or `.`; `basePath` resolves against the working directory, not the
  config file)
- `toc.numbered: false` — list headings verbatim when they carry their own
  numbers (e.g. `## 3. Design`)
- `pageBreaks.duplex: true` — blank verso after cover/TOC so the body starts
  on a fresh sheet when printing double-sided
- `cover.logo` — relative paths resolve against the config file's directory
- `input.file` / `output.defaultDir` — input for config-only runs, default
  output directory
- `timeout` — Go duration, e.g. `"30s"`

### Data priority

```
CLI flags  →  frontmatter  →  config file  →  defaults
 (highest)                                    (lowest)
```

Today only `-o` exists at the CLI layer.

### Frontmatter: dotted keys

Frontmatter keys name the config field they override (`document.title`,
`author.name`) — **not** flat Jekyll-style keys (`title`). One namespace, no
mapping table: an override is exactly `config path = value` (ADR-002).

```markdown
---
document.title: "Q3 Security Review"
document.version: "2.5"
document.date: "auto"
author.name: "Sarah Chen"
watermark.text: "DRAFT"
---

# Q3 Security Review
...
```

Rules:

- Values must be quoted strings of at most 500 characters. Quote numbers:
  `document.version: "2.5"` — an unquoted `2.5` is rejected with a hint to
  quote it.
- Supported keys: every `document.*` and `author.*` field, plus
  `watermark.text`, `footer.text`, and `toc.title`. What each key sets, where
  it lands in the PDF, and when it renders are listed in the
  [Frontmatter key reference](#frontmatter-key-reference) below.
- `mermaid.scale` is the one numeric key: it overrides the diagram symbol
  size per document (see [Transformers](#transformers)). Bare numbers are
  fine here: `mermaid.scale: 0.62`.
- Unknown keys are ignored; run with `--verbose` to list them and catch typos.

In batch mode each file's frontmatter overrides the shared config for that
file only.

#### Frontmatter key reference

Where each key's value lands in the rendered PDF, and the condition that must
hold for it to show. "Appears in" names the block a value is rendered in;
"Shown when" is the gate that block is built behind (a value passed to a
disabled block is silently dropped).

| Key | Purpose | Appears in | Shown when |
|---|---|---|---|
| `document.title` | Document title | Cover title | cover enabled |
| `document.subtitle` | Subtitle under the title | Cover | cover enabled |
| `document.version` | Version string | Cover, and footer (as the footer status) | cover enabled; footer enabled |
| `document.date` | Document date (`"auto"` → today, long format) | Cover, and footer | cover enabled; footer enabled |
| `document.clientName` | Client / customer name | Cover | cover enabled |
| `document.projectName` | Project name | Cover | cover enabled |
| `document.documentType` | Document-type label | Cover | cover enabled |
| `document.documentID` | Document identifier | Cover, and footer | cover enabled; footer enabled **and** `footer.showDocumentID` set |
| `document.description` | Brief summary | Cover | cover enabled |
| `author.name` | Author name | Cover, and signature block | cover enabled; signature enabled |
| `author.title` | Author role / title | Cover, and signature block | cover enabled; signature enabled |
| `author.organization` | Organization | Cover, and signature block | cover enabled; signature enabled |
| `author.email` | Contact email (a `mailto:` link) | Signature block | signature enabled |
| `author.phone` | Contact phone | Signature block | signature enabled |
| `author.address` | Postal address | Signature block | signature enabled |
| `author.department` | Department | Signature block, and cover | signature enabled; cover enabled **and** `cover.showDepartment` set |
| `watermark.text` | Watermark text | Diagonal watermark on every page | self-enabling (see below) |
| `footer.text` | Free-form footer text | Footer | footer enabled (does **not** enable the footer) |
| `toc.title` | Heading above the table of contents | TOC heading | TOC enabled (does **not** enable the TOC) |
| `mermaid.scale` | Diagram symbol size (CSS px per Mermaid unit) | Rendered Mermaid diagrams | whenever the document renders a Mermaid diagram |

Two asymmetries are worth calling out. `watermark.text` **self-enables**:
setting it turns the watermark on for that document even when the config leaves
it off. `footer.text` and `toc.title` do **not** self-enable — the config
decides whether a footer or TOC exists, the document only decides what it says.
And `document.date: "auto"` resolves to today's date in long format (e.g.
`5 January 2025`); any other value is used verbatim.

### Moving metadata into the document: `md2pdf frontmatter`

A config that carries `document.title`, `author.name`, … only fits one
document. `md2pdf frontmatter` migrates that metadata into the document
itself, leaving a style-only config you can reuse across every document:

```sh
md2pdf frontmatter -c trinova-technical.yaml report.md   # migrate once
md2pdf -c trinova-technical.yaml report.md               # renders the same PDF
md2pdf -c trinova-technical.yaml other-doc.md            # config now reusable
```

With `-c`, every eligible key the config carries (`document.*`, `author.*`,
`watermark.text`, `footer.text`, `toc.title`, `mermaid.scale`) is added to the
document's frontmatter block — created if the file has none — and stripped
from the config. The rewrite preserves comments and the ordering of untouched
settings, and drops sections left empty (`document:`, `author:`).

The migration is render-neutral: frontmatter outranks the config, so moving
a key never changes the produced PDF. Keys already present in the document
win and stay byte-for-byte untouched — they are still stripped from the
config, since the document's value outranks the config's even when the two
differ. A key the document carries only as an empty scaffold (a bare
`author.name:`) overrides nothing and stays in the config. Unknown/private
frontmatter keys are never touched.

Without `-c` the subcommand writes an empty `document.*`/`author.*` scaffold
for you to fill in. `--dry-run` prints both rewritten files without touching
disk.

## Examples

`testdata/` holds a working pair demonstrating the config/frontmatter split:
[`company-config.yaml`](testdata/company-config.yaml) carries the org-wide
defaults (author, style, cover, TOC, footer, page setup) and
[`report.md`](testdata/report.md) carries the per-document metadata in its
frontmatter (title, version, `watermark.text: "DRAFT"`) — no metadata is
duplicated between the two. From the repository root:

```sh
md2pdf -c testdata/company-config.yaml testdata/report.md   # → testdata/report.pdf
```

The same pair is the end-to-end test fixture: `go test` converts it for real
(`TestConvertReportEndToEnd`; skipped under `-short` or when `mmdc` is
missing).

The report includes a ` ```mermaid ` fence; it renders as a diagram, which
requires the Mermaid CLI at runtime (see [Transformers](#transformers)).

## Vendored library (ADR-001)

Upstream renamed to picoloom (repo [alnah/picoloom](https://github.com/alnah/picoloom),
module `github.com/alnah/picoloom/v2`). md2pdf depends on the public fork
[trinova-ai/picoloom](https://github.com/trinova-ai/picoloom) as a normal
tagged module: `github.com/trinova-ai/picoloom/v2 vX.Y.Z-trinova.N`, where
`X.Y.Z` is the upstream base and `N` counts our cuts on it.

The fork uses a triangular workflow: remote `upstream` (`alnah/picoloom`) is
fetch-only, remote `origin` (`trinova-ai/picoloom`) receives everything.
Branch `main` mirrors upstream and only fast-forwards. Branch `trinova` is
the patch stack, kept pure and upstream-shaped (`git log main..trinova` is
authoritative — currently 5 commits):

1. fix: keep pre-numbered headings from double numbering in TOC
2. feat: embed PDF document outline from headings
3. feat: add TOC.DisableNumbering to list headings verbatim
4. fix: no blank page after cover/TOC when BeforeH1 is set
5. feat: duplex option keeps cover and TOC on their own sheet

Releases: `scripts/release-picoloom.sh <tag>` stamps a **generated** commit
on a detached head above `trinova` that renames the module path to
`github.com/trinova-ai/picoloom/v2` (go.mod plus self-imports), tags it,
pushes the tag, regenerates the local `dev` branch (= `trinova` + rename),
and bumps `go.mod` here. Because the rename never lives on `trinova`, the
stack stays PR-able and the rename can never conflict — it is re-stamped
fresh at every release. **Published tags are immutable**: the Go checksum
database records them permanently, so never re-point one; mint the next
`-trinova.N` instead.

Sync with upstream (in the fork checkout, then release):

```sh
git fetch upstream
git checkout main    && git merge --ff-only upstream/main && git push origin main
git checkout trinova && git rebase main && git push --force-with-lease origin trinova
GOWORK=off go test ./...                     # conflicts land most often in internal/pipeline/tocinject.go
cd .. && scripts/release-picoloom.sh v2.X.Y-trinova.N   # then commit the go.mod bump
```

Dev loop: keep an untracked checkout at `./picoloom` resting on `dev` and a
personal (never committed) `go.work` with `use .` and `use ./picoloom` —
builds then pick up local library edits instantly. `GOWORK=off` opts out and
builds against the pinned tag, i.e. exactly what users get; the release
script and any release verification use it.

Bootstrap on a fresh clone of this repo (the fork checkout and `go.work` are
untracked, so they don't come with it):

```sh
git clone git@github.com:trinova-ai/picoloom.git picoloom
cd picoloom
git remote add upstream git@github.com:alnah/picoloom.git
git remote set-url --push upstream DISABLED   # rebases fetch upstream, never push to it
git fetch upstream
git checkout trinova                          # the patch stack
git branch dev v2.1.3-trinova.1               # latest release tag — every tag IS a dev snapshot
git checkout dev
cd ..
printf 'go 1.25.4\n\nuse (\n\t.\n\t./picoloom\n)\n' > go.work
```

Commit emails must be a GitHub noreply address (the fork rejects private
emails via GH007); the release script checks this before doing anything.

The patch stack is a permanent feature branch: it is not upstreamed, and the
sync procedure above carries it forward across upstream releases.

## Transformers

The `transform` package provides a sequential markdown-rewriting pipeline that
runs before conversion, with a disposable per-run workspace
(`--keep-workspace` preserves it for debugging). Registered transformers:

- **Mermaid** (`transform/mermaid`) — ` ```mermaid ` fences are rendered to
  SVG and embedded as diagrams in the PDF. Requires the Mermaid CLI (`mmdc`)
  at runtime: `brew install mermaid-cli` (or
  `npm install -g @mermaid-js/mermaid-cli`). If `mmdc` errors about a missing
  browser, run `npx puppeteer browsers install chrome-headless-shell` — the
  Homebrew bottle does not bundle one. Documents without mermaid fences never
  invoke `mmdc`.

  Diagrams are embedded at their **natural size**, so symbols and label text
  are the same size in every diagram regardless of its dimensions (wide
  diagrams still shrink to fit the page width). Tune with `mermaid.scale` in
  the config: `1.0` (default) = Mermaid's native 16px labels, `0.75` shrinks
  symbols to roughly body-text size, values above 1 enlarge.

## Tasks

This project uses [xc](https://github.com/joerdav/xc) as a task runner.

Install xc: `go install github.com/joerdav/xc@latest`

Run tasks from the project root with `xc <task-name>`.

### install

Install into `$GOPATH/bin` stamped with the release tag (`git describe`), so
`md2pdf --version` reports e.g. `v0.0.1` or `v0.0.1-3-gabc1234` between tags.

```sh
go install -ldflags "-X main.Version=$(git describe --tags --always --dirty)" .
```

### install-dev

Install from local source into `$GOPATH/bin`. The version falls back to the
VCS pseudo-version Go embeds in the binary (`dev` when built without VCS
info); use `install` for a tag-stamped binary.

```sh
go install .
```

### test

Run all tests.

```sh
go test ./...
```

### test-verbose

Run tests with coverage.

```sh
go test ./... -v -coverprofile=coverage.out
```

### lint

Run golangci-lint.

```sh
golangci-lint run
```

### release-picoloom

Cut a tagged release of the picoloom fork and bump `go.mod` to it. Set `TAG`
to the next `-trinova.N` version (published tags are immutable — never
reuse one). See [Vendored library](#vendored-library-adr-001).

```sh
scripts/release-picoloom.sh "$TAG"
```

### clean

Remove build artifacts.

```sh
rm -f md2pdf coverage.out
```

### all

Requires: clean, test, lint, install-dev

Full development cycle.
