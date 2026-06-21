<script lang="ts">
  import DAWPage from './DAWPage.svelte';
  import BasicEffectsPanel from './BasicEffectsPanel.svelte';
  import BasicEQPanel from './BasicEQPanel.svelte';
  import MIDIPage from './MIDIPage.svelte';
  import SpectrogramPage from './SpectrogramPage.svelte';
  import { IconSliders, IconPiano, IconSpectrogram } from './icons';
  import {
    applyCompressor,
    applyReverb,
    applyDelay,
    applyChorus,
    applyFlanger,
    applyPhaser,
    applyTremolo,
    applyNoiseGate,
    applyEQ,
  } from './api';
  import type { EqFilter } from './api';

  let viewMode = $state<'basic' | 'medium' | 'full'>('basic');
  let activeFile = $state<string | null>(null);
  let activeTab = $state<'effects' | 'midi' | 'spectrogram'>('effects');

  function handleActiveTrackChange(fileName: string | null) {
    activeFile = fileName;
  }

  // ===== Full mixer state =====
  let dawPageRef = $state<any>(null);
  let selectedChannelId = $state<string>('');
  let fullDetailTab = $state<'effects' | 'eq' | 'routing'>('effects');

  const mixerTracks = $derived<any[]>(dawPageRef?.tracks ?? []);

  let channelPans = $state<Record<string, number>>({});
  let channelInserts = $state<Record<string, (string | null)[]>>({});
  let channelInsertValues = $state<Record<string, Record<number, Record<string, number>>>>({});
  let channelEqBands = $state<Record<string, EqFilter[]>>({});
  let insertLoading = $state<Record<string, boolean>>({});
  let insertResults = $state<Record<string, string>>({});
  let eqLoading = $state(false);
  let eqResult = $state('');
  let eqFiltersApplied = $state(0);

  const selectedTrack = $derived<any | undefined>(
    mixerTracks.find((t) => t.id === selectedChannelId)
  );

  $effect(() => {
    for (const track of mixerTracks) {
      if (!(track.id in channelPans)) channelPans[track.id] = 0;
      if (!(track.id in channelInserts)) channelInserts[track.id] = [null, null, null, null];
      if (!(track.id in channelEqBands)) {
        channelEqBands[track.id] = Array.from({ length: 4 }, (_, i) => ({
          type: 'peak',
          freq: [80, 250, 1000, 4000][i],
          gain: 0,
          q: 1,
        }));
      }
    }
    if (mixerTracks.length > 0 && !selectedChannelId) {
      selectedChannelId = mixerTracks[0].id;
    }
    if (selectedChannelId && !mixerTracks.some((t) => t.id === selectedChannelId)) {
      selectedChannelId = mixerTracks[0]?.id ?? '';
    }
  });

  type EffectParam = {
    key: string;
    label: string;
    min: number;
    max: number;
    step: number;
    default: number;
    unit: string;
  };

  type EffectDef = {
    id: string;
    name: string;
    params: EffectParam[];
    apply: (file: string, values: Record<string, number>) => Promise<string>;
  };

  const effects: EffectDef[] = [
    {
      id: 'compressor',
      name: 'Compressor',
      params: [
        { key: 'threshold', label: 'Threshold', min: -60, max: 0, step: 1, default: -20, unit: 'dB' },
        { key: 'ratio', label: 'Ratio', min: 1, max: 20, step: 0.5, default: 4, unit: ':1' },
        { key: 'attack', label: 'Attack', min: 0.1, max: 100, step: 0.1, default: 5, unit: 'ms' },
        { key: 'release', label: 'Release', min: 10, max: 1000, step: 1, default: 50, unit: 'ms' },
        { key: 'makeup', label: 'Makeup', min: 0, max: 20, step: 0.5, default: 0, unit: 'dB' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyCompressor({
          file,
          threshold: values.threshold,
          ratio: values.ratio,
          attack: values.attack,
          release: values.release,
          makeup: values.makeup,
        });
        return resp.file;
      },
    },
    {
      id: 'reverb',
      name: 'Reverb',
      params: [
        { key: 'room_size', label: 'Room size', min: 0, max: 1, step: 0.01, default: 0.5, unit: '' },
        { key: 'decay', label: 'Decay', min: 0.1, max: 10, step: 0.1, default: 2, unit: 's' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyReverb({
          file,
          room_size: values.room_size,
          decay: values.decay,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'delay',
      name: 'Delay',
      params: [
        { key: 'delay_time', label: 'Delay time', min: 20, max: 2000, step: 10, default: 300, unit: 'ms' },
        { key: 'feedback', label: 'Feedback', min: 0, max: 100, step: 1, default: 30, unit: '%' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyDelay({
          file,
          delay_time: values.delay_time,
          feedback: values.feedback,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'chorus',
      name: 'Chorus',
      params: [
        { key: 'depth', label: 'Depth', min: 0, max: 100, step: 1, default: 30, unit: '%' },
        { key: 'rate', label: 'Rate', min: 0.1, max: 10, step: 0.1, default: 1.5, unit: 'Hz' },
        { key: 'delay_ms', label: 'Delay', min: 10, max: 100, step: 1, default: 25, unit: 'ms' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyChorus({
          file,
          depth: values.depth,
          rate: values.rate,
          delay_ms: values.delay_ms,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'flanger',
      name: 'Flanger',
      params: [
        { key: 'depth', label: 'Depth', min: 0, max: 100, step: 1, default: 30, unit: '%' },
        { key: 'rate', label: 'Rate', min: 0.1, max: 10, step: 0.1, default: 1.5, unit: 'Hz' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyFlanger({
          file,
          depth: values.depth,
          rate: values.rate,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'phaser',
      name: 'Phaser',
      params: [
        { key: 'depth', label: 'Depth', min: 0, max: 100, step: 1, default: 50, unit: '%' },
        { key: 'rate', label: 'Rate', min: 0.1, max: 10, step: 0.1, default: 1.5, unit: 'Hz' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyPhaser({
          file,
          depth: values.depth,
          rate: values.rate,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'tremolo',
      name: 'Tremolo',
      params: [
        { key: 'speed', label: 'Speed', min: 0.1, max: 20, step: 0.1, default: 5, unit: 'Hz' },
        { key: 'depth', label: 'Depth', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyTremolo({
          file,
          speed: values.speed,
          depth: values.depth,
        });
        return resp.file;
      },
    },
    {
      id: 'noisegate',
      name: 'Noise Gate',
      params: [
        { key: 'threshold', label: 'Threshold', min: -80, max: 0, step: 1, default: -40, unit: 'dB' },
        { key: 'attack', label: 'Attack', min: 0.1, max: 100, step: 0.1, default: 5, unit: 'ms' },
        { key: 'release', label: 'Release', min: 10, max: 1000, step: 1, default: 50, unit: 'ms' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyNoiseGate({
          file,
          threshold: values.threshold,
          attack: values.attack,
          release: values.release,
        });
        return resp.file;
      },
    },
  ];

  function setVolume(track: any, vol: number) {
    dawPageRef?.setTrackVolume?.(track, vol);
  }

  function toggleMute(track: any) {
    dawPageRef?.toggleMute?.(track);
  }

  function toggleSolo(track: any) {
    dawPageRef?.toggleSolo?.(track);
  }

  function selectChannel(id: string) {
    selectedChannelId = id;
  }

  function setPan(id: string, pan: number) {
    channelPans[id] = pan;
  }

  function slotKey(trackId: string, slot: number): string {
    return `${trackId}:${slot}`;
  }

  function assignEffect(trackId: string, slot: number, effectId: string | null) {
    if (!channelInserts[trackId]) channelInserts[trackId] = [null, null, null, null];
    channelInserts[trackId][slot] = effectId;
    if (effectId) {
      const effect = effects.find((e) => e.id === effectId);
      if (effect) {
        if (!channelInsertValues[trackId]) channelInsertValues[trackId] = {};
        if (!channelInsertValues[trackId][slot]) channelInsertValues[trackId][slot] = {};
        for (const param of effect.params) {
          channelInsertValues[trackId][slot][param.key] = param.default;
        }
      }
    }
  }

  function removeEffect(trackId: string, slot: number) {
    assignEffect(trackId, slot, null);
  }

  function updateInsertValue(trackId: string, slot: number, key: string, value: number) {
    if (!channelInsertValues[trackId]) channelInsertValues[trackId] = {};
    if (!channelInsertValues[trackId][slot]) channelInsertValues[trackId][slot] = {};
    channelInsertValues[trackId][slot][key] = value;
  }

  async function applyInsert(trackId: string, slot: number) {
    const track = mixerTracks.find((t) => t.id === trackId);
    const effectId = channelInserts[trackId]?.[slot];
    if (!track?.fileName || !effectId) return;
    const effect = effects.find((e) => e.id === effectId);
    if (!effect) return;

    const key = slotKey(trackId, slot);
    insertLoading[key] = true;
    insertResults[key] = '';
    try {
      const outputFile = await effect.apply(track.fileName, channelInsertValues[trackId][slot]);
      insertResults[key] = `Aplicado: ${outputFile}`;
    } catch (err) {
      insertResults[key] = `Error: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      insertLoading[key] = false;
    }
  }

  // ===== EQ helpers =====
  const MIN_FREQ = 20;
  const MAX_FREQ = 20000;
  const LOG_MIN = Math.log10(MIN_FREQ);
  const LOG_MAX = Math.log10(MAX_FREQ);
  const filterTypes: EqFilter['type'][] = ['peak', 'lowshelf', 'highshelf', 'lowpass', 'highpass'];

  function freqToPosition(freq: number): number {
    return (Math.log10(freq) - LOG_MIN) / (LOG_MAX - LOG_MIN);
  }

  function positionToFreq(pos: number): number {
    const clamped = Math.max(0, Math.min(1, pos));
    const logVal = LOG_MIN + clamped * (LOG_MAX - LOG_MIN);
    return Math.round(Math.pow(10, logVal));
  }

  function formatFreq(freq: number): string {
    if (freq >= 1000) return `${(freq / 1000).toFixed(1)}k`;
    return `${freq}`;
  }

  function updateBandType(trackId: string, index: number, type: EqFilter['type']) {
    channelEqBands[trackId][index].type = type;
  }

  function updateBandFreq(trackId: string, index: number, position: number) {
    channelEqBands[trackId][index].freq = positionToFreq(position);
  }

  function updateBandGain(trackId: string, index: number, gain: number) {
    channelEqBands[trackId][index].gain = gain;
  }

  function updateBandQ(trackId: string, index: number, q: number) {
    channelEqBands[trackId][index].q = q;
  }

  function needsGain(type: string): boolean {
    return type === 'peak' || type === 'lowshelf' || type === 'highshelf';
  }

  async function applySelectedEq() {
    const track = selectedTrack;
    if (!track?.fileName) return;
    eqLoading = true;
    eqResult = '';
    try {
      const resp = await applyEQ({ file: track.fileName, filters: channelEqBands[track.id] });
      eqFiltersApplied = resp.filters_applied;
      eqResult = `EQ aplicado: ${resp.file}`;
    } catch (err) {
      eqFiltersApplied = 0;
      eqResult = `Error: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      eqLoading = false;
    }
  }
</script>

<div class="daw-workspace">
  <div class="workspace-toolbar">
    <label class="mode-select">
      <span>Modo</span>
      <select bind:value={viewMode}>
        <option value="basic">Básico</option>
        <option value="medium">Medio</option>
        <option value="full">Completo</option>
      </select>
    </label>

    {#if activeFile}
      <span class="active-file">{activeFile}</span>
    {:else}
      <span class="active-file empty">Sin pista activa</span>
    {/if}
  </div>

  <div class="audio-panel">
    <DAWPage bind:this={dawPageRef} onActiveTrackChange={handleActiveTrackChange} />
  </div>

  <div class="mode-panel">
    {#if viewMode === 'basic'}
      <div class="basic-grid">
        <BasicEffectsPanel activeFile={activeFile} />
        <BasicEQPanel activeFile={activeFile} />
      </div>
    {:else if viewMode === 'medium'}
      <div class="medium-panel">
        <div class="tab-bar">
          <button
            class="tab-button"
            class:active={activeTab === 'effects'}
            onclick={() => (activeTab = 'effects')}
            type="button"
          >
            <span class="tab-icon">{@html IconSliders}</span>
            <span>Efectos + EQ</span>
          </button>
          <button
            class="tab-button"
            class:active={activeTab === 'midi'}
            onclick={() => (activeTab = 'midi')}
            type="button"
          >
            <span class="tab-icon">{@html IconPiano}</span>
            <span>Piano Roll</span>
          </button>
          <button
            class="tab-button"
            class:active={activeTab === 'spectrogram'}
            onclick={() => (activeTab = 'spectrogram')}
            type="button"
          >
            <span class="tab-icon">{@html IconSpectrogram}</span>
            <span>Espectrograma</span>
          </button>
        </div>

        <div class="tab-content">
          {#if activeTab === 'effects'}
            <div class="basic-grid">
              <BasicEffectsPanel activeFile={activeFile} />
              <BasicEQPanel activeFile={activeFile} />
            </div>
          {:else if activeTab === 'midi'}
            <MIDIPage />
          {:else if activeTab === 'spectrogram'}
            <SpectrogramPage />
          {/if}
        </div>
      </div>
    {:else if viewMode === 'full'}
      <div class="full-mixer">
        <div class="mixer-toolbar">
          <button class="btn-back" onclick={() => (viewMode = 'basic')} type="button">
            Volver al modo básico
          </button>
          <span class="mixer-title">Mixer</span>
        </div>

        <div class="mixer-body">
          <div class="mixer-strips">
            {#if mixerTracks.length === 0}
              <div class="mixer-empty">Carga pistas en el DAW para ver los canales.</div>
            {:else}
              {#each mixerTracks as track, idx (track.id)}
                <div
                  class="channel-strip"
                  class:selected={selectedChannelId === track.id}
                >
                  <div class="channel-header">
                    <span class="channel-number">{idx + 1}</span>
                    <span class="channel-name" title={track.name}>{track.name}</span>
                  </div>

                  <div class="fader-wrap">
                    <input
                      type="range"
                      class="fader"
                      orient="vertical"
                      min="0"
                      max="1"
                      step="0.01"
                      value={track.volume}
                      oninput={(e) => setVolume(track, parseFloat(e.currentTarget.value))}
                    />
                    <span class="fader-value">{(track.volume * 100).toFixed(0)}%</span>
                  </div>

                  <div class="pan-wrap">
                    <span class="pan-label">Pan</span>
                    <input
                      type="range"
                      class="pan"
                      min="-1"
                      max="1"
                      step="0.01"
                      value={channelPans[track.id] ?? 0}
                      oninput={(e) => setPan(track.id, parseFloat(e.currentTarget.value))}
                    />
                    <span class="pan-value">{((channelPans[track.id] ?? 0) * 100).toFixed(0)}</span>
                  </div>

                  <div class="channel-buttons">
                    <button
                      class="btn-mute"
                      class:active={track.muted}
                      onclick={() => toggleMute(track)}
                      type="button"
                      title="Mute"
                    >
                      M
                    </button>
                    <button
                      class="btn-solo"
                      class:active={track.solo}
                      onclick={() => toggleSolo(track)}
                      type="button"
                      title="Solo"
                    >
                      S
                    </button>
                    <button
                      class="btn-select"
                      class:active={selectedChannelId === track.id}
                      onclick={() => selectChannel(track.id)}
                      type="button"
                      title="Seleccionar"
                    >
                      Sel
                    </button>
                  </div>
                </div>
              {/each}
            {/if}
          </div>

          <div class="detail-panel">
            {#if !selectedTrack}
              <div class="detail-empty">Selecciona un canal para ver su detalle.</div>
            {:else}
              {@const track = selectedTrack}
              <div class="detail-tabs">
                <button
                  class="detail-tab"
                  class:active={fullDetailTab === 'effects'}
                  onclick={() => (fullDetailTab = 'effects')}
                  type="button"
                >
                  Efectos
                </button>
                <button
                  class="detail-tab"
                  class:active={fullDetailTab === 'eq'}
                  onclick={() => (fullDetailTab = 'eq')}
                  type="button"
                >
                  EQ
                </button>
                <button
                  class="detail-tab"
                  class:active={fullDetailTab === 'routing'}
                  onclick={() => (fullDetailTab = 'routing')}
                  type="button"
                >
                  Routing
                </button>
              </div>

              <div class="detail-content">
                {#if fullDetailTab === 'effects'}
                  <div class="inserts-list">
                    {#each channelInserts[track.id] ?? [null, null, null, null] as effectId, slot (slot)}
                      <div class="insert-slot">
                        <div class="insert-header">
                          <span class="insert-slot-name">Slot {slot + 1}</span>
                          {#if effectId}
                            <button
                              class="btn-remove"
                              onclick={() => removeEffect(track.id, slot)}
                              type="button"
                            >
                              Remove
                            </button>
                          {/if}
                        </div>
                        <select
                          class="effect-select"
                          value={effectId ?? ''}
                          onchange={(e) => assignEffect(track.id, slot, e.currentTarget.value || null)}
                        >
                          <option value="">Empty</option>
                          {#each effects as effect (effect.id)}
                            <option value={effect.id}>{effect.name}</option>
                          {/each}
                        </select>

                        {#if effectId}
                          {@const effect = effects.find((e) => e.id === effectId)}
                          {#if effect}
                            <div class="insert-params">
                              {#each effect.params as param (param.key)}
                                <label class="param-row">
                                  <span class="param-label">{param.label}</span>
                                  <input
                                    type="range"
                                    min={param.min}
                                    max={param.max}
                                    step={param.step}
                                    value={channelInsertValues[track.id]?.[slot]?.[param.key] ?? param.default}
                                    oninput={(e) =>
                                      updateInsertValue(track.id, slot, param.key, parseFloat(e.currentTarget.value))}
                                  />
                                  <span class="param-value">
                                    {channelInsertValues[track.id]?.[slot]?.[param.key] ?? param.default}{param.unit}
                                  </span>
                                </label>
                              {/each}
                              <button
                                class="btn-primary apply-btn"
                                onclick={() => applyInsert(track.id, slot)}
                                disabled={insertLoading[slotKey(track.id, slot)]}
                                type="button"
                              >
                                {insertLoading[slotKey(track.id, slot)] ? 'Aplicando...' : 'Aplicar'}
                              </button>
                              {#if insertResults[slotKey(track.id, slot)]}
                                <div
                                  class="insert-result"
                                  class:error={insertResults[slotKey(track.id, slot)].startsWith('Error')}
                                >
                                  {insertResults[slotKey(track.id, slot)]}
                                </div>
                              {/if}
                            </div>
                          {/if}
                        {/if}
                      </div>
                    {/each}
                  </div>
                {:else if fullDetailTab === 'eq'}
                  <div class="eq-panel">
                    <div class="bands-list">
                      {#each channelEqBands[track.id] ?? [] as band, i (i)}
                        <div class="band-card">
                          <div class="band-header">Banda {i + 1}</div>

                          <label class="field-row">
                            <span>Tipo</span>
                            <select
                              value={band.type}
                              onchange={(e) => updateBandType(track.id, i, e.currentTarget.value as EqFilter['type'])}
                            >
                              {#each filterTypes as t (t)}
                                <option value={t}>{t}</option>
                              {/each}
                            </select>
                          </label>

                          <label class="field-row">
                            <span>Frecuencia</span>
                            <input
                              type="range"
                              min="0"
                              max="1"
                              step="0.001"
                              value={freqToPosition(band.freq)}
                              oninput={(e) => updateBandFreq(track.id, i, parseFloat(e.currentTarget.value))}
                            />
                            <span class="value">{formatFreq(band.freq)} Hz</span>
                          </label>

                          {#if needsGain(band.type)}
                            <label class="field-row">
                              <span>Gain</span>
                              <input
                                type="range"
                                min="-24"
                                max="24"
                                step="0.5"
                                value={band.gain}
                                oninput={(e) => updateBandGain(track.id, i, parseFloat(e.currentTarget.value))}
                              />
                              <span class="value">{band.gain.toFixed(1)} dB</span>
                            </label>
                          {/if}

                          <label class="field-row">
                            <span>Q</span>
                            <input
                              type="range"
                              min="0.1"
                              max="10"
                              step="0.1"
                              value={band.q}
                              oninput={(e) => updateBandQ(track.id, i, parseFloat(e.currentTarget.value))}
                            />
                            <span class="value">{band.q.toFixed(1)}</span>
                          </label>
                        </div>
                      {/each}
                    </div>

                    <button
                      class="btn-primary apply-btn"
                      onclick={applySelectedEq}
                      disabled={!track.fileName || eqLoading}
                      type="button"
                    >
                      {eqLoading ? 'Aplicando EQ...' : 'Aplicar EQ'}
                    </button>

                    {#if eqFiltersApplied > 0}
                      <div class="filters-applied">Filtros aplicados: {eqFiltersApplied}</div>
                    {/if}

                    {#if eqResult}
                      <div class="result" class:error={eqResult.startsWith('Error')}>
                        {eqResult}
                      </div>
                    {/if}
                  </div>
                {:else if fullDetailTab === 'routing'}
                  <div class="routing-panel">
                    <div class="routing-row">
                      <span class="routing-label">Canal</span>
                      <span class="routing-value">{track.name}</span>
                    </div>
                    <div class="routing-row">
                      <span class="routing-label">Origen</span>
                      <span class="routing-value">{track.fileName}</span>
                    </div>
                    <div class="routing-row">
                      <span class="routing-label">Salida</span>
                      <span class="routing-value">Master Bus</span>
                    </div>
                    <div class="routing-row">
                      <span class="routing-label">Volumen</span>
                      <span class="routing-value">{(track.volume * 100).toFixed(0)}%</span>
                    </div>
                    <div class="routing-row">
                      <span class="routing-label">Pan</span>
                      <span class="routing-value">{((channelPans[track.id] ?? 0) * 100).toFixed(0)}</span>
                    </div>
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .daw-workspace {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: 100%;
    height: 100%;
    min-height: 0;
  }

  .workspace-toolbar {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.6rem 0.8rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    flex-shrink: 0;
  }

  .mode-select {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--text-secondary);
    font-size: 0.85rem;
    font-weight: 600;
  }

  .mode-select select {
    padding: 0.4rem 0.6rem;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: var(--bg);
    color: var(--text-primary);
    font-size: 0.85rem;
  }

  .active-file {
    font-size: 0.8rem;
    color: var(--text-primary);
    margin-left: auto;
    max-width: 400px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .active-file.empty {
    color: var(--text-secondary);
  }

  .audio-panel {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .mode-panel {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .basic-grid {
    display: grid;
    grid-template-columns: 2fr 1fr;
    gap: 1rem;
    height: 100%;
    min-height: 0;
  }

  @media (max-width: 1100px) {
    .basic-grid {
      grid-template-columns: 1fr;
      grid-template-rows: 1fr 1fr;
    }
  }

  .medium-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 0.75rem;
  }

  .tab-bar {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.4rem;
    background: #1a1a2e;
    border: 1px solid var(--border);
    border-radius: 10px;
    flex-shrink: 0;
  }

  .tab-button {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0.9rem;
    border: none;
    border-radius: 8px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s ease, color 0.15s ease, box-shadow 0.15s ease;
  }

  .tab-button:hover {
    background: rgba(255, 255, 255, 0.06);
    color: var(--text-primary);
  }

  .tab-button.active {
    background: rgba(255, 255, 255, 0.1);
    color: var(--accent, #7c5cff);
    box-shadow: inset 0 -2px 0 0 var(--accent, #7c5cff);
  }

  .tab-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.1rem;
    height: 1.1rem;
  }

  .tab-icon :global(svg) {
    width: 100%;
    height: 100%;
  }

  .tab-content {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  /* ===== Full mixer mode ===== */
  .full-mixer {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    height: 100%;
    min-height: 0;
    background: #16162a;
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 0.75rem;
    overflow: hidden;
  }

  .mixer-toolbar {
    display: flex;
    align-items: center;
    gap: 1rem;
    flex-shrink: 0;
  }

  .btn-back {
    padding: 0.45rem 0.8rem;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .btn-back:hover {
    background: var(--bg-hover);
    border-color: var(--accent);
  }

  .mixer-title {
    font-size: 0.9rem;
    font-weight: 700;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .mixer-body {
    display: grid;
    grid-template-columns: minmax(220px, 35%) 1fr;
    gap: 0.75rem;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  @media (max-width: 900px) {
    .mixer-body {
      grid-template-columns: 1fr;
      grid-template-rows: 1fr 1fr;
    }
  }

  .mixer-strips {
    display: flex;
    align-items: stretch;
    gap: 0.5rem;
    overflow-x: auto;
    overflow-y: hidden;
    background: #16162a;
    border: 1px solid #2a2a4a;
    border-radius: 10px;
    padding: 0.75rem;
    min-height: 0;
  }

  .mixer-empty {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    color: var(--text-secondary);
    font-size: 0.9rem;
    text-align: center;
    padding: 1rem;
  }

  .channel-strip {
    flex: 0 0 90px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.6rem;
    background: #1e1e38;
    border: 1px solid #2a2a4a;
    border-radius: 10px;
    padding: 0.6rem 0.4rem;
    transition: border-color 0.15s, box-shadow 0.15s;
  }

  .channel-strip.selected {
    border-color: var(--accent, #7c5cff);
    box-shadow: 0 0 0 1px var(--accent, #7c5cff);
  }

  .channel-header {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.2rem;
    width: 100%;
    min-width: 0;
  }

  .channel-number {
    font-size: 0.7rem;
    font-weight: 700;
    color: var(--text-secondary);
  }

  .channel-name {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
    text-align: center;
  }

  .fader-wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.4rem;
    min-height: 0;
  }

  .fader {
    flex: 1;
    min-height: 120px;
    width: 28px;
    accent-color: var(--accent, #7c5cff);
    -webkit-appearance: slider-vertical;
    appearance: slider-vertical;
  }

  .fader-value {
    font-size: 0.7rem;
    color: var(--text-secondary);
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-variant-numeric: tabular-nums;
  }

  .pan-wrap {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.2rem;
    width: 100%;
  }

  .pan-label {
    font-size: 0.65rem;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  .pan {
    width: 100%;
    accent-color: var(--accent, #7c5cff);
  }

  .pan-value {
    font-size: 0.7rem;
    color: var(--text-secondary);
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-variant-numeric: tabular-nums;
  }

  .channel-buttons {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 0.3rem;
    width: 100%;
  }

  .channel-buttons button {
    padding: 0.3rem 0.2rem;
    border-radius: 6px;
    border: 1px solid #2a2a4a;
    background: #16162a;
    color: var(--text-secondary);
    font-size: 0.7rem;
    font-weight: 700;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s, color 0.15s;
  }

  .channel-buttons button:hover {
    border-color: var(--accent, #7c5cff);
    color: var(--text-primary);
  }

  .channel-buttons button.active {
    background: var(--accent, #7c5cff);
    border-color: var(--accent, #7c5cff);
    color: #fff;
  }

  .btn-mute.active {
    background: #e57373 !important;
    border-color: #e57373 !important;
  }

  .btn-solo.active {
    background: #ffb74d !important;
    border-color: #ffb74d !important;
    color: #1a1a2e !important;
  }

  .detail-panel {
    display: flex;
    flex-direction: column;
    background: #1e1e38;
    border: 1px solid #2a2a4a;
    border-radius: 10px;
    overflow: hidden;
    min-height: 0;
  }

  .detail-empty {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 1;
    color: var(--text-secondary);
    font-size: 0.9rem;
    padding: 1rem;
  }

  .detail-tabs {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.4rem;
    background: #16162a;
    border-bottom: 1px solid #2a2a4a;
    flex-shrink: 0;
  }

  .detail-tab {
    padding: 0.5rem 0.9rem;
    border: none;
    border-radius: 8px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
  }

  .detail-tab:hover {
    background: rgba(255, 255, 255, 0.06);
    color: var(--text-primary);
  }

  .detail-tab.active {
    background: rgba(255, 255, 255, 0.1);
    color: var(--accent, #7c5cff);
  }

  .detail-content {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 1rem;
  }

  .inserts-list {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .insert-slot {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.75rem;
    background: #16162a;
    border: 1px solid #2a2a4a;
    border-radius: 10px;
  }

  .insert-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }

  .insert-slot-name {
    font-size: 0.8rem;
    font-weight: 700;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  .btn-remove {
    padding: 0.25rem 0.5rem;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-secondary);
    font-size: 0.75rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
  }

  .btn-remove:hover {
    background: rgba(229, 115, 115, 0.15);
    color: #e57373;
    border-color: #e57373;
  }

  .effect-select {
    padding: 0.45rem 0.6rem;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: var(--bg);
    color: var(--text-primary);
    font-size: 0.85rem;
  }

  .insert-params {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .param-row {
    display: grid;
    grid-template-columns: 90px 1fr 60px;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  .param-label {
    font-weight: 600;
  }

  .param-row input[type='range'] {
    width: 100%;
    accent-color: var(--accent);
  }

  .param-value {
    text-align: right;
    color: var(--text-primary);
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-variant-numeric: tabular-nums;
  }

  .btn-primary {
    padding: 0.5rem 0.9rem;
    border-radius: 8px;
    border: 1px solid var(--accent);
    background: var(--accent);
    color: #fff;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .btn-primary:hover:not(:disabled) {
    background: var(--accent-dark);
  }

  .btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .apply-btn {
    align-self: flex-start;
  }

  .insert-result,
  .result {
    font-size: 0.8rem;
    color: var(--accent-light);
    word-break: break-all;
  }

  .insert-result.error,
  .result.error {
    color: #e57373;
  }

  .eq-panel {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .bands-list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 1rem;
  }

  .band-card {
    border: 1px solid var(--border);
    border-radius: 10px;
    background: #16162a;
    padding: 0.75rem 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }

  .band-header {
    font-weight: 600;
    color: var(--text-primary);
    font-size: 0.9rem;
  }

  .field-row {
    display: grid;
    grid-template-columns: 70px 1fr 70px;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  .field-row span:first-child {
    font-weight: 600;
  }

  .field-row input[type='range'] {
    width: 100%;
    accent-color: var(--accent);
  }

  .field-row select {
    grid-column: span 2;
    padding: 0.35rem 0.5rem;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--bg);
    color: var(--text-primary);
    font-size: 0.85rem;
  }

  .value {
    text-align: right;
    color: var(--text-primary);
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-variant-numeric: tabular-nums;
  }

  .filters-applied {
    font-size: 0.85rem;
    color: var(--accent-light);
    font-weight: 600;
  }

  .routing-panel {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .routing-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.75rem 1rem;
    background: #16162a;
    border: 1px solid #2a2a4a;
    border-radius: 8px;
  }

  .routing-label {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-secondary);
  }

  .routing-value {
    font-size: 0.85rem;
    color: var(--text-primary);
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    word-break: break-all;
    text-align: right;
  }
</style>
