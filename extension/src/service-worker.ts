import { browserAPI, type BrowserAPI } from './browser.js';
import { ChromeNativeClient, ExtensionController, NativeTransportError } from './controller.js';
import { decodeRequest, ProtocolDecodeError } from './protocol.js';

const CACHE_KEY = 'dwnl-state-v1';
export function registerServiceWorker(browser: BrowserAPI): ExtensionController {
  const native = new ChromeNativeClient(name => browser.runtime.connectNative(name), () => browser.runtime.lastError?.message);
  const controller = new ExtensionController(native, { load: async () => (await browser.storage.local.get(CACHE_KEY))[CACHE_KEY], save: async cache => { await browser.storage.local.set({ [CACHE_KEY]: cache }); } }, browser.runtime.getManifest().version);
  browser.runtime.onConnect.addListener(port => {
    if (port.name !== 'dwnl-popup-v1' || port.sender?.id !== browser.runtime.id || port.sender.url !== browser.runtime.getURL('popup.html')) { port.disconnect(); return; }
    const post = (message: unknown) => { try { port.postMessage(message); } catch { /* Closing a panel leaves the native owner intact. */ } };
    const unsubscribe = controller.subscribe((state, message) => post({ kind: 'state', state, ...(message ? { message } : {}) }));
    const onMessage = async (value: unknown) => {
      let id: string | undefined;
      try {
        if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new ProtocolDecodeError('INVALID_MESSAGE', 'Invalid popup request');
        const packet = value as Record<string, unknown>;
        if (['snapshot', 'reconnect', 'clearHistory'].includes(packet.kind as string) && Object.keys(packet).length === 1) {
          if (packet.kind === 'clearHistory') { controller.clearHistory(); return; }
          if (packet.kind === 'reconnect') await controller.connect();
          post({ kind: 'snapshot', state: await controller.snapshot() }); return;
        }
        id = typeof packet.requestId === 'string' ? packet.requestId : undefined;
        if (packet.kind !== 'request' || Object.keys(packet).some(key => !['kind', 'operation', 'payload', 'requestId'].includes(key))) throw new ProtocolDecodeError('INVALID_MESSAGE', 'Invalid popup request');
        const request = decodeRequest({ protocolVersion: 1, requestId: packet.requestId, operation: packet.operation, payload: packet.payload, ...(packet.operation !== 'handshake' ? { hostSessionId: controller.state.hostSessionId ?? 'unconnected' } : {}) });
        const response = await controller.request(request.operation, request.payload, request.requestId);
        post({ kind: 'result', requestId: request.requestId, response });
      } catch (error) {
        post({ kind: 'failure', requestId: id, ...(error instanceof ProtocolDecodeError ? { code: error.code } : { reason: error instanceof NativeTransportError ? error.reason : 'error' }) });
      }
    };
    port.onMessage.addListener(onMessage);
    port.onDisconnect.addListener(() => { unsubscribe(); port.onMessage.removeListener(onMessage); });
  });
  return controller;
}
const browser = browserAPI();
if (browser) registerServiceWorker(browser);
