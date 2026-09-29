package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(code rune, text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: text} }
func TestVimModeAndLiteralTabs(t *testing.T) {
	e := NewEditor("vim", "# Héllo\n")
	if e.Mode != "NORMAL" {
		t.Fatal(e.Mode)
	}
	e.Handle(key('i', "i"))
	e.Handle(key(tea.KeyTab, ""))
	e.Handle(key('?', "?"))
	e.Escape()
	if !strings.HasPrefix(e.Value(), "\t?#") || e.Mode != "NORMAL" {
		t.Fatalf("value %q mode %q", e.Value(), e.Mode)
	}
	e.UndoEdit()
	if e.Value() != "# Héllo\n" {
		t.Fatalf("undo: %q", e.Value())
	}
	e.RedoEdit()
	if !strings.HasPrefix(e.Value(), "\t?#") {
		t.Fatalf("redo: %q", e.Value())
	}
}
func TestTraditionalAndOperator(t *testing.T) {
	e := NewEditor("traditional", "first line\nsecond line\n")
	if e.Mode != "ENTRY" {
		t.Fatal(e.Mode)
	}
	e.Handle(key(tea.KeyTab, ""))
	if e.Value()[0] != '\t' {
		t.Fatal("tab changed")
	}
	e.Escape()
	if e.Mode != "COMMAND" {
		t.Fatal(e.Mode)
	}
	e.UndoEdit()
	if e.Value() != "first line\nsecond line\n" {
		t.Fatalf("traditional undo erased source: %q", e.Value())
	}
	e.Pos = 1
	e.handleNormal("d")
	e.handleNormal("d")
	if strings.Contains(e.Value(), "first line") {
		t.Fatalf("dd: %q", e.Value())
	}
}
func TestTraditionalSelectionReplacesUnicodeText(t *testing.T) {
	e := NewEditor("traditional", "naïve café")
	e.Pos = len(e.Text)
	e.render()
	for i := 0; i < 4; i++ {
		e.Handle(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
	}
	if got := e.Area.Value(); !strings.Contains(got, "⟦café⟧") {
		t.Fatalf("selection is not visible: %q", got)
	}
	e.insert("tea")
	if e.Value() != "naïve tea" || e.Selecting {
		t.Fatalf("selection was not replaced: %q", e.Value())
	}
	e.Escape()
	e.UndoEdit()
	if e.Value() != "naïve café" {
		t.Fatalf("selection replacement did not undo: %q", e.Value())
	}
	for _, deleteKey := range []rune{tea.KeyBackspace, tea.KeyDelete} {
		e = NewEditor("traditional", "αβγδ")
		e.Pos = len(e.Text)
		e.render()
		for i := 0; i < 2; i++ {
			e.Handle(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
		}
		e.Handle(tea.KeyPressMsg{Code: deleteKey})
		if e.Value() != "αβ" {
			t.Fatalf("key %d did not delete selection: %q", deleteKey, e.Value())
		}
	}
}
func TestVimCountAndRepeat(t *testing.T) {
	e := NewEditor("vim", "one\ntwo\nthree\n")
	e.handleNormal("2")
	e.handleNormal("d")
	e.handleNormal("d")
	if e.Value() != "three\n" {
		t.Fatalf("2dd: %q", e.Value())
	}
	e.handleNormal(".")
	if e.Value() != "" {
		t.Fatalf("repeat dd: %q", e.Value())
	}
}
func TestVimLinewisePutAndOpenUndo(t *testing.T) {
	e := NewEditor("vim", "one\ntwo\n")
	e.handleNormal("y")
	e.handleNormal("y")
	e.handleNormal("p")
	if e.Value() != "one\none\ntwo\n" {
		t.Fatalf("yy/p: %q", e.Value())
	}
	e.UndoEdit()
	if e.Value() != "one\ntwo\n" {
		t.Fatalf("put undo: %q", e.Value())
	}
	e.handleNormal("o")
	e.Handle(key('x', "x"))
	e.Escape()
	e.UndoEdit()
	if e.Value() != "one\ntwo\n" {
		t.Fatalf("open-line insert was not one undo step: %q", e.Value())
	}
}
func TestVimCountedLineJumps(t *testing.T) {
	e := NewEditor("vim", "one\ntwo\nthree\n")
	e.handleNormal("2")
	e.handleNormal("G")
	if e.Pos != len([]rune("one\n")) {
		t.Fatalf("2G position: %d", e.Pos)
	}
	e.handleNormal("3")
	e.handleNormal("g")
	e.handleNormal("g")
	if e.Pos != len([]rune("one\ntwo\n")) {
		t.Fatalf("3gg position: %d", e.Pos)
	}
}
func TestReverseVisualSelectionAndChangeUndo(t *testing.T) {
	e := NewEditor("vim", "one two three")
	e.Pos = len([]rune("one tw"))
	e.handleNormal("v")
	e.handleNormal("b")
	if got := e.Area.Value(); !strings.Contains(got, "⟦two⟧") {
		t.Fatalf("visual selection is not visible: %q", got)
	}
	e.handleNormal("y")
	if got := e.Area.Value(); strings.Contains(got, "⟦") || strings.Contains(got, "⟧") {
		t.Fatalf("selection markers remain after yank: %q", got)
	}
	if e.Register != "two" {
		t.Fatalf("reverse selection missed start character: %q", e.Register)
	}
	e.Pos = len([]rune("one tw"))
	e.handleNormal("v")
	e.handleNormal("b")
	e.handleNormal("c")
	e.Handle(key('X', "X"))
	e.Escape()
	if e.Value() != "one X three" {
		t.Fatalf("visual change: %q", e.Value())
	}
	e.UndoEdit()
	if e.Value() != "one two three" {
		t.Fatalf("visual change undo failed: %q", e.Value())
	}
}
func TestReverseVisualLineSelection(t *testing.T) {
	e := NewEditor("vim", "one\ntwo\nthree\n")
	e.Pos = len([]rune("one\ntwo\n"))
	e.handleNormal("V")
	e.handleNormal("k")
	e.handleNormal("y")
	if e.Register != "two\nthree\n" {
		t.Fatalf("reverse line selection: %q", e.Register)
	}
}
func TestVimChangeLineAndRepeatChange(t *testing.T) {
	e := NewEditor("vim", "first\nsecond\n")
	e.handleNormal("c")
	e.handleNormal("c")
	e.Handle(key('N', "N"))
	e.Escape()
	if e.Value() != "N\nsecond\n" {
		t.Fatalf("cc merged with next line: %q", e.Value())
	}
	e.UndoEdit()
	if e.Value() != "first\nsecond\n" {
		t.Fatalf("cc undo: %q", e.Value())
	}
	e = NewEditor("vim", "alpha beta gamma")
	e.handleNormal("c")
	e.handleNormal("w")
	e.Handle(key('o', "o"))
	e.Handle(key('n', "n"))
	e.Handle(key('e', "e"))
	e.Handle(key(' ', " "))
	e.Escape()
	if e.Value() != "one beta gamma" {
		t.Fatalf("cw: %q", e.Value())
	}
	e.Pos = len([]rune("one "))
	e.handleNormal(".")
	if e.Value() != "one one gamma" {
		t.Fatalf("dot did not repeat cw insertion: %q", e.Value())
	}
	e.UndoEdit()
	if e.Value() != "one beta gamma" {
		t.Fatalf("repeated change undo: %q", e.Value())
	}
}
func TestVimRepeatVisualEdits(t *testing.T) {
	e := NewEditor("vim", "cat dog sun")
	e.handleNormal("v")
	e.handleNormal("l")
	e.handleNormal("l")
	e.handleNormal("c")
	for _, r := range "fox" {
		e.Handle(key(r, string(r)))
	}
	e.Escape()
	e.Pos = len([]rune("fox "))
	e.handleNormal(".")
	if e.Value() != "fox fox sun" {
		t.Fatalf("visual change repeat: %q", e.Value())
	}
	e.UndoEdit()
	if e.Value() != "fox dog sun" {
		t.Fatalf("repeated visual change undo: %q", e.Value())
	}
	e = NewEditor("vim", "abc def ghi")
	e.handleNormal("v")
	e.handleNormal("l")
	e.handleNormal("d")
	e.Pos = len([]rune("c "))
	e.handleNormal(".")
	if e.Value() != "c f ghi" {
		t.Fatalf("visual delete repeat: %q", e.Value())
	}
}
