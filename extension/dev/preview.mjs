import { mountPopup } from '../../dist/extension/src/popup.js';
import { NativeTransportError } from '../../dist/extension/src/controller.js';
import { createInitialState, reduceState } from '../../dist/extension/src/state.js';

const popupUrl = new URL('../popup.html', import.meta.url);
const original = await (await fetch(popupUrl)).text();
const popupBody = /<body>([\s\S]*?)<\/body>/.exec(original)[1];
const target = document.getElementById('preview-popup');
const scenario = document.getElementById('scenario');
let mounted;
let fixture;
let open = true;
const baseChoices = [
  { optionId: 'mp41080', container: 'mp4', width: 1920, height: 1080, videoCodec: 'h264', audioCodec: 'aac', hasAudio: true, sizeBytes: 24500000, sizeEstimated: true },
  { optionId: 'webm1080', container: 'webm', width: 1920, height: 1080, videoCodec: 'vp9', audioCodec: 'opus', hasAudio: true, sizeBytes: 18000000, sizeEstimated: true },
  { optionId: 'mp4720', container: 'mp4', width: 1280, height: 720, videoCodec: 'h264', audioCodec: 'aac', hasAudio: true, sizeBytes: 14000000, sizeEstimated: true },
];
class SyntheticTransport {
  state = createInitialState();
  listeners = new Set();
  stateListeners = new Set();
  timers = [];
  constructor(scenario) { this.scenario = scenario; }
  subscribe(listener) { this.listeners.add(listener); return () => this.listeners.delete(listener); }
  subscribeState(listener) { this.stateListeners.add(listener); return () => this.stateListeners.delete(listener); }
  emit(message) { this.state = reduceState(this.state, message); this.stateListeners.forEach(listener => listener(this.state)); this.listeners.forEach(listener => listener(message)); }
  async snapshot() { if (!this.state.hostSessionId) { try { await this.request('handshake', {}, 'preview-handshake'); } catch (error) { this.state.connection = error.reason; } } return this.state; }
  async reconnect() { return this.snapshot(); }
  clearHistory() { this.state.history = []; this.stateListeners.forEach(listener => listener(this.state)); }
  async request(operation, payload, requestId) {
    if (this.scenario === 'missing') throw new NativeTransportError('missing-host');
    if (this.scenario === 'incompatible') throw new NativeTransportError('incompatible');
    let result;
    if (operation === 'handshake') result = { hostVersion: 'prévia', engineVersion: 'sintético', dependencies: [{ name: 'ffmpeg', version: 'prévia', available: true }], capabilities: { video: true, mp3: true, networkAccess: this.scenario !== 'blocked' }, defaultDestination: { destinationId: 'destination-a', label: 'Downloads/Dwnl' } };
    if (operation === 'status') result = { snapshot: this.state.job ?? null };
    if (operation === 'analyze') {
      let videoChoices = structuredClone(baseChoices);
      if (this.scenario === 'high') videoChoices = [{ ...baseChoices[0], optionId: 'video2160', width: 3840, height: 2160 }];
      if (this.scenario === 'unknown') { const { height, width, ...unknown } = baseChoices[0]; videoChoices = [unknown]; }
      if (this.scenario === 'silent') videoChoices = videoChoices.map(({ audioCodec, ...choice }) => ({ ...choice, hasAudio: false }));
      result = { analysisId: 'analysis-preview', hostSessionId: 'session-preview', sourceUrl: payload.url, expiresAt: '2099-01-01T00:00:00Z', omittedMetadata: false,
        items: [{ itemId: 'item-preview', title: 'Um passeio pela cidade · prévia simulada', durationSeconds: 143, hasAudio: this.scenario !== 'silent', videoChoices, omittedMetadata: false }] };
    }
    if (operation === 'chooseDestination') result = { destinationId: 'destination-b', label: 'Vídeos/Dwnl (simulado)', canceled: false };
    if (operation === 'start') {
      result = { jobId: 'job-preview', startRequestId: requestId, sequence: 0, state: 'downloading' };
      this.timers.push(setTimeout(() => this.progress('downloading', 1, undefined), 1000));
      this.timers.push(setTimeout(() => this.progress('downloading', 2, 42), 2600));
      this.timers.push(setTimeout(() => this.progress(payload.mode === 'mp3' ? 'converting' : 'muxing', 3, undefined), 4200));
      this.timers.push(setTimeout(() => this.progress('saving', 4, undefined), 6200));
      this.timers.push(setTimeout(() => this.emit({ protocolVersion: 1, kind: 'event', hostSessionId: 'session-preview', jobId: 'job-preview', sequence: 5, state: 'completed', completedFile: { path: `Downloads/Dwnl/Arquivo simulado.${payload.mode === 'mp3' ? 'mp3' : 'mp4'}`, format: payload.mode === 'mp3' ? 'mp3' : 'mp4', sizeBytes: 24500000, sha256: '0'.repeat(64), ...(payload.mode === 'mp3' ? { audioCodec: 'mp3', mp3BitrateKbps: payload.bitrateKbps } : { videoCodec: 'h264', audioCodec: 'aac' }) } }), 8200));
    }
    if (operation === 'cancel') {
      this.stop(); result = { snapshot: { ...this.state.job, sequence: (this.state.job?.sequence ?? 0) + 1, state: 'canceling', progress: undefined } };
      delete result.snapshot.progress;
      this.timers.push(setTimeout(() => this.emit({ protocolVersion: 1, kind: 'event', hostSessionId: 'session-preview', jobId: 'job-preview', sequence: result.snapshot.sequence + 1, state: 'canceled' }), 1100));
    }
    const response = { protocolVersion: 1, kind: 'response', operation, requestId, hostSessionId: 'session-preview', result }; this.emit(response); return response;
  }
  progress(state, sequence, percent) { this.emit({ protocolVersion: 1, kind: 'event', hostSessionId: 'session-preview', jobId: 'job-preview', sequence, state, progress: { stage: state, ...(percent !== undefined ? { completedBytes: 420, totalBytes: 1000, percent } : {}) } }); }
  stop() { this.timers.splice(0).forEach(clearTimeout); }
}
function show() {
  target.innerHTML = popupBody;
  for (const image of target.querySelectorAll('img[src]')) image.src = new URL(image.getAttribute('src'), popupUrl).href;
  mounted = mountPopup(document, { client: fixture, currentTab: async () => 'https://www.youtube.com/watch?v=preview' });
}
function reset() { mounted?.dispose(); fixture?.stop(); fixture = new SyntheticTransport(scenario.value); open = true; document.getElementById('toggle-panel').textContent = 'Fechar painel'; show(); }
scenario.addEventListener('change', reset);
document.getElementById('toggle-panel').addEventListener('click', () => { open = !open; if (open) show(); else { mounted?.dispose(); target.innerHTML = ''; } document.getElementById('toggle-panel').textContent = open ? 'Fechar painel' : 'Reabrir painel'; });
reset();
