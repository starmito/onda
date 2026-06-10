<script>
	import { IconTone, IconBPM, IconDAW, IconHelp } from './icons';

	/**
	 * @type {{ tabId: 'pitch' | 'bpm' | 'daw' | 'help' }}
	 */
	let { tabId } = $props();

	const content = {
		pitch: { icon: IconTone, title: 'Cambiar Tono' },
		bpm: { icon: IconBPM, title: 'Detectar velocidad' },
		daw: { icon: IconDAW, title: 'DAW' },
		help: { icon: IconHelp, title: 'Ayuda' }
	};

	const current = $derived(content[tabId] ?? content.help);
</script>

<div class="placeholder">
	<span class="icon">{@html current.icon}</span>
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
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.icon :global(svg) {
		width: 64px;
		height: 64px;
	}

	.title {
		font-size: 2rem;
		font-weight: 600;
		color: #ffffff;
		margin: 0.75rem 0 0.5rem;
	}

	.subtitle {
		font-size: 1rem;
		color: var(--text-secondary);
		margin: 0;
	}
</style>
