<script lang="ts">
  import { uploadAudio } from './api';

  export interface QueueFile {
    file: File;
    id: string;
    status: 'waiting' | 'uploading' | 'processing' | 'done' | 'error';
    path?: string;
    progress?: number;
    errorMsg?: string;
    checked: boolean;
  }

  let {
    files = [],
    disabled = false,
    onaddfiles,
    onclear,
    ontoggle,
  }: {
    files?: QueueFile[];
    disabled?: boolean;
    onaddfiles?: (newFiles: File[]) => void;
    onclear?: () => void;
    ontoggle?: (id: string) => void;
  } = $props();

  let fileInput: HTMLInputElement;

  function handleAddClick() {
    fileInput.click();
  }

  function handleFileChange(e: Event) {
    const target = e.target as HTMLInputElement;
    const selected = target.files;
    if (!selected || selected.length === 0) return;
    const audioFiles = Array.from(selected).filter(isAudioFile);
    if (audioFiles.length > 0) {
      onaddfiles?.(audioFiles);
    }
    target.value = '';
  }

  function isAudioFile(f: File): boolean {
    const exts = ['.flac', '.wav', '.mp3', '.ogg', '.m4a', '.aiff', '.wma'];
    return exts.some((e) => f.name.toLowerCase().endsWith(e));
  }

  function formatSize(bytes: number): string {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / 1048576).toFixed(1) + ' MB';
  }

  function statusIcon(s: QueueFile['status']): string {
    switch (s) {
      case 'waiting': return '⏳';
      case 'uploading': return '⬆️';
      case 'processing': return '⚙️';
      case 'done': return '✅';
      case 'error': return '❌';
    }
  }
</script>

<div class="file-queue">
  <div class="queue-header">
    <h3 class="queue-title">📂 File Queue ({files.length})</h3>
    <div class="queue-actions">
      <button class="queue-btn" disabled={disabled} onclick={handleAddClick}>
        + Add Files
      </button>
      <button class="queue-btn clear-btn" disabled={disabled || files.length === 0} onclick={() => onclear?.()}>
        Clear All
      </button>
    </div>
  </div>

  <input
    bind:this={fileInput}
    type="file"
    multiple
    accept=".flac,.wav,.mp3,.ogg,.m4a,.aiff,.wma"
    class="hidden-input"
    onchange={handleFileChange}
  />

  {#if files.length === 0}
    <p class="empty-msg">No files in queue. Drop files above or click "Add Files".</p>
  {:else}
    <div class="queue-list">
      {#each files as qf (qf.id)}
        <div class="queue-item" class:error={qf.status === 'error'} class:done={qf.status === 'done'}>
          <label class="item-check">
            <input
              type="checkbox"
              checked={qf.checked}
              disabled={disabled || qf.status === 'done' || qf.status === 'error'}
              onchange={() => ontoggle?.(qf.id)}
            />
          </label>
          <div class="item-info">
            <span class="item-name" title={qf.file.name}>{qf.file.name}</span>
            <span class="item-size">{formatSize(qf.file.size)}</span>
          </div>
          <span class="item-status">{statusIcon(qf.status)}</span>
          {#if qf.status === 'uploading' || qf.status === 'processing'}
            <div class="item-progress-track">
              <div
                class="item-progress-fill"
                style="width: {((qf.progress ?? 0) * 100).toFixed(0)}%"
              ></div>
            </div>
          {/if}
          {#if qf.status === 'error' && qf.errorMsg}
            <span class="item-error">{qf.errorMsg}</span>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .file-queue {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 1rem;
    background: #1a1a2e;
    border-radius: 8px;
  }

  .queue-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.75rem;
    flex-wrap: wrap;
  }

  .queue-title {
    margin: 0;
    font-size: 0.95rem;
    font-weight: 600;
    color: #e0e0e0;
  }

  .queue-actions {
    display: flex;
    gap: 0.5rem;
  }

  .queue-btn {
    padding: 0.35rem 0.75rem;
    background: #2a2a3e;
    color: #00d4ff;
    border: 1px solid #444;
    border-radius: 6px;
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s, border-color 0.2s;
  }
  .queue-btn:hover:not(:disabled) {
    background: #333355;
    border-color: #00d4ff;
  }
  .queue-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .clear-btn {
    color: #f44336;
  }
  .clear-btn:hover:not(:disabled) {
    border-color: #f44336;
  }

  .hidden-input { display: none; }

  .empty-msg {
    margin: 0;
    font-size: 0.85rem;
    color: #666;
    text-align: center;
    padding: 1rem 0;
  }

  .queue-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-height: 320px;
    overflow-y: auto;
  }

  .queue-item {
    display: grid;
    grid-template-columns: auto 1fr auto;
    grid-template-rows: auto auto;
    align-items: center;
    gap: 0.3rem 0.5rem;
    padding: 0.5rem 0.6rem;
    border-radius: 6px;
    background: #111;
    transition: background 0.2s;
  }
  .queue-item:hover {
    background: #1a1a2e;
  }
  .queue-item.error {
    border-left: 3px solid #f44336;
  }
  .queue-item.done {
    border-left: 3px solid #4caf50;
  }

  .item-check input[type="checkbox"] {
    accent-color: #00d4ff;
    width: 15px;
    height: 15px;
    cursor: pointer;
  }

  .item-info {
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    min-width: 0;
  }
  .item-name {
    font-size: 0.85rem;
    color: #e0e0e0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item-size {
    font-size: 0.7rem;
    color: #777;
  }

  .item-status {
    font-size: 0.9rem;
  }

  .item-progress-track {
    grid-column: 1 / -1;
    height: 4px;
    background: #2a2a3e;
    border-radius: 2px;
    overflow: hidden;
  }
  .item-progress-fill {
    height: 100%;
    background: #00d4ff;
    border-radius: 2px;
    transition: width 0.3s ease;
  }

  .item-error {
    grid-column: 1 / -1;
    font-size: 0.75rem;
    color: #f44336;
  }
</style>
