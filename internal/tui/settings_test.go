package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/theme"
	"github.com/pelletier/go-toml/v2"
)

func settingsFixture(t *testing.T, mode string) *Model {
	t.Helper()
	m := modelFixture(t, mode)
	m.Config.Color = "never"
	b, err := toml.Marshal(m.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(m.Config.Path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return m
}
func settingsCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing settings command")
	}
	m.Update(cmd())
}
func settingsPress(t *testing.T, m *Model, code rune, text string, mod tea.KeyMod) {
	t.Helper()
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Text: text, Mod: mod})
	if cmd != nil {
		settingsCmd(t, m, cmd)
	}
}
func settingsSelect(m *Model, value string) {
	for i, r := range m.Settings.rows() {
		if r.Key == value {
			m.Settings.Selected = i
			m.previewSetting()
			return
		}
	}
	panic("missing picker option: " + value)
}
func TestSettingsPreservesBufferAndScopedText(t *testing.T) {
	for _, mode := range []string{"vim", "traditional"} {
		t.Run(mode, func(t *testing.T) {
			m := settingsFixture(t, mode)
			if err := m.open("question.md"); err != nil {
				t.Fatal(err)
			}
			m.focus("editor")
			if mode == "vim" {
				press(m, 'i', "i", 0)
			}
			press(m, 'x', "x", 0)
			e := m.Editor
			content, pos, undo, dirty, entry := e.Value(), e.Pos, append([]editorSnapshot(nil), e.Undo...), m.Dirty, e.Mode
			m.Query.SetValue("remember")
			m.SearchMode = "INPUT"
			selected, tag := m.Selected, m.SelectedTag
			press(m, 'g', "", tea.ModCtrl)
			_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
			settingsCmd(t, m, cmd)
			if m.Overlay != "settings" || m.Settings.Focus != "filter" {
				t.Fatal("settings shortcut failed")
			}
			m.Update(tea.PasteMsg{Content: "?/ : カフェ"})
			if m.Settings.Filter.Value() != "?/ : カフェ" || m.Overlay != "settings" || m.Query.Value() != "remember" {
				t.Fatal("settings text affected background")
			}
			press(m, tea.KeyTab, "", 0)
			if m.Settings.Focus != "list" || m.Focus != "editor" {
				t.Fatal("Tab escaped modal")
			}
			press(m, '?', "?", 0)
			if m.Overlay != "help" {
				t.Fatal("navigation help failed")
			}
			press(m, tea.KeyEsc, "", 0)
			if m.Overlay != "settings" || m.Settings.Filter.Value() != "?/ : カフェ" {
				t.Fatal("help lost filter")
			}
			press(m, tea.KeyEsc, "", 0)
			if m.Overlay != "" || m.Editor != e || e.Value() != content || e.Pos != pos || !reflect.DeepEqual(e.Undo, undo) || m.Dirty != dirty || e.Mode != entry || m.Focus != "editor" || m.Query.Value() != "remember" || m.SearchMode != "INPUT" || m.Selected != selected || m.SelectedTag != tag {
				t.Fatal("settings did not restore editor/search state")
			}
			press(m, tea.KeyTab, "", 0)
			if m.Focus != "editor" || !strings.Contains(e.Value(), "\t") {
				t.Fatal("editor Tab behavior changed")
			}
		})
	}
}
func TestThemePreviewCancelConfirmAndOverride(t *testing.T) {
	m := settingsFixture(t, "vim")
	settingsCmd(t, m, m.openSettings(true))
	before, _ := os.ReadFile(m.Config.Path)
	settingsSelect(m, "nord")
	if m.schemeName() != "nord" || m.Config.Theme != "catppuccin-mocha" {
		t.Fatal("preview changed runtime config")
	}
	after, _ := os.ReadFile(m.Config.Path)
	if string(before) != string(after) {
		t.Fatal("preview wrote config")
	}
	press(m, 'g', "", tea.ModCtrl)
	press(m, 'h', "h", 0)
	press(m, tea.KeyEsc, "", 0)
	if m.Settings.Preview != "nord" || m.Overlay != "settings" {
		t.Fatal("help lost preview")
	}
	press(m, tea.KeyEsc, "", 0)
	if m.schemeName() != "catppuccin-mocha" || m.Settings != nil {
		t.Fatal("cancel failed to restore palette")
	}
	if err := m.Config.OverrideFrom("theme", "nord", "flag --theme"); err != nil {
		t.Fatal(err)
	}
	if err := m.Config.OverrideFrom("color", "never", "flag --no-color"); err != nil {
		t.Fatal(err)
	}
	settingsCmd(t, m, m.openSettings(true))
	settingsSelect(m, "gruvbox-dark")
	if !strings.Contains(m.View().Content, "flag --theme") || strings.Contains(m.View().Content, "\x1b[") {
		t.Fatal("override or no-color preview missing")
	}
	settingsPress(t, m, tea.KeyEnter, "", 0)
	c, err := config.Load(m.Config.Path)
	if err != nil || c.Theme != "gruvbox-dark" || m.Config.Theme != "nord" || m.Settings != nil {
		t.Fatalf("confirmation/override: %v %+v", err, c)
	}
}
func TestSettingsRoundTripEveryKeyAndReset(t *testing.T) {
	values := map[string]string{"theme": "solarized-light", "color": "always", "tui.editor_mode": "traditional", "tui.autosave_delay": "500ms", "editor": `["vim","-n"]`, "default_category": "work/research", "journal.timezone": "America/Chicago", "git.auto_sync": "true", "git.max_interval": "8m", "git.timeout": "5s", "repo": "./next-repo", "update.repository": "example/notes"}
	for key, value := range values {
		t.Run(key, func(t *testing.T) {
			m := settingsFixture(t, "vim")
			oldStore, oldRepo := m.Store, m.Config.Repo
			settingsCmd(t, m, m.openSettings(false))
			m.beginSetting(key)
			if m.Settings.View == "picker" {
				settingsSelect(m, value)
			} else {
				m.Settings.Input.SetValue(value)
			}
			settingsPress(t, m, tea.KeyEnter, "", 0)
			c, err := config.Load(m.Config.Path)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := config.Get(c, key)
			if key == "repo" {
				value = filepath.Join(filepath.Dir(m.Config.Path), "next-repo")
				if m.Store != oldStore || m.Config.Repo != oldRepo {
					t.Fatal("changed active repository")
				}
				if _, err = os.Stat(value); !os.IsNotExist(err) {
					t.Fatal("repo edit created directory")
				}
			}
			if config.ValueText(got) != value {
				t.Fatalf("%s: got %s want %s", key, config.ValueText(got), value)
			}
			if m.Settings.View != "home" {
				t.Fatal("save did not return home")
			}
			// Exercise reset confirmation, including the duration/editor/path types.
			for i, row := range m.Settings.rows() {
				if row.Key == key {
					m.Settings.Selected = i
				}
			}
			press(m, 'r', "", tea.ModCtrl)
			if m.Settings.View != "reset" {
				t.Fatal("missing reset confirmation")
			}
			settingsPress(t, m, tea.KeyEnter, "", 0)
			s, err := config.Inspect(m.Config.Path, nil)
			if err != nil || s.Stored[key].Present {
				t.Fatal("reset did not unset", err)
			}
		})
	}
}
func TestSettingsValidationConflictsAndMalformedFile(t *testing.T) {
	m := settingsFixture(t, "vim")
	settingsCmd(t, m, m.openSettings(false))
	m.beginSetting("git.timeout")
	before, _ := os.ReadFile(m.Config.Path)
	m.Settings.Input.SetValue("-1s")
	settingsPress(t, m, tea.KeyEnter, "", 0)
	if m.Settings.Error == "" || m.Settings.View != "value" {
		t.Fatal("no inline validation")
	}
	after, _ := os.ReadFile(m.Config.Path)
	if string(before) != string(after) {
		t.Fatal("invalid value persisted")
	}
	m.Settings.Input.SetValue("9s")
	if err := config.Set(m.Config.Path, "git.timeout", "12s"); err != nil {
		t.Fatal(err)
	}
	settingsPress(t, m, tea.KeyEnter, "", 0)
	if m.Settings.View != "stale" || !strings.Contains(m.Settings.Error, "Not saved") {
		t.Fatal("stale edit silently overwrote config")
	}
	settingsPress(t, m, 'r', "r", 0)
	if m.Settings.Input.Value() != "12s" || !strings.Contains(m.Settings.Error, "9s") {
		t.Fatal("reload lost conflict context")
	}
	press(m, tea.KeyEsc, "", 0)
	if err := os.WriteFile(m.Config.Path, []byte("unknown = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m.beginSetting("theme")
	settingsSelect(m, "nord")
	settingsPress(t, m, tea.KeyEnter, "", 0)
	if m.Config.Theme != "catppuccin-mocha" || !strings.Contains(m.Settings.Error, "Not saved") {
		t.Fatal("bad file changed runtime")
	}
	press(m, tea.KeyEsc, "", 0)
	press(m, tea.KeyEsc, "", 0)
	settingsCmd(t, m, m.openSettings(false))
	if m.Settings.View != "error" {
		t.Fatal("malformed config not reported")
	}
}
func TestSettingsModeSchedulingAndNestedConflicts(t *testing.T) {
	m := settingsFixture(t, "vim")
	if err := m.open("question.md"); err != nil {
		t.Fatal(err)
	}
	m.focus("editor")
	press(m, 'i', "i", 0)
	press(m, 'x', "x", 0)
	e := m.Editor
	oldText, oldPos := e.Value(), e.Pos
	settingsCmd(t, m, m.openSettings(false))
	m.beginSetting("tui.editor_mode")
	settingsSelect(m, "traditional")
	settingsPress(t, m, tea.KeyEnter, "", 0)
	if m.Editor != e || e.Flavor != "traditional" || e.Mode != "COMMAND" || e.Value() != oldText || e.Pos != oldPos || len(e.Undo) == 0 {
		t.Fatal("mode change lost editor history")
	}
	m.beginSetting("tui.editor_mode")
	settingsSelect(m, "ask")
	settingsPress(t, m, tea.KeyEnter, "", 0)
	if m.Flavor != "traditional" || m.Config.TUI.EditorMode != "ask" {
		t.Fatal("ask changed session flavor")
	}
	m.SyncRunning = true
	m.SyncFailed = true
	m.SyncPending = true
	m.SyncBlocked = true
	next := time.Now().Add(time.Minute)
	m.NextDue = next
	m.beginSetting("git.timeout")
	m.Settings.Input.SetValue("8s")
	settingsPress(t, m, tea.KeyEnter, "", 0)
	if m.Sync.Timeout != 8*time.Second || !m.SyncRunning || !m.SyncPending || !m.SyncBlocked || !m.SyncFailed {
		t.Fatal("timeout change lost sync state")
	}
	m.beginSetting("git.max_interval")
	m.Settings.Input.SetValue("2m")
	settingsPress(t, m, tea.KeyEnter, "", 0)
	if !m.NextDue.Equal(next) {
		t.Fatal("discarded failure/conflict backoff")
	}
	m.SyncRunning = false
	// Autosave detects an external edit above a settings field and nested help.
	m.beginSetting("default_category")
	m.Settings.Input.SetValue("draft-category")
	if err := os.WriteFile(filepath.Join(m.Store.Repo, "question.md"), []byte("# External\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m.LastEdit = time.Now().Add(-time.Hour)
	m.maybeAutosave(time.Now())
	if m.Overlay != "conflict" {
		t.Fatal("missing note conflict")
	}
	press(m, 'g', "", tea.ModCtrl)
	press(m, 'h', "h", 0)
	m.maybeAutosave(time.Now())
	press(m, tea.KeyEsc, "", 0)
	if m.Overlay != "conflict" {
		t.Fatal("autosave disturbed nested help")
	}
	press(m, tea.KeyEsc, "", 0)
	if m.Overlay != "settings" || m.Settings.Input.Value() != "draft-category" {
		t.Fatal("conflict lost settings draft")
	}
	press(m, tea.KeyEsc, "", 0)
	press(m, tea.KeyEsc, "", 0)
	if m.Focus != "editor" || e.Mode != "COMMAND" {
		t.Fatal("restored stale editor flavor mode")
	}
}
func TestSettingsAsyncSaveHelpAndLateLoads(t *testing.T) {
	m := settingsFixture(t, "vim")
	settingsCmd(t, m, m.openSettings(true))
	settingsSelect(m, "nord")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || !m.Settings.Saving {
		t.Fatal("missing async save")
	}
	_, duplicate := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if duplicate != nil {
		t.Fatal("duplicate write scheduled")
	}
	press(m, 'g', "", tea.ModCtrl)
	press(m, 'h', "h", 0)
	settingsCmd(t, m, cmd)
	press(m, tea.KeyEsc, "", 0)
	if m.Overlay != "" || m.Settings != nil || m.Config.Theme != "nord" {
		t.Fatal("direct picker did not return after saved help")
	}
	old := m.openSettings(false)
	press(m, tea.KeyEsc, "", 0)
	latest := m.openSettings(false)
	settingsCmd(t, m, latest)
	state := m.Settings
	settingsCmd(t, m, old)
	if m.Settings != state || m.Settings.Loading {
		t.Fatal("stale load replaced settings")
	}
}
func TestSettingsViewsFitAndPlainWidgets(t *testing.T) {
	for _, size := range [][2]int{{80, 16}, {100, 30}, {127, 40}} {
		for _, scheme := range theme.Names() {
			t.Run(scheme+"/"+strconv.Itoa(size[0]), func(t *testing.T) {
				m := settingsFixture(t, "vim")
				m.Config.Theme = scheme
				m.Config.Color = "always"
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				settingsCmd(t, m, m.openSettings(false))
				for _, key := range []string{"", "theme", "repo", "@render"} {
					if key != "" {
						m.beginSetting(key)
					}
					v := m.View().Content
					if lipgloss.Width(v) > size[0] || lipgloss.Height(v) > size[1] {
						t.Fatalf("view %q overflows %dx%d: %dx%d", key, size[0], size[1], lipgloss.Width(v), lipgloss.Height(v))
					}
					if !strings.Contains(ansi.Strip(v), "[FOCUS]") {
						t.Fatal("missing visible focus")
					}
					if key != "" {
						m.settingsBack()
					}
				}
				m.Config.Color = "never"
				m.beginSetting("repo")
				if strings.Contains(m.View().Content, "\x1b[") {
					t.Fatal("plain widget contains color/attribute escapes")
				}
				m.Update(tea.WindowSizeMsg{Width: 70, Height: 10})
				if !strings.Contains(m.View().Content, "Resize terminal") {
					t.Fatal("no resize hint")
				}
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				if m.Settings.View != "value" {
					t.Fatal("resize lost field draft")
				}
			})
		}
	}
}

func TestSettingsRestoresFilterCursorAfterPicker(t *testing.T) {
	m := settingsFixture(t, "vim")
	settingsCmd(t, m, m.openSettings(false))
	m.Update(tea.PasteMsg{Content: "theme"})
	if m.Settings.Filter.Position() != 5 {
		t.Fatal("bad initial cursor")
	}
	settingsPress(t, m, tea.KeyEnter, "", 0)
	m.Update(tea.PasteMsg{Content: "nord"})
	settingsPress(t, m, tea.KeyEnter, "", 0)
	if m.Settings.Filter.Value() != "theme" || m.Settings.Filter.Position() != 5 {
		t.Fatal("picker lost filter cursor")
	}
	press(m, 'u', "", tea.ModCtrl)
	m.Update(tea.PasteMsg{Content: "tui.autosave_delay"})
	settingsPress(t, m, tea.KeyEnter, "", 0)
	if m.Settings.Key != "tui.autosave_delay" || m.Settings.View != "value" {
		t.Fatal("restored filter did not select next field")
	}
}

func TestSettingsCommandEntryAndBindingScopes(t *testing.T) {
	for _, command := range []string{"settings", "config", "themes"} {
		m := settingsFixture(t, "vim")
		press(m, tea.KeyEsc, "", 0)
		press(m, ':', ":", 0)
		m.Prompt.SetValue(command)
		settingsPress(t, m, tea.KeyEnter, "", 0)
		if m.Overlay != "settings" {
			t.Fatalf("command %s did not open settings", command)
		}
		if command == "themes" && (m.Settings.View != "picker" || !m.Settings.Direct) {
			t.Fatal("themes did not open direct picker")
		}
	}
	m := settingsFixture(t, "vim")
	settingsCmd(t, m, m.openSettings(false))
	if settingsAction(m.Settings, "j") != "" || settingsAction(m.Settings, "?") != "" {
		t.Fatal("text input intercepted by navigation")
	}
	m.Settings.Focus = "list"
	if settingsAction(m.Settings, "j") != "next" || settingsAction(m.Settings, "?") != "help" {
		t.Fatal("missing scoped navigation")
	}
	m.beginSetting("git.timeout")
	if settingsAction(m.Settings, "ctrl+r") != "" || settingsAction(m.Settings, "?") != "" || settingsAction(m.Settings, "ctrl+s") != "confirm" {
		t.Fatal("wrong value input scope")
	}
}

func TestSettingsWriteFailureRetainsDraftAndRuntime(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	m := settingsFixture(t, "vim")
	settingsCmd(t, m, m.openSettings(false))
	m.beginSetting("theme")
	settingsSelect(m, "nord")
	before, _ := os.ReadFile(m.Config.Path)
	dir := filepath.Dir(m.Config.Path)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	settingsPress(t, m, tea.KeyEnter, "", 0)
	after, _ := os.ReadFile(m.Config.Path)
	if string(before) != string(after) || m.Config.Theme != "catppuccin-mocha" || m.Settings.Preview != "nord" || !strings.Contains(m.Settings.Error, "Not saved") {
		t.Fatal("write failure discarded state or changed configuration")
	}
	press(m, tea.KeyEsc, "", 0)
	if m.schemeName() != "catppuccin-mocha" {
		t.Fatal("cancel after failure retained preview")
	}
}
