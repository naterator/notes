package theme

import (
	"os"
	"strings"
)

type Palette struct {
	Base, Surface, Text, Muted, Border, Selection, SelectionText string
	Accent, Success, Warning, Error                              string
}

type Scheme struct {
	ID, Name, Description, Kind string
	Palette                     Palette
}

// Palette sources: https://catppuccin.com/palette/,
// https://www.nordtheme.com/docs/colors-and-palettes/,
// https://github.com/morhetz/gruvbox, https://ethanschoonover.com/solarized/.
// Selection foregrounds are paired explicitly for readable light themes.
var schemes = []Scheme{
	{"catppuccin-mocha", "Catppuccin Mocha", "Pastel purple · default", "dark", Palette{Base: "#1e1e2e", Surface: "#313244", Text: "#cdd6f4", Muted: "#a6adc8", Border: "#6c7086", Selection: "#45475a", SelectionText: "#cdd6f4", Accent: "#cba6f7", Success: "#a6e3a1", Warning: "#f9e2af", Error: "#f38ba8"}},
	{"catppuccin-frappe", "Catppuccin Frappé", "Soft gray and purple", "dark", Palette{Base: "#303446", Surface: "#414559", Text: "#c6d0f5", Muted: "#a5adce", Border: "#737994", Selection: "#51576d", SelectionText: "#c6d0f5", Accent: "#ca9ee6", Success: "#a6d189", Warning: "#e5c890", Error: "#e78284"}},
	{"catppuccin-macchiato", "Catppuccin Macchiato", "Blue-gray and purple", "dark", Palette{Base: "#24273a", Surface: "#363a4f", Text: "#cad3f5", Muted: "#a5adcb", Border: "#6e738d", Selection: "#494d64", SelectionText: "#cad3f5", Accent: "#c6a0f6", Success: "#a6da95", Warning: "#eed49f", Error: "#ed8796"}},
	{"catppuccin-latte", "Catppuccin Latte", "Light pastel purple", "light", Palette{Base: "#eff1f5", Surface: "#ccd0da", Text: "#4c4f69", Muted: "#5c5f77", Border: "#9ca0b0", Selection: "#bcc0cc", SelectionText: "#303446", Accent: "#8839ef", Success: "#40a02b", Warning: "#df8e1d", Error: "#d20f39"}},
	{"nord", "Nord", "Cool gray and cyan", "dark", Palette{Base: "#2e3440", Surface: "#3b4252", Text: "#eceff4", Muted: "#d8dee9", Border: "#4c566a", Selection: "#434c5e", SelectionText: "#eceff4", Accent: "#88c0d0", Success: "#a3be8c", Warning: "#ebcb8b", Error: "#bf616a"}},
	{"gruvbox-dark", "Gruvbox Dark", "Warm charcoal and yellow", "dark", Palette{Base: "#282828", Surface: "#3c3836", Text: "#ebdbb2", Muted: "#bdae93", Border: "#7c6f64", Selection: "#504945", SelectionText: "#ebdbb2", Accent: "#fabd2f", Success: "#b8bb26", Warning: "#fe8019", Error: "#fb4934"}},
	{"solarized-light", "Solarized Light", "Warm ivory and blue", "light", Palette{Base: "#fdf6e3", Surface: "#eee8d5", Text: "#073642", Muted: "#586e75", Border: "#93a1a1", Selection: "#93a1a1", SelectionText: "#002b36", Accent: "#268bd2", Success: "#859900", Warning: "#b58900", Error: "#dc322f"}},
	// Original Notes palettes; normal and selected text meet 4.5:1 contrast.
	{"midnight", "Midnight", "Deep navy and ice blue", "dark", Palette{Base: "#101827", Surface: "#1a2638", Text: "#e2ebf7", Muted: "#a0b3cc", Border: "#526782", Selection: "#304965", SelectionText: "#f0f6ff", Accent: "#87c5ff", Success: "#94d9b3", Warning: "#f3d18c", Error: "#ff9baf"}},
	{"graphite", "Graphite", "Neutral charcoal and silver", "dark", Palette{Base: "#181a1d", Surface: "#272a2f", Text: "#e4e6e9", Muted: "#adb2bb", Border: "#626975", Selection: "#414852", SelectionText: "#f5f6f8", Accent: "#c1c9d6", Success: "#a8d5b5", Warning: "#e3ca92", Error: "#eaa5a5"}},
	{"ocean", "Ocean", "Deep teal and turquoise", "dark", Palette{Base: "#0e2027", Surface: "#19323b", Text: "#d9eff0", Muted: "#9dc0c5", Border: "#4a7784", Selection: "#28505c", SelectionText: "#eaffff", Accent: "#6eddd6", Success: "#a7dfb2", Warning: "#f0d58f", Error: "#ffa8a0"}},
	{"pine", "Pine", "Forest green and sage", "dark", Palette{Base: "#16211b", Surface: "#26362b", Text: "#e0ecdb", Muted: "#a8bfa5", Border: "#5a795d", Selection: "#3a5140", SelectionText: "#f0fae9", Accent: "#a4d69c", Success: "#86d4ad", Warning: "#e5cf91", Error: "#f2a597"}},
	{"ember", "Ember", "Warm soot and copper", "dark", Palette{Base: "#241a17", Surface: "#372722", Text: "#f4e5d8", Muted: "#ceb3a0", Border: "#89614e", Selection: "#5b3b2d", SelectionText: "#fff1e6", Accent: "#ffba87", Success: "#b4d79d", Warning: "#f7d382", Error: "#ff9b8d"}},
	{"amethyst", "Amethyst", "Deep plum and lavender", "dark", Palette{Base: "#20192c", Surface: "#32263f", Text: "#eee2f7", Muted: "#bfaacc", Border: "#7d6491", Selection: "#503b62", SelectionText: "#f9efff", Accent: "#d1adff", Success: "#a5dcb5", Warning: "#eed29c", Error: "#f6a4be"}},
	{"rose", "Rose", "Burgundy and dusty pink", "dark", Palette{Base: "#281a23", Surface: "#3d2833", Text: "#f5e1ea", Muted: "#cba9bb", Border: "#8e647a", Selection: "#5c3b4d", SelectionText: "#fff0f6", Accent: "#f3a8cd", Success: "#b3d5ac", Warning: "#edd299", Error: "#ffa59e"}},
	{"sand", "Sand", "Warm cream and ochre", "light", Palette{Base: "#faf4e8", Surface: "#eee3ce", Text: "#493c2c", Muted: "#74634d", Border: "#aa9273", Selection: "#d9c49f", SelectionText: "#35291c", Accent: "#805620", Success: "#42683c", Warning: "#855413", Error: "#a23e35"}},
	{"mint", "Mint", "Pale green and deep teal", "light", Palette{Base: "#f0f8f2", Surface: "#dcece1", Text: "#233c32", Muted: "#506c5d", Border: "#83a68e", Selection: "#b9d7c4", SelectionText: "#19352a", Accent: "#216452", Success: "#35663b", Warning: "#805919", Error: "#a33f4b"}},
	{"blueprint", "Blueprint", "Cool white and vivid blue", "light", Palette{Base: "#f3f7fc", Surface: "#e0e9f5", Text: "#22364f", Muted: "#536c88", Border: "#87a3c4", Selection: "#bed2eb", SelectionText: "#18304f", Accent: "#215caa", Success: "#326745", Warning: "#835919", Error: "#ac3d50"}},
	{"terminal", "Terminal", "Inherit terminal colors", "terminal", Palette{}},
}

func List() []Scheme { return append([]Scheme(nil), schemes...) }
func Names() []string {
	names := make([]string, len(schemes))
	for i, s := range schemes {
		names[i] = s.ID
	}
	return names
}
func Lookup(name string) (Scheme, bool) {
	for _, s := range schemes {
		if s.ID == name {
			return s, true
		}
	}
	return Scheme{}, false
}
func Get(name string) Palette {
	if s, ok := Lookup(name); ok {
		return s.Palette
	}
	return schemes[0].Palette
}
func Enabled(mode string, terminal bool) bool {
	if mode == "never" {
		return false
	}
	if mode == "always" {
		return true
	}
	return terminal && os.Getenv("NO_COLOR") == "" && strings.ToLower(os.Getenv("TERM")) != "dumb"
}
