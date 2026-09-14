//go:build windows

package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// Install swaps the downloaded exe in for the running one and starts it. It
// returns only on failure; on success the caller must quit promptly.
//
// Windows will not let a running executable be overwritten or deleted, but
// it will let it be renamed. So the running exe moves aside to ".old", the
// download takes its name, and the next start removes ".old" once this
// process has gone.
func Install(pkg string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("moving the current program aside: %w", err)
	}
	if err := moveFile(pkg, exe); err != nil {
		_ = os.Rename(old, exe)
		return fmt.Errorf("installing the new program: %w", err)
	}
	cmd := exec.Command(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the new program: %w", err)
	}
	return nil
}

// moveFile renames, or copies when the download sits on another volume.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// Cleanup removes the ".old" exe a previous Install left. The old process
// may still be shutting down when the new one starts, so it retries briefly.
func Cleanup() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	old := exe + ".old"
	if _, err := os.Stat(old); err != nil {
		return
	}
	go func() {
		for i := 0; i < 25; i++ {
			if os.Remove(old) == nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()
}
