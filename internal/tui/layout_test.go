package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/notes/internal/theme"
)

func TestBorderlessLayoutAndFooter(t *testing.T) {
	for _, mode := range []string{"vim", "traditional"} {
		m := modelFixture(t, mode)
		if err := m.open("question.md"); err != nil {
			t.Fatal(err)
		}
		for _, scheme := range theme.Names() {
			for _, size := range [][2]int{{80, 16}, {96, 28}, {100, 30}, {127, 40}} {
				t.Run(fmt.Sprintf("%s/%s/%dx%d", mode, scheme, size[0], size[1]), func(t *testing.T) {
					m.Config.Theme, m.Config.Color = scheme, "always"
					m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					for _, focus := range []string{"search", "tree", "editor"} {
						m.focus(focus)
						v := m.View().Content
						plain := ansi.Strip(v)
						if lipgloss.Width(v) != size[0] || lipgloss.Height(v) != size[1] {
							t.Fatalf("screen overflows or leaves unfilled cells: %dx%d", lipgloss.Width(v), lipgloss.Height(v))
						}
						if strings.ContainsAny(plain, "╭╮╰╯│─") {
							t.Fatal("box borders returned")
						}
						lines := strings.Split(plain, "\n")
						if !strings.Contains(lines[2], "Search all notes") {
							t.Fatal("search is not at the top")
						}
						found := false
						for _, line := range lines {
							a, b := strings.Index(line, "question.md"), strings.Index(line, "Tags / notes")
							if a >= 0 && b > a {
								found = true
								if !strings.Contains(line, "[FOCUS]") && focus != "search" {
									t.Fatal("pane focus marker missing")
								}
							}
						}
						if !found {
							t.Fatal("editor and right-hand sidebar headings are not aligned")
						}
						if !strings.Contains(lines[size[1]-2], "Ctrl+G n New") || !strings.Contains(lines[size[1]-3], m.modeLabel()) {
							t.Fatal("editor pushed status or shortcuts below the terminal")
						}
					}
					m.Config.Color = "never"
					if strings.Contains(m.View().Content, "\x1b[") {
						t.Fatal("monochrome layout contains style escapes")
					}
				})
			}
		}
	}
}

func TestResizeKeepsEditingStateAndLastLine(t *testing.T) {
	m := modelFixture(t, "vim")
	if err := m.open("question.md"); err != nil {
		t.Fatal(err)
	}
	m.focus("editor")
	m.Update(key('i', "i"))
	m.Editor.insert(strings.Repeat("line\n", 240) + "LAST_LINE")
	m.Dirty = true
	value, pos, history, mode := m.Editor.Value(), m.Editor.Pos, len(m.Editor.Undo), m.Editor.Mode
	for _, size := range [][2]int{{80, 16}, {127, 40}, {100, 30}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		v := ansi.Strip(m.View().Content)
		if !strings.Contains(v, "LAST_LINE") {
			t.Fatalf("cursor line disappeared after resize: pos=%d area row=%d area chars=%d\n%s", m.Editor.Pos, m.Editor.Area.Line(), len(m.Editor.Area.Value()), v)
		}
		if m.Editor.Value() != value || m.Editor.Pos != pos || len(m.Editor.Undo) != history || m.Editor.Mode != mode || !m.Dirty || m.Focus != "editor" {
			t.Fatal("resize changed editing state")
		}
	}
}

func TestBorderlessDialogsFit(t *testing.T) {
	m := modelFixture(t, "vim")
	for _, size := range [][2]int{{80, 16}, {100, 30}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, overlay := range []string{"help", "new", "choose", "tags", "command", "find"} {
			if overlay == "new" {
				m.openNew()
			} else {
				m.setOverlay(overlay)
			}
			v := m.View().Content
			if lipgloss.Width(v) > size[0] || lipgloss.Height(v) > size[1] || strings.ContainsAny(ansi.Strip(v), "╭╮╰╯│─") {
				t.Fatalf("dialog %s is not borderless or does not fit", overlay)
			}
			m.closeOverlay()
		}
	}
}

func TestDialogInputsFitBesideTheirLabels(t *testing.T) {
	m := modelFixture(t, "vim")
	for _, size := range [][2]int{{80, 16}, {100, 30}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.openNew()
		plain := ansi.Strip(m.View().Content)
		if strings.Contains(plain, "…") {
			t.Fatal("empty dialog fields overflow their card")
		}
		for i := range m.Fields {
			m.Fields[i].SetValue(strings.Repeat("long-value-", 15) + "TAIL")
			m.Fields[i].CursorEnd()
			plain = ansi.Strip(m.View().Content)
			if !strings.Contains(plain, "TAIL") {
				t.Fatalf("field %d clips the cursor end of its value", i)
			}
			m.Fields[i].SetValue("")
		}
		m.closeOverlay()
	}
}

func TestNestedWidgetRestoresCanvasBackground(t *testing.T) {
	m := modelFixture(t, "vim")
	m.Config.Color = "always"
	canvas, panel := m.canvasBackground(), m.panelBackground()
	child := m.textStyle(m.palette().Accent, panel).Render("Child")
	v := m.block(child+" after", 20, 1, canvas)
	parent := ansi.Style{}.ForegroundColor(lipgloss.Color(m.palette().Text)).BackgroundColor(lipgloss.Color(canvas)).String()
	if !strings.Contains(v, "Child"+ansi.ResetStyle+parent+" after") {
		t.Fatal("nested style reset leaves subsequent cells on the terminal background")
	}
}
