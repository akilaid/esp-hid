param(
    [string]$Output = "esp-hid-bridge.exe"
)

$ErrorActionPreference = "Stop"

$goArch = if ($env:GOARCH) { $env:GOARCH } else { (go env GOARCH).Trim() }
if ([string]::IsNullOrWhiteSpace($goArch)) {
    throw "Unable to detect GOARCH for resource generation."
}

$goPath = (go env GOPATH).Trim()
$rsrcExe = Join-Path $goPath "bin\rsrc.exe"

if (-not (Test-Path $rsrcExe)) {
    go install github.com/akavel/rsrc@v0.10.2
}

$sysoName = "rsrc_windows_$goArch.syso"

# Embed both app and remote-mode icons directly into the EXE resources.
& $rsrcExe -arch $goArch -ico "app.ico,on.ico" -o $sysoName

# Build as a Windows GUI app so launching the EXE does not open a console window.
go build -trimpath -ldflags "-H=windowsgui -s -w" -o $Output .

Write-Host "Built $Output (windowsgui subsystem, embedded icons)."