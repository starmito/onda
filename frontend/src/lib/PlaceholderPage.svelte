<script>
	/**
	 * @type {{ tabId: 'pitch' | 'bpm' | 'daw' | 'help' }}
	 */
	let { tabId } = $props();

	const content = {
		pitch: { icon: '🎛', title: 'Cambiar Tono' },
		bpm: { icon: '📊', title: 'Detectar velocidad' },
		daw: { icon: '🎼', title: 'DAW' },
		help: { icon: '❓', title: 'Ayuda' }
	};

	const current = $derived(content[tabId] ?? content.help);
</script>

<div class="placeholder">
	<span class="icon">{current.icon}</span>
	<h2 class="title">{current.title}</h2>
	<p class="subtitle">Próximamente…</p>
</div>

<style>
	.placeholder {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		height: 100%;
		width: 100%;
		background: transparent;
		text-align: center;
		padding: 2rem;
		box-sizing: border-box;
	}

	.icon {
		font-size: 4rem;
		line-height: 1.2;
	}

	.title {
		font-size: 2rem;
		font-weight: 600;
		color: #ffffff;
		margin: 0.75rem 0 0.5rem;
	}

	.subtitle {
		font-size: 1rem;
		color: #999999;
		margin: 0;
	}
</style>
