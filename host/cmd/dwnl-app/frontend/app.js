import {activeStates, stateLabels, defaultChoice, choiceLabel, bytes, progressPercent, errorMessage, partitionTasks, appendChoices, isVideoURL, pollDelay} from './model.mjs';

const $ = id => document.getElementById(id);
const state = {snapshot:null, analysis:null, item:null, view:'queue', analyzing:false, enqueuing:false, loadingFormats:false, requestId:null, requestKey:null, failures:0, pollTimer:null, refreshing:false, queueSignature:'', selectionVersion:0, exiting:false, archiving:false};
const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

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

function showView(view) {
  state.view = view;
  $('nav-queue').classList.toggle('selected', view === 'queue');
  $('nav-history').classList.toggle('selected', view === 'history');
  $('nav-queue').toggleAttribute('aria-current', view === 'queue');
  $('nav-history').toggleAttribute('aria-current', view === 'history');
  (view === 'queue' ? $('nav-queue') : $('nav-history')).setAttribute('aria-current','page');
  $('page-title').textContent = view === 'queue' ? 'Seus downloads.' : 'Tudo que você salvou.';
  $('page-subtitle').textContent = view === 'queue' ? 'Cole o link. Escolha o formato. Salve do seu jeito.' : 'Arquivos concluídos e resultados das suas transferências.';
  $('composer').hidden = view !== 'queue';
  $('new-button').hidden = view === 'queue';
  $('transfers-title').firstChild.textContent = view === 'queue' ? 'Fila de downloads ' : 'Histórico ';
  $('queue-hint').hidden = view !== 'queue';
  state.queueSignature = '';
  renderTasks();
}

function renderReadiness() {
  const snapshot = state.snapshot;
  const ready = snapshot?.readiness === 'ready';
  const preparing = !snapshot || snapshot.readiness === 'preparing';
  const service = $('service-status');
  service.className = 'service-status ' + (ready ? 'ready' : state.failures ? 'failed' : '');
  service.lastElementChild.textContent = state.failures ? 'Conexão interrompida' : ready ? 'App conectado' : preparing ? 'Preparando downloads…' : 'Disponibilidade limitada';
  $('version-label').textContent = snapshot?.version ? 'Dwnl ' + snapshot.version : 'Dwnl para Windows';
  $('destination-label').textContent = snapshot?.destination?.label || 'Escolha uma pasta para salvar';
  $('destination-label').title = snapshot?.destination?.label || '';
  $('start-at-login').checked = Boolean(snapshot?.startAtLogin);
  const serviceError = snapshot?.error || (snapshot?.readiness === 'degraded' ? 'Alguns componentes não estão disponíveis. Consulte Configurações para verificar.' : '');
  notice('service-notice', serviceError);
  $('analyze-button').disabled = state.analyzing || !snapshot || preparing || state.failures > 0 || !snapshot.capabilities?.networkAccess;
  $('analyze-button').textContent = state.analyzing ? 'Analisando…' : 'Analisar link';
  $('footer-status').textContent = state.failures ? 'Reconectando automaticamente…' : preparing ? 'Preparando o aplicativo…' : 'Os downloads continuam com a janela fechada.';
  const dependencies = snapshot?.dependencies ?? [];
  $('dependency-list').innerHTML = dependencies.length ? dependencies.map(dep => '<li><span>' + escapeHTML(dep.name) + '</span><span>' + escapeHTML(dep.available ? dep.version || 'Disponível' : 'Indisponível') + '</span></li>').join('') : '<li>Aguardando verificação dos componentes.</li>';
  updateSelection();
}

function taskMarkup(task) {
  const percent = progressPercent(task.progress);
  const progressLabel = percent === null ? '' : Math.round(percent) + '%';
  const size = bytes(task.progress?.completedBytes);
  const total = bytes(task.progress?.totalBytes);
  const status = stateLabels[task.state] || task.state;
  const detail = task.mode === 'mp3' ? 'MP3' + (task.bitrateKbps ? ' / ' + task.bitrateKbps + ' kbps' : '') : 'Vídeo';
  const sizeLabel = task.state === 'completed' ? bytes(task.completedFile?.sizeBytes) : size ? size + (total ? ' de ' + total : '') : '';
  const icon = task.state === 'completed' ? '✓' : task.state === 'failed' ? '!' : task.mode === 'mp3' ? '♪' : '↓';
  let actions = '';
  if (task.state === 'completed' && task.completedFile) {
    actions = '<button class="text-button" data-action="openFile" data-id="' + escapeHTML(task.id) + '">Abrir arquivo</button><button class="text-button" data-action="openFolder" data-id="' + escapeHTML(task.id) + '">Ver na pasta</button>';
  } else if (!['completed','canceled','failed'].includes(task.state)) {
    actions = '<button class="text-button" data-action="cancel" data-id="' + escapeHTML(task.id) + '"' + (task.state === 'canceling' ? ' disabled' : '') + '>Cancelar</button>';
  }
  const progress = activeStates.has(task.state) ? '<div class="progress-line' + (percent === null ? ' indeterminate' : '') + '" role="progressbar" aria-label="' + escapeHTML(status) + '"' + (percent !== null ? ' aria-valuenow="' + Math.round(percent) + '" aria-valuemin="0" aria-valuemax="100"' : '') + '><div class="progress-fill"' + (percent !== null ? ' style="width:' + percent + '%"' : '') + '></div></div>' : '';
  return '<article class="task ' + escapeHTML(task.state) + '"><div class="task-symbol" aria-hidden="true">' + icon + '</div><div><h3 class="task-title">' + escapeHTML(task.title || task.url || 'Vídeo') + '</h3><div class="task-meta"><span class="task-status">' + escapeHTML(status) + '</span><span>' + escapeHTML(detail) + '</span>' + (progressLabel ? '<span>' + progressLabel + '</span>' : '') + (sizeLabel ? '<span>' + escapeHTML(sizeLabel) + '</span>' : '') + '</div>' + progress + (task.error ? '<p class="task-error">' + escapeHTML(errorMessage(task.error)) + '</p>' : '') + '</div><div class="task-actions">' + actions + '</div></article>';
}

function renderTasks() {
  const {queue, history} = partitionTasks(state.snapshot?.tasks);
  $('archive-history').hidden = state.view !== 'history';
  $('archive-history').disabled = state.archiving || history.length === 0 || state.failures > 0;
  $('history-status').hidden = state.view !== 'history' || !$('history-status').textContent;
  const limitReached = (state.snapshot?.tasks?.length ?? 0) >= 200;
  $('queue-limit-notice').hidden = !limitReached;
  $('queue-limit-text').textContent = history.length ? 'Limite de 200 registros atingido. Arquive o histórico para liberar espaço na fila.' : 'Limite de 200 registros atingido. Após concluir ou cancelar downloads, arquive o histórico para liberar espaço na fila.';
  $('show-history-limit').hidden = state.view === 'history';

  $('queue-count').textContent = queue.length;
  const tasks = state.view === 'queue' ? queue : history;
  $('task-count').textContent = tasks.length ? String(tasks.length) : '';
  $('paused-notice').hidden = state.view !== 'queue' || !state.snapshot?.paused || queue.length === 0;
  const signature = JSON.stringify([state.view, tasks]);
  if (signature !== state.queueSignature) {
    const focused = $('task-list').contains(document.activeElement) ? {id:document.activeElement.dataset.id, action:document.activeElement.dataset.action} : null;
    $('task-list').innerHTML = tasks.map(taskMarkup).join('');
    if (focused) [...$('task-list').querySelectorAll('button')].find(button => button.dataset.id === focused.id && button.dataset.action === focused.action)?.focus({preventScroll:true});
    state.queueSignature = signature;
  }
  $('empty-state').hidden = tasks.length > 0;
  $('empty-state').querySelector('h3').textContent = state.view === 'history' ? 'Seus arquivos vão aparecer aqui.' : 'Sua próxima descoberta começa com um link.';
  $('empty-state').querySelector('p').textContent = state.view === 'history' ? 'Depois de baixar, abra o arquivo ou encontre a pasta com um clique.' : 'Adicione um vídeo acima. Você pode fechar a janela enquanto o download continua na bandeja.';
  const legacy = state.snapshot?.legacyHistory ?? [];
  $('legacy-section').hidden = state.view !== 'history' || legacy.length === 0;
  const legacySignature = JSON.stringify(legacy);
  if (legacySignature !== state.legacySignature) {
    $('legacy-list').innerHTML = legacy.slice().reverse().map(record => {
      const file = record.completedFile;
      const name = file?.path ? file.path.replaceAll('\\','/').split('/').pop() : 'Download anterior';
      const status = stateLabels[record.state] || record.state;
      return '<article class="task ' + escapeHTML(record.state) + '"><div class="task-symbol" aria-hidden="true">' + (record.state === 'completed' ? '✓' : '↶') + '</div><div><h3 class="task-title">' + escapeHTML(name) + '</h3><div class="task-meta"><span class="task-status">' + escapeHTML(status) + '</span>' + (file?.format ? '<span>' + escapeHTML(file.format.toUpperCase()) + '</span>' : '') + (file?.sizeBytes ? '<span>' + escapeHTML(bytes(file.sizeBytes)) + '</span>' : '') + '</div>' + (record.error ? '<p class="task-error">' + escapeHTML(errorMessage(record.error)) + '</p>' : '') + '</div></article>';
    }).join('');
    state.legacySignature = legacySignature;
  }
  if (state.view === 'history' && legacy.length) $('empty-state').hidden = true;

}

function currentChoice() {
  return state.item?.videoChoices?.find(c => c.optionId === $('quality-select').value);
}

function updateSelection() {
  $('selection').hidden = !state.analysis || !state.item;
  if (!state.item) return;
  const mp3 = $('mode-select').value === 'mp3';
  $('quality-field').hidden = mp3;
  $('bitrate-field').hidden = !mp3;
  $('more-formats').hidden = mp3 || !state.item.formatsCursor;
  $('more-formats').disabled = state.loadingFormats;
  $('more-formats').textContent = state.loadingFormats ? 'Carregando…' : 'Mostrar mais qualidades';
  const videoAvailable = Boolean(state.snapshot?.capabilities?.video && state.item.videoChoices?.length);
  const mp3Available = Boolean(state.snapshot?.capabilities?.mp3 && (state.item.hasAudio || state.item.audioUnknown));
  $('mode-select').options[0].disabled = !videoAvailable;
  $('mode-select').options[1].disabled = !mp3Available;
  $('enqueue-button').disabled = state.enqueuing || !(mp3 ? mp3Available : videoAvailable && currentChoice()) || !state.snapshot?.destination?.destinationId || state.failures > 0;
  $('enqueue-button').textContent = state.enqueuing ? 'Adicionando…' : 'Adicionar à fila';
  $('format-hint').textContent = mp3 ? (mp3Available ? 'O bitrate escolhido será usado na conversão.' : 'Áudio MP3 indisponível para este vídeo ou neste dispositivo.') : !videoAvailable ? 'Nenhuma qualidade de vídeo disponível.' : !currentChoice()?.hasAudio ? (currentChoice()?.audioUnknown ? 'A presença de áudio será verificada durante o download.' : 'Esta qualidade não contém áudio.') : 'O download usa exatamente a qualidade selecionada.';
}

function selectItem() {
  state.item = state.analysis.items.find(item => item.itemId === $('item-select').value) || state.analysis.items[0];
  if (!state.item) { updateSelection(); return; }
  $('media-title').textContent = state.item.title || 'Vídeo sem título';
  const duration = state.item.durationSeconds;
  const seconds = Number.isFinite(duration) ? Math.floor(duration) : null;
  $('media-meta').textContent = [seconds !== null ? Math.floor(seconds/60) + ':' + String(seconds % 60).padStart(2, '0') : '', new URL(state.analysis.sourceUrl).hostname].filter(Boolean).join(' · ');
  renderChoices(defaultChoice(state.item.videoChoices));
  $('mode-select').value = state.snapshot?.capabilities?.video && state.item.videoChoices.length ? 'video' : 'mp3';
  updateSelection();
}

function renderChoices(selected) {
  $('quality-select').innerHTML = (state.item.videoChoices || []).map(choice => '<option value="' + escapeHTML(choice.optionId) + '">' + escapeHTML(choiceLabel(choice)) + '</option>').join('');
  $('quality-select').value = selected;
}

async function analyze(event) {
  event.preventDefault();
  if (state.analyzing || $('analyze-button').disabled) return;
  const url = $('source-url').value.trim();
  if (!isVideoURL(url)) { notice('analysis-error','Cole um link HTTP ou HTTPS válido, sem usuário ou senha.'); return; }
  state.analyzing = true;
  const version = ++state.selectionVersion;
  state.analysis = null;
  state.item = null;
  notice('analysis-error', null);
  notice('analysis-status', 'Buscando os formatos disponíveis…');
  renderReadiness();
  try {
    const analysis = await call('analyze', {url}, 120000);
    if (version !== state.selectionVersion) return;
    if (!analysis?.items?.length) throw new Error('Nenhum vídeo individual foi encontrado neste link.');
    state.analysis = analysis;
    $('item-field').hidden = analysis.items.length <= 1;
    $('item-select').innerHTML = analysis.items.map(item => '<option value="' + escapeHTML(item.itemId) + '">' + escapeHTML(item.title || 'Vídeo') + '</option>').join('');
    selectItem();
    $('mode-select').focus({preventScroll:true});
  } catch (error) { if (version === state.selectionVersion) notice('analysis-error',error); }
  finally {
    state.analyzing = false;
    notice('analysis-status',null);
    renderReadiness();
  }
}

async function enqueue() {
  if (state.enqueuing || $('enqueue-button').disabled || !state.analysis || !state.item) return;
  const payload = {analysisId:state.analysis.analysisId,itemId:state.item.itemId,mode:$('mode-select').value,destinationId:state.snapshot.destination.destinationId};
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
    notice('analysis-status','Download adicionado à fila.');
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
    state.snapshot = snapshot;
    state.failures = 0;
    notice('global-error',null);
    renderReadiness();
    renderTasks();
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
  try { await call(method,payload,method === 'chooseDestination' ? 300000 : 15000); await refresh(); }
  catch (error) { notice('global-error',error); }
  finally { button.disabled = false; }
}

$('analyze-form').addEventListener('submit',analyze);
$('enqueue-button').addEventListener('click',enqueue);
$('more-formats').addEventListener('click',loadMore);
$('item-select').addEventListener('change',selectItem);
$('mode-select').addEventListener('change',updateSelection);
$('quality-select').addEventListener('change',updateSelection);
$('source-url').addEventListener('input',() => {
  state.selectionVersion++;
  if (state.analysis) { state.analysis = null; state.item = null; updateSelection(); notice('analysis-status',null); }
});
$('nav-queue').addEventListener('click',() => showView('queue'));
$('nav-history').addEventListener('click',() => showView('history'));
$('new-button').addEventListener('click',() => { showView('queue'); $('source-url').focus(); });
$('destination-button').addEventListener('click',() => action($('destination-button'),'chooseDestination'));
$('continue-button').addEventListener('click',() => action($('continue-button'),'continue'));
$('task-list').addEventListener('click',event => {
  const button = event.target.closest('button[data-action]');
  if (button) action(button,button.dataset.action,{id:button.dataset.id});
});
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
  button.textContent = 'Encerrando…';
  notice('settings-error',null);
  try {
    const exit = window.go?.main?.App?.Exit;
    if (typeof exit !== 'function') throw new Error('Não foi possível encerrar o aplicativo. Use Sair no menu da bandeja.');
    await exit();
  } catch (error) {
    state.exiting = false;
    button.disabled = false;
    button.textContent = 'Sair do aplicativo';
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
  $('archive-confirm').textContent = 'Arquivando…';
  notice('archive-error',null);
  try {
    const result = await call('archiveHistory',{});
    if (!Number.isInteger(result?.archived) || result.archived < 0) throw new Error('Não foi possível confirmar o arquivamento. Confira a lista antes de repetir.');
    notice('history-status',result.archived === 1 ? '1 registro arquivado. Arquivo e recibo preservados.' : result.archived + ' registros arquivados. Arquivos e recibos preservados.');
    $('archive-dialog').close();
    await refresh();
  } catch (error) { notice('archive-error',error); }
  finally {
    state.archiving = false;
    $('archive-confirm').disabled = false;
    $('archive-cancel').disabled = false;
    $('archive-confirm').textContent = 'Arquivar histórico';
    renderTasks();
  }
});

window.addEventListener('beforeunload',() => clearTimeout(state.pollTimer));
refresh();
