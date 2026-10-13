import { readFile, writeFile, mkdir, readdir, copyFile, lstat, realpath, rm } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

const root = await realpath(path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..'));
const source = path.join(root, 'extension');
const dist = path.join(root, 'dist');
const output = path.join(dist, 'extension');
const markerName = '.dwnl-extension-build';
const marker = 'Dwnl owned extension build v1\n';
const identity = JSON.parse(await readFile(path.join(root, 'contracts/extension-identity.json'), 'utf8'));
const manifest = JSON.parse(await readFile(path.join(source, 'manifest.json'), 'utf8'));

if (manifest.manifest_version !== 3 || manifest.externally_connectable || manifest.content_scripts || manifest.host_permissions?.length) {
  throw new Error('Manifesto fora do escopo aprovado do protótipo.');
}
const permissions = new Set(manifest.permissions ?? []);
const required = ['activeTab', 'nativeMessaging', 'storage'];
if (required.some(p => !permissions.has(p)) || [...permissions].some(p => ![...required, 'downloads'].includes(p))) {
  throw new Error('Permissões do manifesto não correspondem ao desenho aprovado.');
}
manifest.key = identity.key;
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
await mkdir(output);
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
    if (rel === 'manifest.json') continue;
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
