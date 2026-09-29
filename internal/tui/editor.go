package tui

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

type editorSnapshot struct {
	Text string
	Pos  int
}
type Editor struct {
	Area                textarea.Model
	Text                []rune
	Pos                 int
	Flavor, Mode        string
	Prefix              string
	Count               int
	PendingCount        int
	Register            string
	RegisterLine        bool
	CutBuffer           string
	VisualStart         int
	SelectionStart      int
	Selecting           bool
	Undo, Redo          []editorSnapshot
	insertBase          editorSnapshot
	insertStart         editorSnapshot
	pendingChangeMotion string
	pendingChangeCount  int
	pendingChangeLine   bool
	repeatChangeMotion  string
	repeatChangeCount   int
	repeatChangeLine    bool
	repeatChangeText    string
	lastEdit            string
}

func NewEditor(flavor, content string) Editor {
	a := textarea.New()
	a.ShowLineNumbers = true
	a.Prompt = ""
	a.SetWidth(60)
	a.SetHeight(15)
	e := Editor{Area: a, Text: []rune(content), Flavor: flavor, Mode: "NORMAL"}
	if flavor == "traditional" {
		e.Mode = "ENTRY"
	}
	e.render()
	if flavor == "traditional" {
		e.insertBase = e.snapshot()
		e.insertStart = e.insertBase
	}
	return e
}
func (e *Editor) Value() string            { return string(e.Text) }
func (e *Editor) snapshot() editorSnapshot { return editorSnapshot{e.Value(), e.Pos} }
func (e *Editor) pushUndo(s editorSnapshot) {
	if s.Text != e.Value() {
		e.Undo = append(e.Undo, s)
		if len(e.Undo) > 200 {
			e.Undo = e.Undo[len(e.Undo)-200:]
		}
		e.Redo = nil
	}
}
func (e *Editor) replaceText(s string, pos int) {
	e.Text = []rune(s)
	e.Selecting = false
	if pos < 0 {
		pos = 0
	}
	if pos > len(e.Text) {
		pos = len(e.Text)
	}
	e.Pos = pos
	e.render()
}
func (e *Editor) SetSize(w, h int) { e.Area.SetWidth(w); e.Area.SetHeight(h); e.render() }
func (e *Editor) selectionRange() (int, int, bool) {
	if e.Flavor == "traditional" && e.Selecting && e.Mode == "ENTRY" {
		a, b := e.SelectionStart, e.Pos
		if a > b {
			a, b = b, a
		}
		return a, b, a != b
	}
	if e.Mode != "VISUAL" && e.Mode != "VISUAL_LINE" {
		return 0, 0, false
	}
	a, b := e.VisualStart, e.Pos
	if a > b {
		a, b = b, a
	}
	if e.Mode == "VISUAL_LINE" {
		return e.lineStart(a), e.nextLine(b), true
	}
	return a, min(len(e.Text), b+1), true
}
func (e *Editor) render() {
	// Bubbles sanitizes tab input. Keep the original runes in Text, expanding tabs
	// only for the display model. Visual brackets are display-only selection
	// markers and remain visible even when this pane loses focus.
	var b strings.Builder
	displayRow, displayCol := 0, 0
	targetRow, targetCol := 0, 0
	selectA, selectB, selected := e.selectionRange()
	for i, r := range e.Text {
		if selected && i == selectA {
			b.WriteRune('⟦')
			displayCol++
		}
		if selected && i == selectB {
			b.WriteRune('⟧')
			displayCol++
		}
		if i == e.Pos {
			targetRow, targetCol = displayRow, displayCol
		}
		if r == '\t' {
			n := 4 - displayCol%4
			b.WriteString(strings.Repeat(" ", n))
			displayCol += n
		} else {
			b.WriteRune(r)
			if r == '\n' {
				displayRow++
				displayCol = 0
			} else {
				displayCol++
			}
		}
	}
	if selected && selectA == len(e.Text) {
		b.WriteRune('⟦')
		displayCol++
	}
	if selected && selectB == len(e.Text) {
		b.WriteRune('⟧')
		displayCol++
	}
	if e.Pos == len(e.Text) {
		targetRow, targetCol = displayRow, displayCol
	}
	e.Area.SetValue(b.String())
	e.Area.MoveToBegin()
	for i := 0; i < targetRow; i++ {
		e.Area.CursorDown()
	}
	e.Area.SetCursorColumn(targetCol)
}
func (e *Editor) beginInsert() {
	e.beginInsertFrom(e.snapshot())
}
func (e *Editor) beginInsertFrom(base editorSnapshot) {
	e.insertBase = base
	e.insertStart = e.snapshot()
	e.Mode = "INSERT"
	if e.Flavor == "traditional" {
		e.Mode = "ENTRY"
	}
}
func (e *Editor) Escape() {
	e.Selecting = false
	if e.Mode == "INSERT" || e.Mode == "ENTRY" {
		before := []rune(e.insertStart.Text)
		after := e.Text
		at := e.insertStart.Pos
		recordable := false
		if at <= len(before) && at <= len(after) && string(before[:at]) == string(after[:at]) {
			suffix := before[at:]
			if len(after) >= at+len(suffix) && string(after[len(after)-len(suffix):]) == string(suffix) {
				recordable = true
				inserted := string(after[at : len(after)-len(suffix)])
				if e.pendingChangeMotion != "" {
					e.repeatChangeMotion = e.pendingChangeMotion
					e.repeatChangeCount = e.pendingChangeCount
					e.repeatChangeLine = e.pendingChangeLine
					e.repeatChangeText = inserted
					e.lastEdit = "change"
				} else {
					e.lastEdit = "insert:" + inserted
				}
			}
		}
		if !recordable && e.insertBase.Text != e.Value() {
			e.lastEdit = ""
		}
		e.pendingChangeMotion = ""
		e.pendingChangeCount = 0
		e.pendingChangeLine = false
		e.pushUndo(e.insertBase)
		e.Mode = "NORMAL"
		if e.Flavor == "traditional" {
			e.Mode = "COMMAND"
		}
	} else if e.Mode == "VISUAL" || e.Mode == "VISUAL_LINE" {
		e.Mode = "NORMAL"
	}
	e.Prefix = ""
	e.Count = 0
	e.PendingCount = 0
	e.render()
}
func (e *Editor) insert(s string) {
	if e.Flavor == "traditional" && e.Selecting {
		if a, b, ok := e.selectionRange(); ok {
			e.Text = append(e.Text[:a], e.Text[b:]...)
			e.Pos = a
		}
		e.Selecting = false
	}
	r := []rune(s)
	e.Text = append(append(append([]rune{}, e.Text[:e.Pos]...), r...), e.Text[e.Pos:]...)
	e.Pos += len(r)
	e.render()
}
func (e *Editor) insertWithUndo(s string) {
	before := e.snapshot()
	e.insert(s)
	e.pushUndo(before)
	e.lastEdit = "insert:" + s
}
func (e *Editor) backspace() {
	if e.Flavor == "traditional" && e.Selecting {
		if a, b, ok := e.selectionRange(); ok {
			e.deleteRange(a, b)
			return
		}
		e.Selecting = false
	}
	if e.Pos > 0 {
		e.Text = append(e.Text[:e.Pos-1], e.Text[e.Pos:]...)
		e.Pos--
		e.render()
	}
}
func (e *Editor) deleteRange(a, b int) {
	e.Selecting = false
	if a < 0 {
		a = 0
	}
	if b > len(e.Text) {
		b = len(e.Text)
	}
	if b < a {
		a, b = b, a
	}
	e.Text = append(e.Text[:a], e.Text[b:]...)
	e.Pos = a
	e.render()
}
func (e *Editor) lineStart(p int) int {
	for p > 0 && e.Text[p-1] != '\n' {
		p--
	}
	return p
}
func (e *Editor) lineEnd(p int) int {
	for p < len(e.Text) && e.Text[p] != '\n' {
		p++
	}
	return p
}
func (e *Editor) nextLine(p int) int {
	end := e.lineEnd(p)
	if end >= len(e.Text) {
		return len(e.Text)
	}
	return end + 1
}
func (e *Editor) prevLine(p int) int {
	start := e.lineStart(p)
	if start == 0 {
		return 0
	}
	return e.lineStart(start - 1)
}
func (e *Editor) nextWord(p int) int {
	if p >= len(e.Text) {
		return p
	}
	space := unicode.IsSpace(e.Text[p])
	for p < len(e.Text) && unicode.IsSpace(e.Text[p]) == space {
		p++
	}
	for p < len(e.Text) && unicode.IsSpace(e.Text[p]) {
		p++
	}
	return p
}
func (e *Editor) prevWord(p int) int {
	if p <= 0 {
		return 0
	}
	p--
	for p > 0 && unicode.IsSpace(e.Text[p]) {
		p--
	}
	for p > 0 && !unicode.IsSpace(e.Text[p-1]) {
		p--
	}
	return p
}
func (e *Editor) endWord(p int) int {
	for p < len(e.Text) && unicode.IsSpace(e.Text[p]) {
		p++
	}
	for p+1 < len(e.Text) && !unicode.IsSpace(e.Text[p+1]) {
		p++
	}
	return p
}
func (e *Editor) move(key string, count int) {
	if count < 1 {
		count = 1
	}
	for i := 0; i < count; i++ {
		switch key {
		case "h", "left":
			if e.Pos > 0 {
				e.Pos--
			}
		case "l", "right":
			if e.Pos < len(e.Text) {
				e.Pos++
			}
		case "j", "down":
			col := e.Pos - e.lineStart(e.Pos)
			n := e.nextLine(e.Pos)
			e.Pos = n + min(col, e.lineEnd(n)-n)
		case "k", "up":
			col := e.Pos - e.lineStart(e.Pos)
			n := e.prevLine(e.Pos)
			e.Pos = n + min(col, e.lineEnd(n)-n)
		case "w":
			e.Pos = e.nextWord(e.Pos)
		case "b":
			e.Pos = e.prevWord(e.Pos)
		case "e":
			e.Pos = e.endWord(e.Pos)
		case "0", "^":
			e.Pos = e.lineStart(e.Pos)
		case "$":
			e.Pos = e.lineEnd(e.Pos)
		case "G":
			e.Pos = len(e.Text)
		case "ctrl+f":
			for j := 0; j < 15; j++ {
				e.Pos = e.nextLine(e.Pos)
			}
		case "ctrl+b":
			for j := 0; j < 15; j++ {
				e.Pos = e.prevLine(e.Pos)
			}
		case "ctrl+d":
			for j := 0; j < 7; j++ {
				e.Pos = e.nextLine(e.Pos)
			}
		case "ctrl+u":
			for j := 0; j < 7; j++ {
				e.Pos = e.prevLine(e.Pos)
			}
		}
	}
	e.render()
}
func (e *Editor) UndoEdit() {
	if len(e.Undo) == 0 {
		return
	}
	s := e.Undo[len(e.Undo)-1]
	e.Undo = e.Undo[:len(e.Undo)-1]
	e.Redo = append(e.Redo, e.snapshot())
	e.replaceText(s.Text, s.Pos)
}
func (e *Editor) RedoEdit() {
	if len(e.Redo) == 0 {
		return
	}
	s := e.Redo[len(e.Redo)-1]
	e.Redo = e.Redo[:len(e.Redo)-1]
	e.Undo = append(e.Undo, e.snapshot())
	e.replaceText(s.Text, s.Pos)
}
func (e *Editor) operation(op, motion string, count int) {
	before := e.snapshot()
	a, b := e.Pos, e.Pos
	if motion == op && (op == "d" || op == "c" || op == "y") {
		a = e.lineStart(e.Pos)
		b = e.nextLine(e.Pos)
		for i := 1; i < count; i++ {
			b = e.nextLine(b)
		}
	} else {
		e.move(motion, count)
		b = e.Pos
		e.Pos = before.Pos
		if motion == "e" && b < len(e.Text) {
			b++
		}
		if motion == "$" {
			b = e.lineEnd(a)
		}
	}
	if a > b {
		a, b = b, a
	}
	if a == b {
		if op == "c" {
			e.pendingChangeMotion = motion
			e.pendingChangeCount = count
			e.beginInsertFrom(before)
		}
		return
	}
	e.Register = string(e.Text[a:b])
	e.RegisterLine = motion == op
	if op == "d" || op == "c" {
		e.deleteRange(a, b)
		e.lastEdit = op + motion
		if op == "c" {
			if e.RegisterLine {
				e.insert("\n")
				e.Pos = a
				e.render()
			}
			e.pendingChangeMotion = motion
			e.pendingChangeCount = count
			e.beginInsertFrom(before)
		} else {
			e.pushUndo(before)
		}
	}
}
func (e *Editor) repeatSelectionRange(count int, linewise bool) (int, int) {
	if !linewise {
		return e.Pos, min(len(e.Text), e.Pos+count)
	}
	a := e.lineStart(e.Pos)
	b := a
	for i := 0; i < count; i++ {
		b = e.nextLine(b)
	}
	return a, b
}
func (e *Editor) handleNormal(k string) bool {
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' || e.Count > 0 && len(k) == 1 && k[0] >= '0' && k[0] <= '9' {
		e.Count = e.Count*10 + int(k[0]-'0')
		return false
	}
	hadCount := e.Count > 0
	count := e.Count
	if count == 0 {
		count = 1
	}
	e.Count = 0
	if e.Prefix != "" {
		prefix := e.Prefix
		e.Prefix = ""
		if prefix == "g" {
			if k == "g" {
				e.Pos = 0
				for i := 1; i < max(1, e.PendingCount); i++ {
					e.Pos = e.nextLine(e.Pos)
				}
				e.render()
			}
			e.PendingCount = 0
			return false
		}
		if prefix == "d" || prefix == "c" || prefix == "y" {
			e.operation(prefix, k, count*max(1, e.PendingCount))
			e.PendingCount = 0
			return true
		}
		return false
	}
	if e.Mode == "VISUAL" || e.Mode == "VISUAL_LINE" {
		if k == "y" || k == "d" || k == "c" {
			a, b, _ := e.selectionRange()
			linewise := e.Mode == "VISUAL_LINE"
			length := b - a
			if linewise {
				length = 0
				for p := a; p < b; p = e.nextLine(p) {
					length++
				}
			}
			e.Register = string(e.Text[a:b])
			e.RegisterLine = linewise
			before := e.snapshot()
			e.Mode = "NORMAL"
			if k != "y" {
				e.deleteRange(a, b)
				if k == "d" {
					e.repeatChangeCount = length
					e.repeatChangeLine = linewise
					e.lastEdit = "visual-delete"
					e.pushUndo(before)
				}
			}
			if k == "c" {
				if linewise {
					e.insert("\n")
					e.Pos = a
				}
				e.pendingChangeMotion = "visual"
				e.pendingChangeCount = length
				e.pendingChangeLine = linewise
				e.beginInsertFrom(before)
			}
			e.render()
			return k != "y"
		}
	}
	switch k {
	case "g", "d", "c", "y":
		e.Prefix = k
		e.PendingCount = count
	case "h", "j", "k", "l", "left", "right", "up", "down", "w", "b", "e", "0", "^", "$", "ctrl+f", "ctrl+b", "ctrl+d", "ctrl+u":
		e.move(k, count)
	case "G":
		if hadCount {
			e.Pos = 0
			for i := 1; i < count; i++ {
				e.Pos = e.nextLine(e.Pos)
			}
			e.render()
		} else {
			e.move("G", 1)
		}
	case "i":
		e.beginInsert()
	case "a":
		if e.Pos < len(e.Text) {
			e.Pos++
		}
		e.beginInsert()
	case "I":
		e.Pos = e.lineStart(e.Pos)
		e.beginInsert()
	case "A":
		e.Pos = e.lineEnd(e.Pos)
		e.beginInsert()
	case "o":
		before := e.snapshot()
		e.Pos = e.lineEnd(e.Pos)
		e.insert("\n")
		e.beginInsertFrom(before)
	case "O":
		before := e.snapshot()
		e.Pos = e.lineStart(e.Pos)
		e.insert("\n")
		e.Pos--
		e.beginInsertFrom(before)
	case "x":
		before := e.snapshot()
		if e.Pos < len(e.Text) {
			e.deleteRange(e.Pos, e.Pos+count)
			e.pushUndo(before)
			e.lastEdit = "x"
		}
	case "p", "P":
		if e.Register != "" {
			before := e.snapshot()
			if e.RegisterLine {
				if k == "p" {
					e.Pos = e.nextLine(e.Pos)
					if e.Pos == len(e.Text) && e.Pos > 0 && e.Text[e.Pos-1] != '\n' {
						e.insert("\n")
					}
				} else {
					e.Pos = e.lineStart(e.Pos)
				}
			} else if k == "p" && e.Pos < len(e.Text) {
				e.Pos++
			}
			e.insert(e.Register)
			e.pushUndo(before)
			e.lastEdit = k
		}
	case "u":
		e.UndoEdit()
	case "ctrl+r":
		e.RedoEdit()
	case "v":
		e.VisualStart = e.Pos
		e.Mode = "VISUAL"
		e.render()
	case "V":
		e.VisualStart = e.Pos
		e.Mode = "VISUAL_LINE"
		e.render()
	case ".":
		if e.lastEdit != "" {
			last := e.lastEdit
			if last == "change" {
				motion, n, inserted := e.repeatChangeMotion, e.repeatChangeCount, e.repeatChangeText
				if motion == "visual" {
					before := e.snapshot()
					a, b := e.repeatSelectionRange(n, e.repeatChangeLine)
					e.deleteRange(a, b)
					if e.repeatChangeLine {
						e.insert("\n")
						e.Pos = a
					}
					e.pendingChangeMotion = "visual"
					e.pendingChangeCount = n
					e.pendingChangeLine = e.repeatChangeLine
					e.beginInsertFrom(before)
				} else {
					e.operation("c", motion, n)
				}
				if e.Mode == "INSERT" || e.Mode == "ENTRY" {
					e.insert(inserted)
					e.Escape()
				}
			} else if last == "visual-delete" {
				before := e.snapshot()
				a, b := e.repeatSelectionRange(e.repeatChangeCount, e.repeatChangeLine)
				if a < b {
					e.Register = string(e.Text[a:b])
					e.RegisterLine = e.repeatChangeLine
					e.deleteRange(a, b)
					e.pushUndo(before)
				}
			} else if strings.HasPrefix(last, "insert:") {
				e.insertWithUndo(strings.TrimPrefix(last, "insert:"))
			} else if len(last) == 2 && strings.ContainsRune("dcy", rune(last[0])) {
				e.handleNormal(last[:1])
				e.handleNormal(last[1:])
			} else {
				e.handleNormal(last)
			}
		}
	}
	return false
}
func (e *Editor) Handle(k tea.KeyPressMsg) bool {
	key := keyName(k)
	if key == "esc" {
		e.Escape()
		return false
	}
	if e.Mode == "INSERT" || e.Mode == "ENTRY" {
		if e.Flavor == "traditional" {
			motion := ""
			switch key {
			case "shift+left", "shift+right", "shift+up", "shift+down":
				motion = strings.TrimPrefix(key, "shift+")
			case "shift+home":
				motion = "0"
			case "shift+end":
				motion = "$"
			}
			if motion != "" {
				if !e.Selecting {
					e.SelectionStart = e.Pos
					e.Selecting = true
				}
				e.move(motion, 1)
				return false
			}
			if key == "left" || key == "right" || key == "up" || key == "down" || key == "home" || key == "end" || key == "ctrl+a" || key == "ctrl+e" {
				e.Selecting = false
			}
		}
		switch key {
		case "tab":
			e.insert("\t")
			return true
		case "enter":
			e.insert("\n")
			return true
		case "backspace":
			e.backspace()
			return true
		case "delete":
			if e.Flavor == "traditional" && e.Selecting {
				if a, b, ok := e.selectionRange(); ok {
					e.deleteRange(a, b)
				}
			} else if e.Pos < len(e.Text) {
				e.deleteRange(e.Pos, e.Pos+1)
			}
			return true
		case "home":
			e.move("0", 1)
			return false
		case "end":
			e.move("$", 1)
			return false
		}
		if e.Flavor == "traditional" {
			switch key {
			case "ctrl+a":
				e.move("0", 1)
				return false
			case "ctrl+e":
				e.move("$", 1)
				return false
			case "ctrl+o":
				return false
			case "ctrl+k":
				start, end := e.lineStart(e.Pos), e.nextLine(e.Pos)
				e.CutBuffer = string(e.Text[start:end])
				e.deleteRange(start, end)
				return true
			case "ctrl+u":
				if e.CutBuffer != "" {
					e.insert(e.CutBuffer)
				}
				return true
			}
		}
		if len(k.Text) > 0 {
			e.insert(k.Text)
			return true
		}
		if key == "left" || key == "right" || key == "up" || key == "down" {
			e.move(key, 1)
		}
		return false
	}
	if e.Flavor == "traditional" && e.Mode == "COMMAND" {
		if key == "i" || key == "enter" {
			e.beginInsert()
			return false
		}
	}
	return e.handleNormal(key)
}

// SetFlavor changes keys without replacing the buffer or its undo history.
func (e *Editor) SetFlavor(flavor string) {
	if e.Flavor == flavor {
		return
	}
	e.Escape()
	e.Flavor = flavor
	e.Mode = "NORMAL"
	if flavor == "traditional" {
		e.Mode = "COMMAND"
	}
	e.lastEdit = ""
	e.pendingChangeMotion = ""
	e.render()
}
