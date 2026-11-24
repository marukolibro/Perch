package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/internal/session"
)

func newTestDesktop(t *testing.T) (*Desktop, *[]*App) {
	t.Helper()
	d := NewDesktop()
	var focused []*App
	d.newAppWindow = func(root string) *App {
		a := New()
		a.desktop = d
		a.session.dir = d.sessionDir
		if root != "" {
			if err := a.loadFolder(root); err != nil {
				t.Fatalf("loadFolder(%q): %v", root, err)
			}
		}
		d.windows = append(d.windows, a)
		return a
	}
	d.focusWindow = func(a *App) {
		focused = append(focused, a)
	}
	return d, &focused
}

func TestHandleCLIInfo(t *testing.T) {
	var out bytes.Buffer
	if !HandleCLIInfo([]string{"--help"}, "0.1.3", &out) {
		t.Fatal("--help did not report handled")
	}
	if !strings.Contains(out.String(), "Usage: perch") {
		t.Errorf("help output = %q", out.String())
	}

	out.Reset()
	if !HandleCLIInfo([]string{"-v"}, "0.1.3", &out) {
		t.Fatal("-v did not report handled")
	}
	if got := out.String(); got != "Perch 0.1.3\n" {
		t.Errorf("version output = %q, want %q", got, "Perch 0.1.3\n")
	}

	out.Reset()
	if HandleCLIInfo([]string{"view.go:10"}, "0.1.3", &out) {
		t.Fatal("file target was treated as info flag")
	}
}

func TestParseLaunchArgsFilesLinesColsAndFlags(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/app/view.go", "line1\nline2\nline3\n")
	writeFile(t, root, "notes:10", "literal colon file\nsecond line\n")
	writeFile(t, root, "-dash.go", "package dash\n")

	args := []string{
		"-psn_0_12345",
		"-g",
		".",
		"internal/app/view.go",
		"internal/app/view.go:120",
		"internal/app/view.go:120:5",
		"notes:10",
		"notes:10:2",
		"--",
		"-dash.go:1",
		"missing.go:42",
	}
	got := parseLaunchArgs(args, root)
	if len(got) != 8 {
		t.Fatalf("got %d targets (%+v), want 8", len(got), got)
	}

	if !got[0].isDir || got[0].path != root {
		t.Errorf("target[0] = %+v, want dir %q", got[0], root)
	}
	viewPath := filepath.Join(root, "internal", "app", "view.go")
	if got[1].isDir || got[1].path != viewPath || got[1].line != 0 || got[1].col != 0 {
		t.Errorf("target[1] = %+v, want file %q", got[1], viewPath)
	}
	if got[2].path != viewPath || got[2].line != 120 || got[2].col != 0 {
		t.Errorf("target[2] = %+v, want %q:120", got[2], viewPath)
	}
	if got[3].path != viewPath || got[3].line != 120 || got[3].col != 5 {
		t.Errorf("target[3] = %+v, want %q:120:5", got[3], viewPath)
	}
	colonPath := filepath.Join(root, "notes:10")
	if got[4].path != colonPath || got[4].line != 0 {
		t.Errorf("target[4] = %+v, want literal file %q", got[4], colonPath)
	}
	if got[5].path != colonPath || got[5].line != 2 || got[5].col != 0 {
		t.Errorf("target[5] = %+v, want %q:2", got[5], colonPath)
	}
	dashPath := filepath.Join(root, "-dash.go")
	if got[6].path != dashPath || got[6].line != 1 {
		t.Errorf("target[6] = %+v, want %q:1", got[6], dashPath)
	}
	if got[7].err == nil || !strings.HasSuffix(got[7].path, "missing.go") {
		t.Errorf("target[7] = %+v, want missing.go error", got[7])
	}
}

func TestLineColCandidatesKeepsDriveLetters(t *testing.T) {
	if got := lineColCandidates(`C:\`); len(got) != 0 {
		t.Errorf("drive root candidates = %+v", got)
	}
	if got := lineColCandidates(`C:12`); len(got) != 0 {
		t.Errorf("drive-only prefix candidates = %+v", got)
	}
	got := lineColCandidates(`C:\repo\view.go:120:5`)
	if len(got) != 2 {
		t.Fatalf("candidates = %+v, want 2", got)
	}
	if got[0] != (pathLineCol{path: `C:\repo\view.go`, line: 120, col: 5}) {
		t.Errorf("first candidate = %+v", got[0])
	}
	if got[1] != (pathLineCol{path: `C:\repo\view.go:120`, line: 5}) {
		t.Errorf("second candidate = %+v", got[1])
	}
}

func TestProjectRootForFileFindsGitOrParent(t *testing.T) {
	root := t.TempDir()
	// Standard repo with .git directory.
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoFile := filepath.Join(repo, "internal", "app", "view.go")
	writeFile(t, repo, "internal/app/view.go", "package app\n")
	if got := projectRootForFile(repoFile); got != repo {
		t.Errorf("git dir root = %q, want %q", got, repo)
	}

	// Worktree with .git file.
	wt := filepath.Join(root, "worktree")
	writeFile(t, wt, ".git", "gitdir: ../repo/.git/worktrees/wt\n")
	wtFile := filepath.Join(wt, "sub", "file.go")
	writeFile(t, wt, "sub/file.go", "package sub\n")
	if got := projectRootForFile(wtFile); got != wt {
		t.Errorf("git file root = %q, want %q", got, wt)
	}

	// Plain directory without .git falls back to the file's parent directory.
	plain := filepath.Join(root, "plain", "nested")
	plainFile := filepath.Join(plain, "note.txt")
	writeFile(t, plain, "note.txt", "hello\n")
	if got := projectRootForFile(plainFile); got != plain {
		t.Errorf("plain root = %q, want %q", got, plain)
	}
}

func TestPositionForLineColHandlesUTF8AndClamping(t *testing.T) {
	buf := editor.NewBuffer("alpha\n你好世界\nend\n")
	if got := positionForLineCol(buf, 2, 3); got != (editor.Pos{Line: 1, Col: len("你好")}) {
		t.Errorf("UTF-8 col 3 = %+v, want byte col %d", got, len("你好"))
	}
	if got := positionForLineCol(buf, 999, 1); got.Line != buf.LineCount()-1 || got.Col != 0 {
		t.Errorf("clamped line = %+v", got)
	}
	if got := positionForLineCol(buf, 2, 999); got != (editor.Pos{Line: 1, Col: len("你好世界")}) {
		t.Errorf("clamped col = %+v", got)
	}
}

func TestCLIOpensFileAtLineInExistingAndNewWindows(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "internal/app/view.go", "line1\nline2\nline3\nline4\n")
	writeFile(t, repo, "README.md", "# Title\n\nline3\nline4\nline5\n")

	d, focused := newTestDesktop(t)

	// Start with an empty welcome window open.
	empty := d.openWindow("")

	// Opening a file inside a Git repo reuses the empty window and opens the repo root.
	d.openLaunch([]string{"internal/app/view.go:3:4"}, repo)
	if len(d.windows) != 1 || d.windows[0] != empty {
		t.Fatalf("expected empty window to be reused, got %d windows", len(d.windows))
	}
	if empty.root != repo {
		t.Fatalf("root = %q, want %q", empty.root, repo)
	}
	doc := empty.activeDocument()
	if doc == nil || doc.path != "internal/app/view.go" {
		t.Fatalf("active doc = %+v", doc)
	}
	if got := doc.editor.CursorPos(); got != (editor.Pos{Line: 2, Col: 3}) {
		t.Errorf("cursor = %+v, want line 2 col 3", got)
	}

	// Edit the open tab, open a modal overlay, then relaunch with a new line on the same file.
	appendText(doc, "line5\n")
	empty.settingsOpen = true
	d.openLaunch([]string{"internal/app/view.go:5"}, repo)
	if len(d.windows) != 1 {
		t.Fatalf("existing window duplicated: %d windows", len(d.windows))
	}
	if empty.settingsOpen {
		t.Error("settings overlay remained open after CLI file jump")
	}
	if !doc.modified() {
		t.Error("unsaved edit was lost when jumping to line in open tab")
	}
	if got := doc.editor.CursorPos(); got != (editor.Pos{Line: 4, Col: 0}) {
		t.Errorf("cursor after second jump = %+v, want line 4 col 0", got)
	}

	// Markdown without line stays in preview; with line switches to source.
	d.openLaunch([]string{"README.md"}, repo)
	md := empty.activeDocument()
	if md == nil || md.path != "README.md" || !md.showsPreview() {
		t.Fatalf("README.md without line should stay in preview, got %+v", md)
	}
	d.openLaunch([]string{"README.md:4"}, repo)
	if md.showsPreview() {
		t.Error("README.md:4 did not switch to source view")
	}
	if got := md.editor.CursorPos(); got != (editor.Pos{Line: 3, Col: 0}) {
		t.Errorf("README.md cursor = %+v, want line 3 col 0", got)
	}
	if len(*focused) == 0 || (*focused)[len(*focused)-1] != empty {
		t.Error("target window was not brought to front")
	}
}

func TestCLIMultiArgsNestedWindowsAndZeroWindows(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	other := t.TempDir()
	writeFile(t, child, "a.go", "a1\na2\na3\n")
	writeFile(t, child, "b.go", "b1\nb2\nb3\n")
	writeFile(t, other, "c.go", "c1\nc2\n")

	d, focused := newTestDesktop(t)

	// No windows open: bare launch opens a welcome window and focuses it.
	d.openLaunch(nil, parent)
	if len(d.windows) != 1 || d.windows[0].root != "" {
		t.Fatalf("bare launch with 0 windows gave %d windows", len(d.windows))
	}

	// Open parent and child windows, then open a file inside child: deepest window wins.
	d.openLaunch([]string{parent, child}, parent)
	if len(d.windows) != 2 {
		t.Fatalf("expected 2 windows (reusing welcome + 1 new), got %d", len(d.windows))
	}
	childWin := d.windows[1]

	// Multi-arg: open two files in child in one command.
	d.openLaunch([]string{"a.go:2", "b.go:3:2"}, child)
	if len(d.windows) != 2 {
		t.Fatalf("multi-arg inside open window created extra window: %d", len(d.windows))
	}
	if len(childWin.documents) != 2 {
		t.Fatalf("expected 2 open tabs in child window, got %d", len(childWin.documents))
	}
	if got := childWin.activeDocument(); got == nil || got.path != "b.go" {
		t.Fatalf("active doc = %+v, want b.go", got)
	}
	if pos := childWin.activeDocument().editor.CursorPos(); pos != (editor.Pos{Line: 2, Col: 1}) {
		t.Errorf("b.go cursor = %+v, want line 2 col 1", pos)
	}

	// Opening a file in another folder opens a new window without replacing existing ones.
	d.openLaunch([]string{filepath.Join(other, "c.go") + ":2"}, parent)
	if len(d.windows) != 3 {
		t.Fatalf("expected 3rd window for other folder, got %d", len(d.windows))
	}
	otherWin := d.windows[2]
	if otherWin.root != other || otherWin.activeDocument() == nil || otherWin.activeDocument().path != "c.go" {
		t.Fatalf("other window state unexpected: root=%q doc=%+v", otherWin.root, otherWin.activeDocument())
	}
	if (*focused)[len(*focused)-1] != otherWin {
		t.Error("new window was not focused")
	}
}

func TestCLISymlinkMatchesExistingWindow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks may require elevated privileges on Windows")
	}
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	linkDir := filepath.Join(base, "link")
	writeFile(t, realDir, "main.go", "package main\nfunc main() {}\n")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}

	d, _ := newTestDesktop(t)
	d.openLaunch([]string{realDir}, base)
	d.openLaunch([]string{filepath.Join(linkDir, "main.go") + ":2"}, base)
	if len(d.windows) != 1 {
		t.Fatalf("symlink launch opened duplicate window: %d", len(d.windows))
	}
	doc := d.windows[0].activeDocument()
	if doc == nil || doc.path != "main.go" || doc.editor.CursorPos().Line != 1 {
		t.Fatalf("symlink file open failed: doc=%+v", doc)
	}
}

func TestCLIOpenSurvivesAsyncSessionRestore(t *testing.T) {
	root := t.TempDir()
	sessionDir := t.TempDir()
	writeFile(t, root, "old.go", "old1\nold2\n")
	writeFile(t, root, "target.go", "t1\nt2\nt3\nt4\n")

	// Save a previous session where old.go was active and target.go was at line 0.
	saved := session.Session{
		Version: session.Version,
		Active:  0,
		Tabs: []session.Tab{
			{Path: "old.go", Line: 1},
			{Path: "target.go", Line: 0, Scroll: 100},
		},
	}
	if err := session.Save(session.Path(sessionDir, root), saved); err != nil {
		t.Fatal(err)
	}

	a, tester, posted := openSessionProject(t, root, sessionDir)
	// CLI opens target.go at line 4 right after loadFolder starts async restore.
	a.openFileAtLine("target.go", 4, 2)
	waitForSession(t, a, tester, posted, 2)

	doc := a.activeDocument()
	if doc == nil || doc.path != "target.go" {
		t.Fatalf("session restore stole active tab: got %+v", doc)
	}
	if pos := doc.editor.CursorPos(); pos != (editor.Pos{Line: 3, Col: 1}) {
		t.Errorf("session restore overwrote caret: got %+v, want line 3 col 1", pos)
	}
	if doc.scrollPending {
		t.Error("restored scrollPending was not cleared for CLI line jump")
	}
}

func TestCLIInstallAndUninstallScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher script test")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "perch")
	exe := "/Applications/Perch Dev/It's Perch.app/Contents/MacOS/Perch"

	if err := installCLITo(target, exe); err != nil {
		t.Fatalf("installCLITo failed: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("installed script is not executable: %v", info.Mode())
	}
	content, managed := isPerchManagedScript(target)
	if !managed {
		t.Fatal("installed script missing Perch marker")
	}
	wantBin := "BIN='/Applications/Perch Dev/It'\\''s Perch.app/Contents/MacOS/Perch'"
	if !strings.Contains(content, wantBin) {
		t.Errorf("script missing safely quoted BIN line:\n%s", content)
	}

	// Updating an existing Perch-managed script succeeds.
	if err := installCLITo(target, "/Applications/Perch.app/Contents/MacOS/Perch"); err != nil {
		t.Fatalf("updating managed script failed: %v", err)
	}

	// Uninstalling removes the Perch-managed script.
	if err := uninstallCLIFrom(target); err != nil {
		t.Fatalf("uninstallCLIFrom failed: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("script still exists after uninstall: %v", err)
	}

	// Refuses to overwrite or delete an unrelated binary.
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho custom\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installCLITo(target, exe); err == nil {
		t.Error("expected installCLITo to refuse overwriting non-Perch file")
	}
	if err := uninstallCLIFrom(target); err == nil {
		t.Error("expected uninstallCLIFrom to refuse deleting non-Perch file")
	}
}

func TestPerchMenuIncludesCLIAction(t *testing.T) {
	d := NewDesktop()
	perchItems := d.menu().Items()[0].Submenu
	found := false
	for _, item := range perchItems {
		if strings.Contains(item.Label, "'perch'") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Perch menu missing CLI install/uninstall item: %+v", perchItems)
	}
}
