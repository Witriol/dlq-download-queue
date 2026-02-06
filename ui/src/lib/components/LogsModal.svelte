<script>
  export let show = false;
  export let logsJob = null;
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
  <div class="modal panel" role="dialog" aria-modal="true">
    <div class="modal-header">
      <div>
        <h2 style="margin: 0;">Job Events</h2>
        {#if logsJob}
          <p class="notice">Job #{logsJob.id} · {logsJob.status}</p>
        {/if}
      </div>
      <button class="btn ghost" on:click={onClose}>Close</button>
    </div>
    <div class="toolbar" style="margin-bottom: 12px;">
      <label class="small">
        Tail
        <input type="number" min="1" max="500" style="width: 90px" bind:value={logsLimit} />
      </label>
      <label class="small">
        <input type="checkbox" bind:checked={logsAutoRefresh} /> auto refresh
      </label>
      <label class="small">
        every
        <input type="number" min="1" max="60" style="width: 64px" bind:value={logsInterval} />
        s
      </label>
      <button class="btn ghost" on:click={onRefresh} disabled={logsLoading}>Refresh</button>
    </div>
    {#if logsError}
      <p class="notice">Logs: {logsError}</p>
    {/if}
    <div class="result-list" style="max-height: 420px;">
      {#if logsEvents.length === 0}
        <div class="result-item">No events yet.</div>
      {:else}
        {#each logsEvents as line}
          <div class="result-item">{line}</div>
        {/each}
      {/if}
    </div>
  </div>
{/if}
