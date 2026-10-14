[CmdletBinding()]
param()

# A workspace-only acquisition/capability proof. This is not an installer and
# never edits the production dependency lock or registers a native host.
$ErrorActionPreference = 'Stop'
$project = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
. (Join-Path $PSScriptRoot 'package.ps1')
$relative = '.tools/supplier-proof/gyan-9.0.2'
$root = Resolve-DwnlPayloadPath $project $relative
if (Test-Path -LiteralPath $root) { throw 'Proof directory already exists; no overwrites or recursive cleanup.' }
New-Item -ItemType Directory -Path $root | Out-Null
$archive = Join-Path $root 'ffmpeg-9.0.2-essentials_build.zip'
$url = 'https://github.com/GyanD/codexffmpeg/releases/download/9.0.2/ffmpeg-9.0.2-essentials_build.zip'
$expected = '60f467265b1e312373dbcd92200c2618a74850f98d3d078e94296bb3fa2047ba'
Invoke-WebRequest -Uri $url -OutFile $archive -TimeoutSec 180
if ((Get-DwnlSha256 $archive) -cne $expected -or (Get-Item -LiteralPath $archive).Length -ne 114768076) { throw 'Supplier archive hash/size mismatch; nothing extracted or executed.' }

$bin = Join-Path $root 'bin'
New-Item -ItemType Directory -Path $bin | Out-Null
$zip = [IO.Compression.ZipFile]::OpenRead($archive)
$inventory = @()
try {
    foreach ($entry in $zip.Entries) {
        $name = $entry.FullName
        $mode = ($entry.ExternalAttributes -shr 16) -band 0xf000
        if ([IO.Path]::IsPathRooted($name) -or $name.Contains(':') -or $name.Contains('\') -or $name -match '(^|/)\.\.(/|$)' -or $mode -eq 0xa000 -or $entry.Length -gt 536870912) { throw "Unsafe archive entry: $name" }
    }
    foreach ($name in @('ffmpeg','ffprobe')) {
        $entryName = "ffmpeg-9.0.2-essentials_build/bin/$name.exe"
        $entries = @($zip.Entries | Where-Object { $_.FullName -ceq $entryName })
        if ($entries.Count -ne 1) { throw "Expected exactly one $entryName" }
        $dest = Join-Path $bin "$name.exe"
        $inputStream = $entries[0].Open()
        $outputStream = [IO.File]::Open($dest, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        try { $inputStream.CopyTo($outputStream) } finally { $outputStream.Dispose(); $inputStream.Dispose() }
        $item = [ordered]@{ name=$name; version='9.0.2'; platform='windows-x86_64'; path="$relative/bin/$name.exe"; sha256=(Get-DwnlSha256 $dest); sizeBytes=(Get-Item -LiteralPath $dest).Length }
        [void](Assert-DwnlBinary $project $item)
        $inventory += $item
    }
    foreach ($leaf in @('LICENSE','README.txt')) {
        $entries = @($zip.Entries | Where-Object { $_.FullName -ceq "ffmpeg-9.0.2-essentials_build/$leaf" })
        if ($entries.Count -ne 1) { throw "Missing supplier collateral: $leaf" }
        $dest = Join-Path $root $leaf
        $inputStream = $entries[0].Open(); $outputStream = [IO.File]::Open($dest,[IO.FileMode]::CreateNew)
        try { $inputStream.CopyTo($outputStream) } finally { $outputStream.Dispose(); $inputStream.Dispose() }
    }
} finally { $zip.Dispose() }

function Invoke-ProofTool {
    param([string]$Name, [string[]]$Arguments)
    $lines = @(& (Join-Path $bin "$Name.exe") @Arguments 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "$Name failed ($LASTEXITCODE): $($lines -join [Environment]::NewLine)" }
    return $lines -join [Environment]::NewLine
}
$versions = @{}
foreach ($name in @('ffmpeg','ffprobe')) {
    $version = Invoke-ProofTool $name @('-version')
    if ($version -notmatch "^$name version 9\.0\.2-essentials_build-www\.gyan\.dev") { throw "Unexpected $name version" }
    $versions[$name] = $version
}
if ((Invoke-ProofTool ffmpeg @('-hide_banner','-encoders')) -notmatch 'libmp3lame') { throw 'libmp3lame unavailable.' }
$video = Join-Path $root 'synthetic-video.mp4'; $audio = Join-Path $root 'synthetic-audio.m4a'; $merged = Join-Path $root 'synthetic-merged.mp4'
[void](Invoke-ProofTool ffmpeg @('-nostdin','-v','error','-f','lavfi','-i','testsrc2=size=160x90:rate=24','-t','2','-an','-c:v','libx264','-threads','1','-pix_fmt','yuv420p',$video))
[void](Invoke-ProofTool ffmpeg @('-nostdin','-v','error','-f','lavfi','-i','sine=frequency=880:sample_rate=44100','-t','2','-c:a','aac',$audio))
[void](Invoke-ProofTool ffmpeg @('-nostdin','-v','error','-i',$video,'-i',$audio,'-map','0:v:0','-map','1:a:0','-c','copy',$merged))
$mergedProbe = Invoke-ProofTool ffprobe @('-v','error','-show_streams','-show_format','-of','json',$merged) | ConvertFrom-Json
if (@($mergedProbe.streams | Where-Object { $_.codec_type -eq 'video' -and $_.codec_name -eq 'h264' }).Count -ne 1 -or @($mergedProbe.streams | Where-Object { $_.codec_type -eq 'audio' -and $_.codec_name -eq 'aac' }).Count -ne 1) { throw 'Stream-copy metadata mismatch.' }
$streamHashes = @{}
foreach ($stream in @('v','a')) {
    $original = if ($stream -eq 'v') { $video } else { $audio }
    $before = Invoke-ProofTool ffmpeg @('-nostdin','-v','error','-i',$original,'-map',"0:${stream}:0",'-c','copy','-f','hash','-hash','sha256','-')
    $after = Invoke-ProofTool ffmpeg @('-nostdin','-v','error','-i',$merged,'-map',"0:${stream}:0",'-c','copy','-f','hash','-hash','sha256','-')
    if ($before -cne $after) { throw "Stream-copy $stream packet hash changed." }
    $streamHashes[$stream] = $after
}
$mp3 = @()
foreach ($bitrate in @(128,192,320)) {
    $path = Join-Path $root "synthetic-$bitrate.mp3"
    [void](Invoke-ProofTool ffmpeg @('-nostdin','-v','error','-i',$audio,'-vn','-c:a','libmp3lame','-b:a',"${bitrate}k",$path))
    $probe = Invoke-ProofTool ffprobe @('-v','error','-show_streams','-show_format','-of','json',$path) | ConvertFrom-Json
    if (@($probe.streams).Count -ne 1 -or $probe.streams[0].codec_name -ne 'mp3' -or [int]$probe.streams[0].bit_rate -ne ($bitrate*1000) -or [double]$probe.format.duration -lt 2 -or [double]$probe.format.duration -gt 2.2) { throw "MP3 $bitrate metadata mismatch." }
    [void](Invoke-ProofTool ffmpeg @('-nostdin','-v','error','-i',$path,'-f','null','-'))
    $mp3 += [ordered]@{ bitrateKbps=$bitrate; codec=$probe.streams[0].codec_name; duration=$probe.format.duration; sha256=(Get-DwnlSha256 $path); fullDecode=$true }
}
[void](Invoke-ProofTool ffmpeg @('-nostdin','-v','error','-i',$merged,'-f','null','-'))
$report = [ordered]@{ schemaVersion=1; scope='workspace-acquisition-proof-only'; supplier='Gyan'; version='9.0.2'; url=$url; archiveSha256=$expected; sizeBytes=114768076; checksumSource='https://www.gyan.dev/ffmpeg/builds/packages/ffmpeg-9.0.2-essentials_build.zip.sha256'; tools=$inventory; versions=$versions; streamCopyPacketHashes=$streamHashes; mergedProbe=$mergedProbe; mergedFullDecode=$true; mp3=$mp3; productDependencyChanged=$false; installerExecuted=$false; chromeOrHostRegistrationChanged=$false; completeCorrespondingSource=$false; redistributionReady=$false }
[IO.File]::WriteAllText((Join-Path $root 'proof-report.json'),($report|ConvertTo-Json -Depth 20),[Text.UTF8Encoding]::new($false))
Write-Output "Supplier acquisition and local capability proof passed: $root"
