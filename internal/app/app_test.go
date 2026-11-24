package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/internal/review"
	"github.com/marukolibro/Perch/internal/session"
	"github.com/marukolibro/Perch/internal/settings"
	"github.com/marukolibro/Perch/internal/workspace"
	"github.com/marukolibro/Perch/locales"
)

const (
	testWidth  int = 1100
	testHeight int = 700
)

// TestMain runs the tests in English whatever language the machine reads.
func TestMain(m *testing.M) {
	i18n.SetLocaleFunc(func() string { return "en" })
	os.Exit(m.Run())
}

func writeFile(t *testing.T, root string, name string, text string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePNG(t *testing.T, root string, name string, width int, height int) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	handle, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(handle, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(errors.Join(err, handle.Close()))
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, root string, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func openWorkspace(t *testing.T, root string) (*App, *ui.Tester) {
	t.Helper()
	a := New()
	if err := a.loadFolder(root); err != nil {
		t.Fatal(err)
	}
	return a, ui.NewTester(a.view, testWidth, testHeight)
}

func openSessionProject(t *testing.T, root string, sessionDir string) (*App, *ui.Tester, <-chan func()) {
	t.Helper()
	a := New()
	a.session.dir = sessionDir
	posted := make(chan func(), 128)
	a.post = func(action func()) { posted <- action }
	if err := a.loadFolder(root); err != nil {
		t.Fatal(err)
	}
	return a, ui.NewTester(a.view, testWidth, testHeight), posted
}

func waitForSession(t *testing.T, a *App, tester *ui.Tester, posted <-chan func(), tabs int) {
	t.Helper()
	followPosts(t, posted, tester, func() bool {
		return !a.session.restoring && len(a.documents) == tabs
	})
	tester.Frame()
}

func clickFile(t *testing.T, tester *ui.Tester, name string) {
	t.Helper()
	if err := tester.Click(name); err != nil {
		t.Fatalf("click %s: %v; texts %q", name, err, tester.Texts())
	}
	tester.Frame()
}

func appendText(d *document, text string) {
	end := d.editor.Buffer.Len()
	d.editor.Buffer.Edit(editor.Edit{Start: end, End: end, Text: text})
}

var writeEvent = []mygo.FileEvent{{Op: mygo.FileWrite, Path: "main.go"}}

func TestOpenEditAndSaveKeepsLineEndings(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\r\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "main.go")

	d := a.activeDocument()
	if d == nil {
		t.Fatalf("no document opened; texts %q", tester.Texts())
	}
	appendText(d, "// edited\n")
	tester.Frame()
	if !tester.HasText("Edited") {
		t.Errorf("status bar does not show unsaved edits; texts %q", tester.Texts())
	}
	a.saveActive()
	if a.notice != (toast{}) {
		t.Fatalf("save reported %+v", a.notice)
	}
	if got, want := readFile(t, root, "main.go"), "package main\r\n// edited\r\n"; got != want {
		t.Errorf("saved %q, want %q", got, want)
	}
	if d.modified() {
		t.Error("document is still modified after saving")
	}
}

func TestMarkdownRendersAndSwitchesToSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", "# Perch Title\n\nBody text\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "README.md")

	if !tester.HasText("Perch Title") || tester.HasText("# Perch Title") {
		t.Errorf("heading is not rendered; texts %q", tester.Texts())
	}
	a.toggleMarkdownView()
	tester.Frame()
	if a.activeDocument().showsPreview() {
		t.Error("document still shows its preview after switching to source")
	}
}

func TestMarkdownShowsItsHTMLBlocks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", `<h1 align="center">Perch Readme</h1>

<p align="center">
  <a href="docs/guide.md">The guide</a>
  <img src="https://img.shields.io/badge/ci.svg" alt="Build status">
</p>

Body text
`)
	writeFile(t, root, "docs/guide.md", "# Guide\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "README.md")
	tester.Frame()

	for _, want := range []string{"Perch Readme", "The guide", "Build status", "Body text"} {
		if !tester.HasText(want) {
			t.Errorf("preview does not show %q; texts %q", want, tester.Texts())
		}
	}
	if tester.HasText(`<h1 align="center">Perch Readme</h1>`) {
		t.Error("preview shows the HTML source")
	}
	a.openLink(a.activeDocument(), "docs/guide.md")
	if d := a.activeDocument(); d == nil || d.path != "docs/guide.md" {
		t.Errorf("relative link opened %+v", d)
	}
	guide := a.activeDocument()
	a.openFile("README.md")
	a.openLink(a.activeDocument(), filepath.Join("docs", "guide.md"))
	if d := a.activeDocument(); d != guide {
		t.Errorf("link with native separators opened %+v rather than reusing the guide", d)
	}
}

func TestHTMLImagePathUsesNativeSeparators(t *testing.T) {
	address := filepath.Join("assets", "icons") + "/../../cover.png"
	if !isWorkspaceImage("README.md", address) {
		t.Errorf("rejected workspace image %q", address)
	}
	rooted := string(filepath.Separator) + "cover.png"
	if isWorkspaceImage("docs/README.md", rooted) {
		t.Errorf("accepted rooted image %q", rooted)
	}
}

func TestCleanDocumentFollowsTheDisk(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "main.go")

	writeFile(t, root, "main.go", "package agent\n")
	a.applyFileEvents(writeEvent)
	tester.Frame()
	d := a.activeDocument()
	if got := d.editor.Buffer.String(); got != "package agent\n" {
		t.Errorf("buffer %q after the agent's write", got)
	}
	if d.modified() || d.conflict != conflictNone {
		t.Errorf("modified %v, conflict %d after reloading", d.modified(), d.conflict)
	}
}

func TestUnsavedEditsAreNeverOverwritten(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "main.go")
	d := a.activeDocument()
	appendText(d, "// mine\n")

	writeFile(t, root, "main.go", "package agent\n")
	if err := a.save(d); !errors.Is(err, workspace.ErrChangedOnDisk) {
		t.Fatalf("save over the agent's write returned %v", err)
	}
	tester.Frame()
	if d.conflict != conflictChanged || !tester.HasText("Keep Mine") {
		t.Fatalf("conflict %d; texts %q", d.conflict, tester.Texts())
	}
	if got := readFile(t, root, "main.go"); got != "package agent\n" {
		t.Errorf("disk %q after the refused save", got)
	}

	if err := tester.Click("Keep Mine"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if got, want := readFile(t, root, "main.go"), "package main\n// mine\n"; got != want {
		t.Errorf("disk %q after keeping mine, want %q", got, want)
	}
	if d.conflict != conflictNone || d.modified() {
		t.Errorf("conflict %d, modified %v after keeping mine", d.conflict, d.modified())
	}
}

func TestUseDiskVersionDropsEdits(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "main.go")
	d := a.activeDocument()
	appendText(d, "// mine\n")

	writeFile(t, root, "main.go", "package agent\n")
	a.applyFileEvents(writeEvent)
	tester.Frame()
	if err := tester.Click("Use Disk Version"); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	tester.Frame()
	if got := d.editor.Buffer.String(); got != "package agent\n" || d.modified() {
		t.Errorf("buffer %q, modified %v after using the disk version", got, d.modified())
	}
}

func TestDeletedFileCanBeRestored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/main.go", "package main\n")
	a := New()
	if err := a.loadFolder(root); err != nil {
		t.Fatal(err)
	}
	a.openFile("pkg/main.go")
	d := a.activeDocument()
	if err := os.RemoveAll(filepath.Join(root, "pkg")); err != nil {
		t.Fatal(err)
	}
	a.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileRemove, Path: "pkg", IsDir: true}})
	if d.conflict != conflictRemoved {
		t.Fatalf("conflict %d after the file was deleted", d.conflict)
	}
	a.keepMine(d)
	if got := readFile(t, root, "pkg/main.go"); got != "package main\n" {
		t.Errorf("restored %q", got)
	}
}

func TestBinaryFileIsRefused(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "image.bin", "a\x00b")
	a := New()
	if err := a.loadFolder(root); err != nil {
		t.Fatal(err)
	}
	a.openFile("image.bin")
	if a.activeDocument() != nil || a.brief.title != "image.bin is not a text file" {
		t.Errorf("document %v, message %+v for a binary file", a.activeDocument(), a.brief)
	}
}

func TestImageOpensWithItsDimensions(t *testing.T) {
	root := t.TempDir()
	writePNG(t, root, "photo.png", 3, 2)
	a, tester, posted := openWithPosts(t, root)
	clickFile(t, tester, "photo.png")
	followPosts(t, posted, tester, func() bool {
		d := a.activeDocument()
		return d != nil && d.image != nil && d.image.decoded
	})
	tester.Frame()

	d := a.activeDocument()
	if d == nil || d.path != "photo.png" || !d.isRasterImage() {
		t.Fatalf("opened %+v; texts %q", d, tester.Texts())
	}
	if d.editor != nil {
		t.Error("a picture tab has an editor")
	}
	if d.image.width != 3 || d.image.height != 2 {
		t.Errorf("picture is %d × %d pixels", d.image.width, d.image.height)
	}
	if !tester.HasText("3 × 2") {
		t.Errorf("status line does not show the dimensions; texts %q", tester.Texts())
	}
	if a.brief != (toast{}) {
		t.Errorf("opening the picture reported %+v", a.brief)
	}
}

func TestCorruptImageShowsAToastAndOpensNothing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "broken.png", "not a png")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "broken.png")

	if a.activeDocument() != nil || len(a.documents) != 0 {
		t.Fatalf("opened %+v for a corrupt picture", a.documents)
	}
	if want := "broken.png could not be shown"; a.brief.title != want {
		t.Errorf("message %+v, want %q", a.brief, want)
	}
	if !tester.HasText("broken.png could not be shown") {
		t.Errorf("texts %q", tester.Texts())
	}
}

func TestSVGSwitchesBetweenPreviewAndSource(t *testing.T) {
	root := t.TempDir()
	source := `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="80"><rect width="120" height="80" fill="#1464F0"/></svg>`
	writeFile(t, root, "icon.svg", source)
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "icon.svg")

	d := a.activeDocument()
	if d == nil || !d.isSVG() || !d.showsSVGPreview() {
		t.Fatalf("opened %+v; texts %q", d, tester.Texts())
	}
	if d.editor == nil || d.editor.Buffer.String() != source {
		t.Errorf("editor buffer %q", d.editor.Buffer.String())
	}
	if !tester.HasText("120 × 80") {
		t.Errorf("status line does not show the dimensions; texts %q", tester.Texts())
	}
	if err := tester.Click("Source"); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	tester.Frame()
	if d.showsSVGPreview() {
		t.Error("the SVG stayed on its preview")
	}
	if !tester.HasText("Ln 1, Col 1") {
		t.Errorf("the source lost its status bar; texts %q", tester.Texts())
	}
	if err := tester.Click("Preview"); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	tester.Frame()
	if !d.showsSVGPreview() {
		t.Error("the SVG did not return to its preview")
	}
	edited := strings.Replace(source, `width="120" height="80"`, `width="200" height="100"`, 1)
	d.editor.Buffer.Edit(editor.Edit{Start: 0, End: d.editor.Buffer.Len(), Text: edited})
	tester.Frame()
	if d.image.width != 200 || !tester.HasText("200 × 100") {
		t.Errorf("preview followed the edit to %d × %d; texts %q", d.image.width, d.image.height, tester.Texts())
	}
}

func TestBrokenSVGOpensOnItsSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "broken.svg", "<svg")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "broken.svg")

	d := a.activeDocument()
	if d == nil || d.showsSVGPreview() || d.editor.Buffer.String() != "<svg" {
		t.Fatalf("opened %+v; message %+v", d, a.brief)
	}
}

func TestHalfWrittenImageKeepsTheLastPicture(t *testing.T) {
	root := t.TempDir()
	writePNG(t, root, "photo.png", 40, 30)
	a, tester, posted := openWithPosts(t, root)
	clickFile(t, tester, "photo.png")
	followPosts(t, posted, tester, func() bool {
		d := a.activeDocument()
		return d != nil && d.image != nil && d.image.decoded
	})
	d := a.activeDocument()
	before := d.image.raster

	file := filepath.Join(root, "photo.png")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data[:len(data)-12], 0o644); err != nil {
		t.Fatal(err)
	}
	a.applyFileEvents(writeEvent)
	followPosts(t, posted, tester, func() bool { return a.brief != (toast{}) })
	if !slices.Contains(a.documents, d) || d.image.raster != before || d.image.width != 40 {
		t.Errorf("the tab lost its picture: open %v, %d wide", slices.Contains(a.documents, d), d.image.width)
	}
}

func TestImageFollowsTheDisk(t *testing.T) {
	root := t.TempDir()
	writePNG(t, root, "photo.png", 2, 2)
	a, tester, posted := openWithPosts(t, root)
	clickFile(t, tester, "photo.png")
	followPosts(t, posted, tester, func() bool {
		d := a.activeDocument()
		return d != nil && d.image != nil && d.image.decoded
	})
	d := a.activeDocument()
	before := d.image.raster

	writePNG(t, root, "photo.png", 4, 3)
	a.applyFileEvents(writeEvent)
	followPosts(t, posted, tester, func() bool {
		return d.image.raster != before && d.image.width == 4 && d.image.height == 3
	})
	tester.Frame()
	if !tester.HasText("4 × 3") {
		t.Errorf("status line did not follow the disk; texts %q", tester.Texts())
	}
}

func TestRootMenuCreatesAtTheWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	a, tester := openWorkspace(t, root)
	tester.Frame()

	tree, ok := tester.AccessByRole(ui.RoleTree, "Files")
	if !ok {
		t.Fatalf("no tree; texts %q", tester.Texts())
	}
	tester.RightClickAt(tree.Bounds.X+20, tree.Bounds.Y+tree.Bounds.H-10)
	if menu := tester.Menu(); !slices.Contains(menu, "New File…") || slices.Contains(menu, "Rename…") {
		t.Fatalf("root menu %q", menu)
	}
	if err := tester.ChooseMenuItem("New File…"); err != nil {
		t.Fatalf("%v; menu %q", err, tester.Menu())
	}
	tester.Frame()
	tester.Type("root.txt")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	if d := a.activeDocument(); d == nil || d.path != "root.txt" {
		t.Errorf("opened %+v; texts %q", d, tester.Texts())
	}
}

func TestRowMenuCoversTheWholeRow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "notes.txt", "keep\n")
	_, tester := openWorkspace(t, root)
	tester.Frame()
	row, found := tester.AccessByRole(ui.RoleTreeItem, "notes.txt")
	if !found {
		t.Fatalf("no row; texts %q", tester.Texts())
	}
	tester.RightClickAt(row.Bounds.X+row.Bounds.W-20, row.Bounds.Y+row.Bounds.H/2)
	if !slices.Contains(tester.Menu(), "Rename…") {
		t.Errorf("the end of the row opened the menu %q", tester.Menu())
	}
}

func TestNewFolderUsesTheClickedFolder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/keep.txt", "keep\n")
	a, tester := openWorkspace(t, root)
	tester.Frame()

	if err := tester.RightClick("docs"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("New Folder…"); err != nil {
		t.Fatalf("%v; menu %q", err, tester.Menu())
	}
	tester.Frame()
	tester.Type("notes")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	info, err := os.Stat(filepath.Join(root, "docs", "notes"))
	if err != nil || !info.IsDir() {
		t.Fatalf("new folder: %v, %+v", err, info)
	}
	if _, listed := a.entries["docs/notes"]; !listed {
		t.Errorf("the tree did not refresh; entries %+v", a.entries)
	}
}

func TestCollapseAllKeepsVisibleSelection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/nested/child.txt", "child\n")
	writeFile(t, root, "root.txt", "root\n")
	a, tester := openWorkspace(t, root)
	tester.Frame()

	clickFile(t, tester, "docs")
	clickFile(t, tester, "nested")
	clickFile(t, tester, "child.txt")
	clickFile(t, tester, locales.CollapseAll())
	if _, visible := tester.AccessByRole(ui.RoleTreeItem, "nested"); visible || a.selectedTreePath() != "" {
		t.Fatalf("nested rows or selection survived collapse: %q, %q", tester.Texts(), a.selectedTreePath())
	}

	clickFile(t, tester, "docs")
	clickFile(t, tester, "root.txt")
	clickFile(t, tester, locales.CollapseAll())
	if got := a.selectedTreePath(); got != "root.txt" {
		t.Errorf("selected %q after collapse, want root.txt", got)
	}
}

func TestNewFileOpensAndAnExistingNameIsRefused(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "notes.txt", "keep\n")
	a, tester := openWorkspace(t, root)
	tester.Frame()

	if err := tester.RightClick("notes.txt"); err != nil {
		t.Fatal(err)
	}
	if menu := tester.Menu(); !slices.Contains(menu, "Rename…") || !slices.Contains(menu, "Move to Trash…") {
		t.Fatalf("row menu %q", menu)
	}
	if err := tester.ChooseMenuItem("New File…"); err != nil {
		t.Fatalf("%v; menu %q", err, tester.Menu())
	}
	tester.Frame()
	tester.Type("hello.go")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	d := a.activeDocument()
	if d == nil || d.path != "hello.go" {
		t.Fatalf("opened %+v; texts %q", d, tester.Texts())
	}
	if info, err := os.Stat(filepath.Join(root, "hello.go")); err != nil || info.Size() != 0 {
		t.Fatalf("new file: %v, size %v", err, info)
	}
	if entry, listed := a.entries["hello.go"]; !listed || entry.IsDir {
		t.Errorf("the tree did not refresh: entry %+v, listed %v", entry, listed)
	}
	a.applyFileEvents(writeEvent)
	if d.conflict != conflictNone {
		t.Errorf("the watcher reported a conflict %d after the create", d.conflict)
	}

	if err := tester.RightClick("notes.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("New File…"); err != nil {
		t.Fatalf("%v; menu %q", err, tester.Menu())
	}
	tester.Frame()
	tester.Type("notes.txt")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if got := readFile(t, root, "notes.txt"); got != "keep\n" {
		t.Errorf("existing file became %q", got)
	}
	if !tester.HasText("Something with this name already exists.") {
		t.Errorf("texts %q", tester.Texts())
	}
	if got := a.activeDocument(); got == nil || got.path != "hello.go" {
		t.Errorf("refused name opened %+v", got)
	}
}

func TestRenameKeepsUnsavedEditsAndSavesToTheNewPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	a, tester := openWorkspace(t, root)
	a.openFile("main.go")
	d := a.activeDocument()
	appendText(d, "// mine\n")
	tester.Frame()

	a.startRename("main.go")
	tester.Frame()
	tester.Type("renamed.go")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	if d.path != "renamed.go" || d.editor.Label != "renamed.go" {
		t.Fatalf("document path %q, label %q", d.path, d.editor.Label)
	}
	if got, want := d.editor.Buffer.String(), "package main\n// mine\n"; got != want {
		t.Errorf("buffer %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "main.go")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("old path still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.go")); err != nil {
		t.Fatal(err)
	}
	a.applyFileEvents(writeEvent)
	if d.conflict != conflictNone {
		t.Errorf("the watcher reported a conflict %d after the rename", d.conflict)
	}
	if err := a.save(d); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, root, "renamed.go"), "package main\n// mine\n"; got != want {
		t.Errorf("saved %q, want %q", got, want)
	}
}

func TestRenameFolderMovesItsTabs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\n")
	writeFile(t, root, "src/util.go", "package main\n")
	a, tester := openWorkspace(t, root)
	a.openFile("src/main.go")
	a.openFile("src/util.go")
	tester.Frame()
	clickFile(t, tester, "src")
	if !a.tree.Open.Has("src") {
		t.Fatal("the folder did not open in the tree")
	}

	a.startRename("src")
	tester.Frame()
	tester.Type("lib")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	var paths []string
	for _, d := range a.documents {
		paths = append(paths, d.path)
		if d.editor.Label != d.path {
			t.Errorf("%s is labeled %s", d.path, d.editor.Label)
		}
	}
	if want := []string{"lib/main.go", "lib/util.go"}; !slices.Equal(paths, want) {
		t.Errorf("tabs %q, want %q", paths, want)
	}
	for _, name := range []string{"lib/main.go", "lib/util.go"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "src")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("old folder still there: %v", err)
	}
	if !a.tree.Open.Has("lib") || a.tree.Open.Has("src") {
		t.Errorf("the tree's open folders are %q", a.openTreePaths())
	}
}

func TestTypingInTheRenameDialogReplacesTheName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "notes.txt", "kept\n")
	_, tester := openWorkspace(t, root)
	tester.Frame()
	if err := tester.RightClick("notes.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("Rename…"); err != nil {
		t.Fatalf("%v; menu %q", err, tester.Menu())
	}
	tester.Frame()
	tester.Frame()
	tester.Frame()
	tester.Type("plan.txt")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if got := readFile(t, root, "plan.txt"); got != "kept\n" {
		t.Errorf("renamed file holds %q", got)
	}
}

func TestRenameChangesOnlyTheCaseOfAName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "notes.txt", "kept\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "notes.txt")
	a.startRename("notes.txt")
	a.renameTarget("NOTES.txt")
	names, err := os.ReadDir(root)
	if err != nil || len(names) != 1 || names[0].Name() != "NOTES.txt" {
		t.Fatalf("folder holds %v, %v; failure %q", names, err, a.files.failure)
	}
	if d := a.activeDocument(); d.path != "NOTES.txt" {
		t.Errorf("tab shows %s", d.path)
	}
}

func TestRenameShowsTheFileAsItsNewKind(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "notes.txt", "# Title\n")
	writePNG(t, root, "photo.png", 2, 2)
	a, tester, posted := openWithPosts(t, root)
	a.openFile("notes.txt")
	tester.Frame()
	a.startRename("notes.txt")
	a.renameTarget("notes.md")
	if d := a.activeDocument(); !d.isMarkdown() {
		t.Error("a file renamed to Markdown is not shown as Markdown")
	}

	a.openFile("photo.png")
	followPosts(t, posted, tester, func() bool { return a.activeDocument().image.decoded })
	a.startRename("photo.png")
	a.renameTarget("photo.bin")
	if d := a.activeDocument(); d != nil && d.path == "photo.bin" && d.isRasterImage() {
		t.Error("a file no longer named as a picture still shows as one")
	}
}

func TestRenameNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "a\n")
	writeFile(t, root, "b.txt", "b\n")
	a, tester := openWorkspace(t, root)
	a.openFile("a.txt")
	tester.Frame()

	a.startRename("a.txt")
	tester.Frame()
	tester.Type("b.txt")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	if !tester.HasText("Something with this name already exists.") {
		t.Errorf("texts %q", tester.Texts())
	}
	if got := readFile(t, root, "a.txt"); got != "a\n" {
		t.Errorf("a.txt became %q", got)
	}
	if got := readFile(t, root, "b.txt"); got != "b\n" {
		t.Errorf("b.txt became %q", got)
	}
}

func TestRenameRefusesANameOutsideTheWorkspace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	a, tester := openWorkspace(t, root)
	a.openFile("main.go")
	tester.Frame()

	a.startRename("main.go")
	tester.Frame()
	outside := "escape-" + filepath.Base(root) + ".go"
	tester.Type("../" + outside)
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	if !tester.HasText("Names cannot contain / or a NUL byte.") {
		t.Errorf("texts %q", tester.Texts())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), outside)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a name outside the workspace was created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "main.go")); err != nil {
		t.Errorf("the original file is gone: %v", err)
	}
	if got := a.activeDocument(); got == nil || got.path != "main.go" {
		t.Errorf("document moved to %+v", got)
	}
}

func TestFileNameRejectsNativeSeparator(t *testing.T) {
	name := filepath.Join("nested", "file.go")
	if err := validFileName(name); !errors.Is(err, errNameInvalid) {
		t.Errorf("validating path %q as a name: got %v, want %v", name, err, errNameInvalid)
	}
}

func TestTrashConfirmsAndClosesUnsavedTabs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "gone.txt", "bye\n")
	writeFile(t, root, "keep.txt", "keep\n")
	a, tester := openWorkspace(t, root)
	a.openFile("gone.txt")
	d := a.activeDocument()
	appendText(d, "// mine\n")
	tester.Frame()

	var trashed string
	a.trashItem = func(file string) error {
		trashed = file
		return os.Remove(file)
	}
	a.askTrash("gone.txt")
	tester.Frame()
	if !tester.HasText("Move gone.txt to Trash?") || !tester.HasText("1 open tab has unsaved edits.") {
		t.Fatalf("texts %q", tester.Texts())
	}
	if err := tester.Click("Move to Trash"); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	tester.Frame()
	if want := filepath.Join(root, "gone.txt"); trashed != want {
		t.Errorf("trashed %q, want %q", trashed, want)
	}
	if len(a.documents) != 0 {
		t.Errorf("tabs left after the trash: %+v", a.documents)
	}
	if _, err := os.Stat(filepath.Join(root, "gone.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("gone.txt was not removed: %v", err)
	}
	if _, listed := a.entries["gone.txt"]; listed {
		t.Error("the tree still lists gone.txt")
	}
	if _, err := os.Stat(filepath.Join(root, "keep.txt")); err != nil {
		t.Errorf("keep.txt: %v", err)
	}
}

func TestOpenFailureGoesAwayByItself(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server", "a\x00b")
	writeFile(t, root, "main.go", "package main\n")
	a, tester := openWorkspace(t, root)
	var expire func()
	a.after = func(_ time.Duration, action func()) { expire = action }

	clickFile(t, tester, "server")
	if !tester.HasText("server is not a text file") {
		t.Fatalf("texts %q", tester.Texts())
	}
	expire()
	tester.Frame()
	if tester.HasText("server is not a text file") {
		t.Error("the message outlived its time")
	}

	clickFile(t, tester, "server")
	clickFile(t, tester, "main.go")
	if tester.HasText("server is not a text file") {
		t.Error("the message stayed after another file opened")
	}
}

func TestTooLargeFileSaysItsSize(t *testing.T) {
	err := &workspace.FileTooLargeError{Path: "bin/server", Size: 59712482, Limit: workspace.MaxTextFileBytes}
	if !errors.Is(err, workspace.ErrFileTooLarge) {
		t.Error("the error does not match ErrFileTooLarge")
	}
	got := openFailure("bin/server", err)
	want := toast{title: "server is too large to open", detail: "It is 57 MB. Perch opens files up to 20 MB."}
	if got != want {
		t.Errorf("message %+v", got)
	}
}

// followPosts runs what background work hands to the window on the test's
// thread until done reports true.
func followPosts(t *testing.T, posted <-chan func(), tester *ui.Tester, done func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !done() {
		select {
		case action := <-posted:
			action()
			tester.Frame()
		case <-deadline:
			t.Fatalf("background work did not finish; texts %q", tester.Texts())
		}
	}
}

func openWithPosts(t *testing.T, root string) (*App, *ui.Tester, <-chan func()) {
	t.Helper()
	a, tester := openWorkspace(t, root)
	posted := make(chan func(), 64)
	a.post = func(action func()) { posted <- action }
	return a, tester, posted
}

func TestQuickOpenGoesToAFileAndLine(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/app/view.go", "package app\n\nfunc view() {}\n")
	writeFile(t, root, "README.md", "# Perch\n")
	a, tester, posted := openWithPosts(t, root)
	tester.Frame()

	a.showQuickOpen()
	tester.Frame()
	followPosts(t, posted, tester, func() bool { return a.quick.loaded })
	tester.Type("view:3")
	tester.Frame()
	if len(a.quick.matches) != 1 || a.quick.matches[0].Path != "internal/app/view.go" {
		t.Fatalf("matches %+v for query %q", a.quick.matches, a.quick.query)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	d := a.activeDocument()
	if d == nil || d.path != "internal/app/view.go" {
		t.Fatalf("opened %+v; texts %q", d, tester.Texts())
	}
	if got := d.editor.CursorPos().Line; got != 2 {
		t.Errorf("caret on line %d, want 2", got)
	}
	if a.quick.open || a.quick.listing.Paths != nil {
		t.Errorf("finder still open or still holding its list: %+v", a.quick.listing)
	}
}

func TestQuickOpenRowOpensOnClick(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "alpha.go", "package alpha\n")
	writeFile(t, root, "beta.go", "package beta\n")
	a, tester, posted := openWithPosts(t, root)
	tester.Frame()
	a.showQuickOpen()
	tester.Frame()
	followPosts(t, posted, tester, func() bool { return a.quick.loaded })
	tester.Frame()
	tester.Key(0, ui.KeyDown)
	tester.Frame()
	if a.quick.chosen != 1 {
		t.Errorf("Down chose row %d", a.quick.chosen)
	}
	if err := tester.Click("beta.go"); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	tester.Frame()
	if d := a.activeDocument(); d == nil || d.path != "beta.go" {
		t.Errorf("opened %+v", d)
	}
}

func TestFindInFilesOpensTheMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/plan.md", "# Plan\n\nThe needle is here\n")
	writeFile(t, root, "main.go", "package main\n")
	a, tester, posted := openWithPosts(t, root)
	tester.Frame()

	a.showFindInFiles()
	tester.Frame()
	a.find.query = "NEEDLE"
	a.startSearch()
	followPosts(t, posted, tester, func() bool { return a.find.finished })
	if a.find.summary.Hits != 1 || !tester.HasText("1 result in 1 file") {
		t.Fatalf("summary %+v; texts %q", a.find.summary, tester.Texts())
	}
	a.openSearchRow(a.find.chosen)
	tester.Frame()

	d := a.activeDocument()
	if d == nil || d.path != "docs/plan.md" {
		t.Fatalf("opened %+v", d)
	}
	if d.showsPreview() {
		t.Error("a match in Markdown stays hidden behind the preview")
	}
	if got := d.editor.SelectedText(); got != "needle" {
		t.Errorf("selected %q", got)
	}
	if got := d.editor.CursorPos().Line; got != 2 {
		t.Errorf("caret on line %d", got)
	}
}

func TestANewSearchReplacesTheLastOne(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "first second\n")
	a, tester, posted := openWithPosts(t, root)
	tester.Frame()
	a.find.query = "first"
	a.startSearch()
	a.find.query = "second"
	a.startSearch()
	followPosts(t, posted, tester, func() bool { return a.find.finished })
	if len(a.find.rows) != 2 || a.find.rows[1].hit.Preview != "first second" || a.find.rows[1].hit.Column != 6 {
		t.Errorf("rows %+v", a.find.rows)
	}

	a.find.regexp = true
	a.find.query = "("
	a.startSearch()
	tester.Frame()
	if !tester.HasText("Invalid regular expression") && a.sidebarTab == sidebarSearch {
		t.Errorf("texts %q", tester.Texts())
	}
	if a.find.failure == "" {
		t.Error("an invalid regular expression reported nothing")
	}
}

func TestRecentSearchesSurviveProjectSession(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "words.txt", "Foo\nfoo\n")
	sessions := t.TempDir()
	a, tester, posted := openSessionProject(t, root, sessions)
	a.showFindInFiles()
	tester.Frame()
	if !tester.HasText(locales.SearchEmptyHint()) || tester.HasText(locales.RecentSearches()) {
		t.Fatalf("empty search: %q", tester.Texts())
	}

	// A query counts once a result of it is opened, not while it is typed.
	run := func(query string) {
		a.find.query = query
		a.startSearch()
		followPosts(t, posted, tester, func() bool { return a.find.finished })
		a.openSearchRow(1)
	}
	const expression = "F[o]+"
	a.find.query = "F[o"
	a.startSearch()
	run(expression)
	run("foo")
	a.find.caseSensitive, a.find.regexp = true, true
	run(expression)
	a.release()

	saved, err := session.Load(session.Path(sessions, root))
	if err != nil {
		t.Fatal(err)
	}
	want := []session.RecentSearch{{Query: expression, CaseSensitive: true, Regexp: true}, {Query: "foo"}}
	if !slices.Equal(saved.RecentSearches, want) {
		t.Fatalf("saved searches: %+v, want %+v", saved.RecentSearches, want)
	}

	reopened, view, posts := openSessionProject(t, root, sessions)
	t.Cleanup(reopened.release)
	view.Frame()
	clickFile(t, view, expression)
	followPosts(t, posts, view, func() bool { return reopened.find.finished })
	if reopened.find.summary.Hits != 1 || view.HasText(locales.RecentSearches()) {
		t.Errorf("repeated search: %+v; texts %q", reopened.find.summary, view.Texts())
	}
}

func TestFindBarOpensOverMarkdownSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", "# Title\n\nBody\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "README.md")
	a.runFind(editor.Find)
	tester.Frame()
	if a.activeDocument().showsPreview() {
		t.Error("find left the document on its preview")
	}
}

func TestLaterLaunchesNameFoldersFromTheirOwnDirectory(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	other := filepath.Join(root, "other", "beta")
	if got := folderArgument([]string{"beta"}, work); got != filepath.Join(work, "beta") {
		t.Errorf("relative folder became %q", got)
	}
	if got := folderArgument([]string{other}, work); got != other {
		t.Errorf("absolute folder became %q", got)
	}
	if got := folderArgument(nil, work); got != "" {
		t.Errorf("no argument became %q", got)
	}
}

func TestAFolderGoesToTheRightWindow(t *testing.T) {
	empty := &App{}
	project := &App{root: "/work/alpha"}
	windows := []*App{project, empty}

	if got := chooseDestination(windows, empty, "/work/alpha"); got.existing != project {
		t.Errorf("an open project went to %+v", got)
	}
	if got := chooseDestination(windows, empty, "/work/beta"); got.empty != empty {
		t.Errorf("the empty window was passed over: %+v", got)
	}
	if got := chooseDestination(windows, project, "/work/beta"); got != (folderDestination{}) {
		t.Errorf("a project window was replaced: %+v", got)
	}
}

func gitIn(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func reviewedRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitIn(t, root, "init", "--quiet")
	writeFile(t, root, "a.go", "package a\n")
	writeFile(t, root, "b.go", "package b\n")
	writeFile(t, root, "plan.md", "# Plan\n\nFirst step\n\nSecond step\n")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "--quiet", "-m", "start")
	return root
}

func settleChanges(t *testing.T, a *App, tester *ui.Tester, posted <-chan func()) {
	t.Helper()
	followPosts(t, posted, tester, func() bool { return a.review.loaded && !a.review.busy() })
	tester.Frame()
}

func TestReviewFollowsAgentWorkToTheEnd(t *testing.T) {
	root := reviewedRepository(t)
	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	tester.Frame()
	a.showChanges()
	settleChanges(t, a, tester, posted)

	writeFile(t, root, "a.go", "package a\n\nfunc Agent() {}\n")
	writeFile(t, root, "b.go", "package b\n\nvar Two = 2\n")
	gitIn(t, root, "add", "a.go")
	gitIn(t, root, "commit", "--quiet", "-m", "agent commit")
	a.refreshChanges()
	settleChanges(t, a, tester, posted)
	if len(a.review.listing.Changes) != 2 || !tester.HasText("0 of 2 files reviewed") {
		t.Fatalf("listing %+v; texts %q", a.review.listing, tester.Texts())
	}

	a.selectChange("a.go")
	followPosts(t, posted, tester, func() bool { return a.review.diff != nil })
	tester.Frame()
	if !tester.HasText("Mark Reviewed") {
		t.Fatalf("texts %q", tester.Texts())
	}
	a.markSelectedReviewed()
	if a.review.selected != "b.go" {
		t.Errorf("moved on to %q", a.review.selected)
	}
	followPosts(t, posted, tester, func() bool {
		change, _ := a.review.change("a.go")
		return change.Reviewed && a.review.diff != nil && !a.review.busy()
	})

	a.setReviewed("b.go", true)
	settleChanges(t, a, tester, posted)
	writeFile(t, root, "a.go", "package a\n\nfunc Agent() {}\n\nfunc More() {}\n")
	a.refreshChanges()
	followPosts(t, posted, tester, func() bool {
		change, _ := a.review.change("a.go")
		return !change.Reviewed && !a.review.busy()
	})
	if change, _ := a.review.change("a.go"); change.Added != 2 {
		t.Errorf("a new edit shows %+v", change)
	}

	// Leave a listing in flight while making both review actions.
	a.refreshChanges()
	a.markAllReviewed()
	tester.Frame()
	if err := tester.Click("Finish Review"); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	followPosts(t, posted, tester, func() bool {
		return !a.review.busy() && tester.HasText("No changes since your last review.") && tester.HasText("Undo Finish Review")
	})

	if err := tester.Click("Undo Finish Review"); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	followPosts(t, posted, tester, func() bool {
		return !a.review.busy() && tester.HasText("2 of 2 files reviewed") && !tester.HasText("Undo Finish Review")
	})
}

func TestEditorMarksTheLinesLeftToRead(t *testing.T) {
	root := reviewedRepository(t)
	writeFile(t, root, ".gitignore", "secret.txt\n")
	writeFile(t, root, "secret.txt", "private\n")
	writeFile(t, root, "a.go", "package a\n\nfunc Agent() {}\n")
	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	tester.Frame()
	a.showChanges()
	settleChanges(t, a, tester, posted)

	a.openFile("a.go")
	tester.Frame()
	d := a.activeDocument()
	followPosts(t, posted, tester, func() bool { return d.editor.DiffBase != nil })
	if got := d.editor.DiffBase.String(); got != "package a\n" {
		t.Errorf("compared with %q", got)
	}

	a.setReviewed("a.go", true)
	followPosts(t, posted, tester, func() bool {
		return !a.review.busy() && d.editor.DiffBase != nil && d.editor.DiffBase.String() == d.editor.Buffer.String()
	})

	a.openFile("b.go")
	tester.Frame()
	if d.editor.DiffBase != nil {
		t.Error("a tab out of sight kept its comparison text")
	}
	ignored, err := a.review.store.Baseline(context.Background(), "secret.txt")
	if err != nil || ignored.Known {
		t.Errorf("an ignored file has baseline %+v, %v", ignored, err)
	}
}

func TestMarkdownChangeRendersAndOpensTheFile(t *testing.T) {
	root := reviewedRepository(t)
	writeFile(t, root, "plan.md", "# Plan\n\nFirst step\n\nSecond step changed\n")
	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	tester.Frame()
	a.showChanges()
	settleChanges(t, a, tester, posted)
	a.selectChange("plan.md")
	followPosts(t, posted, tester, func() bool { return a.review.preview != nil })
	tester.Frame()
	tester.Frame()

	if !tester.HasText("Second step changed") || tester.HasText("# Plan") {
		t.Fatalf("texts %q", tester.Texts())
	}
	if want := []lineRange{{first: 4, end: 5}}; !slices.Equal(a.review.marks, want) {
		t.Errorf("marks %+v", a.review.marks)
	}
	if err := tester.Click("Open File"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if d := a.activeDocument(); d == nil || d.path != "plan.md" || a.showsChange() {
		t.Errorf("document %+v, still in the diff: %v", d, a.showsChange())
	}
}

func TestGitPageComparesAndShowsHistory(t *testing.T) {
	root := reviewedRepository(t)
	gitIn(t, root, "branch", "-M", "main")
	gitIn(t, root, "switch", "--quiet", "-c", "feature")
	writeFile(t, root, "a.go", "package a\n\nfunc Committed() {}\n")
	gitIn(t, root, "commit", "--quiet", "-am", "add committed")
	writeFile(t, root, "new.go", "package a\n")
	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	tester.Frame()
	a.showChanges()
	settleChanges(t, a, tester, posted)
	followPosts(t, posted, tester, func() bool { return len(a.history.commits) == 2 })
	tester.Frame()
	if !tester.HasText("feature") || !tester.HasText("1 uncommitted") || !tester.HasText("add committed") {
		t.Fatalf("texts %q", tester.Texts())
	}
	a.startTreeStatus()
	t.Cleanup(a.stopTreeStatus)
	followPosts(t, posted, tester, func() bool { return a.status.kinds != nil })
	if kind, found := a.status.kinds["new.go"]; !found || kind != review.Untracked || len(a.status.kinds) != 1 {
		t.Errorf("the tree colors %v", a.status.kinds)
	}
	a.after = func(_ time.Duration, action func()) { go a.post(action) }
	gitIn(t, root, "add", "new.go")
	gitIn(t, root, "commit", "--quiet", "-m", "commit from a terminal")
	followPosts(t, posted, tester, func() bool { return len(a.status.kinds) == 0 && !a.status.running })
	a.after = func(time.Duration, func()) {}
	settleChanges(t, a, tester, posted)

	if err := tester.Click("Uncommitted"); err != nil {
		t.Fatal(err)
	}
	settleChanges(t, a, tester, posted)
	if got := changePaths(a.review.listing.Changes); len(got) != 0 || !tester.HasText("Everything is committed.") {
		t.Fatalf("uncommitted %v; texts %q", got, tester.Texts())
	}

	if err := tester.Click("Branch"); err != nil {
		t.Fatal(err)
	}
	settleChanges(t, a, tester, posted)
	if got := changePaths(a.review.listing.Changes); a.review.branch != "main" || !slices.Equal(got, []string{"a.go", "new.go"}) {
		t.Fatalf("since %q: %v", a.review.branch, got)
	}
	a.selectChange("a.go")
	followPosts(t, posted, tester, func() bool { return a.review.diff != nil })
	if got := a.review.diff.Old.String(); got != "package a\n" {
		t.Errorf("compared with %q", got)
	}

	if err := tester.Click("add committed"); err != nil {
		t.Fatal(err)
	}
	followPosts(t, posted, tester, func() bool { return a.history.diff != nil })
	tester.Frame()
	if a.showsChange() || !a.showsCommit() || a.history.file != "a.go" {
		t.Fatalf("shows change %v, commit %v, file %q", a.showsChange(), a.showsCommit(), a.history.file)
	}
	if got := a.history.diff.New.String(); got != "package a\n\nfunc Committed() {}\n" {
		t.Errorf("the commit's change is %q", got)
	}
	a.openFile("b.go")
	if a.showsCommit() {
		t.Error("opening a file left the commit on screen")
	}
}

func TestFileHistoryAndBlameOpenCommits(t *testing.T) {
	root := reviewedRepository(t)
	writeFile(t, root, "a.go", "package a\n\nfunc Second() {}\n")
	gitIn(t, root, "commit", "--quiet", "-am", "second")
	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	a.startTreeStatus()
	t.Cleanup(a.stopTreeStatus)
	followPosts(t, posted, tester, func() bool { return a.inGit() })
	a.openFile("a.go")
	tester.Frame()

	if err := tester.Click("File History"); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	followPosts(t, posted, tester, func() bool { return a.fileHistory.complete })
	tester.Frame()
	if len(a.fileHistory.commits) != 2 || !tester.HasText("second") {
		t.Fatalf("file history %+v; texts %q", a.fileHistory.commits, tester.Texts())
	}
	if err := tester.Click("start"); err != nil {
		t.Fatal(err)
	}
	followPosts(t, posted, tester, func() bool { return a.history.diff != nil })
	if !a.showsCommit() || a.history.file != "a.go" || a.fileHistory.open {
		t.Errorf("commit %q at %q; history open %v", a.history.commit.Subject, a.history.file, a.fileHistory.open)
	}

	a.openFile("a.go")
	d := a.activeDocument()
	tester.Frame()
	if err := tester.Click("Blame Lines"); err != nil {
		t.Fatal(err)
	}
	followPosts(t, posted, tester, func() bool { return d.blame != nil && !d.blame.loading })
	blame := d.blame.blame
	if len(blame.Lines) != 3 || blame.Commits[blame.Lines[0]].Summary != "start" || blame.Commits[blame.Lines[2]].Summary != "second" {
		t.Errorf("blame %+v", blame)
	}
	d.editor.Buffer.Set("// note\n" + d.editor.Buffer.String())
	a.blameHover(d, editor.HoverRequest{Offset: 0, Version: d.editor.Buffer.Version()})
	followPosts(t, posted, tester, func() bool { return !d.blame.loading && d.blame.pending == nil })
	if first := d.blame.blame.Commits[d.blame.blame.Lines[0]]; !first.Uncommitted {
		t.Errorf("an unsaved line is blamed on %+v", first)
	}
}

func changePaths(changes []review.Change) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	return paths
}

func TestChangesExplainAFolderOutsideGit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n")
	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	tester.Frame()
	a.showChanges()
	settleChanges(t, a, tester, posted)
	if !tester.HasText("Changes need a Git repository.") {
		t.Errorf("texts %q", tester.Texts())
	}
}

// gitMarkPixel reads the window at the center of where the Git button's dot
// is drawn, at scale 1: the third button of the sidebar's header, in from its
// top right corner.
func gitMarkPixel(t *testing.T, tester *ui.Tester) uint32 {
	t.Helper()
	x := controlsGap + 2*(sidebarTabWidth+2) + sidebarTabWidth - sidebarMarkInset - sidebarMarkSize/2
	y := (headerHeight-sidebarTabHeight)/2 + sidebarMarkInset + sidebarMarkSize/2
	tester.SetScale(1)
	tester.Frame()
	r, g, b, _ := tester.Image().At(int(x+0.5), int(y+0.5)).RGBA()
	return uint32(uint8(r>>8))<<16 | uint32(uint8(g>>8))<<8 | uint32(uint8(b>>8))
}

// waitForUnread runs what background work hands to the window until the review
// reports want unread files and has nothing left to do.
func waitForUnread(t *testing.T, a *App, tester *ui.Tester, posted <-chan func(), want int) {
	t.Helper()
	followPosts(t, posted, tester, func() bool {
		return !a.review.opening && !a.review.busy() && !a.review.queued && a.review.unread == want
	})
}

// TestGitButtonMarksUnreadChangesWhileAnotherViewShows pins the quiet mark:
// an agent's writes put a dot on the Git button while the Files page shows,
// background checks skip head/branch reads, the dot leaves once everything is
// reviewed, and the Git page never wears it.
func TestGitButtonMarksUnreadChangesWhileAnotherViewShows(t *testing.T) {
	root := reviewedRepository(t)
	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	// The delay of a refresh ends at once, on the test's thread.
	a.after = func(_ time.Duration, action func()) { go a.post(action) }
	tester.Frame()
	// Opening the folder begins the review and reads it once for the mark.
	a.beginReview()
	followPosts(t, posted, tester, func() bool {
		return a.review.loaded && !a.review.opening && !a.review.busy() && !a.review.queued
	})
	if a.sidebarTab != sidebarFiles {
		t.Fatalf("the sidebar opened on tab %d", a.sidebarTab)
	}
	if a.review.head.Commit != "" || len(a.review.branches) != 0 {
		t.Errorf("background check read full Git page state: head=%+v branches=%v", a.review.head, a.review.branches)
	}
	before := gitMarkPixel(t, tester)

	writeFile(t, root, "a.go", "package a\n\nfunc Agent() {}\n")
	a.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.go"}})
	waitForUnread(t, a, tester, posted, 1)
	if a.sidebarTab != sidebarFiles {
		t.Fatalf("the sidebar turned to tab %d", a.sidebarTab)
	}
	if a.review.head.Commit != "" || len(a.review.branches) != 0 {
		t.Errorf("background refresh read full Git page state: head=%+v branches=%v", a.review.head, a.review.branches)
	}
	marked := gitMarkPixel(t, tester)
	accent := a.look.palette.accent
	if want := uint32(accent.R)<<16 | uint32(accent.G)<<8 | uint32(accent.B); marked != want {
		t.Fatalf("the dot is %06x at the Git button's corner, want the accent %06x; before it was %06x", marked, want, before)
	}
	if marked == before {
		t.Fatalf("the Git button's corner did not change: %06x", before)
	}

	a.markAllReviewed()
	waitForUnread(t, a, tester, posted, 0)
	if cleared := gitMarkPixel(t, tester); cleared != before {
		t.Errorf("the dot stayed after everything was reviewed: %06x, want the bare corner %06x", cleared, before)
	}

	writeFile(t, root, "b.go", "package b\n\nvar Two = 2\n")
	a.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "b.go"}})
	waitForUnread(t, a, tester, posted, 1)
	if again := gitMarkPixel(t, tester); again != marked {
		t.Errorf("a later write put no dot back: %06x, want %06x", again, marked)
	}

	a.showChanges()
	settleChanges(t, a, tester, posted)
	if a.review.head.Commit == "" || len(a.review.branches) == 0 {
		t.Errorf("showing the Git page did not read head/branches: head=%+v branches=%v", a.review.head, a.review.branches)
	}
	if shown := gitMarkPixel(t, tester); shown == marked {
		t.Errorf("the Git page wears its own dot: %06x", shown)
	}
}

// TestGitButtonMarkTracksReviewAcrossCompareModes ensures the quiet dot always
// reflects unread review changes even when the Git page is left on Branch or
// Uncommitted comparison.
func TestGitButtonMarkTracksReviewAcrossCompareModes(t *testing.T) {
	root := reviewedRepository(t)
	gitIn(t, root, "branch", "-M", "main")
	gitIn(t, root, "switch", "--quiet", "-c", "feature")
	writeFile(t, root, "a.go", "package a\n\nfunc Committed() {}\n")
	gitIn(t, root, "commit", "--quiet", "-am", "committed before review")

	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	a.after = func(_ time.Duration, action func()) { go a.post(action) }
	tester.Frame()
	a.beginReview()
	followPosts(t, posted, tester, func() bool {
		return a.review.loaded && !a.review.opening && !a.review.busy()
	})
	before := gitMarkPixel(t, tester)
	accent := a.look.palette.accent
	wantDot := uint32(accent.R)<<16 | uint32(accent.G)<<8 | uint32(accent.B)

	// Switch the Git page to Branch comparison (1 file differs from main,
	// none unread in the review), then return to Files: the dot must stay off.
	a.showChanges()
	settleChanges(t, a, tester, posted)
	if err := tester.Click("Branch"); err != nil {
		t.Fatal(err)
	}
	settleChanges(t, a, tester, posted)
	if len(a.review.listing.Changes) != 1 {
		t.Fatalf("branch listing %+v, want 1 file", a.review.listing.Changes)
	}
	a.switchSidebarTab(sidebarFiles)
	waitForUnread(t, a, tester, posted, 0)
	if got := gitMarkPixel(t, tester); got != before {
		t.Errorf("branch comparison lit the review dot: %06x, want bare corner %06x", got, before)
	}

	// Switch the Git page to Uncommitted comparison, return to Files, and have
	// an agent write and commit a change: Uncommitted is empty, but the review
	// has an unread file so the dot must appear.
	a.showChanges()
	settleChanges(t, a, tester, posted)
	if err := tester.Click("Uncommitted"); err != nil {
		t.Fatal(err)
	}
	settleChanges(t, a, tester, posted)
	a.switchSidebarTab(sidebarFiles)
	waitForUnread(t, a, tester, posted, 0)

	writeFile(t, root, "b.go", "package b\n\nfunc AgentCommitted() {}\n")
	gitIn(t, root, "commit", "--quiet", "-am", "agent commit")
	a.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "b.go"}})
	waitForUnread(t, a, tester, posted, 1)
	if got := gitMarkPixel(t, tester); got != wantDot {
		t.Errorf("committed agent change under Uncommitted mode did not light the dot: %06x, want %06x", got, wantDot)
	}
}

func TestBeginReviewRemembersAFolderOutsideGit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n")
	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	a.after = func(_ time.Duration, action func()) { go a.post(action) }
	tester.Frame()
	a.beginReview()
	followPosts(t, posted, tester, func() bool {
		return !a.review.opening && a.review.notRepository
	})
	a.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.go"}})
	if a.review.queued || a.review.busy() || a.review.unread != 0 {
		t.Errorf("outside Git: queued=%v busy=%v unread=%d", a.review.queued, a.review.busy(), a.review.unread)
	}
}

func TestGitButtonDefersBackgroundRefreshWhileWindowHidden(t *testing.T) {
	root := reviewedRepository(t)
	a, tester, posted := openWithPosts(t, root)
	t.Cleanup(a.stopChanges)
	a.after = func(_ time.Duration, action func()) { go a.post(action) }
	tester.Frame()
	a.beginReview()
	followPosts(t, posted, tester, func() bool {
		return a.review.loaded && !a.review.opening && !a.review.busy()
	})
	accent := a.look.palette.accent
	wantDot := uint32(accent.R)<<16 | uint32(accent.G)<<8 | uint32(accent.B)

	// While nothing of the window is on screen, file events must not schedule
	// timers or run Git refreshes.
	tester.SetWindowHidden(true)
	writeFile(t, root, "a.go", "package a\n\nfunc First() {}\n")
	a.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.go"}})
	writeFile(t, root, "b.go", "package b\n\nfunc Second() {}\n")
	a.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "b.go"}})
	if a.review.queued || a.review.busy() || !a.review.staleWhileHidden || a.review.unread != 0 {
		t.Fatalf("while hidden: queued=%v busy=%v stale=%v unread=%d",
			a.review.queued, a.review.busy(), a.review.staleWhileHidden, a.review.unread)
	}

	// Returning to screen runs one refresh immediately and lights the dot.
	tester.SetWindowHidden(false)
	waitForUnread(t, a, tester, posted, 2)
	if a.review.staleWhileHidden {
		t.Error("staleWhileHidden stayed true after returning to screen")
	}
	if got := gitMarkPixel(t, tester); got != wantDot {
		t.Errorf("returning to screen did not light the dot: %06x, want %06x", got, wantDot)
	}
}

func TestLinesMapAcrossADiff(t *testing.T) {
	hunks := []editor.DiffHunk{{OldStart: 2, OldEnd: 4, NewStart: 2, NewEnd: 7}}
	cases := []struct {
		side editor.DiffSide
		line int
		want int
	}{
		{editor.DiffNew, 5, 5},
		{editor.DiffOld, 1, 1},
		{editor.DiffOld, 3, 2},
		{editor.DiffOld, 6, 9},
	}
	for _, test := range cases {
		if got := newLineFor(hunks, test.side, test.line); got != test.want {
			t.Errorf("line %d of side %d became %d, want %d", test.line, test.side, got, test.want)
		}
	}
}

func TestSettingsChangeTheOpenWindow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, "README.md", "# Title\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "main.go")

	a.showSettings()
	tester.Frame()
	for _, want := range []string{"Settings", "Theme", "Grok Bot", "Paper", "Light or dark"} {
		if !tester.HasText(want) {
			t.Fatalf("settings do not show %q; texts %q", want, tester.Texts())
		}
	}
	if err := tester.Click("Reading"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("Source"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if a.settings.MarkdownView != settings.MarkdownSource {
		t.Errorf("settings are %+v", a.settings)
	}
	if err := tester.Click("Close Settings"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if a.settingsOpen {
		t.Error("the settings stayed open")
	}

	next := a.settings
	next.FontSize = 18
	next.WordWrap = true
	next.TabSize = 2
	a.updateSettings(next)
	tester.Frame()
	d := a.activeDocument()
	if d.editor.Font.Size != 18 || !d.editor.SoftWrap || d.editor.TabSize != 2 {
		t.Errorf("editor font %v, wrap %v, tab %d", d.editor.Font.Size, d.editor.SoftWrap, d.editor.TabSize)
	}

	a.openFile("README.md")
	tester.Frame()
	if a.activeDocument().showsPreview() {
		t.Error("Markdown opened as a preview against the setting")
	}
}

func TestSidebarTooltipStaysClearOfTheWindowControls(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	_, tester := openWorkspace(t, root)
	tester.Frame()

	const buttonBottom float32 = (headerHeight + sidebarTabHeight) / 2
	tester.Move(controlsGap+sidebarTabWidth/2, headerHeight/2)
	tester.Advance(2 * time.Second)
	tester.Frame()
	tip, shown := tester.AccessByRole(ui.RoleTooltip, "")
	if !shown {
		t.Fatalf("no tooltip; texts %q", tester.Texts())
	}
	if tip.Bounds.Y < buttonBottom {
		t.Errorf("tooltip at y %v covers the window controls above y %v", tip.Bounds.Y, buttonBottom)
	}
}

func TestSessionSurvivesClosingAndReopening(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.md", "# Title\n\nBody\n")
	writeFile(t, root, "b.go", "package main\n"+strings.Repeat("// line\n", 100))
	writeFile(t, root, "c.txt", "c\n")
	writeFile(t, root, "dir/keep.txt", "keep\n")
	sessions := t.TempDir()

	a, tester, _ := openSessionProject(t, root, sessions)
	tester.Frame()
	a.openFile("a.md")
	a.openFile("b.go")
	a.openFile("c.txt")
	tester.Frame()
	a.documents[0].markdownView = markdownSource
	b := a.documents[1]
	b.editor.GoTo(editor.Pos{Line: 2, Col: 1})
	appendText(b, "// unsaved\n")
	a.chooseTab(1)
	a.sidebarWidth = 300
	a.sidebarTab = sidebarSearch
	a.tree.Open.Add("dir")
	tester.Frame()
	b.editor.SetScrollY(200)
	tester.Frame()
	a.release()

	a2, tester2, posted := openSessionProject(t, root, sessions)
	waitForSession(t, a2, tester2, posted, 3)

	var paths []string
	for _, d := range a2.documents {
		paths = append(paths, d.path)
	}
	if want := []string{"a.md", "b.go", "c.txt"}; !slices.Equal(paths, want) {
		t.Fatalf("tabs %q, want %q", paths, want)
	}
	if a2.active != 1 || a2.activeDocument().path != "b.go" {
		t.Errorf("active %d, %+v", a2.active, a2.activeDocument())
	}
	if a2.documents[0].markdownView != markdownSource {
		t.Error("Markdown came back on its preview")
	}
	b2 := a2.documents[1]
	if b2.modified() {
		t.Error("the restored tab is modified")
	}
	if strings.Contains(b2.editor.Buffer.String(), "// unsaved") {
		t.Error("unsaved edits were restored")
	}
	if got := b2.editor.CursorPos(); got.Line != 2 || got.Col != 1 {
		t.Errorf("caret at %+v", got)
	}
	if got := b2.editor.ScrollY(); got <= 0 || got > 500 {
		t.Errorf("scroll at %v", got)
	}
	if a2.sidebarWidth != 300 {
		t.Errorf("sidebar width %v", a2.sidebarWidth)
	}
	if a2.sidebarTab != sidebarSearch {
		t.Errorf("sidebar tab %d", a2.sidebarTab)
	}
	if !a2.tree.Open.Has("dir") {
		t.Errorf("open folders %q", a2.openTreePaths())
	}
}

func TestSessionSkipsDeletedAndCorruptEntries(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n")
	writeFile(t, root, "gone.md", "# Gone\n")
	sessions := t.TempDir()

	a, tester, _ := openSessionProject(t, root, sessions)
	a.openFile("a.go")
	a.openFile("gone.md")
	tester.Frame()
	a.release()
	if err := os.Remove(filepath.Join(root, "gone.md")); err != nil {
		t.Fatal(err)
	}

	a2, tester2, posted := openSessionProject(t, root, sessions)
	waitForSession(t, a2, tester2, posted, 1)
	if len(a2.documents) != 1 || a2.documents[0].path != "a.go" {
		t.Fatalf("restored %+v", a2.documents)
	}
	if a2.notice != (toast{}) || a2.brief != (toast{}) {
		t.Errorf("messages %+v, %+v", a2.notice, a2.brief)
	}
	a2.release()

	if err := os.WriteFile(session.Path(sessions, root), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	a3, tester3, _ := openSessionProject(t, root, sessions)
	tester3.Frame()
	if len(a3.documents) != 0 || a3.session.restoring {
		t.Errorf("corrupt session restored %+v", a3.documents)
	}
	if a3.notice != (toast{}) || a3.brief != (toast{}) {
		t.Errorf("messages %+v, %+v", a3.notice, a3.brief)
	}
}

func TestSessionClampsCaretAndScroll(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "short.txt", strings.Repeat("line\n", 100))
	sessions := t.TempDir()

	a, tester, _ := openSessionProject(t, root, sessions)
	a.openFile("short.txt")
	tester.Frame()
	d := a.activeDocument()
	d.editor.GoTo(editor.Pos{Line: 80, Col: 3})
	d.editor.SetScrollY(500)
	tester.Frame()
	a.release()

	writeFile(t, root, "short.txt", "one")
	a2, tester2, posted := openSessionProject(t, root, sessions)
	waitForSession(t, a2, tester2, posted, 1)
	if len(a2.documents) != 1 {
		t.Fatalf("restored %+v", a2.documents)
	}
	d2 := a2.documents[0]
	if got := d2.editor.CursorPos(); got.Line != 0 || got.Col > 3 {
		t.Errorf("caret at %+v in a one-line file", got)
	}
	if got := d2.editor.ScrollY(); got != 0 {
		t.Errorf("scroll at %v in a one-line file", got)
	}
}

func TestSessionRestoresAtMostTwelveTextTabs(t *testing.T) {
	root := t.TempDir()
	for i := range 15 {
		writeFile(t, root, fmt.Sprintf("f%02d.txt", i), fmt.Sprintf("%d\n", i))
	}
	sessions := t.TempDir()

	a, tester, _ := openSessionProject(t, root, sessions)
	for i := range 15 {
		a.openFile(fmt.Sprintf("f%02d.txt", i))
	}
	a.chooseTab(0)
	tester.Frame()
	a.release()

	a2, tester2, posted := openSessionProject(t, root, sessions)
	waitForSession(t, a2, tester2, posted, maxRestoredTextTabs)
	if len(a2.documents) != maxRestoredTextTabs {
		t.Fatalf("restored %d tabs", len(a2.documents))
	}
	var paths []string
	for _, d := range a2.documents {
		paths = append(paths, d.path)
	}
	if a2.activeDocument() == nil || a2.activeDocument().path != "f00.txt" {
		t.Errorf("active %+v, tabs %q", a2.activeDocument(), paths)
	}
	if paths[0] != "f00.txt" {
		t.Errorf("tabs start at %q", paths[0])
	}
	if !slices.Contains(paths, "f14.txt") {
		t.Errorf("the newest tab is missing from %q", paths)
	}
	if slices.Contains(paths, "f02.txt") {
		t.Errorf("an old tab came back in %q", paths)
	}
}

func TestSessionPlanRestoresOnlyTheActiveRaster(t *testing.T) {
	tabs := make([]session.Tab, 0, 15)
	for i := range 14 {
		tabs = append(tabs, session.Tab{Path: fmt.Sprintf("f%02d.txt", i)})
	}
	tabs = append(tabs, session.Tab{Path: "image.png"})

	plan := restorePlan(session.Session{Tabs: tabs, Active: 14})
	if len(plan.tabs) != 13 || plan.active != 12 || plan.tabs[12].Path != "image.png" {
		t.Errorf("active raster plan %+v", plan)
	}
	plan = restorePlan(session.Session{Tabs: tabs, Active: 0})
	if slices.ContainsFunc(plan.tabs, func(tab session.Tab) bool { return tab.Path == "image.png" }) {
		t.Errorf("an inactive raster came back in %+v", plan)
	}
	if len(plan.tabs) != maxRestoredTextTabs || plan.active != 0 {
		t.Errorf("text plan %+v", plan)
	}
}

func TestSessionSavesAfterChangesSettle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n")
	sessions := t.TempDir()
	a, tester, _ := openSessionProject(t, root, sessions)

	var scheduled func()
	var delay time.Duration
	a.after = func(d time.Duration, action func()) {
		delay, scheduled = d, action
	}
	a.openFile("a.go")
	tester.Frame()
	if scheduled == nil {
		t.Fatal("no session save was scheduled")
	}
	if delay != sessionSaveDelay {
		t.Errorf("session saved after %v, want %v", delay, sessionSaveDelay)
	}
	scheduled()
	a.session.wg.Wait()

	s, err := session.Load(session.Path(sessions, root))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tabs) != 1 || s.Tabs[0].Path != "a.go" || s.Active != 0 {
		t.Errorf("saved session %+v", s)
	}
}

func TestRecentProjectsKeepOrderAndLimit(t *testing.T) {
	root := t.TempDir()
	var list []string
	for i := range 12 {
		list = addRecent(list, filepath.Join(root, fmt.Sprintf("p%02d", i)))
	}
	if len(list) != maxRecentProjects {
		t.Fatalf("recent list has %d entries", len(list))
	}
	if list[0] != filepath.Join(root, "p11") || list[len(list)-1] != filepath.Join(root, "p02") {
		t.Errorf("recent list %q", list)
	}
	reopened := filepath.Join(root, "p05")
	list = addRecent(list, reopened)
	if len(list) != maxRecentProjects || list[0] != reopened {
		t.Fatalf("after reopening p05: %q", list)
	}
	count := 0
	for _, path := range list {
		if path == reopened {
			count++
		}
	}
	if count != 1 {
		t.Errorf("p05 appears %d times", count)
	}

	dirA, dirB := t.TempDir(), t.TempDir()
	if got, want := cleanRecent([]string{dirB, dirA, dirB, "relative"}), []string{dirB, dirA}; !slices.Equal(got, want) {
		t.Errorf("cleanRecent %q, want %q", got, want)
	}

	missing := filepath.Join(dirA, "missing")
	d := &Desktop{recent: []string{dirA, missing, dirB}}
	if got, want := d.recentExisting(), []string{dirA, dirB}; !slices.Equal(got, want) {
		t.Errorf("existing recent %q, want %q", got, want)
	}

	file := filepath.Join(t.TempDir(), "recent.json")
	if err := saveRecent(file, []string{dirA, dirB}); err != nil {
		t.Fatal(err)
	}
	if got, want := loadRecent(file), []string{dirA, dirB}; !slices.Equal(got, want) {
		t.Errorf("loaded recent %q, want %q", got, want)
	}
}

func TestWelcomeShowsOnlyExistingRecentProjects(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing-project")
	d := &Desktop{recent: []string{root, missing}}
	a := New()
	a.desktop = d
	posted := make(chan func(), 1)
	a.post = func(action func()) { posted <- action }
	tester := ui.NewTester(a.view, testWidth, testHeight)
	tester.Frame()
	followPosts(t, posted, tester, func() bool { return len(a.recentProjects) > 0 })
	tester.Frame()

	if !tester.HasText("Recent Projects") || !tester.HasText(filepath.Base(root)) {
		t.Errorf("texts %q", tester.Texts())
	}
	if tester.HasText(filepath.Base(missing)) {
		t.Errorf("a missing project shows; texts %q", tester.Texts())
	}
	if !tester.HasText(shortenProjectPath(root)) {
		t.Errorf("the shortened path is missing; texts %q", tester.Texts())
	}
}

func TestEveryMessageIsTranslated(t *testing.T) {
	read := func(name string) map[string]any {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "locales", name))
		if err != nil {
			t.Fatal(err)
		}
		var catalog map[string]any
		if err := json.Unmarshal(data, &catalog); err != nil {
			t.Fatal(err)
		}
		return catalog
	}
	english, chinese := read("en.json"), read("zh-Hans.json")
	for key, text := range english {
		if strings.HasPrefix(key, "@") {
			continue
		}
		translated, found := chinese[key]
		if !found || translated == "" || translated == text {
			t.Errorf("%s has no Chinese text: %v", key, translated)
		}
	}
}

// cleanupLanguageServers waits for servers before earlier cleanups remove the project.
func cleanupLanguageServers(t *testing.T, a *App) {
	t.Helper()
	t.Cleanup(func() {
		if pool := a.definition.pool; pool != nil {
			pool.Close()
			a.definition.pool = nil
		}
		a.stopDefinitions()
	})
}

// definitionProject writes a Go module and opens its main file.
func definitionProject(t *testing.T) (*App, *ui.Tester, <-chan func()) {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.test\n\ngo 1.22\n")
	writeFile(t, root, "util.go", "package main\n\n// Helper is here.\nfunc Helper() int { return 1 }\n")
	writeFile(t, root, "main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(Helper())\n}\n")
	a, tester, posted := openWithPosts(t, root)
	cleanupLanguageServers(t, a)
	tester.Frame()
	a.openFile("main.go")
	tester.Frame()
	return a, tester, posted
}

func definitionOffset(d *document, word string) int {
	return strings.Index(d.editor.Buffer.String(), word) + 1
}

func TestDefinitionOpensTheFileAndComesBack(t *testing.T) {
	a, tester, posted := definitionProject(t)
	start := a.activeDocument()
	a.goToDefinition(start, definitionOffset(start, "Helper"))
	followPosts(t, posted, tester, func() bool { return !a.definition.pending })
	d := a.activeDocument()
	if d == nil || d.path != "util.go" {
		t.Fatalf("opened %+v; message %+v", d, a.brief)
	}
	if got := d.editor.SelectedText(); got != "Helper" || d.editor.CursorPos().Line != 3 {
		t.Errorf("selected %q on line %d", got, d.editor.CursorPos().Line)
	}

	a.goBack()
	if d := a.activeDocument(); d.path != "main.go" || d.editor.CursorPos().Line != 5 {
		t.Errorf("back at %s line %d", d.path, d.editor.CursorPos().Line)
	}
	a.goForward()
	if d := a.activeDocument(); d.path != "util.go" {
		t.Errorf("forward at %s", d.path)
	}
}

func TestDefinitionInALibraryOpensReadOnly(t *testing.T) {
	a, tester, posted := definitionProject(t)
	start := a.activeDocument()
	a.goToDefinition(start, definitionOffset(start, "Println"))
	followPosts(t, posted, tester, func() bool { return !a.definition.pending })
	tester.Frame()
	d := a.activeDocument()
	if d == nil || !d.external || d.name() != "print.go" || !d.editor.ReadOnly {
		t.Fatalf("opened %+v; message %+v", d, a.brief)
	}
	if !tester.HasText("Read only") {
		t.Errorf("texts %q", tester.Texts())
	}
	if err := a.save(d); err != nil {
		t.Errorf("saving a library file: %v", err)
	}
}

func TestDefinitionExplainsAFileWithoutAServer(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "notes.txt", "plain words\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "notes.txt")
	a.goToDefinition(a.activeDocument(), 1)
	if want := "Go to definition is not available for notes.txt"; a.brief.title != want {
		t.Errorf("message %+v", a.brief)
	}
}

func TestTurnedOffLanguageSaysWhereToTurnItOn(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	a, tester := openWorkspace(t, root)
	clickFile(t, tester, "main.go")
	a.setLanguageEnabled("go", false)
	a.goToDefinition(a.activeDocument(), 1)
	if want := "Go to definition is turned off for Go"; a.brief.title != want {
		t.Errorf("message %+v", a.brief)
	}
	if a.definition.pool != nil {
		t.Error("a server was prepared for a language turned off")
	}
}

func TestCustomLanguageNamesItsInstallCommand(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.perchlang", "hello\n")
	config := filepath.Join(t.TempDir(), "language-servers.json")
	text := `{"languageServers":{"perchlang":{"name":"Perch Lang","command":"perch-no-such-server","extensions":[".perchlang"],"install":"brew install perch-lang"}}}`
	if err := os.WriteFile(config, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	a, tester, posted := openWithPosts(t, root)
	cleanupLanguageServers(t, a)
	a.languageFile = config
	clickFile(t, tester, "main.perchlang")
	a.goToDefinition(a.activeDocument(), 1)
	followPosts(t, posted, tester, func() bool { return !a.definition.pending })
	if a.brief.detail != "Install it with: brew install perch-lang" {
		t.Errorf("message %+v", a.brief)
	}
}

func TestBrokenLanguageConfigurationKeepsBuiltInLanguages(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(t.TempDir(), "language-servers.json")
	if err := os.WriteFile(config, []byte(`{"languageServers":{"x":{"command":"./evil","extensions":[".go"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := openWorkspace(t, root)
	a.languageFile = config
	language, _, found := a.languageRegistry().Match("main.go")
	if !found || language.Server.Program != "gopls" || a.notice.title != "The language server configuration has a problem" {
		t.Errorf("matched %+v; message %+v", language, a.notice)
	}
}

func TestLanguageSettingsListEveryServer(t *testing.T) {
	root := t.TempDir()
	a, tester, posted := openWithPosts(t, root)
	a.showSettings()
	tester.Frame()
	if err := tester.Click(locales.SettingLanguageServers()); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	followPosts(t, posted, tester, func() bool { return a.languages.checked })
	tester.Frame()
	if !tester.HasText("Zig") {
		t.Errorf("texts %q", tester.Texts())
	}
}

func TestChoosingAThemeRecolorsTheWindow(t *testing.T) {
	a, tester := openWorkspace(t, t.TempDir())
	tester.Frame()
	if a.settings.Theme != settings.ThemeGrokBot || a.look.palette.background != paletteFor(settings.ThemeGrokBot, false).background {
		t.Fatalf("first launch shows %q", a.settings.Theme)
	}
	a.showSettings()
	tester.Frame()
	if err := tester.Click("Paper"); err != nil {
		t.Fatalf("%v; texts %q", err, tester.Texts())
	}
	tester.Frame()
	tester.Frame()
	if a.settings.Theme != settings.ThemePaper {
		t.Fatalf("the theme is %q", a.settings.Theme)
	}
	if a.look.palette.background != paletteFor(settings.ThemePaper, false).background || a.look.grain == nil {
		t.Errorf("the window did not change: %+v", a.look.palette.background)
	}
}

func TestKeysSwitchBetweenTabs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeFile(t, root, name, name+"\n")
	}
	a, tester := openWorkspace(t, root)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		a.openFile(name)
	}
	tester.Frame()
	steps := []struct {
		mods ui.Modifiers
		key  ui.Key
		want string
	}{
		{ui.Ctrl, ui.KeyTab, "a.txt"},
		{ui.Ctrl | ui.Shift, ui.KeyTab, "c.txt"},
		{ui.Cmd, ui.Key2, "b.txt"},
		{ui.Cmd, ui.Key8, "b.txt"},
		{ui.Cmd, ui.Key9, "c.txt"},
	}
	for _, step := range steps {
		tester.Key(step.mods, step.key)
		tester.Frame()
		if got := a.activeDocument().path; got != step.want {
			t.Errorf("keys %v+%v showed %s, want %s", step.mods, step.key, got, step.want)
		}
	}
	if !a.activeDocument().editor.Focused() {
		t.Error("the keyboard did not follow the tab")
	}
}

func TestQuickOpenStillWorksAfterAJump(t *testing.T) {
	a, tester, posted := definitionProject(t)
	start := a.activeDocument()
	a.goToDefinition(start, definitionOffset(start, "Helper"))
	followPosts(t, posted, tester, func() bool { return !a.definition.pending })
	tester.Frame()
	a.goBack()
	tester.Frame()
	a.goForward()
	tester.Frame()

	a.showQuickOpen()
	tester.Frame()
	followPosts(t, posted, tester, func() bool { return a.quick.loaded })
	tester.Frame()
	tester.Type("main:6")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if d := a.activeDocument(); d.path != "main.go" || d.editor.CursorPos().Line != 5 {
		t.Errorf("opened %s line %d", d.path, d.editor.CursorPos().Line)
	}
	tester.Frame()
	if !a.activeDocument().editor.Focused() {
		t.Error("the keyboard did not reach the editor")
	}
	tester.Key(0, ui.KeyRight)
	tester.Frame()
	if got := a.activeDocument().editor.CursorPos(); got.Line != 5 || got.Col != 1 {
		t.Errorf("Right moved the caret to %+v", got)
	}
}

func TestTreeMarksWhatGitIgnores(t *testing.T) {
	root := t.TempDir()
	gitIn(t, root, "init", "--quiet")
	writeFile(t, root, ".gitignore", "build/\n*.log\n:odd\n")
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, "kept.log", "tracked\n")
	writeFile(t, root, "debug.log", "noise\n")
	writeFile(t, root, "build/out/app.o", "binary\n")
	gitIn(t, root, "add", "-f", "kept.log")

	ignored := map[string]bool{
		"main.go": false, "kept.log": false, ".gitignore": false,
		"debug.log": true, "build": true, "build/out/app.o": true,
	}
	if runtime.GOOS != "windows" {
		// Windows forbids colons in file names.
		writeFile(t, root, ":odd", "noise\n")
		ignored[":odd"] = true
	}

	a := New()
	posted := make(chan func(), 16)
	a.post = func(action func()) { posted <- action }
	if err := a.loadFolder(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopIgnoreMarks)
	a.startIgnoreMarks()
	settle := func() {
		t.Helper()
		a.childPaths(".")
		for a.ignored.asking {
			select {
			case action := <-posted:
				action()
			case <-time.After(10 * time.Second):
				t.Fatal("Git did not answer")
			}
		}
	}
	settle()

	for item, want := range ignored {
		if got := a.treeRowIgnored(item); got != want {
			t.Errorf("%s ignored = %v, want %v", item, got, want)
		}
	}

	writeFile(t, root, ".gitignore", "main.go\n")
	a.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileHint, Path: "."}})
	settle()
	if !a.treeRowIgnored("main.go") || a.treeRowIgnored("build") {
		t.Errorf("marks did not follow .gitignore: %+v", a.ignored.names)
	}
}

func TestTreeHasNoMarksOutsideGit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "debug.log", "noise\n")
	found, err := review.IgnoredNames(t.Context(), root, []string{"debug.log"})
	if !errors.Is(err, review.ErrNotRepository) || found != nil {
		t.Fatalf("found %q, err %v", found, err)
	}
}
