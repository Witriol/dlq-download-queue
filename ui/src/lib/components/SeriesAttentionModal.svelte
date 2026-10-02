<script>
  import CandidateRow from '$lib/components/CandidateRow.svelte';
  import Modal from '$lib/components/Modal.svelte';

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
  <Modal show eyebrow="Manual fallback" title={`Review ${watch.display_name}`} describedBy="series-attention-description" className="series-attention-dialog" {onClose}>
    <p id="series-attention-description" class="muted">Choose one of the persisted release alternatives below. The server validates the release before it is added to the normal download queue.</p>
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    {#if loading}
      <div class="profile-empty" aria-live="polite">Loading episodes needing attention…</div>
    {:else if episodes.length === 0}
      <div class="profile-empty">No episodes currently need attention. The watcher may have been resolved by another check.</div>
    {:else}
      <div class="attention-episodes">
        {#each episodes as episode (episode.id)}
          <section class="attention-episode" aria-labelledby={`attention-episode-${episode.id}`}>
            <div class="attention-episode-header"><div><h3 id={`attention-episode-${episode.id}`}>{episodeLabel(episode)}</h3><small>{formatDate(episode.air_timestamp)}</small></div><span class="series-status attention">Needs review</span></div>
            {#if episode.candidates.length === 0}
              <div class="profile-empty">No persisted alternatives are available for this episode.</div>
            {:else}
              <div class="candidate-list">
                {#each episode.candidates as candidate (candidate.ident)}
                  <CandidateRow {candidate} name={candidateName(candidate)} reasons={reasons(candidate)} showScore showReasons>
                    <button slot="actions" class="btn tiny primary" type="button" on:click={() => onQueueCandidate(episode, candidate)} disabled={!candidate.ident || busy !== ''}>{busy === `${episode.id}:${candidate.ident}` ? 'Queueing…' : 'Queue this release'}</button>
                  </CandidateRow>
                {/each}
              </div>
            {/if}
          </section>
        {/each}
      </div>
    {/if}
    <div slot="footer" class="modal-actions"><button class="btn ghost" type="button" on:click={onClose} disabled={busy !== ''}>Close</button></div>
  </Modal>
{/if}
