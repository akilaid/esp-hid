//go:build darwin

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeBundle writes a minimal, ad-hoc signed .app so the swap can be
// exercised for real: ditto, codesign --verify, and both renames.
func makeBundle(t *testing.T, dir, name, marker string) string {
	t.Helper()
	app := filepath.Join(dir, name+".app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.espbridge.test</string>
<key>CFBundleExecutable</key><string>bin</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(macos, "bin")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", app).CombinedOutput(); err != nil {
		t.Skipf("codesign unavailable: %v: %s", err, out)
	}
	return app
}

func TestInstallBundleSwapsAndKeepsPrevious(t *testing.T) {
	root := t.TempDir()
	current := makeBundle(t, filepath.Join(root, "installed"), "ESP HID Bridge", "old")
	fresh := makeBundle(t, filepath.Join(root, "built"), "ESP HID Bridge", "new")

	zip := filepath.Join(root, "ESP-HID-Bridge-v9.9.9-macos.zip")
	if out, err := exec.Command("/usr/bin/ditto", "-c", "-k", "--keepParent", fresh, zip).CombinedOutput(); err != nil {
		t.Fatalf("ditto: %v: %s", err, out)
	}

	if err := installBundle(zip, current); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(current, "Contents", "MacOS", "bin"))
	if err != nil || string(got) != "#!/bin/sh\necho new\n" {
		t.Fatalf("installed executable: %q, %v", got, err)
	}
	prev, err := os.ReadFile(filepath.Join(current+".previous", "Contents", "MacOS", "bin"))
	if err != nil || string(prev) != "#!/bin/sh\necho old\n" {
		t.Fatalf("previous bundle: %q, %v", prev, err)
	}
	if stale, _ := filepath.Glob(filepath.Join(filepath.Dir(current), ".esp-hid-update-*")); len(stale) != 0 {
		t.Errorf("staging left behind: %v", stale)
	}
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", current).CombinedOutput(); err != nil {
		t.Errorf("installed app no longer verifies: %v: %s", err, out)
	}
}

func TestInstallBundleRejectsUnsignedApp(t *testing.T) {
	root := t.TempDir()
	current := makeBundle(t, filepath.Join(root, "installed"), "ESP HID Bridge", "old")
	fresh := makeBundle(t, filepath.Join(root, "built"), "ESP HID Bridge", "new")
	// Break the signature after signing: a byte change in the executable.
	if err := os.WriteFile(filepath.Join(fresh, "Contents", "MacOS", "bin"), []byte("#!/bin/sh\necho tampered\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(root, "bad-macos.zip")
	if out, err := exec.Command("/usr/bin/ditto", "-c", "-k", "--keepParent", fresh, zip).CombinedOutput(); err != nil {
		t.Fatalf("ditto: %v: %s", err, out)
	}
	if err := installBundle(zip, current); err == nil {
		t.Fatal("tampered bundle was installed")
	}
	got, _ := os.ReadFile(filepath.Join(current, "Contents", "MacOS", "bin"))
	if string(got) != "#!/bin/sh\necho old\n" {
		t.Fatalf("current app was disturbed: %q", got)
	}
}

func TestInstallBundleRejectsZipWithoutApp(t *testing.T) {
	root := t.TempDir()
	current := makeBundle(t, filepath.Join(root, "installed"), "ESP HID Bridge", "old")
	loose := filepath.Join(root, "loose")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loose, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(root, "loose-macos.zip")
	if out, err := exec.Command("/usr/bin/ditto", "-c", "-k", loose, zip).CombinedOutput(); err != nil {
		t.Fatalf("ditto: %v: %s", err, out)
	}
	if err := installBundle(zip, current); err == nil {
		t.Fatal("zip without an .app was accepted")
	}
}
