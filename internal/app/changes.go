package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"github.com/marukolibro/Perch/internal/review"
	"github.com/marukolibro/Perch/internal/workspace"
	"github.com/marukolibro/Perch/locales"
)

const (
	changesRefreshDelay time.Duration = 700 * time.Millisecond
	changeRowHeight     float32       = 28
	changeMarkSize      float32       = 18
	changedBlockBar     float32       = 3
	kindLetterWidth     float32       = 10
	// initialChangesShare is the percentage of the Git page the changes take
	// above the history at first.
	initialChangesShare float32 = 50
)

// What the Git page compares the work tree with, as indexes of its switch.
const (
	compareReview      int = 0
	compareUncommitted int = 1
	compareBranch      int = 2
)

// compareRequest is what one refresh of the changes reads.
// full is true when the Git page itself is visible and needs the chosen
// comparison, branch head and branch list; false reads only the review to keep
// the Git button's unread mark up to date.
type compareRequest struct {
	compare int
	branch  string
	full    bool
}

// gitReading is what one refresh of the Git page reads.
type gitReading struct {
	store    *review.Store
	listing  review.Listing
	head     review.Head
	branches []string
	err      error
}

// Indexes of the switch of a Markdown file's change.
const (
	changeRendered int = 0
	changeSource   int = 1
)

// lineRange is the lines from first up to end, exclusive, counted from 0.
type lineRange struct {
	first int
	end   int
}

// reviewAction changes the review's marks, off the main thread.
type reviewAction func(context.Context, *review.Store) error

// changesPanel is the state of the review of the window's project.
type changesPanel struct {
	store *review.Store
	// listing is the changes compared with what compare names. Its Version
	// is the review's snapshot, or the commit the work tree is compared with.
	listing          review.Listing
	unread           int
	loaded           bool
	notRepository    bool
	opening          bool
	staleWhileHidden bool
	failure          string

	compare int
	// branch is the branch compareBranch compares with.
	branch   string
	branches []string
	head     review.Head
	// split is the share of the page the changes take above the history.
	split float32

	running bool // a listing or review action is in flight
	again   bool
	queued  bool
	ctx     context.Context
	cancel  context.CancelFunc

	actions      []reviewAction
	actionWorker sync.WaitGroup

	selected string
	// shownVersion is the version whose texts the diff shows, which is the
	// one a mark on the selected file records.
	shownVersion string
	diff         *editor.DiffState
	unreadable   bool
	view         int
	preview      *document
	lineStarts   []int
	marks        []lineRange
	list         ui.ListState
	// reopen is where the last finished review stood, to undo finishing it.
	reopen *review.Checkpoint
	// readGeneration counts the changes to what has been read, and baselineKey
	// names the text the shown document is compared with.
	readGeneration uint64
	baselineKey    lineBaselineKey
	// landing is where the next file shown by moving between changes lands.
	landing changeLanding
}

// setWindowHidden pauses automatic review refreshes while nothing of the window
// shows on screen, and runs one refresh as soon as the window returns if files
// changed meanwhile.
func (a *App) setWindowHidden(hidden bool) {
	if a.windowHidden == hidden {
		return
	}
	a.windowHidden = hidden
	if !hidden && a.review.staleWhileHidden {
		a.review.staleWhileHidden = false
		if !a.review.notRepository && a.root != "" {
			a.refreshChanges()
		}
	}
}

// showsChangesPanel reports whether the Git page is currently visible in the
// sidebar.
func (a *App) showsChangesPanel() bool {
	return !a.sidebarHidden && a.sidebarTab == sidebarChanges
}

// switchSidebarTab shows tab in the sidebar, and refreshes the quiet review
// mark when leaving the Git page from another comparison so the dot stays true
// to what is left to read.
func (a *App) switchSidebarTab(tab int) {
	leavingOtherCompare := a.sidebarTab == sidebarChanges && tab != sidebarChanges && a.review.compare != compareReview
	a.sidebarHidden = false
	a.sidebarTab = tab
	if leavingOtherCompare && !a.review.notRepository {
		a.refreshChanges()
	}
}

// showChanges turns the sidebar to the review and brings it up to date.
func (a *App) showChanges() {
	if a.root == "" {
		return
	}
	a.sidebarHidden = false
	a.sidebarTab = sidebarChanges
	a.refreshChanges()
}

// beginReview fixes where the project's review starts as soon as the project
// opens, before an agent commits anything. The goroutine ends with that work, or
// when stopChanges cancels its context.
func (a *App) beginReview() {
	r := &a.review
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.opening = true
	ctx, root := r.ctx, a.root
	go func() {
		store, err := review.Open(ctx, root)
		// Only a folder outside Git is handed over without a store, so that
		// the window stops asking for changes it can never list. Any other
		// failure leaves that to the Git page, which reports it.
		outside := errors.Is(err, review.ErrNotRepository)
		if err != nil && !outside {
			slog.Warn("begin review", "root", root, "error", err)
		} else if err == nil {
			if err := store.Begin(ctx); err != nil {
				slog.Warn("begin review", "root", root, "error", err)
			}
		}
		a.post(func() { a.reviewBegun(ctx, store, outside) })
	}()
}

// reviewBegun takes the review's store once it is open, and reads the review
// for the Git button's mark. A folder outside Git is remembered instead: it
// lists nothing however its files change.
func (a *App) reviewBegun(ctx context.Context, store *review.Store, outside bool) {
	if ctx.Err() != nil {
		closeStore(store)
		return
	}
	a.review.opening = false
	if a.review.store != nil {
		closeStore(store)
		return
	}
	if outside {
		a.review.notRepository = true
		return
	}
	if store == nil {
		return
	}
	a.review.store = store
	if a.windowHidden {
		a.review.staleWhileHidden = true
		return
	}
	a.refreshChanges()
}

// stopChanges ends the review's background work and lets go of its store.
func (a *App) stopChanges() {
	r := &a.review
	if r.cancel != nil {
		r.cancel()
	}
	r.actionWorker.Wait()
	closeStore(r.store)
	a.review = changesPanel{}
	a.stopHistory()
}

// closeStore removes the store's temporary files off the main thread.
func closeStore(store *review.Store) {
	if store == nil {
		return
	}
	go func() {
		if err := store.Close(); err != nil {
			slog.Warn("close review store", "error", err)
		}
	}()
}

// refreshChanges lists the changes again. Listings and review actions take
// turns. If an action was queued during a listing, discard that listing so it
// cannot overwrite optimistic marks. List again after the last action.
// stopChanges cancels the listing's goroutine.
func (a *App) refreshChanges() {
	r := &a.review
	if r.busy() {
		r.again = true
		return
	}
	if r.ctx == nil {
		r.ctx, r.cancel = context.WithCancel(context.Background())
	}
	r.running, r.again, r.staleWhileHidden = true, false, false
	ctx, store, root := r.ctx, r.store, a.root
	request := a.changesRequest()
	go func() {
		reading := readGitPage(ctx, store, root, request)
		a.post(func() { a.changesListed(ctx, request, reading) })
	}()
}

// busy reports whether a listing or a review action runs or waits its turn.
func (r *changesPanel) busy() bool {
	return r.running || len(r.actions) != 0
}

func (r *changesPanel) request() compareRequest {
	return compareRequest{compare: r.compare, branch: r.branch}
}

func (a *App) changesRequest() compareRequest {
	if !a.showsChangesPanel() {
		return compareRequest{compare: compareReview}
	}
	request := a.review.request()
	request.full = true
	return request
}

// readGitPage lists the changes and, when the Git page itself is showing,
// reads the branch the work tree is on and the branches to compare with.
func readGitPage(ctx context.Context, store *review.Store, root string, request compareRequest) gitReading {
	reading := gitReading{store: store}
	if reading.store == nil {
		reading.store, reading.err = review.Open(ctx, root)
		if reading.err != nil {
			return reading
		}
	}
	if reading.listing, reading.err = listChanges(ctx, reading.store, root, request); reading.err != nil {
		return reading
	}
	if !request.full {
		return reading
	}
	if reading.head, reading.err = review.ReadHead(ctx, root); reading.err != nil {
		return reading
	}
	reading.branches, reading.err = review.Branches(ctx, root)
	return reading
}

// listChanges lists what differs from what the request compares with.
func listChanges(ctx context.Context, store *review.Store, root string, request compareRequest) (review.Listing, error) {
	switch request.compare {
	case compareUncommitted:
		comparison, err := review.CompareWithHead(ctx, root)
		return review.Listing{Version: comparison.Base, Changes: comparison.Changes}, err
	case compareBranch:
		if request.branch == "" {
			return review.Listing{}, nil
		}
		comparison, err := review.CompareWithBranch(ctx, root, request.branch)
		return review.Listing{Version: comparison.Base, Changes: comparison.Changes}, err
	default:
		return store.Changes(ctx)
	}
}

func (a *App) changesListed(ctx context.Context, request compareRequest, reading gitReading) {
	store := reading.store
	if ctx.Err() != nil {
		if store != nil && store != a.review.store {
			closeStore(store)
		}
		return
	}
	r := &a.review
	switch {
	case r.store == nil:
		r.store = store
	case store != nil && store != r.store:
		closeStore(store)
	}
	r.running = false
	if a.startQueuedReviewAction() {
		return
	}
	want := a.changesRequest()
	if want.full && request != want {
		// The Git page opened or changed comparison meanwhile.
		a.refreshChanges()
		return
	}
	err := reading.err
	if errors.Is(err, review.ErrNotRepository) {
		r.notRepository = true
	} else if err == nil {
		r.notRepository = false
	}
	if !request.full {
		if err != nil {
			if !r.notRepository {
				slog.Warn("check unread changes", "root", a.root, "error", err)
			}
		} else {
			r.unread = unreadCount(reading.listing.Changes)
			if r.compare == compareReview {
				r.listing = reading.listing
				r.loaded = true
				r.failure = ""
			}
		}
		if r.again {
			r.again = false
			if a.windowHidden {
				r.staleWhileHidden = true
			} else {
				a.refreshChanges()
			}
		}
		return
	}
	r.loaded = true
	r.failure = ""
	if err != nil {
		slog.Warn("list changes", "root", a.root, "compare", request.compare, "branch", request.branch, "error", err)
		r.failure = locales.ChangesFailed()
	} else {
		r.listing = reading.listing
		r.head = reading.head
		r.branches = reading.branches
		if request.compare == compareReview {
			r.unread = unreadCount(reading.listing.Changes)
		}
		a.followSelectedChange()
		if a.showsChangesPanel() {
			a.followHead()
		}
		// Commits change what the tree shows without touching its files.
		a.refreshTreeStatus()
	}
	if r.again {
		r.again = false
		if a.windowHidden {
			r.staleWhileHidden = true
		} else {
			a.refreshChanges()
		}
	}
}

// followSelectedChange shows the selected file as it now is, or leaves the diff
// when the file no longer differs.
func (a *App) followSelectedChange() {
	r := &a.review
	if r.selected == "" {
		return
	}
	change, found := r.change(r.selected)
	if !found {
		a.closeChange()
		return
	}
	a.loadSides(change)
}

func (r *changesPanel) change(relPath string) (review.Change, bool) {
	index := slices.IndexFunc(r.listing.Changes, func(c review.Change) bool { return c.Path == relPath })
	if index < 0 {
		return review.Change{}, false
	}
	return r.listing.Changes[index], true
}

// scheduleChangesRefresh follows the disk wherever the sidebar stands, waiting
// for a burst of writes to pass. While nothing of the window shows on screen,
// changes are remembered and refreshed once the window returns.
func (a *App) scheduleChangesRefresh() {
	r := &a.review
	// Outside Git there is nothing to list; while beginReview is still opening
	// the store, reviewBegun starts the first refresh.
	if r.notRepository || (r.opening && r.store == nil) || a.root == "" {
		return
	}
	if a.windowHidden {
		r.staleWhileHidden = true
		return
	}
	if r.queued {
		return
	}
	ctx := r.ctx
	r.queued = true
	a.after(changesRefreshDelay, func() {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		a.review.queued = false
		if a.review.notRepository || a.root == "" {
			return
		}
		if a.windowHidden {
			a.review.staleWhileHidden = true
			return
		}
		a.refreshChanges()
	})
}

// selectChange shows a file's change in the main area.
func (a *App) selectChange(relPath string) {
	change, found := a.review.change(relPath)
	if !found {
		return
	}
	a.closeChange()
	a.closeCommit()
	a.review.selected = relPath
	a.loadSides(change)
}

// closeChange leaves the diff and lets go of its texts.
func (a *App) closeChange() {
	r := &a.review
	r.selected, r.shownVersion = "", ""
	r.diff, r.preview = nil, nil
	r.unreadable = false
	r.lineStarts, r.marks = nil, nil
	r.view = changeRendered
	r.landing = landNowhere
}

func (a *App) showsChange() bool {
	return a.sidebarTab == sidebarChanges && a.review.selected != ""
}

// leaveGitView puts away the change or commit the main area shows.
func (a *App) leaveGitView() {
	a.closeChange()
	a.closeCommit()
}

// loadSides reads the texts of a change off the main thread.
func (a *App) loadSides(change review.Change) {
	r := &a.review
	ctx, store, root, version, request := r.ctx, r.store, a.root, r.listing.Version, r.request()
	go func() {
		sides, err := readSides(ctx, store, root, request, version, change)
		a.post(func() {
			if ctx.Err() == nil && a.review.selected == change.Path && a.review.request() == request {
				a.sidesLoaded(change, version, sides, err)
			}
		})
	}()
}

// readSides reads the two texts of a change: what is left to read of it in the
// review, or the file in the commit compared with and on disk.
func readSides(ctx context.Context, store *review.Store, root string, request compareRequest, version string, change review.Change) (review.Sides, error) {
	if request.compare == compareReview {
		return store.Sides(ctx, version, change)
	}
	return review.WorkTreeSides(ctx, root, version, change)
}

func (a *App) sidesLoaded(change review.Change, version string, sides review.Sides, err error) {
	r := &a.review
	if err != nil {
		slog.Warn("read change", "path", change.Path, "error", err)
		r.failure = locales.ChangesFailed()
		return
	}
	r.shownVersion = version
	r.unreadable = sides.Unreadable
	if sides.Unreadable {
		r.diff, r.preview = nil, nil
		return
	}
	if r.diff != nil && r.diff.Old.String() == sides.Old && r.diff.New.String() == sides.New {
		return
	}
	r.diff = a.newChangeDiff(change.Path, sides)
	r.preview = a.newChangePreview(change, sides.New)
	r.lineStarts = lineStartsOf(sides.New)
}

func (a *App) newChangeDiff(relPath string, sides review.Sides) *editor.DiffState {
	language := editor.LanguageForText(path.Base(relPath), sides.New+sides.Old)
	before, after := editor.NewBuffer(sides.Old), editor.NewBuffer(sides.New)
	before.SetLanguage(language)
	after.SetLanguage(language)
	return &editor.DiffState{Old: before, New: after, Layout: editor.DiffUnified, LineHeight: codeLineHeight, ScrollPastEnd: true}
}

// newChangePreview prepares the rendered view of a Markdown file that still
// exists, and returns nil for any other file.
func (a *App) newChangePreview(change review.Change, text string) *document {
	d := newDocument(a.root, change.Path, workspace.TextFile{Text: text})
	if !d.isMarkdown() || change.Kind == review.Deleted {
		return nil
	}
	a.attachMarkdownActions(d)
	plain := &markdown.Options{Theme: a.look.markdown, LoadImage: d.markdownOptions.LoadImage, OnLink: d.markdownOptions.OnLink, HTML: d.markdownOptions.HTML}
	d.markdownOptions.Block = func(c *ui.Context, doc *parser.Document, index int) bool {
		return a.changedBlockView(c, doc, index, plain, d.previewFront.size)
	}
	return d
}

// changedBlockView marks a top-level block whose lines changed. Its offsets
// count from the end of the front matter, which takes the first skipped bytes.
func (a *App) changedBlockView(c *ui.Context, doc *parser.Document, index int, plain *markdown.Options, skipped int) bool {
	block := doc.Block(index)
	if block.Parent != 0 {
		return false
	}
	if !touchesRanges(a.review.marks, sourceLines(a.review.lineStarts, block.Start+skipped, block.End+skipped)) {
		return false
	}
	accent := a.look.palette.accent
	ui.Column(c).FillWidth().Padding(4, 10, 4, 12).Radius(radiusSmall).Background(accent.Alpha(0.07)).
		BorderWidth(0, 0, 0, changedBlockBar).BorderColor(accent).Children(func() {
		markdown.View(c, doc.Source()[block.Start:block.End], plain).FillWidth()
	})
	return true
}

// lineStartsOf returns the offset each line of text starts at.
func lineStartsOf(text string) []int {
	starts := []int{0}
	for offset := 0; ; {
		next := strings.IndexByte(text[offset:], '\n')
		if next < 0 {
			return starts
		}
		offset += next + 1
		starts = append(starts, offset)
	}
}

// sourceLines returns the lines a span of bytes covers.
func sourceLines(lineStarts []int, start int, end int) lineRange {
	first := sort.SearchInts(lineStarts, start+1) - 1
	last := sort.SearchInts(lineStarts, max(start, end-1)+1) - 1
	return lineRange{first: first, end: last + 1}
}

// changedRanges returns the new lines of each change. A deletion marks the line
// that follows it.
func changedRanges(hunks []editor.DiffHunk) []lineRange {
	ranges := make([]lineRange, 0, len(hunks))
	for _, hunk := range hunks {
		ranges = append(ranges, lineRange{first: hunk.NewStart, end: max(hunk.NewEnd, hunk.NewStart+1)})
	}
	return ranges
}

func touchesRanges(ranges []lineRange, lines lineRange) bool {
	return slices.ContainsFunc(ranges, func(r lineRange) bool { return r.first < lines.end && lines.first < r.end })
}

// newLineFor returns the line of the new text at a line of a diff's side.
func newLineFor(hunks []editor.DiffHunk, side editor.DiffSide, line int) int {
	if side == editor.DiffNew {
		return line
	}
	shift := 0
	for _, hunk := range hunks {
		if line < hunk.OldStart {
			break
		}
		if line < hunk.OldEnd {
			return hunk.NewStart
		}
		shift += (hunk.NewEnd - hunk.NewStart) - (hunk.OldEnd - hunk.OldStart)
	}
	return line + shift
}

// openSelectedChange goes from the diff to the same line of the full file.
func (a *App) openSelectedChange() {
	r := &a.review
	change, found := r.change(r.selected)
	if !found || change.Kind == review.Deleted {
		return
	}
	line := 0
	if r.diff != nil && !a.showsRenderedChange() {
		line = newLineFor(r.diff.Hunks(), r.diff.ActiveSide(), r.diff.CursorPos().Line)
	}
	position := editor.Pos{Line: line}
	a.openAt(change.Path, position, position)
}

func (a *App) showsRenderedChange() bool {
	return a.review.preview != nil && a.review.view == changeRendered
}

// runReviewAction starts an action or queues it in the order requested.
func (a *App) runReviewAction(action reviewAction) {
	r := &a.review
	if r.store == nil {
		return
	}
	if r.busy() {
		r.actions = append(r.actions, action)
		return
	}
	a.startReviewAction(action)
}

// startQueuedReviewAction gives the oldest action its turn after other work ends.
func (a *App) startQueuedReviewAction() bool {
	r := &a.review
	if len(r.actions) == 0 {
		return false
	}
	action := r.actions[0]
	r.actions[0] = nil
	r.actions = r.actions[1:]
	a.startReviewAction(action)
	return true
}

// startReviewAction starts a worker owned by this window's review.
// stopChanges cancels and waits for it before discarding the rest of the queue.
func (a *App) startReviewAction(action reviewAction) {
	r := &a.review
	ctx, store := r.ctx, r.store
	r.running = true
	r.actionWorker.Go(func() {
		if ctx.Err() != nil {
			return
		}
		err := action(ctx, store)
		if ctx.Err() != nil {
			return
		}
		a.post(func() {
			if ctx.Err() != nil {
				return
			}
			r.running = false
			if err != nil {
				a.notice = toast{title: locales.ReviewUpdateFailed(), detail: err.Error()}
			}
			r.readGeneration++
			if !a.startQueuedReviewAction() {
				a.refreshChanges()
			}
		})
	})
}

// setReviewed marks a file, showing the mark at once.
func (a *App) setReviewed(relPath string, reviewed bool) {
	r := &a.review
	version := r.listing.Version
	if relPath == r.selected && r.shownVersion != "" {
		version = r.shownVersion
	}
	index := slices.IndexFunc(r.listing.Changes, func(c review.Change) bool { return c.Path == relPath })
	if index < 0 {
		return
	}
	changes := slices.Clone(r.listing.Changes)
	marked := []review.Change{changes[index]}
	changes[index].Reviewed = reviewed
	r.listing.Changes = changes
	if r.compare == compareReview {
		r.unread = unreadCount(changes)
	}
	a.runReviewAction(func(ctx context.Context, store *review.Store) error {
		if reviewed {
			return store.MarkReviewed(ctx, version, marked)
		}
		return store.MarkUnreviewed(ctx, marked)
	})
}

// markSelectedReviewed marks the file on screen and moves on to the next unread
// one.
func (a *App) markSelectedReviewed() {
	r := &a.review
	if !a.showsChange() || r.compare != compareReview {
		return
	}
	next := nextUnread(r.listing.Changes, r.selected)
	a.setReviewed(r.selected, true)
	if next != "" {
		a.selectChange(next)
	}
}

// nextUnread returns the unread file after relPath, going round to the start,
// or nothing when no other file is unread.
func nextUnread(changes []review.Change, relPath string) string {
	start := slices.IndexFunc(changes, func(c review.Change) bool { return c.Path == relPath })
	for step := 1; step < len(changes); step++ {
		candidate := changes[(start+step+len(changes))%len(changes)]
		if !candidate.Reviewed && candidate.Path != relPath {
			return candidate.Path
		}
	}
	return ""
}

func (a *App) markAllReviewed() {
	r := &a.review
	version := r.listing.Version
	changes := slices.Clone(r.listing.Changes)
	marked := make([]review.Change, 0, len(changes))
	for index := range changes {
		if !changes[index].Reviewed {
			marked = append(marked, changes[index])
			changes[index].Reviewed = true
		}
	}
	r.listing.Changes = changes
	if r.compare == compareReview {
		r.unread = 0
	}
	a.runReviewAction(func(ctx context.Context, store *review.Store) error {
		return store.MarkReviewed(ctx, version, marked)
	})
}

// finishReview starts a new review from what was just read.
func (a *App) finishReview() {
	version := a.review.listing.Version
	a.closeChange()
	a.review.listing.Changes = nil
	a.review.unread = 0
	a.runReviewAction(func(ctx context.Context, store *review.Store) error {
		checkpoint, err := store.Finish(ctx, version)
		if err != nil {
			return err
		}
		a.post(func() {
			if ctx.Err() == nil {
				a.review.reopen = &checkpoint
			}
		})
		return nil
	})
}

// reopenReview undoes finishing the last review, keeping the offer until the
// review is back.
func (a *App) reopenReview() {
	checkpoint := a.review.reopen
	a.runReviewAction(func(ctx context.Context, store *review.Store) error {
		if err := store.Reopen(ctx, *checkpoint); err != nil {
			return err
		}
		a.post(func() {
			if ctx.Err() == nil && a.review.reopen == checkpoint {
				a.review.reopen = nil
			}
		})
		return nil
	})
}

func unreadCount(changes []review.Change) int {
	count := 0
	for index := range changes {
		if !changes[index].Reviewed {
			count++
		}
	}
	return count
}

// changesPanelView builds the Git page: the branch, what the work tree is
// compared with and its changes, and the history below them.
func (a *App) changesPanelView(c *ui.Context) {
	r := &a.review
	if r.notRepository {
		a.panelMessageView(c, locales.ChangesNeedGit())
		return
	}
	if r.split == 0 {
		r.split = initialChangesShare
	}
	ui.Column(c).Grow(1).FillWidth().Children(func() {
		a.headView(c)
		a.compareView(c)
		ui.SplitVerticalPercent(c, &r.split, func() {
			ui.Column(c).Fill().Children(func() { a.changeListView(c) })
		}, func() {
			ui.Column(c).Fill().Children(func() { a.historyView(c) })
		}).Grow(1).FillWidth()
	})
}

// panelMessageView fills a part of the sidebar with a message.
func (a *App) panelMessageView(c *ui.Context, message string) {
	ui.Column(c).Grow(1).FillWidth().Center().Gap(12).Padding(0, 20).Children(func() {
		ui.Text(c, message).TextColor(a.look.palette.muted).TextAlign(ui.Center)
		a.reopenReviewView(c)
	})
}

func (a *App) changeListView(c *ui.Context) {
	r := &a.review
	if message := changesMessage(r); message != "" {
		a.panelMessageView(c, message)
		return
	}
	if r.compare == compareReview {
		a.changesHeaderView(c)
	}
	ui.List(c, &r.list, len(r.listing.Changes), func(row int) {
		a.changeRowView(c, r.listing.Changes[row])
	}).Grow(1).FillWidth().Padding(2, 6)
	if r.compare == compareReview {
		a.changesFooterView(c)
	}
}

// changesMessage says why the panel lists no files, and is empty when it does.
func changesMessage(r *changesPanel) string {
	switch {
	case r.failure != "":
		return r.failure
	case !r.loaded:
		return locales.ReadingChanges()
	case r.compare == compareBranch && r.branch == "":
		return locales.NoOtherBranch()
	case len(r.listing.Changes) > 0:
		return ""
	case r.compare == compareUncommitted:
		return locales.NothingUncommitted()
	case r.compare == compareBranch:
		return locales.NoChangesSinceBranch(r.branch)
	default:
		return locales.NoChanges()
	}
}

func (a *App) changesHeaderView(c *ui.Context) {
	changes := a.review.listing.Changes
	read := len(changes) - unreadCount(changes)
	ui.Row(c).FillWidth().Shrink(0).Padding(2, 14, 4, 14).Children(func() {
		ui.Text(c, locales.ReviewProgress(read, len(changes))).
			FontSize(smallFontSize - 1).TextColor(a.look.palette.muted)
	})
}

func (a *App) changesFooterView(c *ui.Context) {
	ui.Column(c).FillWidth().Shrink(0).Gap(6).Padding(8, 10).BorderWidth(1, 0, 0, 0).BorderColor(a.look.palette.border).Children(func() {
		a.reopenReviewView(c)
		if unreadCount(a.review.listing.Changes) > 0 {
			if ui.Button(c, locales.MarkAllReviewed()).FillWidth().Clicked() {
				a.markAllReviewed()
			}
			return
		}
		if ui.PrimaryButton(c, locales.FinishReview()).FillWidth().Clicked() {
			a.finishReview()
		}
	})
}

// reopenReviewView offers to undo finishing the last review.
func (a *App) reopenReviewView(c *ui.Context) {
	if a.review.reopen != nil && ui.Button(c, locales.ReopenReview()).FillWidth().Clicked() {
		a.reopenReview()
	}
}

func (a *App) changeRowView(c *ui.Context, change review.Change) {
	reviewing := a.review.compare == compareReview
	clicks := a.fileChangeRowView(c, change, change.Path == a.review.selected, reviewing)
	switch {
	case clicks.mark:
		a.setReviewed(change.Path, !change.Reviewed)
	case clicks.row:
		a.selectChange(change.Path)
	}
}

// rowClicks are the clicks on a row of changed files.
type rowClicks struct {
	row  bool
	mark bool
}

// fileChangeRowView builds a row of a changed file, with its review mark when
// marked is true.
func (a *App) fileChangeRowView(c *ui.Context, change review.Change, selected bool, marked bool) rowClicks {
	colors := a.look.palette
	name := path.Base(change.Path)
	row := ui.Row(c).FillWidth().Height(changeRowHeight).Gap(7).Padding(0, 8).Radius(radiusControl).AlignItems(ui.Center)
	switch {
	case selected:
		row.Background(colors.hover)
	case row.Hovered():
		row.Background(colors.hover.Alpha(0.5))
	}
	clicks := rowClicks{row: row.Clicked()}
	row.Children(func() {
		if marked {
			clicks.mark = a.changeMarkView(c, change)
		}
		letter, color := kindLetter(change.Kind, colors)
		ui.Text(c, letter).Font("monospace").FontSize(smallFontSize - 1).FontWeight(600).TextColor(color).Width(kindLetterWidth).Shrink(0)
		// The name gives way last: its folder is cut first, as it shrinks
		// from no width of its own.
		text := ui.Text(c, name).MaxLines(1).Shrink(1)
		if marked && change.Reviewed {
			text.TextColor(colors.muted)
		}
		if change.Kind == review.Renamed {
			ui.Text(c, "← "+change.OldPath).FontSize(smallFontSize - 1).TextColor(colors.muted).MaxLines(1).Shrink(1).MinWidth(0)
		} else if dir := path.Dir(change.Path); dir != "." {
			ui.Text(c, dir).FontSize(smallFontSize - 1).TextColor(colors.muted).MaxLines(1).Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		a.changeCountsView(c, change)
	})
	return clicks
}

// kindLetter is the letter Git shows for what happened to a file, in its color.
func kindLetter(kind review.Kind, colors palette) (string, ui.Color) {
	switch kind {
	case review.Added:
		return "A", colors.str
	case review.Deleted:
		return "D", colors.danger
	case review.Untracked:
		return "U", colors.str
	case review.Renamed:
		return "R", colors.accent
	default:
		return "M", colors.warning
	}
}

// changeMarkView builds the button that marks a file and reports a click on it.
func (a *App) changeMarkView(c *ui.Context, change review.Change) bool {
	colors := a.look.palette
	label := locales.MarkPathReviewed(change.Path)
	icon, color := iconCircle, colors.muted
	if change.Reviewed {
		label = locales.MarkPathUnread(change.Path)
		icon, color = iconCheck, colors.accent
	}
	button := ui.Box(c).Size(changeMarkSize, changeMarkSize).Center().Shrink(0).Label(label)
	clicked := button.Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(iconFontSize).TextColor(color)
	})
	return clicked
}

func (a *App) changeCountsView(c *ui.Context, change review.Change) {
	colors := a.look.palette
	small := smallFontSize - 1
	switch {
	case change.Binary:
		ui.Text(c, locales.BinaryFile()).FontSize(small).TextColor(colors.muted).Shrink(0)
	default:
		if change.Added > 0 {
			ui.Text(c, fmt.Sprintf("+%d", change.Added)).FontSize(small).TextColor(colors.str).Shrink(0)
		}
		if change.Removed > 0 {
			ui.Text(c, fmt.Sprintf("−%d", change.Removed)).FontSize(small).TextColor(colors.danger).Shrink(0)
		}
	}
}

func (a *App) changeView(c *ui.Context) {
	r := &a.review
	change, found := r.change(r.selected)
	if !found {
		return
	}
	a.landOnChange()
	a.changeBarView(c, change)
	c.Key(r.selected)
	switch {
	case r.unreadable:
		a.changeNoteView(c, locales.CannotShowAsText())
	case r.diff == nil:
		a.changeNoteView(c, locales.ReadingChange())
	case a.showsRenderedChange():
		a.renderedChangeView(c)
	default:
		r.diff.Theme = a.look.code
		a.applyDiffSettings(r.diff)
		editor.DiffView(c, r.diff).Grow(1).FillWidth().OnShortcut(0, ui.KeyEnter, a.openSelectedChange).
			ContextMenu(a.diffMenu)
	}
}

func (a *App) changeNoteView(c *ui.Context, message string) {
	ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
		ui.Text(c, message).TextColor(a.look.palette.muted)
	})
}

func (a *App) renderedChangeView(c *ui.Context) {
	r := &a.review
	r.marks = changedRanges(r.diff.Hunks())
	ui.Scroll(c).Grow(1).FillWidth().Padding(28, 32).AlignItems(ui.Center).Children(func() {
		r.preview.markdownOptions.Theme = a.look.markdown
		a.markdownPreviewView(c, r.preview, func(body string) ui.Element { return markdown.View(c, body, r.preview.markdownOptions) })
	})
}

func (a *App) changeBarView(c *ui.Context, change review.Change) {
	colors := a.look.palette
	r := &a.review
	ui.Row(c).FillWidth().Height(pathBarHeight+8).Shrink(0).Gap(5).Padding(0, 8, 0, 14).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(colors.border).Children(func() {
		a.pathCrumbsView(c, change.Path)
		ui.Text(c, changeKindLabel(change)).FontSize(smallFontSize-1).TextColor(colors.muted).Padding(0, 0, 0, 6)
		ui.Spacer(c)
		if r.preview != nil {
			ui.Segmented(c, &r.view, locales.ChangeRendered(), locales.ChangeDiff())
		}
		if change.Kind != review.Deleted && ui.Button(c, locales.OpenFile()).Clicked() {
			a.openSelectedChange()
		}
		if r.compare != compareReview {
			return
		}
		if change.Reviewed {
			if ui.Button(c, locales.MarkUnread()).Clicked() {
				a.setReviewed(change.Path, false)
			}
			return
		}
		if ui.PrimaryButton(c, locales.MarkReviewed()).Clicked() {
			a.markSelectedReviewed()
		}
	})
}

func changeKindLabel(change review.Change) string {
	switch change.Kind {
	case review.Added:
		return locales.KindAdded()
	case review.Deleted:
		return locales.KindDeleted()
	case review.Untracked:
		return locales.KindUntracked()
	case review.Renamed:
		return locales.KindRenamed(change.OldPath)
	default:
		return locales.KindModified()
	}
}
