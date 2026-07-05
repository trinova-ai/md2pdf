package main

// Frontmatter writer/merger for the `md2pdf frontmatter` subcommand (G7.2):
// pure functions that move document-specific metadata from a config into the
// document's own frontmatter block. The merge is render-neutral by
// construction — frontmatter already outranks the config (data priority), so
// adding a key the document lacks changes nothing about the produced PDF,
// and keys the document already carries are never touched.

import (
	"bytes"
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

// migratedConfigKeys returns the subset of the config's eligible keys (the
// keys of values) that the merged document's frontmatter now carries WITH a
// value — the keys safe to strip from the config. A key just added by the
// merge always qualifies; a key the document already carried with its own
// value qualifies too, because frontmatter outranks the config even when the
// two values differ — stripping the config's copy is render-neutral either
// way. A key the document carries only as an empty scaffold (`author.name:`
// or `author.name: ""`) does NOT override the config and must stay.
func migratedConfigKeys(mergedDoc string, values map[string]string) ([]string, error) {
	_, fm, _, err := extractFrontmatter(mergedDoc)
	if err != nil {
		return nil, err
	}
	var keys []string
	for key := range values {
		if fm[key] != "" {
			keys = append(keys, key)
		}
	}
	sortFrontmatterKeys(keys)
	return keys, nil
}

// stripConfigKeys removes the given dotted keys (section.field) from the raw
// config YAML via the yaml.v3 node API, so comments and the ordering of every
// untouched setting survive the rewrite. A comment on a removed entry is
// removed with it — it documented that setting. A section mapping emptied by
// the removals (`document:`, `author:`) is dropped too; a head comment sitting
// directly on the dropped section key is transferred to the next top-level key
// so a file banner cannot vanish (a banner separated by a blank line attaches
// to the document node and is never at risk). removed lists the keys actually
// deleted; when it is empty the input bytes come back byte-for-byte unchanged
// — no re-encoding, no formatting normalization.
func stripConfigKeys(data []byte, keys []string) (out []byte, removed []string, err error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parsing config: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return data, nil, nil // empty config: nothing to strip
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return data, nil, nil
	}

	for _, key := range keys {
		section, field, ok := strings.Cut(key, ".")
		if !ok {
			continue
		}
		si := findMapEntry(root, section)
		if si < 0 || root.Content[si+1].Kind != yaml.MappingNode {
			continue
		}
		sec := root.Content[si+1]
		fi := findMapEntry(sec, field)
		if fi < 0 {
			continue
		}
		sec.Content = append(sec.Content[:fi], sec.Content[fi+2:]...)
		removed = append(removed, key)
		if len(sec.Content) == 0 {
			dropMapEntry(root, si)
		}
	}
	if len(removed) == 0 {
		return data, nil, nil
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, nil, fmt.Errorf("re-encoding config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, nil, fmt.Errorf("re-encoding config: %w", err)
	}
	return buf.Bytes(), removed, nil
}

// findMapEntry returns the Content index of the key node named key in a
// mapping node, or -1. Mapping Content alternates key, value.
func findMapEntry(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// dropMapEntry deletes the key/value pair at key index i, transferring the
// removed key's head comment to the following key (prepended, blank line
// between) so a banner sitting directly on the first section survives its
// removal. When the removed pair is the last one the comment has no anchor
// left and goes with it.
func dropMapEntry(mapping *yaml.Node, i int) {
	head := mapping.Content[i].HeadComment
	mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
	if head == "" || i >= len(mapping.Content) {
		return
	}
	next := mapping.Content[i]
	if next.HeadComment != "" {
		head += "\n\n" + next.HeadComment
	}
	next.HeadComment = head
}
