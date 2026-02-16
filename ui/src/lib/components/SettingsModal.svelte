<script>
  export let show = false;
  export let settingsConcurrency = 2;
  export let settingsAutoDecrypt = true;
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
      <button class="btn icon-btn close-btn" type="button" aria-label="Close dialog" on:click={onClose}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
        </svg>
      </button>
    </div>
    <div class="form-grid">
      <div>
        <label for="settings-concurrency">Concurrency (1-10)</label>
        <input id="settings-concurrency" type="number" min="1" max="10" bind:value={settingsConcurrency} />
        <p class="notice">Number of concurrent downloads</p>
      </div>
      <div>
        <label class="small" for="settings-auto-decrypt">
          <input id="settings-auto-decrypt" type="checkbox" bind:checked={settingsAutoDecrypt} />
          auto decrypt archives after download
        </label>
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
