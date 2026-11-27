package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"github.com/marukolibro/Perch/locales"
)

func TestSplitKeepsAnEditorPerPaneAndTradesRolesOnFocus(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n\nfunc one() {}\n")
	writeFile(t, root, "b.go", "package b\n")
	a, tester := openWorkspace(t, root)
	a.openFile("a.go")
	tester.Frame()

	if err := tester.Click(locales.MenuSplitRight()); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	docA := a.activeDocument()
	first := docA.editor
	if a.split.layout != splitRight || a.split.doc != docA || a.split.view == nil || a.split.view == first || a.split.view.Buffer != first.Buffer {
		t.Fatalf("split after Split Right = %+v, active editor %p", a.split, first)
	}

	// A click in the other pane gives it the keyboard's role, with its own
	// editor and the document's actions.
	twin := a.split.view
	a.focusOtherPane()
	tester.Frame()
	if docA.editor != twin || a.split.view != first || !a.split.otherFirst {
		t.Fatalf("after focusing the other pane: active %p, other %p, otherFirst %v", docA.editor, a.split.view, a.split.otherFirst)
	}
	if twin.OnDefinition == nil || first.OnDefinition != nil {
		t.Fatal("definitions did not move to the focused pane's editor")
	}

	// The focused pane opens another file; the other pane keeps a.go.
	a.openFile("b.go")
	tester.Frame()
	docB := a.activeDocument()
	if a.split.doc != docA || a.split.view != first {
		t.Fatalf("other pane changed to %q", a.split.doc.path)
	}
	a.focusOtherPane()
	tester.Frame()
	if a.activeDocument() != docA || docA.editor != first || a.split.doc != docB || a.split.view != docB.editor {
		t.Fatalf("after focusing a.go: active %q, other %q", a.activeDocument().path, a.split.doc.path)
	}

	// Closing the other pane's document shows the active one there too.
	a.closeTab(docB)
	tester.Frame()
	if a.split.doc != docA || a.split.view == docA.editor || a.split.view.Buffer != docA.editor.Buffer {
		t.Fatalf("after closing b.go the other pane shows %q", a.split.doc.path)
	}

	a.closeTab(docA)
	tester.Frame()
	if a.split.layout != splitNone {
		t.Fatalf("split stayed open with no documents: %+v", a.split)
	}
}

func TestCaretWordMarksWholeWordsAroundTheView(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n\nfunc count() int { return count() + counter }\n")
	a, tester := openWorkspace(t, root)
	a.openFile("a.go")
	tester.Frame()
	d := a.activeDocument()
	d.editor.GoTo(editor.Pos{Line: 2, Col: 6})
	tester.Frame()
	if got := len(d.editor.TextBackgrounds); got != 2 {
		t.Fatalf("marks of count = %d, want 2", got)
	}

	d.editor.Select(0, 7)
	tester.Frame()
	if len(d.editor.TextBackgrounds) != 0 {
		t.Fatal("marks stayed while text is selected")
	}
}

func TestWordAroundAndWholeWordMatches(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		col       int
		wordChars string
		word      string
		found     bool
		matches   []span
	}{
		{name: "inside", line: "a := foo(foo_bar, foo)", col: 6, word: "foo", found: true, matches: []span{{5, 8}, {18, 21}}},
		{name: "after the word", line: "foo.bar", col: 3, word: "foo", found: true, matches: []span{{0, 3}}},
		{name: "number", line: "x = 42", col: 5, found: false},
		{name: "space", line: "a  b", col: 2, found: false},
		{name: "language word characters", line: ".btn-primary .btn", col: 3, wordChars: "-", word: "btn-primary", found: true, matches: []span{{1, 12}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, found := wordAround(test.line, test.col, test.wordChars)
			if found != test.found {
				t.Fatalf("found = %v, want %v", found, test.found)
			}
			if !found {
				return
			}
			if word := test.line[start:end]; word != test.word {
				t.Fatalf("word = %q, want %q", word, test.word)
			}
			if got := wholeWordMatches(test.line, test.word, test.wordChars); !slices.Equal(got, test.matches) {
				t.Fatalf("matches = %v, want %v", got, test.matches)
			}
		})
	}
}
