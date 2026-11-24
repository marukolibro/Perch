package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/internal/settings"
	"github.com/marukolibro/Perch/locales"
)

const (
	windowWidth   int = 1180
	windowHeight  int = 780
	cascadeOffset int = 28
	// stateKeyBytes is how much of a folder's hash names its window state.
	stateKeyBytes int = 12

	settingsFileName = "settings.json"
	languageFileName = "language-servers.json"
	sessionsDirName  = "sessions"
)

// Desktop owns Perch's windows, one project each, and the menu they share. Its
// methods run on the main thread.
type Desktop struct {
	windows      []*App
	settings     settings.Settings
	settingsFile string
	languageFile string
	recentFile   string
	recent       []string
	sessionDir   string
	cliBusy      bool
	// newAppWindow creates and shows a window for root; tests replace it.
	newAppWindow func(root string) *App
	// focusWindow brings the app and window to the front; tests replace it.
	focusWindow func(a *App)
}

// NewDesktop returns a desktop with no windows.
func NewDesktop() *Desktop {
	d := &Desktop{settings: settings.Default()}
	d.newAppWindow = d.openNativeWindow
	d.focusWindow = bringWindowToFront
	return d
}

// windowPlacement is where a new window opens. The state remembered under
// stateKey wins over x and y.
type windowPlacement struct {
	x        int
	y        int
	stateKey string
}

// Start opens the first window with the targets named by args, the command line
// after the program's name, and takes later launches.
func (d *Desktop) Start(args []string) {
	loadErr := d.loadSettings()
	mygo.App.SetMenu(d.menu())
	mygo.App.OnSecondInstance(d.openLaunch)
	mygo.App.OnLocaleChanged(func(string) { mygo.App.SetMenu(d.menu()) })
	workingDir, _ := os.Getwd()
	d.openArgs(args, workingDir)
	if loadErr != nil {
		d.reportSettingsFailure(locales.SettingsReadFailed(), loadErr)
	}
}

// loadSettings reads the saved settings and applies their appearance. Perch
// runs on the defaults when they cannot be read.
func (d *Desktop) loadSettings() error {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return fmt.Errorf("find the settings folder: %w", err)
	}
	d.settingsFile = filepath.Join(dir, settingsFileName)
	d.languageFile = filepath.Join(dir, languageFileName)
	d.recentFile = filepath.Join(dir, recentFileName)
	d.recent = loadRecent(d.recentFile)
	d.sessionDir = filepath.Join(dir, sessionsDirName)
	loaded, err := settings.Load(d.settingsFile)
	if len(loaded.Shortcuts) > 0 {
		loaded.Shortcuts = cleanShortcutOverrides(loaded.Shortcuts, d.menu())
	}
	d.settings = loaded
	applyAppearance(loaded.Appearance)
	return err
}

func applyAppearance(appearance settings.Appearance) {
	switch appearance {
	case settings.AppearanceLight:
		mygo.Theme.SetSource(mygo.ThemeLight)
	case settings.AppearanceDark:
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// settingsChanged applies new settings to every window and saves them.
func (d *Desktop) settingsChanged(next settings.Settings) {
	shortcutsChanged := !maps.Equal(next.Shortcuts, d.settings.Shortcuts)
	appearanceChanged := next.Appearance != d.settings.Appearance
	d.settings = next
	for _, a := range d.windows {
		a.setWindowSettings(next)
		a.window.Update(func() {})
	}
	if shortcutsChanged {
		mygo.App.SetMenu(d.menu())
	}
	if appearanceChanged {
		applyAppearance(next.Appearance)
	}
	if d.settingsFile == "" {
		d.reportSettingsFailure(locales.SettingsSaveFailed(), errors.New("the settings folder is unknown"))
		return
	}
	if err := settings.Save(d.settingsFile, next); err != nil {
		d.reportSettingsFailure(locales.SettingsSaveFailed(), err)
	}
}

func (d *Desktop) reportSettingsFailure(title string, err error) {
	if a := d.currentApp(nil); a != nil {
		a.tell(toast{title: title, detail: err.Error()})
	}
}

// openLaunch answers another launch of Perch: its folders and files open, and a
// launch without arguments brings the current window forward (or opens one when
// none exist).
func (d *Desktop) openLaunch(args []string, workingDir string) {
	d.openArgs(args, workingDir)
}

// openArgs handles command-line targets for both initial and subsequent
// launches.
func (d *Desktop) openArgs(args []string, workingDir string) {
	targets := parseLaunchArgs(args, workingDir)
	if len(targets) == 0 {
		a := d.currentApp(nil)
		if a == nil {
			a = d.openWindow("")
		}
		d.bringToFront(a)
		return
	}

	var lastApp *App
	var lastFailure toast
	for _, target := range targets {
		if target.err != nil {
			lastFailure = openFailure(target.path, target.err)
			continue
		}
		if target.isDir {
			if a := d.openFolderWindow(nil, target.path); a != nil {
				lastApp = a
			}
			continue
		}
		if a := d.openFileTarget(target); a != nil {
			lastApp = a
		}
	}

	if len(d.windows) == 0 {
		lastApp = d.openWindow("")
	}
	if lastApp == nil {
		lastApp = d.currentApp(nil)
	}
	if lastFailure != (toast{}) && lastApp != nil {
		lastApp.tell(lastFailure)
		lastApp.refreshWindow()
	}
	d.bringToFront(lastApp)
}

// openFileTarget opens a file target in the deepest matching open window, or
// opens its project root (Git root or parent folder) first.
func (d *Desktop) openFileTarget(target launchTarget) *App {
	a, relPath := windowForFile(d.windows, target.path)
	if a == nil {
		root := projectRootForFile(target.path)
		a = d.openFolderWindow(nil, root)
		if a == nil {
			return nil
		}
		var ok bool
		relPath, ok = insideProject(a.root, target.path)
		if !ok {
			a.dismissOverlaysForOpen()
			a.openExternalFile(target.path)
			if doc := a.activeDocument(); doc != nil && doc.path == target.path && target.line > 0 && doc.editor != nil {
				doc.scrollPending = false
				pos := positionForLineCol(doc.editor.Buffer, target.line, target.col)
				doc.reveal(pos, pos)
			}
			a.refreshWindow()
			return a
		}
	}
	a.openFileAtLine(relPath, target.line, target.col)
	a.refreshWindow()
	return a
}

// openWindow opens a window, with the project at root when root is not empty.
func (d *Desktop) openWindow(root string) *App {
	if d.newAppWindow != nil {
		return d.newAppWindow(root)
	}
	return d.openNativeWindow(root)
}

func (d *Desktop) openNativeWindow(root string) *App {
	a := New()
	a.desktop = d
	a.setWindowSettings(d.settings)
	a.languageFile = d.languageFile
	a.session.dir = d.sessionDir
	a.chosenLanguage = mygo.App.ChosenLocale
	a.setLanguage = mygo.App.SetLocale
	a.show(root, d.placementFor(root))
	a.window.OnClosed(func() { d.windowClosed(a) })
	d.windows = append(d.windows, a)
	return a
}

func (d *Desktop) bringToFront(a *App) {
	if d.focusWindow != nil {
		d.focusWindow(a)
		return
	}
	bringWindowToFront(a)
}

func bringWindowToFront(a *App) {
	if a == nil || a.window == nil {
		return
	}
	if mygo.App.IsHidden() {
		mygo.App.Show()
	}
	mygo.App.Focus()
	if a.window.IsMinimized() {
		a.window.Restore()
	}
	a.window.Show()
	a.window.Focus()
}

// placementFor sets a new window off from the current one, so that neither
// hides the other, and has each project remember its own window.
func (d *Desktop) placementFor(root string) windowPlacement {
	placement := windowPlacement{stateKey: stateKeyFor(root)}
	if a := d.currentApp(nil); a != nil {
		x, y := a.window.Position()
		placement.x, placement.y = x+cascadeOffset, y+cascadeOffset
	}
	return placement
}

// stateKeyFor names the remembered window of a project without keeping its
// path in the state file.
func stateKeyFor(root string) string {
	if root == "" {
		return "welcome"
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		absolute = root
	}
	sum := sha256.Sum256([]byte(absolute))
	return "project-" + hex.EncodeToString(sum[:stateKeyBytes])
}

func (d *Desktop) windowClosed(a *App) {
	a.release()
	d.windows = slices.DeleteFunc(slices.Clone(d.windows), func(other *App) bool { return other == a })
}

// folderDestination says which window takes a folder.
type folderDestination struct {
	// existing already shows the folder, and comes forward.
	existing *App
	// empty has no project yet, and takes the folder.
	empty *App
}

// chooseDestination finds the window for a folder. When neither field is set,
// the folder gets a new window.
func chooseDestination(windows []*App, from *App, root string) folderDestination {
	index := slices.IndexFunc(windows, func(a *App) bool { return sameFolder(a.root, root) })
	if index >= 0 {
		return folderDestination{existing: windows[index]}
	}
	if from != nil && from.root == "" {
		return folderDestination{empty: from}
	}
	if from == nil {
		if emptyIndex := slices.IndexFunc(windows, func(a *App) bool { return a.root == "" }); emptyIndex >= 0 {
			return folderDestination{empty: windows[emptyIndex]}
		}
	}
	return folderDestination{}
}

// openFolder shows a folder: in the window that has it, in an empty window, or
// in a new window.
func (d *Desktop) openFolder(from *App, folder string) {
	d.openFolderWindow(from, folder)
}

func (d *Desktop) openFolderWindow(from *App, folder string) *App {
	root, err := filepath.Abs(folder)
	if err != nil {
		d.reportOpenFailure(from, err)
		return nil
	}
	destination := chooseDestination(d.windows, from, root)
	switch {
	case destination.existing != nil:
		d.recordRecent(root)
		d.bringToFront(destination.existing)
		return destination.existing
	case destination.empty != nil:
		destination.empty.openFolder(root)
		destination.empty.refreshWindow()
		d.bringToFront(destination.empty)
		return destination.empty
	default:
		return d.openWindow(root)
	}
}

func (d *Desktop) reportOpenFailure(from *App, err error) {
	if a := d.currentApp(from); a != nil {
		a.tell(toast{title: locales.FolderOpenFailed(), detail: err.Error()})
	}
}

// newWindow brings forward an empty window, or opens one.
func (d *Desktop) newWindow() {
	index := slices.IndexFunc(d.windows, func(a *App) bool { return a.root == "" })
	if index >= 0 {
		d.windows[index].window.Focus()
		return
	}
	d.openWindow("")
}

// currentApp returns the app of a window, of the focused window, or of the
// newest one.
func (d *Desktop) currentApp(preferred *App) *App {
	if preferred != nil {
		return preferred
	}
	focused := mygo.FocusedWindow()
	index := slices.IndexFunc(d.windows, func(a *App) bool { return a.window == focused })
	if index >= 0 {
		return d.windows[index]
	}
	if len(d.windows) > 0 {
		return d.windows[len(d.windows)-1]
	}
	return nil
}

func (d *Desktop) appOf(window *mygo.Window) *App {
	index := slices.IndexFunc(d.windows, func(a *App) bool { return a.window == window })
	if index >= 0 {
		return d.windows[index]
	}
	return d.currentApp(nil)
}

func (d *Desktop) menu() *mygo.Menu {
	windowMenu := &mygo.MenuItem{Role: mygo.RoleWindowMenu}
	if runtime.GOOS != "darwin" {
		// The framework also binds Close Window to Ctrl+W in this menu.
		windowMenu.Submenu = []*mygo.MenuItem{{Role: mygo.RoleMinimize}, d.menuItem(closeWindowKeys, func(a *App) { a.window.Close() })}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Perch", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			d.menuItem(settingsKeys, (*App).showSettings),
			d.cliMenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: locales.MenuFile(), Submenu: []*mygo.MenuItem{
			{Label: newWindowKeys.label(), Accelerator: newWindowKeys.binding(d.settings).accelerator(), Click: func(*mygo.MenuItem, *mygo.Window) { d.newWindow() }},
			d.menuItem(openFolderKeys, (*App).chooseFolder),
			{Label: locales.MenuOpenRecent(), Submenu: d.recentMenuItems()},
			d.menuItem(goToFileKeys, (*App).showQuickOpen),
			mygo.Separator(),
			d.menuItem(saveKeys, (*App).saveActive),
			d.menuItem(copyLineRefKeys, (*App).copyLineReference),
			mygo.Separator(),
			d.menuItem(closeTabKeys, (*App).closeActiveTab),
			d.menuItem(reopenClosedTabKeys, (*App).reopenClosedTab),
			d.menuItem(closeWindowKeys, func(a *App) { a.window.Close() }),
		}},
		{Role: mygo.RoleEditMenu},
		{Label: locales.MenuFind(), Submenu: []*mygo.MenuItem{
			d.menuItem(findInFileKeys, func(a *App) { a.runFind(editor.Find) }),
			d.menuItem(findReplaceKeys, func(a *App) { a.runFind(editor.FindReplace) }),
			mygo.Separator(),
			d.menuItem(findInFilesKeys, (*App).showFindInFiles),
		}},
		{Label: locales.MenuGo(), Submenu: []*mygo.MenuItem{
			d.menuItem(goToLineKeys, (*App).showGoToLine),
			d.menuItem(goToSymbolKeys, (*App).showOutline),
			d.menuItem(goToProjectSymbolKeys, (*App).showProjectSymbols),
			d.menuItem(goToDefinitionKeys, (*App).definitionAtCaret),
			d.menuItem(findReferencesKeys, (*App).referencesAtCaret),
			mygo.Separator(),
			d.menuItem(goBackKeys, (*App).goBack),
			d.menuItem(goForwardKeys, (*App).goForward),
			mygo.Separator(),
			d.menuItem(nextTabKeys, (*App).nextTab),
			d.menuItem(previousTabKeys, (*App).previousTab),
		}},
		{Label: locales.MenuView(), Submenu: []*mygo.MenuItem{
			d.menuItem(toggleSidebarKeys, (*App).toggleSidebar),
			d.menuItem(revealInTreeKeys, (*App).revealActiveFile),
			d.menuItem(toggleMarkdownKeys, (*App).toggleMarkdownView),
			d.menuItem(collapseAllKeys, (*App).collapseFileTree),
			mygo.Separator(),
			d.menuItem(zoomInKeys, func(a *App) { a.changeFontSize(1) }),
			d.menuItem(zoomOutKeys, func(a *App) { a.changeFontSize(-1) }),
			d.menuItem(resetZoomKeys, (*App).resetFontSize),
			mygo.Separator(),
			d.menuItem(showChangesKeys, (*App).showChanges),
			d.menuItem(markReviewedKeys, (*App).markSelectedReviewed),
			mygo.Separator(),
			d.menuItem(nextChangeKeys, func(a *App) { a.moveToChange(1) }),
			d.menuItem(previousChangeKeys, func(a *App) { a.moveToChange(-1) }),
		}},
		windowMenu,
	})
}

// cliMenuItem builds the menu action to install or remove the perch CLI in
// PATH, reflecting the current installation state.
func (d *Desktop) cliMenuItem() *mygo.MenuItem {
	installed := cliInstalledPath() != ""
	label := locales.MenuInstallCLI()
	if installed {
		label = locales.MenuUninstallCLI()
	}
	return &mygo.MenuItem{
		Label: label,
		Click: func(_ *mygo.MenuItem, window *mygo.Window) {
			d.toggleCLIInstall(d.appOf(window), installed)
		},
	}
}

// toggleCLIInstall installs or uninstalls the perch CLI off the main thread so
// any system authorization dialog does not block the UI.
func (d *Desktop) toggleCLIInstall(preferred *App, uninstall bool) {
	if d.cliBusy {
		return
	}
	d.cliBusy = true
	go func() {
		var message toast
		if uninstall {
			removed, err := uninstallCLI()
			switch {
			case errors.Is(err, errCLIInstallCanceled):
			case err != nil:
				message = toast{title: locales.CliUninstallFailed(), detail: err.Error()}
			case removed != "":
				message = toast{tone: toneInfo, title: locales.CliUninstalled(), detail: removed}
			}
		} else {
			result, err := installCLI()
			switch {
			case errors.Is(err, errCLIInstallCanceled):
			case err != nil:
				message = toast{title: locales.CliInstallFailed(), detail: err.Error()}
			case result.inPath:
				message = toast{tone: toneInfo, title: locales.CliInstalled(), detail: result.path}
			default:
				message = toast{
					tone:   toneInfo,
					title:  locales.CliInstalled(),
					detail: locales.CliInstalledAddPath(result.path, filepath.Dir(result.path)),
				}
			}
		}
		mygo.App.WhenReady(func() {
			d.cliBusy = false
			mygo.App.SetMenu(d.menu())
			if message != (toast{}) {
				if a := d.currentApp(preferred); a != nil {
					a.tell(message)
					a.refreshWindow()
				}
			}
		})
	}()
}

// menuItem runs action on the chosen window's app, as an update of that window
// so that it redraws.
func (d *Desktop) menuItem(command shortcutAction, action func(*App)) *mygo.MenuItem {
	return &mygo.MenuItem{
		Label:       command.label(),
		Accelerator: command.binding(d.settings).accelerator(),
		Click: func(_ *mygo.MenuItem, window *mygo.Window) {
			a := d.appOf(window)
			if a != nil {
				a.post(func() { action(a) })
			}
		},
	}
}
