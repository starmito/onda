<script lang="ts">
  import { getVramEstimate } from './api';

  export interface Preset {
    key: string;
    name: string;
    description: string;
    models: Record<string, string>;
  }

  let {
    preset = null,
  }: {
    preset?: Preset | null;
  } = $props();

  let estimatedMb = $state<number | null>(null);
  let availableMb = $state<number | null>(null);
  let loading = $state(false);
  let error = $state('');

  $effect(() => {
    if (!preset || !preset.models || Object.keys(preset.models).length === 0) {
      estimatedMb = null;
      availableMb = null;
      error = '';
      return;
    }

    // Build query string: vocal=polarformer,stems=htdemucs_ft
    const queryParts = Object.entries(preset.models)
      .filter(([, model]) => model)
      .map(([key, model]) => `${key}=${model}`);
    const query = queryParts.join(',');

    if (!query) {
      estimatedMb = null;
      availableMb = null;
      return;
    }

    loading = true;
    error = '';

    getVramEstimate(query)
      .then((res) => {
        estimatedMb = res.estimated_mb;
        availableMb = res.available_mb;
      })
      .catch((err) => {
        error = err.message || 'Error al estimar VRAM';
        estimatedMb = null;
        availableMb = null;
      })
      .finally(() => {
        loading = false;
      });
  });

  function fits(): boolean | null {
    if (estimatedMb === null || availableMb === null) return null;
    return estimatedMb <= availableMb;
  }

  function missingMb(): number {
    if (estimatedMb === null || availableMb === null) return 0;
    return estimatedMb - availableMb;
  }
</script>

<div class="vram-calculator">
  {#if !preset}
    <p class="info-text">Selecciona un preset para ver el consumo de VRAM</p>
  {:else if loading}
    <p class="info-text">⏳ Calculando consumo de VRAM...</p>
  {:else if error}
    <p class="error-text">⚠️ {error}</p>
  {:else if estimatedMb !== null && availableMb !== null}
    <p class="estimate-text">
      Este preset consumirá ~<strong>{estimatedMb} MB</strong> de VRAM
    </p>
    {#if fits()}
      <p class="fits-text">
        VRAM disponible: {availableMb} MB → Cabe ✅
      </p>
    {:else}
      <p class="no-fits-text">
        ¡No cabe! ❌ (faltan {missingMb()} MB)
      </p>
    {/if}
  {:else}
    <p class="info-text">No se pudo obtener la estimación de VRAM</p>
  {/if}
</div>

<style>
  .vram-calculator {
    padding: 1rem;
    background-color: #1a1a2e;
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .info-text {
    margin: 0;
    color: #888;
    font-size: 0.9rem;
    text-align: center;
  }

  .error-text {
    margin: 0;
    color: #f4a236;
    font-size: 0.9rem;
    text-align: center;
  }

  .estimate-text {
    margin: 0;
    color: #e0e0e0;
    font-size: 0.95rem;
    text-align: center;
  }

  .estimate-text strong {
    color: #00d4ff;
  }

  .fits-text {
    margin: 0;
    color: #4caf50;
    font-size: 0.9rem;
    text-align: center;
    font-weight: 500;
  }

  .no-fits-text {
    margin: 0;
    color: #ff5252;
    font-size: 0.9rem;
    text-align: center;
    font-weight: 500;
  }
</style>
