import { decodeMessage, type NativeClient, type Operation, type PayloadByOperation, type Response, type JobEvent, ProtocolDecodeError } from './protocol.js';
import type { BrowserPort } from './browser.js';
import { NativeTransportError } from './controller.js';
import { createInitialState, type AppState, type ConnectionState } from './state.js';

export class PopupNativeClient implements NativeClient {
  private pending = new Map<string, { signature: string; operation: Operation; promise: Promise<Response>; resolve(value: Response): void; reject(error: unknown): void }>();
  private listeners = new Set<(message: Response | JobEvent) => void>();
  private stateListeners = new Set<(state: AppState) => void>();
  private snapshotWaiters: ((state: AppState) => void)[] = [];
  private closed = false;
  private lastState = createInitialState();
  constructor(private port: BrowserPort, private reconnectPort?: () => BrowserPort) { this.attach(); }
  private attach(): void { this.port.onMessage.addListener(this.receive); this.port.onDisconnect.addListener(this.disconnected); }
  request<O extends Operation>(operation: O, payload: PayloadByOperation[O], requestId: string): Promise<Response> {
    if (this.closed) return Promise.reject(new NativeTransportError('disconnected'));
    const signature = JSON.stringify({ operation, payload });
    const existing = this.pending.get(requestId);
    if (existing) return existing.signature === signature ? existing.promise : Promise.reject(new ProtocolDecodeError('REQUEST_CONFLICT', 'Request identity already has a different payload'));
    let resolve!: (response: Response) => void; let reject!: (error: unknown) => void;
    const promise = new Promise<Response>((yes, no) => { resolve = yes; reject = no; });
    this.pending.set(requestId, { signature, operation, promise, resolve, reject });
    try { this.port.postMessage({ kind: 'request', operation, payload, requestId }); }
    catch { this.pending.delete(requestId); reject(new NativeTransportError('disconnected')); }
    return promise;
  }
  subscribe(listener: (message: Response | JobEvent) => void): () => void { this.listeners.add(listener); return () => this.listeners.delete(listener); }
  subscribeState(listener: (state: AppState) => void): () => void { this.stateListeners.add(listener); return () => this.stateListeners.delete(listener); }
  snapshot(): Promise<AppState> {
    if (this.closed) return Promise.reject(new NativeTransportError('disconnected'));
    return new Promise(resolve => { this.snapshotWaiters.push(resolve); this.port.postMessage({ kind: 'snapshot' }); });
  }
  reconnect(): Promise<AppState> {
    if (this.closed) {
      if (!this.reconnectPort) return Promise.reject(new NativeTransportError('disconnected'));
      this.port = this.reconnectPort(); this.closed = false; this.attach();
    }
    return new Promise(resolve => { this.snapshotWaiters.push(resolve); this.port.postMessage({ kind: 'reconnect' }); });
  }
  clearHistory(): void { if (!this.closed) this.port.postMessage({ kind: 'clearHistory' }); }
  close(): void { if (!this.closed) { this.port.disconnect(); this.disconnected(); } }
  private receive = (value: unknown): void => {
    if (typeof value !== 'object' || !value) return;
    const packet = value as { kind?: string; state?: AppState; message?: unknown; requestId?: string; response?: unknown; reason?: ConnectionState; code?: string };
    if (packet.kind === 'state' && packet.state) {
      this.lastState = packet.state;
      this.stateListeners.forEach(listener => listener(packet.state!));
      if (packet.message) {
        try { const message = decodeMessage(packet.message); this.listeners.forEach(listener => listener(message)); } catch { this.disconnected(); }
      }
    } else if (packet.kind === 'snapshot' && packet.state) {
      this.lastState = packet.state;
      this.stateListeners.forEach(listener => listener(packet.state!));
      this.snapshotWaiters.splice(0).forEach(resolve => resolve(packet.state!));
    } else if (packet.kind === 'result' && packet.requestId) {
      const pending = this.pending.get(packet.requestId); if (!pending) return;
      this.pending.delete(packet.requestId);
      try { const response = decodeMessage(packet.response); if (response.kind !== 'response' || response.requestId !== packet.requestId || response.operation !== pending.operation) throw new Error(); pending.resolve(response); }
      catch { pending.reject(new NativeTransportError('error')); }
    } else if (packet.kind === 'failure' && packet.requestId) {
      const pending = this.pending.get(packet.requestId); this.pending.delete(packet.requestId);
      pending?.reject(packet.code ? new ProtocolDecodeError(packet.code as ProtocolDecodeError['code'], packet.code) : new NativeTransportError(packet.reason ?? 'error'));
    }
  };
  private disconnected = (): void => {
    if (this.closed) return;
    this.closed = true;
    this.port.onMessage.removeListener(this.receive); this.port.onDisconnect.removeListener(this.disconnected);
    this.pending.forEach(pending => pending.reject(new NativeTransportError('disconnected'))); this.pending.clear();
    this.lastState = { ...this.lastState, connection: 'disconnected', confirmation: this.lastState.job || this.lastState.pendingStartRequestId ? 'checking' : 'unconfirmed' };
    this.stateListeners.forEach(listener => listener(this.lastState));
    this.snapshotWaiters.splice(0).forEach(resolve => resolve(this.lastState));
  };
}
