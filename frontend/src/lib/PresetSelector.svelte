<script lang="ts">
	let {
		presets,
		disabled,
		onseparate,
		modelsError = false,
	}: {
		presets: Record<string, { name: string; description: string }>;
		disabled: boolean;
		onseparate: (preset: string) => void | Promise<void>;
		modelsError?: boolean;
	} = $props();

	let selectedPreset = $state("");

	$effect(() => {
		// Reset selection when presets change
		if (presets && Object.keys(presets).length > 0 && !presets[selectedPreset]) {
			selectedPreset = "";
		}
	});

	function handleSeparate() {
		if (selectedPreset && !disabled) {
			onseparate(selectedPreset);
		}
	}
</script>

<div class="preset-selector">
	{#if modelsError}
		<p class="loading-text error-text">⚠️ No se pudieron cargar los presets (API no disponible)</p>
	{:else if !presets || Object.keys(presets).length === 0}
		<p class="loading-text">Cargando presets...</p>
	{:else}
		<label class="preset-label" for="preset-select">Preset:</label>
		<select
			id="preset-select"
			class="preset-dropdown"
			bind:value={selectedPreset}
			disabled={disabled}
		>
			<option value="" disabled>-- Seleccionar preset --</option>
			{#each Object.entries(presets) as [key, preset]}
				<option value={key}>
					{preset.name} — {preset.description}
				</option>
			{/each}
		</select>

		<button
			class="separate-button"
			disabled={disabled || !selectedPreset}
			onclick={handleSeparate}
		>
			▶ START
		</button>
	{/if}
</div>

<style>
	.preset-selector {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 1rem;
		background-color: #1a1a2e;
		border-radius: 8px;
	}

	.loading-text {
		margin: 0;
		color: #888;
		font-size: 0.95rem;
		text-align: center;
		width: 100%;
	}

	.error-text {
		color: #f4a236;
	}

	.preset-label {
		color: #e0e0e0;
		font-size: 0.95rem;
		font-weight: 500;
		white-space: nowrap;
	}

	.preset-dropdown {
		flex: 1;
		padding: 0.5rem 0.75rem;
		background-color: #111;
		color: #e0e0e0;
		border: 1px solid #444;
		border-radius: 6px;
		font-size: 0.9rem;
		cursor: pointer;
		outline: none;
		transition: border-color 0.2s ease;
	}

	.preset-dropdown:hover:not(:disabled) {
		border-color: #666;
	}

	.preset-dropdown:focus {
		border-color: #00d4ff;
		box-shadow: 0 0 8px rgba(0, 212, 255, 0.15);
	}

	.preset-dropdown:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.separate-button {
		padding: 0.5rem 1.5rem;
		background-color: #00d4ff;
		color: #111;
		border: none;
		border-radius: 6px;
		font-size: 0.95rem;
		font-weight: 600;
		cursor: pointer;
		transition: background-color 0.2s ease, opacity 0.2s ease;
		white-space: nowrap;
	}

	.separate-button:hover:not(:disabled) {
		background-color: #00b8e0;
	}

	.separate-button:disabled {
		background-color: #444;
		color: #888;
		cursor: not-allowed;
		opacity: 0.7;
	}
</style>
