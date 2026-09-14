//go:build darwin

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Install swaps the downloaded zip in for the running .app and arranges a
// relaunch once this process has exited. It returns only on failure; on
// success the caller must quit promptly.
//
// The zip is unpacked with ditto, which keeps the permission bits and the
// code signature, into a staging folder beside the app — same volume, so
// the two renames are atomic and cannot leave a half-copied bundle. The old
// bundle is kept as ".previous" until the next launch cleans it up, so a
// relaunch that fails still leaves something to fall back to.
func Install(pkg string) error {
	app, err := runningBundle()
	if err != nil {
		return err
	}
	if err := installBundle(pkg, app); err != nil {
		return err
	}
	return relaunchAfterExit(app)
}

// installBundle is the swap itself, separated from the running-process
// plumbing so a test can drive it against a bundle in a temp dir.
func installBundle(pkg, app string) error {
	parent := filepath.Dir(app)
	staging, err := os.MkdirTemp(parent, ".esp-hid-update-")
	if err != nil {
		return fmt.Errorf("the app's folder is not writable (%v); move it to Applications and try again", err)
	}
	defer os.RemoveAll(staging)

	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", pkg, staging).CombinedOutput(); err != nil {
		return fmt.Errorf("unpacking %s: %v: %s", filepath.Base(pkg), err, strings.TrimSpace(string(out)))
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	newApp := ""
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
			newApp = filepath.Join(staging, e.Name())
			break
		}
	}
	if newApp == "" {
		return fmt.Errorf("%s does not contain an .app", filepath.Base(pkg))
	}
	// A package that unpacks but does not verify is corrupt or tampered with;
	// either way it must not replace a working app.
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", "--deep", newApp).CombinedOutput(); err != nil {
		return fmt.Errorf("the downloaded app failed signature verification: %s", strings.TrimSpace(string(out)))
	}

	previous := app + ".previous"
	os.RemoveAll(previous)
	if err := os.Rename(app, previous); err != nil {
		return fmt.Errorf("moving the current app aside: %w", err)
	}
	if err := os.Rename(newApp, app); err != nil {
		// Put the old one back rather than leave nothing at the path.
		_ = os.Rename(previous, app)
		return fmt.Errorf("installing the new app: %w", err)
	}
	return nil
}

// runningBundle resolves the .app this process was launched from.
func runningBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	// <App>.app/Contents/MacOS/<exe>
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(app, ".app") {
		return "", errors.New("not running from an .app bundle; update by hand")
	}
	return app, nil
}

// relaunchAfterExit starts a detached shell that waits for this process to
// go away, then opens the app. Opening from a still-running instance would
// only activate it, and the serial port and the event tap must be released
// before the new instance takes them over.
func relaunchAfterExit(app string) error {
	pid := strconv.Itoa(os.Getpid())
	script := "while kill -0 " + pid + " 2>/dev/null; do sleep 0.2; done; " +
		"exec /usr/bin/open " + shellQuote(app)
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("scheduling the relaunch: %w", err)
	}
	// Not waited on: it outlives this process by design.
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Cleanup removes what a previous Install left behind: the ".previous"
// bundle and any staging folder from an attempt that did not finish. Called
// once at startup; a no-op outside a bundle.
func Cleanup() {
	app, err := runningBundle()
	if err != nil {
		return
	}
	os.RemoveAll(app + ".previous")
	stale, _ := filepath.Glob(filepath.Join(filepath.Dir(app), ".esp-hid-update-*"))
	for _, dir := range stale {
		os.RemoveAll(dir)
	}
}
