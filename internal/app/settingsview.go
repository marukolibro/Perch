package app

import (
	"fmt"
	"maps"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/internal/settings"
	"github.com/marukolibro/Perch/locales"
)

const (
	settingsMaxWidth   float32 = 800
	settingsMaxHeight  float32 = 580
	settingsWidthPart  float32 = 94
	settingsHeightPart float32 = 92
	settingsRailWidth  float32 = 180
	settingsRowHeight  float32 = 52
	settingsNavHeight  float32 = 32
	settingsPagePad    float32 = 24
	themeTileWidth     float32 = 150
	themeTileHeight    float32 = 92

	languageSystem  = ""
	languageEnglish = "en"
	languageChinese = "zh-Hans"
)

// settingsPage is a page of the settings.
type settingsPage uint8

// The pages of the settings, in the order of their list.
const (
	settingsAppearance settingsPage = iota
	settingsEditor
	settingsReading
	settingsLanguages
	settingsShortcuts
)

// settingsNavItem is a page in the list beside the settings.
type settingsNavItem struct {
	page  settingsPage
	icon  *ui.SVG
	label string
}

// settingLine is one row of a group of settings.
type settingLine struct {
	label  string
	detail string
	// control is the row's control, nil for a row that only says something.
	control func()
}

func settingsNavItems() []settingsNavItem {
	return []settingsNavItem{
		{settingsAppearance, iconPalette, locales.SectionAppearance()},
		{settingsEditor, iconCode, locales.SectionEditor()},
		{settingsReading, iconBook, locales.SectionReading()},
		{settingsLanguages, iconBraces, locales.SettingLanguageServers()},
		{settingsShortcuts, iconKeyboard, locales.SectionShortcuts()},
	}
}

// showSettings opens the settings over the window, on their first page.
func (a *App) showSettings() {
	a.languages = languagesPage{}
	a.shortcutEdit = shortcutEdit{}
	a.settingsPage = settingsAppearance
	a.settingsOpen = true
}

// chooseSettingsPage shows a page, finding out what it lists the first time.
func (a *App) chooseSettingsPage(page settingsPage) {
	a.shortcutEdit = shortcutEdit{}
	a.settingsPage = page
	if page == settingsLanguages && !a.languages.open {
		a.showLanguages()
	}
}

// updateSettings applies new settings to every window.
func (a *App) updateSettings(next settings.Settings) {
	if a.desktop != nil {
		a.desktop.settingsChanged(next)
	} else {
		a.setWindowSettings(next)
	}
}

// toggleSidebar hides or shows the sidebar in this window.
func (a *App) toggleSidebar() {
	if a.root == "" {
		return
	}
	a.sidebarHidden = !a.sidebarHidden
	if a.showsChangesPanel() {
		a.refreshChanges()
	}
}

// changeFontSize steps the code font size within its limits and saves it.
func (a *App) changeFontSize(delta int) {
	next := a.settings
	next.FontSize = max(settings.MinFontSize, min(settings.MaxFontSize, next.FontSize+delta))
	if next.FontSize != a.settings.FontSize {
		a.updateSettings(next)
	}
}

// resetFontSize returns the code font size to its default and saves it.
func (a *App) resetFontSize() {
	if a.settings.FontSize == settings.DefaultFontSize {
		return
	}
	next := a.settings
	next.FontSize = settings.DefaultFontSize
	a.updateSettings(next)
}

func (a *App) setWindowSettings(next settings.Settings) {
	if !maps.Equal(a.settings.Shortcuts, next.Shortcuts) {
		a.editorBindings = editorShortcutBindings(next)
	}
	a.settings = next
}

// codeFont returns the font of code in the current settings.
func (a *App) codeFont() ui.Font {
	return ui.Font{Size: float32(a.settings.FontSize)}
}

// applyEditorSettings gives an editor the current settings.
func (a *App) applyEditorSettings(state *editor.State) {
	state.Bindings = a.editorBindings
	state.Font = a.codeFont()
	state.TabSize = a.settings.TabSize
	state.SoftWrap = a.settings.WordWrap
}

// applyDiffSettings gives a change's diff the current settings.
func (a *App) applyDiffSettings(diff *editor.DiffState) {
	diff.Bindings = a.editorBindings
	diff.Font = a.codeFont()
	diff.TabSize = a.settings.TabSize
	diff.Wrap = a.settings.WordWrap
	diff.Layout = editor.DiffUnified
	if a.settings.DiffLayout == settings.DiffSplit {
		diff.Layout = editor.DiffSplit
	}
}

func (a *App) settingsView(c *ui.Context) {
	if !a.settingsOpen {
		return
	}
	colors := a.look.palette
	ui.DialogBase(c, &a.settingsOpen, func(backdrop ui.Element, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.28))
		panel.WidthPercent(settingsWidthPart).MaxWidth(settingsMaxWidth).HeightPercent(settingsHeightPart).MaxHeight(settingsMaxHeight).
			Background(colors.background).Radius(radiusDialog).Border(1, colors.border).Clip().
			Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.28)).Label(locales.SettingsTitle())
		ui.Row(c).Fill().Children(func() {
			a.settingsRailView(c)
			a.settingsPageView(c)
		})
	})
}

// settingsRailView lists the pages of the settings.
func (a *App) settingsRailView(c *ui.Context) {
	colors := a.look.palette
	ui.Column(c).Width(settingsRailWidth).Shrink(0).FillHeight().Gap(2).Padding(14, 10).Background(colors.sidebar).
		BorderWidth(0, 1, 0, 0).BorderColor(colors.border).Children(func() {
		ui.Text(c, locales.SettingsTitle()).FontSize(15).FontWeight(600).Padding(4, 8, 12, 8)
		for _, item := range settingsNavItems() {
			c.Key(item.label)
			a.settingsNavRow(c, item)
		}
	})
}

func (a *App) settingsNavRow(c *ui.Context, item settingsNavItem) {
	colors := a.look.palette
	chosen := a.settingsPage == item.page
	row := ui.Row(c).FillWidth().Height(settingsNavHeight).Gap(9).Padding(0, 8).AlignItems(ui.Center).Radius(radiusControl).Label(item.label)
	if chosen || row.Hovered() {
		row.Background(colors.hover)
	}
	if row.Clicked() {
		a.chooseSettingsPage(item.page)
	}
	row.Children(func() {
		tint, weight := colors.muted, 400
		if chosen {
			tint, weight = colors.text, 600
		}
		ui.Icon(c, item.icon).FontSize(15).TextColor(tint).Shrink(0)
		ui.Text(c, item.label).FontWeight(weight).TextColor(tint).MaxLines(1)
	})
}

// settingsPageView shows the chosen page under its title.
func (a *App) settingsPageView(c *ui.Context) {
	colors := a.look.palette
	title := settingsNavItems()[a.settingsPage].label
	ui.Column(c).Grow(1).Shrink(1).Basis(0).MinWidth(0).FillHeight().Children(func() {
		ui.Row(c).FillWidth().Height(56).Shrink(0).Padding(0, 14, 0, settingsPagePad).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(16).FontWeight(600)
			ui.Spacer(c)
			button := ui.Box(c).Size(26, 26).Center().Radius(radiusSmall).Label(locales.CloseSettings())
			if button.Hovered() {
				button.Background(colors.hover)
			}
			if button.Clicked() {
				a.settingsOpen = false
			}
			button.Children(func() { ui.Icon(c, iconClose).FontSize(14).TextColor(colors.muted) })
		})
		c.Key(title + a.shortcutEdit.action.id)
		ui.Scroll(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(20).Padding(2, settingsPagePad, settingsPagePad, settingsPagePad).Children(func() {
				a.settingsPageContent(c)
			})
		})
	})
}

func (a *App) settingsPageContent(c *ui.Context) {
	switch a.settingsPage {
	case settingsEditor:
		a.settingsGroup(c, "",
			settingLine{label: locales.SettingFontSize(), control: func() { a.fontSizeControl(c) }},
			settingLine{label: locales.SettingTabSize(), control: func() { a.tabSizeControl(c) }},
			settingLine{label: locales.SettingWrap(), control: func() { a.wordWrapControl(c) }},
		)
	case settingsReading:
		a.settingsGroup(c, "",
			settingLine{label: locales.SettingMarkdown(), control: func() { a.markdownViewControl(c) }},
			settingLine{label: locales.SettingDiff(), control: func() { a.diffLayoutControl(c) }},
		)
	case settingsShortcuts:
		a.shortcutSettingsView(c)
	case settingsLanguages:
		a.languagesContent(c)
	default:
		a.themeGroup(c)
		a.settingsGroup(c, "",
			settingLine{label: locales.SettingColorMode(), control: func() { a.appearanceControl(c) }},
			settingLine{label: locales.SettingLanguage(), control: func() { a.languageControl(c) }},
		)
	}
}

// settingsTitle names a group of settings.
func (a *App) settingsTitle(c *ui.Context, title string) {
	if title != "" {
		ui.Text(c, title).FontSize(smallFontSize).FontWeight(600).TextColor(a.look.palette.muted)
	}
}

// settingsGroup shows rows of settings on one card, under an optional title.
func (a *App) settingsGroup(c *ui.Context, title string, lines ...settingLine) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		a.settingsTitle(c, title)
		a.settingsCard(c, func() {
			for index, line := range lines {
				c.Key(line.label)
				a.settingLineView(c, line, index > 0)
			}
		})
	})
}

// settingsCard is the rounded surface that groups rows.
func (a *App) settingsCard(c *ui.Context, rows func()) {
	colors := a.look.palette
	ui.Column(c).FillWidth().Radius(radiusCard).Border(1, colors.border).Background(colors.sidebar.Alpha(0.45)).Clip().Children(rows)
}

func (a *App) settingLineView(c *ui.Context, line settingLine, divided bool) {
	colors := a.look.palette
	row := ui.Row(c).FillWidth().MinHeight(settingsRowHeight).Gap(14).Padding(8, 14).AlignItems(ui.Center)
	if divided {
		row.BorderWidth(1, 0, 0, 0).BorderColor(colors.border)
	}
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, line.label).MaxLines(1)
			if line.detail != "" {
				ui.Text(c, line.detail).FontSize(smallFontSize - 1).TextColor(colors.muted)
			}
		})
		if line.control != nil {
			line.control()
		}
	})
}

// themeGroup chooses the theme from a small window of each. The arrow keys
// move the choice while a window has the keyboard.
func (a *App) themeGroup(c *ui.Context) {
	choices := themeChoices()
	chosen := max(slices.IndexFunc(choices, func(choice themeChoice) bool { return choice.id == a.settings.Theme }), 0)
	selected := chosen
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		a.settingsTitle(c, locales.SettingTheme())
		tiles := ui.SegmentedBase(c.Key("themes"), &selected, len(choices))
		tiles.Track.FillWidth().Wrap().Gap(14).AlignItems(ui.Start).Label(locales.SettingTheme()).Children(func() {
			for index, choice := range choices {
				a.themeTile(c, tiles.Segment(index), choice, index == selected)
			}
		})
	})
	if selected != chosen {
		next := a.settings
		next.Theme = choices[selected].id
		a.updateSettings(next)
	}
}

func (a *App) themeTile(c *ui.Context, tile ui.Element, choice themeChoice, chosen bool) {
	colors := a.look.palette
	preview := paletteFor(choice.id, c.Theme().Dark)
	tile.Column().Width(themeTileWidth).Shrink(0).Padding(0).Gap(7).AlignItems(ui.Start).Background(ui.Transparent).
		Label(choice.name()).Tooltip(choice.about())
	hovered := tile.Hovered()
	tile.Children(func() {
		edge, width := colors.border, float32(1)
		switch {
		case chosen:
			edge, width = colors.accent, 2
		case hovered:
			edge = colors.muted
		}
		ui.Box(c.Key("preview")).Width(themeTileWidth).Height(themeTileHeight).Shrink(0).Children(func() {
			ui.Box(c.Key("window")).Fill().Radius(radiusControl).Clip().
				Draw(func(p *ui.Painter, r ui.Rect) { themePreview(p, r, preview) })
			// The edge lies over the drawing, which would otherwise cover it.
			ui.Box(c.Key("edge")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(radiusControl).Border(width, edge).PassThrough()
		})
		ui.Column(c.Key("name")).FillWidth().Gap(1).Children(func() {
			ui.Text(c, choice.name()).FontWeight(600).MaxLines(1)
			ui.Text(c, choice.about()).FontSize(smallFontSize - 1).TextColor(colors.muted)
		})
	})
}

// choiceControl shows options and returns the one chosen.
func choiceControl[T comparable](c *ui.Context, current T, values []T, labels ...string) T {
	index := max(slices.Index(values, current), 0)
	ui.Segmented(c, &index, labels...)
	return values[index]
}

// languageControl names each language in itself, so that a person who cannot
// read the current one still finds their own.
func (a *App) languageControl(c *ui.Context) {
	current := a.chosenLanguage()
	values := []string{languageSystem, languageEnglish, languageChinese}
	chosen := choiceControl(c, current, values, locales.FollowSystem(), "English", "简体中文")
	if chosen != current {
		a.setLanguage(chosen)
	}
}

func (a *App) appearanceControl(c *ui.Context) {
	values := []settings.Appearance{settings.AppearanceSystem, settings.AppearanceLight, settings.AppearanceDark}
	chosen := choiceControl(c, a.settings.Appearance, values, locales.FollowSystem(), locales.ThemeLight(), locales.ThemeDark())
	if chosen != a.settings.Appearance {
		next := a.settings
		next.Appearance = chosen
		a.updateSettings(next)
	}
}

func (a *App) fontSizeControl(c *ui.Context) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("%d px", a.settings.FontSize)).TextColor(a.look.palette.muted)
		size := float64(a.settings.FontSize)
		ui.Stepper(c, &size, float64(settings.MinFontSize), float64(settings.MaxFontSize), 1).Label(locales.SettingFontSize())
		if int(size) != a.settings.FontSize {
			next := a.settings
			next.FontSize = int(size)
			a.updateSettings(next)
		}
	})
}

func (a *App) tabSizeControl(c *ui.Context) {
	chosen := choiceControl(c, a.settings.TabSize, settings.TabSizes, locales.TabSizeAuto(), "2", "4", "8")
	if chosen != a.settings.TabSize {
		next := a.settings
		next.TabSize = chosen
		a.updateSettings(next)
	}
}

func (a *App) wordWrapControl(c *ui.Context) {
	wrap := a.settings.WordWrap
	ui.Switch(c, &wrap).Label(locales.SettingWrap())
	if wrap != a.settings.WordWrap {
		next := a.settings
		next.WordWrap = wrap
		a.updateSettings(next)
	}
}

func (a *App) markdownViewControl(c *ui.Context) {
	values := []settings.MarkdownView{settings.MarkdownPreview, settings.MarkdownSource}
	chosen := choiceControl(c, a.settings.MarkdownView, values, locales.MarkdownPreview(), locales.MarkdownSource())
	if chosen != a.settings.MarkdownView {
		next := a.settings
		next.MarkdownView = chosen
		a.updateSettings(next)
	}
}

func (a *App) diffLayoutControl(c *ui.Context) {
	values := []settings.DiffLayout{settings.DiffUnified, settings.DiffSplit}
	chosen := choiceControl(c, a.settings.DiffLayout, values, locales.DiffUnified(), locales.DiffSideBySide())
	if chosen != a.settings.DiffLayout {
		next := a.settings
		next.DiffLayout = chosen
		a.updateSettings(next)
	}
}
