package app

import (
	"errors"
	"hash/maphash"
	"log/slog"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/internal/session"
	"github.com/marukolibro/Perch/internal/workspace"
)

const (
	// maxRestoredTextTabs is how many text tabs a project restores.
	maxRestoredTextTabs int = 12
	// sessionSaveDelay is how long the state stays unchanged before it is saved.
	sessionSaveDelay time.Duration = 2 * time.Second
	// maxRestoredSidebarWidth keeps a corrupt width from leaving no room for
	// the document.
	maxRestoredSidebarWidth float32 = 680
)

// Sidebar names in a session file.
const (
	sessionSidebarFiles   = "files"
	sessionSidebarSearch  = "search"
	sessionSidebarChanges = "changes"
)

// sessionState is what a window keeps of saving its session.
type sessionState struct {
	dir string
	// restoreID drops the posts of a restore whose folder is gone.
	restoreID uint64
	// restoreDone ends the goroutine reading a session's files.
	restoreDone chan struct{}
	restoring   bool
	// preserveActive keeps an explicitly opened tab (for example from the CLI)
	// active when an asynchronous session restore finishes.
	preserveActive bool
	// hash is the state last looked at, and saveID the last delay scheduled.
	hash   uint64
	saveID uint64
	// latestSeq and nextSeq order writes, so an older snapshot never
	// replaces a newer one. nextSeq is guarded by mu.
	latestSeq uint64
	nextSeq   uint64
	mu        sync.Mutex
	cond      *sync.Cond
	wg        sync.WaitGroup
	closed    bool
}

// init prepares the condition variable that orders writes.
func (s *sessionState) init() {
	if s.cond == nil {
		s.cond = sync.NewCond(&s.mu)
		s.nextSeq = 1
	}
}

// sessionPlan is the part of a session a restore reads, in the order it shows.
type sessionPlan struct {
	tabs []session.Tab
	// active is the position of the active tab in tabs, or -1.
	active int
}

// restorePlan chooses the tabs a session restores: at most
// maxRestoredTextTabs text tabs, the active tab, and a raster image only when
// it is active. It keeps the order the tabs had.
func restorePlan(s session.Session) sessionPlan {
	chosen := make([]bool, len(s.Tabs))
	active := s.Active
	validActive := active >= 0 && active < len(s.Tabs) && validSessionPath(s.Tabs[active].Path)
	textLeft := maxRestoredTextTabs
	if validActive {
		chosen[active] = true
		if imageKindOf(s.Tabs[active].Path) != imageKindRaster {
			textLeft--
		}
	}
	for i := len(s.Tabs) - 1; i >= 0 && textLeft > 0; i-- {
		if chosen[i] || !validSessionPath(s.Tabs[i].Path) {
			continue
		}
		if imageKindOf(s.Tabs[i].Path) == imageKindRaster {
			continue
		}
		chosen[i] = true
		textLeft--
	}
	seen := make(map[string]bool, len(s.Tabs))
	plan := sessionPlan{active: -1}
	for i, tab := range s.Tabs {
		if !chosen[i] || seen[tab.Path] {
			continue
		}
		seen[tab.Path] = true
		if i == active {
			plan.active = len(plan.tabs)
		}
		plan.tabs = append(plan.tabs, tab)
	}
	if plan.active < 0 && len(plan.tabs) > 0 {
		plan.active = len(plan.tabs) - 1
	}
	return plan
}

// validSessionPath reports a path that stays inside the workspace.
func validSessionPath(relPath string) bool {
	if relPath == "" || strings.ContainsRune(relPath, 0) {
		return false
	}
	return filepath.IsLocal(filepath.FromSlash(relPath))
}

// prepareSessionForFolder saves the old project's session and drops its
// restore, before loadFolder replaces the project.
func (a *App) prepareSessionForFolder() {
	a.session.init()
	if a.root != "" {
		a.flushSessionAsync()
	}
	a.session.restoreID++
	a.session.saveID++
	a.cancelSessionRestore()
	a.session.restoring = false
	a.session.preserveActive = false
	a.session.hash = ^uint64(0)
	a.session.closed = false
}

// restoreSession applies a project's layout and reads its tabs off the main
// thread.
func (a *App) restoreSession() {
	if a.session.dir == "" || a.root == "" {
		return
	}
	file := session.Path(a.session.dir, a.root)
	s, err := session.Load(file)
	if err != nil {
		if !errors.Is(err, session.ErrVersion) {
			slog.Warn("read session", "file", file, "root", a.root, "error", err)
		}
		return
	}
	a.restoreSessionLayout(s)
	plan := restorePlan(s)
	if len(plan.tabs) == 0 {
		return
	}
	a.session.restoring = true
	done := make(chan struct{})
	a.session.restoreDone = done
	go a.readRestoredTabs(plan, a.root, a.session.restoreID, done)
}

// restoreSessionLayout restores the sidebar and the tree, which costs no file
// reads.
func (a *App) restoreSessionLayout(s session.Session) {
	if s.SidebarWidth >= minSidebarWidth {
		maxWidth := maxRestoredSidebarWidth
		if a.window != nil {
			if width, _ := a.window.ContentSize(); width > 0 {
				maxWidth = min(maxWidth, float32(width)-minSidebarWidth)
			}
		}
		a.sidebarWidth = min(s.SidebarWidth, maxWidth)
	}
	a.sidebarTab = sessionSidebarTab(s.SidebarTab)
	a.sidebarHidden = s.SidebarHidden
	a.find.recent = s.RecentSearches[:min(len(s.RecentSearches), maxRecentSearches)]
	a.tree.Open.Clear()
	for _, dir := range s.OpenFolders {
		if dir != "." && validSessionPath(dir) {
			a.tree.Open.Add(dir)
		}
	}
}

// sessionSidebarTab maps a session's sidebar name to the switch.
func sessionSidebarTab(name string) int {
	switch name {
	case sessionSidebarSearch:
		return sidebarSearch
	case sessionSidebarChanges:
		return sidebarChanges
	default:
		return sidebarFiles
	}
}

// sessionSidebarName maps the switch to a session's sidebar name.
func sessionSidebarName(tab int) string {
	switch tab {
	case sidebarSearch:
		return sessionSidebarSearch
	case sidebarChanges:
		return sessionSidebarChanges
	default:
		return sessionSidebarFiles
	}
}

// readRestoredTabs reads a session's files. The goroutine belongs to the
// window's restore: it ends after the last planned tab, or when done closes,
// and each result is posted and dropped if the folder changed or the window
// closed meanwhile.
func (a *App) readRestoredTabs(plan sessionPlan, root string, id uint64, done <-chan struct{}) {
	activeRead := false
	for i, tab := range plan.tabs {
		select {
		case <-done:
			return
		default:
		}
		active := i == plan.active
		if imageKindOf(tab.Path) == imageKindRaster {
			file, err := readRaster(root, tab.Path)
			if err != nil {
				continue
			}
			if active {
				activeRead = true
			}
			a.post(func() {
				d := a.addRestoredRaster(id, tab, file)
				if active {
					a.activateRestored(id, d)
				}
			})
			continue
		}
		file, err := workspace.ReadText(root, tab.Path)
		if err != nil {
			continue
		}
		if active {
			activeRead = true
		}
		a.post(func() {
			d := a.addRestoredText(id, tab, file)
			if active {
				a.activateRestored(id, d)
			}
		})
	}
	a.post(func() { a.sessionRestored(id, activeRead) })
}

// addRestoredText puts a text or SVG tab back, at its clamped caret and
// scroll, without reading it again.
func (a *App) addRestoredText(id uint64, tab session.Tab, file workspace.TextFile) *document {
	if id != a.session.restoreID {
		return nil
	}
	if existing := a.openDocumentAt(tab.Path); existing != nil {
		return existing
	}
	var d *document
	if imageKindOf(tab.Path) == imageKindSVG {
		d = a.newSVGDocument(tab.Path, file)
	} else {
		d = newDocument(a.root, tab.Path, file)
		a.attachMarkdownActions(d)
	}
	a.attachDefinitions(d)
	a.attachEditorMenu(d)
	a.applyRestoredView(d, tab)
	a.restoreTabState(d, tab)
	a.documents = append(a.documents, d)
	if a.active == noDocument {
		a.active = len(a.documents) - 1
	}
	return d
}

// addRestoredRaster puts the active image tab back and starts decoding it.
func (a *App) addRestoredRaster(id uint64, tab session.Tab, file rasterFile) *document {
	if id != a.session.restoreID {
		return nil
	}
	if existing := a.openDocumentAt(tab.Path); existing != nil {
		return existing
	}
	d := a.newRasterDocument(tab.Path, file)
	d.image.silent = true
	a.documents = append(a.documents, d)
	if a.active == noDocument {
		a.active = len(a.documents) - 1
	}
	a.startImageDecode(d, file)
	return d
}

// activateRestored shows the tab a restore restored as active.
func (a *App) activateRestored(id uint64, d *document) {
	if id != a.session.restoreID || d == nil || a.session.preserveActive {
		return
	}
	if index := slices.Index(a.documents, d); index != noDocument {
		a.chooseTab(index)
	}
}

// applyRestoredView restores a Markdown or SVG tab's Preview / Source choice.
func (a *App) applyRestoredView(d *document, tab session.Tab) {
	if !d.isMarkdown() && !d.isSVG() {
		return
	}
	if tab.Source {
		d.markdownView = markdownSource
		return
	}
	d.markdownView = markdownPreview
}

// restoreTabState restores a text tab's caret and scroll, clamping both to the
// file as it is now.
func (a *App) restoreTabState(d *document, tab session.Tab) {
	if d.editor == nil {
		return
	}
	buffer := d.editor.Buffer
	pos := buffer.Pos(buffer.Offset(editor.Pos{Line: tab.Line, Col: tab.Col}))
	d.editor.GoTo(pos)
	scroll := tab.Scroll
	if scroll < 0 || math.IsNaN(scroll) || math.IsInf(scroll, 0) {
		scroll = 0
	}
	d.restoredScroll = scroll
	d.scrollPending = true
}

// sessionRestored finishes a restore, choosing a tab when the active one could
// not be read.
func (a *App) sessionRestored(id uint64, activeRead bool) {
	if id != a.session.restoreID {
		return
	}
	if !activeRead && !a.session.preserveActive && len(a.documents) > 0 {
		a.active = len(a.documents) - 1
	}
	a.session.restoring = false
	a.session.preserveActive = false
	a.session.restoreDone = nil
}

// cancelSessionRestore ends a restore's file reads.
func (a *App) cancelSessionRestore() {
	if a.session.restoreDone != nil {
		close(a.session.restoreDone)
		a.session.restoreDone = nil
	}
}

// closeSession saves the session of a closing window and waits for its writes.
func (a *App) closeSession() {
	a.session.init()
	a.session.closed = true
	a.session.saveID++
	a.session.restoreID++
	a.cancelSessionRestore()
	a.saveSessionNow()
	a.session.restoring = false
}

// trackSession schedules a save when what a session keeps last changed.
func (a *App) trackSession() {
	if a.root == "" || a.session.dir == "" || a.session.closed || a.session.restoring {
		return
	}
	hash := a.sessionFingerprint()
	if hash == a.session.hash {
		return
	}
	a.session.hash = hash
	a.session.saveID++
	id := a.session.saveID
	a.after(sessionSaveDelay, func() {
		if id == a.session.saveID {
			a.flushSessionAsync()
		}
	})
}

var sessionHashSeed = maphash.MakeSeed()

// sessionFingerprint hashes everything a session keeps, without building the
// session or allocating for its tab list.
func (a *App) sessionFingerprint() uint64 {
	hash := uint64(14695981039346656037)
	mix := func(value uint64) {
		hash = (hash ^ value) * 1099511628211
	}
	mix(uint64(a.sidebarTab))
	mix(uint64(math.Float32bits(a.sidebarWidth)))
	if a.sidebarHidden {
		mix(1)
	} else {
		mix(0)
	}
	mix(uint64(a.active))
	var folders uint64
	for dir := range a.tree.Open.All() {
		folders ^= maphash.String(sessionHashSeed, dir)
	}
	mix(folders)
	for _, recent := range a.find.recent {
		mix(maphash.Comparable(sessionHashSeed, recent))
	}
	for _, d := range a.documents {
		if d.external {
			continue
		}
		mix(maphash.String(sessionHashSeed, d.path))
		if d.editor != nil {
			pos := d.editor.CursorPos()
			mix(uint64(pos.Line))
			mix(uint64(pos.Col))
			mix(math.Float64bits(d.editor.ScrollY()))
		}
		mix(uint64(d.markdownView))
	}
	return hash
}

// sessionSnapshot builds the session of the window as it is now.
func (a *App) sessionSnapshot() session.Session {
	s := session.Session{
		Version:        session.Version,
		Active:         noDocument,
		SidebarWidth:   a.sidebarWidth,
		SidebarTab:     sessionSidebarName(a.sidebarTab),
		SidebarHidden:  a.sidebarHidden,
		OpenFolders:    a.openTreePaths(),
		RecentSearches: slices.Clone(a.find.recent),
	}
	slices.Sort(s.OpenFolders)
	for i, d := range a.documents {
		if d.external {
			continue
		}
		if i == a.active {
			s.Active = len(s.Tabs)
		}
		tab := session.Tab{Path: d.path}
		if d.editor != nil {
			pos := d.editor.CursorPos()
			tab.Line, tab.Col = pos.Line, pos.Col
			tab.Scroll = d.editor.ScrollY()
			if d.scrollPending {
				tab.Scroll = d.restoredScroll
			}
		}
		if d.isMarkdown() || d.isSVG() {
			tab.Source = d.markdownView == markdownSource
		}
		s.Tabs = append(s.Tabs, tab)
	}
	return s
}

// flushSessionAsync saves the current session off the main thread, after the
// debounce. The goroutine writes one snapshot; closeSession waits for the ones
// still running.
func (a *App) flushSessionAsync() {
	if a.session.dir == "" || a.root == "" || a.session.restoring || a.session.closed {
		return
	}
	a.session.init()
	snapshot := a.sessionSnapshot()
	file := session.Path(a.session.dir, a.root)
	a.session.latestSeq++
	a.startSessionWrite(file, snapshot, a.session.latestSeq)
}

// startSessionWrite owns the goroutine that writes one session snapshot.
func (a *App) startSessionWrite(file string, s session.Session, seq uint64) {
	a.session.wg.Add(1)
	go func() {
		defer a.session.wg.Done()
		a.writeSession(file, s, seq)
	}()
}

// writeSession writes snapshots in the order they were taken. The condition
// variable gives each its turn, so an older one never replaces a newer one.
func (a *App) writeSession(file string, s session.Session, seq uint64) {
	a.session.mu.Lock()
	defer a.session.mu.Unlock()
	for seq != a.session.nextSeq {
		a.session.cond.Wait()
	}
	a.session.nextSeq++
	a.session.cond.Broadcast()
	if err := session.Save(file, s); err != nil {
		slog.Warn("save session", "file", file, "error", err)
	}
}

// saveSessionNow saves the session on the closing window and waits for every
// write. It skips a window still reading its session, keeping the last one.
func (a *App) saveSessionNow() {
	if a.session.dir == "" || a.root == "" || a.session.restoring {
		a.session.wg.Wait()
		return
	}
	a.session.init()
	snapshot := a.sessionSnapshot()
	file := session.Path(a.session.dir, a.root)
	a.session.latestSeq++
	a.writeSession(file, snapshot, a.session.latestSeq)
	a.session.wg.Wait()
}
