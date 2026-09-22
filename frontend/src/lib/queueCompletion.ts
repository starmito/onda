/**
 * Queue completion decision logic.
 *
 * These pure functions decide when the UI may declare a batch "done".  A single
 * poll is never enough: we require consecutive settled polls, a grace period,
 * and (for chained pipelines) all steps to be finished.  Keeping the rules here
 * makes them unit-testable without real timers.
 */

import type { QueueJob } from './api';
import type { QueueFile } from './queueDefaults';

export const ACTIVE_JOB_STATUSES: QueueJob['status'][] = ['processing', 'waiting', 'blocked_no_gpu'];
const ACTIVE_QUEUE_FILE_STATUSES = ['uploading', 'waiting', 'processing', 'blocked_no_gpu'];

/**
 * True when the job is in a terminal state and, for chained pipelines, all
 * steps have been executed.
 */
export function isJobSettled(job: QueueJob): boolean {
  if (job.status === 'error') return true;
  if (job.status !== 'done') return false;
  const total = job.total_steps ?? 1;
  if (total > 1) {
    return (job.current_step ?? 0) === total;
  }
  return true;
}

/** True when every job is settled and none is actively running/waiting. */
export function isQueueSettled(jobs: QueueJob[]): boolean {
  if (jobs.length === 0) return false;
  return jobs.every(isJobSettled);
}

export function hasActiveJob(jobs: QueueJob[]): boolean {
  return jobs.some(j => ACTIVE_JOB_STATUSES.includes(j.status));
}

export function hasActiveQueueFile(queueFiles: QueueFile[]): boolean {
  return queueFiles.some(qf => ACTIVE_QUEUE_FILE_STATUSES.includes(qf.status));
}

export interface CompletionDecisionInput {
  jobs: QueueJob[];
  queueFiles?: QueueFile[];
  settledTicks: number;
  graceStartTime: number | null;
  requiredSettledTicks: number;
  gracePeriodMs: number;
  now: number;
}

export interface CompletionDecisionOutput {
  /** This poll sees the queue as settled. */
  settled: boolean;
  /** Settlement has been confirmed and polling can stop. */
  confirmed: boolean;
  /** Consecutive settled polls so far. */
  settledTicks: number;
  /** When the grace period started, or null. */
  graceStartTime: number | null;
  /** Whether there is any active backend job in this poll. */
  hasActive: boolean;
}

/**
 * Decides whether the queue has really finished.
 *
 * - Needs `requiredSettledTicks` consecutive polls that look settled.
 * - Keeps polling for `gracePeriodMs` after the first settled poll.
 * - If anything goes back to active during the grace period, the counter and
 *   timer are reset so the UI never flips to "Completado" prematurely.
 */
export function decideCompletion(input: CompletionDecisionInput): CompletionDecisionOutput {
  const { jobs, queueFiles, settledTicks, graceStartTime, requiredSettledTicks, gracePeriodMs, now } = input;

  const queueHasActive = queueFiles ? hasActiveQueueFile(queueFiles) : false;
  const settled = isQueueSettled(jobs) && !queueHasActive;
  const hasActive = hasActiveJob(jobs);

  if (settled && !hasActive) {
    const nextTicks = settledTicks + 1;
    const nextGraceStart = graceStartTime ?? now;
    const confirmed = nextTicks >= requiredSettledTicks && (now - nextGraceStart) >= gracePeriodMs;
    return {
      settled: true,
      confirmed,
      settledTicks: nextTicks,
      graceStartTime: nextGraceStart,
      hasActive,
    };
  }

  return {
    settled: false,
    confirmed: false,
    settledTicks: 0,
    graceStartTime: null,
    hasActive,
  };
}
