package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/store"
)

func (m *Model) panel(title, body string, width, height int, focused bool) string {
	p := m.palette()
	label := "  " + title
	if focused {
		label = "> " + title + " [FOCUS]"
	}
	border := p.Border
	if focused {
		border = p.Accent
	}
	background := p.Base
	if m.Overlay != "" {
		background = p.Surface
	}
	style := m.textStyle(p.Text, background).Width(width).Height(height).Border(lipgloss.RoundedBorder())
	if m.fixedColor() {
		style = style.BorderForeground(lipgloss.Color(border)).BorderBackground(lipgloss.Color(background))
	}
	if !m.colored() {
		body = ansi.Strip(body)
	}
	return style.Render(clipText(label, width-2) + "\n" + body)
}
func clipText(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, max(0, width), "…")
}
func (m *Model) treeView(height, width int) string {
	if len(m.Rows) == 0 {
		return "(no notes)"
	}
	start := 0
	if m.Selected >= height {
		start = m.Selected - height + 1
	}
	end := min(len(m.Rows), start+height)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		r := m.Rows[i]
		prefix := "  "
		if i == m.Selected {
			prefix = "› "
		}
		line := prefix + r.Label
		line = clipText(line, width)
		if i == m.Selected {
			line = m.selectedStyle().Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
func (m *Model) modeLabel() string {
	switch m.Focus {
	case "search":
		return "Search " + m.SearchMode
	case "tree":
		return "Tree NAVIGATION"
	case "editor":
		if m.Editor != nil {
			return "Editor " + strings.ToUpper(m.Flavor) + " " + m.Editor.Mode
		}
		return "Editor EMPTY"
	}
	return m.Focus
}
func helpLines(m *Model) []string {
	entry := m.Focus == "search" && m.SearchMode == "INPUT" || m.Focus == "editor" && m.Editor != nil && (m.Editor.Mode == "INSERT" || m.Editor.Mode == "ENTRY")
	lines := []string{"General actions"}
	for _, b := range bindings {
		if b.Scope == "all" || b.Scope == "navigation" || b.Scope == "command" {
			mark := "  "
			if b.Scope == "all" || b.Scope == "navigation" && !entry {
				mark = "› "
			}
			lines = append(lines, fmt.Sprintf("%s%-18s %s", mark, b.Sequence, b.Description))
		}
	}
	lines = append(lines, "", "Search and tree")
	for _, b := range bindings {
		if b.Scope == "search/command" {
			lines = append(lines, fmt.Sprintf("› %-18s %s", b.Sequence, b.Description))
		}
	}
	lines = append(lines, "  j/k or arrows       Previous/next result", "  h/l                 Collapse/open a tree row", "  gg/G                First/last result", "  Ctrl+F/B/D/U        Page or half-page", "  Enter               Open selected note", "", "Editor modes")
	for _, b := range bindings {
		if b.Scope == "traditional entry" {
			mark := "  "
			if m.Flavor == "traditional" && entry && m.Focus == "editor" {
				mark = "› "
			}
			lines = append(lines, fmt.Sprintf("%s%-18s %s", mark, b.Sequence, b.Description))
		}
	}
	lines = append(lines, "  Vim NORMAL          h/j/k/l w/b/e 0/^/$ gg/G; counts", "  Vim INSERT          i/a/I/A/o/O; Esc returns to NORMAL", "  Vim edits           x dd/dw/d$ cc/cw/c$ yy/yw p/P u Ctrl+R .", "  Vim selection       v/V, motion, then y/d/c", "  Search              / then n/N; Esc cancels prompt", "  Traditional ENTRY   Ctrl+A/E line edges; Ctrl+K/U cut/uncut", "  Traditional COMMAND Esc from ENTRY; i/Enter resumes", "  Text entry          ? types ?; Ctrl+V quotes next key; Tab inserts tab", "  Ctrl+C              Cancel prompt/operator or leave entry", "", "Esc closes help · j/k scroll · Ctrl+F/B page")
	lines = append(lines, "", "Settings")
	for _, b := range settingsBindings {
		lines = append(lines, fmt.Sprintf("  %-18s %s", b.Sequence, b.Description))
	}
	return lines
}
func (m *Model) overlayView() string {
	title, body := "", ""
	switch m.Overlay {
	case "settings":
		return m.settingsView()
	case "choose":
		title = "Choose editor keys"
		markV, markT := "› ", "  "
		if m.Field == 1 {
			markV, markT = "  ", "› "
		}
		body = markV + "Vim (default)\n" + markT + "Traditional (nano-like)\n\n↑/↓ or j/k · Enter select · Esc quit"
	case "help":
		lines := helpLines(m)
		window := max(5, m.Height-10)
		m.HelpOffset = min(max(0, m.HelpOffset), max(0, len(lines)-window))
		end := min(len(lines), m.HelpOffset+window)
		title = fmt.Sprintf("Keyboard shortcuts (%d-%d/%d)", m.HelpOffset+1, end, len(lines))
		body = strings.Join(lines[m.HelpOffset:end], "\n") + "\n\nEsc close · j/k scroll · Ctrl+F/B page"
	case "new":
		title = "New note"
		for i, f := range m.Fields {
			prefix := "  "
			if i == m.Field {
				prefix = "› "
			}
			body += prefix + f.Placeholder + ": " + f.View() + "\n"
		}
		body += "\nTab next field · Enter advance/create · Esc cancel"
	case "tags":
		title = "Edit tags"
		body = "Tags: " + m.Prompt.View() + "\n\nSpace-separated tags · Enter apply · Esc cancel"
	case "confirm-tags":
		title = "Confirm tag changes"
		body = m.Message
	case "find":
		title = "Find in current note"
		body = m.Prompt.View() + "\n\nEnter search · Esc cancel"
	case "command":
		title = "Command"
		body = ":" + m.Prompt.View() + "\n\nnew journal tags find search tree editor w sync refresh q help settings config themes"
	case "conflict":
		title = "Note changed outside Notes"
		body = m.Message + "\n\nr reload disk · c save a recovery draft outside Git · Esc keep editing"
	}
	return m.panel(title, body, max(40, min(m.Width-4, 90)), max(5, min(m.Height-5, strings.Count(body, "\n")+3)), true)
}
func (m *Model) View() tea.View {
	m.refreshStyles()
	if m.Width < 80 || m.Height < 16 {
		v := tea.NewView("Resize terminal to at least 80×16. Your notes remain open.")
		v.AltScreen = true
		return v
	}
	if m.Overlay != "" {
		v := tea.NewView(m.finishView(lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, m.overlayView())))
		v.AltScreen = true
		return v
	}
	search := m.panel("Search all notes", m.Query.View(), m.Width-4, 2, m.Focus == "search")
	leftW := max(20, m.Width/4)
	rightW := max(30, m.Width-leftW-4)
	left := m.panel("Tags / notes", m.treeView(m.Height-8, leftW), leftW, m.Height-8, m.Focus == "tree")
	content := "Open a note from the tree"
	title := "Note editor"
	if m.Active != nil {
		title = m.Active.ID
		if m.Dirty {
			title += " *"
		}
		if m.SyncRunning {
			title += " [READ-ONLY SYNC]"
		}
		content = m.Editor.Area.View()
	}
	right := m.panel(title, content, rightW, m.Height-8, m.Focus == "editor")
	status := m.modeLabel() + " · " + m.Message
	if m.SyncRunning {
		status += " · Syncing · editor read-only"
	}
	marker := m.palette().Success
	if m.SyncFailed || m.SyncBlocked {
		marker = m.palette().Error
	} else if m.SyncPending || m.Dirty {
		marker = m.palette().Warning
	}
	status = m.textStyle(marker, m.palette().Base).Render("●") + " " + status
	footerHint := "Ctrl+G n New · Ctrl+G c Settings · Ctrl+G h Help · Tab Focus"
	if m.Leader {
		status, footerHint = leaderHints(m.Width - 4)
	}
	footer := m.textStyle(m.palette().Text, m.palette().Base).Width(m.Width - 4).Render(clipText(status, m.Width-4) + "\n" + clipText(footerHint, m.Width-4))
	v := tea.NewView(m.finishView(lipgloss.JoinVertical(lipgloss.Left, search, lipgloss.JoinHorizontal(lipgloss.Top, left, right), footer)))
	v.AltScreen = true
	return v
}

func Run(ctx context.Context, c config.Config, s *store.Store, start string, in io.Reader, out io.Writer) error {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	if !inputOK || !outputOK || !term.IsTerminal(input.Fd()) || !term.IsTerminal(output.Fd()) {
		return fmt.Errorf("notes tui requires an interactive terminal")
	}
	m, e := NewModel(c, s, start)
	if e != nil {
		return e
	}
	m.Context = ctx
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out))
	_, e = p.Run()
	return e
}
