package tui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/store"
	"github.com/naterator/notes/internal/syncgit"
)

type tickMsg time.Time
type syncMsg struct {
	err      error
	skipped  bool
	dueAfter time.Duration
}
type searchMsg struct {
	Generation uint64
	Rows       []treeRow
}
type treeRow struct {
	Label, ID, Tag string
	Root           bool
}
type Binding struct {
	Sequence, Action, Scope, Description string
	Commands                             []string
	Priority                             int
}

var bindings = []Binding{
	{"?", "help", "navigation", "Show all shortcuts", []string{"help"}, 0},
	{"ctrl+g h", "help", "all", "Show all shortcuts", nil, 0},
	{"tab", "next-focus", "navigation", "Search → tree → editor", nil, 1},
	{"shift+tab", "previous-focus", "navigation", "Move focus backward", nil, 1},
	{"ctrl+g c", "settings", "all", "Configure Notes", []string{"settings", "config"}, 3},
	{"themes", "themes", "command", "Choose a theme", []string{"themes"}, 3},
	{"ctrl+g n", "new", "all", "Create a note", []string{"new"}, 3},
	{"ctrl+g j", "journal", "all", "Open today's journal", []string{"journal"}, 3},
	{"ctrl+g t", "tags", "all", "Edit active note tags", []string{"tags"}, 3},
	{"ctrl+g f", "find", "all", "Find within the active note", []string{"find"}, 3},
	{"ctrl+g a", "search", "all", "Focus all-notes search", []string{"search"}, 3},
	{"ctrl+g 1", "search", "all", "Focus all-notes search", nil, 3},
	{"ctrl+g b", "tree", "all", "Focus tag tree", []string{"tree"}, 3},
	{"ctrl+g 2", "tree", "all", "Focus tag tree", nil, 3},
	{"ctrl+g e", "editor", "all", "Focus note editor", []string{"editor"}, 3},
	{"ctrl+g 3", "editor", "all", "Focus note editor", nil, 3},
	{"ctrl+s", "save", "all", "Save note", []string{"w", "write"}, 3},
	{"ctrl+g s", "save", "all", "Save note", nil, 3},
	{"ctrl+g y", "sync", "all", "Save and sync", []string{"sync"}, 3},
	{"ctrl+g r", "refresh", "all", "Refresh notes", []string{"refresh"}, 3},
	{"ctrl+q", "quit", "all", "Quit safely", []string{"q", "quit"}, 3},
	{"ctrl+g q", "quit", "all", "Quit safely", nil, 3},
	{"/", "find", "search/command", "Find in current note", nil, 3},
	{":", "command", "navigation", "Run a note command", nil, 3},
	{"ctrl+o", "save", "traditional entry", "Save note", nil, 4},
	{"wq", "save-quit", "command", "Save and quit", []string{"wq"}, 3},
}

func actionForSequence(sequence string) string {
	for _, b := range bindings {
		if b.Sequence == sequence {
			return b.Action
		}
	}
	return ""
}
func actionForCommand(command string) string {
	for _, b := range bindings {
		for _, alias := range b.Commands {
			if alias == command {
				return b.Action
			}
		}
	}
	return ""
}
func leaderHints(width int) (string, string) {
	order := []string{}
	sequences := map[string][]string{}
	for _, b := range bindings {
		if strings.HasPrefix(b.Sequence, "ctrl+g ") {
			if _, seen := sequences[b.Action]; !seen {
				order = append(order, b.Action)
			}
			sequences[b.Action] = append(sequences[b.Action], strings.TrimPrefix(b.Sequence, "ctrl+g "))
		}
	}
	first, second := "Ctrl+G: ", ""
	for _, action := range order {
		part := strings.Join(sequences[action], "/") + " " + action
		if first != "Ctrl+G: " && len([]rune(first))+len([]rune(part))+3 > width {
			if second != "" {
				second += " · "
			}
			second += part
		} else {
			if first != "Ctrl+G: " {
				first += " · "
			}
			first += part
		}
	}
	return first, second
}

type Model struct {
	Context                    context.Context
	Config                     config.Config
	Store                      *store.Store
	Sync                       syncgit.Client
	Width, Height              int
	Focus, SearchMode, Overlay string
	OverlayStack               []overlayFrame
	Settings                   *settingsState
	Flavor                     string
	Query, Prompt              textinput.Model
	QueryVim                   *Editor
	Fields                     []textinput.Model
	Field                      int
	Notes                      []store.Note
	Rows                       []treeRow
	Selected                   int
	SelectedTag                string
	Expanded                   map[string]bool
	SearchGeneration           uint64
	SearchApplied              uint64
	Active                     *store.Note
	Editor                     *Editor
	Saved                      [32]byte
	Dirty                      bool
	LastEdit                   time.Time
	LastSyncAttempt            time.Time
	SyncRunning                bool
	SyncFailed                 bool
	SyncBlocked                bool
	SyncPending                bool
	NextDue                    time.Time
	Leader, Quoted             bool
	NavPrefix                  string
	NavCount                   int
	Message                    string
	NoteSearch                 string
	MatchAt                    int
	PendingRemove              []string
	HelpOffset                 int
}

func NewModel(c config.Config, s *store.Store, start string) (*Model, error) {
	notes, e := s.List()
	if e != nil {
		return nil, e
	}
	q := textinput.New()
	q.Placeholder = "Search all notes"
	q.SetWidth(60)
	q.Focus()
	p := textinput.New()
	p.SetWidth(60)
	timeout, _ := time.ParseDuration(c.Git.Timeout)
	m := &Model{Context: context.Background(), Config: c, Store: s, Sync: syncgit.Client{Store: s, Timeout: timeout}, Width: 100, Height: 30, Focus: "search", SearchMode: "INPUT", Flavor: c.TUI.EditorMode, Query: q, Prompt: p, Notes: notes, Expanded: map[string]bool{"all": true}}
	if c.TUI.EditorMode == "ask" {
		m.Overlay = "choose"
		m.Flavor = "vim"
	}
	m.rebuildRows()
	if start != "" {
		if e = m.open(start); e != nil {
			return nil, e
		}
		m.focus("editor")
	}
	return m, nil
}
func (m *Model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}
func noteInTag(n store.Note, tag string) bool {
	switch tag {
	case "", "all":
		return true
	case "untagged":
		return len(n.Tags) == 0
	default:
		for _, own := range n.Tags {
			if own == tag || strings.HasPrefix(own, tag+"/") {
				return true
			}
		}
		return false
	}
}
func buildRows(notes []store.Note, query, selectedTag string, expanded map[string]bool) []treeRow {
	counts := map[string]int{}
	untagged := 0
	for _, n := range notes {
		if len(n.Tags) == 0 {
			untagged++
		}
		seen := map[string]bool{}
		for _, tag := range n.Tags {
			parts := strings.Split(tag, "/")
			for i := 1; i <= len(parts); i++ {
				parent := strings.Join(parts[:i], "/")
				if !seen[parent] {
					counts[parent]++
					seen[parent] = true
				}
			}
		}
	}
	keys := make([]string, 0, len(counts))
	for tag := range counts {
		keys = append(keys, tag)
	}
	sort.Strings(keys)
	candidates := make([]store.Note, 0, len(notes))
	query = strings.ToLower(query)
	for _, n := range notes {
		if !noteInTag(n, selectedTag) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(n.Title+"\n"+string(n.Content)), query) {
			continue
		}
		candidates = append(candidates, n)
	}
	rows := make([]treeRow, 0, len(notes)+len(keys)+2)
	addRoot := func(tag, label string) {
		if expanded[tag] {
			label = "▾ " + label
		} else {
			label = "▸ " + label
		}
		rows = append(rows, treeRow{Label: label, Tag: tag, Root: true})
		if expanded[tag] {
			for _, n := range candidates {
				if noteInTag(n, tag) {
					rows = append(rows, treeRow{Label: "  " + n.Title, ID: n.ID, Tag: tag})
				}
			}
		}
	}
	addRoot("all", fmt.Sprintf("All notes (%d)", len(notes)))
	addRoot("untagged", fmt.Sprintf("Untagged (%d)", untagged))
	for _, tag := range keys {
		label := fmt.Sprintf("#%s (%d)", tag, counts[tag])
		if strings.Contains(tag, "/") {
			label = "  " + label
		}
		addRoot(tag, label)
	}
	return rows
}
func (m *Model) applyRows(rows []treeRow) {
	var prior treeRow
	if m.Selected >= 0 && m.Selected < len(m.Rows) {
		prior = m.Rows[m.Selected]
	}
	m.Rows = rows
	if len(rows) == 0 {
		m.Selected = 0
		return
	}
	if m.Focus == "search" && m.Query.Value() != "" && prior.Root {
		for i, row := range rows {
			if !row.Root {
				m.Selected = i
				return
			}
		}
	}
	for i, row := range rows {
		if row.ID == prior.ID && row.Tag == prior.Tag && row.Root == prior.Root {
			m.Selected = i
			return
		}
	}
	if m.Focus == "search" {
		for i, row := range rows {
			if !row.Root {
				m.Selected = i
				return
			}
		}
	}
	m.Selected = min(m.Selected, len(rows)-1)
}
func (m *Model) rebuildRows() {
	m.SearchGeneration++
	m.applyRows(buildRows(m.Notes, m.Query.Value(), m.SelectedTag, m.Expanded))
	m.SearchApplied = m.SearchGeneration
}
func (m *Model) scheduleSearch() tea.Cmd {
	m.SearchGeneration++
	generation := m.SearchGeneration
	notes := append([]store.Note(nil), m.Notes...)
	query, tag := m.Query.Value(), m.SelectedTag
	expanded := make(map[string]bool, len(m.Expanded))
	for key, value := range m.Expanded {
		expanded[key] = value
	}
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return searchMsg{Generation: generation, Rows: buildRows(notes, query, tag, expanded)}
	})
}
func (m *Model) refresh() error {
	notes, e := m.Store.List()
	if e != nil {
		return e
	}
	m.Notes = notes
	m.rebuildRows()
	return nil
}
func (m *Model) open(id string) error {
	if m.Dirty {
		if e := m.save(); e != nil {
			return e
		}
	}
	n, e := m.Store.Read(id)
	if e != nil {
		return e
	}
	if len(n.Content) > store.MaxEditable {
		return fmt.Errorf("note exceeds 2 MiB editor limit; use notes edit")
	}
	m.Active = &n
	m.Editor = ptrEditor(NewEditor(m.Flavor, string(n.Content)))
	m.sizeEditor()
	m.focus(m.Focus)
	m.Saved = sha256.Sum256(n.Content)
	m.Dirty = false
	m.Message = "Opened " + n.ID
	return nil
}
func ptrEditor(e Editor) *Editor { return &e }
func (m *Model) markChanged(id string) string {
	if m.Config.Git.AutoSync {
		if ignored, err := m.Sync.Ignored(m.Context, id); err == nil && ignored {
			return id + " is ignored by Git and will not sync"
		}
	}
	m.SyncPending = true
	return ""
}
func (m *Model) save() error {
	if !m.Dirty || m.Active == nil || m.Editor == nil {
		return nil
	}
	b := []byte(m.Editor.Value())
	if len(b) > store.MaxEditable {
		return fmt.Errorf("note exceeds 2 MiB editor limit")
	}
	if sha256.Sum256(b) == m.Saved {
		m.Dirty = false
		return nil
	}
	if e := m.Store.Save(m.Active.ID, m.Saved, b); e != nil {
		m.setOverlay("conflict")
		m.Message = e.Error()
		return e
	}
	warning := m.markChanged(m.Active.ID)
	m.Saved = sha256.Sum256(b)
	m.Dirty = false
	n, e := m.Store.Read(m.Active.ID)
	if e != nil {
		return e
	}
	m.Active = &n
	m.Message = "Saved " + m.Active.ID
	if e := m.refresh(); e != nil {
		return e
	}
	if warning != "" {
		m.Message = warning
	}
	return nil
}
func (m *Model) maybeAutosave(now time.Time) {
	if !m.Dirty || m.hasOverlay("conflict") {
		return
	}
	delay, e := time.ParseDuration(m.Config.TUI.AutosaveDelay)
	if e != nil {
		return
	}
	if now.Sub(m.LastEdit) >= delay {
		if e = m.save(); e != nil {
			m.Message = e.Error()
		}
	}
}
func (m *Model) startSync(force bool) tea.Cmd {
	if m.SyncRunning {
		return nil
	}
	if e := m.save(); e != nil {
		m.Message = e.Error()
		return nil
	}
	hasOrigin, err := m.Sync.HasOrigin(m.Context)
	if err != nil {
		m.Message = "Sync pending: " + err.Error()
		m.NextDue = time.Now().Add(time.Minute)
		return nil
	}
	if !hasOrigin {
		m.SyncPending = false
		m.SyncFailed = false
		m.SyncBlocked = false
		m.NextDue = time.Now().Add(time.Minute)
		m.Message = "Local Git repo ready; add origin to enable sync"
		return nil
	}
	m.SyncRunning = true
	m.LastSyncAttempt = time.Now()
	client, ctx := m.Sync, m.Context
	interval, _ := time.ParseDuration(m.Config.Git.MaxInterval)
	return func() tea.Msg {
		if !force {
			wait, err := client.DueIn(ctx, interval)
			if err != nil {
				return syncMsg{err: err}
			}
			if wait > 0 {
				return syncMsg{skipped: true, dueAfter: wait}
			}
		}
		return syncMsg{err: client.Sync(ctx, "", true)}
	}
}
func (m *Model) saveCopy() error {
	if m.Editor == nil || m.Active == nil {
		return nil
	}
	path, e := m.Store.SaveRecovery(m.Active.ID, []byte(m.Editor.Value()))
	if e != nil {
		return e
	}
	id := m.Active.ID
	m.Dirty = false
	m.closeOverlay()
	if e := m.open(id); e != nil {
		return fmt.Errorf("buffer saved at %s; reload failed: %w", path, e)
	}
	m.Message = "Recovery draft saved outside Git: " + path
	return nil
}
func (m *Model) focus(area string) {
	m.Focus = area
	m.Leader = false
	m.NavPrefix = ""
	m.NavCount = 0
	if area == "search" {
		m.Query.Focus()
	} else {
		m.Query.Blur()
	}
	if m.Editor != nil {
		if area == "editor" {
			m.Editor.Area.Focus()
		} else {
			m.Editor.Area.Blur()
		}
	}
}
func (m *Model) cycle(back bool) error {
	order := []string{"search", "tree", "editor"}
	n := 0
	for i, v := range order {
		if v == m.Focus {
			n = i
		}
	}
	if back {
		n = (n + 2) % 3
	} else {
		n = (n + 1) % 3
	}
	if order[n] == "search" {
		if e := m.save(); e != nil {
			return e
		}
	}
	m.focus(order[n])
	return nil
}

type overlayFrame struct{ Overlay, Focus, SearchMode, EditorMode, Flavor string }

func (m *Model) hasOverlay(name string) bool {
	if m.Overlay == name {
		return true
	}
	for _, f := range m.OverlayStack {
		if f.Overlay == name {
			return true
		}
	}
	return false
}
func (m *Model) setOverlay(name string) {
	if m.Overlay == name {
		return
	}
	frame := overlayFrame{Overlay: m.Overlay, Focus: m.Focus, SearchMode: m.SearchMode, Flavor: m.Flavor}
	if m.Editor != nil {
		frame.EditorMode = m.Editor.Mode
		m.Editor.Prefix = ""
		m.Editor.Count = 0
		m.Editor.PendingCount = 0
	}
	m.OverlayStack = append(m.OverlayStack, frame)
	m.Overlay = name
	m.Query.Blur()
	if m.Editor != nil {
		m.Editor.Area.Blur()
	}
	m.Leader = false
	m.Quoted = false
	m.NavPrefix = ""
	m.NavCount = 0
}
func (m *Model) closeOverlay() {
	if len(m.OverlayStack) == 0 {
		m.Overlay = ""
		m.focus(m.Focus)
		return
	}
	f := m.OverlayStack[len(m.OverlayStack)-1]
	m.OverlayStack = m.OverlayStack[:len(m.OverlayStack)-1]
	m.Overlay = f.Overlay
	m.focus(f.Focus)
	m.SearchMode = f.SearchMode
	if m.Editor != nil && f.Flavor == m.Flavor && f.EditorMode != "" {
		m.Editor.Mode = f.EditorMode
	}
	if m.Overlay != "" {
		m.Query.Blur()
		if m.Editor != nil {
			m.Editor.Area.Blur()
		}
	}
	if m.Overlay == "settings" && m.Settings != nil && m.Settings.CloseOnReturn {
		m.Settings = nil
		m.closeOverlay()
	}
}
func (m *Model) choose(flavor string) {
	m.Flavor = flavor
	m.Overlay = ""
	m.OverlayStack = nil
	if e := config.Set(m.Config.Path, "tui.editor_mode", flavor); e != nil {
		m.Message = "Mode selected for this session; could not save setting: " + e.Error()
	} else {
		m.Config.TUI.EditorMode = flavor
	}
	if m.Active != nil {
		m.Editor = ptrEditor(NewEditor(flavor, m.Editor.Value()))
	}
	if m.Active != nil {
		m.focus("editor")
	} else {
		m.focus("search")
	}
}
func (m *Model) promptFor(name, initial string) {
	m.setOverlay(name)
	m.Prompt.SetValue(initial)
	m.Prompt.Focus()
}
func (m *Model) openNew() {
	m.setOverlay("new")
	m.Fields = make([]textinput.Model, 3)
	for i, placeholder := range []string{"Title", "Category", "Tags (space-separated)"} {
		m.Fields[i] = textinput.New()
		m.Fields[i].Placeholder = placeholder
		m.Fields[i].SetWidth(max(20, m.Width-12))
	}
	m.Fields[1].SetValue(m.Config.DefaultCategory)
	m.Field = 0
	m.Fields[0].Focus()
}
func (m *Model) openTags() {
	if m.Active == nil {
		m.Message = "Open a note first"
		return
	}
	m.promptFor("tags", strings.Join(m.Active.Tags, " "))
}
func (m *Model) find() {
	if m.Active == nil {
		m.Message = "Open a note to search within it"
		return
	}
	m.promptFor("find", "")
}
func (m *Model) match(forward bool) {
	if m.Editor == nil || m.NoteSearch == "" {
		return
	}
	body := strings.ToLower(m.Editor.Value())
	query := strings.ToLower(m.NoteSearch)
	start := m.Editor.Pos
	if forward {
		start++
	}
	if start >= len([]rune(body)) {
		start = 0
	}
	offset := len(string([]rune(body)[:start]))
	at := strings.Index(body[offset:], query)
	if at < 0 {
		at = strings.Index(body, query)
		offset = 0
	}
	if !forward {
		at = strings.LastIndex(body[:offset], query)
		if at < 0 {
			at = strings.LastIndex(body, query)
		}
		offset = 0
	}
	if at >= 0 {
		m.Editor.Pos = len([]rune(body[:offset+at]))
		m.Editor.render()
		m.MatchAt = m.Editor.Pos
		m.Message = fmt.Sprintf("Match at character %d", m.MatchAt+1)
	} else {
		m.Message = "No match"
	}
}
func (m *Model) action(a string) tea.Cmd {
	if m.SyncRunning && (a == "new" || a == "journal" || a == "tags" || a == "refresh" || a == "quit" || a == "save-quit") {
		m.Message = "Sync in progress; wait for it to finish"
		return nil
	}
	switch a {
	case "settings":
		return m.openSettings(false)
	case "themes":
		return m.openSettings(true)
	case "help":
		m.setOverlay("help")
		m.HelpOffset = 0
	case "new":
		if e := m.save(); e != nil {
			m.Message = e.Error()
			return nil
		}
		m.openNew()
	case "journal":
		if e := m.save(); e != nil {
			m.Message = e.Error()
			return nil
		}
		now := time.Now()
		day, _, e := store.JournalDate("", m.Config.Journal.Timezone, now)
		if e != nil {
			m.Message = e.Error()
			return nil
		}
		_, oldErr := m.Store.Read(store.JournalID(day))
		n, e := m.Store.EnsureJournal("", m.Config.Journal.Timezone, now)
		if e != nil {
			m.Message = e.Error()
		} else {
			warning := ""
			if os.IsNotExist(oldErr) {
				warning = m.markChanged(n.ID)
			}
			if e = m.open(n.ID); e != nil {
				m.Message = e.Error()
			} else {
				m.focus("editor")
				if warning != "" {
					m.Message = warning
				}
			}
		}
	case "tags":
		if e := m.save(); e != nil {
			m.Message = e.Error()
			return nil
		}
		m.openTags()
	case "find":
		m.find()
	case "search":
		if e := m.save(); e != nil {
			m.Message = e.Error()
			return nil
		}
		m.focus("search")
		m.SearchMode = "INPUT"
	case "tree":
		m.focus("tree")
	case "editor":
		m.focus("editor")
	case "save":
		if e := m.save(); e != nil {
			m.Message = e.Error()
		}
	case "sync":
		return m.startSync(true)
	case "refresh":
		if e := m.save(); e != nil {
			m.Message = e.Error()
		} else if e = m.refresh(); e != nil {
			m.Message = e.Error()
		}
	case "quit":
		if e := m.save(); e != nil {
			m.Message = e.Error()
		} else {
			return tea.Quit
		}
	case "save-quit":
		if e := m.save(); e != nil {
			m.Message = e.Error()
		} else {
			return tea.Quit
		}
	case "command":
		m.promptFor("command", "")
	}
	return nil
}
