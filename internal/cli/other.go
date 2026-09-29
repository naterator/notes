package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/syncgit"
	"github.com/naterator/notes/internal/tui"
	"github.com/naterator/notes/internal/update"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

func (a *App) configCommands() {
	var renderJSON bool
	render := func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return usage("config render takes no arguments")
		}
		c, e := a.config()
		if e != nil {
			return e
		}
		var b []byte
		if renderJSON {
			b, e = json.MarshalIndent(c, "", "  ")
			b = append(b, '\n')
		} else {
			b, e = toml.Marshal(c)
		}
		if e != nil {
			return e
		}
		_, e = a.Out.Write(b)
		return e
	}
	cmd := &cobra.Command{Use: "config", Short: "Inspect or change settings", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return unknownSubcommand(cmd, args)
		}
		return render(cmd, args)
	}}
	cmd.Flags().BoolVar(&renderJSON, "json", false, "print effective JSON")
	r := &cobra.Command{Use: "render", RunE: render}
	r.Flags().BoolVar(&renderJSON, "json", false, "print effective JSON")
	cmd.AddCommand(r)
	cmd.AddCommand(&cobra.Command{Use: "path", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		_, e := fmt.Fprintln(a.Out, config.SelectedPath(a.ConfigFile))
		return e
	}})
	cmd.AddCommand(&cobra.Command{Use: "get KEY", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, e := a.config()
		if e != nil {
			return e
		}
		v, e := config.Get(c, args[0])
		if e != nil {
			return UsageError{e}
		}
		if s, ok := v.(string); ok {
			_, e = fmt.Fprintln(a.Out, s)
			return e
		}
		return json.NewEncoder(a.Out).Encode(v)
	}})
	cmd.AddCommand(&cobra.Command{Use: "set KEY VALUE", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if e := config.Set(config.SelectedPath(a.ConfigFile), args[0], args[1]); e != nil {
			return UsageError{e}
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "unset KEY", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if e := config.Unset(config.SelectedPath(a.ConfigFile), args[0]); e != nil {
			return UsageError{e}
		}
		for _, pair := range [][2]string{{"repo", "NOTES_REPO"}, {"theme", "NOTES_THEME"}, {"color", "NOTES_COLOR"}} {
			if args[0] == pair[0] {
				if _, set := os.LookupEnv(pair[1]); set {
					_, _ = fmt.Fprintf(a.Err, "%s remains overridden by %s\n", args[0], pair[1])
				}
			}
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "edit", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		path := config.SelectedPath(a.ConfigFile)
		c, e := config.Load(path)
		if e != nil {
			c = config.Defaults(path)
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		f.Close()
		if e = a.launchEditor(cmd.Context(), c, path); e != nil {
			return e
		}
		_, e = config.Load(path)
		return e
	}})
	for _, child := range cmd.Commands() {
		if child.Name() == "get" || child.Name() == "set" || child.Name() == "unset" {
			child.ValidArgsFunction = func(command *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
				if len(args) == 0 {
					keys := []string{}
					for _, d := range config.Settings() {
						keys = append(keys, d.Key+"\t"+d.Label)
					}
					return keys, cobra.ShellCompDirectiveNoFileComp
				}
				if command.Name() == "set" && len(args) == 1 {
					for _, d := range config.Settings() {
						if d.Key == args[0] {
							return d.Options, cobra.ShellCompDirectiveNoFileComp
						}
					}
				}
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}
	}
	a.Root.AddCommand(cmd)
}
func (a *App) syncCommands() {
	var message string
	var statusJSON bool
	cmd := &cobra.Command{Use: "sync", Aliases: []string{"save"}, Short: "Commit, pull, and push notes", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return unknownSubcommand(cmd, args)
		}
		c, s, e := a.store()
		if e != nil {
			return e
		}
		timeout, _ := time.ParseDuration(c.Git.Timeout)
		return (syncgit.Client{Store: s, Timeout: timeout}).Sync(cmd.Context(), message, false)
	}}
	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message")
	status := &cobra.Command{Use: "status", Short: "Show local Git sync state", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, s, e := a.store()
		if e != nil {
			return e
		}
		timeout, _ := time.ParseDuration(c.Git.Timeout)
		v, e := (syncgit.Client{Store: s, Timeout: timeout}).Status(cmd.Context())
		if e != nil {
			return e
		}
		if statusJSON {
			return json.NewEncoder(a.Out).Encode(v)
		}
		_, e = fmt.Fprintf(a.Out, "branch: %s\ndirty notes: %d\nahead: %d\nbehind: %d\nlast sync: %s\nlast commit: %s\n", v.Branch, len(v.Dirty), v.Ahead, v.Behind, v.LastSync, v.LastCommit)
		return e
	}}
	status.Flags().BoolVar(&statusJSON, "json", false, "print JSON")
	cmd.AddCommand(status)
	a.Root.AddCommand(cmd)
}
func (a *App) miscCommands() {
	a.Root.AddCommand(&cobra.Command{Use: "tui [NOTE]", Short: "Open interactive notes", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, s, e := a.store()
		if e != nil {
			return e
		}
		start := ""
		if len(args) > 0 {
			start = args[0]
		}
		return tui.Run(cmd.Context(), c, s, start, a.In, a.Out)
	}})
	var check bool
	var releaseVersion string
	u := &cobra.Command{Use: "update", Short: "Check and install GitHub releases", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, e := a.config()
		if e != nil {
			return e
		}
		client := update.Client{}
		result, release, e := client.Check(cmd.Context(), c.Update.Repository, Version, releaseVersion)
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintf(a.Out, "current: %s\nlatest: %s\navailable: %t\n", result.Current, result.Latest, result.Available); e != nil {
			return e
		}
		if check || !result.Available {
			return nil
		}
		if Version == "dev" && releaseVersion == "" {
			return usage("development binary requires --version for replacement")
		}
		if e = client.Install(cmd.Context(), release); e != nil {
			return e
		}
		_, e = fmt.Fprintln(a.Out, "installed", release.Tag)
		return e
	}}
	u.Flags().BoolVar(&check, "check", false, "check without replacing binary")
	u.Flags().StringVar(&releaseVersion, "version", "", "install one release tag")
	a.Root.AddCommand(u)
}
