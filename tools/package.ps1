[CmdletBinding()]
param([string]$ProjectRoot, [string]$OutputRoot, [switch]$StageOnly, [switch]$ValidationOnly)

# Dot sourcing exposes validation functions without building, probing or registering anything.
function Resolve-DwnlPayloadPath {
    param([Parameter(Mandatory)][string]$ProjectRoot, [Parameter(Mandatory)][string]$RelativePath)
    if ([IO.Path]::IsPathRooted($RelativePath) -or $RelativePath -match '(^|[\\/])\.\.([\\/]|$)' -or $RelativePath.Contains(':')) {
        throw "Relative payload path required: $RelativePath"
    }
    $root = [IO.Path]::GetFullPath($ProjectRoot).TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
    $path = [IO.Path]::GetFullPath((Join-Path $root $RelativePath))
    if (-not $path.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Relative payload path escapes project.' }
    $cursor = $path
    while ($cursor -and $cursor.Length -ge $root.Length) {
        if (Test-Path -LiteralPath $cursor) {
            if (((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "Reparse point rejected: $cursor" }
        }
        if ($cursor.Equals($root, [StringComparison]::OrdinalIgnoreCase)) { break }
        $cursor = [IO.Path]::GetDirectoryName($cursor)
    }
    return $path
}

function Get-DwnlSha256 { param([string]$Path) return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }

function Assert-DwnlBinary {
    param([string]$ProjectRoot, $Entry)
    $path = Resolve-DwnlPayloadPath $ProjectRoot $Entry.path
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing payload: $($Entry.name)" }
    if ($Entry.sha256 -notmatch '^[a-f0-9]{64}$' -or (Get-DwnlSha256 $path) -ne $Entry.sha256) { throw "Hash mismatch: $($Entry.name)" }
    if ((Get-Item -LiteralPath $path).Length -ne $Entry.sizeBytes) { throw "Size mismatch: $($Entry.name)" }
    if ($Entry.platform -ne 'windows-x86_64' -or [string]::IsNullOrWhiteSpace($Entry.version)) { throw "Architecture/version metadata rejected: $($Entry.name)" }
    $stream = [IO.File]::OpenRead($path)
    $reader = [IO.BinaryReader]::new($stream)
    try {
        if ($stream.Length -lt 64 -or $reader.ReadUInt16() -ne 0x5a4d) { throw "Architecture: invalid PE $($Entry.name)" }
        $stream.Position = 60; $offset = $reader.ReadUInt32()
        if ($offset -gt ($stream.Length - 6)) { throw "Architecture: invalid PE offset $($Entry.name)" }
        $stream.Position = $offset
        if ($reader.ReadUInt32() -ne 0x4550 -or $reader.ReadUInt16() -ne 0x8664) { throw "Architecture: x64 PE required for $($Entry.name)" }
    } finally { $reader.Dispose(); $stream.Dispose() }
    return $path
}

function Assert-DwnlIdentity {
    param($Identity)
    if ($Identity.hostName -ne 'com.dwnl.host' -or $Identity.protocolVersion -ne 1) { throw 'Unexpected native-host identity/protocol.' }
    try { $der = [Convert]::FromBase64String($Identity.key) } catch { throw 'Invalid public identity key.' }
    $rsa = [Security.Cryptography.RSA]::Create()
    try { $read = 0; $rsa.ImportSubjectPublicKeyInfo($der, [ref]$read); if ($read -ne $der.Length) { throw 'Trailing key data.' } } finally { $rsa.Dispose() }
    $digest = [Security.Cryptography.SHA256]::HashData($der)
    $derived = -join ($digest[0..15] | ForEach-Object { [char](97 + ($_ -shr 4)); [char](97 + ($_ -band 15)) })
    if ($Identity.extensionId -cne $derived) { throw 'Native origin must match key-derived extension ID.' }
}

function Assert-DwnlCollateral {
    param([string]$ProjectRoot, $Lock, [switch]$RequireCompleteSource)
    if ($Lock.schemaVersion -ne 1 -or ($Lock.tools.name | Sort-Object) -join ',' -ne 'deno,ffmpeg,ffprobe,yt-dlp') { throw 'Dependency lock must cover exactly four private tools.' }
    if (-not $Lock.capabilities.libmp3lame) { throw 'Missing libmp3lame capability evidence.' }
    if (-not $Lock.licenseFiles.Count) { throw 'License inventory is missing.' }
    foreach ($license in $Lock.licenseFiles) {
        $path = Resolve-DwnlPayloadPath $ProjectRoot $license.path
        if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or (Get-Item -LiteralPath $path).Length -eq 0 -or (Get-DwnlSha256 $path) -ne $license.sha256) { throw "License hash/content missing: $($license.path)" }
    }
    $licenseNames = @($Lock.licenseFiles | ForEach-Object { [IO.Path]::GetFileName($_.path) })
    if (@($licenseNames | Sort-Object -Unique).Count -ne $licenseNames.Count) { throw 'License collateral destination names collide.' }
    foreach ($tool in $Lock.tools) {
        if ([string]::IsNullOrWhiteSpace($tool.license) -or -not $tool.licenseFiles.Count) { throw "License coverage missing: $($tool.name)" }
        foreach ($license in $tool.licenseFiles) { if ($license -notin $Lock.licenseFiles.path) { throw "License coverage missing: $($tool.name) / $license" } }
        if (-not $tool.sourceReferences.Count -or @($tool.sourceReferences | Where-Object { $_ -notmatch '^https://[^\s]+$' }).Count) { throw "Source references missing: $($tool.name)" }
    }
    $noticePath = Resolve-DwnlPayloadPath $ProjectRoot $Lock.notices.path
    if (-not (Test-Path -LiteralPath $noticePath -PathType Leaf) -or (Get-Item -LiteralPath $noticePath).Length -eq 0 -or (Get-DwnlSha256 $noticePath) -ne $Lock.notices.sha256) { throw 'Notices hash/content missing.' }
    if ($RequireCompleteSource) {
        if (-not $Lock.completeCorrespondingSource -or -not $Lock.sourceArtifacts.Count) { throw 'Release blocked: complete corresponding source archives have not been assembled.' }
        foreach ($tool in $Lock.tools) {
            if (-not @($Lock.sourceArtifacts | Where-Object { $tool.name -in $_.covers }).Count) { throw "Release blocked: complete corresponding source coverage missing for $($tool.name)." }
        }
        foreach ($source in $Lock.sourceArtifacts) {
            $path = Resolve-DwnlPayloadPath $ProjectRoot $source.path
            if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or (Get-DwnlSha256 $path) -ne $source.sha256) { throw 'Release blocked: source archive hash mismatch.' }
        }
    }
}

function New-DwnlNativeManifest {
    param($Identity, [string]$InstallRoot)
    Assert-DwnlIdentity $Identity
    return [ordered]@{ name = $Identity.hostName; description = 'Dwnl private native helper'; path = [IO.Path]::Combine([IO.Path]::GetFullPath($InstallRoot), 'dwnl-host.exe'); type = 'stdio'; allowed_origins = @("chrome-extension://$($Identity.extensionId)/") }
}

function Get-DwnlRemovalPlan {
    param([string]$InstallRoot, $Identity, [string]$RegisteredManifest)
    $install = [IO.Path]::GetFullPath($InstallRoot)
    $manifestPath = [IO.Path]::Combine($install, 'native-host.json')
    $owned = $false
    $registered = if ($RegisteredManifest) { [IO.Path]::GetFullPath($RegisteredManifest) } else { '' }
    if ($registered -and $registered.Equals($manifestPath, [StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
        try {
            [void](Resolve-DwnlPayloadPath $install 'native-host.json')
            $current = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
            $expected = New-DwnlNativeManifest $Identity $install
            $owned = $current.name -ceq $expected.name -and $current.type -ceq 'stdio' -and $current.path -ceq $expected.path -and @($current.allowed_origins).Count -eq 1 -and $current.allowed_origins[0] -ceq $expected.allowed_origins[0]
        } catch { $owned = $false }
    }
    return [ordered]@{ removeRegistration = $owned; removeManifest = $owned; files = @('dwnl-host.exe','bin/yt-dlp.exe','bin/deno.exe','bin/ffmpeg.exe','bin/ffprobe.exe','dependencies.lock.json','THIRD_PARTY_NOTICES.md','install-policy.json'); preserveDownloads = $true; preserveExtension = $true }
}

function Assert-DwnlInactiveFiles {
    param([string[]]$Paths)
    foreach ($path in $Paths) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { continue }
        try { $handle = [IO.File]::Open($path, [IO.FileMode]::Open, [IO.FileAccess]::Write, [IO.FileShare]::None); $handle.Dispose() }
        catch { throw "Active owned file or denied exclusive access: $path. Close Dwnl jobs before changing this installation." }
    }
}

function New-DwnlStage {
    param([string]$ProjectRoot, [string]$OutputRoot, $Identity, $Lock, $HostMetadata, [switch]$ValidationOnly)
    Assert-DwnlIdentity $Identity
    Assert-DwnlCollateral -ProjectRoot $ProjectRoot -Lock $Lock -RequireCompleteSource:(-not $ValidationOnly)
    $hostPath = Assert-DwnlBinary $ProjectRoot $HostMetadata
    $toolPaths = @{}; foreach ($entry in $Lock.tools) { $toolPaths[$entry.name] = Assert-DwnlBinary $ProjectRoot $entry }
    $root = [IO.Path]::GetFullPath($ProjectRoot).TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
    $out = [IO.Path]::GetFullPath($OutputRoot)
    $dist = Join-Path $root 'dist'
    if (-not $out.StartsWith($dist + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Package output must be a child of project dist.' }
    $relative = [IO.Path]::GetRelativePath($root, $out)
    [void](Resolve-DwnlPayloadPath $root $relative)
    if (Test-Path -LiteralPath $out) { throw 'Package output already exists; choose a fresh OutputRoot. No automatic recursive cleanup.' }
    $payload = Join-Path $out 'payload'
    New-Item -ItemType Directory -Path (Join-Path $payload 'bin'), (Join-Path $payload 'licenses') -Force | Out-Null
    Copy-Item -LiteralPath $hostPath -Destination (Join-Path $payload 'dwnl-host.exe')
    $files = @('dwnl-host.exe')
    foreach ($entry in $Lock.tools) { $rel = "bin/$($entry.name).exe"; Copy-Item -LiteralPath $toolPaths[$entry.name] -Destination (Join-Path $payload $rel); $files += $rel }
    $runtimeLock = [ordered]@{ schemaVersion = 1; distributionScope = 'private-prototype'; completeCorrespondingSource = [bool]$Lock.completeCorrespondingSource; sourceBundleStatus = $Lock.sourceBundleStatus; tools = @(); capabilities = $Lock.capabilities; licenseFiles = @(); notices = [ordered]@{ path='THIRD_PARTY_NOTICES.md'; sha256=$Lock.notices.sha256 }; sourceArtifacts = @() }
    foreach ($entry in $Lock.tools) { $copy = $entry | ConvertTo-Json -Depth 30 | ConvertFrom-Json; $copy.path = "bin/$($entry.name).exe"; $copy.licenseFiles = @($copy.licenseFiles | ForEach-Object { 'licenses/' + [IO.Path]::GetFileName($_) }); if ($copy.PSObject.Properties['absolutePath']) { $copy.PSObject.Properties.Remove('absolutePath') }; $runtimeLock.tools += $copy }
    foreach ($license in $Lock.licenseFiles) { $runtimeLock.licenseFiles += [ordered]@{ path='licenses/' + [IO.Path]::GetFileName($license.path); sha256=$license.sha256 } }
    if ($Lock.sourceArtifacts.Count) {
        New-Item -ItemType Directory -Path (Join-Path $payload 'sources') | Out-Null
        foreach ($source in $Lock.sourceArtifacts) {
            $sourcePath = Resolve-DwnlPayloadPath $root $source.path
            if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf) -or (Get-DwnlSha256 $sourcePath) -ne $source.sha256) { throw 'Source archive hash mismatch.' }
            $rel = 'sources/' + [IO.Path]::GetFileName($source.path)
            if (Test-Path -LiteralPath (Join-Path $payload $rel)) { throw 'Source archive destination names collide.' }
            Copy-Item -LiteralPath $sourcePath -Destination (Join-Path $payload $rel); $files += $rel
            $runtimeLock.sourceArtifacts += [ordered]@{ path=$rel; sha256=$source.sha256; covers=$source.covers }
        }
    }
    $runtimeLock | ConvertTo-Json -Depth 40 | Set-Content -LiteralPath (Join-Path $payload 'dependencies.lock.json') -Encoding utf8
    $files += 'dependencies.lock.json'
    foreach ($license in $Lock.licenseFiles) { $rel = 'licenses/' + [IO.Path]::GetFileName($license.path); Copy-Item -LiteralPath (Resolve-DwnlPayloadPath $root $license.path) -Destination (Join-Path $payload $rel); $files += $rel }
    Copy-Item -LiteralPath (Resolve-DwnlPayloadPath $root $Lock.notices.path) -Destination (Join-Path $payload 'THIRD_PARTY_NOTICES.md'); $files += 'THIRD_PARTY_NOTICES.md'
    $policy = [ordered]@{ schemaVersion = 1; hostName = $Identity.hostName; allowedOrigin = "chrome-extension://$($Identity.extensionId)/"; executableFiles = @('dwnl-host.exe','bin/yt-dlp.exe','bin/deno.exe','bin/ffmpeg.exe','bin/ffprobe.exe'); registryRoot = 'HKCU'; registryKey = 'Software\Google\Chrome\NativeMessagingHosts\com.dwnl.host'; manifestRelativePath = 'native-host.json'; removeManifestAndKeyOnlyWhenBothOwned = $true; preserveDownloads = $true; preserveExtension = $true; refuseActiveOwnedFiles = $true; terminateProcesses = $false }
    $policy | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $payload 'install-policy.json') -Encoding utf8; $files += 'install-policy.json'
    $hashes = @(); foreach ($rel in $files) { $hashes += [ordered]@{ path = $rel; sha256 = Get-DwnlSha256 (Join-Path $payload $rel); sizeBytes = (Get-Item -LiteralPath (Join-Path $payload $rel)).Length } }
    $report = [ordered]@{ schemaVersion = 1; packageMode = $(if ($ValidationOnly) { 'validation-only-do-not-distribute' } else { 'private-prototype' }); redistributionReady = $false; completeCorrespondingSource = [bool]$Lock.completeCorrespondingSource; sourceBundleStatus = $Lock.sourceBundleStatus; identity = $Identity; host = $HostMetadata; payloadFiles = $files; payloadHashes = $hashes; compiledInstaller = $null; installedOrRegistered = $false }
    $report | ConvertTo-Json -Depth 40 | Set-Content -LiteralPath (Join-Path $out 'package-report.json') -Encoding utf8
    return $report
}

function Assert-DwnlMediaCapabilities {
    param([string]$ProjectRoot, $Lock, [string]$OutputRoot)
    foreach ($entry in $Lock.tools) {
        $path = Assert-DwnlBinary $ProjectRoot $entry
        $args = $(if ($entry.name -in @('ffmpeg','ffprobe')) { @('-version') } else { @('--version') })
        $version = & $path @args 2>&1 | Out-String
        if ($LASTEXITCODE -ne 0 -or -not $version.Contains($entry.version)) { throw "Runtime version probe failed: $($entry.name)" }
    }
    $ffmpeg = Resolve-DwnlPayloadPath $ProjectRoot ($Lock.tools | Where-Object name -eq 'ffmpeg').path
    $ffprobe = Resolve-DwnlPayloadPath $ProjectRoot ($Lock.tools | Where-Object name -eq 'ffprobe').path
    $encoder = & $ffmpeg -hide_banner -encoders 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0 -or $encoder -notmatch '\blibmp3lame\b') { throw 'Runtime libmp3lame capability probe failed.' }
    $smoke = Join-Path $OutputRoot 'capability-smoke.mp3'
    & $ffmpeg -hide_banner -loglevel error -nostdin -f lavfi -i 'sine=frequency=997:sample_rate=44100:duration=0.1' -c:a libmp3lame -b:a 192k -n $smoke 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Runtime MP3 encoder smoke failed.' }
    $probe = & $ffprobe -v error -show_streams -show_format -of json $smoke 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { throw 'Runtime ffprobe smoke failed.' }
    $metadata = $probe | ConvertFrom-Json
    if ($metadata.streams[0].codec_name -ne 'mp3' -or [double]$metadata.format.duration -le 0) { throw 'Runtime MP3 capability readback failed.' }
    $probe | Set-Content -LiteralPath (Join-Path $OutputRoot 'capability-readback.json') -Encoding utf8
}

function Invoke-DwnlPackage {
    param([string]$ProjectRoot, [string]$OutputRoot, [switch]$StageOnly, [switch]$ValidationOnly)
    $root = [IO.Path]::GetFullPath($ProjectRoot)
    $identity = Get-Content -LiteralPath (Join-Path $root 'contracts/extension-identity.json') -Raw | ConvertFrom-Json
    $lock = Get-Content -LiteralPath (Join-Path $root 'release/dependencies.lock.json') -Raw | ConvertFrom-Json
    $hostMetadata = Get-Content -LiteralPath (Join-Path $root 'dist/helper/build.json') -Raw | ConvertFrom-Json
    # Stage-only is validation-only and cannot weaken the normal release source gate.
    $validation = $ValidationOnly -or $StageOnly
    $report = New-DwnlStage -ProjectRoot $root -OutputRoot $OutputRoot -Identity $identity -Lock $lock -HostMetadata $hostMetadata -ValidationOnly:$validation
    Assert-DwnlMediaCapabilities -ProjectRoot $root -Lock $lock -OutputRoot $OutputRoot
    if (-not $StageOnly) {
        $compilerMetadata = Get-Content -LiteralPath (Join-Path $root '.tools/inno/verified-inno.json') -Raw | ConvertFrom-Json
        $compiler = Assert-DwnlBinary -ProjectRoot $root -Entry ([pscustomobject]@{ name='ISCC'; version=$compilerMetadata.version; platform=$compilerMetadata.platform; path=$compilerMetadata.path; sha256=$compilerMetadata.sha256; sizeBytes=(Get-Item -LiteralPath (Join-Path $root $compilerMetadata.path)).Length })
        $payload = Join-Path $OutputRoot 'payload'
        $include = @(); foreach ($file in $report.payloadFiles) {
            $source = Join-Path $payload $file; $destination = '{app}'
            $dir = [IO.Path]::GetDirectoryName($file.Replace('/', '\'))
            if ($dir) { $destination += '\' + $dir }
            $include += 'Source: "' + $source.Replace('"','""') + '"; DestDir: "' + $destination + '"; Flags: ignoreversion'
        }
        $include | Set-Content -LiteralPath (Join-Path $OutputRoot 'payload-files.iss') -Encoding utf8
        $ownedPaths = @($report.payloadFiles + 'native-host.json' | ForEach-Object { "'" + $_.Replace('/', '\').Replace("'", "''") + "'" })
        ('Files := [' + ($ownedPaths -join ', ') + '];') | Set-Content -LiteralPath (Join-Path $OutputRoot 'owned-paths.iss') -Encoding utf8
        $fileName = $(if ($validation) { 'Dwnl-Validation-DO-NOT-DISTRIBUTE' } else { 'Dwnl-Setup' })
        $defines = @('/Qp', "/DPayloadRoot=$payload", "/DPackageRoot=$OutputRoot", "/DOutputName=$fileName", "/DExtensionId=$($identity.extensionId)", '/DAppVersion=0.1.0')
        if ($validation) { $defines += '/DValidationOnly=1' }
        $compileOutput = & $compiler @defines (Join-Path $root 'installer/dwnl.iss') 2>&1
        $compileCode = $LASTEXITCODE
        $compileOutput | Set-Content -LiteralPath (Join-Path $OutputRoot 'inno-compile.log') -Encoding utf8
        if ($compileCode -ne 0) { throw "ISCC compilation failed ($compileCode); see inno-compile.log." }
        $installer = Join-Path $OutputRoot ($fileName + '.exe')
        if (-not (Test-Path -LiteralPath $installer -PathType Leaf)) { throw 'ISCC did not produce the expected installer.' }
        $report.compiledInstaller = [ordered]@{ path=$installer; sha256=Get-DwnlSha256 $installer; sizeBytes=(Get-Item -LiteralPath $installer).Length; compilerVersion=$compilerMetadata.version; validationOnly=$validation; executed=$false }
        $report | ConvertTo-Json -Depth 40 | Set-Content -LiteralPath (Join-Path $OutputRoot 'package-report.json') -Encoding utf8
    }
    return $report
}

if ($MyInvocation.InvocationName -ne '.') {
    $ErrorActionPreference = 'Stop'
    if (-not $ProjectRoot) { $ProjectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..')) }
    if (-not $OutputRoot) { $OutputRoot = Join-Path $ProjectRoot 'dist/package' }
    Invoke-DwnlPackage -ProjectRoot $ProjectRoot -OutputRoot $OutputRoot -StageOnly:$StageOnly -ValidationOnly:$ValidationOnly | ConvertTo-Json -Depth 40
}
