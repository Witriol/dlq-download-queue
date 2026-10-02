<script>
  import Modal from './Modal.svelte';

  export let show = false;
  export let title = '';
  export let message = '';
  export let confirmLabel = 'Confirm';
  export let confirmTone = 'danger';
  export let busy = false;
  export let error = '';
  export let onConfirm = () => {};
  export let onClose = () => {};
</script>

<Modal {show} {title} className="modal-narrow" {onClose}>
  <p class="notice">{message}</p>
  {#if error}
    <p class="alert error" role="alert">{error}</p>
  {/if}
  <svelte:fragment slot="footer">
    <div class="actions">
      <button class={`btn ${confirmTone}`} type="button" disabled={busy} on:click={onConfirm}>
        {busy ? `${confirmLabel}…` : confirmLabel}
      </button>
      <button class="btn ghost" type="button" data-initial-focus on:click={onClose}>Cancel</button>
    </div>
  </svelte:fragment>
</Modal>
