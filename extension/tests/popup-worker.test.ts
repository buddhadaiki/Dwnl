import { describe, expect, it } from 'vitest';
import type { BrowserAPI, BrowserPort } from '../src/browser.js';
import { registerServiceWorker } from '../src/service-worker.js';
import { PopupNativeClient } from '../src/popup-client.js';
import type { Request } from '../src/protocol.js';
import { handshake, job, response } from './fixtures.js';

function channel(sender: BrowserPort['sender']) {
  const messages = [new Set<(value: unknown) => void>(), new Set<(value: unknown) => void>()];
  const disconnects = [new Set<() => void>(), new Set<() => void>()];
  let closed = false;
  const ports = [0, 1].map(index => ({
    name: 'dwnl-popup-v1', ...(index === 1 ? { sender } : {}),
    postMessage(value: unknown) { if (closed) throw new Error('closed'); messages[1 - index].forEach(listener => listener(value)); },
    disconnect() { if (closed) return; closed = true; disconnects.forEach(listeners => listeners.forEach(listener => listener())); },
    onMessage: { addListener(listener: (value: unknown) => void) { messages[index].add(listener); }, removeListener(listener: (value: unknown) => void) { messages[index].delete(listener); } },
    onDisconnect: { addListener(listener: () => void) { disconnects[index].add(listener); }, removeListener(listener: () => void) { disconnects[index].delete(listener); } },
  } satisfies BrowserPort));
  return { ui: ports[0], worker: ports[1], closed: () => closed };
}
function environment(current = job()) {
  const onConnect = new Set<(port: BrowserPort) => void>();
  const nativeMessages = new Set<(message: unknown) => void>();
  const requests: Request[] = [];
  let connections = 0; let sequence = 1;
  const native: BrowserPort = {
    postMessage(value) {
      const request = value as Request; requests.push(request);
      let valueResponse;
      if (request.operation === 'handshake') valueResponse = response('handshake', handshake, request.requestId);
      else if (request.operation === 'status') valueResponse = response('status', { snapshot: { ...current, sequence: sequence++ } }, request.requestId);
      else if (request.operation === 'cancel') valueResponse = response('cancel', { snapshot: job({ state: 'canceling', sequence: sequence++ }) }, request.requestId);
      if (valueResponse) nativeMessages.forEach(listener => listener(valueResponse));
    }, disconnect() {},
    onMessage: { addListener(listener) { nativeMessages.add(listener); }, removeListener(listener) { nativeMessages.delete(listener); } },
    onDisconnect: { addListener() {}, removeListener() {} },
  };
  const storage: Record<string, unknown> = {};
  const browser: BrowserAPI = {
    runtime: { id: 'extension-a', getManifest: () => ({ version: '0.1.0' }), getURL: path => `chrome-extension://extension-a/${path}`,
      connect: () => { const ports = channel({ id: 'extension-a', url: 'chrome-extension://extension-a/popup.html' }); onConnect.forEach(listener => listener(ports.worker)); return ports.ui; },
      connectNative: () => { connections++; return native; },
      onConnect: { addListener(listener) { onConnect.add(listener); }, removeListener(listener) { onConnect.delete(listener); } } },
    tabs: { query: async () => [] }, storage: { local: { get: async key => ({ [key]: storage[key] }), set: async values => { Object.assign(storage, values); } } },
  };
  const controller = registerServiceWorker(browser);
  return { browser, controller, requests, connections: () => connections, connectPort: (port: BrowserPort) => onConnect.forEach(listener => listener(port)) };
}
describe('real popup to service worker bridge', () => {
  it('duplicate popup request promises both resolve from one worker response', async () => {
    const ports = channel({ id: 'extension-a', url: 'chrome-extension://extension-a/popup.html' });
    const client = new PopupNativeClient(ports.ui); const outgoing: unknown[] = [];
    ports.worker.onMessage.addListener(packet => outgoing.push(packet));
    const first = client.request('status', {}, 'shared-status'); const second = client.request('status', {}, 'shared-status');
    ports.worker.postMessage({ kind: 'result', requestId: 'shared-status', response: response('status', { snapshot: null }, 'shared-status') });
    const result = await Promise.race([Promise.all([first, second]), new Promise(resolve => setTimeout(() => resolve('unresolved'), 20))]);
    expect(result).toEqual([response('status', { snapshot: null }, 'shared-status'), response('status', { snapshot: null }, 'shared-status')]);
    expect(outgoing).toHaveLength(1); client.close();
  });
  it('worker disconnection keeps the last job visible as unconfirmed', async () => {
    const ports = channel({ id: 'extension-a', url: 'chrome-extension://extension-a/popup.html' });
    const client = new PopupNativeClient(ports.ui); let observed;
    client.subscribeState(state => { observed = state; });
    ports.worker.postMessage({ kind: 'state', state: { connection: 'connected', mode: 'video', bitrateKbps: 192, history: [], confirmation: 'confirmed', job: job() } });
    ports.worker.disconnect();
    expect(observed).toMatchObject({ connection: 'disconnected', confirmation: 'checking', job: { jobId: 'job-a' } });
  });
  it('snapshots survive closing a popup and native ownership remains in the worker', async () => {
    const host = environment(); const first = new PopupNativeClient(host.browser.runtime.connect({ name: 'dwnl-popup-v1' }));
    expect((await first.snapshot()).job?.sequence).toBe(1);
    first.close();
    const second = new PopupNativeClient(host.browser.runtime.connect({ name: 'dwnl-popup-v1' }));
    expect((await second.snapshot()).job?.sequence).toBe(2);
    expect(host.connections()).toBe(1);
    expect(host.requests.some(request => request.operation === 'cancel')).toBe(false); second.close();
  });
  it('a page or a different extension cannot open the native connection', () => {
    const host = environment();
    const wrongExtension = channel({ id: 'other-extension', url: 'chrome-extension://extension-a/popup.html' }); host.connectPort(wrongExtension.worker);
    const page = channel({ id: 'extension-a', url: 'https://www.youtube.com/watch?v=public' }); host.connectPort(page.worker);
    expect(wrongExtension.closed()).toBe(true); expect(page.closed()).toBe(true); expect(host.connections()).toBe(0);
  });
  it('unapproved path or CLI fields are rejected before native forwarding', async () => {
    const host = environment(); const client = new PopupNativeClient(host.browser.runtime.connect({ name: 'dwnl-popup-v1' })); await client.snapshot();
    await expect(client.request('chooseDestination', { userGesture: true, path: 'C:/outside' } as never, 'bad-path')).rejects.toMatchObject({ code: 'INVALID_MESSAGE' });
    expect(host.requests.some(request => request.requestId === 'bad-path')).toBe(false); client.close();
  });
  it('clearing extension history has no filesystem action and preserves an active task', async () => {
    const host = environment(); const client = new PopupNativeClient(host.browser.runtime.connect({ name: 'dwnl-popup-v1' })); await client.snapshot();
    client.clearHistory();
    expect(host.controller.state.history).toHaveLength(0);
    expect(host.controller.state.job?.state).toBe('downloading');
    expect(host.requests.some(request => request.operation === 'cancel')).toBe(false); client.close();
  });
  it('a fresh terminal snapshot cannot repopulate history immediately after user clears it', async () => {
    const host = environment(job({ state: 'canceled' })); const client = new PopupNativeClient(host.browser.runtime.connect({ name: 'dwnl-popup-v1' })); await client.snapshot();
    expect(host.controller.state.history).toHaveLength(1);
    client.clearHistory(); await client.snapshot();
    expect(host.controller.state.history).toHaveLength(0);
    expect(host.controller.state.job).toBeUndefined();
    expect(host.requests.some(request => request.operation === 'cancel')).toBe(false); client.close();
  });
});
