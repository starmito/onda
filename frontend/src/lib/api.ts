const API_BASE = 'http://192.168.1.87:3000';

export interface HealthResponse {
  status: string;
  container: string;
  gpu: boolean;
  gpu_info?: string;
  disk?: string;
  docker?: string;
  version: string;
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
  viperx?: boolean;
  viperx_keep?: 'both' | 'vocals' | 'instrumental';
  demucs?: boolean;
  demucs_keep?: string[];
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
    const body: Record<string, string> = {
      preset: opts.preset,
      input: opts.input,
    };
    if (opts.pitch !== undefined && opts.pitch !== 0) {
      body.pitch = String(opts.pitch);
    }
    if (opts.viperx) {
      body.viperx = 'on';
      body.viperx_keep = opts.viperx_keep ?? 'both';
    }
    if (opts.demucs) {
      body.demucs = 'on';
      body.demucs_keep = (opts.demucs_keep ?? ['drums', 'bass', 'other', 'vocals']).join(',');
    }
    if (opts.output !== undefined) {
      body.output = opts.output;
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
