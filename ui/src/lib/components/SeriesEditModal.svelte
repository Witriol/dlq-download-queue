<script>
  export let watch = null;
  export let draft = {};
  export let saving = false;
  export let error = '';
  export let onClose = () => {};
  export let onSave = () => {};
  export let onRemove = () => {};
  export let onOpenBrowser = () => {};
  export let outputExample = () => '';
</script>

{#if watch}
  <div class="modal-backdrop" role="button" tabindex="0" aria-label="Close edit dialog" on:click={onClose} on:keydown={(e) => (e.key === 'Escape' || e.key === 'Enter') && onClose()}></div>
  <div class="modal panel series-edit-dialog" role="dialog" aria-modal="true" aria-labelledby="edit-series-title">
    <div class="modal-header"><div><p class="eyebrow">Automation settings</p><h2 id="edit-series-title">Edit {watch.display_name}</h2></div><button class="btn icon-btn close-btn" type="button" aria-label="Close edit dialog" on:click={onClose}>×</button></div>
    {#if error}<div class="series-alert error" role="alert">{error}</div>{/if}
    <div class="form-grid series-edit-form">
      <div><label class="field-label" for="edit-series-folder">Series folder</label><div class="series-folder-input"><input id="edit-series-folder" bind:value={draft.out_dir} /><button class="btn ghost" type="button" on:click={onOpenBrowser}>Browse</button></div><p class="field-hint">Episodes will be stored as <code>{outputExample(draft.out_dir, '', draft.organize_by_season)}</code>.</p></div>
      <label class="series-checkbox"><input type="checkbox" bind:checked={draft.organize_by_season} /> Create a season folder for each season</label>
      <label for="edit-fallback-policy">If no exact match<select id="edit-fallback-policy" bind:value={draft.fallback_policy}><option value="strict">Strict — exact match only</option><option value="balanced">Balanced — best alternative after a wait</option><option value="manual">Manual — ask me after a wait</option></select></label>
      {#if draft.fallback_policy !== 'strict'}<label for="edit-preferred-wait">Wait (hours)<input id="edit-preferred-wait" type="number" min="0" bind:value={draft.preferred_wait_hours} /><span class="field-hint">After this wait, {draft.fallback_policy === 'manual' ? 'needs review is raised' : 'the best non-exact release downloads'}.</span></label>{/if}
      <label for="edit-search-title">Webshare search title<input id="edit-search-title" bind:value={draft.search_title} /></label>
    </div>
    <div class="modal-actions">
      <button class="btn danger-ghost" type="button" on:click={onRemove} disabled={saving}>Remove watcher</button>
      <div class="actions"><button class="btn ghost" type="button" on:click={onClose}>Cancel</button><button class="btn primary" type="button" on:click={onSave} disabled={saving}>{saving ? 'Saving…' : 'Save changes'}</button></div>
    </div>
  </div>
{/if}
