#requires -Version 7.0
[CmdletBinding()]
param([string]$OutputRoot)

$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$compiler = Join-Path $root '.tools/go/bin/go.exe'
if (-not (Test-Path -LiteralPath $compiler -PathType Leaf)) { throw 'Prepare o Go privado em .tools/go/bin/go.exe.' }
$version = (Get-Content -LiteralPath (Join-Path $root 'extension/manifest.json') -Raw | ConvertFrom-Json).version
if ($version -notmatch '^\d+\.\d+\.\d+$') { throw 'Versão numérica esperada.' }
if (-not $OutputRoot) { $OutputRoot = Join-Path $root ('dist/release-linux-' + $version + '-' + (Get-Date -Format 'yyyyMMdd-HHmmss')) }
$out = [IO.Path]::GetFullPath($OutputRoot)
if (-not $out.StartsWith((Join-Path $root 'dist') + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Use um diretório novo dentro de dist.' }
for ($cursor = $out; $cursor; $cursor = [IO.Path]::GetDirectoryName($cursor)) {
    if ((Test-Path -LiteralPath $cursor) -and ((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Link/reparse não permitido: $cursor" }
}
if (Test-Path -LiteralPath $out) { throw 'Diretório de saída já existe.' }
$stage = Join-Path $out 'build'
$payload = Join-Path $stage 'installer'
New-Item -ItemType Directory -Path $payload -Force | Out-Null
$envNames = @('GOOS','GOARCH','CGO_ENABLED','GOTOOLCHAIN','GOCACHE','GOMODCACHE','GOTMPDIR')
$previousEnv = @{}
foreach ($name in $envNames) { $previousEnv[$name] = [Environment]::GetEnvironmentVariable($name) }
try {
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    $env:GOTOOLCHAIN = 'local'
    $env:GOCACHE = Join-Path $root '.tools/gocache'
    $env:GOMODCACHE = Join-Path $root '.tools/gomodcache'
    $env:GOTMPDIR = Join-Path $root '.tools/tmp'
    foreach ($path in @($env:GOCACHE,$env:GOMODCACHE,$env:GOTMPDIR)) { New-Item -ItemType Directory -Path $path -Force | Out-Null }
    & $compiler -C (Join-Path $root 'host') build -trimpath -ldflags '-s -w' -o (Join-Path $payload 'dwnl-host') ./cmd/dwnl-host
    if ($LASTEXITCODE -ne 0) { throw 'Build Linux falhou.' }
} finally {
    foreach ($name in $envNames) { [Environment]::SetEnvironmentVariable($name, $previousEnv[$name]) }
}
$chrome = Join-Path $stage 'chrome'
$extension = Join-Path $chrome 'extension'
& node (Join-Path $root 'tools/build-extension.mjs') --output $extension
if ($LASTEXITCODE -ne 0) { throw 'Build da extensão Linux falhou.' }
# The Ubuntu TAR is a complete manual-install download; the ZIP is also
# supplied separately for users updating only the unpacked extension.
Copy-Item -LiteralPath $extension -Destination (Join-Path $payload 'extension') -Recurse
foreach ($name in @('install.sh','uninstall.sh','install.py','NOTICES.md')) { Copy-Item -LiteralPath (Join-Path $root "installer/linux/$name") -Destination $payload }
$licenses = Join-Path $payload 'licenses'
New-Item -ItemType Directory -Path $licenses | Out-Null
foreach ($name in @('Go-1.27.2-LICENSE.txt','golang.org-x-sys-v0.38.0-LICENSE.txt')) { Copy-Item -LiteralPath (Join-Path $root "release/licenses/$name") -Destination $licenses }
Copy-Item -LiteralPath (Join-Path $root 'contracts/extension-identity.json') -Destination $payload
Copy-Item -LiteralPath (Join-Path $root 'release/suppliers.linux.lock.json') -Destination $payload
Copy-Item -LiteralPath (Join-Path $root 'release/INSTALL.ubuntu.pt-BR.md') -Destination (Join-Path $payload 'LEIA-ME.md')
Copy-Item -LiteralPath (Join-Path $root 'release/INSTALL.ubuntu.pt-BR.md') -Destination (Join-Path $chrome 'LEIA-ME.md')
& python (Join-Path $root 'installer/linux/package.py') --stage $stage --assets (Join-Path $out 'assets') --version $version
if ($LASTEXITCODE -ne 0) { throw 'Empacotamento/verificação Linux falhou.' }
Write-Output "Pacote Ubuntu e extensão prontos: $(Join-Path $out 'assets')"
