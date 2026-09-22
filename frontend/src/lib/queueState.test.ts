import { describe, it, expect } from 'vitest';
import {
  groupBaseName,
  isSongDoneOnDisk,
  nextCopyGroupName,
  resolveOutputGroupName,
  deriveQueueFileStatus,
  songNameForQueueFile,
} from './queueState';
import type { ResultsGroup, QueueJob } from './api';
import type { QueueFile } from './queueDefaults';

function makeGroup(song: string, files: string[]): ResultsGroup {
  return {
    song,
    files: files.map((name) => ({ name, path: `/api/files/${encodeURIComponent(song)}/${name}` })),
  };
}

function makeQueueFile(name: string, path?: string, status = 'waiting'): QueueFile {
  return {
    file: new File([], name),
    id: `id-${name}`,
    status,
    checked: false,
    path,
  };
}

describe('groupBaseName', () => {
  it('returns the base name for plain groups', () => {
    expect(groupBaseName('song')).toBe('song');
  });

  it('strips copy suffixes', () => {
    expect(groupBaseName('song (copia01)')).toBe('song');
    expect(groupBaseName('song (copia12)')).toBe('song');
    expect(groupBaseName('Mi canción (copia03)')).toBe('Mi canción');
  });

  it('strips pitch suffixes', () => {
    expect(groupBaseName('song_pitch-1')).toBe('song');
    expect(groupBaseName('song_pitch+2')).toBe('song');
    expect(groupBaseName('song (pitch -1)')).toBe('song');
  });

  it('does not strip unknown suffixes', () => {
    expect(groupBaseName('song (remix)')).toBe('song (remix)');
  });
});

describe('isSongDoneOnDisk', () => {
  it('returns true when the base group has stems', () => {
    const groups = [makeGroup('song', ['vocals.wav'])];
    expect(isSongDoneOnDisk('song', groups)).toBe(true);
  });

  it('returns true when a copy group has stems', () => {
    const groups = [makeGroup('song (copia01)', ['vocals.wav'])];
    expect(isSongDoneOnDisk('song', groups)).toBe(true);
  });

  it('returns false when the group exists but has no stems', () => {
    const groups = [makeGroup('song', [])];
    expect(isSongDoneOnDisk('song', groups)).toBe(false);
  });

  it('returns false when only an unrelated group exists', () => {
    const groups = [makeGroup('other', ['vocals.wav'])];
    expect(isSongDoneOnDisk('song', groups)).toBe(false);
  });

  it('returns false when the folder is gone (empty groups list)', () => {
    expect(isSongDoneOnDisk('song', [])).toBe(false);
  });
});

describe('nextCopyGroupName', () => {
  it('returns (copia01) when the base group exists', () => {
    const groups = [makeGroup('song', ['vocals.wav'])];
    expect(nextCopyGroupName('song', groups)).toBe('song (copia01)');
  });

  it('returns the next free copy number', () => {
    const groups = [
      makeGroup('song', ['vocals.wav']),
      makeGroup('song (copia01)', ['vocals.wav']),
      makeGroup('song (copia02)', ['vocals.wav']),
    ];
    expect(nextCopyGroupName('song', groups)).toBe('song (copia03)');
  });

  it('fills the first gap when a copy is missing', () => {
    const groups = [
      makeGroup('song', ['vocals.wav']),
      makeGroup('song (copia01)', ['vocals.wav']),
      makeGroup('song (copia03)', ['vocals.wav']),
    ];
    expect(nextCopyGroupName('song', groups)).toBe('song (copia04)');
  });

  it('respects reserved names', () => {
    const groups = [makeGroup('song', ['vocals.wav'])];
    const reserved = new Set(['song (copia01)']);
    expect(nextCopyGroupName('song', groups, reserved)).toBe('song (copia02)');
  });

  it('ignores unrelated groups and pitch subgroups', () => {
    const groups = [
      makeGroup('song', ['vocals.wav']),
      makeGroup('other (copia99)', ['vocals.wav']),
      makeGroup('song_pitch-1', ['vocals_pitch-1.wav']),
    ];
    expect(nextCopyGroupName('song', groups)).toBe('song (copia01)');
  });
});

describe('resolveOutputGroupName', () => {
  it('uses the base name when no group exists yet', () => {
    expect(resolveOutputGroupName('song', [])).toBe('song');
  });

  it('creates a copy when the base group already exists', () => {
    const groups = [makeGroup('song', ['vocals.wav'])];
    expect(resolveOutputGroupName('song', groups)).toBe('song (copia01)');
  });

  it('avoids collisions with names reserved in the current batch', () => {
    const groups = [makeGroup('song', ['vocals.wav'])];
    const reserved = new Set(['song (copia01)']);
    expect(resolveOutputGroupName('song', groups, reserved)).toBe('song (copia02)');
  });
});

describe('deriveQueueFileStatus', () => {
  it('marks done when stems exist on disk', () => {
    const qf = makeQueueFile('song.wav', '/srv/input/song.wav');
    const groups = [makeGroup('song', ['vocals.wav'])];
    const update = deriveQueueFileStatus(qf, groups, []);
    expect(update.status).toBe('done');
    expect(update.checked).toBe(false);
    expect(update.progress).toBe(100);
  });

  it('marks waiting when stems have been deleted', () => {
    const qf = makeQueueFile('song.wav', '/srv/input/song.wav', 'done');
    const update = deriveQueueFileStatus(qf, [], []);
    expect(update.status).toBe('waiting');
    expect(update.checked).toBe(true);
    expect(update.progress).toBe(0);
  });

  it('prefers an active backend job over disk state', () => {
    const qf = makeQueueFile('song.wav', '/srv/input/song.wav', 'done');
    const groups = [makeGroup('song', ['vocals.wav'])];
    const jobs: QueueJob[] = [{ song: 'song', status: 'processing', progress: 42 }];
    const update = deriveQueueFileStatus(qf, groups, jobs);
    expect(update.status).toBe('processing');
    expect(update.progress).toBe(42);
  });

  it('does not mark done/100 when the song has stems but also has an active job', () => {
    const qf = makeQueueFile('song.wav', '/srv/input/song.wav', 'done');
    const groups = [makeGroup('song', ['vocals.wav'])];
    const jobs: QueueJob[] = [{ song: 'song', status: 'waiting', progress: 0 }];
    const update = deriveQueueFileStatus(qf, groups, jobs);
    expect(update.status).not.toBe('done');
    expect(update.progress).not.toBe(100);
    expect(update.status).toBe('waiting');
    expect(update.progress).toBe(0);
  });

  it('treats blocked_no_gpu jobs as active so the row does not look done', () => {
    const qf = makeQueueFile('song.wav', '/srv/input/song.wav');
    const groups = [makeGroup('song', ['vocals.wav'])];
    const jobs: QueueJob[] = [{ song: 'song', status: 'blocked_no_gpu', progress: 0 }];
    const update = deriveQueueFileStatus(qf, groups, jobs);
    expect(update.status).toBe('blocked_no_gpu');
    expect(update.progress).toBe(0);
  });

  it('marks waiting when the group folder exists but is empty of stems', () => {
    const qf = makeQueueFile('song.wav', '/srv/input/song.wav', 'done');
    const groups = [makeGroup('song', [])];
    const update = deriveQueueFileStatus(qf, groups, []);
    expect(update.status).toBe('waiting');
    expect(update.progress).toBe(0);
  });
});

describe('songNameForQueueFile', () => {
  it('uses the path basename when available', () => {
    const qf = makeQueueFile('ignored.mp3', '/srv/input/my song.flac');
    expect(songNameForQueueFile(qf)).toBe('my song');
  });

  it('falls back to the file name without extension', () => {
    const qf = makeQueueFile('my song.flac');
    expect(songNameForQueueFile(qf)).toBe('my song');
  });
});
