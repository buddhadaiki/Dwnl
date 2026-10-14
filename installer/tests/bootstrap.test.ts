import {afterEach,expect,it} from 'vitest';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,cpSync,rmSync,realpathSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve,sep} from 'node:path';
import {createHash} from 'node:crypto';
import {spawnSync} from 'node:child_process';
const project=resolve(import.meta.dirname,'../..');
const roots:string[]=[];
const quote=(v:string)=>`'${v.replaceAll("'","''")}'`;
function run(code:string){return spawnSync('pwsh.exe',['-NoProfile','-NonInteractive','-EncodedCommand',Buffer.from(`$ErrorActionPreference='Stop'; . ${quote(join(project,'tools/bootstrap.ps1'))}; ${code}`,'utf16le').toString('base64')],{encoding:'utf8'});}
function fixture(){
 const root=mkdtempSync(join(tmpdir(),'dwnl-bootstrap-test-')); roots.push(root);
 cpSync(join(project,'release'),join(root,'release'),{recursive:true}); cpSync(join(project,'contracts'),join(root,'contracts'),{recursive:true});
 mkdirSync(join(root,'dist/helper'),{recursive:true});
 const pe=Buffer.alloc(256); pe.write('MZ');pe.writeUInt32LE(128,60);pe.write('PE\0\0',128,'ascii');pe.writeUInt16LE(0x8664,132);
 const sha256=createHash('sha256').update(pe).digest('hex');
 writeFileSync(join(root,'dist/helper/dwnl-host.exe'),pe);
 writeFileSync(join(root,'dist/helper/build.json'),JSON.stringify({name:'dwnl-host',version:'test',platform:'windows-x86_64',path:'dist/helper/dwnl-host.exe',sizeBytes:256,sha256}));
 return {root,pe,sha256};
}
afterEach(()=>{for(const root of roots.splice(0)){const absolute=realpathSync(root); if(!absolute.toLowerCase().startsWith(realpathSync(tmpdir()).toLowerCase()+sep)) throw Error('unsafe cleanup');rmSync(absolute,{recursive:true,force:true});}});
it('stages the helper and notices without embedding any supplier executable',()=>{
 const f=fixture(); const output=join(f.root,'dist/bootstrap');
 const result=run(`New-DwnlBootstrapStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(output)} | ConvertTo-Json -Depth 30 -Compress`);
 expect(result.status,result.stderr).toBe(0); const report=JSON.parse(result.stdout);
 expect(report.packageMode).toBe('supplier-acquisition');expect(report.completeCorrespondingSource).toBe(false);expect(report.redistributionReady).toBe(false);
 expect(report.payloadFiles.filter((p:string)=>p.endsWith('.exe'))).toEqual(['dwnl-host.exe']);
 expect(report.supplierFiles.filter((p:any)=>p.path.endsWith('.exe'))).toHaveLength(4);
 expect(readFileSync(join(output,'payload/dwnl-host.exe'))).toEqual(f.pe);
 const include=readFileSync(join(output,'supplier-acquisition.iss'),'utf8');
 expect(include).toContain('https://github.com/denoland/deno/releases/download/v2.9.7/');expect(include).toContain('RequireHash(');
 expect(include).toContain('66674953fe251b89f4d08c5f0e35e0728679bd67ab3d7d05c0562af101dd3e7a');
});
it('checks acquired bytes before selecting any archive member',()=>{
 const f=fixture(); const source=join(f.root,'supplier.exe'); writeFileSync(source,f.pe);
 const supplier={name:'fixture',version:'test',archiveType:'exe',sha256:f.sha256,sizeBytes:256,files:[{entry:'',path:'bin/fixture.exe',sha256:f.sha256,sizeBytes:256}]};
 const lock=join(f.root,'fixture.json');writeFileSync(lock,JSON.stringify(supplier));
 const command=`$s=Get-Content -Raw ${quote(lock)}|ConvertFrom-Json; Import-DwnlSupplierArtifact -Artifact ${quote(source)} -OutputRoot ${quote(join(f.root,'acquired'))} -Supplier $s`;
 expect(run(command).status).toBe(0); expect(readFileSync(join(f.root,'acquired/bin/fixture.exe'))).toEqual(f.pe);
 writeFileSync(source,Buffer.from('changed'));
 const failed=run(command.replace(quote(join(f.root,'acquired')),quote(join(f.root,'rejected'))));
 expect(failed.status).not.toBe(0);expect(failed.stderr).toMatch(/Supplier artifact hash\/size mismatch/);
});
it('fails if an official URL floats, inner hash is missing, or output already exists',()=>{
 const f=fixture();const lock=join(f.root,'release/suppliers.lock.json');const content=JSON.parse(readFileSync(lock,'utf8'));
 content.suppliers[0].url=content.suppliers[0].url.replace('2026.08.19','latest');writeFileSync(lock,JSON.stringify(content));
 let result=run(`New-DwnlBootstrapStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(join(f.root,'dist/bad'))}`);expect(result.status).not.toBe(0);expect(result.stderr).toMatch(/exact official supplier URL/);
 content.suppliers[0].url='https://github.com/yt-dlp/yt-dlp/releases/download/2026.08.19/yt-dlp.exe';content.suppliers[1].files[0].sha256='';writeFileSync(lock,JSON.stringify(content));
 result=run(`New-DwnlBootstrapStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(join(f.root,'dist/bad2'))}`);expect(result.status).not.toBe(0);expect(result.stderr).toMatch(/Supplier file pin/);
});
it('executes stage-only entrypoint without losing explicit output arguments',()=>{
 const f=fixture(); const output=join(f.root,'dist/cli-stage');
 const code=`$ErrorActionPreference='Stop'; & ${quote(join(project,'tools/bootstrap.ps1'))} -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(output)} -StageOnly`;
 const result=spawnSync('pwsh.exe',['-NoProfile','-NonInteractive','-EncodedCommand',Buffer.from(code,'utf16le').toString('base64')],{encoding:'utf8'});
 expect(result.status,result.stderr).toBe(0);
 expect(readFileSync(join(output,'package-report.json'),'utf8')).toContain('supplier-acquisition');
});
it('rejects a hash-valid archive containing traversal before extracting its selected member',()=>{
 const f=fixture();const archive=join(f.root,'hostile.zip'); const output=join(f.root,'rejected-zip');
 const result=run(`$zip=[IO.Compression.ZipFile]::Open(${quote(archive)},[IO.Compression.ZipArchiveMode]::Create); try {$zip.CreateEntry('../escape.exe')|Out-Null; $entry=$zip.CreateEntry('fixture.exe'); $stream=$entry.Open(); try {$stream.Write([IO.File]::ReadAllBytes(${quote(join(f.root,'dist/helper/dwnl-host.exe'))}))} finally {$stream.Dispose()}} finally {$zip.Dispose()}; $supplier=[pscustomobject]@{name='fixture';version='test';archiveType='zip';sha256=Get-DwnlSha256 ${quote(archive)};sizeBytes=(Get-Item ${quote(archive)}).Length;files=@([pscustomobject]@{entry='fixture.exe';path='bin/fixture.exe';sha256=${quote(f.sha256)};sizeBytes=256})}; Import-DwnlSupplierArtifact -Artifact ${quote(archive)} -OutputRoot ${quote(output)} -Supplier $supplier`);
 expect(result.status).not.toBe(0);expect(result.stderr).toMatch(/Unsafe or duplicate supplier archive entry/);
});
