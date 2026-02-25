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

  function focusFirst(node) {
    const el = node.querySelector('textarea, input, button:not(.close-btn)');
    el?.focus();
  }
</script>

<svelte:window on:keydown={(e) => { if (show && e.key === 'Escape') onClose(); }} />

{#if show}
  <div
    class="modal-backdrop"
    role="button"
    tabindex="0"
    aria-label="Close dialog"
    on:click={onClose}
    on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onClose(); } }}
  ></div>
  <div class="modal panel modal-wide add-jobs-dialog" role="dialog" aria-modal="true" use:focusFirst>
    <div class="modal-header">
      <h2 class="modal-title">Add Jobs</h2>
      <button class="btn icon-btn close-btn" type="button" aria-label="Close dialog" on:click={onClose}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
        </svg>
      </button>
    </div>
    <div class="form-grid add-jobs-form">
      <div class="form-field">
        <label for="add-urls">URLs{parsedUrlCount > 0 ? ` · ${parsedUrlCount} detected` : ''}</label>
        <textarea id="add-urls" bind:value={addUrlsText} placeholder="https://...\nhttps://..."></textarea>
        <p class="field-hint">Auto-detects site per URL. Unsupported URLs will be marked after adding.</p>
      </div>
      <div class="form-field">
        <label for="add-out-dir">Out Directory</label>
        <div class="actions">
          <input class="grow-input" id="add-out-dir" type="text" placeholder={outDirPlaceholder} bind:value={addOutDir} />
          <button class="btn ghost" type="button" on:click={onOpenBrowser}>Browse</button>
        </div>
      </div>
      {#if outDirPresets.length > 0}
        <div class="presets-row">
          <span class="presets-label">Presets</span>
          <div class="presets-list">
            {#each outDirPresets as preset}
              <button class="preset-btn" type="button" on:click={() => (addOutDir = preset)}>{preset}</button>
            {/each}
          </div>
        </div>
      {/if}
      <div class="form-field">
        <label for="add-archive-password">Archive Password</label>
        <input
          id="add-archive-password"
          type="text"
          bind:value={addArchivePassword}
          placeholder="Optional — applied to all links in this batch"
          autocomplete="off"
        />
      </div>
      <div class="actions add-jobs-actions">
        <label class="btn ghost">
          Import file(s)
          <input class="hidden-file-input" type="file" multiple accept=".txt" on:change={onHandleFiles} />
        </label>
        <button class="btn ghost danger-ghost" type="button" on:click={onClearUrls}>Clear</button>
        <span class="actions-spacer"></span>
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
          <div class="result-item result-item-error">
            <span class="result-url">{result.url}</span>
            <span class="result-error">{result.error}</span>
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/if}
