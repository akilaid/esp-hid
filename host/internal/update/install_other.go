//go:build !darwin && !windows

package update

import "errors"

// Install is unsupported here: there is no GUI build for other systems.
func Install(pkg string) error {
	return errors.New("updates are installed on Windows and macOS only")
}

// Cleanup has nothing to do without an installer.
func Cleanup() {}
