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
	// TitleQuery is a query string for searching note titles
	TitleQuery string
	// WithinQuery is an optional query string for searching metadata and body
	WithinQuery string
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
	cmd.cli = app.Command("find", "Find notes by title and optionally by metadata/body text")
	cmd.cli.Arg("title-query", "Query string to search in note titles").Required().StringVar(&cmd.TitleQuery)
	cmd.cli.Arg("within-query", "Optional query string to search in note metadata and body").StringVar(&cmd.WithinQuery)
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

	titleQuery := strings.ToLower(strings.TrimSpace(cmd.TitleQuery))
	withinQuery := strings.ToLower(strings.TrimSpace(cmd.WithinQuery))

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
			if !strings.Contains(strings.ToLower(note.Title), titleQuery) {
				continue
			}
			if withinQuery != "" {
				searchable, err := note.SearchableText()
				if err != nil {
					return err
				}
				if !strings.Contains(strings.ToLower(searchable), withinQuery) {
					continue
				}
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
