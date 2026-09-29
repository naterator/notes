package tui

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/notes/internal/store"
	"github.com/naterator/notes/internal/syncgit"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case settingsLoadedMsg:
		return m, m.settingsLoaded(v)
	case settingsSavedMsg:
		return m, m.settingsSaved(v)
	case tea.WindowSizeMsg:
		m.Width, m.Height = v.Width, v.Height
		m.Query.SetWidth(max(20, v.Width-8))
		m.Prompt.SetWidth(max(20, v.Width-12))
		if m.Editor != nil {
			m.Editor.SetSize(max(20, v.Width-v.Width/4-8), max(5, v.Height-7))
		}
		if m.Settings != nil {
			m.Settings.Input.SetWidth(max(10, min(v.Width-12, 84)))
			m.Settings.Filter.SetWidth(max(10, min(v.Width-26, 70)))
		}
		return m, nil
	case tickMsg:
		now := time.Time(v)
		m.maybeAutosave(now)
		var cmd tea.Cmd
		if m.Config.Git.AutoSync && m.Overlay == "" && !m.SyncRunning && !m.SyncBlocked && !now.Before(m.NextDue) {
			interval, _ := time.ParseDuration(m.Config.Git.MaxInterval)
			if m.SyncFailed {
				interval = time.Minute
			}
			if m.LastSyncAttempt.IsZero() || now.Sub(m.LastSyncAttempt) >= interval || m.SyncPending && !m.SyncFailed {
				cmd = m.startSync(!m.SyncPending)
			}
		}
		return m, tea.Batch(cmd, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }))
	case syncMsg:
		m.SyncRunning = false
		if v.skipped {
			m.NextDue = time.Now().Add(v.dueAfter)
			return m, nil
		}
		m.SyncFailed = v.err != nil
		m.SyncBlocked = errors.Is(v.err, syncgit.ErrConflict)
		if v.err != nil {
			m.SyncPending = true
			m.NextDue = time.Now().Add(time.Minute)
			m.Message = "Sync pending: " + v.err.Error()
		} else {
			m.SyncPending = false
			m.NextDue = time.Time{}
			m.Message = "Synced with origin"
			if e := m.refresh(); e != nil {
				m.Message = e.Error()
			}
			if m.Active != nil && !m.Dirty {
				if n, e := m.Store.Read(m.Active.ID); e == nil && string(n.Content) != m.Editor.Value() {
					m.Active = &n
					m.Editor = ptrEditor(NewEditor(m.Flavor, string(n.Content)))
					m.Editor.SetSize(max(20, m.Width-m.Width/4-8), max(5, m.Height-7))
					m.focus(m.Focus)
					m.Saved = sha256Sum(n.Content)
				}
			}
		}
		return m, nil
	case searchMsg:
		if v.Generation == m.SearchGeneration {
			m.applyRows(v.Rows)
			m.SearchApplied = v.Generation
		}
		return m, nil
	case tea.PasteMsg:
		if m.Overlay == "settings" {
			return m, m.settingsPaste(v)
		}
		if m.SyncRunning && m.Overlay == "" && m.Focus == "editor" {
			m.Message = "Sync in progress; editor is read-only"
			return m, nil
		}
		if m.Overlay != "" {
			if m.Overlay == "new" && len(m.Fields) > 0 {
				field, cmd := m.Fields[m.Field].Update(v)
				m.Fields[m.Field] = field
				return m, cmd
			} else if m.Overlay == "tags" || m.Overlay == "find" || m.Overlay == "command" {
				prompt, cmd := m.Prompt.Update(v)
				m.Prompt = prompt
				return m, cmd
			}
			return m, nil
		}
		if m.Focus == "editor" && m.Editor != nil && (m.Editor.Mode == "INSERT" || m.Editor.Mode == "ENTRY") {
			m.Editor.insert(v.Content)
			m.Dirty = true
			m.LastEdit = time.Now()
		} else if m.Focus == "search" && m.SearchMode == "INPUT" {
			query, cmd := m.Query.Update(v)
			m.Query = query
			return m, tea.Batch(cmd, m.scheduleSearch())
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(v)
	}
	return m, nil
}
func sha256Sum(b []byte) [32]byte { return sha256.Sum256(b) }
func (m *Model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := keyName(k)
	if m.SyncRunning && m.Overlay == "" && m.Focus == "editor" && !m.Leader {
		entry := m.Editor != nil && (m.Editor.Mode == "INSERT" || m.Editor.Mode == "ENTRY")
		allowed := key == "esc" || key == "ctrl+c" || key == "ctrl+g" || key == "ctrl+q" || key == "ctrl+s" || !entry && (key == "tab" || key == "shift+tab" || key == "?" || key == ":")
		if !allowed {
			m.Quoted = false
			m.Message = "Sync in progress; editor is read-only"
			return m, nil
		}
	}
	// A pending literal quote bypasses all ordinary key bindings.
	if m.Quoted {
		m.Quoted = false
		if key == "esc" || key == "ctrl+c" {
			return m, nil
		}
		if m.Focus == "search" && m.Overlay == "" {
			q, _ := m.Query.Update(k)
			m.Query = q
			return m, m.scheduleSearch()
		} else if m.Focus == "editor" && m.Editor != nil && m.Overlay == "" {
			m.Editor.insert(k.Text)
			m.Dirty = true
			m.LastEdit = time.Now()
		}
		return m, nil
	}
	if key == "ctrl+g" {
		m.Leader = true
		return m, nil
	}
	if m.Leader {
		m.Leader = false
		if key == "esc" || key == "ctrl+c" {
			return m, nil
		}
		sequence := "ctrl+g " + key
		if actionForSequence(sequence) == "help" {
			return m, m.action("help")
		}
		if m.Overlay != "" {
			m.Message = "Close the dialog first"
			if m.Settings != nil && m.hasOverlay("settings") {
				m.Settings.Error = m.Message
			}
			return m, nil
		}
		if action := actionForSequence(sequence); action != "" {
			return m, m.action(action)
		}
		m.Message = "Unknown Ctrl+G shortcut"
		return m, nil
	}
	if m.Overlay != "" {
		return m.handleOverlay(k)
	}
	if key == "ctrl+c" {
		m.Quoted = false
		m.NavPrefix = ""
		m.NavCount = 0
		if m.Focus == "editor" && m.Editor != nil {
			m.Editor.Escape()
		} else if m.Focus == "search" {
			m.SearchMode = "NORMAL"
			m.QueryVim = ptrEditor(NewEditor("vim", m.Query.Value()))
			m.QueryVim.Pos = m.Query.Position()
		}
		return m, nil
	}
	entry := m.Focus == "search" && m.SearchMode == "INPUT" || m.Focus == "editor" && m.Editor != nil && (m.Editor.Mode == "INSERT" || m.Editor.Mode == "ENTRY")
	if key == "?" && !entry {
		return m, m.action(actionForSequence("?"))
	}
	if key == "ctrl+v" && entry {
		m.Quoted = true
		return m, nil
	}
	if key == "esc" {
		m.NavPrefix = ""
		m.NavCount = 0
		if m.Focus == "editor" && m.Editor != nil {
			m.Editor.Escape()
		} else if m.Focus == "search" {
			m.SearchMode = "NORMAL"
			m.QueryVim = ptrEditor(NewEditor("vim", m.Query.Value()))
			m.QueryVim.Pos = m.Query.Position()
		} else {
			m.SelectedTag = "all"
			m.rebuildRows()
		}
		return m, nil
	}
	if key == "tab" || key == "shift+tab" {
		if m.Focus == "editor" && m.Editor != nil && (m.Editor.Mode == "INSERT" || m.Editor.Mode == "ENTRY") {
			if key == "tab" {
				m.Editor.insert("\t")
				m.Dirty = true
				m.LastEdit = time.Now()
			} else {
				m.Message = "Esc, then Shift+Tab to move focus"
			}
			return m, nil
		}
		if e := m.cycle(key == "shift+tab"); e != nil {
			m.Message = e.Error()
		}
		return m, nil
	}
	if key == "ctrl+s" {
		return m, m.action(actionForSequence(key))
	}
	if key == "ctrl+o" && m.Focus == "editor" && m.Editor != nil && m.Flavor == "traditional" {
		return m, m.action(actionForSequence(key))
	}
	if key == "ctrl+q" {
		return m, m.action(actionForSequence(key))
	}
	if key == "/" && m.Focus == "search" {
		return m, m.action(actionForSequence(key))
	}
	if !entry && key == ":" {
		return m, m.action(actionForSequence(key))
	}
	switch m.Focus {
	case "search":
		if m.SearchMode == "INPUT" {
			if key == "up" || key == "down" {
				m.stepResult(key == "down", 1)
				return m, nil
			}
			if key == "enter" {
				if m.SearchApplied != m.SearchGeneration {
					m.rebuildRows()
				}
				m.openSelected()
				return m, nil
			}
			q, cmd := m.Query.Update(k)
			m.Query = q
			return m, tea.Batch(cmd, m.scheduleSearch())
		}
		if m.listKey(key) {
			return m, nil
		}
		if key == "enter" {
			if m.SearchApplied != m.SearchGeneration {
				m.rebuildRows()
			}
			m.openSelected()
		} else {
			if m.QueryVim == nil {
				m.QueryVim = ptrEditor(NewEditor("vim", m.Query.Value()))
			}
			if m.NavCount > 0 {
				m.QueryVim.Count = m.NavCount
				m.NavCount = 0
			}
			m.QueryVim.Handle(k)
			m.Query.SetValue(m.QueryVim.Value())
			m.Query.SetCursor(m.QueryVim.Pos)
			if m.QueryVim.Mode == "INSERT" {
				m.SearchMode = "INPUT"
			}
			return m, m.scheduleSearch()
		}
	case "tree":
		if m.listKey(key) {
			return m, nil
		}
		if key == "enter" || key == "l" || key == "right" {
			m.openSelected()
		} else if key == "h" || key == "left" {
			m.collapseSelected()
		}
	case "editor":
		if m.Editor == nil {
			m.Message = "Open a note from the tree"
			return m, nil
		}
		if m.SyncRunning {
			m.Message = "Sync in progress; editor input resumes shortly"
			return m, nil
		}
		if m.Editor.Mode == "NORMAL" || m.Editor.Mode == "COMMAND" {
			if key == "/" {
				m.find()
				return m, nil
			}
			if key == "n" {
				m.match(true)
				return m, nil
			}
			if key == "N" {
				m.match(false)
				return m, nil
			}
		}
		before := m.Editor.Value()
		m.Editor.Handle(k)
		if before != m.Editor.Value() {
			m.Dirty = true
			m.LastEdit = time.Now()
		}
	}
	return m, nil
}
func (m *Model) stepResult(down bool, count int) {
	for count > 0 {
		next := m.Selected
		for {
			if down {
				next++
			} else {
				next--
			}
			if next < 0 || next >= len(m.Rows) {
				return
			}
			if !m.Rows[next].Root {
				m.Selected = next
				break
			}
		}
		count--
	}
}
func (m *Model) jumpResult(last bool) {
	if last {
		for i := len(m.Rows) - 1; i >= 0; i-- {
			if !m.Rows[i].Root {
				m.Selected = i
				return
			}
		}
		return
	}
	for i, row := range m.Rows {
		if !row.Root {
			m.Selected = i
			return
		}
	}
}
func (m *Model) listKey(key string) bool {
	if m.NavPrefix == "g" {
		m.NavPrefix = ""
		if key == "g" {
			if m.Focus == "search" {
				m.jumpResult(false)
			} else {
				m.Selected = 0
			}
			m.NavCount = 0
			return true
		}
	}
	if key == "g" {
		m.NavPrefix = "g"
		return true
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' || m.NavCount > 0 && key == "0" {
		m.NavCount = min(9999, m.NavCount*10+int(key[0]-'0'))
		return true
	}
	hadCount := m.NavCount > 0
	count := max(1, m.NavCount)
	m.NavCount = 0
	switch key {
	case "j", "down", "ctrl+f", "ctrl+d":
		if key == "ctrl+f" {
			count *= max(1, m.Height-10)
		} else if key == "ctrl+d" {
			count *= max(1, (m.Height-10)/2)
		}
		if m.Focus == "search" {
			m.stepResult(true, count)
		} else {
			m.Selected = min(max(0, len(m.Rows)-1), m.Selected+count)
		}
		return true
	case "k", "up", "ctrl+b", "ctrl+u":
		if key == "ctrl+b" {
			count *= max(1, m.Height-10)
		} else if key == "ctrl+u" {
			count *= max(1, (m.Height-10)/2)
		}
		if m.Focus == "search" {
			m.stepResult(false, count)
		} else {
			m.Selected = max(0, m.Selected-count)
		}
		return true
	case "G":
		if m.Focus == "search" {
			m.jumpResult(true)
		} else {
			m.Selected = max(0, len(m.Rows)-1)
		}
		return true
	}
	if m.Focus == "search" && hadCount {
		m.NavCount = count
	}
	return false
}
func (m *Model) openSelected() {
	if len(m.Rows) == 0 {
		return
	}
	r := m.Rows[m.Selected]
	if r.Root {
		m.SelectedTag = r.Tag
		m.Expanded[r.Tag] = true
		m.rebuildRows()
		return
	}
	if e := m.open(r.ID); e != nil {
		m.Message = e.Error()
	} else {
		m.focus("editor")
	}
}
func (m *Model) collapseSelected() {
	if len(m.Rows) == 0 {
		return
	}
	r := m.Rows[m.Selected]
	if !r.Root {
		for i, row := range m.Rows {
			if row.Root && row.Tag == r.Tag {
				m.Selected = i
				return
			}
		}
	}
	if m.Expanded[r.Tag] {
		m.Expanded[r.Tag] = false
		m.rebuildRows()
	} else {
		m.SelectedTag = "all"
		m.rebuildRows()
	}
}
func (m *Model) handleOverlay(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.Overlay == "settings" {
		return m, m.settingsKey(k)
	}
	key := keyName(k)
	if key == "esc" || key == "ctrl+c" {
		if m.Overlay == "choose" {
			return m, tea.Quit
		}
		m.closeOverlay()
		return m, nil
	}
	if key == "?" && m.Overlay != "help" && m.Overlay != "new" && m.Overlay != "tags" && m.Overlay != "find" && m.Overlay != "command" {
		return m, m.action("help")
	}
	switch m.Overlay {
	case "choose":
		if key == "j" || key == "down" {
			m.Field = 1
		} else if key == "k" || key == "up" {
			m.Field = 0
		} else if key == "enter" {
			if m.Field == 1 {
				m.choose("traditional")
			} else {
				m.choose("vim")
			}
		}
	case "help":
		if key == "enter" || key == "?" {
			m.closeOverlay()
		} else if key == "j" || key == "down" {
			m.HelpOffset++
		} else if key == "k" || key == "up" {
			m.HelpOffset = max(0, m.HelpOffset-1)
		} else if key == "ctrl+f" {
			m.HelpOffset += max(1, m.Height-10)
		} else if key == "ctrl+b" {
			m.HelpOffset = max(0, m.HelpOffset-max(1, m.Height-10))
		} else if key == "G" {
			m.HelpOffset = len(helpLines(m))
		} else if key == "g" {
			m.HelpOffset = 0
		}
	case "conflict":
		if key == "r" {
			n, e := m.Store.Read(m.Active.ID)
			if e != nil {
				m.Message = e.Error()
			} else {
				m.Active = &n
				m.Editor = ptrEditor(NewEditor(m.Flavor, string(n.Content)))
				m.Saved = sha256Sum(n.Content)
				m.Dirty = false
				m.closeOverlay()
			}
		} else if key == "c" {
			if e := m.saveCopy(); e != nil {
				m.Message = e.Error()
			}
		}
	case "new":
		if key == "tab" || key == "shift+tab" {
			m.Fields[m.Field].Blur()
			if key == "tab" {
				m.Field = (m.Field + 1) % 3
			} else {
				m.Field = (m.Field + 2) % 3
			}
			m.Fields[m.Field].Focus()
			return m, nil
		}
		if key == "enter" {
			if m.Field < 2 {
				m.Fields[m.Field].Blur()
				m.Field++
				m.Fields[m.Field].Focus()
				return m, nil
			}
			title := m.Fields[0].Value()
			cat := m.Fields[1].Value()
			tags := strings.Fields(m.Fields[2].Value())
			n, e := m.Store.Create(store.NewOptions{Title: title, Category: cat, Tags: tags})
			if e != nil {
				m.Message = e.Error()
				return m, nil
			}
			warning := m.markChanged(n.ID)
			m.closeOverlay()
			if e = m.refresh(); e != nil {
				m.Message = e.Error()
			}
			if e = m.open(n.ID); e != nil {
				m.Message = e.Error()
			} else {
				m.focus("editor")
				if warning != "" {
					m.Message = warning
				}
			}
			return m, nil
		}
		field, cmd := m.Fields[m.Field].Update(k)
		m.Fields[m.Field] = field
		return m, cmd
	case "tags", "confirm-tags":
		if m.Overlay == "confirm-tags" {
			if key == "enter" {
				if m.applyTags() {
					m.closeOverlay()
				}
			}
			return m, nil
		}
		if key == "enter" {
			desired := strings.Fields(m.Prompt.Value())
			want := map[string]bool{}
			for _, t := range desired {
				want[strings.ToLower(strings.TrimPrefix(t, "#"))] = true
			}
			m.PendingRemove = nil
			for _, t := range m.Active.Tags {
				if !want[t] {
					m.PendingRemove = append(m.PendingRemove, t)
				}
			}
			if len(m.PendingRemove) > 0 {
				m.Overlay = "confirm-tags"
				m.Message = tagRemovalPreview(m.Active.Content, m.PendingRemove)
				return m, nil
			}
			if m.applyTags() {
				m.closeOverlay()
			}
			return m, nil
		}
		p, cmd := m.Prompt.Update(k)
		m.Prompt = p
		return m, cmd
	case "find":
		if key == "enter" {
			m.NoteSearch = m.Prompt.Value()
			m.closeOverlay()
			m.focus("editor")
			if m.Editor != nil {
				m.Editor.Escape()
			}
			m.match(true)
			return m, nil
		}
		p, cmd := m.Prompt.Update(k)
		m.Prompt = p
		return m, cmd
	case "command":
		if key == "tab" {
			prefix := strings.TrimSpace(strings.TrimPrefix(m.Prompt.Value(), ":"))
			matches := []string{}
			for _, b := range bindings {
				for _, alias := range b.Commands {
					if strings.HasPrefix(alias, prefix) && !slices.Contains(matches, alias) {
						matches = append(matches, alias)
					}
				}
			}
			if len(matches) == 1 {
				m.Prompt.SetValue(matches[0])
			} else if len(matches) > 1 {
				m.Message = "Commands: " + strings.Join(matches, ", ")
			}
			return m, nil
		}
		if key == "enter" {
			name := strings.TrimSpace(strings.TrimPrefix(m.Prompt.Value(), ":"))
			m.closeOverlay()
			if a := actionForCommand(name); a != "" {
				return m, m.action(a)
			}
			m.Message = "Unknown command: " + name
			return m, nil
		}
		p, cmd := m.Prompt.Update(k)
		m.Prompt = p
		return m, cmd
	}
	return m, nil
}
func tagRemovalPreview(content []byte, removed []string) string {
	want := map[string]bool{}
	for _, tag := range removed {
		want[tag] = true
	}
	lines := []string{fmt.Sprintf("Remove %s?", strings.Join(removed, ", "))}
	occurrences := 0
	for _, span := range store.TagSpans(content) {
		if !want[span.Tag] {
			continue
		}
		occurrences++
		if occurrences > 5 {
			continue
		}
		lineNo := bytes.Count(content[:span.Start], []byte("\n")) + 1
		start := bytes.LastIndexByte(content[:span.Start], '\n') + 1
		end := len(content)
		if next := bytes.IndexByte(content[span.End:], '\n'); next >= 0 {
			end = span.End + next
		}
		preview := strings.TrimSpace(string(content[start:end]))
		kind := "prose: remove #"
		if strings.HasPrefix(preview, "Tags:") {
			kind = "managed: remove token"
		}
		lines = append(lines, fmt.Sprintf("line %d (%s): %s", lineNo, kind, clipText(preview, 55)))
	}
	if occurrences > 5 {
		lines = append(lines, fmt.Sprintf("...and %d more occurrences", occurrences-5))
	}
	lines = append(lines, "Enter applies · Esc keeps the note unchanged")
	return strings.Join(lines, "\n")
}
func remapEditorPosition(before, after []rune, pos int) int {
	sharedStart := 0
	for sharedStart < len(before) && sharedStart < len(after) && before[sharedStart] == after[sharedStart] {
		sharedStart++
	}
	sharedEnd := 0
	for sharedEnd < len(before)-sharedStart && sharedEnd < len(after)-sharedStart && before[len(before)-1-sharedEnd] == after[len(after)-1-sharedEnd] {
		sharedEnd++
	}
	if pos < sharedStart {
		return pos
	}
	if pos >= len(before)-sharedEnd {
		return min(len(after), pos+len(after)-len(before))
	}
	return sharedStart
}
func editorAfterTagEdit(previous *Editor, content string, width, height int) *Editor {
	next := NewEditor(previous.Flavor, content)
	next.Mode = previous.Mode
	next.Pos = remapEditorPosition(previous.Text, next.Text, previous.Pos)
	next.VisualStart = remapEditorPosition(previous.Text, next.Text, previous.VisualStart)
	next.SelectionStart = remapEditorPosition(previous.Text, next.Text, previous.SelectionStart)
	next.Selecting = previous.Selecting
	next.Register, next.RegisterLine, next.CutBuffer = previous.Register, previous.RegisterLine, previous.CutBuffer
	next.Undo = append(next.Undo, previous.Undo...)
	if (previous.Mode == "INSERT" || previous.Mode == "ENTRY") && previous.insertBase.Text != previous.Value() {
		next.Undo = append(next.Undo, previous.insertBase)
	}
	next.Undo = append(next.Undo, previous.snapshot())
	if len(next.Undo) > 200 {
		next.Undo = next.Undo[len(next.Undo)-200:]
	}
	if next.Mode == "INSERT" || next.Mode == "ENTRY" {
		next.insertBase = next.snapshot()
		next.insertStart = next.insertBase
	}
	next.SetSize(width, height)
	return &next
}
func (m *Model) applyTags() bool {
	if m.Active == nil {
		return false
	}
	wanted := strings.Fields(m.Prompt.Value())
	b := append([]byte{}, m.Active.Content...)
	var e error
	if len(m.PendingRemove) > 0 {
		b, e = store.RemoveTags(b, m.PendingRemove)
		if e != nil {
			m.Message = e.Error()
			return false
		}
	}
	if len(wanted) > 0 {
		b, e = store.AddTags(b, wanted)
		if e != nil {
			m.Message = e.Error()
			return false
		}
	}
	changed := !bytes.Equal(b, m.Active.Content)
	if e = m.Store.Save(m.Active.ID, m.Saved, b); e != nil {
		m.Message = e.Error()
		return false
	}
	if !changed {
		return true
	}
	warning := ""
	warning = m.markChanged(m.Active.ID)
	n, e := m.Store.Read(m.Active.ID)
	if e != nil {
		m.Message = e.Error()
		return false
	}
	m.Active = &n
	m.Editor = editorAfterTagEdit(m.Editor, string(n.Content), max(20, m.Width-m.Width/4-8), max(5, m.Height-7))
	m.Saved = sha256Sum(n.Content)
	m.Dirty = false
	if e = m.refresh(); e != nil {
		m.Message = e.Error()
		return false
	}
	if warning != "" {
		m.Message = warning
	}
	return true
}
