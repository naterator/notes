<img src="assets/logo.svg" alt="notes logo" width="64" height="64">

# notes

[![CI](https://github.com/naterator/notes/actions/workflows/ci.yml/badge.svg)](https://github.com/naterator/notes/actions/workflows/ci.yml) [![Latest release](https://img.shields.io/github/v/release/naterator/notes?label=release&color=cba6f7)](https://github.com/naterator/notes/releases/latest)

`notes` is a Go CLI for Markdown notes, daily journals, tags, and a terminal editor. Notes stay as ordinary files in a Git repository.

- Pipe-friendly commands for creating, finding, listing, editing, moving, and deleting notes.
- Daily journals with Activities, Actions, and Notes sections.
- A three-pane TUI with visible focus, tag navigation, search, and Traditional or Vim-style editing.
- Optional automatic Git sync, themes, and explicit self-updates.

![Recorded Notes session: creating and editing in the TUI, then CLI commands](assets/demo.gif)

[Replayable terminal recording](assets/demo.cast) · [Recording script](scripts/record_demo.py)

## Install

Download the latest binary for macOS or Linux on amd64 or arm64:

```sh
curl -fLo notes "https://github.com/naterator/notes/releases/latest/download/notes-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
chmod +x notes
sudo mv notes /usr/local/bin/
```

## Build from source

Requires Go 1.27.1 or newer and Git:

```sh
git clone https://github.com/naterator/notes.git
cd notes
go build -trimpath -o notes ./cmd/notes
./notes version
sudo install -m 0755 notes /usr/local/bin/notes
```

## Set up

- Settings: `${XDG_CONFIG_HOME:-$HOME/.config}/notes/config.toml`.
- Notes: `${XDG_CONFIG_HOME:-$HOME/.config}/notes/repo` by default; use `--repo`, `NOTES_REPO`, or `notes config set repo PATH` to change it.
- Notes initializes that directory as its own Git repository on first use. Add your remote to enable syncing:

```sh
repo="${XDG_CONFIG_HOME:-$HOME/.config}/notes/repo"
notes --repo "$repo" list >/dev/null
git -C "$repo" remote add origin YOUR_REMOTE_URL
```

Git also needs `user.name` and `user.email`. For a local-only notebook, run `notes config set git.auto_sync false`.

Without an `origin` remote, notes stay local and automatic sync waits for one to be added.

## Commands

- `notes new "Idea" --tag inbox` creates a note and prints its path. Add `--category work`, `--path work/idea.md`, `--template FILE`, or `--stdin` as needed.
- `notes show NOTE` prints exact Markdown; `notes edit NOTE` opens the TUI unless an external editor is configured with `EDITOR`, `VISUAL`, or `notes config set editor '["vim"]'`.
- `notes list` (`l`, `ls`) shows `base: REPO` and `/`-prefixed paths in a terminal. In a pipeline it prints absolute paths; `notes find QUERY` also prints paths. Use `--tag`, `--category`, `--sort`, `--limit`, `--relative`, `--print0`, `--json`, or `--long`; `find` also accepts `--regex` and `--matches`.
- `notes tags`, `notes tags add NOTE TAG...`, and `notes tags remove NOTE TAG...` manage `#tags` in Markdown. Inline tags count; code, URLs, and escaped hashes do not.
- `notes move NOTE DESTINATION` (`mv`), `notes delete NOTE --yes` (`rm`), and `notes categories` (`cats`) manage files and folders.
- `notes journal` prints today's journal path. `notes journal add "Reviewed notes" --section actions --tag work` appends an entry; use `--date YYYY-MM-DD` for another day.
- `notes sync` (`save`) commits note changes, pulls from `origin`, and pushes. `notes sync status --json` inspects local state without network access.
- `notes config render`, `get KEY`, `set KEY VALUE`, `unset KEY`, `path`, and `edit` manage TOML settings.
- `notes tui` opens the terminal interface; `notes update --check`, `notes version`, and `notes completion SHELL` provide maintenance commands.
- Top-level commands accept short prefixes: `notes c` for categories, `co` for config, `com` for completion, `l` for list, `s` for show, and `sy` for sync. Other commands use `d`, `e`, `f`, `h`, `j`, `m`, `n`, `ta`, `t`, `u`, or `v`; longer prefixes work too.
- Nested commands also accept unambiguous prefixes, such as `notes co rend`, `notes j a`, `notes ta rem`, and `notes sy stat`.

For pipelines, path output has no color or headers. Use `--print0` for names with whitespace or newlines:

```sh
note=$(notes new "Quick thought" --tag inbox)
notes show "$note"
notes find 'thought' --print0 | xargs -0 -n 1 cat
```

`new --stdin` and `journal add --stdin` read prose from stdin. A saved note whose automatic sync fails returns exit code 3; retry `notes sync` instead of repeating the write. Run `notes --help` or `notes COMMAND --help` for all flags.

## Terminal editor

- On first launch, Vim is preselected; choose Vim or Traditional (nano-like). The choice is saved as `tui.editor_mode`.
- The search box starts focused when opening the TUI without a note; `notes tui NOTE` and TUI-backed `notes edit NOTE` start with the editor focused. `[FOCUS]`, a pane marker, and the footer show the current area and mode, including without color.
- `Ctrl+G` shows action keys: `n` new, `j` journal, `t` tags, `f` find in note, `a` all-notes search, `b` tree, `e` editor, `s` save, `y` sync, `r` refresh, `c` settings, `q` quit, `h` help. `Ctrl+S` and `Ctrl+Q` also work where the terminal passes them through.
- `Tab` cycles search → tree → editor. In editor text entry, Tab inserts a real tab; press `Esc` before using Tab to change focus. Shift+Tab cycles backward outside entry mode.
- `?` opens help in command/navigation mode and types `?` during text entry. `Ctrl+G h` always opens help. `/` in the all-notes search opens search within the current note; `Ctrl+V` then `/` inserts a literal slash in the all-notes query.
- `:` in command mode opens built-in commands such as `:new`, `:tags`, `:find`, `:sync`, `:settings`/`:config`, `:themes`, `:w`, `:q`, and `:wq`. It is not a shell.
- Vim mode supports `h/j/k/l`, `w/b/e`, `0/^/$`, `gg/G`, counts, `i/a/I/A/o/O`, `x`, `dd/dw/d$`, `cc/cw/c$`, `yy/yw`, `p/P`, `u`, `Ctrl+R`, `.`, `v/V`, `/`, and `n/N`. This is a built-in subset, not full Vim. Traditional mode supports arrows, selection, `Ctrl+A/E`, `Ctrl+K/U`, and `Ctrl+O`.
- The TUI edits files up to 2 MiB. Configure an external editor to use `notes edit NOTE` for larger files. Autosave checks for external changes and offers a recovery draft instead of overwriting them.

## Settings and sync

- Defaults include `theme = "catppuccin-mocha"`, `git.auto_sync = true`, `git.max_interval = "5m"`, and `tui.autosave_delay = "1s"`.
- Themes include `catppuccin-mocha`, `catppuccin-frappe`, `catppuccin-macchiato`, `catppuccin-latte`, `nord`, `gruvbox-dark`, `solarized-light`, and `terminal`.
- Original dark themes: `midnight` (navy/ice blue), `graphite` (charcoal/silver), `ocean` (teal/turquoise), `pine` (forest/sage), `ember` (soot/copper), `amethyst` (plum/lavender), and `rose` (burgundy/pink).
- Original light themes: `sand` (cream/ochre), `mint` (pale green/teal), and `blueprint` (cool white/blue). Set one with `notes config set theme NAME` or preview all 18 with `:themes`.
- Open interactive settings with `Ctrl+G c` or `:settings`. Search grouped settings, press Enter to edit/save one value, and use `Ctrl+R` to remove its saved preference. Esc cancels an edit or closes settings; `Ctrl+G h` shows help.
- `:themes` previews schemes while browsing; Enter saves the choice and Esc restores the original appearance. Settings shows saved values and their effective source, including active environment/flag overrides.
- Repository changes apply on the next launch and do not move notes. Editor keys, colors, and future autosave/sync settings can change during the session; selecting editor mode `ask` restores the prompt on the next launch. Config edits use the same validation and TOML rewriting as `notes config set`, including its comment/formatting behavior.
- Use `notes config render` for effective settings. Explicit flags override supported environment variables, which override TOML and defaults. `--theme`, `--color auto|always|never`, and `--no-color` control colors.
- Git due checks run after note-changing CLI commands and on a timer while the TUI is open. A closed CLI cannot sync on a wall-clock deadline; schedule `notes sync` with your OS if needed.
- Sync stages Markdown changes only, fetches, commits when needed, pulls, then pushes. Conflicts and offline failures leave local changes intact and pending; resolve them with Git and rerun `notes sync`.

## Updates and license

- `notes update --check` checks GitHub without changing the binary. `notes update` installs a newer release; a development build needs `--version X.Y.Z` (or `vX.Y.Z`) to install one explicitly.
- Releases contain one binary per supported OS/architecture, named `notes-OS-ARCH`. The updater checks the SHA-256 digest in GitHub's asset metadata before replacing the installed binary; this detects corruption, not a compromised release account.
- Source is BSD 3-Clause licensed; see [LICENSE](LICENSE). Color palettes: [Catppuccin](https://catppuccin.com/palette/), [Nord](https://www.nordtheme.com/docs/colors-and-palettes/), [Gruvbox](https://github.com/morhetz/gruvbox), and [Solarized](https://ethanschoonover.com/solarized/).
