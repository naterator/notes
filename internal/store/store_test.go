package store

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	base := t.TempDir()
	return New(filepath.Join(base, "repo"), filepath.Join(base, "state"))
}
func TestCanonicalRepositoryLockPath(t *testing.T) {
	s := fixture(t)
	if err := os.MkdirAll(s.Repo, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(s.Repo, alias); err != nil {
		t.Fatal(err)
	}
	actual, err := s.lockPath()
	if err != nil {
		t.Fatal(err)
	}
	other, err := New(alias, s.StateDir).lockPath()
	if err != nil {
		t.Fatal(err)
	}
	if actual != other {
		t.Fatalf("repository aliases have different locks: %q, %q", actual, other)
	}
}
func TestNonRegularMarkdownFailsBeforeReading(t *testing.T) {
	s := fixture(t)
	if err := os.MkdirAll(s.Repo, 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(s.Repo, "pipe.md"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read("pipe.md"); err == nil || !strings.Contains(err.Error(), "not a regular note") {
		t.Fatalf("FIFO was read or accepted: %v", err)
	}
	if _, err := s.List(); err == nil || !strings.Contains(err.Error(), "not a regular note") {
		t.Fatalf("list silently accepted FIFO: %v", err)
	}
}
func TestRecoveryDraftOutsideRepository(t *testing.T) {
	s := fixture(t)
	content := []byte("# Local draft\n\nUnsaved #tag\n")
	path, err := s.SaveRecovery("notes/external.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if rel, _ := filepath.Rel(s.Repo, path); rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("recovery draft is inside Git repository: %s", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(content) {
		t.Fatalf("recovery draft lost buffer: %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("recovery permissions: %v", info.Mode().Perm())
	}
}
func TestRoundTripAndContainment(t *testing.T) {
	s := fixture(t)
	now := time.Date(2026, 9, 28, 10, 30, 0, 0, time.Local)
	n, err := s.Create(NewOptions{Title: "Release checklist", Category: "work", Tags: []string{"work"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Release checklist" || !reflect.DeepEqual(n.Tags, []string{"work"}) {
		t.Fatalf("unexpected note: %+v", n)
	}
	if _, err = s.Create(NewOptions{Title: "duplicate", Path: n.ID}); err == nil {
		t.Fatal("duplicate replaced")
	}
	if _, _, err = s.Resolve("../outside.md"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, _, err = s.Resolve(".git/config.md"); err == nil {
		t.Fatal("control path accepted")
	}
	if err = s.Save(n.ID, sha256.Sum256(n.Content), []byte("# Edited\n")); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(n.ID, sha256.Sum256(n.Content), []byte("# stale\n")); err == nil {
		t.Fatal("stale save overwritten")
	}
	m, err := s.Move(n.ID, "archive/release.md")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Edited" || m.ID != "archive/release.md" {
		t.Fatalf("move: %+v", m)
	}
	if err = s.Delete(m.ID); err != nil {
		t.Fatal(err)
	}
	items, err := s.List()
	if err != nil || len(items) != 0 {
		t.Fatalf("remaining: %v, %v", items, err)
	}
}
func TestDeleteRejectsExternalChange(t *testing.T) {
	s := fixture(t)
	n, err := s.Create(NewOptions{Title: "Keep", Path: "keep.md"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(n.Path, []byte("# Changed elsewhere\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteIfUnchanged(n.ID, sha256.Sum256(n.Content)); err == nil {
		t.Fatal("deleted a note that changed after confirmation")
	}
	if got, err := os.ReadFile(n.Path); err != nil || string(got) != "# Changed elsewhere\n" {
		t.Fatalf("external note lost: %q, %v", got, err)
	}
}
func TestSaveFailurePreservesOriginal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write through read-only directory permissions")
	}
	s := fixture(t)
	n, err := s.Create(NewOptions{Title: "Original", Path: "original.md"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.Repo, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(s.Repo, 0700)
	if err := s.Save(n.ID, sha256.Sum256(n.Content), []byte("# Replacement\n")); err == nil {
		t.Fatal("save succeeded without permission to stage atomic replacement")
	}
	content, err := os.ReadFile(n.Path)
	if err != nil || !bytes.Equal(content, n.Content) {
		t.Fatalf("failed atomic save damaged original: %q, %v", content, err)
	}
}
func TestTagSourceEdits(t *testing.T) {
	src := []byte("# Note\n\nTags: #work\n\nVisible #ideas and [#linked](https://example.com/#hidden). `#code`\\#escaped\n\n```\n#fence\n```\n")
	if got := Tags(src); !reflect.DeepEqual(got, []string{"ideas", "linked", "work"}) {
		t.Fatalf("tags: %q", got)
	}
	added, err := AddTags(src, []string{"urgent"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(added), "Tags: #work #urgent") {
		t.Fatalf("add: %s", added)
	}
	removed, err := RemoveTags(added, []string{"work", "ideas"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(removed), "#work") || !strings.Contains(string(removed), "Visible ideas") || !strings.Contains(string(removed), "#code") {
		t.Fatalf("remove: %s", removed)
	}
}
func TestUnicodeOpeningPunctuationStartsTag(t *testing.T) {
	src := []byte("# Note\n\nSee «#Café» and word#notatag.\n")
	if got := Tags(src); !reflect.DeepEqual(got, []string{"café"}) {
		t.Fatalf("opening punctuation boundary: %q", got)
	}
}
func TestManagedTagsOnHeadingFreeMarkdown(t *testing.T) {
	source := []byte("Ordinary first line\r\nSecond line with #inline.\r\n")
	withWork, err := AddTags(source, []string{"work"})
	if err != nil {
		t.Fatal(err)
	}
	withTwo, err := AddTags(withWork, []string{"urgent"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(withTwo, []byte("Tags: #work #urgent\r\n\r\n")) || !bytes.HasSuffix(withTwo, source) || bytes.Count(withTwo, []byte("Tags:")) != 1 {
		t.Fatalf("managed line duplicated or source changed: %q", withTwo)
	}
	withoutWork, err := RemoveTags(withTwo, []string{"work"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(withoutWork, []byte("Tags: #urgent\r\n")) || !bytes.HasSuffix(withoutWork, source) {
		t.Fatalf("managed removal on heading-free note: %q", withoutWork)
	}
}
func TestTitleAndManagedTagsUseMarkdownHeading(t *testing.T) {
	for _, test := range []struct {
		name, source, title, suffix string
	}{
		{"atx after fence", "```md\n# fake\n```\n\n# **Real** title\n\nBody\n", "Real title", "# **Real** title\n\nTags: #work\n\nBody\n"},
		{"setext", "Title\r\n=====\r\n\r\nBody\r\n", "Title", "Title\r\n=====\r\n\r\nTags: #work\r\n\r\nBody\r\n"},
		{"blockquote atx", "> # Quoted title\n> Body\n", "Quoted title", "> # Quoted title\n\nTags: #work\n\n> Body\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := []byte(test.source)
			if got := Title(input, "fallback.md"); got != test.title {
				t.Fatalf("title = %q", got)
			}
			output, err := AddTags(input, []string{"work"})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasSuffix(output, []byte(test.suffix)) {
				t.Fatalf("managed tags after wrong heading: %q", output)
			}
		})
	}
}
func TestIndentedCodeIsNotManagedTags(t *testing.T) {
	input := []byte("# Note\n\n    Tags: #example\n\nBody\n")
	output, err := AddTags(input, []string{"work"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output, []byte("\n\nTags: #work\n\n    Tags: #example\n")) || !reflect.DeepEqual(Tags(output), []string{"work"}) {
		t.Fatalf("indented code modified or parsed as managed tags: %q", output)
	}
}
func TestJournalReuseAndSections(t *testing.T) {
	s := fixture(t)
	now := time.Date(2026, 9, 28, 10, 30, 0, 0, time.UTC)
	n, err := s.AddJournal("", "UTC", "actions", "Send update", []string{"work"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != "journal/2026/09/2026-09-28.md" {
		t.Fatal(n.ID)
	}
	_, err = s.AddJournal("", "UTC", "notes", "First line\nSecond line", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "# 2026-09-28") != 1 || !strings.Contains(string(b), "- [ ] Send update #work") || !strings.Contains(string(b), "  Second line") {
		t.Fatalf("journal: %s", b)
	}
	if _, _, err = JournalDate("2026-02-30", "UTC", now); err == nil {
		t.Fatal("invalid date accepted")
	}
}
func TestJournalSectionDetectionPreservesMarkdownAndCRLF(t *testing.T) {
	s := fixture(t)
	now := time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC)
	n, err := s.EnsureJournal("2026-11-01", "America/Chicago", now)
	if err != nil {
		t.Fatal(err)
	}
	content := "# 2026-11-01\r\n\r\n## Activities\r\n\r\nA sentence mentioning ## Actions inline.\r\n\r\n```md\r\n## Actions\r\n```\r\n\r\n<!--\r\n## Actions\r\n-->\r\n\r\n> ## Actions\r\n\r\n## Actions\r\n\r\n- [ ] Existing\r\n\r\n## Notes\r\n\r\nKeep this ending.\r\n"
	if err := os.WriteFile(n.Path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.AddJournal("2026-11-01", "America/Chicago", "actions", "New action", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(got), "- [ ] New action") != 1 || !strings.Contains(string(got), "- [ ] Existing\r\n\r\n- [ ] New action\r\n") {
		t.Fatalf("action landed outside actual section: %q", got)
	}
	if !strings.Contains(string(got), "A sentence mentioning ## Actions inline.\r\n\r\n```md\r\n## Actions\r\n```\r\n\r\n<!--\r\n## Actions\r\n-->\r\n\r\n> ## Actions\r\n") || !strings.Contains(string(got), "## Notes\r\n\r\nKeep this ending.\r\n") {
		t.Fatalf("unrelated Markdown changed: %q", got)
	}
	if strings.Contains(strings.ReplaceAll(string(got), "\r\n", ""), "\n") {
		t.Fatalf("LF introduced into CRLF journal: %q", got)
	}
}
func TestJournalMissingSectionAndTimezoneBoundary(t *testing.T) {
	s := fixture(t)
	now := time.Date(2026, 3, 8, 7, 59, 0, 0, time.UTC)
	beforeMidnight, _, err := JournalDate("", "America/Chicago", time.Date(2026, 3, 8, 5, 59, 0, 0, time.UTC))
	if err != nil || beforeMidnight != "2026-03-07" {
		t.Fatalf("before local midnight: %q, %v", beforeMidnight, err)
	}
	afterMidnight, _, err := JournalDate("", "America/Chicago", time.Date(2026, 3, 8, 6, 1, 0, 0, time.UTC))
	if err != nil || afterMidnight != "2026-03-08" {
		t.Fatalf("after local midnight: %q, %v", afterMidnight, err)
	}
	date, _, err := JournalDate("", "America/Chicago", now)
	if err != nil || date != "2026-03-08" {
		t.Fatalf("before DST date: %q, %v", date, err)
	}
	date, _, err = JournalDate("", "America/Chicago", now.Add(2*time.Minute))
	if err != nil || date != "2026-03-08" {
		t.Fatalf("after DST date: %q, %v", date, err)
	}
	n, err := s.EnsureJournal("2026-03-08", "America/Chicago", now)
	if err != nil {
		t.Fatal(err)
	}
	content := "# 2026-03-08\n\n## Activities\n\nOriginal text.\n\n\n"
	if err := os.WriteFile(n.Path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.AddJournal("2026-03-08", "America/Chicago", "notes", "First line\nSecond line", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), content) || !strings.Contains(string(got), "## Notes\n\n- 01:59 First line\n  Second line\n") {
		t.Fatalf("missing section append changed prior bytes or lost multiline entry: %q", got)
	}
}
func TestSymlinkContainmentAndUnicodeTags(t *testing.T) {
	s := fixture(t)
	outside := t.TempDir()
	if e := os.MkdirAll(s.Repo, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(s.Repo, "link")); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Create(NewOptions{Title: "escape", Path: "link/escape.md"}); e == nil {
		t.Fatal("created note through symlink")
	}
	if _, e := os.Stat(filepath.Join(outside, "escape.md")); !os.IsNotExist(e) {
		t.Fatalf("outside path touched: %v", e)
	}
	n, e := s.Create(NewOptions{Title: "Unicode", Tags: []string{"#Café", "#研究"}})
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(n.Tags, []string{"café", "研究"}) {
		t.Fatalf("unicode tags: %q", n.Tags)
	}
}
