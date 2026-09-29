package tui

// All panes share this cell budget so padding cannot hide the status/footer.
type screenLayout struct {
	width, bodyHeight, editorWidth, sidebarWidth int
}

func layoutFor(width, height int) screenLayout {
	width, height = max(80, width), max(16, height)
	inner := width - 4
	sidebar := min(34, max(24, inner/4))
	return screenLayout{inner, height - 9, inner - sidebar - 2, sidebar}
}

func (l screenLayout) editorSize() (int, int) { return l.editorWidth - 4, l.bodyHeight - 3 }

func (m *Model) sizeEditor() {
	if m.Editor != nil {
		m.Editor.SetSize(layoutFor(m.Width, m.Height).editorSize())
	}
}
