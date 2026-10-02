<script>
  import { tick } from 'svelte';
  import Modal from '$lib/components/Modal.svelte';

  export let show = false;
  export let watches = [];
  export let loading = false;
  export let error = '';
  export let busy = '';
  export let focusWatchId = null;
  export let onClose = () => {};
  export let onChooseRelease = () => {};
  export let onSearchAgain = () => {};
  export let onRetryJobs = () => {};
  export let onViewLog = () => {};
  export let formatDate = () => '';
  export let episodeCode = () => '';

  const SEARCH_WINDOW_HINT = 'Searched for 72 hours without finding a matching release on Webshare.';
  const STRICT_NOT_FOUND_HINT = 'No release matched your quality profile exactly within 72 hours; the strict policy rejects alternatives. Change the policy or quality profile, or check the log.';
  const PAUSED_HINT = 'Resume the watch to search again.';

  $: sections = (watches || []).map((watch) => ({ watch, rows: buildRows(watch) }));
  let scrolledOnce = false;
  $: if (!show) {
    scrolledOnce = false;
  } else if (!loading && focusWatchId != null && !scrolledOnce) {
    scrolledOnce = true;
    scrollToWatch(focusWatchId);
  }

  async function scrollToWatch(id) {
    await tick();
    document.getElementById(`issues-watch-${id}`)?.scrollIntoView({ block: 'start' });
  }

  function buildRows(watch) {
    const rows = [];
    if (watch.last_error) {
      rows.push({ key: 'check_failed', kind: 'check_failed', label: 'Check failed', episodes: [] });
    }
    const groups = [
      ['choose_release', 'Choose release', (ep) => ep.kind === 'choose_release'],
      ['download_failed', 'Download failed', (ep) => ep.kind === 'download_failed' && ep.state !== 'decrypt_failed'],
      ['decrypt_failed', 'Extraction failed', (ep) => ep.kind === 'download_failed' && ep.state === 'decrypt_failed'],
      ['not_found', 'Not found', (ep) => ep.kind === 'not_found']
    ];
    for (const [kind, label, predicate] of groups) {
      const episodes = (watch.episodes || []).filter(predicate);
      if (episodes.length > 0) {
        rows.push({ key: kind, kind, label, episodes });
      }
    }
    return rows;
  }

  function range(episodes) {
    const first = episodeCode(episodes[0]);
    if (episodes.length === 1) {
      return episodes[0].episode_name ? `${first} · ${episodes[0].episode_name}` : first;
    }
    return `${first}–${episodeCode(episodes[episodes.length - 1])} · ${episodes.length} episodes`;
  }

  function candidateTotal(episodes) {
    return episodes.reduce((total, ep) => total + (Number(ep.candidate_count) || 0), 0);
  }

  function retryableIds(episodes) {
    return jobIds(episodes.filter((ep) => ep.job_status === 'failed'));
  }

  function retryStarted(episodes) {
    return episodes.some((ep) => ep.job_status && ep.job_status !== 'failed');
  }

  function failureReason(episodes) {
    const errors = [...new Set(episodes.map((ep) => ep.job_error).filter(Boolean))];
    return errors.length > 0 ? errors.join('; ') : 'no error reported';
  }

  function jobIds(episodes) {
    return episodes.map((ep) => ep.job_id).filter((id) => id != null && id !== 0);
  }
</script>

<Modal {show} eyebrow="Automations" title="Series issues" className="series-issues-dialog" {onClose}>
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    {#if loading && sections.length === 0}
      <div class="profile-empty" aria-live="polite">Loading issues…</div>
    {:else if sections.length === 0}
      <div class="profile-empty">No issues.</div>
    {:else}
      <div class="issues-list">
        {#each sections as { watch, rows } (watch.watch_id)}
          <section class="issues-watch" class:focused={String(watch.watch_id) === String(focusWatchId)} id={`issues-watch-${watch.watch_id}`} aria-labelledby={`issues-watch-title-${watch.watch_id}`}>
            <div class="issues-watch-header">
              <div>
                <span class="issues-watch-label">Series</span>
                <h3 id={`issues-watch-title-${watch.watch_id}`}>{watch.display_name}{#if !watch.enabled} <span class="series-status" data-status="paused">Paused</span>{/if}</h3>
                <small>{[watch.show_status, watch.fallback_policy ? `${watch.fallback_policy} policy` : '', watch.last_checked_at ? `last checked ${formatDate(watch.last_checked_at)}` : 'not checked yet'].filter(Boolean).join(' · ')}</small>
              </div>
              <button class="btn tiny ghost" type="button" on:click={() => onViewLog(watch)}>View log</button>
            </div>
            {#if !watch.enabled}<p class="muted issues-hint">{PAUSED_HINT}</p>{/if}
            {#each rows as row (row.key)}
              {@const rowBusy = busy === `${watch.watch_id}:${row.key}`}
              {@const ids = retryableIds(row.episodes)}
              <div class="issues-row">
                <div class="issues-row-main">
                  <span class="series-status" data-status={row.kind === 'not_found' ? 'not_found' : 'needs_attention'}>{row.label}</span>
                  {#if row.episodes.length > 0}<strong>{range(row.episodes)}</strong>{/if}
                  <p>
                    {#if row.kind === 'check_failed'}Last check failed: {watch.last_error}
                    {:else if row.kind === 'choose_release'}No exact match for your quality profile; {#if row.episodes.length > 1}{row.episodes.length} episodes have alternatives waiting for you to choose.{:else}{candidateTotal(row.episodes)} alternative{candidateTotal(row.episodes) === 1 ? ' is' : 's are'} waiting for you to choose.{/if}
                    {:else if row.kind === 'not_found'}{watch.fallback_policy === 'strict' ? STRICT_NOT_FOUND_HINT : SEARCH_WINDOW_HINT}
                    {:else if row.kind === 'download_failed'}Download failed: {failureReason(row.episodes)}
                    {:else}Archive could not be extracted; fix it in the Queue tab.{/if}
                  </p>
                  {#if row.episodes.length > 1}
                    <details class="issues-episodes">
                      <summary>Show episodes</summary>
                      <ul>{#each row.episodes as ep (ep.id)}<li><strong>{episodeCode(ep)}</strong> <span>{ep.episode_name || ''}</span> <small>{formatDate(ep.air_timestamp)}</small></li>{/each}</ul>
                    </details>
                  {/if}
                </div>
                {#if row.kind === 'choose_release'}
                  <button class="btn tiny primary" type="button" disabled={!watch.enabled || busy !== ''} on:click={() => onChooseRelease(watch)}>Choose release</button>
                {:else if row.kind === 'not_found' || row.kind === 'check_failed'}
                  <button class="btn tiny primary" type="button" disabled={!watch.enabled || busy !== ''} on:click={() => onSearchAgain(watch, row.key)}>{rowBusy ? 'Searching…' : 'Check now'}</button>
                {:else if row.kind === 'download_failed' && ids.length === 0 && retryStarted(row.episodes)}
                  <small class="muted">Retry started; status updates within a minute.</small>
                {:else if row.kind === 'download_failed'}
                  <button class="btn tiny primary" type="button" disabled={!watch.enabled || ids.length === 0 || busy !== ''} on:click={() => onRetryJobs(watch, row.key, ids)}>{rowBusy ? 'Retrying…' : 'Retry download'}</button>
                {/if}
              </div>
            {/each}
          </section>
        {/each}
      </div>
    {/if}
    <div slot="footer" class="modal-actions"><button class="btn ghost" type="button" on:click={onClose} disabled={busy !== ''}>Close</button></div>
</Modal>
