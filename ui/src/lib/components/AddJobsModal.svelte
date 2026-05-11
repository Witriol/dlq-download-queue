<script>
  export let show = false;
  export let addOutDir = '';
  export let addUrlsText = '';
  export let addArchivePassword = '';
  export let outDirPlaceholder = 'Select a preset or type a path';
  export let outDirPresets = [];
  export let outDirFavorites = [];
  export let parsedUrlCount = 0;
  export let adding = false;
  export let addError = '';
  export let metaError = '';
  export let addErrors = [];

  export let onClose = () => {};
  export let onOpenBrowser = () => {};
  export let onRemoveFavorite = () => {};
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
        {#if outDirFavorites.length > 0}
          <div class="presets-row favorite-folders-row">
            <span class="presets-label">Favorites</span>
            <div class="presets-list favorite-folders-list">
              {#each outDirFavorites as favorite}
                <div class:active={addOutDir === favorite} class="favorite-folder-chip">
                  <button class="favorite-folder-btn" type="button" title={favorite} on:click={() => (addOutDir = favorite)}>
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                      <path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3l-5.6 2.9 1.1-6.2L3 9.6l6.2-.9L12 3z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" fill="currentColor" />
                    </svg>
                    <span>{favorite}</span>
                  </button>
                  <button class="favorite-remove-btn" type="button" title="Remove favorite" aria-label={`Remove favorite ${favorite}`} on:click={() => onRemoveFavorite(favorite)}>
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                      <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
                    </svg>
                  </button>
                </div>
              {/each}
            </div>
          </div>
        {/if}
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
        <div class="actions out-dir-actions">
          <button class="btn ghost" type="button" on:click={onOpenBrowser}>Browse</button>
          <input class="grow-input" id="add-out-dir" type="text" placeholder={outDirPlaceholder} bind:value={addOutDir} />
        </div>
      </div>
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
