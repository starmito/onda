import { describe, it, expect } from 'vitest';
import {
  isJobSettled,
  isQueueSettled,
  hasActiveJob,
  hasActiveQueueFile,
  decideCompletion,
} from './queueCompletion';
import type { QueueJob } from './api';
import type { QueueFile } from './queueDefaults';

function makeJob(overrides: Partial<QueueJob> & { song: string; status: QueueJob['status'] }): QueueJob {
  return {
    song: overrides.song,
    status: overrides.status,
    progress: 0,
    ...overrides,
  };
}

function makeQueueFile(status: string): QueueFile {
  return {
    file: new File([], 'song.wav'),
    id: 'id',
    status,
    checked: false,
  };
}

describe('isJobSettled', () => {
  it('treats done single-step jobs as settled', () => {
    expect(isJobSettled(makeJob({ song: 'a', status: 'done' }))).toBe(true);
  });

  it('treats error jobs as settled', () => {
    expect(isJobSettled(makeJob({ song: 'a', status: 'error' }))).toBe(true);
  });

  it('does not treat processing jobs as settled', () => {
    expect(isJobSettled(makeJob({ song: 'a', status: 'processing' }))).toBe(false);
  });

  it('requires the last step for chained done jobs', () => {
    const notLast = makeJob({ song: 'a', status: 'done', current_step: 1, total_steps: 2 });
    expect(isJobSettled(notLast)).toBe(false);

    const last = makeJob({ song: 'a', status: 'done', current_step: 2, total_steps: 2 });
    expect(isJobSettled(last)).toBe(true);
  });
});

describe('isQueueSettled', () => {
  it('returns false for an empty queue', () => {
    expect(isQueueSettled([])).toBe(false);
  });

  it('returns true when all jobs are settled', () => {
    const jobs = [
      makeJob({ song: 'a', status: 'done' }),
      makeJob({ song: 'b', status: 'error' }),
    ];
    expect(isQueueSettled(jobs)).toBe(true);
  });

  it('returns false when any job is still active', () => {
    const jobs = [
      makeJob({ song: 'a', status: 'done' }),
      makeJob({ song: 'b', status: 'processing' }),
    ];
    expect(isQueueSettled(jobs)).toBe(false);
  });
});

describe('hasActiveJob', () => {
  it('detects waiting, processing and blocked jobs', () => {
    expect(hasActiveJob([makeJob({ song: 'a', status: 'waiting' })])).toBe(true);
    expect(hasActiveJob([makeJob({ song: 'a', status: 'processing' })])).toBe(true);
    expect(hasActiveJob([makeJob({ song: 'a', status: 'blocked_no_gpu' })])).toBe(true);
  });

  it('returns false for done/error jobs', () => {
    expect(hasActiveJob([makeJob({ song: 'a', status: 'done' })])).toBe(false);
    expect(hasActiveJob([makeJob({ song: 'a', status: 'error' })])).toBe(false);
  });
});

describe('hasActiveQueueFile', () => {
  it('detects uploading, waiting, processing and blocked rows', () => {
    expect(hasActiveQueueFile([makeQueueFile('uploading')])).toBe(true);
    expect(hasActiveQueueFile([makeQueueFile('waiting')])).toBe(true);
    expect(hasActiveQueueFile([makeQueueFile('processing')])).toBe(true);
    expect(hasActiveQueueFile([makeQueueFile('blocked_no_gpu')])).toBe(true);
  });

  it('returns false for done/error rows', () => {
    expect(hasActiveQueueFile([makeQueueFile('done')])).toBe(false);
    expect(hasActiveQueueFile([makeQueueFile('error')])).toBe(false);
  });
});

describe('decideCompletion', () => {
  const baseInput = {
    jobs: [makeJob({ song: 'a', status: 'done' })],
    settledTicks: 0,
    graceStartTime: null,
    requiredSettledTicks: 2,
    gracePeriodMs: 10000,
    now: 1000,
  };

  it('does not confirm after a single settled poll', () => {
    const result = decideCompletion(baseInput);
    expect(result.settled).toBe(true);
    expect(result.confirmed).toBe(false);
    expect(result.settledTicks).toBe(1);
  });

  it('confirms after the required ticks and grace period', () => {
    let state = decideCompletion({ ...baseInput, settledTicks: 1, graceStartTime: 0, now: 11000 });
    expect(state.confirmed).toBe(true);
    expect(state.settledTicks).toBe(2);
  });

  it('does not confirm if the grace period has not elapsed', () => {
    const state = decideCompletion({ ...baseInput, settledTicks: 1, graceStartTime: 0, now: 5000 });
    expect(state.confirmed).toBe(false);
  });

  it('resets when a job goes back to active', () => {
    const state = decideCompletion({
      ...baseInput,
      settledTicks: 5,
      graceStartTime: 0,
      jobs: [makeJob({ song: 'a', status: 'processing' })],
    });
    expect(state.settled).toBe(false);
    expect(state.confirmed).toBe(false);
    expect(state.settledTicks).toBe(0);
    expect(state.graceStartTime).toBeNull();
  });

  it('resets when a queue file goes back to active even if jobs look settled', () => {
    const state = decideCompletion({
      ...baseInput,
      settledTicks: 5,
      graceStartTime: 0,
      queueFiles: [makeQueueFile('processing')],
    });
    expect(state.settled).toBe(false);
    expect(state.confirmed).toBe(false);
    expect(state.settledTicks).toBe(0);
  });

  it('keeps the grace start timestamp across settled polls', () => {
    const first = decideCompletion(baseInput);
    expect(first.graceStartTime).toBe(baseInput.now);

    const second = decideCompletion({ ...baseInput, settledTicks: first.settledTicks, graceStartTime: first.graceStartTime, now: 2000 });
    expect(second.graceStartTime).toBe(baseInput.now);
  });
});
