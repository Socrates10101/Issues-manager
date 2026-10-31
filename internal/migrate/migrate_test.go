package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Socrates10101/issues-manager/internal/store"
)

const legacyActive = `---
project: homelab
---
# Issue 008: WAF カスタムルール (Phase2-C)

## 報告日
2026-06-20

## ステータス

解決済（2026-06-28 apply・実機検証済）

## 概要

CF Access の手前の edge に軽い WAF を追加。
`

const legacyNew = `---
project: dotfiles
---
# Issue 030: desubmodule dotfiles

## ステータス
**新規**

## 概要
本文。
`

func TestRunMigration(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "homelab", "008-waf-custom-rules.md"), legacyActive)
	mustWrite(t, filepath.Join(src, "dotfiles", "030-desubmodule-dotfiles.md"), legacyNew)
	mustWrite(t, filepath.Join(src, "archive", "dotfiles", "006-jj-diff-viewer.md"),
		"---\nproject: dotfiles\n---\n# Issue 006: jj diff viewer\n\n## ステータス\n解決済\n")

	dst := t.TempDir()
	st, err := store.New(dst, func() string { return "2026-06-28" })
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(src, st)
	if err != nil {
		t.Fatal(err)
	}
	if res.Active != 2 || res.Archived != 1 {
		t.Fatalf("counts: active=%d archived=%d", res.Active, res.Archived)
	}

	got, err := st.Get("homelab", 8)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "WAF カスタムルール (Phase2-C)" {
		t.Fatalf("title: %q", got.Title)
	}
	if got.Status != "解決済" {
		t.Fatalf("status: %q", got.Status)
	}
	if got.Created != "2026-06-20" {
		t.Fatalf("created: %q", got.Created)
	}
	// preserved slug
	if _, err := os.Stat(filepath.Join(dst, "homelab", "008-waf-custom-rules.md")); err != nil {
		t.Fatalf("slug not preserved: %v", err)
	}

	nw, _ := st.Get("dotfiles", 30)
	if nw.Status != "新規" {
		t.Fatalf("new status: %q", nw.Status)
	}

	arch, _ := st.Get("dotfiles", 6)
	if !arch.Archived {
		t.Fatalf("expected archived issue")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
