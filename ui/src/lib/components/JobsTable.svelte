<script>
  import { fileName, folderPath, formatETA, formatProgress, formatSpeed } from '$lib/format';

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
          <option value={status}>{status || 'all statuses'}</option>
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
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each sortedJobs as job}
            <tr>
              <td>{job.id}</td>
              <td><span class="status" data-status={job.status}>{job.status}</span></td>
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
              <td>
                <div class="actions">
                  {#if job.status === 'downloading'}
                    <button class="btn" on:click={() => onJobAction(job.id, 'pause')}>Pause</button>
                  {/if}
                  {#if job.status === 'paused'}
                    <button class="btn" on:click={() => onJobAction(job.id, 'resume')}>Resume</button>
                  {/if}
                  {#if job.status === 'failed' || job.status === 'decrypt_failed'}
                    <button class="btn" on:click={() => onJobAction(job.id, 'retry')}>Retry</button>
                  {/if}
                  <button class="btn ghost" on:click={() => onOpenLogs(job)}>Logs</button>
                  <button class="btn ghost" on:click={() => onJobAction(job.id, 'remove')}>Remove</button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
