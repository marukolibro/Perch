package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/internal/search"
	"github.com/marukolibro/Perch/internal/session"
	"github.com/marukolibro/Perch/locales"
)

const (
	searchDelay         time.Duration = 150 * time.Millisecond
	searchFlushInterval time.Duration = 60 * time.Millisecond

	searchFieldHeight float32 = 32
	searchOptionSize  float32 = 22
	maxRecentSearches int     = 8
)

// Indexes of the sidebar's switch.
const (
	sidebarFiles   int = 0
	sidebarSearch  int = 1
	sidebarChanges int = 2
)

// resultRow is a row of the search results: a file, or a match in one.
type resultRow struct {
	path   string
	isFile bool
	count  int
	hit    search.Hit
}

// findInFiles is the state of the search across the workspace.
type findInFiles struct {
	query         string
	caseSensitive bool
	regexp        bool
	recent        []session.RecentSearch
	rows          []resultRow
	summary       search.Summary
	running       bool
	finished      bool
	failure       string
	chosen        int
	list          ui.ListState
	input         ui.Handle
	cancel        context.CancelFunc
}

// showFindInFiles turns the sidebar to the search, starting from the text
// selected in the current file.
func (a *App) showFindInFiles() {
	if a.root == "" {
		return
	}
	a.switchSidebarTab(sidebarSearch)
	a.find.input.SelectAll()
	d := a.activeDocument()
	if d == nil || d.editor == nil {
		return
	}
	selected := d.editor.SelectedText()
	if selected != "" && !strings.Contains(selected, "\n") && selected != a.find.query {
		a.find.query = selected
		a.startSearch()
	}
}

// stopSearch ends the running search, if any.
func (a *App) stopSearch() {
	if a.find.cancel != nil {
		a.find.cancel()
		a.find.cancel = nil
	}
	a.find.running = false
}

// startSearch replaces the results with a search for the current query.
func (a *App) startSearch() {
	a.stopSearch()
	a.find.rows = nil
	a.find.summary = search.Summary{}
	a.find.finished = false
	a.find.failure = ""
	a.find.chosen = noRow
	if strings.TrimSpace(a.find.query) == "" {
		return
	}
	query := search.Query{Text: a.find.query, CaseSensitive: a.find.caseSensitive, Regexp: a.find.regexp}
	pattern, err := query.Compile()
	if err != nil {
		a.find.failure = locales.InvalidRegexp()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.find.cancel = cancel
	a.find.running = true
	go a.runSearch(ctx, a.root, pattern)
}

// runSearch waits for typing to pause and streams the results to the window.
// The goroutine belongs to ctx: the next search and opening another folder
// cancel it.
func (a *App) runSearch(ctx context.Context, root string, pattern *search.Pattern) {
	select {
	case <-time.After(searchDelay):
	case <-ctx.Done():
		return
	}
	listing, err := search.ListFiles(ctx, root)
	if err != nil {
		a.finishSearch(ctx, search.Summary{}, err)
		return
	}
	var pending []resultRow
	flushed := time.Now()
	summary, err := search.Text(ctx, root, listing.Paths, pattern, func(hits search.FileHits) {
		pending = append(pending, rowsOf(hits)...)
		if time.Since(flushed) >= searchFlushInterval {
			a.addSearchRows(ctx, pending)
			pending = nil
			flushed = time.Now()
		}
	})
	a.addSearchRows(ctx, pending)
	a.finishSearch(ctx, summary, err)
}

// rememberSearch puts the query whose result was opened first among the
// recent searches, with its options, keeping one of each query. Opening a
// result is what tells a finished query from one typed on the way to it.
func (a *App) rememberSearch(query search.Query) {
	recent := a.find.recent
	if i := slices.IndexFunc(recent, func(previous session.RecentSearch) bool { return previous.Query == query.Text }); i >= 0 {
		recent = slices.Delete(recent, i, i+1)
	}
	recent = recent[:min(len(recent), maxRecentSearches-1)]
	a.find.recent = slices.Insert(recent, 0, session.RecentSearch{
		Query: query.Text, CaseSensitive: query.CaseSensitive, Regexp: query.Regexp,
	})
}

func rowsOf(hits search.FileHits) []resultRow {
	rows := make([]resultRow, 0, len(hits.Hits)+1)
	rows = append(rows, resultRow{path: hits.Path, isFile: true, count: len(hits.Hits)})
	for _, hit := range hits.Hits {
		rows = append(rows, resultRow{path: hits.Path, hit: hit})
	}
	return rows
}

func (a *App) addSearchRows(ctx context.Context, rows []resultRow) {
	if len(rows) == 0 {
		return
	}
	a.post(func() {
		if ctx.Err() != nil {
			return
		}
		a.find.rows = append(a.find.rows, rows...)
		if a.find.chosen == noRow {
			a.find.chosen = nextMatchRow(a.find.rows, noRow, 1)
		}
	})
}

func (a *App) finishSearch(ctx context.Context, summary search.Summary, err error) {
	a.post(func() {
		if ctx.Err() != nil {
			return
		}
		a.find.running = false
		a.find.finished = true
		a.find.summary = summary
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("search files", "root", a.root, "error", err)
			a.find.failure = locales.SearchFailed()
		}
	})
}

// nextMatchRow returns the nearest match row after from in the direction of
// step, or from when there is none.
func nextMatchRow(rows []resultRow, from int, step int) int {
	for row := from + step; row >= 0 && row < len(rows); row += step {
		if !rows[row].isFile {
			return row
		}
	}
	return from
}

func (a *App) moveSearchChoice(step int) {
	a.find.chosen = nextMatchRow(a.find.rows, a.find.chosen, step)
	if a.find.chosen != noRow {
		a.find.list.ScrollIntoView(a.find.chosen)
	}
}

// openSearchRow shows a result: a match at its place, and a file as it opens.
func (a *App) openSearchRow(index int) {
	if index < 0 || index >= len(a.find.rows) {
		return
	}
	row := a.find.rows[index]
	a.rememberSearch(search.Query{Text: a.find.query, CaseSensitive: a.find.caseSensitive, Regexp: a.find.regexp})
	if row.isFile {
		a.openFile(row.path)
		a.chooseTab(a.active)
		return
	}
	a.find.chosen = index
	start := editor.Pos{Line: row.hit.Line, Col: row.hit.Column}
	end := editor.Pos{Line: row.hit.Line, Col: row.hit.EndColumn}
	a.openAt(row.path, start, end)
}

func (a *App) findInFilesView(c *ui.Context) {
	colors := a.look.palette
	ui.Column(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Shrink(0).Gap(6).Padding(4, 10, 8, 10).Children(func() {
			a.searchFieldView(c)
			if status := searchStatus(&a.find); status != "" {
				ui.Text(c, status).FontSize(smallFontSize-1).TextColor(colors.muted).MaxLines(1).Padding(0, 2)
			}
		})
		if strings.TrimSpace(a.find.query) == "" {
			a.searchEmptyView(c)
			return
		}
		ui.Box(c).FillWidth().Height(1).Shrink(0).Background(colors.border)
		a.searchResultsView(c)
	})
}

// searchEmptyView gives the search scope and a way to repeat a past query.
func (a *App) searchEmptyView(c *ui.Context) {
	colors := a.look.palette
	ui.Column(c).FillWidth().Gap(12).Padding(6, 12, 8, 12).Children(func() {
		ui.Text(c, locales.SearchEmptyHint()).FontSize(smallFontSize).TextColor(colors.muted)
		if len(a.find.recent) == 0 {
			return
		}
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Text(c, locales.RecentSearches()).FontSize(smallFontSize).TextColor(colors.muted)
			ui.Row(c).FillWidth().Wrap().Gap(4).AlignItems(ui.Start).Children(func() {
				for i, recent := range a.find.recent {
					if a.recentSearchChip(c.Key(i), recent.Query) {
						a.find.query = recent.Query
						a.find.caseSensitive = recent.CaseSensitive
						a.find.regexp = recent.Regexp
						a.find.input.Focus()
						a.startSearch()
					}
				}
			})
		})
	})
}

// recentSearchChip builds one recent query and reports a click on it.
func (a *App) recentSearchChip(c *ui.Context, query string) bool {
	colors := a.look.palette
	chip := ui.ButtonBase(c).Role(ui.RoleButton).Label(query).Tooltip(query).
		MinWidth(0).Shrink(1).Padding(4, 8).Radius(radiusSmall).Background(colors.sidebar)
	if chip.Hovered() {
		chip.Background(colors.hover)
	}
	if chip.FocusVisible() {
		chip.Border(1, colors.accent)
	}
	chip.Children(func() {
		ui.Text(c, query).FontSize(smallFontSize).MaxLines(1).MinWidth(0)
	})
	return chip.Clicked()
}

// searchFieldView is the search box, with its options inside at the end.
func (a *App) searchFieldView(c *ui.Context) {
	colors := a.look.palette
	box := ui.Row(c).FillWidth().Height(searchFieldHeight).Shrink(0).Gap(6).PaddingStart(9).PaddingEnd(5).
		AlignItems(ui.Center).Radius(radiusControl).Background(colors.sidebar)
	edge := colors.border
	if box.FocusWithin() {
		edge = colors.accent
	}
	box.Border(1, edge).Children(func() {
		ui.Icon(c, iconSearch).FontSize(iconFontSize).TextColor(colors.muted).Shrink(0)
		field := ui.TextInputBase(c, &a.find.query).Bind(&a.find.input).Grow(1).Shrink(1).MinWidth(0).
			Placeholder(locales.SearchInFiles()).Label(locales.SearchInFiles()).
			OnShortcut(0, ui.KeyDown, func() { a.moveSearchChoice(1) }).
			OnShortcut(0, ui.KeyUp, func() { a.moveSearchChoice(-1) })
		if field.Changed() {
			a.startSearch()
		}
		if field.Submitted() {
			a.openSearchRow(a.find.chosen)
		}
		if a.find.query != "" {
			a.searchClearButton(c)
		}
		a.searchOption(c, &a.find.caseSensitive, "Aa", locales.MatchCase())
		a.searchOption(c, &a.find.regexp, ".*", locales.RegularExpression())
	})
}

func (a *App) searchClearButton(c *ui.Context) {
	colors := a.look.palette
	button := ui.Box(c).Size(searchOptionSize, searchOptionSize).Center().Radius(radiusSmall).Shrink(0).Label(locales.ClearSearch())
	if button.Hovered() {
		button.Background(colors.hover)
	}
	if button.Clicked() {
		a.find.query = ""
		a.startSearch()
	}
	button.Children(func() { ui.Icon(c, iconClose).FontSize(12).TextColor(colors.muted) })
}

// searchOption is a small switch inside the search box.
func (a *App) searchOption(c *ui.Context, on *bool, text string, label string) {
	colors := a.look.palette
	option := ui.ToggleBase(c, on).Size(searchOptionSize, searchOptionSize).Center().Radius(radiusSmall).Shrink(0).Label(label).Tooltip(label)
	tint := colors.muted
	switch {
	case *on:
		option.Background(colors.accent.Alpha(0.16))
		tint = colors.accent
	case option.Hovered():
		option.Background(colors.hover)
	}
	if option.Changed() {
		a.startSearch()
	}
	option.Children(func() { ui.Text(c, text).FontSize(smallFontSize - 1).FontWeight(600).TextColor(tint) })
}

// searchStatus says how the search stands.
func searchStatus(find *findInFiles) string {
	switch {
	case find.failure != "":
		return find.failure
	case find.running:
		return locales.Searching()
	case !find.finished:
		return ""
	case find.summary.Hits == 0:
		return locales.NoResults()
	case find.summary.Truncated:
		return locales.FirstResults(find.summary.Hits)
	default:
		return locales.ResultsInFiles(find.summary.Hits, find.summary.Files)
	}
}

func (a *App) searchResultsView(c *ui.Context) {
	rows := a.find.rows
	a.find.list.Key = func(row int) any { return row }
	ui.List(c, &a.find.list, len(rows), func(row int) {
		if rows[row].isFile {
			a.searchFileRow(c, row)
			return
		}
		a.searchHitRow(c, row)
	}).Grow(1).FillWidth().Padding(4, 6)
}

func (a *App) searchFileRow(c *ui.Context, index int) {
	colors := a.look.palette
	result := a.find.rows[index]
	name := path.Base(result.path)
	row := ui.Row(c).FillWidth().Gap(6).Padding(5, 6, 3, 6).AlignItems(ui.Center)
	if row.Clicked() {
		a.openSearchRow(index)
		return
	}
	row.Children(func() {
		a.fileIconView(c, result.path, smallFontSize)
		ui.Text(c, name).FontSize(smallFontSize).FontWeight(600).MaxLines(1).Shrink(0)
		if dir := path.Dir(result.path); dir != "." {
			ui.Text(c, dir).FontSize(smallFontSize - 1).TextColor(colors.muted).MaxLines(1).Shrink(1)
		}
		ui.Spacer(c)
		ui.Text(c, fmt.Sprint(result.count)).FontSize(smallFontSize - 1).TextColor(colors.muted)
	})
}

func (a *App) searchHitRow(c *ui.Context, index int) {
	colors := a.look.palette
	hit := a.find.rows[index].hit
	row := ui.Row(c).FillWidth().Gap(8).Padding(3, 6, 3, 12).Radius(radiusControl).AlignItems(ui.Center)
	switch {
	case index == a.find.chosen:
		row.Background(colors.hover)
	case row.Hovered():
		row.Background(colors.hover.Alpha(0.5))
	}
	if row.Clicked() {
		a.openSearchRow(index)
		return
	}
	row.Children(func() {
		ui.Text(c, fmt.Sprint(hit.Line+1)).FontSize(smallFontSize - 1).TextColor(colors.muted).Shrink(0)
		marks := []textMark{{start: hit.PreviewStart, end: hit.PreviewEnd}}
		markedText(c, hit.Preview, marks, colors.text, colors.accent)
	})
}
