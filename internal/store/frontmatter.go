package store

import (
	"fmt"
	"strconv"
	"strings"
)

// minimal YAML-frontmatter handling. We own the on-disk format, so a tiny
// `key: value` subset is enough — no external YAML dependency.

// parseFrontmatter splits a markdown document into its frontmatter map and body.
// If the document has no `---` frontmatter block, meta is empty and body is the
// whole input.
func parseFrontmatter(data string) (meta map[string]string, body string) {
	meta = map[string]string{}
	s := strings.ReplaceAll(data, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return meta, s
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		// closing fence at very end without trailing newline
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - len("\n---")
			block := rest[:end]
			parseKV(block, meta)
			return meta, ""
		}
		return meta, s // malformed, treat as no frontmatter
	}
	block := rest[:end]
	body = rest[end+len("\n---\n"):]
	parseKV(block, meta)
	return meta, body
}

func parseKV(block string, meta map[string]string) {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
}

// renderFrontmatter writes an Issue's metadata as a frontmatter block followed
// by the body. Field order is fixed for stable, diff-friendly output.
func renderFrontmatter(i Issue) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "project: %s\n", i.Project)
	fmt.Fprintf(&b, "number: %d\n", i.Number)
	fmt.Fprintf(&b, "title: %s\n", i.Title)
	fmt.Fprintf(&b, "status: %s\n", i.Status)
	if i.Created != "" {
		fmt.Fprintf(&b, "created: %s\n", i.Created)
	}
	if i.Updated != "" {
		fmt.Fprintf(&b, "updated: %s\n", i.Updated)
	}
	b.WriteString("---\n")
	body := i.Body
	if body != "" && !strings.HasPrefix(body, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(body)
	return b.String()
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}
