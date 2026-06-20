const API_BASE = '';

export interface HealthComponent {
  ok: boolean;
  detail?: string;
  version?: string;
  type?: 'cuda' | 'rocm' | 'cpu';
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
export interface QueueJob {
  song: string;
  status: 'waiting' | 'processing' | 'done' | 'error';
  progress: number;
  current_step?: number;
  total_steps?: number;
  step_name?: string;
  eta?: string;
  device?: string;
  error?: string;
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

export async function cancelQueue(): Promise<void> {
  const res = await fetch(`${API_BASE}/api/queue/cancel`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  });
  if (!res.ok) {
    throw new Error(`Queue cancel failed with status ${res.status}: ${res.statusText}`);
  }
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
}

export async function getInputs(): Promise<InputEntry[]> {
  try {
    const res = await fetch(`${API_BASE}/api/inputs`);
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

// ---- DAW audio operations ----
export interface TrimResponse {
  file: string;
}

export async function trimAudio(file: string, start: number, end: number): Promise<TrimResponse> {
  const res = await fetch(`${API_BASE}/api/audio/trim`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file, start, end }),
  });
  if (!res.ok) {
    throw new Error(`Trim failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as TrimResponse;
}

export interface FadeResponse {
  file: string;
}

export async function fadeAudio(
  file: string,
  type: 'in' | 'out',
  start: number,
  duration: number,
): Promise<FadeResponse> {
  const res = await fetch(`${API_BASE}/api/audio/fade`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file, type, start, duration }),
  });
  if (!res.ok) {
    throw new Error(`Fade failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as FadeResponse;
}

export interface ExportResponse {
  file: string;
  format: string;
  size: number;
}

export async function exportAudio(file: string, format: string): Promise<ExportResponse> {
  const res = await fetch(`${API_BASE}/api/audio/export`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file, format }),
  });
  if (!res.ok) {
    throw new Error(`Export failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as ExportResponse;
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
