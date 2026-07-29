param(
    [string]$Output = "esp-hid-bridge.exe",
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"

$goArch = $env:GOARCH
if (-not $goArch) {
    $goArch = (go env GOARCH).Trim()
}
if (-not $goArch) {
    throw "Unable to detect GOARCH for resource generation."
}

$rsrcExe = Join-Path (go env GOPATH).Trim() "bin/rsrc.exe"
if (-not (Test-Path $rsrcExe)) {
    go install github.com/akavel/rsrc@v0.10.2
}

# Icons become resources 1 (app) and 2 (remote-mode) — the IDs the GUI loads.
& $rsrcExe -arch $goArch -ico "app.ico,on.ico" -o "cmd/bridge/rsrc_windows_$goArch.syso"

go build -trimpath -ldflags "-H=windowsgui -s -w -X main.version=$Version" -o $Output ./cmd/bridge

Write-Host "Built $Output ($Version, windowsgui subsystem, embedded icons)."
