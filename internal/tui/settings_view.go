package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/theme"
)

func (m *Model) settingsView() string {
	s := m.Settings
	width := max(40, min(m.Width-4, 92))
	inner := width - 2
	budget := m.Height - 7 // Title and borders leave space around the centered dialog.
	title := "Settings"
	lines := []string{"Config: " + m.Config.Path}
	footer := "↑/↓ Select · Enter Edit · Tab Focus · Ctrl+R Reset · Ctrl+G h Help"
	if s.Loading {
		lines = append(lines, "Loading settings…", "Esc close")
	} else if s.View == "error" || s.View == "stale" {
		if s.View == "stale" {
			title = "Setting changed outside Notes"
			lines = append(lines, "Attempted: "+s.Attempted)
		}
		lines = append(lines, m.textStyle(m.palette().Error, m.palette().Surface).Render("!")+" "+s.Error, "r/Enter Reload · Esc Cancel · Ctrl+G h Help")
	} else if s.View == "render" {
		title = "Effective configuration"
		content := m.effectiveLines()
		window := max(1, budget-2)
		s.Offset = min(max(0, s.Offset), max(0, len(content)-window))
		lines = append(lines, content[s.Offset:min(len(content), s.Offset+window)]...)
		lines = append(lines, "j/k or arrows Scroll · Ctrl+F/B Page · Esc Back · Ctrl+G h Help")
	} else if s.View == "value" || s.View == "reset" {
		d := settingFor(s.Key)
		title = d.Label
		lines = append(lines, m.settingDetail(s.Key)...)
		if s.View == "reset" {
			title = "Reset " + d.Label
			lines = append(lines, "Remove the saved value of "+s.Key+"?", m.resetDetail())
		} else {
			lines = append(lines, "Value [FOCUS]: "+s.Input.View())
		}
		if s.Error != "" {
			lines = append(lines, m.textStyle(m.palette().Error, m.palette().Surface).Render("!")+" Error: "+s.Error)
		}
		if s.Saving {
			lines = append(lines, "Saving…")
		}
		lines = append(lines, "Enter/Ctrl+S Confirm · Esc Cancel · Ctrl+G h Help")
	} else {
		if s.View == "picker" {
			title = settingFor(s.Key).Label
		}
		focus := "Filter [FOCUS]: "
		if s.Focus != "filter" {
			focus = "List [FOCUS] · Filter: "
		}
		lines = append(lines, focus+s.Filter.View())
		rows := s.rows()
		row, selected := s.selected()
		details := []string{"No matching settings", "Change or clear the filter"}
		if s.View == "picker" {
			details = []string{"Current saved preference: " + s.Initial, settingFor(s.Key).Timing}
			if s.Key == "theme" {
				name := m.Config.Theme
				if s.Preview != "" {
					name = s.Preview
				}
				scheme, _ := theme.Lookup(name)
				details = []string{"Preview: " + scheme.Name + " · " + scheme.Kind + " · temporary", "Preference: " + s.Initial + " · Active: " + m.Config.Theme, m.themeSample()}
				if source := s.Snapshot.Sources[s.Key]; source != "default" && source != "file" {
					details[0] += " · overridden by " + source
				}
			}
			footer = "↑/↓ Select · Tab Focus · Enter/Ctrl+S Save · Esc Cancel · Ctrl+G h Help"
		} else if selected {
			details = m.settingDetail(row.Key)
		}
		window := max(1, budget-len(lines)-len(details)-1)
		if s.Error != "" || s.Saving {
			window--
		}
		window = max(1, window)
		display, currentLine, group := []string{}, 0, ""
		for i, r := range rows {
			if r.Group != "" && r.Group != group {
				display = append(display, m.textStyle(m.palette().Accent, m.palette().Surface).Bold(true).Render(r.Group))
				group = r.Group
			}
			mark := "  "
			if i == s.Selected {
				mark = "› "
				currentLine = len(display)
			}
			label := r.Label
			if s.View == "picker" {
				value, _ := config.Get(s.Snapshot.Effective, s.Key)
				if r.Key == config.ValueText(value) {
					label += " [CURRENT]"
				}
				if r.Detail != "" {
					label += " · " + r.Detail
				}
			} else {
				value := r.Value
				if r.Key == "theme" {
					if scheme, ok := theme.Lookup(value); ok {
						value = scheme.Name
					}
				}
				if r.Key == "editor" && value == "[]" {
					value = "Use environment/TUI"
				}
				label = lipgloss.NewStyle().Width(min(29, inner/3)).Render(label) + " " + value
			}
			line := clipText(mark+label, inner)
			if i == s.Selected {
				line = m.selectedStyle().Width(inner).Render(line)
			}
			display = append(display, line)
		}
		if len(display) == 0 {
			display = []string{"(no results)"}
		}
		start := max(0, currentLine-window+1)
		lines = append(lines, display[start:min(len(display), start+window)]...)
		lines = append(lines, details...)
		if s.Saving {
			lines = append(lines, "Saving…")
		} else if s.Error != "" {
			lines = append(lines, m.textStyle(m.palette().Error, m.palette().Surface).Render("!")+" Error: "+s.Error)
		}
		lines = append(lines, footer)
	}
	// Clip individual lines by display cells; never split a color escape.
	for i := range lines {
		lines[i] = clipText(lines[i], inner)
	}
	if len(lines) > budget {
		lines = append(lines[:budget-1], lines[len(lines)-1])
	}
	hint := " · Esc back"
	if s.View == "home" || s.View == "error" || s.Direct {
		hint = " · Esc close"
	}
	return m.panel(title+hint, strings.Join(lines, "\n"), width, min(m.Height-4, len(lines)+3), true)
}
func (m *Model) themeSample() string {
	p := m.palette()
	bg := p.Surface
	focus := m.textStyle(p.Accent, bg).Render("[FOCUS]")
	selected := m.selectedStyle().Render("› Sample note")
	markers := m.textStyle(p.Success, bg).Render("●") + " Saved  " + m.textStyle(p.Warning, bg).Render("●") + " Pending  " + m.textStyle(p.Error, bg).Render("●") + " Error"
	return fmt.Sprintf("%s %s  %s", focus, selected, markers)
}

func (m *Model) finishView(content string) string {
	if m.fixedColor() {
		return m.textStyle(m.palette().Text, m.palette().Base).Width(m.Width).Height(m.Height).Render(content)
	}
	return content
}
