package notes

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/kballard/go-shellquote"
	"github.com/rhysd/go-fakeio"
)

func outputLines(out string) []string {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func firstFields(lines []string) []string {
	fields := make([]string, 0, len(lines))
	for _, line := range lines {
		fields = append(fields, strings.Fields(line)[0])
	}
	return fields
}

func TestFindCmd(t *testing.T) {
	old := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = old }()

	cfg := testNewConfigForListCmd("normal")

	for _, tc := range []struct {
		what      string
		title     string
		within    string
		wantPaths []string
	}{
		{
			what:   "title only with case-insensitive search",
			title:  "THIS IS TITLE",
			within: "",
			wantPaths: []string{
				"c/3.md",
				"b/2.md",
				"c/5.md",
				"a/1.md",
				"a/4.md",
			},
		},
		{
			what:   "title and body",
			title:  "title",
			within: "gubergren",
			wantPaths: []string{
				"b/2.md",
			},
		},
		{
			what:   "title and body case-insensitive",
			title:  "title",
			within: "GUBERGREN",
			wantPaths: []string{
				"b/2.md",
			},
		},
		{
			what:   "title and metadata tags",
			title:  "title",
			within: "A-BIT-LONG",
			wantPaths: []string{
				"c/5.md",
			},
		},
		{
			what:   "title and metadata created",
			title:  "text from",
			within: "2118-10-30",
			wantPaths: []string{
				"b/6.md",
			},
		},
		{
			what:      "no match",
			title:     "no-matching-title",
			within:    "",
			wantPaths: nil,
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			var buf bytes.Buffer
			cmd := &FindCmd{
				Config:      cfg,
				Out:         &buf,
				TitleQuery:  tc.title,
				WithinQuery: tc.within,
			}

			if err := cmd.Do(); err != nil {
				t.Fatal(err)
			}

			lines := outputLines(buf.String())
			have := firstFields(lines)
			want := make([]string, len(tc.wantPaths))
			for i, p := range tc.wantPaths {
				want[i] = filepath.FromSlash(p)
			}

			if !reflect.DeepEqual(want, have) {
				t.Fatalf("Expected paths %v but have %v. Full output:\n%s", want, have, buf.String())
			}
		})
	}
}

func TestFindRelative(t *testing.T) {
	cfg := testNewConfigForListCmd("normal")
	var buf bytes.Buffer
	cmd := &FindCmd{
		Config:     cfg,
		Out:        &buf,
		TitleQuery: "this is title",
		Relative:   true,
	}

	if err := cmd.Do(); err != nil {
		t.Fatal(err)
	}

	have := outputLines(buf.String())
	want := []string{
		filepath.FromSlash("c/3.md"),
		filepath.FromSlash("b/2.md"),
		filepath.FromSlash("c/5.md"),
		filepath.FromSlash("a/1.md"),
		filepath.FromSlash("a/4.md"),
	}
	if !reflect.DeepEqual(want, have) {
		t.Fatalf("Expected paths %v but have %v", want, have)
	}
}

func TestFindSortByFilename(t *testing.T) {
	cfg := testNewConfigForListCmd("normal")
	var buf bytes.Buffer
	cmd := &FindCmd{
		Config:     cfg,
		Out:        &buf,
		TitleQuery: "this is title",
		SortBy:     "filename",
	}

	if err := cmd.Do(); err != nil {
		t.Fatal(err)
	}

	lines := outputLines(buf.String())
	have := firstFields(lines)
	want := []string{
		filepath.FromSlash("a/1.md"),
		filepath.FromSlash("b/2.md"),
		filepath.FromSlash("c/3.md"),
		filepath.FromSlash("a/4.md"),
		filepath.FromSlash("c/5.md"),
	}
	if !reflect.DeepEqual(want, have) {
		t.Fatalf("Expected paths %v but have %v", want, have)
	}
}

func TestFindCmdEditOption(t *testing.T) {
	fake := fakeio.Stdout()
	defer fake.Restore()

	exe, err := exec.LookPath("echo")
	panicIfErr(err)
	exe = shellquote.Join(exe) // On Windows it may contain 'Program Files' so quoting is necessary

	cfg := testNewConfigForListCmd("normal")
	cfg.EditorCmd = exe

	var buf bytes.Buffer
	cmd := &FindCmd{
		Config:     cfg,
		Out:        &buf,
		TitleQuery: "this is title",
		Edit:       true,
	}

	if err := cmd.Do(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if out != "" {
		t.Fatal("Unexpected output from command itself:", out)
	}

	stdout, err := fake.String()
	panicIfErr(err)

	have := strings.Split(strings.TrimRight(stdout, "\n"), " ")
	want := []string{}
	for _, p := range []string{
		"c/3.md",
		"b/2.md",
		"c/5.md",
		"a/1.md",
		"a/4.md",
	} {
		p = filepath.Join(cfg.HomePath, filepath.FromSlash(p))
		want = append(want, p)
	}

	if !reflect.DeepEqual(want, have) {
		t.Fatal("Args passed to editor is not expected:", have, "wanted", want)
	}
}

func TestFindWriteError(t *testing.T) {
	cfg := testNewConfigForListCmd("normal")
	cmd := &FindCmd{
		Config:     cfg,
		Out:        alwaysErrorWriter{},
		TitleQuery: "title",
	}
	if err := cmd.Do(); err == nil || !strings.Contains(err.Error(), "Write error for test") {
		t.Fatal("Unexpected error", err)
	}
}

func TestFindNoHome(t *testing.T) {
	cfg := &Config{HomePath: "/path/to/unknown/directory"}
	err := (&FindCmd{Config: cfg, TitleQuery: "title"}).Do()
	if err == nil {
		t.Fatal("Error did not occur")
	}
	if !strings.Contains(err.Error(), "Cannot read home") {
		t.Fatal("Unexpected error:", err)
	}
}

func TestFindBrokenNote(t *testing.T) {
	cfg := testNewConfigForListCmd("fail")
	cmd := &FindCmd{
		Config:     cfg,
		TitleQuery: "title",
	}
	err := cmd.Do()
	if err == nil {
		t.Fatal("Error did not occur")
	}
	if !strings.Contains(err.Error(), "Cannot parse created date time") {
		t.Fatal("Unexpected error:", err)
	}
}

func TestFindPagingWithPager(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("`cat` command is necessary to run this test")
	}

	var buf bytes.Buffer

	cfg := testNewConfigForListCmd("normal")
	cfg.PagerCmd = "cat"
	cmd := &FindCmd{
		Config:     cfg,
		Out:        &buf,
		TitleQuery: "title",
	}

	if err := cmd.Do(); err != nil {
		t.Fatal(err)
	}

	for _, l := range outputLines(buf.String()) {
		p := strings.Fields(l)[0]
		p = filepath.Join(cfg.HomePath, p)
		if _, err := os.Stat(p); err != nil {
			t.Fatal(p, "does not exist:", err)
		}
	}
}

func TestFindPagingError(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want string
		out  io.Writer
	}{
		{
			cmd:  "'foo",
			want: "Cannot parsing",
		},
		{
			cmd:  "/path/to/bin/unknown",
			want: "Cannot start pager command",
		},
		{
			cmd:  "cat",
			want: "Pager command did not run successfully: Write error for test",
			out:  alwaysErrorWriter{},
		},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			cfg := testNewConfigForListCmd("normal")
			cfg.PagerCmd = tc.cmd

			out := tc.out
			if out == nil {
				out = io.Discard
			}

			cmd := &FindCmd{
				Config:     cfg,
				Out:        out,
				TitleQuery: "title",
			}

			if err := cmd.Do(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal("Error unexpected", err)
			}
		})
	}
}
