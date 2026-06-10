<script lang="ts">
  import { onMount } from 'svelte';

  const accentColors = [
    { name: 'Púrpura', value: '#6c5ce7' },
    { name: 'Azul', value: '#2196f3' },
    { name: 'Verde', value: '#4caf50' },
    { name: 'Naranja', value: '#ff9800' },
    { name: 'Rojo', value: '#f44336' },
    { name: 'Rosa', value: '#e91e63' },
    { name: 'Cian', value: '#00bcd4' },
    { name: 'Ámbar', value: '#ffc107' },
  ];

  let selectedAccent = $state('#6c5ce7');
  let isLight = $state(false);

  onMount(() => {
    // Load saved preferences
    const savedAccent = localStorage.getItem('onda-accent');
    if (savedAccent) {
      selectedAccent = savedAccent;
      applyAccent(savedAccent);
    }
    const savedTheme = localStorage.getItem('onda-theme');
    if (savedTheme === 'light') {
      isLight = true;
      applyTheme(true);
    }
  });

  function applyAccent(color: string) {
    const body = document.body;
    body.style.setProperty('--accent', color);
    // Calculate lighter version (accent-light)
    body.style.setProperty('--accent-light', adjustColor(color, 40));
    body.style.setProperty('--accent-dark', adjustColor(color, -30));
    body.style.setProperty('--accent-glow', color + '4d');
    body.style.setProperty('--accent-subtle', color + '14');
    body.style.setProperty('--accent-bg', color + '22');
    body.style.setProperty('--accent-border', color + '33');
    localStorage.setItem('onda-accent', color);
  }

  function adjustColor(hex: string, amount: number): string {
    // Simple lighten/darken by adjusting RGB
    const num = parseInt(hex.replace('#', ''), 16);
    const r = Math.min(255, Math.max(0, (num >> 16) + amount));
    const g = Math.min(255, Math.max(0, ((num >> 8) & 0xff) + amount));
    const b = Math.min(255, Math.max(0, (num & 0xff) + amount));
    return `rgb(${r}, ${g}, ${b})`;
  }

  function handleAccentClick(color: string) {
    selectedAccent = color;
    applyAccent(color);
  }

  function applyTheme(light: boolean) {
    if (light) {
      document.body.classList.add('light-theme');
    } else {
      document.body.classList.remove('light-theme');
    }
    localStorage.setItem('onda-theme', light ? 'light' : 'dark');
  }

  function handleThemeToggle() {
    isLight = !isLight;
    applyTheme(isLight);
  }
</script>

<div class="interface-settings">
  <!-- Accent color picker -->
  <div class="setting-group">
    <h3 class="group-title">Color de acento</h3>
    <p class="group-desc">Elige el color principal de la interfaz</p>
    <div class="color-grid">
      {#each accentColors as c}
        <button
          class="color-swatch"
          class:active={selectedAccent === c.value}
          style="background: {c.value}"
          onclick={() => handleAccentClick(c.value)}
          title={c.name}
        >
          {#if selectedAccent === c.value}
            <span class="check">✓</span>
          {/if}
        </button>
      {/each}
    </div>
  </div>

  <!-- Theme toggle -->
  <div class="setting-group">
    <h3 class="group-title">Tema</h3>
    <p class="group-desc">Alterna entre tema oscuro y claro</p>
    <div class="theme-toggle-row">
      <span class="theme-label">🌙 Oscuro</span>
      <button
        class="toggle-switch"
        class:active={isLight}
        onclick={handleThemeToggle}
        role="switch"
        aria-checked={isLight}
      >
        <span class="toggle-knob"></span>
      </button>
      <span class="theme-label">☀️ Claro</span>
    </div>
  </div>
</div>

<style>
  .interface-settings {
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 2rem;
  }

  .setting-group {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .group-title {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
    color: var(--text-primary);
  }

  .group-desc {
    margin: 0;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  /* Color picker grid */
  .color-grid {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }

  .color-swatch {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    border: 3px solid transparent;
    cursor: pointer;
    transition: transform 0.15s, border-color 0.15s;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0;
  }

  .color-swatch:hover {
    transform: scale(1.15);
  }

  .color-swatch.active {
    border-color: var(--text-primary);
    transform: scale(1.1);
  }

  .check {
    color: white;
    font-size: 18px;
    font-weight: bold;
    text-shadow: 0 1px 2px rgba(0,0,0,0.5);
  }

  /* Theme toggle switch */
  .theme-toggle-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .theme-label {
    font-size: 0.85rem;
    color: var(--text-secondary);
    font-weight: 500;
  }

  .toggle-switch {
    width: 48px;
    height: 26px;
    border-radius: 13px;
    background: #3a3a5a;
    border: 1px solid var(--border);
    cursor: pointer;
    position: relative;
    transition: background 0.2s;
    padding: 0;
  }

  .toggle-switch.active {
    background: var(--accent);
    border-color: var(--accent);
  }

  .toggle-knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: white;
    transition: left 0.2s;
    box-shadow: 0 1px 3px rgba(0,0,0,0.3);
  }

  .toggle-switch.active .toggle-knob {
    left: 24px;
  }
</style>
