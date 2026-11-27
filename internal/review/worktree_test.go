package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWorktreesListsMainAndLinkedWorktrees(t *testing.T) {
	ctx := context.Background()
	mainDir := t.TempDir()
	runGit(t, mainDir, "init", "--quiet", "-b", "main")
	runGit(t, mainDir, "config", "user.name", "Test")
	runGit(t, mainDir, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(mainDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, mainDir, "add", "README.md")
	runGit(t, mainDir, "commit", "--quiet", "-m", "initial")

	linkedDir := filepath.Join(t.TempDir(), "feature-wt")
	runGit(t, mainDir, "worktree", "add", "--quiet", "-b", "feature/ui", linkedDir)

	list, err := Worktrees(ctx, mainDir)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d worktrees (%+v), want 2", len(list), list)
	}
	if !list[0].Current || list[0].Branch != "main" {
		t.Errorf("first worktree = %+v, want current main", list[0])
	}
	if list[1].Current || list[1].Branch != "feature/ui" {
		t.Errorf("second worktree = %+v, want non-current feature/ui", list[1])
	}

	// Querying from the linked worktree marks it as Current instead.
	fromLinked, err := Worktrees(ctx, linkedDir)
	if err != nil {
		t.Fatalf("Worktrees(linked): %v", err)
	}
	if fromLinked[0].Current || !fromLinked[1].Current {
		t.Errorf("current flags from linked = %+v", fromLinked)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
