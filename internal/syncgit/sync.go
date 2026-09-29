package syncgit

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/naterator/notes/internal/store"
)

type Client struct {
	Store   *store.Store
	Timeout time.Duration
	Now     func() time.Time
}
type Status struct {
	Branch     string   `json:"branch"`
	Dirty      []string `json:"dirty"`
	Ahead      int      `json:"ahead"`
	Behind     int      `json:"behind"`
	LastSync   string   `json:"last_sync,omitempty"`
	LastCommit string   `json:"last_commit,omitempty"`
}

var ErrConflict = errors.New("Git conflict requires manual resolution")

// EnsureRepo initializes the dedicated notes repository on first use.
func (g Client) EnsureRepo(ctx context.Context) error {
	release, err := g.Store.Lock()
	if err != nil {
		return err
	}
	defer release()
	if err := os.MkdirAll(g.Store.Repo, 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(g.Store.Repo, ".git")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = g.run(ctx, false, "init", "-b", "main")
	return err
}

func (g Client) HasOrigin(ctx context.Context) (bool, error) {
	remotes, err := g.run(ctx, false, "remote")
	if err != nil {
		return false, err
	}
	for _, remote := range strings.Fields(remotes) {
		if remote == "origin" {
			return true, nil
		}
	}
	return false, nil
}

func (g Client) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g Client) run(ctx context.Context, background bool, args ...string) (string, error) {
	if background && g.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", g.Store.Repo}, args...)...)
	if background {
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never")
		if os.Getenv("GIT_SSH_COMMAND") == "" && os.Getenv("GIT_SSH") == "" {
			cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
		}
	} else {
		cmd.Stdin = os.Stdin
	}
	b, err := cmd.CombinedOutput()
	if err != nil {
		if len(b) > 0 {
			return string(b), fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(b)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(b), nil
}
func (g Client) preflight(ctx context.Context) (string, error) {
	root, err := g.run(ctx, false, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("notes repository is not a Git working tree: %w", err)
	}
	a, e := filepath.Abs(strings.TrimSpace(root))
	if e != nil {
		return "", e
	}
	b, e := filepath.Abs(g.Store.Repo)
	if e != nil {
		return "", e
	}
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	if a != b {
		return "", fmt.Errorf("Git root %s differs from configured notes repo %s", a, b)
	}
	branch, err := g.run(ctx, false, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("notes repository has detached HEAD or no branch: %w", err)
	}
	if _, err = g.run(ctx, false, "remote", "get-url", "origin"); err != nil {
		return "", fmt.Errorf("origin remote is required: %w", err)
	}
	for _, ref := range []string{"MERGE_HEAD", "REBASE_HEAD"} {
		if _, err = g.run(ctx, false, "rev-parse", "-q", "--verify", ref); err == nil {
			return "", fmt.Errorf("%w: Git %s is in progress", ErrConflict, ref)
		}
	}
	for _, marker := range []string{"rebase-merge", "rebase-apply"} {
		path, e := g.run(ctx, false, "rev-parse", "--git-path", marker)
		if e != nil {
			return "", e
		}
		path = strings.TrimSpace(path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(g.Store.Repo, path)
		}
		if _, e = os.Stat(path); e == nil {
			return "", fmt.Errorf("%w: Git rebase is in progress", ErrConflict)
		} else if !errors.Is(e, os.ErrNotExist) {
			return "", e
		}
	}
	if unresolved, e := g.run(ctx, false, "ls-files", "-u", "-z"); e != nil {
		return "", e
	} else if unresolved != "" {
		return "", fmt.Errorf("%w: Git has unresolved paths", ErrConflict)
	}
	if _, err = g.run(ctx, false, "diff", "--cached", "--quiet"); err != nil {
		return "", fmt.Errorf("Git index has staged changes; commit or unstage them before notes sync")
	}
	for _, field := range []string{"user.name", "user.email"} {
		v, e := g.run(ctx, false, "config", "--get", field)
		if e != nil || strings.TrimSpace(v) == "" {
			return "", fmt.Errorf("Git %s is not configured", field)
		}
	}
	return strings.TrimSpace(branch), nil
}
func (g Client) remoteBranch(ctx context.Context, local string) string {
	upstream, err := g.run(ctx, false, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err == nil {
		if branch, ok := strings.CutPrefix(strings.TrimSpace(upstream), "origin/"); ok && branch != "" {
			return branch
		}
	}
	return local
}
func (g Client) changed(ctx context.Context) ([]string, error) {
	raw, err := g.run(ctx, false, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	data := []byte(raw)
	paths := []string{}
	for len(data) > 0 {
		end := strings.IndexByte(string(data), 0)
		if end < 0 {
			return nil, fmt.Errorf("malformed Git status")
		}
		entry := string(data[:end])
		data = data[end+1:]
		if len(entry) < 4 {
			return nil, fmt.Errorf("malformed Git status entry")
		}
		xy, path := entry[:2], entry[3:]
		if strings.ContainsAny(xy, "RC") {
			return nil, fmt.Errorf("Git rename/copy pending; resolve it before sync")
		}
		if syncablePath(path) {
			info, statErr := os.Lstat(filepath.Join(g.Store.Repo, filepath.FromSlash(path)))
			if statErr == nil && !info.Mode().IsRegular() {
				return nil, fmt.Errorf("non-regular note %q blocks sync", path)
			}
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				return nil, statErr
			}
			paths = append(paths, path)
		} else {
			return nil, fmt.Errorf("non-note change %q blocks automatic staging", path)
		}
	}
	return paths, nil
}
func syncablePath(path string) bool {
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		return false
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ".") && (i != len(parts)-1 || part != ".template.md") {
			return false
		}
	}
	return true
}
func (g Client) Ignored(ctx context.Context, id string) (bool, error) {
	_, err := g.run(ctx, false, "check-ignore", "--quiet", "--", id)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}
func (g Client) snapshotPaths(paths []string) (map[string][32]byte, error) {
	result := make(map[string][32]byte, len(paths))
	for _, path := range paths {
		f, err := os.Open(filepath.Join(g.Store.Repo, filepath.FromSlash(path)))
		if errors.Is(err, os.ErrNotExist) {
			result[path] = [32]byte{}
			continue
		}
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		var digest [32]byte
		copy(digest[:], h.Sum(nil))
		result[path] = digest
	}
	return result, nil
}
func (g Client) Status(ctx context.Context) (Status, error) {
	branch, err := g.preflight(ctx)
	if err != nil {
		return Status{}, err
	}
	paths, err := g.changed(ctx)
	if err != nil {
		return Status{}, err
	}
	s := Status{Branch: branch, Dirty: paths}
	ref := "refs/remotes/origin/" + g.remoteBranch(ctx, branch)
	if _, err = g.run(ctx, false, "show-ref", "--verify", "--quiet", ref); err == nil {
		v, e := g.run(ctx, false, "rev-list", "--left-right", "--count", "HEAD..."+ref)
		if e == nil {
			parts := strings.Fields(v)
			if len(parts) == 2 {
				s.Ahead, _ = strconv.Atoi(parts[0])
				s.Behind, _ = strconv.Atoi(parts[1])
			}
		}
	} else if _, headErr := g.run(ctx, false, "rev-parse", "--verify", "HEAD"); headErr == nil {
		v, e := g.run(ctx, false, "rev-list", "--count", "HEAD")
		if e == nil {
			s.Ahead, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	if b, err := os.ReadFile(filepath.Join(g.Store.StateDir, "last-sync")); err == nil {
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) > 0 {
			s.LastSync = lines[0]
		}
		if len(lines) > 1 {
			s.LastCommit = lines[1]
		}
	}
	return s, nil
}
func (g Client) Due(ctx context.Context, max time.Duration) (bool, error) {
	wait, err := g.DueIn(ctx, max)
	return wait <= 0, err
}
func (g Client) DueIn(ctx context.Context, max time.Duration) (time.Duration, error) {
	st, err := g.Status(ctx)
	if err != nil {
		return 0, err
	}
	if st.Ahead > 0 {
		return 0, nil
	}
	if len(st.Dirty) == 0 {
		return max, nil
	}
	log, err := g.run(ctx, false, "log", "-1", "--format=%ct")
	if err != nil {
		return 0, nil
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(log), 10, 64)
	if err != nil {
		return 0, nil
	}
	age := g.now().Sub(time.Unix(seconds, 0))
	if age < 0 {
		age = 0
	}
	return max - age, nil
}
func (g Client) Sync(ctx context.Context, message string, background bool) error {
	release, err := g.Store.Lock()
	if err != nil {
		return err
	}
	defer release()
	branch, err := g.preflight(ctx)
	if err != nil {
		return err
	}
	paths, err := g.changed(ctx)
	if err != nil {
		return err
	}
	before, err := g.snapshotPaths(paths)
	if err != nil {
		return err
	}
	_, fetchErr := g.run(ctx, background, "fetch", "origin")
	after, err := g.snapshotPaths(paths)
	if err != nil {
		return err
	}
	if !maps.Equal(before, after) {
		return fmt.Errorf("note files changed during remote check; retry sync after reviewing the external edits")
	}
	if _, e := g.run(ctx, false, "diff", "--cached", "--quiet"); e != nil {
		return fmt.Errorf("Git index changed during remote check; leave externally staged work untouched")
	}
	committed := false
	if len(paths) > 0 {
		args := []string{"add", "-A", "--"}
		for _, path := range paths {
			args = append(args, ":(literal)"+path)
		}
		if _, err = g.run(ctx, false, args...); err != nil {
			return err
		}
		if _, err = g.run(ctx, false, "diff", "--cached", "--quiet"); err != nil {
			if message == "" {
				message = fmt.Sprintf("notes: update %d files", len(paths))
			}
			if _, err = g.run(ctx, false, "commit", "-m", message); err != nil {
				return err
			}
			committed = true
		}
	}
	if fetchErr != nil {
		if committed {
			return fmt.Errorf("local note commit saved; remote check failed: %w", fetchErr)
		}
		return fmt.Errorf("remote check failed; local state preserved: %w", fetchErr)
	}
	remoteBranch := g.remoteBranch(ctx, branch)
	ref := "refs/remotes/origin/" + remoteBranch
	if _, err = g.run(ctx, false, "show-ref", "--verify", "--quiet", ref); err == nil {
		if _, err = g.run(ctx, background, "pull", "--no-rebase", "--no-edit", "--no-autostash", "origin", remoteBranch); err != nil {
			if _, verifyErr := g.run(ctx, false, "rev-parse", "-q", "--verify", "MERGE_HEAD"); verifyErr == nil {
				unresolved, _ := g.run(ctx, false, "diff", "--name-only", "--diff-filter=U", "-z")
				files := []string{}
				for _, path := range strings.Split(strings.TrimSuffix(unresolved, "\x00"), "\x00") {
					if path != "" {
						files = append(files, fmt.Sprintf("%q", path))
					}
				}
				where := ""
				if len(files) > 0 {
					where = " in " + strings.Join(files, ", ")
				}
				if _, abortErr := g.run(ctx, false, "merge", "--abort"); abortErr != nil {
					return fmt.Errorf("%w%s: pull failed: %v; merge abort failed: %v", ErrConflict, where, err, abortErr)
				}
				if _, mergeErr := g.run(ctx, false, "rev-parse", "-q", "--verify", "MERGE_HEAD"); mergeErr == nil {
					return fmt.Errorf("%w%s: merge abort returned success but MERGE_HEAD remains; resolve Git state manually", ErrConflict, where)
				}
				if unresolvedAfter, checkErr := g.run(ctx, false, "ls-files", "-u", "-z"); checkErr != nil || unresolvedAfter != "" {
					return fmt.Errorf("%w%s: unresolved Git paths remain after abort; resolve Git state manually", ErrConflict, where)
				}
				return fmt.Errorf("%w%s: pull failed; local notes remain committed: %v", ErrConflict, where, err)
			}
			return fmt.Errorf("pull failed; local state preserved: %w", err)
		}
	}
	if _, e := g.run(ctx, false, "diff", "--cached", "--quiet"); e != nil {
		return fmt.Errorf("Git index changed during sync; leave externally staged work untouched")
	}
	if pending, e := g.changed(ctx); e != nil {
		return fmt.Errorf("working tree changed during sync: %w", e)
	} else if len(pending) > 0 {
		return fmt.Errorf("note files changed during sync; local edits remain pending: %s", strings.Join(pending, ", "))
	}
	if _, err = g.run(ctx, background, "push", "-u", "origin", "HEAD:"+remoteBranch); err != nil {
		return fmt.Errorf("push failed; local state remains pending: %w", err)
	}
	if pending, e := g.changed(ctx); e != nil {
		return fmt.Errorf("push succeeded but working tree changed during sync: %w", e)
	} else if len(pending) > 0 {
		return fmt.Errorf("push succeeded but new local edits remain pending: %s", strings.Join(pending, ", "))
	}
	if err = os.MkdirAll(g.Store.StateDir, 0700); err != nil {
		return err
	}
	commit, err := g.run(ctx, false, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	state := g.now().UTC().Format(time.RFC3339) + "\n" + strings.TrimSpace(commit) + "\n"
	return os.WriteFile(filepath.Join(g.Store.StateDir, "last-sync"), []byte(state), 0600)
}

var ErrNotDue = errors.New("sync not due")
