<script>
  import { onDestroy } from 'svelte';
  import Modal from './Modal.svelte';
  import { errMsg } from '$lib/errors';
  import { formatDateTime } from '$lib/format';

  export let show = false;
  export let title = 'Job events';
  export let subtitle = '';
  export let load = async (limit) => [];
  export let onClose = () => {};

  const DEFAULT_LIMIT = 50;
  const DEFAULT_INTERVAL_SECONDS = 3;

  let limit = DEFAULT_LIMIT;
  let autoRefresh = true;
  let interval = DEFAULT_INTERVAL_SECONDS;

  let events = [];
  let error = '';
  let loading = false;
  let loaded = false;

  let timer = null;
  let wasShown = false;
  // Bumped on open and close; responses from an older generation are dropped.
  let generation = 0;

  $: syncShow(show);
  // Deliberately does not read `load`: callers pass inline arrows.
  $: syncTimer(show, autoRefresh, interval);

  function syncShow(value) {
    if (value === wasShown) {
      return;
    }
    wasShown = value;
    generation += 1;
    events = [];
    error = '';
    loading = false;
    loaded = false;
    if (value) {
      refresh();
    }
  }

  function stopTimer() {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  function syncTimer(isShown, auto, seconds) {
    stopTimer();
    if (typeof window === 'undefined' || !isShown || !auto) {
      return;
    }
    const intervalMs = Math.max(1, Number(seconds) || 1) * 1000;
    timer = setInterval(refresh, intervalMs);
  }

  async function refresh() {
    if (loading) {
      return;
    }
    const current = generation;
    loading = true;
    try {
      const result = await load(Number(limit) || DEFAULT_LIMIT);
      if (current !== generation) {
        return;
      }
      events = Array.isArray(result) ? result : [];
      error = '';
      loaded = true;
    } catch (err) {
      if (current !== generation) {
        return;
      }
      error = errMsg(err);
    } finally {
      if (current === generation) {
        loading = false;
      }
    }
  }

  onDestroy(() => {
    generation += 1;
    stopTimer();
  });

  function formatEventLine(line) {
    const text = typeof line === 'string' ? line : '';
    const match = text.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\s+(\w+)\s+(.*)$/);
    if (!match) {
      return text;
    }
    const dt = new Date(match[1]);
    if (Number.isNaN(dt.getTime())) {
      return text;
    }
    const ts = formatDateTime(dt.toISOString());
    return `${ts} ${match[2]} ${match[3]}`;
  }
</script>

<Modal {show} {title} {subtitle} className="modal-logs" {onClose}>
  <div class="toolbar logs-toolbar" style="margin-bottom: 12px;">
    <label class="small">
      Tail
      <input class="num-input num-input-medium" type="number" min="1" max="500" bind:value={limit} />
    </label>
    <label class="small">
      <input type="checkbox" bind:checked={autoRefresh} /> Auto refresh
    </label>
    <label class="small">
      every
      <input class="num-input num-input-small" type="number" min="1" max="60" bind:value={interval} />
      s
    </label>
    <button class="btn ghost" type="button" on:click={refresh} disabled={loading}>Refresh</button>
  </div>
  {#if error}
    <p class="alert error" role="alert">{error}</p>
  {/if}
  <div class="result-list logs-list">
    {#if !loaded && !error}
      <div class="result-item">Loading…</div>
    {:else if loaded && events.length === 0}
      <div class="result-item">No events yet.</div>
    {:else}
      {#each events as line}
        <div class="result-item">{formatEventLine(line)}</div>
      {/each}
    {/if}
  </div>
</Modal>
