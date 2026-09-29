package store

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

type TagSpan struct {
	Tag        string
	Start, End int
}

var tagPattern = regexp.MustCompile(`#[\pL\pN][\pL\pN_-]*(?:/[\pL\pN][\pL\pN_-]*)*`)
var markdown = goldmark.New(goldmark.WithExtensions(extension.Linkify))

func TagSpans(source []byte) []TagSpan {
	root := markdown.Parser().Parse(text.NewReader(source))
	out := []TagSpan{}
	_ = ast.Walk(root, func(node ast.Node, enter bool) (ast.WalkStatus, error) {
		if !enter {
			return ast.WalkContinue, nil
		}
		switch node.(type) {
		case *ast.CodeSpan, *ast.CodeBlock, *ast.FencedCodeBlock, *ast.AutoLink, *ast.Image, *ast.RawHTML, *ast.HTMLBlock:
			return ast.WalkSkipChildren, nil
		}
		n, ok := node.(*ast.Text)
		if !ok {
			return ast.WalkContinue, nil
		}
		start, end := n.Segment.Start, n.Segment.Stop
		if start < 0 || end > len(source) || start >= end {
			return ast.WalkContinue, nil
		}
		for _, m := range tagPattern.FindAllIndex(source[start:end], -1) {
			a, z := start+m[0], start+m[1]
			if a > 0 {
				prev, _ := utf8.DecodeLastRune(source[:a])
				if !(unicode.IsSpace(prev) || unicode.Is(unicode.Ps, prev) || unicode.Is(unicode.Pi, prev) || strings.ContainsRune("([{'\"<,;:", prev)) {
					continue
				}
			}
			if a > 0 && source[a-1] == '\\' {
				continue
			}
			if z < len(source) {
				next, _ := utf8.DecodeRune(source[z:])
				if unicode.IsLetter(next) || unicode.IsNumber(next) || next == '_' || next == '-' {
					continue
				}
			}
			out = append(out, TagSpan{Tag: strings.ToLower(string(source[a+1 : z])), Start: a, End: z})
		}
		return ast.WalkContinue, nil
	})
	return out
}
func Tags(b []byte) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range TagSpans(b) {
		if !seen[s.Tag] {
			out = append(out, s.Tag)
			seen[s.Tag] = true
		}
	}
	sort.Strings(out)
	return out
}
func firstHeading(b []byte) (string, int, bool) {
	root := markdown.Parser().Parse(text.NewReader(b))
	var title string
	var end int
	_ = ast.Walk(root, func(node ast.Node, enter bool) (ast.WalkStatus, error) {
		if !enter {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Level != 1 || heading.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		candidate := strings.TrimSpace(string(heading.Text(b)))
		if candidate == "" {
			return ast.WalkContinue, nil
		}
		title = candidate
		end = headingEnd(b, heading)
		return ast.WalkStop, nil
	})
	return title, end, title != ""
}
func headingEnd(b []byte, heading *ast.Heading) int {
	first := heading.Lines().At(0).Start
	last := heading.Lines().At(heading.Lines().Len() - 1).Stop
	end := lineEnd(b, last)
	lineStart := bytes.LastIndexByte(b[:first], '\n') + 1
	prefix := bytes.TrimSpace(b[lineStart:first])
	if !bytes.HasSuffix(prefix, []byte("#")) {
		// Setext headings include an underline that is not in Lines().
		end = lineEnd(b, end)
	}
	return end
}
func lineEnd(b []byte, from int) int {
	for from < len(b) && b[from] != '\n' {
		from++
	}
	if from < len(b) {
		from++
	}
	return from
}
func titleEnd(b []byte) (int, bool) {
	_, end, found := firstHeading(b)
	return end, found
}
func managedRange(b []byte) (int, int, bool) {
	offset, _ := titleEnd(b)
	for _, line := range bytes.SplitAfter(b[offset:], []byte("\n")) {
		trim := strings.TrimSpace(string(line))
		indent := line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
		if len(indent) <= 3 && !bytes.ContainsRune(indent, '\t') && strings.HasPrefix(trim, "Tags:") {
			return offset, offset + len(line), true
		}
		if trim != "" {
			break
		}
		offset += len(line)
	}
	return 0, 0, false
}
func AddTags(b []byte, tags []string) ([]byte, error) {
	requested, err := cleanTags(tags)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, v := range Tags(b) {
		seen[v] = true
	}
	var add []string
	for _, v := range requested {
		if !seen[v] {
			add = append(add, "#"+v)
		}
	}
	if len(add) == 0 {
		return b, nil
	}
	a, z, ok := managedRange(b)
	if ok {
		line := strings.TrimRight(string(b[a:z]), "\r\n")
		suffix := b[a+len(line) : z]
		out := append([]byte{}, b[:a]...)
		out = append(out, []byte(line+" "+strings.Join(add, " "))...)
		out = append(out, suffix...)
		out = append(out, b[z:]...)
		return out, nil
	}
	newline := "\n"
	if bytes.Contains(b, []byte("\r\n")) {
		newline = "\r\n"
	}
	tagLine := "Tags: " + strings.Join(add, " ") + newline
	if len(b) == 0 {
		return []byte(tagLine), nil
	}
	if end, found := titleEnd(b); found {
		before := newline
		if end == 0 || b[end-1] != '\n' {
			before += newline
		}
		after := ""
		if end < len(b) && !bytes.HasPrefix(b[end:], []byte(newline)) {
			after = newline
		}
		out := append([]byte{}, b[:end]...)
		out = append(out, []byte(before+tagLine+after)...)
		out = append(out, b[end:]...)
		return out, nil
	}
	out := append([]byte(tagLine+newline), b...)
	return out, nil
}
func RemoveTags(b []byte, tags []string) ([]byte, error) {
	requested, err := cleanTags(tags)
	if err != nil {
		return nil, err
	}
	remove := map[string]bool{}
	for _, v := range requested {
		remove[v] = true
	}
	spans := TagSpans(b)
	a, z, managed := managedRange(b)
	out := append([]byte{}, b...)
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		if !remove[s.Tag] || managed && s.Start >= a && s.Start < z {
			continue
		}
		out = append(out[:s.Start], out[s.Start+1:]...) // prose: retain the word
	}
	if managed {
		// A tag in the title may have shifted the original managed-line offset.
		a, z, _ = managedRange(out)
		line := string(out[a:z])
		keep := []string{}
		for _, word := range strings.Fields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Tags:"))) {
			if !remove[strings.ToLower(strings.TrimPrefix(word, "#"))] {
				keep = append(keep, word)
			}
		}
		replacement := "Tags: " + strings.Join(keep, " ")
		if len(keep) == 0 {
			replacement = ""
		} else if strings.HasSuffix(line, "\r\n") {
			replacement += "\r\n"
		} else if strings.HasSuffix(line, "\n") {
			replacement += "\n"
		}
		out = append(append([]byte{}, out[:a]...), append([]byte(replacement), out[z:]...)...)
	}
	return out, nil
}
func (s *Store) TagsEdit(id string, tags []string, remove bool) (bool, error) {
	n, err := s.Read(id)
	if err != nil {
		return false, err
	}
	var next []byte
	if remove {
		next, err = RemoveTags(n.Content, tags)
	} else {
		next, err = AddTags(n.Content, tags)
	}
	if err != nil {
		return false, err
	}
	if bytes.Equal(next, n.Content) {
		return false, nil
	}
	if err := s.Save(id, sha256.Sum256(n.Content), next); err != nil {
		return false, err
	}
	return true, nil
}
func TagCounts(notes []Note) map[string]int {
	counts := map[string]int{}
	for _, n := range notes {
		for _, t := range n.Tags {
			counts[t]++
		}
	}
	return counts
}
func ValidateTag(tag string) error {
	_, err := cleanTags([]string{tag})
	if err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}
