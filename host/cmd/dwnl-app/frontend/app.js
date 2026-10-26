import {activeStates, terminalStates, stateLabels, recentLimit, defaultChoice, choiceLabel, bytes, clock, progressPercent, errorMessage, partitionTasks, newlyCompleted, appendChoices, isVideoURL, pollDelay} from './model.mjs';

const $ = id => document.getElementById(id);
const state = {snapshot:null, analysis:null, item:null, view:'queue', analyzing:false, enqueuing:false, loadingFormats:false, requestId:null, requestKey:null, failures:0, pollTimer:null, refreshing:false, selectionVersion:0, exiting:false, archiving:false, seen:null, toastTask:null, toastTimer:null, pastedWhileAnalyzing:false};
const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const icons = {
  video:'<rect x="3" y="5" width="18" height="14" rx="3"/><path d="m10 9.5 4.5 2.5-4.5 2.5z"/>',
  mp3:'<path d="M9 18V6l11-2v12"/><circle cx="6.5" cy="18" r="2.5"/><circle cx="17.5" cy="16" r="2.5"/>',
  completed:'<circle cx="12" cy="12" r="9"/><path d="m8 12.5 2.8 2.8L16.2 9.6"/>',
  failed:'<circle cx="12" cy="12" r="9"/><path d="M12 7.5v5.5M12 16.5v.01"/>',
  canceled:'<circle cx="12" cy="12" r="9"/><path d="M8.5 15.5l7-7"/>',
  folder:'<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
};
const icon = name => '<svg class="icon" viewBox="0 0 24 24" aria-hidden="true">' + icons[name] + '</svg>';

async function call(method, payload = {}, timeout = 15000) {
  const bridge = window.go?.main?.App?.Call;
  if (typeof bridge !== 'function') throw new Error('A interface não conseguiu conectar ao aplicativo. Feche e abra o Dwnl novamente.');
  let timer;
  try {
    const result = await Promise.race([
      bridge(method, JSON.stringify(payload)),
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('O aplicativo demorou para responder. Tente novamente; sua fila permanece preservada.')), timeout); }),
    ]);
    const parsed = typeof result === 'string' ? JSON.parse(result) : result;
    // The native method returns data directly and rejects on operational errors.
    return parsed;
  } finally { clearTimeout(timer); }
}

function notice(id, value) {
  const element = $(id);
  element.textContent = value ? errorMessage(value) : '';
  element.hidden = !value;
}

const mode = () => document.querySelector('input[name="mode"]:checked')?.value ?? 'video';
function setMode(value) { $(value === 'mp3' ? 'mode-mp3' : 'mode-video').checked = true; }

function showView(view) {
  state.view = view;
  for (const [id, name] of [['nav-queue','queue'],['nav-history','history']]) {
    $(id).classList.toggle('selected', view === name);
    if (view === name) $(id).setAttribute('aria-current','page'); else $(id).removeAttribute('aria-current');
  }
  $('composer').hidden = view !== 'queue';
  $('queue-hint').hidden = view !== 'queue';
  renderTasks();
}

function renderReadiness() {
  const snapshot = state.snapshot;
  const ready = snapshot?.readiness === 'ready';
  const preparing = !snapshot || snapshot.readiness === 'preparing';
  const service = $('service-status');
  service.className = 'service-status ' + (state.failures ? 'failed' : ready ? 'ready' : '');
  service.lastElementChild.textContent = state.failures ? 'Reconectando…' : ready ? 'Pronto' : preparing ? 'Preparando…' : 'Requer atenção';
  $('version-label').textContent = snapshot?.version ? 'Dwnl ' + snapshot.version : 'Dwnl para Windows';
  $('destination-label').textContent = snapshot?.destination?.label || 'Escolha uma pasta para salvar';
  $('destination-label').title = snapshot?.destination?.label || '';
  $('start-at-login').checked = Boolean(snapshot?.startAtLogin);
  const serviceError = snapshot?.error || (snapshot?.readiness === 'degraded' ? 'Alguns componentes não estão disponíveis. Veja em Configurações.' : '');
  notice('service-notice', serviceError);
  const canAnalyze = Boolean(snapshot) && !preparing && state.failures === 0 && snapshot.capabilities?.networkAccess;
  $('analyze-button').disabled = state.analyzing || !canAnalyze;
  $('analyze-button').textContent = state.analyzing ? 'Buscando…' : 'Buscar';
  const dependencies = snapshot?.dependencies ?? [];
  $('dependency-list').innerHTML = dependencies.length ? dependencies.map(dep => '<li><span>' + escapeHTML(dep.name) + '</span><span>' + escapeHTML(dep.available ? dep.version || 'Disponível' : 'Indisponível') + '</span></li>').join('') : '<li>Aguardando verificação dos componentes.</li>';
  updateSelection();
}

function taskActions(task) {
  if (task.state === 'completed' && task.completedFile) return [['openFolder', icon('folder') + 'Mostrar na pasta'], ['openFile', 'Abrir']];
  if (!terminalStates.has(task.state)) return [['cancel', 'Cancelar']];
  return [];
}

// Rows are updated in place, so progress bars animate between polls and
// hovered or focused buttons survive each refresh.
function updateRow(row, task) {
  const signature = JSON.stringify(task);
  if (row.dataset.signature === signature) return;
  row.dataset.signature = signature;
  row.className = 'task ' + task.state;
  const kind = terminalStates.has(task.state) ? task.state : task.mode === 'mp3' ? 'mp3' : 'video';
  if (row.dataset.icon !== kind) { row.querySelector('.task-icon').innerHTML = icon(kind); row.dataset.icon = kind; }
  const title = task.title || task.url || 'Vídeo';
  row.querySelector('.task-title').textContent = title;
  row.querySelector('.task-title').title = title;
  const percent = progressPercent(task.progress);
  const active = activeStates.has(task.state);
  const status = stateLabels[task.state] || task.state;
  const received = bytes(task.progress?.completedBytes), total = bytes(task.progress?.totalBytes);
  const size = task.state === 'completed' ? bytes(task.completedFile?.sizeBytes) : received && total ? received + ' de ' + total : received;
  const meta = [status, active && percent !== null ? Math.round(percent) + '%' : '', task.mode === 'mp3' ? 'MP3' : 'MP4', size].filter(Boolean);
  row.querySelector('.task-meta').innerHTML = meta.map((text, index) => '<span' + (index === 0 ? ' class="task-status"' : '') + '>' + escapeHTML(text) + '</span>').join('');
  const progress = row.querySelector('.progress');
  progress.hidden = !active;
  progress.classList.toggle('indeterminate', percent === null);
  progress.setAttribute('aria-label', status);
  if (percent === null) progress.removeAttribute('aria-valuenow'); else progress.setAttribute('aria-valuenow', String(Math.round(percent)));
  progress.firstElementChild.style.transform = percent === null ? '' : 'scaleX(' + (percent / 100) + ')';
  const error = row.querySelector('.task-error');
  error.textContent = task.error ? errorMessage(task.error) : '';
  error.hidden = !task.error;
  const actions = taskActions(task);
  const actionKey = actions.map(([name]) => name).join() + (task.state === 'canceling' ? ':busy' : '');
  if (row.dataset.actions !== actionKey) {
    row.dataset.actions = actionKey;
    row.querySelector('.task-actions').innerHTML = actions.map(([name, label]) => '<button type="button" data-action="' + name + '" data-id="' + escapeHTML(task.id) + '"' + (task.state === 'canceling' ? ' disabled' : '') + '>' + label + '</button>').join('');
  }
}

function renderList(container, tasks) {
  const rows = new Map([...container.children].map(row => [row.dataset.id, row]));
  tasks.forEach((task, index) => {
    let row = rows.get(task.id);
    rows.delete(task.id);
    if (!row) {
      row = document.createElement('article');
      row.dataset.id = task.id;
      row.innerHTML = '<span class="task-icon"></span><div class="task-body"><h3 class="task-title"></h3><p class="task-meta"></p><div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" hidden><div class="progress-fill"></div></div><p class="task-error" hidden></p></div><div class="task-actions"></div>';
    }
    updateRow(row, task);
    if (container.children[index] !== row) container.insertBefore(row, container.children[index] ?? null);
  });
  for (const row of rows.values()) row.remove();
}

function renderTasks() {
  const {queue, history} = partitionTasks(state.snapshot?.tasks);
  const queueView = state.view === 'queue';
  const tasks = queueView ? queue : history;
  const recent = queueView ? history.slice(0, recentLimit) : [];
  $('archive-history').hidden = queueView;
  $('archive-history').disabled = state.archiving || history.length === 0 || state.failures > 0;
  $('history-status').hidden = queueView || !$('history-status').textContent;
  const limitReached = (state.snapshot?.tasks?.length ?? 0) >= 200;
  $('queue-limit-notice').hidden = !limitReached;
  $('queue-limit-text').textContent = history.length ? 'Limite de 200 registros atingido. Limpe o histórico para liberar espaço na fila.' : 'Limite de 200 registros atingido. Depois de concluir ou cancelar downloads, limpe o histórico para liberar espaço na fila.';
  $('show-history-limit').hidden = !queueView;

  $('queue-count').textContent = queue.length;
  $('queue-count').hidden = queue.length === 0;
  $('transfers-title').textContent = queueView ? 'Na fila' : 'Histórico';
  $('task-count').textContent = tasks.length ? String(tasks.length) : '';
  $('list-heading').hidden = queueView && queue.length === 0;
  $('paused-notice').hidden = !queueView || !state.snapshot?.paused || queue.length === 0;
  renderList($('task-list'), tasks);
  renderList($('recent-list'), recent);
  $('recent-section').hidden = recent.length === 0;

  const legacy = state.snapshot?.legacyHistory ?? [];
  $('legacy-section').hidden = queueView || legacy.length === 0;
  const legacySignature = JSON.stringify(legacy);
  if (legacySignature !== state.legacySignature) {
    $('legacy-list').innerHTML = legacy.slice().reverse().map(record => {
      const file = record.completedFile;
      const name = file?.path ? file.path.replaceAll('\\','/').split('/').pop() : 'Download anterior';
      const status = stateLabels[record.state] || record.state;
      const kind = terminalStates.has(record.state) ? record.state : 'video';
      return '<article class="task ' + escapeHTML(record.state) + '"><span class="task-icon">' + icon(kind) + '</span><div><h3 class="task-title" title="' + escapeHTML(name) + '">' + escapeHTML(name) + '</h3><p class="task-meta"><span class="task-status">' + escapeHTML(status) + '</span>' + (file?.format ? '<span>' + escapeHTML(file.format.toUpperCase()) + '</span>' : '') + (file?.sizeBytes ? '<span>' + escapeHTML(bytes(file.sizeBytes)) + '</span>' : '') + '</p>' + (record.error ? '<p class="task-error">' + escapeHTML(errorMessage(record.error)) + '</p>' : '') + '</div></article>';
    }).join('');
    state.legacySignature = legacySignature;
  }
  const empty = tasks.length === 0 && recent.length === 0 && (queueView || legacy.length === 0);
  $('empty-state').hidden = !empty;
  $('empty-state').querySelector('h3').textContent = queueView ? 'Nenhum download na fila' : 'Nada no histórico ainda';
  $('empty-state').querySelector('p').textContent = queueView ? 'Cole um link acima para começar. Os downloads continuam mesmo com a janela fechada.' : 'Os arquivos baixados aparecem aqui, com atalho para a pasta.';
}

function showToast(task) {
  state.toastTask = task.id;
  $('toast-title').textContent = task.title || 'Vídeo';
  $('toast').classList.remove('leaving');
  $('toast').hidden = false;
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(hideToast, 8000);
}
// Fade out first; the 200ms matches the toast transition.
function hideToast() {
  clearTimeout(state.toastTimer);
  state.toastTask = null;
  $('toast').classList.add('leaving');
  state.toastTimer = setTimeout(() => { $('toast').hidden = true; $('toast').classList.remove('leaving'); }, 200);
}

function currentChoice() {
  return state.item?.videoChoices?.find(c => c.optionId === $('quality-select').value);
}

function updateSelection() {
  $('selection').hidden = !state.analysis || !state.item;
  if (!state.item) return;
  const mp3 = mode() === 'mp3';
  $('quality-field').hidden = mp3;
  $('bitrate-field').hidden = !mp3;
  $('more-formats').hidden = mp3 || !state.item.formatsCursor;
  $('more-formats').disabled = state.loadingFormats;
  $('more-formats').textContent = state.loadingFormats ? 'Carregando…' : 'Mostrar mais qualidades';
  const videoAvailable = Boolean(state.snapshot?.capabilities?.video && state.item.videoChoices?.length);
  const mp3Available = Boolean(state.snapshot?.capabilities?.mp3 && (state.item.hasAudio || state.item.audioUnknown));
  $('mode-video').disabled = !videoAvailable;
  $('mode-mp3').disabled = !mp3Available;
  $('enqueue-button').disabled = state.enqueuing || !(mp3 ? mp3Available : videoAvailable && currentChoice()) || !state.snapshot?.destination?.destinationId || state.failures > 0;
  $('enqueue-button').textContent = state.enqueuing ? 'Adicionando…' : 'Baixar';
  const choice = currentChoice();
  $('format-hint').textContent = mp3 ? (mp3Available ? '' : 'Áudio indisponível para este vídeo.') : !videoAvailable ? 'Nenhuma qualidade de vídeo disponível.' : choice && !choice.hasAudio ? (choice.audioUnknown ? 'O som será confirmado durante o download.' : 'Este vídeo não tem som.') : '';
}

function selectItem() {
  state.item = state.analysis.items.find(item => item.itemId === $('item-select').value) || state.analysis.items[0];
  if (!state.item) { updateSelection(); return; }
  $('media-title').textContent = state.item.title || 'Vídeo sem título';
  $('media-meta').textContent = [clock(state.item.durationSeconds), new URL(state.analysis.sourceUrl).hostname.replace(/^www\./, '')].filter(Boolean).join(' · ');
  renderChoices(defaultChoice(state.item.videoChoices));
  setMode(state.snapshot?.capabilities?.video && state.item.videoChoices.length ? 'video' : 'mp3');
  updateSelection();
}

function renderChoices(selected) {
  $('quality-select').innerHTML = (state.item.videoChoices || []).map(choice => '<option value="' + escapeHTML(choice.optionId) + '">' + escapeHTML(choiceLabel(choice)) + '</option>').join('');
  $('quality-select').value = selected;
}

async function analyze(event) {
  event?.preventDefault();
  if (state.analyzing || $('analyze-button').disabled) return;
  const url = $('source-url').value.trim();
  if (!isVideoURL(url)) { notice('analysis-error','Cole um link HTTP ou HTTPS válido, sem usuário ou senha.'); return; }
  state.analyzing = true;
  const version = ++state.selectionVersion;
  state.analysis = null;
  state.item = null;
  notice('analysis-error', null);
  notice('analysis-status', 'Buscando informações do vídeo…');
  renderReadiness();
  try {
    const analysis = await call('analyze', {url}, 120000);
    if (version !== state.selectionVersion) return;
    if (!analysis?.items?.length) throw new Error('Nenhum vídeo individual foi encontrado neste link.');
    state.analysis = analysis;
    $('item-field').hidden = analysis.items.length <= 1;
    $('item-select').innerHTML = analysis.items.map(item => '<option value="' + escapeHTML(item.itemId) + '">' + escapeHTML(item.title || 'Vídeo') + '</option>').join('');
    selectItem();
    $('enqueue-button').focus({preventScroll:true});
  } catch (error) { if (version === state.selectionVersion) notice('analysis-error',error); }
  finally {
    state.analyzing = false;
    notice('analysis-status',null);
    renderReadiness();
    if (state.pastedWhileAnalyzing) { state.pastedWhileAnalyzing = false; analyzeIfLink(); }
  }
}

// A pasted link is analyzed right away; typing still waits for Buscar/Enter.
function analyzeIfLink() {
  if (!isVideoURL($('source-url').value.trim())) return;
  if (state.analyzing) state.pastedWhileAnalyzing = true;
  else void analyze();
}

async function pasteLink() {
  let text = '';
  try { text = await (window.runtime?.ClipboardGetText?.() ?? navigator.clipboard?.readText?.()) ?? ''; } catch {}
  text = text.trim();
  if (!isVideoURL(text)) { $('source-url').focus(); notice('analysis-error', text ? 'A área de transferência não contém um link de vídeo.' : 'Não foi possível ler a área de transferência. Use Ctrl+V no campo do link.'); return; }
  $('source-url').value = text;
  $('source-url').dispatchEvent(new Event('input'));
  analyzeIfLink();
}

async function enqueue() {
  if (state.enqueuing || $('enqueue-button').disabled || !state.analysis || !state.item) return;
  const payload = {analysisId:state.analysis.analysisId,itemId:state.item.itemId,mode:mode(),destinationId:state.snapshot.destination.destinationId};
  if (payload.mode === 'mp3') payload.bitrateKbps = Number($('bitrate-select').value);
  else payload.videoOptionId = $('quality-select').value;
  const key = JSON.stringify(payload);
  // A retry after an ambiguous timeout must reuse the same idempotency key.
  if (key !== state.requestKey) { state.requestKey = key; state.requestId = crypto.randomUUID(); }
  payload.requestId = state.requestId;
  state.enqueuing = true;
  notice('analysis-error',null);
  updateSelection();
  try {
    await call('enqueue',payload);
    state.analysis = null; state.item = null;
    $('source-url').value = '';
    $('source-url').focus({preventScroll:true});
    await refresh();
  } catch (error) { notice('analysis-error',error); }
  finally { state.enqueuing = false; updateSelection(); }
}

async function loadMore() {
  if (state.loadingFormats || !state.item?.formatsCursor) return;
  const item = state.item;
  const analysis = state.analysis;
  state.loadingFormats = true;
  updateSelection();
  try {
    const result = await call('listFormats',{analysisId:analysis.analysisId,itemId:item.itemId,cursor:item.formatsCursor},30000);
    if (state.item !== item || state.analysis !== analysis) return;
    const selected = $('quality-select').value;
    item.videoChoices = appendChoices(item.videoChoices,result.choices || []);
    item.formatsCursor = result.nextCursor || '';
    renderChoices(selected);
  } catch (error) { notice('analysis-error',error); }
  finally { state.loadingFormats = false; updateSelection(); }
}

function schedulePoll() {
  clearTimeout(state.pollTimer);
  if (!document.hidden && !state.exiting) state.pollTimer = setTimeout(refresh, pollDelay(state.snapshot,state.failures));
}

async function refresh() {
  if (state.refreshing) return;
  state.refreshing = true;
  try {
    const snapshot = await call('snapshot',{},10000);
    if (!snapshot || !Array.isArray(snapshot.tasks)) throw new Error('O aplicativo retornou um estado de fila inválido.');
    // The first snapshot only records states; tasks finished before it are not news.
    const finished = state.seen ? newlyCompleted(state.seen, snapshot.tasks) : [];
    state.seen = new Map(snapshot.tasks.map(task => [task.id, task.state]));
    state.snapshot = snapshot;
    state.failures = 0;
    notice('global-error',null);
    renderReadiness();
    renderTasks();
    if (finished.length) showToast(finished.at(-1));
  } catch (error) {
    state.failures++;
    notice('global-error',error);
    renderReadiness();
  } finally { state.refreshing = false; schedulePoll(); }
}

async function action(button, method, payload = {}) {
  if (button.disabled) return;
  button.disabled = true;
  notice('global-error',null);
  // openFile re-hashes the media first; wait as long as the native call allows.
  try { await call(method,payload,method === 'chooseDestination' ? 300000 : method === 'openFile' ? 130000 : 15000); await refresh(); }
  catch (error) { notice('global-error',error); }
  finally { button.disabled = false; }
}

$('analyze-form').addEventListener('submit',analyze);
$('enqueue-button').addEventListener('click',enqueue);
$('more-formats').addEventListener('click',loadMore);
$('item-select').addEventListener('change',selectItem);
for (const input of document.querySelectorAll('input[name="mode"]')) input.addEventListener('change',updateSelection);
$('quality-select').addEventListener('change',updateSelection);
$('source-url').addEventListener('input',() => {
  state.selectionVersion++;
  if (state.analysis) { state.analysis = null; state.item = null; updateSelection(); notice('analysis-status',null); }
});
$('source-url').addEventListener('paste',() => setTimeout(analyzeIfLink,0));
$('paste-button').addEventListener('click',pasteLink);
$('nav-queue').addEventListener('click',() => showView('queue'));
$('nav-history').addEventListener('click',() => showView('history'));
$('show-history').addEventListener('click',() => showView('history'));
$('destination-button').addEventListener('click',() => action($('destination-button'),'chooseDestination'));
$('continue-button').addEventListener('click',() => action($('continue-button'),'continue'));
for (const list of ['task-list','recent-list']) $(list).addEventListener('click',event => {
  const button = event.target.closest('button[data-action]');
  if (button) action(button,button.dataset.action,{id:button.dataset.id});
});
$('toast-folder').addEventListener('click',() => { const id = state.toastTask; hideToast(); if (id) action($('toast-folder'),'openFolder',{id}); });
$('toast-close').addEventListener('click',hideToast);
$('toast').addEventListener('mouseenter',() => clearTimeout(state.toastTimer));
$('toast').addEventListener('mouseleave',() => { if (!$('toast').hidden) state.toastTimer = setTimeout(hideToast, 4000); });
$('settings-button').addEventListener('click',() => $('settings-dialog').showModal());
$('start-at-login').addEventListener('change', async () => {
  const input = $('start-at-login');
  input.disabled = true;
  const selected = input.checked;
  notice('settings-error',null);
  try { await call('settings',{startAtLogin:selected}); await refresh(); }
  catch (error) { input.checked = Boolean(state.snapshot?.startAtLogin); notice('settings-error',error); }
  finally { input.disabled = false; }
});
document.addEventListener('visibilitychange',() => { clearTimeout(state.pollTimer); if (!document.hidden) refresh(); });
window.addEventListener('focus',() => { if (!document.hidden && state.failures) refresh(); });

$('exit-button').addEventListener('click', async () => {
  const button = $('exit-button');
  if (state.exiting) return;
  state.exiting = true;
  clearTimeout(state.pollTimer);
  button.disabled = true;
  button.textContent = 'Saindo…';
  notice('settings-error',null);
  try {
    const exit = window.go?.main?.App?.Exit;
    if (typeof exit !== 'function') throw new Error('Não foi possível encerrar o aplicativo. Use Sair no menu da bandeja.');
    await exit();
  } catch (error) {
    state.exiting = false;
    button.disabled = false;
    button.textContent = 'Sair';
    notice('settings-error',error);
    schedulePoll();
  }
});


$('show-history-limit').addEventListener('click', () => { showView('history'); $('archive-history').focus(); });
$('archive-history').addEventListener('click', () => {
  notice('archive-error',null);
  $('archive-dialog').showModal();
});
$('archive-cancel').addEventListener('click', () => { if (!state.archiving) $('archive-dialog').close(); });
$('archive-dialog').addEventListener('cancel', event => { if (state.archiving) event.preventDefault(); });
$('archive-confirm').addEventListener('click', async () => {
  if (state.archiving) return;
  state.archiving = true;
  $('archive-confirm').disabled = true;
  $('archive-cancel').disabled = true;
  $('archive-confirm').textContent = 'Limpando…';
  notice('archive-error',null);
  try {
    const result = await call('archiveHistory',{});
    if (!Number.isInteger(result?.archived) || result.archived < 0) throw new Error('Não foi possível confirmar a limpeza. Confira a lista antes de repetir.');
    notice('history-status',result.archived === 1 ? '1 registro removido da lista. O arquivo continua na pasta.' : result.archived + ' registros removidos da lista. Os arquivos continuam na pasta.');
    $('archive-dialog').close();
    await refresh();
  } catch (error) { notice('archive-error',error); }
  finally {
    state.archiving = false;
    $('archive-confirm').disabled = false;
    $('archive-cancel').disabled = false;
    $('archive-confirm').textContent = 'Limpar histórico';
    renderTasks();
  }
});

window.addEventListener('beforeunload',() => { clearTimeout(state.pollTimer); clearTimeout(state.toastTimer); });
refresh();
