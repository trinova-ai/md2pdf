# trinova-ai/md2pdf — Implementation Plan

A custom CLI and transformation pipeline built as a **separate project** that uses
[`alnah/go-md2pdf`](https://github.com/alnah/go-md2pdf) as a library dependency.
No changes to the upstream library are required.

---

## Motivation

`alnah/go-md2pdf` is a solid Markdown-to-PDF engine, but it treats frontmatter as
throwaway data and offers no hook for custom markdown transformations. Rather than
fork or patch the library, we build a thin orchestration layer on top of it that:

1. **Parses frontmatter** and feeds it into the library's `md2pdf.Input` struct.
2. **Runs a pluggable transformation pipeline** on the markdown *before* handing
   it to the library (e.g., render a `mermaid` code block to SVG and replace it
   with an image reference).
3. **Manages a temp workspace** for intermediate artifacts (generated SVGs, etc.).
4. **Loads a config file** for organizational defaults (logo, company, style)
   that rarely change, keeping per-document metadata in frontmatter where it
   belongs.

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│  trinova-ai/md2pdf  (this project)                       │
│                                                          │
│  ┌────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │ Config     │  │ Frontmatter  │  │ Transformer      │  │
│  │ Loader     │  │ Parser       │  │ Pipeline         │  │
│  │ (YAML)     │  │ (YAML)       │  │ (pluggable)      │  │
│  └─────┬──────┘  └──────┬───────┘  └────────┬─────────┘  │
│        │                │                    │            │
│        ▼                ▼                    ▼            │
│  ┌─────────────────────────────────────────────────────┐  │
│  │                  Orchestrator                       │  │
│  │  config + frontmatter → md2pdf.Input                │  │
│  │  raw markdown → transformers → cleaned markdown     │  │
│  │  cleaned markdown → md2pdf.Convert()                │  │
│  └──────────────────────┬──────────────────────────────┘  │
│                         │                                 │
│                         ▼                                 │
│  ┌─────────────────────────────────────────────────────┐  │
│  │  alnah/go-md2pdf  (library, unchanged)              │  │
│  │  md2pdf.NewConverter() → conv.Convert(ctx, input)   │  │
│  └─────────────────────────────────────────────────────┘  │
│                                                          │
│  ┌─────────────────────────────────────────────────────┐  │
│  │  Temp Workspace                                     │  │
│  │  os.MkdirTemp → generated SVGs, images, etc.        │  │
│  │  cleaned up after conversion (or kept with --keep)  │  │
│  └─────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

### Data Priority

```
CLI flags  →  frontmatter  →  config file  →  defaults
 (highest)                                    (lowest)
```

---

## Current State

- No project exists yet. `trinova-ai/md2pdf` is greenfield.
- `alnah/go-md2pdf` is available as a Go library (`go get github.com/alnah/go-md2pdf`).
- The library's `Convert()` accepts an `md2pdf.Input` struct with all metadata
  (Cover, Footer, Signature, etc.) and a raw `Markdown` string.
- The library already strips frontmatter internally during preprocessing, so
  passing markdown that still contains frontmatter is safe — it simply gets
  discarded by the library. We parse it *before* that happens.

## Target State

- `trinova-ai/md2pdf` is a standalone Go module with its own `go.mod`.
- A single `md2pdf` binary that reads `.md` files, applies transformations,
  and produces PDFs using the upstream library.
- Frontmatter in each `.md` file provides per-document metadata.
- A shared config file provides organizational defaults.
- A transformer pipeline allows custom code-block processing (e.g., Mermaid → SVG).

---

## Design Decisions

| Decision | Rationale |
|---|---|
| **Separate repository** (`trinova-ai/md2pdf`) | Keeps upstream library untouched; our custom logic lives in our own codebase |
| **Use library API only** (`md2pdf.NewConverter`, `conv.Convert`) | No dependency on upstream internals; resilient to upstream refactors |
| **Flat frontmatter** (not nested) | Matches conventions of Jekyll, Hugo, etc. Users expect `title:` not `document:\n  title:` |
| **Lenient frontmatter parsing** (unknown fields ignored) | Authors may include fields like `tags:` or `classification:` that aren't mapped |
| **Transformer interface** with registry | Extensible — add new block types (mermaid, plantuml, d2, etc.) without touching core |
| **Temp workspace per conversion** | Transformers that generate files (SVGs, images) need a place to write them; auto-cleaned after |
| **Config file mirrors upstream types** | Our config struct maps 1:1 to `md2pdf.Input` sub-structs; no abstraction mismatch |

---

## Frontmatter → `md2pdf.Input` Field Mapping

| Frontmatter (flat) | Library target |
|---|---|
| `title` | `Input.Cover.Title` |
| `subtitle` | `Input.Cover.Subtitle` |
| `author` | `Input.Cover.Author` |
| `date` | `Input.Cover.Date` + `Input.Footer.Date` |
| `version` | `Input.Cover.Version` + `Input.Footer.Status` |
| `documentID` | `Input.Cover.DocumentID` + `Input.Footer.DocumentID` |
| `documentType` | `Input.Cover.DocumentType` |
| `clientName` | `Input.Cover.ClientName` |
| `projectName` | `Input.Cover.ProjectName` |
| `description` | `Input.Cover.Description` |
| `department` | `Input.Cover.Department` + `Input.Signature.Department` |
| `watermark` | `Input.Watermark.Text` (+ enables watermark) |

Fields *not* in frontmatter (live in config):

| Config field | Why config, not frontmatter |
|---|---|
| `organization` | Company-wide constant |
| `logo` | Company-wide constant |
| `authorTitle` (role) | Rarely changes per document |
| `email`, `phone`, `address` | Personal defaults |
| `style` | Organizational standard |
| `footer.*`, `signature.*`, `toc.*`, `page.*`, `pageBreaks.*` | Layout preferences |

---

## Transformer Pipeline

### Interface

```go
// Transformer processes markdown content before PDF conversion.
// It receives the raw markdown and a workspace directory for intermediate files.
// It returns the transformed markdown (or the original if nothing matched).
type Transformer interface {
    // Name returns a human-readable identifier for logging.
    Name() string

    // Transform processes the markdown content.
    // workDir is a temporary directory for writing intermediate files (SVGs, etc.).
    // sourceDir is the directory of the source .md file (for resolving relative paths).
    Transform(content string, workDir string, sourceDir string) (string, error)
}
```

### Example: Mermaid Transformer

Detects fenced code blocks tagged ` ```mermaid `, renders each to an SVG file in
the temp workspace, and replaces the code block with `![](path/to/generated.svg)`.

```markdown
<!-- input -->
    ```mermaid
    graph LR
        A --> B --> C
    ```

<!-- output after transformation -->
![diagram](/.tmp/workspace-abc123/mermaid-1.svg)
```

Because the library resolves relative image paths via `Input.SourceDir`, the
orchestrator sets `SourceDir` to the temp workspace (or rewrites paths to be
absolute) so that generated images are found during HTML rendering.

### Transformer Registration

```go
pipeline := transform.NewPipeline(
    mermaid.NewTransformer(),    // ```mermaid → SVG
    // plantuml.NewTransformer(), // future
    // d2.NewTransformer(),       // future
)
```

Transformers run sequentially in registration order. Each receives the output of
the previous one.

---

## SMART Objectives

### Objective 1 — Project scaffold and config loader

**Specific:** Create the `trinova-ai/md2pdf` Go module with:

- `go.mod` depending on `github.com/alnah/go-md2pdf`.
- A `config` package with a `Config` struct that maps to `md2pdf.Input` sub-structs
  (Cover, Footer, Signature, Page, Watermark, TOC, PageBreaks) plus organizational
  fields (logo, organization, authorTitle, email, style).
- A `LoadConfig(path string) (*Config, error)` function that reads a YAML file.
- A `ToInput(cfg *Config) md2pdf.Input` function that produces a baseline `md2pdf.Input`.

**Measurable:**

- `LoadConfig` parses a valid YAML config and populates all fields.
- `LoadConfig` returns a clear error for missing or malformed files.
- `ToInput` produces an `md2pdf.Input` where Cover, Footer, Signature, etc.
  are correctly populated from config (nil when not enabled).
- Unit tests cover: valid config, missing file, malformed YAML, partial config.

**Achievable:** ~150 lines of Go + ~100 lines of tests. Standard YAML unmarshalling.

**Relevant:** Foundation — config is loaded before anything else.

**Time-bound:** Delivered as the first working commit with passing tests.

---

### Objective 2 — Frontmatter parser

**Specific:** Create a `frontmatter` package with:

- A `Frontmatter` struct with flat YAML-tagged fields for the 12 mapped fields.
- A `Parse(content string) (*Frontmatter, string, error)` that extracts YAML
  between `---` delimiters, parses it, and returns the remaining markdown.
  Returns `(nil, original, nil)` when no valid frontmatter is present.
- A `Validate() error` method enforcing field length limits.

**Measurable:**

- `Parse` correctly extracts all 12 fields from valid frontmatter.
- `Parse` returns `nil` (no error) for missing, malformed, or empty frontmatter.
- Unknown frontmatter keys (e.g., `tags`, `classification`) are silently ignored.
- Frontmatter inside fenced code blocks is not extracted.
- `Validate` rejects fields exceeding length limits with clear error messages.
- At least 10 test cases: full, partial, empty, absent, malformed, code-block,
  unknown keys, length violations, `date: auto` handling, whitespace edge cases.

**Achievable:** ~80 lines of Go + ~120 lines of tests. Regex extraction + YAML parse.

**Relevant:** Core feature — makes frontmatter usable instead of discarded.

**Time-bound:** Delivered with unit tests as the second commit.

---

### Objective 3 — Config + frontmatter merging into `md2pdf.Input`

**Specific:** Create a `merge` package (or function within `config`) with:

- `Apply(cfg *Config, fm *frontmatter.Frontmatter) md2pdf.Input` that:
  1. Starts from `ToInput(cfg)` as the baseline.
  2. Overlays non-empty frontmatter fields onto the appropriate `md2pdf.Input`
     sub-structs.
  3. Handles the `watermark` field by enabling `Input.Watermark` and setting its
     text.
  4. Handles `date: auto` by resolving to the current date.
  5. Returns the final `md2pdf.Input` ready for `conv.Convert()`.

**Measurable:**

- Config-only: produces correct `md2pdf.Input` from config alone.
- Frontmatter overrides: `title` in frontmatter replaces `Cover.Title` from config.
- Preservation: config fields not present in frontmatter are retained.
- Watermark auto-enable: `watermark: DRAFT` enables watermark and sets text.
- Date resolution: `date: auto` becomes today's date string.
- Nil safety: works when frontmatter is `nil` (no frontmatter in file).

**Achievable:** ~60 lines of Go + ~80 lines of tests. Field-by-field conditional merge.

**Relevant:** Connects objectives 1 and 2 into a usable `md2pdf.Input`.

**Time-bound:** Delivered with unit tests as the third commit.

---

### Objective 4 — Transformer pipeline and temp workspace

**Specific:** Create a `transform` package with:

- The `Transformer` interface (Name, Transform).
- A `Pipeline` struct that holds an ordered list of transformers and runs them
  sequentially.
- A `Workspace` struct that wraps `os.MkdirTemp`, exposes the path, and provides
  a `Cleanup()` method (also usable with `defer`).
- A no-op `Pipeline` (empty transformer list) that passes markdown through unchanged.

**Measurable:**

- `Pipeline.Run(content, workDir, sourceDir)` applies transformers in order.
- Each transformer receives the output of the previous one.
- `Workspace.Dir()` returns a valid temp directory path.
- `Workspace.Cleanup()` removes the directory and all contents.
- An error in any transformer stops the pipeline and returns the error.
- A pipeline with zero transformers returns the original content unchanged.

**Achievable:** ~60 lines of Go + ~80 lines of tests. No external tool dependencies
for the pipeline itself — individual transformers bring their own.

**Relevant:** Enables the extensible processing the user needs (Mermaid, PlantUML, etc.).

**Time-bound:** Delivered with unit tests as the fourth commit. Actual transformer
implementations (Mermaid, etc.) are separate objectives.

---

### Objective 5 — Orchestrator and CLI

**Specific:** Create the main orchestrator in `cmd/md2pdf/` that ties everything together:

1. Parse CLI flags (input path, config path, output path, `--keep-workspace`).
2. Load config via `config.LoadConfig()`.
3. For each input `.md` file:
   a. Read the file.
   b. Parse frontmatter via `frontmatter.Parse()`.
   c. Validate frontmatter via `fm.Validate()`.
   d. Create a per-file `md2pdf.Input` via `merge.Apply(cfg, fm)`.
   e. Create a temp workspace.
   f. Run the transformer pipeline on the markdown.
   g. Set `Input.Markdown` to the transformed content.
   h. Set `Input.SourceDir` to the source file's directory (or workspace if
      transformers generated files).
   i. Call `conv.Convert(ctx, input)`.
   j. Write `result.PDF` to the output path.
   k. Clean up the workspace (unless `--keep-workspace`).
4. Support batch mode (directory input).

**Measurable:**

- Single file: `md2pdf report.md` produces `report.pdf` using defaults.
- With config: `md2pdf -c company.yaml report.md` loads org defaults.
- With frontmatter: a file with `title: My Report` in frontmatter gets that title
  on the cover page.
- Batch: `md2pdf ./docs/ -o ./output/` converts all `.md` files.
- `--keep-workspace` preserves the temp directory for debugging.
- Exit code 0 on success, non-zero on failure with clear error messages.

**Achievable:** ~200 lines of Go. Uses `pflag` for CLI flags (same as upstream).

**Relevant:** This is the user-facing entry point — where everything comes together.

**Time-bound:** Delivered as the fifth commit with a working end-to-end flow.

---

### Objective 6 — Mermaid transformer (first concrete transformer)

**Specific:** Create a `transform/mermaid` package that implements `Transformer`:

- Detects ` ```mermaid ` fenced code blocks in the markdown.
- For each block, extracts the Mermaid diagram source.
- Invokes `mmdc` (Mermaid CLI) or a headless rendering approach to produce an SVG.
- Writes the SVG to `{workDir}/mermaid-{n}.svg`.
- Replaces the code block with `![diagram]({workDir}/mermaid-{n}.svg)`.
- Returns an error if `mmdc` is not installed (with a helpful message).

**Measurable:**

- A markdown file with one `mermaid` block produces one SVG and the block is
  replaced with an image reference.
- A markdown file with three `mermaid` blocks produces three SVGs with sequential
  names.
- A markdown file with zero `mermaid` blocks passes through unchanged.
- Non-mermaid code blocks (e.g., ` ```go `) are not affected.
- Missing `mmdc` returns a clear "mermaid CLI not found" error.

**Achievable:** ~100 lines of Go + ~60 lines of tests. Requires `mmdc` installed
on the system (documented as a prerequisite).

**Relevant:** The user's primary use case for the transformer pipeline.

**Time-bound:** Delivered after the pipeline is working (Objective 4), as a separate
commit.

---

### Objective 7 — Documentation and examples

**Specific:**

- Write a `README.md` for `trinova-ai/md2pdf` documenting:
  - Installation and prerequisites (Go, Chrome/Chromium, optional: mmdc).
  - Config file format with annotated example.
  - Frontmatter fields with a full example `.md` file.
  - Priority table: CLI flags → frontmatter → config → defaults.
  - How to write a custom transformer.
  - `--keep-workspace` for debugging transformers.
- Create `examples/company-config.yaml` — shared organizational config.
- Create `examples/report.md` — document with frontmatter + mermaid diagram.

**Measurable:**

- README documents all frontmatter fields in a table.
- README includes a "Writing a Transformer" section with the interface and a
  skeleton implementation.
- Example config and markdown files are internally consistent (no duplicate
  metadata).
- A new user can go from `git clone` to a generated PDF by following the README.

**Achievable:** Documentation only.

**Relevant:** Without docs, the tool is unusable by anyone but the author.

**Time-bound:** Delivered as the final commit.

---

## Dependency Graph

```
Objective 1 (Config)
  └─→ Objective 3 (Merge)
        └─→ Objective 5 (Orchestrator + CLI)
Objective 2 (Frontmatter)        │
  └─→ Objective 3 (Merge)        │
Objective 4 (Pipeline + Workspace)│
  └─→ Objective 5 (Orchestrator)──┘
        └─→ Objective 6 (Mermaid Transformer)
        └─→ Objective 7 (Docs)
```

## Project Layout (Estimated)

```
trinova-ai/md2pdf/
├── go.mod                              # module github.com/trinova-ai/md2pdf
├── go.sum
├── README.md
├── cmd/
│   └── md2pdf/
│       └── main.go                     # CLI entry point + orchestrator
├── config/
│   ├── config.go                       # Config struct, LoadConfig, ToInput
│   └── config_test.go
├── frontmatter/
│   ├── frontmatter.go                  # Frontmatter struct, Parse, Validate
│   └── frontmatter_test.go
├── merge/
│   ├── merge.go                        # Apply(cfg, fm) → md2pdf.Input
│   └── merge_test.go
├── transform/
│   ├── pipeline.go                     # Transformer interface, Pipeline
│   ├── pipeline_test.go
│   ├── workspace.go                    # Temp workspace management
│   ├── workspace_test.go
│   └── mermaid/
│       ├── mermaid.go                  # Mermaid code-block → SVG transformer
│       └── mermaid_test.go
└── examples/
    ├── company-config.yaml             # Shared org config (logo, company, style)
    └── report.md                       # Document with frontmatter + mermaid
```

## Files in `alnah/go-md2pdf` Changed

**None.** The upstream library is used as-is via its public API. No fork, no patch.

---

## Example: End-to-End Flow

### `examples/company-config.yaml` — shared across all documents

```yaml
organization: "Trinova AI"
authorTitle: "Solutions Architect"
email: "team@trinova.ai"
logo: "https://trinova.ai/logo.png"

style: technical

cover:
  enabled: true

toc:
  enabled: true
  title: "Contents"
  maxDepth: 3

signature:
  enabled: true

footer:
  enabled: true
  showPageNumber: true
  position: "right"

page:
  size: a4
  orientation: portrait
  margin: 0.75
```

### `examples/report.md` — per-document metadata in frontmatter

```markdown
---
title: Infrastructure Migration Plan
subtitle: AWS to GCP — Phase 1
author: René
date: auto
version: "0.3"
documentType: Technical Proposal
projectName: Cloud Migration
clientName: Acme Corp
watermark: DRAFT
---

# Infrastructure Migration Plan

## Current Architecture

The system currently runs on AWS with the following topology:

    ```mermaid
    graph TD
        LB[Load Balancer] --> A[Service A]
        LB --> B[Service B]
        A --> DB[(PostgreSQL)]
        B --> DB
    ```

## Migration Steps

1. Provision GCP infrastructure
2. Set up Cloud SQL
3. Migrate data
4. Switch DNS
```

### Command

```bash
md2pdf -c examples/company-config.yaml examples/report.md -o output/
```

### What happens

1. Config loaded → org defaults (Trinova AI, logo, style, etc.).
2. Frontmatter parsed → title, author, date, version, watermark override config.
3. Merged into `md2pdf.Input` → Cover has "Infrastructure Migration Plan" as title,
   "René" as author, "Trinova AI" as organization, today's date, "DRAFT" watermark.
4. Mermaid transformer → code block rendered to SVG in temp workspace.
5. `conv.Convert()` → HTML → PDF.
6. `output/report.pdf` written. Temp workspace cleaned up.
