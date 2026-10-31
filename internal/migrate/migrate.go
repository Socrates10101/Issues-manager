// Package migrate is a one-off importer for a legacy, pre-frontmatter layout
// (frontmatter with only `project:`, title in a `# Issue NNN:` heading, status
// in a `## ステータス` section). It is no longer wired into the CLI and is kept
// only as an example of using store.Import for bulk imports. History is
// intentionally dropped; only the current state of each file is copied.
package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Socrates10101/issues-manager/internal/store"
)

// Result summarizes a migration run.
type Result struct {
	Active   int
	Archived int
	Files    []string // imported issue IDs
}

var headingRe = regexp.MustCompile(`(?m)^#\s+Issue\s+(\d+)\s*[:：]\s*(.+?)\s*$`)

// Run walks `from` and imports every NNN-*.md into st.
func Run(from string, st *store.Store) (Result, error) {
	var res Result

	// active: <from>/<project>/NNN-*.md  (project != "archive")
	// archived: <from>/archive/<project>/NNN-*.md
	importDir := func(base string, archived bool) error {
		projects, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, p := range projects {
			if !p.IsDir() {
				continue
			}
			project := p.Name()
			if !archived && project == "archive" {
				continue
			}
			files, err := os.ReadDir(filepath.Join(base, project))
			if err != nil {
				return err
			}
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
					continue
				}
				iss, slug, err := parseLegacy(filepath.Join(base, project, f.Name()), project, archived)
				if err != nil {
					return fmt.Errorf("%s/%s: %w", project, f.Name(), err)
				}
				if err := st.Import(iss, slug); err != nil {
					return err
				}
				res.Files = append(res.Files, iss.ID())
				if archived {
					res.Archived++
				} else {
					res.Active++
				}
			}
		}
		return nil
	}

	if err := importDir(from, false); err != nil {
		return res, err
	}
	if err := importDir(filepath.Join(from, "archive"), true); err != nil {
		return res, err
	}
	return res, nil
}

func parseLegacy(path, project string, archived bool) (store.Issue, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return store.Issue{}, "", err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	// strip legacy frontmatter (we only need project from it, which we already
	// have from the directory; prefer the frontmatter value if present).
	if fp := frontmatterProject(text); fp != "" {
		project = fp
	}
	body := stripFrontmatter(text)

	name := filepath.Base(path)
	number := numberFromFilename(name)
	slug := slugFromFilename(name)

	title := fmt.Sprintf("Issue %d", number)
	if m := headingRe.FindStringSubmatch(text); m != nil {
		if n, e := strconv.Atoi(m[1]); e == nil {
			number = n
		}
		title = strings.TrimSpace(m[2])
	}

	iss := store.Issue{
		Project:  project,
		Number:   number,
		Title:    title,
		Status:   normalizeStatus(sectionValue(text, "ステータス")),
		Created:  firstDate(sectionValue(text, "報告日")),
		Updated:  firstDate(sectionValue(text, "報告日")),
		Body:     strings.TrimLeft(body, "\n"),
		Archived: archived,
	}
	return iss, slug, nil
}

func frontmatterProject(text string) string {
	if !strings.HasPrefix(text, "---\n") {
		return ""
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "project" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func stripFrontmatter(text string) string {
	if !strings.HasPrefix(text, "---\n") {
		return text
	}
	rest := text[4:]
	if idx := strings.Index(rest, "\n---\n"); idx >= 0 {
		return rest[idx+5:]
	}
	return text
}

// sectionValue returns the first non-empty line under a `## <name>` heading.
func sectionValue(text, name string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		h := strings.TrimSpace(line)
		if strings.HasPrefix(h, "## ") && strings.TrimSpace(strings.TrimPrefix(h, "## ")) == name {
			for _, next := range lines[i+1:] {
				t := strings.TrimSpace(next)
				if t == "" {
					continue
				}
				if strings.HasPrefix(t, "#") {
					break
				}
				return t
			}
		}
	}
	return ""
}

// normalizeStatus maps a raw status line ("**解決済**", "解決済（…）", "新規") to a
// canonical lifecycle value.
func normalizeStatus(raw string) string {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "*", ""))
	// drop any parenthetical suffix
	for _, sep := range []string{"（", "(", " ", "\t"} {
		if idx := strings.Index(s, sep); idx >= 0 {
			s = s[:idx]
		}
	}
	s = strings.TrimSpace(s)
	for _, v := range store.ValidStatuses {
		if s == v {
			return v
		}
	}
	// best-effort prefix match against the cleaned raw
	clean := strings.TrimSpace(strings.ReplaceAll(raw, "*", ""))
	for _, v := range store.ValidStatuses {
		if strings.HasPrefix(clean, v) {
			return v
		}
	}
	if s == "" {
		return "新規"
	}
	return s
}

var dateRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

func firstDate(s string) string {
	if m := dateRe.FindString(s); m != "" {
		return m
	}
	return ""
}

var numFnRe = regexp.MustCompile(`^(\d+)-(.+)\.md$`)

func numberFromFilename(name string) int {
	if m := numFnRe.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func slugFromFilename(name string) string {
	if m := numFnRe.FindStringSubmatch(name); m != nil {
		return m[2]
	}
	return strings.TrimSuffix(name, ".md")
}
