package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func journalNewline(b []byte) string {
	if bytes.Contains(b, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// journalSection finds top-level Markdown headings. Offsets point to the
// original bytes, so an append leaves unrelated Markdown intact.
func journalSection(b []byte, wanted string) (int, int, bool) {
	start, end := -1, len(b)
	root := markdown.Parser().Parse(text.NewReader(b))
	_ = ast.Walk(root, func(node ast.Node, enter bool) (ast.WalkStatus, error) {
		if !enter {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Lines().Len() == 0 || heading.Level > 2 || heading.Parent() != root {
			return ast.WalkContinue, nil
		}
		lineStart := bytes.LastIndexByte(b[:heading.Lines().At(0).Start], '\n') + 1
		if start >= 0 {
			end = lineStart
			return ast.WalkStop, nil
		}
		if heading.Level == 2 && strings.TrimSpace(string(heading.Text(b))) == wanted {
			start = headingEnd(b, heading)
		}
		return ast.WalkContinue, nil
	})
	if start >= 0 {
		return start, end, true
	}
	return 0, 0, false
}

func location(name string) (*time.Location, error) {
	if name == "" || name == "local" {
		return time.Local, nil
	}
	return time.LoadLocation(name)
}
func JournalDate(date, tz string, now time.Time) (string, time.Time, error) {
	loc, err := location(tz)
	if err != nil {
		return "", time.Time{}, err
	}
	local := now.In(loc)
	if date == "" {
		date = local.Format("2006-01-02")
	}
	parsed, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil || parsed.Format("2006-01-02") != date {
		return "", time.Time{}, fmt.Errorf("invalid date %q", date)
	}
	return date, local, nil
}
func JournalID(date string) string {
	return filepath.ToSlash(filepath.Join("journal", date[:4], date[5:7], date+".md"))
}
func (s *Store) EnsureJournal(date, tz string, now time.Time) (Note, error) {
	date, _, err := JournalDate(date, tz, now)
	if err != nil {
		return Note{}, err
	}
	id := JournalID(date)
	if note, err := s.Read(id); err == nil {
		return note, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Note{}, err
	}
	release, err := s.lock()
	if err != nil {
		return Note{}, err
	}
	defer release()
	path, _, err := s.Resolve(id)
	if err != nil {
		return Note{}, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return Note{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return s.Read(id)
	}
	if err != nil {
		return Note{}, err
	}
	body := "# " + date + "\n\nTags: #journal\n\n## Activities\n\n## Actions\n\n## Notes\n\n"
	if _, err = f.WriteString(body); err != nil {
		f.Close()
		_ = os.Remove(path)
		return Note{}, err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(path)
		return Note{}, err
	}
	return s.Read(id)
}
func (s *Store) AddJournal(date, tz, section, body string, tags []string, now time.Time) (Note, error) {
	if !utf8.ValidString(body) || strings.TrimSpace(body) == "" {
		return Note{}, fmt.Errorf("journal entry is empty or invalid UTF-8")
	}
	if section == "" {
		section = "activities"
	}
	if section != "activities" && section != "actions" && section != "notes" {
		return Note{}, fmt.Errorf("invalid journal section %q", section)
	}
	tagList, err := cleanTags(tags)
	if err != nil {
		return Note{}, err
	}
	note, err := s.EnsureJournal(date, tz, now)
	if err != nil {
		return Note{}, err
	}
	_, local, err := JournalDate(date, tz, now)
	if err != nil {
		return Note{}, err
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	heading := "## " + strings.ToUpper(section[:1]) + section[1:]
	err = s.Mutate(note.ID, func(b []byte) ([]byte, error) {
		newline := journalNewline(b)
		var entry strings.Builder
		entry.Grow(len(body) + len(tagList)*24 + 32)
		entry.WriteString("- ")
		if section == "actions" {
			entry.WriteString("[ ] ")
		} else {
			entry.WriteString(local.Format("15:04"))
			entry.WriteByte(' ')
		}
		entry.WriteString(lines[0])
		if len(tagList) > 0 {
			entry.WriteString(" #")
			entry.WriteString(strings.Join(tagList, " #"))
		}
		entry.WriteString(newline)
		for _, line := range lines[1:] {
			entry.WriteString("  ")
			entry.WriteString(line)
			entry.WriteString(newline)
		}
		start, end, found := journalSection(b, heading[3:])
		if !found {
			out := append([]byte{}, b...)
			if len(out) > 0 && !bytes.HasSuffix(out, []byte(newline)) {
				out = append(out, []byte(newline)...)
			}
			if len(out) > 0 && !bytes.HasSuffix(out, []byte(newline+newline)) {
				out = append(out, []byte(newline)...)
			}
			out = append(out, []byte(heading+newline+newline+entry.String())...)
			return out, nil
		}
		insert := end
		for insert > start && (b[insert-1] == '\n' || b[insert-1] == '\r') {
			insert--
		}
		separator := newline + newline
		if insert == start && start > 0 && b[start-1] == '\n' {
			separator = newline
		}
		out := append([]byte{}, b[:insert]...)
		out = append(out, []byte(separator+entry.String())...)
		out = append(out, b[insert:]...)
		return out, nil
	})
	if err != nil {
		return Note{}, err
	}
	return s.Read(note.ID)
}
