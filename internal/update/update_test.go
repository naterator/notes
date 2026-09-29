package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAndVerifiedReplacement(t *testing.T) {
	name := "notes-darwin-arm64"
	payload := []byte("new binary")
	sum := sha256.Sum256(payload)
	bad := false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/naterator/notes/releases/latest", "/repos/naterator/notes/releases/tags/v1.1.0":
			_ = json.NewEncoder(w).Encode(Release{Tag: "v1.1.0", Assets: []Asset{{Name: name, URL: server.URL + "/binary", Digest: "sha256:" + hex.EncodeToString(sum[:])}}})
		case "/binary":
			if bad {
				_, _ = w.Write([]byte("tampered binary"))
			} else {
				_, _ = w.Write(payload)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "notes")
	if err := os.WriteFile(target, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir := t.TempDir()
	t.Setenv("TMPDIR", downloadDir)
	c := Client{HTTP: server.Client(), APIBase: server.URL, Executable: target, OS: "darwin", Arch: "arm64"}
	result, release, err := c.Check(context.Background(), "naterator/notes", "v1.0.0", "")
	if err != nil || !result.Available {
		t.Fatalf("check: %+v %v", result, err)
	}
	unchanged, _, err := c.Check(context.Background(), "naterator/notes", "v1.1.0", "")
	if err != nil || unchanged.Available {
		t.Fatalf("same release should not update: %+v %v", unchanged, err)
	}
	selected, _, err := c.Check(context.Background(), "naterator/notes", "v2.0.0", "v1.1.0")
	if err != nil || !selected.Available {
		t.Fatalf("explicit release was not selected: %+v %v", selected, err)
	}
	if _, _, err = c.Check(context.Background(), "naterator/notes", "v1.0.0", "../bad"); err == nil {
		t.Fatal("invalid requested tag accepted")
	}
	bad = true
	if err = c.Install(context.Background(), release); err == nil {
		t.Fatal("binary with wrong digest accepted")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "original" {
		t.Fatal("replacement occurred before verification")
	}
	bad = false
	if err = c.Install(context.Background(), release); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(target)
	if string(b) != "new binary" {
		t.Fatal("verified binary not installed")
	}
	entries, err := os.ReadDir(downloadDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "notes-update-") {
			t.Fatalf("download staging directory was not removed: %s", entry.Name())
		}
	}
	if os.Geteuid() != 0 {
		lockedDir := filepath.Join(t.TempDir(), "read-only")
		if err := os.Mkdir(lockedDir, 0700); err != nil {
			t.Fatal(err)
		}
		lockedTarget := filepath.Join(lockedDir, "notes")
		if err := os.WriteFile(lockedTarget, []byte("keep original"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(lockedDir, 0500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(lockedDir, 0700)
		locked := c
		locked.Executable = lockedTarget
		if err := locked.Install(context.Background(), release); err == nil {
			t.Fatal("replacement unexpectedly succeeded in read-only directory")
		}
		kept, err := os.ReadFile(lockedTarget)
		if err != nil || string(kept) != "keep original" {
			t.Fatalf("replacement failure damaged original: %q, %v", kept, err)
		}
	}
	release.Assets[0].Digest = ""
	if err = c.Install(context.Background(), release); err == nil {
		t.Fatal("binary without a digest accepted")
	}
	b, _ = os.ReadFile(target)
	if string(b) != "new binary" {
		t.Fatal("missing digest changed installed binary")
	}
}

func TestUnprefixedReleaseTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/naterator/notes/releases/latest" && r.URL.Path != "/repos/naterator/notes/releases/tags/1.0.0" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Release{Tag: "1.0.0"})
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), APIBase: server.URL}
	for _, current := range []string{"1.0.0", "v1.0.0"} {
		result, _, err := client.Check(context.Background(), "naterator/notes", current, "")
		if err != nil || result.Available {
			t.Fatalf("same release %q should not update: %+v %v", current, result, err)
		}
	}
	result, release, err := client.Check(context.Background(), "naterator/notes", "v0.9.0", "1.0.0")
	if err != nil || !result.Available || release.Tag != "1.0.0" {
		t.Fatalf("unprefixed release was not selected: %+v %+v %v", result, release, err)
	}
}

func TestPermissionErrorGivesManualReplacement(t *testing.T) {
	message := replacementError(fs.ErrPermission, "/usr/local/bin/notes", "https://github.com/naterator/notes/releases/tag/v1.0.0")
	if message == nil || !strings.Contains(message.Error(), "replace the binary manually") || !strings.Contains(message.Error(), "/releases/tag/v1.0.0") {
		t.Fatalf("permission hint: %v", message)
	}
}
func TestReleaseErrorsAndUnsupportedTarget(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	c := Client{HTTP: server.Client(), APIBase: server.URL, OS: "darwin", Arch: "arm64"}
	if _, _, err := c.Check(context.Background(), "naterator/notes", "v1.0.0", ""); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("HTTP failure was not reported: %v", err)
	}
	c.OS = "windows"
	if err := c.Install(context.Background(), Release{Tag: "v1.0.0"}); err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("unsupported platform was accepted: %v", err)
	}
	c.OS = "linux"
	if err := c.Install(context.Background(), Release{Tag: "v1.0.0"}); err == nil || !strings.Contains(err.Error(), "lacks") {
		t.Fatalf("missing target asset was accepted: %v", err)
	}
}
func TestBinaryDownloadIsBoundedOnDisk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte{'x'}, 64<<10)
		for i := 0; i <= maxDownload/len(chunk); i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "notes-linux-amd64")
	client := Client{HTTP: server.Client()}
	if err := client.downloadFile(context.Background(), server.URL, path); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unbounded binary download: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxDownload+1 {
		t.Fatalf("download exceeded size bound: %v, %v", info, err)
	}
}
