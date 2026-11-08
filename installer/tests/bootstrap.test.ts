import {afterEach,expect,it} from 'vitest';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,cpSync,rmSync,realpathSync,readdirSync} from 'node:fs';
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
 // yt-dlp arrives as the official onedir ZIP; its runtime folder lands beside the executable.
 expect(include).toContain("DownloadPage.Add('https://github.com/yt-dlp/yt-dlp/releases/download/2026.08.19/yt-dlp_win.zip', 'yt-dlp.zip', '30b4c14aafab6082becff7881e41b76df46dc43ea7633479410a91e29da492bf');");
 expect(include).toContain("RequireHash(ExpandConstant('{app}\\bin\\yt-dlp.exe'), '25a7fd29381d2487ea8380b9fe1cff34ae5bb264bc733cfd430595bae52aa309');");
 expect(include).toContain("RequireHash(ExpandConstant('{tmp}\\supplier-yt-dlp\\_internal\\python310.dll')");
 expect(include).toContain("DeleteIfOwned(ExpandConstant('{app}\\bin\\_internal\\python310.dll')");
 const removals=include.slice(include.indexOf('procedure RemoveOwnedDirectories'));
 expect(removals.indexOf("'{app}\\bin\\_internal\\Cryptodome\\Cipher'")).toBeGreaterThan(-1);
 expect(removals.indexOf("'{app}\\bin\\_internal\\Cryptodome\\Cipher'")).toBeLessThan(removals.indexOf("'{app}\\bin\\_internal'"));
 expect(removals.indexOf("'{app}\\bin\\_internal'")).toBeLessThan(removals.indexOf("'{app}\\bin'"));
});
function desktopFixture(){
 const f=fixture();
 mkdirSync(join(f.root,'dist/desktop'),{recursive:true});
 for(const name of ['Dwnl.exe','dwnl-host.exe']){
  writeFileSync(join(f.root,'dist/desktop',name),f.pe);
  writeFileSync(join(f.root,'dist/desktop',name+'.json'),JSON.stringify({name,version:'test',platform:'windows-x86_64',path:'dist/desktop/'+name,sizeBytes:256,sha256:f.sha256}));
 }
 writeFileSync(join(f.root,'dist/desktop/module-licenses.json'),'[]');
 const identity=JSON.parse(readFileSync(join(f.root,'contracts/extension-identity.json'),'utf8'));
 const extension=join(f.root,'dist/extension-build');
 mkdirSync(join(extension,'src'),{recursive:true});
 writeFileSync(join(extension,'manifest.json'),JSON.stringify({manifest_version:3,name:'Dwnl',version:'test',key:identity.key}));
 writeFileSync(join(extension,'src/desktop-worker.js'),'export {};\n');
 writeFileSync(join(extension,'.dwnl-extension-build'),'Dwnl owned extension build v1\n');
 return {...f,extension,identity};
}
it('stages the Chrome extension beside the desktop app and removes its folders on uninstall',()=>{
 const f=desktopFixture(); const output=join(f.root,'dist/desktop-bootstrap');
 const result=run(`New-DwnlBootstrapStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(output)} -Desktop -ExtensionRoot ${quote(f.extension)} | ConvertTo-Json -Depth 30 -Compress`);
 expect(result.status,result.stderr).toBe(0); const report=JSON.parse(result.stdout);
 expect(report.payloadFiles).toEqual(expect.arrayContaining(['Dwnl.exe','extension/manifest.json','extension/src/desktop-worker.js']));
 // The build's ownership marker belongs to dist, not to the installation.
 expect(report.payloadFiles.some((p:string)=>p.includes('.dwnl-extension-build'))).toBe(false);
 expect(readFileSync(join(output,'payload/extension/src/desktop-worker.js'),'utf8')).toBe('export {};\n');
 const include=readFileSync(join(output,'supplier-acquisition.iss'),'utf8');
 expect(include).toContain("RequireHash(ExpandConstant('{app}\\extension\\src\\desktop-worker.js')");
 const removals=include.slice(include.indexOf('procedure RemoveOwnedDirectories'));
 expect(removals.indexOf("'{app}\\extension\\src'")).toBeGreaterThan(-1);
 expect(removals.indexOf("'{app}\\extension\\src'")).toBeLessThan(removals.indexOf("'{app}\\extension'"));
 expect(removals).toContain("'{app}\\bin'");expect(removals).toContain("'{app}\\licenses'");
});
it('refuses a desktop setup without the extension or with another extension identity',()=>{
 const f=desktopFixture();
 let result=run(`New-DwnlBootstrapStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(join(f.root,'dist/no-extension'))} -Desktop`);
 expect(result.status).not.toBe(0);expect(result.stderr).toMatch(/requires the built extension/);
 writeFileSync(join(f.extension,'manifest.json'),JSON.stringify({manifest_version:3,name:'Other',version:'test',key:'MIIB'}));
 result=run(`New-DwnlBootstrapStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(join(f.root,'dist/other-extension'))} -Desktop -ExtensionRoot ${quote(f.extension)}`);
 expect(result.status).not.toBe(0);expect(result.stderr).toMatch(/does not match the native host/);
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
 content.suppliers[0].url='https://github.com/yt-dlp/yt-dlp/releases/download/2026.08.19/yt-dlp_win.zip';content.suppliers[1].files[0].sha256='';writeFileSync(lock,JSON.stringify(content));
 result=run(`New-DwnlBootstrapStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(join(f.root,'dist/bad2'))}`);expect(result.status).not.toBe(0);expect(result.stderr).toMatch(/Supplier file pin/);
});
it('keeps the runtime folder to yt-dlp, under bin/_internal and named after its own archive members',()=>{
 const damages:((c:any)=>void)[]=[
  c=>{c.suppliers.find((s:any)=>s.name==='deno').files.push({entry:'_internal/x.dll',path:'bin/_internal/x.dll',sha256:'a'.repeat(64),sizeBytes:1});},
  c=>{c.suppliers[0].files[1].path='bin/_internal/other.pyd';},
  c=>{c.suppliers[0].files[1].entry='_internal/../escape.pyd';c.suppliers[0].files[1].path='bin/_internal/../escape.pyd';},
  c=>{c.suppliers[0].files[0].sizeBytes=0;},
 ];
 for(const [index,damage] of damages.entries()){
  const f=fixture();const lock=join(f.root,'release/suppliers.lock.json');const content=JSON.parse(readFileSync(lock,'utf8'));
  damage(content);writeFileSync(lock,JSON.stringify(content));
  const result=run(`New-DwnlBootstrapStage -ProjectRoot ${quote(f.root)} -OutputRoot ${quote(join(f.root,'dist/bad'))}`);
  expect(result.status,'damage '+index).not.toBe(0);expect(result.stderr).toMatch(/Supplier file pin/);
 }
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
it('asks a running Dwnl to quit before replacing or removing it, and reopens it after installing',()=>{
 const script=readFileSync(join(project,'installer/bootstrap.iss'),'utf8');
 expect(script).not.toContain('Close Dwnl jobs');
 expect(script).toMatch(/^Filename: "\{app\}.Dwnl\.exe"; WorkingDir: "\{app\}"; Description: "Abrir o Dwnl"; Flags: postinstall nowait skipifdoesntexist\r?$/m);
 expect(script).toContain("Params := '--quit';");
 expect(script).toContain("if Force then Params := Params + ' --force';");
 const prepare=script.slice(script.indexOf('function PrepareToInstall'),script.indexOf('procedure CurStepChanged'));
 expect(prepare.match(/CloseRunningDwnl\('atualizar'\)/g)).toHaveLength(2);
 expect(prepare).not.toContain('OwnedFilesInactive()');
 expect(script.slice(script.indexOf('function InitializeUninstall'))).toContain("Result := CloseRunningDwnl('desinstalar');");
 // A download in progress is closed only on the person's yes.
 const close=script.slice(script.indexOf('function CloseRunningDwnl'),script.indexOf('procedure RequireHash'));
 expect(close).toMatch(/Code := AskDwnlToQuit\(False\);\s*if Code = 3 then begin[\s\S]*MB_YESNO, \['Fechar o Dwnl', 'Agora não'\], 0, IDNO\) <> IDYES then Exit;\s*Code := AskDwnlToQuit\(True\);/);
 // What still runs is ended only on Fechar o Dwnl; silent runs cancel instead.
 expect(close).toMatch(/'Fechar o Dwnl'#13#10'[^']*',\s*'Tentar de novo'#13#10'[^']*',\s*'Cancelar'\], 0, IDCANCEL\);\s*if Choice = IDCANCEL then Exit;\s*if Choice = IDYES then begin[\s\S]*EndProcessesIn\(ExpandConstant\('\{app\}'\)\);\s*end;/);
 expect(script.match(/EndProcessesIn\(/g)).toHaveLength(1);
 expect(script).not.toMatch(/taskkill|TerminateProcess/i);
});
it('looks like Dwnl: Portuguese, light or dark as Windows, and the Dwnl images',()=>{
 const script=readFileSync(join(project,'installer/bootstrap.iss'),'utf8');
 expect(script).toMatch(/^WizardStyle=modern dynamic windows11\r?$/m);
 expect(script).toMatch(/^Name: "ptbr"; MessagesFile: "compiler:Languages\\BrazilianPortuguese\.isl"\r?$/m);
 const images: Record<string, number> = {};
 for (const [, directive, pattern] of script.matchAll(/^(SetupIconFile|WizardSmallImageFile|WizardImageFile|WizardImageFileDynamicDark)=assets\\(\S+)\r?$/gm)) {
  const matcher = new RegExp('^' + pattern.replaceAll('.', '\\.').replaceAll('*', '.*') + '$');
  images[directive] = readdirSync(join(project, 'installer/assets')).filter(name => matcher.test(name)).length;
 }
 expect(images).toEqual({ SetupIconFile: 1, WizardSmallImageFile: 7, WizardImageFile: 7, WizardImageFileDynamicDark: 7 });
});
