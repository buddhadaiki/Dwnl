// @vitest-environment happy-dom
import { readFileSync } from 'node:fs';
import { afterEach, describe, expect, it } from 'vitest';
import { mountDesktopPopup } from '../src/desktop-popup.js';
import { environment } from './desktop-fixtures.js';
const html = readFileSync('extension/desktop-popup.html', 'utf8');
const cleanup: (() => void)[] = [];
afterEach(() => { cleanup.splice(0).forEach(fn => fn()); document.body.innerHTML = ''; });
const element = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const tick = () => new Promise(resolve => setTimeout(resolve, 0));
async function mount(host = environment()) { document.body.innerHTML = /<body>([\s\S]*?)<\/body>/.exec(html)![1].replace(/<script[\s\S]*?<\/script>/g, ''); cleanup.push(mountDesktopPopup(document, host.browser)); await tick(); return host; }
async function analyze() { element<HTMLInputElement>('url').value = 'https://www.youtube.com/shorts/public'; element('analyze-form').dispatchEvent(new Event('submit', { cancelable: true })); await tick(); }
describe('desktop quick panel', () => {
  it('reads active tab only on explicit gesture, then analyzes and sends exact quality', async () => {
    const host = await mount(); expect(host.state.tabReads).toBe(0);
    element<HTMLButtonElement>('current-tab').click(); await tick(); expect(host.state.tabReads).toBe(1);
    expect(element<HTMLInputElement>('url').value).toContain('/shorts/');
    await analyze(); expect(element<HTMLSelectElement>('quality').value).toBe('mp41080');
    element<HTMLButtonElement>('enqueue').click(); element<HTMLButtonElement>('enqueue').click(); await tick();
    expect(host.requests.filter(request => request.method === 'enqueue')).toHaveLength(1);
    expect(host.requests.find(request => request.method === 'enqueue')?.payload).toMatchObject({ mode: 'video', videoOptionId: 'mp41080' });
    expect(host.snapshot.tasks).toHaveLength(1);
    expect(host.storage['dwnl-pending-enqueue-v2']).toBeNull();
  });
  it('shows connected preparing state while media actions remain unavailable', async () => {
    const host = environment(); host.snapshot.readiness = 'preparing'; await mount(host);
    expect(element('status').textContent).toContain('preparando');
    expect(element<HTMLButtonElement>('analyze').disabled).toBe(true);
    expect(element('reconnect').hidden).toBe(true);
    expect(element<HTMLButtonElement>('open-app').disabled).toBe(false);
  });
  it('offers MP3 128/192/320 and never silently changes selected bitrate', async () => {
    const host = await mount(); await analyze();
    element<HTMLSelectElement>('mode').value = 'mp3'; element('mode').dispatchEvent(new Event('change'));
    expect([...element<HTMLSelectElement>('quality').options].map(option => option.value)).toEqual(['128', '192', '320']);
    expect(element<HTMLSelectElement>('quality').value).toBe('192');
    element<HTMLSelectElement>('quality').value = '320'; element('quality').dispatchEvent(new Event('change'));
    element<HTMLButtonElement>('enqueue').click(); await tick();
    expect(host.requests.find(request => request.method === 'enqueue')?.payload).toMatchObject({ mode: 'mp3', bitrateKbps: 320 });
  });
  it('restores uncertain intent across reopening and retries with the same requestId', async () => {
    const host = environment(); host.state.enqueueError = 'APP_UNAVAILABLE'; await mount(host); await analyze();
    element<HTMLButtonElement>('enqueue').click(); await tick();
    const original = host.requests.find(request => request.method === 'enqueue')!.payload;
    expect(host.storage['dwnl-pending-enqueue-v2']).toEqual(original);
    cleanup.pop()!(); host.state.enqueueError = ''; await mount(host);
    expect(element('retry-enqueue').hidden).toBe(false);
    element<HTMLButtonElement>('retry-enqueue').click(); await tick();
    expect(host.requests.filter(request => request.method === 'enqueue').map(request => request.payload)).toEqual([original, original]);
    expect(host.snapshot.tasks).toHaveLength(1); expect(element('retry-enqueue').hidden).toBe(true);
  });
  it('reconciles an already accepted pending intent without sending it again', async () => {
    const host = environment();
    host.storage['dwnl-pending-enqueue-v2'] = { requestId: 'intent', analysisId: 'a', itemId: 'i', mode: 'mp3', bitrateKbps: 192, destinationId: 'destination-a' };
    host.snapshot.tasks.push({ id: 'job', requestId: 'intent', url: 'https://youtu.be/public', title: 'Video', mode: 'mp3', state: 'queued', createdAt: new Date().toISOString() });
    await mount(host); expect(host.storage['dwnl-pending-enqueue-v2']).toBeNull();
    expect(host.requests.some(request => request.method === 'enqueue')).toBe(false); expect(element('feedback').textContent).toContain('confirmado');
  });
  it('does not send enqueue if saving the intent fails', async () => {
    const host = await mount(); await analyze(); host.state.storageError = true;
    element<HTMLButtonElement>('enqueue').click(); await tick();
    expect(host.requests.some(request => request.method === 'enqueue')).toBe(false);
  });
  it('allows reanalysis after a definitive rejection and confirms no task was accepted', async () => {
    const host = await mount(); await analyze(); host.state.enqueueError = 'APP_ERROR';
    element<HTMLButtonElement>('enqueue').click(); await tick();
    expect(host.storage['dwnl-pending-enqueue-v2']).toBeNull();
    expect(element<HTMLButtonElement>('analyze').disabled).toBe(false);
    expect(element('feedback').textContent).toContain('Falha de teste');
  });
});
