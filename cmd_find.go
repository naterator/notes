package notes

import (
	"bytes"
	"io"
	"strings"

	"github.com/pkg/errors"
	"gopkg.in/alecthomas/kingpin.v2"
)

// FindCmd represents `notes find` command. Each public fields represent options of the command.
// Out field represents where this command should output.
type FindCmd struct {
	cli    *kingpin.CmdClause
	out    io.Writer
	Config *Config
	// Query is a query string for searching notes
	Query string
	// Relative is a flag equivalent to --relative
	Relative bool
	// SortBy is a string indicating how to sort the list. This value is equivalent to --sort option
	SortBy string
	// Edit is a flag equivalent to --edit
	Edit bool
	// Out is a writer to write output of this command. Kind of stdout is expected
	Out io.Writer
}

func (cmd *FindCmd) defineCLI(app *kingpin.Application) {
	cmd.cli = app.Command("find", "Find notes by query in title, tags, metadata and body text")
	cmd.cli.Arg("query", "Query string to search in notes").Required().StringVar(&cmd.Query)
	cmd.cli.Flag("relative", "Show relative paths from $NOTES_HOME directory").Short('r').BoolVar(&cmd.Relative)
	cmd.cli.Flag("sort", "Sort list by 'modified', 'created', 'filename' or 'category'. Default is 'created'").Short('s').EnumVar(&cmd.SortBy, "modified", "created", "filename", "category")
	cmd.cli.Flag("edit", "Open listed notes with your favorite editor. $NOTES_EDITOR must be set. Paths of listed notes are passed to the editor command's arguments").Short('e').BoolVar(&cmd.Edit)
}

func (cmd *FindCmd) matchesCmdline(cmdline string) bool {
	return cmd.cli.FullCommand() == cmdline
}

func (cmd *FindCmd) printNotes(notes []*Note) error {
	switch strings.ToLower(cmd.SortBy) {
	case "filename":
		sortByFilename(notes)
	case "category":
		sortByCategory(notes)
	case "modified":
		if err := sortByModified(notes); err != nil {
			return err
		}
	default:
		sortByCreated(notes)
	}

	if cmd.Edit {
		args := make([]string, 0, len(notes))
		for _, n := range notes {
			args = append(args, n.FilePath())
		}
		return openEditor(cmd.Config, args...)
	}

	if cmd.Relative {
		var b bytes.Buffer
		for _, note := range notes {
			b.WriteString(note.RelFilePath())
			b.WriteRune('\n')
		}
		_, err := cmd.out.Write(b.Bytes())
		return err
	}

	return printOnelineNotesTo(cmd.out, notes)
}

// Do runs `notes find` command and returns an error if occurs
func (cmd *FindCmd) Do() error {
	cats, err := CollectCategories(cmd.Config, 0)
	if err != nil {
		return err
	}

	query := strings.ToLower(strings.TrimSpace(cmd.Query))

	numNotes := 0
	for _, c := range cats {
		numNotes += len(c.NotePaths)
	}

	notes := make([]*Note, 0, numNotes)
	for _, cat := range cats {
		for _, p := range cat.NotePaths {
			note, err := LoadNote(p, cmd.Config)
			if err != nil {
				return err
			}
			searchable, err := note.SearchableText()
			if err != nil {
				return err
			}
			if !findQueryMatch(searchable, query) {
				continue
			}
			notes = append(notes, note)
		}
	}

	if len(notes) == 0 {
		return nil
	}

	if cmd.Config.PagerCmd == "" {
		cmd.out = cmd.Out
		return cmd.printNotes(notes)
	}

	pager, err := StartPagerWriter(cmd.Config.PagerCmd, cmd.Out)
	if err != nil {
		return err
	}

	cmd.out = pager
	if err := cmd.printNotes(notes); err != nil {
		if pager.Err != nil {
			err = errors.Wrap(err, "Pager command did not run successfully")
		}
		return err
	}

	pager.Wait()
	return errors.Wrap(pager.Err, "Pager command did not run successfully")
}

func findQueryMatch(text, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}

	text = strings.ToLower(text)
	if strings.Contains(text, query) {
		return true
	}

	for _, token := range strings.Fields(query) {
		if strings.Contains(text, token) {
			continue
		}
		if !isFuzzyTokenMatch(token, text) {
			return false
		}
	}
	return true
}

func isFuzzyTokenMatch(token, text string) bool {
	if len(token) < 3 {
		return false
	}

	tokenRunes := []rune(token)
	for _, word := range strings.Fields(text) {
		wordRunes := []rune(word)
		if len(wordRunes) < len(tokenRunes) || len(wordRunes)-len(tokenRunes) > 3 {
			continue
		}
		if tokenRunes[0] != wordRunes[0] {
			continue
		}
		if isSubsequence(token, word) {
			return true
		}
	}

	return false
}

func isSubsequence(needle, haystack string) bool {
	if needle == "" {
		return true
	}

	runes := []rune(needle)
	i := 0
	for _, r := range haystack {
		if r != runes[i] {
			continue
		}
		i++
		if i == len(runes) {
			return true
		}
	}
	return false
}
