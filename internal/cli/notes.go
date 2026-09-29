package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/store"
	"github.com/naterator/notes/internal/tui"
	"github.com/spf13/cobra"
)

func (a *App) notesCommands() {
	a.newCommand()
	a.showCommand()
	a.listCommand(false)
	a.listCommand(true)
	a.tagsCommand()
	a.editCommand()
	a.deleteCommand()
	a.moveCommand()
	a.categoriesCommand()
	a.journalCommand()
}
func (a *App) newCommand() {
	var category, path, template string
	var tags []string
	var fromStdin bool
	cmd := &cobra.Command{Use: "new TITLE", Short: "Create one Markdown note", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, s, e := a.store()
		if e != nil {
			return e
		}
		if category == "" {
			category = c.DefaultCategory
		}
		var body []byte
		if fromStdin {
			body, e = io.ReadAll(a.In)
			if e != nil {
				return e
			}
		}
		n, e := s.Create(store.NewOptions{Title: args[0], Category: category, Path: path, Template: template, Tags: tags, Stdin: body})
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintln(a.Out, n.Path); e != nil {
			return e
		}
		return a.maybeSync(cmd.Context(), c, s, n.ID)
	}}
	cmd.Flags().StringVar(&category, "category", "", "category directory")
	cmd.Flags().StringVar(&path, "path", "", "exact relative .md path")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "tag to add (repeatable)")
	cmd.Flags().StringVar(&template, "template", "", "body template file")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "append stdin to note")
	a.Root.AddCommand(cmd)
}
func (a *App) showCommand() {
	a.Root.AddCommand(&cobra.Command{Use: "show NOTE", Short: "Print exact Markdown source", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, s, e := a.store()
		if e != nil {
			return e
		}
		n, e := s.Read(args[0])
		if e != nil {
			return e
		}
		_, e = a.Out.Write(n.Content)
		return e
	}})
}

type queryFlags struct {
	Tags                                                        []string
	Category, Sort                                              string
	Reverse                                                     bool
	Limit                                                       int
	Relative, Print0, JSON, Long, Matches, CaseSensitive, Regex bool
}

func addQueryFlags(cmd *cobra.Command, f *queryFlags, find bool) {
	cmd.Flags().StringArrayVar(&f.Tags, "tag", nil, "require tag (repeatable)")
	cmd.Flags().StringVar(&f.Category, "category", "", "category and descendants")
	cmd.Flags().StringVar(&f.Sort, "sort", "path", "path, modified, title, or category")
	cmd.Flags().BoolVar(&f.Reverse, "reverse", false, "reverse sort")
	cmd.Flags().IntVar(&f.Limit, "limit", 0, "maximum notes (0 means all)")
	cmd.Flags().BoolVar(&f.Relative, "relative", false, "print repository-relative paths")
	cmd.Flags().BoolVar(&f.Print0, "print0", false, "NUL-delimit path records")
	cmd.Flags().BoolVar(&f.JSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&f.Long, "long", false, "print human-readable summaries")
	if find {
		cmd.Flags().BoolVar(&f.Matches, "matches", false, "print matching source lines")
		cmd.Flags().BoolVar(&f.CaseSensitive, "case-sensitive", false, "case-sensitive search")
		cmd.Flags().BoolVar(&f.Regex, "regex", false, "treat query as Go regexp")
	}
}
func (f *queryFlags) validate(cmd *cobra.Command) error {
	if f.Limit < 0 {
		return usage("--limit cannot be negative")
	}
	if f.Sort != "path" && f.Sort != "modified" && f.Sort != "title" && f.Sort != "category" {
		return usage("invalid --sort %q", f.Sort)
	}
	modes := 0
	for _, v := range []bool{f.JSON, f.Long, f.Matches} {
		if v {
			modes++
		}
	}
	if modes > 1 || f.Print0 && modes > 0 {
		return usage("output modes --json, --long, --matches, and --print0 conflict")
	}
	return nil
}
func findMatcher(query string, regex, caseSensitive bool) (*regexp.Regexp, error) {
	if !regex {
		query = regexp.QuoteMeta(query)
	}
	if !caseSensitive {
		query = "(?i)" + query
	}
	return regexp.Compile(query)
}
func filtered(notes []store.Note, f queryFlags, query *regexp.Regexp) []store.Note {
	out := make([]store.Note, 0, len(notes))
	for _, n := range notes {
		if f.Category != "" && n.Category != f.Category && !strings.HasPrefix(n.Category, f.Category+"/") {
			continue
		}
		ok := true
		for _, tag := range f.Tags {
			tag = strings.ToLower(strings.TrimPrefix(tag, "#"))
			seen := false
			for _, t := range n.Tags {
				if t == tag {
					seen = true
					break
				}
			}
			if !seen {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if query != nil && !query.Match(n.Content) && !query.MatchString(n.Title) {
			continue
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		switch f.Sort {
		case "modified":
			if !out[i].ModifiedAt.Equal(out[j].ModifiedAt) {
				return out[i].ModifiedAt.Before(out[j].ModifiedAt)
			}
		case "title":
			if out[i].Title != out[j].Title {
				return out[i].Title < out[j].Title
			}
		case "category":
			if out[i].Category != out[j].Category {
				return out[i].Category < out[j].Category
			}
		}
		return out[i].ID < out[j].ID
	})
	if f.Reverse {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out
}
func (a *App) outputNotes(c config.Config, notes []store.Note, f queryFlags, matcher *regexp.Regexp, compactList bool) error {
	if f.JSON {
		records := make([]map[string]any, 0, len(notes))
		for _, n := range notes {
			p := n.Path
			if f.Relative {
				p = n.ID
			}
			records = append(records, map[string]any{"id": n.ID, "path": p, "title": n.Title, "category": n.Category, "tags": n.Tags, "modified_at": n.ModifiedAt.Format(time.RFC3339)})
		}
		return json.NewEncoder(a.Out).Encode(records)
	}
	if !f.Print0 {
		if compactList && strings.ContainsAny(c.Repo, "\n\r\t") {
			return usage("repository path contains newline/tab; use --print0 or --json")
		}
		for _, n := range notes {
			p := n.Path
			if f.Relative {
				p = n.ID
			} else if compactList {
				p = "/" + n.ID
			}
			if strings.ContainsAny(p, "\n\r\t") {
				return usage("path contains newline/tab; use --print0 or --json")
			}
		}
	}
	if compactList {
		if _, e := fmt.Fprintf(a.Out, "base: %s\n", c.Repo); e != nil {
			return e
		}
	}
	for _, n := range notes {
		p := n.Path
		if f.Relative {
			p = n.ID
		} else if compactList {
			p = "/" + n.ID
		}
		if f.Matches {
			count := 0
			for i, line := range bytes.Split(n.Content, []byte("\n")) {
				if matcher.Match(line) {
					if _, e := fmt.Fprintf(a.Out, "%s:%d:%s\n", p, i+1, line); e != nil {
						return e
					}
					count++
					if count >= 3 {
						break
					}
				}
			}
			continue
		}
		if f.Long {
			if _, e := fmt.Fprintf(a.Out, "%s\t%s\t%s\t%s\n", p, humanColor(a.Out, c, "accent", n.Title), humanColor(a.Out, c, "muted", strings.Join(n.Tags, ",")), n.ModifiedAt.Format(time.RFC3339)); e != nil {
				return e
			}
			continue
		}
		end := "\n"
		if f.Print0 {
			end = "\x00"
		}
		if _, e := io.WriteString(a.Out, p+end); e != nil {
			return e
		}
	}
	return nil
}
func (a *App) listCommand(find bool) {
	f := &queryFlags{}
	name := "list"
	short := "List Markdown notes"
	if find {
		name = "find QUERY"
		short = "Find Markdown notes"
	}
	cmd := &cobra.Command{Use: name, Short: short, RunE: func(cmd *cobra.Command, args []string) error {
		if find && len(args) != 1 {
			return usage("find requires one nonempty query")
		}
		if !find && len(args) != 0 {
			return usage("list takes no arguments")
		}
		if e := f.validate(cmd); e != nil {
			return e
		}
		var matcher *regexp.Regexp
		var e error
		if find {
			if args[0] == "" {
				return usage("empty search query")
			}
			matcher, e = findMatcher(args[0], f.Regex, f.CaseSensitive)
			if e != nil {
				return usage("invalid regex: %v", e)
			}
		}
		c, s, e := a.store()
		if e != nil {
			return e
		}
		notes, e := s.List()
		if e != nil {
			return e
		}
		out, fileOutput := a.Out.(*os.File)
		compactList := !find && fileOutput && term.IsTerminal(out.Fd()) && !f.Relative && !f.Print0 && !f.JSON && !f.Long
		return a.outputNotes(c, filtered(notes, *f, matcher), *f, matcher, compactList)
	}}
	if !find {
		cmd.Aliases = []string{"ls"}
	}
	addQueryFlags(cmd, f, find)
	a.Root.AddCommand(cmd)
}
func (a *App) tagsCommand() {
	var note, category string
	var counts, asJSON bool
	cmd := &cobra.Command{Use: "tags", Short: "List and edit note tags", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return unknownSubcommand(cmd, args)
		}
		_, s, e := a.store()
		if e != nil {
			return e
		}
		var notes []store.Note
		if note != "" {
			n, e := s.Read(note)
			if e != nil {
				return e
			}
			notes = []store.Note{n}
		} else {
			notes, e = s.List()
			if e != nil {
				return e
			}
		}
		if category != "" {
			notes = filtered(notes, queryFlags{Category: category, Sort: "path"}, nil)
		}
		m := store.TagCounts(notes)
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if asJSON {
			return json.NewEncoder(a.Out).Encode(m)
		}
		for _, k := range keys {
			if counts {
				_, e = fmt.Fprintf(a.Out, "#%s\t%d\n", k, m[k])
			} else {
				_, e = fmt.Fprintln(a.Out, "#"+k)
			}
			if e != nil {
				return e
			}
		}
		return nil
	}}
	cmd.Flags().StringVar(&note, "note", "", "tags in one note")
	cmd.Flags().StringVar(&category, "category", "", "category and descendants")
	cmd.Flags().BoolVar(&counts, "counts", false, "show note counts")
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON counts")
	for _, remove := range []bool{false, true} {
		name := "add"
		if remove {
			name = "remove"
		}
		child := &cobra.Command{Use: name + " NOTE TAG...", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			c, s, e := a.store()
			if e != nil {
				return e
			}
			changed, e := s.TagsEdit(args[0], args[1:], remove)
			if e != nil {
				return e
			}
			if !changed {
				return nil
			}
			return a.maybeSync(cmd.Context(), c, s, args[0])
		}}
		cmd.AddCommand(child)
	}
	a.Root.AddCommand(cmd)
}
func (a *App) editCommand() {
	a.Root.AddCommand(&cobra.Command{Use: "edit NOTE", Args: cobra.ExactArgs(1), Short: "Open a note in the TUI or configured editor", RunE: func(cmd *cobra.Command, args []string) error {
		c, s, e := a.store()
		if e != nil {
			return e
		}
		n, e := s.Read(args[0])
		if e != nil {
			return e
		}
		if _, e = resolveEditor(c); errors.Is(e, errNoEditor) {
			return tui.Run(cmd.Context(), c, s, n.ID, a.In, a.Out)
		} else if e != nil {
			return e
		}
		if e = a.launchEditor(cmd.Context(), c, n.Path); e != nil {
			return e
		}
		after, e := os.ReadFile(n.Path)
		if e != nil {
			return e
		}
		if !bytes.Equal(after, n.Content) {
			return a.maybeSync(cmd.Context(), c, s, n.ID)
		}
		return nil
	}})
}
func (a *App) deleteCommand() {
	var yes bool
	cmd := &cobra.Command{Use: "delete NOTE", Aliases: []string{"rm"}, Args: cobra.ExactArgs(1), Short: "Delete one note", RunE: func(cmd *cobra.Command, args []string) error {
		c, s, e := a.store()
		if e != nil {
			return e
		}
		n, e := s.Read(args[0])
		if e != nil {
			return e
		}
		if !yes {
			input, ok := a.In.(*os.File)
			if !ok {
				return usage("delete in a pipeline requires --yes")
			}
			if !term.IsTerminal(input.Fd()) {
				return usage("delete in a pipeline requires --yes")
			}
			fmt.Fprintf(a.Err, "Delete %s? [y/N] ", n.ID)
			line, _ := bufio.NewReader(a.In).ReadString('\n')
			if strings.TrimSpace(strings.ToLower(line)) != "y" {
				return nil
			}
		}
		if e = s.DeleteIfUnchanged(n.ID, sha256.Sum256(n.Content)); e != nil {
			return e
		}
		return a.maybeSync(cmd.Context(), c, s, n.ID)
	}}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm deletion")
	a.Root.AddCommand(cmd)
}
func (a *App) moveCommand() {
	a.Root.AddCommand(&cobra.Command{Use: "move NOTE DESTINATION", Aliases: []string{"mv"}, Args: cobra.ExactArgs(2), Short: "Move a note", RunE: func(cmd *cobra.Command, args []string) error {
		c, s, e := a.store()
		if e != nil {
			return e
		}
		n, e := s.Move(args[0], args[1])
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintln(a.Out, n.Path); e != nil {
			return e
		}
		return a.maybeSync(cmd.Context(), c, s, n.ID)
	}})
}
func (a *App) categoriesCommand() {
	a.Root.AddCommand(&cobra.Command{Use: "categories", Aliases: []string{"cats"}, Short: "List note-containing categories", RunE: func(cmd *cobra.Command, args []string) error {
		_, s, e := a.store()
		if e != nil {
			return e
		}
		notes, e := s.List()
		if e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, n := range notes {
			seen[n.Category] = true
		}
		keys := make([]string, 0, len(seen))
		for k := range seen {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if strings.ContainsAny(k, "\n\r\t") {
				return usage("category contains newline/tab; use notes list --json to inspect it")
			}
		}
		for _, k := range keys {
			if _, e = fmt.Fprintln(a.Out, k); e != nil {
				return e
			}
		}
		return nil
	}})
}
func (a *App) journalCommand() {
	var date, section string
	var fromStdin bool
	var tags []string
	cmd := &cobra.Command{Use: "journal", Short: "Open today's daily journal", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return unknownSubcommand(cmd, args)
		}
		c, s, e := a.store()
		if e != nil {
			return e
		}
		day, _, e := store.JournalDate(date, c.Journal.Timezone, time.Now())
		if e != nil {
			return usage("%v", e)
		}
		id := store.JournalID(day)
		_, oldErr := s.Read(id)
		n, e := s.EnsureJournal(date, c.Journal.Timezone, time.Now())
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintln(a.Out, n.Path); e != nil {
			return e
		}
		if oldErr == nil {
			return nil
		}
		return a.maybeSync(cmd.Context(), c, s, n.ID)
	}}
	cmd.PersistentFlags().StringVar(&date, "date", "", "journal date YYYY-MM-DD")
	add := &cobra.Command{Use: "add [TEXT]", Short: "Append one journal entry", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 || fromStdin && len(args) > 0 {
			return usage("journal add accepts TEXT or --stdin")
		}
		body := ""
		if fromStdin {
			b, e := io.ReadAll(a.In)
			if e != nil {
				return e
			}
			body = string(b)
		} else if len(args) == 1 {
			body = args[0]
		} else {
			return usage("journal add requires TEXT or --stdin")
		}
		c, s, e := a.store()
		if e != nil {
			return e
		}
		n, e := s.AddJournal(date, c.Journal.Timezone, section, body, tags, time.Now())
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintln(a.Out, n.Path); e != nil {
			return e
		}
		return a.maybeSync(cmd.Context(), c, s, n.ID)
	}}
	add.Flags().StringVar(&section, "section", "activities", "activities, actions, or notes")
	add.Flags().BoolVar(&fromStdin, "stdin", false, "read entry from stdin")
	add.Flags().StringArrayVar(&tags, "tag", nil, "tag to append (repeatable)")
	cmd.AddCommand(add)
	a.Root.AddCommand(cmd)
}
