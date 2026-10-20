import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DesktopClient, desktopRequest, desktopResponse, bestDesktopVideo } from '../src/desktop-client.js';
import type { BrowserPort } from '../src/browser.js';
import { ChromeNativeClient, ExtensionController } from '../src/controller.js';
import { handshake, response, choices } from './fixtures.js';
export const readySnapshot = { version: '0.2.0', readiness: 'ready', capabilities: { video: true, mp3: true, networkAccess: true }, dependencies: [], destination: { destinationId: 'default', label: 'Downloads' }, paused: false, tasks: [] };
function endpoint() {
  const messages = new Set<(v: unknown) => void>(), disconnects = new Set<() => void>(), sent: any[] = [];
  const port: BrowserPort = { postMessage(value) { sent.push(value); }, disconnect: vi.fn(() => { disconnects.forEach(fn => fn()); }), onMessage: { addListener(fn) { messages.add(fn); }, removeListener(fn) { messages.delete(fn); } }, onDisconnect: { addListener(fn) { disconnects.add(fn); }, removeListener(fn) { disconnects.delete(fn); } } };
  return { port, sent, reply(value: unknown) { messages.forEach(fn => fn(value)); } };
}
describe('desktop transport boundaries', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); });
  it('accepts preparing snapshots immediately without waiting for dependency checks', async () => {
    const wire = endpoint(), client = new DesktopClient(() => wire.port);
    const result = client.request('snapshot', {}, 'snapshot-1');
    wire.reply({ version: 2, id: 'snapshot-1', result: { ...readySnapshot, readiness: 'preparing', capabilities: { video: false, mp3: false, networkAccess: false } } });
    expect(await result).toMatchObject({ readiness: 'preparing' }); client.disconnect();
  });
  it('timeout rejects every pending request, closes port and ignores old late reply after reconnect', async () => {
    const first = endpoint(), second = endpoint(), connect = vi.fn().mockReturnValueOnce(first.port).mockReturnValueOnce(second.port);
    const client = new DesktopClient(connect, undefined, 20);
    const a = client.request('snapshot', {}, 'a').catch(error => error);
    const b = client.request('snapshot', {}, 'b').catch(error => error);
    await vi.advanceTimersByTimeAsync(20);
    expect(await a).toMatchObject({ code: 'TIMEOUT' }); expect(await b).toMatchObject({ code: 'TIMEOUT' });
    expect(first.port.disconnect).toHaveBeenCalledOnce();
    const next = client.request('snapshot', {}, 'next');
    first.reply({ version: 2, id: 'next', result: readySnapshot });
    expect(client.busy).toBe(true);
    second.reply({ version: 2, id: 'next', result: readySnapshot });
    expect(await next).toEqual(readySnapshot); expect(connect).toHaveBeenCalledTimes(2); client.disconnect();
  });
  it.each([
    { version: 3, id: 'a', result: readySnapshot },
    { version: 2, id: 'a', result: { ...readySnapshot, tasks: null } },
    { version: 2, id: 'a', result: readySnapshot, error: { code: 'OOPS', message: 'bad' } },
    { version: 2, id: 'a', error: { code: 'OOPS', message: {} } },
  ])('rejects malformed response and permits a clean reconnect: %j', async value => {
    const wire = endpoint(), client = new DesktopClient(() => wire.port);
    const result = client.request('snapshot', {}, 'a').catch(error => error);
    wire.reply(value); expect((await result).code).toMatch(/INVALID_MESSAGE|INCOMPATIBLE_PROTOCOL/);
    expect(wire.port.disconnect).toHaveBeenCalledOnce();
  });
  it('deduplicates a pending enqueue and rejects conflicting reuse', async () => {
    const wire = endpoint(), client = new DesktopClient(() => wire.port);
    const payload = { requestId: 'intent', analysisId: 'analysis-a', itemId: 'item-a', mode: 'mp3' as const, bitrateKbps: 192 as const, destinationId: 'default' };
    const first = client.request('enqueue', payload, 'rpc');
    const same = client.request('enqueue', payload, 'rpc');
    await expect(client.request('enqueue', { ...payload, bitrateKbps: 320 }, 'rpc')).rejects.toMatchObject({ code: 'REQUEST_CONFLICT' });
    expect(wire.sent).toHaveLength(1);
    const task = { id: 'job', requestId: 'intent', url: 'https://youtu.be/public', title: 'Video', mode: 'mp3', state: 'queued', createdAt: new Date().toISOString() };
    wire.reply({ version: 2, id: 'rpc', result: task });
    expect(await first).toEqual(task); expect(await same).toEqual(task); client.disconnect();
  });
  it('disconnect rejects pending calls without exposing raw native error', async () => {
    const wire = endpoint(), client = new DesktopClient(() => wire.port, () => 'Specified native messaging host not found. secret path');
    const result = client.request('snapshot', {}).catch(error => error);
    wire.port.disconnect(); expect(await result).toMatchObject({ code: 'MISSING_HOST', message: 'Instale o aplicativo Dwnl para conectar a extensão.' });
  });
  it('keeps bounded engine deadline for analysis', async () => {
    const wire = endpoint(), client = new DesktopClient(() => wire.port);
    let settled = false;
    const result = client.request('analyze', { url: 'https://www.youtube.com/shorts/public' }).catch(error => { settled = true; return error; });
    await vi.advanceTimersByTimeAsync(90001); expect(settled).toBe(false);
    await vi.advanceTimersByTimeAsync(29999); expect(await result).toMatchObject({ code: 'TIMEOUT' });
  });
  it('validates exact selections and supported origins before IPC', () => {
    expect(() => desktopRequest({ version: 2, id: 'id', method: 'analyze', payload: { url: 'http://localhost:8080' } })).toThrow();
    expect(() => desktopRequest({ version: 2, id: 'id', method: 'enqueue', payload: { requestId: 'one', analysisId: 'a', itemId: 'i', mode: 'mp3', bitrateKbps: 256, destinationId: 'd' } })).toThrow();
    expect(() => desktopResponse({ version: 2, id: 'id', result: 'x'.repeat(524288) }, 'openApp')).toThrow();
  });
  it('chooses vertical 1080x1920 as 1080p and never silently defaults to 4K', () => {
    const vertical = { ...choices[0], width: 1080, height: 1920, optionId: 'short-1080' };
    const high = { ...choices[0], width: 2160, height: 3840, optionId: 'short-4k' };
    expect(bestDesktopVideo([high, vertical])?.optionId).toBe('short-1080');
    expect(bestDesktopVideo([high])).toBeUndefined();
  });
  it('legacy handshake timeout leaves connecting state and releases stale session before retry', async () => {
    const wire = endpoint(), next = endpoint(), connector = vi.fn().mockReturnValueOnce(wire.port).mockReturnValueOnce(next.port);
    const native = new ChromeNativeClient(connector, () => undefined, 10);
    const controller = new ExtensionController(native, { load: async () => undefined, save: async () => {} }, 'test');
    const first = controller.connect(); await vi.advanceTimersByTimeAsync(10); await first;
    expect(controller.state.connection).toBe('disconnected'); expect(controller.state.error?.code).toBe('NETWORK_ERROR');
    const reconnect = controller.connect(); await vi.advanceTimersByTimeAsync(0);
    const hello = next.sent[0];
    wire.reply(response('handshake', handshake, hello.requestId));
    expect(controller.state.connection).toBe('connecting');
    next.reply(response('handshake', handshake, hello.requestId)); await vi.advanceTimersByTimeAsync(0);
    const status = next.sent[1]; next.reply(response('status', { snapshot: null }, status.requestId)); await reconnect;
    expect(controller.state.connection).toBe('connected'); expect(connector).toHaveBeenCalledTimes(2);
  });
});
