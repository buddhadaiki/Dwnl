[CmdletBinding()]
param([string]$OutputRoot,[switch]$SkipBuild)
$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if(-not $OutputRoot){$OutputRoot=Join-Path $root ('dist/desktop-release-'+(Get-Date -Format 'yyyyMMdd-HHmmss'))}
$out=[IO.Path]::GetFullPath($OutputRoot)
if(-not $out.StartsWith((Join-Path $root 'dist')+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'Saída deve ficar dentro de dist'}
$cursor=$out
while($cursor){if(Test-Path -LiteralPath $cursor){if((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Reparse point no destino'}};$cursor=Split-Path $cursor -Parent}
if(Test-Path -LiteralPath $out){throw 'Use um diretório de saída novo'}
if(-not $SkipBuild){& (Join-Path $PSScriptRoot 'build-desktop.ps1')}
New-Item -ItemType Directory -Path $out | Out-Null
$stage=Join-Path $out 'bootstrap'
& (Join-Path $PSScriptRoot 'bootstrap.ps1') -ProjectRoot $root -OutputRoot $stage -Desktop *> (Join-Path $out 'bootstrap.log')
$report=Get-Content -Raw -LiteralPath (Join-Path $stage 'package-report.json')|ConvertFrom-Json
if(-not $report.desktop -or -not $report.compiledInstaller -or $report.installedOrRegistered){throw 'Bootstrap incompleto'}
$bundle=Join-Path $out 'Dwnl-0.2.1-windows-x64'
New-Item -ItemType Directory -Path $bundle | Out-Null
& node (Join-Path $PSScriptRoot 'build-extension.mjs') --desktop --output (Join-Path $bundle 'extension')
if($LASTEXITCODE){throw 'Build da extensão falhou'}
$setup=Join-Path $bundle 'Dwnl-Setup.exe'
Copy-Item -LiteralPath (Join-Path $stage 'Dwnl-Setup.exe') -Destination $setup
if((Get-FileHash -LiteralPath $setup -Algorithm SHA256).Hash.ToLowerInvariant() -cne $report.compiledInstaller.sha256){throw 'Hash do setup copiado diverge'}
Copy-Item -LiteralPath (Join-Path $root 'release/INSTALL.desktop.pt-BR.md') -Destination (Join-Path $bundle 'LEIA-ME.md')
$hashes=Get-ChildItem -LiteralPath $bundle -File -Recurse | Sort-Object FullName | ForEach-Object { (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()+'  '+[IO.Path]::GetRelativePath($bundle,$_.FullName).Replace('\','/') }
$hashes|Set-Content -LiteralPath (Join-Path $bundle 'SHA256SUMS.txt') -Encoding utf8
$zip=Join-Path $out 'Dwnl-0.2.1-windows-x64.zip'
Compress-Archive -LiteralPath $bundle -DestinationPath $zip -CompressionLevel Optimal
$archive=[IO.Compression.ZipFile]::OpenRead($zip)
try {
 $expected=@{}
 Get-ChildItem -LiteralPath $bundle -File -Recurse | ForEach-Object { $expected[[IO.Path]::GetFileName($bundle)+'/'+[IO.Path]::GetRelativePath($bundle,$_.FullName).Replace('\','/')]=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
 $seen=@{}
 foreach($entry in $archive.Entries){
   if($entry.FullName.EndsWith('/')){continue}
   $entryName=$entry.FullName.Replace('\','/')
   if(-not $expected.ContainsKey($entryName) -or $seen.ContainsKey($entryName)){throw 'Entrada inesperada/duplicada no ZIP'}
   $stream=$entry.Open();$hasher=[Security.Cryptography.SHA256]::Create()
   try{$actual=[Convert]::ToHexString($hasher.ComputeHash($stream)).ToLowerInvariant()}finally{$stream.Dispose();$hasher.Dispose()}
   if($actual -cne $expected[$entryName]){throw 'Hash diverge dentro do ZIP'}
   $seen[$entryName]=$true
 }
 if($seen.Count -ne $expected.Count){throw 'Arquivos ausentes no ZIP'}
} finally {$archive.Dispose()}
$artifact=Get-Item -LiteralPath $zip
[ordered]@{version='0.2.1';zip=$artifact.FullName;sha256=(Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant();sizeBytes=$artifact.Length;installed=$false;published=$false}|ConvertTo-Json|Tee-Object -FilePath (Join-Path $out 'release-report.json')
