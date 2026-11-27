package notes_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/naterator/notes"
)

func TestNotebookAPI(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	book := notes.New(filepath.Join(dir, "notebook"), filepath.Join(dir, "state"))
	note, err := book.Create(ctx, notes.NewOptions{
		Title: "Ideas", Path: "work/ideas.md", Tags: []string{"work/ideas"},
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := book.Query(ctx, notes.Query{TagPrefix: "work"})
	if err != nil || len(found) != 1 || found[0].ID != note.ID {
		t.Fatalf("query returned %v, %v", found, err)
	}
	release, err := book.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 35*time.Millisecond)
	body := []byte("# Revised ideas\n")
	lockedSave := book.Save(wait, note.ID, note.Revision(), body)
	cancel()
	release()
	if !errors.Is(lockedSave, context.DeadlineExceeded) {
		t.Fatalf("save should honor cancellation while waiting for a lock: %v", lockedSave)
	}
	if err := book.Save(ctx, note.ID, note.Revision(), body); err != nil {
		t.Fatal(err)
	}
	if err := book.Save(ctx, note.ID, note.Revision(), []byte("stale")); !errors.Is(err, notes.ErrChanged) {
		t.Fatalf("stale Save should return ErrChanged: %v", err)
	}
	saved, err := book.Read(ctx, note.Path)
	if err != nil || string(saved.Content) != string(body) {
		t.Fatalf("readback returned %q, %v", saved.Content, err)
	}
}
