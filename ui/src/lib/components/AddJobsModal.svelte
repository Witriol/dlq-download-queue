<script>
  import Modal from '$lib/components/Modal.svelte';
  import FolderPicker from '$lib/components/FolderPicker.svelte';

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
  export let addProgress = null;

  export let onClose = () => {};
  export let onOpenBrowser = () => {};
  export let onRemoveFavorite = () => {};
  export let onHandleFiles = () => {};
  export let onClearUrls = () => {};
  export let onSubmit = () => {};
</script>

<Modal {show} title="Add jobs" className="modal-wide add-jobs-dialog" closeLabel="Close dialog" {onClose}>
  <div class="form-grid add-jobs-form">
    <div class="form-field">
      <label for="add-urls">URLs{parsedUrlCount > 0 ? ` · ${parsedUrlCount} detected` : ''}</label>
      <textarea id="add-urls" bind:value={addUrlsText} placeholder="https://…\nhttps://…"></textarea>
      <p class="field-hint">Auto-detects site per URL. Unsupported URLs will be marked after adding.</p>
    </div>
    <FolderPicker
      bind:value={addOutDir}
      inputId="add-out-dir"
      presets={outDirPresets}
      favorites={outDirFavorites}
      placeholder={outDirPlaceholder}
      onBrowse={onOpenBrowser}
      {onRemoveFavorite}
    />
    <div class="form-field">
      <label for="add-archive-password">Archive password</label>
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
        {adding ? (addProgress ? `Adding ${addProgress.done}/${addProgress.total}…` : 'Adding…') : 'Add jobs'}
      </button>
    </div>
  </div>

  {#if addError}
    <p class="alert error" role="alert">{addError}</p>
  {/if}
  {#if metaError}
    <p class="alert error" role="alert">Presets: {metaError}</p>
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
</Modal>
