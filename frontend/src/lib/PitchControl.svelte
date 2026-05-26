<script lang="ts">
  let {
    value = 0,
    disabled = false,
    onapply,
  }: {
    value?: number;
    disabled?: boolean;
    onapply?: (pitch: number) => void;
  } = $props();

  let pitch = $state(0);

  // Sync when external value changes and init
  $effect(() => {
    pitch = value;
  });

  function formatPitch(v: number): string {
    if (v === 0) return '0 st';
    return v > 0 ? `+${v} st` : `${v} st`;
  }

  function handleApply() {
    onapply?.(pitch);
  }
</script>

<div class="pitch-control">
  <label class="pitch-label" for="pitch-slider">
    🎹 Pitch: <strong>{formatPitch(pitch)}</strong>
  </label>
  <input
    id="pitch-slider"
    type="range"
    min="-12"
    max="12"
    step="1"
    bind:value={pitch}
    disabled={disabled}
    class="pitch-slider"
  />
  <div class="pitch-range-labels">
    <span>-12</span>
    <span>0</span>
    <span>+12</span>
  </div>
  <button
    class="apply-btn"
    disabled={disabled || pitch === value}
    onclick={handleApply}
  >
    Apply Pitch
  </button>
</div>

<style>
  .pitch-control {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    background: #1a1a2e;
    border-radius: 8px;
  }

  .pitch-label {
    font-size: 0.9rem;
    color: #ccc;
  }
  .pitch-label strong {
    color: #00d4ff;
    font-weight: 600;
  }

  .pitch-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 6px;
    border-radius: 3px;
    background: linear-gradient(to right, #b388ff, #00d4ff, #b388ff);
    outline: none;
    cursor: pointer;
  }
  .pitch-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #00d4ff;
    border: 2px solid #0a0a14;
    cursor: pointer;
    box-shadow: 0 0 8px rgba(0, 212, 255, 0.4);
  }
  .pitch-slider:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .pitch-range-labels {
    display: flex;
    justify-content: space-between;
    font-size: 0.7rem;
    color: #666;
    margin-top: -0.3rem;
  }

  .apply-btn {
    padding: 0.4rem 1rem;
    background: #2a2a3e;
    color: #00d4ff;
    border: 1px solid #444;
    border-radius: 6px;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s, border-color 0.2s;
    align-self: flex-end;
  }
  .apply-btn:hover:not(:disabled) {
    background: #333355;
    border-color: #00d4ff;
  }
  .apply-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
</style>
