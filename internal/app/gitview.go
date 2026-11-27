package app

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/internal/review"
	"github.com/marukolibro/Perch/locales"
)

// gitTabLabel names the Git page, the same in every language.
const gitTabLabel = "Git"

const (
	// historyPage is how many commits the history reads at a time.
	historyPage         int     = 50
	commitRowHeight     float32 = 46
	commitFilesWidth    float32 = 260
	refChipFontSize     float32 = 10
	historyDotSize      float32 = 7
	compareRowPadding   float32 = 10
	compareSwitchHeight float32 = 28
	// initialMessageHeight is the room a commit message takes at first.
	initialMessageHeight float32 = 120
)

// historyPanel is the history of the project on the Git page, read a page at
// a time, and the commit the main area shows.
type historyPanel struct {
	commits []review.Commit
	// head is the commit the history was read from, and generation counts
	// the times it was read again from the start.
	head       string
	generation uint64
	loading    bool
	complete   bool
	failure    string
	list       ui.ListState

	// selected names the commit the main area shows, which commit is, and
	// preferred is the file to show of it first.
	selected   string
	commit     review.Commit
	preferred  string
	detail     *review.CommitDetail
	file       string
	diff       *editor.DiffState
	unreadable bool
	landing    changeLanding
	files      ui.ListState
	// messageHeight is the height of the commit message above its files.
	messageHeight float32
}

// branchCandidates are the branches compared with first, when the work tree
// is on none of them.
var branchCandidates = []string{"main", "master", "origin/main", "origin/master"}

// headView shows the branch the work tree is on, how far it is from its
// upstream, and how many files are not committed.
func (a *App) headView(c *ui.Context) {
	head := a.review.head
	colors := a.look.palette
	ui.Row(c).FillWidth().Shrink(0).Gap(6).Padding(2, 14, 6, 14).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconChanges).FontSize(smallFontSize).TextColor(colors.muted)
		name := head.Branch
		if name == "" {
			name = locales.DetachedAt(head.Commit)
		}
		ui.Text(c, name).FontSize(smallFontSize).FontWeight(600).MaxLines(1).Shrink(1)
		if head.Upstream != "" {
			ui.Text(c, fmt.Sprintf("↑%d ↓%d", head.Ahead, head.Behind)).FontSize(smallFontSize - 1).TextColor(colors.muted).Shrink(0).
				Tooltip(head.Upstream)
		}
		ui.Spacer(c)
		if head.Uncommitted > 0 {
			ui.Text(c, locales.UncommittedCount(head.Uncommitted)).FontSize(smallFontSize - 1).TextColor(colors.muted).Shrink(0)
		}
		if len(a.review.worktrees) > 1 {
			a.worktreesButtonView(c)
		}
	})
}

const worktreeListWidth float32 = 280

func (a *App) worktreesButtonView(c *ui.Context) {
	colors := a.look.palette
	r := &a.review
	button := ui.ButtonBase(c).Height(22).Padding(0, 6).Gap(4).Radius(radiusSmall).AlignItems(ui.Center).
		Label(locales.WorktreesLabel()).Tooltip(locales.WorktreesLabel()).Shrink(0)
	tint := colors.muted
	switch {
	case r.worktreesOpen:
		button.Background(colors.accent.Alpha(0.14))
		tint = colors.accent
	case button.Hovered():
		button.Background(colors.hover)
		tint = colors.text
	}
	if button.Clicked() {
		r.worktreesOpen = !r.worktreesOpen
	}
	button.Children(func() {
		ui.Icon(c, iconWorktrees).FontSize(smallFontSize).TextColor(tint)
		ui.Text(c, fmt.Sprint(len(r.worktrees))).FontSize(smallFontSize - 1).TextColor(tint)
	})
	ui.Popover(c, button, &r.worktreesOpen, func() {
		a.worktreesListView(c)
	})
}

func (a *App) worktreesListView(c *ui.Context) {
	colors := a.look.palette
	r := &a.review
	ui.Column(c).Width(worktreeListWidth).Gap(2).Children(func() {
		ui.Text(c, locales.WorktreesLabel()).FontSize(smallFontSize-1).FontWeight(600).
			TextColor(colors.muted).Padding(2, 8, 4, 8)
		for _, wt := range r.worktrees {
			a.worktreeRowView(c, wt)
		}
	})
}

func (a *App) worktreeRowView(c *ui.Context, wt review.Worktree) {
	colors := a.look.palette
	title := wt.Branch
	if title == "" {
		title = locales.DetachedAt(wt.Head)
	}
	row := ui.ButtonBase(c.Key(wt.Path)).FillWidth().Padding(6, 8).Radius(radiusControl).
		Justify(ui.Start).AlignItems(ui.Center).Label(title)
	switch {
	case wt.Current:
		row.Background(colors.accent.Alpha(0.1))
	case row.Hovered():
		row.Background(colors.hover)
	}
	if row.Clicked() {
		a.review.worktreesOpen = false
		if !wt.Current {
			a.openWorktree(wt.Path)
		}
	}
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(smallFontSize).FontWeight(600).MaxLines(1).Shrink(1)
				if wt.Current {
					ui.Spacer(c)
					ui.Text(c, locales.CurrentWorktree()).FontSize(refChipFontSize).TextColor(colors.accent).Shrink(0)
				}
			})
			ui.Text(c, shortenProjectPath(wt.Path)).FontSize(smallFontSize - 1).TextColor(colors.muted).MaxLines(1)
		})
	})
}

func (a *App) openWorktree(path string) {
	if a.desktop != nil {
		a.desktop.openFolder(a, path)
		return
	}
	a.openFolder(path)
}

// compareView chooses what the work tree is compared with.
func (a *App) compareView(c *ui.Context) {
	r := &a.review
	compare, branch := r.compare, r.branch
	ui.Column(c).FillWidth().Shrink(0).Gap(6).Padding(0, compareRowPadding, 8, compareRowPadding).Children(func() {
		a.compareSwitchView(c, &r.compare)
		if r.compare == compareBranch && len(r.branches) > 0 {
			ui.Select(c, &r.branch, otherBranches(r.branches, r.head.Branch)).FillWidth()
		}
	})
	if r.compare != compare || r.branch != branch {
		a.chooseCompare()
	}
}

// compareSwitchView is a switch whose choices share the sidebar's width
// evenly, however narrow it is.
func (a *App) compareSwitchView(c *ui.Context, selected *int) {
	colors := a.look.palette
	labels := []string{locales.CompareReview(), locales.CompareUncommitted(), locales.CompareBranch()}
	parts := ui.SegmentedBase(c.Key("compare"), selected, len(labels))
	parts.Track.FillWidth().Height(compareSwitchHeight).Padding(2).Gap(2).Radius(radiusControl).Background(colors.hover).Children(func() {
		for index, label := range labels {
			segment := parts.Segment(index).Grow(1).Shrink(1).MinWidth(0).FillHeight().Padding(0, 4).Center().
				Radius(radiusControl - 2).Label(label)
			color := colors.muted
			switch {
			case index == *selected:
				segment.Background(colors.background).Border(1, colors.border)
				color = colors.text
			case segment.Hovered():
				color = colors.text
			}
			segment.Children(func() {
				ui.Text(c, label).FontSize(smallFontSize).TextColor(color).MaxLines(1)
			})
		}
	})
}

// chooseCompare lists the changes again against what was just chosen.
func (a *App) chooseCompare() {
	r := &a.review
	a.closeChange()
	if r.compare == compareBranch && r.branch == "" {
		r.branch = defaultBranch(r.branches, r.head.Branch)
	}
	r.listing = review.Listing{}
	r.loaded = false
	r.failure = ""
	a.refreshChanges()
}

// otherBranches are the branches besides current.
func otherBranches(branches []string, current string) []string {
	return slices.DeleteFunc(slices.Clone(branches), func(b string) bool { return b == current })
}

// defaultBranch picks the branch to compare with: the usual main line, or the
// first other branch, or none.
func defaultBranch(branches []string, current string) string {
	others := otherBranches(branches, current)
	for _, candidate := range branchCandidates {
		if slices.Contains(others, candidate) {
			return candidate
		}
	}
	if len(others) > 0 {
		return others[0]
	}
	return ""
}

// followHead reads the history again once HEAD has moved.
func (a *App) followHead() {
	h := &a.history
	if a.review.head.Commit == h.head && (h.commits != nil || h.loading || h.complete) {
		return
	}
	h.head = a.review.head.Commit
	h.commits, h.complete, h.failure = nil, false, ""
	h.loading = false
	h.generation++
	a.loadMoreHistory()
}

// loadMoreHistory reads the next page of the history off the main thread.
func (a *App) loadMoreHistory() {
	h := &a.history
	if h.loading || h.complete {
		return
	}
	h.loading = true
	ctx, root, skip, generation := a.review.ctx, a.root, len(h.commits), h.generation
	go func() {
		commits, err := review.Log(ctx, root, skip, historyPage)
		a.post(func() {
			if ctx.Err() == nil && a.history.generation == generation {
				a.historyLoaded(commits, err)
			}
		})
	}()
}

func (a *App) historyLoaded(commits []review.Commit, err error) {
	h := &a.history
	h.loading = false
	if err != nil {
		slog.Warn("read history", "root", a.root, "skip", len(h.commits), "error", err)
		h.failure = locales.HistoryFailed()
		return
	}
	h.commits = append(h.commits, commits...)
	h.complete = len(commits) < historyPage
}

func (a *App) historyView(c *ui.Context) {
	h := &a.history
	colors := a.look.palette
	ui.Row(c).FillWidth().Shrink(0).Padding(10, 14, 4, 14).BorderWidth(1, 0, 0, 0).BorderColor(colors.border).Children(func() {
		ui.Text(c, locales.History()).FontSize(smallFontSize - 1).FontWeight(600).TextColor(colors.muted)
	})
	switch {
	case h.failure != "":
		a.panelMessageView(c, h.failure)
		return
	case len(h.commits) == 0 && h.loading:
		a.panelMessageView(c, locales.ReadingHistory())
		return
	case len(h.commits) == 0:
		a.panelMessageView(c, locales.NoCommits())
		return
	}
	now := time.Now()
	ui.List(c, &h.list, len(h.commits), func(row int) {
		a.commitRowView(c, h.commits[row], now)
	}).Grow(1).FillWidth().Padding(2, 6)
	if h.list.AtEnd() {
		a.loadMoreHistory()
	}
}

func (a *App) commitRowView(c *ui.Context, commit review.Commit, now time.Time) {
	colors := a.look.palette
	row := ui.Row(c).FillWidth().Height(commitRowHeight).Gap(9).Padding(0, 8).Radius(radiusControl).AlignItems(ui.Center).
		Label(commit.Subject).Clip().Tooltip(commit.Author)
	switch {
	case commit.Hash == a.history.selected:
		row.Background(colors.hover)
	case row.Hovered():
		row.Background(colors.hover.Alpha(0.5))
	}
	if row.Clicked() {
		a.selectCommit(commit, "")
	}
	row.Children(func() {
		dot := colors.border
		if slices.Contains(commit.Refs, "HEAD") {
			dot = colors.accent
		}
		ui.Box(c).Size(historyDotSize, historyDotSize).Radius(historyDotSize / 2).Background(dot).Shrink(0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, commit.Subject).MaxLines(1)
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, commit.Short).Font("monospace").FontSize(smallFontSize - 1).TextColor(colors.muted).Shrink(0)
				ui.Text(c, relativeTime(commit.When, now)).FontSize(smallFontSize - 1).TextColor(colors.muted).MaxLines(1).Shrink(0)
				a.refChipsView(c, commit.Refs)
			})
		})
	})
}

// refChipsView shows the branches and tags at a commit, HEAD among them.
func (a *App) refChipsView(c *ui.Context, refs []string) {
	colors := a.look.palette
	for _, ref := range refs {
		color := colors.muted
		if ref == "HEAD" {
			color = colors.accent
		}
		ui.Text(c, ref).FontSize(refChipFontSize).TextColor(color).Padding(0, 5).Radius(radiusSmall).
			Border(1, color.Alpha(0.4)).MaxLines(1).Shrink(0)
	}
}

// relativeTime says how long ago a moment was, or its date once it is old.
func relativeTime(when time.Time, now time.Time) string {
	elapsed := now.Sub(when)
	switch {
	case elapsed < time.Minute:
		return locales.JustNow()
	case elapsed < time.Hour:
		return locales.MinutesAgo(int(elapsed / time.Minute))
	case elapsed < 24*time.Hour:
		return locales.HoursAgo(int(elapsed / time.Hour))
	case elapsed < 30*24*time.Hour:
		return locales.DaysAgo(int(elapsed / (24 * time.Hour)))
	default:
		return when.Format(time.DateOnly)
	}
}

// selectCommit shows a commit in the main area, at preferred when it changed
// that file, or else at its first file.
func (a *App) selectCommit(commit review.Commit, preferred string) {
	a.closeChange()
	a.closeCommit()
	h := &a.history
	hash := commit.Hash
	h.selected, h.commit, h.preferred = hash, commit, preferred
	ctx, root := a.review.ctx, a.root
	go func() {
		detail, err := review.ReadCommit(ctx, root, hash)
		a.post(func() {
			if ctx.Err() == nil && a.history.selected == hash {
				a.commitLoaded(detail, err)
			}
		})
	}()
}

func (a *App) commitLoaded(detail review.CommitDetail, err error) {
	h := &a.history
	if err != nil {
		slog.Warn("read commit", "root", a.root, "commit", h.selected, "error", err)
		h.failure = locales.HistoryFailed()
		h.selected = ""
		return
	}
	h.detail = &detail
	if slices.ContainsFunc(detail.Changes, func(change review.Change) bool { return change.Path == h.preferred }) {
		a.selectCommitFile(h.preferred)
		return
	}
	if len(detail.Changes) > 0 {
		a.selectCommitFile(detail.Changes[0].Path)
	}
}

// selectCommitFile shows what the shown commit changed in a file.
func (a *App) selectCommitFile(relPath string) {
	h := &a.history
	if h.detail == nil {
		return
	}
	index := slices.IndexFunc(h.detail.Changes, func(change review.Change) bool { return change.Path == relPath })
	if index < 0 {
		return
	}
	h.file, h.diff, h.unreadable, h.landing = relPath, nil, false, landNowhere
	ctx, root, detail, change := a.review.ctx, a.root, *h.detail, h.detail.Changes[index]
	go func() {
		sides, err := review.CommitSides(ctx, root, detail, change)
		a.post(func() {
			if ctx.Err() == nil && a.history.selected == detail.Hash && a.history.file == relPath {
				a.commitSidesLoaded(relPath, sides, err)
			}
		})
	}()
}

func (a *App) commitSidesLoaded(relPath string, sides review.Sides, err error) {
	h := &a.history
	if err != nil {
		slog.Warn("read commit change", "commit", h.selected, "path", relPath, "error", err)
		h.unreadable = true
		return
	}
	h.unreadable = sides.Unreadable
	if !sides.Unreadable {
		h.diff = a.newChangeDiff(relPath, sides)
	}
}

// closeCommit puts the shown commit away and lets go of its texts.
func (a *App) closeCommit() {
	h := &a.history
	h.selected, h.commit, h.preferred = "", review.Commit{}, ""
	h.detail, h.file, h.diff = nil, "", nil
	h.unreadable = false
	h.landing = landNowhere
}

func (a *App) showsCommit() bool {
	return a.sidebarTab == sidebarChanges && a.history.selected != ""
}

// commitView shows a commit: its message, the files it changed, and the change
// of the chosen one.
func (a *App) commitView(c *ui.Context) {
	h := &a.history
	commit := h.commit
	h.landing = landDiff(h.diff, h.unreadable, h.landing)
	c.Key(h.selected)
	a.commitHeaderView(c, commit)
	if h.detail == nil {
		a.changeNoteView(c, locales.ReadingChange())
		return
	}
	body := commitBody(h.detail.Message)
	if body == "" {
		a.commitChangesView(c)
		return
	}
	if h.messageHeight == 0 {
		h.messageHeight = initialMessageHeight
	}
	ui.SplitVertical(c, &h.messageHeight, func() {
		ui.Scroll(c).Fill().Padding(0, 16, 10, 16).Children(func() {
			ui.Text(c, body).FontSize(smallFontSize).TextColor(a.look.palette.muted).FillWidth().Selectable()
		})
	}, func() {
		ui.Column(c).Fill().Children(func() { a.commitChangesView(c) })
	}).Grow(1).FillWidth()
}

// commitBody is a commit message without its subject line.
func commitBody(message string) string {
	_, body, _ := strings.Cut(message, "\n")
	return strings.TrimSpace(body)
}

// commitChangesView lists the files a commit changed beside the change of the
// chosen one.
func (a *App) commitChangesView(c *ui.Context) {
	h := &a.history
	colors := a.look.palette
	ui.Row(c).Grow(1).FillWidth().BorderWidth(1, 0, 0, 0).BorderColor(colors.border).Children(func() {
		ui.Column(c).Width(commitFilesWidth).FillHeight().Shrink(0).BorderWidth(0, 1, 0, 0).BorderColor(colors.border).Children(func() {
			changes := h.detail.Changes
			ui.List(c, &h.files, len(changes), func(row int) {
				change := changes[row]
				if a.fileChangeRowView(c, change, change.Path == h.file, false).row {
					a.selectCommitFile(change.Path)
				}
			}).Grow(1).FillWidth().Padding(6, 6)
		})
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			c.Key(h.file)
			switch {
			case len(h.detail.Changes) == 0:
				a.changeNoteView(c, locales.CommitChangesNothingHere())
			case h.unreadable:
				a.changeNoteView(c, locales.CannotShowAsText())
			case h.diff == nil:
				a.changeNoteView(c, locales.ReadingChange())
			default:
				h.diff.Theme = a.look.code
				a.applyDiffSettings(h.diff)
				editor.DiffView(c, h.diff).Grow(1).FillWidth().ContextMenu(a.diffMenu)
			}
		})
	})
}

// commitHeaderView shows a commit's subject, who made it and when.
func (a *App) commitHeaderView(c *ui.Context, commit review.Commit) {
	colors := a.look.palette
	ui.Column(c).FillWidth().Shrink(0).Gap(6).Padding(12, 16, 10, 16).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, commit.Subject).FontWeight(600).MaxLines(2).Grow(1).Shrink(1)
			if a.history.diff != nil && !a.history.unreadable {
				a.diffLayoutButtonView(c)
			}
			if ui.Button(c, locales.CloseCommit()).Clicked() {
				a.closeCommit()
			}
		})
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, commit.Short).Font("monospace").FontSize(smallFontSize - 1).TextColor(colors.muted)
			a.refChipsView(c, commit.Refs)
			ui.Text(c, commit.Author+" · "+commit.When.Format("2006-01-02 15:04")).FontSize(smallFontSize - 1).TextColor(colors.muted)
		})
	})
}

// moveThroughCommit goes to a neighbouring change of the commit on screen, or
// to the first or last change of its neighbouring file.
func (a *App) moveThroughCommit(step int) {
	h := &a.history
	if h.detail == nil || (h.diff == nil && !h.unreadable) {
		return
	}
	if stepDiff(h.diff, step) {
		return
	}
	next := neighbourChange(h.detail.Changes, h.file, step)
	if next == "" {
		a.tellEndOfChanges(step)
		return
	}
	a.selectCommitFile(next)
	h.landing = landingFor(step)
}

// stopHistory forgets the history, whose reading ends with the review's
// context.
func (a *App) stopHistory() {
	a.history = historyPanel{}
}
