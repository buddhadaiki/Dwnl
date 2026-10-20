import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {Window} from 'happy-dom';
import {defaultChoice, qualityPixels, progressPercent, appendChoices, partitionTasks, isVideoURL, pollDelay, choiceLabel} from './model.mjs';

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
  assert.equal(pollDelay({readiness:'ready',tasks:[{state:'downloading'}]}),2000);
  assert.equal(pollDelay(null,10),30000);
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
