package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	semver "github.com/blang/semver/v4"
)

const maxDownload = 64 << 20

var releaseTag = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)

type Client struct {
	HTTP       *http.Client
	APIBase    string
	Executable string
	OS, Arch   string
}
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}
type Release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
	HTMLURL    string  `json:"html_url"`
}
type CheckResult struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	URL       string `json:"url"`
}

func (c Client) defaults() Client {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if c.APIBase == "" {
		c.APIBase = "https://api.github.com"
	}
	if c.OS == "" {
		c.OS = runtime.GOOS
	}
	if c.Arch == "" {
		c.Arch = runtime.GOARCH
	}
	return c
}
func (c Client) request(ctx context.Context, url string) (io.ReadCloser, error) {
	c = c.defaults()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "notes-updater")
	res, e := c.HTTP.Do(req)
	if e != nil {
		return nil, e
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("download %s: HTTP %s", url, res.Status)
	}
	return res.Body, nil
}
func (c Client) get(ctx context.Context, url string) ([]byte, error) {
	body, e := c.request(ctx, url)
	if e != nil {
		return nil, e
	}
	defer body.Close()
	b, e := io.ReadAll(io.LimitReader(body, maxDownload+1))
	if e != nil {
		return nil, e
	}
	if len(b) > maxDownload {
		return nil, fmt.Errorf("release download too large")
	}
	return b, nil
}
func (c Client) downloadFile(ctx context.Context, url, path string) error {
	body, err := c.request(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(body, maxDownload+1))
	if err != nil {
		return err
	}
	if n > maxDownload {
		return fmt.Errorf("release download too large")
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}
func (c Client) Release(ctx context.Context, repo, version string) (Release, error) {
	c = c.defaults()
	endpoint := "latest"
	if version != "" {
		if !releaseTag.MatchString(version) {
			return Release{}, fmt.Errorf("release version must be MAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH")
		}
		endpoint = "tags/" + version
	}
	b, e := c.get(ctx, strings.TrimRight(c.APIBase, "/")+"/repos/"+repo+"/releases/"+endpoint)
	if e != nil {
		return Release{}, e
	}
	var r Release
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	if r.Draft || version == "" && r.Prerelease {
		return r, fmt.Errorf("release is draft or prerelease")
	}
	if !releaseTag.MatchString(r.Tag) {
		return r, fmt.Errorf("invalid release tag %q", r.Tag)
	}
	if version != "" && r.Tag != version {
		return r, fmt.Errorf("release endpoint returned %s instead of requested %s", r.Tag, version)
	}
	if _, e = semver.Parse(strings.TrimPrefix(r.Tag, "v")); e != nil {
		return r, fmt.Errorf("invalid release tag %q", r.Tag)
	}
	return r, nil
}
func (c Client) Check(ctx context.Context, repo, current, version string) (CheckResult, Release, error) {
	r, e := c.Release(ctx, repo, version)
	if e != nil {
		return CheckResult{}, r, e
	}
	result := CheckResult{Current: current, Latest: r.Tag, URL: r.HTMLURL}
	latest, _ := semver.Parse(strings.TrimPrefix(r.Tag, "v"))
	have, e := semver.Parse(strings.TrimPrefix(current, "v"))
	result.Available = e != nil || version != "" && r.Tag != current || latest.GT(have)
	return result, r, nil
}
func asset(r Release, name string) (Asset, error) {
	for _, a := range r.Assets {
		if a.Name == name && a.URL != "" {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("release %s lacks %s (%s)", r.Tag, name, r.HTMLURL)
}
func digest(a Asset) (string, error) {
	value, ok := strings.CutPrefix(a.Digest, "sha256:")
	if !ok || len(value) != 64 {
		return "", fmt.Errorf("release asset %s lacks a valid SHA-256 digest", a.Name)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("release asset %s has an invalid SHA-256 digest: %w", a.Name, err)
	}
	return strings.ToLower(value), nil
}
func (c Client) Install(ctx context.Context, r Release) error {
	c = c.defaults()
	if c.OS != "darwin" && c.OS != "linux" || c.Arch != "amd64" && c.Arch != "arm64" {
		return fmt.Errorf("unsupported platform %s/%s", c.OS, c.Arch)
	}
	name := fmt.Sprintf("notes-%s-%s", c.OS, c.Arch)
	binaryAsset, e := asset(r, name)
	if e != nil {
		return e
	}
	expected, e := digest(binaryAsset)
	if e != nil {
		return e
	}
	tempDir, e := os.MkdirTemp("", "notes-update-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tempDir)
	binaryPath := filepath.Join(tempDir, name)
	if e = c.downloadFile(ctx, binaryAsset.URL, binaryPath); e != nil {
		return e
	}
	binary, e := os.ReadFile(binaryPath)
	if e != nil {
		return e
	}
	if len(binary) == 0 {
		return fmt.Errorf("release asset %s is empty", name)
	}
	sum := sha256.Sum256(binary)
	if hex.EncodeToString(sum[:]) != expected {
		return fmt.Errorf("digest mismatch for %s", name)
	}
	target := c.Executable
	if target == "" {
		target, e = os.Executable()
		if e != nil {
			return e
		}
	}
	target, e = filepath.EvalSymlinks(target)
	if e != nil {
		return e
	}
	info, e := os.Stat(target)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("installed executable is not a regular file")
	}
	f, e := os.CreateTemp(filepath.Dir(target), ".notes-update-*")
	if e != nil {
		return replacementError(e, target, r.HTMLURL)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(info.Mode().Perm()); e != nil {
		return e
	}
	if _, e = f.Write(binary); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return replacementError(os.Rename(f.Name(), target), target, r.HTMLURL)
}

func replacementError(err error, target, releaseURL string) error {
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("cannot replace %s: %w; download %s and replace the binary manually", target, err, releaseURL)
	}
	return err
}
