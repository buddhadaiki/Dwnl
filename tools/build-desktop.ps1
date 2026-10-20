[CmdletBinding()]
param([string]$OutputRoot)
$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if(-not $OutputRoot){$OutputRoot=Join-Path $root 'dist/desktop'}
$out=[IO.Path]::GetFullPath($OutputRoot)
if(-not $out.StartsWith((Join-Path $root 'dist')+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)){throw 'Output deve ficar dentro de dist'}
$cursor=$out;while($cursor){if(Test-Path -LiteralPath $cursor){if((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Reparse point no build'}};$cursor=Split-Path $cursor -Parent}
New-Item -ItemType Directory -Force -Path $out | Out-Null
$env:GOOS='windows';$env:GOARCH='amd64';$env:CGO_ENABLED='0';$env:GOTOOLCHAIN='local'
$env:GOCACHE=Join-Path $root '.tools/gocache';$env:GOMODCACHE=Join-Path $root '.tools/gomodcache';$env:GOTMPDIR=Join-Path $root '.tools/tmp'
$compiler=Join-Path $root '.tools/go/bin/go.exe'
& $compiler -C (Join-Path $root 'host') build -trimpath -tags 'production,wv2runtime.error' -ldflags '-s -w -H windowsgui' -o (Join-Path $out 'Dwnl.exe') ./cmd/dwnl-app
if($LASTEXITCODE){throw 'Build do aplicativo falhou'}
& $compiler -C (Join-Path $root 'host') build -trimpath -ldflags '-s -w' -o (Join-Path $out 'dwnl-host.exe') ./cmd/dwnl-desktop-host
if($LASTEXITCODE){throw 'Build da ponte falhou'}
$compilerVersion=& $compiler version
foreach($name in @('Dwnl.exe','dwnl-host.exe')){
 $file=Get-Item -LiteralPath (Join-Path $out $name)
 $metadata=[ordered]@{schemaVersion=1;name=[IO.Path]::GetFileNameWithoutExtension($name);version='0.2.0';platform='windows-x86_64';path=[IO.Path]::GetRelativePath($root,$file.FullName).Replace('\','/');sha256=(Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant();sizeBytes=$file.Length;compiler=$compilerVersion;cgoEnabled=$false;registered=$false}
 $metadata | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $out ($name+'.json')) -Encoding utf8
}
$licenseDir=Join-Path $out 'licenses';New-Item -ItemType Directory -Force -Path $licenseDir | Out-Null
$modules=& $compiler -C (Join-Path $root 'host') list -deps -tags 'production,wv2runtime.error' -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}' ./cmd/dwnl-app ./cmd/dwnl-desktop-host
if($LASTEXITCODE){throw 'Não foi possível inventariar módulos'}
$licenses=@()
foreach($line in ($modules | Where-Object {$_} | Sort-Object -Unique)){$parts=$line.Split('|');if(-not $parts[1]){continue};$dir=$parts[2];if(-not $dir){throw "Módulo sem fonte local: $($parts[0])"};$sources=@(Get-ChildItem -LiteralPath $dir -File | Where-Object Name -Match '^(LICENSE|LICENCE|COPYING|NOTICE)(\..*)?$');if(-not $sources.Count){throw "Licença não encontrada: $($parts[0])"};foreach($source in $sources){$name=($parts[0]+'-'+$parts[1]+'-'+$source.Name) -replace '[^A-Za-z0-9._-]','_';$dest=Join-Path $licenseDir $name;Copy-Item -LiteralPath $source.FullName -Destination $dest -Force;$licenses+=@{module=$parts[0];version=$parts[1];path=[IO.Path]::GetRelativePath($root,$dest).Replace('\','/');sha256=(Get-FileHash -LiteralPath $dest -Algorithm SHA256).Hash.ToLowerInvariant()}}}
$licenses | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $out 'module-licenses.json') -Encoding utf8
Write-Output "Aplicativo e ponte compilados em $out"