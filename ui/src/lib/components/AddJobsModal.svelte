<script>
  export let show = false;
  export let addOutDir = '';
  export let addUrlsText = '';
  export let addArchivePassword = '';
  export let outDirPlaceholder = 'Select a preset or type a path';
  export let outDirPresets = [];
  export let parsedUrlCount = 0;
  export let adding = false;
  export let addError = '';
  export let metaError = '';
  export let addErrors = [];

  export let onClose = () => {};
  export let onOpenBrowser = () => {};
  export let onHandleFiles = () => {};
  export let onClearUrls = () => {};
  export let onSubmit = () => {};

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
  <div class="modal panel modal-wide add-jobs-dialog" role="dialog" aria-modal="true">
    <div class="modal-header">
      <div>
        <h2 class="modal-title">Add Jobs</h2>
        <p class="notice add-jobs-subtitle">Auto-detects site per URL; unsupported URLs will be marked.</p>
      </div>
      <button class="btn icon-btn close-btn" type="button" aria-label="Close dialog" on:click={onClose}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
        </svg>
      </button>
    </div>
    <div class="form-grid add-jobs-form">
      <div class="form-field">
        <label for="add-out-dir">Out Directory</label>
        <div class="actions">
          <input id="add-out-dir" type="text" placeholder={outDirPlaceholder} bind:value={addOutDir} style="flex: 1;" />
          <button class="btn ghost" type="button" on:click={onOpenBrowser}>Browse</button>
        </div>
      </div>
      <div class="presets-row">
        <span class="presets-label">Presets</span>
        {#if outDirPresets.length === 0}
          <span class="presets-empty">No presets available.</span>
        {:else}
          <div class="presets-list">
            {#each outDirPresets as preset}
              <button class="preset-btn" type="button" on:click={() => (addOutDir = preset)}>{preset}</button>
            {/each}
          </div>
        {/if}
      </div>
      <div class="form-field">
        <label for="add-urls">URLs</label>
        <textarea id="add-urls" bind:value={addUrlsText} placeholder="https://...\nhttps://..."></textarea>
      </div>
      <div class="form-field">
        <label for="add-archive-password">Archive Password for This Batch (optional)</label>
        <input
          id="add-archive-password"
          type="text"
          bind:value={addArchivePassword}
          placeholder="One password for all links in this batch"
          autocomplete="off"
        />
      </div>
      <div class="badge add-jobs-count">
        URLs: {parsedUrlCount}
      </div>
      <div class="actions add-jobs-actions">
        <label class="btn ghost">
          Import file(s)
          <input type="file" multiple accept=".txt" style="display: none" on:change={onHandleFiles} />
        </label>
        <button class="btn ghost" type="button" on:click={onClearUrls}>Clear</button>
        <button class="btn primary" type="button" on:click={onSubmit} disabled={adding}>
          {adding ? 'Adding...' : 'Add Jobs'}
        </button>
      </div>
    </div>

    {#if addError}
      <p class="notice">{addError}</p>
    {/if}
    {#if metaError}
      <p class="notice">Presets: {metaError}</p>
    {/if}

    {#if addErrors.length > 0}
      <div class="divider"></div>
      <div class="result-list">
        {#each addErrors as result}
          <div class="result-item">
            [ERR] {result.url} -> {result.error}
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/if}
