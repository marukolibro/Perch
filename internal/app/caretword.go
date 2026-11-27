package app

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// caretWordLines is how many lines above and below the view the marks of
// the word at the caret reach, so that a short scroll still shows them.
const caretWordLines int = 80

// caretWordState remembers which editor shows the marks of the word at its
// caret, and for which text, caret and view, so that they are found again
// only when one of these changes.
type caretWordState struct {
	state   *editor.State
	version uint64
	caret   int
	first   int
	last    int
}

// span is a range of bytes from Start to End.
type span struct {
	start int
	end   int
}

// markCaretWord marks the other places of the word at the caret of the
// focused editor, as VS Code does without a language server: the editor
// itself marks those of selected text only.
func (a *App) markCaretWord(state *editor.State) {
	first, last := state.VisibleLines()
	wanted := caretWordState{state: state, version: state.Buffer.Version(), caret: state.Cursor(), first: first, last: last}
	if wanted == a.caretWord {
		return
	}
	if previous := a.caretWord.state; previous != nil && previous != state {
		previous.TextBackgrounds = nil
	}
	a.caretWord = wanted
	state.TextBackgrounds = caretWordBackgrounds(state, a.look.code.Occurrence)
}

// forgetCaretWord takes the marks away from the editor that shows them.
func (a *App) forgetCaretWord() {
	if a.caretWord.state != nil {
		a.caretWord.state.TextBackgrounds = nil
	}
	a.caretWord = caretWordState{}
}

// caretWordBackgrounds returns the fills of the places of the word at the
// caret in the lines around the view, none while text is selected.
func caretWordBackgrounds(state *editor.State, color ui.Color) []editor.TextBackground {
	selection := state.Selection()
	if selection.Anchor != selection.Head || len(state.Selections()) != 1 {
		return nil
	}
	buffer := state.Buffer
	wordChars := ""
	if language := buffer.Language(); language != nil {
		wordChars = language.WordChars
	}
	caret := buffer.Pos(selection.Head)
	start, end, found := wordAround(buffer.Line(caret.Line), caret.Col, wordChars)
	if !found {
		return nil
	}
	word := buffer.Line(caret.Line)[start:end]
	first, last := state.VisibleLines()
	first = max(0, first-caretWordLines)
	last = min(buffer.LineCount()-1, last+caretWordLines)
	var backgrounds []editor.TextBackground
	for line := first; line <= last; line++ {
		lineStart := buffer.Offset(editor.Pos{Line: line})
		for _, match := range wholeWordMatches(buffer.Line(line), word, wordChars) {
			backgrounds = append(backgrounds, editor.TextBackground{Start: lineStart + match.start, End: lineStart + match.end, Color: color})
		}
	}
	return backgrounds
}

// isWordRune reports whether r belongs to a word: a letter, a digit, _ or
// one of the language's extra word characters.
func isWordRune(r rune, wordChars string) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(wordChars, r)
}

// wordAround returns the bytes of the word of line touching column col,
// before or after it, and whether there is one. A run of digits alone is no
// word, as numbers are rarely worth following.
func wordAround(line string, col int, wordChars string) (int, int, bool) {
	col = min(max(col, 0), len(line))
	start := col
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:start])
		if !isWordRune(r, wordChars) {
			break
		}
		start -= size
	}
	end := col
	for end < len(line) {
		r, size := utf8.DecodeRuneInString(line[end:])
		if !isWordRune(r, wordChars) {
			break
		}
		end += size
	}
	if start == end || !strings.ContainsFunc(line[start:end], func(r rune) bool { return !unicode.IsDigit(r) }) {
		return 0, 0, false
	}
	return start, end, true
}

// wholeWordMatches returns the places of word in line that no other word
// character touches.
func wholeWordMatches(line string, word string, wordChars string) []span {
	var matches []span
	for at := 0; at <= len(line)-len(word); {
		index := strings.Index(line[at:], word)
		if index < 0 {
			break
		}
		start := at + index
		end := start + len(word)
		before, _ := utf8.DecodeLastRuneInString(line[:start])
		after, _ := utf8.DecodeRuneInString(line[end:])
		if (start == 0 || !isWordRune(before, wordChars)) && (end == len(line) || !isWordRune(after, wordChars)) {
			matches = append(matches, span{start: start, end: end})
		}
		at = end
	}
	return matches
}
