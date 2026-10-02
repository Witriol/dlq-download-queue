<script>
  import FolderPicker from '$lib/components/FolderPicker.svelte';
  import Modal from '$lib/components/Modal.svelte';

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
  <Modal show eyebrow="Automation settings" title={`Edit ${watch.display_name}`} className="series-edit-dialog" {onClose}>
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    <div class="form-grid series-edit-form">
      <FolderPicker inputId="edit-series-folder" bind:value={draft.out_dir} showFavorites={false} onBrowse={onOpenBrowser} />
      <label class="series-checkbox"><input type="checkbox" bind:checked={draft.organize_by_season} /> Season folders → <code>{outputExample(draft.out_dir, '', draft.organize_by_season, watch.next_episode?.season || watch.last_episode?.season || 1)}</code></label>
      <div class="fallback-rule">
        <p class="fallback-sentence">If the exact release isn't out{#if draft.fallback_policy !== 'strict'}{' '}after <span class="input-with-suffix fallback-hours"><input id="edit-preferred-wait" type="number" min="0" step="1" bind:value={draft.preferred_wait_hours} aria-label="Hours to wait" /><span>h</span></span>{/if} <span class="fallback-arrow" aria-hidden="true">→</span> <select id="edit-fallback-policy" bind:value={draft.fallback_policy} aria-label="Fallback action"><option value="balanced">Download the best alternative</option><option value="manual">Ask me</option><option value="strict">Keep waiting for exact (max 3 days)</option></select></p>
        <p class="field-hint">Exact = same resolution, codec and release group as the reference.</p>
      </div>
      <label for="edit-search-title">Webshare search title<input id="edit-search-title" bind:value={draft.search_title} /></label>
    </div>
    <div slot="footer" class="modal-actions">
      <button class="btn danger-ghost" type="button" on:click={onRemove} disabled={saving}>Remove watcher</button>
      <div class="actions"><button class="btn ghost" type="button" on:click={onClose}>Cancel</button><button class="btn primary" type="button" on:click={onSave} disabled={saving}>{saving ? 'Saving…' : 'Save changes'}</button></div>
    </div>
  </Modal>
{/if}
