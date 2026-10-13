function Test-WintunSignature {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Publisher
    )

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Wintun payload file is missing: $Path"
    }

    $signature = Get-AuthenticodeSignature -FilePath $Path
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid) {
        throw "Wintun Authenticode signature is not valid (status: $($signature.Status)): $Path"
    }
    if ($null -eq $signature.SignerCertificate) {
        throw "Wintun signature has no signer certificate: $Path"
    }

    $actualPublisher = $signature.SignerCertificate.GetNameInfo(
        [System.Security.Cryptography.X509Certificates.X509NameType]::SimpleName,
        $false
    )
    if (-not [string]::Equals(
        $actualPublisher,
        $Publisher,
        [System.StringComparison]::OrdinalIgnoreCase
    )) {
        throw "Wintun publisher '$actualPublisher' does not match expected publisher '$Publisher'."
    }

    return $true
}
