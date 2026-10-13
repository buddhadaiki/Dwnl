export interface ListenerEvent<F extends (...args: never[]) => unknown> { addListener(listener: F): void; removeListener(listener: F): void }
export interface BrowserPort {
  name?: string; sender?: { id?: string; url?: string; origin?: string };
  postMessage(message: unknown): void; disconnect(): void;
  onMessage: ListenerEvent<(message: unknown) => void>; onDisconnect: ListenerEvent<() => void>;
}
export interface BrowserAPI {
  runtime: {
    id: string; lastError?: { message?: string }; getManifest(): { version: string; permissions?: string[] }; getURL(path: string): string;
    connect(options: { name: string }): BrowserPort; connectNative(name: string): BrowserPort;
    onConnect: ListenerEvent<(port: BrowserPort) => void>;
  };
  tabs: { query(query: { active: true; currentWindow: true }): Promise<{ url?: string }[]> };
  storage: { local: { get(key: string): Promise<Record<string, unknown>>; set(values: Record<string, unknown>): Promise<void> } };
  downloads?: { download(options: { url: string; filename: string; saveAs: true }): Promise<number> };
}
export function browserAPI(): BrowserAPI | undefined {
  const browser = (globalThis as typeof globalThis & { chrome?: BrowserAPI }).chrome;
  return browser?.runtime && typeof browser.runtime.connectNative === 'function' ? browser : undefined;
}
