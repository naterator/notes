package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/naterator/notes"
)

func TestConfirmRepositoryDeclinesWithoutWriting(t *testing.T) {
	base := t.TempDir()
	s := store.New(filepath.Join(base, "repo"), filepath.Join(base, "state"))
	var output bytes.Buffer
	ready, err := confirmRepository(context.Background(), s, strings.NewReader("n\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("declining repository setup opened the TUI")
	}
	if _, err := os.Stat(filepath.Join(s.Repo, ".git")); !os.IsNotExist(err) {
		t.Fatalf("declining setup created a repository: %v", err)
	}
	if !strings.Contains(output.String(), "Initialize a Notes repository here? [y/N]") {
		t.Fatalf("prompt missing: %q", output.String())
	}
}

func TestConfirmRepositoryAcceptsYesAndDeclinesEOFAnswer(t *testing.T) {
	base := t.TempDir()
	s := store.New(filepath.Join(base, "repo"), filepath.Join(base, "state"))
	var output bytes.Buffer
	ready, err := confirmRepository(context.Background(), s, strings.NewReader("yes\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("accepting repository setup did not continue to the TUI")
	}
	if _, err := os.Stat(filepath.Join(s.Repo, ".git")); err != nil {
		t.Fatalf("accepting setup did not initialize Git: %v", err)
	}

	base = t.TempDir()
	s = store.New(filepath.Join(base, "repo"), filepath.Join(base, "state"))
	output.Reset()
	ready, err = confirmRepository(context.Background(), s, strings.NewReader("y"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("partial affirmative at EOF initialized a repository")
	}
	if _, err := os.Stat(filepath.Join(s.Repo, ".git")); !os.IsNotExist(err) {
		t.Fatalf("partial affirmative at EOF created a repository: %v", err)
	}
}
