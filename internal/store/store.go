package store

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxEditable = 2 << 20

type Store struct{ Repo, StateDir string }
type Note struct {
	ID         string    `json:"id"`
	Path       string    `json:"path"`
	Title      string    `json:"title"`
	Category   string    `json:"category"`
	Tags       []string  `json:"tags"`
	ModifiedAt time.Time `json:"modified_at"`
	Content    []byte    `json:"-"`
}

func New(repo, state string) *Store { return &Store{Repo: repo, StateDir: state} }

// SaveRecovery keeps a conflicted editor buffer outside the Git working tree.
func (s *Store) SaveRecovery(id string, content []byte) (string, error) {
	if !utf8.Valid(content) {
		return "", fmt.Errorf("recovery content is not UTF-8")
	}
	dir := filepath.Join(s.StateDir, "recovery")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	name := time.Now().Format("20060102-150405") + "-" + Slug(strings.TrimSuffix(filepath.Base(id), filepath.Ext(id))) + "-*.md"
	f, err := os.CreateTemp(dir, name)
	if err != nil {
		return "", err
	}
	path := f.Name()
	if err := f.Chmod(0600); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
func (s *Store) lockPath() (string, error) {
	repo, err := filepath.Abs(s.Repo)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	sum := sha256.Sum256([]byte(repo))
	return filepath.Join(s.StateDir, "repo-"+hex.EncodeToString(sum[:8])+".lock"), nil
}
func (s *Store) lock() (func(), error) {
	if err := os.MkdirAll(s.StateDir, 0700); err != nil {
		return nil, err
	}
	name, err := s.lockPath()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

// Lock serializes app-managed writes and Git transactions for this repository.
func (s *Store) Lock() (func(), error) { return s.lock() }
func (s *Store) Resolve(id string) (string, string, error) {
	if id == "" {
		return "", "", fmt.Errorf("empty note path")
	}
	var abs string
	if filepath.IsAbs(id) {
		abs = filepath.Clean(id)
	} else {
		abs = filepath.Join(s.Repo, filepath.FromSlash(id))
	}
	rel, err := filepath.Rel(s.Repo, abs)
	if err != nil {
		return "", "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("note path escapes repository: %s", id)
	}
	for _, p := range strings.Split(rel, string(filepath.Separator)) {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") {
			return "", "", fmt.Errorf("hidden/control note path: %s", id)
		}
	}
	if strings.ToLower(filepath.Ext(rel)) != ".md" {
		return "", "", fmt.Errorf("note must be a .md file: %s", id)
	}
	cur := s.Repo
	parts := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts {
		cur = filepath.Join(cur, p)
		fi, e := os.Lstat(cur)
		if e == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("symlink note path: %s", id)
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return "", "", e
		}
	}
	return abs, filepath.ToSlash(rel), nil
}
func (s *Store) Read(id string) (Note, error) {
	path, rel, err := s.Resolve(id)
	if err != nil {
		return Note{}, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return Note{}, err
	}
	if !fi.Mode().IsRegular() {
		return Note{}, fmt.Errorf("not a regular note: %s", rel)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Note{}, err
	}
	if !utf8.Valid(b) {
		return Note{}, fmt.Errorf("invalid UTF-8 note %s", rel)
	}
	cat := filepath.ToSlash(filepath.Dir(rel))
	if cat == "." {
		cat = "."
	}
	return Note{ID: rel, Path: path, Title: Title(b, rel), Category: cat, Tags: Tags(b), ModifiedAt: fi.ModTime(), Content: b}, nil
}
func (s *Store) List() ([]Note, error) {
	items := []Note{}
	_, err := os.Stat(s.Repo)
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(s.Repo, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path == s.Repo {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(d.Name())) != ".md" {
			return nil
		}
		n, e := s.Read(path)
		if e != nil {
			return e
		}
		items = append(items, n)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

var slugSplit = regexp.MustCompile(`[^a-z0-9]+`)

func Slug(title string) string {
	v := strings.ToLower(title)
	v = slugSplit.ReplaceAllString(v, "-")
	v = strings.Trim(v, "-")
	if v == "" {
		return "note"
	}
	if len(v) > 60 {
		v = strings.Trim(v[:60], "-")
	}
	return v
}
func Title(b []byte, id string) string {
	if title, _, found := firstHeading(b); found {
		return title
	}
	return strings.TrimSuffix(filepath.Base(id), filepath.Ext(id))
}
func validateCategory(cat string) error {
	if cat == "" || filepath.IsAbs(cat) || strings.Contains(cat, "\\") || hasControl(cat) {
		return fmt.Errorf("invalid category %q", cat)
	}
	for _, p := range strings.Split(cat, "/") {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") {
			return fmt.Errorf("invalid category %q", cat)
		}
	}
	return nil
}
func cleanTags(tags []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range tags {
		t := strings.ToLower(strings.TrimPrefix(v, "#"))
		if !validTag(t) {
			return nil, fmt.Errorf("invalid tag %q", v)
		}
		if !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	return out, nil
}
func validTag(t string) bool {
	parts := strings.Split(t, "/")
	for _, p := range parts {
		if p == "" {
			return false
		}
		for i, r := range p {
			if i == 0 && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
				return false
			}
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-' {
				return false
			}
		}
	}
	return true
}
func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

type NewOptions struct {
	Title, Category, Path, Template string
	Tags                            []string
	Stdin                           []byte
	Now                             time.Time
}

func (s *Store) Create(o NewOptions) (Note, error) {
	if strings.TrimSpace(o.Title) == "" {
		return Note{}, fmt.Errorf("title is required")
	}
	if hasControl(o.Title) {
		return Note{}, fmt.Errorf("title contains control characters")
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.Category == "" {
		o.Category = "misc"
	}
	if err := validateCategory(o.Category); err != nil {
		return Note{}, err
	}
	tags, err := cleanTags(o.Tags)
	if err != nil {
		return Note{}, err
	}
	var body []byte
	if o.Template != "" {
		body, err = os.ReadFile(o.Template)
		if err != nil {
			return Note{}, err
		}
	} else {
		for _, candidate := range []string{filepath.Join(s.Repo, filepath.FromSlash(o.Category), ".template.md"), filepath.Join(s.Repo, ".template.md")} {
			body, err = os.ReadFile(candidate)
			if err == nil {
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return Note{}, err
			}
			body = nil
		}
	}
	if len(o.Stdin) > 0 {
		body = append(append([]byte{}, body...), o.Stdin...)
	}
	if !utf8.Valid(body) {
		return Note{}, fmt.Errorf("note body is not UTF-8")
	}
	content := "# " + strings.TrimSpace(o.Title) + "\n\n"
	if len(tags) > 0 {
		content += "Tags: #" + strings.Join(tags, " #") + "\n\n"
	}
	content += string(body)
	path := o.Path
	if path != "" && hasControl(path) {
		return Note{}, fmt.Errorf("new note path contains control characters")
	}
	if path == "" {
		path = filepath.ToSlash(filepath.Join(o.Category, o.Now.Format("20060102-150405")+"-"+Slug(o.Title)+".md"))
	}
	release, err := s.lock()
	if err != nil {
		return Note{}, err
	}
	defer release()
	abs, rel, err := s.Resolve(path)
	if err != nil {
		return Note{}, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return Note{}, err
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) && o.Path == "" {
		var suffix [3]byte
		if _, e := rand.Read(suffix[:]); e != nil {
			return Note{}, e
		}
		path = strings.TrimSuffix(rel, ".md") + "-" + hex.EncodeToString(suffix[:]) + ".md"
		abs, _, err = s.Resolve(path)
		if err == nil {
			f, err = os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		}
	}
	if err != nil {
		return Note{}, err
	}
	if _, err = f.WriteString(content); err != nil {
		f.Close()
		_ = os.Remove(abs)
		return Note{}, err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(abs)
		return Note{}, err
	}
	return s.Read(abs)
}
func atomicReplace(path string, b []byte, mode fs.FileMode, expected [32]byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".notes-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(mode.Perm()); err != nil {
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
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if sha256.Sum256(current) != expected {
		return fmt.Errorf("note changed on disk during save; local changes were not written")
	}
	return os.Rename(f.Name(), path)
}
func (s *Store) Save(id string, expected [32]byte, content []byte) error {
	if !utf8.Valid(content) {
		return fmt.Errorf("note is not UTF-8")
	}
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()
	path, _, err := s.Resolve(id)
	if err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if sha256.Sum256(old) != expected {
		return fmt.Errorf("note changed on disk; save a new copy or reload before retrying")
	}
	if bytes.Equal(old, content) {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	return atomicReplace(path, content, fi.Mode(), expected)
}
func (s *Store) Mutate(id string, fn func([]byte) ([]byte, error)) error {
	n, err := s.Read(id)
	if err != nil {
		return err
	}
	b, err := fn(n.Content)
	if err != nil {
		return err
	}
	if string(b) == string(n.Content) {
		return nil
	}
	return s.Save(id, sha256.Sum256(n.Content), b)
}
func (s *Store) Delete(id string) error {
	n, err := s.Read(id)
	if err != nil {
		return err
	}
	return s.DeleteIfUnchanged(id, sha256.Sum256(n.Content))
}
func (s *Store) DeleteIfUnchanged(id string, expected [32]byte) error {
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()
	p, _, err := s.Resolve(id)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if sha256.Sum256(content) != expected {
		return fmt.Errorf("note changed on disk; review it before deleting")
	}
	return os.Remove(p)
}
func (s *Store) Move(src, dst string) (Note, error) {
	if hasControl(dst) {
		return Note{}, fmt.Errorf("destination path contains control characters")
	}
	release, err := s.lock()
	if err != nil {
		return Note{}, err
	}
	defer release()
	a, _, err := s.Resolve(src)
	if err != nil {
		return Note{}, err
	}
	b, _, err := s.Resolve(dst)
	if err != nil {
		return Note{}, err
	}
	if a == b {
		return Note{}, fmt.Errorf("source and destination are the same")
	}
	if _, err = os.Stat(a); err != nil {
		return Note{}, err
	}
	if err = os.MkdirAll(filepath.Dir(b), 0700); err != nil {
		return Note{}, err
	}
	if err = os.Link(a, b); err != nil {
		return Note{}, err
	}
	if err = os.Remove(a); err != nil {
		_ = os.Remove(b)
		return Note{}, err
	}
	return s.Read(b)
}
