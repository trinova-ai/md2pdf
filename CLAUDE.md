# md2pdf

Thin Go CLI (`github.com/trinova-ai/md2pdf`) over a **tagged fork** of the
picoloom library (`github.com/trinova-ai/picoloom/v2 vX.Y.Z-trinova.N`) — a
plain require, no replace, no submodule.

Authoritative docs — read before touching the fork workflow:

- `README.md` § "Vendored library (ADR-001)": fork layout, release script,
  sync procedure, dev loop, fresh-checkout bootstrap.
- `PLAN.md` § "Reference": ADRs (vendored fork, dotted frontmatter keys),
  data priority, transformer contract. PLAN.md is driven by `mdplan`
  (`/md:plan` skill); completed phases are the project record.

Rules:

- Never edit `picoloom/` as a side effect of wrapper work — it is a separate
  git repo (untracked here) with its own patch-stack discipline. Library
  changes are commits on its pure `trinova` branch, released via
  `scripts/release-picoloom.sh <tag>`.
- Published fork tags are **immutable** (Go checksum DB) — never re-point
  one; mint the next `-trinova.N`.
- `go.work` is personal and untracked. `GOWORK=off` builds against the
  pinned tag — exactly what users get; release verification must use it.
- Commit emails must be a GitHub noreply address (GH007) — never a private
  one. Read any previously untracked file before `git add`; this repo once
  leaked a sensitive document by treating unread files as fixtures.
- Tasks run via `xc <name>` (see README § Tasks); `xc all` = clean, test,
  lint, install-dev. The full test suite renders real PDFs (needs `mmdc`;
  `go test -short` skips those).
