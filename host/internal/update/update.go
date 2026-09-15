// Package update keeps the host app current from the project's GitHub
// releases: find the latest release, decide whether it is newer than the
// running build, fetch this platform's package, check it against the
// release's SHA256SUMS, and swap it in for the running program.
//
// It never acts on its own. The GUI asks it to check, shows what it found,
// and calls Install only when the user clicks — an update replaces the
// program the user is running, so it is theirs to start.
//
// The check and download are portable; the swap is the platform file
// (install_darwin.go, install_windows.go). Development builds, whose version
// is not a release tag, are never offered an update: there is nothing to
// compare against, and overwriting a build made five minutes ago with last
// week's release would be a surprise.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases come from.
const Repo = "akilaid/esp-hid"

// APIBase is the GitHub REST endpoint; tests point it at a local server.
const APIBase = "https://api.github.com"

// SumsAsset is the checksum file the release job attaches, in sha256sum
// format: one "hash  filename" line per asset.
const SumsAsset = "SHA256SUMS"

// ErrDevBuild is returned by Check when the running version is not a
// release tag, i.e. a local build.
var ErrDevBuild = errors.New("not a release build; updates are not tracked")

// Timeout bounds every request. A stalled check must never hold the GUI's
// goroutine hostage, and the packages are a few megabytes at most.
const Timeout = 60 * time.Second

// Asset is one downloadable file on a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is a newer release than the running build, with this platform's
// package picked out.
type Release struct {
	Version string // the tag, e.g. "v2.3.0"
	URL     string // the release page
	Notes   string // the release body, plain text (see Notes)
	Package Asset  // this platform's installable
	Sums    *Asset // SHA256SUMS, nil when the release has none
}

type release struct {
	TagName    string  `json:"tag_name"`
	HTMLURL    string  `json:"html_url"`
	Body       string  `json:"body"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Check fetches the latest release and returns it if it is newer than
// current, or nil when current is up to date. current is the build's version
// string ("v2.3.0"; "dev" and anything else unparsable yields ErrDevBuild).
func Check(ctx context.Context, client *http.Client, apiBase, current string) (*Release, error) {
	return checkFor(ctx, client, apiBase, current, runtime.GOOS)
}

// checkFor is Check with the platform spelled out, so the logic is tested on
// every CI platform and not only where a package exists.
func checkFor(ctx context.Context, client *http.Client, apiBase, current, goos string) (*Release, error) {
	have, ok := parseVersion(current)
	if !ok {
		return nil, ErrDevBuild
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		apiBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "esp-hid-bridge/"+current)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release lookup: %s", resp.Status)
	}
	var latest release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&latest); err != nil {
		return nil, fmt.Errorf("release lookup: %w", err)
	}
	want, ok := parseVersion(latest.TagName)
	if !ok {
		return nil, fmt.Errorf("release lookup: unexpected tag %q", latest.TagName)
	}
	if !newer(want, have) {
		return nil, nil
	}
	pkg, ok := packageFor(goos, latest.Assets)
	if !ok {
		return nil, fmt.Errorf("%s has no package for %s", latest.TagName, goos)
	}
	rel := &Release{Version: latest.TagName, URL: latest.HTMLURL, Notes: PlainNotes(latest.Body), Package: pkg}
	for i := range latest.Assets {
		if latest.Assets[i].Name == SumsAsset {
			rel.Sums = &latest.Assets[i]
		}
	}
	return rel, nil
}

// packageFor picks the asset the installer for goos knows how to apply.
func packageFor(goos string, assets []Asset) (Asset, bool) {
	for _, a := range assets {
		switch goos {
		case "darwin":
			if strings.HasSuffix(a.Name, "-macos.zip") {
				return a, true
			}
		case "windows":
			if a.Name == "esp-hid-bridge.exe" {
				return a, true
			}
		}
	}
	return Asset{}, false
}

// Download fetches rel.Package into dir and returns its path. When the
// release carries SHA256SUMS the file is checked against it and a mismatch
// is an error; the partial file is removed on any failure. progress, if not
// nil, is called as bytes arrive.
func Download(ctx context.Context, client *http.Client, rel *Release, dir string,
	progress func(done, total int64)) (string, error) {
	var want string
	if rel.Sums != nil {
		text, err := fetch(ctx, client, rel.Sums.URL, 1<<16)
		if err != nil {
			return "", fmt.Errorf("checksums: %w", err)
		}
		sum, ok := parseSums(string(text))[rel.Package.Name]
		if !ok {
			return "", fmt.Errorf("checksums: no entry for %s", rel.Package.Name)
		}
		want = sum
	}

	path := filepath.Join(dir, rel.Package.Name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.Package.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: %s", resp.Status)
	}

	hash := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return "", err
			}
			hash.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if want != "" {
		if got := hex.EncodeToString(hash.Sum(nil)); got != want {
			return "", fmt.Errorf("%s does not match the release's SHA256SUMS", rel.Package.Name)
		}
	}
	ok = true
	return path, nil
}

// PlainNotes turns a release body — the CHANGELOG section the release job
// attaches, followed by GitHub's generated notes — into text a dialog can
// show: headings lose their hashes, bullets become bullets, links keep their
// text, emphasis markers go, and the "by @user in <pull URL>" tails of the
// generated list are dropped. It is deliberately a light touch, not a
// Markdown renderer.
func PlainNotes(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, " ")
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case strings.HasPrefix(trimmed, "#"):
			line = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			indent := line[:len(line)-len(trimmed)]
			line = indent + "• " + trimmed[2:]
		}
		line = mdLink.ReplaceAllString(line, "$1")
		line = mdByLine.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, "**", "")
		line = strings.ReplaceAll(line, "`", "")
		out = append(out, line)
	}
	// Collapse runs of blank lines and trim the ends.
	var kept []string
	blank := true
	for _, line := range out {
		if strings.TrimSpace(line) == "" {
			if !blank {
				kept = append(kept, "")
			}
			blank = true
			continue
		}
		kept = append(kept, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

var (
	mdLink   = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdByLine = regexp.MustCompile(` by @\S+ in \S+$`)
)

func fetch(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// parseSums reads sha256sum output: "<hex>  <name>" per line. A leading '*'
// on the name (binary mode) is dropped.
func parseSums(text string) map[string]string {
	sums := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || len(fields[0]) != sha256.Size*2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		sums[name] = strings.ToLower(fields[0])
	}
	return sums
}

type version struct{ major, minor, patch int }

// parseVersion reads "v2.3.0" or "2.3.0". Anything else — "dev", "0.0.0"
// from an unparsable build string, a tag with a suffix — is not a release.
func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		out[i] = n
	}
	v := version{out[0], out[1], out[2]}
	if v == (version{}) {
		return version{}, false
	}
	return v, true
}

func newer(a, b version) bool {
	if a.major != b.major {
		return a.major > b.major
	}
	if a.minor != b.minor {
		return a.minor > b.minor
	}
	return a.patch > b.patch
}
