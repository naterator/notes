package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/naterator/notes/internal/theme"
)

func TestSettingCoverageAndThemeRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	c := Defaults(path)
	b, _ := json.Marshal(c)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	flat := flatten(raw, "")
	seen := map[string]bool{}
	for _, d := range Settings() {
		if _, ok := flat[d.Key]; !ok || seen[d.Key] {
			t.Fatal("invalid setting", d.Key)
		}
		seen[d.Key] = true
	}
	if len(seen) != len(flat) || len(seen) != 12 {
		t.Fatal("incomplete settings coverage")
	}
	for _, name := range theme.Names() {
		if err := Set(path, "theme", name); err != nil {
			t.Fatal(err)
		}
		s, err := Inspect(path, nil)
		if err != nil || s.File.Theme != name {
			t.Fatal(name, err)
		}
		t.Setenv("NOTES_THEME", name)
		loaded, err := Load(path)
		if err != nil || loaded.Theme != name {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(path)
	if err := Set(path, "theme", "unknown"); err == nil {
		t.Fatal("invalid theme accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid theme changed file")
	}
}
func TestInspectionAndConditionalChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Set(path, "repo", "./data"); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "theme", "nord"); err != nil {
		t.Fatal(err)
	}
	overrides := []Override{{"theme", "nord", "env NOTES_THEME"}, {"theme", "catppuccin-mocha", "flag --theme"}, {"color", "never", "flag --no-color"}}
	s, err := Inspect(path, overrides)
	if err != nil {
		t.Fatal(err)
	}
	if s.Sources["theme"] != "flag --theme" || s.Effective.Theme != "catppuccin-mocha" || s.File.Theme != "nord" || s.File.Repo != filepath.Join(filepath.Dir(path), "data") {
		t.Fatalf("inspection: %+v", s)
	}
	if s.Stored["color"].Present || s.Sources["color"] != "flag --no-color" {
		t.Fatal("lost override provenance")
	}
	expected := s.Stored["theme"]
	if err = Set(path, "git.timeout", "8s"); err != nil {
		t.Fatal(err)
	}
	s, err = Change(path, "theme", "gruvbox-dark", false, &expected, overrides)
	if err != nil {
		t.Fatal(err)
	}
	if s.File.Theme != "gruvbox-dark" || s.Effective.Theme != "catppuccin-mocha" || s.File.Git.Timeout != "8s" || s.Stored["color"].Present {
		t.Fatalf("mutation leaked/lost values: %+v", s)
	}
	before, _ := os.ReadFile(path)
	if _, err = Change(path, "theme", "solarized-light", false, &expected, overrides); !errors.Is(err, ErrChanged) {
		t.Fatalf("expected conflict, got %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("conflict wrote file")
	}
	expected = s.Stored["theme"]
	s, err = Change(path, "theme", "", true, &expected, overrides)
	if err != nil {
		t.Fatal(err)
	}
	if s.Stored["theme"].Present || s.Effective.Theme != "catppuccin-mocha" || s.Sources["theme"] != "flag --theme" {
		t.Fatal("incorrect reset")
	}
}
func TestSettingValidationAtomicityAndNoMaterializedDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	values := map[string]string{"editor": `["vim","-n"]`, "default_category": "work/research", "journal.timezone": "America/Chicago", "git.auto_sync": "false", "git.max_interval": "7m", "git.timeout": "5s", "tui.autosave_delay": "500ms", "tui.editor_mode": "traditional", "repo": "~/notes-test", "update.repository": "example/notes"}
	for key, value := range values {
		if err := Set(path, key, value); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	s, err := Inspect(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Stored["theme"].Present || s.Stored["color"].Present {
		t.Fatal("saved defaults unexpectedly")
	}
	before, _ := os.ReadFile(path)
	for key, value := range map[string]string{"editor": `[""]`, "default_category": "../escape", "journal.timezone": "invalid/zone", "git.max_interval": "0s", "git.timeout": "-1s", "tui.autosave_delay": "soon", "tui.editor_mode": "emacs", "repo": "", "update.repository": "https://example.invalid/notes"} {
		if err = Set(path, key, value); err == nil {
			t.Fatalf("%s accepted invalid value", key)
		}
		after, _ := os.ReadFile(path)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("failed validation modified file")
		}
	}
	if err = Unset(path, "git"); err == nil {
		t.Fatal("reset accepted a whole table")
	}
	missing := filepath.Join(t.TempDir(), "missing.toml")
	expected := StoredValue{}
	if _, err = Change(missing, "theme", "", true, &expected, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("reset created config", err)
	}
	t.Setenv("NOTES_THEME", "invalid")
	if err = Set(path, "theme", "nord"); err != nil {
		t.Fatal("file edit depended on invalid environment", err)
	}
	if err = os.WriteFile(path, []byte("unknown = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Set(path, "theme", "nord"); err == nil {
		t.Fatal("rewrote invalid file")
	}
}
