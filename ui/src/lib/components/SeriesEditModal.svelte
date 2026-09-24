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
      <div>
        <label class="field-label" for="edit-series-folder">Folder</label>
        <div class="series-folder-input"><input id="edit-series-folder" bind:value={draft.out_dir} /><button class="btn ghost" type="button" on:click={onOpenBrowser}>Browse</button></div>
      </div>
      <label class="series-checkbox"><input type="checkbox" bind:checked={draft.organize_by_season} /> Season folders → <code>{outputExample(draft.out_dir, '', draft.organize_by_season, watch.next_episode?.season || watch.last_episode?.season || 1)}</code></label>
      <div class="fallback-rule">
        <p class="fallback-sentence">If the exact release isn't out{#if draft.fallback_policy !== 'strict'}{' '}after <span class="input-with-suffix fallback-hours"><input id="edit-preferred-wait" type="number" min="0" step="1" bind:value={draft.preferred_wait_hours} aria-label="Hours to wait" /><span>h</span></span>{/if} <span class="fallback-arrow" aria-hidden="true">→</span> <select id="edit-fallback-policy" bind:value={draft.fallback_policy} aria-label="Fallback action"><option value="balanced">Download the best alternative</option><option value="manual">Ask me</option><option value="strict">Keep waiting for exact (max 3 days)</option></select></p>
        <p class="field-hint">Exact = same resolution, codec and release group as the reference.</p>
      </div>
      <label for="edit-search-title">Webshare search title<input id="edit-search-title" bind:value={draft.search_title} /></label>
    </div>
    <div class="modal-actions">
      <button class="btn danger-ghost" type="button" on:click={onRemove} disabled={saving}>Remove watcher</button>
      <div class="actions"><button class="btn ghost" type="button" on:click={onClose}>Cancel</button><button class="btn primary" type="button" on:click={onSave} disabled={saving}>{saving ? 'Saving…' : 'Save changes'}</button></div>
    </div>
  </div>
{/if}
