import { describe, expect, it } from 'vitest';
import { DesktopClient } from '../src/desktop-client.js';
import { channel, environment } from './desktop-fixtures.js';
describe('desktop worker ownership', () => {
  it('closes bridge after popup closes without requesting task cancellation', async () => {
    const host = environment(), client = new DesktopClient(() => host.browser.runtime.connect({ name: 'dwnl-desktop-v2' }));
    await client.request('snapshot', {}); expect(host.nativeChannels[0].closed()).toBe(false);
    client.disconnect(); expect(host.nativeChannels[0].closed()).toBe(true);
    expect(host.requests.map(request => request.method)).toEqual(['snapshot']);
  });
  it('keeps in-flight operation until its response even when popup has closed', async () => {
    const host = environment(); host.state.defer = 'analyze';
    const client = new DesktopClient(() => host.browser.runtime.connect({ name: 'dwnl-desktop-v2' }));
    const pending = client.request('analyze', { url: 'https://youtu.be/public' }).catch(error => error);
    client.disconnect(); await pending;
    expect(host.nativeChannels[0].closed()).toBe(false);
    host.reply(host.nativeChannels[0].worker, host.requests[0], host.state.media);
    await new Promise(resolve => setTimeout(resolve, 0));
    expect(host.nativeChannels[0].closed()).toBe(true); expect(host.requests.some(request => request.method === 'cancel')).toBe(false);
  });
  it.each([
    { id: 'foreign', url: 'chrome-extension://extension-a/desktop-popup.html' },
    { id: 'extension-a', url: 'https://example.com' },
    { id: 'extension-a', url: 'chrome-extension://extension-a/popup.html' },
  ])('rejects a sender outside the desktop popup: %j', sender => {
    const host = environment(), ports = channel(sender); host.connectPort(ports.worker);
    expect(ports.closed()).toBe(true); expect(host.nativeChannels).toHaveLength(0);
  });
});
