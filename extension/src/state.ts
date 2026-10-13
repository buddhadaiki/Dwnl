import { decodeMessage, decodeRequest, type Analysis, type Destination, type HandshakeResult, type JobEvent, type JobSnapshot, type JobState, type MP3Bitrate, type ProtocolError, type Response, type StartPayload, type VideoChoice } from './protocol.js';
export type ConnectionState = 'connecting' | 'connected' | 'missing-host' | 'incompatible' | 'disconnected' | 'error';
export interface AppState {
  connection: ConnectionState; hostSessionId?: string; handshake?: HandshakeResult; analysis?: Analysis;
  selectedItemId?: string; videoOptionId?: string; mode: 'video' | 'mp3'; bitrateKbps: MP3Bitrate;
  destination?: Destination; job?: JobSnapshot; history: JobSnapshot[]; confirmation: 'confirmed' | 'checking' | 'unconfirmed';
  pendingStartRequestId?: string; pendingStartHostSessionId?: string; error?: ProtocolError;
  clearedJob?: Pick<JobSnapshot, 'hostSessionId' | 'jobId'>;
}
export function createInitialState(): AppState { return { connection: 'connecting', mode: 'video', bitrateKbps: 192, history: [], confirmation: 'unconfirmed' }; }
export function isTerminal(state: JobState): boolean { return ['completed', 'canceled', 'failed', 'interrupted'].includes(state); }
export function chooseBestVideo(choices: VideoChoice[]): VideoChoice | undefined {
  const compatibility = (choice: VideoChoice) => choice.container === 'mp4' && /^(h264|avc1)/i.test(choice.videoCodec) && (!choice.hasAudio || /^(aac|mp4a)/i.test(choice.audioCodec ?? '')) ? 1 : 0;
  return choices.filter(choice => choice.height !== undefined && choice.height <= 1080)
    .sort((a, b) => b.height! - a.height! || (b.width ?? 0) - (a.width ?? 0) || compatibility(b) - compatibility(a))[0];
}
export function selectItem(state: AppState, itemId: string): AppState {
  const item = state.analysis?.items.find(value => value.itemId === itemId);
  if (!item) return state;
  return { ...state, selectedItemId: itemId, videoOptionId: chooseBestVideo(item.videoChoices)?.optionId, mode: item.hasAudio ? state.mode : 'video' };
}
function applySnapshot(state: AppState, snapshot: JobSnapshot, authoritative = false): AppState {
  if (isTerminal(snapshot.state) && state.clearedJob?.hostSessionId === snapshot.hostSessionId && state.clearedJob.jobId === snapshot.jobId) return { ...state, job: state.job?.jobId === snapshot.jobId ? undefined : state.job, confirmation: state.pendingStartRequestId ? 'checking' : 'confirmed' };
  const previous = state.job;
  if (previous?.hostSessionId === snapshot.hostSessionId && previous.jobId === snapshot.jobId && state.confirmation === 'confirmed') {
    if (snapshot.sequence < previous.sequence || (!authoritative && snapshot.sequence === previous.sequence)) return state;
    if (isTerminal(previous.state) && !isTerminal(snapshot.state)) return state;
    if (previous.state === 'canceling' && !isTerminal(snapshot.state) && snapshot.state !== 'canceling') return state;
  }
  const history = isTerminal(snapshot.state)
    ? [snapshot, ...state.history.filter(record => record.hostSessionId !== snapshot.hostSessionId || record.jobId !== snapshot.jobId)].slice(0, 20)
    : state.history.slice(0, 19);
  return { ...state, job: snapshot, history, pendingStartRequestId: undefined, pendingStartHostSessionId: undefined, confirmation: 'confirmed' };
}
export function reduceState(state: AppState, message: Response | JobEvent): AppState {
  if (message.kind === 'event') {
    if (message.hostSessionId !== state.hostSessionId || message.jobId !== state.job?.jobId || message.hostSessionId !== state.job?.hostSessionId) return state;
    const { protocolVersion: _, kind: __, ...details } = message;
    return applySnapshot(state, { ...details, startRequestId: state.job.startRequestId });
  }
  if (message.operation !== 'handshake' && message.hostSessionId !== state.hostSessionId) return state;
  if (message.error) return { ...state, error: message.error, ...(message.operation === 'start' ? { pendingStartRequestId: undefined } : {}), ...(message.operation === 'handshake' ? { connection: message.error.code === 'INCOMPATIBLE_PROTOCOL' ? 'incompatible' as const : 'error' as const } : {}) };
  switch (message.operation) {
    case 'handshake': {
      const changedSession = state.hostSessionId !== message.hostSessionId;
      return { ...state, connection: 'connected', hostSessionId: message.hostSessionId, handshake: message.result, destination: message.result.defaultDestination, error: undefined,
        ...(changedSession ? { analysis: undefined, selectedItemId: undefined, videoOptionId: undefined, ...(state.job ? { confirmation: 'checking' as const } : {}) } : {}) };
    }
    case 'analyze': return selectItem({ ...state, analysis: message.result, error: undefined }, message.result.items[0].itemId);
    case 'listFormats': {
      if (message.result.analysisId !== state.analysis?.analysisId) return state;
      const items = state.analysis.items.map(item => {
        if (item.itemId !== message.result.itemId) return item;
        const merged = new Map(item.videoChoices.map(choice => [choice.optionId, choice]));
        message.result.choices.forEach(choice => merged.set(choice.optionId, choice));
        return { ...item, videoChoices: [...merged.values()], formatsCursor: message.result.nextCursor, omittedMetadata: item.omittedMetadata || message.result.omittedMetadata };
      });
      return { ...state, analysis: { ...state.analysis, items } };
    }
    case 'chooseDestination': return message.result.canceled ? state : { ...state, destination: { destinationId: message.result.destinationId, label: message.result.label }, error: undefined };
    case 'start': {
      if (isTerminal(message.result.state)) {
        if (state.confirmation === 'confirmed' && state.job?.startRequestId === message.result.startRequestId && state.job.hostSessionId === message.hostSessionId && isTerminal(state.job.state)) return state;
        return { ...state, pendingStartRequestId: message.result.startRequestId, pendingStartHostSessionId: message.hostSessionId, confirmation: 'checking' };
      }
      return applySnapshot(state, { hostSessionId: message.hostSessionId, ...message.result });
    }
    case 'status': return message.result.snapshot ? applySnapshot(state, message.result.snapshot, true) : state.job?.hostSessionId === state.hostSessionId || state.pendingStartHostSessionId === state.hostSessionId ? { ...state, job: undefined, pendingStartRequestId: undefined, pendingStartHostSessionId: undefined, confirmation: 'confirmed' } : state;
    case 'cancel': return applySnapshot(state, message.result.snapshot, true);
    case 'reconcile': {
      const previousSession = state.pendingStartHostSessionId ?? state.job?.hostSessionId;
      if (previousSession && previousSession !== message.result.previousHostSessionId) return state;
      const snapshot = message.result.snapshot ?? (!state.pendingStartRequestId && state.job ? { hostSessionId: state.job.hostSessionId, jobId: state.job.jobId, startRequestId: state.job.startRequestId, sequence: state.job.sequence + 1, state: 'interrupted' as const } : undefined);
      const reconciled = snapshot ? applySnapshot(state, snapshot, true) : { ...state, ...(state.pendingStartRequestId ? { job: undefined, error: { code: 'SESSION_ENDED' as const, message: 'Previous start could not be confirmed', retryable: true } } : {}) };
      return { ...reconciled, confirmation: message.result.outcome === 'confirmed' ? 'confirmed' : 'unconfirmed', pendingStartRequestId: undefined, pendingStartHostSessionId: undefined };
    }
  }
}
export function startPayload(state: AppState): StartPayload | undefined {
  if (!state.analysis || !state.hostSessionId || !state.destination || state.connection !== 'connected' || state.pendingStartRequestId || state.confirmation === 'checking' || (state.job && !isTerminal(state.job.state))) return undefined;
  const item = state.analysis.items.find(value => value.itemId === state.selectedItemId);
  if (!item || !state.handshake?.capabilities.video && state.mode === 'video' || !state.handshake?.capabilities.mp3 && state.mode === 'mp3') return undefined;
  const payload: StartPayload = state.mode === 'mp3'
    ? { analysisId: state.analysis.analysisId, itemId: item.itemId, mode: 'mp3', bitrateKbps: state.bitrateKbps, destinationId: state.destination.destinationId }
    : { analysisId: state.analysis.analysisId, itemId: item.itemId, mode: 'video', videoOptionId: state.videoOptionId ?? '', destinationId: state.destination.destinationId };
  try { decodeRequest({ protocolVersion: 1, hostSessionId: state.hostSessionId, requestId: 'validation', operation: 'start', payload }, { hostSessionId: state.hostSessionId, analysis: state.analysis }); }
  catch { return undefined; }
  return payload;
}
function cacheSnapshot(snapshot: JobSnapshot): JobSnapshot {
  return { ...snapshot, ...(snapshot.error ? { error: { ...snapshot.error, message: snapshot.error.code } } : {}) };
}
export function serializeCache(state: AppState): unknown {
  const limit = state.pendingStartRequestId || state.job && !isTerminal(state.job.state) ? 19 : 20;
  return { version: 1, ...(state.job ? { job: cacheSnapshot(state.job) } : {}), ...(state.pendingStartRequestId && state.pendingStartHostSessionId ? { pendingStart: { hostSessionId: state.pendingStartHostSessionId, startRequestId: state.pendingStartRequestId } } : {}), history: state.history.slice(0, limit).map(cacheSnapshot) };
}
function parseCachedSnapshot(value: unknown): JobSnapshot | undefined {
  try {
    const snapshot = value as JobSnapshot;
    const message = decodeMessage({ protocolVersion: 1, kind: 'response', operation: 'status', requestId: 'cache', hostSessionId: snapshot.hostSessionId, result: { snapshot } });
    if (message.kind === 'response' && message.operation === 'status' && message.result?.snapshot) return cacheSnapshot(message.result.snapshot);
  } catch { /* Corrupt or obsolete cache has no authority. */ }
  return undefined;
}
export function restoreCache(cache: unknown): AppState {
  const state = createInitialState();
  if (typeof cache !== 'object' || !cache || (cache as { version?: unknown }).version !== 1) return state;
  const raw = cache as { job?: unknown; history?: unknown; pendingStart?: unknown };
  const job = parseCachedSnapshot(raw.job);
  const history = Array.isArray(raw.history) ? raw.history.slice(0, 20).map(parseCachedSnapshot).filter((snapshot): snapshot is JobSnapshot => !!snapshot && isTerminal(snapshot.state)) : [];
  const pending = raw.pendingStart as { hostSessionId?: unknown; startRequestId?: unknown } | undefined;
  const validId = (value: unknown): value is string => typeof value === 'string' && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(value);
  const pendingState = pending && validId(pending.hostSessionId) && validId(pending.startRequestId) ? { pendingStartHostSessionId: pending.hostSessionId, pendingStartRequestId: pending.startRequestId } : {};
  return { ...state, job, history: history.slice(0, pendingState.pendingStartRequestId || job && !isTerminal(job.state) ? 19 : 20), ...pendingState, confirmation: job || pendingState.pendingStartRequestId ? 'checking' : 'unconfirmed' };
}
