param(
    [string]$Output = "esp-hid-bridge.exe"
)

$ErrorActionPreference = "Stop"

# Build as a Windows GUI app so launching the EXE does not open a console window.
go build -trimpath -ldflags "-H=windowsgui -s -w" -o $Output .

Write-Host "Built $Output (windowsgui subsystem)."