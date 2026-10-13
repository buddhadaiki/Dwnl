[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$compiler = Join-Path $projectRoot '.tools/go/bin/go.exe'
if (-not (Test-Path -LiteralPath $compiler -PathType Leaf)) {
    throw 'Go privado não foi preparado em .tools/go/bin/go.exe.'
}
$outputRoot = Join-Path $projectRoot 'dist/helper'
New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
foreach ($folder in @('.tools/gocache', '.tools/gomodcache', '.tools/tmp')) {
    New-Item -ItemType Directory -Path (Join-Path $projectRoot $folder) -Force | Out-Null
}

$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$env:GOTOOLCHAIN = 'local'
$env:GOCACHE = Join-Path $projectRoot '.tools/gocache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools/gomodcache'
$env:GOTMPDIR = Join-Path $projectRoot '.tools/tmp'
$outputPath = Join-Path $outputRoot 'dwnl-host.exe'
& $compiler -C (Join-Path $projectRoot 'host') build -trimpath -ldflags '-s -w' -o $outputPath ./cmd/dwnl-host
if ($LASTEXITCODE -ne 0) { throw "Build do helper falhou ($LASTEXITCODE)." }
$hash = (Get-FileHash -LiteralPath $outputPath -Algorithm SHA256).Hash.ToLowerInvariant()
$info = Get-Item -LiteralPath $outputPath
$goVersion = & $compiler version
if ($LASTEXITCODE -ne 0) { throw 'Não foi possível verificar a versão do compilador.' }
[ordered]@{
    schemaVersion = 1
    name = 'dwnl-host'
    version = '0.1.0'
    platform = 'windows-x86_64'
    path = 'dist/helper/dwnl-host.exe'
    sha256 = $hash
    sizeBytes = $info.Length
    compiler = $goVersion
    cgoEnabled = $false
    registered = $false
} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $outputRoot 'build.json') -Encoding UTF8
Write-Output "Helper compilado: $outputPath"
Write-Output "SHA256: $hash"
