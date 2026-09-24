<script>
  import { onMount } from 'svelte';
  import { addJobsBatch, clearJobs, getEvents, getMeta, getSettings, listJobs, listSeries, postAction, postGroupAction, updateSettings } from '$lib/api';
  import { displayStatus } from '$lib/status';
  import { humanBytes, humanDuration, localTimeZone } from '$lib/format';
  import { countsFor, detectSite, parseUrls, sortJobs } from '$lib/job-utils';
  import JobsTable from '$lib/components/JobsTable.svelte';
  import AddJobsModal from '$lib/components/AddJobsModal.svelte';
  import LogsModal from '$lib/components/LogsModal.svelte';
  import ClearConfirmModal from '$lib/components/ClearConfirmModal.svelte';
  import SettingsModal from '$lib/components/SettingsModal.svelte';
  import FolderBrowser from '$lib/components/FolderBrowser.svelte';
  import SeriesSection from '$lib/components/SeriesSection.svelte';

  const statusOptions = ['', 'queued', 'resolving', 'downloading', 'paused', 'decrypting', 'completed', 'failed', 'decrypt_failed', 'deleted'];
  const outDirFavoritesStorageKey = 'dlq.outDirFavorites';
  const seriesAttentionPollMs = 60_000;

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
  let activeTab = 'queue';
  let watcherModalOpen = false;

  let addOutDir = '';
  let addUrlsText = '';
  let addArchivePassword = '';
  let addResults = [];
  let addErrors = [];
  let adding = false;
  let addError = '';
  let outDirPresets = [];
  let outDirFavorites = [];
  let metaError = '';
  let metaVersion = '';
  let outDirPlaceholder = 'Select a preset or type a path';

  let showLogs = false;
  let logsJob = null;
  let logsEvents = [];
  let logsLimit = 50;
  let logsAutoRefresh = true;
  let logsInterval = 3;
  let logsError = '';
  let logsLoading = false;
  let logsTimer = null;

  let showSettings = false;
  let settingsConcurrency = 2;
  let settingsMaxAttempts = 5;
  let settingsAutoDecrypt = true;
  let settingsError = '';
  let settingsSaving = false;

  let showBrowser = false;
  let bodyLockState = null;

  let seriesWatches = [];
  let seriesPollTimer = null;

  $: counts = countsFor(jobs);
  $: activeCount = counts.queued + counts.resolving + counts.downloading + counts.paused + counts.decrypting;
  $: failedCount = counts.failed + counts.decrypt_failed;
  $: totalSpeed = jobs.reduce((sum, job) => {
    if (job.status !== 'downloading') return sum;
    return sum + (job.download_speed ?? 0);
  }, 0);
  $: totalSpeedLabel = totalSpeed > 0 ? `${humanBytes(totalSpeed)}/s` : '-';
  $: inProgressJobs = jobs.filter((job) => (
    job.status === 'queued' ||
    job.status === 'resolving' ||
    job.status === 'downloading' ||
    job.status === 'paused' ||
    job.status === 'decrypting'
  ));
  $: inProgressBytesRemaining = inProgressJobs.reduce((sum, job) => {
    const total = job.size_bytes ?? 0;
    if (total <= 0) return sum;
    const done = Math.max(0, Math.min(total, job.bytes_done ?? 0));
    return sum + Math.max(0, total - done);
  }, 0);
  $: inProgressKnownSizeCount = inProgressJobs.reduce((sum, job) => {
    const total = job.size_bytes ?? 0;
    return total > 0 ? sum + 1 : sum;
  }, 0);
  $: overallEtaSeconds = totalSpeed > 0 && inProgressBytesRemaining > 0
    ? Math.ceil(inProgressBytesRemaining / totalSpeed)
    : 0;
  $: overallEtaLabel = overallEtaSeconds > 0 ? humanDuration(overallEtaSeconds) : '-';
  $: overallEtaHint = inProgressJobs.length > 0 && inProgressKnownSizeCount < inProgressJobs.length
    ? `${inProgressKnownSizeCount}/${inProgressJobs.length} sized`
    : '';
  $: seriesAttentionCount = seriesWatches.reduce((total, watch) => {
    const attention = Number(watch.attention_count) || 0;
    return total + attention + (watch.last_error ? 1 : 0);
  }, 0);

  async function refresh() {
    lastError = '';
    try {
      const include = includeDeleted || statusFilter === 'deleted';
      jobs = await listJobs(statusFilter || undefined, include);
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    }
  }

  async function refreshSeriesAttention() {
    try {
      seriesWatches = await listSeries();
    } catch {
      // Series API may be unconfigured; keep the last known count rather than erroring the page.
    }
  }

  function stopSeriesPoll() {
    if (seriesPollTimer) {
      clearInterval(seriesPollTimer);
      seriesPollTimer = null;
    }
  }

  function startSeriesPoll() {
    stopSeriesPoll();
    seriesPollTimer = setInterval(refreshSeriesAttention, seriesAttentionPollMs);
  }

  function stopTimer() {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  function startTimer() {
    stopTimer();
    if (!autoRefresh || activeTab !== 'queue') return;
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

  function setSort(key) {
    if (sortKey === key) return;
    sortKey = key;
    sortDir = 'asc';
  }

  function toggleSortDirection() {
    sortDir = sortDir === 'asc' ? 'desc' : 'asc';
  }

  async function handleAdd() {
    addError = '';
    addResults = [];
    const urls = parseUrls(addUrlsText);
    const outDir = (addOutDir ?? '').trim();
    if (!outDir) {
      addError = 'Out directory is required.';
      return;
    }
    if (urls.length === 0) {
      addError = 'Add at least one URL.';
      return;
    }
    adding = true;
    addResults = await addJobsBatch({
      urls,
      out_dir: outDir,
      archive_password: addArchivePassword || undefined
    }, (url) => detectSite(url) || undefined);
    adding = false;
    await refresh();
    if (addResults.every((r) => r.ok)) {
      addUrlsText = '';
      addArchivePassword = '';
      addResults = [];
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

  async function handleGroupAction(groupId, action) {
    lastError = '';
    try {
      await postGroupAction(groupId, action);
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

  async function loadMeta() {
    metaError = '';
    try {
      const meta = await getMeta();
      outDirPresets = Array.isArray(meta.out_dir_presets) ? meta.out_dir_presets : [];
      metaVersion = typeof meta.version === 'string' ? meta.version : '';
    } catch (err) {
      metaError = err instanceof Error ? err.message : String(err);
      outDirPresets = [];
      metaVersion = '';
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

  async function loadSettings() {
    settingsError = '';
    try {
      const settings = await getSettings();
      settingsConcurrency = settings.concurrency;
      settingsMaxAttempts = settings.max_attempts;
      settingsAutoDecrypt = settings.auto_decrypt;
    } catch (err) {
      settingsError = err instanceof Error ? err.message : String(err);
    }
  }

  async function saveSettings() {
    settingsError = '';
    settingsSaving = true;
    try {
      const updated = await updateSettings({
        concurrency: settingsConcurrency,
        max_attempts: settingsMaxAttempts,
        auto_decrypt: settingsAutoDecrypt
      });
      settingsConcurrency = updated.concurrency;
      settingsMaxAttempts = updated.max_attempts;
      settingsAutoDecrypt = updated.auto_decrypt;
      showSettings = false;
    } catch (err) {
      settingsError = err instanceof Error ? err.message : String(err);
    } finally {
      settingsSaving = false;
    }
  }

  function openSettings() {
    showSettings = true;
    loadSettings();
  }

  function normalizeOutDirPath(path) {
    const trimmed = String(path ?? '').trim();
    if (!trimmed || trimmed === '/') return trimmed;
    return trimmed.replace(/\/+$/, '');
  }

  function loadOutDirFavorites() {
    if (typeof localStorage === 'undefined') return;
    try {
      const stored = JSON.parse(localStorage.getItem(outDirFavoritesStorageKey) || '[]');
      outDirFavorites = Array.isArray(stored)
        ? [...new Set(stored.map(normalizeOutDirPath).filter(Boolean))]
        : [];
    } catch {
      outDirFavorites = [];
    }
  }

  function saveOutDirFavorites() {
    if (typeof localStorage === 'undefined') return;
    localStorage.setItem(outDirFavoritesStorageKey, JSON.stringify(outDirFavorites));
  }

  function addOutDirFavorite(path) {
    const favorite = normalizeOutDirPath(path);
    if (!favorite || outDirFavorites.includes(favorite)) return;
    outDirFavorites = [favorite, ...outDirFavorites].slice(0, 20);
    saveOutDirFavorites();
  }

  function removeOutDirFavorite(path) {
    const favorite = normalizeOutDirPath(path);
    outDirFavorites = outDirFavorites.filter((item) => item !== favorite);
    saveOutDirFavorites();
  }

  function openBrowser() {
    showBrowser = true;
  }

  function selectBrowserPath(path) {
    addOutDir = path;
    showBrowser = false;
  }

  function tabFromURL() {
    if (typeof window === 'undefined') return 'queue';
    return new URL(window.location.href).searchParams.get('tab') === 'automations' ? 'automations' : 'queue';
  }

  function writeTabToURL(tab, replace = false) {
    if (typeof window === 'undefined') return;
    const url = new URL(window.location.href);
    url.searchParams.set('tab', tab);
    window.history[replace ? 'replaceState' : 'pushState']({}, '', url);
  }

  function selectTab(tab, focus = false, updateURL = true) {
    if (tab !== 'queue' && tab !== 'automations') return;
    // Keep the tab controls locked while a watcher dialog is open, but still
    // honour browser back/forward navigation. The popstate handler passes
    // updateURL=false, so navigation cannot be stranded behind a modal.
    if (watcherModalOpen && tab !== activeTab && updateURL) return;
    const changed = activeTab !== tab;
    activeTab = tab;
    if (updateURL && changed) writeTabToURL(tab);
    if (tab === 'queue') refresh();
    if (focus && typeof document !== 'undefined') {
      requestAnimationFrame(() => document.getElementById(`${tab}-tab`)?.focus());
    }
  }

  function handleTabKey(event) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    selectTab(activeTab === 'queue' ? 'automations' : 'queue', true);
  }

  function syncBodyScrollLock(isOpen) {
    if (typeof document === 'undefined') return;
    const body = document.body;
    if (isOpen && !bodyLockState) {
      bodyLockState = {
        overflow: body.style.overflow,
        paddingRight: body.style.paddingRight
      };
      const scrollbarWidth = window.innerWidth - document.documentElement.clientWidth;
      const hasStableScrollbarGutter = typeof CSS !== 'undefined' && CSS.supports?.('scrollbar-gutter: stable');
      body.style.overflow = 'hidden';
      if (!hasStableScrollbarGutter && scrollbarWidth > 0) body.style.paddingRight = `${scrollbarWidth}px`;
    } else if (!isOpen && bodyLockState) {
      body.style.overflow = bodyLockState.overflow;
      body.style.paddingRight = bodyLockState.paddingRight;
      bodyLockState = null;
    }
  }

  $: {
    autoRefresh;
    refreshInterval;
    activeTab;
    startTimer();
  }

  $: parsedUrls = parseUrls(addUrlsText);
  $: sortedJobs = sortJobs(jobs, sortKey, sortDir);
  $: addErrors = addResults.filter((result) => !result.ok);
  $: outDirPlaceholder = outDirFavorites[0] ?? outDirPresets[0] ?? 'Select a preset or type a path';

  $: {
    logsAutoRefresh;
    logsInterval;
    if (showLogs) startLogsTimer();
  }

  $: logsSubtitle = logsJob ? `Job #${logsJob.id} · ${displayStatus(logsJob)} · ${localTimeZone()}` : '';

  $: pageModalOpen = showAdd || showBrowser || showLogs || showSettings || showClearConfirm || watcherModalOpen;
  $: syncBodyScrollLock(pageModalOpen);

  onMount(() => {
    activeTab = tabFromURL();
    writeTabToURL(activeTab, true);
    const handlePopState = () => selectTab(tabFromURL(), false, false);
    window.addEventListener('popstate', handlePopState);
    refresh();
    loadMeta();
    loadOutDirFavorites();
    refreshSeriesAttention();
    startSeriesPoll();
    return () => {
      stopTimer();
      stopLogsTimer();
      stopSeriesPoll();
      window.removeEventListener('popstate', handlePopState);
      syncBodyScrollLock(false);
    };
  });
</script>

<main class="page">
  <header class="header">
    <div class="header-main">
      <div class="brand">
        <div class="brand-title">
          <h1>DLQ Control Deck</h1>
          {#if metaVersion}
            <span class="badge badge-version">{metaVersion}</span>
          {/if}
        </div>
      </div>
      <div class="app-tabs" role="tablist" aria-label="DLQ sections">
        <button class:active={activeTab === 'queue'} role="tab" aria-selected={activeTab === 'queue'} aria-controls="queue-panel" id="queue-tab" tabindex={activeTab === 'queue' ? 0 : -1} type="button" disabled={watcherModalOpen} on:click={() => selectTab('queue')} on:keydown={handleTabKey}>Queue</button>
        <button class:active={activeTab === 'automations'} role="tab" aria-selected={activeTab === 'automations'} aria-controls="automations-panel" id="automations-tab" tabindex={activeTab === 'automations' ? 0 : -1} type="button" disabled={watcherModalOpen} on:click={() => selectTab('automations')} on:keydown={handleTabKey}>
          Automations
          {#if seriesAttentionCount > 0}
            <span class="tab-badge" aria-label={`${seriesAttentionCount} automation${seriesAttentionCount === 1 ? '' : 's'} need${seriesAttentionCount === 1 ? 's' : ''} attention`}>{seriesAttentionCount}</span>
          {/if}
        </button>
      </div>
      {#if lastError}
        <span class="badge badge-error" role="alert" title={lastError}>Error: {lastError}</span>
      {/if}
    </div>
    <div class="toolbar">
      <button class="btn ghost" on:click={openSettings}>Settings</button>
    </div>
  </header>

  <div id="queue-panel" role="tabpanel" aria-labelledby="queue-tab" hidden={activeTab !== 'queue'}>
      <div class="stats">
        <div class="stat stat-total"><span>Total Jobs</span><strong>{jobs.length}</strong></div>
        <div class="stat stat-active"><span>Active</span><strong>{activeCount}</strong></div>
        <div class="stat stat-success"><span>Completed</span><strong>{counts.completed}</strong></div>
        <div class="stat stat-failed"><span>Failed</span><strong>{failedCount}</strong></div>
        <div class="stat stat-speed"><span>Total Speed</span><strong>{totalSpeedLabel}</strong></div>
        <div class="stat stat-eta"><span>Overall ETA</span><strong>{overallEtaLabel}</strong>{#if overallEtaHint}<small>{overallEtaHint}</small>{/if}</div>
      </div>
      <JobsTable
        {jobs}
        {sortedJobs}
        {statusOptions}
        {sortKey}
        {sortDir}
        bind:statusFilter
        bind:includeDeleted
        bind:autoRefresh
        bind:refreshInterval
        {sortIndicator}
        onRefresh={refresh}
        onToggleSort={toggleSort}
        onSetSort={setSort}
        onToggleSortDirection={toggleSortDirection}
        onRequestClear={() => (showClearConfirm = true)}
        onOpenLogs={openLogs}
        onJobAction={handleAction}
        onGroupAction={handleGroupAction}
      />
  </div>
  <div id="automations-panel" role="tabpanel" aria-labelledby="automations-tab" hidden={activeTab !== 'automations'}>
    {#if activeTab === 'automations'}
      <SeriesSection {outDirPresets} {outDirFavorites} active={activeTab === 'automations'} onAddFavorite={addOutDirFavorite} onRemoveFavorite={removeOutDirFavorite} onModalOpenChange={(open) => (watcherModalOpen = open)} onChanged={refresh} />
    {/if}
  </div>
</main>

{#if activeTab === 'queue'}
  <button class="fab" on:click={() => (showAdd = true)} aria-label="Add jobs">
    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M11 5h2v14h-2zM5 11h14v2H5z" /></svg>
  </button>
{/if}

<AddJobsModal
  show={showAdd}
  bind:addOutDir
  bind:addUrlsText
  bind:addArchivePassword
  {outDirPlaceholder}
  {outDirPresets}
  {outDirFavorites}
  parsedUrlCount={parsedUrls.length}
  {adding}
  {addError}
  {metaError}
  {addErrors}
  onClose={() => (showAdd = false)}
  onOpenBrowser={openBrowser}
  onRemoveFavorite={removeOutDirFavorite}
  onHandleFiles={handleFiles}
  onClearUrls={() => (addUrlsText = '')}
  onSubmit={handleAdd}
/>

<LogsModal
  show={showLogs}
  title="Job Events"
  subtitle={logsSubtitle}
  {logsEvents}
  bind:logsLimit
  bind:logsAutoRefresh
  bind:logsInterval
  {logsError}
  {logsLoading}
  onClose={closeLogs}
  onRefresh={refreshLogs}
/>

<ClearConfirmModal
  show={showClearConfirm}
  onClose={() => (showClearConfirm = false)}
  onConfirm={confirmClear}
/>

<SettingsModal
  show={showSettings}
  bind:settingsConcurrency
  bind:settingsMaxAttempts
  bind:settingsAutoDecrypt
  {settingsError}
  {settingsSaving}
  onClose={() => (showSettings = false)}
  onSave={saveSettings}
/>

<FolderBrowser
  bind:show={showBrowser}
  favoritePaths={outDirFavorites}
  onSelect={selectBrowserPath}
  onAddFavorite={addOutDirFavorite}
/>
