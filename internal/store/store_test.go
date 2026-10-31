package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := New(t.TempDir(), func() string { return "2026-06-28" })
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestCreateNumbering(t *testing.T) {
	st := newTestStore(t)
	a, err := st.Create("tools", "first", "body one")
	if err != nil {
		t.Fatal(err)
	}
	if a.Number != 1 || a.Status != "新規" {
		t.Fatalf("got %+v", a)
	}
	b, _ := st.Create("tools", "second", "")
	if b.Number != 2 {
		t.Fatalf("want 2, got %d", b.Number)
	}
	// different project restarts numbering
	c, _ := st.Create("infra", "other", "")
	if c.Number != 1 {
		t.Fatalf("want 1, got %d", c.Number)
	}
}

func TestRoundTripBody(t *testing.T) {
	st := newTestStore(t)
	body := "## 概要\n\n本文テスト\n"
	i, _ := st.Create("infra", "ダッシュボード改善", body)
	got, err := st.Get("infra", i.Number)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got.Body) != strings.TrimSpace(body) {
		t.Fatalf("body mismatch:\n%q\nvs\n%q", got.Body, body)
	}
	if got.Title != "ダッシュボード改善" {
		t.Fatalf("title mismatch: %q", got.Title)
	}
}

func TestArchiveGate(t *testing.T) {
	st := newTestStore(t)
	i, _ := st.Create("tools", "task", "")
	if _, err := st.Archive("tools", i.Number); err != ErrNotResolved {
		t.Fatalf("expected ErrNotResolved, got %v", err)
	}
	resolved := ResolvedStatus
	if _, err := st.Update("tools", i.Number, nil, &resolved, nil); err != nil {
		t.Fatal(err)
	}
	got, err := st.Archive("tools", i.Number)
	if err != nil {
		t.Fatalf("archive after resolve: %v", err)
	}
	if !got.Archived {
		t.Fatal("expected archived")
	}
	// file moved into archive/
	if _, err := os.Stat(filepath.Join(st.Root(), "archive", "tools")); err != nil {
		t.Fatalf("archive dir missing: %v", err)
	}
	// active file gone
	actives, _ := st.List(Filter{Project: "tools", Archived: boolPtr(false)})
	if len(actives) != 0 {
		t.Fatalf("expected 0 active, got %d", len(actives))
	}
}

func TestUpdateBadStatus(t *testing.T) {
	st := newTestStore(t)
	i, _ := st.Create("tools", "task", "")
	bad := "なんか変な状態"
	if _, err := st.Update("tools", i.Number, nil, &bad, nil); err != ErrBadStatus {
		t.Fatalf("expected ErrBadStatus, got %v", err)
	}
}

func TestListFilters(t *testing.T) {
	st := newTestStore(t)
	st.Create("tools", "a", "")
	b, _ := st.Create("tools", "b", "")
	st.Create("infra", "c", "")
	prog := "対応中"
	st.Update("tools", b.Number, nil, &prog, nil)

	all, _ := st.List(Filter{})
	if len(all) != 3 {
		t.Fatalf("want 3, got %d", len(all))
	}
	df, _ := st.List(Filter{Project: "tools"})
	if len(df) != 2 {
		t.Fatalf("want 2 tools issues, got %d", len(df))
	}
	inProg, _ := st.List(Filter{Status: "対応中"})
	if len(inProg) != 1 || inProg[0].Number != b.Number {
		t.Fatalf("status filter failed: %+v", inProg)
	}
}

func TestTitleChangeRenamesFile(t *testing.T) {
	st := newTestStore(t)
	i, _ := st.Create("tools", "old title", "")
	nt := "brand new title"
	if _, err := st.Update("tools", i.Number, &nt, nil, nil); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Join(st.Root(), "tools"))
	if len(files) != 1 {
		t.Fatalf("expected exactly 1 file after rename, got %d", len(files))
	}
	if !strings.Contains(files[0].Name(), "brand-new-title") {
		t.Fatalf("file not renamed: %s", files[0].Name())
	}
}

// A status-only update on an issue whose on-disk slug differs from
// slugify(title) (e.g. migrated with a custom slug) must not leave the old file
// behind — the number must stay unique. Regression test for duplicate
// numbers that appeared after imports with custom slugs.
func TestStatusUpdateSweepsMismatchedSlug(t *testing.T) {
	st := newTestStore(t)
	// Simulate a migrated issue: number 2, title in Japanese, but a custom
	// English slug on disk that slugify(title) would never produce.
	iss := Issue{Project: "infra", Number: 2, Title: "日本語タイトル", Status: "新規"}
	if err := st.Import(iss, "commit-host-slimming-changes"); err != nil {
		t.Fatal(err)
	}
	resolved := ResolvedStatus
	if _, err := st.Update("infra", 2, nil, &resolved, nil); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Join(st.Root(), "infra"))
	if len(files) != 1 {
		var names []string
		for _, f := range files {
			names = append(names, f.Name())
		}
		t.Fatalf("expected exactly 1 file after status update, got %d: %v", len(files), names)
	}
	// And it must be archivable now (single 解決済 file).
	if _, err := st.Archive("infra", 2); err != nil {
		t.Fatalf("archive after resolve: %v", err)
	}
	actives, _ := os.ReadDir(filepath.Join(st.Root(), "infra"))
	if len(actives) != 0 {
		t.Fatalf("expected active dir empty after archive, got %d", len(actives))
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"WAF Custom Rules": "waf-custom-rules",
		"  trim  me  ":     "trim-me",
		"a/b:c*d":          "a-b-c-d",
		"":                 "issue",
		"日本語タイトル":          "日本語タイトル",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q)=%q want %q", in, got, want)
		}
	}
}

func boolPtr(b bool) *bool { return &b }
