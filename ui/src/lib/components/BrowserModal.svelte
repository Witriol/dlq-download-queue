<script>
  export let show = false;
  export let browserPath = '';
  export let browserDirs = [];
  export let browserParent = '';
  export let browserIsRoot = false;
  export let browserError = '';
  export let browserLoading = false;
  export let browserNewFolderName = '';

  export let onClose = () => {};
  export let onLoadBrowser = () => {};
  export let onCreateFolder = () => {};
  export let onSelectPath = () => {};

  let browserSearch = '';
  let previousBrowserPath = '';

  $: browserSearchQuery = browserSearch.trim().toLowerCase();
  $: filteredBrowserDirs = browserSearchQuery
    ? browserDirs.filter((dir) => dir.toLowerCase().includes(browserSearchQuery))
    : browserDirs;
  $: if (browserPath !== previousBrowserPath) {
    browserSearch = '';
    previousBrowserPath = browserPath;
  }

  function nextPath(dir) {
    if (dir.startsWith('/')) return dir;
    return browserPath ? `${browserPath}/${dir}` : `/${dir}`;
  }

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
  <div class="modal panel modal-wide browser-dialog" role="dialog" aria-modal="true">
    <div class="modal-header">
      <div>
        <h2 style="margin: 0;">Select Folder</h2>
        <p class="notice">Browse and select a destination folder</p>
      </div>
      <button class="btn icon-btn close-btn" type="button" aria-label="Close dialog" on:click={onClose}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
        </svg>
      </button>
    </div>

    <div class="browser-body">
      <div class="browser-main">
        {#if browserPath}
          <div class="toolbar browser-path-toolbar">
            <span class="badge">Path:</span>
            {#if browserParent && !browserIsRoot}
              <button class="btn ghost" on:click={() => onLoadBrowser(browserParent)}>↑ Up</button>
            {/if}
            <button class="btn ghost" on:click={() => onLoadBrowser('')}>Root</button>
            <span class="browser-current-path">{browserPath}</span>
          </div>
        {/if}

        <div class="browser-search">
          <label for="browser-search">Search in This Folder</label>
          <input
            id="browser-search"
            type="text"
            placeholder="Type folder name..."
            bind:value={browserSearch}
            disabled={browserLoading || browserDirs.length === 0}
          />
          {#if browserSearchQuery}
            <p class="small browser-search-meta">Showing {filteredBrowserDirs.length} of {browserDirs.length} folders</p>
          {/if}
        </div>

        <div class="result-list browser-list">
          {#if browserLoading}
            <div class="result-item">Loading...</div>
          {:else if browserDirs.length === 0}
            <div class="result-item">No subdirectories</div>
          {:else if filteredBrowserDirs.length === 0}
            <div class="result-item">No matching folders in this directory</div>
          {:else}
            {#each filteredBrowserDirs as dir}
              <div class="result-item browser-dir-item">
                <button class="btn ghost browser-dir-btn" on:click={() => onLoadBrowser(nextPath(dir))}>
                  {dir}
                </button>
              </div>
            {/each}
          {/if}
        </div>

        {#if browserError}
          <p class="notice">Error: {browserError}</p>
        {/if}
      </div>

      <div class="browser-footer">
        <div class="form-grid">
          <div>
            <label for="browser-new-folder">Create New Folder</label>
            <div class="actions">
              <input class="grow-input" id="browser-new-folder" type="text" placeholder="Folder name" bind:value={browserNewFolderName} />
              <button class="btn ghost" on:click={onCreateFolder} disabled={!browserNewFolderName.trim()}>
                + Create
              </button>
            </div>
          </div>
        </div>

        <div class="actions">
          <button class="btn primary" on:click={() => onSelectPath(browserPath)}>
            Select Current Folder
          </button>
          <button class="btn ghost" on:click={onClose}>Cancel</button>
        </div>
      </div>
    </div>
  </div>
{/if}
