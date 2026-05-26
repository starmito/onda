<script lang="ts">
	let { files }: { files: { name: string; path: string }[] } = $props();

	let playingIndex = $state<number | null>(null);

	function handlePlay(index: number) {
		playingIndex = playingIndex === index ? null : index;
	}
</script>

{#if files.length > 0}
	<div class="results">
		<h2 class="results-title">Resultados</h2>

		<div class="results-list">
			{#each files as file, i}
				<div class="result-card" class:playing={playingIndex === i}>
					<div class="result-info">
						<span class="result-name" title={file.name}>{file.name}</span>

						<div class="result-actions">
							<button
								class="play-button"
								onclick={() => handlePlay(i)}
								aria-label={playingIndex === i ? 'Pausar' : 'Reproducir'}
							>
								{playingIndex === i ? '⏸' : '▶'}
							</button>

							<a
								class="download-link"
								href={file.path}
								download={file.name}
								aria-label={`Descargar ${file.name}`}
							>
								⬇
							</a>
						</div>
					</div>

					{#if playingIndex === i}
						<div class="audio-wrapper">
							<audio
								class="audio-player"
								controls
								src={file.path}
								autoplay
							></audio>
						</div>
					{/if}
				</div>
			{/each}
		</div>
	</div>
{/if}

<style>
	.results {
		width: 100%;
		margin-top: 1.5rem;
	}

	.results-title {
		margin: 0 0 0.75rem 0;
		font-size: 1.25rem;
		font-weight: 600;
		color: #e0e0e0;
	}

	.results-list {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}

	.result-card {
		background-color: #1a1a2e;
		border-radius: 8px;
		padding: 0.75rem 1rem;
		transition: background-color 0.2s ease, box-shadow 0.2s ease;
		border: 1px solid transparent;
	}

	.result-card:hover {
		background-color: #22223a;
		box-shadow: 0 2px 12px rgba(0, 0, 0, 0.3);
	}

	.result-card.playing {
		border-color: #00d4ff;
		box-shadow: 0 0 12px rgba(0, 212, 255, 0.15);
	}

	.result-info {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
	}

	.result-name {
		flex: 1;
		color: #e0e0e0;
		font-size: 0.95rem;
		font-weight: 500;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.result-actions {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-shrink: 0;
	}

	.play-button {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		border: 1px solid #444;
		border-radius: 6px;
		background-color: #2a2a3e;
		color: #00d4ff;
		font-size: 1rem;
		cursor: pointer;
		transition: background-color 0.2s ease, border-color 0.2s ease;
		padding: 0;
	}

	.play-button:hover {
		background-color: #333355;
		border-color: #00d4ff;
	}

	.download-link {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		border: 1px solid #444;
		border-radius: 6px;
		background-color: #2a2a3e;
		color: #888;
		font-size: 1rem;
		text-decoration: none;
		transition: background-color 0.2s ease, border-color 0.2s ease, color 0.2s ease;
	}

	.download-link:hover {
		background-color: #333355;
		border-color: #666;
		color: #00d4ff;
	}

	.audio-wrapper {
		margin-top: 0.75rem;
		padding-top: 0.75rem;
		border-top: 1px solid #2a2a3e;
	}

	.audio-player {
		width: 100%;
		height: 36px;
		border-radius: 4px;
	}

	/* Style the native audio controls for dark theme */
	.audio-player::-webkit-media-controls-panel {
		background-color: #2a2a3e;
	}

	.audio-player::-webkit-media-controls-current-time-display,
	.audio-player::-webkit-media-controls-time-remaining-display {
		color: #e0e0e0;
	}
</style>
