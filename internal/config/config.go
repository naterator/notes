package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/naterator/notes/internal/theme"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Repo            string     `toml:"repo" json:"repo"`
	Editor          []string   `toml:"editor" json:"editor"`
	DefaultCategory string     `toml:"default_category" json:"default_category"`
	Theme           string     `toml:"theme" json:"theme"`
	Color           string     `toml:"color" json:"color"`
	Journal         Journal    `toml:"journal" json:"journal"`
	Git             Git        `toml:"git" json:"git"`
	TUI             TUI        `toml:"tui" json:"tui"`
	Update          Update     `toml:"update" json:"update"`
	Path            string     `toml:"-" json:"-"`
	StateDir        string     `toml:"-" json:"-"`
	Launch          []Override `toml:"-" json:"-"`
}
type Journal struct {
	Timezone string `toml:"timezone" json:"timezone"`
}
type Git struct {
	AutoSync    bool   `toml:"auto_sync" json:"auto_sync"`
	MaxInterval string `toml:"max_interval" json:"max_interval"`
	Timeout     string `toml:"timeout" json:"timeout"`
}
type TUI struct {
	AutosaveDelay string `toml:"autosave_delay" json:"autosave_delay"`
	EditorMode    string `toml:"editor_mode" json:"editor_mode"`
}
type Update struct {
	Repository string `toml:"repository" json:"repository"`
}

var githubRepository = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func xdg(name, fallback string) string {
	v := os.Getenv(name)
	if filepath.IsAbs(v) {
		return v
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, fallback)
}

func DefaultPath() string {
	return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "notes", "config.toml")
}
func DefaultStateDir() string { return filepath.Join(xdg("XDG_STATE_HOME", ".local/state"), "notes") }
func SelectedPath(flag string) string {
	if flag != "" {
		return absExpand(flag, "")
	}
	if v := os.Getenv("NOTES_CONFIG"); v != "" {
		return absExpand(v, "")
	}
	return DefaultPath()
}
func absExpand(s, base string) string {
	if s == "~" || strings.HasPrefix(s, "~/") {
		h, _ := os.UserHomeDir()
		s = filepath.Join(h, strings.TrimPrefix(s, "~/"))
	}
	if !filepath.IsAbs(s) {
		if base == "" {
			base, _ = os.Getwd()
		}
		s = filepath.Join(base, s)
	}
	a, _ := filepath.Abs(s)
	return a
}
func Defaults(path string) Config {
	return Config{
		Repo: filepath.Join(filepath.Dir(DefaultPath()), "repo"), Editor: []string{}, DefaultCategory: "misc", Theme: "catppuccin-mocha", Color: "auto",
		Journal: Journal{Timezone: "local"}, Git: Git{AutoSync: true, MaxInterval: "5m", Timeout: "30s"},
		TUI: TUI{AutosaveDelay: "1s", EditorMode: "ask"}, Update: Update{Repository: "naterator/notes"},
		Path: path, StateDir: DefaultStateDir(),
	}
}

func Load(path string) (Config, error) {
	snapshot, err := Inspect(path, EnvironmentOverrides())
	return snapshot.Effective, err
}
func flatten(m map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok {
			for ck, cv := range flatten(child, key) {
				out[ck] = cv
			}
		} else {
			out[key] = v
		}
	}
	return out
}
func duration(v string) error {
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fmt.Errorf("invalid positive duration %q", v)
	}
	return nil
}
func normalizeCategory(v string) error {
	if v == "" || filepath.IsAbs(v) || v == "." || v == ".." || strings.Contains(v, "\\") {
		return fmt.Errorf("invalid category %q", v)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return fmt.Errorf("invalid category %q", v)
		}
	}
	for _, p := range strings.Split(filepath.ToSlash(v), "/") {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") {
			return fmt.Errorf("invalid category %q", v)
		}
	}
	return nil
}
func (c *Config) apply(k string, value any, base string) error {
	str, _ := value.(string)
	switch k {
	case "repo":
		if str == "" {
			return fmt.Errorf("repo must be a path")
		}
		c.Repo = absExpand(str, base)
	case "editor":
		a, ok := value.([]any)
		if !ok {
			return fmt.Errorf("editor must be an array of strings")
		}
		c.Editor = []string{}
		for _, v := range a {
			s, ok := v.(string)
			if !ok || s == "" {
				return fmt.Errorf("editor must be an array of nonempty strings")
			}
			c.Editor = append(c.Editor, s)
		}
	case "default_category":
		if err := normalizeCategory(str); err != nil {
			return err
		}
		c.DefaultCategory = str
	case "theme":
		if _, ok := theme.Lookup(str); !ok {
			return fmt.Errorf("unknown theme %q (choose %s)", str, strings.Join(theme.Names(), ", "))
		}
		c.Theme = str
	case "color":
		if str != "auto" && str != "always" && str != "never" {
			return fmt.Errorf("invalid color %q", str)
		}
		c.Color = str
	case "journal.timezone":
		if str != "local" {
			if _, err := time.LoadLocation(str); err != nil {
				return fmt.Errorf("invalid timezone %q", str)
			}
		}
		c.Journal.Timezone = str
	case "git.auto_sync":
		b, ok := value.(bool)
		if !ok {
			return fmt.Errorf("git.auto_sync must be boolean")
		}
		c.Git.AutoSync = b
	case "git.max_interval":
		if err := duration(str); err != nil {
			return err
		}
		c.Git.MaxInterval = str
	case "git.timeout":
		if err := duration(str); err != nil {
			return err
		}
		c.Git.Timeout = str
	case "tui.autosave_delay":
		if err := duration(str); err != nil {
			return err
		}
		c.TUI.AutosaveDelay = str
	case "tui.editor_mode":
		if str != "ask" && str != "vim" && str != "traditional" {
			return fmt.Errorf("invalid tui.editor_mode %q", str)
		}
		c.TUI.EditorMode = str
	case "update.repository":
		if !githubRepository.MatchString(str) {
			return fmt.Errorf("invalid GitHub repository %q", str)
		}
		c.Update.Repository = str
	default:
		return fmt.Errorf("unknown key %q", k)
	}
	return nil
}
func (c *Config) Override(key string, value any) error { return c.apply(key, value, "") }

func ParseValue(key, text string) (any, error) {
	switch key {
	case "git.auto_sync":
		return strconv.ParseBool(text)
	case "editor":
		var a []string
		if err := json.Unmarshal([]byte(text), &a); err != nil {
			return nil, err
		}
		out := make([]any, len(a))
		for i, v := range a {
			out[i] = v
		}
		return out, nil
	default:
		return text, nil
	}
}
func Set(path, key, text string) error {
	_, err := Change(path, key, text, false, nil, nil)
	return err
}
func Unset(path, key string) error {
	_, err := Change(path, key, "", true, nil, nil)
	return err
}
func withLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(path), ".config.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
func defaultValue(c Config, key string) any {
	switch key {
	case "repo":
		return c.Repo
	case "editor":
		return []any{}
	case "default_category":
		return c.DefaultCategory
	case "theme":
		return c.Theme
	case "color":
		return c.Color
	case "journal.timezone":
		return c.Journal.Timezone
	case "git.auto_sync":
		return c.Git.AutoSync
	case "git.max_interval":
		return c.Git.MaxInterval
	case "git.timeout":
		return c.Git.Timeout
	case "tui.autosave_delay":
		return c.TUI.AutosaveDelay
	case "tui.editor_mode":
		return c.TUI.EditorMode
	case "update.repository":
		return c.Update.Repository
	}
	return nil
}
func readRaw(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err = toml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
func put(m map[string]any, k string, v any) {
	p := strings.Split(k, ".")
	for _, s := range p[:len(p)-1] {
		child, ok := m[s].(map[string]any)
		if !ok {
			child = map[string]any{}
			m[s] = child
		}
		m = child
	}
	m[p[len(p)-1]] = v
}
func drop(m map[string]any, k string) bool {
	p := strings.Split(k, ".")
	for _, s := range p[:len(p)-1] {
		child, ok := m[s].(map[string]any)
		if !ok {
			return false
		}
		m = child
	}
	if _, ok := m[p[len(p)-1]]; !ok {
		return false
	}
	delete(m, p[len(p)-1])
	return true
}
func writeRaw(path string, m map[string]any) error {
	b, err := toml.Marshal(m)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func Get(c Config, key string) (any, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	p := strings.Split(key, ".")
	var v any = m
	for _, s := range p {
		child, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unknown key %q", key)
		}
		v, ok = child[s]
		if !ok {
			return nil, fmt.Errorf("unknown key %q", key)
		}
	}
	return v, nil
}
