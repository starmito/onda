<script lang="ts">
	let { oncomplete }: { oncomplete: () => void } = $props();

	let progress = $state(0);
	let step = $state('');
	let eta = $state(0);
	let status = $state('idle'); // idle, running, done, error
	let errorMsg = $state('');

	let eventSource: EventSource | null = null;

	export function start() {
		if (eventSource) return;

		status = 'running';
		eventSource = new EventSource('http://192.168.1.87:3000/api/events');

		eventSource.onmessage = (event: MessageEvent) => {
			try {
				const data = JSON.parse(event.data);

				progress = data.progress ?? progress;
				step = data.step ?? step;
				eta = data.eta ?? eta;

				if (data.status === 'done') {
					status = 'done';
					eventSource?.close();
					eventSource = null;
					oncomplete();
				} else if (data.status === 'error') {
					status = 'error';
					errorMsg = data.error ?? 'Error desconocido';
					eventSource?.close();
					eventSource = null;
				} else {
					status = data.status ?? 'running';
				}
			} catch {
				// Ignorar mensajes mal formados
			}
		};

		eventSource.onerror = () => {
			status = 'error';
			errorMsg = 'Error de conexión con el servidor';
			eventSource?.close();
			eventSource = null;
		};
	}

	function reset() {
		eventSource?.close();
		eventSource = null;
		progress = 0;
		step = '';
		eta = 0;
		status = 'idle';
		errorMsg = '';
	}
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
				{Math.round(progress * 100)}% — {step ? `separando ${step}` : 'procesando'} — {eta > 0 ? `${eta}s restantes` : 'calculando...'}
			</p>
		{/if}

		{#if status === 'done'}
			<p class="progress-text progress-done">✅ Completado</p>
		{/if}

		{#if status === 'error'}
			<p class="progress-text progress-error">❌ {errorMsg}</p>
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
