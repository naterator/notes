package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestXDGAndOverrides(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	path := DefaultPath()
	if path != filepath.Join(base, "notes", "config.toml") {
		t.Fatal(path)
	}
	if err := Set(path, "git.auto_sync", "false"); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "repo", "./relative-repo"); err != nil {
		t.Fatal(err)
	}
	fromFile, err := Load(path)
	if err != nil || fromFile.Repo != filepath.Join(filepath.Dir(path), "relative-repo") {
		t.Fatalf("relative repo path: %+v %v", fromFile, err)
	}
	t.Setenv("NOTES_REPO", "")
	if _, err := Load(path); err == nil {
		t.Fatal("empty NOTES_REPO accepted")
	}
	t.Setenv("NOTES_REPO", filepath.Join(base, "env-repo"))
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Git.AutoSync || c.Repo != filepath.Join(base, "env-repo") {
		t.Fatalf("precedence: %+v", c)
	}
}
func TestRelativeXDGValuesFallBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "relative-config")
	t.Setenv("XDG_STATE_HOME", "relative-state")
	if got := DefaultPath(); got != filepath.Join(home, ".config", "notes", "config.toml") {
		t.Fatalf("relative XDG config value was used: %q", got)
	}
	if got := DefaultStateDir(); got != filepath.Join(home, ".local", "state", "notes") {
		t.Fatalf("relative XDG state value was used: %q", got)
	}
}
func TestConfigRoundTripAndReject(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "config.toml")
	t.Setenv("NOTES_REPO", filepath.Join(base, "env-repo"))
	if err := Set(path, "tui.editor_mode", "traditional"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.TUI.EditorMode != "traditional" || c.Repo != filepath.Join(base, "env-repo") {
		t.Fatalf("config: %+v", c)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "git.max_interval", "-5m"); err == nil {
		t.Fatal("invalid duration accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid set changed file")
	}
	if err := Unset(path, "tui.editor_mode"); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.TUI.EditorMode != "ask" {
		t.Fatal(c.TUI.EditorMode)
	}
	if err = os.WriteFile(path, []byte("unknown = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); err == nil {
		t.Fatal("unknown key accepted")
	}
}
func TestUnsetMissingKeyDoesNotCreateConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings", "config.toml")
	if err := Unset(path, "theme"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unset created a config file: %v", err)
	}
	if err := os.WriteFile(path, []byte("[git\nauto_sync = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed TOML accepted")
	}
}
func TestRepositoryAndEditorModeValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, value := range []string{"owner/repo\nmore", "owner/repo%2fextra", "owner//repo"} {
		if err := Set(path, "update.repository", value); err == nil {
			t.Fatalf("invalid release coordinates accepted: %q", value)
		}
	}
	if err := Set(path, "tui.editor_mode", "unsupported"); err == nil {
		t.Fatal("unknown TUI editor mode accepted")
	}
	if err := Set(path, "journal.timezone", "invalid/place"); err == nil {
		t.Fatal("invalid timezone accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid settings created a config file: %v", err)
	}
}
