<script>
	import { IconMenu, IconStar, IconMusic, IconTone, IconBPM, IconDAW, IconHelp, IconSettings } from './icons';

	/**
	 * Sidebar.svelte — Sidebar vertical colapsable al estilo vocalremover.org
	 *
	 * Props (Svelte 5 $props):
	 *   activeTab  : string                  — id del tab activo
	 *   presetTabs : {id, name, icon}[]      — presets del usuario
	 *   collapsed  : boolean                 — colapsado/expandido
	 *   ontoggle   : () => void              — colapsar/expandir
	 *   ontabchange: (tabId: string) => void — cambiar tab activo
	 */
	let {
		activeTab = '',
		presetTabs = [],
		collapsed = false,
		ontoggle = () => {},
		ontabchange = (tabId) => {},
	} = $props();

	/** Items estáticos: [id, label, icon] */
	const staticItems = [
		{ id: 'pitch',    label: 'Cambiar Tono',        icon: IconTone },
		{ id: 'bpm',      label: 'Detectar velocidad',  icon: IconBPM },
		{ id: 'daw',      label: 'DAW',                  icon: IconDAW },
	];

	const bottomItems = [
		{ id: 'help',     label: 'Ayuda',               icon: IconHelp },
		{ id: 'settings', label: 'Ajustes',             icon: IconSettings },
	];

	function handleClick(tabId) {
		ontabchange(tabId);
	}
</script>

<aside class="sidebar" class:collapsed>
	<!-- Botón toggle arriba del todo -->
	<button class="toggle-btn" onclick={ontoggle} aria-label={collapsed ? 'Expandir sidebar' : 'Colapsar sidebar'}>
		<span class="icon-only">{@html IconMenu}</span>
		<span class="label-text">Menú</span>
	</button>

	<!-- Presets dinámicos -->
	{#each presetTabs as tab (tab.id)}
		<button
			class="nav-item"
			class:active={activeTab === tab.id}
			onclick={() => handleClick(tab.id)}
		>
			<span class="icon-only">{@html tab.icon}</span>
			<span class="label-text">{tab.name}</span>
		</button>
	{/each}

	<!-- Separador -->
	<div class="separator" role="separator"></div>

	<!-- Items estáticos (middle group) -->
	{#each staticItems as item (item.id)}
		<button
			class="nav-item"
			class:active={activeTab === item.id}
			onclick={() => handleClick(item.id)}
		>
			<span class="icon-only">{@html item.icon}</span>
			<span class="label-text">{item.label}</span>
		</button>
	{/each}

	<!-- Spacer para empujar bottomItems al fondo -->
	<div class="spacer"></div>

	<!-- Separador -->
	<div class="separator" role="separator"></div>

	<!-- Items del fondo -->
	{#each bottomItems as item (item.id)}
		<button
			class="nav-item"
			class:active={activeTab === item.id}
			onclick={() => handleClick(item.id)}
		>
			<span class="icon-only">{@html item.icon}</span>
			<span class="label-text">{item.label}</span>
		</button>
	{/each}

	<!-- Selector de idioma (placeholder) -->
	<div class="lang-selector">
		<span class="icon-only">🌐</span>
		<span class="label-text">ES</span>
	</div>
</aside>

<style>
	.sidebar {
		display: flex;
		flex-direction: column;
		width: 200px;
		height: 100%;
		background: #1e1e2a;
		overflow-x: hidden;
		overflow-y: auto;
		transition: width 0.2s ease;
		flex-shrink: 0;
	}

	.sidebar.collapsed {
		width: 58px;
	}

	/* ---------- Botón toggle ---------- */
	.toggle-btn {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 4px;
		width: 100%;
		padding: 16px 4px 12px;
		background: none;
		border: none;
		color: #888;
		font-size: 22px;
		cursor: pointer;
		transition: background 0.15s;
		font-family: 'Segoe UI', system-ui, -apple-system, sans-serif;
		font-weight: 600;
		letter-spacing: 0.03em;
	}

	.toggle-btn:hover {
		background: #2a2a3e;
	}

	/* ---------- Items de navegación ---------- */
	.nav-item {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 4px;
		width: 100%;
		padding: 12px 4px;
		background: none;
		border: none;
		border-left: none;
		border-bottom: 2px solid transparent;
		color: #888;
		font-size: 11px;
		cursor: pointer;
		transition: background 0.15s, color 0.15s, border-color 0.15s;
		white-space: nowrap;
		text-align: center;
		box-sizing: border-box;
		font-family: 'Segoe UI', system-ui, -apple-system, sans-serif;
		font-weight: 500;
		letter-spacing: 0.02em;
	}

	.nav-item:hover {
		background: rgba(108, 92, 231, 0.08);
	}

	.nav-item.active {
		background: rgba(108, 92, 231, 0.12);
		border-bottom-color: #6c5ce7;
		color: #e0e0e0;
	}

	/* ---------- SVG icon sizing ---------- */
	.icon-only :global(svg) {
		width: 22px;
		height: 22px;
		display: block;
	}

	/* ---------- Icono y texto ---------- */
	.icon-only {
		font-size: 20px;
		line-height: 1;
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.label-text {
		font-size: 10px;
		line-height: 1.2;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 100%;
		white-space: nowrap;
		font-family: 'Segoe UI', system-ui, -apple-system, sans-serif;
		font-weight: 500;
		letter-spacing: 0.02em;
	}

	/* Cuando colapsado: ocultar texto */
	.collapsed .label-text {
		display: none;
	}

	.collapsed .nav-item {
		padding: 10px 0;
	}

	.collapsed .toggle-btn {
		justify-content: center;
		padding: 14px 0;
	}

	.collapsed .lang-selector {
		justify-content: center;
		padding: 10px 0;
	}

	/* ---------- Separador ---------- */
	.separator {
		height: 1px;
		background: #2a2a3e;
		margin: 4px 16px;
		flex-shrink: 0;
	}

	/* ---------- Spacer ---------- */
	.spacer {
		flex: 1;
	}

	/* ---------- Selector de idioma ---------- */
	.lang-selector {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 4px;
		width: 100%;
		padding: 10px 4px;
		color: #555;
		font-size: 10px;
		cursor: default;
	}
</style>
