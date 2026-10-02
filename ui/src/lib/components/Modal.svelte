<script context="module">
  // Open instances, bottom to top. Only the last one handles Escape and Tab.
  const stack = [];
  let nextId = 0;
</script>

<script>
  import { onDestroy, tick } from 'svelte';

  export let show = false;
  export let title = '';
  export let subtitle = '';
  export let eyebrow = '';
  export let describedBy = '';
  export let className = '';
  export let closeLabel = 'Close';
  export let onClose = () => {};

  const FOCUSABLE =
    'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';

  const instance = {};
  const titleId = `modal-title-${nextId++}`;

  let dialog;
  let opened = false;
  let depth = 0;
  let previousFocus = null;

  $: syncOpen(show);

  function syncOpen(value) {
    if (typeof document === 'undefined') {
      return;
    }
    if (value && !opened) {
      open();
      return;
    }
    if (!value && opened) {
      close();
    }
  }

  function open() {
    opened = true;
    depth = stack.length;
    stack.push(instance);
    previousFocus = document.activeElement;
    tick().then(focusInitial);
  }

  function close() {
    opened = false;
    const index = stack.indexOf(instance);
    if (index >= 0) {
      stack.splice(index, 1);
    }

    const target = previousFocus;
    previousFocus = null;
    if (!target || !document.contains(target)) {
      return;
    }

    const active = document.activeElement;
    const focusInside = !!dialog && !!active && dialog.contains(active);
    if (focusInside || active === document.body || !active) {
      target.focus();
    }
  }

  onDestroy(() => {
    if (opened) {
      close();
    }
  });

  function isTopmost() {
    return opened && stack[stack.length - 1] === instance;
  }

  function focusableControls() {
    if (!dialog) {
      return [];
    }
    return [...dialog.querySelectorAll(FOCUSABLE)].filter((el) => !el.hidden && el.getClientRects().length);
  }

  function focusInitial() {
    if (!dialog || !opened) {
      return;
    }
    const target =
      dialog.querySelector('[data-initial-focus]') ||
      dialog.querySelector('[autofocus]') ||
      focusableControls().find((el) => !el.classList.contains('close-btn')) ||
      focusableControls()[0] ||
      dialog;
    target.focus();
  }

  function onKeydown(event) {
    // A dialog above, or a nested input, already handled this key.
    if (event.defaultPrevented || !isTopmost()) {
      return;
    }

    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onClose();
      return;
    }

    if (event.key !== 'Tab') {

      return;

    }

    const controls = focusableControls();
    if (controls.length === 0) {
      event.preventDefault();
      dialog?.focus();
      return;
    }

    const first = controls[0];
    const last = controls[controls.length - 1];
    const active = document.activeElement;

    // Focus can fall to <body> on step changes; pull it back in.
    if (!active || !dialog.contains(active) || active === dialog) {
      event.preventDefault();
      (event.shiftKey ? last : first).focus();
      return;
    }
    if (event.shiftKey && active === first) {
      event.preventDefault();
      last.focus();
      return;
    }
    if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }
</script>

<svelte:window on:keydown={onKeydown} />

{#if show}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="modal-backdrop" style="z-index: {40 + depth * 10}" on:click={onClose}></div>
  <div
    class={`modal panel ${className}`.trim()}
    style="z-index: {45 + depth * 10}"
    role="dialog"
    aria-modal="true"
    aria-labelledby={titleId}
    aria-describedby={describedBy || undefined}
    tabindex="-1"
    bind:this={dialog}
  >
    <div class="modal-header">
      <div>
        {#if eyebrow}
          <p class="eyebrow">{eyebrow}</p>
        {/if}
        <h2 id={titleId} class="modal-title">{title}</h2>
        {#if subtitle}
          <p class="modal-subtitle">{subtitle}</p>
        {/if}
      </div>
      <slot name="header-actions" />
      <button class="btn icon-btn close-btn" type="button" aria-label={closeLabel} on:click={onClose}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
        </svg>
      </button>
    </div>
    <slot />
    {#if $$slots.footer}
      <div class="modal-footer">
        <slot name="footer" />
      </div>
    {/if}
  </div>
{/if}
