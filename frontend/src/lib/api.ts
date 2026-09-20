export const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? '';

// Structured error returned by DAW audio endpoints when the source file is missing.
export interface DAWErrorResponse {
  error: string;
  code?: string;
  file?: string;
  help?: string;
}

// Error thrown when a DAW audio endpoint reports a missing file.
export class DAWAudioNotFoundError extends Error {
  public readonly code = 'file_not_found';
  public readonly fileName: string;
  constructor(fileName: string, help?: string) {
    super(help || `Archivo no encontrado: ${fileName}. Vuelve a subirlo para continuar.`);
    this.fileName = fileName;
  }
}

async function parseDAWError(response: Response): Promise<Error> {
  try {
    const data = (await response.json()) as DAWErrorResponse;
    if (response.status === 404 && data.code === 'file_not_found') {
      return new DAWAudioNotFoundError(data.file || 'archivo desconocido', data.help);
    }
    return new Error(data.error || `Request failed with status ${response.status}`);
  } catch {
    return new Error(`Request failed with status ${response.status}: ${response.statusText}`);
  }
}

async function dawFetch<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
  const res = await fetch(input, init);
  if (!res.ok) {
    throw await parseDAWError(res);
  }
  return (await res.json()) as T;
}

export interface HealthComponent {
  ok: boolean;
  detail?: string;
  version?: string;
  type?: 'cuda' | 'cpu';
  warning?: string;
  info?: string;
}

export interface VersionMismatchItem {
  component: string;
  expected: string;
  actual: string;
}

export interface VersionMismatch {
  ok: boolean;
  detail?: VersionMismatchItem[];
}

export interface HealthResponse {
  status: string;
  version: string;
  backend: HealthComponent;
  frontend: HealthComponent;
  pipeline: HealthComponent;
  gpu: HealthComponent;
  disk: HealthComponent;
  docker: HealthComponent;
  version_mismatch: VersionMismatch;
}

export interface BackendActionResponse {
  ok: boolean;
  detail: string;
}

export interface SeparateResponse {
  status: string;
  song: string;
}

export interface SeparateOptions {
  preset: string;
  input: string;
  pitch?: number;
  steps?: PipelineStep[];
  output?: string;
  force_vram?: boolean;
  force_ram?: boolean;
}

export interface StatusResponse {
  status: string;
  progress: number;
  step: string;
  song: string;
  elapsed: number;
  eta: number;
  files?: { name: string; path: string }[];
  error?: string;
  preset?: string;
  vocal_model?: string;
  stem_model?: string;
  drums_model?: string;
  bass_model?: string;
  pitch?: number;
}

export interface UploadResponse {
  path: string;
}

export function downloadUrl(song: string, file: string): string {
  return `${API_BASE}/api/files/${encodeURIComponent(song)}/${encodeURIComponent(file)}`;
}

export function pitchInputDownloadUrl(filename: string): string {
  return `${API_BASE}/input_rubberband/${encodeURIComponent(filename)}`;
}

export function pitchDownloadUrl(song: string, pitch: number, file: string): string {
  const pitchStr = pitch > 0 ? '+' + pitch : String(pitch);
  return `${API_BASE}/api/pitch/files/${encodeURIComponent(song)}/${encodeURIComponent(pitchStr)}/${encodeURIComponent(file)}`;
}

export async function uploadAudio(file: File): Promise<UploadResponse> {
  try {
    const formData = new FormData();
    formData.append('file', file);
    const res = await fetch(`${API_BASE}/api/upload`, {
      method: 'POST',
      body: formData,
    });
    if (!res.ok) {
      throw new Error(`Upload failed with status ${res.status}: ${res.statusText}`);
    }
    return (await res.json()) as UploadResponse;
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error during upload: ${String(err)}`);
  }
}

export async function uploadPitchAudio(file: File): Promise<UploadResponse> {
  try {
    const formData = new FormData();
    formData.append('file', file);
    const res = await fetch(`${API_BASE}/api/upload/pitch`, {
      method: 'POST',
      body: formData,
    });
    if (!res.ok) {
      throw new Error(`Pitch upload failed with status ${res.status}: ${res.statusText}`);
    }
    return (await res.json()) as UploadResponse;
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error during pitch upload: ${String(err)}`);
  }
}

export async function getHealth(): Promise<HealthResponse> {
  try {
    const res = await fetch(`${API_BASE}/api/health`);
    if (!res.ok) {
      throw new Error(`Health check failed with status ${res.status}: ${res.statusText}`);
    }
    return (await res.json()) as HealthResponse;
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error during health check: ${String(err)}`);
  }
}

export async function separateAudio(opts: SeparateOptions): Promise<SeparateResponse> {
  try {
    const body: Record<string, any> = {
      preset: opts.preset,
      input: opts.input,
    };
    if (opts.output) body.output = opts.output;
    if (opts.pitch !== undefined && opts.pitch !== 0) {
      body.pitch = opts.pitch;
    }
    if (opts.steps && opts.steps.length > 0) {
      body.steps = opts.steps;
    }
    if (opts.force_vram) {
      body.force_vram = true;
    }
    if (opts.force_ram) {
      body.force_ram = true;
    }
    const res = await fetch(`${API_BASE}/api/separate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      throw new Error(`Separation failed with status ${res.status}: ${res.statusText}`);
    }
    return (await res.json()) as SeparateResponse;
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error during separation: ${String(err)}`);
  }
}

export async function getStatus(): Promise<StatusResponse> {
  try {
    const res = await fetch(`${API_BASE}/api/status`);
    if (!res.ok) {
      throw new Error(`Status check failed with status ${res.status}: ${res.statusText}`);
    }
    return (await res.json()) as StatusResponse;
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error during status check: ${String(err)}`);
  }
}

export async function deleteSong(song: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/files/${encodeURIComponent(song)}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    throw new Error(`Delete failed with status ${res.status}: ${res.statusText}`);
  }
}

export async function deleteStem(song: string, name: string): Promise<void> {
  const res = await fetch(
    `${API_BASE}/api/delete?file=${encodeURIComponent(song + '/' + name)}`,
    { method: 'DELETE' },
  );
  if (!res.ok) {
    throw new Error(`Delete failed with status ${res.status}: ${res.statusText}`);
  }
}

// ---- ModelLoader ---- 
export interface LocalModel {
  name: string;
  display_name?: string;
  category: string;
  size_mb: number;
  vram_estimate_mb?: number;
  path: string;
}

export interface LocalModelsResponse {
  models: LocalModel[];
}

export async function getLocalModels(): Promise<LocalModelsResponse> {
  const res = await fetch(`${API_BASE}/api/models/list`);
  if (!res.ok) {
    throw new Error(`Model list failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as LocalModelsResponse;
}

export interface DownloadModelRequest {
  source: 'huggingface';
  repo: string;
  filename?: string;  // optional specific file to download
}

export interface DownloadModelResponse {
  status: string;
  message?: string;
}

export interface DownloadStatusResponse {
  status: string;
  progress: string;
  percentage: number;
  total_bytes: number;
  downloaded_bytes: number;
  repo: string;
  target: string;
  error?: string;
  filename?: string;
  source: string;
}

export async function downloadModel(repo: string, filename?: string): Promise<DownloadModelResponse> {
  const body: DownloadModelRequest = { source: 'huggingface', repo };
  if (filename) body.filename = filename;
  const res = await fetch(`${API_BASE}/api/models/download`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`Model download failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as DownloadModelResponse;
}

export async function getDownloadStatus(repo: string): Promise<DownloadStatusResponse> {
  const res = await fetch(`${API_BASE}/api/models/download/status?repo=${encodeURIComponent(repo)}`);
  if (!res.ok) {
    throw new Error(`Download status fetch failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as DownloadStatusResponse;
}

export async function uploadModel(file: File): Promise<UploadResponse> {
  const formData = new FormData();
  formData.append('file', file);
  const res = await fetch(`${API_BASE}/api/upload?type=model`, {
    method: 'POST',
    body: formData,
  });
  if (!res.ok) {
    throw new Error(`Model upload failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as UploadResponse;
}

// ---- GPU monitor ----
export interface GpuInfo {
  name: string;
  vram_total_mb: number;
  vram_used_mb: number;
  vram_free_mb: number;
  temperature_c: number;
  runtime: string;
  ok: boolean;
}

export async function getGpuInfo(): Promise<GpuInfo> {
  const res = await fetch(`${API_BASE}/api/gpu/info`);
  if (!res.ok) {
    throw new Error(`Failed to fetch GPU info (status ${res.status}): ${res.statusText}`);
  }
  const gpu = (await res.json()) as GpuInfo;
  if (!gpu.ok) {
    throw new Error(`GPU not available: ${(gpu as any).error || 'unknown error'}`);
  }
  return gpu;
}

export async function startBackend(): Promise<BackendActionResponse> {
  const res = await fetch(`${API_BASE}/api/backend/start`, { method: 'POST' });
  if (!res.ok) {
    throw new Error(`Backend start failed (${res.status}): ${res.statusText}`);
  }
  return (await res.json()) as BackendActionResponse;
}

export async function restartBackend(): Promise<BackendActionResponse> {
  const res = await fetch(`${API_BASE}/api/backend/restart`, { method: 'POST' });
  if (!res.ok) {
    throw new Error(`Backend restart failed (${res.status}): ${res.statusText}`);
  }
  return (await res.json()) as BackendActionResponse;
}

export async function stopBackend(): Promise<BackendActionResponse> {
  const res = await fetch(`${API_BASE}/api/backend/stop`, { method: 'POST' });
  if (!res.ok) {
    throw new Error(`Backend stop failed (${res.status}): ${res.statusText}`);
  }
  return (await res.json()) as BackendActionResponse;
}

// ---- Queue (cola secuencial) ----
export interface FailureDetails {
  step: string;
  exit_code: number;
  error: string;
  stderr: string;
  failed_dir: string;
}

export interface QueueJob {
  song: string;
  status: 'waiting' | 'processing' | 'done' | 'error' | 'blocked_no_gpu';
  progress: number;
  current_step?: number;
  total_steps?: number;
  step_name?: string;
  eta?: string;
  device?: string;
  current_model?: string;
  current_flags?: string;
  error?: string;
  failure_details?: FailureDetails;
  files?: { name: string; path: string }[];
}

export interface QueueStatusResponse {
  jobs: QueueJob[];
}

export async function getQueueStatus(): Promise<QueueStatusResponse> {
  try {
    const res = await fetch(`${API_BASE}/api/queue/status`);
    if (!res.ok) {
      throw new Error(`Queue status failed with status ${res.status}: ${res.statusText}`);
    }
    return (await res.json()) as QueueStatusResponse;
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error during queue status check: ${String(err)}`);
  }
}

export async function clearQueue(): Promise<void> {
  const res = await fetch(`${API_BASE}/api/queue`, { method: 'DELETE' });
  if (!res.ok) {
    throw new Error(`Queue clear failed with status ${res.status}: ${res.statusText}`);
  }
}

export async function cancelQueue(): Promise<{ status: string }> {
  const res = await fetch(`${API_BASE}/api/queue/cancel`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  });
  if (!res.ok) {
    throw new Error(`Queue cancel failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as { status: string };
}

export interface ProcessStatus {
  queue_jobs: any[];
  gpu: {
    total_mb: number;
    used_mb: number;
    free_mb: number;
    runtime: string;
    name: string;
  };
  pipeline_process: {
    alive: boolean;
    pid: number;
    cmd: string;
    elapsed_sec: number;
  };
  blocked: { song: string; message: string }[];
}

export async function getProcessesStatus(): Promise<ProcessStatus> {
  const res = await fetch(`${API_BASE}/api/processes/status`);
  if (!res.ok) {
    throw new Error(`Processes status failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as ProcessStatus;
}

// ---- Results (file system persistence) ----
export interface ResultsGroup {
  song: string;
  files: { name: string; path: string }[];
}

export async function getResults(): Promise<ResultsGroup[]> {
  try {
    const res = await fetch(`${API_BASE}/api/results`);
    if (!res.ok) {
      throw new Error(`Results fetch failed with status ${res.status}: ${res.statusText}`);
    }
    return (await res.json()) as ResultsGroup[];
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error fetching results: ${String(err)}`);
  }
}

// ---- Inputs (file system persistence) ----
export interface InputEntry {
  name: string;
  path: string;
  source?: string;
  processed?: boolean;
  song?: string;
}

export async function getInputs(include?: 'input' | 'daw-data' | 'all'): Promise<InputEntry[]> {
  try {
    const qs = include ? `?include=${encodeURIComponent(include)}` : '';
    const res = await fetch(`${API_BASE}/api/inputs${qs}`);
    if (!res.ok) {
      throw new Error(`Inputs fetch failed with status ${res.status}: ${res.statusText}`);
    }
    return (await res.json()) as InputEntry[];
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error fetching inputs: ${String(err)}`);
  }
}

export async function deleteInput(name: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/inputs/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    throw new Error(`Delete input failed with status ${res.status}: ${res.statusText}`);
  }
}

export async function deletePitchUpload(name: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/uploads/pitch/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    throw new Error(`Delete pitch upload failed with status ${res.status}: ${res.statusText}`);
  }
}

// ---- ModelConfig ----
export interface ModelConfigResponse {
  segment_size: number;
  overlap: number;
  chunk_size: number;
  batch_size: number;
  device: string;
  // Demucs PyTorch-specific
  shifts?: number;
  segment?: number;
  jobs?: number;
  // Raw YAML inference values (for MDX/SCNet display)
  dim_t?: number;
  num_overlap?: number;
}

export async function getModelConfig(modelName: string): Promise<ModelConfigResponse> {
  const res = await fetch(`${API_BASE}/api/models/${encodeURIComponent(modelName)}/config`);
  if (!res.ok) {
    throw new Error(`Failed to fetch model config (${res.status}): ${res.statusText}`);
  }
  return (await res.json()) as ModelConfigResponse;
}

export async function setModelConfig(cfg: ModelConfigResponse, modelName: string): Promise<{ ok: string; detail: string }> {
  const res = await fetch(`${API_BASE}/api/models/${encodeURIComponent(modelName)}/config`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  if (!res.ok) {
    throw new Error(`Failed to save model config (${res.status}): ${res.statusText}`);
  }
  return (await res.json()) as { ok: string; detail: string };
}

// ---- Model Catalog (UVR) ----
export interface UVRModelEntry {
  name: string;
  display_name?: string;
  category: string;
  download_url?: string;
  huggingface_repo?: string;
  filename: string;
  size_mb: number;
  description?: string;
  downloaded: boolean;
}

export interface HFModelEntry {
  name: string;
  filename: string;
  hf_path: string;
  size_mb: number;
  category: string;
}

export interface HfCatalogResponse {
  categories: Record<string, { models: HFModelEntry[] }>;
}

export async function getModelCatalog(): Promise<UVRModelEntry[]> {
  try {
    const res = await fetch(`${API_BASE}/api/models/catalog`);
    if (!res.ok) {
      throw new Error(`Catalog fetch failed with status ${res.status}: ${res.statusText}`);
    }
    const data = (await res.json()) as UVRModelEntry[];
    // Map download_url to huggingface_repo for UI compatibility
    return data.map((entry: any) => ({
      ...entry,
      huggingface_repo: entry.huggingface_repo || entry.download_url,
    }));
  } catch (err) {
    if (err instanceof Error) throw err;
    throw new Error(`Unexpected error fetching model catalog: ${String(err)}`);
  }
}

export async function getHfCatalog(): Promise<HfCatalogResponse> {
  const res = await fetch(`${API_BASE}/api/models/catalog/hf`);
  if (!res.ok) {
    throw new Error(`HF catalog fetch failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as HfCatalogResponse;
}

// ---- Model Catalog (Official Demucs) ----
export interface DemucsCatalogEntry {
  name: string;
  display_name: string;
  repo: string;
  downloaded: boolean;
  downloads: number;
  likes: number;
  source: string;
}

export async function getDemucsCatalog(): Promise<DemucsCatalogEntry[]> {
  const res = await fetch(`${API_BASE}/api/models/catalog/demucs`);
  if (!res.ok) {
    throw new Error(`Demucs catalog fetch failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as DemucsCatalogEntry[];
}

export interface DeleteModelResponse {
  ok: boolean;
  detail: string;
}

export async function deleteModel(name: string): Promise<DeleteModelResponse> {
  const res = await fetch(`${API_BASE}/api/models/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    throw new Error(`Model delete failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as DeleteModelResponse;
}

// ---- Presets API ---- 
export interface StemRoute {
  action: string;    // 'save' | 'route' | 'discard'
  target?: string;   // 'result' o step id
}

export interface PipelineStep {
  id: string;
  model: string;
  type: string;      // 'vocal' | 'viperx' | 'demucs'
  enabled: boolean;
  stems: Record<string, StemRoute>;
}

export interface PresetData {
  name: string;
  steps: PipelineStep[];
  pitch?: number;
  description?: string;
  locked?: boolean;
}

export async function getPresets(): Promise<Record<string, PresetData>> {
  const res = await fetch(`${API_BASE}/api/presets`);
  if (!res.ok) throw new Error(`Failed to fetch presets: ${res.status}`);
  return res.json();
}

export async function savePreset(preset: PresetData): Promise<void> {
  const res = await fetch(`${API_BASE}/api/presets`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(preset),
  });
  if (!res.ok) throw new Error(`Failed to save preset: ${res.status}`);
}

export async function deletePreset(name: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/presets/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error(`Failed to delete preset: ${res.status}`);
}

export async function getDefaultPreset(): Promise<{name: string} | null> {
  const res = await fetch(`${API_BASE}/api/presets/default`);
  if (!res.ok) return null;
  return res.json();
}

export async function setDefaultPreset(name: string): Promise<void> {
  await fetch(`${API_BASE}/api/presets/default`, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({name}),
  });
}

export interface PitchResponse {
  song: string;
  pitch: number;
  files: Array<{ name: string; path: string }>;
}

export async function pitchStems(song: string, pitch: number): Promise<PitchResponse> {
  const res = await fetch(`${API_BASE}/api/pitch`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ song, pitch }),
  });
  if (!res.ok) throw new Error(`Pitch shift failed: ${res.status}`);
  return res.json();
}

export async function pitchFile(file: string, pitch: number): Promise<PitchResponse> {
  const res = await fetch(`${API_BASE}/api/pitch/file`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file, pitch }),
  });
  if (!res.ok) throw new Error(`Pitch file failed: ${res.status}`);
  return res.json();
}

export interface PitchUpload {
  name: string;
  path: string;
  subgroups: PitchSubgroup[];
}

export async function getPitchUploads(): Promise<PitchUpload[]> {
  const res = await fetch(`${API_BASE}/api/uploads/pitch`);
  if (!res.ok) return [];
  return res.json();
}

export interface PitchSubgroup {
  pitch: number;
  files: Array<{ name: string; path: string }>;
}

export async function getPitchSubgroups(song: string, signal?: AbortSignal): Promise<PitchSubgroup[]> {
  const res = await fetch(`${API_BASE}/api/pitch/${encodeURIComponent(song)}`, { signal });
  if (!res.ok) return [];
  return res.json();
}

export async function deletePitchSubgroup(song: string, pitch: number): Promise<void> {
  const pitchStr = pitch > 0 ? '+' + pitch : String(pitch);
  const res = await fetch(`${API_BASE}/api/pitch/${encodeURIComponent(song)}/${encodeURIComponent(pitchStr)}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error(`Failed to delete pitch subgroup: ${res.status}`);
}

export async function deletePitchStem(song: string, pitch: number, fileName: string): Promise<void> {
  const pitchStr = pitch > 0 ? '+' + pitch : String(pitch);
  const res = await fetch(`${API_BASE}/api/pitch/${encodeURIComponent(song)}/${encodeURIComponent(pitchStr)}/${encodeURIComponent(fileName)}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error(`Failed to delete pitch stem: ${res.status}`);
}

export interface KeyAlternative {
  key: string;
  scale: string;
  strength: number;
}

export interface KeyResponse {
  key: string;
  scale: string;
  strength: number;
  alternatives: KeyAlternative[];
  dubious: boolean;
}

export async function detectKey(file: File): Promise<KeyResponse> {
  const formData = new FormData();
  formData.append('file', file);
  const res = await fetch(`${API_BASE}/api/key`, {
    method: 'POST',
    body: formData,
  });
  if (!res.ok) {
    let detail = `Key detection failed with status ${res.status}: ${res.statusText}`;
    try {
      const data = (await res.json()) as { error?: string; detail?: string };
      if (data.error) detail = data.error;
      if (data.detail) detail += `: ${data.detail}`;
    } catch {
      // keep default detail
    }
    throw new Error(detail);
  }
  return (await res.json()) as KeyResponse;
}

export interface TempoGridBar {
  bar: number;
  start: number;
  end: number;
}

export interface TempoGridResponse {
  bpm: number;
  beats: number[];
  bars: TempoGridBar[];
  duration: number;
}

export async function getTempoGrid(file: string): Promise<TempoGridResponse> {
  return dawFetch(
    `${API_BASE}/api/audio/tempo-grid?file=${encodeURIComponent(file)}`,
  );
}

export interface TempoResponse {
  bpm: number;
  beats: number[];
  duration: number;
}

export async function detectBpm(file: string): Promise<TempoResponse> {
  return dawFetch(
    `${API_BASE}/api/audio/tempo?file=${encodeURIComponent(file)}`,
  );
}

// ---- DAW audio operations ----
export interface TrimResponse {
  file: string;
  path: string;
  url: string;
  name: string;
}

export async function trimAudio(file: string, start: number, end: number): Promise<TrimResponse> {
  return dawFetch(`${API_BASE}/api/audio/trim`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file, start, end }),
  });
}

export interface FadeResponse {
  file: string;
  path: string;
  url: string;
  name: string;
}

export async function fadeAudio(
  file: string,
  type: 'in' | 'out',
  start: number,
  duration: number,
): Promise<FadeResponse> {
  return dawFetch(`${API_BASE}/api/audio/fade`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file, type, start, duration }),
  });
}

export interface ExportResponse {
  file: string;
  path: string;
  url: string;
  name: string;
  format: string;
  size: number;
}

export async function exportAudio(
  file: string,
  format: string,
  bitrate?: string,
): Promise<ExportResponse> {
  const body: Record<string, string> = { file, format };
  if (bitrate) {
    body.bitrate = bitrate;
  }
  return dawFetch(`${API_BASE}/api/audio/export`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

// ---- Stem merge / mixdown export ----
export interface MergeResponse {
  file: string;
  path?: string;
  url?: string;
  format: string;
  size: number;
}

export interface FormatProfile {
  bitDepth?: string;
  sampleRate?: string;
  compression?: number;
  bitrate?: string;
  mode?: string;
}

export interface AudioExportProfiles {
  defaultFormat: string;
  nameTemplate?: string;
  formats: Record<string, FormatProfile>;
}

export async function mergeStems(
  song: string,
  stems: string[],
  format: string,
  outputName?: string,
): Promise<MergeResponse> {
  const body: Record<string, unknown> = { song, stems, format };
  if (outputName) {
    body.outputName = outputName;
  }
  const res = await fetch(`${API_BASE}/api/stems/merge`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`Merge failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as MergeResponse;
}

export async function getExportProfiles(): Promise<AudioExportProfiles> {
  const res = await fetch(`${API_BASE}/api/export/profiles`);
  if (!res.ok) {
    throw new Error(`Get export profiles failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as AudioExportProfiles;
}

export async function saveExportProfiles(
  profiles: AudioExportProfiles,
): Promise<{ status: string }> {
  const res = await fetch(`${API_BASE}/api/export/profiles`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(profiles),
  });
  if (!res.ok) {
    throw new Error(`Save export profiles failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as { status: string };
}

// ---- DAW stem import ----
export interface PitchStemEntry {
  song: string;
  pitch: string;
  stem: string;
}

export interface StemsResponse {
  output: Record<string, string[]>;
  pitch: PitchStemEntry[];
}

export interface DAWImportResponse {
  file: string;
  path: string;
  url: string;
  name: string;
  size: number;
}

export async function listStems(): Promise<StemsResponse> {
  const res = await fetch(`${API_BASE}/api/daw/stems`);
  if (!res.ok) {
    throw new Error(`List stems failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as StemsResponse;
}

export async function importStem(
  source: string,
  song?: string,
  stem?: string,
  pitch?: string,
  file?: string,
): Promise<DAWImportResponse> {
  const body: Record<string, unknown> = { source };
  if (song !== undefined) body.song = song;
  if (stem !== undefined) body.stem = stem;
  if (pitch !== undefined) body.pitch = pitch;
  if (file !== undefined) body.file = file;
  const res = await fetch(`${API_BASE}/api/daw/import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw await parseDAWError(res);
  }
  return (await res.json()) as DAWImportResponse;
}

export async function uploadAudioDAW(file: File): Promise<DAWImportResponse> {
  const formData = new FormData();
  formData.append('file', file);
  const res = await fetch(`${API_BASE}/api/daw/upload`, {
    method: 'POST',
    body: formData,
  });
  if (!res.ok) {
    throw new Error(`DAW upload failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as DAWImportResponse;
}

export interface DAWSongDeleteResult {
  deleted: boolean;
  song: string;
  files: number;
  bytes: number;
}

export async function deleteDawSong(song: string): Promise<DAWSongDeleteResult> {
  const res = await fetch(`${API_BASE}/api/daw/songs/${encodeURIComponent(song)}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    throw await parseDAWError(res);
  }
  return (await res.json()) as DAWSongDeleteResult;
}

// ---- VRAM Calculator ----
export interface VRAMModelEntry {
  name: string;
  type: string;
  vram_mb: number;
}

export interface VRAMCalculatorResponse {
  models: VRAMModelEntry[];
  total_vram_mb: number;
  available_vram_mb: number;
  free_after_mb: number;
  fits: boolean;
}

export async function getVRAMCalculator(params: {
  models: string;
  chunk_size?: number;
  shifts?: number;
  segment_size?: number;
  overlap?: number;
  batch_size?: number;
  demucs_segment?: number;
}): Promise<VRAMCalculatorResponse> {
  const qs = new URLSearchParams();
  qs.set('models', params.models);
  if (params.chunk_size !== undefined && params.chunk_size > 0) {
    qs.set('chunk_size', String(params.chunk_size));
  }
  if (params.shifts !== undefined && params.shifts > 0) {
    qs.set('shifts', String(params.shifts));
  }
  if (params.segment_size !== undefined && params.segment_size > 0) {
    qs.set('segment_size', String(params.segment_size));
  }
  if (params.overlap !== undefined && params.overlap > 0) {
    qs.set('overlap', String(params.overlap));
  }
  if (params.batch_size !== undefined && params.batch_size > 0) {
    qs.set('batch_size', String(params.batch_size));
  }
  if (params.demucs_segment !== undefined) {
    qs.set('demucs_segment', String(params.demucs_segment));
  }
  const res = await fetch(`${API_BASE}/api/gpu/vram-calculator?${qs.toString()}`);
  if (!res.ok) {
    throw new Error(`VRAM calculator failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as VRAMCalculatorResponse;
}

// ---- UI Settings (accent, theme, fontSize, scale) ----
export interface UISettings {
  accent: string;
  theme: string;    // 'light' | 'dark'
  fontSize: string; // 'small' | 'medium' | 'large'
  scale: number;    // 75-150
}

export async function loadUISettings(): Promise<UISettings | null> {
  try {
    const res = await fetch(`${API_BASE}/api/settings/ui`);
    if (!res.ok) return null;
    return (await res.json()) as UISettings;
  } catch {
    return null;
  }
}

export async function saveUISettings(settings: UISettings): Promise<void> {
  try {
    await fetch(`${API_BASE}/api/settings/ui`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(settings),
    });
  } catch {
    // Silently fail — localStorage is the fallback
  }
}

// ---- MIDI Types ----
export interface MidiNote {
  track: number;
  channel: number;
  key: number;
  velocity: number;
  start_ms: number;
  end_ms: number;
}

export interface MidiTrack {
  index: number;
  name: string;
  notes: MidiNote[];
}

export interface MidiParseResponse {
  tracks: MidiTrack[];
  bpm: number;
}

export interface MidiDevice {
  name: string;
  port: number;
  is_output: boolean;
}

export async function midiParse(file: string): Promise<MidiParseResponse> {
  const res = await fetch(`${API_BASE}/api/daw/midi/parse`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file }),
  });
  if (!res.ok) throw new Error(`MIDI parse failed: ${res.status} ${res.statusText}`);
  return (await res.json()) as MidiParseResponse;
}

export async function midiExport(tracks: MidiTrack[], bpm: number): Promise<Blob> {
  const res = await fetch(`${API_BASE}/api/daw/midi/export`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ tracks, bpm }),
  });
  if (!res.ok) throw new Error(`MIDI export failed: ${res.status} ${res.statusText}`);
  return await res.blob();
}

export async function midiDevices(): Promise<MidiDevice[]> {
  const res = await fetch(`${API_BASE}/api/daw/midi/devices`);
  if (!res.ok) return [];
  return (await res.json()) as MidiDevice[];
}

// ---- DAW Effects ----
export interface EffectResponse {
  file: string;
  path: string;
  url: string;
  name: string;
  parameters?: Record<string, number>;
}

export interface CompressorRequest {
  file: string;
  threshold: number;
  ratio: number;
  attack: number;
  release: number;
  makeup: number;
}

export interface ReverbRequest {
  file: string;
  room_size: number;
  decay: number;
  wet_dry: number;
}

export interface DelayRequest {
  file: string;
  delay_time: number;
  feedback: number;
  wet_dry: number;
}

export interface ChorusRequest {
  file: string;
  depth: number;
  rate: number;
  delay_ms: number;
  wet_dry: number;
}

export interface FlangerRequest {
  file: string;
  depth: number;
  rate: number;
  wet_dry: number;
}

export interface PhaserRequest {
  file: string;
  depth: number;
  rate: number;
  wet_dry: number;
}

export interface TremoloRequest {
  file: string;
  speed: number;
  depth: number;
}

export interface NoiseGateRequest {
  file: string;
  threshold: number;
  attack: number;
  release: number;
}

export async function applyCompressor(req: CompressorRequest): Promise<EffectResponse> {
  return dawFetch(`${API_BASE}/api/daw/compressor`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

export async function applyReverb(req: ReverbRequest): Promise<EffectResponse> {
  return dawFetch(`${API_BASE}/api/daw/reverb`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

export async function applyDelay(req: DelayRequest): Promise<EffectResponse> {
  return dawFetch(`${API_BASE}/api/daw/delay`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

export async function applyChorus(req: ChorusRequest): Promise<EffectResponse> {
  return dawFetch(`${API_BASE}/api/daw/chorus`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

export async function applyFlanger(req: FlangerRequest): Promise<EffectResponse> {
  return dawFetch(`${API_BASE}/api/daw/flanger`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

export async function applyPhaser(req: PhaserRequest): Promise<EffectResponse> {
  return dawFetch(`${API_BASE}/api/daw/phaser`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

export async function applyTremolo(req: TremoloRequest): Promise<EffectResponse> {
  return dawFetch(`${API_BASE}/api/daw/tremolo`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

export async function applyNoiseGate(req: NoiseGateRequest): Promise<EffectResponse> {
  return dawFetch(`${API_BASE}/api/daw/noisegate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

// ---- Storage config ----
export interface StorageFolder {
  exists: boolean;
  files: number;
  bytes: number;
}

export interface StorageConfig {
  current_root: string;
  source: 'env' | 'settings' | 'default';
  exists: boolean;
  writable: boolean;
  folders: Record<string, StorageFolder>;
  mode: 'container' | 'standalone';
  candidates: string[];
  note: string;
  export_dir: string;
  export_source: 'env' | 'settings' | 'default';
  export_exists: boolean;
  export_writable: boolean;
}

export async function getStorageConfig(): Promise<StorageConfig> {
  const res = await fetch(`${API_BASE}/api/storage/config`);
  if (!res.ok) {
    throw new Error(`Storage config fetch failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as StorageConfig;
}

export async function setStorageConfig(root: string): Promise<StorageConfig> {
  const res = await fetch(`${API_BASE}/api/storage/config`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ root }),
  });
  if (!res.ok) {
    let detail = `Request failed with status ${res.status}: ${res.statusText}`;
    try {
      const data = (await res.json()) as { error?: string };
      if (data.error) detail = data.error;
    } catch {
      // keep default detail
    }
    throw new Error(detail);
  }
  return (await res.json()) as StorageConfig;
}

export async function setExportDir(exportDir: string): Promise<StorageConfig> {
  const res = await fetch(`${API_BASE}/api/storage/config`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ export_dir: exportDir }),
  });
  if (!res.ok) {
    let detail = `Request failed with status ${res.status}: ${res.statusText}`;
    try {
      const data = (await res.json()) as { error?: string };
      if (data.error) detail = data.error;
    } catch {
      // keep default detail
    }
    throw new Error(detail);
  }
  return (await res.json()) as StorageConfig;
}

// ---- DAW EQ ----
export interface EqFilter {
  type: 'peak' | 'lowshelf' | 'highshelf' | 'lowpass' | 'highpass';
  freq: number;
  gain: number;
  q: number;
}

export interface EqRequest {
  file: string;
  filters: EqFilter[];
}

export interface EqResponse {
  file: string;
  path: string;
  url: string;
  name: string;
  filters_applied: number;
}

export async function applyEQ(req: EqRequest): Promise<EqResponse> {
  return dawFetch(`${API_BASE}/api/daw/eq`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}
