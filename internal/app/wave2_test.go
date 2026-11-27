package app

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/marukolibro/Perch/internal/review"
	"github.com/marukolibro/Perch/internal/settings"
	"github.com/marukolibro/Perch/locales"
)

func TestChangeTreeCompactsFoldersAndMarksDirectoryReviewed(t *testing.T) {
	changes := []review.Change{
		{Path: "README.md", Kind: review.Modified},
		{Path: "internal/app/a.go", Kind: review.Modified},
		{Path: "internal/app/b.go", Kind: review.Added, Reviewed: true},
		{Path: "internal/review/c.go", Kind: review.Modified},
	}

	rows := buildChangeTreeRows(changes, nil)
	// Expected order: internal (dir) -> app (dir) -> a.go, b.go -> review (dir) -> c.go -> README.md.
	var labels []string
	for _, r := range rows {
		if r.isDir {
			labels = append(labels, "dir:"+r.label)
		} else {
			labels = append(labels, "file:"+r.change.Path)
		}
	}
	want := []string{
		"dir:internal",
		"dir:app",
		"file:internal/app/a.go",
		"file:internal/app/b.go",
		"dir:review",
		"file:internal/review/c.go",
		"file:README.md",
	}
	if !slices.Equal(labels, want) {
		t.Fatalf("tree rows = %v, want %v", labels, want)
	}

	// Single-child chains compact into one row (e.g. "pkg/sub/deep").
	compacted := buildChangeTreeRows([]review.Change{{Path: "pkg/sub/deep/x.go"}}, nil)
	if len(compacted) != 2 || !compacted[0].isDir || compacted[0].label != "pkg/sub/deep" || compacted[0].dir != "pkg/sub/deep" {
		t.Fatalf("compacted = %+v", compacted)
	}

	// Collapsing a folder hides its descendants.
	collapsed := buildChangeTreeRows(changes, map[string]bool{"internal/app": true})
	for _, r := range collapsed {
		if !r.isDir && (r.change.Path == "internal/app/a.go" || r.change.Path == "internal/app/b.go") {
			t.Errorf("collapsed folder still showed %s", r.change.Path)
		}
	}
}

func TestChangesUnreadFilterAndDirBatchReview(t *testing.T) {
	root := reviewedRepository(t)
	writeFile(t, root, "pkg/one.go", "package pkg\n")
	writeFile(t, root, "pkg/two.go", "package pkg\n")
	writeFile(t, root, "top.go", "package top\n")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "--quiet", "-m", "seed")

	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	tester.Frame()
	a.showChanges()
	settleChanges(t, a, tester, posted)

	writeFile(t, root, "pkg/one.go", "package pkg\n// changed\n")
	writeFile(t, root, "pkg/two.go", "package pkg\n// changed\n")
	writeFile(t, root, "top.go", "package top\n// changed\n")
	a.refreshChanges()
	settleChanges(t, a, tester, posted)

	// Mark entire pkg directory reviewed.
	a.review.treeView = true
	a.review.unreadOnly = true
	a.setDirReviewed("pkg", true)
	settleChanges(t, a, tester, posted)
	tester.Frame()

	shown := a.review.visibleChanges()
	if len(shown) != 1 || shown[0].Path != "top.go" {
		t.Fatalf("visible unread changes = %+v, want [top.go]", shown)
	}

	// Marking the remaining file reviewed leaves Finish Review available.
	a.setReviewed("top.go", true)
	settleChanges(t, a, tester, posted)
	tester.Frame()
	if len(a.review.visibleChanges()) != 0 {
		t.Fatalf("expected 0 visible unread changes, got %+v", a.review.visibleChanges())
	}
	if !tester.HasText(locales.AllFilesReviewed()) || !tester.HasText(locales.FinishReview()) {
		t.Errorf("expected all-reviewed message and Finish Review button; texts %q", tester.Texts())
	}
}

func TestToggleDiffLayoutSwitchesUnifiedAndSplit(t *testing.T) {
	a := New()
	if a.settings.DiffLayout != settings.DiffUnified {
		t.Fatalf("initial layout = %q, want unified", a.settings.DiffLayout)
	}
	a.toggleDiffLayout()
	if a.settings.DiffLayout != settings.DiffSplit {
		t.Fatalf("after first toggle = %q, want split", a.settings.DiffLayout)
	}
	a.toggleDiffLayout()
	if a.settings.DiffLayout != settings.DiffUnified {
		t.Fatalf("after second toggle = %q, want unified", a.settings.DiffLayout)
	}
}

func TestFindInFilesAppliesIncludeAndExcludeFilters(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/app/main.go", "package app\nconst targetWord = 1\n")
	writeFile(t, root, "internal/app/main_test.go", "package app\nconst targetWord = 2\n")
	writeFile(t, root, "docs/guide.md", "targetWord in docs\n")

	a, tester, posted := openWithPosts(t, root)
	a.after = func(_ time.Duration, action func()) { action() }
	a.showFindInFiles()
	a.find.query = "targetWord"
	a.find.include = "internal/app"
	a.find.exclude = "*_test.go"
	a.startSearch()
	followPosts(t, posted, tester, func() bool { return a.find.finished })
	tester.Frame()

	if a.find.summary.Files != 1 || a.find.summary.Hits != 1 {
		t.Fatalf("filtered summary = %+v, want 1 file / 1 hit", a.find.summary)
	}
	if len(a.find.rows) == 0 || a.find.rows[0].path != "internal/app/main.go" {
		t.Fatalf("rows = %+v, want internal/app/main.go", a.find.rows)
	}

	// Invalid glob reports InvalidPathFilter without running a search.
	a.find.include = "[bad"
	a.startSearch()
	if a.find.failure != locales.InvalidPathFilter() {
		t.Fatalf("invalid filter failure = %q, want %q", a.find.failure, locales.InvalidPathFilter())
	}
}

func TestGitPageListsLinkedWorktrees(t *testing.T) {
	root := reviewedRepository(t)
	linked := filepath.Join(t.TempDir(), "wt-second")
	gitIn(t, root, "worktree", "add", "--quiet", "-b", "feature/wt", linked)

	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	tester.Frame()
	a.showChanges()
	settleChanges(t, a, tester, posted)
	tester.Frame()

	if len(a.review.worktrees) != 2 {
		t.Fatalf("worktrees = %+v, want 2", a.review.worktrees)
	}
	a.review.worktreesOpen = true
	tester.Frame()
	if !tester.HasText("feature/wt") {
		t.Errorf("worktree popover missing feature/wt; texts %q", tester.Texts())
	}
}
