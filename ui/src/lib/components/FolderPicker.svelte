<script>
  export let value = '';
  export let presets = [];
  export let favorites = [];
  export let placeholder = 'Select a preset or type a path';
  export let label = 'Download folder';
  export let inputId;
  export let showFavorites = true;
  export let onBrowse = () => {};
  export let onRemoveFavorite = () => {};
  export let onInput = () => {};

  function pick(path) {
    value = path;
    onInput(value);
  }

  function handleInput() {
    onInput(value);
  }
</script>

<div class="folder-picker form-field">
  <label class="field-label" for={inputId}>{label}</label>
  {#if showFavorites && favorites.length > 0}
    <div class="presets-row favorite-folders-row">
      <span class="presets-label">Favorites</span>
      <div class="presets-list favorite-folders-list">
        {#each favorites as favorite}
          <div class:active={value === favorite} class="favorite-folder-chip">
            <button class="favorite-folder-btn" type="button" title={favorite} on:click={() => pick(favorite)}>
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
  {#if presets.length > 0}
    <div class="presets-row">
      <span class="presets-label">Presets</span>
      <div class="presets-list">
        {#each presets as preset}
          <button class="preset-btn" type="button" on:click={() => pick(preset)}>{preset}</button>
        {/each}
      </div>
    </div>
  {/if}
  <div class="folder-input-row">
    <input id={inputId} type="text" {placeholder} bind:value on:input={handleInput} />
    <button class="btn ghost" type="button" on:click={onBrowse}>Browse</button>
  </div>
</div>
