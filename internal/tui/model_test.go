package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/store"
	"github.com/naterator/notes/internal/syncgit"
)

func modelFixture(t *testing.T, mode string) *Model {
	t.Helper()
	base := t.TempDir()
	c := config.Defaults(filepath.Join(base, "config.toml"))
	c.Repo = filepath.Join(base, "repo")
	c.StateDir = filepath.Join(base, "state")
	c.Git.AutoSync = false
	c.TUI.EditorMode = mode
	s := store.New(c.Repo, c.StateDir)
	if _, e := s.Create(store.NewOptions{Title: "Question?", Path: "question.md"}); e != nil {
		t.Fatal(e)
	}
	m, e := NewModel(c, s, "")
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestTUIRequiresActualTerminal(t *testing.T) {
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := Run(context.Background(), config.Config{}, nil, "", input, output); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("TUI accepted character devices without a TTY: %v", err)
	}
}

func TestLocalRepoWithoutOriginShowsSetupHint(t *testing.T) {
	m := modelFixture(t, "vim")
	if err := m.Sync.EnsureRepo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cmd := m.startSync(false); cmd != nil || m.SyncRunning || !strings.Contains(m.Message, "add origin to enable sync") {
		t.Fatalf("unconfigured sync state: command %v, running %t, message %q", cmd, m.SyncRunning, m.Message)
	}
	if strings.Contains(m.Message, "Sync pending") || strings.Contains(m.Message, "not a Git working tree") {
		t.Fatalf("new local repo shown as failed sync: %q", m.Message)
	}
}

func TestLargeNoteUsesExternalEditor(t *testing.T) {
	m := modelFixture(t, "vim")
	large := strings.Repeat(strings.Repeat("a", 1023)+"\n", store.MaxEditable/1024+1)
	if err := os.WriteFile(filepath.Join(m.Store.Repo, "question.md"), []byte(large), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewModel(m.Config, m.Store, "question.md"); err == nil || !strings.Contains(err.Error(), "notes edit") {
		t.Fatalf("large note entered TUI editor: %v", err)
	}
}
func press(m *Model, code rune, text string, mod tea.KeyMod) {
	m.Update(tea.KeyPressMsg{Code: code, Text: text, Mod: mod})
}
func TestFocusHelpAndLiteralEntry(t *testing.T) {
	m := modelFixture(t, "vim")
	if m.Focus != "search" || m.SearchMode != "INPUT" {
		t.Fatalf("startup: %s %s", m.Focus, m.SearchMode)
	}
	if !strings.Contains(m.View().Content, "[FOCUS]") {
		t.Fatal("no visible focus")
	}
	press(m, '?', "?", 0)
	if m.Overlay != "" || m.Query.Value() != "?" {
		t.Fatalf("? in entry: overlay %q query %q", m.Overlay, m.Query.Value())
	}
	press(m, tea.KeyEsc, "", 0)
	press(m, '?', "?", 0)
	if m.Overlay != "help" {
		t.Fatalf("? in navigation: %q", m.Overlay)
	}
	press(m, tea.KeyEsc, "", 0)
	press(m, tea.KeyTab, "", 0)
	if m.Focus != "tree" {
		t.Fatal(m.Focus)
	}
	press(m, tea.KeyTab, "", 0)
	if m.Focus != "editor" {
		t.Fatal(m.Focus)
	}
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	press(m, 'i', "i", 0)
	press(m, tea.KeyTab, "", 0)
	if m.Focus != "editor" || !strings.HasPrefix(m.Editor.Value(), "\t") {
		t.Fatalf("tab in insert: %q, %s", m.Editor.Value(), m.Focus)
	}
	press(m, tea.KeyEsc, "", 0)
	press(m, tea.KeyTab, "", 0)
	if m.Focus != "search" {
		t.Fatal(m.Focus)
	}
	press(m, '/', "/", 0)
	if m.Overlay != "find" {
		t.Fatalf("slash search scope: %q", m.Overlay)
	}
}
func TestEditorRightBorderAlignsWithSearch(t *testing.T) {
	m := modelFixture(t, "vim")
	m.Config.Color = "never"
	if err := m.open("question.md"); err != nil {
		t.Fatal(err)
	}
	m.focus("editor")
	for _, width := range []int{80, 100, 127} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		view := m.View().Content
		borderColumn := func(label string) int {
			t.Helper()
			for _, line := range strings.Split(view, "\n") {
				if !strings.Contains(line, label) {
					continue
				}
				border := strings.LastIndex(line, "│")
				if border >= 0 {
					return lipgloss.Width(line[:border+len("│")])
				}
			}
			t.Fatalf("missing %q border at width %d", label, width)
			return 0
		}
		search := borderColumn("Search all notes")
		editor := borderColumn("question.md [FOCUS]")
		if editor != search {
			t.Fatalf("width %d: search border at %d, editor border at %d", width, search, editor)
		}
	}
}
func TestSpecificNoteStartsInEditor(t *testing.T) {
	for _, mode := range []string{"vim", "traditional", "ask"} {
		t.Run(mode, func(t *testing.T) {
			fixture := modelFixture(t, mode)
			m, err := NewModel(fixture.Config, fixture.Store, "question.md")
			if err != nil {
				t.Fatal(err)
			}
			if m.Focus != "editor" || m.Active == nil || m.Active.ID != "question.md" {
				t.Fatalf("note startup: focus=%q active=%v", m.Focus, m.Active)
			}
			if mode == "ask" {
				if m.Overlay != "choose" {
					t.Fatalf("missing mode choice: %q", m.Overlay)
				}
				press(m, tea.KeyEnter, "", 0)
				if m.Overlay != "" || m.Focus != "editor" {
					t.Fatalf("focus after mode choice: overlay=%q focus=%q", m.Overlay, m.Focus)
				}
			}
		})
	}
}
func TestFirstRunChoicePersists(t *testing.T) {
	m := modelFixture(t, "ask")
	if m.Overlay != "choose" || m.Flavor != "vim" {
		t.Fatalf("choose: %q %q", m.Overlay, m.Flavor)
	}
	press(m, tea.KeyEnter, "", 0)
	if m.Overlay != "" || m.Focus != "search" {
		t.Fatalf("after choice: %q %q", m.Overlay, m.Focus)
	}
	c, e := config.Load(m.Config.Path)
	if e != nil {
		t.Fatal(e)
	}
	if c.TUI.EditorMode != "vim" {
		t.Fatal(c.TUI.EditorMode)
	}
	if _, e = os.Stat(m.Config.Path); e != nil {
		t.Fatal(e)
	}
}
func TestBindingRegistryHasNoCollisions(t *testing.T) {
	sequences := map[string]bool{}
	commands := map[string]bool{}
	for _, b := range bindings {
		if sequences[b.Sequence] {
			t.Fatalf("duplicate key sequence %q", b.Sequence)
		}
		sequences[b.Sequence] = true
		if got := actionForSequence(b.Sequence); got != b.Action {
			t.Fatalf("%s resolves to %s, want %s", b.Sequence, got, b.Action)
		}
		for _, alias := range b.Commands {
			if commands[alias] {
				t.Fatalf("duplicate command alias %q", alias)
			}
			commands[alias] = true
			if got := actionForCommand(alias); got != b.Action {
				t.Fatalf("%s resolves to %s, want %s", alias, got, b.Action)
			}
		}
	}
	first, second := leaderHints(76)
	if len([]rune(first)) > 76 || len([]rune(second)) > 76 || !strings.Contains(first+second, "q quit") {
		t.Fatalf("leader hint overflows or omits quit: %q / %q", first, second)
	}
}
func TestTagDialogDoesNotOverwriteExternalEdit(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.action("tags")
	m.Prompt.SetValue("urgent")
	external := []byte("# Question?\n\nExternal change\n")
	if e := os.WriteFile(m.Active.Path, external, 0600); e != nil {
		t.Fatal(e)
	}
	press(m, tea.KeyEnter, "", 0)
	if m.Overlay != "tags" {
		t.Fatalf("dialog closed after conflict: %q", m.Overlay)
	}
	actual, e := os.ReadFile(m.Active.Path)
	if e != nil {
		t.Fatal(e)
	}
	if string(actual) != string(external) {
		t.Fatalf("external edit overwritten: %q", actual)
	}
}
func TestTagDialogPreservesEditorState(t *testing.T) {
	for _, mode := range []string{"vim", "traditional"} {
		t.Run(mode, func(t *testing.T) {
			m := modelFixture(t, mode)
			if err := m.open("question.md"); err != nil {
				t.Fatal(err)
			}
			m.focus("editor")
			m.Editor.Pos = 5
			if mode == "vim" {
				m.Editor.Mode = "VISUAL"
				m.Editor.VisualStart = 2
			} else {
				m.Editor.Selecting = true
				m.Editor.SelectionStart = 2
			}
			m.Editor.render()
			prior := m.Editor
			m.action("tags")
			press(m, tea.KeyEnter, "", 0)
			if m.Overlay != "" || m.Focus != "editor" || m.Editor != prior || m.Editor.Pos != 5 {
				t.Fatalf("unchanged tags reset editor: overlay=%q focus=%q pos=%d", m.Overlay, m.Focus, m.Editor.Pos)
			}
			if mode == "vim" && (m.Editor.Mode != "VISUAL" || m.Editor.VisualStart != 2) || mode == "traditional" && (!m.Editor.Selecting || m.Editor.SelectionStart != 2) {
				t.Fatalf("selection lost: %+v", m.Editor)
			}
		})
	}
}
func TestTagEditRetainsInsertModeCursorAndUndo(t *testing.T) {
	m := modelFixture(t, "vim")
	if err := m.open("question.md"); err != nil {
		t.Fatal(err)
	}
	m.focus("editor")
	before := m.Editor.Value()
	m.Editor.Pos = len(m.Editor.Text)
	oldPos := m.Editor.Pos
	m.Editor.beginInsert()
	m.action("tags")
	m.Prompt.SetValue("urgent")
	press(m, tea.KeyEnter, "", 0)
	if m.Overlay != "" || m.Focus != "editor" || m.Editor.Mode != "INSERT" || len(m.Active.Tags) != 1 || m.Active.Tags[0] != "urgent" {
		t.Fatalf("tag edit lost mode or tags: overlay=%q focus=%q mode=%q tags=%v", m.Overlay, m.Focus, m.Editor.Mode, m.Active.Tags)
	}
	withTag := m.Editor.Value()
	if m.Editor.Pos != oldPos+len([]rune(withTag))-len([]rune(before)) {
		t.Fatalf("cursor did not follow original text: got %d, want %d; before %q after %q", m.Editor.Pos, oldPos+len([]rune(withTag))-len([]rune(before)), before, withTag)
	}
	press(m, 'x', "x", 0)
	press(m, tea.KeyEsc, "", 0)
	m.Editor.UndoEdit()
	if m.Editor.Value() != withTag {
		t.Fatal("undo of post-dialog typing removed the tag edit")
	}
	m.Editor.UndoEdit()
	if m.Editor.Value() != before {
		t.Fatal("tag edit was not a separate undo step")
	}
}
func TestHelpRestoresFirstRunChoiceAndCommandCompletion(t *testing.T) {
	m := modelFixture(t, "ask")
	press(m, '?', "?", 0)
	if m.Overlay != "help" {
		t.Fatal(m.Overlay)
	}
	press(m, tea.KeyEsc, "", 0)
	if m.Overlay != "choose" {
		t.Fatalf("help did not restore choice: %q", m.Overlay)
	}
	press(m, tea.KeyEnter, "", 0)
	press(m, tea.KeyEsc, "", 0)
	press(m, ':', ":", 0)
	if m.Overlay != "command" {
		t.Fatal(m.Overlay)
	}
	m.Prompt.SetValue("wq")
	press(m, tea.KeyTab, "", 0)
	if m.Prompt.Value() != "wq" {
		t.Fatalf("completion changed command: %q", m.Prompt.Value())
	}
}
func TestNavigationUsesSharedListMotions(t *testing.T) {
	m := modelFixture(t, "vim")
	press(m, tea.KeyEsc, "", 0)
	press(m, 'G', "G", 0)
	if m.Rows[m.Selected].ID != "question.md" {
		t.Fatalf("G selected %d of %d", m.Selected, len(m.Rows))
	}
	press(m, 'g', "g", 0)
	press(m, 'g', "g", 0)
	if m.Rows[m.Selected].ID != "question.md" {
		t.Fatalf("gg selected %d", m.Selected)
	}
	press(m, tea.KeyTab, "", 0)
	press(m, 'G', "G", 0)
	press(m, 'k', "k", 0)
	if m.Selected != len(m.Rows)-2 {
		t.Fatalf("tree k selected %d", m.Selected)
	}
}
func TestSaveRefreshesActiveTags(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	content := "# Question?\n\n#urgent\n"
	m.Editor.replaceText(content, len([]rune(content)))
	m.Dirty = true
	if e := m.save(); e != nil {
		t.Fatal(e)
	}
	if len(m.Active.Tags) != 1 || m.Active.Tags[0] != "urgent" {
		t.Fatalf("stale active tags: %q", m.Active.Tags)
	}
}
func TestConflictOverlayRestoresEditorFocus(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.focus("editor")
	m.Editor.insert("local ")
	m.Dirty = true
	if e := os.WriteFile(m.Active.Path, []byte("# external\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := m.save(); e == nil || m.Overlay != "conflict" {
		t.Fatalf("conflict missing: %v %q", e, m.Overlay)
	}
	press(m, tea.KeyEsc, "", 0)
	if m.Overlay != "" || m.Focus != "editor" || !m.Dirty {
		t.Fatalf("editor state lost: overlay=%q focus=%q dirty=%t", m.Overlay, m.Focus, m.Dirty)
	}
}
func TestConflictRecoveryKeepsBothVersions(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.focus("editor")
	m.Editor.insert("local ")
	want := m.Editor.Value()
	m.Dirty = true
	if e := os.WriteFile(m.Active.Path, []byte("# external\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := m.save(); e == nil || m.Overlay != "conflict" {
		t.Fatalf("conflict missing: %v", e)
	}
	press(m, 'c', "c", 0)
	if m.Overlay != "" || m.Dirty || m.Editor.Value() != "# external\n" {
		t.Fatalf("external version not reloaded: overlay=%q dirty=%t value=%q", m.Overlay, m.Dirty, m.Editor.Value())
	}
	files, e := filepath.Glob(filepath.Join(m.Store.StateDir, "recovery", "*.md"))
	if e != nil || len(files) != 1 {
		t.Fatalf("recovery files: %q, %v", files, e)
	}
	b, e := os.ReadFile(files[0])
	if e != nil || string(b) != want {
		t.Fatalf("recovery content: %q, %v", b, e)
	}
	items, e := m.Store.List()
	if e != nil || len(items) != 1 {
		t.Fatalf("recovery draft appeared in note index: %v, %v", items, e)
	}
}
func TestQuotedSlashStaysInGlobalQuery(t *testing.T) {
	m := modelFixture(t, "vim")
	press(m, 'v', "", tea.ModCtrl)
	press(m, '/', "/", 0)
	if m.Overlay != "" || m.Query.Value() != "/" {
		t.Fatalf("quoted slash changed scope: overlay=%q query=%q", m.Overlay, m.Query.Value())
	}
}
func TestCurrentNoteSearchCancelKeepsGlobalState(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.Query.SetValue("question")
	m.SelectedTag = "all"
	selected := m.Selected
	press(m, '/', "/", 0)
	if m.Overlay != "find" {
		t.Fatalf("slash did not open current-note search: %q", m.Overlay)
	}
	press(m, tea.KeyEsc, "", 0)
	if m.Overlay != "" || m.Focus != "search" || m.SearchMode != "INPUT" || m.Query.Value() != "question" || m.Selected != selected || m.SelectedTag != "all" {
		t.Fatalf("cancel lost global state: overlay=%q focus=%q mode=%q query=%q selected=%d tag=%q", m.Overlay, m.Focus, m.SearchMode, m.Query.Value(), m.Selected, m.SelectedTag)
	}
}
func TestPasteAndUnfocusedVisualSelection(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.focus("editor")
	press(m, 'i', "i", 0)
	pasted := "? /: カフェ\n\t#tag"
	m.Update(tea.PasteMsg{Content: pasted})
	if m.Overlay != "" || !strings.HasPrefix(m.Editor.Value(), pasted) {
		t.Fatalf("paste was interpreted as shortcuts: overlay=%q text=%q", m.Overlay, m.Editor.Value())
	}
	press(m, tea.KeyEsc, "", 0)
	m.Editor.Pos = 0
	m.Editor.handleNormal("v")
	m.Editor.handleNormal("l")
	m.focus("tree")
	if got := m.View().Content; !strings.Contains(got, "⟦?") || !strings.Contains(got, "Tree NAVIGATION") {
		t.Fatalf("unfocused selection or focus missing: %q", got)
	}
	if e := m.save(); e != nil {
		t.Fatal(e)
	}
	n, e := m.Store.Read("question.md")
	if e != nil || !strings.HasPrefix(string(n.Content), pasted) {
		t.Fatalf("pasted bytes changed on save: %q, %v", n.Content, e)
	}
}
func TestPastedQueryInsertsAtCursorWithoutShortcuts(t *testing.T) {
	m := modelFixture(t, "vim")
	m.Query.SetValue("ac")
	m.Query.SetCursor(1)
	m.Update(tea.PasteMsg{Content: "b?/"})
	if m.Query.Value() != "ab?/c" || m.Overlay != "" {
		t.Fatalf("query paste lost position or triggered shortcut: %q, overlay=%q", m.Query.Value(), m.Overlay)
	}
}
func TestSyncShowsReadOnlyEditorState(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.focus("editor")
	press(m, 'i', "i", 0)
	before := m.Editor.Value()
	m.SyncRunning = true
	if got := m.View().Content; !strings.Contains(got, "READ-ONLY SYNC") {
		t.Fatal("sync did not visibly mark editor read-only")
	}
	press(m, tea.KeyTab, "", 0)
	press(m, 'x', "x", 0)
	m.Update(tea.PasteMsg{Content: "pasted"})
	if m.Editor.Value() != before {
		t.Fatalf("editor changed during sync: %q", m.Editor.Value())
	}
	press(m, 'g', "", tea.ModCtrl)
	press(m, 'h', "h", 0)
	if m.Overlay != "help" {
		t.Fatal("help unavailable during read-only sync")
	}
}
func TestQuitWaitsForBackgroundSync(t *testing.T) {
	m := modelFixture(t, "vim")
	m.SyncRunning = true
	for _, action := range []string{"quit", "save-quit"} {
		if cmd := m.action(action); cmd != nil || !strings.Contains(m.Message, "Sync in progress") {
			t.Fatalf("%s exited during sync: command=%v message=%q", action, cmd, m.Message)
		}
	}
	if _, cmd := m.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatal("Ctrl+Q exited during sync")
	}
	m.Update(syncMsg{skipped: true, dueAfter: time.Minute})
	if cmd := m.action("quit"); cmd == nil {
		t.Fatal("quit stayed blocked after sync finished")
	}
}
func TestIgnoredSavedNoteWarnsInTUI(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	m := modelFixture(t, "vim")
	if out, e := exec.Command("git", "init", "-b", "main", m.Store.Repo).CombinedOutput(); e != nil {
		t.Fatalf("init: %v: %s", e, out)
	}
	if e := os.WriteFile(filepath.Join(m.Store.Repo, ".gitignore"), []byte("question.md\n"), 0600); e != nil {
		t.Fatal(e)
	}
	m.Config.Git.AutoSync = true
	warning := m.markChanged("question.md")
	if !strings.Contains(warning, "ignored by Git") || m.SyncPending {
		t.Fatalf("ignored note warning or pending state wrong: %q, pending=%t", warning, m.SyncPending)
	}
}
func TestShiftedPrintableTerminalKeys(t *testing.T) {
	m := modelFixture(t, "vim")
	press(m, '/', "?", tea.ModShift)
	if m.Query.Value() != "?" || m.Overlay != "" {
		t.Fatalf("shifted question mark in entry: %q %q", m.Query.Value(), m.Overlay)
	}
	press(m, tea.KeyEsc, "", 0)
	press(m, '/', "?", tea.ModShift)
	if m.Overlay != "help" {
		t.Fatalf("shifted question mark did not open help: %q", m.Overlay)
	}
	press(m, tea.KeyEsc, "", 0)
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.focus("editor")
	press(m, 'a', "A", tea.ModShift)
	if m.Editor.Mode != "INSERT" || m.Editor.Pos != len([]rune("# Question?")) {
		t.Fatalf("shifted A did not enter insert at line end: %s %d", m.Editor.Mode, m.Editor.Pos)
	}
}
func TestCurrentNoteSearchWrapsBothWays(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.Editor.replaceText("alpha beta alpha", 0)
	m.NoteSearch = "alpha"
	m.match(false)
	if m.Editor.Pos != len([]rune("alpha beta ")) {
		t.Fatalf("backward wrap: %d", m.Editor.Pos)
	}
	m.match(true)
	if m.Editor.Pos != 0 {
		t.Fatalf("forward wrap: %d", m.Editor.Pos)
	}
}
func TestAutosaveKeepsEditorUndo(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.focus("editor")
	before := m.Editor.Value()
	press(m, 'i', "i", 0)
	press(m, 'X', "X", 0)
	m.LastEdit = time.Now().Add(-2 * time.Second)
	m.maybeAutosave(time.Now())
	if m.Dirty {
		t.Fatal("autosave did not clear dirty state")
	}
	press(m, tea.KeyEsc, "", 0)
	m.Editor.UndoEdit()
	if m.Editor.Value() != before {
		t.Fatalf("undo lost after autosave: %q", m.Editor.Value())
	}
}
func TestTagRemovalPreviewsAffectedLines(t *testing.T) {
	m := modelFixture(t, "vim")
	path := filepath.Join(m.Store.Repo, "question.md")
	content := "# Question?\n\nTags: #work\n\nBody #idea\n"
	if e := os.WriteFile(path, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.action("tags")
	m.Prompt.SetValue("")
	press(m, tea.KeyEnter, "", 0)
	if m.Overlay != "confirm-tags" || !strings.Contains(m.Message, "line 3 (managed") || !strings.Contains(m.Message, "line 5 (prose") {
		t.Fatalf("missing removal preview: %q %q", m.Overlay, m.Message)
	}
	press(m, tea.KeyEsc, "", 0)
	b, e := os.ReadFile(path)
	if e != nil || string(b) != content {
		t.Fatalf("cancel changed the note: %q %v", b, e)
	}
}
func TestTagTreeExpansionAndParentFilter(t *testing.T) {
	m := modelFixture(t, "vim")
	if _, e := m.Store.Create(store.NewOptions{Title: "Alpha", Path: "alpha.md", Tags: []string{"work/project"}}); e != nil {
		t.Fatal(e)
	}
	if e := m.refresh(); e != nil {
		t.Fatal(e)
	}
	m.focus("tree")
	for i, row := range m.Rows {
		if row.Root && row.Tag == "work" {
			m.Selected = i
			break
		}
	}
	m.openSelected()
	if !m.Expanded["work"] || m.SelectedTag != "work" {
		t.Fatalf("parent tag did not expand: %v %q", m.Expanded, m.SelectedTag)
	}
	for i, row := range m.Rows {
		if row.Root && row.Tag == "work/project" {
			m.Selected = i
			break
		}
	}
	m.openSelected()
	duplicates := 0
	for _, row := range m.Rows {
		if row.ID == "alpha.md" {
			duplicates++
		}
		if row.ID == "question.md" {
			t.Fatal("unrelated note survived tag filter")
		}
	}
	if duplicates != 3 {
		t.Fatalf("note did not appear under expanded All/parent/child roots: %d", duplicates)
	}
	press(m, tea.KeyEsc, "", 0)
	if m.SelectedTag != "all" {
		t.Fatalf("clear filter failed: %q", m.SelectedTag)
	}
}
func TestSearchDebounceDropsStaleResults(t *testing.T) {
	m := modelFixture(t, "vim")
	m.Query.SetValue("question")
	stale := m.scheduleSearch()
	m.Query.SetValue("not present")
	latest := m.scheduleSearch()
	m.Update(stale())
	if m.SearchApplied == m.SearchGeneration {
		t.Fatal("stale search applied")
	}
	m.Update(latest())
	if m.SearchApplied != m.SearchGeneration {
		t.Fatal("latest search was not applied")
	}
	for _, row := range m.Rows {
		if !row.Root {
			t.Fatalf("stale note result remained: %+v", row)
		}
	}
}
func TestMovingToGlobalSearchFlushesEditor(t *testing.T) {
	m := modelFixture(t, "vim")
	if e := m.open("question.md"); e != nil {
		t.Fatal(e)
	}
	m.focus("editor")
	m.Editor.insert("freshword ")
	m.Dirty = true
	press(m, tea.KeyTab, "", 0)
	if m.Focus != "search" || m.Dirty {
		t.Fatalf("dirty editor entered search: %q dirty=%t", m.Focus, m.Dirty)
	}
	if b, e := os.ReadFile(m.Active.Path); e != nil || !strings.Contains(string(b), "freshword") {
		t.Fatalf("buffer not flushed: %q %v", b, e)
	}
	m.Query.SetValue("freshword")
	m.rebuildRows()
	if m.Rows[m.Selected].ID != "question.md" {
		t.Fatalf("saved note absent from search: %+v", m.Rows)
	}
}
func TestConflictStopsAutomaticRetry(t *testing.T) {
	m := modelFixture(t, "vim")
	m.Config.Git.AutoSync = true
	m.Update(syncMsg{err: fmt.Errorf("%w: shared.md", syncgit.ErrConflict)})
	if !m.SyncBlocked {
		t.Fatal("conflict did not block retry")
	}
	m.Update(tickMsg(time.Now()))
	if m.SyncRunning {
		t.Fatal("automatic retry started while conflict unresolved")
	}
}
