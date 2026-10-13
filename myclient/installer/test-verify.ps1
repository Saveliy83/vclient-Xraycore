$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'verify.ps1')

$script:MockSignatureStatus = [System.Management.Automation.SignatureStatus]::NotSigned
function Get-AuthenticodeSignature {
    param([string]$FilePath)
    return [pscustomobject]@{
        Status = $script:MockSignatureStatus
        SignerCertificate = $null
    }
}

$missingPath = Join-Path ([IO.Path]::GetTempPath()) ("vclient-missing-{0}.dll" -f [guid]::NewGuid())
try {
    Test-WintunSignature -Path $missingPath -Publisher 'WireGuard LLC' | Out-Null
    throw 'Expected a missing Wintun payload to be rejected.'
} catch {
    if ($_.Exception.Message -notlike 'Wintun payload file is missing:*') {
        throw
    }
}

$fakePath = Join-Path ([IO.Path]::GetTempPath()) ("vclient-invalid-{0}.dll" -f [guid]::NewGuid())
try {
    [IO.File]::WriteAllBytes($fakePath, [byte[]](0x4D, 0x5A))
    try {
        Test-WintunSignature -Path $fakePath -Publisher 'WireGuard LLC' | Out-Null
        throw 'Expected an invalid Wintun signature to be rejected.'
    } catch {
        if ($_.Exception.Message -notlike 'Wintun Authenticode signature is not valid*') {
            throw
        }
    }
} finally {
    if (Test-Path -LiteralPath $fakePath) {
        Remove-Item -LiteralPath $fakePath -Force
    }
}

Write-Output 'Signature verification tests passed: missing file and invalid signature are rejected.'
