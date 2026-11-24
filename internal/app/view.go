package app

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/marukolibro/Perch/locales"
)

const (
	headerHeight    float32 = 40
	pathBarHeight   float32 = 36
	statusBarHeight float32 = 24
	// controlsGap is the room left between the window's own controls and the
	// first of Perch's beside them.
	controlsGap      float32 = 8
	markdownMaxWidth float32 = 780
	// readingPadTop and readingPadSide are the room around rendered Markdown.
	readingPadTop  float32 = 36
	readingPadSide float32 = 40
	smallFontSize  float32 = 12
	iconFontSize   float32 = 14

	sidebarTabWidth    float32 = 34
	sidebarTabHeight   float32 = 28
	sidebarTabIconSize float32 = 16
	// sidebarMarkSize is the dot on the Git button that says files are left
	// to read, and sidebarMarkInset how far in from the button's corner it sits.
	sidebarMarkSize   float32 = 7
	sidebarMarkInset  float32 = 5
	projectNameHeight float32 = 30
	minSidebarWidth   float32 = 232

	tabHeight   float32 = 30
	tabIconSize float32 = 13
	// inactiveTabIconAlpha quiets the icons of the tabs behind the chosen one,
	// as their names are.
	inactiveTabIconAlpha float32 = 0.7
	panelMargin          float32 = 8
	// panelGap is the space each panel keeps from the divider between them.
	panelGap     float32 = 4
	treeRowInset float32 = 8
	// tabTextLift raises a tab's name, whose lowercase letters sit below the
	// middle of its line, to the middle of the tab.
	tabTextLift float32 = 2
)

// view builds the window.
func (a *App) view(c *ui.Context) {
	a.pathPopover = nil
	a.look = a.lookFor(c.Theme())
	c.SetTheme(a.look.theme)
	c.SetMenuPresentation(ui.PopoverMenus)
	defer a.grainView(c)
	a.toastsView(c)
	if a.root == "" {
		a.welcomeView(c)
		a.outlineView(c)
		a.settingsView(c)
		return
	}
	a.setWindowHidden(c.WindowHidden())
	a.sidebarWidth = max(a.sidebarWidth, minSidebarWidth)
	if a.sidebarHidden {
		a.mainView(c)
	} else {
		ui.Themed(c, a.look.shell).Fill().Children(func() {
			ui.Split(c, &a.sidebarWidth, func() {
				ui.Themed(c, a.look.theme).Fill().Children(func() { a.sidebarView(c) })
			}, func() {
				ui.Themed(c, a.look.theme).Fill().Children(func() { a.mainView(c) })
			}).Fill()
		})
	}
	a.quickOpenView(c)
	a.outlineView(c)
	a.projectSymbolsView(c)
	a.referencesView(c)
	a.settingsView(c)
	a.fileDialogView(c)
	a.closeTabAlert(c)
	a.closeWindowAlert(c)
	a.trackSession()
}

func (a *App) sidebarView(c *ui.Context) {
	colors := a.look.palette
	ui.Column(c).Fill().Background(colors.sidebar).Children(func() {
		a.sidebarHeaderView(c)
		ui.Column(c).Grow(1).FillWidth().Padding(0, panelGap, panelMargin, panelMargin).Children(func() {
			a.panelView(c, func() {
				a.projectNameView(c)
				a.sidebarContentView(c)
			})
		})
	})
}

// sidebarContentView shows the sidebar's chosen view.
func (a *App) sidebarContentView(c *ui.Context) {
	switch a.sidebarTab {
	case sidebarSearch:
		a.findInFilesView(c)
	case sidebarChanges:
		a.changesPanelView(c)
	default:
		a.fileTreeView(c)
	}
}

// panelView builds one of the window's rounded panels.
func (a *App) panelView(c *ui.Context, content func()) {
	colors := a.look.palette
	ui.Column(c).Fill().Clip().Radius(radiusCard).Background(colors.background).Border(1, colors.border).Children(content)
}

// sidebarHeaderView puts the sidebar's views beside the window's controls.
func (a *App) sidebarHeaderView(c *ui.Context) {
	bar := c.TitleBar()
	ui.Row(c).FillWidth().Height(headerHeight).Shrink(0).Gap(2).PaddingStart(bar.Left + controlsGap).PaddingEnd(8).
		AlignItems(ui.Center).DragWindow().Children(func() {
		if a.sidebarTabView(c, iconFiles, locales.TabFiles(), a.sidebarTab == sidebarFiles, false) {
			a.switchSidebarTab(sidebarFiles)
		}
		if a.sidebarTabView(c, iconSearch, locales.TabSearch(), a.sidebarTab == sidebarSearch, false) {
			a.showFindInFiles()
		}
		if a.sidebarTabView(c, iconChanges, gitTabLabel, a.sidebarTab == sidebarChanges, a.review.unread > 0) {
			a.showChanges()
		}
		ui.Spacer(c)
		if a.sidebarTabView(c, iconSettings, locales.SettingsTitle(), false, false) {
			a.showSettings()
		}
	})
}

// projectNameView names the project on a line of its own, with room for a long
// name, and shows where it is when the pointer rests on it.
func (a *App) projectNameView(c *ui.Context) {
	colors := a.look.palette
	row := ui.Row(c).FillWidth().Height(projectNameHeight).Shrink(0).Padding(0, 14).Gap(4).
		AlignItems(ui.Center).Justify(ui.SpaceBetween)
	row.Children(func() {
		name := ui.Text(c, filepath.Base(a.root)).FontWeight(600).MaxLines(1).Shrink(1).MinWidth(0)
		sidebarTooltip(c, name, a.root, colors)
		if a.sidebarTab == sidebarFiles {
			toolbar := ui.Row(c).AlignItems(ui.Center).Gap(2).Shrink(0)
			// Keep the buttons in the tab order and accessibility tree while hidden.
			if !row.Hovered() && !row.FocusWithin() {
				toolbar.Opacity(0)
			}
			toolbar.Children(func() { a.fileTreeToolbarView(c) })
		}
	})
}

// sidebarTabView builds a button of the sidebar's views and reports a click.
// When marked is true and the view is not showing, a quiet dot sits at its top
// right corner.
func (a *App) sidebarTabView(c *ui.Context, icon *ui.SVG, label string, active bool, marked bool) bool {
	colors := a.look.palette
	button := ui.Box(c).Size(sidebarTabWidth, sidebarTabHeight).Center().Radius(radiusControl).Label(label)
	sidebarTooltip(c, button, label, colors)
	color := colors.muted
	switch {
	case active:
		button.Background(colors.accent.Alpha(0.14))
		color = colors.accent
	case button.Hovered():
		button.Background(colors.hover)
		color = colors.text
	}
	clicked := button.Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(sidebarTabIconSize).TextColor(color)
		if marked && !active {
			// The dot only tells, so it takes no pointer of its own: the button
			// around it is what the click and assistive technology reach.
			ui.Box(c).Absolute().Top(sidebarMarkInset).Right(sidebarMarkInset).Size(sidebarMarkSize, sidebarMarkSize).
				Radius(sidebarMarkSize / 2).Background(colors.accent).IgnoreInput(true).AccessHidden(true)
		}
	})
	return clicked
}

// sidebarTooltip names a sidebar button below it, clear of the window's controls
// above the sidebar.
func sidebarTooltip(c *ui.Context, button ui.Element, label string, colors palette) {
	ui.TooltipBase(c, button, func(tip ui.Element) {
		tip.AttachTo(button, ui.AnchorBottom, ui.AnchorTop).Margin(6, 0, 0, 0).Padding(4, 8).Radius(radiusSmall).
			Background(colors.text).TextColor(colors.background).FontSize(smallFontSize)
		tip.Children(func() { ui.Text(c, label) })
	})
}

func (a *App) fileTreeView(c *ui.Context) {
	ui.Themed(c, a.look.tree).Grow(1).FillWidth().Children(func() {
		selected := a.selectedTreePath()
		_, listed := a.children["."]
		roots := a.childPaths(".")
		if len(roots) == 0 {
			a.treeRow = noRow
		}
		a.tree.List.Selected = &a.treeRow
		clicked := ""
		tree := ui.Outline(c, &a.tree, roots, a.treeChildPaths, func(item string) {
			if a.treeRowView(c, item) {
				clicked = item
			}
		}).Grow(1).FillWidth().Padding(0, treeRowInset).Label(locales.TabFiles()).ContextMenu(func(m *ui.Menu) {
			a.fileMenu(m, ".")
		})
		if !listed {
			a.pruneOpenTreePaths()
		}
		if a.pendingReveal != "" {
			target := a.pendingReveal
			a.pendingReveal = ""
			if row := a.treeRowFor(target); row != noRow {
				a.tree.List.Choose(row)
				a.tree.List.ScrollIntoView(row)
				c.After(0)
			}
		}
		switch {
		case clicked != "":
			a.clickTreeItem(clicked)
		case tree.Changed():
			a.followTreeChoice()
		case selected != "" && a.treeRow == noRow:
			// The list follows its chosen row by key only so far; after a large
			// collapse the row may have moved farther, so it is found again.
			if row := a.treeRowFor(selected); row != noRow {
				a.tree.List.Choose(row)
				c.After(0)
			}
		}
	})
}

// treeChildPaths gives the tree a folder's children, and nothing for a file.
func (a *App) treeChildPaths(item string) []string {
	if !a.entries[item].IsDir {
		return nil
	}
	return a.childPaths(item)
}

// selectedTreePath returns the chosen row's path, empty when none is chosen.
func (a *App) selectedTreePath() string {
	if a.treeRow < 0 || a.treeRow >= a.tree.Rows() {
		return ""
	}
	return a.tree.Item(a.treeRow)
}

// treeRowFor returns the row that shows a path, noRow when it is hidden.
func (a *App) treeRowFor(item string) int {
	for row := range a.tree.Rows() {
		if a.tree.Item(row) == item {
			return row
		}
	}
	return noRow
}

// treeRowView builds one row of the tree and reports a click on it, which
// the arrow's own clicks are not.
func (a *App) treeRowView(c *ui.Context, item string) bool {
	entry := a.entries[item]
	colors := a.look.palette
	ignored := a.treeRowIgnored(item)
	quiet := colors.muted.Alpha(ignoredRowAlpha)
	row := ui.Row(c).Grow(1).FillWidth().Gap(7).AlignItems(ui.Center)
	row.Children(func() {
		a.treeIconView(c, item, entry.IsDir, ignored)
		name := ui.Text(c, entry.Name).MaxLines(1).Shrink(1)
		if color, changed := a.treeNameColor(item); changed {
			name.TextColor(color)
		} else if ignored {
			name.TextColor(quiet)
		}
		a.treeStatusView(c, item, entry.IsDir)
	}).ContextMenu(func(m *ui.Menu) {
		a.fileMenu(m, item)
	})
	return row.RowClicked()
}

// clickTreeItem opens or closes a clicked folder, on every click of its row
// and not only the one that chooses it, and opens a clicked file.
func (a *App) clickTreeItem(item string) {
	if a.entries[item].IsDir {
		toggleOpen(&a.tree.Open, item)
		return
	}
	a.openFile(item)
}

// followTreeChoice opens the file the keys chose. A folder they choose stays
// as it is: Right and Left open and close it.
func (a *App) followTreeChoice() {
	item := a.selectedTreePath()
	if item == "" || a.entries[item].IsDir {
		return
	}
	a.openFile(item)
}

func toggleOpen(open *ui.Selection[string], item string) {
	if open.Has(item) {
		open.Remove(item)
		return
	}
	open.Add(item)
}

// chooseTab shows a tab and marks its file in the tree when the row shows.
func (a *App) chooseTab(index int) {
	a.closeOutline()
	a.leaveGitView()
	a.active = index
	d := a.documents[index]
	if !d.external {
		a.recordRecentFile(d.path)
	}
	a.treeRow = a.treeRowFor(d.path)
}

// mainView floats the open file on a rounded panel below its tabs.
func (a *App) mainView(c *ui.Context) {
	a.tabShortcuts(c)
	colors := a.look.palette
	leftPad := panelGap
	if a.sidebarHidden {
		leftPad = panelMargin
	}
	ui.Column(c).Fill().Background(colors.sidebar).Children(func() {
		a.tabsView(c)
		ui.Column(c).Grow(1).FillWidth().Padding(0, panelMargin, panelMargin, leftPad).Children(func() {
			a.panelView(c, func() { a.panelContentView(c) })
		})
	})
}

// panelContentView shows the review, the open file, or an invitation to open one.
func (a *App) panelContentView(c *ui.Context) {
	d := a.activeDocument()
	a.syncBreadcrumb(c, d)
	if a.showsChange() {
		a.changeView(c)
		return
	}
	if a.showsCommit() {
		a.commitView(c)
		return
	}
	if d == nil {
		a.noFileView(c)
		return
	}
	a.pathBarView(c, d)
	a.conflictView(c, d)
	a.documentView(c, d)
	a.statusView(c, d)
}

func (a *App) tabsView(c *ui.Context) {
	colors := a.look.palette
	var closing *document
	// On Windows and Linux the window's controls sit at the end of the tabs.
	bar := c.TitleBar()
	leftPad := panelGap
	if a.sidebarHidden {
		leftPad = max(panelMargin, bar.Left+controlsGap)
	}
	ui.Row(c).FillWidth().Height(headerHeight).Shrink(0).Gap(4).PaddingStart(leftPad).PaddingEnd(max(panelMargin, bar.Right+controlsGap)).
		AlignItems(ui.Center).Background(colors.sidebar).DragWindow().Children(func() {
		if a.sidebarHidden && a.sidebarTabView(c, iconSidebar, locales.ShowSidebar(), false, false) {
			a.toggleSidebar()
		}
		ui.ScrollHorizontal(c).Shrink(1).FillHeight().Children(func() {
			ui.Row(c).FillHeight().Gap(4).AlignItems(ui.Center).Children(func() {
				for i, d := range a.documents {
					c.Key(d.path)
					if a.tabView(c, d, i == a.active, i) {
						closing = d
					}
				}
			})
		})
	})
	if closing != nil {
		a.requestCloseTab(closing)
	}
}

// tabView builds a tab and reports a click on its close button.
func (a *App) tabView(c *ui.Context, d *document, active bool, index int) bool {
	colors := a.look.palette
	closed := false
	tab := ui.Row(c).Height(tabHeight).Gap(6).PaddingStart(12).PaddingEnd(6).AlignItems(ui.Center).Radius(radiusControl).
		ContextMenu(func(m *ui.Menu) { a.tabMenu(m, d, index) })
	if active {
		tab.Background(colors.background).Border(1, colors.border)
	} else if tab.Hovered() {
		tab.Background(colors.hover.Alpha(0.5))
	}
	if tab.Clicked() {
		a.chooseTab(index)
	}
	hovered := tab.Hovered()
	tab.Children(func() {
		icon := a.fileIconView(c, d.path, tabIconSize)
		if !active {
			icon.Opacity(inactiveTabIconAlpha)
		}
		name := ui.Text(c, d.name()).MaxLines(1).Padding(0, 0, tabTextLift, 0)
		if !active {
			name.TextColor(colors.muted)
		}
		closed = a.tabCloseView(c, d, hovered || active)
	})
	return closed
}

// tabCloseView shows the unsaved mark of a tab until the pointer comes to
// the tab, and then its close button.
func (a *App) tabCloseView(c *ui.Context, d *document, showClose bool) bool {
	colors := a.look.palette
	button := ui.Box(c).Size(18, 18).Center().Radius(radiusSmall).Label(locales.ClosePath(d.path))
	if button.Hovered() {
		button.Background(colors.hover)
	}
	clicked := button.Clicked()
	button.Children(func() {
		switch {
		case d.modified() && !button.Hovered():
			ui.Box(c).Size(7, 7).Radius(4).Background(colors.accent)
		case showClose:
			ui.Icon(c, iconClose).FontSize(12).TextColor(colors.muted)
		}
	})
	return clicked
}

// fitsInView reports whether every line of an editor shows at once, as
// laid out in the last frame. Such a text then cannot scroll past its end,
// so that it has nothing to scroll and shows no scroll bar; a longer one
// still scrolls its last line up to the top.
func fitsInView(state *editor.State) bool {
	viewport := state.ViewportHeight()
	return viewport > 0 && state.LineTop(state.Buffer.LineCount()) <= viewport
}

// pathCrumbsView shows a file's path, its folders quieter than its name.
func (a *App) pathCrumbsView(c *ui.Context, relPath string) {
	colors := a.look.palette
	ui.Row(c).MinWidth(0).ClipX().Gap(5).AlignItems(ui.Center).Children(func() {
		for {
			part, rest, folder := strings.Cut(relPath, "/")
			text := ui.Text(c, part).FontSize(smallFontSize).MinWidth(0).MaxLines(1)
			if !folder {
				break
			}
			text.TextColor(colors.muted)
			ui.Text(c, "›").FontSize(smallFontSize).TextColor(colors.muted)
			relPath = rest
		}
	})
}

// pathBarView lets the path shrink without squeezing the controls on its right.
func (a *App) pathBarView(c *ui.Context, d *document) {
	colors := a.look.palette
	ui.Row(c).FillWidth().Height(pathBarHeight).Shrink(0).Gap(5).Padding(0, 14).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colors.border).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			if d.external {
				a.pathCrumbsView(c, d.displayPath())
			} else {
				a.pathLinksView(c, d.path)
			}
			a.symbolCrumbsView(c, d)
		})
		ui.Row(c).Shrink(0).Gap(5).AlignItems(ui.Center).Children(func() {
			if d.external {
				ui.Text(c, locales.ReadOnly()).FontSize(smallFontSize - 1).TextColor(colors.muted)
			}
			switch {
			case d.isMarkdown():
				ui.Segmented(c, &d.markdownView, locales.MarkdownPreview(), locales.MarkdownEdit())
			case d.isSVG():
				ui.Segmented(c, &d.markdownView, locales.MarkdownPreview(), locales.MarkdownSource())
			}
			a.gitButtonsView(c, d)
		})
	})
}

// bannerView shows a message tinted with color, with actions at its end.
func (a *App) bannerView(c *ui.Context, message string, color ui.Color, actions func()) {
	ui.Row(c).FillWidth().Shrink(0).Gap(8).Padding(7, 14).AlignItems(ui.Center).Background(color.Alpha(0.1)).Children(func() {
		ui.Text(c, message).TextColor(color).Grow(1).Shrink(1)
		actions()
	})
}

// conflictView asks which version wins when the file on disk disagrees with
// the document.
func (a *App) conflictView(c *ui.Context, d *document) {
	if d.conflict == conflictNone {
		return
	}
	message, keep, drop := conflictWording(d.conflict)
	a.bannerView(c, message, a.look.palette.warning, func() {
		if ui.Button(c, drop).Clicked() {
			a.useDisk(d)
		}
		if d.editor == nil {
			return
		}
		if ui.PrimaryButton(c, keep).Clicked() {
			a.keepMine(d)
		}
	})
}

func conflictWording(kind conflict) (message string, keep string, drop string) {
	if kind == conflictRemoved {
		return locales.ConflictRemoved(), locales.RestoreFile(), locales.CloseTab()
	}
	return locales.ConflictChanged(), locales.KeepMine(), locales.UseDiskVersion()
}

func (a *App) documentView(c *ui.Context, d *document) {
	c.Key(d.path)
	a.followLineBaseline(d)
	switch {
	case d.isRasterImage():
		a.rasterImageView(c, d)
		return
	case d.showsSVGPreview():
		a.svgImageView(c, d)
		return
	}
	if d.showsPreview() {
		d.markdownOptions.Theme = a.look.markdown
		ui.Scroll(c).TrackScroll(&d.previewScroll).Grow(1).FillWidth().Padding(readingPadTop, readingPadSide).AlignItems(ui.Center).Children(func() {
			a.markdownPreviewView(c, d, func(body string) ui.Element { return markdown.View(c, body, d.markdownOptions) })
		})
		return
	}
	d.editor.Theme = a.look.code
	a.applyEditorSettings(d.editor)
	d.editor.NoScrollPastEnd = fitsInView(d.editor)
	if d.scrollPending && d.editor.ViewportHeight() > 0 {
		d.editor.SetScrollY(d.restoredScroll)
		d.scrollPending = false
	}
	editor.View(c, d.editor).Bind(&d.focus).Grow(1).FillWidth()
	if d.scrollPending {
		c.After(0)
	}
}

func (a *App) statusView(c *ui.Context, d *document) {
	colors := a.look.palette
	item := func(text string) {
		ui.Text(c, text).FontSize(smallFontSize - 1).TextColor(colors.muted)
	}
	if d.isRasterImage() || d.showsSVGPreview() {
		ui.Row(c).FillWidth().Height(statusBarHeight).Shrink(0).Padding(0, 14).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(colors.border).Children(func() {
			ui.Spacer(c)
			item(d.image.status())
		})
		return
	}
	position := d.editor.CursorPos()
	language := locales.PlainText()
	if l := d.editor.Buffer.Language(); l != nil {
		language = l.Name
	}
	state := locales.StatusSaved()
	if d.modified() {
		state = locales.StatusEdited()
	}
	ui.Row(c).FillWidth().Height(statusBarHeight).Shrink(0).Gap(16).Padding(0, 14).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(colors.border).Children(func() {
		item(state)
		if a.definition.pending {
			item(locales.FindingDefinition())
		}
		ui.Spacer(c)
		item(locales.CursorPosition(position.Line+1, position.Col+1))
		item(locales.LineCount(d.editor.Buffer.LineCount()))
		item(language)
	})
}

const (
	alertDiscard int = 1
	alertSave    int = 2
)

func (a *App) closeTabAlert(c *ui.Context) {
	if a.closingDocument == nil {
		return
	}
	d := a.closingDocument
	title := locales.SaveChangesTo(d.name())
	proceed := false
	switch ui.AlertDialog(c, &a.askCloseTab, title, locales.UnsavedDetail(), locales.Cancel(), locales.DontSave(), locales.Save()) {
	case alertDiscard:
		a.closeTab(d)
		proceed = true
	case alertSave:
		if err := a.save(d); err != nil {
			a.notice = saveFailure(d.name(), err)
			a.pendingClose = nil
		} else {
			a.closeTab(d)
			proceed = true
		}
	}
	if !a.askCloseTab {
		a.closingDocument = nil
		if proceed {
			a.continuePendingClose()
		} else {
			a.pendingClose = nil
		}
	}
}

func (a *App) closeWindowAlert(c *ui.Context) {
	if !a.askCloseWindow {
		return
	}
	switch ui.AlertDialog(c, &a.askCloseWindow, locales.SaveBeforeClosing(), locales.UnsavedDetail(), locales.Cancel(), locales.DontSave(), locales.SaveAll()) {
	case alertDiscard:
		a.confirmCloseWindow()
	case alertSave:
		if a.saveAll() {
			a.confirmCloseWindow()
		}
	}
}
