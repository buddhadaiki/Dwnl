import { beforeAll, afterAll, expect, it } from 'vitest';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, symlinkSync, rmSync, realpathSync, existsSync } from 'node:fs';
import { join, resolve, sep, basename } from 'node:path';
import { spawnSync } from 'node:child_process';

const project = resolve(import.meta.dirname, '../..');
const origin = 'chrome-extension://lphdonkmiecidfgjfplhdfnpphmdioeh/';
let root: string;
let results: Record<string, string>;
function marker(installRoot: string) {
  return `Dwnl private installation owner v1\nAppId={B01F7A32-74CA-4C9E-BDD2-635B305A5480}\nHostName=com.dwnl.host\nAllowedOrigin=${origin}\nInstallRoot=${installRoot}\n`;
}
function manifest(installRoot: string) {
  return `{\n  "name": "com.dwnl.host",\n  "description": "Dwnl private native helper",\n  "path": ${JSON.stringify(join(installRoot, 'dwnl-host.exe'))},\n  "type": "stdio",\n  "allowed_origins": ["${origin}"]\n}\n`;
}
beforeAll(() => {
  const temporary = join(project, '.tools/tmp');
  mkdirSync(temporary, { recursive: true });
  root = mkdtempSync(join(temporary, 'dwnl-installer-ownership-'));
  for (const name of ['arbitrary', 'marker-valid-資料', 'marker-invalid', 'marker-moved', 'marker-reparse', 'manifest-altered', 'registration-external', 'registration-non-string', 'legacy', 'legacy-invalid-marker', 'active', 'external']) {
    const installRoot = join(root, name); mkdirSync(installRoot);
    if (name.startsWith('marker-') && name !== 'marker-reparse' || name.startsWith('manifest-') || name.startsWith('registration-')) writeFileSync(join(installRoot, '.dwnl-install-owner'), marker(installRoot));
  }
  writeFileSync(join(root, 'marker-invalid/.dwnl-install-owner'), 'unrelated marker');
  writeFileSync(join(root, 'marker-moved/.dwnl-install-owner'), marker(join(root, 'marker-valid-資料')));
  symlinkSync(join(root, 'external'), join(root, 'marker-reparse/.dwnl-install-owner'), 'junction');
  symlinkSync(join(root, 'external'), join(root, 'root-reparse'), 'junction');
  writeFileSync(join(root, 'manifest-altered/native-host.json'), '{"name":"other.host"}');
  for (const name of ['legacy', 'legacy-invalid-marker']) writeFileSync(join(root, name, 'native-host.json'), manifest(join(root, name)));
  writeFileSync(join(root, 'legacy-invalid-marker/.dwnl-install-owner'), 'unrelated marker');
  writeFileSync(join(root, 'active/dwnl-host.exe'), 'active fixture');
  const compiler = join(project, '.tools/inno/compiler/ISCC.exe');
  const compile = spawnSync(compiler, ['/Q', `/DFixtureRoot=${root}`, '/DExtensionId=lphdonkmiecidfgjfplhdfnpphmdioeh', join(project, 'installer/tests/bootstrap-ownership-harness.iss')], { encoding: 'utf8', windowsHide: true, timeout: 30000 });
  expect(compile.status, compile.stdout + compile.stderr).toBe(0);
  const run = spawnSync(join(root, 'ownership-harness.exe'), ['/VERYSILENT', '/SUPPRESSMSGBOXES', '/SP-', '/NORESTART', `/LOG=${join(root, 'harness.log')}`], { encoding: 'utf8', windowsHide: true, timeout: 30000 });
  expect(run.error, `${run.stdout}\n${run.stderr}`).toBeUndefined();
  // Returning False from InitializeSetup aborts the harness before any install step.
  expect(existsSync(join(root, 'results.txt')), `Harness exit ${run.status}; ${existsSync(join(root, 'harness.log')) ? readFileSync(join(root, 'harness.log'), 'utf8') : 'no log'}`).toBe(true);
  results = Object.fromEntries(readFileSync(join(root, 'results.txt'), 'utf8').trim().split('\n').map(line => line.split('=')));
}, 60000);
afterAll(() => {
  if (!root) return;
  const actual = realpathSync(root);
  if (!actual.toLowerCase().startsWith(realpathSync(join(project, '.tools/tmp')).toLowerCase() + sep) || !basename(actual).startsWith('dwnl-installer-ownership-')) throw Error('Unsafe ownership fixture cleanup');
  rmSync(actual, { recursive: true, force: true });
});

it.each([
  ['fresh', '1'], ['arbitrary', '0'], ['marker-valid-資料', '1'], ['marker-invalid', '0'],
  ['marker-moved', '0'], ['marker-reparse', '0'], ['root-reparse', '0'], ['manifest-altered', '0'],
  ['registration-external', '0'], ['registration-non-string', '0'], ['legacy', '1'], ['legacy-invalid-marker', '0'],
  ['different-root', '0'], ['active-file', '0'], ['inactive-file', '1'], ['legacy-migrated', '1'],
  ['active-marker', '0'],
  ['prepare-fresh', '1'], ['remove-owned-manifest', '1'], ['reinstall-preserved-state', '1'],
  ['retry-failed-first-copy', '1'], ['preserve-replaced-manifest', '1'],
  ['compensate-new-manifest', '1'], ['retry-failed-registration', '1'],
  ['preserve-working-manifest', '1'], ['preserve-compensation-replacement', '1'],
])('ownership behavior: %s', (name, expected) => {
  expect(results[name]).toBe(expected);
});
it('preserves state, downloads and unknown files through owned payload removal', () => {
  const installRoot = join(root, 'lifecycle-資料');
  expect(readFileSync(join(installRoot, 'state/ledger.json'), 'utf8')).toBe('keep state bytes');
  expect(readFileSync(join(installRoot, 'unknown.txt'), 'utf8')).toBe('keep unknown bytes');
  expect(readFileSync(join(root, 'download.mp3'), 'utf8')).toBe('keep download bytes');
  expect(readFileSync(join(root, 'manifest-altered/native-host.json'), 'utf8')).toBe('{"name":"other.host"}');
  expect(readFileSync(join(root, 'legacy/native-host.json'), 'utf8')).toBe(manifest(join(root, 'legacy')));
  expect(existsSync(join(installRoot, 'dwnl-host.exe'))).toBe(false);
  expect(existsSync(join(installRoot, 'native-host.json'))).toBe(false);
});
it('writes a persistent marker bound to the actual Unicode install path', () => {
  const installRoot = join(root, 'lifecycle-資料');
  expect(readFileSync(join(installRoot, '.dwnl-install-owner'), 'utf8')).toBe(marker(installRoot));
});
