// Package store is the markdown-backed CRUD layer for issues. The on-disk
// layout is the SSOT:
//
//	<root>/<project>/<NNN>-<slug>.md           active issues
//	<root>/archive/<project>/<NNN>-<slug>.md    archived issues
//
// Each file carries YAML frontmatter (project/number/title/status/created/
// updated) plus a free-form markdown body, so it stays human-readable over ssh
// while remaining structured enough to query.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ResolvedStatus is the only status from which an issue may be archived.
const ResolvedStatus = "解決済"

// ValidStatuses is the issue lifecycle, in order.
var ValidStatuses = []string{"新規", "調査中", "対応中", "修正完了・検証待ち", "解決済"}

var (
	ErrNotFound   = errors.New("issue not found")
	ErrExists     = errors.New("issue already exists")
	ErrBadStatus  = errors.New("invalid status")
	ErrNotResolved = errors.New("issue is not 解決済; cannot archive")
)

// Issue is a single tracked item.
type Issue struct {
	Project  string `json:"project"`
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Created  string `json:"created,omitempty"`
	Updated  string `json:"updated,omitempty"`
	Body     string `json:"body,omitempty"`
	Archived bool   `json:"archived"`
}

// ID returns the canonical "project:NNN" identifier.
func (i Issue) ID() string { return fmt.Sprintf("%s:%03d", i.Project, i.Number) }

// Store is a concurrency-safe handle over the data directory.
type Store struct {
	root string
	mu   sync.Mutex
	// now returns the current date as YYYY-MM-DD; injectable for tests.
	now func() string
}

// New opens (creating if needed) a store rooted at dir.
func New(dir string, now func() string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty data dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() string { return "" }
	}
	return &Store{root: dir, now: now}, nil
}

// Root returns the data directory.
func (s *Store) Root() string { return s.root }

// Filter narrows a List query. Empty fields match everything.
type Filter struct {
	Project  string
	Status   string
	Archived *bool // nil = both, true = only archived, false = only active
}

func (s *Store) activeDir(project string) string  { return filepath.Join(s.root, project) }
func (s *Store) archiveDir(project string) string { return filepath.Join(s.root, "archive", project) }

// List returns issues matching f, sorted by project then number.
func (s *Store) List(f Filter) ([]Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(f)
}

func (s *Store) listLocked(f Filter) ([]Issue, error) {
	var out []Issue
	scan := func(archived bool) error {
		base := s.root
		if archived {
			base = filepath.Join(s.root, "archive")
		}
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			project := e.Name()
			if !archived && project == "archive" {
				continue
			}
			if f.Project != "" && project != f.Project {
				continue
			}
			files, err := os.ReadDir(filepath.Join(base, project))
			if err != nil {
				return err
			}
			for _, fe := range files {
				if fe.IsDir() || !strings.HasSuffix(fe.Name(), ".md") {
					continue
				}
				iss, err := s.readFile(filepath.Join(base, project, fe.Name()), project, archived)
				if err != nil {
					return err
				}
				if f.Status != "" && iss.Status != f.Status {
					continue
				}
				out = append(out, iss)
			}
		}
		return nil
	}
	wantActive := f.Archived == nil || !*f.Archived
	wantArchive := f.Archived == nil || *f.Archived
	if wantActive {
		if err := scan(false); err != nil {
			return nil, err
		}
	}
	if wantArchive {
		if err := scan(true); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Project != out[b].Project {
			return out[a].Project < out[b].Project
		}
		return out[a].Number < out[b].Number
	})
	return out, nil
}

// Get returns a single issue (active first, then archive).
func (s *Store) Get(project string, number int) (Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(project, number)
}

func (s *Store) getLocked(project string, number int) (Issue, error) {
	if p, archived, err := s.findFile(project, number); err == nil {
		return s.readFile(p, project, archived)
	}
	return Issue{}, ErrNotFound
}

// findFile locates the on-disk path for an issue, searching active then archive.
func (s *Store) findFile(project string, number int) (path string, archived bool, err error) {
	for _, a := range []bool{false, true} {
		dir := s.activeDir(project)
		if a {
			dir = s.archiveDir(project)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		prefix := fmt.Sprintf("%03d-", number)
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".md") {
				return filepath.Join(dir, e.Name()), a, nil
			}
		}
	}
	return "", false, ErrNotFound
}

func (s *Store) readFile(path, project string, archived bool) (Issue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Issue{}, err
	}
	meta, body := parseFrontmatter(string(data))
	iss := Issue{
		Project:  project,
		Number:   atoiDefault(meta["number"], numberFromFilename(filepath.Base(path))),
		Title:    meta["title"],
		Status:   meta["status"],
		Created:  meta["created"],
		Updated:  meta["updated"],
		Body:     body,
		Archived: archived,
	}
	if meta["project"] != "" {
		iss.Project = meta["project"]
	}
	return iss, nil
}

// NextNumber returns the next free number in a project (max active+archive + 1).
func (s *Store) NextNumber(project string) (int, error) {
	max := 0
	for _, a := range []bool{false, true} {
		dir := s.activeDir(project)
		if a {
			dir = s.archiveDir(project)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if n := numberFromFilename(e.Name()); n > max {
				max = n
			}
		}
	}
	return max + 1, nil
}

// Create adds a new issue, assigning the next number in the project.
func (s *Store) Create(project, title, body string) (Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if project == "" || title == "" {
		return Issue{}, errors.New("project and title are required")
	}
	n, err := s.NextNumber(project)
	if err != nil {
		return Issue{}, err
	}
	now := s.now()
	iss := Issue{
		Project: project,
		Number:  n,
		Title:   title,
		Status:  "新規",
		Created: now,
		Updated: now,
		Body:    body,
	}
	if err := s.writeLocked(iss, false); err != nil {
		return Issue{}, err
	}
	return iss, nil
}

// Update applies non-nil fields to an existing issue.
func (s *Store) Update(project string, number int, title, status, body *string) (Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	iss, err := s.getLocked(project, number)
	if err != nil {
		return Issue{}, err
	}
	if status != nil {
		if !validStatus(*status) {
			return Issue{}, ErrBadStatus
		}
		iss.Status = *status
	}
	if title != nil {
		iss.Title = *title
	}
	if body != nil {
		iss.Body = *body
	}
	iss.Updated = s.now()
	// Identity is the number, not the slug. Any write (even a status-only
	// change) re-derives the filename from the title, so we must remove any
	// stale NNN-* file whose slug no longer matches — otherwise a slug
	// mismatch (e.g. a migrated issue with a custom slug) silently orphans the
	// old file and the number shows up twice.
	if err := s.writeReplacing(iss, iss.Archived); err != nil {
		return Issue{}, err
	}
	return iss, nil
}

// Archive moves a 解決済 issue into archive/. Refuses any other status.
func (s *Store) Archive(project string, number int) (Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, archived, err := s.findFile(project, number)
	if err != nil {
		return Issue{}, ErrNotFound
	}
	iss, err := s.readFile(old, project, archived)
	if err != nil {
		return Issue{}, err
	}
	if iss.Archived {
		return iss, nil // already archived; idempotent
	}
	if iss.Status != ResolvedStatus {
		return Issue{}, ErrNotResolved
	}
	iss.Archived = true
	iss.Updated = s.now()
	if err := s.writeReplacing(iss, true); err != nil {
		return Issue{}, err
	}
	_ = os.Remove(old) // old lives in the active dir; writeReplacing only swept archive/
	return iss, nil
}

// Import writes an issue verbatim (used by migration); preserves slug if given.
func (s *Store) Import(iss Issue, slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeWithSlug(iss, iss.Archived, slug)
}

func (s *Store) writeLocked(iss Issue, archived bool) error {
	return s.writeWithSlug(iss, archived, "")
}

// writeReplacing writes iss (slug derived from the title) and then removes any
// other NNN-*.md file for the same number in the target dir. This keeps the
// number as the sole identity: a slug change — or a pre-existing duplicate —
// can never leave an orphan behind. The new file is written first, so a failed
// write never destroys the existing one.
func (s *Store) writeReplacing(iss Issue, archived bool) error {
	if err := s.writeWithSlug(iss, archived, ""); err != nil {
		return err
	}
	dir := s.activeDir(iss.Project)
	if archived {
		dir = s.archiveDir(iss.Project)
	}
	keep := fmt.Sprintf("%03d-%s.md", iss.Number, slugify(iss.Title))
	prefix := fmt.Sprintf("%03d-", iss.Number)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // write succeeded; cleanup is best-effort
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == keep {
			continue
		}
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".md") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}

func (s *Store) writeWithSlug(iss Issue, archived bool, slug string) error {
	dir := s.activeDir(iss.Project)
	if archived {
		dir = s.archiveDir(iss.Project)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if slug == "" {
		slug = slugify(iss.Title)
	}
	name := fmt.Sprintf("%03d-%s.md", iss.Number, slug)
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(renderFrontmatter(iss)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var (
	numRe   = regexp.MustCompile(`^(\d+)-`)
	slugBad = regexp.MustCompile(`[\s/\\:*?"<>|]+`)
)

func numberFromFilename(name string) int {
	m := numRe.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// slugify makes a filesystem-safe slug, lowercasing ASCII and preserving
// non-ASCII (e.g. Japanese) runes. Falls back to "issue" if empty.
func slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = slugBad.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		return "issue"
	}
	return s
}

func validStatus(st string) bool {
	for _, v := range ValidStatuses {
		if v == st {
			return true
		}
	}
	return false
}
