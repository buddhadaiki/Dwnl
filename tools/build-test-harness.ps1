[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$compiler = Join-Path $projectRoot '.tools/go/bin/go.exe'
foreach ($folder in @('.tools/gocache', '.tools/gomodcache', '.tools/tmp', '.tools/integration')) {
    New-Item -ItemType Directory -Path (Join-Path $projectRoot $folder) -Force | Out-Null
}
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$env:GOTOOLCHAIN = 'local'
$env:GOCACHE = Join-Path $projectRoot '.tools/gocache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools/gomodcache'
$env:GOTMPDIR = Join-Path $projectRoot '.tools/tmp'
& $compiler -C (Join-Path $projectRoot 'host') build -trimpath -o (Join-Path $projectRoot '.tools/integration/dwnl-test-harness.exe') ./cmd/dwnl-test-harness
if ($LASTEXITCODE -ne 0) { throw 'Não foi possível compilar o harness isolado.' }
Write-Output 'Harness isolado compilado; nenhum host registrado ou instalado.'
