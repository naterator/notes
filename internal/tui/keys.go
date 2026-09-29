package tui

import (
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Printable bindings follow the typed character. Terminal decoders commonly
// report Shift+A as shift+a and Shift+/ as shift+/, while Text is A or ?.
func keyName(k tea.KeyPressMsg) string {
	if k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModHyper|tea.ModSuper) == 0 && utf8.RuneCountInString(k.Text) == 1 {
		r, _ := utf8.DecodeRuneInString(k.Text)
		if unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return k.Text
		}
	}
	return k.Keystroke()
}
