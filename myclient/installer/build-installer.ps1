[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$WintunPublisher = 'WireGuard LLC',
    [string]$SignCert,
    [switch]$SkipGuiBuild,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root 'dist'
$payload = Join-Path $PSScriptRoot 'payload'
$setupPath = Join-Path $dist "vclient-setup-$Version.exe"
$hashPath = Join-Path $dist "vclient-setup-$Version.sha256"
$guiSource = Join-Path $dist 'vclient-gui.exe'
$wintunDist = Join-Path $dist 'wintun.dll'
$wintunAsset = Join-Path $root 'assets\wintun.dll'
$guiPayload = Join-Path $payload 'vclient-gui.exe'
$wintunPayload = Join-Path $payload 'wintun.dll'
$iss = Join-Path $PSScriptRoot 'vclient.iss'
$guiBuiltThisRun = $false

if ($Version -notmatch '^\d+\.\d+\.\d+(\.\d+)?$') {
    throw "Version '$Version' must be a numeric version with three or four components, for example 0.1.0."
}
if ([string]::IsNullOrWhiteSpace($WintunPublisher)) {
    throw 'WintunPublisher must not be empty.'
}
if ((Test-Path -LiteralPath $setupPath -PathType Leaf) -and -not $Force) {
    throw "Refusing to overwrite $setupPath. Re-run with -Force to replace this setup file."
}
if ((Test-Path -LiteralPath $hashPath -PathType Leaf) -and -not $Force) {
    throw "Refusing to overwrite $hashPath. Re-run with -Force to replace this checksum file."
}

. (Join-Path $PSScriptRoot 'verify.ps1')

if (-not $SkipGuiBuild) {
    $buildScript = Join-Path $root 'build.ps1'
    $buildArguments = @{
        Target = 'gui'
        Version = $Version
    }
    if ($Force) {
        $buildArguments.Force = $true
    }
    & $buildScript @buildArguments
    if ($LASTEXITCODE -ne 0) {
        throw "GUI build failed with exit code $LASTEXITCODE."
    }
    $guiBuiltThisRun = $true
}

if (-not (Test-Path -LiteralPath $guiSource -PathType Leaf)) {
    throw "GUI executable was not found: $guiSource"
}

$wintunSource = $null
if (Test-Path -LiteralPath $wintunDist -PathType Leaf) {
    $wintunSource = $wintunDist
} elseif (Test-Path -LiteralPath $wintunAsset -PathType Leaf) {
    $wintunSource = $wintunAsset
}
if (-not $wintunSource) {
    throw "Wintun DLL was not found. Place the official DLL at '$wintunAsset' or build the GUI first."
}

$null = Test-WintunSignature -Path $wintunSource -Publisher $WintunPublisher
New-Item -ItemType Directory -Force -Path $payload | Out-Null
Copy-Item -LiteralPath $guiSource -Destination $guiPayload -Force
Copy-Item -LiteralPath $wintunSource -Destination $wintunPayload -Force
$null = Test-WintunSignature -Path $wintunPayload -Publisher $WintunPublisher

$mt = Get-Command mt.exe -ErrorAction SilentlyContinue
if ($mt) {
    $manifestPath = Join-Path ([IO.Path]::GetTempPath()) "vclient-$PID.manifest"
    try {
        & $mt.Source -nologo "-inputresource:$guiPayload;#1" "-out:$manifestPath"
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
            throw 'mt.exe could not extract the GUI executable manifest.'
        }
        [xml]$manifest = Get-Content -LiteralPath $manifestPath -Raw
        $level = $manifest.SelectSingleNode("//*[local-name()='requestedExecutionLevel']")
        if ($null -eq $level -or $level.GetAttribute('level') -ne 'requireAdministrator') {
            throw 'GUI executable manifest does not request requireAdministrator.'
        }
    } finally {
        if (Test-Path -LiteralPath $manifestPath) {
            Remove-Item -LiteralPath $manifestPath -Force
        }
    }
} elseif ($guiBuiltThisRun) {
    $resourceScript = Join-Path $root 'cmd\vclient-gui\app.rc'
    $manifestSource = Join-Path $root 'cmd\vclient-gui\app.manifest'
    $appResource = Get-Content -LiteralPath $resourceScript -Raw
    [xml]$manifest = Get-Content -LiteralPath $manifestSource -Raw
    $level = $manifest.SelectSingleNode("//*[local-name()='requestedExecutionLevel']")
    if ($appResource -notmatch 'app\.manifest' -or
        $null -eq $level -or $level.GetAttribute('level') -ne 'requireAdministrator') {
        throw 'The GUI build resource does not include a requireAdministrator manifest.'
    }
    Write-Warning 'mt.exe is unavailable; requireAdministrator is verified from the manifest resource configured by build.ps1.'
} else {
    throw 'mt.exe is required to verify requireAdministrator when -SkipGuiBuild is used. Install the Windows SDK or run without -SkipGuiBuild.'
}

$iscc = Get-Command ISCC.exe -ErrorAction SilentlyContinue
if (-not $iscc) {
    $defaultIscc = 'C:\Program Files (x86)\Inno Setup 6\ISCC.exe'
    if (Test-Path -LiteralPath $defaultIscc -PathType Leaf) {
        $isccPath = $defaultIscc
    } else {
        throw 'Inno Setup 6 compiler ISCC.exe was not found. Install it with: winget install JRSoftware.InnoSetup'
    }
} else {
    $isccPath = $iscc.Source
}

& $isccPath "/DAppVersion=$Version" $iss
if ($LASTEXITCODE -ne 0) {
    throw "Inno Setup compilation failed with exit code $LASTEXITCODE."
}
if (-not (Test-Path -LiteralPath $setupPath -PathType Leaf)) {
    throw "Inno Setup did not create the expected output: $setupPath"
}

if ($SignCert) {
    if (-not (Test-Path -LiteralPath $SignCert -PathType Leaf)) {
        throw "Signing certificate file was not found: $SignCert"
    }
    $signtool = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if (-not $signtool) {
        throw 'signtool.exe was not found. Install the Windows SDK or add signtool.exe to PATH.'
    }
    & $signtool.Source sign /f $SignCert /fd SHA256 /tr 'http://timestamp.digicert.com' /td SHA256 $setupPath
    if ($LASTEXITCODE -ne 0) {
        throw "SignTool failed with exit code $LASTEXITCODE."
    }
} else {
    Write-Warning 'The setup executable will be unsigned; Windows SmartScreen may show a warning.'
}

$hash = (Get-FileHash -LiteralPath $setupPath -Algorithm SHA256).Hash.ToLowerInvariant()
"$hash  $(Split-Path -Leaf $setupPath)" | Set-Content -LiteralPath $hashPath -Encoding ascii
Write-Output "Created $setupPath"
Write-Output "SHA256 $hash (saved to $hashPath)"
