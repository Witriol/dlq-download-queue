<script>
  import { onMount } from 'svelte';
  import { fileName, folderPath, formatETA, formatProgress, formatSpeed, retryIn } from '$lib/format';
  import { displayStatus, displayStatusFilter, isWebshareJob } from '$lib/status';

  export let jobs = [];
  export let sortedJobs = [];
  export let statusOptions = [];
  export let statusFilter = '';
  export let includeDeleted = false;
  export let autoRefresh = true;
  export let refreshInterval = 3;
  export let sortKey = 'id';
  export let sortDir = 'desc';

  export let sortIndicator = () => '';
  export let onRefresh = () => {};
  export let onToggleSort = () => {};
  export let onSetSort = () => {};
  export let onToggleSortDirection = () => {};
  export let onRequestClear = () => {};
  export let onOpenLogs = () => {};
  export let onJobAction = () => {};
  export let onGroupAction = () => {};

  let showFilters = false;
  let isMobile = false;
  let groupedRows = [];

  const sortOptions = [
    { value: 'id', label: 'ID' },
    { value: 'status', label: 'Status' },
    { value: 'name', label: 'Name' },
    { value: 'progress', label: 'Progress' },
    { value: 'speed', label: 'Speed' },
    { value: 'eta', label: 'ETA' },
    { value: 'path', label: 'Path' },
    { value: 'url', label: 'URL' }
  ];

  const readyStatuses = new Set(['decrypting', 'decrypt_failed', 'completed']);

  $: groupedRows = buildGroupedRows(sortedJobs);

  function buildGroupedRows(list) {
    const membersByGroup = new Map();
    const groupOrder = [];
    for (const job of list) {
      const gid = typeof job.archive_group_id === 'string' ? job.archive_group_id : '';
      if (!gid || !job.archive_is_multipart) {
        continue;
      }
      let members = membersByGroup.get(gid);
      if (!members) {
        members = [];
        membersByGroup.set(gid, members);
        groupOrder.push(gid);
      }
      members.push(job);
    }

    const jobGroup = new Map();
    const firstJobIDs = new Set();
    const lastJobIDs = new Set();
    const summaryByGroup = new Map();
    for (const gid of groupOrder) {
      const members = membersByGroup.get(gid) || [];
      if (members.length === 0) {
        continue;
      }
      summaryByGroup.set(gid, summarizeGroup(gid, members));
      firstJobIDs.add(members[0].id);
      lastJobIDs.add(members[members.length - 1].id);
      for (const member of members) {
        jobGroup.set(member.id, gid);
      }
    }

    const rows = [];
    for (const job of list) {
      const gid = jobGroup.get(job.id) || '';
      const isGroupFirst = gid && firstJobIDs.has(job.id);
      if (gid && firstJobIDs.has(job.id)) {
        rows.push({ type: 'group', groupId: gid, summary: summaryByGroup.get(gid) });
      }
      rows.push({ type: 'job', job, groupId: gid, isGroupFirst, isGroupLast: gid && lastJobIDs.has(job.id) });
    }
    return rows;
  }

  function groupStatusLabel(statusTag) {
    switch (statusTag) {
      case 'downloading':
      case 'queued':
      case 'resolving':
      case 'paused':
        return 'waiting';
      case 'failed':
        return 'blocked';
      case 'decrypt_failed':
        return 'decrypt failed';
      default:
        return statusTag.replaceAll('_', ' ');
    }
  }

  function summarizeGroup(groupId, jobsInGroup) {
    const maxPartNumber = jobsInGroup.reduce((max, job) => {
      const part = Number(job.archive_part_number || 0);
      return part > max ? part : max;
    }, 0);
    const partsTotal = maxPartNumber > 0 ? maxPartNumber : jobsInGroup.length;
    const partsReady = jobsInGroup.filter((job) => readyStatuses.has(job.status)).length;
    const failedDownloads = jobsInGroup.filter((job) => job.status === 'failed').length;
    const decryptFailed = jobsInGroup.filter((job) => job.status === 'decrypt_failed').length;
    const activeDownloads = jobsInGroup.filter((job) => (
      job.status === 'queued' ||
      job.status === 'resolving' ||
      job.status === 'downloading' ||
      job.status === 'paused'
    )).length;
    const decrypting = jobsInGroup.filter((job) => job.status === 'decrypting').length;
    const completed = jobsInGroup.filter((job) => job.status === 'completed').length;

    let stateText = 'ready';
    let statusTag = 'decrypting';
    let decryptDisabledReason = '';
    if (failedDownloads > 0) {
      stateText = 'blocked by failed part download';
      statusTag = 'failed';
      decryptDisabledReason = 'retry failed part downloads first';
    } else if (activeDownloads > 0) {
      stateText = 'waiting for remaining part downloads';
      statusTag = 'downloading';
      decryptDisabledReason = 'wait for all part downloads to finish';
    } else if (decrypting > 0) {
      stateText = 'decrypting';
      statusTag = 'decrypting';
      decryptDisabledReason = 'decrypt already in progress';
    } else if (decryptFailed > 0) {
      stateText = 'decrypt failed';
      statusTag = 'decrypt_failed';
    } else if (completed === jobsInGroup.length) {
      stateText = 'completed';
      statusTag = 'completed';
      decryptDisabledReason = 'no decrypt failures in group';
    }

    const canRetryDecrypt = decryptFailed > 0 && failedDownloads === 0 && activeDownloads === 0 && decrypting === 0;
    if (!canRetryDecrypt && !decryptDisabledReason) {
      decryptDisabledReason = 'no decrypt failures in group';
    }

    return {
      groupId,
      label: jobsInGroup.find((job) => job.archive_group_label)?.archive_group_label || jobsInGroup[0].archive_group_id || groupId,
      partsTotal,
      partsReady,
      stateText,
      statusTag,
      canRetryDecrypt,
      decryptDisabledReason
    };
  }

  onMount(() => {
    if (typeof window === 'undefined') return undefined;
    const mediaQuery = window.matchMedia('(max-width: 960px)');
    const updateViewport = () => {
      isMobile = mediaQuery.matches;
    };
    updateViewport();
    if (typeof mediaQuery.addEventListener === 'function') {
      mediaQuery.addEventListener('change', updateViewport);
      return () => mediaQuery.removeEventListener('change', updateViewport);
    }
    mediaQuery.addListener(updateViewport);
    return () => mediaQuery.removeListener(updateViewport);
  });
</script>

<section class="panel">
  <div class="toolbar table-toolbar" style="margin-bottom: 12px;">
    <div class="toolbar-primary">
      <select bind:value={statusFilter} on:change={onRefresh}>
        {#each statusOptions as status}
          <option value={status}>{displayStatusFilter(status)}</option>
        {/each}
      </select>
      {#if isMobile}
        <button
          class="btn tiny ghost filter-toggle-btn"
          type="button"
          aria-expanded={showFilters}
          on:click={() => (showFilters = !showFilters)}
        >
          {showFilters ? 'Less' : 'More'}
          <span class="filter-chevron" aria-hidden="true">{showFilters ? '▴' : '▾'}</span>
        </button>
      {/if}
    </div>
    {#if !isMobile || showFilters}
      <div class="filter-panel">
        <div class="toolbar-group">
          <label class="small">
            <input type="checkbox" bind:checked={includeDeleted} on:change={onRefresh} /> include deleted
          </label>
          <label class="small">
            <input type="checkbox" bind:checked={autoRefresh} /> auto refresh
          </label>
          <label class="small">
            every
            <input class="num-input num-input-small" type="number" min="1" max="60" bind:value={refreshInterval} />
            s
          </label>
        </div>
        <div class="toolbar-group toolbar-sort">
          <label class="small" for="jobs-sort-key">sort</label>
          <select
            id="jobs-sort-key"
            value={sortKey}
            on:change={(event) => onSetSort(event.currentTarget.value)}
          >
            {#each sortOptions as option}
              <option value={option.value}>{option.label}</option>
            {/each}
          </select>
          <button class="btn tiny ghost sort-direction-btn" type="button" on:click={onToggleSortDirection}>
            {sortDir === 'asc' ? 'Ascending' : 'Descending'}
          </button>
        </div>
        <button class="btn danger tiny" on:click={onRequestClear}>Clear completed</button>
      </div>
    {/if}
  </div>

  {#if jobs.length === 0}
    <p class="notice">No jobs yet. Add URLs to start the queue.</p>
  {:else}
    <div class="table-wrap">
      <table class="table">
        <colgroup>
          <col class="col-id" />
          <col class="col-status" />
          <col class="col-name" />
          <col class="col-progress" />
          <col class="col-speed" />
          <col class="col-eta" />
          <col class="col-path" />
          <col class="col-url" />
          <col class="col-actions" />
        </colgroup>
        <thead>
          <tr>
            <th><button class="sort" on:click={() => onToggleSort('id')}>ID{sortIndicator('id')}</button></th>
            <th><button class="sort" on:click={() => onToggleSort('status')}>Status{sortIndicator('status')}</button></th>
            <th><button class="sort" on:click={() => onToggleSort('name')}>Name{sortIndicator('name')}</button></th>
            <th><button class="sort" on:click={() => onToggleSort('progress')}>Progress{sortIndicator('progress')}</button></th>
            <th><button class="sort" on:click={() => onToggleSort('speed')}>Speed{sortIndicator('speed')}</button></th>
            <th><button class="sort" on:click={() => onToggleSort('eta')}>ETA{sortIndicator('eta')}</button></th>
            <th><button class="sort" on:click={() => onToggleSort('path')}>Path{sortIndicator('path')}</button></th>
            <th><button class="sort" on:click={() => onToggleSort('url')}>URL{sortIndicator('url')}</button></th>
            <th class="actions-col">Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each groupedRows as row}
            {#if row.type === 'group'}
              <tr class="row-group" data-status={row.summary.statusTag}>
                <td colspan="9" class="cell-group">
                  <div class="group-inline">
                    <div class="group-text">
                      <strong class="group-title">{row.summary.label}</strong>
                      <span class="group-status" data-status={row.summary.statusTag}>{groupStatusLabel(row.summary.statusTag)}</span>
                      <span class="group-meta">parts {row.summary.partsReady}/{row.summary.partsTotal}</span>
                    </div>
                    <div class="actions row-actions">
                      <button
                        class="btn icon-btn action-btn action-retry"
                        type="button"
                        title={row.summary.canRetryDecrypt ? 'Retry decrypt (group)' : row.summary.decryptDisabledReason}
                        aria-label={`Retry decrypt group ${row.summary.label}`}
                        disabled={!row.summary.canRetryDecrypt}
                        on:click={() => onGroupAction(row.groupId, 'retry-decrypt')}
                      >
                        <svg viewBox="0 0 24 24" aria-hidden="true">
                          <path d="M17.7 6.3A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.8-4.3L13 11h8V3z" />
                        </svg>
                      </button>
                      <button
                        class="btn icon-btn action-btn action-remove"
                        type="button"
                        title="Remove group"
                        aria-label={`Remove group ${row.summary.label}`}
                        on:click={() => onGroupAction(row.groupId, 'remove')}
                      >
                        <svg viewBox="0 0 24 24" aria-hidden="true">
                          <path d="M7 7h10l-1 13H8zm2-3h6l1 2h4v2H4V6h4z" />
                        </svg>
                      </button>
                    </div>
                  </div>
                </td>
              </tr>
            {:else}
              {@const job = row.job}
              {@const inGroup = !!row.groupId}
              {@const progress = formatProgress(job)}
              {@const speed = formatSpeed(job)}
              {@const eta = formatETA(job)}
              <tr
                data-status={job.status}
                class:row-group-child={inGroup}
                class:row-group-child-first={inGroup && row.isGroupFirst}
                class:row-group-child-last={inGroup && row.isGroupLast}
              >
                <td class="cell-id" data-label="ID">{job.id}</td>
                <td class="cell-status" data-label="Status"><span class="status" data-status={job.status}>{displayStatus(job)}</span></td>
                <td class="cell-name" data-label="Name">{fileName(job)}</td>
                <td class="cell-progress" data-label="Progress">
                  <span class="metric-badge metric-progress">{progress}</span>
                  {#if (job.size_bytes ?? 0) > 0}
                    {@const pct = Math.min(100, ((job.bytes_done ?? 0) / job.size_bytes) * 100)}
                    <div class="progress-track">
                      <div class="progress-fill" style="width: {pct.toFixed(1)}%"></div>
                    </div>
                  {/if}
                </td>
                <td class="cell-speed" data-label="Speed">
                  <span class="metric-badge metric-speed" class:metric-empty={speed === '-'}>{speed}</span>
                </td>
                <td class="cell-eta" data-label="ETA">
                  <span class="metric-badge metric-eta" class:metric-empty={eta === '-'}>{eta}</span>
                </td>
                <td class="cell-path" data-label="Path">{folderPath(job)}</td>
                <td class="cell-url" data-label="URL">
                  <div>{job.url}</div>
                </td>
                <td class="actions-col" data-label="Actions">
                  <div class="actions row-actions">
                    {#if job.status === 'queued' || job.status === 'resolving' || job.status === 'downloading'}
                      {#if isWebshareJob(job)}
                        <button
                          class="btn icon-btn action-btn action-stop"
                          type="button"
                          title="Stop"
                          aria-label={`Stop job ${job.id}`}
                          on:click={() => onJobAction(job.id, 'pause')}
                        >
                          <svg viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M7 7h10v10H7z" />
                          </svg>
                        </button>
                      {:else}
                        <button
                          class="btn icon-btn action-btn action-pause"
                          type="button"
                          title="Pause"
                          aria-label={`Pause job ${job.id}`}
                          on:click={() => onJobAction(job.id, 'pause')}
                        >
                          <svg viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M7 5h4v14H7zm6 0h4v14h-4z" />
                          </svg>
                        </button>
                      {/if}
                    {/if}
                    {#if job.status === 'paused'}
                      {#if isWebshareJob(job)}
                        <button
                          class="btn icon-btn action-btn action-retry"
                          type="button"
                          title="Retry"
                          aria-label={`Retry job ${job.id}`}
                          on:click={() => onJobAction(job.id, 'retry')}
                        >
                          <svg viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M17.7 6.3A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.8-4.3L13 11h8V3z" />
                          </svg>
                        </button>
                      {:else}
                        <button
                          class="btn icon-btn action-btn action-resume"
                          type="button"
                          title="Resume"
                          aria-label={`Resume job ${job.id}`}
                          on:click={() => onJobAction(job.id, 'resume')}
                        >
                          <svg viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M8 5v14l11-7z" />
                          </svg>
                        </button>
                      {/if}
                    {/if}
                    {#if job.status === 'failed' || (job.status === 'decrypt_failed' && !inGroup)}
                      <button
                        class="btn icon-btn action-btn action-retry"
                        type="button"
                        title="Retry"
                        aria-label={`Retry job ${job.id}`}
                        on:click={() => onJobAction(job.id, 'retry')}
                      >
                        <svg viewBox="0 0 24 24" aria-hidden="true">
                          <path d="M17.7 6.3A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.8-4.3L13 11h8V3z" />
                        </svg>
                      </button>
                    {/if}
                    <button
                      class="btn icon-btn action-btn action-logs"
                      type="button"
                      title="Logs"
                      aria-label={`Open logs for job ${job.id}`}
                      on:click={() => onOpenLogs(job)}
                    >
                      <svg viewBox="0 0 24 24" aria-hidden="true">
                        <path d="M7 4h8l4 4v12H7z" stroke="currentColor" stroke-width="2" fill="none" />
                        <path d="M15 4v4h4M10 13h6M10 16h6" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" />
                      </svg>
                    </button>
                    {#if !inGroup}
                      <button
                        class="btn icon-btn action-btn action-remove"
                        type="button"
                        title="Remove"
                        aria-label={`Remove job ${job.id}`}
                        on:click={() => onJobAction(job.id, 'remove')}
                      >
                        <svg viewBox="0 0 24 24" aria-hidden="true">
                          <path d="M7 7h10l-1 13H8zm2-3h6l1 2h4v2H4V6h4z" />
                        </svg>
                      </button>
                    {/if}
                  </div>
                </td>
              </tr>
              {#if job.error_code}
                <tr class="row-error" data-status={job.status}>
                  <td colspan="9" class="cell-row-error">
                    <span class="error-inline">error: {job.error_code}{retryIn(job.next_retry_at) || (job.error ? ` · ${job.error}` : '')}</span>
                  </td>
                </tr>
              {/if}
            {/if}
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
