<script>
  import { formatDateTime } from '$lib/format';

  export let show = false;
  export let title = 'Job Events';
  export let subtitle = '';
  export let logsEvents = [];
  export let logsLimit = 50;
  export let logsAutoRefresh = true;
  export let logsInterval = 3;
  export let logsError = '';
  export let logsLoading = false;

  export let onClose = () => {};
  export let onRefresh = () => {};

  function onBackdropKeydown(event) {
    if (event.key === 'Enter' || event.key === ' ' || event.key === 'Escape') {
      event.preventDefault();
      onClose();
    }
  }

  function formatEventLine(line) {
    const text = typeof line === 'string' ? line : '';
    const match = text.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\s+(\w+)\s+(.*)$/);
    if (!match) return text;
    const dt = new Date(match[1]);
    if (Number.isNaN(dt.getTime())) return text;
    const ts = formatDateTime(dt.toISOString());
    return `${ts} ${match[2]} ${match[3]}`;
  }
</script>

{#if show}
  <div
    class="modal-backdrop"
    role="button"
    tabindex="0"
    aria-label="Close dialog"
    on:click={onClose}
    on:keydown={onBackdropKeydown}
  ></div>
  <div class="modal panel modal-logs" role="dialog" aria-modal="true">
    <div class="modal-header">
      <div>
        <h2 style="margin: 0;">{title}</h2>
        {#if subtitle}
          <p class="notice">{subtitle}</p>
        {/if}
      </div>
      <button class="btn icon-btn close-btn" type="button" aria-label="Close dialog" on:click={onClose}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
        </svg>
      </button>
    </div>
    <div class="toolbar logs-toolbar" style="margin-bottom: 12px;">
      <label class="small">
        Tail
        <input class="num-input num-input-medium" type="number" min="1" max="500" bind:value={logsLimit} />
      </label>
      <label class="small">
        <input type="checkbox" bind:checked={logsAutoRefresh} /> auto refresh
      </label>
      <label class="small">
        every
        <input class="num-input num-input-small" type="number" min="1" max="60" bind:value={logsInterval} />
        s
      </label>
      <button class="btn ghost" on:click={onRefresh} disabled={logsLoading}>Refresh</button>
    </div>
    {#if logsError}
      <p class="notice">Logs: {logsError}</p>
    {/if}
    <div class="result-list logs-list">
      {#if logsEvents.length === 0}
        <div class="result-item">No events yet.</div>
      {:else}
        {#each logsEvents as line}
          <div class="result-item">{formatEventLine(line)}</div>
        {/each}
      {/if}
    </div>
  </div>
{/if}
