<script>
  export let show = false;
  export let settingsConcurrency = 2;
  export let settingsError = '';
  export let settingsSaving = false;

  export let onClose = () => {};
  export let onSave = () => {};

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
        <h2 style="margin: 0;">Settings</h2>
        <p class="notice">Configure runtime settings</p>
      </div>
      <button class="btn ghost" on:click={onClose}>Close</button>
    </div>
    <div class="form-grid">
      <div>
        <label for="settings-concurrency">Concurrency (1-10)</label>
        <input id="settings-concurrency" type="number" min="1" max="10" bind:value={settingsConcurrency} />
        <p class="notice">Number of concurrent downloads</p>
      </div>
      <div class="actions">
        <button class="btn primary" on:click={onSave} disabled={settingsSaving}>
          {settingsSaving ? 'Saving...' : 'Save'}
        </button>
        <button class="btn ghost" on:click={onClose}>Cancel</button>
      </div>
    </div>
    {#if settingsError}
      <p class="notice">Error: {settingsError}</p>
    {/if}
  </div>
{/if}
