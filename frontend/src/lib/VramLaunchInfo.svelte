<script lang="ts">
  import { onMount } from 'svelte';
  import {
    getGpuInfo,
    getModelConfig,
    getVRAMCalculator,
    buildVRAMCalculatorParams,
    type PipelineStep,
    type GpuInfo,
    type ModelFlagsResponse,
    type VRAMCalculatorResponse,
    type VRAMModelEntry,
  } from './api';

  interface Props {
    steps?: PipelineStep[];
  }

  let { steps = [] }: Props = $props();

  let gpuInfo = $state<GpuInfo | null>(null);
  let gpuError = $state(false);
  let modelConfigs = $state<Record<string, ModelFlagsResponse>>({});
  let stepEstimates = $state<VRAMCalculatorResponse[]>([]);
  let loading = $state(false);
  let error = $state('');

  const enabledSteps = $derived(steps.filter((s) => s.enabled && s.model));

  function formatMb(mb: number): string {
    if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
    return `${Math.round(mb)} MB`;
  }

  function pickEntryMB(entry: VRAMModelEntry): {
    mb: number;
    source: 'measured' | 'estimated';
    measuredMb: number;
    measuredN: number;
    estimatedMb: number;
  } {
    if (entry.source === 'measured' && entry.measured_mb && entry.measured_mb > 0) {
      return {
        mb: entry.measured_mb,
        source: 'measured',
        measuredMb: entry.measured_mb,
        measuredN: entry.measured_n ?? 0,
        estimatedMb: entry.estimated_mb ?? entry.vram_mb,
      };
    }
    return {
      mb: entry.vram_mb,
      source: 'estimated',
      measuredMb: entry.measured_mb ?? 0,
      measuredN: entry.measured_n ?? 0,
      estimatedMb: entry.estimated_mb ?? entry.vram_mb,
    };
  }

  onMount(() => {
    async function loadGpu() {
      try {
        const info = await getGpuInfo();
        if (info.ok && info.vram_total_mb > 0) {
          gpuInfo = info;
          gpuError = false;
        } else {
          gpuError = true;
        }
      } catch {
        gpuError = true;
      }
    }
    loadGpu();
  });

  $effect(() => {
    const models = enabledSteps.map((s) => s.model);
    if (models.length === 0) return;

    let cancelled = false;
    loading = true;
    error = '';
    modelConfigs = {};

    async function load() {
      const configs: Record<string, ModelFlagsResponse> = {};
      try {
        for (const model of models) {
          if (!configs[model]) {
            configs[model] = await getModelConfig(model);
          }
        }
      } catch (err: any) {
        if (!cancelled) {
          error = err?.message || 'No se pudo leer la configuración de modelos';
          loading = false;
        }
        return;
      }
      if (cancelled) return;
      modelConfigs = configs;

      try {
        const estimates: VRAMCalculatorResponse[] = [];
        for (const step of enabledSteps) {
          const cfg = configs[step.model];
          const values: Record<string, number | string> = {};
          for (const f of cfg?.flags ?? []) {
            values[f.name] = f.value;
          }
          const params = buildVRAMCalculatorParams(step.model, values);
          const estimate = await getVRAMCalculator(params);
          estimates.push(estimate);
        }
        if (!cancelled) {
          stepEstimates = estimates;
          loading = false;
        }
      } catch (err: any) {
        if (!cancelled) {
          error = err?.message || 'No se pudo estimar la VRAM';
          loading = false;
        }
      }
    }

    load();
    return () => {
      cancelled = true;
    };
  });

  const stepSummaries = $derived(
    stepEstimates.map((estimate, index) => {
      const step = enabledSteps[index];
      const entry = estimate.models[0];
      const picked = entry ? pickEntryMB(entry) : null;
      return {
        name: step?.model ?? '',
        mb: picked?.mb ?? estimate.total_vram_mb,
        source: picked?.source ?? 'estimated',
        measuredMb: picked?.measuredMb ?? 0,
        measuredN: picked?.measuredN ?? 0,
        estimatedMb: picked?.estimatedMb ?? estimate.total_vram_mb,
      };
    }),
  );

  const totalRequiredMb = $derived(
    stepSummaries.reduce((sum, s) => sum + s.mb, 0),
  );
  const maxStepMb = $derived(
    stepSummaries.length > 0 ? Math.max(...stepSummaries.map((s) => s.mb)) : 0,
  );
  const maxStepSummary = $derived(
    stepSummaries.length > 0
      ? stepSummaries.reduce((max, s) => (s.mb > max.mb ? s : max), stepSummaries[0])
      : null,
  );
  const fits = $derived(gpuInfo ? gpuInfo.vram_free_mb >= maxStepMb : false);
  const tight = $derived(
    gpuInfo && gpuInfo.vram_free_mb >= maxStepMb && gpuInfo.vram_free_mb < totalRequiredMb,
  );
</script>

<div class="vram-launch-info" data-testid="vram-launch-info">
  {#if loading}
    <span class="vram-text muted">Calculando VRAM...</span>
  {:else if error}
    <span class="vram-text error">{error}</span>
  {:else if gpuError}
    <span class="vram-text warning">No se puede leer la GPU. El trabajo puede bloquearse.</span>
  {:else if gpuInfo}
    <div class="vram-row">
      <span class="vram-label">VRAM:</span>
      <span class="vram-value">
        {#if stepSummaries.length > 0 && maxStepSummary}
          {#if maxStepSummary.source === 'measured'}
            medido: ~{formatMb(maxStepSummary.mb)} por paso
            {#if maxStepSummary.measuredN > 0}
              (n={maxStepSummary.measuredN})
            {/if}
          {:else}
            estimado: ~{formatMb(maxStepSummary.mb)} por paso
          {/if}
          {#if stepSummaries.length > 1}
            · hasta ~{formatMb(totalRequiredMb)} si se solapan
          {/if}
        {:else}
          sin modelo seleccionado
        {/if}
      </span>
    </div>
    <div class="vram-row">
      <span class="vram-label">Tarjeta:</span>
      <span class="vram-value">
        {formatMb(gpuInfo.vram_free_mb)} libres / {formatMb(gpuInfo.vram_total_mb)} total
      </span>
    </div>
    {#if stepSummaries.length > 0}
      <div class="vram-row">
        <span class="vram-badge" class:fits class:tight>
          {#if fits}
            {#if tight}
              ⚠️ Cabe justo
            {:else}
              ✓ Cabe
            {/if}
          {:else}
            ✗ Puede no caber
          {/if}
        </span>
      </div>
    {/if}
  {/if}
</div>

<style>
  .vram-launch-info {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    padding: 0.6rem 0.75rem;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 8px;
    font-size: 0.8rem;
  }

  .vram-row {
    display: flex;
    gap: 0.4rem;
    align-items: center;
  }

  .vram-label {
    color: var(--text-secondary);
    font-weight: 600;
  }

  .vram-value {
    color: var(--text-primary);
  }

  .vram-text {
    color: var(--text-primary);
  }

  .vram-text.muted {
    color: var(--text-muted);
  }

  .vram-text.error {
    color: #e57373;
  }

  .vram-text.warning {
    color: #ffd54f;
  }

  .vram-badge {
    display: inline-flex;
    align-items: center;
    padding: 0.15rem 0.5rem;
    border-radius: 10px;
    font-size: 0.75rem;
    font-weight: 700;
    background: #2a2a4a;
    color: var(--text-secondary);
  }

  .vram-badge.fits {
    background: #1b3a1b;
    color: #81c784;
  }

  .vram-badge.tight {
    background: #3a3a1b;
    color: #ffd54f;
  }
</style>
