package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/store"
)

func run(t *testing.T, in string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := New(strings.NewReader(in), &out, &errOut)
	e := a.Execute(context.Background(), args)
	return out.String(), e
}

func TestTopLevelShortcuts(t *testing.T) {
	a := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	for _, pair := range [][2]string{
		{"c", "categories"}, {"ca", "categories"}, {"cat", "categories"}, {"cate", "categories"},
		{"co", "config"}, {"con", "config"}, {"conf", "config"},
		{"com", "completion"}, {"comp", "completion"}, {"compl", "completion"},
		{"d", "delete"}, {"e", "edit"}, {"f", "find"}, {"h", "help"},
		{"j", "journal"}, {"l", "list"}, {"li", "list"}, {"lis", "list"},
		{"m", "move"}, {"n", "new"}, {"s", "show"}, {"sh", "show"}, {"sho", "show"},
		{"sy", "sync"}, {"syn", "sync"}, {"ta", "tags"}, {"tag", "tags"},
		{"t", "tui"}, {"tu", "tui"}, {"u", "update"}, {"v", "version"},
	} {
		cmd, _, err := a.Root.Find([]string{pair[0]})
		if err != nil || cmd == nil {
			t.Fatalf("%q did not resolve: %v", pair[0], err)
		}
		if cmd.Name() != pair[1] {
			t.Fatalf("%q resolved to %q; want %q", pair[0], cmd.Name(), pair[1])
		}
	}
	for _, cmd := range a.Root.Commands() {
		if cmd.Hidden {
			continue
		}
		found := false
		for _, alias := range cmd.Aliases {
			if strings.HasPrefix(cmd.Name(), alias) && alias != cmd.Name() {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("top-level command %q has no prefix shortcut", cmd.Name())
		}
	}
	out, err := run(t, "", "h")
	if err != nil || !strings.Contains(out, "Available Commands") {
		t.Fatalf("help shortcut did not execute: %q, %v", out, err)
	}
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	args := []string{"--config", filepath.Join(base, "config.toml"), "--repo", filepath.Join(base, "repo")}
	path, err := run(t, "", append(args, "n", "Shortcut")...)
	if err != nil {
		t.Fatal(err)
	}
	out, err = run(t, "", append(args, "l")...)
	if err != nil || out != path {
		t.Fatalf("list shortcut output: %q, %v; want %q", out, err, path)
	}
}

func TestNestedShortcuts(t *testing.T) {
	a := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	for _, tc := range []struct{ parent, shortcut, command string }{
		{"co", "r", "render"}, {"co", "rend", "render"},
		{"co", "p", "path"}, {"co", "g", "get"},
		{"co", "s", "set"}, {"co", "u", "unset"}, {"co", "e", "edit"},
		{"sy", "s", "status"}, {"ta", "a", "add"},
		{"ta", "rem", "remove"}, {"j", "a", "add"},
		{"comp", "f", "fish"},
	} {
		cmd, args, err := a.Root.Find([]string{tc.parent, tc.shortcut})
		if err != nil || cmd == nil || cmd.Name() != tc.command || len(args) != 0 {
			t.Fatalf("%s %s resolved to %v with %v: %v", tc.parent, tc.shortcut, cmd, args, err)
		}
	}

	base := t.TempDir()
	configPath := filepath.Join(base, "config.toml")
	out, err := run(t, "", "--config", configPath, "co", "rend", "--json")
	if err != nil || !json.Valid([]byte(out)) {
		t.Fatalf("config render shortcut: %q, %v", out, err)
	}
	out, err = run(t, "", "--config", configPath, "co", "r")
	if err != nil || !strings.Contains(out, "default_category") {
		t.Fatalf("shortest config render shortcut: %q, %v", out, err)
	}
	for _, args := range [][]string{{"co", "renderd"}, {"ta", "removee"}, {"j", "addd"}, {"sy", "statuss"}} {
		_, err := run(t, "", args...)
		if ExitCode(err) != 2 || !strings.Contains(err.Error(), "unknown subcommand") {
			t.Fatalf("%v: expected unknown subcommand error, got %v", args, err)
		}
	}
}

func TestEditFallsBackToTUIWithoutExternalEditor(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	configPath := filepath.Join(base, "config.toml")
	repo := filepath.Join(base, "repo")
	path, err := run(t, "", "--config", configPath, "--repo", repo, "j")
	if err != nil {
		t.Fatal(err)
	}
	_, err = run(t, "", "--config", configPath, "--repo", repo, "e", strings.TrimSpace(path))
	if err == nil || !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("edit did not select TUI without an external editor: %v", err)
	}
}

func TestCLIFilePipeline(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	configPath := filepath.Join(base, "config.toml")
	repo := filepath.Join(base, "repo")
	if _, e := run(t, "", "--config", configPath, "config", "set", "git.auto_sync", "false"); e != nil {
		t.Fatal(e)
	}
	args := []string{"--config", configPath, "--repo", repo}
	newOut, e := run(t, "", append(args, "new", "Release checklist", "--category", "work", "--tag", "release")...)
	if e != nil {
		t.Fatal(e)
	}
	path := strings.TrimSpace(newOut)
	if !strings.HasPrefix(path, repo) {
		t.Fatal(path)
	}
	if _, e := run(t, "", append(args, "tags", "add", path, "urgent")...); e != nil {
		t.Fatal(e)
	}
	out, e := run(t, "", append(args, "find", "release", "--tag", "urgent", "--json")...)
	if e != nil {
		t.Fatal(e)
	}
	var results []map[string]any
	if e = json.Unmarshal([]byte(out), &results); e != nil || len(results) != 1 {
		t.Fatalf("find: %s %v", out, e)
	}
	out, e = run(t, "", append(args, "list", "--print0", "--relative")...)
	if e != nil || !strings.HasSuffix(out, "\x00") || strings.Contains(out, repo) {
		t.Fatalf("NUL output: %q %v", out, e)
	}
	t.Setenv("NO_COLOR", "1")
	out, e = run(t, "", append(args, "--color", "always", "list")...)
	if e != nil || strings.Contains(out, "\x1b[") {
		t.Fatalf("path output gained ANSI: %q %v", out, e)
	}
	out, e = run(t, "", append(args, "--color", "always", "list", "--long")...)
	if e != nil || !strings.Contains(out, "\x1b[") {
		t.Fatalf("human color missing: %q %v", out, e)
	}
	spaced, e := run(t, "", append(args, "new", "Spaced", "--path", "work/a spaced note.md")...)
	if e != nil {
		t.Fatal(e)
	}
	out, e = run(t, "", append(args, "find", "spaced", "--relative", "--print0")...)
	if e != nil || out != "work/a spaced note.md\x00" {
		t.Fatalf("spaced NUL path: %q %v", out, e)
	}
	if _, e = run(t, "", append(args, "find", "(", "--regex")...); ExitCode(e) != 2 {
		t.Fatalf("malformed regex exit: %v", e)
	}
	if _, e = run(t, "", append(args, "delete", strings.TrimSpace(spaced))...); ExitCode(e) != 2 {
		t.Fatalf("non-interactive delete exit: %v", e)
	}
	if _, e = run(t, "", append(args, "delete", strings.TrimSpace(spaced), "--yes")...); e != nil {
		t.Fatal(e)
	}
	moved, e := run(t, "", append(args, "move", path, "archive/release.md")...)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = run(t, "", append(args, "delete", strings.TrimSpace(moved), "--yes")...); e != nil {
		t.Fatal(e)
	}
	out, e = run(t, "", append(args, "list", "--json")...)
	if e != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty list: %s %v", out, e)
	}
}
func TestPipedNotesCanExceedTUIEditorLimit(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	configPath := filepath.Join(base, "config.toml")
	repo := filepath.Join(base, "repo")
	if _, err := run(t, "", "--config", configPath, "config", "set", "git.auto_sync", "false"); err != nil {
		t.Fatal(err)
	}
	args := []string{"--config", configPath, "--repo", repo}
	large := strings.Repeat(strings.Repeat("a", 1023)+"\n", store.MaxEditable/1024+1)
	noteBody := large + "unique-large-note-marker\n"
	if out, err := run(t, noteBody, append(args, "new", "Large", "--path", "large.md", "--stdin")...); err != nil || strings.TrimSpace(out) != filepath.Join(repo, "large.md") {
		t.Fatalf("new with large stdin: %q, %v", out, err)
	}
	if out, err := run(t, "", append(args, "show", "large.md")...); err != nil || out != "# Large\n\n"+noteBody {
		t.Fatalf("large note changed: length %d, %v", len(out), err)
	}
	if out, err := run(t, "", append(args, "find", "unique-large-note-marker", "--relative")...); err != nil || out != "large.md\n" {
		t.Fatalf("large note not searchable: %q, %v", out, err)
	}
	journalBody := large + "unique-large-journal-marker\n"
	if out, err := run(t, journalBody, append(args, "journal", "add", "--date", "2026-09-28", "--section", "actions", "--stdin")...); err != nil || strings.TrimSpace(out) != filepath.Join(repo, "journal", "2026", "09", "2026-09-28.md") {
		t.Fatalf("journal with large stdin: %q, %v", out, err)
	}
	if out, err := run(t, "", append(args, "show", "journal/2026/09/2026-09-28.md")...); err != nil || !strings.Contains(out, "  unique-large-journal-marker\n") || len(out) <= store.MaxEditable {
		t.Fatalf("large journal changed: length %d, %v", len(out), err)
	}
}
func TestFirstUseInitializesLocalRepo(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	repo := filepath.Join(base, "repo")
	out, e := run(t, "", "--config", filepath.Join(base, "missing.toml"), "--repo", repo, "new", "Offline note")
	if e != nil {
		t.Fatalf("initial local note failed: %v", e)
	}
	if _, e = os.Stat(strings.TrimSpace(out)); e != nil {
		t.Fatalf("note lost: %v", e)
	}
	if _, e = os.Stat(filepath.Join(repo, ".git")); e != nil {
		t.Fatalf("notes data repo was not initialized: %v", e)
	}
	if _, e = run(t, "", "--config", filepath.Join(base, "missing.toml"), "--repo", repo, "sync"); e == nil || !strings.Contains(e.Error(), "origin remote is required") {
		t.Fatalf("manual sync without origin: %v", e)
	}
}
func TestNoOpTagEditDoesNotAttemptGit(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	configPath := filepath.Join(base, "config.toml")
	repo := filepath.Join(base, "repo")
	// Creation saves the note locally and initializes the repo without a remote.
	path, err := run(t, "", "--config", configPath, "--repo", repo, "new", "Tagged", "--tag", "work")
	if err != nil {
		t.Fatalf("local creation: %v", err)
	}
	id := strings.TrimSpace(path)
	if out, err := run(t, "", "--config", configPath, "--repo", repo, "tags", "add", id, "work"); err != nil || out != "" {
		t.Fatalf("no-op tag edit attempted sync or wrote output: %q, %v", out, err)
	}
	if out, err := run(t, "", "--config", configPath, "--repo", repo, "tags", "remove", id, "absent"); err != nil || out != "" {
		t.Fatalf("no-op tag removal attempted sync or wrote output: %q, %v", out, err)
	}
}
func TestNewlineNamedExistingNotesRequireSafeOutput(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	configPath := filepath.Join(base, "config.toml")
	repo := filepath.Join(base, "repo")
	if _, err := run(t, "", "--config", configPath, "config", "set", "git.auto_sync", "false"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.md"), []byte("# A\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id := "z\nname.md"
	if err := os.WriteFile(filepath.Join(repo, id), []byte("# Z\n"), 0600); err != nil {
		t.Fatal(err)
	}
	unsafeCategory := filepath.Join(repo, "bad\ncategory")
	if err := os.Mkdir(unsafeCategory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unsafeCategory, "nested.md"), []byte("# Nested\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--config", configPath, "--repo", repo}
	if out, err := run(t, "", append(args, "list")...); ExitCode(err) != 2 || out != "" {
		t.Fatalf("line output was partial or accepted newline path: %q, %v", out, err)
	}
	if out, err := run(t, "", append(args, "list", "--relative", "--print0")...); err != nil || out != "a.md\x00bad\ncategory/nested.md\x00"+id+"\x00" {
		t.Fatalf("NUL output lost newline path: %q, %v", out, err)
	}
	if out, err := run(t, "", append(args, "categories")...); ExitCode(err) != 2 || out != "" {
		t.Fatalf("categories emitted ambiguous partial output: %q, %v", out, err)
	}
	if out, err := run(t, "", append(args, "new", "bad", "--path", "bad\nname.md")...); err == nil || out != "" {
		t.Fatalf("created ambiguous path: %q, %v", out, err)
	}
	if out, err := run(t, "", append(args, "new", "bad", "--category", "bad\ncat")...); err == nil || out != "" {
		t.Fatalf("created ambiguous category: %q, %v", out, err)
	}
	if out, err := run(t, "", append(args, "move", "a.md", "bad\nname.md")...); err == nil || out != "" {
		t.Fatalf("moved to ambiguous path: %q, %v", out, err)
	}
}
func TestCharacterDeviceIsNotAnInteractiveTerminal(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	configPath := filepath.Join(base, "config.toml")
	repo := filepath.Join(base, "repo")
	if _, err := run(t, "", "--config", configPath, "config", "set", "git.auto_sync", "false"); err != nil {
		t.Fatal(err)
	}
	path, err := run(t, "", "--config", configPath, "--repo", repo, "new", "Keep", "--path", "keep.md")
	if err != nil {
		t.Fatal(err)
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	var out, errOut bytes.Buffer
	app := New(null, &out, &errOut)
	if err := app.Execute(context.Background(), []string{"--config", configPath, "--repo", repo, "delete", "keep.md"}); ExitCode(err) != 2 {
		t.Fatalf("delete accepted non-TTY character device: %v", err)
	}
	if _, err := os.Stat(strings.TrimSpace(path)); err != nil {
		t.Fatalf("note was deleted without interactive confirmation: %v", err)
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	if colored := humanColor(null, config.Defaults(configPath), "accent", "plain"); colored != "plain" {
		t.Fatalf("automatic color treated /dev/null as TTY: %q", colored)
	}
}
func TestFailedExternalEditorLeavesItsChanges(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	configPath := filepath.Join(base, "config.toml")
	repo := filepath.Join(base, "repo")
	if _, err := run(t, "", "--config", configPath, "config", "set", "git.auto_sync", "false"); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(base, "edit-and-fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '\\neditor changed\\n' >> \"$1\"\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal([]string{script})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "", "--config", configPath, "config", "set", "editor", string(args)); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "", "--config", configPath, "--repo", repo, "new", "Test", "--path", "test.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "", "--config", configPath, "--repo", repo, "edit", "test.md"); ExitCode(err) != 1 {
		t.Fatalf("failed external editor was reported as success: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(repo, "test.md"))
	if err != nil || !bytes.Contains(content, []byte("editor changed")) {
		t.Fatalf("external editor changes were lost: %q, %v", content, err)
	}
}

func TestThemeCompletionAndLaunchContext(t *testing.T) {
	for _, args := range [][]string{{"__complete", "--theme", ""}, {"__complete", "co", "s", "theme", ""}} {
		out, err := run(t, "", args...)
		if err != nil || !strings.Contains(out, "catppuccin-frappe") || !strings.Contains(out, "solarized-light") {
			t.Fatalf("theme completion: %q %v", out, err)
		}
	}
	base := t.TempDir()
	path := filepath.Join(base, "config.toml")
	if err := config.Set(path, "theme", "gruvbox-dark"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTES_THEME", "catppuccin-mocha")
	var out, errOut bytes.Buffer
	a := New(strings.NewReader(""), &out, &errOut)
	if err := a.Execute(context.Background(), []string{"--config", path, "--theme", "nord", "--color", "always", "--no-color", "co", "rend", "--json"}); err != nil {
		t.Fatal(err)
	}
	c, err := a.config()
	if err != nil {
		t.Fatal(err)
	}
	s, err := config.Inspect(path, c.Launch)
	if err != nil || s.Effective.Theme != "nord" || s.Effective.Color != "never" || s.Sources["theme"] != "flag --theme" || s.Sources["color"] != "flag --no-color" || s.File.Theme != "gruvbox-dark" {
		t.Fatalf("launch context: %+v %v", s, err)
	}
	if strings.Contains(out.String(), "Launch") || strings.Contains(out.String(), "Source") {
		t.Fatal("render leaked internal launch metadata")
	}
}
