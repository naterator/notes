package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/naterator/notes/internal/theme"
)

type Setting struct {
	Key, Label, Group, Help, Kind, Timing string
	Options                               []string
}

func Settings() []Setting {
	return []Setting{
		{"theme", "Theme", "Appearance", "Choose a color scheme", "theme", "Applies immediately; launch overrides remain active", theme.Names()},
		{"color", "Color output", "Appearance", "auto respects NO_COLOR and TERM; always enables color; never disables it", "enum", "Applies immediately; --no-color wins", []string{"auto", "always", "never"}},
		{"tui.editor_mode", "Editor keys", "Editing", "Vim or Traditional; ask selects keys on the next launch", "enum", "Switches to command mode; ask applies next launch", []string{"vim", "traditional", "ask"}},
		{"tui.autosave_delay", "Autosave delay", "Editing", "Positive duration, for example 1s or 500ms", "duration", "Next autosave tick", nil},
		{"editor", "External editor", "Editing", `JSON argv, e.g. ["vim", "-n"]; [] uses VISUAL, EDITOR, then the note TUI`, "array", "Next external-editor invocation", nil},
		{"default_category", "Default category", "Notes and journal", "Relative category, e.g. misc or projects/work; no hidden or parent components", "string", "Next New note dialog", nil},
		{"journal.timezone", "Journal timezone", "Notes and journal", "local or an IANA zone such as America/Chicago", "string", "Next journal action", nil},
		{"git.auto_sync", "Automatic sync", "Git sync", "Enable periodic Git sync while Notes is running", "bool", "After closing settings; in-flight work finishes", []string{"true", "false"}},
		{"git.max_interval", "Maximum sync interval", "Git sync", "Positive duration, for example 5m", "duration", "Next automatic due check", nil},
		{"git.timeout", "Git timeout", "Git sync", "Positive duration, for example 30s", "duration", "Next Git operation", nil},
		{"repo", "Notes repository", "Advanced", "Path; relative paths resolve beside this config file. No notes are moved", "path", "Next launch; active repository stays open", nil},
		{"update.repository", "Update repository", "Advanced", "GitHub owner/repo, e.g. naterator/notes", "string", "Next notes update invocation", nil},
	}
}

type Override struct {
	Key    string
	Value  any
	Source string
}

func EnvironmentOverrides() []Override {
	var out []Override
	for _, pair := range [][2]string{{"NOTES_REPO", "repo"}, {"NOTES_THEME", "theme"}, {"NOTES_COLOR", "color"}} {
		if v, ok := os.LookupEnv(pair[0]); ok {
			out = append(out, Override{pair[1], v, "env " + pair[0]})
		}
	}
	return out
}
func (c *Config) OverrideFrom(key string, value any, source string) error {
	if err := c.Override(key, value); err != nil {
		return err
	}
	c.Launch = append(c.Launch, Override{key, value, source})
	return nil
}

type StoredValue struct {
	Present bool
	Value   any
}
type Snapshot struct {
	File, Effective Config
	Stored          map[string]StoredValue
	Sources         map[string]string
}

var ErrChanged = errors.New("setting changed outside Notes; reload and edit again")

func Inspect(path string, overrides []Override) (Snapshot, error) {
	if path == "" {
		path = SelectedPath("")
	}
	raw, err := readRaw(path)
	if err != nil {
		return Snapshot{Effective: Defaults(path)}, fmt.Errorf("config %s: %w", path, err)
	}
	return inspectRaw(path, raw, overrides)
}
func fileConfig(path string, raw map[string]any) (Config, error) {
	c := Defaults(path)
	for key, value := range flatten(raw, "") {
		if err := c.apply(key, value, filepath.Dir(path)); err != nil {
			return c, fmt.Errorf("config %s: %w", path, err)
		}
	}
	return c, nil
}
func inspectRaw(path string, raw map[string]any, overrides []Override) (Snapshot, error) {
	c, err := fileConfig(path, raw)
	s := Snapshot{File: c, Effective: c, Stored: map[string]StoredValue{}, Sources: map[string]string{}}
	if err != nil {
		return s, err
	}
	flat := flatten(raw, "")
	for _, setting := range Settings() {
		v, ok := flat[setting.Key]
		s.Stored[setting.Key] = StoredValue{ok, v}
		s.Sources[setting.Key] = "default"
		if ok {
			s.Sources[setting.Key] = "file"
		}
	}
	for _, override := range overrides {
		if err := s.Effective.OverrideFrom(override.Key, override.Value, override.Source); err != nil {
			return s, fmt.Errorf("%s: %w", override.Source, err)
		}
		s.Sources[override.Key] = override.Source
	}
	return s, nil
}

// ValidateText shares the same value parser and file-relative validation as Set.
func ValidateText(path, key, text string) error {
	v, err := ParseValue(key, text)
	if err != nil {
		return err
	}
	c := Defaults(path)
	return c.apply(key, v, filepath.Dir(path))
}
func ValueText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Change updates one saved key. A nil expectation retains CLI last-write behavior.
// The snapshot is derived before rename so a committed save cannot be mistaken
// for a failed write due to a later reread.
func Change(path, key, text string, unset bool, expected *StoredValue, overrides []Override) (Snapshot, error) {
	if path == "" {
		path = SelectedPath("")
	}
	var result Snapshot
	c := Defaults(path)
	if err := c.apply(key, defaultValue(c, key), filepath.Dir(path)); err != nil {
		return result, err
	}
	value, err := ParseValue(key, text)
	if !unset && err != nil {
		return result, err
	}
	if !unset {
		if err = c.apply(key, value, filepath.Dir(path)); err != nil {
			return result, err
		}
	}
	err = withLock(path, func() error {
		raw, err := readRaw(path)
		if err != nil {
			return err
		}
		if _, err = fileConfig(path, raw); err != nil {
			return err
		}
		old, present := flatten(raw, "")[key]
		if expected != nil && (expected.Present != present || !reflect.DeepEqual(expected.Value, old)) {
			return ErrChanged
		}
		changed := false
		if unset {
			changed = drop(raw, key)
		} else {
			changed = !present || !reflect.DeepEqual(old, value)
			put(raw, key, value)
		}
		result, err = inspectRaw(path, raw, overrides)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		return writeRaw(path, raw)
	})
	return result, err
}
