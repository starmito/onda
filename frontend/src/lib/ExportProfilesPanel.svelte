<script lang="ts">
  import {
    getExportProfiles,
    saveExportProfiles,
    type AudioExportProfiles,
  } from './api';

  const wavBitDepths = ['16', '24', '32f'];
  const flacBitDepths = ['16', '24'];
  const mp3Bitrates = ['128k', '192k', '320k'];

  const defaultProfiles: AudioExportProfiles = {
    defaultFormat: 'flac',
    nameTemplate: '{song} ({pitches}) ({suffix})',
    formats: {
      wav: { bitDepth: '32f', sampleRate: 'source' },
      flac: { compression: 5, bitDepth: '24' },
      mp3: { bitrate: '320k', mode: 'cbr' },
    },
  };

  let profiles = $state<AudioExportProfiles>(structuredClone(defaultProfiles));
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);

  function normalizeProfiles(p: AudioExportProfiles): AudioExportProfiles {
    return {
      defaultFormat: p.defaultFormat || defaultProfiles.defaultFormat,
      nameTemplate: p.nameTemplate || defaultProfiles.nameTemplate,
      formats: {
        wav: { ...defaultProfiles.formats.wav, ...(p.formats?.wav || {}) },
        flac: { ...defaultProfiles.formats.flac, ...(p.formats?.flac || {}) },
        mp3: { ...defaultProfiles.formats.mp3, ...(p.formats?.mp3 || {}) },
      },
    };
  }

  $effect(() => {
    getExportProfiles()
      .then((p) => {
        profiles = normalizeProfiles(p);
      })
      .catch(() => {
        profiles = structuredClone(defaultProfiles);
      })
      .finally(() => {
        loading = false;
      });
  });

  function wavBitDepthIndex(): number {
    const idx = wavBitDepths.indexOf(profiles.formats.wav.bitDepth || '32f');
    return idx >= 0 ? idx : wavBitDepths.length - 1;
  }

  function flacBitDepthIndex(): number {
    const idx = flacBitDepths.indexOf(profiles.formats.flac.bitDepth || '24');
    return idx >= 0 ? idx : flacBitDepths.length - 1;
  }

  function mp3BitrateIndex(): number {
    const idx = mp3Bitrates.indexOf(profiles.formats.mp3.bitrate || '320k');
    return idx >= 0 ? idx : mp3Bitrates.length - 1;
  }

  async function handleSave() {
    saving = true;
    saved = false;
    try {
      await saveExportProfiles(profiles);
      saved = true;
      setTimeout(() => {
        saved = false;
      }, 2500);
    } finally {
      saving = false;
    }
  }
</script>

<div class="export-profiles-panel">
  <h2>Perfiles de salida</h2>

  {#if loading}
    <p class="loading-text">Cargando perfiles…</p>
  {:else}
    <div class="field">
      <span class="field-label">Formato predeterminado</span>
      <div class="format-selector">
        {#each ['wav', 'flac', 'mp3'] as fmt}
          <label class="radio-label">
            <input
              type="radio"
              name="defaultFormat"
              value={fmt}
              bind:group={profiles.defaultFormat}
            />
            <span>{fmt.toUpperCase()}</span>
          </label>
        {/each}
      </div>
    </div>

    <div class="field">
      <label class="field-label" for="name-template">Plantilla de nombre</label>
      <input
        id="name-template"
        class="name-template-input"
        type="text"
        bind:value={profiles.nameTemplate}
        placeholder="{song} ({pitches}) ({suffix})"
      />
      <p class="param-desc">
        Variables: <code>{'{song}'}</code>, <code>{'{pitches}'}</code>,
        <code>{'{suffix}'}</code>, <code>{'{format}'}</code>,
        <code>{'{date}'}</code>, <code>{'{time}'}</code>.
      </p>
    </div>

    <!-- WAV -->
    <section class="format-section">
      <h3>WAV</h3>
      <div class="quality-scale">
        <div class="quality-scale-title">Calidad / Profundidad de bits</div>
        <div class="quality-scale-bar">
          <span class="quality-scale-min">↓ Menos calidad</span>
          <span class="quality-scale-max">↑ Más calidad</span>
        </div>
        <div class="quality-scale-note">
          A la derecha: más cabeza dinámica. 32f evita recorte al sumar stems.
        </div>
      </div>

      <div class="field">
        <label for="wav-bitdepth">
          Profundidad: <strong>{profiles.formats.wav.bitDepth}-bit</strong>
        </label>
        <input
          id="wav-bitdepth"
          type="range"
          min="0"
          max={wavBitDepths.length - 1}
          step="1"
          value={wavBitDepthIndex()}
          oninput={(e) => {
            profiles.formats.wav.bitDepth = wavBitDepths[Number(e.currentTarget.value)];
          }}
        />
        <p class="param-desc">
          16 bit = CD; 24 bit = estudio; 32f = punto flotante, máxima cabeza
          dinámica sin saturar.
        </p>
        <div class="slider-labels">
          <span class="slider-min">16 — ⚡ Ligero / -Calidad</span>
          <span class="slider-max">32f — 🎵 Máxima cabeza / +Calidad</span>
        </div>
      </div>

      <div class="field">
        <span class="field-label">Sample rate</span>
        <p class="param-desc">
          Mantener "source" conserva el sample rate original. Subir los Hz NO
          añade calidad; mantén el de la fuente.
        </p>
        <span class="readonly-value">source</span>
      </div>
    </section>

    <!-- FLAC -->
    <section class="format-section">
      <h3>FLAC</h3>
      <div class="quality-scale">
        <div class="quality-scale-title">Compresión / Tamaño</div>
        <div class="quality-scale-bar">
          <span class="quality-scale-min">↓ Más grande</span>
          <span class="quality-scale-max">↑ Más pequeño</span>
        </div>
        <div class="quality-scale-note">
          ⚙️ =Calidad · derecha = más pequeño/lento (FLAC es lossless).
        </div>
      </div>

      <div class="field">
        <label for="flac-compression">
          Compresión: <strong>{profiles.formats.flac.compression}</strong>
        </label>
        <input
          id="flac-compression"
          type="range"
          min="0"
          max="8"
          step="1"
          bind:value={profiles.formats.flac.compression}
        />
        <p class="param-desc">
          0 = compresión mínima / archivo grande; 8 = máxima compresión /
          archivo pequeño. No altera la calidad, solo el tiempo de codificación.
        </p>
        <div class="slider-labels">
          <span class="slider-min">0 — ⚡ Rápido / +Tamaño / =Calidad</span>
          <span class="slider-max">8 — 🐌 Lento / -Tamaño / =Calidad</span>
        </div>
      </div>

      <div class="field">
        <label for="flac-bitdepth">
          Profundidad: <strong>{profiles.formats.flac.bitDepth}-bit</strong>
        </label>
        <input
          id="flac-bitdepth"
          type="range"
          min="0"
          max={flacBitDepths.length - 1}
          step="1"
          value={flacBitDepthIndex()}
          oninput={(e) => {
            profiles.formats.flac.bitDepth = flacBitDepths[Number(e.currentTarget.value)];
          }}
        />
        <p class="param-desc">
          24 bit conserva el material grabado a 24 bit. 16 bit es compatible con
          más reproductores pero descarta información.
        </p>
        <div class="slider-labels">
          <span class="slider-min">16 — ⚡ Compatible / -Calidad</span>
          <span class="slider-max">24 — 🎵 Estudio / +Calidad</span>
        </div>
      </div>
    </section>

    <!-- MP3 -->
    <section class="format-section">
      <h3>MP3</h3>
      <div class="quality-scale">
        <div class="quality-scale-title">Calidad / Transparencia</div>
        <div class="quality-scale-bar">
          <span class="quality-scale-min">↓ Menos calidad</span>
          <span class="quality-scale-max">↑ Más calidad</span>
        </div>
        <div class="quality-scale-note">
          A la derecha: más calidad. 320k CBR = máxima transparencia.
        </div>
      </div>

      <div class="field">
        <label for="mp3-bitrate">
          Bitrate: <strong>{profiles.formats.mp3.bitrate}</strong>
        </label>
        <input
          id="mp3-bitrate"
          type="range"
          min="0"
          max={mp3Bitrates.length - 1}
          step="1"
          value={mp3BitrateIndex()}
          oninput={(e) => {
            profiles.formats.mp3.bitrate = mp3Bitrates[Number(e.currentTarget.value)];
          }}
        />
        <p class="param-desc">
          128k = compresión audible; 192k = buen equilibrio; 320k = prácticamente
          transparente.
        </p>
        <div class="slider-labels">
          <span class="slider-min">128k — ⚡ Pequeño / -Calidad</span>
          <span class="slider-max">320k — 🎵 Transparente / +Calidad</span>
        </div>
      </div>

      <div class="field">
        <label for="mp3-mode">Modo</label>
        <select id="mp3-mode" bind:value={profiles.formats.mp3.mode}>
          <option value="cbr">CBR (Constant Bitrate)</option>
          <option value="vbr">VBR (Variable Bitrate)</option>
        </select>
        <p class="param-desc">
          CBR a 320k ofrece transparencia máxima y tamaño predecible. VBR ajusta
          el bitrate por frame.
        </p>
      </div>
    </section>

    <button class="btn-apply" onclick={handleSave} disabled={saving}>
      {saving ? 'Guardando…' : 'Guardar por defecto'}
    </button>
    {#if saved}
      <p class="feedback success">Perfiles guardados correctamente.</p>
    {/if}
  {/if}
</div>

<style>
  .export-profiles-panel {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
    padding: 1rem;
    max-width: 700px;
  }

  h2 {
    margin: 0;
    font-size: 1.1rem;
    color: var(--text-primary);
  }

  h3 {
    margin: 0;
    font-size: 0.95rem;
    color: var(--accent-light);
  }

  .format-section {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .field label,
  .field-label {
    font-size: 0.85rem;
    color: var(--text-primary);
  }

  .field label strong {
    color: var(--accent-light);
  }

  .field input[type='range'] {
    width: 100%;
    accent-color: var(--accent);
    height: 6px;
  }

  .field select {
    padding: 0.4rem 0.6rem;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 0.85rem;
    outline: none;
    cursor: pointer;
    width: 100%;
  }

  .field select:focus {
    border-color: var(--accent);
  }

  .name-template-input {
    padding: 0.4rem 0.6rem;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 0.85rem;
    outline: none;
    width: 100%;
    box-sizing: border-box;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }
  .name-template-input:focus {
    border-color: var(--accent);
  }

  .param-desc {
    font-size: 0.75rem;
    color: var(--text-secondary);
    margin: 2px 0 4px;
  }

  .slider-labels {
    display: flex;
    justify-content: space-between;
    font-size: 0.7rem;
    color: var(--text-muted);
  }

  .slider-min,
  .slider-max {
    color: var(--text-muted);
    font-size: 0.65rem;
  }

  .quality-scale {
    margin-bottom: 1rem;
    padding: 0.75rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  .quality-scale-title {
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: 0.4rem;
  }

  .quality-scale-bar {
    position: relative;
    height: 10px;
    border-radius: 5px;
    background: linear-gradient(90deg, #2a2a4a 0%, var(--accent) 50%, #81c784 100%);
    margin-bottom: 0.3rem;
  }

  .quality-scale-min,
  .quality-scale-max {
    position: absolute;
    top: 14px;
    font-size: 0.65rem;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .quality-scale-min {
    left: 0;
  }

  .quality-scale-max {
    right: 0;
  }

  .quality-scale-note {
    font-size: 0.7rem;
    color: var(--text-secondary);
    padding-top: 1.1rem;
  }

  .format-selector {
    display: flex;
    gap: 1rem;
  }

  .radio-label {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    font-size: 0.85rem;
    color: var(--text-primary);
    cursor: pointer;
  }

  .readonly-value {
    color: var(--accent-light);
    font-weight: 600;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 0.85rem;
  }

  .btn-apply {
    padding: 0.6rem 1rem;
    background: linear-gradient(135deg, var(--accent), var(--accent-light));
    border: none;
    border-radius: 8px;
    color: var(--text-primary);
    font-weight: 700;
    font-size: 0.9rem;
    cursor: pointer;
    transition: opacity 0.15s;
  }

  .btn-apply:hover {
    opacity: 0.9;
  }

  .btn-apply:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .feedback {
    text-align: center;
    font-size: 0.85rem;
    font-weight: 600;
    padding: 0.5rem;
    border-radius: 6px;
  }

  .feedback.success {
    background: #1b3a1b;
    color: #81c784;
  }

  .loading-text {
    color: var(--text-secondary);
    font-size: 0.9rem;
  }
</style>
