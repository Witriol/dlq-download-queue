<script>
  import { onMount } from 'svelte';
  import { addJobsBatch, browse, clearJobs, getEvents, getMeta, getSettings, listJobs, mkdir, postAction, updateSettings } from '$lib/api';
  import { humanBytes, humanDuration } from '$lib/format';
  import { countsFor, detectSite, parseUrls, sortJobs } from '$lib/job-utils';
  import JobsTable from '$lib/components/JobsTable.svelte';
  import AddJobsModal from '$lib/components/AddJobsModal.svelte';
  import LogsModal from '$lib/components/LogsModal.svelte';
  import ClearConfirmModal from '$lib/components/ClearConfirmModal.svelte';
  import SettingsModal from '$lib/components/SettingsModal.svelte';
  import BrowserModal from '$lib/components/BrowserModal.svelte';

  const statusOptions = ['', 'queued', 'resolving', 'downloading', 'paused', 'decrypting', 'completed', 'failed', 'decrypt_failed', 'deleted'];

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
  let addUrlsText = '';
  let addArchivePassword = '';
  let addResults = [];
  let addErrors = [];
  let adding = false;
  let addError = '';
  let outDirPresets = [];
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
  let settingsAutoDecrypt = true;
  let settingsError = '';
  let settingsSaving = false;

  let showBrowser = false;
  let browserPath = '';
  let browserDirs = [];
  let browserParent = '';
  let browserIsRoot = false;
  let browserError = '';
  let browserLoading = false;
  let browserNewFolderName = '';

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
    if (!addOutDir) {
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
      out_dir: addOutDir,
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
        auto_decrypt: settingsAutoDecrypt
      });
      settingsConcurrency = updated.concurrency;
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

  async function loadBrowser(path = '') {
    browserLoading = true;
    browserError = '';
    try {
      const result = await browse(path || undefined);
      browserPath = result.path;
      browserParent = result.parent;
      browserDirs = result.dirs;
      browserIsRoot = result.is_root;
      browserNewFolderName = '';
    } catch (err) {
      browserError = err instanceof Error ? err.message : String(err);
    } finally {
      browserLoading = false;
    }
  }

  async function createFolder() {
    if (!browserNewFolderName.trim()) return;
    browserError = '';
    const newPath = browserPath ? `${browserPath}/${browserNewFolderName}` : `/${browserNewFolderName}`;
    try {
      await mkdir(newPath);
      addOutDir = newPath;
      showBrowser = false;
      browserNewFolderName = '';
    } catch (err) {
      browserError = err instanceof Error ? err.message : String(err);
    }
  }

  function openBrowser() {
    showBrowser = true;
    loadBrowser();
  }

  function selectBrowserPath(path) {
    addOutDir = path;
    showBrowser = false;
  }

  $: {
    autoRefresh;
    refreshInterval;
    startTimer();
  }

  $: parsedUrls = parseUrls(addUrlsText);
  $: sortedJobs = sortJobs(jobs, sortKey, sortDir);
  $: addErrors = addResults.filter((result) => !result.ok);
  $: outDirPlaceholder = outDirPresets.length > 0 ? outDirPresets[0] : 'Select a preset or type a path';

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
      <div class="brand-title">
        <h1>DLQ Control Deck</h1>
        {#if metaVersion}
          <span class="badge badge-version">{metaVersion}</span>
        {/if}
      </div>
    </div>
    <div class="toolbar">
      <button class="btn ghost" on:click={openSettings}>Settings</button>
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
      <strong>{failedCount}</strong>
    </div>
    <div class="stat stat-speed">
      <span>Total Speed</span>
      <strong>{totalSpeedLabel}</strong>
    </div>
    <div class="stat stat-eta">
      <span>Overall ETA</span>
      <strong>{overallEtaLabel}</strong>
      {#if overallEtaHint}
        <small>{overallEtaHint}</small>
      {/if}
    </div>
  </div>

  {#if lastError}
    <p class="notice">Error: {lastError}</p>
  {/if}

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
  />
</div>

<button class="fab" on:click={() => (showAdd = true)} aria-label="Add jobs">
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M11 5h2v14h-2zM5 11h14v2H5z" />
  </svg>
</button>

<AddJobsModal
  show={showAdd}
  bind:addOutDir
  bind:addUrlsText
  bind:addArchivePassword
  {outDirPlaceholder}
  {outDirPresets}
  parsedUrlCount={parsedUrls.length}
  {adding}
  {addError}
  {metaError}
  {addErrors}
  onClose={() => (showAdd = false)}
  onOpenBrowser={openBrowser}
  onHandleFiles={handleFiles}
  onClearUrls={() => (addUrlsText = '')}
  onSubmit={handleAdd}
/>

<LogsModal
  show={showLogs}
  {logsJob}
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
  bind:settingsAutoDecrypt
  {settingsError}
  {settingsSaving}
  onClose={() => (showSettings = false)}
  onSave={saveSettings}
/>

<BrowserModal
  show={showBrowser}
  {browserPath}
  {browserDirs}
  {browserParent}
  {browserIsRoot}
  {browserError}
  {browserLoading}
  bind:browserNewFolderName
  onClose={() => (showBrowser = false)}
  onLoadBrowser={loadBrowser}
  onCreateFolder={createFolder}
  onSelectPath={selectBrowserPath}
/>
