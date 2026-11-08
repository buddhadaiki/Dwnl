import { afterAll, beforeAll, expect, it } from 'vitest';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync } from 'node:fs';
import { basename, join, resolve, sep } from 'node:path';
import { spawn, spawnSync, type ChildProcess } from 'node:child_process';

// "Fechar o Dwnl" in the installer ends what still runs from the installation
// folder. A long ping stands in for Dwnl.exe and its tools.
const project = resolve(import.meta.dirname, '../..');
const ping = join(process.env.SystemRoot ?? 'C:\\Windows', 'System32', 'PING.EXE');
let root: string;
let results: Record<string, string>;
const running: Record<string, ChildProcess> = {};
const exited: Record<string, Promise<number | null>> = {};

function start(name: string, path: string) {
  mkdirSync(join(path, '..'), { recursive: true });
  copyFileSync(ping, path);
  const child = spawn(path, ['-n', '120', '127.0.0.1'], { windowsHide: true, stdio: 'ignore' });
  running[name] = child;
  exited[name] = new Promise(done => child.once('exit', code => done(code)));
  return new Promise<void>((ready, fail) => { child.once('spawn', ready); child.once('error', fail); });
}
function within(name: string, ms: number) {
  return Promise.race([exited[name].then(() => true), new Promise<boolean>(done => setTimeout(() => done(false), ms))]);
}

beforeAll(async () => {
  const temporary = join(project, '.tools/tmp');
  mkdirSync(temporary, { recursive: true });
  root = mkdtempSync(join(temporary, 'dwnl-installer-processes-'));
  await start('app', join(root, 'install-資料', 'Dwnl.exe'));
  await start('tool', join(root, 'install-資料', 'bin', 'yt-dlp.exe'));
  // Same name, and a folder whose name starts like the installation's.
  await start('sibling', join(root, 'install-資料-other', 'Dwnl.exe'));
  await start('outside', join(root, 'outside', 'Dwnl.exe'));
  const compile = spawnSync(join(project, '.tools/inno/compiler/ISCC.exe'), ['/Q', `/DFixtureRoot=${root}`, join(project, 'installer/tests/bootstrap-processes-harness.iss')], { encoding: 'utf8', windowsHide: true, timeout: 30000 });
  expect(compile.status, compile.stdout + compile.stderr).toBe(0);
  const log = join(root, 'harness.log');
  const run = spawnSync(join(root, 'processes-harness.exe'), ['/VERYSILENT', '/SUPPRESSMSGBOXES', '/SP-', '/NORESTART', `/LOG=${log}`], { encoding: 'utf8', windowsHide: true, timeout: 60000 });
  expect(run.error, `${run.stdout}\n${run.stderr}`).toBeUndefined();
  expect(existsSync(join(root, 'results.txt')), `Harness exit ${run.status}; ${existsSync(log) ? readFileSync(log, 'utf8') : 'no log'}`).toBe(true);
  results = Object.fromEntries(readFileSync(join(root, 'results.txt'), 'utf8').trim().split('\n').map(line => line.split('=')));
}, 90000);

afterAll(async () => {
  for (const child of Object.values(running)) if (child.exitCode === null && child.signalCode === null) child.kill();
  await Promise.all(Object.values(exited));
  if (!root) return;
  const actual = realpathSync(root);
  if (!actual.toLowerCase().startsWith(realpathSync(join(project, '.tools/tmp')).toLowerCase() + sep) || !basename(actual).startsWith('dwnl-installer-processes-')) throw Error('Unsafe process fixture cleanup');
  rmSync(actual, { recursive: true, force: true });
});

it('ends the app and its tools running from the installation folder', async () => {
  expect(results.ended).toBe('2');
  expect(await within('app', 5000)).toBe(true);
  expect(await within('tool', 5000)).toBe(true);
});

it('leaves a Dwnl running from any other folder alone', async () => {
  expect(await within('sibling', 1000)).toBe(false);
  expect(await within('outside', 1000)).toBe(false);
});
