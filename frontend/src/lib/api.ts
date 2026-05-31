const API_BASE = 'http://192.168.1.87:3000';

export interface HealthComponent {
  ok: boolean;
  detail?: string;
  version?: string;
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

export interface ModelsResponse {
  [key: string]: {
    name: string;
    description: string;
  };
}

export interface SeparateResponse {
  status: string;
  song: string;
}

export interface SeparateOptions {
  preset: string;
  input: string;
  pitch?: number;
  vocal_model?: string;
  stem_model?: string;
  viperx?: boolean;
  viperx_keep?: 'both' | 'vocals' | 'instrumental';
  viperx_model?: string;
  viperx_stems?: string[];
  demucs?: boolean;
  demucs_keep?: string[];
  demucs_model?: string;
  demucs_stems?: string[];
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

export async function uploadMultiple(files: File[]): Promise<UploadResponse[]> {
  const results: UploadResponse[] = [];
  for (const file of files) {
    results.push(await uploadAudio(file));
  }
  return results;
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

export async function getModels(): Promise<ModelsResponse> {
  try {
    const res = await fetch(`${API_BASE}/api/models`);
    if (!res.ok) {
      throw new Error(`Failed to fetch models (status ${res.status}): ${res.statusText}`);
    }
    return (await res.json()) as ModelsResponse;
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error fetching models: ${String(err)}`);
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
    if (opts.vocal_model) body.vocal_model = opts.vocal_model;
    if (opts.stem_model) body.stem_model = opts.stem_model;
    // PipelineConfig flags
    if (opts.viperx !== undefined) body.viperx = opts.viperx;
    if (opts.demucs !== undefined) body.demucs = opts.demucs;
    if (opts.viperx_keep) body.viperx_keep = opts.viperx_keep;
    if (opts.viperx_model) body.viperx_model = opts.viperx_model;
    if (opts.viperx_stems && opts.viperx_stems.length > 0) body.viperx_stems = opts.viperx_stems;
    if (opts.demucs_keep && opts.demucs_keep.length > 0) body.demucs_keep = opts.demucs_keep;
    if (opts.demucs_model) body.demucs_model = opts.demucs_model;
    if (opts.demucs_stems && opts.demucs_stems.length > 0) body.demucs_stems = opts.demucs_stems;
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

// ---- VramCalculator ---- 
export interface VramEstimateResponse {
  total_vram_mb: number;
  available_vram_mb: number;
}

export async function getVramEstimate(models: string): Promise<VramEstimateResponse> {
  const res = await fetch(`${API_BASE}/api/gpu/vram-calculator?models=${encodeURIComponent(models)}`);
  if (!res.ok) {
    throw new Error(`VRAM estimate failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as VramEstimateResponse;
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
}

export interface DownloadModelResponse {
  status: string;
  message?: string;
}

export async function downloadModel(repo: string): Promise<DownloadModelResponse> {
  const body: DownloadModelRequest = { source: 'huggingface', repo };
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

// ---- Model list (per-model info) ----
export interface ModelInfo {
  name: string;
  category: string;
  description?: string;
}

export async function getModelList(): Promise<ModelInfo[]> {
  try {
    const res = await fetch(`${API_BASE}/api/models/list`);
    if (!res.ok) {
      throw new Error(`Failed to fetch model list (status ${res.status}): ${res.statusText}`);
    }
    return (await res.json()) as ModelInfo[];
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
    throw new Error(`Unexpected error fetching model list: ${String(err)}`);
  }
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
