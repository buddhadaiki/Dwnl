import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ChromeNativeClient, ExtensionController } from '../src/controller.js';
import type { BrowserPort } from '../src/browser.js';
import type { Request, Response } from '../src/protocol.js';
import { startPayload } from '../src/state.js';
import { analysis, job, handshake, response, startFailure } from './fixtures.js';

function endpoint(timeoutMs?: number) {
  const messages = new Set<(message: unknown) => void>();
  const disconnects = new Set<() => void>();
  const sent: Request[] = [];
  let process: ((request: Request) => void) | undefined;
  const port: BrowserPort = { postMessage(value) { sent.push(value as Request); process?.(value as Request); }, disconnect() { disconnects.forEach(listener => listener()); },
    onMessage: { addListener(listener) { messages.add(listener); }, removeListener(listener) { messages.delete(listener); } },
    onDisconnect: { addListener(listener) { disconnects.add(listener); }, removeListener(listener) { disconnects.delete(listener); } } };
  let connections = 0;
  return { sent, port, client: new ChromeNativeClient(() => { connections++; return port; }, () => undefined, timeoutMs), connections: () => connections,
    receive(value: unknown) { messages.forEach(listener => listener(value)); }, route(handler: (request: Request) => void) { process = handler; } };
}
async function connect(native: ReturnType<typeof endpoint>) {
  const pending = native.client.request('handshake', { extensionVersion: '0.1.0' }, 'hello');
  native.receive(response('handshake', handshake, 'hello'));
  return pending;
}
describe('native request deadlines', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); });

  it('delivers an analysis response after the engine has used its full 90-second budget', async () => {
    const native = endpoint(); await connect(native);
    const result = native.client.request('analyze', { url: 'https://youtu.be/public' }, 'slow-analysis')
      .then(response => ({ response }), error => ({ error }));
    await vi.advanceTimersByTimeAsync(90001);
    native.receive(response('analyze', analysis(), 'slow-analysis'));
    expect(await result).toMatchObject({ response: { operation: 'analyze', result: { analysisId: 'analysis-a' } } });
  });

  it('rejects an unanswered analysis after the engine budget and a finite response margin', async () => {
    const native = endpoint(); await connect(native);
    let outcome: { response?: Response; error?: unknown } | undefined;
    const finished = native.client.request('analyze', { url: 'https://youtu.be/public' }, 'unanswered-analysis')
      .then(response => { outcome = { response }; }, error => { outcome = { error }; });
    await vi.advanceTimersByTimeAsync(119999);
    expect(outcome).toBeUndefined();
    await vi.advanceTimersByTimeAsync(1);
    await finished;
    expect(outcome).toMatchObject({ error: { reason: 'timeout' } });
  });

  it.each([
    { operation: 'status', deadlineMs: 30000 },
    { operation: 'chooseDestination', deadlineMs: 300000 },
  ] as const)('keeps the existing $operation deadline', async ({ operation, deadlineMs }) => {
    const native = endpoint(); await connect(native);
    let outcome: { response?: Response; error?: unknown } | undefined;
    const pending = operation === 'status' ? native.client.request('status', {}, 'unanswered-request')
      : native.client.request('chooseDestination', { userGesture: true }, 'unanswered-request');
    const finished = pending.then(response => { outcome = { response }; }, error => { outcome = { error }; });
    await vi.advanceTimersByTimeAsync(deadlineMs - 1);
    expect(outcome).toBeUndefined();
    await vi.advanceTimersByTimeAsync(1);
    await finished;
    expect(outcome).toMatchObject({ error: { reason: 'timeout' } });
  });
});

describe('popup native transport and worker ownership', () => {
  it('one native connection validates responses and correlates operation/request identity', async () => {
    const native = endpoint();
    expect((await connect(native)).hostSessionId).toBe('session-a');
    const status = native.client.request('status', {}, 'status-a');
    native.receive(response('status', { snapshot: job() }, 'status-a'));
    expect((await status).operation).toBe('status');
    expect(native.connections()).toBe(1);
    expect(native.sent[1]).toMatchObject({ hostSessionId: 'session-a', operation: 'status', payload: {} });
  });
  it('duplicate start identity shares acknowledgement, conflicting payload never reaches host', async () => {
    const native = endpoint(); await connect(native);
    const payload = { analysisId: 'analysis-a', itemId: 'item-a', mode: 'mp3' as const, bitrateKbps: 192 as const, destinationId: 'destination-a' };
    const first = native.client.request('start', payload, 'start-one');
    const second = native.client.request('start', payload, 'start-one');
    await expect(native.client.request('start', { ...payload, bitrateKbps: 320 }, 'start-one')).rejects.toMatchObject({ code: 'REQUEST_CONFLICT' });
    native.receive(response('start', { jobId: 'job-a', startRequestId: 'start-one', sequence: 0, state: 'downloading' }, 'start-one'));
    expect((await first).requestId).toBe('start-one'); expect((await second).requestId).toBe('start-one');
    expect(native.sent.filter(request => request.operation === 'start')).toHaveLength(1);
  });
  it('foreign sessions cannot satisfy pending requests and invalid protocol becomes incompatible', async () => {
    const native = endpoint(); await connect(native);
    const pending = native.client.request('status', {}, 'status-current');
    native.receive(response('status', { snapshot: null }, 'status-current', 'session-old'));
    native.receive({ protocolVersion: 2, kind: 'response', operation: 'status', requestId: 'status-current', hostSessionId: 'session-a', result: { snapshot: null } });
    await expect(pending).rejects.toMatchObject({ reason: 'incompatible' });
  });
  it('missing host is distinct from normal disconnection and raw Chrome error is hidden', async () => {
    const native = endpoint();
    const client = new ChromeNativeClient(() => native.port, () => 'Specified native messaging host not found. private diagnostic');
    const request = client.request('handshake', { extensionVersion: '0.1.0' }, 'hello');
    native.port.disconnect();
    await expect(request).rejects.toMatchObject({ reason: 'missing-host', message: 'missing-host' });
  });
  it('lost acknowledgement queries status by the original start identity without resending start', async () => {
    const native = endpoint(12);
    native.route(request => {
      if (request.operation === 'handshake') native.receive(response('handshake', handshake, request.requestId));
      if (request.operation === 'status') native.receive(response('status', { snapshot: 'startRequestId' in request.payload ? job({ startRequestId: request.payload.startRequestId }) : null }, request.requestId));
    });
    const saved: unknown[] = [];
    const worker = new ExtensionController(native.client, { load: async () => undefined, save: async cache => { saved.push(cache); } }, '0.1.0');
    await worker.connect();
    await worker.request('start', { analysisId: 'analysis-a', itemId: 'item-a', mode: 'mp3', bitrateKbps: 192, destinationId: 'destination-a' }, 'start-lost');
    expect(worker.state.job?.startRequestId).toBe('start-lost');
    expect(worker.state.confirmation).toBe('confirmed');
    expect(native.sent.filter(request => request.operation === 'start')).toHaveLength(1);
    expect(native.sent.at(-1)).toMatchObject({ operation: 'status', payload: { startRequestId: 'start-lost' } });
    expect(saved.length).toBeGreaterThan(0);
  });
  it('closing popup subscription preserves native job and reopening requests a fresh snapshot', async () => {
    const native = endpoint();
    native.route(request => {
      if (request.operation === 'handshake') native.receive(response('handshake', handshake, request.requestId));
      if (request.operation === 'status') native.receive(response('status', { snapshot: job({ sequence: 8 }) }, request.requestId));
    });
    const worker = new ExtensionController(native.client, { load: async () => undefined, save: async () => {} }, '0.1.0');
    await worker.connect();
    const unmount = worker.subscribe(() => {}); unmount();
    const snapshot = await worker.snapshot();
    expect(snapshot.job?.sequence).toBe(8);
    expect(native.connections()).toBe(1);
    expect(native.sent.filter(request => request.operation === 'cancel')).toHaveLength(0);
  });
  it('old cached task is reconciled read only in a new session and never automatically restarted', async () => {
    const native = endpoint();
    native.route(request => {
      if (request.operation === 'handshake') native.receive(response('handshake', handshake, request.requestId));
      if (request.operation === 'reconcile') native.receive(response('reconcile', { previousHostSessionId: 'session-old', outcome: 'unconfirmed', snapshot: job({ hostSessionId: 'session-old', state: 'interrupted' }) }, request.requestId));
      if (request.operation === 'status') native.receive(response('status', { snapshot: null }, request.requestId));
    });
    const worker = new ExtensionController(native.client, { load: async () => ({ version: 1, job: job({ hostSessionId: 'session-old' }), history: [] }), save: async () => {} }, '0.1.0');
    await worker.connect();
    expect(worker.state.job?.state).toBe('interrupted');
    expect(worker.state.confirmation).toBe('unconfirmed');
    expect(native.sent.some(request => request.operation === 'reconcile')).toBe(true);
    expect(native.sent.some(request => request.operation === 'start')).toBe(false);
  });
  it('malformed start cannot leave a permanent checking task', async () => {
    const native = endpoint();
    native.route(request => {
      if (request.operation === 'handshake') native.receive(response('handshake', handshake, request.requestId));
      if (request.operation === 'status') native.receive(response('status', { snapshot: null }, request.requestId));
    });
    const worker = new ExtensionController(native.client, { load: async () => undefined, save: async () => {} }, '0.1.0');
    await worker.connect();
    await expect(worker.request('start', { analysisId: '', itemId: 'item-a', mode: 'mp3', bitrateKbps: 192, destinationId: 'destination-a' }, 'bad-start')).rejects.toMatchObject({ code: 'INVALID_MESSAGE' });
    expect(worker.state.pendingStartRequestId).toBeUndefined();
  });
  it('replaying the same start cannot replace a completed task with an old acknowledgement', async () => {
    const native = endpoint(); let current: ReturnType<typeof job> | null = null;
    native.route(request => {
      if (request.operation === 'handshake') native.receive(response('handshake', handshake, request.requestId));
      if (request.operation === 'status') native.receive(response('status', { snapshot: current }, request.requestId));
      if (request.operation === 'start') { current = job({ startRequestId: request.requestId, sequence: 0 }); native.receive(response('start', { jobId: 'job-a', startRequestId: request.requestId, sequence: 0, state: 'downloading' }, request.requestId)); }
    });
    const worker = new ExtensionController(native.client, { load: async () => undefined, save: async () => {} }, '0.1.0'); await worker.connect();
    const payload = { analysisId: 'analysis-a', itemId: 'item-a', mode: 'mp3' as const, bitrateKbps: 192 as const, destinationId: 'destination-a' };
    await worker.request('start', payload, 'same-start');
    current = job({ startRequestId: 'same-start', sequence: 3, state: 'completed', completedFile: { path: 'Downloads/Dwnl/public.mp3', format: 'mp3', sizeBytes: 1000, sha256: 'a'.repeat(64), audioCodec: 'mp3', mp3BitrateKbps: 192 } });
    const { startRequestId: _, ...event } = current;
    native.receive({ ...event, protocolVersion: 1, kind: 'event' });
    await worker.request('start', payload, 'same-start');
    expect(worker.state.job?.state).toBe('completed');
    expect(worker.state.job?.completedFile?.path).toBe('Downloads/Dwnl/public.mp3');
    expect(worker.state.pendingStartRequestId).toBeUndefined();
    expect(native.sent.filter(request => request.operation === 'start')).toHaveLength(1);
  });
  it('recovery prioritizes an uncertain new start over an older terminal job still in cache', async () => {
    const native = endpoint();
    const old = job({ hostSessionId: 'session-old', jobId: 'job-old', startRequestId: 'old-start', state: 'canceled' });
    const completed = job({ hostSessionId: 'session-old', jobId: 'job-new', startRequestId: 'uncertain-new', state: 'completed', completedFile: { path: 'Downloads/Dwnl/new.mp3', format: 'mp3', sizeBytes: 1000, sha256: 'b'.repeat(64), audioCodec: 'mp3', mp3BitrateKbps: 192 } });
    native.route(request => {
      if (request.operation === 'handshake') native.receive(response('handshake', handshake, request.requestId));
      if (request.operation === 'reconcile') native.receive(response('reconcile', { previousHostSessionId: 'session-old', outcome: 'confirmed', snapshot: request.payload.startRequestId === 'uncertain-new' ? completed : old }, request.requestId));
    });
    const worker = new ExtensionController(native.client, { load: async () => ({ version: 1, job: old, history: [old], pendingStart: { hostSessionId: 'session-old', startRequestId: 'uncertain-new' } }), save: async () => {} }, '0.1.0');
    await worker.connect();
    expect(worker.state.job?.jobId).toBe('job-new');
    expect(worker.state.job?.completedFile?.path).toBe('Downloads/Dwnl/new.mp3');
    expect(native.sent.at(-1)).toMatchObject({ operation: 'reconcile', payload: { previousHostSessionId: 'session-old', startRequestId: 'uncertain-new' } });
    expect(native.sent.some(request => request.operation === 'start')).toBe(false);
  });
  it.each([false, true])('a valid typed start rejection recovers controls and preserves prior cache uncertainty (cached=%s)', async cached => {
    const native = endpoint(); const previous = job({ hostSessionId: 'session-old', jobId: 'old-job', startRequestId: 'old-start', state: 'interrupted' });
    native.route(request => {
      if (request.operation === 'handshake') native.receive(response('handshake', handshake, request.requestId));
      if (request.operation === 'status') native.receive(response('status', { snapshot: null }, request.requestId));
      if (request.operation === 'reconcile') native.receive(response('reconcile', { previousHostSessionId: 'session-old', outcome: 'unconfirmed', snapshot: previous }, request.requestId));
      if (request.operation === 'analyze') native.receive(response('analyze', analysis(), request.requestId));
      if (request.operation === 'start') native.receive(startFailure('DESTINATION_ERROR', request.requestId));
    });
    const worker = new ExtensionController(native.client, { load: async () => cached ? { version: 1, job: previous, history: [] } : undefined, save: async () => {} }, '0.1.0');
    await worker.connect(); await worker.request('analyze', { url: 'https://www.youtube.com/watch?v=public' }, 'analyze-valid');
    const payload = { analysisId: 'analysis-a', itemId: 'item-a', mode: 'video' as const, videoOptionId: 'mp41080', destinationId: 'destination-a' };
    const result = await worker.request('start', payload, 'start-rejected');
    expect(result.error?.code).toBe('DESTINATION_ERROR');
    expect(worker.state.pendingStartRequestId).toBeUndefined(); expect(worker.state.pendingStartHostSessionId).toBeUndefined();
    expect(worker.state.confirmation).toBe(cached ? 'unconfirmed' : 'confirmed');
    expect(startPayload(worker.state)).toEqual(payload);
    if (cached) expect(worker.state.job).toEqual(previous);
    expect(native.sent.filter(request => request.operation === 'start')).toHaveLength(1);
  });
  it('BUSY start rejection reads current job status without restarting and exposes that active task', async () => {
    const native = endpoint(); let rejected = false;
    native.route(request => {
      if (request.operation === 'handshake') native.receive(response('handshake', handshake, request.requestId));
      if (request.operation === 'status') native.receive(response('status', { snapshot: rejected ? job({ jobId: 'existing-job', startRequestId: 'existing-start' }) : null }, request.requestId));
      if (request.operation === 'start') { rejected = true; native.receive(startFailure('BUSY', request.requestId)); }
    });
    const worker = new ExtensionController(native.client, { load: async () => undefined, save: async () => {} }, '0.1.0'); await worker.connect();
    await worker.request('start', { analysisId: 'analysis-a', itemId: 'item-a', mode: 'mp3', bitrateKbps: 192, destinationId: 'destination-a' }, 'rejected-busy');
    expect(worker.state.job?.jobId).toBe('existing-job'); expect(worker.state.confirmation).toBe('confirmed');
    expect(native.sent.at(-1)).toMatchObject({ operation: 'status', payload: {} });
    expect(native.sent.filter(request => request.operation === 'start')).toHaveLength(1);
  });
});
