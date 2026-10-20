import type { BrowserPort } from './browser.js';
import { decodeMessage, decodeRequest, HOST_NAME, MAX_MESSAGE_BYTES, type Analysis, type Capabilities, type CompletedFile, type Dependency, type Destination, type ListFormatsResult, type Progress, type StartPayload, type VideoChoice } from './protocol.js';

export const DESKTOP_PORT = 'dwnl-desktop-v2';
export type DesktopMethod = 'snapshot' | 'analyze' | 'listFormats' | 'enqueue' | 'cancel' | 'continue' | 'chooseDestination' | 'openApp';
export type EnqueuePayload = StartPayload & { requestId: string };
export interface DesktopTask { id: string; requestId: string; url: string; title: string; mode: 'video' | 'mp3'; bitrateKbps?: number; state: string; createdAt: string; error?: string; progress?: Progress; completedFile?: CompletedFile }
export interface DesktopSnapshot { version: string; readiness: 'preparing' | 'ready' | 'degraded'; capabilities: Capabilities; dependencies: Dependency[]; destination: Destination; paused: boolean; tasks: DesktopTask[]; error?: string }
export interface DesktopPayloads { snapshot: Record<string, never>; analyze: { url: string }; listFormats: { analysisId: string; itemId: string; cursor?: string }; enqueue: EnqueuePayload; cancel: { id: string }; continue: Record<string, never>; chooseDestination: Record<string, never>; openApp: Record<string, never> }
export interface DesktopResults { snapshot: DesktopSnapshot; analyze: Analysis; listFormats: ListFormatsResult; enqueue: DesktopTask; cancel: unknown; continue: unknown; chooseDestination: Destination & { canceled?: boolean }; openApp: unknown }
export interface DesktopRequest { version: 2; id: string; method: DesktopMethod; payload: DesktopPayloads[DesktopMethod] }
export interface DesktopResponse { version: 2; id: string; result?: unknown; error?: { code: string; message: string } }
export class DesktopError extends Error { constructor(public readonly code: string, message: string) { super(message); this.name = 'DesktopError'; } }
const methods: DesktopMethod[] = ['snapshot', 'analyze', 'listFormats', 'enqueue', 'cancel', 'continue', 'chooseDestination', 'openApp'];
const validId = (v: unknown): v is string => typeof v === 'string' && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(v);
const object = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
function assert(ok: unknown): asserts ok { if (!ok) throw new DesktopError('INVALID_MESSAGE', 'Resposta inválida do aplicativo. Reconecte para tentar novamente.'); }
function bounded(value: unknown): void { assert(new TextEncoder().encode(JSON.stringify(value)).length <= MAX_MESSAGE_BYTES); }
export function desktopRequest(value: unknown): DesktopRequest {
  bounded(value); assert(object(value)); assert(value.version === 2 && validId(value.id) && methods.includes(value.method as DesktopMethod));
  assert(Object.keys(value).every(key => ['version', 'id', 'method', 'payload'].includes(key)) && object(value.payload));
  const { method, payload } = value;
  if (method === 'analyze' || method === 'listFormats') decodeRequest({ protocolVersion: 1, requestId: value.id, hostSessionId: 'desktop', operation: method, payload });
  else if (method === 'enqueue') {
    assert(validId(payload.requestId));
    const { requestId: _, ...selection } = payload;
    decodeRequest({ protocolVersion: 1, requestId: value.id, hostSessionId: 'desktop', operation: 'start', payload: selection });
  } else if (method === 'cancel') assert(Object.keys(payload).length === 1 && validId(payload.id));
  else assert(Object.keys(payload).length === 0);
  return value as unknown as DesktopRequest;
}
function destination(value: unknown): void { assert(object(value) && validId(value.destinationId) && typeof value.label === 'string' && value.label.length <= 4096); }
function task(value: unknown): void {
  assert(object(value) && validId(value.id) && validId(value.requestId) && typeof value.url === 'string' && typeof value.title === 'string');
  assert(['video', 'mp3'].includes(value.mode as string) && typeof value.state === 'string' && ['queued', 'analyzing', 'ready', 'downloading', 'processing', 'muxing', 'converting', 'saving', 'completed', 'canceling', 'canceled', 'failed', 'interrupted'].includes(value.state));
  assert(typeof value.createdAt === 'string' && Number.isFinite(Date.parse(value.createdAt)));
  assert(value.error === undefined || typeof value.error === 'string');
  if (value.progress !== undefined) {
    assert(object(value.progress));
    assert(value.progress.percent === undefined || typeof value.progress.percent === 'number' && Number.isFinite(value.progress.percent) && value.progress.percent >= 0 && value.progress.percent <= 100);
  }
  if (value.completedFile !== undefined) assert(object(value.completedFile) && typeof value.completedFile.path === 'string');
}
export function desktopResponse(value: unknown, method: DesktopMethod): DesktopResponse {
  bounded(value); assert(object(value));
  if (value.version !== 2) throw new DesktopError('INCOMPATIBLE_PROTOCOL', 'Atualize o aplicativo e a extensão para a mesma versão.');
  assert(validId(value.id) && Object.keys(value).every(key => ['version', 'id', 'result', 'error'].includes(key)));
  if (value.error !== undefined) {
    assert(object(value.error) && typeof value.error.code === 'string' && typeof value.error.message === 'string');
    assert(value.result === undefined || value.result === null);
  } else {
    assert(Object.hasOwn(value, 'result'));
    const result = value.result;
    if (method === 'analyze' || method === 'listFormats') decodeMessage({ protocolVersion: 1, kind: 'response', requestId: value.id, hostSessionId: method === 'analyze' && object(result) ? result.hostSessionId : 'desktop', operation: method, result });
    else if (method === 'snapshot') {
      assert(object(result) && typeof result.version === 'string' && ['preparing', 'ready', 'degraded'].includes(result.readiness as string));
      assert(typeof result.paused === 'boolean' && object(result.capabilities) && ['video', 'mp3', 'networkAccess'].every(key => typeof (result.capabilities as Record<string, unknown>)[key] === 'boolean'));
      assert(Array.isArray(result.dependencies) && result.dependencies.every(dep => object(dep) && typeof dep.name === 'string' && typeof dep.available === 'boolean' && typeof dep.version === 'string'));
      assert(Array.isArray(result.tasks)); result.tasks.forEach(task); destination(result.destination);
      assert(result.error === undefined || typeof result.error === 'string');
    } else if (method === 'enqueue') task(result);
    else if (method === 'chooseDestination') { destination(result); assert(object(result) && (result.canceled === undefined || typeof result.canceled === 'boolean')); }
  }
  return value as unknown as DesktopResponse;
}

export class DesktopClient {
  private port?: BrowserPort;
  private pending = new Map<string, { method: DesktopMethod; signature: string; promise: Promise<unknown>; resolve(value: unknown): void; reject(error: unknown): void; timer: ReturnType<typeof setTimeout> }>();
  constructor(private connect: () => BrowserPort, private lastError: () => string | undefined = () => undefined, private timeoutMs = 10000) {}
  get busy(): boolean { return this.pending.size > 0; }
  request<M extends DesktopMethod>(method: M, payload: DesktopPayloads[M], id: string = crypto.randomUUID()): Promise<DesktopResults[M]> {
    try {
      const request = desktopRequest({ version: 2, id, method, payload });
      const signature = JSON.stringify(request);
      const previous = this.pending.get(id);
      if (previous) {
        if (previous.signature !== signature) throw new DesktopError('REQUEST_CONFLICT', 'Este envio já possui outra seleção.');
        return previous.promise as Promise<DesktopResults[M]>;
      }
      if (!this.port) {
        this.port = this.connect();
        this.port.onMessage.addListener(this.receive);
        this.port.onDisconnect.addListener(this.disconnected);
      }
      let resolve!: (value: unknown) => void; let reject!: (error: unknown) => void;
      const promise = new Promise<unknown>((yes, no) => { resolve = yes; reject = no; });
      const timeout = method === 'analyze' ? 120000 : method === 'chooseDestination' ? 300000 : this.timeoutMs;
      const timer = setTimeout(() => this.disconnect(new DesktopError('TIMEOUT', 'O aplicativo não respondeu. Reconecte para confirmar o estado da fila.')), timeout);
      this.pending.set(id, { method, signature, promise, resolve, reject, timer });
      try { this.port.postMessage(request); } catch { this.disconnect(); }
      return promise as Promise<DesktopResults[M]>;
    } catch (error) { return Promise.reject(error); }
  }
  private receive = (value: unknown): void => {
    try {
      assert(object(value) && validId(value.id));
      const pending = this.pending.get(value.id);
      if (!pending) return;
      const response = desktopResponse(value, pending.method);
      clearTimeout(pending.timer); this.pending.delete(response.id);
      if (response.error) pending.reject(new DesktopError(response.error.code, response.error.message));
      else pending.resolve(response.result);
    } catch (error) { this.disconnect(error instanceof DesktopError ? error : new DesktopError('INVALID_MESSAGE', 'Resposta inválida do aplicativo.')); }
  };
  private disconnected = (): void => {
    const missing = /native messaging host not found/i.test(this.lastError() ?? '');
    this.disconnect(new DesktopError(missing ? 'MISSING_HOST' : 'DISCONNECTED', missing ? 'Instale o aplicativo Dwnl para conectar a extensão.' : 'Conexão encerrada. Reconecte para continuar.'));
  };
  disconnect(error: DesktopError = new DesktopError('DISCONNECTED', 'Conexão encerrada. Reconecte para continuar.')): void {
    const port = this.port; this.port = undefined;
    if (port) { port.onMessage.removeListener(this.receive); port.onDisconnect.removeListener(this.disconnected); try { port.disconnect(); } catch { /* Already closed. */ } }
    for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(error); }
    this.pending.clear();
  }
}
export function nativeDesktopClient(connectNative: (name: string) => BrowserPort, lastError: () => string | undefined): DesktopClient { return new DesktopClient(() => connectNative(HOST_NAME), lastError); }
export function videoResolution(choice: VideoChoice): number | undefined { return choice.width && choice.height ? Math.min(choice.width, choice.height) : choice.height; }
export function bestDesktopVideo(choices: VideoChoice[]): VideoChoice | undefined {
  const compatible = (c: VideoChoice) => c.container === 'mp4' && /^(h264|avc1)/i.test(c.videoCodec) && (!c.hasAudio || /^(aac|mp4a)/i.test(c.audioCodec ?? '')) ? 1 : 0;
  return choices.filter(c => { const quality = videoResolution(c); return quality !== undefined && quality <= 1080; }).sort((a, b) => videoResolution(b)! - videoResolution(a)! || compatible(b) - compatible(a))[0];
}
