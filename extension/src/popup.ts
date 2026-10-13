import { decodeMessage, decodeRequest, ProtocolDecodeError, type ErrorCode, type JobState, type MP3Bitrate, type NativeClient, type Operation, type PayloadByOperation, type Response, type VideoChoice } from './protocol.js';
import { createInitialState, isTerminal, reduceState, selectItem, startPayload, type AppState } from './state.js';
import { NativeTransportError, requestId } from './controller.js';
import { browserAPI } from './browser.js';
import { PopupNativeClient } from './popup-client.js';

export interface PopupClient extends NativeClient {
  snapshot?(): Promise<AppState>; subscribeState?(listener: (state: AppState) => void): () => void;
  reconnect?(): Promise<AppState>; clearHistory?(): void; close?(): void;
}
export interface PopupOptions { client: PopupClient; currentTab(): Promise<string | undefined>; initialState?: AppState; installer?: { available(): Promise<boolean>; download(): Promise<void> } }
const stateLabels: Record<JobState, string> = { analyzing: 'Analisando', ready: 'Pronto', downloading: 'Baixando', muxing: 'Juntando áudio e vídeo', converting: 'Convertendo para MP3', saving: 'Salvando', completed: 'Concluído', canceling: 'Cancelando', canceled: 'Cancelado', failed: 'Falhou', interrupted: 'Interrompido' };
const errorLabels: Record<ErrorCode, string> = {
  INVALID_MESSAGE: 'Use um link HTTPS de YouTube, Instagram, TikTok ou X, sem credenciais.', MESSAGE_TOO_LARGE: 'O auxiliar enviou dados demais. Tente analisar novamente.', INCOMPATIBLE_PROTOCOL: 'Esta extensão e o auxiliar usam versões incompatíveis. Instale o pacote correspondente.', UNKNOWN_OPERATION: 'O auxiliar não reconheceu a operação. Confira a versão do pacote.', INVALID_SELECTION: 'A opção escolhida não está mais disponível. Analise o link novamente.', SESSION_ENDED: 'A conexão anterior terminou. Verifique o resultado antes de criar outra tarefa.', ANALYSIS_EXPIRED: 'A análise expirou. Analise o link novamente.', INVALID_CURSOR: 'Não foi possível carregar os formatos. Analise o link novamente.', BUSY: 'Já existe uma tarefa ativa. Aguarde ou cancele antes de iniciar outra.', REQUEST_CONFLICT: 'A identidade desta solicitação já foi usada com outra seleção.', UNKNOWN_JOB: 'A tarefa não foi encontrada. Verifique a conexão e o histórico.', DEPENDENCY_UNAVAILABLE: 'Falta uma dependência no auxiliar. Confira a instalação do pacote Dwnl.', MP3_UNAVAILABLE: 'O encoder MP3 não está disponível neste pacote.', NO_AUDIO: 'Este conteúdo não tem áudio disponível para MP3.', ACCESS_RESTRICTED: 'Este conteúdo exige acesso restrito. O Dwnl não solicita login nem contorna bloqueios.', NETWORK_ERROR: 'O auxiliar ou a rede não respondeu. Verifique a tarefa antes de tentar novamente.', NETWORK_POLICY_BLOCKED: 'A extração está bloqueada pela proteção de rede do motor.', DESTINATION_ERROR: 'Não foi possível gravar na pasta escolhida. Escolha outra pasta.', DISK_FULL: 'O disco está cheio. Libere espaço ou escolha outra pasta.', ENGINE_ERROR: 'O motor não conseguiu processar este conteúdo.', CANCEL_FAILED: 'O encerramento da tarefa não foi confirmado. Verifique a tarefa.', INVALID_OUTPUT: 'O arquivo não passou na verificação. A tarefa não foi concluída.', INTERNAL_ERROR: 'O auxiliar encontrou um erro interno. Verifique a tarefa antes de tentar novamente.',
};
export function formatBytes(bytes?: number, estimated = false): string {
  if (bytes === undefined) return 'tamanho desconhecido';
  const units = ['B', 'KB', 'MB', 'GB']; let value = bytes; let index = 0;
  while (value >= 1000 && index < units.length - 1) { value /= 1000; index++; }
  return `${estimated ? '≈ ' : ''}${new Intl.NumberFormat('pt-BR', { maximumFractionDigits: index > 0 ? 1 : 0 }).format(value)} ${units[index]}${estimated ? ' estimados' : ''}`;
}
function choiceLabel(choice: VideoChoice): string {
  const resolution = choice.height ? `${choice.height}p${choice.width ? ` (${choice.width} × ${choice.height})` : ''}` : 'Resolução desconhecida';
  return `${resolution} · ${choice.container.toUpperCase()} · ${formatBytes(choice.sizeBytes, choice.sizeEstimated)}`;
}
function formatDuration(seconds?: number): string {
  return seconds === undefined ? 'Duração desconhecida' : `${Math.floor(seconds / 60)} min ${Math.floor(seconds % 60).toString().padStart(2, '0')} s`;
}
export function mountPopup(document: Document, options: PopupOptions): { ready: Promise<void>; dispose(): void } {
  let state = options.initialState ?? createInitialState();
  let pending: Operation | 'tab' | 'reconnect' | 'installer' | undefined;
  let closed = false;
  let installerAvailable = false;
  let installerInstruction: string | undefined;
  const cleanups: (() => void)[] = [];
  const el = <T extends HTMLElement>(id: string): T => {
    const element = document.getElementById(id); if (!element) throw new Error(`Popup element missing: ${id}`); return element as T;
  };
  const text = (id: string, value: string) => { el(id).textContent = value; };
  const hide = (id: string, hidden: boolean) => { el(id).hidden = hidden; };
  const disable = (id: string, disabled: boolean) => { el<HTMLButtonElement>(id).disabled = disabled; };
  const listen = (id: string, name: string, handler: (event: Event) => void) => { el(id).addEventListener(name, handler); cleanups.push(() => el(id).removeEventListener(name, handler)); };
  const fillOptions = (id: string, values: { value: string; label: string }[], selected = '') => {
    const select = el<HTMLSelectElement>(id);
    const signature = JSON.stringify(values);
    if (select.dataset.options !== signature) {
      select.replaceChildren(...values.map(value => { const option = document.createElement('option'); option.value = value.value; option.textContent = value.label; return option; }));
      select.dataset.options = signature;
    }
    select.value = selected;
  };
  function render(): void {
    if (closed) return;
    const item = state.analysis?.items.find(value => value.itemId === state.selectedItemId);
    const active = !!state.job && !isTerminal(state.job.state);
    const connected = state.connection === 'connected';
    const networkBlocked = connected && state.handshake?.capabilities.networkAccess === false;
    const connectionLabels = { connecting: 'Conectando…', connected: 'Auxiliar conectado', 'missing-host': 'Auxiliar ausente', incompatible: 'Versão incompatível', disconnected: 'Desconectado', error: 'Erro de conexão' };
    text('connection', connectionLabels[state.connection]); el('connection').dataset.state = state.connection;
    hide('onboarding', connected || state.connection === 'connecting');
    text('onboarding-title', state.connection === 'incompatible' ? 'Atualize o pacote Dwnl' : state.connection === 'missing-host' ? 'Falta o auxiliar local' : 'Reconecte o auxiliar');
    text('onboarding-detail', state.connection === 'incompatible' ? errorLabels.INCOMPATIBLE_PROTOCOL : state.connection === 'missing-host' ? 'Instale o auxiliar uma vez para salvar arquivos e converter áudio no seu computador.' : 'A conexão terminou. O resultado anterior precisa ser verificado; nenhuma tarefa será reiniciada automaticamente.');
    text('local-installer', installerInstruction ?? (installerAvailable ? 'Baixe o instalador incluído e abra-o manualmente. Depois, tente conectar novamente.' : 'O instalador não está incluído nesta extensão. Quando houver um instalador validado no pacote local, abra-o e tente conectar novamente. Consulte as instruções do protótipo.'));
    hide('install', !installerAvailable); disable('install', !!pending); disable('reconnect', !!pending);
    hide('network-notice', !networkBlocked);
    hide('error', !state.error); text('error', state.error ? errorLabels[state.error.code] : '');
    disable('current-tab', pending === 'tab');
    disable('analyze', !connected || !!pending || active || networkBlocked || state.confirmation === 'checking');
    text('analyze', pending === 'analyze' ? 'Analisando…' : 'Analisar →');
    hide('selection', !item);
    if (item) {
      fillOptions('item', state.analysis!.items.map(value => ({ value: value.itemId, label: value.title })), item.itemId);
      hide('item', state.analysis!.items.length === 1); hide('item-label', state.analysis!.items.length === 1);
      text('media-title', item.title); text('media-detail', `${formatDuration(item.durationSeconds)} · ${item.hasAudio ? 'Com áudio' : 'Sem áudio'}`);
      const audioAvailable = item.hasAudio && !!state.handshake?.capabilities.mp3;
      disable('mode-mp3', !audioAvailable || !!pending);
      disable('mode-video', !state.handshake?.capabilities.video || !!pending);
      el('mode-video').setAttribute('aria-pressed', String(state.mode === 'video')); el('mode-mp3').setAttribute('aria-pressed', String(state.mode === 'mp3'));
      hide('mp3-reason', audioAvailable); text('mp3-reason', item.hasAudio ? 'MP3 indisponível: confira o encoder e as dependências do pacote.' : 'Este vídeo não tem áudio. MP3 indisponível.');
      hide('video-settings', state.mode !== 'video'); hide('mp3-settings', state.mode !== 'mp3');
      fillOptions('quality', [{ value: '', label: 'Selecione uma qualidade' }, ...item.videoChoices.map(choice => ({ value: choice.optionId, label: choiceLabel(choice) }))], state.videoOptionId);
      const choice = item.videoChoices.find(value => value.optionId === state.videoOptionId);
      const expired = new Date(state.analysis!.expiresAt).getTime() <= Date.now();
      text('quality-note', expired ? errorLabels.ANALYSIS_EXPIRED : choice ? `${choice.container === 'webm' ? 'WebM' : choice.container.toUpperCase()} · ${choice.videoCodec}${choice.audioCodec ? ` / ${choice.audioCodec}` : ''}. ${choice.hasAudio ? 'Sem redução silenciosa de resolução.' : 'A saída não contém áudio.'}` : item.videoChoices.length ? 'Não há qualidade conhecida até 1080p nesta lista: escolha uma opção para continuar.' : 'Nenhum formato de vídeo disponível.');
      if (item.omittedMetadata || state.analysis!.omittedMetadata) text('quality-note', `${el('quality-note').textContent} Alguns metadados não foram fornecidos.`);
      el<HTMLSelectElement>('bitrate').value = String(state.bitrateKbps);
      hide('more-formats', !item.formatsCursor); disable('more-formats', !!pending);
      text('destination', state.destination?.label ?? 'Pasta não confirmada'); disable('choose-destination', !connected || !!pending || active);
      disable('start', !!pending || networkBlocked || !startPayload(state)); text('start', pending === 'start' ? 'Confirmando início…' : state.mode === 'mp3' ? 'Criar MP3 ↧' : 'Salvar vídeo ↧');
    }
    const job = state.job;
    hide('job', !job && !state.pendingStartRequestId);
    if (job || state.pendingStartRequestId) {
      const confirmed = state.confirmation === 'confirmed';
      text('job-state', !confirmed ? state.confirmation === 'checking' ? 'Verificando tarefa' : job?.state === 'interrupted' ? 'Interrompido' : 'Resultado não confirmado' : job ? stateLabels[job.state] : 'Início não confirmado');
      const progress = el<HTMLProgressElement>('job-progress');
      const measuredPercent = confirmed ? job?.progress?.percent : undefined;
      if (measuredPercent === undefined) progress.removeAttribute('value'); else progress.value = measuredPercent;
      text('job-percent', measuredPercent === undefined ? '' : `${new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 }).format(measuredPercent)}%`);
      hide('job-progress', !!job && isTerminal(job.state) && confirmed);
      let detail = 'Aguardando dados do auxiliar. Nenhum percentual estimado.';
      if (!confirmed && state.confirmation === 'checking') detail = 'O último estado local ainda precisa de confirmação do auxiliar.';
      else if (!confirmed) detail = 'A tarefa anterior foi interrompida ou seu resultado não pôde ser confirmado. Nenhum trabalho foi retomado.';
      else if (job?.state === 'canceling') detail = 'Aguardando o encerramento de todos os processos da tarefa.';
      else if (job?.state === 'canceled') detail = 'Encerramento confirmado pelo auxiliar.';
      else if (job?.state === 'completed') detail = 'Arquivo verificado pelo auxiliar e salvo na pasta escolhida.';
      else if (job?.error) detail = errorLabels[job.error.code];
      else if (job?.state === 'interrupted') detail = 'A conexão terminou. Inicie uma nova tarefa somente por uma nova ação.';
      else if (job?.progress?.totalBytes !== undefined) detail = `${formatBytes(job.progress.completedBytes)} de ${formatBytes(job.progress.totalBytes)} nesta etapa.`;
      else if (job?.progress?.completedBytes !== undefined) detail = `${formatBytes(job.progress.completedBytes)} recebidos · total desconhecido.`;
      else if (job?.progress?.durationSeconds !== undefined) detail = `${formatDuration(job.progress.processedSeconds)} de ${formatDuration(job.progress.durationSeconds)} nesta etapa.`;
      text('job-detail', detail);
      hide('completed-file', !confirmed || !job?.completedFile);
      text('completed-file', confirmed && job?.completedFile ? `${job.completedFile.path} · ${job.completedFile.format.toUpperCase()} · ${formatBytes(job.completedFile.sizeBytes)}${job.completedFile.mp3BitrateKbps ? ` · saída ${job.completedFile.mp3BitrateKbps} kbps` : ''}` : '');
      hide('cancel', !confirmed || !job || isTerminal(job.state)); disable('cancel', !!pending || job?.state === 'canceling'); text('cancel', pending === 'cancel' ? 'Solicitando cancelamento…' : job?.state === 'canceling' ? 'Cancelamento em andamento' : 'Cancelar tarefa');
      hide('verify-job', confirmed && state.connection === 'connected'); disable('verify-job', !!pending);
    }
    text('history-count', String(state.history.length)); disable('clear-history', state.history.length === 0);
    el('history-list').replaceChildren(...state.history.slice(0, 20).map(record => { const row = document.createElement('li'); row.textContent = `${stateLabels[record.state]} · ${record.completedFile?.path ?? record.jobId} · registro local`; return row; }));
  }
  function applyFailure(error: unknown): void {
    if (error instanceof NativeTransportError && error.reason !== 'timeout') state = { ...state, connection: error.reason };
    else state = { ...state, error: { code: error instanceof ProtocolDecodeError ? error.code : 'NETWORK_ERROR', message: 'Erro', retryable: true } };
    render();
  }
  async function request<O extends Operation>(operation: O, payload: PayloadByOperation[O], id = requestId()): Promise<Response | undefined> {
    pending = operation; state = { ...state, error: undefined }; render();
    try { const response = options.client.request(operation, payload, id); const message = decodeMessage(await response); if (message.kind !== 'response') throw new Error(); state = reduceState(state, message); return message; }
    catch (error) { applyFailure(error); return undefined; }
    finally { pending = undefined; render(); }
  }
  cleanups.push(options.client.subscribe(message => { try { state = reduceState(state, decodeMessage(message)); render(); } catch { applyFailure(new NativeTransportError('error')); } }));
  if (options.client.subscribeState) cleanups.push(options.client.subscribeState(snapshot => {
    const sameAnalysis = state.analysis && snapshot.analysis?.analysisId === state.analysis.analysisId && snapshot.hostSessionId === state.hostSessionId;
    state = sameAnalysis ? { ...snapshot, selectedItemId: state.selectedItemId, videoOptionId: state.videoOptionId, mode: state.mode, bitrateKbps: state.bitrateKbps } : snapshot;
    render();
  }));
  listen('current-tab', 'click', async () => {
    if (pending === 'tab') return; pending = 'tab'; render();
    try { const url = await options.currentTab(); if (!url) throw new ProtocolDecodeError('INVALID_MESSAGE', 'No current tab URL'); el<HTMLInputElement>('url').value = url; el<HTMLInputElement>('url').focus(); }
    catch (error) { applyFailure(error); } finally { pending = undefined; render(); }
  });
  listen('source-form', 'submit', async event => {
    event.preventDefault(); if (el<HTMLButtonElement>('analyze').disabled) return;
    const url = el<HTMLInputElement>('url').value.trim();
    try { decodeRequest({ protocolVersion: 1, requestId: 'validate-url', hostSessionId: state.hostSessionId, operation: 'analyze', payload: { url } }); }
    catch (error) { applyFailure(error); return; }
    await request('analyze', { url });
  });
  listen('item', 'change', () => { state = selectItem(state, el<HTMLSelectElement>('item').value); render(); });
  listen('quality', 'change', () => { state = { ...state, videoOptionId: el<HTMLSelectElement>('quality').value || undefined }; render(); });
  listen('mode-video', 'click', () => { if (el<HTMLButtonElement>('mode-video').disabled) return; state = { ...state, mode: 'video' }; render(); });
  listen('mode-mp3', 'click', () => { if (el<HTMLButtonElement>('mode-mp3').disabled) return; state = { ...state, mode: 'mp3' }; render(); });
  listen('bitrate', 'change', () => { const bitrate = Number(el<HTMLSelectElement>('bitrate').value); if ([128, 192, 320].includes(bitrate)) state = { ...state, bitrateKbps: bitrate as MP3Bitrate }; render(); });
  listen('choose-destination', 'click', async () => { if (!el<HTMLButtonElement>('choose-destination').disabled) await request('chooseDestination', { userGesture: true }); });
  listen('more-formats', 'click', async () => { const item = state.analysis?.items.find(value => value.itemId === state.selectedItemId); if (!state.analysis || !item?.formatsCursor || pending) return; await request('listFormats', { analysisId: state.analysis.analysisId, itemId: item.itemId, cursor: item.formatsCursor }); });
  listen('start', 'click', async () => { const payload = startPayload(state); if (!payload || pending || el<HTMLButtonElement>('start').disabled) return; const id = requestId(); state = { ...state, pendingStartRequestId: id, pendingStartHostSessionId: state.hostSessionId, confirmation: 'checking' }; await request('start', payload, id); });
  listen('cancel', 'click', async () => { if (!state.job || pending || el<HTMLButtonElement>('cancel').disabled) return; await request('cancel', { jobId: state.job.jobId }); });
  async function reconnect(): Promise<void> {
    if (pending) return; pending = 'reconnect'; render();
    try {
      if (options.client.reconnect) state = await options.client.reconnect();
      else { pending = undefined; await request('handshake', { extensionVersion: '0.1.0' }); await request('status', {}); }
    } catch (error) { applyFailure(error); } finally { pending = undefined; render(); }
  }
  listen('reconnect', 'click', reconnect); listen('verify-job', 'click', reconnect);
  listen('clear-history', 'click', () => { if (options.client.clearHistory) options.client.clearHistory(); else { state = { ...state, history: [], ...(state.job && isTerminal(state.job.state) ? { job: undefined } : {}) }; render(); } });
  listen('install', 'click', async () => { if (!installerAvailable || !options.installer || pending) return; pending = 'installer'; render(); try { await options.installer.download(); installerInstruction = 'Abra o instalador baixado para instalar o auxiliar. Depois, tente conectar novamente.'; } catch { installerInstruction = 'O Chrome não conseguiu copiar o recurso. Abra manualmente installer/Dwnl-Setup.exe no pacote local.'; } finally { pending = undefined; render(); } });
  const timer = setInterval(render, 1000); cleanups.push(() => clearInterval(timer));
  render();
  const ready = (async () => {
    if (options.client.snapshot) { try { state = await options.client.snapshot(); render(); } catch (error) { applyFailure(error); } }
    else { await request('handshake', { extensionVersion: '0.1.0' }); if (state.connection === 'connected') await request('status', {}); }
    if (options.installer) { try { installerAvailable = await options.installer.available(); } catch { installerAvailable = false; } render(); }
  })();
  return { ready, dispose() { if (closed) return; closed = true; cleanups.splice(0).forEach(cleanup => cleanup()); options.client.close?.(); } };
}

const browser = browserAPI();
if (browser && typeof document !== 'undefined') {
  const connectPopup = () => browser.runtime.connect({ name: 'dwnl-popup-v1' });
  const client = new PopupNativeClient(connectPopup(), connectPopup);
  const popup = mountPopup(document, { client, currentTab: async () => (await browser.tabs.query({ active: true, currentWindow: true }))[0]?.url,
    installer: {
      available: async () => { if (!browser.runtime.getManifest().permissions?.includes('downloads') || !browser.downloads) return false; const resource = await fetch(browser.runtime.getURL('installer/Dwnl-Setup.exe'), { method: 'HEAD' }); return resource.ok; },
      download: async () => { if (!browser.downloads) throw new Error(); await browser.downloads.download({ url: browser.runtime.getURL('installer/Dwnl-Setup.exe'), filename: 'Dwnl-Setup.exe', saveAs: true }); },
    } });
  window.addEventListener('pagehide', () => popup.dispose(), { once: true });
}
