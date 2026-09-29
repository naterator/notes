package tui

import (
	"image/color"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/naterator/notes/internal/theme"
)

func (m *Model) schemeName() string {
	if m.Settings != nil && m.hasOverlay("settings") && m.Settings.Preview != "" {
		return m.Settings.Preview
	}
	return m.Config.Theme
}
func (m *Model) palette() theme.Palette { return theme.Get(m.schemeName()) }
func (m *Model) colored() bool          { return theme.Enabled(m.Config.Color, true) }
func (m *Model) fixedColor() bool       { return m.colored() && m.schemeName() != "terminal" }

// Dark palettes use their lighter surface for the editor canvas. Light
// palettes keep their light base. Search/sidebar cards use the darker shade.
func (m *Model) canvasBackground() string {
	p := m.palette()
	s, _ := theme.Lookup(m.schemeName())
	if s.Kind == "light" {
		return p.Base
	}
	return p.Surface
}
func (m *Model) panelBackground() string {
	p := m.palette()
	s, _ := theme.Lookup(m.schemeName())
	if s.Kind == "light" {
		return p.Surface
	}
	return p.Base
}
func (m *Model) textStyle(fg, bg string) lipgloss.Style {
	s := lipgloss.NewStyle()
	if m.fixedColor() {
		if fg != "" {
			s = s.Foreground(lipgloss.Color(fg))
		}
		if bg != "" {
			s = s.Background(lipgloss.Color(bg))
		}
	}
	return s
}
func (m *Model) selectedStyle() lipgloss.Style {
	p := m.palette()
	if m.schemeName() == "terminal" && m.colored() {
		return lipgloss.NewStyle().Reverse(true)
	}
	return m.textStyle(p.SelectionText, p.Selection)
}
func (m *Model) styleInput(input *textinput.Model, background string) {
	p := m.palette()
	normal := m.textStyle(p.Text, background)
	muted := m.textStyle(p.Muted, background)
	accent := m.textStyle(p.Accent, background)
	state := textinput.StyleState{Text: normal, Placeholder: muted, Suggestion: muted, Prompt: accent}
	var cursor color.Color
	if m.fixedColor() {
		cursor = lipgloss.Color(p.Accent)
	}
	input.SetStyles(textinput.Styles{Focused: state, Blurred: state, Cursor: textinput.CursorStyle{Color: cursor}})
}
func (m *Model) refreshStyles() {
	p := m.palette()
	canvas, panel := m.canvasBackground(), m.panelBackground()
	m.styleInput(&m.Query, panel)
	m.styleInput(&m.Prompt, panel)
	for i := range m.Fields {
		m.styleInput(&m.Fields[i], panel)
	}
	if m.Settings != nil {
		m.styleInput(&m.Settings.Filter, panel)
		m.styleInput(&m.Settings.Input, panel)
	}
	if m.Editor == nil {
		return
	}
	normal := m.textStyle(p.Text, canvas)
	muted := m.textStyle(p.Muted, canvas)
	focused := textarea.StyleState{Base: normal, Text: normal, CursorLine: normal, LineNumber: muted, CursorLineNumber: m.textStyle(p.Accent, canvas), EndOfBuffer: muted, Placeholder: muted, Prompt: muted, Selection: m.selectedStyle()}
	blurred := focused
	blurred.CursorLineNumber = muted
	var cursor color.Color
	if m.fixedColor() {
		cursor = lipgloss.Color(p.Accent)
	}
	m.Editor.Area.SetStyles(textarea.Styles{Focused: focused, Blurred: blurred, Cursor: textarea.CursorStyle{Color: cursor}})
}
