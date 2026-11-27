package notes

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// Query selects notes by category, exact tags, a hierarchical tag, and
// a pattern matched against Markdown and title. Combine filters with AND.
type Query struct {
	Category  string
	Tags      []string
	TagPrefix string
	Untagged  bool
	Matcher   *regexp.Regexp
	Sort      string // path (default), modified, title, or category
	Reverse   bool
	Limit     int // zero means all
}

// Query reads the current notebook and filters the resulting snapshot.
func (s *Store) Query(ctx context.Context, q Query) ([]Note, error) {
	records, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	return FilterNotes(ctx, records, q)
}

// FilterNotes matches a snapshot without reading from disk. It leaves the
// input order and records intact. With no sort, results are ordered by ID.
func FilterNotes(ctx context.Context, notes []Note, f Query) ([]Note, error) {
	out := make([]Note, 0, len(notes))
	for _, n := range notes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if f.Category != "" && n.Category != f.Category && !strings.HasPrefix(n.Category, f.Category+"/") {
			continue
		}
		if f.Untagged && len(n.Tags) != 0 {
			continue
		}
		if f.TagPrefix != "" && !n.HasTagPrefix(f.TagPrefix) {
			continue
		}
		ok := true
		for _, tag := range f.Tags {
			tag = strings.ToLower(strings.TrimPrefix(tag, "#"))
			seen := false
			for _, t := range n.Tags {
				if t == tag {
					seen = true
					break
				}
			}
			if !seen {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if f.Matcher != nil && !f.Matcher.Match(n.Content) && !f.Matcher.MatchString(n.Title) {
			continue
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		switch f.Sort {
		case "modified":
			if !out[i].ModifiedAt.Equal(out[j].ModifiedAt) {
				return out[i].ModifiedAt.Before(out[j].ModifiedAt)
			}
		case "title":
			if out[i].Title != out[j].Title {
				return out[i].Title < out[j].Title
			}
		case "category":
			if out[i].Category != out[j].Category {
				return out[i].Category < out[j].Category
			}
		}
		return out[i].ID < out[j].ID
	})
	if f.Reverse {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, ctx.Err()
}
