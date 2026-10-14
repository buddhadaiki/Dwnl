[CmdletBinding()]
param([string]$ProjectRoot,[string]$OutputRoot,[switch]$StageOnly)
$bootstrapInvocation=$MyInvocation.InvocationName
$bootstrapProject=$ProjectRoot
$bootstrapOutput=$OutputRoot
$bootstrapStageOnly=[bool]$StageOnly
. (Join-Path $PSScriptRoot 'package.ps1')

function Assert-DwnlSuppliers {
    param($Lock)
    if ($Lock.schemaVersion -ne 1 -or $Lock.mode -ne 'supplier-acquisition' -or ($Lock.suppliers.name | Sort-Object) -join ',' -ne 'deno,gyan-ffmpeg,yt-dlp') { throw 'Expected three pinned suppliers.' }
    $urls=@{
        'yt-dlp'="https://github.com/yt-dlp/yt-dlp/releases/download/$($Lock.suppliers.Where({$_.name -eq 'yt-dlp'})[0].version)/yt-dlp.exe"
        'deno'="https://github.com/denoland/deno/releases/download/v$($Lock.suppliers.Where({$_.name -eq 'deno'})[0].version)/deno-x86_64-pc-windows-msvc.zip"
        'gyan-ffmpeg'="https://github.com/GyanD/codexffmpeg/releases/download/$($Lock.suppliers.Where({$_.name -eq 'gyan-ffmpeg'})[0].version)/ffmpeg-$($Lock.suppliers.Where({$_.name -eq 'gyan-ffmpeg'})[0].version)-essentials_build.zip"
    }
    $destinations=@{}
    foreach($supplier in $Lock.suppliers) {
        if ($supplier.version -notmatch '^\d+(\.\d+){2}$' -or $supplier.url -cne $urls[$supplier.name]) {throw 'An exact official supplier URL is required; no floating latest.'}
        if ($supplier.sha256 -notmatch '^[a-f0-9]{64}$' -or $supplier.sizeBytes -le 0 -or $supplier.archiveType -notin @('exe','zip') -or -not $supplier.files.Count -or -not $supplier.license -or -not $supplier.sourceReference) {throw 'Supplier artifact pin/license is incomplete.'}
        foreach($file in $supplier.files) {
            if ($file.path -notmatch '^(bin/[a-z-]+\.exe|licenses/[A-Za-z0-9._-]+\.txt)$' -or $file.sha256 -notmatch '^[a-f0-9]{64}$' -or $file.sizeBytes -le 0 -or $file.sizeBytes -gt 536870912 -or $destinations.ContainsKey($file.path.ToLowerInvariant())) {throw 'Supplier file pin/destination is invalid.'}
            if ($supplier.archiveType -eq 'zip' -and ($file.entry -notmatch '^[A-Za-z0-9._/-]+$' -or $file.entry -match '(^|/)\.\.(/|$)' -or $file.entry.StartsWith('/'))) {throw 'Supplier archive member is unsafe.'}
            $destinations[$file.path.ToLowerInvariant()]=$true
        }
    }
    if (@($destinations.Keys | Where-Object {$_ -like 'bin/*'} | Sort-Object) -join ',' -ne 'bin/deno.exe,bin/ffmpeg.exe,bin/ffprobe.exe,bin/yt-dlp.exe') {throw 'Exactly four private executables must be acquired.'}
}

# Workspace validation of the same exact selected files used by the installer.
# Never execute, register or install the acquired files from this function.
function Import-DwnlSupplierArtifact {
    param([string]$Artifact,[string]$OutputRoot,$Supplier)
    if ((Get-DwnlSha256 $Artifact) -cne $Supplier.sha256 -or (Get-Item -LiteralPath $Artifact).Length -ne $Supplier.sizeBytes) {throw 'Supplier artifact hash/size mismatch; nothing extracted.'}
    $target=[IO.Path]::GetFullPath($OutputRoot)
    if (Test-Path -LiteralPath $target) {throw 'Acquisition output must be fresh.'}
    $parent=Split-Path $target -Parent
    [void](Resolve-DwnlPayloadPath -ProjectRoot $parent -RelativePath (Split-Path $target -Leaf))
    New-Item -ItemType Directory -Path $target | Out-Null
    $zip=$null
    try {
        if($Supplier.archiveType -eq 'zip') {
            $zip=[IO.Compression.ZipFile]::OpenRead($Artifact)
            $names=@{}; $expanded=0L
            foreach($entry in $zip.Entries) {
                $name=$entry.FullName; $mode=($entry.ExternalAttributes -shr 16) -band 0xf000
                if ([IO.Path]::IsPathRooted($name) -or $name.Contains(':') -or $name.Contains('\') -or $name -match '(^|/)\.\.(/|$)' -or $mode -eq 0xa000 -or $entry.Length -gt 536870912 -or $names.ContainsKey($name.ToLowerInvariant())) {throw 'Unsafe or duplicate supplier archive entry.'}
                $expanded+=$entry.Length; if($expanded -gt 1073741824) {throw 'Supplier archive expansion exceeds limit.'}
                $names[$name.ToLowerInvariant()]=$true
            }
        }
        foreach($file in $Supplier.files) {
            $dest=Resolve-DwnlPayloadPath $target $file.path
            New-Item -ItemType Directory -Path (Split-Path $dest -Parent) -Force | Out-Null
            if($zip) {
                $entries=@($zip.Entries | Where-Object FullName -CEQ $file.entry)
                if($entries.Count -ne 1 -or $entries[0].Length -ne $file.sizeBytes) {throw 'Expected supplier member/size is absent.'}
                $inputStream=$entries[0].Open(); $outputStream=[IO.File]::Open($dest,[IO.FileMode]::CreateNew)
                try {$inputStream.CopyTo($outputStream)} finally {$outputStream.Dispose();$inputStream.Dispose()}
            } else {Copy-Item -LiteralPath $Artifact -Destination $dest}
            if ((Get-DwnlSha256 $dest) -cne $file.sha256 -or (Get-Item -LiteralPath $dest).Length -ne $file.sizeBytes) {throw 'Supplier extracted file hash/size mismatch.'}
            if($file.path.EndsWith('.exe')) {
                [void](Assert-DwnlBinary $target ([pscustomobject]@{name=$file.path;path=$file.path;sha256=$file.sha256;sizeBytes=$file.sizeBytes;platform='windows-x86_64';version=$Supplier.version}))
            }
        }
    } finally {if($zip){$zip.Dispose()}}
    return [ordered]@{supplier=$Supplier.name;artifactSha256=$Supplier.sha256;selectedFiles=$Supplier.files;archiveEntriesValidated=$true;executed=$false;installedOrRegistered=$false}
}

function Write-DwnlUTF8 {param([string]$Path,[string]$Text) [IO.File]::WriteAllText($Path,$Text,[Text.UTF8Encoding]::new($false))}
function New-DwnlBootstrapStage {
    param([string]$ProjectRoot,[string]$OutputRoot)
    $root=[IO.Path]::GetFullPath($ProjectRoot).TrimEnd('\','/')
    $lock=Get-Content -Raw -LiteralPath (Join-Path $root 'release/suppliers.lock.json') | ConvertFrom-Json
    Assert-DwnlSuppliers $lock
    $identity=Get-Content -Raw -LiteralPath (Join-Path $root 'contracts/extension-identity.json') | ConvertFrom-Json
    Assert-DwnlIdentity $identity
    $hostMetadata=Get-Content -Raw -LiteralPath (Join-Path $root 'dist/helper/build.json') | ConvertFrom-Json
    $hostPath=Assert-DwnlBinary $root $hostMetadata
    $out=[IO.Path]::GetFullPath($OutputRoot)
    if(-not $out.StartsWith((Join-Path $root 'dist')+'\',[StringComparison]::OrdinalIgnoreCase)) {throw 'Bootstrap output must be within project dist.'}
    [void](Resolve-DwnlPayloadPath $root ([IO.Path]::GetRelativePath($root,$out)))
    if(Test-Path -LiteralPath $out) {throw 'Bootstrap output already exists; use a fresh output.'}
    $payload=Join-Path $out 'payload'
    $expected=@{'dwnl-host.exe'=$hostMetadata.sha256;'suppliers.lock.json'=(Get-DwnlSha256 (Join-Path $root 'release/suppliers.lock.json'));'THIRD_PARTY_NOTICES.md'=(Get-DwnlSha256 (Join-Path $root 'release/SUPPLIER_ACQUISITION_NOTICES.md'))}
    New-Item -ItemType Directory -Path (Join-Path $payload 'licenses') -Force | Out-Null
    Copy-Item -LiteralPath $hostPath -Destination (Join-Path $payload 'dwnl-host.exe')
    $files=@('dwnl-host.exe','suppliers.lock.json','THIRD_PARTY_NOTICES.md')
    Copy-Item -LiteralPath (Join-Path $root 'release/suppliers.lock.json') -Destination (Join-Path $payload 'suppliers.lock.json')
    Copy-Item -LiteralPath (Join-Path $root 'release/SUPPLIER_ACQUISITION_NOTICES.md') -Destination (Join-Path $payload 'THIRD_PARTY_NOTICES.md')
    $existingLock=Get-Content -Raw -LiteralPath (Join-Path $root 'release/dependencies.lock.json') | ConvertFrom-Json
    $licenseNames=@('Go-1.27.2-LICENSE.txt','golang.org-x-sys-v0.38.0-LICENSE.txt','Inno-Setup-LICENSE.txt','yt-dlp-LICENSE.txt','yt-dlp-THIRD_PARTY_LICENSES.txt','deno-LICENSE.txt','deno-node-types-LICENSE.txt','deno-undici-types-LICENSE.txt','deno-webgpu-LICENSE.txt')
    foreach($name in $licenseNames) {
        $license=@($existingLock.licenseFiles | Where-Object {[IO.Path]::GetFileName($_.path) -ceq $name})
        if($license.Count -ne 1) {throw 'Pinned embedded notice is missing.'}
        $path=Resolve-DwnlPayloadPath $root $license[0].path
        if((Get-DwnlSha256 $path) -cne $license[0].sha256) {throw 'Pinned embedded notice hash mismatch.'}
        $relative='licenses/'+$name; Copy-Item -LiteralPath $path -Destination (Join-Path $payload $relative); $files+=$relative
        $expected[$relative]=$license[0].sha256
    }
    $hashes=@();foreach($relative in $files){$path=Join-Path $payload $relative;$sha=Get-DwnlSha256 $path;if($sha -cne $expected[$relative]){throw 'Staged bootstrap payload hash mismatch.'};$hashes+=[ordered]@{path=$relative;sha256=$sha;sizeBytes=(Get-Item -LiteralPath $path).Length}}
    if(($hashes | Where-Object path -eq 'dwnl-host.exe').sha256 -cne $hostMetadata.sha256) {throw 'Staged helper hash mismatch.'}
    $supplierFiles=@($lock.suppliers.files)
    $sourceLines=@();foreach($file in $hashes){$destDir='{app}';$directory=[IO.Path]::GetDirectoryName($file.path.Replace('/','\'));if($directory){$destDir+='\'+$directory};$sourceLines+='Source: "'+(Join-Path $payload $file.path)+'"; DestDir: "'+$destDir+'"; Hash: "'+$file.sha256+'"; Flags: ignoreversion uninsneveruninstall'}
    $acquire=@('procedure AcquireSuppliers();','begin','  DownloadPage.Clear;')
    foreach($supplier in $lock.suppliers) {$acquire+="  DownloadPage.Add('$($supplier.url)', '$($supplier.name).$($supplier.archiveType)', '$($supplier.sha256)');"}
    $acquire+=@('  DownloadPage.Show;','  try','    DownloadPage.Download;','  finally','    DownloadPage.Hide;','  end;')
    $verify=@('procedure VerifyInstalledPayload();','begin')
    $delete=@('procedure DeleteOwnedPayload();','begin')
    foreach($supplier in $lock.suppliers) {
        $leaf=$supplier.name+'.'+$supplier.archiveType; $staging='{tmp}\supplier-'+$supplier.name
        if($supplier.archiveType -eq 'zip') {$acquire+="  ExtractArchive(ExpandConstant('{tmp}\$leaf'), ExpandConstant('$staging'), '', True, nil);"}
        foreach($file in $supplier.files) {
            $source=if($supplier.archiveType -eq 'zip') {$staging+'\'+$file.entry.Replace('/','\')} else {'{tmp}\'+$leaf}
            $dest=$file.path.Replace('/','\')
            $acquire+="  RequireHash(ExpandConstant('$source'), '$($file.sha256)');"
            $destDir='{app}\'+[IO.Path]::GetDirectoryName($dest)
            $sourceLines+='Source: "'+$source+'"; DestDir: "'+$destDir+'"; DestName: "'+[IO.Path]::GetFileName($dest)+'"; Flags: external ignoreversion uninsneveruninstall'
            $verify+="  RequireHash(ExpandConstant('{app}\$dest'), '$($file.sha256)');"
            $delete+="  DeleteIfOwned(ExpandConstant('{app}\$dest'), '$($file.sha256)');"
        }
    }
    foreach($file in $hashes){$dest=$file.path.Replace('/','\');$verify+="  RequireHash(ExpandConstant('{app}\$dest'), '$($file.sha256)');";$delete+="  DeleteIfOwned(ExpandConstant('{app}\$dest'), '$($file.sha256)');"}
    $acquire+='end;';$verify+='end;';$delete+='end;'
    Write-DwnlUTF8 (Join-Path $out 'payload-files.iss') ($sourceLines -join "`r`n")
    Write-DwnlUTF8 (Join-Path $out 'supplier-acquisition.iss') (($acquire+$verify+$delete) -join "`r`n")
    $owned=@($files+$supplierFiles.path+'native-host.json' | ForEach-Object {"'"+$_.Replace('/','\')+"'"})
    Write-DwnlUTF8 (Join-Path $out 'owned-paths.iss') ('Files := ['+($owned -join ', ')+'];')
    $report=[ordered]@{schemaVersion=1;packageMode='supplier-acquisition';redistributionReady=$false;completeCorrespondingSource=$false;identity=$identity;host=$hostMetadata;payloadFiles=$files;payloadHashes=$hashes;supplierFiles=$supplierFiles;compiledInstaller=$null;installedOrRegistered=$false}
    Write-DwnlUTF8 (Join-Path $out 'package-report.json') ($report | ConvertTo-Json -Depth 40)
    return $report
}

if($bootstrapInvocation -ne '.') {
    $ErrorActionPreference='Stop'
    $ProjectRoot=$bootstrapProject;$OutputRoot=$bootstrapOutput;$StageOnly=$bootstrapStageOnly
    if(-not $ProjectRoot){$ProjectRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))}
    if(-not $OutputRoot){$OutputRoot=Join-Path $ProjectRoot 'dist/bootstrap'}
    $report=New-DwnlBootstrapStage $ProjectRoot $OutputRoot
    if(-not $StageOnly) {
        $metadata=Get-Content -Raw -LiteralPath (Join-Path $ProjectRoot '.tools/inno/verified-inno.json') | ConvertFrom-Json
        $compiler=Assert-DwnlBinary $ProjectRoot ([pscustomobject]@{name='ISCC';version=$metadata.version;platform=$metadata.platform;path=$metadata.path;sha256=$metadata.sha256;sizeBytes=(Get-Item -LiteralPath (Join-Path $ProjectRoot $metadata.path)).Length})
        $result=& $compiler '/Qp' "/DPackageRoot=$OutputRoot" "/DExtensionId=$($report.identity.extensionId)" '/DAppVersion=0.1.0' (Join-Path $ProjectRoot 'installer/bootstrap.iss') 2>&1
        $compileCode=$LASTEXITCODE;Write-DwnlUTF8 (Join-Path $OutputRoot 'inno-compile.log') ($result -join "`r`n")
        if($compileCode -ne 0){throw "Bootstrap compilation failed ($compileCode); see inno-compile.log."}
        $setup=Join-Path $OutputRoot 'Dwnl-Setup.exe'
        $report.compiledInstaller=[ordered]@{path=$setup;sha256=Get-DwnlSha256 $setup;sizeBytes=(Get-Item -LiteralPath $setup).Length;compilerVersion=$metadata.version;compilerExitCode=$compileCode;executed=$false}
        Write-DwnlUTF8 (Join-Path $OutputRoot 'package-report.json') ($report|ConvertTo-Json -Depth 40)
    }
    $report|ConvertTo-Json -Depth 40
}
