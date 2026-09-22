/**
 * Queue state derived from disk.
 *
 * These are pure functions: they take the current filesystem results (groups
 * of stems) and the backend job list and compute the status that the UI must
 * show.  Keeping them testable and separate from Svelte reactivity makes the
 * "completed = stems exist on disk" rule explicit.
 */

import type { ResultsGroup, QueueJob } from './api';
import type { QueueFile } from './queueDefaults';
import { getDefaultChecked } from './queueDefaults';

const COPY_RE = /^(.*)\s\(copia(\d+)\)$/;
const PITCH_SUFFIX_RE = /_pitch[+-]?\d+$/;
const PITCH_SPACE_RE = /\s\(pitch\s*[+-]?\d+\)$/;

/** Returns the base song name for a result group, stripping copy/pitch suffixes. */
export function groupBaseName(name: string): string {
  const copyMatch = name.match(COPY_RE);
  if (copyMatch) return copyMatch[1].trim();
  if (PITCH_SUFFIX_RE.test(name)) return name.replace(PITCH_SUFFIX_RE, '').trim();
  if (PITCH_SPACE_RE.test(name)) return name.replace(PITCH_SPACE_RE, '').trim();
  return name.trim();
}

/** True when any non-empty result group belongs to the given song. */
export function isSongDoneOnDisk(song: string, groups: ResultsGroup[]): boolean {
  return groups.some((g) => g.files.length > 0 && groupBaseName(g.song) === song);
}

const ACTIVE_JOB_STATUSES: QueueJob['status'][] = ['processing', 'waiting', 'blocked_no_gpu'];

export interface QueueFileStatusUpdate {
  status: QueueFile['status'];
  checked: boolean;
  /** Progress to show in the row. Filled from active jobs or disk state. */
  progress: number;
}

/**
 * Derives the queue row status from disk and backend jobs.
 *
 * Active backend jobs take precedence: if a song has a job that is waiting,
 * processing or blocked, the row reflects that job and its real progress.
 * Only when there is no active job for that song can the disk state mark the
 * row as "done" (100 %).  If there is no active job and no stems on disk, the
 * row falls back to "waiting" so the user can reprocess it.
 */
export function deriveQueueFileStatus(
  qf: QueueFile,
  groups: ResultsGroup[],
  jobs: QueueJob[],
): QueueFileStatusUpdate {
  const song = songNameForQueueFile(qf);
  const job = jobs.find((j) => j.song === song || j.song.startsWith(song));

  if (job && ACTIVE_JOB_STATUSES.includes(job.status)) {
    return {
      status: job.status,
      checked: getDefaultChecked(job.status),
      progress: job.progress ?? 0,
    };
  }

  if (isSongDoneOnDisk(song, groups)) {
    return { status: 'done', checked: getDefaultChecked('done'), progress: 100 };
  }

  return { status: 'waiting', checked: getDefaultChecked('waiting'), progress: 0 };
}

/** Extracts the song name from a queue file (input path or file name). */
export function songNameForQueueFile(qf: QueueFile): string {
  const raw = qf.path?.split('/').pop() || qf.file.name;
  return raw.replace(/\.[^.]+$/, '');
}

/** Returns the next available `(copiaNN)` name for a song, never overwriting an existing group. */
export function nextCopyGroupName(
  baseSong: string,
  groups: ResultsGroup[],
  reserved?: Set<string>,
): string {
  const names = new Set(groups.map((g) => g.song));
  if (reserved) {
    reserved.forEach((n) => names.add(n));
  }

  let maxCopy = 0;
  for (const name of names) {
    if (groupBaseName(name) !== baseSong) continue;
    if (name === baseSong) {
      maxCopy = Math.max(maxCopy, 0);
      continue;
    }
    const match = name.match(COPY_RE);
    if (match) {
      maxCopy = Math.max(maxCopy, parseInt(match[2], 10));
    }
  }

  const next = maxCopy + 1;
  const suffix = `(copia${String(next).padStart(2, '0')})`;
  return `${baseSong} ${suffix}`;
}

/**
 * Chooses the output group name for a separation run.
 *
 * If the base song group does not exist yet, it is used as-is.  If any group
 * for the song already exists, a fresh `(copiaNN)` name is returned so the
 * new run does not overwrite previous stems.
 */
export function resolveOutputGroupName(
  baseSong: string,
  groups: ResultsGroup[],
  reserved?: Set<string>,
): string {
  const names = new Set(groups.map((g) => g.song));
  if (reserved) {
    reserved.forEach((n) => names.add(n));
  }

  if (!names.has(baseSong) && groupBaseName(baseSong) === baseSong) {
    return baseSong;
  }

  return nextCopyGroupName(baseSong, groups, reserved);
}
