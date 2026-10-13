import { describe, expect, it } from 'vitest';
import { chooseBestVideo, createInitialState, reduceState, restoreCache, selectItem, serializeCache, startPayload } from '../src/state.js';
import { analysis, choices, handshake, job, response } from './fixtures.js';

function ready() { return reduceState(reduceState(createInitialState(), response('handshake', handshake)), response('analyze', analysis())); }
describe('popup state', () => {
  it('ChooseBestAtMost1080 prefers compatible MP4 only at equal quality', () => {
    expect(chooseBestVideo(choices)?.optionId).toBe('mp41080');
    expect(chooseBestVideo(choices.slice(0, 2))?.optionId).toBe('webm1080');
  });
  it('RequireChoiceIfOnlyAbove1080 and never invent unknown dimensions', () => {
    const high = { ...choices[0], height: 2160, width: 3840 };
    expect(chooseBestVideo([high])).toBeUndefined();
    const { height: _, width: __, ...unknown } = high;
    expect(chooseBestVideo([unknown])).toBeUndefined();
    expect(startPayload(reduceState(ready(), response('analyze', analysis([high]))))).toBeUndefined();
  });
  it('Mp3192DefaultAndNoAudioDisabled preserves the approved output options', () => {
    const state = ready();
    expect(state.bitrateKbps).toBe(192);
    expect(startPayload({ ...state, mode: 'mp3', bitrateKbps: 128 })).toMatchObject({ mode: 'mp3', bitrateKbps: 128 });
    expect(startPayload({ ...state, mode: 'mp3', bitrateKbps: 320 })).toMatchObject({ mode: 'mp3', bitrateKbps: 320 });
    const noAudio = reduceState(state, response('analyze', analysis(choices, false)));
    expect(startPayload({ ...noAudio, mode: 'mp3' })).toBeUndefined();
    expect(selectItem({ ...noAudio, mode: 'mp3' }, 'item-a').mode).toBe('video');
  });
  it('IgnoreOldSequence, foreign sessions/jobs and regression after canceling', () => {
    let state = reduceState(ready(), response('status', { snapshot: job({ sequence: 4, state: 'canceling' }) }));
    const event = { protocolVersion: 1 as const, kind: 'event' as const, hostSessionId: 'session-a', jobId: 'job-a', sequence: 4, state: 'downloading' as const };
    expect(reduceState(state, event).job?.state).toBe('canceling');
    expect(reduceState(state, { ...event, sequence: 5 }).job?.state).toBe('canceling');
    expect(reduceState(state, { ...event, hostSessionId: 'other', sequence: 8 }).job?.sequence).toBe(4);
    expect(reduceState(state, { ...event, jobId: 'other', sequence: 8 }).job?.sequence).toBe(4);
    state = reduceState(state, { ...event, sequence: 6, state: 'canceled' });
    expect(state.job?.state).toBe('canceled');
    expect(reduceState(state, { ...event, sequence: 9 }).job?.state).toBe('canceled');
  });
  it('UnknownProgressHasNoFakePercent', () => {
    const state = reduceState(ready(), response('status', { snapshot: job({ progress: { stage: 'downloading', completedBytes: 500 } }) }));
    expect(state.job?.progress?.percent).toBeUndefined();
    expect(state.job?.progress?.completedBytes).toBe(500);
  });
  it('keeps only 20 unique terminal records and clearing cache never stores URLs', () => {
    let state = ready();
    for (let i = 0; i < 23; i++) state = reduceState(state, response('status', { snapshot: job({ jobId: `job-${i}`, startRequestId: `start-${i}`, state: 'canceled' }) }));
    expect(state.history).toHaveLength(20);
    expect(state.history[0].jobId).toBe('job-22');
    state = reduceState(state, response('status', { snapshot: state.history[0] }));
    expect(state.history).toHaveLength(20);
    const cache = JSON.stringify(serializeCache(state));
    expect(cache).not.toContain('youtube');
    expect(cache).not.toContain('analysis-a');
  });
  it('cached active task stays unconfirmed until authoritative snapshot', () => {
    const state = restoreCache({ version: 1, job: job(), history: [] });
    expect(state.confirmation).toBe('checking');
    expect(startPayload({ ...ready(), ...state })).toBeUndefined();
    const current = reduceState(reduceState(state, response('handshake', handshake)), response('status', { snapshot: job({ sequence: 7 }) }));
    expect(current.confirmation).toBe('confirmed');
    expect(current.job?.sequence).toBe(7);
    expect(restoreCache({ version: 1, job: { ...job(), path: 'untrusted' }, history: [] }).job).toBeUndefined();
  });
  it('expired analysis and pending acknowledgement cannot start a second task', () => {
    expect(startPayload({ ...ready(), pendingStartRequestId: 'start-pending' })).toBeUndefined();
    const expired = ready();
    expired.analysis = { ...analysis(), expiresAt: '2000-01-01T00:00:00Z' };
    expect(startPayload(expired)).toBeUndefined();
  });
  it('terminal start acknowledgement does not claim a verified file before status', () => {
    const state = reduceState(ready(), response('start', { jobId: 'job-a', startRequestId: 'req-start', sequence: 7, state: 'completed' }));
    expect(state.confirmation).toBe('checking');
    expect(state.job?.completedFile).toBeUndefined();
    expect(state.pendingStartRequestId).toBe('req-start');
  });
  it('null status confirms that an uncertain start in this session was not accepted', () => {
    const state = reduceState({ ...ready(), pendingStartRequestId: 'pending', pendingStartHostSessionId: 'session-a', confirmation: 'checking' }, response('status', { snapshot: null }));
    expect(state.pendingStartRequestId).toBeUndefined();
    expect(state.confirmation).toBe('confirmed');
  });
  it('canceling a folder dialog retains the approved destination ID', () => {
    const state = reduceState(ready(), response('chooseDestination', { destinationId: 'destination-b', label: 'Other', canceled: true }));
    expect(state.destination).toEqual({ destinationId: 'destination-a', label: 'Downloads/Dwnl' });
  });
  it('the active or uncertain task also counts toward the 20-task persisted limit', () => {
    let state = ready();
    for (let i = 0; i < 20; i++) state = reduceState(state, response('status', { snapshot: job({ jobId: `old-${i}`, startRequestId: `old-start-${i}`, state: 'canceled' }) }));
    state = reduceState(state, response('status', { snapshot: job({ jobId: 'active', startRequestId: 'active-start' }) }));
    const cache = serializeCache(state) as { job: ReturnType<typeof job>; history: ReturnType<typeof job>[] };
    expect(new Set([cache.job.jobId, ...cache.history.map(record => record.jobId)]).size).toBe(20);
  });
});
