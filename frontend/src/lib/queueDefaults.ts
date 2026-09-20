/**
 * Queue default-selection logic.
 *
 * Centralising the rule "pending files are checked by default, terminal files
 * are not" in a plain TS module keeps it unit-testable and reusable from both
 * App.svelte and PipelineView.svelte.
 */

export interface QueueFile {
  file: File;
  id: string;
  status: string;
  checked: boolean;
  userTouched?: boolean;
  progress?: number;
  path?: string;
  errorMsg?: string;
  current_step?: number;
  total_steps?: number;
  step_name?: string;
}

/**
 * Returns the default checked state for a queue row based on its job status.
 *
 * Pending / working states should be selected so the user can run them with a
 * single click. Terminal states (done, error, blocked) should not be selected
 * by default, otherwise completed songs would be reprocessed accidentally.
 */
export function getDefaultChecked(status: string): boolean {
  switch (status) {
    case 'waiting':
    case 'uploading':
    case 'processing':
      return true;
    case 'done':
    case 'error':
    case 'blocked_no_gpu':
      return false;
    default:
      return false;
  }
}

/**
 * Applies the default checked state to every file the user has NOT manually
 * toggled. Files marked as `userTouched` keep their current state.
 *
 * Returns the same array reference when nothing changes, to avoid unnecessary
 * Svelte re-renders.
 */
export function applyDefaultChecked(files: QueueFile[]): QueueFile[] {
  let changed = false;
  const updated = files.map((qf) => {
    if (qf.userTouched) return qf;
    const target = getDefaultChecked(qf.status);
    if (qf.checked === target) return qf;
    changed = true;
    return { ...qf, checked: target };
  });
  return changed ? updated : files;
}

/**
 * Toggles the checked state of a single file and records that the user touched
 * it, so future default resets will not overwrite this choice.
 */
export function withToggledCheck(files: QueueFile[], id: string): QueueFile[] {
  return files.map((qf) =>
    qf.id === id ? { ...qf, checked: !qf.checked, userTouched: true } : qf,
  );
}

/**
 * Toggles the checked state of all files (select-all / deselect-all) and marks
 * every row as user-touched.
 */
export function withToggledAll(files: QueueFile[]): QueueFile[] {
  const allChecked = files.every((qf) => qf.checked);
  return files.map((qf) => ({
    ...qf,
    checked: !allChecked,
    userTouched: true,
  }));
}
