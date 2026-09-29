package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/theme"
	"github.com/pelletier/go-toml/v2"
)

var settingsBindings = []Binding{
	{"?", "help", "settings navigation", "Show help; literal in text entry", nil, 0},
	{"tab, shift+tab", "focus", "settings choices", "Switch filter/list focus", nil, 1},
	{"up, ctrl+p", "previous", "settings choices", "Select previous row", nil, 3},
	{"down, ctrl+n", "next", "settings choices", "Select next row", nil, 3},
	{"k", "previous", "settings list", "Select previous row", nil, 4},
	{"j", "next", "settings list", "Select next row", nil, 4},
	{"g", "first-prefix", "settings list", "gg selects the first row", nil, 4},
	{"home", "first", "settings list", "Select first row", nil, 4},
	{"G, end", "last", "settings list", "Select last row", nil, 4},
	{"enter", "confirm", "settings", "Edit row or confirm value", nil, 3},
	{"ctrl+s", "confirm", "settings", "Save this setting (not the note)", nil, 3},
	{"ctrl+r", "reset", "settings home", "Reset the selected saved preference", nil, 3},
	{"esc, ctrl+c", "cancel", "settings", "Cancel draft or close settings", nil, 3},
}

func settingsAction(s *settingsState, key string) string {
	for _, b := range settingsBindings {
		active := b.Scope == "settings" || b.Scope == "settings home" && s.View == "home" ||
			b.Scope == "settings choices" && (s.View == "home" || s.View == "picker") ||
			b.Scope == "settings list" && s.Focus == "list" && (s.View == "home" || s.View == "picker") ||
			b.Scope == "settings navigation" && s.View != "value" && s.Focus != "filter"
		if !active {
			continue
		}
		for _, sequence := range strings.Split(b.Sequence, ",") {
			if strings.TrimSpace(sequence) == key {
				return b.Action
			}
		}
	}
	return ""
}

type settingsState struct {
	View, Key, Focus, Error, Preview, Initial, HomeFilter, HomeFocus, Attempted string
	Filter, Input                                                               textinput.Model
	Selected, HomeSelected, HomePosition, Offset                                int
	Snapshot                                                                    config.Snapshot
	Expected                                                                    config.StoredValue
	Loading, Saving, Direct, Prefix, CloseOnReturn                              bool
	Overrides                                                                   []config.Override
}
type settingsLoadedMsg struct {
	State    *settingsState
	Snapshot config.Snapshot
	Err      error
}
type settingsSavedMsg struct {
	State    *settingsState
	Snapshot config.Snapshot
	Key      string
	Err      error
}
type settingRow struct{ Key, Label, Group, Value, Detail string }

func (m *Model) openSettings(direct bool) tea.Cmd {
	f, i := textinput.New(), textinput.New()
	f.Placeholder = "Search settings"
	f.SetWidth(max(10, min(m.Width-26, 70)))
	i.SetWidth(max(10, min(m.Width-12, 84)))
	f.Focus()
	s := &settingsState{View: "home", Focus: "filter", Filter: f, Input: i, Direct: direct, Overrides: append([]config.Override(nil), m.Config.Launch...)}
	m.Settings = s
	m.setOverlay("settings")
	return m.loadSettings()
}
func (m *Model) loadSettings() tea.Cmd {
	s, path := m.Settings, m.Config.Path
	s.Loading = true
	s.Error = ""
	return func() tea.Msg {
		snapshot, err := config.Inspect(path, s.Overrides)
		return settingsLoadedMsg{s, snapshot, err}
	}
}
func (m *Model) settingsLoaded(msg settingsLoadedMsg) tea.Cmd {
	s := m.Settings
	if s == nil || s != msg.State {
		return nil
	}
	s.Loading = false
	if msg.Err != nil {
		s.Error = msg.Err.Error()
		s.View = "error"
		s.Focus = "list"
		s.Filter.Blur()
		return nil
	}
	s.Snapshot = msg.Snapshot
	if s.View == "stale" {
		attempted := s.Attempted
		m.beginSetting(s.Key)
		s.Error = "Reloaded. Previous attempt: " + attempted
	} else if s.Direct {
		m.beginSetting("theme")
	} else {
		s.View = "home"
		s.Selected = 0
	}
	return nil
}
func settingFor(key string) config.Setting {
	for _, v := range config.Settings() {
		if v.Key == key {
			return v
		}
	}
	return config.Setting{}
}
func matchSetting(row settingRow, query string) bool {
	haystack := strings.ToLower(row.Key + " " + row.Label + " " + row.Group + " " + row.Detail + " " + row.Value)
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(haystack, word) {
			return false
		}
	}
	return true
}
func (s *settingsState) rows() []settingRow {
	var rows []settingRow
	if s.View == "picker" {
		d := settingFor(s.Key)
		for _, option := range d.Options {
			row := settingRow{Key: option, Label: option}
			if d.Kind == "theme" {
				scheme, _ := theme.Lookup(option)
				row.Label, row.Group, row.Detail = scheme.Name, scheme.Kind, scheme.Description
			} else if d.Kind == "bool" {
				row.Label = "Off"
				if option == "true" {
					row.Label = "On"
				}
			}
			if matchSetting(row, s.Filter.Value()) {
				rows = append(rows, row)
			}
		}
	} else {
		for _, d := range config.Settings() {
			v, _ := config.Get(s.Snapshot.Effective, d.Key)
			row := settingRow{d.Key, d.Label, d.Group, config.ValueText(v), d.Help}
			if matchSetting(row, s.Filter.Value()) {
				rows = append(rows, row)
			}
		}
		row := settingRow{Key: "@render", Label: "View effective configuration", Group: "Advanced", Detail: "Inspect resolved TOML and active session differences"}
		if matchSetting(row, s.Filter.Value()) {
			rows = append(rows, row)
		}
	}
	return rows
}
func (s *settingsState) selected() (settingRow, bool) {
	rows := s.rows()
	if len(rows) == 0 {
		return settingRow{}, false
	}
	s.Selected = min(max(0, s.Selected), len(rows)-1)
	return rows[s.Selected], true
}
func (m *Model) previewSetting() {
	s := m.Settings
	if s.View != "picker" || s.Key != "theme" {
		return
	}
	if row, ok := s.selected(); ok {
		s.Preview = row.Key
	} else {
		s.Preview = ""
	}
}
func (m *Model) beginSetting(key string) {
	s := m.Settings
	if s.View == "home" {
		s.rememberHome()
	}
	s.Key, s.Error, s.Preview, s.Prefix, s.Offset = key, "", "", false, 0
	if key == "@render" {
		s.View = "render"
		s.Focus = "list"
		return
	}
	s.Expected = s.Snapshot.Stored[key]
	value, _ := config.Get(s.Snapshot.File, key)
	if s.Expected.Present {
		value = s.Expected.Value
	}
	s.Initial = config.ValueText(value)
	s.Input.SetValue(s.Initial)
	s.Input.CursorEnd()
	d := settingFor(key)
	if len(d.Options) > 0 {
		s.View, s.Focus, s.Selected = "picker", "filter", 0
		s.Filter.SetValue("")
		s.Filter.Placeholder = "Search choices"
		s.Filter.Focus()
		s.Input.Blur()
		for i, row := range s.rows() {
			if row.Key == s.Initial {
				s.Selected = i
			}
		}
		m.previewSetting()
	} else {
		s.View, s.Focus = "value", "value"
		s.Filter.Blur()
		s.Input.Focus()
	}
}
func (s *settingsState) rememberHome() {
	s.HomeFilter, s.HomeSelected, s.HomeFocus, s.HomePosition = s.Filter.Value(), s.Selected, s.Focus, s.Filter.Position()
}
func (s *settingsState) home() {
	s.View, s.Focus, s.Selected = "home", s.HomeFocus, s.HomeSelected
	s.Preview, s.Error, s.Prefix = "", "", false
	s.Filter.Placeholder = "Search settings"
	s.Filter.SetValue(s.HomeFilter)
	s.Filter.SetCursor(s.HomePosition)
	s.Input.Blur()
	if s.Focus == "filter" {
		s.Filter.Focus()
	} else {
		s.Filter.Blur()
	}
}
func (m *Model) settingsBack() {
	s := m.Settings
	s.Preview, s.Error, s.Prefix = "", "", false
	if s.View == "home" || s.View == "error" || s.Direct {
		m.closeOverlay()
		m.Settings = nil
		return
	}
	s.home()

}
func (m *Model) saveSetting(unset bool) tea.Cmd {
	s := m.Settings
	if s.Loading || s.Saving {
		return nil
	}
	text := s.Input.Value()
	if s.View == "picker" {
		row, ok := s.selected()
		if !ok {
			s.Error = "No matching choices"
			return nil
		}
		text = row.Key
	}
	if !unset {
		if err := config.ValidateText(m.Config.Path, s.Key, text); err != nil {
			s.Error = err.Error()
			return nil
		}
		if text == s.Initial {
			m.settingsBack()
			return nil
		}
	}
	s.Saving, s.Error, s.Attempted = true, "", text
	key, path, expected, overrides := s.Key, m.Config.Path, s.Expected, s.Overrides
	return func() tea.Msg {
		snapshot, err := config.Change(path, key, text, unset, &expected, overrides)
		return settingsSavedMsg{s, snapshot, key, err}
	}
}
func (m *Model) settingsSaved(msg settingsSavedMsg) tea.Cmd {
	s := m.Settings
	if s == nil || s != msg.State {
		return nil
	}
	s.Saving = false
	if msg.Err != nil {
		s.Error = "Not saved: " + msg.Err.Error()
		if errors.Is(msg.Err, config.ErrChanged) {
			s.View = "stale"
			s.Focus = "list"
		}
		return nil
	}
	s.Snapshot = msg.Snapshot
	m.applySetting(msg.Snapshot.Effective, msg.Key)
	m.Message = "Saved setting: " + msg.Key
	if m.Overlay == "settings" {
		m.settingsBack()
	} else {
		// A conflict/help overlay may have arrived while the write was running.
		s.CloseOnReturn = s.Direct
		s.home()
	}
	return nil
}
func (m *Model) applySetting(resolved config.Config, key string) {
	if key == "repo" {
		return
	}
	value, _ := config.Get(resolved, key)
	_ = m.Config.Override(key, value)
	if key == "tui.editor_mode" && resolved.TUI.EditorMode != "ask" {
		m.Flavor = resolved.TUI.EditorMode
		if m.Editor != nil {
			m.Editor.SetFlavor(m.Flavor)
		}
	}
	if key == "git.timeout" {
		m.Sync.Timeout, _ = time.ParseDuration(m.Config.Git.Timeout)
	}
	if key == "git.max_interval" || key == "git.auto_sync" {
		if !m.SyncFailed && !m.SyncBlocked {
			m.NextDue = time.Time{}
			m.LastSyncAttempt = time.Time{}
		}
	}
}
func (m *Model) settingsPaste(v tea.PasteMsg) tea.Cmd {
	s := m.Settings
	if s.Loading || s.Saving {
		return nil
	}
	if s.View == "value" {
		var cmd tea.Cmd
		s.Input, cmd = s.Input.Update(v)
		s.Error = ""
		return cmd
	}
	if (s.View == "home" || s.View == "picker") && s.Focus == "filter" {
		var cmd tea.Cmd
		s.Filter, cmd = s.Filter.Update(v)
		s.Selected = 0
		m.previewSetting()
		return cmd
	}
	return nil
}
func (m *Model) settingsKey(k tea.KeyPressMsg) tea.Cmd {
	s, key := m.Settings, keyName(k)
	action := settingsAction(s, key)
	if action == "help" {
		return m.action("help")
	}
	if s.Saving {
		s.Error = "Saving… please wait"
		return nil
	}
	if action == "cancel" {
		m.settingsBack()
		return nil
	}
	if s.Loading {
		return nil
	}
	if s.View == "error" || s.View == "stale" {
		if key == "r" || key == "enter" {
			return m.loadSettings()
		}
		return nil
	}
	if s.View == "reset" {
		if action == "confirm" {
			return m.saveSetting(true)
		}
		return nil
	}
	if s.View == "value" {
		if action == "confirm" {
			return m.saveSetting(false)
		}
		if key == "tab" || key == "shift+tab" {
			return nil
		}
		var cmd tea.Cmd
		s.Input, cmd = s.Input.Update(k)
		s.Error = ""
		return cmd
	}
	if s.View == "render" {
		switch key {
		case "down", "j":
			s.Offset++
		case "up", "k":
			s.Offset = max(0, s.Offset-1)
		case "ctrl+f", "pgdown":
			s.Offset += max(1, m.Height-10)
		case "ctrl+b", "pgup":
			s.Offset = max(0, s.Offset-max(1, m.Height-10))
		case "g", "home":
			s.Offset = 0
		case "G", "end":
			s.Offset = 1000
		}
		return nil
	}
	if action == "focus" {
		if s.Focus == "filter" {
			s.Focus = "list"
			s.Filter.Blur()
		} else {
			s.Focus = "filter"
			s.Filter.Focus()
		}
		s.Prefix = false
		return nil
	}
	if action == "reset" {
		if row, ok := s.selected(); ok && row.Key != "@render" {
			s.rememberHome()
			s.Key, s.Expected, s.View, s.Focus, s.Error = row.Key, s.Snapshot.Stored[row.Key], "reset", "list", ""
		}
		return nil
	}
	if action == "confirm" {
		if s.View == "picker" {
			return m.saveSetting(false)
		}
		if key == "ctrl+s" {
			s.Error = "Choose a setting with Enter to edit it"
			return nil
		}
		if row, ok := s.selected(); ok {
			m.beginSetting(row.Key)
		}
		return nil
	}
	rows := s.rows()
	move := 0
	if action == "previous" {
		move = -1
	}
	if action == "next" {
		move = 1
	}
	if move != 0 {
		s.Selected = min(max(0, s.Selected+move), max(0, len(rows)-1))
		s.Prefix = false
		m.previewSetting()
		return nil
	}
	if s.Focus == "list" {
		switch action {
		case "first":
			s.Selected = 0
			s.Prefix = false
		case "last":
			s.Selected = max(0, len(rows)-1)
			s.Prefix = false
		case "first-prefix":
			if s.Prefix {
				s.Selected = 0
				s.Prefix = false
			} else {
				s.Prefix = true
			}
		default:
			s.Prefix = false
		}
		m.previewSetting()
		return nil
	}

	var cmd tea.Cmd
	s.Filter, cmd = s.Filter.Update(k)
	s.Selected = 0
	s.Error = ""
	m.previewSetting()
	return cmd
}

func (m *Model) resetDetail() string {
	s := m.Settings
	v, _ := config.Get(config.Defaults(m.Config.Path), s.Key)
	source := "default"
	if s.Snapshot.Sources[s.Key] != "file" && s.Snapshot.Sources[s.Key] != "default" {
		v, _ = config.Get(s.Snapshot.Effective, s.Key)
		source = s.Snapshot.Sources[s.Key]
	}
	return "Will inherit: " + config.ValueText(v) + " · " + source
}
func (m *Model) settingDetail(key string) []string {
	s := m.Settings
	if key == "@render" {
		return []string{"Resolved TOML under this launch's overrides", "Active session differences are listed separately"}
	}
	d := settingFor(key)
	stored := "not set"
	if v := s.Snapshot.Stored[key]; v.Present {
		stored = config.ValueText(v.Value)
	}
	value, _ := config.Get(s.Snapshot.Effective, key)
	active, _ := config.Get(m.Config, key)
	first := key + " · Source: " + s.Snapshot.Sources[key] + " · Saved: " + stored
	second := d.Timing + " · " + d.Help
	if config.ValueText(value) != config.ValueText(active) {
		second = d.Timing + " · Active: " + config.ValueText(active) + " · Resolved: " + config.ValueText(value)
	}
	if key == "tui.editor_mode" && config.ValueText(value) == "ask" {
		second = "This session: " + m.Flavor + " · Ask on next launch"
	}
	return []string{first, second}
}
func (m *Model) effectiveLines() []string {
	b, err := toml.Marshal(m.Settings.Snapshot.Effective)
	if err != nil {
		return []string{err.Error()}
	}
	lines := []string{"Resolved for next launch under the current overrides:"}
	lines = append(lines, strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")...)
	lines = append(lines, "", "Active session differences:")
	count := 0
	for _, d := range config.Settings() {
		active, _ := config.Get(m.Config, d.Key)
		resolved, _ := config.Get(m.Settings.Snapshot.Effective, d.Key)
		if config.ValueText(active) != config.ValueText(resolved) {
			lines = append(lines, fmt.Sprintf("%s: active %s; resolved %s", d.Key, config.ValueText(active), config.ValueText(resolved)))
			count++
		}
	}
	if m.Settings.Snapshot.Effective.TUI.EditorMode == "ask" {
		lines = append(lines, "Editor keys this session: "+m.Flavor)
		count++
	}
	if count == 0 {
		lines = append(lines, "(none)")
	}
	return lines
}
