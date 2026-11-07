import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {Window} from 'happy-dom';
import {defaultChoice, qualityPixels, progressPercent, appendChoices, partitionTasks, isVideoURL, pollDelay, capability, choiceLabel, qualityName, newlyCompleted, newlyFinished, listLayout, clock, showExtensionCard, retryLabel} from './model.mjs';

test('quality selection treats a portrait Short as 1080p, without selecting 4K', () => {
  const choices = [
    {optionId:'4k',width:2160,height:3840,hasAudio:true},
    {optionId:'portrait',width:1080,height:1920,hasAudio:true},
    {optionId:'720',width:1280,height:720,hasAudio:true},
  ];
  assert.equal(defaultChoice(choices),'portrait');
  assert.equal(qualityPixels(choices[1]),1080);
  assert.match(choiceLabel(choices[1]),/^1080p/);
});

test('default choice is deterministic and prefers audio at equal resolution', () => {
  assert.equal(defaultChoice([{optionId:'silent',height:1080,hasAudio:false},{optionId:'audio',height:1080,hasAudio:true}]),'audio');
  assert.equal(defaultChoice([{optionId:'2160',height:2160},{optionId:'1440',height:1440}]),'1440');
  assert.equal(defaultChoice([]),'');
});

test('progress is bounded; unknown progress stays indeterminate', () => {
  assert.equal(progressPercent({percent:400}),100);
  assert.equal(progressPercent({percent:-5}),0);
  assert.equal(progressPercent({completedBytes:20,totalBytes:80}),25);
  assert.equal(progressPercent({processedSeconds:30,durationSeconds:60}),50);
  assert.equal(progressPercent({}),null);
  assert.equal(progressPercent({percent:NaN}),null);
});

test('pagination keeps selected option identity and omits duplicates', () => {
  assert.deepEqual(appendChoices([{optionId:'a'}],[{optionId:'a'},{optionId:'b'},{optionId:'b'}]),[{optionId:'a'},{optionId:'b'}]);
});

test('interrupted tasks remain actionable in queue; history is newest first', () => {
  const {queue,history} = partitionTasks([{id:'a',state:'completed'},{id:'b',state:'interrupted'},{id:'c',state:'failed'}]);
  assert.deepEqual(queue.map(t=>t.id),['b']);
  assert.deepEqual(history.map(t=>t.id),['c','a']);
});

test('links reject credential-bearing and script URLs', () => {
  assert.equal(isVideoURL('https://youtube.com/shorts/123'),true);
  assert.equal(isVideoURL('javascript:alert(1)'),false);
  assert.equal(isVideoURL('https://user:secret@example.com/a'),false);
});

test('polling slows when idle or disconnected', () => {
  assert.equal(pollDelay({readiness:'ready',tasks:[]}),8000);
  assert.equal(pollDelay({readiness:'ready',tasks:[{state:'downloading'}]}),1000);
  assert.equal(pollDelay(null,10),30000);
  // While the user follows the Chrome steps, the connection shows up quickly.
  assert.equal(pollDelay({readiness:'ready',tasks:[]},0,true),2000);
  assert.equal(pollDelay({readiness:'ready',tasks:[{state:'downloading'}]},0,true),1000);
  // Preparing ends within seconds, so Baixar unlocks without a visible wait.
  assert.equal(pollDelay({readiness:'preparing',tasks:[]}),250);
});

test('capabilities are provisional while the app prepares', () => {
  const none = {video:false, mp3:false, networkAccess:false};
  assert.equal(capability({readiness:'preparing',capabilities:none},'video'),true);
  assert.equal(capability({readiness:'ready',capabilities:none},'video'),false);
  assert.equal(capability({readiness:'degraded',capabilities:{...none, mp3:true}},'mp3'),true);
  assert.equal(capability({readiness:'degraded',capabilities:none},'networkAccess'),false);
  assert.equal(capability(null,'video'),false);
});

test('the Chrome card appears only for an installed, unconnected, undismissed extension', () => {
  const extension = {folder:'C:\\Dwnl\\extension',connected:false};
  assert.equal(showExtensionCard({extension},false),true);
  assert.equal(showExtensionCard({extension},true),false);
  assert.equal(showExtensionCard({extension:{...extension,connected:true}},false),false);
  assert.equal(showExtensionCard({},false),false);
  assert.equal(showExtensionCard(null,false),false);
});

test('quality labels name the resolution and size, never codecs or containers', () => {
  assert.equal(choiceLabel({width:3840,height:2160,hasAudio:true,videoCodec:'av01.0.13M.08',container:'mp4'}),'4K');
  assert.equal(choiceLabel({width:1920,height:1080,hasAudio:true,sizeBytes:268435456}),'1080p Full HD · 256 MB');
  assert.equal(choiceLabel({width:1280,height:720,hasAudio:true,sizeBytes:1048576,sizeEstimated:true}),'720p HD · ≈ 1 MB');
  assert.equal(choiceLabel({width:576,height:1024,hasAudio:false}),'576p · sem som');
  assert.equal(qualityName({}),'Qualidade padrão');
  // Named like the engine groups them: a widescreen crop and 16:10 are 1080p.
  assert.equal(qualityName({width:1920,height:800}),'1080p Full HD');
  assert.equal(qualityName({width:1920,height:1200}),'1080p Full HD');
  assert.equal(qualityName({height:720}),'720p HD');
});

test('durations read like a player clock', () => {
  assert.equal(clock(634.5),'10:34');
  assert.equal(clock(5),'0:05');
  assert.equal(clock(3725),'1:02:05');
  assert.equal(clock(undefined),'');
});

test('only tasks seen unfinished count as newly completed', () => {
  const seen = new Map([['a','downloading'],['b','completed'],['c','saving']]);
  const tasks = [{id:'a',state:'completed'},{id:'b',state:'completed'},{id:'c',state:'failed'},{id:'d',state:'completed'}];
  assert.deepEqual(newlyCompleted(seen,tasks).map(t=>t.id),['a']);
});

test('a task that ends in any way gets a new icon; only a completed one gets a toast', () => {
  const seen = new Map([['a','downloading'],['b','saving'],['c','queued'],['d','completed']]);
  const tasks = [{id:'a',state:'completed'},{id:'b',state:'failed'},{id:'c',state:'downloading'},{id:'d',state:'completed'},{id:'e',state:'canceled'}];
  assert.deepEqual(newlyFinished(seen,tasks).map(t=>t.id),['a','b']);
  assert.deepEqual(newlyCompleted(seen,tasks).map(t=>t.id),['a']);
});

test('the list layout changes when rows arrive, leave or change state, not with progress', () => {
  const running = {id:'a',state:'downloading',progress:{completedBytes:10}};
  const base = {tasks:[{id:'old',state:'completed'},running]};
  const layout = listLayout(base,'queue');
  assert.equal(listLayout({tasks:[base.tasks[0],{...running,progress:{completedBytes:99}}]},'queue'),layout);
  assert.notEqual(listLayout({tasks:[...base.tasks,{id:'b',state:'queued'}]},'queue'),layout);
  assert.notEqual(listLayout({tasks:[base.tasks[0],{...running,state:'failed',error:'x'}]},'queue'),layout);
  assert.notEqual(listLayout({tasks:[running]},'queue'),layout);
  assert.notEqual(listLayout({...base,paused:true},'queue'),layout);
  assert.notEqual(listLayout(base,'history'),layout);
  // History shows finished rows only, so a queued arrival leaves it in place.
  assert.equal(listLayout({tasks:[...base.tasks,{id:'b',state:'queued'}]},'history'),listLayout(base,'history'));
});

test('desktop uses native state, explicit quality, guards double clicks and escapes titles', async () => {
  const window = new Window({url:'http://localhost/',settings:{disableJavaScriptFileLoading:true,disableJavaScriptEvaluation:true}});
  window.document.write(await readFile(new URL('./index.html',import.meta.url),'utf8'));
  globalThis.window = window;
  globalThis.document = window.document;
  const snapshot = {version:'test',readiness:'ready',capabilities:{video:true,mp3:true,networkAccess:true},dependencies:[],destination:{destinationId:'dest',label:'Downloads'},paused:false,tasks:[]};
  const calls = [];
  let completeEnqueue;
  const analysis = {analysisId:'analysis',sourceUrl:'https://youtube.com/shorts/123',items:[{itemId:'item',title:'<img src=x onerror=alert(1)>',hasAudio:true,videoChoices:[{optionId:'4k',width:2160,height:3840,hasAudio:true},{optionId:'short',width:1080,height:1920,hasAudio:true}],formatsCursor:'page2'}]};
  window.go = {main:{App:{Call:async(method,serialized) => {
    const payload = JSON.parse(serialized);
    calls.push({method,payload});
    if (method === 'snapshot') return snapshot;
    if (method === 'analyze') return analysis;
    if (method === 'listFormats') return {choices:[{optionId:'720',width:1280,height:720,hasAudio:true}]};
    if (method === 'enqueue') return new Promise(resolve => { completeEnqueue=resolve; });
    throw new Error('Unexpected method '+method);
  }}}};
  const settle = async () => { for (let i=0;i<5;i++) await new Promise(resolve=>setTimeout(resolve,0)); };
  try {
    await import('./app.js?test='+Date.now());
    await settle();
    const $ = id => window.document.getElementById(id);
    assert.equal($('analyze-button').disabled,false);
    $('source-url').value='https://youtube.com/shorts/123';
    $('analyze-form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));
    await settle();
    assert.equal($('quality-select').value,'short');
    assert.equal($('media-title').textContent,'<img src=x onerror=alert(1)>');
    assert.equal(window.document.querySelector('img'),null);
    $('more-formats').click();
    await settle();
    assert.equal($('quality-select').value,'short');
    $('enqueue-button').click();
    $('enqueue-button').click();
    await settle();
    assert.equal(calls.filter(c=>c.method==='enqueue').length,1);
    const queued = calls.find(c=>c.method==='enqueue').payload;
    assert.equal(queued.videoOptionId,'short');
    assert.equal(queued.destinationId,'dest');
    assert.match(queued.requestId,/^[a-f0-9-]{36}$/);
    snapshot.tasks = [{id:'task1',title:'Safe title',url:analysis.sourceUrl,state:'queued',mode:'video'}];
    snapshot.legacyHistory = [{hostSessionId:'old',jobId:'old-job',state:'completed',completedFile:{path:'C:/Downloads/old-video.mp4',format:'mp4',sizeBytes:1234}}];
    completeEnqueue({id:'task1'});
    await settle();
    assert.equal($('selection').hidden,true);
    assert.equal($('queue-count').textContent,'1');
    assert.match($('task-list').textContent,/Safe title/);
    snapshot.tasks = [{id:'task1',title:'Safe title',url:analysis.sourceUrl,state:'completed',mode:'video',completedFile:{path:'C:/Downloads/Safe title.mp4',format:'mp4',sizeBytes:2048}}];
    window.document.dispatchEvent(new window.Event('visibilitychange'));
    await settle();
    // Finished downloads stay in the Downloads view, with the folder shortcut.
    assert.equal($('task-list').children.length,0);
    assert.equal($('recent-section').hidden,false);
    assert.ok($('recent-list').querySelector('button[data-action="openFolder"][data-id="task1"]'));
    assert.equal($('toast').hidden,false);
    assert.equal($('toast-title').textContent,'Safe title');
    $('nav-history').click();
    assert.equal($('composer').hidden,true);
    assert.equal($('empty-state').hidden,true);
    assert.equal($('legacy-section').hidden,false);
    assert.match($('legacy-list').textContent,/old-video.mp4/);
    assert.equal($('legacy-list').querySelectorAll('button').length,0);
    assert.equal($('nav-history').getAttribute('aria-current'),'page');
  } finally {
    window.dispatchEvent(new window.Event('beforeunload'));
    await window.happyDOM.abort();
    delete globalThis.document; delete globalThis.window;
  }
});

test('late analysis results do not replace a link edited while analysis was running', async () => {
  const window = new Window({url:'http://localhost/',settings:{disableJavaScriptFileLoading:true,disableJavaScriptEvaluation:true}});
  window.document.write(await readFile(new URL('./index.html',import.meta.url),'utf8'));
  globalThis.window = window; globalThis.document = window.document;
  let finishAnalysis;
  window.go={main:{App:{Call:async(method) => {
    if(method==='snapshot')return {readiness:'ready',capabilities:{video:true,mp3:true,networkAccess:true},destination:{destinationId:'d'},tasks:[]};
    if(method==='analyze')return new Promise(resolve=>{finishAnalysis=resolve;});
  }}}};
  const settle=async()=>{for(let i=0;i<4;i++)await new Promise(resolve=>setTimeout(resolve,0));};
  try {
    await import('./app.js?stale='+Date.now()); await settle();
    const $=id=>window.document.getElementById(id);
    $('source-url').value='https://youtube.com/shorts/first';
    $('analyze-form').dispatchEvent(new window.Event('submit',{cancelable:true})); await settle();
    $('source-url').value='https://youtube.com/shorts/second';
    $('source-url').dispatchEvent(new window.Event('input'));
    finishAnalysis({analysisId:'old',sourceUrl:'https://youtube.com/shorts/first',items:[{itemId:'i',title:'Old result',videoChoices:[]}]});
    await settle();
    assert.equal($('selection').hidden,true);
    assert.equal($('source-url').value,'https://youtube.com/shorts/second');
    assert.equal($('analyze-button').disabled,false);
  } finally {
    window.dispatchEvent(new window.Event('beforeunload')); await window.happyDOM.abort();
    delete globalThis.document;delete globalThis.window;
  }
});

test('the Chrome steps open the extensions page and notice the connection', async () => {
  const window = new Window({url:'http://localhost/',settings:{disableJavaScriptFileLoading:true,disableJavaScriptEvaluation:true}});
  window.document.write(await readFile(new URL('./index.html',import.meta.url),'utf8'));
  globalThis.window = window; globalThis.document = window.document;
  const folder = 'C:\\Users\\Ana\\AppData\\Local\\Dwnl\\extension';
  const snapshot = {readiness:'ready',capabilities:{video:true,mp3:true,networkAccess:true},destination:{destinationId:'d'},tasks:[],extension:{folder,connected:false}};
  const opened = []; let copied = '';
  window.go={main:{App:{
    Call:async method => { if(method==='snapshot')return snapshot; throw new Error('Unexpected method '+method); },
    OpenChrome:async () => { opened.push('chrome'); },
    OpenExtensionFolder:async () => { opened.push('folder'); },
  }}};
  window.runtime={ClipboardSetText:async text => { copied = text; return true; }};
  const settle=async()=>{for(let i=0;i<4;i++)await new Promise(resolve=>setTimeout(resolve,0));};
  try {
    await import('./app.js?extension='+Date.now()); await settle();
    const $=id=>window.document.getElementById(id);
    assert.equal($('extension-card').hidden,false);
    $('extension-connect').click(); await settle();
    assert.equal($('extension-dialog').open,true);
    assert.equal($('extension-folder').textContent,folder);
    $('extension-open-chrome').click(); await settle();
    $('extension-open-folder').click(); await settle();
    assert.deepEqual(opened,['chrome','folder']);
    // Chrome ignores chrome:// on its command line, so the address is copied instead.
    $('extension-copy-address').click(); await settle();
    assert.equal(copied,'chrome://extensions');
    $('extension-copy').click(); await settle();
    assert.equal(copied,folder);
    assert.equal($('extension-copy').textContent,'Copiado');
    assert.equal($('extension-copy-address').textContent,'Copiar');
    assert.match($('extension-wait').textContent,/Aguardando/);
    snapshot.extension = {folder,connected:true};
    window.document.dispatchEvent(new window.Event('visibilitychange')); await settle();
    assert.match($('extension-wait').textContent,/conectada/);
    assert.equal($('extension-card').hidden,true);
    assert.match($('extension-setting-status').textContent,/Conectada/);
  } finally {
    window.dispatchEvent(new window.Event('beforeunload')); await window.happyDOM.abort();
    delete globalThis.document;delete globalThis.window;
  }
});

test('dismissing the Chrome card keeps it closed on the next start', async () => {
  const window = new Window({url:'http://localhost/',settings:{disableJavaScriptFileLoading:true,disableJavaScriptEvaluation:true}});
  window.document.write(await readFile(new URL('./index.html',import.meta.url),'utf8'));
  globalThis.window = window; globalThis.document = window.document;
  window.go={main:{App:{Call:async method => {
    if(method==='snapshot')return {readiness:'ready',capabilities:{video:true,mp3:true,networkAccess:true},destination:{destinationId:'d'},tasks:[],extension:{folder:'C:\\Dwnl\\extension',connected:false}};
  }}}};
  const settle=async()=>{for(let i=0;i<4;i++)await new Promise(resolve=>setTimeout(resolve,0));};
  try {
    await import('./app.js?dismiss='+Date.now()); await settle();
    const $=id=>window.document.getElementById(id);
    $('extension-dismiss').click(); await settle();
    assert.equal($('extension-card').hidden,true);
    assert.equal(window.localStorage.getItem('dwnl.extensionCardDismissed'),'1');
    // Settings still offer the steps, so the card is never the only way in.
    assert.equal($('extension-setting').hidden,false);
  } finally {
    window.dispatchEvent(new window.Event('beforeunload')); await window.happyDOM.abort();
    delete globalThis.document;delete globalThis.window;
  }
});

test('history rows describe their buttons, confirm removal and rename the video', async () => {
  const window = new Window({url:'http://localhost/',settings:{disableJavaScriptFileLoading:true,disableJavaScriptEvaluation:true}});
  window.document.write(await readFile(new URL('./index.html',import.meta.url),'utf8'));
  globalThis.window = window; globalThis.document = window.document;
  const done = {id:'done',title:'Aula 1',url:'https://youtube.com/watch?v=1',state:'completed',mode:'video',completedFile:{path:'C:\\Downloads\\Aula 1.mp4',format:'mp4',sizeBytes:2048}};
  const failed = {id:'failed',title:'Falhou',url:'https://youtube.com/watch?v=2',state:'failed',mode:'video',error:'Sem rede'};
  const snapshot = {readiness:'ready',capabilities:{video:true,mp3:true,networkAccess:true},destination:{destinationId:'d'},tasks:[failed,done]};
  const calls = [];
  window.go={main:{App:{Call:async(method,serialized) => {
    const payload = JSON.parse(serialized);
    if (method === 'snapshot') return snapshot;
    calls.push({method,payload});
    if (method === 'renameTask' && payload.title === 'Repetido') throw new Error('Já existe um arquivo com esse nome na pasta. Escolha outro nome.');
    if (method === 'renameTask') { done.title = payload.title; return done; }
    if (method === 'archiveHistory') { snapshot.tasks = snapshot.tasks.filter(task => task.id !== payload.id); return {archived:1}; }
    throw new Error('Unexpected method '+method);
  }}}};
  const settle=async()=>{for(let i=0;i<5;i++)await new Promise(resolve=>setTimeout(resolve,0));};
  try {
    await import('./app.js?history='+Date.now()); await settle();
    const $=id=>window.document.getElementById(id);
    $('nav-history').click();
    const row = id => $('task-list').querySelector('article[data-id="'+id+'"]');
    const buttons = id => [...row(id).querySelectorAll('.task-actions button')];
    // Every icon names its action for the hover tip and for screen readers.
    assert.deepEqual(buttons('done').map(b => [b.dataset.action, b.dataset.tip, b.getAttribute('aria-label')]), [
      ['openFile','Abrir o vídeo','Abrir o vídeo'],
      ['openFolder','Mostrar na pasta','Mostrar na pasta'],
      ['rename','Renomear o vídeo','Renomear o vídeo'],
      ['remove','Apagar do histórico','Apagar do histórico'],
    ]);
    assert.deepEqual(buttons('failed').map(b => b.dataset.action), ['retry','remove']);

    buttons('done')[3].click(); await settle();
    assert.equal($('remove-dialog').open,true);
    assert.match($('remove-description').textContent,/Aula 1/);
    assert.match($('remove-description').textContent,/continua na pasta/);
    assert.equal($('remove-confirm').textContent,'Apagar do histórico');
    assert.equal($('remove-cancel').textContent,'Cancelar');
    $('remove-cancel').click(); await settle();
    assert.equal($('remove-dialog').open,false);
    assert.equal(calls.length,0);

    buttons('done')[2].click(); await settle();
    assert.equal($('rename-dialog').open,true);
    assert.equal($('rename-input').value,'Aula 1');
    $('rename-input').value = 'Repetido';
    $('rename-form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true})); await settle();
    assert.equal($('rename-dialog').open,true);
    assert.match($('rename-error').textContent,/Já existe/);
    $('rename-input').value = '  Aula 1 revisada  ';
    $('rename-form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true})); await settle();
    assert.deepEqual(calls.at(-1),{method:'renameTask',payload:{id:'done',title:'Aula 1 revisada'}});
    assert.equal($('rename-dialog').open,false);
    assert.match(row('done').textContent,/Aula 1 revisada/);

    buttons('failed')[1].click(); await settle();
    assert.doesNotMatch($('remove-description').textContent,/continua na pasta/);
    $('remove-confirm').click(); await settle();
    assert.deepEqual(calls.at(-1),{method:'archiveHistory',payload:{id:'failed'}});
    assert.equal($('remove-dialog').open,false);
    assert.equal(row('failed'),null);
  } finally {
    window.dispatchEvent(new window.Event('beforeunload')); await window.happyDOM.abort();
    delete globalThis.document;delete globalThis.window;
  }
});

test('only failed or canceled downloads with a link offer to download again', () => {
  const url = 'https://youtube.com/watch?v=1';
  assert.equal(retryLabel({state:'failed',url}),'Tentar de novo');
  assert.equal(retryLabel({state:'canceled',url}),'Baixar de novo');
  for (const state of ['completed','queued','downloading','interrupted']) assert.equal(retryLabel({state,url}),'');
  assert.equal(retryLabel({state:'failed',url:''}),'');
  assert.equal(retryLabel({state:'failed',url:'file:///C:/x.mp4'}),'');
  assert.equal(retryLabel(undefined),'');
});

test('trying a failed MP3 again brings its link back and keeps the format', async () => {
  const window = new Window({url:'http://localhost/',settings:{disableJavaScriptFileLoading:true,disableJavaScriptEvaluation:true}});
  window.document.write(await readFile(new URL('./index.html',import.meta.url),'utf8'));
  globalThis.window = window; globalThis.document = window.document;
  const failed = {id:'failed',title:'Podcast',url:'https://youtube.com/watch?v=pod',state:'failed',mode:'mp3',bitrateKbps:320,error:'Sem rede'};
  const canceled = {id:'canceled',title:'Clipe',url:'https://x.com/a/status/1',state:'canceled',mode:'video'};
  const snapshot = {readiness:'ready',capabilities:{video:true,mp3:true,networkAccess:true},destination:{destinationId:'d'},tasks:[canceled,failed]};
  const analyzed = [];
  window.go={main:{App:{Call:async(method,serialized) => {
    if (method === 'snapshot') return snapshot;
    if (method === 'analyze') { const {url} = JSON.parse(serialized); analyzed.push(url); return {analysisId:'a',sourceUrl:url,items:[{itemId:'i',title:'Podcast',hasAudio:true,videoChoices:[{optionId:'v',width:1280,height:720,hasAudio:true}]}]}; }
    throw new Error('Unexpected method '+method);
  }}}};
  const settle=async()=>{for(let i=0;i<5;i++)await new Promise(resolve=>setTimeout(resolve,0));};
  try {
    await import('./app.js?retry='+Date.now()); await settle();
    const $=id=>window.document.getElementById(id);
    $('nav-history').click();
    const retry = id => $('task-list').querySelector('article[data-id="'+id+'"] button[data-action="retry"]');
    assert.equal(retry('canceled').textContent,'Baixar de novo');
    assert.equal(retry('failed').textContent,'Tentar de novo');
    retry('failed').click(); await settle();
    // Back on Downloads with the link in the field and a fresh analysis.
    assert.equal($('composer').hidden,false);
    assert.equal($('nav-queue').getAttribute('aria-current'),'page');
    assert.equal($('source-url').value,failed.url);
    assert.deepEqual(analyzed,[failed.url]);
    assert.equal($('selection').hidden,false);
    assert.equal($('mode-mp3').checked,true);
    assert.equal($('bitrate-select').value,'320');
  } finally {
    window.dispatchEvent(new window.Event('beforeunload')); await window.happyDOM.abort();
    delete globalThis.document;delete globalThis.window;
  }
});

// Regression: a link pasted while the app prepared waited for the whole
// dependency check before the search started.
test('a link pasted while the app prepares is searched at once and Baixar opens when ready', async () => {
  const window = new Window({url:'http://localhost/',settings:{disableJavaScriptFileLoading:true,disableJavaScriptEvaluation:true}});
  window.document.write(await readFile(new URL('./index.html',import.meta.url),'utf8'));
  globalThis.window = window; globalThis.document = window.document;
  let readiness = 'preparing';
  const analyzed = [];
  window.go={main:{App:{Call:async(method,serialized) => {
    if (method === 'snapshot') return {readiness,capabilities:{video:readiness === 'ready',mp3:readiness === 'ready',networkAccess:readiness === 'ready'},destination:{destinationId:'d'},tasks:[]};
    if (method === 'analyze') { const {url} = JSON.parse(serialized); analyzed.push(url); return {analysisId:'a',sourceUrl:url,items:[{itemId:'i',title:'Clipe',hasAudio:true,videoChoices:[{optionId:'v',width:1080,height:1920,hasAudio:true}]}]}; }
    throw new Error('Unexpected method '+method);
  }}}};
  const settle=async()=>{for(let i=0;i<5;i++)await new Promise(resolve=>setTimeout(resolve,0));};
  try {
    await import('./app.js?preparing='+Date.now()); await settle();
    const $=id=>window.document.getElementById(id);
    assert.equal($('analyze-button').disabled,false);
    $('source-url').value='https://youtube.com/shorts/early';
    $('source-url').dispatchEvent(new window.Event('paste')); await settle();
    assert.deepEqual(analyzed,['https://youtube.com/shorts/early']);
    // Unknown capabilities are not missing ones: the video stays selected.
    assert.equal($('selection').hidden,false);
    assert.equal($('mode-video').checked,true);
    assert.equal($('enqueue-button').disabled,true);
    assert.match($('format-hint').textContent,/Preparando/);
    readiness = 'ready';
    for (let i = 0; i < 100 && $('enqueue-button').disabled; i++) await new Promise(resolve=>setTimeout(resolve,10));
    assert.equal($('enqueue-button').disabled,false);
    assert.equal($('format-hint').textContent,'');
  } finally {
    window.dispatchEvent(new window.Event('beforeunload')); await window.happyDOM.abort();
    delete globalThis.document;delete globalThis.window;
  }
});

test('a link pasted while another is being analyzed is analyzed next', async () => {
  const window = new Window({url:'http://localhost/',settings:{disableJavaScriptFileLoading:true,disableJavaScriptEvaluation:true}});
  window.document.write(await readFile(new URL('./index.html',import.meta.url),'utf8'));
  globalThis.window = window; globalThis.document = window.document;
  const analyzed = []; const pending = [];
  window.go={main:{App:{Call:async(method,serialized) => {
    if(method==='snapshot')return {readiness:'ready',capabilities:{video:true,mp3:true,networkAccess:true},destination:{destinationId:'d'},tasks:[]};
    if(method==='analyze'){analyzed.push(JSON.parse(serialized).url);return new Promise(resolve=>pending.push(resolve));}
  }}}};
  const settle=async()=>{for(let i=0;i<4;i++)await new Promise(resolve=>setTimeout(resolve,0));};
  try {
    await import('./app.js?paste='+Date.now()); await settle();
    const $=id=>window.document.getElementById(id);
    $('source-url').value='https://youtube.com/shorts/first';
    $('source-url').dispatchEvent(new window.Event('paste')); await settle();
    $('source-url').value='https://youtube.com/shorts/second';
    $('source-url').dispatchEvent(new window.Event('input'));
    $('source-url').dispatchEvent(new window.Event('paste')); await settle();
    assert.deepEqual(analyzed,['https://youtube.com/shorts/first']);
    pending[0]({analysisId:'old',sourceUrl:'https://youtube.com/shorts/first',items:[{itemId:'i',title:'Old',videoChoices:[]}]});
    await settle();
    assert.deepEqual(analyzed,['https://youtube.com/shorts/first','https://youtube.com/shorts/second']);
    // Settle the call so its 120 s timeout does not keep the runner alive.
    pending[1]({analysisId:'new',sourceUrl:'https://youtube.com/shorts/second',items:[{itemId:'i',title:'New',videoChoices:[]}]});
    await settle();
  } finally {
    window.dispatchEvent(new window.Event('beforeunload')); await window.happyDOM.abort();
    delete globalThis.document;delete globalThis.window;
  }
});
