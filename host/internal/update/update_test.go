package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want version
		ok   bool
	}{
		{"v2.3.0", version{2, 3, 0}, true},
		{"2.3.0", version{2, 3, 0}, true},
		{" v2.10.1 ", version{2, 10, 1}, true},
		{"dev", version{}, false},
		{"0.0.0", version{}, false},
		{"v2.3", version{}, false},
		{"v2.3.0-rc1", version{}, false},
		{"", version{}, false},
	}
	for _, tc := range cases {
		got, ok := parseVersion(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNewer(t *testing.T) {
	v := func(a, b, c int) version { return version{a, b, c} }
	if !newer(v(2, 3, 0), v(2, 2, 9)) || !newer(v(3, 0, 0), v(2, 9, 9)) || !newer(v(2, 2, 1), v(2, 2, 0)) {
		t.Error("higher version not reported as newer")
	}
	if newer(v(2, 2, 0), v(2, 2, 0)) || newer(v(2, 1, 9), v(2, 2, 0)) {
		t.Error("equal or lower version reported as newer")
	}
}

func TestPackageFor(t *testing.T) {
	assets := []Asset{
		{Name: "esp-hid-bridge.exe"},
		{Name: "ESP-HID-Bridge-v2.3.0.dmg"},
		{Name: "ESP-HID-Bridge-v2.3.0-macos.zip"},
		{Name: "firmware-esp32c3.zip"},
		{Name: "SHA256SUMS"},
	}
	if a, ok := packageFor("darwin", assets); !ok || a.Name != "ESP-HID-Bridge-v2.3.0-macos.zip" {
		t.Errorf("darwin picked %v, %v", a, ok)
	}
	if a, ok := packageFor("windows", assets); !ok || a.Name != "esp-hid-bridge.exe" {
		t.Errorf("windows picked %v, %v", a, ok)
	}
	if _, ok := packageFor("linux", assets); ok {
		t.Error("linux has no package but one was picked")
	}
	// The firmware zip must never be mistaken for the macOS package.
	if _, ok := packageFor("darwin", []Asset{{Name: "firmware-esp32c3.zip"}}); ok {
		t.Error("firmware zip picked as the macOS package")
	}
}

func TestParseSums(t *testing.T) {
	text := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  one.zip\n" +
		"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB *two.exe\n" +
		"not a checksum line\n" +
		"short  three\n"
	sums := parseSums(text)
	if sums["one.zip"] != strings.Repeat("a", 64) {
		t.Errorf("one.zip = %q", sums["one.zip"])
	}
	if sums["two.exe"] != strings.Repeat("b", 64) {
		t.Errorf("two.exe (binary marker, upper-case hash) = %q", sums["two.exe"])
	}
	if len(sums) != 2 {
		t.Errorf("parsed %d entries, want 2: %v", len(sums), sums)
	}
}

// fakeRelease serves a releases/latest document plus its assets, so Check
// and Download run end to end without the network. The darwin package is
// the one downloaded; the tests name the platform explicitly so they run
// the same on every CI machine.
type fakeRelease struct {
	srv     *httptest.Server
	tag     string
	payload []byte
	sums    string
	pkgName string
}

func newFakeRelease(t *testing.T, tag string, withSums bool) *fakeRelease {
	t.Helper()
	fr := &fakeRelease{tag: tag, payload: []byte("pretend this is a program")}
	fr.pkgName = "ESP-HID-Bridge-" + tag + "-macos.zip"
	sum := sha256.Sum256(fr.payload)
	fr.sums = hex.EncodeToString(sum[:]) + "  " + fr.pkgName + "\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request had no User-Agent; GitHub rejects those")
		}
		assets := `{"name":"` + fr.pkgName + `","browser_download_url":"` + fr.srv.URL + `/pkg","size":25}` +
			`,{"name":"esp-hid-bridge.exe","browser_download_url":"` + fr.srv.URL + `/pkg","size":25}` +
			`,{"name":"firmware-esp32c3.zip","browser_download_url":"` + fr.srv.URL + `/pkg","size":25}`
		if withSums {
			assets += `,{"name":"SHA256SUMS","browser_download_url":"` + fr.srv.URL + `/sums","size":90}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","html_url":"https://example.invalid/rel",` +
			`"body":"## Added\n\n- A **thing** ([#12](https://x/12))\n\n## What's Changed\n* Fix by @someone in https://x/pull/3\n",` +
			`"assets":[` + assets + `]}`))
	})
	mux.HandleFunc("/pkg", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(fr.payload) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(fr.sums)) })
	fr.srv = httptest.NewServer(mux)
	t.Cleanup(fr.srv.Close)
	return fr
}

func TestCheckFindsNewerRelease(t *testing.T) {
	fr := newFakeRelease(t, "v2.3.0", true)
	rel, err := checkFor(context.Background(), fr.srv.Client(), fr.srv.URL, "v2.2.0", "darwin")
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil || rel.Version != "v2.3.0" || rel.Package.Name != fr.pkgName || rel.Sums == nil {
		t.Fatalf("got %+v", rel)
	}
	if rel.URL != "https://example.invalid/rel" {
		t.Errorf("URL = %q", rel.URL)
	}
	if want := "Added\n\n• A thing (#12)\n\nWhat's Changed\n• Fix"; rel.Notes != want {
		t.Errorf("Notes = %q, want %q", rel.Notes, want)
	}
	win, err := checkFor(context.Background(), fr.srv.Client(), fr.srv.URL, "v2.2.0", "windows")
	if err != nil || win.Package.Name != "esp-hid-bridge.exe" {
		t.Fatalf("windows: %+v, %v", win, err)
	}
}

func TestCheckNewerButNoPackageForPlatform(t *testing.T) {
	fr := newFakeRelease(t, "v2.3.0", true)
	if _, err := checkFor(context.Background(), fr.srv.Client(), fr.srv.URL, "v2.2.0", "linux"); err == nil {
		t.Fatal("a release with no package for the platform was offered")
	}
}

func TestCheckUpToDateAndOlder(t *testing.T) {
	fr := newFakeRelease(t, "v2.3.0", true)
	for _, current := range []string{"v2.3.0", "v2.4.0", "v3.0.0"} {
		rel, err := checkFor(context.Background(), fr.srv.Client(), fr.srv.URL, current, "darwin")
		if err != nil || rel != nil {
			t.Errorf("current %s: got %+v, %v; want nil, nil", current, rel, err)
		}
	}
}

func TestCheckRefusesDevBuild(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", true)
	for _, current := range []string{"dev", "", "0.0.0"} {
		if _, err := Check(context.Background(), fr.srv.Client(), fr.srv.URL, current); !errors.Is(err, ErrDevBuild) {
			t.Errorf("current %q: err = %v, want ErrDevBuild", current, err)
		}
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	fr := newFakeRelease(t, "v2.3.0", true)
	rel, err := checkFor(context.Background(), fr.srv.Client(), fr.srv.URL, "v2.2.0", "darwin")
	if err != nil || rel == nil {
		t.Fatalf("check: %+v, %v", rel, err)
	}
	dir := t.TempDir()
	var calls int
	path, err := Download(context.Background(), fr.srv.Client(), rel, dir, func(done, total int64) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(fr.payload) || filepath.Base(path) != fr.pkgName || calls == 0 {
		t.Fatalf("path %s, %d bytes, %d progress calls", path, len(got), calls)
	}

	// Tamper with the payload after the sums were computed: the download
	// must be rejected and removed.
	fr.payload = []byte("something else entirely!!")
	if _, err := Download(context.Background(), fr.srv.Client(), rel, dir, nil); err == nil {
		t.Fatal("tampered package accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("rejected download left on disk: %v", err)
	}
}

func TestDownloadWithoutSumsStillWorks(t *testing.T) {
	fr := newFakeRelease(t, "v2.3.0", false)
	rel, err := checkFor(context.Background(), fr.srv.Client(), fr.srv.URL, "v2.2.0", "darwin")
	if err != nil || rel == nil || rel.Sums != nil {
		t.Fatalf("check: %+v, %v", rel, err)
	}
	if _, err := Download(context.Background(), fr.srv.Client(), rel, t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestPlainNotes(t *testing.T) {
	in := "## [2.4.0] — 2026-09-15\r\n\r\n### Changed\r\n\r\n- The dropdown is gone. See [the docs](https://x/y).\r\n  Continued line.\r\n- `code` and **bold**\r\n\r\n\r\n\r\n## What's Changed\r\n* Pick the device by @akilaid in https://github.com/akilaid/esp-hid/pull/15\r\n\r\n**Full Changelog**: https://github.com/akilaid/esp-hid/compare/v2.3.0...v2.4.0\r\n"
	want := "[2.4.0] — 2026-09-15\n\nChanged\n\n• The dropdown is gone. See the docs.\n  Continued line.\n• code and bold\n\nWhat's Changed\n• Pick the device\n\nFull Changelog: https://github.com/akilaid/esp-hid/compare/v2.3.0...v2.4.0"
	if got := PlainNotes(in); got != want {
		t.Errorf("PlainNotes:\n got %q\nwant %q", got, want)
	}
	if got := PlainNotes(""); got != "" {
		t.Errorf("empty body gave %q", got)
	}
}
