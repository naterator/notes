package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/store"
)

func (m *Model) panel(title, body string, width, height int, focused bool) string {
	background := m.panelBackground()
	content := "\n" + m.paneTitle(title, width-4, focused, background) + "\n\n" + body
	return m.paddedBlock(content, width, height, background)
}

func (m *Model) paneTitle(title string, width int, focused bool, background string) string {
	style := m.textStyle(m.palette().Text, background)
	if m.colored() {
		style = style.Bold(true)
	}
	if focused {
		label := clipText(title, width-lipgloss.Width(" [FOCUS]")) + " [FOCUS]"
		if m.fixedColor() {
			style = style.Foreground(lipgloss.Color(m.palette().Accent))
		}
		return style.Render(label)
	}
	return style.Render(clipText(title, width))
}

func (m *Model) block(content string, width, height int, background string) string {
	if !m.colored() {
		content = ansi.Strip(content)
	} else if m.fixedColor() {
		// Nested widgets reset SGR after their text. Restore this block's
		// colors so subsequent spaces do not expose the terminal background.
		base := ansi.Style{}.ForegroundColor(lipgloss.Color(m.palette().Text)).BackgroundColor(lipgloss.Color(background)).String()
		content = strings.NewReplacer(ansi.ResetStyle, ansi.ResetStyle+base, "\x1b[0m", "\x1b[0m"+base).Replace(content)
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = clipText(lines[i], width)
	}
	return m.textStyle(m.palette().Text, background).Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m *Model) paddedBlock(content string, width, height int, background string) string {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = "  " + clipText(lines[i], width-4) + "  "
	}
	return m.block(strings.Join(lines, "\n"), width, height, background)
}

func inputView(input *textinput.Model, width int) string {
	// The widget's width excludes its prompt and cursor cell.
	width = max(1, width-lipgloss.Width(input.Prompt)-1)
	if input.Width() != width {
		pos := input.Position()
		input.SetWidth(width)
		// SetWidth alone leaves the widget's horizontal viewport unchanged.
		input.CursorStart()
		input.SetCursor(pos)
	}
	return input.View()
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
			line = m.selectedStyle().Width(width).Render(line)
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
	width := max(40, min(m.Width-4, 90))
	inner := width - 4
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
			label := prefix + f.Placeholder + ": "
			body += label + inputView(&m.Fields[i], inner-lipgloss.Width(label)) + "\n"
		}
		body += "\nTab next field · Enter advance/create · Esc cancel"
	case "tags":
		title = "Edit tags"
		body = "Tags: " + inputView(&m.Prompt, inner-6) + "\n\nSpace-separated tags · Enter apply · Esc cancel"
	case "confirm-tags":
		title = "Confirm tag changes"
		body = m.Message
	case "find":
		title = "Find in current note"
		body = inputView(&m.Prompt, inner) + "\n\nEnter search · Esc cancel"
	case "command":
		title = "Command"
		body = ":" + inputView(&m.Prompt, inner-1) + "\n\nnew journal tags find search tree editor w sync refresh q help settings config themes"
	case "conflict":
		title = "Note changed outside Notes"
		body = m.Message + "\n\nr reload disk · c save a recovery draft outside Git · Esc keep editing"
	}
	return m.panel(title, body, width, min(m.Height-4, strings.Count(body, "\n")+5), true)
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
	l := layoutFor(m.Width, m.Height)
	p := m.palette()
	canvas, panel := m.canvasBackground(), m.panelBackground()
	searchContent := "\n" + m.paneTitle("Search all notes", l.width-4, m.Focus == "search", panel) + "\n" + m.Query.View()
	search := m.paddedBlock(searchContent, l.width, 4, panel)
	sidebarContent := "\n" + m.paneTitle("Tags / notes", l.sidebarWidth-4, m.Focus == "tree", panel) + "\n\n" + m.treeView(l.bodyHeight-4, l.sidebarWidth-4)
	sidebar := m.paddedBlock(sidebarContent, l.sidebarWidth, l.bodyHeight, panel)
	content := m.textStyle(p.Muted, canvas).Render("Open a note from the sidebar\nCtrl+G n creates a new note")
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
	editorContent := "\n" + m.paneTitle(title, l.editorWidth-4, m.Focus == "editor", canvas) + "\n\n" + content
	editor := m.paddedBlock(editorContent, l.editorWidth, l.bodyHeight, canvas)
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
	status = m.textStyle(marker, canvas).Render("●") + " " + status
	footerHint := "Ctrl+G n New · Ctrl+G c Settings · Ctrl+G h Help · Tab Focus"
	if m.Leader {
		status, footerHint = leaderHints(l.width - 4)
	}
	footer := m.paddedBlock(clipText(status, l.width-4)+"\n"+m.textStyle(p.Muted, canvas).Render(clipText(footerHint, l.width-4)), l.width, 2, canvas)
	body := lipgloss.JoinHorizontal(lipgloss.Top, editor, m.block("", 2, l.bodyHeight, canvas), sidebar)
	content = lipgloss.JoinVertical(lipgloss.Left, search, m.block("", l.width, 1, canvas), body, footer)
	v := tea.NewView(m.paddedBlock("\n"+content+"\n", m.Width, m.Height, canvas))
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
