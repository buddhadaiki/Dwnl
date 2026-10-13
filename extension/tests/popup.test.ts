// @vitest-environment happy-dom
import { readFileSync } from 'node:fs';
import { afterEach, describe, expect, it } from 'vitest';
import { mountPopup, type PopupOptions } from '../src/popup.js';
import { NativeTransportError } from '../src/controller.js';
import { createInitialState, reduceState } from '../src/state.js';
import type { JobEvent, NativeClient, Operation, PayloadByOperation, ProtocolError, Response } from '../src/protocol.js';
import { analysis, choices, handshake, job, response, startFailure } from './fixtures.js';

const html = readFileSync('extension/popup.html', 'utf8');
class HostFixture implements NativeClient {
  listeners = new Set<(message: Response | JobEvent) => void>();
  media = analysis();
  capabilities = handshake.capabilities;
  failure?: 'missing-host' | 'incompatible';
  startError?: ProtocolError['code'];
  deferredOperation?: Operation;
  deferredResponse?: Promise<Response>;
  requests: { operation: Operation; payload: unknown; requestId: string }[] = [];
  async request<O extends Operation>(operation: O, payload: PayloadByOperation[O], requestId: string): Promise<Response> {
    if (this.failure) throw new NativeTransportError(this.failure);
    this.requests.push({ operation, payload, requestId });
    if (operation === this.deferredOperation && this.deferredResponse) return this.deferredResponse;
    if (operation === 'handshake') return response('handshake', { ...handshake, capabilities: this.capabilities }, requestId);
    if (operation === 'analyze') return response('analyze', this.media, requestId);
    if (operation === 'status') return response('status', { snapshot: null }, requestId);
    if (operation === 'cancel') return response('cancel', { snapshot: job({ sequence: 2, state: 'canceling' }) }, requestId);
    if (operation === 'start') return this.startError ? startFailure(this.startError, requestId) : response('start', { jobId: 'job-a', startRequestId: requestId, sequence: 0, state: 'downloading' }, requestId);
    return response('chooseDestination', { destinationId: 'destination-b', label: 'Vídeos', canceled: false }, requestId);
  }
  subscribe(listener: (message: Response | JobEvent) => void): () => void { this.listeners.add(listener); return () => this.listeners.delete(listener); }
  emit(message: Response | JobEvent) { this.listeners.forEach(listener => listener(message)); }
}
const disposed: (() => void)[] = [];
afterEach(() => { disposed.splice(0).forEach(dispose => dispose()); document.body.innerHTML = ''; });
function element<T extends HTMLElement>(id: string): T { return document.getElementById(id) as T; }
async function tick() { await new Promise(resolve => setTimeout(resolve, 0)); }
async function setup(host = new HostFixture(), currentTab: PopupOptions['currentTab'] = async () => 'https://www.youtube.com/watch?v=chosen', installer?: PopupOptions['installer'], initialState?: PopupOptions['initialState']) {
  document.body.innerHTML = /<body>([\s\S]*?)<\/body>/.exec(html)![1];
  const popup = mountPopup(document, { client: host, currentTab, installer, initialState }); disposed.push(popup.dispose); await popup.ready;
  return host;
}
async function analyze() { element<HTMLInputElement>('url').value = 'https://www.youtube.com/watch?v=public'; element<HTMLFormElement>('source-form').dispatchEvent(new Event('submit', { cancelable: true })); await tick(); }
describe('Portuguese popup', () => {
  it('CurrentTabOnlyOnGesture reads just the chosen tab and presents its URL before analysis', async () => {
    let reads = 0;
    const host = await setup(new HostFixture(), async () => { reads++; return 'https://www.youtube.com/watch?v=chosen'; });
    expect(reads).toBe(0); expect(element<HTMLInputElement>('url').value).toBe('');
    element<HTMLButtonElement>('current-tab').click(); await tick();
    expect(element<HTMLInputElement>('url').value).toBe('https://www.youtube.com/watch?v=chosen');
    expect(reads).toBe(1); expect(host.requests.filter(request => request.operation === 'analyze')).toHaveLength(0);
    await analyze(); expect(element('media-title').textContent).toBe('Um vídeo público');
  });
  it('shows actual quality and MP3 choices, defaulting to 192 without enabling audio for silent media', async () => {
    const host = await setup(); await analyze();
    expect(element<HTMLSelectElement>('quality').value).toBe('mp41080');
    element<HTMLButtonElement>('mode-mp3').click();
    expect(element<HTMLSelectElement>('bitrate').value).toBe('192');
    expect([...element<HTMLSelectElement>('bitrate').options].map(option => option.value)).toEqual(['128', '192', '320']);
    expect(element('mp3-settings').hidden).toBe(false);
    host.media = analysis(choices, false); await analyze();
    expect(element<HTMLButtonElement>('mode-mp3').disabled).toBe(true);
    expect(element('mp3-reason').textContent).toContain('não tem áudio');
  });
  it('requires an explicit higher-than-1080 or unknown quality choice and names available container', async () => {
    const host = new HostFixture(); host.media = analysis([{ ...choices[0], height: 2160, width: 3840 }]); await setup(host); await analyze();
    expect(element<HTMLSelectElement>('quality').value).toBe('');
    expect(element<HTMLButtonElement>('start').disabled).toBe(true);
    expect(element('quality-note').textContent).toContain('escolha');
    element<HTMLSelectElement>('quality').value = 'webm1080'; element('quality').dispatchEvent(new Event('change'));
    expect(element<HTMLButtonElement>('start').disabled).toBe(false);
    expect(element('quality-note').textContent).toContain('WebM');
  });
  it('renders indeterminate progress and canceling until confirmed terminal event', async () => {
    const host = await setup(); await analyze(); element<HTMLButtonElement>('start').click(); await tick();
    host.emit({ protocolVersion: 1, kind: 'event', hostSessionId: 'session-a', jobId: 'job-a', sequence: 1, state: 'downloading', progress: { stage: 'downloading', completedBytes: 500 } });
    expect(element('job-progress').hasAttribute('value')).toBe(false);
    expect(element('job-percent').textContent).toBe('');
    element<HTMLButtonElement>('cancel').click(); await tick();
    expect(element('job-state').textContent).toBe('Cancelando');
    host.emit({ protocolVersion: 1, kind: 'event', hostSessionId: 'session-a', jobId: 'job-a', sequence: 3, state: 'canceled' });
    expect(element('job-state').textContent).toBe('Cancelado');
  });
  it('networkAccess false shows an honest gate and keeps real analysis disabled', async () => {
    const host = new HostFixture(); host.capabilities = { video: true, mp3: true, networkAccess: false }; await setup(host);
    expect(element('network-notice').hidden).toBe(false); expect(element<HTMLButtonElement>('analyze').disabled).toBe(true);
    expect(element('network-notice').textContent).toContain('bloqueada');
    expect(element<HTMLButtonElement>('install').hidden).toBe(true);
  });
  it('title metadata is rendered as text and closing UI unsubscribes without canceling', async () => {
    const host = new HostFixture(); host.media.items[0].title = '<img src=x onerror=alert(1)>'; await setup(host); await analyze();
    expect(element('media-title').textContent).toBe('<img src=x onerror=alert(1)>');
    expect(element('media-title').querySelector('img')).toBeNull();
    disposed.pop()!(); expect(host.listeners.size).toBe(0);
    expect(host.requests.some(request => request.operation === 'cancel')).toBe(false);
  });
  it.each([128, 192, 320])('sends selected MP3 output bitrate %s through the actual UI gesture', async bitrate => {
    const host = await setup(); await analyze(); element<HTMLButtonElement>('mode-mp3').click();
    element<HTMLSelectElement>('bitrate').value = String(bitrate); element('bitrate').dispatchEvent(new Event('change'));
    element<HTMLButtonElement>('start').click(); await tick();
    expect(host.requests.find(request => request.operation === 'start')?.payload).toMatchObject({ mode: 'mp3', bitrateKbps: bitrate, destinationId: 'destination-a' });
  });
  it.each(['missing-host', 'incompatible'] as const)('%s onboarding is distinct and has no fabricated installer URL', async reason => {
    const host = new HostFixture(); host.failure = reason; await setup(host);
    expect(element('onboarding').hidden).toBe(false);
    expect(element('onboarding-title').textContent).toBe(reason === 'missing-host' ? 'Falta o auxiliar local' : 'Atualize o pacote Dwnl');
    expect(element<HTMLButtonElement>('install').hidden).toBe(true);
    expect(element('local-installer').textContent).toContain('não está incluído');
    expect(element('onboarding').querySelector('a')).toBeNull();
  });
  it('failed private installer transfer leaves manual package instructions visible', async () => {
    const host = new HostFixture(); host.failure = 'missing-host';
    await setup(host, async () => undefined, { available: async () => true, download: async () => { throw new Error('Chrome refuses private resource'); } });
    element<HTMLButtonElement>('install').click(); await tick();
    expect(element('local-installer').textContent).toContain('Abra manualmente installer/Dwnl-Setup.exe');
  });
  it('typed restriction errors never render raw diagnostics or signed media URLs', async () => {
    const host = await setup(); await analyze(); element<HTMLButtonElement>('start').click(); await tick();
    host.emit({ protocolVersion: 1, kind: 'event', hostSessionId: 'session-a', jobId: 'job-a', sequence: 1, state: 'failed', error: { code: 'ACCESS_RESTRICTED', message: 'secret-token https://cdn.example/stream?token=secret-token', retryable: false } });
    expect(element('job-detail').textContent).toContain('acesso restrito');
    expect(document.body.textContent).not.toContain('secret-token');
  });
  it.each(['no-job', 'old-unconfirmed', 'old-confirmed'] as const)('typed start rejection allows analyzing again without reopening (%s)', async prior => {
    const host = new HostFixture(); host.startError = 'ANALYSIS_EXPIRED';
    const base = reduceState(createInitialState(), response('handshake', handshake));
    const previous = prior === 'no-job' ? undefined : job({ hostSessionId: 'session-old', jobId: 'old-job', startRequestId: 'old-start', state: 'canceled' });
    await setup(host, undefined, undefined, { ...base, job: previous, confirmation: prior === 'old-confirmed' ? 'confirmed' : 'unconfirmed' });
    await analyze(); element<HTMLButtonElement>('start').click(); await tick();
    expect(element('error').textContent).toContain('análise expirou');
    expect(element<HTMLButtonElement>('analyze').disabled).toBe(false);
    if (prior === 'old-unconfirmed') { expect(element('job-state').textContent).toBe('Resultado não confirmado'); expect(element('verify-job').hidden).toBe(false); }
    if (prior === 'old-confirmed') expect(element('job-state').textContent).toBe('Cancelado');
    await analyze(); expect(element<HTMLButtonElement>('start').disabled).toBe(false);
    expect(host.requests.filter(request => request.operation === 'analyze')).toHaveLength(2);
    expect(host.requests.filter(request => request.operation === 'start')).toHaveLength(1);
  });
  it.each(['analyze', 'chooseDestination'] as const)('current-tab gesture cannot release an outstanding %s operation', async operation => {
    let tabReads = 0; const host = await setup(new HostFixture(), async () => { tabReads++; return 'https://www.youtube.com/watch?v=other'; });
    if (operation === 'chooseDestination') await analyze();
    let finish!: (value: Response) => void;
    host.deferredOperation = operation; host.deferredResponse = new Promise(resolve => { finish = resolve; });
    if (operation === 'analyze') { element<HTMLInputElement>('url').value = 'https://www.youtube.com/watch?v=public'; element('source-form').dispatchEvent(new Event('submit', { cancelable: true })); }
    else element<HTMLButtonElement>('choose-destination').click();
    await tick(); element('current-tab').dispatchEvent(new Event('click')); await tick();
    expect(element<HTMLButtonElement>('analyze').disabled).toBe(true);
    expect(element<HTMLButtonElement>('current-tab').disabled).toBe(true);
    expect(tabReads).toBe(0);
    element('source-form').dispatchEvent(new Event('submit', { cancelable: true })); await tick();
    expect(host.requests.filter(request => request.operation === operation)).toHaveLength(1);
    const requestId = host.requests.find(request => request.operation === operation)!.requestId;
    finish(operation === 'analyze' ? response('analyze', analysis(), requestId) : response('chooseDestination', { destinationId: 'destination-b', label: 'Vídeos', canceled: false }, requestId));
    await tick(); expect(element<HTMLButtonElement>('analyze').disabled).toBe(false); expect(element<HTMLButtonElement>('current-tab').disabled).toBe(false);
    if (operation === 'chooseDestination') expect(element('destination').textContent).toBe('Vídeos');
  });
});
