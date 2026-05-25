<script lang="ts">
  let version = "v2.0.0-alpha";
</script>

<main>
  <h1>Onda</h1>
  <p>Audio separation tool — {version}</p>
  <p>Frontend: Svelte + TypeScript + Tauri</p>
</main>

<style>
  :global(body) {
    margin: 0;
    padding: 0;
    background: #111;
    color: #eee;
    font-family: -apple-system, sans-serif;
  }
  main {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100vh;
  }
</style>
