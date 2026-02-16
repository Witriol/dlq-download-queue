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

  export let sortIndicator = () => '';
  export let onRefresh = () => {};
  export let onToggleSort = () => {};
  export let onRequestClear = () => {};
  export let onOpenLogs = () => {};
  export let onJobAction = () => {};
</script>

<section class="panel">
  <div class="toolbar table-toolbar" style="margin-bottom: 12px;">
    <div class="toolbar-group">
      <select bind:value={statusFilter} on:change={onRefresh}>
        {#each statusOptions as status}
          <option value={status}>{displayStatusFilter(status)}</option>
        {/each}
      </select>
      <label class="small">
        <input type="checkbox" bind:checked={includeDeleted} on:change={onRefresh} /> include deleted
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
    <button class="btn danger tiny" on:click={onRequestClear}>Clear completed</button>
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
            <tr>
              <td>{job.id}</td>
              <td><span class="status" data-status={job.status}>{displayStatus(job)}</span></td>
              <td class="cell-name">{fileName(job)}</td>
              <td>{formatProgress(job)}</td>
              <td>{formatSpeed(job)}</td>
              <td>{formatETA(job)}</td>
              <td class="cell-path">{folderPath(job)}</td>
              <td class="cell-url">
                <div>{job.url}</div>
                {#if job.error_code}
                  <div class="badge">error: {job.error_code} {job.error}</div>
                {/if}
              </td>
              <td class="actions-col">
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
