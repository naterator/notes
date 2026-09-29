package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/naterator/notes/internal/config"
	"github.com/naterator/notes/internal/theme"
)

func humanColor(out io.Writer, c config.Config, kind, value string) string {
	terminal := false
	if f, ok := out.(*os.File); ok {
		terminal = term.IsTerminal(f.Fd())
	}
	if !theme.Enabled(c.Color, terminal) {
		return value
	}
	if c.Theme == "terminal" {
		code := "36"
		if kind == "muted" {
			code = "2"
		}
		return "\x1b[" + code + "m" + value + "\x1b[0m"
	}
	p := theme.Get(c.Theme)
	hex := p.Accent
	if kind == "muted" {
		hex = p.Muted
	}
	hex = strings.TrimPrefix(hex, "#")
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, value)
}
