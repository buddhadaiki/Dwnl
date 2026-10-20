export const terminalStates = new Set(['completed', 'canceled', 'failed']);
export const activeStates = new Set(['analyzing', 'downloading', 'muxing', 'converting', 'saving', 'canceling']);
export const stateLabels = {queued:'Na fila',analyzing:'Analisando',downloading:'Baixando',muxing:'Unindo vídeo e áudio',converting:'Convertendo áudio',saving:'Salvando',completed:'Concluído',canceling:'Cancelando',canceled:'Cancelado',failed:'Falhou',interrupted:'Interrompido'};

export function qualityPixels(choice) {
  const dimensions = [choice.width, choice.height].filter(n => Number.isFinite(n) && n > 0);
  return dimensions.length ? Math.min(...dimensions) : 0;
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

export function choiceLabel(choice) {
  const dimension = qualityPixels(choice);
  const parts = [dimension ? dimension + 'p' : 'Resolução não informada', choice.container?.toUpperCase()];
  if (!choice.hasAudio) parts.push(choice.audioUnknown ? 'áudio a verificar' : 'sem áudio');
  if (choice.sizeBytes) parts.push((choice.sizeEstimated ? '≈ ' : '') + bytes(choice.sizeBytes));
  return parts.filter(Boolean).join(' · ');
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

export function appendChoices(existing, incoming) {
  const ids = new Set(existing.map(c => c.optionId));
  return [...existing, ...incoming.filter(c => !ids.has(c.optionId) && ids.add(c.optionId))];
}

export function isVideoURL(value) {
  try { const url = new URL(value); return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password; }
  catch { return false; }
}

export function pollDelay(snapshot, failures = 0) {
  if (failures) return Math.min(30000, 2000 * 2 ** Math.min(failures, 4));
  return snapshot?.readiness === 'preparing' || snapshot?.tasks?.some(t => activeStates.has(t.state)) ? 2000 : 8000;
}
