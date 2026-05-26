<script lang="ts">
	let { onfile }: { onfile: (file: File) => void } = $props();

	let dragging = $state(false);
	let fileName = $state<string | null>(null);
	let fileInput: HTMLInputElement;

	function handleDragOver(e: DragEvent) {
		e.preventDefault();
		if (e.dataTransfer) {
			e.dataTransfer.dropEffect = "copy";
		}
		dragging = true;
	}

	function handleDragLeave(e: DragEvent) {
		e.preventDefault();
		dragging = false;
	}

	function handleDrop(e: DragEvent) {
		e.preventDefault();
		dragging = false;

		const files = e.dataTransfer?.files;
		if (!files || files.length === 0) return;

		const file = files[0];
		if (isAudioFile(file)) {
			fileName = file.name;
			onfile(file);
		}
	}

	function handleClick() {
		fileInput.click();
	}

	function handleFileChange(e: Event) {
		const target = e.target as HTMLInputElement;
		const files = target.files;
		if (!files || files.length === 0) return;

		const file = files[0];
		if (isAudioFile(file)) {
			fileName = file.name;
			onfile(file);
		}
	}

	function isAudioFile(file: File): boolean {
		const audioExtensions = [".flac", ".wav", ".mp3", ".ogg", ".m4a", ".aiff", ".wma"];
		const name = file.name.toLowerCase();
		return audioExtensions.some((ext) => name.endsWith(ext));
	}
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
	class="dropzone"
	class:dragging
	role="button"
	tabindex="0"
	ondragover={handleDragOver}
	ondragleave={handleDragLeave}
	ondrop={handleDrop}
	onclick={handleClick}
	onkeydown={(e) => {
		if (e.key === "Enter" || e.key === " ") {
			e.preventDefault();
			handleClick();
		}
	}}
>
	<p class="dropzone-text">
		Arrastra un archivo de audio o haz clic para seleccionar
	</p>
	<p class="dropzone-hint">.flac .wav .mp3 .ogg .m4a .aiff .wma</p>

	<input
		bind:this={fileInput}
		type="file"
		accept=".flac,.wav,.mp3,.ogg,.m4a,.aiff,.wma"
		class="hidden-input"
		onchange={handleFileChange}
	/>
</div>

{#if fileName}
	<p class="file-name">
		🎵 {fileName}
	</p>
{/if}

<style>
	.dropzone {
		border: 2px dashed #444;
		border-radius: 12px;
		padding: 2rem;
		min-height: 200px;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.5rem;
		cursor: pointer;
		transition: border-color 0.3s ease, background-color 0.3s ease;
		background-color: #1a1a2e;
		color: #e0e0e0;
		user-select: none;
	}

	.dropzone:hover {
		border-color: #666;
	}

	.dropzone.dragging {
		border-color: #00d4ff;
		background-color: rgba(0, 212, 255, 0.08);
		box-shadow: 0 0 20px rgba(0, 212, 255, 0.15);
	}

	.dropzone-text {
		margin: 0;
		font-size: 1.1rem;
		font-weight: 500;
		text-align: center;
	}

	.dropzone-hint {
		margin: 0;
		font-size: 0.85rem;
		color: #888;
		text-align: center;
	}

	.hidden-input {
		display: none;
	}

	.file-name {
		margin: 0.75rem 0 0 0;
		font-size: 0.9rem;
		color: #00d4ff;
		text-align: center;
		word-break: break-all;
	}
</style>
