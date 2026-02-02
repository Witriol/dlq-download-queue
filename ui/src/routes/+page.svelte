<script context="module">
  export const ssr = false;
</script>

<script>
  import { onMount } from 'svelte';
  import { addJobsBatch, clearJobs, getEvents, getMeta, listJobs, postAction } from '$lib/api';
  import { filePath, formatETA, formatProgress, formatSpeed } from '$lib/format';

  const statusOptions = ['', 'queued', 'resolving', 'downloading', 'paused', 'completed', 'failed', 'deleted'];

  let jobs = [];
  let lastError = '';

  let statusFilter = '';
  let includeDeleted = false;
  let autoRefresh = true;
  let refreshInterval = 3;
  let timer = null;
  let showAdd = false;
  let sortKey = 'id';
  let sortDir = 'desc';
  let showClearConfirm = false;

  let addOutDir = '';
  let addMaxAttempts = 5;
  let addUrlsText = '';
  let addResults = [];
  let adding = false;
  let addError = '';
  let clipboardError = '';
  let outDirPresets = [];
  let metaError = '';

  let showLogs = false;
  let logsJob = null;
  let logsEvents = [];
  let logsLimit = 50;
  let logsAutoRefresh = true;
  let logsInterval = 3;
  let logsError = '';
  let logsLoading = false;
  let logsTimer = null;

  function parseUrls(text) {
    const lines = text.split(/\r?\n/);
    const out = [];
    for (const rawLine of lines) {
      const line = rawLine.trim();
      if (!line || line.startsWith('#')) continue;
      const tokens = line.split(/[\s,]+/).map((t) => t.trim()).filter(Boolean);
      out.push(...tokens);
    }
    return out;
  }

  function detectSite(url) {
    if (!url) return '';
    try {
      const host = new URL(url).hostname.toLowerCase();
      if (host.includes('mega.nz') || host.includes('mega.co.nz')) return 'mega';
      if (host.includes('webshare.cz')) return 'webshare';
      return '';
    } catch {
      const lower = url.toLowerCase();
      if (lower.includes('mega.nz') || lower.includes('mega.co.nz')) return 'mega';
      if (lower.includes('webshare.cz')) return 'webshare';
      return '';
    }
  }

  function countDetectedSites(urls) {
    const counts = { mega: 0, webshare: 0, unknown: 0 };
    for (const url of urls) {
      const site = detectSite(url);
      if (site === 'mega') counts.mega += 1;
      else if (site === 'webshare') counts.webshare += 1;
      else counts.unknown += 1;
    }
    return counts;
  }

  function countsFor(list) {
    const counts = {
      queued: 0,
      resolving: 0,
      downloading: 0,
      paused: 0,
      completed: 0,
      failed: 0,
      deleted: 0
    };
    for (const job of list) {
      if (counts[job.status] !== undefined) {
        counts[job.status] += 1;
      }
    }
    return counts;
  }

  $: counts = countsFor(jobs);
  $: activeCount = counts.queued + counts.resolving + counts.downloading + counts.paused;

  async function refresh() {
    lastError = '';
    try {
      const include = includeDeleted || statusFilter === 'deleted';
      jobs = await listJobs(statusFilter || undefined, include);
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    }
  }

  function stopTimer() {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  function startTimer() {
    stopTimer();
    if (!autoRefresh) return;
    const intervalMs = Math.max(1, Number(refreshInterval) || 1) * 1000;
    timer = setInterval(refresh, intervalMs);
  }

  function toggleSort(key) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc';
    } else {
      sortKey = key;
      sortDir = 'asc';
    }
  }

  function sortIndicator(key) {
    if (sortKey !== key) return '';
    return sortDir === 'asc' ? ' ↑' : ' ↓';
  }

  function getSortValue(job, key) {
    switch (key) {
      case 'id':
        return job.id;
      case 'status':
        return job.status;
      case 'progress': {
        const total = job.size_bytes ?? 0;
        const done = job.bytes_done ?? 0;
        if (total <= 0) return done;
        return done / total;
      }
      case 'speed':
        return job.download_speed ?? 0;
      case 'eta':
        return job.eta_seconds ?? 0;
      case 'path':
        return filePath(job);
      case 'url':
        return job.url || '';
      default:
        return job.id;
    }
  }

  function sortJobs(list) {
    const dir = sortDir === 'asc' ? 1 : -1;
    return [...list].sort((a, b) => {
      const av = getSortValue(a, sortKey);
      const bv = getSortValue(b, sortKey);
      if (typeof av === 'number' && typeof bv === 'number') {
        return (av - bv) * dir;
      }
      return String(av).localeCompare(String(bv)) * dir;
    });
  }

  async function handleAdd() {
    addError = '';
    addResults = [];
    clipboardError = '';
    const urls = parseUrls(addUrlsText);
    if (!addOutDir) {
      addError = 'Out directory is required.';
      return;
    }
    if (urls.length === 0) {
      addError = 'Add at least one URL.';
      return;
    }
    const maxAttempts = Math.max(1, Number(addMaxAttempts) || 5);
    adding = true;
    addResults = await addJobsBatch({
      urls,
      out_dir: addOutDir,
      max_attempts: maxAttempts
    }, (url) => detectSite(url) || undefined);
    adding = false;
    await refresh();
    if (addResults.every((r) => r.ok)) {
      showAdd = false;
    }
  }

  async function handleAction(id, action) {
    lastError = '';
    try {
      await postAction(id, action);
      await refresh();
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    }
  }

  async function confirmClear() {
    lastError = '';
    try {
      await clearJobs();
      showClearConfirm = false;
      await refresh();
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    }
  }

  async function handleFiles(event) {
    const input = event.currentTarget;
    if (!input.files || input.files.length === 0) return;
    const texts = [];
    for (const file of input.files) {
      texts.push(await file.text());
    }
    const appended = texts.join('\n');
    addUrlsText = addUrlsText ? `${addUrlsText}\n${appended}` : appended;
    input.value = '';
  }

  async function handlePasteClipboard() {
    clipboardError = '';
    if (!navigator.clipboard || !navigator.clipboard.readText) {
      clipboardError = 'Clipboard access is not available.';
      return;
    }
    try {
      const text = await navigator.clipboard.readText();
      if (!text) return;
      addUrlsText = addUrlsText ? `${addUrlsText}\n${text}` : text;
    } catch (err) {
      clipboardError = err instanceof Error ? err.message : String(err);
    }
  }

  async function loadMeta() {
    metaError = '';
    try {
      const meta = await getMeta();
      outDirPresets = Array.isArray(meta.out_dir_presets) ? meta.out_dir_presets : [];
    } catch (err) {
      metaError = err instanceof Error ? err.message : String(err);
      outDirPresets = [];
    }
  }

  async function refreshLogs() {
    if (!logsJob) return;
    logsLoading = true;
    logsError = '';
    try {
      logsEvents = await getEvents(logsJob.id, Number(logsLimit) || 50);
    } catch (err) {
      logsError = err instanceof Error ? err.message : String(err);
    } finally {
      logsLoading = false;
    }
  }

  function stopLogsTimer() {
    if (logsTimer) {
      clearInterval(logsTimer);
      logsTimer = null;
    }
  }

  function startLogsTimer() {
    stopLogsTimer();
    if (!showLogs || !logsAutoRefresh) return;
    const intervalMs = Math.max(1, Number(logsInterval) || 1) * 1000;
    logsTimer = setInterval(refreshLogs, intervalMs);
  }

  function openLogs(job) {
    logsJob = job;
    logsEvents = [];
    logsError = '';
    showLogs = true;
    refreshLogs();
    startLogsTimer();
  }

  function closeLogs() {
    showLogs = false;
    logsJob = null;
    logsEvents = [];
    logsError = '';
    stopLogsTimer();
  }

  $: {
    autoRefresh;
    refreshInterval;
    startTimer();
  }

  $: parsedUrls = parseUrls(addUrlsText);
  $: detectedCounts = countDetectedSites(parsedUrls);
  $: sortedJobs = sortJobs(jobs);

  $: {
    logsAutoRefresh;
    logsInterval;
    if (showLogs) startLogsTimer();
  }

  onMount(() => {
    refresh();
    loadMeta();
    return () => {
      stopTimer();
      stopLogsTimer();
    };
  });
</script>

<div class="page">
  <header class="header">
    <div class="brand">
      <h1>DLQ Control Deck</h1>
    </div>
    <div class="toolbar">
      <button class="btn danger" on:click={() => (showClearConfirm = true)}>Clear All</button>
    </div>
  </header>

  <div class="stats">
    <div class="stat stat-total">
      <span>Total Jobs</span>
      <strong>{jobs.length}</strong>
    </div>
    <div class="stat stat-active">
      <span>Active</span>
      <strong>{activeCount}</strong>
    </div>
    <div class="stat stat-success">
      <span>Completed</span>
      <strong>{counts.completed}</strong>
    </div>
    <div class="stat stat-failed">
      <span>Failed</span>
      <strong>{counts.failed}</strong>
    </div>
  </div>

  {#if lastError}
    <p class="notice">Error: {lastError}</p>
  {/if}

  <section class="panel">
    <div class="toolbar" style="margin-bottom: 12px;">
      <select bind:value={statusFilter} on:change={refresh}>
        {#each statusOptions as status}
          <option value={status}>{status || 'all statuses'}</option>
        {/each}
      </select>
      <label class="small">
        <input type="checkbox" bind:checked={includeDeleted} on:change={refresh} /> include deleted
      </label>
      <label class="small">
        <input type="checkbox" bind:checked={autoRefresh} /> auto refresh
      </label>
      <label class="small">
        every
        <input type="number" min="1" max="60" style="width: 64px" bind:value={refreshInterval} />
        s
      </label>
    </div>

    {#if jobs.length === 0}
      <p class="notice">No jobs yet. Add URLs to start the queue.</p>
    {:else}
      <table class="table">
        <thead>
          <tr>
            <th><button class="sort" on:click={() => toggleSort('id')}>ID{sortIndicator('id')}</button></th>
            <th><button class="sort" on:click={() => toggleSort('status')}>Status{sortIndicator('status')}</button></th>
            <th><button class="sort" on:click={() => toggleSort('progress')}>Progress{sortIndicator('progress')}</button></th>
            <th><button class="sort" on:click={() => toggleSort('speed')}>Speed{sortIndicator('speed')}</button></th>
            <th><button class="sort" on:click={() => toggleSort('eta')}>ETA{sortIndicator('eta')}</button></th>
            <th><button class="sort" on:click={() => toggleSort('path')}>Path{sortIndicator('path')}</button></th>
            <th><button class="sort" on:click={() => toggleSort('url')}>URL{sortIndicator('url')}</button></th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each sortedJobs as job}
            <tr>
              <td>{job.id}</td>
              <td><span class="status" data-status={job.status}>{job.status}</span></td>
              <td>{formatProgress(job)}</td>
              <td>{formatSpeed(job)}</td>
              <td>{formatETA(job)}</td>
              <td>{filePath(job)}</td>
              <td>
                <div>{job.url}</div>
                {#if job.error_code}
                  <div class="badge">error: {job.error_code} {job.error}</div>
                {/if}
              </td>
              <td>
                <div class="actions">
                  {#if job.status === 'downloading'}
                    <button class="btn" on:click={() => handleAction(job.id, 'pause')}>Pause</button>
                  {/if}
                  {#if job.status === 'paused'}
                    <button class="btn" on:click={() => handleAction(job.id, 'resume')}>Resume</button>
                  {/if}
                  {#if job.status === 'failed'}
                    <button class="btn" on:click={() => handleAction(job.id, 'retry')}>Retry</button>
                  {/if}
                  <button class="btn ghost" on:click={() => openLogs(job)}>Logs</button>
                  <button class="btn ghost" on:click={() => handleAction(job.id, 'remove')}>Remove</button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>
</div>

<button class="fab" on:click={() => (showAdd = true)} aria-label="Add jobs">
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M11 5h2v14h-2zM5 11h14v2H5z" />
  </svg>
</button>

{#if showAdd}
  <div class="modal-backdrop" on:click={() => (showAdd = false)}></div>
  <div class="modal panel" role="dialog" aria-modal="true">
    <div class="modal-header">
      <div>
        <h2 style="margin: 0;">Add Jobs</h2>
        <p class="notice">Auto-detects site per URL; unsupported URLs will be marked.</p>
      </div>
      <button class="btn ghost" on:click={() => (showAdd = false)}>Close</button>
    </div>
    <div class="form-grid">
      <div>
        <label>Out Directory</label>
        <input type="text" placeholder="/data/downloads" bind:value={addOutDir} />
      </div>
      <div class="actions">
        <span class="badge">Presets</span>
        {#if outDirPresets.length === 0}
          <span class="small">No presets available.</span>
        {:else}
          {#each outDirPresets as preset}
            <button class="btn ghost" type="button" on:click={() => (addOutDir = preset)}>{preset}</button>
          {/each}
        {/if}
      </div>
      <div>
        <label>Max Attempts</label>
        <input type="number" min="1" max="20" bind:value={addMaxAttempts} />
      </div>
      <div>
        <label>URLs</label>
        <textarea bind:value={addUrlsText} placeholder="https://...\nhttps://..."></textarea>
      </div>
      <div class="badge">
        URLs: {parsedUrls.length} | detected mega: {detectedCounts.mega}, webshare: {detectedCounts.webshare}, unknown: {detectedCounts.unknown}
      </div>
      <div class="actions">
        <label class="btn ghost">
          Import file(s)
          <input type="file" multiple accept=".txt" style="display: none" on:change={handleFiles} />
        </label>
        <button class="btn ghost" type="button" on:click={handlePasteClipboard}>Paste clipboard</button>
        <button class="btn ghost" type="button" on:click={() => (addUrlsText = '')}>Clear</button>
        <button class="btn primary" type="button" on:click={handleAdd} disabled={adding}>
          {adding ? 'Adding...' : 'Add Jobs'}
        </button>
      </div>
    </div>

    {#if addError}
      <p class="notice">{addError}</p>
    {/if}
    {#if clipboardError}
      <p class="notice">Clipboard: {clipboardError}</p>
    {/if}
    {#if metaError}
      <p class="notice">Presets: {metaError}</p>
    {/if}

    {#if addResults.length > 0}
      <div class="divider"></div>
      <div class="result-list">
        {#each addResults as result}
          <div class="result-item">
            {#if result.ok}
              [OK] {result.url} -> id {result.id}
            {:else}
              [ERR] {result.url} -> {result.error}
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/if}

{#if showLogs}
  <div class="modal-backdrop" on:click={closeLogs}></div>
  <div class="modal panel" role="dialog" aria-modal="true">
    <div class="modal-header">
      <div>
        <h2 style="margin: 0;">Job Events</h2>
        {#if logsJob}
          <p class="notice">Job #{logsJob.id} · {logsJob.status}</p>
        {/if}
      </div>
      <button class="btn ghost" on:click={closeLogs}>Close</button>
    </div>
    <div class="toolbar" style="margin-bottom: 12px;">
      <label class="small">
        Tail
        <input type="number" min="1" max="500" style="width: 90px" bind:value={logsLimit} />
      </label>
      <label class="small">
        <input type="checkbox" bind:checked={logsAutoRefresh} /> auto refresh
      </label>
      <label class="small">
        every
        <input type="number" min="1" max="60" style="width: 64px" bind:value={logsInterval} />
        s
      </label>
      <button class="btn ghost" on:click={refreshLogs} disabled={logsLoading}>Refresh</button>
    </div>
    {#if logsError}
      <p class="notice">Logs: {logsError}</p>
    {/if}
    <div class="result-list" style="max-height: 420px;">
      {#if logsEvents.length === 0}
        <div class="result-item">No events yet.</div>
      {:else}
        {#each logsEvents as line}
          <div class="result-item">{line}</div>
        {/each}
      {/if}
    </div>
  </div>
{/if}

{#if showClearConfirm}
  <div class="modal-backdrop" on:click={() => (showClearConfirm = false)}></div>
  <div class="modal panel" role="dialog" aria-modal="true">
    <div class="modal-header">
      <div>
        <h2 style="margin: 0;">Clear All Jobs</h2>
        <p class="notice">This will delete all jobs and events. This cannot be undone.</p>
      </div>
      <button class="btn ghost" on:click={() => (showClearConfirm = false)}>Close</button>
    </div>
    <div class="actions">
      <button class="btn danger" on:click={confirmClear}>Yes, clear all</button>
      <button class="btn ghost" on:click={() => (showClearConfirm = false)}>Cancel</button>
    </div>
  </div>
{/if}
