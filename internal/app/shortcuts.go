package app

import (
	"maps"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/internal/settings"
	"github.com/marukolibro/Perch/locales"
)

type keyCombo struct {
	mods ui.Modifiers
	key  string
}

type shortcutAction struct {
	id       string
	label    func() string
	defaults keyCombo
}

var (
	settingsKeys          = shortcutAction{"settings", locales.MenuSettings, keyCombo{ui.Cmd, ","}}
	newWindowKeys         = shortcutAction{"newWindow", locales.MenuNewWindow, keyCombo{ui.Cmd | ui.Shift, "N"}}
	openFolderKeys        = shortcutAction{"openFolder", locales.MenuOpenFolder, keyCombo{ui.Cmd, "O"}}
	goToFileKeys          = shortcutAction{"goToFile", locales.MenuGoToFile, keyCombo{ui.Cmd, "P"}}
	saveKeys              = shortcutAction{"save", locales.Save, keyCombo{ui.Cmd, "S"}}
	closeTabKeys          = shortcutAction{"closeTab", locales.CloseTab, keyCombo{ui.Cmd, "W"}}
	closeWindowKeys       = shortcutAction{"closeWindow", locales.MenuCloseWindow, keyCombo{ui.Cmd | ui.Shift, "W"}}
	findInFileKeys        = shortcutAction{"findInFile", locales.MenuFindInFile, keyCombo{ui.Cmd, "F"}}
	findReplaceKeys       = shortcutAction{"findReplace", locales.MenuFindReplace, keyCombo{ui.Cmd | ui.Alt, "F"}}
	findInFilesKeys       = shortcutAction{"findInFiles", locales.MenuFindInFiles, keyCombo{ui.Cmd | ui.Shift, "F"}}
	goToSymbolKeys        = shortcutAction{"goToSymbol", locales.MenuGoToSymbol, keyCombo{ui.Cmd | ui.Shift, "O"}}
	goToProjectSymbolKeys = shortcutAction{"goToProjectSymbol", locales.MenuGoToProjectSymbol, keyCombo{ui.Cmd, "T"}}
	goToDefinitionKeys    = shortcutAction{"goToDefinition", locales.MenuGoToDefinition, keyCombo{0, "F12"}}
	findReferencesKeys    = shortcutAction{"findReferences", locales.MenuFindReferences, keyCombo{ui.Shift, "F12"}}
	goBackKeys            = shortcutAction{"goBack", locales.MenuGoBack, keyCombo{ui.Ctrl, "-"}}
	goForwardKeys         = shortcutAction{"goForward", locales.MenuGoForward, keyCombo{ui.Ctrl | ui.Shift, "-"}}
	nextTabKeys           = shortcutAction{"nextTab", locales.MenuNextTab, keyCombo{ui.Cmd | ui.Shift, "]"}}
	previousTabKeys       = shortcutAction{"previousTab", locales.MenuPreviousTab, keyCombo{ui.Cmd | ui.Shift, "["}}
	toggleMarkdownKeys    = shortcutAction{"toggleMarkdown", locales.MenuToggleMarkdown, keyCombo{ui.Cmd | ui.Shift, "V"}}
	collapseAllKeys       = shortcutAction{"collapseAll", locales.CollapseAll, keyCombo{0, ""}}
	showChangesKeys       = shortcutAction{"showChanges", locales.MenuShowChanges, keyCombo{ui.Cmd | ui.Shift, "R"}}
	markReviewedKeys      = shortcutAction{"markReviewed", locales.MenuMarkReviewed, keyCombo{ui.Cmd, "Enter"}}
	nextChangeKeys        = shortcutAction{"nextChange", locales.MenuNextChange, keyCombo{ui.Alt, "F5"}}
	previousChangeKeys    = shortcutAction{"previousChange", locales.MenuPreviousChange, keyCombo{ui.Alt | ui.Shift, "F5"}}
	copyLineRefKeys       = shortcutAction{"copyLineRef", locales.MenuCopyLineRef, keyCombo{ui.Cmd | ui.Shift, "C"}}
	reopenClosedTabKeys   = shortcutAction{"reopenClosedTab", locales.MenuReopenClosedTab, keyCombo{ui.Cmd | ui.Shift, "T"}}
	goToLineKeys          = shortcutAction{"goToLine", locales.MenuGoToLine, keyCombo{ui.Ctrl, "G"}}
	toggleSidebarKeys     = shortcutAction{"toggleSidebar", locales.MenuToggleSidebar, keyCombo{ui.Cmd, "B"}}
	revealInTreeKeys      = shortcutAction{"revealInTree", locales.MenuRevealInTree, keyCombo{ui.Cmd | ui.Shift, "E"}}
	splitRightKeys        = shortcutAction{"splitRight", locales.MenuSplitRight, keyCombo{ui.Cmd, "\\"}}
	splitDownKeys         = shortcutAction{"splitDown", locales.MenuSplitDown, keyCombo{0, ""}}
	foldAllKeys           = shortcutAction{"foldAll", locales.MenuFoldAll, keyCombo{0, ""}}
	unfoldAllKeys         = shortcutAction{"unfoldAll", locales.MenuUnfoldAll, keyCombo{0, ""}}
	zoomInKeys            = shortcutAction{"zoomIn", locales.MenuZoomIn, keyCombo{ui.Cmd, "="}}
	zoomOutKeys           = shortcutAction{"zoomOut", locales.MenuZoomOut, keyCombo{ui.Cmd, "-"}}
	resetZoomKeys         = shortcutAction{"resetZoom", locales.MenuResetZoom, keyCombo{ui.Cmd, "0"}}
	cycleNextKeys         = shortcutAction{"cycleNextTab", locales.CycleNextTab, keyCombo{ui.Ctrl, "Tab"}}
	cyclePreviousKeys     = shortcutAction{"cyclePreviousTab", locales.CyclePreviousTab, keyCombo{ui.Ctrl | ui.Shift, "Tab"}}
	numberedTabKeys       = func() []shortcutAction {
		var actions []shortcutAction
		for number := 1; number <= lastTabNumber; number++ {
			digit := strconv.Itoa(number)
			label := func() string { return locales.ShortcutTabNumber(number) }
			if number == lastTabNumber {
				label = locales.ShortcutLastTab
			}
			actions = append(actions, shortcutAction{"tab" + digit, label, keyCombo{ui.Cmd, digit}})
		}
		return actions
	}()
)

var shortcutGroups = []struct {
	title   func() string
	actions []shortcutAction
}{
	{locales.MenuFile, []shortcutAction{openFolderKeys, goToFileKeys, newWindowKeys, saveKeys, copyLineRefKeys, closeTabKeys, reopenClosedTabKeys, closeWindowKeys}},
	{locales.MenuFind, []shortcutAction{findInFileKeys, findReplaceKeys, findInFilesKeys}},
	{locales.MenuGo, []shortcutAction{goToLineKeys, goToSymbolKeys, goToProjectSymbolKeys, goToDefinitionKeys, findReferencesKeys, goBackKeys, goForwardKeys}},
	{locales.MenuView, []shortcutAction{settingsKeys, toggleSidebarKeys, revealInTreeKeys, splitRightKeys, splitDownKeys, toggleMarkdownKeys, collapseAllKeys, foldAllKeys, unfoldAllKeys, zoomInKeys, zoomOutKeys, resetZoomKeys, showChangesKeys, markReviewedKeys, nextChangeKeys, previousChangeKeys}},
	{locales.ShortcutTabs, append([]shortcutAction{nextTabKeys, previousTabKeys, cycleNextKeys, cyclePreviousKeys}, numberedTabKeys...)},
}

// Use the same key names for the picker, the saved accelerator and UI events.
var shortcutKeys = func() map[string]ui.Key {
	keys := map[string]ui.Key{
		"Tab": ui.KeyTab, "Enter": ui.KeyEnter, "Space": ui.KeySpace,
		"Backspace": ui.KeyBackspace, "Delete": ui.KeyDelete, "Insert": ui.KeyInsert,
		"Home": ui.KeyHome, "End": ui.KeyEnd, "PageUp": ui.KeyPageUp, "PageDown": ui.KeyPageDown,
		"Left": ui.KeyLeft, "Right": ui.KeyRight, "Up": ui.KeyUp, "Down": ui.KeyDown,
		"-": ui.KeyMinus, "=": ui.KeyEqual, ",": ui.KeyComma, ".": ui.KeyPeriod,
		"/": ui.KeySlash, ";": ui.KeySemicolon, "'": ui.KeyQuote, "[": ui.KeyBracketLeft, "]": ui.KeyBracketRight,
		"\\": ui.KeyBackslash, "`": ui.KeyBackquote,
	}
	for n := range 26 {
		keys[string(rune('A'+n))] = ui.KeyA + ui.Key(n)
	}
	for n := range 10 {
		keys[strconv.Itoa(n)] = ui.Key0 + ui.Key(n)
	}
	for n := range 12 {
		keys["F"+strconv.Itoa(n+1)] = ui.KeyF1 + ui.Key(n)
	}
	return keys
}()

var shortcutKeyNames = slices.Sorted(maps.Keys(shortcutKeys))

type shortcutModifier struct {
	mod          ui.Modifiers
	name, symbol string
}

var shortcutModifiers = func() []shortcutModifier {
	mods := []shortcutModifier{{ui.Ctrl, "Ctrl", "⌃"}, {ui.Alt, "Alt", "⌥"}, {ui.Shift, "Shift", "⇧"}}
	if runtime.GOOS == "darwin" {
		mods = append(mods, shortcutModifier{ui.Super, "Cmd", "⌘"})
	}
	return mods
}()

// format writes the menu accelerator or the label on a key cap.
func (k keyCombo) format(display bool) string {
	if k.key == "" {
		return ""
	}
	var parts []string
	mac := display && runtime.GOOS == "darwin"
	for _, m := range shortcutModifiers {
		if k.mods&m.mod == 0 {
			continue
		}
		name := m.name
		if mac {
			name = m.symbol
		} else if !display && m.mod == ui.Cmd {
			name = "CmdOrCtrl"
		}
		parts = append(parts, name)
	}
	key := k.key
	if mac {
		switch key {
		case "Enter":
			key = "↩"
		case "Tab":
			key = "⇥"
		case "Left":
			key = "←"
		case "Right":
			key = "→"
		case "Up":
			key = "↑"
		case "Down":
			key = "↓"
		}
		return strings.Join(parts, "") + key
	}
	return strings.Join(append(parts, key), "+")
}

func (k keyCombo) accelerator() string { return k.format(false) }
func (k keyCombo) label() string       { return k.format(true) }

func parseShortcut(value string) (keyCombo, bool) {
	if value == "" {
		return keyCombo{}, true
	}
	parts := strings.Split(value, "+")
	k := keyCombo{key: parts[len(parts)-1]}
	if _, ok := shortcutKeys[k.key]; !ok {
		return keyCombo{}, false
	}
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "CmdOrCtrl":
			k.mods |= ui.Cmd
		case "Ctrl":
			k.mods |= ui.Ctrl
		case "Cmd":
			if runtime.GOOS != "darwin" {
				return keyCombo{}, false
			}
			k.mods |= ui.Super
		case "Shift":
			k.mods |= ui.Shift
		case "Alt":
			k.mods |= ui.Alt
		default:
			return keyCombo{}, false
		}
	}
	return k, true
}

func (k keyCombo) valid() bool {
	if k.key == "" {
		return k.mods == 0
	}
	key, ok := shortcutKeys[k.key]
	return ok && (k.mods&^ui.Shift != 0 || (key >= ui.KeyF1 && key <= ui.KeyF12))
}

func (command shortcutAction) binding(s settings.Settings) keyCombo {
	if value, ok := s.Shortcuts[command.id]; ok {
		if k, valid := parseShortcut(value); valid && k.valid() {
			return k
		}
	}
	return command.defaults
}

func (command shortcutAction) pressed(c *ui.Context, s settings.Settings) bool {
	k := command.binding(s)
	return k.key != "" && c.Shortcut(k.mods, shortcutKeys[k.key])
}

// nativeShortcutOwner finds shortcuts supplied by menu roles, such as Copy and Quit.
func nativeShortcutOwner(items []*mygo.MenuItem, k keyCombo) string {
	for _, item := range items {
		if item.Hidden {
			continue
		}
		if item.Role != "" && item.Accelerator != "" {
			if keys, ok := parseShortcut(item.Accelerator); ok && keys == k {
				return item.Label
			}
		}
		if name := nativeShortcutOwner(item.Submenu, k); name != "" {
			return name
		}
	}
	return ""
}

func shortcutConflict(s settings.Settings, command shortcutAction, k keyCombo, menu *mygo.Menu) string {
	if k.key == "" {
		return ""
	}
	for _, group := range shortcutGroups {
		for _, other := range group.actions {
			if other.id != command.id && other.binding(s) == k {
				return other.label()
			}
		}
	}
	if menu != nil {
		return nativeShortcutOwner(menu.Items(), k)
	}
	return ""
}

// Restore valid overrides together so that swaps survive a restart. Falling
// back to a default can uncover another conflict, so recheck after removals.
func cleanShortcutOverrides(saved map[string]string, menu *mygo.Menu) map[string]string {
	var s settings.Settings
	s.Shortcuts = make(map[string]string)
	for _, group := range shortcutGroups {
		for _, action := range group.actions {
			raw, ok := saved[action.id]
			if !ok {
				continue
			}
			if k, ok := parseShortcut(raw); ok && k.valid() && k != action.defaults {
				s.Shortcuts[action.id] = k.accelerator()
			}
		}
	}
	for {
		var conflicts []string
		for _, group := range shortcutGroups {
			for _, action := range group.actions {
				if _, ok := s.Shortcuts[action.id]; ok && shortcutConflict(s, action, action.binding(s), menu) != "" {
					conflicts = append(conflicts, action.id)
				}
			}
		}
		if len(conflicts) == 0 {
			return s.Shortcuts
		}
		for _, id := range conflicts {
			delete(s.Shortcuts, id)
		}
	}
}

// Some menu shortcuts also live in MyGo's editor. Release the original
// binding there when the corresponding Perch command moves.
func editorShortcutBindings(s settings.Settings) []editor.Binding {
	var bindings []editor.Binding
	for _, action := range []shortcutAction{findInFileKeys, findReplaceKeys, goToDefinitionKeys, nextChangeKeys, previousChangeKeys} {
		if action.binding(s) != action.defaults {
			bindings = append(bindings, editor.Binding{Mods: action.defaults.mods, Key: shortcutKeys[action.defaults.key], Command: editor.NoCommand})
		}
	}
	return bindings
}
