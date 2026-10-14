export const PROTOCOL_VERSION = 1 as const;
export const MAX_MESSAGE_BYTES = 524288;
export const HOST_NAME = "com.dwnl.host";
export const MP3_BITRATES = [128, 192, 320] as const;
export type MP3Bitrate = (typeof MP3_BITRATES)[number];
export type Operation = "handshake" | "analyze" | "listFormats" | "chooseDestination" | "start" | "status" | "reconcile" | "cancel";
export type JobState = "analyzing" | "ready" | "downloading" | "muxing" | "converting" | "saving" | "completed" | "canceling" | "canceled" | "failed" | "interrupted";
export type ErrorCode = "INVALID_MESSAGE" | "MESSAGE_TOO_LARGE" | "INCOMPATIBLE_PROTOCOL" | "UNKNOWN_OPERATION" | "INVALID_SELECTION" | "SESSION_ENDED" | "ANALYSIS_EXPIRED" | "INVALID_CURSOR" | "BUSY" | "REQUEST_CONFLICT" | "UNKNOWN_JOB" | "DEPENDENCY_UNAVAILABLE" | "MP3_UNAVAILABLE" | "NO_AUDIO" | "ACCESS_RESTRICTED" | "NETWORK_ERROR" | "NETWORK_POLICY_BLOCKED" | "DESTINATION_ERROR" | "DISK_FULL" | "ENGINE_ERROR" | "CANCEL_FAILED" | "INVALID_OUTPUT" | "INTERNAL_ERROR";
export interface ProtocolError { code: ErrorCode; message: string; retryable: boolean }
export interface Dependency { name: "yt-dlp" | "deno" | "ffmpeg" | "ffprobe"; version: string; available: boolean }
export interface Capabilities { video: boolean; mp3: boolean; networkAccess: boolean }
export interface Destination { destinationId: string; label: string }
export interface VideoChoice { optionId: string; container: "mp4" | "webm" | "mkv"; width?: number; height?: number; videoCodec: string; audioCodec?: string; hasAudio: boolean; audioUnknown?: boolean; sizeBytes?: number; sizeEstimated: boolean }
export interface MediaItem { itemId: string; title: string; durationSeconds?: number; hasAudio: boolean; audioUnknown?: boolean; videoChoices: VideoChoice[]; formatsCursor?: string; omittedMetadata: boolean }
export interface Analysis { analysisId: string; hostSessionId: string; sourceUrl: string; expiresAt: string; items: MediaItem[]; omittedMetadata: boolean }
export interface Progress { stage: "downloading" | "muxing" | "converting" | "saving"; completedBytes?: number; totalBytes?: number; processedSeconds?: number; durationSeconds?: number; percent?: number }
export interface CompletedFile { path: string; format: "mp4" | "webm" | "mkv" | "mp3"; sizeBytes: number; sha256: string; videoCodec?: string; audioCodec?: string; mp3BitrateKbps?: MP3Bitrate }
// Internal resolved engine input, never accepted by decodeRequest.
export interface DownloadPlan { requestId: string; hostSessionId: string; jobId: string; analysis: Analysis; item: MediaItem; mode: "video" | "mp3"; videoChoice?: VideoChoice; mp3BitrateKbps?: MP3Bitrate; destinationId: string; destinationPath: string; workDir: string }
export interface JobSnapshot { hostSessionId: string; jobId: string; startRequestId: string; sequence: number; state: JobState; progress?: Progress; completedFile?: CompletedFile; error?: ProtocolError }
export interface HandshakePayload { extensionVersion: string }
export interface AnalyzePayload { url: string }
export interface ListFormatsPayload { analysisId: string; itemId: string; cursor?: string }
export interface ChooseDestinationPayload { userGesture: true }
export type StartPayload = { analysisId: string; itemId: string; mode: "video"; videoOptionId: string; destinationId: string } | { analysisId: string; itemId: string; mode: "mp3"; bitrateKbps: MP3Bitrate; destinationId: string };
export type StatusPayload = { jobId: string } | { startRequestId: string } | Record<string, never>;
export type ReconcilePayload = { previousHostSessionId: string; jobId: string; startRequestId?: never } | { previousHostSessionId: string; startRequestId: string; jobId?: never };
export interface CancelPayload { jobId: string }
export interface PayloadByOperation { handshake: HandshakePayload; analyze: AnalyzePayload; listFormats: ListFormatsPayload; chooseDestination: ChooseDestinationPayload; start: StartPayload; status: StatusPayload; reconcile: ReconcilePayload; cancel: CancelPayload }
export interface HandshakeResult { hostVersion: string; engineVersion: string; dependencies: Dependency[]; capabilities: Capabilities; defaultDestination: Destination }
export interface ListFormatsResult { analysisId: string; itemId: string; choices: VideoChoice[]; nextCursor?: string; omittedMetadata: boolean }
export interface ChooseDestinationResult extends Destination { canceled: boolean }
export interface StartResult { jobId: string; startRequestId: string; sequence: number; state: JobState }
export interface StatusResult { snapshot: JobSnapshot | null }
export interface ReconcileResult { previousHostSessionId: string; outcome: "confirmed" | "unconfirmed" | "notFound"; snapshot: JobSnapshot | null }
export interface CancelResult { snapshot: JobSnapshot }
export interface ResultByOperation { handshake: HandshakeResult; analyze: Analysis; listFormats: ListFormatsResult; chooseDestination: ChooseDestinationResult; start: StartResult; status: StatusResult; reconcile: ReconcileResult; cancel: CancelResult }
export type Request = { [O in Operation]: { protocolVersion: 1; requestId: string; operation: O; payload: PayloadByOperation[O] } & (O extends "handshake" ? { hostSessionId?: never } : { hostSessionId: string }) }[Operation];
export type Response = { [O in Operation]: { protocolVersion: 1; kind: "response"; requestId: string; hostSessionId: string; operation: O } & ({ result: ResultByOperation[O]; error?: never } | { error: ProtocolError; result?: never }) }[Operation];
export interface JobEvent { protocolVersion: 1; kind: "event"; hostSessionId: string; jobId: string; sequence: number; state: JobState; progress?: Progress; completedFile?: CompletedFile; error?: ProtocolError }
export interface ValidationContext { hostSessionId: string; analysis?: Analysis; now?: string }
export interface NativeClient { request<O extends Operation>(operation: O, payload: PayloadByOperation[O], requestId: string): Promise<Response>; subscribe(listener: (message: Response | JobEvent) => void): () => void }
export class ProtocolDecodeError extends Error { constructor(public readonly code: ErrorCode, message: string) { super(message); this.name = "ProtocolDecodeError"; } }

type Rule = (value: unknown) => void;
type Fields = Record<string, Rule>;
function fail(code: ErrorCode = "INVALID_MESSAGE", message = "Invalid protocol message"): never { throw new ProtocolDecodeError(code, message); }
function requireCondition(condition: unknown): asserts condition { if (!condition) fail(); }
const text = (max = 512): Rule => value => requireCondition(typeof value === "string" && value.length > 0 && !/^[\p{White_Space}\uFEFF]*$/u.test(value) && new TextEncoder().encode(value).length <= max && !/[\u0000-\u001f\u007f]/.test(value));
const id: Rule = value => requireCondition(typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(value));
const bool: Rule = value => requireCondition(typeof value === "boolean");
const literal = (expected: unknown): Rule => value => requireCondition(value === expected);
const oneOf = (values: readonly unknown[]): Rule => value => requireCondition(values.includes(value));
const numeric = (min = 0, max = Number.MAX_VALUE, integer = false): Rule => value => requireCondition(typeof value === "number" && Number.isFinite(value) && value >= min && value <= max && (!integer || Number.isSafeInteger(value)));
const uint = numeric(0, Number.MAX_SAFE_INTEGER, true);
const positive = numeric(Number.MIN_VALUE);
const positiveInt = numeric(1, Number.MAX_SAFE_INTEGER, true);
function record(value: unknown): Record<string, unknown> {
  requireCondition(typeof value === "object" && value !== null && !Array.isArray(value));
  requireCondition(Object.getPrototypeOf(value) === Object.prototype || Object.getPrototypeOf(value) === null);
  return value as Record<string, unknown>;
}
function structure(required: Fields, optional: Fields = {}, refine?: (value: Record<string, unknown>) => void): Rule {
  return value => {
    const object = record(value);
    for (const key of Object.keys(object)) requireCondition(Object.hasOwn(required, key) || Object.hasOwn(optional, key));
    for (const [key, rule] of Object.entries(required)) { requireCondition(Object.hasOwn(object, key)); rule(object[key]); }
    for (const [key, rule] of Object.entries(optional)) if (Object.hasOwn(object, key)) rule(object[key]);
    refine?.(object);
  };
}
const list = (rule: Rule, min = 0): Rule => value => {
  requireCondition(Array.isArray(value) && value.length >= min && value.length <= 100);
  value.forEach(rule);
};
const nullable = (rule: Rule): Rule => value => { if (value !== null) rule(value); };
const operations: Operation[] = ["handshake", "analyze", "listFormats", "chooseDestination", "start", "status", "reconcile", "cancel"];
const states: JobState[] = ["analyzing", "ready", "downloading", "muxing", "converting", "saving", "completed", "canceling", "canceled", "failed", "interrupted"];
const terminalStates: JobState[] = ["completed", "canceled", "failed", "interrupted"];
const errorCodes: ErrorCode[] = ["INVALID_MESSAGE", "MESSAGE_TOO_LARGE", "INCOMPATIBLE_PROTOCOL", "UNKNOWN_OPERATION", "INVALID_SELECTION", "SESSION_ENDED", "ANALYSIS_EXPIRED", "INVALID_CURSOR", "BUSY", "REQUEST_CONFLICT", "UNKNOWN_JOB", "DEPENDENCY_UNAVAILABLE", "MP3_UNAVAILABLE", "NO_AUDIO", "ACCESS_RESTRICTED", "NETWORK_ERROR", "NETWORK_POLICY_BLOCKED", "DESTINATION_ERROR", "DISK_FULL", "ENGINE_ERROR", "CANCEL_FAILED", "INVALID_OUTPUT", "INTERNAL_ERROR"];
const allowedHosts = new Set(["youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be", "instagram.com", "www.instagram.com", "tiktok.com", "www.tiktok.com", "vm.tiktok.com", "vt.tiktok.com", "x.com", "www.x.com", "twitter.com", "www.twitter.com"]);
const sourceURL: Rule = value => {
  text(4096)(value);
  requireCondition(typeof value === "string" && value.startsWith("https://") && value.trim() === value && !/[\\ ]/.test(value) && !/%(?![0-9a-fA-F]{2})/.test(value) && !/^https:\/\/[^/?#]*@/.test(value));
  requireCondition(/^[-A-Za-z0-9.]+(?::443)?$/.test(value.slice(8).split(/[/?#]/)[0]));
  let url: URL;
  try { url = new URL(value); } catch { fail(); }
  requireCondition(url!.protocol === "https:" && !url!.username && !url!.password && (!url!.port || url!.port === "443") && allowedHosts.has(url!.hostname.toLowerCase()));
};
const timestamp: Rule = value => {
  requireCondition(typeof value === "string" && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3})?Z$/.test(value));
  const date = new Date(value);
  requireCondition(Number.isFinite(date.getTime()) && date.toISOString().slice(0, 19) === value.slice(0, 19));
};
const protocolError = structure({ code: oneOf(errorCodes), message: text(4096), retryable: bool });
const destination = { destinationId: id, label: text(4096) };
const videoChoice = structure({ optionId: id, container: oneOf(["mp4", "webm", "mkv"]), videoCodec: text(), hasAudio: bool, sizeEstimated: bool }, { width: positiveInt, height: positiveInt, audioCodec: text(), sizeBytes: uint, audioUnknown: bool }, value => {
  if (value.hasAudio === false) requireCondition(!Object.hasOwn(value, "audioCodec"));
  if (value.sizeEstimated === true) requireCondition(Object.hasOwn(value, "sizeBytes"));
});
function uniqueIDs(values: unknown, key: string) {
  const ids = (values as Record<string, unknown>[]).map(value => value[key]);
  requireCondition(new Set(ids).size === ids.length);
}
const mediaItem = structure({ itemId: id, title: text(4096), hasAudio: bool, videoChoices: list(videoChoice), omittedMetadata: bool }, { durationSeconds: positive, formatsCursor: id, audioUnknown: bool }, value => uniqueIDs(value.videoChoices, "optionId"));
const analysis = structure({ analysisId: id, hostSessionId: id, sourceUrl: sourceURL, expiresAt: timestamp, items: list(mediaItem, 1), omittedMetadata: bool }, {}, value => uniqueIDs(value.items, "itemId"));
const progress = structure({ stage: oneOf(["downloading", "muxing", "converting", "saving"]) }, { completedBytes: uint, totalBytes: positiveInt, processedSeconds: numeric(), durationSeconds: positive, percent: numeric(0, 100) }, value => {
  if (Object.hasOwn(value, "totalBytes")) requireCondition(Object.hasOwn(value, "completedBytes") && (value.completedBytes as number) <= (value.totalBytes as number));
  if (Object.hasOwn(value, "durationSeconds")) requireCondition(Object.hasOwn(value, "processedSeconds") && (value.processedSeconds as number) <= (value.durationSeconds as number));
  if (Object.hasOwn(value, "percent")) requireCondition(Object.hasOwn(value, "totalBytes") || Object.hasOwn(value, "durationSeconds"));
});
const completedFile = structure({ path: text(4096), format: oneOf(["mp4", "webm", "mkv", "mp3"]), sizeBytes: positiveInt, sha256: value => requireCondition(typeof value === "string" && /^[0-9a-f]{64}$/.test(value)) }, { videoCodec: text(), audioCodec: text(), mp3BitrateKbps: oneOf(MP3_BITRATES) }, value => {
  if (value.format === "mp3") requireCondition(value.audioCodec === "mp3" && Object.hasOwn(value, "mp3BitrateKbps") && !Object.hasOwn(value, "videoCodec"));
  else requireCondition(!Object.hasOwn(value, "mp3BitrateKbps"));
});
function stateDetails(value: Record<string, unknown>) {
  requireCondition((value.state === "completed") === Object.hasOwn(value, "completedFile"));
  if (value.state === "failed") requireCondition(Object.hasOwn(value, "error"));
  if (Object.hasOwn(value, "error")) requireCondition(value.state === "failed" || value.state === "interrupted");
  if (Object.hasOwn(value, "progress")) requireCondition((value.progress as Record<string, unknown>).stage === value.state);
}
const snapshot = structure({ hostSessionId: id, jobId: id, startRequestId: id, sequence: uint, state: oneOf(states) }, { progress, completedFile, error: protocolError }, stateDetails);
const startPayload: Rule = value => {
  const object = record(value);
  const common = { analysisId: id, itemId: id, destinationId: id };
  if (object.mode === "video") structure({ ...common, mode: literal("video"), videoOptionId: id })(object);
  else structure({ ...common, mode: literal("mp3"), bitrateKbps: oneOf(MP3_BITRATES) })(object);
};
const requestRules: Record<Operation, Rule> = {
  handshake: structure({ extensionVersion: text() }),
  analyze: structure({ url: sourceURL }),
  listFormats: structure({ analysisId: id, itemId: id }, { cursor: id }),
  chooseDestination: structure({ userGesture: literal(true) }),
  start: startPayload,
  status: structure({}, { jobId: id, startRequestId: id }, value => requireCondition(Object.keys(value).length <= 1)),
  reconcile: structure({ previousHostSessionId: id }, { jobId: id, startRequestId: id }, value => requireCondition(Number(Object.hasOwn(value, "jobId")) + Number(Object.hasOwn(value, "startRequestId")) === 1)),
  cancel: structure({ jobId: id }),
};
const resultRules: Record<Operation, Rule> = {
  handshake: structure({ hostVersion: text(), engineVersion: text(), dependencies: list(structure({ name: oneOf(["yt-dlp", "deno", "ffmpeg", "ffprobe"]), version: text(), available: bool })), capabilities: structure({ video: bool, mp3: bool, networkAccess: bool }), defaultDestination: structure(destination) }, {}, value => uniqueIDs(value.dependencies, "name")),
  analyze: analysis,
  listFormats: structure({ analysisId: id, itemId: id, choices: list(videoChoice), omittedMetadata: bool }, { nextCursor: id }, value => uniqueIDs(value.choices, "optionId")),
  chooseDestination: structure({ ...destination, canceled: bool }),
  start: structure({ jobId: id, startRequestId: id, sequence: uint, state: oneOf(states) }),
  status: structure({ snapshot: nullable(snapshot) }),
  reconcile: structure({ previousHostSessionId: id, outcome: oneOf(["confirmed", "unconfirmed", "notFound"]), snapshot: nullable(snapshot) }, {}, value => {
    const job = value.snapshot as JobSnapshot | null;
    if (job) requireCondition(job.hostSessionId === value.previousHostSessionId && terminalStates.includes(job.state));
    if (value.outcome === "confirmed") requireCondition(job !== null);
    if (value.outcome === "unconfirmed" && job) requireCondition(job.state === "interrupted");
    if (value.outcome === "notFound") requireCondition(job === null);
  }),
  cancel: structure({ snapshot }),
};
function checkSize(value: unknown) {
  let json: string | undefined;
  try { json = JSON.stringify(value); } catch { fail(); }
  requireCondition(json !== undefined);
  if (new TextEncoder().encode(json).length > MAX_MESSAGE_BYTES) fail("MESSAGE_TOO_LARGE", "Protocol message exceeds 512 KiB");
}
function envelope(value: unknown, needsOperation: boolean): Record<string, unknown> {
  checkSize(value);
  const object = record(value);
  if (Object.hasOwn(object, "protocolVersion") && object.protocolVersion !== PROTOCOL_VERSION) fail("INCOMPATIBLE_PROTOCOL", "Protocol version is incompatible");
  requireCondition(object.protocolVersion === PROTOCOL_VERSION);
  if (needsOperation) {
    requireCondition(typeof object.operation === "string");
    if (!operations.includes(object.operation as Operation)) fail("UNKNOWN_OPERATION", "Operation is unsupported");
  }
  return object;
}
export function decodeRequest(value: unknown, context?: ValidationContext): Request {
  const object = envelope(value, true);
  const operation = object.operation as Operation;
  structure({ protocolVersion: literal(1), requestId: id, operation: literal(operation), payload: requestRules[operation], ...(operation === "handshake" ? {} : { hostSessionId: id }) })(object);
  const request = object as unknown as Request;
  if (request.operation === "reconcile" && request.payload.previousHostSessionId === request.hostSessionId) fail("SESSION_ENDED", "Reconciliation requires a previous host session");
  if (context) validateRequestContext(request, context);
  return request;
}
export function validateRequestContext(request: Request, context: ValidationContext): void {
  if (request.operation === "handshake") return;
  if (request.hostSessionId !== context.hostSessionId) fail("SESSION_ENDED", "Host session has ended");
  if (request.operation !== "start" && request.operation !== "listFormats") return;
  const registered = context.analysis;
  if (!registered || registered.analysisId !== request.payload.analysisId) fail("INVALID_SELECTION", "Analysis is not registered in the current session");
  if (registered.hostSessionId !== context.hostSessionId) fail("SESSION_ENDED", "Analysis belongs to another host session");
  const now = context.now ?? new Date().toISOString();
  timestamp(now);
  timestamp(registered.expiresAt);
  if (new Date(now).getTime() >= new Date(registered.expiresAt).getTime()) fail("ANALYSIS_EXPIRED", "Analysis has expired");
  const item = registered.items.find(value => value.itemId === request.payload.itemId);
  if (!item) fail("INVALID_SELECTION", "Item is not part of the registered analysis");
  if (request.operation === "start") {
    if (request.payload.mode === "video") {
      const selectedOptionId = request.payload.videoOptionId;
      if (!item.videoChoices.some(value => value.optionId === selectedOptionId)) fail("INVALID_SELECTION", "Video option is not part of the registered item");
    }
    if (request.payload.mode === "mp3" && !item.hasAudio && !item.audioUnknown) fail("NO_AUDIO", "Selected item has no audio");
  }
}
export function decodeMessage(value: unknown): Response | JobEvent {
  const raw = record(value);
  const object = envelope(value, raw.kind === "response");
  if (object.kind === "event") {
    structure({ protocolVersion: literal(1), kind: literal("event"), hostSessionId: id, jobId: id, sequence: uint, state: oneOf(states) }, { progress, completedFile, error: protocolError }, stateDetails)(object);
    return object as unknown as JobEvent;
  }
  requireCondition(object.kind === "response");
  const operation = object.operation as Operation;
  const hasResult = Object.hasOwn(object, "result");
  requireCondition(hasResult !== Object.hasOwn(object, "error"));
  structure({ protocolVersion: literal(1), kind: literal("response"), requestId: id, hostSessionId: id, operation: literal(operation), ...(hasResult ? { result: resultRules[operation] } : { error: protocolError }) })(object);
  if (hasResult) {
    const result = object.result as Record<string, unknown>;
    if (operation === "analyze") requireCondition(result.hostSessionId === object.hostSessionId);
    if ((operation === "status" || operation === "cancel") && result.snapshot) requireCondition((result.snapshot as JobSnapshot).hostSessionId === object.hostSessionId);
    if (operation === "start") requireCondition(result.startRequestId === object.requestId);
    if (operation === "reconcile") requireCondition(result.previousHostSessionId !== object.hostSessionId);
  }
  return object as unknown as Response;
}
export function decodeJSON(json: string, direction: "request" | "message", context?: ValidationContext): Request | Response | JobEvent {
  if (new TextEncoder().encode(json).length > MAX_MESSAGE_BYTES) fail("MESSAGE_TOO_LARGE", "Protocol message exceeds 512 KiB");
  let value: unknown;
  try { value = JSON.parse(json); } catch { fail(); }
  return direction === "request" ? decodeRequest(value, context) : decodeMessage(value);
}
