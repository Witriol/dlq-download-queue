<script>
  export let watch = null;
  export let draft = {};
  export let saving = false;
  export let onClose = () => {};
  export let onSave = () => {};
  export let onOpenBrowser = () => {};
  export let outputExample = () => '';
</script>

{#if watch}
  <div class="modal-backdrop" role="button" tabindex="0" aria-label="Close edit dialog" on:click={onClose} on:keydown={(e) => (e.key === 'Escape' || e.key === 'Enter') && onClose()}></div>
  <div class="modal panel series-edit-dialog" role="dialog" aria-modal="true" aria-labelledby="edit-series-title">
    <div class="modal-header"><h2 id="edit-series-title">Edit {watch.display_name}</h2><button class="btn icon-btn close-btn" type="button" on:click={onClose}>×</button></div>
    <div class="form-grid"><label>Webshare search title<input bind:value={draft.search_title} /></label><label>Library root<div class="series-folder-input"><input bind:value={draft.out_dir} /><button class="btn ghost" type="button" on:click={onOpenBrowser}>Browse</button></div></label><label>Series folder<input bind:value={draft.series_folder} /></label><label class="series-checkbox"><input type="checkbox" bind:checked={draft.organize_by_season} /> Create a season folder for each season</label><p class="field-hint">Episodes will be stored as <code>{outputExample(draft.out_dir, draft.series_folder, draft.organize_by_season)}</code>.</p><label>Fallback policy<select bind:value={draft.fallback_policy}><option value="strict">Strict</option><option value="balanced">Balanced</option><option value="manual">Manual fallback</option></select></label><div class="form-grid series-two-col"><label>Release delay (hours)<input type="number" min="0" bind:value={draft.release_delay_hours} /><span class="field-hint">Wait after airing before searching.</span></label><label>Preferred wait (hours)<input type="number" min="0" bind:value={draft.preferred_wait_hours} /><span class="field-hint">Wait before Balanced may use an alternative or Manual fallback flags it for review. Strict never relaxes.</span></label></div></div>
    <div class="modal-actions"><button class="btn ghost" type="button" on:click={onClose}>Cancel</button><button class="btn primary" type="button" on:click={onSave} disabled={saving}>{saving ? 'Saving…' : 'Save changes'}</button></div>
  </div>
{/if}
