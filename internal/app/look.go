package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/marukolibro/Perch/internal/settings"
)

// The corner radii of the interface, from its smallest parts to its panels.
const (
	radiusSmall   float32 = 6
	radiusControl float32 = 9
	radiusCard    float32 = 14
	radiusDialog  float32 = 18
)

// palette is the color scheme of a theme in one appearance.
type palette struct {
	background ui.Color
	sidebar    ui.Color
	hover      ui.Color
	border     ui.Color
	text       ui.Color
	muted      ui.Color
	accent     ui.Color
	warning    ui.Color
	danger     ui.Color

	keyword  ui.Color
	typeName ui.Color
	function ui.Color
	str      ui.Color
	number   ui.Color
	comment  ui.Color
	markup   ui.Color

	// fill is the face of primary buttons and checked controls, the accent
	// when a theme leaves it out, under fillText. fillHover and fillPressed
	// are that face under the pointer and pressed.
	fill, fillHover, fillPressed, fillText ui.Color
	// code is the plain text of code, the text color when left out.
	code ui.Color
	// gutter and gutterNow are the line numbers, and that of the caret's line.
	gutter, gutterNow ui.Color
	selection         ui.Color
	// added, changed and removed mark changed lines, the editor's own colors
	// when left out.
	added, changed, removed ui.Color
	// grain is how strongly a fine paper grain marks the window, 0 for none.
	grain float32
}

// lookKey names a look: a theme in one appearance.
type lookKey struct {
	theme settings.Theme
	dark  bool
}

// look is everything the window draws with in one appearance.
type look struct {
	palette palette
	theme   *ui.Theme
	// tree marks the chosen file with a quiet fill, which keeps the colors
	// of its icon readable.
	tree *ui.Theme
	// shell draws the divider beside the sidebar in the sidebar's color, so
	// that the main panel stands on its own.
	shell *ui.Theme
	code  *editor.Theme
	// markdown is how rendered Markdown reads: a document's typography
	// rather than a chat's, in the theme's colors.
	markdown *markdown.Theme
	// hover is how a language server's notes read in the editor's hover:
	// the same colors in smaller, closer text.
	hover *markdown.Theme
	// grain is the tile of the theme's paper grain, nil for a smooth theme.
	grain *ui.Bitmap
}

func newLook(base *ui.Theme, colors palette) *look {
	theme := *base.WithAccent(colors.fill)
	if colors.fillHover.A != 0 {
		theme.AccentHover, theme.AccentPressed = colors.fillHover, colors.fillPressed
	}
	if colors.fillText.A != 0 {
		theme.AccentText = colors.fillText
	}
	theme.InactiveAccent = colors.fill
	theme.Selection = colors.selection
	theme.Focus = colors.accent.Alpha(0.6)
	theme.Background = colors.background
	theme.Surface = colors.sidebar
	theme.SurfaceHover = colors.hover
	theme.SurfacePressed = colors.hover
	theme.Border = colors.border
	theme.Text = colors.text
	theme.TextMuted = colors.muted
	theme.Warning = colors.warning
	theme.Danger = colors.danger
	theme.Radius = radiusControl
	theme.FontSize = 13
	tree := theme
	tree.Accent = colors.hover
	tree.AccentHover = colors.hover
	tree.AccentPressed = colors.hover
	tree.AccentText = colors.text
	tree.InactiveAccent = colors.hover
	shell := theme
	shell.Border = colors.sidebar
	code := newCodeTheme(&theme, colors)
	reading := newMarkdownTheme(colors, code)
	made := &look{palette: colors, theme: &theme, tree: &tree, shell: &shell, code: code, markdown: reading, hover: newHoverMarkdownTheme(reading)}
	if colors.grain > 0 {
		made.grain = ui.NewBitmap(grainImage(colors.grain, base.Dark))
	}
	return made
}

func newCodeTheme(theme *ui.Theme, colors palette) *editor.Theme {
	code := editor.DefaultTheme(theme)
	code.Text = colors.code
	code.ActiveLine = colors.hover.Alpha(0.45)
	code.LineNumber = colors.gutter
	code.ActiveLineNumber = colors.gutterNow
	code.Selection = colors.selection
	code.InactiveSelection = colors.selection.Alpha(0.6)
	code.Cursor = colors.accent
	if colors.added.A != 0 {
		code.Added, code.Modified, code.Deleted = colors.added, colors.changed, colors.removed
	}
	code.IndentGuide = colors.border
	styles := map[editor.TokenKind]ui.Color{
		editor.TokenKeyword:  colors.keyword,
		editor.TokenType:     colors.typeName,
		editor.TokenFunction: colors.function,
		editor.TokenProperty: colors.code,
		editor.TokenVariable: colors.code,
		editor.TokenConstant: colors.number,
		editor.TokenNumber:   colors.number,
		editor.TokenString:   colors.str,
		editor.TokenEscape:   colors.keyword,
		editor.TokenRegexp:   colors.str,
		editor.TokenComment:  colors.comment,
		editor.TokenTag:      colors.keyword,
		editor.TokenMeta:     colors.markup,
		editor.TokenHeading:  colors.keyword,
		editor.TokenLink:     colors.function,
		editor.TokenCode:     colors.str,
	}
	for kind, color := range styles {
		code.Tokens[kind].Color = color
	}
	code.Tokens[editor.TokenComment].Italic = true
	return code
}

// The typography of rendered Markdown, in the units markdown.Theme takes.
const (
	readingFontSize    float32 = 15
	readingLineHeight  float32 = 1.65
	readingCodeHeight  float32 = 1.5
	readingBlockSpace  float32 = 1
	readingItemSpace   float32 = 0.15
	readingListIndent  float32 = 1.8
	readingCodeSize    float32 = 0.86
	readingCodePadding float32 = 1
	readingQuoteBar    float32 = 3
)

// newMarkdownTheme reads like a project's documentation: larger text, room
// between blocks, ruled top headings, links in the accent, and code in the
// editor's own colors.
func newMarkdownTheme(colors palette, code *editor.Theme) *markdown.Theme {
	theme := &markdown.Theme{
		FontSize:            readingFontSize,
		LineHeight:          readingLineHeight,
		CodeLineHeight:      readingCodeHeight,
		Text:                colors.text,
		Muted:               colors.muted,
		Link:                colors.accent,
		Strong:              colors.text,
		StrongWeight:        600,
		Code:                colors.code,
		CodeBackground:      colors.text.Alpha(0.07),
		CodeSize:            readingCodeSize,
		BlockSpacing:        readingBlockSpace,
		ItemSpacing:         readingItemSpace,
		ListIndent:          readingListIndent,
		QuoteBar:            colors.muted.Alpha(0.35),
		QuoteText:           colors.muted,
		QuoteBarWidth:       readingQuoteBar,
		Rule:                colors.border,
		CodeBlockBackground: colors.sidebar,
		CodeBlockBorder:     colors.border,
		CodeBlockRadius:     radiusControl,
		CodeBlockPadding:    readingCodePadding,
		CodeColors:          code,
		TableBorder:         colors.border,
		TableHeader:         colors.sidebar,
		TableStripe:         colors.sidebar.Alpha(0.5),
		TableCellPadding:    [2]float32{0.85, 0.5},
		Checkbox:            colors.accent,
	}
	sizes := [6]float32{2, 1.5, 1.25, 1.05, 0.95, 0.9}
	for level := range theme.Headings {
		heading := markdown.HeadingStyle{Size: sizes[level], Weight: 600, Color: colors.text, Top: 1.4, Bottom: 0.5}
		if level < 2 {
			heading.Top, heading.Bottom, heading.Rule = 1.6, 0.7, true
		}
		theme.Headings[level] = heading
	}
	theme.Headings[5].Color = colors.muted
	return theme
}

// The typography of a hover's Markdown.
const (
	hoverFontSize   float32 = 13
	hoverLineHeight float32 = 1.5
	hoverBlockSpace float32 = 0.6
)

// newHoverMarkdownTheme reads like a tooltip: the reading theme in smaller,
// closer text, with headings barely larger than the text and no rules.
func newHoverMarkdownTheme(reading *markdown.Theme) *markdown.Theme {
	theme := *reading
	theme.FontSize = hoverFontSize
	theme.LineHeight = hoverLineHeight
	theme.BlockSpacing = hoverBlockSpace
	for level := range theme.Headings {
		heading := theme.Headings[level]
		heading.Size, heading.Top, heading.Bottom, heading.Rule = 1, 0.6, 0.3, false
		theme.Headings[level] = heading
	}
	return &theme
}

// lookFor returns the look of the chosen theme in the window's appearance,
// built the first time it shows.
func (a *App) lookFor(base *ui.Theme) *look {
	key := lookKey{theme: a.settings.Theme, dark: base.Dark}
	made, built := a.looks[key]
	if !built {
		made = newLook(base, paletteFor(key.theme, key.dark))
		a.looks[key] = made
	}
	return made
}

const iconAttributes = `xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"`

var (
	iconFolder       = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>`))
	iconFile         = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/></svg>`))
	iconFiles        = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M20 7h-3a2 2 0 0 1-2-2V2"/><path d="M9 18a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h7l4 4v10a2 2 0 0 1-2 2Z"/><path d="M3 7.6v12.8A1.6 1.6 0 0 0 4.6 22h9.8"/></svg>`))
	iconSearch       = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>`))
	iconChanges      = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M13 6h3a2 2 0 0 1 2 2v7"/><path d="M11 18H8a2 2 0 0 1-2-2V9"/></svg>`))
	iconHistory      = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/></svg>`))
	iconBlame        = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><circle cx="12" cy="12" r="3"/><path d="M3 12h6"/><path d="M15 12h6"/></svg>`))
	iconCircle       = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><circle cx="12" cy="12" r="9"/></svg>`))
	iconCheck        = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><circle cx="12" cy="12" r="9"/><path d="m8.5 12 2.5 2.5 4.5-5"/></svg>`))
	iconSettings     = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M21 7h-9"/><path d="M14 17H3"/><circle cx="17" cy="17" r="3"/><circle cx="7" cy="7" r="3"/></svg>`))
	iconAlert        = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><circle cx="12" cy="12" r="10"/><path d="M12 8v4"/><path d="M12 16h.01"/></svg>`))
	iconInfo         = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>`))
	iconClose        = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`))
	iconFilePlus     = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M9 15h6"/><path d="M12 18v-6"/></svg>`))
	iconFolderPlus   = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M12 10v6"/><path d="M9 13h6"/><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>`))
	iconRefresh      = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M20 7a9 9 0 1 0 1 8"/><path d="M20 2v5h-5"/></svg>`))
	iconCollapse     = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="m7 5 5 5 5-5"/><path d="m7 19 5-5 5 5"/></svg>`))
	iconPencil       = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M21.17 6.81a2.82 2.82 0 0 0-3.99-3.99L3.84 16.17a2 2 0 0 0-.5.83l-1.32 4.35a.5.5 0 0 0 .62.62l4.35-1.32a2 2 0 0 0 .83-.5Z"/><path d="m15 5 4 4"/></svg>`))
	iconTrash        = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><path d="M10 11v6"/><path d="M14 11v6"/></svg>`))
	iconCopy         = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2"/></svg>`))
	iconLink         = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>`))
	iconReveal       = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="m6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2"/></svg>`))
	iconPalette      = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M12 22a1 1 0 0 1 0-20 10 9 0 0 1 10 9 5 5 0 0 1-5 5h-2.25a1.75 1.75 0 0 0-1.4 2.8l.3.4a1.75 1.75 0 0 1-1.4 2.8z"/><circle cx="13.5" cy="6.5" r=".5" fill="currentColor"/><circle cx="17.5" cy="10.5" r=".5" fill="currentColor"/><circle cx="6.5" cy="12.5" r=".5" fill="currentColor"/><circle cx="8.5" cy="7.5" r=".5" fill="currentColor"/></svg>`))
	iconCode         = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="m18 16 4-4-4-4"/><path d="m6 8-4 4 4 4"/><path d="m14.5 4-5 16"/></svg>`))
	iconBook         = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M12 7v14"/><path d="M3 18a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-6a3 3 0 0 0-3 3 3 3 0 0 0-3-3z"/></svg>`))
	iconKeyboard     = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><rect x="2" y="5" width="20" height="14" rx="2"/><path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M6 12h.01M10 12h.01M14 12h.01M18 12h.01M8 15h8"/></svg>`))
	iconBraces       = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M8 3H7a2 2 0 0 0-2 2v5a2 2 0 0 1-2 2 2 2 0 0 1 2 2v5c0 1.1.9 2 2 2h1"/><path d="M16 21h1a2 2 0 0 0 2-2v-5c0-1.1.9-2 2-2a2 2 0 0 1-2-2V5a2 2 0 0 0-2-2h-1"/></svg>`))
	iconPerch        = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M3 20h18"/><path d="M11 20v-3"/><path d="M5 12a7 7 0 0 1 12.5-4.3L21 7l-2.6 3.1A7 7 0 0 1 12 17H9a4 4 0 0 1-4-4Z"/><path d="M15 9h.01"/></svg>`))
	iconSidebar      = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/></svg>`))
	iconLocate       = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><line x1="2" x2="5" y1="12" y2="12"/><line x1="19" x2="22" y1="12" y2="12"/><line x1="12" x2="12" y1="2" y2="5"/><line x1="12" x2="12" y1="19" y2="22"/><circle cx="12" cy="12" r="7"/><circle cx="12" cy="12" r="2"/></svg>`))
	iconFilter       = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/></svg>`))
	iconTree         = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M20 10a1 1 0 0 0 1-1V6a1 1 0 0 0-1-1h-2.5a1 1 0 0 1-.8-.4l-.9-1.2A1 1 0 0 0 15 3h-2a1 1 0 0 0-1 1v5a1 1 0 0 0 1 1Z"/><path d="M20 21a1 1 0 0 0 1-1v-3a1 1 0 0 0-1-1h-2.9a1 1 0 0 1-.88-.55l-.42-.85a1 1 0 0 0-.92-.6H13a1 1 0 0 0-1 1v5a1 1 0 0 0 1 1Z"/><path d="M3 5a2 2 0 0 0 2 2h3"/><path d="M3 3v13a2 2 0 0 0 2 2h3"/></svg>`))
	iconEyeOff       = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49"/><path d="M14.084 14.158a3 3 0 0 1-4.242-4.242"/><path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143"/><path d="m2 2 20 20"/></svg>`))
	iconColumns      = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M12 3v18"/></svg>`))
	iconRows         = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M3 12h18"/></svg>`))
	iconChevronRight = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="m9 18 6-6-6-6"/></svg>`))
	iconChevronDown  = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><path d="m6 9 6 6 6-6"/></svg>`))
	iconWorktrees    = ui.MustParseSVG([]byte(`<svg ` + iconAttributes + `><circle cx="12" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><circle cx="18" cy="6" r="3"/><path d="M18 9v2c0 .6-.4 1-1 1H7c-.6 0-1-.4-1-1V9"/><path d="M12 12v3"/></svg>`))
)
