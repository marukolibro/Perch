package app

import (
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/locales"
)

// splitLayout is how the main panel divides into two panes.
type splitLayout uint8

const (
	splitNone splitLayout = iota
	// splitRight shows the panes side by side.
	splitRight
	// splitDown shows one pane above the other.
	splitDown
)

const (
	initialSplitPercent float32 = 50
	// otherPaneTextAlpha quiets the path of the pane without the keyboard.
	otherPaneTextAlpha float32 = 0.7
)

// splitState is the other pane of a split panel. The focused pane shows the
// active document with its own editor, as without a split, so that every
// command keeps working on what has the keyboard. The other pane shows its
// document in an editor of its own: the document's, or one sharing its
// buffer while the focused pane shows the same document, so that each pane
// keeps its own scroll and caret. A click in the other pane trades the
// roles, and the panes keep their places.
type splitState struct {
	layout splitLayout
	// doc is the document the other pane shows.
	doc *document
	// view is the editor the other pane shows doc in, nil for images.
	view  *editor.State
	focus ui.Handle
	// previewScroll is where the other pane's rendered Markdown is while
	// the focused pane renders the same document.
	previewScroll ui.ScrollState
	// scrollTo is where view scrolls once it has laid out, which SetScrollY
	// needs before it can clamp the position.
	scrollTo      float64
	scrollPending bool
	// otherFirst puts the other pane on the left or at the top.
	otherFirst bool
	percent    float32
}

func (a *App) splitRight() {
	a.toggleSplit(splitRight)
}

func (a *App) splitDown() {
	a.toggleSplit(splitDown)
}

// toggleSplit opens the other pane with the active document in layout,
// turns an open split to layout, or closes the split already in layout.
func (a *App) toggleSplit(layout splitLayout) {
	switch {
	case a.split.layout == layout:
		a.closeSplit()
	case a.split.layout != splitNone:
		a.split.layout = layout
	case a.activeDocument() != nil:
		a.split = splitState{layout: layout, doc: a.activeDocument(), percent: initialSplitPercent}
	}
}

// closeSplit leaves the focused pane alone in the panel.
func (a *App) closeSplit() {
	a.split = splitState{}
}

// syncSplit keeps the other pane on an open document, in an editor of its
// own, after tabs closed or the focused pane changed document.
func (a *App) syncSplit(active *document) {
	s := &a.split
	if !slices.Contains(a.documents, s.doc) {
		s.doc, s.view = active, nil
	}
	if s.doc.editor == nil {
		s.view = nil
		return
	}
	if s.view != nil && s.view.Buffer != s.doc.editor.Buffer {
		s.view = nil
	}
	switch {
	case s.doc == active && (s.view == nil || s.view == active.editor):
		s.view = twinEditor(active.editor)
		s.scrollTo, s.scrollPending = active.editor.ScrollY(), true
	case s.view == nil:
		s.view = s.doc.editor
	}
}

// twinEditor returns an editor of the same buffer at the same caret, for a
// second pane on one document. It answers no hovers or definitions while it
// is the other pane's: adoptEditor gives it those of the document.
func twinEditor(state *editor.State) *editor.State {
	twin := &editor.State{Buffer: state.Buffer, Label: state.Label, LineHeight: state.LineHeight, ReadOnly: state.ReadOnly, DiffBase: state.DiffBase}
	twin.SetSelections(state.Selections()...)
	return twin
}

// adoptEditor makes state the editor of d, with the actions and the change
// bars of the editor it replaces, which keeps none of them.
func adoptEditor(d *document, state *editor.State) {
	previous := d.editor
	if previous == state {
		return
	}
	state.OnDefinition, previous.OnDefinition = previous.OnDefinition, nil
	state.OnHover, previous.OnHover = previous.OnHover, nil
	state.ContextMenu, previous.ContextMenu = previous.ContextMenu, nil
	state.DiffBase = previous.DiffBase
	d.editor = state
}

// focusOtherPane gives the keyboard's role to the other pane, after a click
// in it: its document becomes the active one, in the editor it shows.
func (a *App) focusOtherPane() {
	a.forgetCaretWord()
	s := &a.split
	active := a.activeDocument()
	other := s.doc
	if other == active {
		if s.view != nil {
			mine := active.editor
			adoptEditor(active, s.view)
			s.view = mine
		}
		active.previewScroll, s.previewScroll = s.previewScroll, active.previewScroll
	} else {
		if s.view != nil {
			adoptEditor(other, s.view)
		}
		s.doc, s.view = active, active.editor
		a.chooseTab(slices.Index(a.documents, other))
	}
	s.otherFirst = !s.otherFirst
}

// splitPanesView shows the active document and the other pane's, in their
// places.
func (a *App) splitPanesView(c *ui.Context, d *document) {
	a.syncSplit(d)
	s := &a.split
	first, second := func() { a.focusedPaneView(c, d) }, func() { a.otherPaneView(c) }
	if s.otherFirst {
		first, second = second, first
	}
	if s.layout == splitDown {
		ui.SplitVerticalPercent(c, &s.percent, first, second).Grow(1).FillWidth()
		return
	}
	ui.SplitPercent(c, &s.percent, first, second).Grow(1).FillWidth()
}

// focusedPaneView shows the active document as the panel does without a
// split.
func (a *App) focusedPaneView(c *ui.Context, d *document) {
	ui.Column(c).Fill().Children(func() {
		a.pathBarView(c, d)
		a.conflictView(c, d)
		a.documentView(c, d)
	})
}

// otherPaneView shows the other pane's document, quieter, and hands it the
// keyboard's role when it is clicked.
func (a *App) otherPaneView(c *ui.Context) {
	s := &a.split
	d := s.doc
	closed := false
	pane := ui.Column(c).Fill()
	pane.Children(func() {
		closed = a.otherPathBarView(c, d)
		a.conflictView(c, d)
		a.otherDocumentView(c, d)
	})
	// Both change the panes after they were built: build them again.
	switch {
	case closed:
		a.closeSplit()
		c.After(0)
	case pane.PressedInside():
		a.focusOtherPane()
		c.After(0)
	}
}

// otherPathBarView shows the other pane's path, and reports a click on its
// close button.
func (a *App) otherPathBarView(c *ui.Context, d *document) bool {
	colors := a.look.palette
	closed := false
	ui.Row(c).FillWidth().Height(pathBarHeight).Shrink(0).Gap(5).Padding(0, 14).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colors.border).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Opacity(otherPaneTextAlpha).Children(func() {
			a.pathCrumbsView(c, d.displayPath())
		})
		closed = a.pathBarButton(c, iconClose, locales.CloseSplit(), false).Clicked()
	})
	return closed
}

// otherDocumentView shows the other pane's document as documentView does,
// in the other pane's editor.
func (a *App) otherDocumentView(c *ui.Context, d *document) {
	s := &a.split
	c.Key(d.path)
	previewScroll := &d.previewScroll
	if d == a.activeDocument() {
		previewScroll = &s.previewScroll
	}
	if a.documentPreviewView(c, d, previewScroll) {
		return
	}
	if s.scrollPending && s.view.ViewportHeight() > 0 {
		s.view.SetScrollY(s.scrollTo)
		s.scrollPending = false
	}
	a.codeView(c, s.view, &s.focus)
	if s.scrollPending {
		c.After(0)
	}
}

// splitButtonsView offers to split the panel to the right or down, lit
// while it is split that way.
func (a *App) splitButtonsView(c *ui.Context) {
	if a.activeDocument() == nil {
		return
	}
	ui.Row(c).Shrink(0).Gap(2).AlignItems(ui.Center).Children(func() {
		if a.pathBarButton(c, iconColumns, splitRightKeys.label(), a.split.layout == splitRight).Clicked() {
			a.splitRight()
		}
		if a.pathBarButton(c, iconRows, splitDownKeys.label(), a.split.layout == splitDown).Clicked() {
			a.splitDown()
		}
	})
}
