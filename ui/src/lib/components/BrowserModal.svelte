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
  let showSearch = false;
  let showNewFolder = false;
  let newFolderName = '';

  $: browserSearchQuery = browserSearch.trim().toLowerCase();
  $: filteredBrowserDirs = browserSearchQuery
    ? browserDirs.filter((dir) => dir.toLowerCase().includes(browserSearchQuery))
    : browserDirs;
  $: if (browserPath !== previousBrowserPath) {
    browserSearch = '';
    showSearch = false;
    showNewFolder = false;
    newFolderName = '';
    previousBrowserPath = browserPath;
  }

  function nextPath(dir) {
    if (dir.startsWith('/')) return dir;
    return browserPath ? `${browserPath}/${dir}` : `/${dir}`;
  }

  function breadcrumbSegments(path) {
    return path.split('/').filter(Boolean);
  }

  function breadcrumbPath(segments, index) {
    return '/' + segments.slice(0, index + 1).join('/');
  }

  function handleCreateFolder() {
    if (!newFolderName.trim()) return;
    browserNewFolderName = newFolderName;
    onCreateFolder();
    newFolderName = '';
    showNewFolder = false;
  }

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
  <div class="modal panel modal-wide browser-dialog" role="dialog" aria-modal="true" use:focusFirst>
    <div class="modal-header">
      <h2 style="margin: 0;">Select Folder</h2>
      <button class="btn icon-btn close-btn" type="button" aria-label="Close dialog" on:click={onClose}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
        </svg>
      </button>
    </div>

    <div class="browser-body">
      <div class="browser-main">
        <div class="toolbar browser-path-toolbar">
          <nav class="breadcrumb">
            <button class="crumb-btn" on:click={() => onLoadBrowser('')}>/</button>
            {#each breadcrumbSegments(browserPath) as seg, i}
              <span class="crumb-sep">›</span>
              <button class="crumb-btn" on:click={() => onLoadBrowser(breadcrumbPath(breadcrumbSegments(browserPath), i))}>
                {seg}
              </button>
            {/each}
          </nav>
          <div class="browser-toolbar-actions">
            {#if browserParent && !browserIsRoot}
              <button class="btn icon-btn ghost tiny" title="Up" on:click={() => onLoadBrowser(browserParent)}>
                <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16">
                  <path d="M12 19V5M5 12l7-7 7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" fill="none"/>
                </svg>
              </button>
            {/if}
            <button class="btn icon-btn ghost tiny" title="Search folders" on:click={() => { showSearch = !showSearch; }}>
              <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16">
                <circle cx="11" cy="11" r="7" stroke="currentColor" stroke-width="2" fill="none"/>
                <path d="M16.5 16.5 21 21" stroke="currentColor" stroke-width="2" stroke-linecap="round" fill="none"/>
              </svg>
            </button>
            <button class="btn icon-btn ghost tiny" title="New folder" on:click={() => { showNewFolder = !showNewFolder; }}>
              <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16">
                <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.17a2 2 0 0 1-1.41-.59l-.83-.82A2 2 0 0 0 9.17 4H4a2 2 0 0 0-2 2v12c0 1.1.9 2 2 2z" stroke="currentColor" stroke-width="1.8" fill="none"/>
                <path d="M12 11v6M9 14h6" stroke="currentColor" stroke-width="2" stroke-linecap="round" fill="none"/>
              </svg>
            </button>
          </div>
        </div>

        {#if showSearch}
          <div class="browser-search">
            <input
              id="browser-search"
              type="text"
              placeholder="Search folders..."
              bind:value={browserSearch}
              disabled={browserLoading || browserDirs.length === 0}
            />
            {#if browserSearchQuery}
              <p class="small browser-search-meta">Showing {filteredBrowserDirs.length} of {browserDirs.length} folders</p>
            {/if}
          </div>
        {/if}

        <div class="result-list browser-list">
          {#if showNewFolder}
            <div class="new-folder-row">
              <input
                type="text"
                placeholder="New folder name"
                bind:value={newFolderName}
                on:keydown={(e) => { if (e.key === 'Enter') handleCreateFolder(); if (e.key === 'Escape') { showNewFolder = false; newFolderName = ''; } }}
              />
              <button class="btn tiny ghost" on:click={handleCreateFolder} disabled={!newFolderName.trim()}>
                Create
              </button>
              <button class="btn icon-btn ghost tiny" on:click={() => { showNewFolder = false; newFolderName = ''; }} title="Cancel">
                <svg viewBox="0 0 24 24" aria-hidden="true" width="14" height="14">
                  <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
                </svg>
              </button>
            </div>
          {/if}

          {#if browserLoading}
            {#each [1, 2, 3] as _}
              <div class="result-item skeleton-row"></div>
            {/each}
          {:else if browserDirs.length === 0}
            <div class="result-item">No subdirectories</div>
          {:else if filteredBrowserDirs.length === 0}
            <div class="result-item">No matching folders in this directory</div>
          {:else}
            {#each filteredBrowserDirs as dir}
              <div class="result-item browser-dir-item">
                <button class="btn ghost browser-dir-btn" on:click={() => onLoadBrowser(nextPath(dir))}>
                  <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16" style="flex-shrink:0">
                    <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.17a2 2 0 0 1-1.41-.59l-.83-.82A2 2 0 0 0 9.17 4H4a2 2 0 0 0-2 2v12c0 1.1.9 2 2 2z" stroke="currentColor" stroke-width="1.8" fill="none"/>
                  </svg>
                  {dir}
                </button>
                <button class="btn icon-btn ghost tiny browser-dir-use" title="Use this folder" on:click={() => onSelectPath(nextPath(dir))}>
                  <svg viewBox="0 0 24 24" aria-hidden="true" width="14" height="14">
                    <path d="M5 13l4 4L19 7" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" fill="none"/>
                  </svg>
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
        <div class="actions">
          <button class="btn primary" on:click={() => onSelectPath(browserPath)} disabled={!browserPath}>
            {browserPath ? `Use "${browserPath.split('/').at(-1) || browserPath}"` : 'Navigate to a folder'}
          </button>
          <button class="btn ghost browser-cancel-btn" on:click={onClose}>Cancel</button>
        </div>
      </div>
    </div>
  </div>
{/if}
