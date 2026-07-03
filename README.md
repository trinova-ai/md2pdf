# md2pdf

Convert Markdown to styled PDFs from the command line. A thin Go CLI
(`github.com/trinova/md2pdf`) around a vendored fork of
[picoloom](https://github.com/alnah/go-md2pdf), which renders through headless
Chrome (Chromium is auto-downloaded by the library on first run).

- Cover page, table of contents (auto-numbered, or verbatim with
  `toc.numbered: false`), footers with page numbers, signature block,
  watermarks, PDF document outline
- 8 built-in CSS themes (academic, corporate, creative, default, invoice,
  legal, manuscript, technical) or a custom `.css` file
- YAML config plus per-document frontmatter overrides (dotted keys, see below)
- Batch mode: a directory as input converts every `.md` in it, sequentially,
  continuing past per-file failures
- Duplex-printing support (`pageBreaks.duplex`)

## Installation

This repository is local-only: the module path `github.com/trinova/md2pdf` has
no public remote, so `go install github.com/trinova/md2pdf@latest` does **not**
work. Install from a checkout of this repository:

```sh
cd md2pdf   # this repository's root
go install .
```

The picoloom dependency is satisfied by the `replace` directive in `go.mod`,
which points at the vendored copy in `./alnah:picoloom` (see
[Vendored library](#vendored-library-adr-001)). Requires Go 1.25+.

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

## Configuration

`md2pdf init` writes a template documenting every option (the same content as
`all-options.yaml` in this repository). Highlights:

- `style` — theme name or path to a custom `.css` file
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
  `watermark.text`, which sets the text **and** enables the watermark for that
  document even when the config leaves the watermark off.
- Unknown keys are ignored; run with `--verbose` to list them and catch typos.
- `document.date: "auto"` resolves to today's date in long format.

In batch mode each file's frontmatter overrides the shared config for that
file only.

## Vendored library (ADR-001)

Upstream renamed to picoloom (module `github.com/alnah/picoloom/v2`). This
repository vendors a fork at `./alnah:picoloom`, wired in via a `replace`
directive in `go.mod`, and carries a local patch stack rebased onto upstream's
`origin/main` (`git log origin/main..main` in that directory is authoritative
— currently 5 commits):

1. fix: keep pre-numbered headings from double numbering in TOC
2. feat: embed PDF document outline from headings
3. feat: add TOC.DisableNumbering to list headings verbatim
4. fix: no blank page after cover/TOC when BeforeH1 is set
5. feat: duplex option keeps cover and TOC on their own sheet

Sync procedure:

```sh
cd alnah:picoloom
git fetch origin && git rebase origin/main   # conflicts most likely in internal/pipeline/tocinject.go
go test ./...
cd .. && go install .                        # rebuild the wrapper
```

Long-term exit: upstream these commits as PRs; the rebase then drops them
automatically.

## Transformers

The `transform` package provides a sequential markdown-rewriting pipeline that
runs before conversion, with a disposable per-run workspace
(`--keep-workspace` preserves it for debugging). A Mermaid transformer exists
(`transform/mermaid`, renders ` ```mermaid ` fences to SVG via `mmdc`) but is
**not yet registered** in the pipeline — the pipeline currently runs empty, so
Mermaid blocks still render as code listings.

## Tasks

This project uses [xc](https://github.com/joerdav/xc) as a task runner.

Install xc: `go install github.com/joerdav/xc@latest`

Run tasks from the project root with `xc <task-name>`.

### install-dev

Install from local source into `$GOPATH/bin`.

```sh
go install .
```

### build-dev

Build development binary to `bin/md2pdf-dev`.

```sh
go build -o bin/md2pdf-dev .
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

### clean

Remove build artifacts.

```sh
rm -rf bin/ coverage.out
```

### all

Requires: clean, test, lint, build-dev

Full development cycle.
