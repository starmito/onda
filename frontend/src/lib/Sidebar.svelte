<script>
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
		{ id: 'pitch',    label: 'Cambiar Tono',        icon: '🎛' },
		{ id: 'bpm',      label: 'Detectar velocidad',  icon: '📊' },
		{ id: 'daw',      label: 'DAW',                  icon: '🎼' },
	];

	const bottomItems = [
		{ id: 'help',     label: 'Ayuda',               icon: '❓' },
		{ id: 'settings', label: 'Ajustes',             icon: '⚙️' },
	];

	function handleClick(tabId) {
		ontabchange(tabId);
	}
</script>

<aside class="sidebar" class:collapsed>
	<!-- Botón toggle arriba del todo -->
	<button class="toggle-btn" onclick={ontoggle} aria-label={collapsed ? 'Expandir sidebar' : 'Colapsar sidebar'}>
		<span class="icon-only">≡</span>
		<span class="label-text">Menú</span>
	</button>

	<!-- Presets dinámicos -->
	{#each presetTabs as tab (tab.id)}
		<button
			class="nav-item"
			class:active={activeTab === tab.id}
			onclick={() => handleClick(tab.id)}
		>
			<span class="icon-only">{tab.icon}</span>
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
			<span class="icon-only">{item.icon}</span>
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
			<span class="icon-only">{item.icon}</span>
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
		width: 64px;
	}

	/* ---------- Botón toggle ---------- */
	.toggle-btn {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 14px 16px;
		background: none;
		border: none;
		color: #ccc;
		font-size: 20px;
		cursor: pointer;
		transition: background 0.15s;
		white-space: nowrap;
	}

	.toggle-btn:hover {
		background: #2a2a3e;
	}

	/* ---------- Items de navegación ---------- */
	.nav-item {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 10px 16px;
		background: none;
		border: none;
		border-left: 3px solid transparent;
		color: #ccc;
		font-size: 14px;
		cursor: pointer;
		transition: background 0.15s, border-color 0.15s;
		white-space: nowrap;
		text-align: left;
	}

	.nav-item:hover {
		background: #2a2a3e;
	}

	.nav-item.active {
		background: #3a3a5e;
		border-left-color: #6c5ce7;
		color: #fff;
	}

	/* ---------- Icono y texto ---------- */
	.icon-only {
		font-size: 18px;
		min-width: 22px;
		text-align: center;
		flex-shrink: 0;
	}

	.label-text {
		overflow: hidden;
		text-overflow: ellipsis;
		opacity: 1;
		transition: opacity 0.15s;
	}

	/* Cuando colapsado: ocultar texto */
	.collapsed .label-text {
		opacity: 0;
		width: 0;
		margin: 0;
		pointer-events: none;
	}

	.collapsed .toggle-btn {
		justify-content: center;
		padding: 14px 0;
	}

	.collapsed .nav-item {
		justify-content: center;
		padding: 10px 0;
	}

	/* ---------- Separador ---------- */
	.separator {
		height: 1px;
		background: #2e2e3e;
		margin: 6px 12px;
		flex-shrink: 0;
	}

	/* ---------- Spacer ---------- */
	.spacer {
		flex: 1;
	}

	/* ---------- Selector de idioma ---------- */
	.lang-selector {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 10px 16px;
		color: #888;
		font-size: 14px;
		cursor: default;
		white-space: nowrap;
	}

	.collapsed .lang-selector {
		justify-content: center;
		padding: 10px 0;
	}
</style>
