package app

import (
	"cmp"
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/marukolibro/Perch/internal/workspace"
	"github.com/marukolibro/Perch/locales"
)

const fileDialogWidth float32 = 380

// fileAction is what the open file tree dialog does.
type fileAction uint8

const (
	fileActionNone fileAction = iota
	fileActionNewFile
	fileActionNewFolder
	fileActionRename
	fileActionTrash
)

// fileTreeState is the dialog the file tree opens.
type fileTreeState struct {
	action fileAction
	// target is the row's path, or "." for the project root.
	target string
	// isDir is the kind of target when the dialog opened, to notice a target
	// that changed before the operation runs.
	isDir   bool
	open    bool
	name    string
	failure string
	input   ui.Handle
	// selectName selects the prefilled name once the field has the keyboard,
	// so that typing replaces it.
	selectName bool
	// unsaved counts the open tabs with edits that a trash would discard.
	unsaved int
}

var (
	errNameEmpty   = errors.New("the name is empty")
	errNameInvalid = errors.New("the name contains a path separator or a NUL byte")
	errNameDot     = errors.New("the name is a dot path")
)

// validFileName checks a name typed in the file tree dialog.
func validFileName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errNameEmpty
	case strings.ContainsRune(name, '/'), strings.ContainsRune(name, 0),
		filepath.Separator != '/' && strings.ContainsRune(name, filepath.Separator):
		return errNameInvalid
	case name == ".", name == "..":
		return errNameDot
	default:
		return nil
	}
}

// nameFailure says in plain words why a typed name is refused.
func nameFailure(err error) string {
	switch {
	case errors.Is(err, errNameEmpty):
		return locales.NameEmpty()
	case errors.Is(err, errNameInvalid):
		return locales.NameInvalid()
	case errors.Is(err, errNameDot):
		return locales.NameDot()
	case errors.Is(err, fs.ErrExist):
		return locales.NameExists()
	default:
		return ""
	}
}

// fileMenuIcons are the pictures of the file tree's menu.
type fileMenuIcons struct {
	newFile   *mygo.MenuIcon
	newFolder *mygo.MenuIcon
	rename    *mygo.MenuIcon
	trash     *mygo.MenuIcon
	copyPath  *mygo.MenuIcon
	copyLink  *mygo.MenuIcon
	reveal    *mygo.MenuIcon
	locate    *mygo.MenuIcon
}

// menuIcons makes the menu's pictures the first time the menu opens.
var menuIcons = sync.OnceValue(func() fileMenuIcons {
	return fileMenuIcons{
		newFile: mustMenuIcon(iconFilePlus), newFolder: mustMenuIcon(iconFolderPlus), rename: mustMenuIcon(iconPencil),
		trash: mustMenuIcon(iconTrash), copyPath: mustMenuIcon(iconCopy), copyLink: mustMenuIcon(iconLink),
		reveal: mustMenuIcon(iconReveal), locate: mustMenuIcon(iconLocate),
	}
})

// mustMenuIcon makes a menu picture of an icon built into Perch.
func mustMenuIcon(icon *ui.SVG) *mygo.MenuIcon {
	made, err := mygo.NewMenuIconSVG(icon, true)
	if err != nil {
		panic("app: make a menu icon: " + err.Error())
	}
	return made
}

// fileMenu fills the context menu of a tree row, or of the project root.
func (a *App) fileMenu(m *ui.Menu, target string) {
	icons := menuIcons()
	if m.Item(locales.FileMenuNewFile()).Icon(icons.newFile).Chosen() {
		a.startCreate(fileActionNewFile, target)
	}
	if m.Item(locales.FileMenuNewFolder()).Icon(icons.newFolder).Chosen() {
		a.startCreate(fileActionNewFolder, target)
	}
	m.Separator()
	if m.Item(locales.FileMenuCopyPath()).Icon(icons.copyPath).Chosen() {
		a.copyText(a.fullPath(target))
	}
	if target != "." && m.Item(locales.FileMenuCopyRelativePath()).Icon(icons.copyLink).Chosen() {
		a.copyText(target)
	}
	if m.Item(revealLabel(runtime.GOOS)).Icon(icons.reveal).Chosen() {
		a.revealInFolder(target)
	}
	if target == "." {
		return
	}
	m.Separator()
	if m.Item(locales.FileMenuRename()).Icon(icons.rename).Chosen() {
		a.startRename(target)
	}
	trash := m.Item(locales.FileMenuTrash()).Icon(icons.trash).IconColor(a.look.palette.danger).Danger()
	if trash.Chosen() {
		a.askTrash(target)
	}
}

// fullPath returns where a path relative to the workspace is on the machine.
func (a *App) fullPath(target string) string {
	if target == "." {
		return a.root
	}
	return filepath.Join(a.root, filepath.FromSlash(target))
}

// revealLabel names the file manager of the platform.
func revealLabel(goos string) string {
	switch goos {
	case "darwin":
		return locales.FileMenuReveal()
	case "windows":
		return locales.FileMenuRevealExplorer()
	default:
		return locales.FileMenuRevealFolder()
	}
}

// revealInFolder shows a file or folder in the system's file manager.
func (a *App) revealInFolder(target string) {
	if target != "." {
		if _, err := workspace.Stat(a.root, target); err != nil {
			a.tell(openFailure(target, err))
			return
		}
	}
	mygo.Shell.ShowItemInFolder(a.fullPath(target))
}

// startCreate opens the name dialog for a new file or folder, checking that the
// row it belongs to still is what it was.
func (a *App) startCreate(action fileAction, target string) {
	entry, err := workspace.Stat(a.root, target)
	if err != nil {
		a.tell(openFailure(target, err))
		return
	}
	a.openNameDialog(action, target, entry.IsDir, "")
}

// fileTreeToolbarView builds the buttons over the file tree: new items go in
// the chosen folder, or beside the chosen file, or at the top.
func (a *App) fileTreeToolbarView(c *ui.Context) {
	if a.pathBarButton(c, iconFilePlus, locales.FileMenuNewFile(), false).Clicked() {
		a.startCreate(fileActionNewFile, cmp.Or(a.selectedTreePath(), "."))
	}
	if a.pathBarButton(c, iconFolderPlus, locales.FileMenuNewFolder(), false).Clicked() {
		a.startCreate(fileActionNewFolder, cmp.Or(a.selectedTreePath(), "."))
	}
	if d := a.activeDocument(); d != nil && !d.external {
		if a.pathBarButton(c, iconLocate, locales.MenuRevealInTree(), false).Clicked() {
			a.revealInTree(d.path)
		}
	}
	if a.pathBarButton(c, iconRefresh, locales.RefreshFileTree(), false).Clicked() {
		a.refreshFileTree()
	}
	if a.pathBarButton(c, iconCollapse, locales.CollapseAll(), false).Clicked() {
		a.collapseFileTree()
	}
}

// revealActiveFile shows and selects the active workspace file in the file tree.
func (a *App) revealActiveFile() {
	if a.root == "" {
		return
	}
	d := a.activeDocument()
	if d == nil || d.external {
		return
	}
	a.revealInTree(d.path)
}

// revealInTree opens the sidebar's file tree, expands every folder above
// relPath, and schedules selection and scrolling once the outline builds.
func (a *App) revealInTree(relPath string) {
	if a.root == "" || relPath == "" || relPath == "." {
		return
	}
	a.switchSidebarTab(sidebarFiles)
	a.expandTreeTo(path.Dir(relPath))
	a.pendingReveal = relPath
}

// startRename opens the name dialog with the current name, selected.
func (a *App) startRename(target string) {
	if target == "." {
		return
	}
	entry, err := workspace.Stat(a.root, target)
	if err != nil {
		a.tell(openFailure(target, err))
		return
	}
	a.openNameDialog(fileActionRename, target, entry.IsDir, path.Base(target))
}

// openNameDialog opens the name dialog for action.
func (a *App) openNameDialog(action fileAction, target string, isDir bool, name string) {
	input := a.files.input
	a.files = fileTreeState{
		action: action, target: target, isDir: isDir, open: true, name: name, input: input, selectName: name != "",
	}
	a.files.input.Focus()
}

// askTrash confirms moving a file or folder to the Trash, naming the tabs whose
// edits the move would discard.
func (a *App) askTrash(target string) {
	if target == "." {
		return
	}
	if _, err := workspace.Stat(a.root, target); err != nil {
		a.tell(openFailure(target, err))
		return
	}
	input := a.files.input
	a.files = fileTreeState{action: fileActionTrash, target: target, open: true, unsaved: a.unsavedTabs(target), input: input}
}

// removesDocument reports whether trashing target would remove an open file.
func removesDocument(target string, documentPath string) bool {
	return documentPath == target || strings.HasPrefix(documentPath, target+"/")
}

// unsavedTabs counts the open tabs with edits that trashing target would
// discard.
func (a *App) unsavedTabs(target string) int {
	count := 0
	for _, d := range a.documents {
		if !d.external && d.modified() && removesDocument(target, d.path) {
			count++
		}
	}
	return count
}

// fileDialogView shows the file tree's confirmation or name dialog.
func (a *App) fileDialogView(c *ui.Context) {
	if a.files.action == fileActionNone || !a.files.open {
		return
	}
	if a.files.action == fileActionTrash {
		a.trashDialogView(c)
		return
	}
	a.nameDialogView(c)
}

// trashDialogView asks before moving an item to the Trash.
func (a *App) trashDialogView(c *ui.Context) {
	name := path.Base(a.files.target)
	detail := locales.TrashConfirmDetail()
	if a.files.unsaved > 0 {
		detail += " " + locales.TrashUnsaved(a.files.unsaved)
	}
	switch ui.AlertDialog(c, &a.files.open, locales.TrashConfirmTitle(name), detail, locales.Cancel(), locales.TrashAction()) {
	case 1:
		a.trashTarget()
	}
}

// nameDialogView shows the field that names a new file, a new folder or a
// rename.
func (a *App) nameDialogView(c *ui.Context) {
	colors := a.look.palette
	submitted := false
	ui.Modal(c, &a.files.open, func() {
		ui.Column(c).Width(fileDialogWidth).Gap(10).Children(func() {
			ui.Text(c, a.fileDialogTitle()).FontSize(15).FontWeight(600)
			input := ui.TextInput(c, &a.files.name).Bind(&a.files.input).
				Placeholder(locales.FileNameLabel()).Label(locales.FileNameLabel())
			submitted = input.Submitted()
			if a.files.selectName && a.files.input.Focused(c) {
				a.files.selectName = false
				a.files.input.SelectAll()
			}
			if a.files.failure != "" {
				ui.Text(c, a.files.failure).FontSize(smallFontSize).TextColor(colors.danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, locales.Cancel()).Clicked() {
					a.files.open = false
				}
				if ui.PrimaryButton(c, a.fileDialogAction()).Clicked() {
					submitted = true
				}
			})
		})
	})
	if submitted {
		a.submitFileName()
	}
}

// fileDialogTitle names the open name dialog.
func (a *App) fileDialogTitle() string {
	switch a.files.action {
	case fileActionNewFile:
		return locales.NewFileTitle()
	case fileActionNewFolder:
		return locales.NewFolderTitle()
	case fileActionRename:
		return locales.RenameTitle(path.Base(a.files.target))
	default:
		return ""
	}
}

// fileDialogAction names the button that accepts the open name dialog.
func (a *App) fileDialogAction() string {
	if a.files.action == fileActionRename {
		return locales.RenameAction()
	}
	return locales.CreateAction()
}

// submitFileName checks a typed name and runs the dialog's operation.
func (a *App) submitFileName() {
	a.files.name = strings.TrimSpace(a.files.name)
	if err := validFileName(a.files.name); err != nil {
		a.files.failure = nameFailure(err)
		a.files.input.Focus()
		return
	}
	a.files.failure = ""
	switch a.files.action {
	case fileActionNewFile:
		a.createFile(a.files.name)
	case fileActionNewFolder:
		a.createFolder(a.files.name)
	case fileActionRename:
		a.renameTarget(a.files.name)
	}
}

// openDocumentAt returns the open workspace document at relPath, or nil.
func (a *App) openDocumentAt(relPath string) *document {
	for _, d := range a.documents {
		if !d.external && d.path == relPath {
			return d
		}
	}
	return nil
}

// destinationOpen reports whether a tab other than the renamed item's own
// shows relPath or a file inside it.
func (a *App) destinationOpen(oldPath string, relPath string) bool {
	for _, d := range a.documents {
		if d.external || removesDocument(oldPath, d.path) {
			continue
		}
		if removesDocument(relPath, d.path) {
			return true
		}
	}
	return false
}

// createFile creates a file in the target's folder and opens it.
func (a *App) createFile(name string) {
	dir, ok := a.createDir()
	if !ok {
		return
	}
	relPath := path.Join(dir, name)
	if a.openDocumentAt(relPath) != nil {
		a.files.failure = locales.FileAlreadyOpen()
		a.files.input.Focus()
		return
	}
	if err := workspace.CreateFile(a.root, relPath); err != nil {
		a.createFailed(relPath, err)
		return
	}
	a.finishFileChange(dir)
	a.openFile(relPath)
	a.files.open = false
}

// createFolder creates a folder in the target's folder.
func (a *App) createFolder(name string) {
	dir, ok := a.createDir()
	if !ok {
		return
	}
	relPath := path.Join(dir, name)
	if a.openDocumentAt(relPath) != nil {
		a.files.failure = locales.FileAlreadyOpen()
		a.files.input.Focus()
		return
	}
	if err := workspace.CreateDir(a.root, relPath); err != nil {
		a.createFailed(relPath, err)
		return
	}
	a.finishFileChange(dir)
	a.files.open = false
}

// createDir returns the folder a new item goes in, checking that the target
// still has the kind it had when the menu opened.
func (a *App) createDir() (string, bool) {
	entry, err := workspace.Stat(a.root, a.files.target)
	if err != nil {
		a.tell(openFailure(a.files.target, err))
		a.files.open = false
		return "", false
	}
	if entry.IsDir != a.files.isDir {
		name := path.Base(a.files.target)
		a.tell(toast{title: locales.TargetChanged(name), detail: locales.TargetChangedDetail()})
		a.files.open = false
		return "", false
	}
	if entry.IsDir {
		return a.files.target, true
	}
	return path.Dir(a.files.target), true
}

// createFailed says why a new item was not created, inline for a name that
// exists and in a toast for anything else.
func (a *App) createFailed(relPath string, err error) {
	if errors.Is(err, fs.ErrExist) {
		a.files.failure = locales.NameExists()
		a.files.input.Focus()
		return
	}
	if errors.Is(err, fs.ErrNotExist) {
		a.tell(openFailure(a.files.target, err))
		a.files.open = false
		return
	}
	a.fileOperationFailed(locales.CreateFailed(path.Base(relPath)), err)
}

// renameTarget renames the dialog's target and moves its open tabs.
func (a *App) renameTarget(name string) {
	oldPath := a.files.target
	if oldPath == "." {
		return
	}
	newPath := path.Join(path.Dir(oldPath), name)
	if newPath == oldPath {
		a.files.open = false
		return
	}
	if _, err := workspace.Stat(a.root, oldPath); err != nil {
		a.tell(openFailure(oldPath, err))
		a.files.open = false
		return
	}
	if a.destinationOpen(oldPath, newPath) {
		a.files.failure = locales.FileAlreadyOpen()
		a.files.input.Focus()
		return
	}
	if err := workspace.Rename(a.root, oldPath, newPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			a.files.failure = locales.NameExists()
			a.files.input.Focus()
			return
		}
		if errors.Is(err, fs.ErrNotExist) {
			a.tell(openFailure(oldPath, err))
			a.files.open = false
			return
		}
		a.fileOperationFailed(locales.RenameFailed(path.Base(oldPath)), err)
		return
	}
	a.renameDocuments(oldPath, newPath)
	a.renameTreePaths(oldPath, newPath)
	a.finishFileChange(path.Dir(newPath))
	a.files.open = false
}

// renameDocuments makes open tabs follow a renamed file or folder, keeping
// their buffers and unsaved edits.
func (a *App) renameDocuments(oldPath string, newPath string) {
	for _, d := range a.documents {
		if d.external || !removesDocument(oldPath, d.path) {
			continue
		}
		previous := d.path
		d.path = newPath + strings.TrimPrefix(d.path, oldPath)
		if d.editor != nil {
			d.editor.Label = d.path
		}
		if d.markdownOptions != nil {
			dir := filepath.Join(a.root, filepath.FromSlash(path.Dir(d.path)))
			d.markdownOptions.LoadImage = markdown.FileImages(dir)
		}
		a.followFileKind(d, previous)
	}
	a.definition.back = renamedPlaces(a.definition.back, oldPath, newPath)
	a.definition.forward = renamedPlaces(a.definition.forward, oldPath, newPath)
	a.renameTrackedPaths(oldPath, newPath)
}

// followFileKind shows a renamed document as what its new name says it is. A
// picture and a text cannot share a tab, so that change opens the file again,
// unless that would lose unsaved edits.
func (a *App) followFileKind(d *document, previous string) {
	if d.editor != nil && path.Base(previous) != path.Base(d.path) {
		text := d.editor.Buffer.String()
		d.editor.Buffer.SetLanguage(editor.LanguageForText(path.Base(d.path), text))
	}
	kind := imageKindOf(d.path)
	switch {
	case imageKindOf(previous) == kind:
	case d.editor == nil, kind == imageKindRaster && !d.modified():
		a.closeTabUnrecorded(d)
		a.openFile(d.path)
	case kind == imageKindSVG:
		d.image = &imageView{kind: imageKindSVG, size: int64(d.editor.Buffer.Len())}
	default:
		d.image = nil
	}
}

// renamedPlaces returns the places of a history with a rename applied.
func renamedPlaces(places []place, oldPath string, newPath string) []place {
	renamed := slices.Clone(places)
	for index, at := range renamed {
		if !at.external && removesDocument(oldPath, at.path) {
			renamed[index].path = newPath + strings.TrimPrefix(at.path, oldPath)
		}
	}
	return renamed
}

// renameTreePaths makes the tree's open folders follow a rename.
func (a *App) renameTreePaths(oldPath string, newPath string) {
	open := a.openTreePaths()
	a.tree.Open.Clear()
	for _, dir := range open {
		if removesDocument(oldPath, dir) {
			dir = newPath + strings.TrimPrefix(dir, oldPath)
		}
		a.tree.Open.Add(dir)
	}
}

// finishFileChange refreshes the tree at once rather than waiting for the
// watcher, and shows dir's new children.
func (a *App) finishFileChange(dir string) {
	a.expandTreeTo(dir)
	a.invalidateFileTree()
	a.treeRow = noRow
}

// refreshFileTree reads the tree again from disk, with its Git colors, for a
// change the watcher missed.
func (a *App) refreshFileTree() {
	a.invalidateFileTree()
	a.scheduleTreeStatus()
}

// invalidateFileTree leaves relisting and its ignore checks to the next read.
func (a *App) invalidateFileTree() {
	clear(a.children)
	clear(a.entries)
}

// collapseFileTree closes every folder, and lets go of a chosen row that the
// folders it was in now hide.
func (a *App) collapseFileTree() {
	a.tree.Open.Clear()
	if path.Dir(a.selectedTreePath()) != "." {
		a.treeRow = noRow
	}
}

// pruneOpenTreePaths forgets missing folders without reading closed branches.
func (a *App) pruneOpenTreePaths() {
	for dir := range a.tree.Open.All() {
		for item := dir; item != "."; item = path.Dir(item) {
			if a.children[path.Dir(item)] != nil && !a.entries[item].IsDir {
				a.tree.Open.Remove(dir)
				break
			}
		}
	}
}

// expandTreeTo opens dir and its parent folders, so that a new item shows.
func (a *App) expandTreeTo(dir string) {
	for dir != "." && dir != "/" {
		a.tree.Open.Add(dir)
		dir = path.Dir(dir)
	}
}

// openTreePaths returns the tree's open folders.
func (a *App) openTreePaths() []string {
	paths := make([]string, 0, a.tree.Open.Len())
	for dir := range a.tree.Open.All() {
		paths = append(paths, dir)
	}
	return paths
}

// fileOperationFailed shows the toast of a file tree operation that failed
// after its name was accepted.
func (a *App) fileOperationFailed(title string, err error) {
	a.files.open = false
	detail := err.Error()
	if errors.Is(err, fs.ErrPermission) {
		detail = locales.CheckPermissions()
	}
	a.tell(toast{title: title, detail: detail})
}

// trashTarget moves the dialog's target to the system Trash and closes its
// tabs.
func (a *App) trashTarget() {
	target := a.files.target
	a.files.open = false
	if target == "." {
		return
	}
	if _, err := workspace.Stat(a.root, target); err != nil {
		a.tell(openFailure(target, err))
		return
	}
	if err := a.trashItem(a.fullPath(target)); err != nil {
		a.tell(toast{title: locales.TrashFailed(path.Base(target)), detail: err.Error()})
		return
	}
	a.closeTrashedDocuments(target)
	a.removeTreePaths(target)
	a.invalidateFileTree()
	a.treeRow = noRow
}

// closeTrashedDocuments closes the tabs of a trashed file or folder.
func (a *App) closeTrashedDocuments(target string) {
	var removed []*document
	for _, d := range a.documents {
		if !d.external && removesDocument(target, d.path) {
			removed = append(removed, d)
		}
	}
	for _, d := range removed {
		a.closeTabUnrecorded(d)
	}
	a.removeTrackedPaths(target)
}

// removeTreePaths drops a trashed file or folder and its children from the
// tree's open folders.
func (a *App) removeTreePaths(target string) {
	open := a.openTreePaths()
	a.tree.Open.Clear()
	for _, dir := range open {
		if !removesDocument(target, dir) {
			a.tree.Open.Add(dir)
		}
	}
}
