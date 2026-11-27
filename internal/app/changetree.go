package app

import (
	"context"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/marukolibro/Perch/internal/review"
	"github.com/marukolibro/Perch/locales"
)

const changeTreeIndent float32 = 14

// changeTreeRow is one visible row of the changes tree: either a (possibly
// compacted) folder or a changed file.
type changeTreeRow struct {
	isDir       bool
	dir         string
	label       string
	depth       int
	totalCount  int
	unreadCount int
	change      review.Change
}

type changeTreeNode struct {
	name   string
	dir    string
	dirs   map[string]*changeTreeNode
	files  []review.Change
	total  int
	unread int
}

// visibleChanges returns the changes shown under the current filter.
func (r *changesPanel) visibleChanges() []review.Change {
	if r.compare != compareReview || !r.unreadOnly {
		return r.listing.Changes
	}
	filtered := make([]review.Change, 0, len(r.listing.Changes))
	for _, change := range r.listing.Changes {
		if !change.Reviewed {
			filtered = append(filtered, change)
		}
	}
	return filtered
}

// buildChangeTreeRows groups changes into a directory tree with single-child
// folder chains compacted (e.g. "internal/app").
func buildChangeTreeRows(changes []review.Change, collapsed map[string]bool) []changeTreeRow {
	root := &changeTreeNode{dirs: map[string]*changeTreeNode{}}
	for _, change := range changes {
		dir := path.Dir(change.Path)
		node := root
		if dir != "." {
			acc := ""
			for seg := range strings.SplitSeq(dir, "/") {
				if acc == "" {
					acc = seg
				} else {
					acc += "/" + seg
				}
				child, exists := node.dirs[seg]
				if !exists {
					child = &changeTreeNode{name: seg, dir: acc, dirs: map[string]*changeTreeNode{}}
					node.dirs[seg] = child
				}
				node = child
			}
		}
		node.files = append(node.files, change)
	}
	tallyChangeTree(root)
	var rows []changeTreeRow
	appendChangeTreeRows(&rows, root, 0, collapsed)
	return rows
}

func tallyChangeTree(node *changeTreeNode) (total int, unread int) {
	for _, child := range node.dirs {
		ct, cu := tallyChangeTree(child)
		total += ct
		unread += cu
	}
	for _, f := range node.files {
		total++
		if !f.Reviewed {
			unread++
		}
	}
	node.total, node.unread = total, unread
	return total, unread
}

func appendChangeTreeRows(rows *[]changeTreeRow, node *changeTreeNode, depth int, collapsed map[string]bool) {
	names := make([]string, 0, len(node.dirs))
	for name := range node.dirs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child, label := compactChangeDir(node.dirs[name])
		*rows = append(*rows, changeTreeRow{
			isDir:       true,
			dir:         child.dir,
			label:       label,
			depth:       depth,
			totalCount:  child.total,
			unreadCount: child.unread,
		})
		if !collapsed[child.dir] {
			appendChangeTreeRows(rows, child, depth+1, collapsed)
		}
	}
	for _, file := range node.files {
		*rows = append(*rows, changeTreeRow{
			isDir:  false,
			depth:  depth,
			change: file,
		})
	}
}

// compactChangeDir merges chains of directories that contain only one subfolder
// and no files into a single row like "internal/app".
func compactChangeDir(node *changeTreeNode) (*changeTreeNode, string) {
	label := node.name
	for len(node.files) == 0 && len(node.dirs) == 1 {
		for _, only := range node.dirs {
			node = only
			label += "/" + node.name
		}
	}
	return node, label
}

// toggleChangeDir expands or collapses a directory row in the changes tree.
func (a *App) toggleChangeDir(dir string) {
	if a.review.collapsedDirs == nil {
		a.review.collapsedDirs = map[string]bool{}
	}
	if a.review.collapsedDirs[dir] {
		delete(a.review.collapsedDirs, dir)
		return
	}
	a.review.collapsedDirs[dir] = true
}

// setDirReviewed marks every changed file under dir reviewed or unread.
func (a *App) setDirReviewed(dir string, reviewed bool) {
	r := &a.review
	version := r.listing.Version
	changes := slices.Clone(r.listing.Changes)
	prefix := dir + "/"
	var marked []review.Change
	for index := range changes {
		if !strings.HasPrefix(changes[index].Path, prefix) {
			continue
		}
		if changes[index].Reviewed == reviewed {
			continue
		}
		marked = append(marked, changes[index])
		changes[index].Reviewed = reviewed
	}
	if len(marked) == 0 {
		return
	}
	r.listing.Changes = changes
	a.runReviewAction(func(ctx context.Context, store *review.Store) error {
		if reviewed {
			return store.MarkReviewed(ctx, version, marked)
		}
		return store.MarkUnreviewed(ctx, marked)
	})
}

// changesToolbarView shows the review progress and the tree / unread toggles.
func (a *App) changesToolbarView(c *ui.Context) {
	r := &a.review
	changes := r.listing.Changes
	ui.Row(c).FillWidth().Shrink(0).Padding(0, 8, 4, 14).Gap(4).AlignItems(ui.Center).Children(func() {
		if r.compare == compareReview {
			read := len(changes) - unreadCount(changes)
			ui.Text(c, locales.ReviewProgress(read, len(changes))).
				FontSize(smallFontSize - 1).TextColor(a.look.palette.muted).MaxLines(1).Shrink(1)
		}
		ui.Spacer(c)
		if r.compare == compareReview {
			if a.pathBarButton(c, iconEyeOff, locales.HideReviewed(), r.unreadOnly).Clicked() {
				r.unreadOnly = !r.unreadOnly
			}
		}
		treeTip := locales.ChangeViewTree()
		if r.treeView {
			treeTip = locales.ChangeViewFlat()
		}
		if a.pathBarButton(c, iconTree, treeTip, r.treeView).Clicked() {
			r.treeView = !r.treeView
		}
	})
}

func (a *App) changeTreeRowView(c *ui.Context, item changeTreeRow) {
	if item.isDir {
		a.changeTreeDirRowView(c, item)
		return
	}
	a.changeTreeFileRowView(c, item)
}

func (a *App) changeTreeDirRowView(c *ui.Context, item changeTreeRow) {
	colors := a.look.palette
	r := &a.review
	reviewing := r.compare == compareReview
	collapsed := r.collapsedDirs[item.dir]
	allReviewed := item.unreadCount == 0
	row := ui.Row(c).FillWidth().Height(changeRowHeight).Gap(6).
		Padding(0, 8, 0, 8+float32(item.depth)*changeTreeIndent).Radius(radiusControl).AlignItems(ui.Center)
	if row.Hovered() {
		row.Background(colors.hover.Alpha(0.5))
	}
	rowClicked := row.Clicked()
	markClicked := false
	row.Children(func() {
		if reviewing {
			markClicked = a.dirMarkView(c, item.dir, allReviewed)
		}
		chevron := iconChevronDown
		if collapsed {
			chevron = iconChevronRight
		}
		ui.Icon(c, chevron).FontSize(12).TextColor(colors.muted).Shrink(0)
		a.folderIconView(c, path.Base(item.dir), !collapsed)
		label := ui.Text(c, item.label).FontSize(smallFontSize).MaxLines(1).Shrink(1).MinWidth(0)
		if reviewing && allReviewed {
			label.TextColor(colors.muted)
		}
		ui.Spacer(c)
		count := fmt.Sprint(item.totalCount)
		if reviewing && item.unreadCount > 0 && item.unreadCount < item.totalCount {
			count = fmt.Sprintf("%d/%d", item.unreadCount, item.totalCount)
		}
		ui.Text(c, count).FontSize(smallFontSize - 1).TextColor(colors.muted).Shrink(0)
	})
	switch {
	case markClicked:
		a.setDirReviewed(item.dir, !allReviewed)
	case rowClicked:
		a.toggleChangeDir(item.dir)
	}
}

func (a *App) dirMarkView(c *ui.Context, dir string, reviewed bool) bool {
	colors := a.look.palette
	label := locales.MarkFolderReviewed(dir)
	icon, color := iconCircle, colors.muted
	if reviewed {
		label = locales.MarkFolderUnread(dir)
		icon, color = iconCheck, colors.accent
	}
	button := ui.Box(c).Size(changeMarkSize, changeMarkSize).Center().Shrink(0).Label(label)
	clicked := button.Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(iconFontSize).TextColor(color)
	})
	return clicked
}

func (a *App) changeTreeFileRowView(c *ui.Context, item changeTreeRow) {
	colors := a.look.palette
	change := item.change
	reviewing := a.review.compare == compareReview
	selected := change.Path == a.review.selected
	row := ui.Row(c).FillWidth().Height(changeRowHeight).Gap(7).
		Padding(0, 8, 0, 8+float32(item.depth)*changeTreeIndent).Radius(radiusControl).AlignItems(ui.Center)
	switch {
	case selected:
		row.Background(colors.hover)
	case row.Hovered():
		row.Background(colors.hover.Alpha(0.5))
	}
	rowClicked := row.Clicked()
	markClicked := false
	row.Children(func() {
		if reviewing {
			markClicked = a.changeMarkView(c, change)
		}
		letter, color := kindLetter(change.Kind, colors)
		ui.Text(c, letter).Font("monospace").FontSize(smallFontSize - 1).FontWeight(600).TextColor(color).Width(kindLetterWidth).Shrink(0)
		text := ui.Text(c, path.Base(change.Path)).MaxLines(1).Shrink(1)
		if reviewing && change.Reviewed {
			text.TextColor(colors.muted)
		}
		if change.Kind == review.Renamed {
			ui.Text(c, "← "+change.OldPath).FontSize(smallFontSize - 1).TextColor(colors.muted).MaxLines(1).Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		a.changeCountsView(c, change)
	})
	switch {
	case markClicked:
		a.setReviewed(change.Path, !change.Reviewed)
	case rowClicked:
		a.selectChange(change.Path)
	}
}
