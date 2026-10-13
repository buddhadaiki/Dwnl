import type { Analysis, HandshakeResult, JobSnapshot, Operation, ProtocolError, Response, ResultByOperation, VideoChoice } from '../src/protocol.js';

export const choices: VideoChoice[] = [
  { optionId: 'webm1080', container: 'webm', height: 1080, width: 1920, videoCodec: 'vp9', audioCodec: 'opus', hasAudio: true, sizeEstimated: false },
  { optionId: 'mp4720', container: 'mp4', height: 720, width: 1280, videoCodec: 'h264', audioCodec: 'aac', hasAudio: true, sizeEstimated: false },
  { optionId: 'mp41080', container: 'mp4', height: 1080, width: 1920, videoCodec: 'h264', audioCodec: 'aac', hasAudio: true, sizeEstimated: false },
];
export const handshake: HandshakeResult = {
  hostVersion: '0.1.0', engineVersion: 'fixture',
  dependencies: [{ name: 'ffmpeg', version: 'fixture', available: true }],
  capabilities: { video: true, mp3: true, networkAccess: true },
  defaultDestination: { destinationId: 'destination-a', label: 'Downloads/Dwnl' },
};
export function analysis(videoChoices = choices, hasAudio = true): Analysis {
  if (!hasAudio) videoChoices = videoChoices.map(({ audioCodec: _, ...choice }) => ({ ...choice, hasAudio: false }));
  return { analysisId: 'analysis-a', hostSessionId: 'session-a', sourceUrl: 'https://www.youtube.com/watch?v=public', expiresAt: '2099-01-01T00:00:00Z', omittedMetadata: false,
    items: [{ itemId: 'item-a', title: 'Um vídeo público', durationSeconds: 123, hasAudio, videoChoices, omittedMetadata: false }] };
}
export function response<O extends Operation>(operation: O, result: ResultByOperation[O], requestId = `req-${operation}`, hostSessionId = 'session-a'): Response {
  return { protocolVersion: 1, kind: 'response', requestId, hostSessionId, operation, result } as Response;
}
export function job(overrides: Partial<JobSnapshot> = {}): JobSnapshot {
  return { hostSessionId: 'session-a', jobId: 'job-a', startRequestId: 'req-start', sequence: 1, state: 'downloading', ...overrides };
}
export function startFailure(code: ProtocolError['code'], requestId: string): Response {
  return { protocolVersion: 1, kind: 'response', hostSessionId: 'session-a', operation: 'start', requestId, error: { code, message: code, retryable: true } };
}
