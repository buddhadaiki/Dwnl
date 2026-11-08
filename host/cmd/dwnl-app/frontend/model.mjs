export const terminalStates = new Set(['completed', 'canceled', 'failed']);
export const activeStates = new Set(['analyzing', 'downloading', 'muxing', 'converting', 'saving', 'canceling']);
export const stateLabels = {queued:'Na fila',analyzing:'Preparando',downloading:'Baixando',muxing:'Finalizando',converting:'Convertendo para MP3',saving:'Salvando',completed:'Concluído',canceling:'Cancelando',canceled:'Cancelado',failed:'Falhou',interrupted:'Interrompido'};
// The Downloads view keeps the latest finished items in reach of "Mostrar na pasta".
export const recentLimit = 5;

// The resolution people name, as the engine's namedSize computes it: 1080 for
// 1920×1080, a 1080×1920 Short or a 1920×800 crop; 720 and up snap to the
// standard names.
export function qualityPixels(choice) {
  const sides = [choice.width, choice.height].filter(n => Number.isFinite(n) && n > 0);
  if (!sides.length) return 0;
  const size = sides.length === 2 ? Math.max(Math.min(...sides), Math.max(...sides) * 9 / 16) : sides[0];
  return [2160, 1440, 1080, 720].find(standard => size >= standard) ?? Math.round(size);
}

export function defaultChoice(choices = []) {
  // Portrait Shorts use their shorter dimension, just like landscape videos.
  const ranked = choices.map((choice, index) => ({choice, index, quality: qualityPixels(choice)}));
  const bounded = ranked.filter(x => x.quality > 0 && x.quality <= 1080);
  const pool = bounded.length ? bounded : ranked.filter(x => x.quality > 0).sort((a, b) => a.quality - b.quality).slice(0, 1);
  pool.sort((a, b) => b.quality - a.quality || Number(b.choice.hasAudio) - Number(a.choice.hasAudio) || a.index - b.index);
  return (pool[0]?.choice ?? choices[0])?.optionId ?? '';
}

export function bytes(value) {
  if (!Number.isFinite(value) || value < 0) return '';
  if (value < 1024) return Math.round(value) + ' B';
  const units = ['KB', 'MB', 'GB', 'TB'];
  let amount = value / 1024, unit = 0;
  while (amount >= 1024 && unit < units.length - 1) { amount /= 1024; unit++; }
  return new Intl.NumberFormat('pt-BR', {maximumFractionDigits: amount < 10 ? 1 : 0}).format(amount) + ' ' + units[unit];
}

// Names people know from TVs and phones; codecs and containers stay internal.
export function qualityName(choice) {
  const pixels = qualityPixels(choice);
  return {0:'Qualidade padrão', 2160:'4K', 1080:'1080p Full HD', 720:'720p HD'}[pixels] ?? pixels + 'p';
}

export function clock(duration) {
  if (!Number.isFinite(duration) || duration < 0) return '';
  const seconds = Math.floor(duration);
  const rest = String(Math.floor(seconds % 3600 / 60)) + ':' + String(seconds % 60).padStart(2, '0');
  return seconds >= 3600 ? Math.floor(seconds / 3600) + ':' + rest.padStart(5, '0') : rest;
}

export function choiceLabel(choice) {
  const parts = [qualityName(choice)];
  if (choice.sizeBytes) parts.push((choice.sizeEstimated ? '≈ ' : '') + bytes(choice.sizeBytes));
  if (!choice.hasAudio && !choice.audioUnknown) parts.push('sem som');
  return parts.join(' · ');
}

export function progressPercent(progress) {
  if (!progress) return null;
  let percent = progress.percent;
  if (!Number.isFinite(percent) && progress.totalBytes > 0) percent = progress.completedBytes / progress.totalBytes * 100;
  if (!Number.isFinite(percent) && progress.durationSeconds > 0) percent = progress.processedSeconds / progress.durationSeconds * 100;
  return Number.isFinite(percent) ? Math.min(100, Math.max(0, percent)) : null;
}

export function errorMessage(error) {
  if (typeof error === 'string' && error.trim()) return error;
  if (error?.message) return error.message;
  return 'Não foi possível concluir esta ação. Tente novamente.';
}

export function partitionTasks(tasks = []) {
  return {queue: tasks.filter(t => !terminalStates.has(t.state)), history: tasks.filter(t => terminalStates.has(t.state)).slice().reverse()};
}

// Tasks seen running before that are now completed: the moments worth a toast.
export function newlyCompleted(previous, tasks = []) {
  return newlyFinished(previous, tasks).filter(t => t.state === 'completed');
}

// Tasks seen running before that have now ended in any way: their icon changes.
export function newlyFinished(previous, tasks = []) {
  return tasks.filter(t => terminalStates.has(t.state) && previous.has(t.id) && !terminalStates.has(previous.get(t.id)));
}

// What a view lays out: which rows, in which order and state. Progress alone
// leaves it unchanged, so only a row arriving, leaving or changing shape moves
// the list.
export function listLayout(snapshot, view) {
  const {queue, history} = partitionTasks(snapshot?.tasks);
  const rows = view === 'queue' ? [...queue, ...history.slice(0, recentLimit)] : history;
  return [view, snapshot?.paused ? 'paused' : '', snapshot?.legacyHistory?.length ?? 0, ...rows.map(t => t.id + ':' + t.state + (t.error ? '!' : ''))].join('|');
}

export function appendChoices(existing, incoming) {
  const ids = new Set(existing.map(c => c.optionId));
  return [...existing, ...incoming.filter(c => !ids.has(c.optionId) && ids.add(c.optionId))];
}

export function isVideoURL(value) {
  try { const url = new URL(value); return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password; }
  catch { return false; }
}

export function pollDelay(snapshot, failures = 0, waitingForExtension = false) {
  if (failures) return Math.min(30000, 2000 * 2 ** Math.min(failures, 4));
  // Preparing takes seconds at most; following it closely unlocks Baixar at once.
  if (snapshot?.readiness === 'preparing') return 250;
  if (snapshot?.tasks?.some(t => activeStates.has(t.state))) return 1000;
  return waitingForExtension ? 2000 : 8000;
}

// While the app prepares, its capabilities are not known yet rather than
// missing: a search that finishes first must not pick MP3 or report "no video".
export function capability(snapshot, name) {
  return snapshot?.readiness === 'preparing' || Boolean(snapshot?.capabilities?.[name]);
}

// A failed or canceled download can start over from its link; '' when it cannot.
export function retryLabel(task) {
  if (!isVideoURL(task?.url ?? '')) return '';
  return {failed:'Tentar de novo', canceled:'Baixar de novo'}[task.state] ?? '';
}

// The setup ships the extension; the app reports it only when it is installed.
export function showExtensionCard(snapshot, dismissed) {
  return Boolean(snapshot?.extension && !snapshot.extension.connected && !dismissed);
}

// What the Atualizações row shows after a check. Without a check it only offers one.
export function updateView(info) {
  if (!info) return {status: 'Confira se há uma versão nova no GitHub.', action: 'check', notes: false};
  if (!info.available) return {status: `Você está na versão mais recente (${info.current}).`, action: 'check', notes: false};
  if (info.canInstall) return {status: `Dwnl ${info.latest} disponível.`, action: 'install', notes: true};
  return {status: `Dwnl ${info.latest} disponível. ${info.blocked ?? ''}`.trim(), action: 'check', notes: true};
}

export function downloadInProgress(snapshot) {
  return (snapshot?.tasks ?? []).some(task => activeStates.has(task.state));
}
