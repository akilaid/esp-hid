# Changelog

All notable changes to this project, newest first. Versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html).

The project has two generations. **2.x** is the current one: ESP-IDF firmware
(`firmware-idf/`) with a Go host app (`host/`). **1.x** was the Arduino sketch
(`firmware/`) with a Windows-only sender (`software/`); it speaks a different
wire protocol, is not interchangeable with 2.x, and is no longer released.

## [2.4.4] — 2026-09-15

### Fixed

- **Windows: the device search showed no matches while typing.** The list
  was opened and then closed again by a layout pass the toolkit runs once
  the keystroke has been handled; the list now opens after that pass.

## [2.4.3] — 2026-09-15

### Fixed

- **Windows: the app did not start**, and showed nothing — from 2.2.0 for
  anyone whose device resolution was one of the presets, and from 2.4.0 for
  everyone. The toolkit fires a control's change handler while it is still
  building the window, before the controls that handler reaches for exist;
  the handlers now stay inert until the window is complete. A panic at
  startup is no longer silent either: it is written to `bridge.log` and
  shown in a message box.

## [2.4.2] — 2026-09-15

### Changed

- **Check for Updates…** moved out of the Connection & Status buttons, which
  it had left in a lopsided two-row grid, into a footer at the bottom of the
  window beside the running version — which is now shown. The status
  buttons are back to one balanced row.

## [2.4.1] — 2026-09-15

### Fixed

- The 2.4.0 release has no macOS build: the disk-image step failed to
  detach the freshly styled volume on the CI runner ("Resource busy").
  The script now stops Spotlight indexing the volume, closes the Finder
  window it scripted, and retries the detach before forcing it. No change
  to the app itself.

## [2.4.0] — 2026-09-15

### Changed

- The "This Mac sits / This PC sits" dropdown is gone. In its place the
  Device Layout box shows your displays as the OS has them arranged — every
  monitor in place, named, the primary marked — with the device beside them.
  Drag the device to a side of the desktop to choose it. What is saved is
  unchanged (`hostSide`), and so is the switching behaviour.
- Monitors and the device are drawn to scale: monitors at the physical size
  they report, the device from its resolution and pixel density. The built-in
  device table now carries density, and a typed-in resolution borrows the
  density of known devices of that size.
- Windows has the same picture, compile-checked only.

### Added

- A **Check for Updates…** button in the window, next to Start and Stop.
- The update check now *asks*: a prompt with that release's changelog and
  **Install and Relaunch** / **Later**. Later keeps the offer in the window
  and the daily check stays quiet about that version until a newer one
  appears. Releases carry their CHANGELOG section as the release body, which
  is what the prompt shows.

## [2.3.0] — 2026-09-14

### Added

- The app checks GitHub Releases for a newer version at launch and daily,
  and offers **Install and relaunch** in the window. The download is verified
  against the release's `SHA256SUMS`, swapped in for the running program, and
  the app relaunches — no browser download, no quarantine step. **Check for
  Updates…** and **Check for Updates Automatically** in the app menu (Help
  menu on Windows); `-check-updates=false` turns the scheduled check off.
- macOS release builds are signed with one stable certificate, so the
  Accessibility and Input Monitoring grants carry over between versions
  instead of being lost on every update. `packaging/macos/make-signing-cert.sh`
  sets it up; the workflow imports it from a secret and falls back to ad-hoc
  with a warning when the secret is missing.
- Releases carry `ESP-HID-Bridge-<version>-macos.zip` (the updater's
  package) and `SHA256SUMS`.

### Changed

- On macOS the permission banner's strip collapses, and the window shrinks
  with it, once both permissions are granted; it comes back if one is lost.

## [2.2.0] — 2026-09-14

### Added

- **Device** search in both GUIs: type part of a phone or tablet name and the
  best match fills in the resolution; the dropdown lists the other matches,
  each with its size. Backed by a built-in table of about 19,000 phones and
  tablets plus current iPhones and iPads, embedded in the binary.
- **Orientation** picker (Portrait / Landscape) that flips the resolution for
  a device held sideways. It is read off the resolution rather than stored, so
  nothing changes in `settings-v2.json`.
- A hint under the resolution field: the table gives the panel size, and a
  phone that renders lower (FHD+ on a QHD+ Samsung, say) needs that value
  instead. The field remains editable for exactly this.

## [2.1.2] — 2026-09-14

### Fixed

- Leaving remote mode put the host cursor back on the correct edge but always
  at its centre. It now reappears level with the row or column where the
  pointer crossed over, on the same monitor as before.

## [2.1.1] — 2026-09-10

### Fixed

- Firmware reported a hardcoded `1.0.0` in HELLO; it now reports the release
  version it was built from.
- `app_main` had too little stack to survive a BLE report-map change.

## [2.1.0] — 2026-09-09

### Added

- The host identifies its own board by USB serial number and remembers it, so
  several ESP32s can share a machine without the app driving the wrong one. The
  serial is learned by handshake on first run and stored in `settings-v2.json`;
  it is never typed in. ([#10](https://github.com/akilaid/esp-hid/issues/10))
- **Forget device** button in both GUIs, to re-identify after swapping boards.
- `-device-serial` flag, for pinning the board without the GUI.
- Status line now names the bound board, and distinguishes "bridge not
  connected" from "no device attached".
- A `VERSION` file the release workflow honours, so minor and major bumps can
  be expressed in-repo. Without it the pipeline could only increment the patch.

### Changed

- Documentation no longer describes the host as tied to one ESP32 variant. The
  app is chip-agnostic and works with any Espressif board that exposes a native
  USB Serial/JTAG port; prebuilt firmware images remain compiled for the C3.
- `-port` is documented as a debugging escape hatch. Port names are derived
  from USB topology and change when a board moves socket, so they are not a
  stable way to select hardware.

### Fixed

- The app no longer picks an arbitrary board when several are attached. It
  previously took the first USB `303A:1001` match in enumeration order, which
  is neither sorted nor stable, and the mistake was silent: the wrong board
  opened, answered, and reported a healthy status.
- A board whose USB CDC endpoint is wedged no longer hangs startup. Such a port
  can block `open()` indefinitely at the kernel level, which stalled the
  reconnect loop; it is now skipped after a timeout.
- Running `-cli` or `-gui=false` no longer persists `guiMode: false`, which
  made later launches headless. Only the learned device binding is written.

## [2.0.10] — 2026-08-03

- Edge switching returns to macOS, armed by push pressure so the Dock, menu bar
  and window controls on the same borders do not trigger it.

## [2.0.9] — 2026-08-03

- Updated menu bar artwork.

## [2.0.8] — 2026-08-03

- The local pointer is hidden and pinned without the app needing the
  foreground, so a minimized window no longer moves the local cursor.
- Menu bar uses the drawn on/off art, in colour.

## [2.0.7] — 2026-08-03

- Order a window front so the foreground grab works when minimized.

## [2.0.6] — 2026-08-03

- Hold the foreground on macOS so the local cursor stays put.
- Give the focus shim a real GOOS suffix so Linux CI skips it.

## [2.0.5] — 2026-08-01

- Releases are cut by merging to main; the legacy v1 pipeline is deleted. It
  built the v1 app from the same tag namespace, so running it would have
  published a v1 binary under a 2.x tag.
- Auto switching dropped from the macOS GUI (restored in 2.0.10).
- Hotkeys extended to F13–F20.

## [2.0.4] — 2026-08-01

- Fixed macOS edge switching bouncing straight back out.

## [2.0.3] — 2026-08-01

- macOS ships as a drag-to-install disk image.
- Corrected the macOS Gatekeeper instructions.

## [2.0.2] — 2026-08-01

- Native macOS support in the 2.x host app. The retired 1.x macOS app spoke the
  old newline-text protocol and could not talk to the ESP-IDF firmware.

## [2.0.1] — 2026-07-29

- Fixed a silent GUI startup failure by embedding the Common Controls 6
  manifest.

## [2.0.0] — 2026-07-29

- ESP-IDF firmware rewrite and a new Go host app, on a binary framed wire
  protocol with device→host reporting (BLE state, firmware version, errors).
- Fixed macOS serial port auto-detection.

## [1.0.6] — 2026-06-10

- BLE HID ported to NimBLE, adding ESP32-C3/S3 support.
- macOS ESP HID Bridge implementation.
- Restored 460800 baud and a fast BLE connection interval.
- Demo GIF added to the README.

## [1.0.5] — 2026-03-18

- Modifier hotkeys, and auto/manual switching.

## [1.0.4] — 2026-03-16

- Settings persisted; default serial baud raised.
- Connection LED indicator for BLE status.
- Host-side dropdown replaced with a drag layout widget.
- Edge-aware host return and an optional left-swipe.
- Arduino CLI build and flash instructions.

## [1.0.3] — 2026-03-16

- System cursor visibility managed in remote mode.
- Monitor detection and leftward return.
- Serial baud, BLE parameters and input shaping tuned.

## [1.0.2] — 2026-03-15

- Release workflow fix.

## [1.0.1] — 2026-03-15

- Windows icons embedded and loaded from resources.

## [1.0.0] — 2026-03-15

- First release: ESP32 BLE HID firmware and a Windows sender, with a GUI, tray
  icon, configurable toggle hotkey, remote-mode mouse and button handling, and
  a build/release workflow.

[2.4.4]: https://github.com/akilaid/esp-hid/compare/v2.4.3...v2.4.4
[2.4.3]: https://github.com/akilaid/esp-hid/compare/v2.4.2...v2.4.3
[2.4.2]: https://github.com/akilaid/esp-hid/compare/v2.4.1...v2.4.2
[2.4.1]: https://github.com/akilaid/esp-hid/compare/v2.4.0...v2.4.1
[2.4.0]: https://github.com/akilaid/esp-hid/compare/v2.3.0...v2.4.0
[2.3.0]: https://github.com/akilaid/esp-hid/compare/v2.2.0...v2.3.0
[2.2.0]: https://github.com/akilaid/esp-hid/compare/v2.1.2...v2.2.0
[2.1.2]: https://github.com/akilaid/esp-hid/compare/v2.1.1...v2.1.2
[2.1.1]: https://github.com/akilaid/esp-hid/compare/v2.1.0...v2.1.1
[2.1.0]: https://github.com/akilaid/esp-hid/compare/v2.0.10...v2.1.0
[2.0.10]: https://github.com/akilaid/esp-hid/compare/v2.0.9...v2.0.10
[2.0.9]: https://github.com/akilaid/esp-hid/compare/v2.0.8...v2.0.9
[2.0.8]: https://github.com/akilaid/esp-hid/compare/v2.0.7...v2.0.8
[2.0.7]: https://github.com/akilaid/esp-hid/compare/v2.0.6...v2.0.7
[2.0.6]: https://github.com/akilaid/esp-hid/compare/v2.0.5...v2.0.6
[2.0.5]: https://github.com/akilaid/esp-hid/compare/v2.0.4...v2.0.5
[2.0.4]: https://github.com/akilaid/esp-hid/compare/v2.0.3...v2.0.4
[2.0.3]: https://github.com/akilaid/esp-hid/compare/v2.0.2...v2.0.3
[2.0.2]: https://github.com/akilaid/esp-hid/compare/v2.0.1...v2.0.2
[2.0.1]: https://github.com/akilaid/esp-hid/compare/v2.0.0...v2.0.1
[2.0.0]: https://github.com/akilaid/esp-hid/compare/v1.0.6...v2.0.0
[1.0.6]: https://github.com/akilaid/esp-hid/compare/v1.0.5...v1.0.6
[1.0.5]: https://github.com/akilaid/esp-hid/compare/v1.0.4...v1.0.5
[1.0.4]: https://github.com/akilaid/esp-hid/compare/v1.0.3...v1.0.4
[1.0.3]: https://github.com/akilaid/esp-hid/compare/v1.0.2...v1.0.3
[1.0.2]: https://github.com/akilaid/esp-hid/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/akilaid/esp-hid/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/akilaid/esp-hid/releases/tag/v1.0.0
