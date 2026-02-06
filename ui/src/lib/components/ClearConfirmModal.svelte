<script>
  export let show = false;
  export let onClose = () => {};
  export let onConfirm = () => {};

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
        <h2 style="margin: 0;">Clear Completed Jobs</h2>
        <p class="notice">This will mark completed jobs as deleted. This cannot be undone.</p>
      </div>
      <button class="btn ghost" on:click={onClose}>Close</button>
    </div>
    <div class="actions">
      <button class="btn danger" on:click={onConfirm}>Yes, clear completed</button>
      <button class="btn ghost" on:click={onClose}>Cancel</button>
    </div>
  </div>
{/if}
