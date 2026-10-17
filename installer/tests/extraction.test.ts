import { afterEach, expect, it } from 'vitest';
import { createReadStream, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, statSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { basename, join, resolve, sep } from 'node:path';
import { spawnSync } from 'node:child_process';

const project = resolve(import.meta.dirname, '../..');
const temporary = join(project, '.tools/tmp');
const roots: string[] = [];
const lock = JSON.parse(readFileSync(join(project, 'release/suppliers.lock.json'), 'utf8'));
const fixtures = [
  { supplier: 'deno', archive: '.tools/media/downloads/deno-2.9.7-windows-x64.zip' },
  { supplier: 'gyan-ffmpeg', archive: '.tools/supplier-proof/gyan-9.0.2/ffmpeg-9.0.2-essentials_build.zip' },
];
async function sha256(path: string) {
  const digest = createHash('sha256');
  for await (const chunk of createReadStream(path)) digest.update(chunk);
  return digest.digest('hex');
}
afterEach(() => {
  for (const root of roots.splice(0)) {
    const actual = realpathSync(root);
    if (!actual.toLowerCase().startsWith(realpathSync(temporary).toLowerCase() + sep) || !basename(actual).startsWith('dwnl-installer-extraction-')) throw Error('Unsafe extraction fixture cleanup');
    rmSync(actual, { recursive: true, force: true });
  }
});

it.each(fixtures)('extracts the pinned official $supplier ZIP with the production Inno decoder', async ({ supplier, archive }) => {
  const entry = lock.suppliers.find((value: { name: string }) => value.name === supplier);
  const source = join(project, archive);
  const archiveSha256 = await sha256(source);
  expect(archiveSha256).toBe(entry.sha256);
  expect(statSync(source).size).toBe(entry.sizeBytes);
  mkdirSync(temporary, { recursive: true });
  const root = mkdtempSync(join(temporary, 'dwnl-installer-extraction-'));
  roots.push(root);
  const compile = spawnSync(join(project, '.tools/inno/compiler/ISCC.exe'), [
    '/Q', `/DFixtureRoot=${root}`, `/DArchivePath=${source}`, `/DArchiveHash=${entry.sha256}`,
    `/DSelectedEntry=${entry.files[0].entry.replaceAll('/', '\\')}`,
    join(project, 'installer/tests/bootstrap-extraction-harness.iss'),
  ], { encoding: 'utf8', windowsHide: true, timeout: 30000 });
  expect(compile.status, compile.stdout + compile.stderr).toBe(0);
  const log = join(root, 'harness.log');
  const run = spawnSync(join(root, 'extraction-harness.exe'), ['/VERYSILENT', '/SUPPRESSMSGBOXES', '/SP-', '/NORESTART', `/LOG=${log}`], { encoding: 'utf8', windowsHide: true, timeout: 60000 });
  expect(run.error, run.stdout + run.stderr).toBeUndefined();
  const resultPath = join(root, 'extraction-result.txt');
  const details = existsSync(log) ? readFileSync(log, 'utf8') : `Harness exit ${run.status}; no log`;
  expect(existsSync(resultPath), details).toBe(true);
  expect(readFileSync(resultPath, 'utf8'), details).toBe(`ok\n${entry.files[0].sha256}\n`);
  const readback = [];
  for (const file of entry.files) {
    const output = join(root, 'extracted', file.entry);
    const digest = await sha256(output);
    expect(digest, file.entry).toBe(file.sha256);
    expect(statSync(output).size, file.entry).toBe(file.sizeBytes);
    readback.push({ entry: file.entry, sha256: digest, sizeBytes: statSync(output).size });
  }
  console.info(JSON.stringify({ supplier, archiveSha256, readback, decoderLog: details.split('\n').filter(line => line.includes('Archive extraction mode:') || line.includes('Decoder 26.02')), installedOrRegistered: false }));
}, 90000);
