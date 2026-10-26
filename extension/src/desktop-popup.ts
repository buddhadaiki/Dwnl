import { browserAPI, type BrowserAPI } from './browser.js';
import { DESKTOP_PORT, DesktopClient, DesktopError, bestDesktopVideo, desktopRequest, type DesktopSnapshot, type EnqueuePayload } from './desktop-client.js';
import type { Analysis, MediaItem, MP3Bitrate, VideoChoice } from './protocol.js';
import { qualityName } from './quality.js';

const PENDING_KEY = 'dwnl-pending-enqueue-v2';
export function mountDesktopPopup(doc: Document, browser: BrowserAPI): () => void {
  const element = <T extends HTMLElement>(id: string) => doc.getElementById(id) as T;
  const client = new DesktopClient(() => browser.runtime.connect({ name: DESKTOP_PORT }), () => browser.runtime.lastError?.message);
  const url = element<HTMLInputElement>('url'), quality = element<HTMLSelectElement>('quality'), mode = element<HTMLSelectElement>('mode'), itemSelect = element<HTMLSelectElement>('item');
  let snapshot: DesktopSnapshot | undefined, analysis: Analysis | undefined, pending: EnqueuePayload | undefined;
  let busy = false, refreshing = false, stopped = false, connected = false, timer: ReturnType<typeof setTimeout> | undefined;
  const listeners: (() => void)[] = [];
  const on = (id: string, event: string, action: (event: Event) => void) => { const node = element(id); node.addEventListener(event, action); listeners.push(() => node.removeEventListener(event, action)); };
  const feedback = (text: string, error = false) => { const node = element('feedback'); node.textContent = text; node.hidden = !text; node.dataset.error = String(error); };
  const current = (): MediaItem | undefined => analysis?.items.find(item => item.itemId === itemSelect.value);
  // 'degraded' still serves requests; the app checks each capability itself.
  const ready = () => connected && !!snapshot && snapshot.readiness !== 'preparing';
  function controls(): void {
    const disabled = busy || !!pending;
    element<HTMLButtonElement>('analyze').disabled = !ready() || disabled;
    element<HTMLButtonElement>('current-tab').disabled = disabled;
    element<HTMLButtonElement>('choose-destination').disabled = !connected || disabled;
    element<HTMLButtonElement>('more-formats').disabled = !ready() || disabled;
    element<HTMLButtonElement>('open-app').disabled = busy;
    element<HTMLButtonElement>('continue').disabled = busy || !ready();
    url.disabled = disabled; mode.disabled = disabled; quality.disabled = disabled; itemSelect.disabled = disabled;
    element<HTMLButtonElement>('enqueue').disabled = !ready() || disabled || !quality.value || !(mode.value === 'mp3' ? snapshot?.capabilities.mp3 : snapshot?.capabilities.video);
    element<HTMLButtonElement>('retry-enqueue').hidden = !pending;
    element<HTMLButtonElement>('retry-enqueue').disabled = busy || !ready();
    element<HTMLButtonElement>('reconnect').hidden = connected;
    element<HTMLButtonElement>('reconnect').disabled = busy;
  }
  function renderSnapshot(): void {
    element('status').textContent = connected ? snapshot?.readiness === 'preparing' ? 'App conectado · preparando ferramentas…' : snapshot?.readiness === 'degraded' ? 'App conectado · requer atenção' : 'App conectado · pronto' : 'Aplicativo desconectado';
    if (snapshot) {
      element('destination').textContent = snapshot.destination.label;
      element('destination').title = snapshot.destination.label;
      const tasks = snapshot.tasks, active = tasks.find(t => ['analyzing', 'downloading', 'processing', 'muxing', 'converting', 'saving', 'canceling'].includes(t.state));
      const queued = tasks.filter(t => ['queued', 'interrupted'].includes(t.state)).length;
      element('queue').textContent = active ? active.title + (active.progress?.percent !== undefined ? ' · ' + Math.round(active.progress.percent) + '%' : ' · em andamento') + (queued ? ' · ' + queued + ' na fila' : '') : snapshot.paused && queued ? queued + ' download(s) aguardando Continuar fila.' : queued ? queued + ' download(s) na fila.' : 'Nenhum download pendente. Histórico completo no app.';
      element('continue').hidden = !snapshot.paused || queued === 0;
      if (snapshot.error && !busy) feedback(snapshot.error, true);
    }
    controls();
  }
  function renderQuality(selected?: string): void {
    const item = current(); quality.replaceChildren();
    if (!item) return;
    element('media-title').textContent = item.title;
    const mp3Option = mode.querySelector<HTMLOptionElement>('option[value="mp3"]')!;
    mp3Option.disabled = !snapshot?.capabilities.mp3 || !(item.hasAudio || item.audioUnknown);
    if (mode.value === 'mp3' && mp3Option.disabled) mode.value = 'video';
    const add = (value: string, label: string) => { const option = doc.createElement('option'); option.value = value; option.textContent = label; quality.append(option); };
    if (mode.value === 'mp3') { for (const bitrate of [128, 192, 320]) add(String(bitrate), bitrate + ' kbps'); quality.value = selected && ['128','192','320'].includes(selected) ? selected : '192'; }
    else {
      for (const choice of item.videoChoices) add(choice.optionId, describeVideo(choice));
      const best = bestDesktopVideo(item.videoChoices);
      quality.value = selected && item.videoChoices.some(c => c.optionId === selected) ? selected : best?.optionId ?? '';
      if (!quality.value) { const placeholder = doc.createElement('option'); placeholder.value = ''; placeholder.textContent = item.videoChoices.length ? 'Escolha uma qualidade' : 'Nenhum formato de vídeo'; placeholder.selected = true; quality.prepend(placeholder); }
    }
    element('more-formats').hidden = !item.formatsCursor || mode.value === 'mp3';
    const choice = item.videoChoices.find(c => c.optionId === quality.value);
    element('media-note').textContent = mode.value === 'video' && choice && !choice.hasAudio ? choice.audioUnknown ? 'O som será confirmado durante o download.' : 'Este vídeo não tem som.' : item.audioUnknown ? 'O som ainda não foi confirmado.' : '';
    controls();
  }
  const fail = (error: unknown) => {
    if (!(error instanceof DesktopError) || ['TIMEOUT', 'DISCONNECTED', 'MISSING_HOST', 'APP_UNAVAILABLE', 'INVALID_MESSAGE', 'INCOMPATIBLE_PROTOCOL'].includes(error.code)) { connected = false; renderSnapshot(); }
    feedback(error instanceof Error ? error.message : 'Não foi possível concluir a ação.', true);
  };
  async function refresh(): Promise<void> {
    if (refreshing || stopped || busy) return;
    refreshing = true;
    try { snapshot = await client.request('snapshot', {}); connected = true; if (pending && snapshot.tasks.some(task => task.requestId === pending!.requestId)) { await browser.storage.local.set({ [PENDING_KEY]: null }); pending = undefined; feedback('Envio anterior confirmado na fila do app.'); } renderSnapshot(); }
    catch (error) { fail(error); }
    finally { refreshing = false; }
  }
  async function action(work: () => Promise<void>): Promise<void> {
    if (busy) return;
    busy = true; controls(); feedback('');
    try { await work(); } catch (error) { fail(error); } finally { busy = false; controls(); if (connected) await refresh(); }
  }
  async function sendPending(): Promise<void> {
    if (!pending) return;
    try {
      const task = await client.request('enqueue', pending);
      await browser.storage.local.set({ [PENDING_KEY]: null });
      pending = undefined;
      feedback('Adicionado à fila: ' + task.title + '. O app cuida do download.');
    } catch (error) {
      if (error instanceof DesktopError && ['APP_ERROR', 'ANALYSIS_EXPIRED', 'INVALID_SELECTION', 'DESTINATION_ERROR', 'REQUEST_CONFLICT', 'DEPENDENCY_UNAVAILABLE', 'BUSY'].includes(error.code)) {
        // Reconcile with the source of truth before allowing a fresh intent.
        const confirmed = await client.request('snapshot', {});
        const accepted = confirmed.tasks.some(task => task.requestId === pending!.requestId);
        await browser.storage.local.set({ [PENDING_KEY]: null }); pending = undefined;
        if (accepted) { feedback('Envio confirmado na fila do app.'); return; }
      }
      throw error;
    }
  }
  on('analyze-form', 'submit', event => { event.preventDefault(); void action(async () => {
    analysis = undefined; element('selection').hidden = true;
    feedback('Analisando o vídeo…');
    const result = await client.request('analyze', { url: url.value.trim() });
    analysis = result; itemSelect.replaceChildren();
    for (const item of result.items) { const option = doc.createElement('option'); option.value = item.itemId; option.textContent = item.title; itemSelect.append(option); }
    element('selection').hidden = false; renderQuality(); feedback('');
  }); });
  on('current-tab', 'click', () => { void action(async () => { const [tab] = await browser.tabs.query({ active: true, currentWindow: true }); if (!tab?.url) throw new Error('Não foi possível ler esta aba. Cole o link do vídeo.'); url.value = tab.url; analysis = undefined; element('selection').hidden = true; }); });
  on('url', 'input', () => { analysis = undefined; element('selection').hidden = true; });
  on('item', 'change', () => renderQuality());
  on('mode', 'change', () => renderQuality());
  on('quality', 'change', () => renderQuality(quality.value));
  on('more-formats', 'click', () => { void action(async () => {
    const item = current(); if (!item?.formatsCursor || !analysis) return;
    const selected = quality.value;
    const result = await client.request('listFormats', { analysisId: analysis.analysisId, itemId: item.itemId, cursor: item.formatsCursor });
    if (result.analysisId !== analysis.analysisId || result.itemId !== item.itemId) throw new DesktopError('INVALID_MESSAGE', 'As qualidades não pertencem a este vídeo.');
    item.videoChoices = [...item.videoChoices, ...result.choices.filter(c => !item.videoChoices.some(old => old.optionId === c.optionId))];
    item.formatsCursor = result.nextCursor; renderQuality(selected);
  }); });
  on('enqueue', 'click', () => { void action(async () => {
    const item = current(); if (!analysis || !item || !snapshot || !quality.value) return;
    const common = { requestId: crypto.randomUUID(), analysisId: analysis.analysisId, itemId: item.itemId, destinationId: snapshot.destination.destinationId };
    const payload: EnqueuePayload = mode.value === 'mp3' ? { ...common, mode: 'mp3', bitrateKbps: Number(quality.value) as MP3Bitrate } : { ...common, mode: 'video', videoOptionId: quality.value };
    // Persist the exact user intent before IPC; a lost acknowledgment must reuse it.
    await browser.storage.local.set({ [PENDING_KEY]: payload }); pending = payload;
    await sendPending();
  }); });
  on('retry-enqueue', 'click', () => { void action(sendPending); });
  on('choose-destination', 'click', () => { void action(async () => { const result = await client.request('chooseDestination', {}); if (!result.canceled && snapshot) { snapshot.destination = result; renderSnapshot(); } }); });
  const open = () => { void action(async () => { await client.request('openApp', {}); }); };
  on('open-app', 'click', open); on('brand', 'click', event => { event.preventDefault(); open(); });
  on('continue', 'click', () => { void action(async () => { await client.request('continue', {}); feedback('Fila liberada para continuar.'); }); });
  on('reconnect', 'click', () => { client.disconnect(); void refresh(); });
  async function initialize(): Promise<void> {
    busy = true; controls();
    try {
      const saved = (await browser.storage.local.get(PENDING_KEY))[PENDING_KEY];
      if (saved) { const request = desktopRequest({ version: 2, id: 'restore', method: 'enqueue', payload: saved }); pending = request.payload as EnqueuePayload; feedback('Um envio anterior aguarda confirmação. Use “Confirmar envio pendente” para consultar/enviar a mesma tarefa.'); }
    } catch (error) { feedback('Não foi possível recuperar o envio pendente. Abra o app para conferir a fila antes de adicionar outro.', true); stopped = true; }
    finally { busy = false; controls(); }
    if (stopped) return;
    await refresh();
    const poll = async () => { if (stopped) return; if (connected && !busy) await refresh(); timer = setTimeout(() => { void poll(); }, snapshot?.readiness === 'preparing' ? 1000 : 2500); };
    timer = setTimeout(() => { void poll(); }, 1000);
  }
  void initialize();
  return () => { stopped = true; if (timer) clearTimeout(timer); client.disconnect(); for (const remove of listeners) remove(); };
}
export function describeVideo(choice: VideoChoice): string {
  return qualityName(choice) + (!choice.hasAudio && !choice.audioUnknown ? ' · sem som' : '');
}
const browser = browserAPI();
if (browser && typeof document !== 'undefined') {
  const unmount = mountDesktopPopup(document, browser);
  window.addEventListener('pagehide', unmount, { once: true });
}
