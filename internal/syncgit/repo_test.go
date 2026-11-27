package syncgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/naterator/notes"
)

func TestCheckRepoIsReadOnlyAndDoesNotAcceptAnAncestor(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, "init", "-b", "main", parent)
	repo := filepath.Join(parent, "notes")
	client := Client{Store: store.New(repo, filepath.Join(base, "state"))}
	err := client.CheckRepo(context.Background())
	if !errors.Is(err, ErrNoRepo) {
		t.Fatalf("ancestor checkout should not satisfy CheckRepo: %v", err)
	}
	if _, statErr := os.Stat(repo); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("CheckRepo created or changed the missing repo path: %v", statErr)
	}
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRepo(t.Context()); !errors.Is(err, ErrNoRepo) {
		t.Fatalf("a notebook directory inside another repo should need setup: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CheckRepo created Git metadata: %v", err)
	}
	if _, err := os.Lstat(client.Store.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CheckRepo created application state: %v", err)
	}
}

func TestEnsureRepoInitializesAndCheckRepoIsIdempotent(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	repo := filepath.Join(base, "notes")
	client := Client{Store: store.New(repo, filepath.Join(base, "state"))}
	if err := client.EnsureRepo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRepo(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := git(t, "-C", repo, "rev-parse", "--show-toplevel")
	resolvedGot, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedGot != resolvedRepo {
		t.Fatalf("initialized root %q, want %q", resolvedGot, resolvedRepo)
	}
	if err := client.EnsureRepo(context.Background()); err != nil {
		t.Fatalf("second EnsureRepo failed: %v", err)
	}
}

func TestCheckRepoRejectsInvalidAndBareRepositories(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	invalid := filepath.Join(base, "invalid")
	if err := os.MkdirAll(filepath.Join(invalid, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalid, ".git", "HEAD"), []byte("garbage\n"), 0600); err != nil {
		t.Fatal(err)
	}
	invalidClient := Client{Store: store.New(invalid, filepath.Join(base, "invalid-state"))}
	if err := invalidClient.CheckRepo(context.Background()); err == nil || errors.Is(err, ErrNoRepo) {
		t.Fatalf("invalid .git metadata was classified as missing: %v", err)
	}
	if err := invalidClient.EnsureRepo(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid Git metadata") {
		t.Fatalf("EnsureRepo repaired invalid existing metadata: %v", err)
	}

	bare := filepath.Join(base, "bare")
	git(t, "init", "--bare", bare)
	bareClient := Client{Store: store.New(bare, filepath.Join(base, "bare-state"))}
	if err := bareClient.CheckRepo(context.Background()); err == nil || errors.Is(err, ErrNoRepo) || !strings.Contains(err.Error(), "bare Git repository") {
		t.Fatalf("bare repository was not rejected explicitly: %v", err)
	}
}

func TestCheckRepoAcceptsGitWorktreeMarkerAndSymlinkedPath(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	actual := filepath.Join(base, "actual")
	git(t, "init", "-b", "main", actual)
	git(t, "-C", actual, "-c", "user.name=Notes", "-c", "user.email=notes@example.invalid", "commit", "--allow-empty", "-m", "Start")
	worktree := filepath.Join(base, "worktree")
	git(t, "-C", actual, "worktree", "add", "-b", "notes-worktree", worktree)
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(worktree, alias); err != nil {
		t.Fatal(err)
	}
	client := Client{Store: store.New(alias, filepath.Join(base, "state"))}
	if err := client.CheckRepo(t.Context()); err != nil {
		t.Fatalf("symlink to a linked worktree was rejected: %v", err)
	}
}
