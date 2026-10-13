param(
    [ValidateSet('build', 'gui', 'test', 'lint', 'fuzz', 'release')]
    [string]$Target = 'test',
    [string]$Version = '',
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$goCommand = Get-Command go -ErrorAction SilentlyContinue
if ($goCommand) {
    $go = $goCommand.Source
} else {
    $go = 'C:\Program Files\Go\bin\go.exe'
}
if (-not (Test-Path $go)) {
    throw 'Go executable was not found. Install Go and add it to PATH.'
}

if (-not $Version) {
    if ($env:VCLIENT_VERSION) {
        $Version = $env:VCLIENT_VERSION
    } else {
        $git = Get-Command git -ErrorAction SilentlyContinue
        if ($git) {
            $Version = (& $git.Source describe --tags --always --dirty 2>$null)
            if ($LASTEXITCODE -ne 0) { $Version = '' }
        }
        if (-not $Version) { $Version = 'dev' }
    }
}

function Build-GuiRelease {
    param([string]$ReleaseVersion, [bool]$Overwrite)

    $dist = Join-Path $PSScriptRoot 'dist'
    $wintunSource = Join-Path $PSScriptRoot 'assets\wintun.dll'
    $wintunDestination = Join-Path $dist 'wintun.dll'
    $outputBinary = Join-Path $dist 'vclient-gui.exe'
    if ((Test-Path -LiteralPath $outputBinary -PathType Leaf) -and -not $Overwrite) {
        throw "Refusing to overwrite $outputBinary. Re-run with -Force to replace the existing GUI binary."
    }
    if (-not (Test-Path -LiteralPath $wintunSource -PathType Leaf)) {
        throw 'assets\wintun.dll is required for TUN mode. Download the official Wintun AMD64 DLL from https://www.wintun.net/ and place it at assets\wintun.dll.'
    }
    $cgo = (& $go env CGO_ENABLED).Trim()
    if ($cgo -ne '1') {
        throw "GUI release requires CGO_ENABLED=1; run `$env:CGO_ENABLED = '1' in this PowerShell session."
    }
    $gcc = Get-Command gcc -ErrorAction SilentlyContinue
    if (-not $gcc) {
        throw 'gcc was not found. Install MSYS2, run `pacman -S --noconfirm mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-binutils`, and add C:\msys64\ucrt64\bin to PATH.'
    }
    $gccOutput = & $gcc.Source --version 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "gcc --version failed: $($gccOutput | Out-String)"
    }
    $resourceCompiler = Get-Command windres -ErrorAction SilentlyContinue
    if (-not $resourceCompiler) {
        throw 'windres was not found. Install MSYS2, run `pacman -S --noconfirm mingw-w64-ucrt-x86_64-binutils`, and add C:\msys64\ucrt64\bin to PATH.'
    }
    $windresOutput = & $resourceCompiler.Source --version 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "windres --version failed: $($windresOutput | Out-String)"
    }
    $resourceObject = Join-Path $PSScriptRoot 'cmd\vclient-gui\rsrc_windows_amd64.syso'
    if (Test-Path -LiteralPath $resourceObject) {
        throw "Refusing to overwrite existing resource object: $resourceObject"
    }
    New-Item -ItemType Directory -Force -Path $dist | Out-Null
    $temporaryBinary = Join-Path ([IO.Path]::GetTempPath()) "vclient-gui-$PID.exe"
    try {
        & $resourceCompiler.Source -i cmd\vclient-gui\app.rc -o cmd\vclient-gui\rsrc_windows_amd64.syso
        if ($LASTEXITCODE -ne 0) { throw "windres failed with exit code $LASTEXITCODE." }
        $ldflags = "-H windowsgui -s -w -X main.version=$ReleaseVersion"
        & $go build -trimpath -ldflags $ldflags -o $temporaryBinary ./cmd/vclient-gui
        if ($LASTEXITCODE -ne 0) { throw "GUI build failed with exit code $LASTEXITCODE." }
        Copy-Item -LiteralPath $temporaryBinary -Destination $outputBinary -Force
        Copy-Item -LiteralPath $wintunSource -Destination $wintunDestination -Force
        if (-not (Test-Path -LiteralPath $outputBinary -PathType Leaf)) {
            throw "GUI output was not created: $outputBinary"
        }
        if (-not (Test-Path -LiteralPath $wintunDestination -PathType Leaf)) {
            throw "Wintun DLL was not copied: $wintunDestination"
        }
        $hash = (Get-FileHash -LiteralPath $outputBinary -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  vclient-gui.exe" | Set-Content -LiteralPath "$outputBinary.sha256" -Encoding ascii
        Write-Output "Built $outputBinary ($ReleaseVersion), SHA256 $hash; copied $wintunDestination"
    } finally {
        if (Test-Path -LiteralPath $temporaryBinary) {
            Remove-Item -LiteralPath $temporaryBinary -Force
        }
        if (Test-Path -LiteralPath $resourceObject) {
            Remove-Item -LiteralPath $resourceObject -Force
        }
    }
}

switch ($Target) {
    'build' {
        & $go build -o vclient.exe ./cmd/vclient
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    'gui' {
        Build-GuiRelease -ReleaseVersion $Version -Overwrite $Force.IsPresent
    }
    'test' {
        & $go test -tags ci ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    'lint' {
        $lint = Get-Command golangci-lint -ErrorAction SilentlyContinue
        if (-not $lint) { throw 'golangci-lint is required for the lint target.' }
        & $lint.Source run --build-tags ci
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    'fuzz' {
        & $go test ./internal/config/ -run=^$ -fuzz=FuzzParseVLESSLink -fuzztime=30s
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    'release' {
        Build-GuiRelease -ReleaseVersion $Version -Overwrite $Force.IsPresent
    }
}
