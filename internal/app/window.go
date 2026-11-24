package app

import (
	"context"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/locales"
)

// show opens the window, with the project at root when root is not empty.
func (a *App) show(root string, placement windowPlacement) {
	a.after = func(delay time.Duration, action func()) {
		time.AfterFunc(delay, func() { a.post(action) })
	}
	a.window = mygo.NewWindow(mygo.WindowOptions{
		Title:     "Perch",
		X:         placement.x,
		Y:         placement.y,
		Width:     windowWidth,
		Height:    windowHeight,
		MinWidth:  640,
		MinHeight: 420,
		StateKey:  placement.stateKey,
		// The sidebar and the tabs run under the window's controls.
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 16, Y: 13},
		// The controls of Windows and Linux fill the height of the tabs.
		TitleBarHeight: int(headerHeight),
		Content:        ui.View(a.view),
	})
	a.window.OnClose(a.handleWindowClose)
	if root != "" {
		a.openFolder(root)
	}
}

// release stops the work that belongs to the window once it has closed.
func (a *App) release() {
	a.closeSession()
	if a.stopWatch != nil {
		a.stopWatch()
	}
	a.stopSearch()
	a.hideQuickOpen()
	a.closeOutline()
	a.clearBreadcrumb()
	a.closeProjectSymbols()
	a.stopChanges()
	a.stopDefinitions()
	a.stopIgnoreMarks()
	a.stopTreeStatus()
	a.stopFileHistory()
}

// runFind opens the find bar of the current file, which shows over its
// source.
func (a *App) runFind(command editor.Command) {
	d := a.activeDocument()
	if d == nil || d.editor == nil {
		return
	}
	d.markdownView = markdownSource
	d.editor.Run(command)
}

func (a *App) toggleMarkdownView() {
	d := a.activeDocument()
	if d == nil || !d.isMarkdown() {
		return
	}
	if d.markdownView == markdownPreview {
		d.markdownView = markdownSource
		return
	}
	d.markdownView = markdownPreview
}

// chooseFolder asks for a folder off the main thread, which the dialog needs
// to stay free.
func (a *App) chooseFolder() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    a.window,
			Title:     locales.OpenFolder(),
			Directory: true,
		})
		a.window.Update(func() {
			if err != nil {
				a.notice = toast{title: locales.FolderDialogFailed(), detail: err.Error()}
				return
			}
			if len(paths) > 0 {
				a.desktop.openFolder(a, paths[0])
			}
		})
	}()
}

// openFolder makes root this window's project and follows its changes.
func (a *App) openFolder(root string) {
	if err := a.loadFolder(root); err != nil {
		a.notice = toast{title: locales.FolderOpenFailed(), detail: err.Error()}
		return
	}
	if a.desktop != nil {
		a.desktop.recordRecent(a.root)
	}
	if a.window == nil {
		return
	}
	a.window.SetTitle(filepath.Base(a.root))
	ctx, cancel := context.WithCancel(context.Background())
	a.stopWatch = cancel
	if err := a.watch(ctx, a.root); err != nil {
		a.notice = toast{title: locales.LiveUpdatesStopped(), detail: err.Error()}
	}
	a.beginReview()
	a.startIgnoreMarks()
	a.startTreeStatus()
}

// handleWindowClose keeps the window open while unsaved edits need an answer.
func (a *App) handleWindowClose(event *mygo.CloseEvent) {
	if a.closeConfirmed || !slices.ContainsFunc(a.documents, (*document).modified) {
		return
	}
	event.PreventDefault()
	a.window.Update(func() { a.askCloseWindow = true })
}

func (a *App) confirmCloseWindow() {
	a.closeConfirmed = true
	a.window.Close()
}
