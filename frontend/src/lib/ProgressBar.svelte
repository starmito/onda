<script lang="ts">
  let {
    status = 'idle',
    step = '',
    model = '',
    progress = 0,
    error = '',
  }: {
    status?: string;
    step?: string;
    model?: string;
    progress?: number;
    error?: string;
  } = $props();
</script>

{#if status !== 'idle'}
  <div class="progress-bar-container">
    <div class="progress-bar-track">
      <div
        class="progress-bar-fill"
        style="width: {progress * 100}%"
      ></div>
    </div>

    {#if status === 'running'}
      <p class="progress-text">
        {Math.round(progress * 100)}% — {step ? `separando ${step}` : 'procesando'}{model ? ` (${model})` : ''}
      </p>
    {/if}

    {#if status === 'done'}
      <p class="progress-text progress-done">✅ Completado</p>
    {/if}

    {#if status === 'error'}
      <p class="progress-text progress-error">❌ {error || 'Error desconocido'}</p>
    {/if}
  </div>
{/if}

<style>
  .progress-bar-container {
    width: 100%;
    padding: 1rem 0;
  }

  .progress-bar-track {
    width: 100%;
    height: 8px;
    background-color: #2a2a3e;
    border-radius: 4px;
    overflow: hidden;
  }

  .progress-bar-fill {
    height: 100%;
    background-color: #00d4ff;
    border-radius: 4px;
    transition: width 0.3s ease;
  }

  .progress-text {
    margin: 0.5rem 0 0 0;
    font-size: 0.9rem;
    color: #ccc;
    text-align: center;
  }

  .progress-done {
    color: #4caf50;
  }

  .progress-error {
    color: #f44336;
  }
</style>
