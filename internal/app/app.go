// Package app is Perch's window: a file tree, tabs of code and rendered
// Markdown, and the rules that keep edits safe while agents change the same
// files.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/internal/settings"
	"github.com/marukolibro/Perch/internal/workspace"
	"github.com/marukolibro/Perch/locales"
)

const (
	initialSidebarWidth float32 = 248
	noRow               int     = -1
	noDocument          int     = -1
)

// errUnresolvedConflict reports a save of a document that disagrees with the
// file on disk until the person chooses a version.
var errUnresolvedConflict = errors.New("resolve the conflict with the file on disk first")

// App is the state of one window and its project. Its methods run on the main
// thread.
type App struct {
	desktop *Desktop
	window  *mygo.Window
	root    string

	entries       map[string]workspace.Entry
	children      map[string][]string
	ignored       ignoreMarks
	tree          ui.OutlineState[string]
	treeRow       int
	pendingReveal string
	sidebarWidth  float32
	sidebarTab    int
	sidebarHidden bool
	windowHidden  bool

	quick   quickOpen
	outline outlineState
	// outlineRequest invalidates answers of a closed finder or an older
	// request.
	outlineRequest uint64
	breadcrumb     breadcrumbState
	folderList     folderListState
	find           findInFiles
	files          fileTreeState
	session        sessionState
	// pathPopover is the open flag of the popover drawn on the current path bar.
	pathPopover *bool
	// recentProjects are the recent projects the welcome screen lists, known
	// once recentChecked has been answered.
	recentProjects []recentProject
	recentChecked  bool
	review         changesPanel
	history        historyPanel
	status         treeStatus
	fileHistory    fileHistory
	fileIcons      fileIconCache
	definition     definitionState
	hover          hoverState
	references     referencesState
	// projectSymbols is the finder of symbols across the project.
	projectSymbols projectSymbolsState

	settings       settings.Settings
	settingsOpen   bool
	settingsPage   settingsPage
	editorBindings []editor.Binding
	shortcutEdit   shortcutEdit
	languages      languagesPage
	// languageFile is the person's configuration of language servers, empty
	// when there is no place for one.
	languageFile string
	// chosenLanguage and setLanguage read and change the language the person
	// chose, empty while Perch follows the system.
	chosenLanguage func() string
	setLanguage    func(string)

	documents []*document
	active    int
	// notice is a failure that stays until it is dismissed.
	notice toast
	// brief is a message that goes away by itself.
	brief        toast
	briefID      uint64
	briefHovered bool

	look *look
	// looks are the looks that have shown, built once each.
	looks map[lookKey]*look

	closingDocument *document
	pendingClose    []*document
	askCloseTab     bool
	askCloseWindow  bool
	closeConfirmed  bool

	closedTabs  []closedTab
	recentFiles []string

	stopWatch context.CancelFunc

	// trashItem moves a path to the system Trash. Tests replace it.
	trashItem func(string) error
	// copyText writes text to the system clipboard. Tests replace it.
	copyText func(string)
	// post runs a function on the main thread and redraws.
	post func(func())
	// after runs a function on the main thread once a delay has passed.
	after func(time.Duration, func())
}

// New returns the app with no folder open.
func New() *App {
	a := &App{
		entries:      map[string]workspace.Entry{},
		children:     map[string][]string{},
		treeRow:      noRow,
		sidebarWidth: initialSidebarWidth,
		active:       noDocument,
		looks:        map[lookKey]*look{},
		settings:     settings.Default(),
		trashItem:    mygo.Shell.TrashItem,
		copyText:     mygo.Clipboard.WriteText,
	}
	a.post = func(action func()) { a.window.Update(action) }
	a.after = func(time.Duration, func()) {}
	a.session.init()
	language := ""
	a.chosenLanguage = func() string { return language }
	a.setLanguage = func(tag string) { language = tag }
	return a
}

// loadFolder makes root the workspace, closing the documents of the last one.
func (a *App) loadFolder(root string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("open folder %q: %w", root, err)
	}
	if _, err := workspace.ListDir(absolute, "."); err != nil {
		return fmt.Errorf("open folder %q: %w", absolute, err)
	}
	a.prepareSessionForFolder()
	a.hideQuickOpen()
	a.closeOutline()
	a.clearBreadcrumb()
	a.folderList = folderListState{}
	a.closeProjectSymbols()
	a.stopSearch()
	a.stopChanges()
	a.stopDefinitions()
	a.stopIgnoreMarks()
	a.stopTreeStatus()
	a.stopFileHistory()
	a.find = findInFiles{chosen: noRow}
	a.files = fileTreeState{input: a.files.input}
	a.sidebarWidth = initialSidebarWidth
	a.sidebarTab = sidebarFiles
	a.sidebarHidden = false
	a.root = absolute
	a.entries = map[string]workspace.Entry{}
	a.children = map[string][]string{}
	a.fileIcons = fileIconCache{}
	a.tree = ui.OutlineState[string]{}
	a.treeRow = noRow
	a.pendingReveal = ""
	a.closingDocument = nil
	a.pendingClose = nil
	a.askCloseTab = false
	a.askCloseWindow = false
	a.closeConfirmed = false
	a.closedTabs = nil
	a.recentFiles = nil
	a.documents = nil
	a.active = noDocument
	a.notice = toast{}
	a.brief = toast{}
	a.restoreSession()
	return nil
}

// childPaths lists a directory the first time it is asked for.
func (a *App) childPaths(dir string) []string {
	if paths, listed := a.children[dir]; listed {
		return paths
	}
	entries, err := workspace.ListDir(a.root, dir)
	if err != nil {
		a.notice = toast{title: locales.FolderReadFailed(), detail: err.Error()}
		a.children[dir] = nil
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		a.entries[entry.Path] = entry
		paths = append(paths, entry.Path)
	}
	a.children[dir] = paths
	a.askIgnored(dir)
	return paths
}

// openFile shows a file in a tab, reusing the tab it already has.
func (a *App) openFile(relPath string) {
	a.closeOutline()
	a.leaveGitView()
	index := slices.IndexFunc(a.documents, func(d *document) bool { return d.path == relPath })
	if index != noDocument {
		a.active = index
		a.recordRecentFile(relPath)
		a.brief = toast{}
		return
	}
	switch imageKindOf(relPath) {
	case imageKindRaster:
		a.openRasterImage(relPath)
		return
	case imageKindSVG:
		a.openSVGFile(relPath)
		return
	}
	file, err := workspace.ReadText(a.root, relPath)
	if err != nil {
		a.tell(openFailure(relPath, err))
		return
	}
	a.brief = toast{}
	d := newDocument(a.root, relPath, file)
	a.attachMarkdownActions(d)
	a.attachDefinitions(d)
	a.attachEditorMenu(d)
	if a.settings.MarkdownView == settings.MarkdownSource {
		d.markdownView = markdownSource
	}
	a.documents = append(a.documents, d)
	a.active = len(a.documents) - 1
	a.recordRecentFile(relPath)
}

// openAt shows a file with the range from start to end selected.
func (a *App) openAt(relPath string, start editor.Pos, end editor.Pos) {
	a.openFile(relPath)
	d := a.activeDocument()
	if d == nil || d.path != relPath {
		return
	}
	a.chooseTab(a.active)
	d.reveal(start, end)
}

func (a *App) activeDocument() *document {
	if a.active == noDocument {
		return nil
	}
	return a.documents[a.active]
}

// activeTextDocument returns the shown document when it has an editor.
func (a *App) activeTextDocument() *document {
	d := a.activeDocument()
	if d == nil || d.editor == nil {
		return nil
	}
	return d
}

// save writes a document, refusing to replace a version it has not seen.
func (a *App) save(d *document) error {
	if d.external || d.editor == nil {
		return nil
	}
	if d.conflict != conflictNone {
		return fmt.Errorf("save %q: %w", d.path, errUnresolvedConflict)
	}
	data, err := d.content()
	if err != nil {
		return err
	}
	digest, err := workspace.WriteText(a.root, d.path, data, d.digest)
	if errors.Is(err, workspace.ErrChangedOnDisk) {
		a.reconcile(d)
		return err
	}
	if err != nil {
		return err
	}
	d.markSaved(digest)
	if d.isSVG() {
		d.image.size = int64(len(data))
	}
	return nil
}

func (a *App) saveActive() {
	d := a.activeDocument()
	if d == nil {
		return
	}
	if err := a.save(d); err != nil {
		a.notice = saveFailure(d.name(), err)
	}
}

// saveAll saves every modified document and reports whether all were saved.
func (a *App) saveAll() bool {
	saved := true
	for _, d := range a.documents {
		if !d.modified() {
			continue
		}
		if err := a.save(d); err != nil {
			a.notice = saveFailure(d.name(), err)
			saved = false
		}
	}
	return saved
}

// requestCloseTab closes a document, asking first when it has unsaved edits.
func (a *App) requestCloseTab(d *document) {
	if d.modified() {
		a.closingDocument = d
		a.askCloseTab = true
		return
	}
	a.closeTab(d)
}

// closeActiveTab handles the File menu's close command. Dismiss dialogs in
// reverse drawing order, then the visible panel, before touching a document.
func (a *App) closeActiveTab() {
	switch {
	case a.askCloseWindow:
		a.askCloseWindow = false
	case a.askCloseTab:
		a.askCloseTab = false
		a.closingDocument = nil
		a.pendingClose = nil
	case a.files.open:
		a.files.open = false
	case a.settingsOpen:
		a.settingsOpen = false
	case a.root != "" && a.references.open:
		a.closeReferences()
	case a.root != "" && a.projectSymbols.open:
		a.closeProjectSymbols()
	case a.outline.open:
		a.dismissOutline()
	case a.quick.open:
		a.hideQuickOpen()
	case a.showsChange():
		a.closeChange()
	case a.showsCommit():
		a.closeCommit()
	case a.pathPopover != nil && *a.pathPopover:
		*a.pathPopover = false
	default:
		if d := a.activeDocument(); d != nil {
			a.requestCloseTab(d)
		}
	}
}

func (a *App) closeTab(d *document) {
	a.recordClosedTab(d)
	a.closeTabUnrecorded(d)
}

func (a *App) closeTabUnrecorded(d *document) {
	a.closeOutline()
	index := slices.Index(a.documents, d)
	if index == noDocument {
		return
	}
	a.documents = slices.Delete(slices.Clone(a.documents), index, index+1)
	a.active = activeAfterClose(a.active, index, len(a.documents))
	a.treeRow = noRow
	if d.image != nil {
		d.image.release()
	}
}

// activeAfterClose returns the tab to show after the tab at closed went away.
func activeAfterClose(active int, closed int, remaining int) int {
	if remaining == 0 {
		return noDocument
	}
	if closed < active {
		return active - 1
	}
	return min(active, remaining-1)
}

// keepMine resolves a conflict by writing the buffer over the file on disk.
func (a *App) keepMine(d *document) {
	data, err := d.content()
	if err != nil {
		a.notice = saveFailure(d.name(), err)
		return
	}
	digest, err := workspace.OverwriteText(a.root, d.path, data)
	if err != nil {
		a.notice = saveFailure(d.name(), err)
		return
	}
	d.markSaved(digest)
	if d.isSVG() {
		d.image.size = int64(len(data))
	}
}

// useDisk resolves a conflict by dropping the buffer's edits for the file on
// disk, or closing the tab when the file is gone.
func (a *App) useDisk(d *document) {
	if d.conflict == conflictRemoved {
		a.closeTab(d)
		return
	}
	d.reload(d.disk)
}

// applyFileEvents brings the tree and the open documents up to date with
// changes on disk.
func (a *App) applyFileEvents(events []mygo.FileEvent) {
	if slices.ContainsFunc(events, changesTree) {
		a.invalidateFileTree()
	}
	for _, d := range a.documents {
		a.reconcile(d)
	}
	a.scheduleChangesRefresh()
	a.scheduleTreeStatus()
}

func changesTree(event mygo.FileEvent) bool {
	return event.Op != mygo.FileWrite
}

// reconcile compares a document with its file: a document without unsaved
// edits follows the file, and one with unsaved edits shows a conflict rather
// than losing either version.
func (a *App) reconcile(d *document) {
	if d.external {
		return
	}
	if d.isRasterImage() {
		a.reconcileImage(d)
		return
	}
	file, err := workspace.ReadText(a.root, d.path)
	if errors.Is(err, fs.ErrNotExist) {
		d.conflict = conflictRemoved
		return
	}
	if err != nil {
		a.notice = toast{title: locales.FileReadFailed(d.name()), detail: err.Error()}
		return
	}
	if d.isSVG() {
		d.image.size = int64(len(file.Text))
	}
	if file.Digest == d.digest {
		d.conflict = conflictNone
		d.disk = workspace.TextFile{}
		return
	}
	data, err := d.content()
	if err != nil {
		a.notice = toast{title: locales.FileReadFailed(d.name()), detail: err.Error()}
		return
	}
	switch {
	case workspace.DigestOf(data) == file.Digest:
		d.markSaved(file.Digest)
	case d.modified():
		d.conflict = conflictChanged
		d.disk = file
	default:
		d.reload(file)
	}
}

// watch follows the workspace until ctx ends. It asks only for hints, which
// keep no snapshot of the tree in memory: any hint re-reads the open documents
// and the directories shown. The goroutine it starts is owned by the open
// folder: opening another folder cancels ctx, and quitting the app stops the
// watcher.
func (a *App) watch(ctx context.Context, root string) error {
	watcher, err := mygo.WatchFiles(ctx, root, mygo.FileWatchOptions{
		Recursive: true,
		HintsOnly: true,
		Ignore: func(relPath string, _ bool) bool {
			return workspace.IsIgnored(relPath)
		},
	})
	if err != nil {
		return fmt.Errorf("watch folder %q: %w", root, err)
	}
	go a.followWatcher(ctx, watcher)
	return nil
}

func (a *App) followWatcher(ctx context.Context, watcher *mygo.FileWatcher) {
	for {
		events, err := watcher.Next(ctx)
		if err != nil {
			a.watchEnded(ctx, watcher.Root(), err)
			return
		}
		a.window.Update(func() {
			if ctx.Err() == nil {
				a.applyFileEvents(events)
			}
		})
	}
}

// watchEnded reports a watch that stopped while its folder is still open.
func (a *App) watchEnded(ctx context.Context, root string, err error) {
	if ctx.Err() != nil || errors.Is(err, io.EOF) {
		return
	}
	a.window.Update(func() {
		a.notice = toast{title: locales.LiveUpdatesStopped(), detail: fmt.Sprintf("%s: %v", root, err)}
	})
}
