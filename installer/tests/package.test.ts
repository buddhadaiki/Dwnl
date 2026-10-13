import { afterEach, describe, expect, it } from 'vitest';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, symlinkSync, realpathSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve, join, sep, basename } from 'node:path';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';

const project = resolve(import.meta.dirname, '../..');
const roots: string[] = [];
const quote = (value: string) => `'${value.replaceAll("'", "''")}'`;
function run(code: string) {
  const script = `$ErrorActionPreference='Stop'; . ${quote(join(project, 'tools/package.ps1'))}; ${code}`;
  return spawnSync('pwsh.exe', ['-NoProfile', '-NonInteractive', '-EncodedCommand', Buffer.from(script, 'utf16le').toString('base64')], { encoding: 'utf8' });
}
function json(code: string): any {
  const result = run(`$result = ${code}; $result | ConvertTo-Json -Depth 100 -Compress`);
  expect(result.status, result.stderr).toBe(0);
  return JSON.parse(result.stdout.trim());
}
function failure(code: string, message: RegExp) {
  const result = run(code);
  expect(result.status).not.toBe(0);
  expect(result.stderr).toMatch(message);
}
function fixture() {
  const root = mkdtempSync(join(tmpdir(), 'dwnl-package-test-'));
  roots.push(root);
  const pe = Buffer.alloc(256, 0);
  pe.write('MZ'); pe.writeUInt32LE(128, 60); pe.write('PE\0\0', 128, 'ascii'); pe.writeUInt16LE(0x8664, 132);
  // These are controlled headers for validation tests, never executable payloads.
  const sha256 = createHash('sha256').update(pe).digest('hex');
  const binaries = ['yt-dlp', 'deno', 'ffmpeg', 'ffprobe'].map(name => {
    const path = `input/${name}.exe`;
    mkdirSync(join(root, 'input'), { recursive: true });
    writeFileSync(join(root, path), pe);
    return { name, version: 'test-only-1', platform: 'windows-x86_64', path, sha256, sizeBytes: pe.length,
      license: 'TEST-ONLY', licenseFiles: ['licenses/TEST.txt'], sourceReferences: ['https://example.invalid/test-only-source'] };
  });
  mkdirSync(join(root, 'licenses'));
  writeFileSync(join(root, 'licenses/TEST.txt'), 'Test fixture license collateral. Never distribute.\n');
  writeFileSync(join(root, 'notices.md'), 'Controlled notices fixture.\n');
  const licenseHash = createHash('sha256').update(readFileSync(join(root, 'licenses/TEST.txt'))).digest('hex');
  const noticeHash = createHash('sha256').update(readFileSync(join(root, 'notices.md'))).digest('hex');
  const lock = { schemaVersion: 1, distributionScope: 'private-validation', completeCorrespondingSource: false,
    sourceBundleStatus: 'test-only incomplete source', tools: binaries, licenseFiles: [{ path: 'licenses/TEST.txt', sha256: licenseHash }],
    notices: { path: 'notices.md', sha256: noticeHash }, capabilities: { libmp3lame: true }, sourceArtifacts: [] };
  writeFileSync(join(root, 'lock.json'), JSON.stringify(lock));
  writeFileSync(join(root, 'host.exe'), pe);
  const host = { name: 'dwnl-host', version: 'test-only-1', platform: 'windows-x86_64', path: 'host.exe', sha256, sizeBytes: pe.length };
  writeFileSync(join(root, 'host.json'), JSON.stringify(host));
  const identity = JSON.parse(readFileSync(join(project, 'contracts/extension-identity.json'), 'utf8'));
  writeFileSync(join(root, 'identity.json'), JSON.stringify(identity));
  return { root, lock, host, identity, pe, load: `$lock=Get-Content -Raw ${quote(join(root, 'lock.json'))}|ConvertFrom-Json; $identity=Get-Content -Raw ${quote(join(root, 'identity.json'))}|ConvertFrom-Json; $hostMetadata=Get-Content -Raw ${quote(join(root, 'host.json'))}|ConvertFrom-Json;` };
}
afterEach(() => {
  const temporaryRoot = realpathSync(tmpdir()).toLowerCase() + sep;
  for (const root of roots.splice(0)) {
    const absolute = realpathSync(root);
    if (!absolute.toLowerCase().startsWith(temporaryRoot) || !/^dwnl-package-(test|external)-/.test(basename(absolute))) throw new Error('Test cleanup target escaped its named temporary directories');
    rmSync(absolute, { recursive: true, force: true });
  }
});

describe('package controlled-file validation', () => {
  it('stages only five pinned executables and records a key-derived stdio origin', () => {
    const f = fixture();
    const output = join(f.root, 'dist/package');
    const result = run(`${f.load} New-DwnlStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(output)} -Identity $identity -Lock $lock -HostMetadata $hostMetadata -ValidationOnly | ConvertTo-Json -Depth 100 -Compress`);
    expect(result.status, result.stderr).toBe(0);
    const report = JSON.parse(result.stdout);
    expect(report.redistributionReady).toBe(false);
    expect(report.payloadFiles.filter((x: string) => x.endsWith('.exe')).sort()).toEqual(['bin/deno.exe', 'bin/ffmpeg.exe', 'bin/ffprobe.exe', 'bin/yt-dlp.exe', 'dwnl-host.exe']);
    expect(readFileSync(join(output, 'payload/bin/ffmpeg.exe'))).toEqual(f.pe);
    const stagedLock = JSON.parse(readFileSync(join(output, 'payload/dependencies.lock.json'), 'utf8').replace(/^\uFEFF/, ''));
    expect(stagedLock.tools[0].licenseFiles).toEqual(['licenses/TEST.txt']);
    expect(readFileSync(join(output, 'payload', stagedLock.tools[0].licenseFiles[0]), 'utf8')).toContain('Test fixture license collateral');
    const manifest = json(`New-DwnlNativeManifest -Identity (Get-Content -Raw ${quote(join(f.root, 'identity.json'))}|ConvertFrom-Json) -InstallRoot ${quote('C:\\Users\\Fixture\\AppData\\Local\\Dwnl')}`);
    expect(manifest.type).toBe('stdio');
    expect(manifest.path).toBe('C:\\Users\\Fixture\\AppData\\Local\\Dwnl\\dwnl-host.exe');
    expect(manifest.allowed_origins).toEqual(['chrome-extension://lphdonkmiecidfgjfplhdfnpphmdioeh/']);
  });
  it('rejects staged executable bytes changed while being copied', () => {
    const f = fixture();
    const output = join(f.root, 'dist/copy-tamper');
    // Inject a real copied-file corruption; validation must inspect the bytes
    // delivered to the payload, independently of the original source check.
    failure(`${f.load} function Copy-Item { param([string]$LiteralPath,[string]$Destination) Microsoft.PowerShell.Management\\Copy-Item -LiteralPath $LiteralPath -Destination $Destination; if($Destination.EndsWith('ffmpeg.exe')){ [IO.File]::WriteAllBytes($Destination,[byte[]](1,2,3)) } }; New-DwnlStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(output)} -Identity $identity -Lock $lock -HostMetadata $hostMetadata -ValidationOnly`, /Staged payload hash mismatch/);
  });
  it('rejects missing executable, changed hash and non-x64 PE', () => {
    const f = fixture();
    rmSync(join(f.root, f.lock.tools[0].path));
    failure(`${f.load} Assert-DwnlBinary -ProjectRoot ${quote(f.root)} -Entry $lock.tools[0]`, /Missing payload/);
    writeFileSync(join(f.root, f.lock.tools[0].path), Buffer.from('tampered'));
    failure(`${f.load} Assert-DwnlBinary -ProjectRoot ${quote(f.root)} -Entry $lock.tools[0]`, /Hash mismatch/);
    const x86 = Buffer.from(f.pe); x86.writeUInt16LE(0x14c, 132);
    writeFileSync(join(f.root, f.lock.tools[0].path), x86);
    const newHash = createHash('sha256').update(x86).digest('hex');
    failure(`${f.load} $lock.tools[0].sha256=${quote(newHash)}; Assert-DwnlBinary -ProjectRoot ${quote(f.root)} -Entry $lock.tools[0]`, /Architecture/);
  });
  it('rejects a native origin that differs from the public key identity', () => {
    const f = fixture();
    failure(`${f.load} $identity.extensionId='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; Assert-DwnlIdentity -Identity $identity`, /key-derived/);
  });
  it('fails license/hash/source coverage and requires complete sources for a usable package', () => {
    const f = fixture();
    failure(`${f.load} Assert-DwnlCollateral -ProjectRoot ${quote(f.root)} -Lock $lock -RequireCompleteSource`, /complete corresponding source/);
    failure(`${f.load} $lock.tools[1].sourceReferences=@(); Assert-DwnlCollateral -ProjectRoot ${quote(f.root)} -Lock $lock`, /Source references/);
    failure(`${f.load} $lock.tools[2].licenseFiles=@('licenses/absent.txt'); Assert-DwnlCollateral -ProjectRoot ${quote(f.root)} -Lock $lock`, /License coverage/);
    writeFileSync(join(f.root, 'licenses/TEST.txt'), 'tampered');
    failure(`${f.load} Assert-DwnlCollateral -ProjectRoot ${quote(f.root)} -Lock $lock`, /License hash/);
  }, 15000);
  it('refuses missing MP3 capability and source paths outside its root', () => {
    const f = fixture();
    failure(`${f.load} $lock.capabilities.libmp3lame=$false; Assert-DwnlCollateral -ProjectRoot ${quote(f.root)} -Lock $lock`, /libmp3lame/);
    failure(`Resolve-DwnlPayloadPath -ProjectRoot ${quote(f.root)} -RelativePath '../other.exe'`, /Relative payload path/);
  });
  it('refuses reparse points instead of copying files from external directories', () => {
    const f = fixture();
    const outside = mkdtempSync(join(tmpdir(), 'dwnl-package-external-')); roots.push(outside);
    writeFileSync(join(outside, 'external.exe'), f.pe);
    symlinkSync(outside, join(f.root, 'junction'), 'junction');
    failure(`Resolve-DwnlPayloadPath -ProjectRoot ${quote(f.root)} -RelativePath 'junction/external.exe'`, /Reparse/);
  });
  it('preserves a replaced registration or manifest and never selects downloads for removal', () => {
    const f = fixture();
    const install = join(f.root, 'installed'); mkdirSync(install);
    const download = join(f.root, 'download.mp3'); writeFileSync(download, 'preserve media');
    const result = run(`${f.load} $manifest=New-DwnlNativeManifest -Identity $identity -InstallRoot ${quote(install)}; $manifest|ConvertTo-Json -Depth 10|Set-Content -LiteralPath ${quote(join(install, 'native-host.json'))}; Get-DwnlRemovalPlan -InstallRoot ${quote(install)} -Identity $identity -RegisteredManifest ${quote(join(f.root, 'other/native-host.json'))} | ConvertTo-Json -Depth 10 -Compress`);
    expect(result.status, result.stderr).toBe(0);
    const plan = JSON.parse(result.stdout);
    expect(plan.removeRegistration).toBe(false); expect(plan.removeManifest).toBe(false);
    const owned = json(`Get-DwnlRemovalPlan -InstallRoot ${quote(install)} -Identity (Get-Content -Raw ${quote(join(f.root, 'identity.json'))}|ConvertFrom-Json) -RegisteredManifest ${quote(join(install, 'native-host.json'))}`);
    expect(owned.removeRegistration).toBe(true); expect(owned.removeManifest).toBe(true);
    expect(owned.files).not.toContain(download);
    writeFileSync(join(install, 'native-host.json'), JSON.stringify({ name: 'com.other.host', type: 'stdio' }));
    const replaced = json(`Get-DwnlRemovalPlan -InstallRoot ${quote(install)} -Identity (Get-Content -Raw ${quote(join(f.root, 'identity.json'))}|ConvertFrom-Json) -RegisteredManifest ${quote(join(install, 'native-host.json'))}`);
    expect(replaced.removeManifest).toBe(false);
    expect(readFileSync(download, 'utf8')).toBe('preserve media');
  });
  it('refuses an active owned file without killing any process', () => {
    const f = fixture();
    failure(`$handle=[IO.File]::Open(${quote(join(f.root, 'host.exe'))},'Open','Read','None'); try { Assert-DwnlInactiveFiles -Paths @(${quote(join(f.root, 'host.exe'))}) } finally { $handle.Dispose() }`, /Active owned file/);
    expect(run(`Assert-DwnlInactiveFiles -Paths @(${quote(join(f.root, 'host.exe'))})`).status).toBe(0);
  });
});
