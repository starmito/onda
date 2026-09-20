import { describe, it, expect } from 'vitest';
import {
  getDefaultChecked,
  applyDefaultChecked,
  withToggledCheck,
  withToggledAll,
} from './queueDefaults';
import type { QueueFile } from './queueDefaults';

let fileCounter = 0;

function makeFile(status: string, checked: boolean, userTouched = false): QueueFile {
  fileCounter++;
  return {
    file: new File([], `song-${fileCounter}.mp3`),
    id: `${status}-${checked ? 'on' : 'off'}-${userTouched ? 'touched' : 'untouched'}-${fileCounter}`,
    status,
    checked,
    userTouched,
  };
}

describe('getDefaultChecked', () => {
  it('checks pending/working statuses by default', () => {
    expect(getDefaultChecked('waiting')).toBe(true);
    expect(getDefaultChecked('uploading')).toBe(true);
    expect(getDefaultChecked('processing')).toBe(true);
  });

  it('unchecks terminal/error statuses by default', () => {
    expect(getDefaultChecked('done')).toBe(false);
    expect(getDefaultChecked('error')).toBe(false);
    expect(getDefaultChecked('blocked_no_gpu')).toBe(false);
  });

  it('falls back to unchecked for unknown statuses', () => {
    expect(getDefaultChecked('unknown')).toBe(false);
    expect(getDefaultChecked('')).toBe(false);
  });
});

describe('applyDefaultChecked', () => {
  it('marks pending files checked and completed files unchecked', () => {
    const files = [
      makeFile('waiting', false),
      makeFile('processing', false),
      makeFile('done', true),
      makeFile('error', true),
    ];
    const result = applyDefaultChecked(files);
    expect(result[0].checked).toBe(true);
    expect(result[1].checked).toBe(true);
    expect(result[2].checked).toBe(false);
    expect(result[3].checked).toBe(false);
  });

  it('keeps manual user choices untouched', () => {
    const files = [
      makeFile('waiting', false, true), // user explicitly unchecked pending
      makeFile('done', true, true),     // user explicitly checked completed
      makeFile('error', true, true),
    ];
    const result = applyDefaultChecked(files);
    expect(result[0].checked).toBe(false);
    expect(result[1].checked).toBe(true);
    expect(result[2].checked).toBe(true);
  });

  it('leaves already-correct rows unchanged (same array reference)', () => {
    const files = [makeFile('waiting', true), makeFile('done', false)];
    const result = applyDefaultChecked(files);
    expect(result).toBe(files);
  });

  it('updates only the rows that need changing', () => {
    const files = [makeFile('waiting', true), makeFile('done', true)];
    const result = applyDefaultChecked(files);
    expect(result).not.toBe(files);
    expect(result[0]).toBe(files[0]); // unchanged reference
    expect(result[1]).not.toBe(files[1]); // new object
  });
});

describe('withToggledCheck', () => {
  it('toggles the checked state and marks the row as user touched', () => {
    const files = [makeFile('waiting', true)];
    const result = withToggledCheck(files, files[0].id);
    expect(result[0].checked).toBe(false);
    expect(result[0].userTouched).toBe(true);
  });

  it('leaves other rows untouched', () => {
    const a = makeFile('waiting', true);
    const b = makeFile('waiting', true);
    const result = withToggledCheck([a, b], a.id);
    expect(result[1]).toBe(b);
  });
});

describe('withToggledAll', () => {
  it('selects all when at least one is unchecked', () => {
    const files = [makeFile('waiting', true), makeFile('waiting', false)];
    const result = withToggledAll(files);
    expect(result.every((qf) => qf.checked)).toBe(true);
    expect(result.every((qf) => qf.userTouched)).toBe(true);
  });

  it('deselects all when all are checked', () => {
    const files = [makeFile('waiting', true), makeFile('waiting', true)];
    const result = withToggledAll(files);
    expect(result.every((qf) => !qf.checked)).toBe(true);
    expect(result.every((qf) => qf.userTouched)).toBe(true);
  });
});
