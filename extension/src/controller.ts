import { decodeMessage, decodeRequest, HOST_NAME, ProtocolDecodeError, type NativeClient, type Operation, type PayloadByOperation, type Response, type JobEvent } from './protocol.js';
import type { BrowserPort } from './browser.js';
import { createInitialState, isTerminal, reduceState, restoreCache, serializeCache, type AppState, type ConnectionState } from './state.js';
export class NativeTransportError extends Error { constructor(public readonly reason: ConnectionState | 'timeout', message = reason) { super(message); } }
export class ChromeNativeClient implements NativeClient {
  private port?: BrowserPort;
  private sessionId?: string;
  private listeners = new Set<(message: Response | JobEvent) => void>();
  private connectionListeners = new Set<(state: ConnectionState) => void>();
  private pending = new Map<string, { signature: string; operation: Operation; promise: Promise<Response>; resolve(value: Response): void; reject(error: unknown): void; timer: ReturnType<typeof setTimeout> }>();
  private completed = new Map<string, { signature: string; response: Response }>();
  constructor(private connectNative: (name: string) => BrowserPort, private lastError: () => string | undefined, private timeoutMs = 30000) {}
  request<O extends Operation>(operation: O, payload: PayloadByOperation[O], requestId: string): Promise<Response> {
    try {
      if (operation !== 'handshake' && !this.sessionId) throw new NativeTransportError('disconnected');
      const request = decodeRequest({ protocolVersion: 1, requestId, operation, payload, ...(operation !== 'handshake' ? { hostSessionId: this.sessionId } : {}) });
      const signature = JSON.stringify(request);
      const existing = this.pending.get(requestId) ?? this.completed.get(requestId);
      if (existing) {
        if (existing.signature !== signature) throw new ProtocolDecodeError('REQUEST_CONFLICT', 'Request identity already has a different payload');
        return 'promise' in existing ? existing.promise : Promise.resolve(existing.response);
      }
      if (!this.port) {
        this.port = this.connectNative(HOST_NAME);
        this.port.onMessage.addListener(this.receive);
        this.port.onDisconnect.addListener(this.disconnected);
        this.connectionListeners.forEach(listener => listener('connecting'));
      }
      let resolve!: (value: Response) => void;
      let reject!: (error: unknown) => void;
      const promise = new Promise<Response>((yes, no) => { resolve = yes; reject = no; });
      const timer = setTimeout(() => { this.pending.delete(requestId); reject(new NativeTransportError('timeout')); }, operation === 'chooseDestination' ? Math.max(this.timeoutMs, 300000) : this.timeoutMs);
      this.pending.set(requestId, { signature, operation, promise, resolve, reject, timer });
      try { this.port.postMessage(request); } catch { this.failConnection('disconnected'); }
      return promise;
    } catch (error) { return Promise.reject(error); }
  }
  private receive = (value: unknown): void => {
    let message: Response | JobEvent;
    try { message = decodeMessage(value); }
    catch (error) { this.failConnection(error instanceof ProtocolDecodeError && error.code === 'INCOMPATIBLE_PROTOCOL' ? 'incompatible' : 'error'); return; }
    if (message.kind === 'event') {
      if (message.hostSessionId === this.sessionId) this.listeners.forEach(listener => listener(message));
      return;
    }
    const pending = this.pending.get(message.requestId);
    if (!pending || (message.operation !== 'handshake' && message.hostSessionId !== this.sessionId)) return;
    if (message.operation !== pending.operation) { this.failConnection('error'); return; }
    if (message.operation === 'handshake' && message.result) {
      this.sessionId = message.hostSessionId;
      this.connectionListeners.forEach(listener => listener('connected'));
    }
    clearTimeout(pending.timer);
    this.pending.delete(message.requestId);
    this.completed.set(message.requestId, { signature: pending.signature, response: message });
    if (this.completed.size > 100) this.completed.delete(this.completed.keys().next().value!);
    this.listeners.forEach(listener => listener(message));
    pending.resolve(message);
  };
  private disconnected = (): void => {
    const error = this.lastError();
    this.failConnection(error && /native messaging host not found/i.test(error) ? 'missing-host' : 'disconnected');
  };
  private failConnection(reason: ConnectionState): void {
    const port = this.port;
    this.port = undefined;
    this.sessionId = undefined;
    if (port) { port.onMessage.removeListener(this.receive); port.onDisconnect.removeListener(this.disconnected); try { port.disconnect(); } catch { /* Already disconnected. */ } }
    this.pending.forEach(pending => { clearTimeout(pending.timer); pending.reject(new NativeTransportError(reason)); });
    this.pending.clear(); this.completed.clear();
    this.connectionListeners.forEach(listener => listener(reason));
  }
  subscribe(listener: (message: Response | JobEvent) => void): () => void { this.listeners.add(listener); return () => this.listeners.delete(listener); }
  onConnection(listener: (state: ConnectionState) => void): () => void { this.connectionListeners.add(listener); return () => this.connectionListeners.delete(listener); }
}
export interface CacheStore { load(): Promise<unknown>; save(cache: unknown): Promise<void> }
export class ExtensionController {
  state: AppState = createInitialState();
  private listeners = new Set<(state: AppState, message?: Response | JobEvent) => void>();
  private loaded = false;
  private connecting?: Promise<void>;
  private saving: Promise<void> = Promise.resolve();
  constructor(private native: NativeClient, private cache: CacheStore, private extensionVersion: string) {
    native.subscribe(message => { this.state = reduceState(this.state, message); this.publish(message); });
    if (native instanceof ChromeNativeClient) native.onConnection(connection => {
      this.state = { ...this.state, connection, ...((connection !== 'connected' && (this.state.job || this.state.pendingStartRequestId)) ? { confirmation: 'checking' as const } : {}) }; this.publish();
    });
  }
  connect(): Promise<void> {
    if (this.connecting) return this.connecting;
    this.connecting = this.initialize().finally(() => { this.connecting = undefined; });
    return this.connecting;
  }
  private async initialize(): Promise<void> {
    try {
      if (!this.loaded) { this.state = restoreCache(await this.cache.load()); this.loaded = true; this.publish(); }
      const previous = this.state.job;
      const pending = this.state.pendingStartRequestId;
      const previousSession = pending ? this.state.pendingStartHostSessionId : previous?.hostSessionId;
      this.state = { ...this.state, connection: 'connecting', error: undefined }; this.publish();
      const handshake = await this.native.request('handshake', { extensionVersion: this.extensionVersion }, requestId());
      this.state = reduceState(this.state, handshake);
      if (handshake.error) { this.publish(); return; }
      if (previousSession && previousSession !== handshake.hostSessionId) {
        await this.native.request('reconcile', { previousHostSessionId: previousSession, ...(pending ? { startRequestId: pending } : { jobId: previous!.jobId }) }, requestId());
      } else {
        await this.native.request('status', pending ? { startRequestId: pending } : {}, requestId());
      }
      this.publish();
    } catch (error) { this.transportFailure(error); }
  }
  async request<O extends Operation>(operation: O, payload: PayloadByOperation[O], id: string): Promise<Response> {
    if (operation === 'handshake') { await this.connect(); return this.native.request(operation, payload, id); }
    if (this.state.connection !== 'connected') throw new NativeTransportError(this.state.connection);
    decodeRequest({ protocolVersion: 1, hostSessionId: this.state.hostSessionId, requestId: id, operation, payload });
    const replay = operation === 'start' && this.state.job?.hostSessionId === this.state.hostSessionId && this.state.job?.startRequestId === id;
    if (operation === 'start') {
      if (this.state.pendingStartRequestId && this.state.pendingStartRequestId !== id || this.state.job && !isTerminal(this.state.job.state) && this.state.job.startRequestId !== id) throw new ProtocolDecodeError('BUSY', 'A task is already active');
      if (this.state.confirmation === 'checking' && this.state.pendingStartRequestId !== id) throw new ProtocolDecodeError('BUSY', 'Previous task must be confirmed first');
      if (!replay) { this.state = { ...this.state, pendingStartRequestId: id, pendingStartHostSessionId: this.state.hostSessionId, confirmation: 'checking', error: undefined }; this.publish(); }
    }
    try {
      const response = await this.native.request(operation, payload, id);
      this.state = reduceState(this.state, response); this.publish();
      if (response.operation === 'start' && response.result && (replay || isTerminal(response.result.state))) await this.native.request('status', { jobId: response.result.jobId }, requestId());
      return response;
    } catch (error) {
      if (operation === 'start' && error instanceof NativeTransportError) {
        try {
          const status = await this.native.request('status', { startRequestId: id }, requestId());
          if (status.operation === 'status' && status.result?.snapshot?.startRequestId === id) {
            const snapshot = status.result.snapshot;
            return { protocolVersion: 1, kind: 'response', hostSessionId: status.hostSessionId, operation: 'start', requestId: id, result: { jobId: snapshot.jobId, startRequestId: id, sequence: snapshot.sequence, state: snapshot.state } };
          }
          if (status.operation === 'status' && status.result?.snapshot === null) { this.state = { ...this.state, pendingStartRequestId: undefined, pendingStartHostSessionId: undefined, confirmation: 'confirmed' }; this.publish(); }
        } catch { /* Keep the original start identity for read-only reconciliation. */ }
      }
      this.transportFailure(error);
      throw error;
    }
  }
  private transportFailure(error: unknown): void {
    if (error instanceof NativeTransportError && error.reason !== 'timeout') this.state = { ...this.state, connection: error.reason };
    if (error instanceof NativeTransportError && error.reason === 'timeout') this.state = { ...this.state, error: { code: 'NETWORK_ERROR', message: 'O auxiliar não respondeu. Verifique a tarefa antes de tentar novamente.', retryable: true } };
    else if (error instanceof ProtocolDecodeError) this.state = { ...this.state, error: { code: error.code, message: error.code, retryable: false } };
    this.publish();
  }
  private publish(message?: Response | JobEvent): void {
    this.listeners.forEach(listener => listener(this.state, message));
    const cache = serializeCache(this.state);
    this.saving = this.saving.then(() => this.cache.save(cache)).catch(() => { this.state = { ...this.state, error: { code: 'INTERNAL_ERROR', message: 'Não foi possível guardar o histórico local.', retryable: true } }; this.listeners.forEach(listener => listener(this.state)); });
  }
  subscribe(listener: (state: AppState, message?: Response | JobEvent) => void): () => void { this.listeners.add(listener); return () => this.listeners.delete(listener); }
  async snapshot(): Promise<AppState> {
    if (this.connecting) await this.connecting;
    if (!this.loaded) await this.connect();
    else if (this.state.connection === 'connected') {
      try { await this.native.request('status', this.state.pendingStartRequestId ? { startRequestId: this.state.pendingStartRequestId } : {}, requestId()); } catch (error) { this.transportFailure(error); }
    }
    return this.state;
  }
  clearHistory(): void {
    const terminal = this.state.job && isTerminal(this.state.job.state) ? this.state.job : undefined;
    this.state = { ...this.state, history: [], ...(terminal ? { job: undefined, clearedJob: { hostSessionId: terminal.hostSessionId, jobId: terminal.jobId } } : {}) }; this.publish();
  }
}
export function requestId(): string { return `req-${crypto.randomUUID()}`; }
