#requires -Version 7.0
[CmdletBinding()]
param([string]$OutputRoot)

$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$releaseOutput = $OutputRoot
. (Join-Path $PSScriptRoot 'package.ps1')
$OutputRoot = $releaseOutput
$manifest = Get-Content -LiteralPath (Join-Path $root 'extension/manifest.json') -Raw | ConvertFrom-Json
$version = $manifest.version
if ($version -notmatch '^\d+\.\d+\.\d+$') { throw 'Expected a numeric extension version.' }
if (-not $OutputRoot) { $OutputRoot = Join-Path $root ('dist/release-' + $version + '-' + (Get-Date -Format 'yyyyMMdd-HHmmss')) }
$out = [IO.Path]::GetFullPath($OutputRoot)
if (-not $out.StartsWith((Join-Path $root 'dist') + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Release output must be within project dist.' }
[void](Resolve-DwnlPayloadPath $root ([IO.Path]::GetRelativePath($root, $out)))
if (Test-Path -LiteralPath $out) { throw 'Release output already exists; choose a fresh directory.' }
New-Item -ItemType Directory -Path $out | Out-Null

# Build from this checkout; existing installed helper and loaded extension are untouched.
& (Join-Path $PSScriptRoot 'build-host.ps1')
$bootstrap = Join-Path $out 'build/bootstrap'
& (Join-Path $PSScriptRoot 'bootstrap.ps1') -ProjectRoot $root -OutputRoot $bootstrap | Out-Null
$report = Get-Content -LiteralPath (Join-Path $bootstrap 'package-report.json') -Raw | ConvertFrom-Json
if ($report.host.version -cne $version) { throw 'Helper and extension versions differ.' }
$setup = Join-Path $bootstrap 'Dwnl-Setup.exe'
$bundle = Join-Path $out 'build/bundle'
$extension = Join-Path $bundle 'extension'
& node (Join-Path $PSScriptRoot 'build-extension.mjs') --installer $setup --output $extension
if ($LASTEXITCODE -ne 0) { throw 'Release extension build failed.' }

$assets = Join-Path $out 'assets'
New-Item -ItemType Directory -Path $assets | Out-Null
$setupName = "Dwnl-Setup-$version-windows-x64.exe"
$zipName = "Dwnl-$version-chrome.zip"
Copy-Item -LiteralPath $setup -Destination (Join-Path $assets $setupName)
if ((Get-DwnlSha256 (Join-Path $assets $setupName)) -cne $report.compiledInstaller.sha256) { throw 'Release setup copy hash mismatch.' }
Copy-Item -LiteralPath (Join-Path $root 'release/INSTALL.pt-BR.md') -Destination (Join-Path $bundle 'LEIA-ME.md')
[IO.Compression.ZipFile]::CreateFromDirectory($bundle, (Join-Path $assets $zipName))

# Read back every archived byte, including the setup, before exposing release assets.
$zip = [IO.Compression.ZipFile]::OpenRead((Join-Path $assets $zipName))
try {
    $files = @(Get-ChildItem -LiteralPath $bundle -Recurse -File)
    if ($zip.Entries.Count -ne $files.Count) { throw 'Release ZIP inventory mismatch.' }
    foreach ($file in $files) {
        $name = [IO.Path]::GetRelativePath($bundle, $file.FullName).Replace('\', '/')
        $entry = $zip.GetEntry($name)
        if (-not $entry -or $entry.Length -ne $file.Length) { throw "Missing or truncated release entry: $name" }
        $stream = $entry.Open()
        try { $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($stream)).ToLowerInvariant() }
        finally { $stream.Dispose() }
        if ($hash -cne (Get-DwnlSha256 $file.FullName)) { throw "Release ZIP hash mismatch: $name" }
    }
} finally { $zip.Dispose() }
Copy-Item -LiteralPath (Join-Path $root 'release/INSTALL.pt-BR.md') -Destination (Join-Path $assets 'LEIA-ME.md')
$checksums = foreach ($name in @($setupName, $zipName, 'LEIA-ME.md')) { (Get-DwnlSha256 (Join-Path $assets $name)) + '  ' + $name }
[IO.File]::WriteAllLines((Join-Path $assets 'SHA256SUMS.txt'), $checksums, [Text.UTF8Encoding]::new($false))
$notes = @"
# Dwnl $version — versão experimental para Windows x64

Baixe **$zipName**, extraia em uma pasta permanente e siga o **LEIA-ME.md** incluído.
O ZIP inclui a extensão e o instalador em extension/installer/Dwnl-Setup.exe.
Para reinstalar somente o auxiliar, baixe **$setupName**.

O setup precisa de internet: baixa versões verificadas de yt-dlp, Deno e FFmpeg dos fornecedores. Não requer Python nem ferramentas instaladas globalmente. A extensão é carregada manualmente em chrome://extensions, com Modo do desenvolvedor e Carregar sem compactação.

Instalador sem assinatura digital. Versão experimental, sem garantia de suporte a todos os vídeos. Use somente conteúdo cujo download você tenha autorização para realizar. SHA-256 dos anexos em SHA256SUMS.txt.
"@
[IO.File]::WriteAllText((Join-Path $out 'RELEASE-NOTES.md'), $notes, [Text.UTF8Encoding]::new($false))
Write-Output "Arquivos para anexar ao Release: $assets"
Write-Output "Texto do Release: $(Join-Path $out 'RELEASE-NOTES.md')"
