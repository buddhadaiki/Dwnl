import type { BrowserAPI } from './browser.js';

const RELOAD_KEY = 'reloadedForVersion';
const VERSION = /^\d{1,5}\.\d{1,5}\.\d{1,5}$/;

async function readFromDisk(url: string): Promise<string> {
  return (await fetch(url, { cache: 'no-store' })).text();
}

// The Dwnl installer replaces this unpacked extension's files in place, but
// Chrome keeps running the copy it loaded until the extension reloads. When the
// manifest on disk names another version than the running one, this returns a
// reload to run once the worker is idle. Each change is attempted only once, so
// a manifest Chrome cannot load never causes a reload loop.
export async function pendingReload(browser: BrowserAPI, read: (url: string) => Promise<string> = readFromDisk): Promise<(() => Promise<void>) | undefined> {
  const reload = browser.runtime.reload;
  if (typeof reload !== 'function') return undefined;
  let installed: unknown;
  try { installed = (JSON.parse(await read(browser.runtime.getURL('manifest.json'))) as { version?: unknown }).version; }
  catch { return undefined; }
  const running = browser.runtime.getManifest().version;
  if (typeof installed !== 'string' || !VERSION.test(installed) || installed === running) return undefined;
  const change = `${running}->${installed}`;
  try { if ((await browser.storage.local.get(RELOAD_KEY))[RELOAD_KEY] === change) return undefined; }
  catch { return undefined; }
  return async () => {
    try { await browser.storage.local.set({ [RELOAD_KEY]: change }); } catch { return; }
    reload.call(browser.runtime);
  };
}
