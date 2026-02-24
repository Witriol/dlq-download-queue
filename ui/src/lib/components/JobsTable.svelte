<script>
  import { fileName, folderPath, formatETA, formatProgress, formatSpeed } from '$lib/format';
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

  let showFilters = false;

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
</script>

<section class="panel">
  <div class="toolbar table-toolbar" style="margin-bottom: 12px;">
    <div class="toolbar-primary">
      <select bind:value={statusFilter} on:change={onRefresh}>
        {#each statusOptions as status}
          <option value={status}>{displayStatusFilter(status)}</option>
        {/each}
      </select>
      <button
        class="btn tiny ghost filter-toggle-btn"
        type="button"
        aria-expanded={showFilters}
        on:click={() => (showFilters = !showFilters)}
      >
        {showFilters ? 'Less' : 'More'}
        <span class="filter-chevron" aria-hidden="true">{showFilters ? '▴' : '▾'}</span>
      </button>
    </div>
    {#if showFilters}
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
          {#each sortedJobs as job}
            {@const progress = formatProgress(job)}
            {@const speed = formatSpeed(job)}
            {@const eta = formatETA(job)}
            <tr data-status={job.status}>
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
                {#if job.error_code}
                  <div class="badge">error: {job.error_code} {job.error}</div>
                {/if}
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
                  {#if job.status === 'failed' || job.status === 'decrypt_failed'}
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
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
