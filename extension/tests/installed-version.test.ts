import { describe, expect, it } from 'vitest';
import type { BrowserAPI } from '../src/browser.js';
import { DESKTOP_PORT } from '../src/desktop-client.js';
import { registerDesktopWorker } from '../src/desktop-worker.js';
import { pendingReload } from '../src/installed-version.js';
import { channel } from './desktop-fixtures.js';

const flush = async () => { for (let i = 0; i < 5; i++) await new Promise(resolve => setTimeout(resolve, 0)); };

function fakeBrowser(running: string, withReload = true) {
  const storage: Record<string, unknown> = {}, connected = new Set<Parameters<BrowserAPI['runtime']['onConnect']['addListener']>[0]>();
  const state = { reloads: 0, storage, storageFails: false };
  const browser: BrowserAPI = {
    runtime: {
      id: 'extension-a', getManifest: () => ({ version: running }), getURL: path => 'chrome-extension://extension-a/' + path,
      ...(withReload ? { reload: () => { state.reloads++; } } : {}),
      onConnect: { addListener: fn => { connected.add(fn); }, removeListener: fn => { connected.delete(fn); } },
      connect: () => { const pair = channel({ id: 'extension-a', url: 'chrome-extension://extension-a/desktop-popup.html' }); connected.forEach(fn => fn(pair.worker)); return pair.ui; },
      connectNative: () => channel().ui,
    },
    tabs: { query: async () => [] },
    storage: { local: {
      get: async key => ({ [key]: storage[key] }),
      set: async values => { if (state.storageFails) throw new Error('storage failed'); Object.assign(storage, values); },
    } },
  };
  return { browser, state };
}
const manifest = (version: unknown) => async (url: string) => {
  expect(url).toBe('chrome-extension://extension-a/manifest.json');
  return JSON.stringify({ manifest_version: 3, version });
};

describe('following the version the installer put on disk', () => {
  it('offers a reload only when the manifest on disk names another version', async () => {
    const { browser, state } = fakeBrowser('0.3.0');
    expect(await pendingReload(browser, manifest('0.3.0'))).toBeUndefined();
    const reload = await pendingReload(browser, manifest('0.4.0'));
    expect(reload).toBeTypeOf('function');
    expect(state.reloads).toBe(0);
    await reload!();
    expect(state.reloads).toBe(1);
    expect(state.storage.reloadedForVersion).toBe('0.3.0->0.4.0');
  });

  it('tries each change once, so a copy Chrome cannot load never loops', async () => {
    const { browser, state } = fakeBrowser('0.3.0');
    await (await pendingReload(browser, manifest('0.4.0')))!();
    expect(await pendingReload(browser, manifest('0.4.0'))).toBeUndefined();
    // A later update is a new change and reloads again.
    await (await pendingReload(browser, manifest('0.4.1')))!();
    expect(state.reloads).toBe(2);
  });

  it.each([
    ['an unreadable manifest', async () => { throw new Error('missing'); }],
    ['a manifest that is not JSON', async () => 'not json'],
    ['a version that is not MAJOR.MINOR.PATCH', manifest('0.4')],
    ['no version', manifest(undefined)],
  ])('does nothing with %s', async (_, read) => {
    const { browser } = fakeBrowser('0.3.0');
    expect(await pendingReload(browser, read)).toBeUndefined();
  });

  it('does not reload when the reload cannot be remembered, or without the reload API', async () => {
    const { browser, state } = fakeBrowser('0.3.0');
    state.storageFails = true;
    await (await pendingReload(browser, manifest('0.4.0')))!();
    expect(state.reloads).toBe(0);
    let reads = 0;
    expect(await pendingReload(fakeBrowser('0.3.0', false).browser, async () => { reads++; return '{}'; })).toBeUndefined();
    expect(reads).toBe(0);
  });

  it('reloads at once when the worker starts idle', async () => {
    const { browser, state } = fakeBrowser('0.3.0');
    registerDesktopWorker(browser, manifest('0.4.0'));
    await flush();
    expect(state.reloads).toBe(1);
  });

  it('waits for an open panel to close before reloading', async () => {
    const { browser, state } = fakeBrowser('0.3.0');
    registerDesktopWorker(browser, manifest('0.4.0'));
    const panel = browser.runtime.connect({ name: DESKTOP_PORT });
    await flush();
    expect(state.reloads).toBe(0);
    panel.disconnect();
    await flush();
    expect(state.reloads).toBe(1);
  });

  it('keeps running when the installed version is the running one', async () => {
    const { browser, state } = fakeBrowser('0.3.0');
    registerDesktopWorker(browser, manifest('0.3.0'));
    await flush();
    expect(state.reloads).toBe(0);
  });
});
