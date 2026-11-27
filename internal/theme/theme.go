package theme

import (
	"os"
	"strings"
)

const Default = "default"

type Palette struct {
	Field, Key                                                   string
	Base, Surface, Text, Muted, Border, Selection, SelectionText string
	Accent, Success, Warning, Error                              string
}

type Scheme struct {
	ID, Name, Description, Kind string
	Palette                     Palette
}

// qbitctl palettes from naterator/qbitctl 1.4.1 (8842804).
// Notes uses green for success where qbitctl has no corresponding color role.
var schemes = []Scheme{
	qbitctlScheme("default", "Default", "Arctic blue-gray", "#2E3440", "#3B4252", "#343C4C", "#ECEFF4", "#AAB4C3", "#8FBCBB", "#88C0D0", "#4C566A", "#1F2D37", "#8FBCBB", "#FF8F9C", "#EBCB8B"),
	qbitctlScheme("ocean", "Ocean", "Deep blue with bright cyan accents", "#0B1220", "#17233A", "#111D32", "#E5F2FF", "#8EA6C2", "#67E8F9", "#7DD3FC", "#355175", "#07121D", "#67E8F9", "#FB7185", "#FBCB7A"),
	qbitctlScheme("forest", "Forest", "Calm green terminal garden", "#0D1B16", "#17352A", "#122920", "#E8F5EC", "#98BCA7", "#7EE2A8", "#9BE7C2", "#3A6D55", "#092017", "#7EE2A8", "#FF9E9E", "#F6D487"),
	qbitctlScheme("amber", "Amber", "Warm gold on dark brown", "#1F1710", "#382818", "#2B2116", "#FFF3DD", "#BDAE99", "#F4C95D", "#FFD89A", "#715531", "#2A1B06", "#F4C95D", "#FF9B8F", "#FFD17A"),
	qbitctlScheme("rose", "Rose", "Soft rose and plum", "#21141C", "#3B2431", "#2D1C27", "#FFF0F6", "#C2A6B5", "#F5A3C7", "#FFBCD8", "#70415C", "#29101E", "#F5A3C7", "#FF9B9B", "#FFD39A"),
	qbitctlScheme("violet", "Violet", "Purple surfaces with cool highlights", "#171424", "#2A2140", "#211B35", "#F3EEFF", "#B1A6CB", "#C4A7FF", "#B8D5FF", "#5F4C89", "#21143B", "#C4A7FF", "#FF9FAA", "#FFDA8A"),
	qbitctlScheme("mono", "Monochrome", "Quiet grayscale with clear hierarchy", "#181A1F", "#2C3038", "#22262D", "#F2F4F7", "#AEB5C0", "#D3DBE6", "#FFFFFF", "#5C6472", "#1A1D23", "#D3DBE6", "#FFB4B4", "#E9C98E"),
	qbitctlScheme("tookie", "Tookie", "qbitctl's original palette", "#1D2531", "#334155", "#242F3F", "#E9EEF5", "#AFBCCD", "#85D7C8", "#9AC8FF", "#536478", "#102A2D", "#92DACD", "#FFAEA9", "#FFD18E"),
	qbitctlScheme("dracula", "Dracula", "High-contrast purple night", "#1E1F29", "#282A36", "#232532", "#F8F8F2", "#A9A9B7", "#8BE9FD", "#BD93F9", "#6272A4", "#21142E", "#BD93F9", "#FF6E6E", "#FFB86C"),
	qbitctlScheme("solarized", "Solarized", "Classic teal and gold", "#002B36", "#073642", "#06313C", "#EEE8D5", "#93A1A1", "#5FD7CB", "#6CC3FF", "#586E75", "#002B36", "#B58900", "#FF6B66", "#F0C674"),
	qbitctlScheme("gruvbox", "Gruvbox", "Earthy retro terminal colors", "#282828", "#3C3836", "#32302F", "#EBDBB2", "#D5C4A1", "#8EC07C", "#A8C9E8", "#665C54", "#282828", "#FABD2F", "#FF9280", "#FE8019"),
	qbitctlScheme("catppuccin", "Catppuccin", "Pastel accents on mocha", "#1E1E2E", "#313244", "#242437", "#CDD6F4", "#A6ADC8", "#94E2D5", "#89B4FA", "#585B70", "#1E1E2E", "#CBA6F7", "#F38BA8", "#F9E2AF"),
	qbitctlScheme("high-contrast", "High contrast", "Maximum separation for accessibility", "#000000", "#1A1A1A", "#0E0E0E", "#FFFFFF", "#D0D0D0", "#00FFB3", "#4DB8FF", "#777777", "#000000", "#00FFB3", "#FF5F56", "#FFD75F"),
	qbitctlScheme("paper", "Paper", "Light parchment with ink-blue accents", "#F4F1EA", "#E7E1D7", "#F0ECE4", "#252525", "#4E4A45", "#00645B", "#005FA8", "#B6AEA2", "#FFFFFF", "#00645B", "#A62828", "#8A5A00"),
	qbitctlScheme("midnight", "Midnight", "Nearly black with electric cyan", "#0A0F1C", "#111A2E", "#0E1626", "#EBF3FF", "#8798B5", "#76E4F2", "#A9C7FF", "#2D476B", "#05131D", "#76E4F2", "#FF8F9C", "#FBD38D"),
	qbitctlScheme("terminal", "Terminal", "Green phosphor with old-school contrast", "#001B0B", "#003219", "#002610", "#B8FFCB", "#5FCB7A", "#21FF7A", "#5CFFAF", "#1B7843", "#001B0B", "#21FF7A", "#FF7777", "#E5FF77"),
	qbitctlScheme("sunset", "Sunset", "Coral light against plum", "#24151D", "#3D2333", "#2D1B28", "#FFF1E8", "#C4A2A8", "#FFB07C", "#FFCF93", "#75445A", "#2A1720", "#FFB07C", "#FF8C8C", "#FFE095"),
	// Previous Notes palettes and the terminal-native option.
	{"mocha", "Mocha", "Original Notes Mocha", "dark", schemesHistory},
	{"frappe", "Frappé", "Soft gray and purple", "dark", Palette{Base: "#303446", Surface: "#414559", Text: "#c6d0f5", Muted: "#a5adce", Border: "#737994", Selection: "#51576d", SelectionText: "#c6d0f5", Accent: "#ca9ee6", Success: "#a6d189", Warning: "#e5c890", Error: "#e78284"}},
	{"catppuccin-macchiato", "Catppuccin Macchiato", "Blue-gray and purple", "dark", Palette{Base: "#24273a", Surface: "#363a4f", Text: "#cad3f5", Muted: "#a5adcb", Border: "#6e738d", Selection: "#494d64", SelectionText: "#cad3f5", Accent: "#c6a0f6", Success: "#a6da95", Warning: "#eed49f", Error: "#ed8796"}},
	{"catppuccin-latte", "Catppuccin Latte", "Light pastel purple", "light", Palette{Base: "#eff1f5", Surface: "#ccd0da", Text: "#4c4f69", Muted: "#5c5f77", Border: "#9ca0b0", Selection: "#bcc0cc", SelectionText: "#303446", Accent: "#8839ef", Success: "#40a02b", Warning: "#df8e1d", Error: "#d20f39"}},
	{"nord", "Nord", "Cool gray and cyan", "dark", Palette{Base: "#2e3440", Surface: "#3b4252", Text: "#eceff4", Muted: "#d8dee9", Border: "#4c566a", Selection: "#434c5e", SelectionText: "#eceff4", Accent: "#88c0d0", Success: "#a3be8c", Warning: "#ebcb8b", Error: "#bf616a"}},
	{"gruvbox-dark", "Gruvbox Dark", "Warm charcoal and yellow", "dark", Palette{Base: "#282828", Surface: "#3c3836", Text: "#ebdbb2", Muted: "#bdae93", Border: "#7c6f64", Selection: "#504945", SelectionText: "#ebdbb2", Accent: "#fabd2f", Success: "#b8bb26", Warning: "#fe8019", Error: "#fb4934"}},
	{"solarized-light", "Solarized Light", "Warm ivory and blue", "light", Palette{Base: "#fdf6e3", Surface: "#eee8d5", Text: "#073642", Muted: "#586e75", Border: "#93a1a1", Selection: "#93a1a1", SelectionText: "#002b36", Accent: "#268bd2", Success: "#859900", Warning: "#b58900", Error: "#dc322f"}},
	{"notes-midnight", "Notes Midnight", "Deep navy and ice blue", "dark", Palette{Base: "#101827", Surface: "#1a2638", Text: "#e2ebf7", Muted: "#a0b3cc", Border: "#526782", Selection: "#304965", SelectionText: "#f0f6ff", Accent: "#87c5ff", Success: "#94d9b3", Warning: "#f3d18c", Error: "#ff9baf"}},
	{"graphite", "Graphite", "Neutral charcoal and silver", "dark", Palette{Base: "#181a1d", Surface: "#272a2f", Text: "#e4e6e9", Muted: "#adb2bb", Border: "#626975", Selection: "#414852", SelectionText: "#f5f6f8", Accent: "#c1c9d6", Success: "#a8d5b5", Warning: "#e3ca92", Error: "#eaa5a5"}},
	{"notes-ocean", "Notes Ocean", "Deep teal and turquoise", "dark", Palette{Base: "#0e2027", Surface: "#19323b", Text: "#d9eff0", Muted: "#9dc0c5", Border: "#4a7784", Selection: "#28505c", SelectionText: "#eaffff", Accent: "#6eddd6", Success: "#a7dfb2", Warning: "#f0d58f", Error: "#ffa8a0"}},
	{"pine", "Pine", "Forest green and sage", "dark", Palette{Base: "#16211b", Surface: "#26362b", Text: "#e0ecdb", Muted: "#a8bfa5", Border: "#5a795d", Selection: "#3a5140", SelectionText: "#f0fae9", Accent: "#a4d69c", Success: "#86d4ad", Warning: "#e5cf91", Error: "#f2a597"}},
	{"ember", "Ember", "Warm soot and copper", "dark", Palette{Base: "#241a17", Surface: "#372722", Text: "#f4e5d8", Muted: "#ceb3a0", Border: "#89614e", Selection: "#5b3b2d", SelectionText: "#fff1e6", Accent: "#ffba87", Success: "#b4d79d", Warning: "#f7d382", Error: "#ff9b8d"}},
	{"amethyst", "Amethyst", "Deep plum and lavender", "dark", Palette{Base: "#20192c", Surface: "#32263f", Text: "#eee2f7", Muted: "#bfaacc", Border: "#7d6491", Selection: "#503b62", SelectionText: "#f9efff", Accent: "#d1adff", Success: "#a5dcb5", Warning: "#eed29c", Error: "#f6a4be"}},
	{"notes-rose", "Notes Rose", "Burgundy and dusty pink", "dark", Palette{Base: "#281a23", Surface: "#3d2833", Text: "#f5e1ea", Muted: "#cba9bb", Border: "#8e647a", Selection: "#5c3b4d", SelectionText: "#fff0f6", Accent: "#f3a8cd", Success: "#b3d5ac", Warning: "#edd299", Error: "#ffa59e"}},
	{"sand", "Sand", "Warm cream and ochre", "light", Palette{Base: "#faf4e8", Surface: "#eee3ce", Text: "#493c2c", Muted: "#74634d", Border: "#aa9273", Selection: "#d9c49f", SelectionText: "#35291c", Accent: "#805620", Success: "#42683c", Warning: "#855413", Error: "#a23e35"}},
	{"mint", "Mint", "Pale green and deep teal", "light", Palette{Base: "#f0f8f2", Surface: "#dcece1", Text: "#233c32", Muted: "#506c5d", Border: "#83a68e", Selection: "#b9d7c4", SelectionText: "#19352a", Accent: "#216452", Success: "#35663b", Warning: "#805919", Error: "#a33f4b"}},
	{"blueprint", "Blueprint", "Cool white and vivid blue", "light", Palette{Base: "#f3f7fc", Surface: "#e0e9f5", Text: "#22364f", Muted: "#536c88", Border: "#87a3c4", Selection: "#bed2eb", SelectionText: "#18304f", Accent: "#215caa", Success: "#326745", Warning: "#835919", Error: "#ac3d50"}},
	{"terminal-native", "Terminal Native", "Inherit terminal colors", "terminal", Palette{}},
	{"history", "History", "Previous Notes default (Mocha)", "dark", schemesHistory},
}

var schemesHistory = Palette{Base: "#1e1e2e", Surface: "#313244", Text: "#cdd6f4", Muted: "#a6adc8", Border: "#6c7086", Selection: "#45475a", SelectionText: "#cdd6f4", Accent: "#cba6f7", Success: "#a6e3a1", Warning: "#f9e2af", Error: "#f38ba8"}

func qbitctlScheme(id, label, description, base, panel, field, text, muted, accent, key, rule, selectedText, selected, failure, warning string) Scheme {
	kind, success := "dark", "#A6E3A1"
	if id == "paper" {
		kind, success = "light", "#326745"
	}
	return Scheme{ID: id, Name: label, Description: description, Kind: kind, Palette: Palette{
		Base: base, Surface: panel, Field: field, Text: text, Muted: muted, Border: rule,
		Selection: selected, SelectionText: selectedText, Accent: accent, Key: key,
		Success: success, Warning: warning, Error: failure,
	}}
}

func List() []Scheme { return append([]Scheme(nil), schemes...) }

var aliases = map[string]string{"catppuccin-mocha": "mocha", "catppuccin-frappe": "frappe"}

func Names() []string {
	names := make([]string, len(schemes))
	for i, s := range schemes {
		names[i] = s.ID
	}
	return names
}
func AcceptedNames() []string { return append(Names(), "catppuccin-mocha", "catppuccin-frappe") }

func Lookup(name string) (Scheme, bool) {
	if id, ok := aliases[name]; ok {
		name = id
	}
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
