//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const commonControlsV6Manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity
    version="1.0.0.0"
    processorArchitecture="*"
    name="esp-hid.software"
    type="win32"
  />
  <dependency>
    <dependentAssembly>
      <assemblyIdentity
        type="win32"
        name="Microsoft.Windows.Common-Controls"
        version="6.0.0.0"
        processorArchitecture="*"
        publicKeyToken="6595b64144ccf1df"
        language="*"
      />
    </dependentAssembly>
  </dependency>
</assembly>
`

const invalidHandleValue = ^uintptr(0)

type actCtx struct {
	cbSize                  uint32
	dwFlags                 uint32
	lpSource                *uint16
	wProcessorArchitecture  uint16
	wLangID                 uint16
	lpAssemblyDirectory     *uint16
	lpResourceName          *uint16
	lpApplicationName       *uint16
	hModule                 windows.Handle
}

var (
	kernel32ProcCreateActCtxW   = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateActCtxW")
	kernel32ProcActivateActCtx  = windows.NewLazySystemDLL("kernel32.dll").NewProc("ActivateActCtx")
	kernel32ProcDeactivateActCtx = windows.NewLazySystemDLL("kernel32.dll").NewProc("DeactivateActCtx")
	kernel32ProcReleaseActCtx   = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReleaseActCtx")
)

func activateCommonControlsV6() (func(), error) {
	manifestFile, err := os.CreateTemp("", "esp-hid-comctl-*.manifest")
	if err != nil {
		return nil, fmt.Errorf("create temp manifest: %w", err)
	}
	manifestPath := manifestFile.Name()

	cleanupFile := func() {
		_ = os.Remove(manifestPath)
	}

	if _, err := manifestFile.WriteString(commonControlsV6Manifest); err != nil {
		_ = manifestFile.Close()
		cleanupFile()
		return nil, fmt.Errorf("write temp manifest: %w", err)
	}
	if err := manifestFile.Close(); err != nil {
		cleanupFile()
		return nil, fmt.Errorf("close temp manifest: %w", err)
	}

	source, err := windows.UTF16PtrFromString(manifestPath)
	if err != nil {
		cleanupFile()
		return nil, fmt.Errorf("manifest path utf16: %w", err)
	}

	ctx := actCtx{
		cbSize:   uint32(unsafe.Sizeof(actCtx{})),
		lpSource: source,
	}

	hActCtx, _, createErr := kernel32ProcCreateActCtxW.Call(uintptr(unsafe.Pointer(&ctx)))
	if hActCtx == invalidHandleValue {
		cleanupFile()
		return nil, fmt.Errorf("CreateActCtxW failed: %w", normalizeSyscallError(createErr))
	}

	var cookie uintptr
	activated, _, activateErr := kernel32ProcActivateActCtx.Call(hActCtx, uintptr(unsafe.Pointer(&cookie)))
	if activated == 0 {
		kernel32ProcReleaseActCtx.Call(hActCtx)
		cleanupFile()
		return nil, fmt.Errorf("ActivateActCtx failed: %w", normalizeSyscallError(activateErr))
	}

	cleanup := func() {
		kernel32ProcDeactivateActCtx.Call(0, cookie)
		kernel32ProcReleaseActCtx.Call(hActCtx)
		cleanupFile()
	}

	return cleanup, nil
}

func normalizeSyscallError(err error) error {
	if err == nil {
		return errors.New("unknown error")
	}
	if errors.Is(err, windows.ERROR_SUCCESS) {
		return errors.New("unknown error")
	}
	return err
}
