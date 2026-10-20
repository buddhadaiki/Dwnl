import { readFile, writeFile, mkdir, readdir, copyFile, lstat, realpath, rm } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';

const root = await realpath(path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..'));
const source = path.join(root, 'extension');
const dist = path.join(root, 'dist');
const markerName = '.dwnl-extension-build';
const marker = 'Dwnl owned extension build v1\n';
const identity = JSON.parse(await readFile(path.join(root, 'contracts/extension-identity.json'), 'utf8'));
const rawArgs = process.argv.slice(2);
const desktop = rawArgs.includes('--desktop');
const args = rawArgs.filter(arg => arg !== '--desktop');
const manifest = JSON.parse(await readFile(path.join(source, desktop ? 'manifest.desktop.json' : 'manifest.json'), 'utf8'));
const options = new Map();
for (let i = 0; i < args.length; i += 2) {
  if (!['--installer', '--output'].includes(args[i]) || !args[i + 1] || options.has(args[i])) throw new Error('Use --installer <verified bootstrap path> and/or --output <fresh dist directory>.');
  options.set(args[i], args[i + 1]);
}
const output = path.resolve(options.get('--output') ?? path.join(dist, 'extension'));
if (!output.startsWith(dist + path.sep) || output === path.join(dist, 'helper')) throw new Error('Extension output must be a child of dist.');
// Custom outputs are fresh staging directories; never replace a loaded extension.
if (options.has('--output')) {
  let cursor = output;
  while (cursor !== root) {
    const stat = await exists(cursor);
    if (stat?.isSymbolicLink()) throw new Error('Extension output contains a link/reparse.');
    if (cursor === output && stat) throw new Error('Custom extension output already exists.');
    cursor = path.dirname(cursor);
  }
}
let installer;
if(options.has('--installer')) {
  const input=await realpath(path.resolve(options.get('--installer')));
  if(!input.startsWith(dist+path.sep) || path.basename(input)!=='Dwnl-Setup.exe') throw new Error('Setup must be the owned dist bootstrap artifact.');
  const report=JSON.parse((await readFile(path.join(path.dirname(input),'package-report.json'),'utf8')).replace(/^\uFEFF/,''));
  const bytes=await readFile(input);
  const sha256=createHash('sha256').update(bytes).digest('hex');
  // A desktop setup is valid only for the explicitly selected desktop build.
  // Keep the legacy allowlist limited to its single native host executable.
  const expectedExecutables = desktop ? ['Dwnl.exe', 'dwnl-host.exe'] : ['dwnl-host.exe'];
  const payloadExecutables = Array.isArray(report.payloadFiles) && report.payloadFiles.every(file => typeof file === 'string')
    ? report.payloadFiles.filter(file => file.toLowerCase().endsWith('.exe')).sort() : [];
  const matchingMode = desktop ? report.desktop === true : report.desktop === undefined || report.desktop === false;
  if(report.packageMode!=='supplier-acquisition' || !matchingMode || report.installedOrRegistered!==false || !report.compiledInstaller || report.compiledInstaller.executed!==false || report.compiledInstaller.sha256!==sha256 || report.compiledInstaller.sizeBytes!==bytes.length || report.identity?.extensionId!==identity.extensionId || JSON.stringify(payloadExecutables)!==JSON.stringify(expectedExecutables)) throw new Error('Bootstrap provenance/hash does not match.');
  installer={input,sha256,sizeBytes:bytes.length};
}

if (manifest.manifest_version !== 3 || manifest.externally_connectable || manifest.content_scripts || manifest.host_permissions?.length) {
  throw new Error('Manifesto fora do escopo aprovado do protótipo.');
}
const permissions = new Set(manifest.permissions ?? []);
const required = ['activeTab', 'nativeMessaging', 'storage'];
if (required.some(p => !permissions.has(p)) || [...permissions].some(p => ![...required, 'downloads'].includes(p))) {
  throw new Error('Permissões do manifesto não correspondem ao desenho aprovado.');
}
manifest.key = identity.key;
if(installer) manifest.permissions=[...permissions,'downloads'].filter((value,index,array)=>array.indexOf(value)===index);
else manifest.permissions=[...permissions].filter(value=>value!=='downloads');
const checked = spawnSync(process.execPath, [path.join(root, 'node_modules/typescript/bin/tsc'), '--noEmit'], { cwd: root, stdio: 'inherit' });
if (checked.status !== 0) throw new Error('Typecheck falhou; extensão não gerada.');

async function exists(target) { try { return await lstat(target); } catch (e) { if (e.code === 'ENOENT') return null; throw e; } }
async function rejectLinks(target) {
  const stat = await lstat(target);
  if (stat.isSymbolicLink()) throw new Error(`Link/reparse não aceito no build: ${target}`);
  if (stat.isDirectory()) for (const entry of await readdir(target)) await rejectLinks(path.join(target, entry));
}
const distStat = await exists(dist);
if (distStat?.isSymbolicLink()) throw new Error('dist não pode ser link/reparse.');
await mkdir(dist, { recursive: true });
if (await exists(output)) {
  if (!output.startsWith(root + path.sep) || path.dirname(output) !== dist || path.basename(output) !== 'extension') throw new Error('Destino de limpeza inválido.');
  await rejectLinks(output);
  if (await readFile(path.join(output, markerName), 'utf8') !== marker) throw new Error('Build anterior sem marcador de ownership.');
  await rm(output, { recursive: true });
}
await mkdir(output, { recursive: true });
await writeFile(path.join(output, markerName), marker);
await writeFile(path.join(output, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');

let compiled = 0;
const productionSources = [];
async function buildDirectory(input, relative = '') {
  for (const entry of await readdir(input, { withFileTypes: true })) {
    if (['tests', 'dev', 'fixtures', 'node_modules'].includes(entry.name) || entry.name.startsWith('.')) continue;
    const rel = path.join(relative, entry.name);
    const from = path.join(input, entry.name);
    if (entry.isSymbolicLink()) throw new Error(`Link de entrada não aceito: ${rel}`);
    if (entry.isDirectory()) { await buildDirectory(from, rel); continue; }
    if (rel === 'manifest.json' || rel === 'manifest.desktop.json') continue;
    if (entry.name.endsWith('.d.ts')) continue;
    let destination = path.join(output, rel);
    if (entry.name.endsWith('.ts')) {
      destination = destination.slice(0, -3) + '.js';
      productionSources.push(from.replaceAll('\\', '/'));
      compiled++;
    } else if (/\.(html|css|svg|png|ico|json)$/i.test(entry.name)) {
      await mkdir(path.dirname(destination), { recursive: true });
      await copyFile(from, destination);
    }
  }
}
await buildDirectory(source);
if(installer) {
  const folder=path.join(output,'installer'); await mkdir(folder);
  const file=path.join(folder,'Dwnl-Setup.exe');await copyFile(installer.input,file);
  if(createHash('sha256').update(await readFile(file)).digest('hex')!==installer.sha256) throw new Error('Copied setup hash changed.');
  await writeFile(path.join(folder,'build-provenance.json'),JSON.stringify({schemaVersion:1,packageMode:'supplier-acquisition',sha256:installer.sha256,sizeBytes:installer.sizeBytes,executed:false},null,2)+'\n');
}
if (!compiled) throw new Error('Nenhum módulo de produção compilado.');
// TypeScript 7 exposes the supported compiler CLI; do not depend on the old
// transpileModule API. Emit only production sources after the full typecheck.
const buildConfig = path.join(root, '.tools', 'extension-build.tsconfig.json');
await mkdir(path.dirname(buildConfig), { recursive: true });
await writeFile(buildConfig, JSON.stringify({
  extends: path.join(root, 'tsconfig.json'),
  compilerOptions: { noEmit: false, rootDir: source, outDir: output, sourceMap: false, declaration: false },
  files: productionSources,
  include: [],
  exclude: [],
}, null, 2));
const emitted = spawnSync(process.execPath, [path.join(root, 'node_modules/typescript/bin/tsc'), '--project', buildConfig], { cwd: root, stdio: 'inherit' });
if (emitted.status !== 0) throw new Error('Compilação dos módulos de produção falhou.');
if (!manifest.background?.service_worker || manifest.background.type !== 'module') throw new Error('Worker MV3 deve ser um módulo local.');
for (const rel of [manifest.background.service_worker, manifest.action?.default_popup]) {
  if (!rel || path.isAbsolute(rel) || rel.includes('..') || !await exists(path.join(output, rel))) throw new Error(`Entrada local do manifesto ausente: ${rel}`);
}
console.log(`Extensão gerada em ${output} (${compiled} módulos; ID ${identity.extensionId}).`);
