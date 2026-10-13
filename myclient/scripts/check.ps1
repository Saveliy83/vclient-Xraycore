$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$issues = [System.Collections.Generic.List[string]]::new()

$goCommand = Get-Command go -ErrorAction SilentlyContinue
if ($goCommand) {
    $go = $goCommand.Source
} elseif (Test-Path 'C:\Program Files\Go\bin\go.exe') {
    $go = 'C:\Program Files\Go\bin\go.exe'
} else {
    $go = $null
}
if (-not $go) {
    $issues.Add('Go was not found. Install Go from https://go.dev/dl/ and add the Go bin directory to PATH.')
} else {
    $cgo = (& $go env CGO_ENABLED).Trim()
    if ($cgo -ne '1') {
        $issues.Add("CGO_ENABLED is '$cgo'. In this PowerShell session run: `$env:CGO_ENABLED = '1'")
    }
}

$gcc = Get-Command gcc -ErrorAction SilentlyContinue
if ($gcc) {
    $gccVersion = & $gcc.Source --version 2>&1
    if ($LASTEXITCODE -ne 0) {
        $issues.Add("gcc was found at '$($gcc.Source)' but `gcc --version` failed with exit code $LASTEXITCODE.")
    } else {
        Write-Host ($gccVersion | Select-Object -First 1)
    }
} else {
    $issues.Add('gcc was not found. Install MSYS2 from https://www.msys2.org/, then run in the MSYS2 UCRT64 shell: pacman -S --noconfirm mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-binutils. Add C:\msys64\ucrt64\bin to PATH and reopen PowerShell. Alternatively install a WinLibs MinGW-w64 build from https://winlibs.com/ and add its mingw64\bin directory to PATH.')
}

$wintun = Join-Path $root 'assets\wintun.dll'
if (-not (Test-Path -LiteralPath $wintun -PathType Leaf)) {
    $issues.Add('assets\wintun.dll is missing. Download the official AMD64 DLL from https://www.wintun.net/ and save it at that path.')
}

$windres = Get-Command windres -ErrorAction SilentlyContinue
if (-not $windres) {
    $issues.Add('windres was not found. Install it with `pacman -S mingw-w64-ucrt-x86_64-binutils` and add C:\msys64\ucrt64\bin to PATH.')
}

if ($issues.Count -gt 0) {
    Write-Host 'Build prerequisites are incomplete:' -ForegroundColor Yellow
    foreach ($issue in $issues) {
        Write-Host " - $issue"
    }
    Write-Host 'After installing prerequisites, run: $env:CGO_ENABLED = ''1''; .\scripts\check.ps1'
    exit 1
}

Write-Host 'Build prerequisites passed: CGO_ENABLED=1, gcc (gcc --version), windres, and assets\wintun.dll are available.' -ForegroundColor Green
