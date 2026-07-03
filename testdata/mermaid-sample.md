# Mermaid sample

This document proves the mermaid transformer end-to-end: the fence below must
render as an embedded SVG diagram in the PDF, not as a code listing.

```mermaid
graph LR
    A[Markdown] --> B[Transformer pipeline]
    B --> C[SVG diagram]
    C --> D[PDF]
```

Text after the diagram, to show normal content is untouched.
