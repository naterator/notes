package syncgit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/naterator/notes/internal/store"
)

func git(t *testing.T, args ...string) string {
	t.Helper()
	b, e := exec.Command("git", args...).CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v: %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func gitIsolation(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
}
func clone(t *testing.T, base, remote, name string) Client {
	t.Helper()
	dir := filepath.Join(base, name)
	git(t, "clone", remote, dir)
	git(t, "-C", dir, "config", "user.name", "Notes Test")
	git(t, "-C", dir, "config", "user.email", "notes@example.invalid")
	return Client{Store: store.New(dir, filepath.Join(base, name+"-state")), Timeout: 5 * time.Second}
}
func TestSyncTwoClonesAndOfflineCheckpoint(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	a := clone(t, base, remote, "a")
	ctx := context.Background()
	if _, e := a.Store.Create(store.NewOptions{Title: "one", Path: "one.md"}); e != nil {
		t.Fatal(e)
	}
	if e := a.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	if status, e := a.Status(ctx); e != nil || status.LastCommit != git(t, "-C", a.Store.Repo, "rev-parse", "HEAD") || status.LastSync == "" {
		t.Fatalf("successful sync state: %+v, %v", status, e)
	}
	b := clone(t, base, remote, "b")
	if _, e := b.Store.Create(store.NewOptions{Title: "two", Path: "two.md"}); e != nil {
		t.Fatal(e)
	}
	if e := b.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	git(t, "-C", a.Store.Repo, "fetch", "origin")
	git(t, "-C", a.Store.Repo, "remote", "set-url", "origin", filepath.Join(base, "unreachable.git"))
	if status, e := a.Status(ctx); e != nil || status.Ahead != 0 || status.Behind != 1 {
		t.Fatalf("status did not use last fetched remote ref: %+v, %v", status, e)
	}
	git(t, "-C", a.Store.Repo, "remote", "set-url", "origin", remote)
	if e := a.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(a.Store.Repo, "two.md")); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Store.Create(store.NewOptions{Title: "offline", Path: "offline.md"}); e != nil {
		t.Fatal(e)
	}
	git(t, "-C", a.Store.Repo, "remote", "set-url", "origin", filepath.Join(base, "missing.git"))
	if e := a.Sync(ctx, "", false); e == nil {
		t.Fatal("offline sync unexpectedly succeeded")
	}
	if _, e := os.Stat(filepath.Join(a.Store.Repo, "offline.md")); e != nil {
		t.Fatal(e)
	}
	git(t, "-C", a.Store.Repo, "remote", "set-url", "origin", remote)
	if e := a.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	git(t, "-C", b.Store.Repo, "pull", "--ff-only")
	if _, e := os.Stat(filepath.Join(b.Store.Repo, "offline.md")); e != nil {
		t.Fatal(e)
	}
	head := git(t, "-C", a.Store.Repo, "rev-parse", "HEAD")
	if e := a.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	if got := git(t, "-C", a.Store.Repo, "rev-parse", "HEAD"); got != head {
		t.Fatalf("clean sync created a commit: %s -> %s", head, got)
	}
	if e := a.Store.Delete("offline.md"); e != nil {
		t.Fatal(e)
	}
	if e := a.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	git(t, "-C", b.Store.Repo, "pull", "--ff-only")
	if _, e := os.Stat(filepath.Join(b.Store.Repo, "offline.md")); !os.IsNotExist(e) {
		t.Fatalf("deletion did not reach origin: %v", e)
	}
}
func TestInitialOfflineCheckpointRemainsDue(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	c := clone(t, base, remote, "a")
	if _, e := c.Store.Create(store.NewOptions{Title: "first", Path: "first.md"}); e != nil {
		t.Fatal(e)
	}
	git(t, "-C", c.Store.Repo, "remote", "set-url", "origin", filepath.Join(base, "missing.git"))
	if e := c.Sync(context.Background(), "", false); e == nil {
		t.Fatal("offline sync unexpectedly succeeded")
	}
	status, e := c.Status(context.Background())
	if e != nil || status.Ahead != 1 || len(status.Dirty) != 0 {
		t.Fatalf("initial checkpoint status: %+v, %v", status, e)
	}
	if due, e := c.Due(context.Background(), 5*time.Minute); e != nil || !due {
		t.Fatalf("initial checkpoint retry was deferred: %t, %v", due, e)
	}
	git(t, "-C", c.Store.Repo, "remote", "set-url", "origin", remote)
	if e := c.Sync(context.Background(), "", false); e != nil {
		t.Fatal(e)
	}
}
func TestSyncRequiresDedicatedRepoOriginAndIdentity(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	git(t, "init", "-b", "main", parent)
	repo := filepath.Join(parent, "notes")
	c := Client{Store: store.New(repo, filepath.Join(base, "state")), Timeout: 5 * time.Second}
	if _, err := c.Store.Create(store.NewOptions{Title: "first", Path: "first.md"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(context.Background(), "", false); err == nil || !strings.Contains(err.Error(), "differs from configured notes repo") {
		t.Fatalf("enclosing Git checkout was accepted: %v", err)
	}
	git(t, "init", "-b", "main", repo)
	if err := c.Sync(context.Background(), "", false); err == nil || !strings.Contains(err.Error(), "origin remote is required") {
		t.Fatalf("missing origin was accepted: %v", err)
	}
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	git(t, "-C", repo, "remote", "add", "origin", remote)
	if err := c.Sync(context.Background(), "", false); err == nil || !strings.Contains(err.Error(), "user.name") {
		t.Fatalf("missing identity was accepted: %v", err)
	}
	if got := git(t, "-C", repo, "status", "--porcelain"); !strings.Contains(got, "first.md") {
		t.Fatalf("note lost during preflight: %q", got)
	}
	git(t, "-C", repo, "config", "user.name", "Notes Test")
	git(t, "-C", repo, "config", "user.email", "notes@example.invalid")
	if err := c.Sync(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
}
func TestEditDuringPushRemainsPending(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	c := clone(t, base, remote, "a")
	if _, e := c.Store.Create(store.NewOptions{Title: "first", Path: "first.md"}); e != nil {
		t.Fatal(e)
	}
	hook := filepath.Join(c.Store.Repo, ".git", "hooks", "pre-push")
	if e := os.WriteFile(hook, []byte("#!/bin/sh\nprintf '\\nexternal edit\\n' >> first.md\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := c.Sync(context.Background(), "", false); e == nil || !strings.Contains(e.Error(), "push succeeded but new local edits remain pending") {
		t.Fatalf("concurrent edit was not reported: %v", e)
	}
	local, e := os.ReadFile(filepath.Join(c.Store.Repo, "first.md"))
	if e != nil || !strings.Contains(string(local), "external edit") {
		t.Fatalf("concurrent edit lost: %q, %v", local, e)
	}
	if remoteBody := git(t, "--git-dir", remote, "show", "main:first.md"); strings.Contains(remoteBody, "external edit") {
		t.Fatalf("concurrent edit unexpectedly committed: %q", remoteBody)
	}
}
func TestConflictingPullPreservesLocalCommit(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	a := clone(t, base, remote, "a")
	ctx := context.Background()
	if _, e := a.Store.Create(store.NewOptions{Title: "original", Path: "shared.md"}); e != nil {
		t.Fatal(e)
	}
	if e := a.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	b := clone(t, base, remote, "b")
	if e := os.WriteFile(filepath.Join(a.Store.Repo, "shared.md"), []byte("# local\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(b.Store.Repo, "shared.md"), []byte("# remote\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := b.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	if e := a.Sync(ctx, "", false); !errors.Is(e, ErrConflict) || !strings.Contains(e.Error(), "shared.md") {
		t.Fatalf("expected merge conflict sentinel, got %v", e)
	}
	if got := git(t, "-C", a.Store.Repo, "show", "HEAD:shared.md"); got != "# local" {
		t.Fatalf("local checkpoint lost: %q", got)
	}
	if got := git(t, "-C", a.Store.Repo, "status", "--porcelain"); got != "" {
		t.Fatalf("merge state remains: %q", got)
	}
}
func TestDivergentNonconflictingNotesMergeAndPush(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	a := clone(t, base, remote, "a")
	ctx := context.Background()
	if _, err := a.Store.Create(store.NewOptions{Title: "base", Path: "base.md"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	b := clone(t, base, remote, "b")
	if _, err := a.Store.Create(store.NewOptions{Title: "local", Path: "local.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Store.Create(store.NewOptions{Title: "remote", Path: "remote.md"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Sync(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx, "", false); err != nil {
		t.Fatalf("nonconflicting divergent notes did not merge: %v", err)
	}
	for _, name := range []string{"base.md", "local.md", "remote.md"} {
		if got := git(t, "--git-dir", remote, "show", "main:"+name); !strings.Contains(got, "# ") {
			t.Fatalf("%s not present at origin: %q", name, got)
		}
	}
	if parents := strings.Fields(git(t, "-C", a.Store.Repo, "rev-list", "--parents", "-n", "1", "HEAD")); len(parents) != 3 {
		t.Fatalf("expected a merge commit, got %v", parents)
	}
}
func TestSyncRefusesStagedAndNonNoteChanges(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	c := clone(t, base, remote, "a")
	if _, e := c.Store.Create(store.NewOptions{Title: "first", Path: "first.md"}); e != nil {
		t.Fatal(e)
	}
	git(t, "-C", c.Store.Repo, "add", "first.md")
	if e := c.Sync(context.Background(), "", false); e == nil || !strings.Contains(e.Error(), "staged") {
		t.Fatalf("staged index accepted: %v", e)
	}
	if got := git(t, "-C", c.Store.Repo, "status", "--porcelain"); !strings.Contains(got, "A  first.md") {
		t.Fatalf("staged file changed: %q", got)
	}
	git(t, "-C", c.Store.Repo, "reset")
	if e := os.WriteFile(filepath.Join(c.Store.Repo, ".gitignore"), []byte("ignored.md\n"), 0600); e != nil {
		t.Fatal(e)
	}
	ignored, e := c.Ignored(context.Background(), "ignored.md")
	if e != nil || !ignored {
		t.Fatalf("ignored note not detected: %t %v", ignored, e)
	}
	if e := os.WriteFile(filepath.Join(c.Store.Repo, "unrelated.txt"), []byte("leave alone"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := c.Sync(context.Background(), "", false); e == nil || !strings.Contains(e.Error(), "non-note") {
		t.Fatalf("non-note change accepted: %v", e)
	}
	if _, e := os.Stat(filepath.Join(c.Store.Repo, "unrelated.txt")); e != nil {
		t.Fatal(e)
	}
}
func TestSnapshotDetectsOutsideEdit(t *testing.T) {
	base := t.TempDir()
	c := Client{Store: store.New(base, filepath.Join(base, "state"))}
	path := filepath.Join(base, "note.md")
	if e := os.WriteFile(path, []byte("first"), 0600); e != nil {
		t.Fatal(e)
	}
	before, e := c.snapshotPaths([]string{"note.md"})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, []byte("second"), 0600); e != nil {
		t.Fatal(e)
	}
	after, e := c.snapshotPaths([]string{"note.md"})
	if e != nil {
		t.Fatal(e)
	}
	if before["note.md"] == after["note.md"] {
		t.Fatal("external edit not detected")
	}
}
func TestSyncUsesOriginUpstreamBranch(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	c := clone(t, base, remote, "a")
	if _, e := c.Store.Create(store.NewOptions{Title: "first", Path: "first.md"}); e != nil {
		t.Fatal(e)
	}
	if e := c.Sync(context.Background(), "", false); e != nil {
		t.Fatal(e)
	}
	git(t, "-C", c.Store.Repo, "branch", "-m", "local")
	if got := c.remoteBranch(context.Background(), "local"); got != "main" {
		t.Fatalf("remote branch: %q", got)
	}
	if _, e := c.Store.Create(store.NewOptions{Title: "second", Path: "second.md"}); e != nil {
		t.Fatal(e)
	}
	if e := c.Sync(context.Background(), "", false); e != nil {
		t.Fatal(e)
	}
	if got := git(t, "--git-dir", remote, "show", "main:second.md"); !strings.Contains(got, "# second") {
		t.Fatalf("tracked branch did not receive note: %q", got)
	}
}
func TestSyncStagesLiteralNotePathsAndRejectsHiddenFiles(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	c := clone(t, base, remote, "a")
	name := ":(exclude)literal.md"
	if _, err := c.Store.Create(store.NewOptions{Title: "literal", Path: name}); err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(context.Background(), "", false); err != nil {
		t.Fatalf("pathspec-shaped note did not sync literally: %v", err)
	}
	if got := git(t, "--git-dir", remote, "show", "main:"+name); !strings.Contains(got, "# literal") {
		t.Fatalf("literal filename did not reach origin: %q", got)
	}
	if err := os.Mkdir(filepath.Join(c.Store.Repo, ".private"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Store.Repo, ".private", "secret.md"), []byte("# secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(context.Background(), "", false); err == nil || !strings.Contains(err.Error(), "non-note") {
		t.Fatalf("hidden control file was staged: %v", err)
	}
}
func TestSyncNewlineNamedNoteAndDeletion(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	c := clone(t, base, remote, "a")
	name := "line\nbreak.md"
	path := filepath.Join(c.Store.Repo, name)
	if err := os.WriteFile(path, []byte("# Newline filename\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(context.Background(), "", false); err != nil {
		t.Fatalf("NUL-delimited note name did not sync: %v", err)
	}
	if got := git(t, "--git-dir", remote, "show", "main:"+name); got != "# Newline filename" {
		t.Fatalf("newline-named note missing from origin: %q", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(context.Background(), "", false); err != nil {
		t.Fatalf("newline-named deletion did not sync: %v", err)
	}
	if err := exec.Command("git", "--git-dir", remote, "cat-file", "-e", "main:"+name).Run(); err == nil {
		t.Fatal("deleted newline-named note still exists at origin")
	}
}
func TestDueUsesCommitAgeAndPendingChanges(t *testing.T) {
	gitIsolation(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(t, "init", "--bare", "-b", "main", remote)
	c := clone(t, base, remote, "a")
	ctx := context.Background()
	if _, e := c.Store.Create(store.NewOptions{Title: "first", Path: "first.md"}); e != nil {
		t.Fatal(e)
	}
	if e := c.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	baseNow := time.Now()
	c.Now = func() time.Time { return baseNow.Add(time.Minute) }
	if _, e := c.Store.Create(store.NewOptions{Title: "second", Path: "second.md"}); e != nil {
		t.Fatal(e)
	}
	if due, e := c.Due(ctx, 5*time.Minute); e != nil || due {
		t.Fatalf("premature sync: due=%t err=%v", due, e)
	}
	if wait, e := c.DueIn(ctx, 5*time.Minute); e != nil || wait < 3*time.Minute || wait > 4*time.Minute {
		t.Fatalf("next commit deadline: %s, %v", wait, e)
	}
	c.Now = func() time.Time { return baseNow.Add(10 * time.Minute) }
	if due, e := c.Due(ctx, 5*time.Minute); e != nil || !due {
		t.Fatalf("overdue sync missed: due=%t err=%v", due, e)
	}
	if e := c.Sync(ctx, "", false); e != nil {
		t.Fatal(e)
	}
	if due, e := c.Due(ctx, 5*time.Minute); e != nil || due {
		t.Fatalf("clean repository marked due: due=%t err=%v", due, e)
	}
}
