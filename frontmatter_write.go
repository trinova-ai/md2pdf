package main

// Frontmatter writer/merger for the `md2pdf frontmatter` subcommand (G7.2):
// pure functions that move document-specific metadata from a config into the
// document's own frontmatter block. The merge is render-neutral by
// construction — frontmatter already outranks the config (data priority), so
// adding a key the document lacks changes nothing about the produced PDF,
// and keys the document already carries are never touched.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontmatterValues collects the eligible frontmatter entries a config
// carries: every key in frontmatterTargets plus mermaid.scale — the same
// single source of truth the reader uses, so the writable and the readable
// key sets cannot drift apart. Empty fields are dropped (nothing to
// migrate). Values are kept verbatim; in particular a date of "auto" stays
// the literal string "auto" — it is resolved at render time, never here.
func frontmatterValues(cfg *Config) map[string]string {
	values := make(map[string]string)
	for key, target := range frontmatterTargets(cfg) {
		if *target != "" {
			values[key] = *target
		}
	}
	if cfg.Mermaid.Scale > 0 {
		values[mermaidScaleKey] = strconv.FormatFloat(cfg.Mermaid.Scale, 'f', -1, 64)
	}
	return values
}

// scaffoldFrontmatterValues returns the empty-value scaffold written when no
// config is given: every document.*/author.* key for the author to fill in.
// The key set is the matching subset of frontmatterTargets — no second list
// to drift. Empty values are render-neutral: applyFrontmatter ignores them.
func scaffoldFrontmatterValues() map[string]string {
	values := make(map[string]string)
	for key := range frontmatterTargets(&Config{}) {
		if strings.HasPrefix(key, "document.") || strings.HasPrefix(key, "author.") {
			values[key] = ""
		}
	}
	return values
}

// sortFrontmatterKeys orders keys for writing: document.* first, then
// author.*, then everything else (watermark.text, footer.text,
// mermaid.scale) — mirroring the config file layout — alphabetically within
// each group. Purely an ordering rule over whatever keys exist; the key set
// itself always comes from frontmatterTargets.
func sortFrontmatterKeys(keys []string) {
	rank := func(key string) int {
		switch {
		case strings.HasPrefix(key, "document."):
			return 0
		case strings.HasPrefix(key, "author."):
			return 1
		default:
			return 2
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if ri, rj := rank(keys[i]), rank(keys[j]); ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
}

// frontmatterValueLine renders one `key: value` line following the parser's
// own rules: mermaid.scale is the one numeric key and is written as a bare
// number; every other value is double-quoted so it always parses back as a
// string ("2.5" stays a version, "auto" stays the literal marker).
// strconv.Quote's escapes (\", \\, \n, \t, \xNN, \uNNNN, \UNNNNNNNN, …) are
// a subset of YAML's double-quoted escapes with identical meaning, so
// parseFrontmatter round-trips the exact value.
func frontmatterValueLine(key, value string) string {
	if key == mermaidScaleKey {
		return key + ": " + value
	}
	return key + ": " + strconv.Quote(value)
}

// splitRawFrontmatter locates an existing frontmatter block without
// interpreting it: inner is the raw text between the --- delimiter lines and
// closeAt is the byte offset where the closing delimiter line starts — the
// insertion point for new keys. Byte offsets, not parsed lines, so callers
// can splice while preserving every existing byte. A document that opens a
// block but never closes it is an error: there is no defined place to write.
func splitRawFrontmatter(content string) (inner string, closeAt int, found bool, err error) {
	first := content
	innerStart := len(content)
	if i := strings.IndexByte(content, '\n'); i >= 0 {
		first = content[:i]
		innerStart = i + 1
	}
	if strings.TrimSpace(first) != "---" {
		return "", 0, false, nil
	}
	for pos := innerStart; pos < len(content); {
		lineEnd := len(content)
		next := lineEnd
		if i := strings.IndexByte(content[pos:], '\n'); i >= 0 {
			lineEnd = pos + i
			next = lineEnd + 1
		}
		if strings.TrimSpace(content[pos:lineEnd]) == "---" {
			return content[innerStart:pos], pos, true, nil
		}
		pos = next
	}
	return "", 0, false, fmt.Errorf("frontmatter: missing closing --- delimiter")
}

// mergeFrontmatter writes the missing entries from values into content's
// frontmatter block, creating the block when the document has none. Keys
// already present in the block always win and are left byte-for-byte
// untouched — known keys, unknown/private keys, and bare `key:` scaffolds
// alike — as is the body: new lines are spliced in just before the closing
// delimiter. added lists the keys actually written, in output order, so the
// caller knows exactly what migrated. New lines follow the block's own line
// endings (CRLF documents stay CRLF).
func mergeFrontmatter(content string, values map[string]string) (merged string, added []string, err error) {
	inner, closeAt, found, err := splitRawFrontmatter(content)
	if err != nil {
		return "", nil, err
	}

	present := make(map[string]bool)
	if found {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(inner), &m); err != nil {
			return "", nil, fmt.Errorf("frontmatter: %w", err)
		}
		for key := range m {
			present[key] = true
		}
	}

	for key := range values {
		if !present[key] {
			added = append(added, key)
		}
	}
	if len(added) == 0 {
		return content, nil, nil
	}
	sortFrontmatterKeys(added)

	region := content
	if found {
		region = content[:closeAt]
	}
	nl := "\n"
	if strings.Contains(region, "\r\n") {
		nl = "\r\n"
	}

	var b strings.Builder
	for _, key := range added {
		b.WriteString(frontmatterValueLine(key, values[key]))
		b.WriteString(nl)
	}

	if found {
		return content[:closeAt] + b.String() + content[closeAt:], added, nil
	}
	block := "---" + nl + b.String() + "---" + nl
	if content != "" {
		block += nl // blank line between the new block and the existing body
	}
	return block + content, added, nil
}
