<script>
  export let watch = null;
  export let episodes = [];
  export let loading = false;
  export let error = '';
  export let busy = '';
  export let onClose = () => {};
  export let onQueueCandidate = () => {};
  export let episodeLabel = () => '';
  export let formatDate = () => '';
  export let candidateName = () => '';
  export let reasons = () => [];
</script>

{#if watch}
  <div class="modal-backdrop" role="button" tabindex="0" aria-label="Close attention review" on:click={onClose} on:keydown={(e) => (e.key === 'Escape' || e.key === 'Enter') && onClose()}></div>
  <div class="modal panel series-attention-dialog" role="dialog" aria-modal="true" aria-labelledby="series-attention-title" aria-describedby="series-attention-description">
    <div class="modal-header"><div><p class="eyebrow">Manual fallback</p><h2 id="series-attention-title">Review {watch.display_name}</h2></div><button class="btn icon-btn close-btn" type="button" aria-label="Close attention review" on:click={onClose}>×</button></div>
    <p id="series-attention-description" class="muted">Choose one of the persisted release alternatives below. The server validates the release before it is added to the normal download queue.</p>
    {#if error}<div class="series-alert error" role="alert">{error}</div>{/if}
    {#if loading}
      <div class="profile-empty" aria-live="polite">Loading episodes needing attention…</div>
    {:else if episodes.length === 0}
      <div class="profile-empty">No episodes currently need attention. The watcher may have been resolved by another check.</div>
    {:else}
      <div class="attention-episodes">{#each episodes as episode (episode.id)}<section class="attention-episode" aria-labelledby={`attention-episode-${episode.id}`}><div class="attention-episode-header"><div><h3 id={`attention-episode-${episode.id}`}>{episodeLabel(episode)}</h3><small>{formatDate(episode.air_timestamp)}</small></div><span class="series-status attention">Needs review</span></div>{#if episode.candidates.length === 0}<div class="profile-empty">No persisted alternatives are available for this episode.</div>{:else}<div class="candidate-list">{#each episode.candidates as candidate (candidate.ident)}<div class="candidate-row attention-candidate"><div><strong>{candidateName(candidate)}</strong><small>{candidate.size_bytes ? `${Math.round(candidate.size_bytes / 1048576)} MB` : 'Size unavailable'}{#if candidate.score != null} · score {candidate.score}{/if}</small></div><span class:accepted={candidate.accepted !== false} class="candidate-decision">{candidate.exact === false ? 'Alternative' : 'Exact'}</span><div class="candidate-reasons">{#each reasons(candidate) as reason}<span class:negative={reason.trim().startsWith('-')}>{reason}</span>{/each}</div><button class="btn tiny primary" type="button" on:click={() => onQueueCandidate(episode, candidate)} disabled={!candidate.ident || busy !== ''}>{busy === `${episode.id}:${candidate.ident}` ? 'Queueing…' : 'Queue this release'}</button></div>{/each}</div>{/if}</section>{/each}</div>
    {/if}
    <div class="modal-actions"><button class="btn ghost" type="button" on:click={onClose} disabled={busy !== ''}>Close</button></div>
  </div>
{/if}
