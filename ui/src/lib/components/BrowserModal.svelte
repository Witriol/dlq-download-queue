<script>
  import { tick } from 'svelte';
  import Modal from '$lib/components/Modal.svelte';

  export let show = false;
  export let browserPath = '';
  export let browserDirs = [];
  export let browserParent = '';
  export let browserIsRoot = false;
  export let browserError = '';
  export let browserLoading = false;
  export let browserNewFolderName = '';
  export let browserFavoritePaths = [];

  export let onClose = () => {};
  export let onLoadBrowser = () => {};
  export let onCreateFolder = () => {};
  export let onSelectPath = () => {};
  export let onAddFavorite = () => {};
  export let onRemoveFavorite = () => {};

  let browserSearch = '';
  let previousBrowserPath = '';
  let showSearch = false;
  let showNewFolder = false;
  let newFolderName = '';
  let searchInput;
  let searchFocusPending = false;

  $: browserSearchQuery = showSearch ? browserSearch.trim().toLowerCase() : '';
  $: filteredBrowserDirs = browserSearchQuery
    ? browserDirs.filter((dir) => dir.toLowerCase().includes(browserSearchQuery))
    : browserDirs;
  $: if (browserPath !== previousBrowserPath) {
    browserSearch = '';
    showSearch = false;
    showNewFolder = false;
    newFolderName = '';
    searchFocusPending = false;
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

  async function toggleSearch() {
    showSearch = !showSearch;
    if (!showSearch) {
      browserSearch = '';
      searchFocusPending = false;
      return;
    }
    searchFocusPending = true;
    await tick();
    focusSearchInput();
  }

  function focusSearchInput() {
    if (!searchInput || browserLoading) return;
    searchInput.focus();
    searchInput.select();
    searchFocusPending = false;
  }

  $: if (searchFocusPending && showSearch && !browserLoading) {
    tick().then(focusSearchInput);
  }
</script>

<Modal {show} title="Select folder" className="modal-wide browser-dialog" closeLabel="Close dialog" {onClose}>
  <div class="browser-body">
    <div class="browser-main">
      <div class="toolbar browser-path-toolbar">
        <nav class="breadcrumb" aria-label="Folder path">
          <button class="crumb-btn" type="button" aria-label="Root folder" on:click={() => onLoadBrowser('')}>/</button>
          {#each breadcrumbSegments(browserPath) as seg, i}
            <span class="crumb-sep">›</span>
            <button class="crumb-btn" type="button" on:click={() => onLoadBrowser(breadcrumbPath(breadcrumbSegments(browserPath), i))}>
              {seg}
            </button>
          {/each}
        </nav>
        <div class="browser-toolbar-actions">
          {#if browserParent && !browserIsRoot}
            <button class="btn icon-btn ghost tiny" type="button" title="Up" aria-label="Parent folder" on:click={() => onLoadBrowser(browserParent)}>
              <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16">
                <path d="M12 19V5M5 12l7-7 7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" fill="none"/>
              </svg>
            </button>
          {/if}
          <button class="btn icon-btn ghost tiny" type="button" title="Search folders" aria-label="Search folders" aria-pressed={showSearch} on:click={toggleSearch}>
            <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16">
              <circle cx="11" cy="11" r="7" stroke="currentColor" stroke-width="2" fill="none"/>
              <path d="M16.5 16.5 21 21" stroke="currentColor" stroke-width="2" stroke-linecap="round" fill="none"/>
            </svg>
          </button>
          <button class="btn icon-btn ghost tiny" type="button" title="New folder" aria-label="New folder" aria-expanded={showNewFolder} on:click={() => { showNewFolder = !showNewFolder; }}>
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
            placeholder="Search folders…"
            bind:value={browserSearch}
            bind:this={searchInput}
            disabled={browserLoading}
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
              on:keydown={(e) => { if (e.key === 'Enter') handleCreateFolder(); if (e.key === 'Escape') { e.stopPropagation(); showNewFolder = false; newFolderName = ''; } }}
            />
            <button class="btn tiny ghost" type="button" on:click={handleCreateFolder} disabled={!newFolderName.trim()}>
              Create
            </button>
            <button class="btn icon-btn ghost tiny" type="button" on:click={() => { showNewFolder = false; newFolderName = ''; }} title="Cancel" aria-label="Cancel new folder">
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
            {@const dirPath = nextPath(dir)}
            {@const isFavorite = browserFavoritePaths.includes(dirPath)}
            <div class="result-item browser-dir-item">
              <button class="btn ghost browser-dir-btn" type="button" on:click={() => onLoadBrowser(dirPath)}>
                <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16" style="flex-shrink:0">
                  <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.17a2 2 0 0 1-1.41-.59l-.83-.82A2 2 0 0 0 9.17 4H4a2 2 0 0 0-2 2v12c0 1.1.9 2 2 2z" stroke="currentColor" stroke-width="1.8" fill="none"/>
                </svg>
                {dir}
              </button>
              <button
                class="btn icon-btn ghost tiny browser-dir-favorite"
                class:is-favorite={isFavorite}
                type="button"
                title={isFavorite ? 'Remove from favorites' : 'Add to favorites'}
                aria-label={`Favorite ${dirPath}`}
                aria-pressed={isFavorite}
                on:click={() => (isFavorite ? onRemoveFavorite(dirPath) : onAddFavorite(dirPath))}
              >
                <svg viewBox="0 0 24 24" aria-hidden="true" width="14" height="14">
                  <path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3l-5.6 2.9 1.1-6.2L3 9.6l6.2-.9L12 3z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" fill={isFavorite ? 'currentColor' : 'none'} />
                </svg>
              </button>
            </div>
          {/each}
        {/if}
      </div>

      {#if browserError}
        <p class="alert error" role="alert">{browserError}</p>
      {/if}
    </div>

    <div class="browser-footer">
      <div class="actions">
        {#if browserPath}
          <button class="btn primary" type="button" on:click={() => onSelectPath(browserPath)}>
            Use "{browserPath.split('/').at(-1) || browserPath}"
          </button>
        {/if}
        <button class="btn ghost browser-cancel-btn" type="button" on:click={onClose}>Cancel</button>
      </div>
    </div>
  </div>
</Modal>
