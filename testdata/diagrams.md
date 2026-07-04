---
document.title: "Diagram Scaling Sample"
document.subtitle: "Wide and narrow Mermaid diagrams"
document.version: "1.0"
document.date: "auto"
mermaid.scale: 0.8
---

# Diagram Scaling Sample

Two diagrams of very different natural widths, with `mermaid.scale` set as a
bare numeric frontmatter key (the only numeric key; everything else must be a
quoted string). Wide diagrams must not be compressed to unreadable symbols
and narrow ones must not blow up to fill the page — both render at their
natural size times the scale factor.

## A wide pipeline

```mermaid
flowchart LR
    A[Ingest] --> B[Validate] --> C[Normalize] --> D[Enrich] --> E[Score]
    E --> F[Route] --> G[Archive]
    E --> H[Alert] --> I[Review]
```

## A narrow decision

```mermaid
flowchart TD
    S[Start] --> Q{Valid?}
    Q -->|yes| P[Process]
    Q -->|no| R[Reject]
```
