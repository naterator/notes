package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/shlex"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/store"
	"github.com/naterator/notes/internal/syncgit"
	"github.com/naterator/notes/internal/theme"
	"github.com/spf13/cobra"
)

var Version = "dev"
var Commit = "unknown"
var BuildDate = "unknown"
var errNoEditor = errors.New("no editor configured")

type UsageError struct{ Err error }

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }

type SavedSyncError struct{ Err error }

func (e SavedSyncError) Error() string {
	return "note saved locally, but sync failed; run `notes sync` to retry: " + e.Err.Error()
}
func (e SavedSyncError) Unwrap() error { return e.Err }
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var se SavedSyncError
	if errors.As(err, &se) {
		return 3
	}
	var ue UsageError
	if errors.As(err, &ue) {
		return 2
	}
	s := err.Error()
	if strings.Contains(s, "unknown flag") || strings.Contains(s, "requires ") || strings.Contains(s, "unknown command") {
		return 2
	}
	return 1
}

type App struct {
	In                             io.Reader
	Out, Err                       io.Writer
	ConfigFile, Repo, Theme, Color string
	NoColor                        bool
	Root                           *cobra.Command
}

func New(in io.Reader, out, errOut io.Writer) *App {
	a := &App{In: in, Out: out, Err: errOut}
	r := &cobra.Command{
		Use: "notes", Short: "Manage Markdown notes", SilenceUsage: true, SilenceErrors: true,
		Example: "  notes new \"Release checklist\" --tag planning\n  notes list --tag planning\n  notes find release --print0 | xargs -0 -n 1 cat\n  notes tui",
	}
	r.SetIn(in)
	r.SetOut(out)
	r.SetErr(errOut)
	r.RunE = func(cmd *cobra.Command, args []string) error { return cmd.Help() }
	r.PersistentFlags().StringVar(&a.ConfigFile, "config", "", "settings TOML file")
	r.PersistentFlags().StringVar(&a.Repo, "repo", "", "notes repository")
	r.PersistentFlags().StringVar(&a.Theme, "theme", "", "color theme")
	r.PersistentFlags().StringVar(&a.Color, "color", "", "auto, always, or never")
	r.PersistentFlags().BoolVar(&a.NoColor, "no-color", false, "disable ANSI color")
	_ = r.RegisterFlagCompletionFunc("theme", func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		return theme.Names(), cobra.ShellCompDirectiveNoFileComp
	})
	_ = r.RegisterFlagCompletionFunc("color", func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		return []string{"auto", "always", "never"}, cobra.ShellCompDirectiveNoFileComp
	})
	a.Root = r
	a.notesCommands()
	a.configCommands()
	a.syncCommands()
	a.miscCommands()
	versionCmd := &cobra.Command{Use: "version", Short: "Show build version", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return usage("version takes no arguments")
		}
		if jsonFlag(cmd) {
			return json.NewEncoder(out).Encode(map[string]string{"version": Version, "commit": Commit, "build_date": BuildDate})
		}
		_, e := fmt.Fprintf(out, "%s (%s, %s)\n", Version, Commit, BuildDate)
		return e
	}}
	versionCmd.Flags().Bool("json", false, "print JSON")
	r.AddCommand(versionCmd)
	addCommandShortcuts(r)
	return a
}

func addCommandShortcuts(root *cobra.Command) {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	minimum := map[string]int{
		"categories": 1, "config": 2, "completion": 3,
		"delete": 1, "edit": 1, "find": 1, "help": 1,
		"journal": 1, "list": 1, "move": 1, "new": 1,
		"show": 1, "sync": 2, "tags": 2, "tui": 1,
		"update": 1, "version": 1,
	}
	for _, cmd := range root.Commands() {
		min, ok := minimum[cmd.Name()]
		if ok {
			for length := min; length < len(cmd.Name()); length++ {
				cmd.Aliases = append(cmd.Aliases, cmd.Name()[:length])
			}
		}
		addNestedShortcuts(cmd)
	}
}

func addNestedShortcuts(parent *cobra.Command) {
	children := parent.Commands()
	for _, cmd := range children {
		if !cmd.Hidden {
			for length := 1; length < len(cmd.Name()); length++ {
				prefix := cmd.Name()[:length]
				unique := true
				for _, other := range children {
					if other == cmd {
						continue
					}
					if strings.HasPrefix(other.Name(), prefix) {
						unique = false
						break
					}
					for _, alias := range other.Aliases {
						if strings.HasPrefix(alias, prefix) {
							unique = false
							break
						}
					}
					if !unique {
						break
					}
				}
				if unique {
					cmd.Aliases = append(cmd.Aliases, prefix)
				}
			}
		}
		addNestedShortcuts(cmd)
	}
}

func unknownSubcommand(cmd *cobra.Command, args []string) error {
	return usage("unknown subcommand %q for %q", args[0], cmd.CommandPath())
}
func usage(format string, args ...any) error { return UsageError{fmt.Errorf(format, args...)} }
func jsonFlag(cmd *cobra.Command) bool       { v, _ := cmd.Flags().GetBool("json"); return v }
func (a *App) Execute(ctx context.Context, args []string) error {
	a.Root.SetArgs(args)
	return a.Root.ExecuteContext(ctx)
}
func (a *App) config() (config.Config, error) {
	c, e := config.Load(config.SelectedPath(a.ConfigFile))
	if e != nil {
		return c, UsageError{e}
	}
	for _, item := range []struct{ flag, key, value string }{{"repo", "repo", a.Repo}, {"theme", "theme", a.Theme}, {"color", "color", a.Color}} {
		if a.Root.PersistentFlags().Changed(item.flag) {
			v, e := config.ParseValue(item.key, item.value)
			if e != nil {
				return c, UsageError{e}
			}
			if e := c.OverrideFrom(item.key, v, "flag --"+item.flag); e != nil {
				return c, UsageError{e}
			}
		}
	}
	if a.NoColor {
		if e := c.OverrideFrom("color", "never", "flag --no-color"); e != nil {
			return c, UsageError{e}
		}
	}
	return c, nil
}
func (a *App) store() (config.Config, *store.Store, error) {
	c, e := a.config()
	if e != nil {
		return c, nil, e
	}
	s := store.New(c.Repo, c.StateDir)
	if e := (syncgit.Client{Store: s}).EnsureRepo(context.Background()); e != nil {
		return c, nil, e
	}
	return c, s, nil
}
func (a *App) maybeSync(ctx context.Context, c config.Config, s *store.Store, ids ...string) error {
	if !c.Git.AutoSync {
		return nil
	}
	max, _ := time.ParseDuration(c.Git.MaxInterval)
	timeout, _ := time.ParseDuration(c.Git.Timeout)
	g := syncgit.Client{Store: s, Timeout: timeout}
	hasOrigin, e := g.HasOrigin(ctx)
	if e != nil {
		return SavedSyncError{e}
	}
	if !hasOrigin {
		return nil
	}
	for _, id := range ids {
		if ignored, e := g.Ignored(ctx, id); e == nil && ignored {
			_, _ = fmt.Fprintf(a.Err, "warning: %s is ignored by Git and will not sync\n", id)
		}
	}
	due, e := g.Due(ctx, max)
	if e != nil {
		return SavedSyncError{e}
	}
	if !due {
		return nil
	}
	if e = g.Sync(ctx, "", true); e != nil {
		return SavedSyncError{e}
	}
	return nil
}
func resolveEditor(c config.Config) ([]string, error) {
	if len(c.Editor) > 0 {
		return c.Editor, nil
	}
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if v := os.Getenv(name); v != "" {
			parts, e := shlex.Split(v)
			if e != nil {
				return nil, e
			}
			if len(parts) > 0 {
				return parts, nil
			}
		}
	}
	return nil, fmt.Errorf("%w; set EDITOR or run notes config set editor '[\"vim\"]'", errNoEditor)
}
func (a *App) launchEditor(ctx context.Context, c config.Config, path string) error {
	parts, e := resolveEditor(c)
	if e != nil {
		return e
	}
	cmd := exec.CommandContext(ctx, parts[0], append(parts[1:], path)...)
	cmd.Stdin = a.In
	cmd.Stdout = a.Err
	cmd.Stderr = a.Err
	return cmd.Run()
}
