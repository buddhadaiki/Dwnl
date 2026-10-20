import { browserAPI, type BrowserAPI } from './browser.js';
import { DESKTOP_PORT, desktopRequest, nativeDesktopClient, DesktopError } from './desktop-client.js';

export function registerDesktopWorker(browser: BrowserAPI): void {
  const native = nativeDesktopClient(name => browser.runtime.connectNative(name), () => browser.runtime.lastError?.message);
  let panels = 0;
  const release = () => { if (panels === 0 && !native.busy) native.disconnect(); };
  browser.runtime.onConnect.addListener(port => {
    if (port.name !== DESKTOP_PORT || port.sender?.id !== browser.runtime.id || port.sender.url !== browser.runtime.getURL('desktop-popup.html')) { port.disconnect(); return; }
    panels++;
    let closed = false;
    const post = (value: unknown) => { if (!closed) try { port.postMessage(value); } catch { /* Panel already closed; app owns the operation. */ } };
    const onMessage = async (value: unknown) => {
      let id: string | undefined;
      try {
        const request = desktopRequest(value); id = request.id;
        const result = await native.request(request.method, request.payload, request.id);
        post({ version: 2, id, result });
      } catch (error) {
        if (!id && value && typeof value === 'object' && 'id' in value && typeof value.id === 'string') id = value.id;
        post({ version: 2, id: id ?? 'invalid', error: { code: error instanceof DesktopError ? error.code : 'INVALID_MESSAGE', message: error instanceof Error ? error.message : 'Solicitação inválida.' } });
      } finally { release(); }
    };
    port.onMessage.addListener(onMessage);
    port.onDisconnect.addListener(() => { if (closed) return; closed = true; panels--; port.onMessage.removeListener(onMessage); release(); });
  });
}
const browser = browserAPI();
if (browser) registerDesktopWorker(browser);
