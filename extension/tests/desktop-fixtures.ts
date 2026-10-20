import type { BrowserAPI, BrowserPort } from '../src/browser.js';
import { registerDesktopWorker } from '../src/desktop-worker.js';
import { DESKTOP_PORT, type DesktopRequest, type DesktopSnapshot, type DesktopTask } from '../src/desktop-client.js';
import { analysis } from './fixtures.js';
export function channel(sender?: BrowserPort['sender']) {
  const messages = [new Set<(value: unknown) => void>(), new Set<(value: unknown) => void>()], disconnects = [new Set<() => void>(), new Set<() => void>()];
  let closed = false;
  const ports = [0, 1].map(index => ({
    name: DESKTOP_PORT, ...(index === 1 ? { sender } : {}),
    postMessage(value: unknown) { if (closed) throw new Error('closed'); messages[1-index].forEach(fn => fn(value)); },
    disconnect() { if (closed) return; closed = true; disconnects.forEach(list => list.forEach(fn => fn())); },
    onMessage: { addListener(fn: (value: unknown) => void) { messages[index].add(fn); }, removeListener(fn: (value: unknown) => void) { messages[index].delete(fn); } },
    onDisconnect: { addListener(fn: () => void) { disconnects[index].add(fn); }, removeListener(fn: () => void) { disconnects[index].delete(fn); } },
  } satisfies BrowserPort));
  return { ui: ports[0], worker: ports[1], closed: () => closed };
}
export function environment() {
  const onConnect = new Set<(port: BrowserPort) => void>(), requests: DesktopRequest[] = [], nativeChannels: ReturnType<typeof channel>[] = [];
  const storage: Record<string, unknown> = {};
  const snapshot: DesktopSnapshot = { version: '0.2.0', readiness: 'ready', capabilities: { video: true, mp3: true, networkAccess: true }, dependencies: [], destination: { destinationId: 'destination-a', label: 'Downloads' }, paused: false, tasks: [] };
  const env = { snapshot, storage, requests, nativeChannels, defer: '' as string, enqueueError: '' as string, tabReads: 0, media: analysis(), storageError: false };
  const reply = (port: BrowserPort, request: DesktopRequest, result: unknown) => port.postMessage({ version: 2, id: request.id, result });
  const browser: BrowserAPI = {
    runtime: {
      id: 'extension-a', getManifest: () => ({ version: '0.2.0' }), getURL: path => 'chrome-extension://extension-a/' + path,
      onConnect: { addListener(fn) { onConnect.add(fn); }, removeListener(fn) { onConnect.delete(fn); } },
      connect: () => { const pair = channel({ id: 'extension-a', url: 'chrome-extension://extension-a/desktop-popup.html' }); onConnect.forEach(fn => fn(pair.worker)); return pair.ui; },
      connectNative: () => {
        const pair = channel(); nativeChannels.push(pair);
        pair.worker.onMessage.addListener(value => {
          const request = value as DesktopRequest; requests.push(request);
          if (env.defer === request.method) return;
          if (request.method === 'snapshot') reply(pair.worker, request, structuredClone(snapshot));
          else if (request.method === 'analyze') reply(pair.worker, request, structuredClone(env.media));
          else if (request.method === 'enqueue') {
            if (env.enqueueError) { pair.worker.postMessage({ version: 2, id: request.id, error: { code: env.enqueueError, message: 'Falha de teste' } }); return; }
            const payload = request.payload as { requestId: string; mode: 'video' | 'mp3' };
            let task = snapshot.tasks.find(task => task.requestId === payload.requestId);
            if (!task) { task = { id: 'job-' + snapshot.tasks.length, requestId: payload.requestId, url: env.media.sourceUrl, title: env.media.items[0].title, mode: payload.mode, state: 'queued', createdAt: new Date().toISOString() }; snapshot.tasks.push(task); }
            reply(pair.worker, request, structuredClone(task));
          } else if (request.method === 'chooseDestination') reply(pair.worker, request, { destinationId: 'new-destination', label: 'Vídeos', canceled: false });
          else if (request.method === 'continue') { snapshot.paused = false; reply(pair.worker, request, snapshot); }
          else reply(pair.worker, request, {});
        });
        return pair.ui;
      },
    },
    tabs: { query: async () => { env.tabReads++; return [{ url: 'https://www.youtube.com/shorts/public' }]; } },
    storage: { local: { get: async key => ({ [key]: storage[key] }), set: async values => { if (env.storageError) throw new Error('storage failed'); Object.assign(storage, structuredClone(values)); } } },
  };
  registerDesktopWorker(browser);
  return { ...env, state: env, browser, connectPort: (port: BrowserPort) => onConnect.forEach(fn => fn(port)), reply };
}
