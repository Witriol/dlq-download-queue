<script>
  export let show = false;
  export let stepLabels = [];
  export let wizardStep = 0;
  export let wizardBusy = false;
  export let wizardError = '';
  export let wizardNotice = '';
  export let draft = {};
  export let outDirPresets = [];
  export let outDirFavorites = [];
  export let profileFields = [];
  export let tvmazeQuery = '';
  export let tvmazeResults = [];
  export let tvmazeBusy = false;
  export let selectedShow = null;
  export let visibleCandidates = [];
  export let previewTotalCandidates = 0;
  export let previewSeason = 1;
  export let onClose = () => {};
  export let onNext = () => {};
  export let onBack = () => {};
  export let onActivate = () => {};
  export let onFindShows = () => {};
  export let onChooseShow = () => {};
  export let onOpenBrowser = () => {};
  export let onRemoveFavorite = () => {};
  export let outputExample = () => '';
  export let prettyPolicy = (value) => value;
  export let candidateName = (candidate) => candidate?.name || '';
  export let reasons = () => [];
</script>

{#if show}
  <div class="modal-backdrop" role="button" tabindex="0" aria-label="Close series wizard" on:click={onClose} on:keydown={(e) => e.key === 'Escape' && onClose()}></div>
  <div class="modal panel modal-wide series-wizard" role="dialog" aria-modal="true" aria-labelledby="series-wizard-title">
    <div class="modal-header">
      <div><p class="eyebrow">New watcher</p><h2 id="series-wizard-title">Add a series</h2></div>
      <button class="btn icon-btn close-btn" type="button" aria-label="Close" on:click={onClose}>×</button>
    </div>
    <div class="series-steps" aria-label="Wizard progress">
      {#each stepLabels as label, index}<div class:current={wizardStep === index} class:complete={wizardStep > index}><span>{index + 1}</span>{label}</div>{/each}
    </div>
    {#if wizardError}<div class="series-alert error">{wizardError}</div>{/if}
    {#if wizardNotice}<div class="series-alert notice">{wizardNotice}</div>{/if}

    {#if wizardStep === 0}
      <div class="series-wizard-body">
        <div class="series-intro"><span class="step-kicker">01 / Reference</span><h3>Start with one episode you already trust</h3><p>We’ll derive a release profile from the filename. The reference file is never queued again automatically.</p></div>
        <div class="form-grid series-form-grid">
          <label for="series-reference">Webshare reference URL</label>
          <input id="series-reference" type="url" bind:value={draft.reference_url} placeholder="https://webshare.cz/#/file/..." autocomplete="off" />
          <label for="series-out-dir">Library root</label>
          {#if outDirFavorites.length}
            <div class="presets-row favorite-folders-row"><span class="presets-label">Favorites</span><div class="presets-list favorite-folders-list">{#each outDirFavorites as favorite}<div class:active={draft.out_dir === favorite} class="favorite-folder-chip"><button class="favorite-folder-btn" type="button" title={favorite} on:click={() => (draft.out_dir = favorite)}><span>{favorite}</span></button><button class="favorite-remove-btn" type="button" aria-label={`Remove favorite ${favorite}`} on:click={() => onRemoveFavorite(favorite)}>×</button></div>{/each}</div></div>
          {/if}
          {#if outDirPresets.length}<div class="presets-row"><span class="presets-label">Presets</span><div class="presets-list">{#each outDirPresets as preset}<button class="preset-btn" type="button" on:click={() => (draft.out_dir = preset)}>{preset}</button>{/each}</div></div>{/if}
          <div class="series-folder-input"><input id="series-out-dir" bind:value={draft.out_dir} placeholder="Choose a DATA_* folder" /><button class="btn ghost" type="button" on:click={onOpenBrowser}>Browse</button></div>
          <label for="series-folder">Series folder</label>
          <input id="series-folder" bind:value={draft.series_folder} placeholder="Derived from the selected show" />
          <label class="series-checkbox"><input type="checkbox" bind:checked={draft.organize_by_season} /> Create a season folder for each season</label>
        </div>
        <p class="field-hint">Only paths accepted by the server’s DATA_* presets are valid. Episodes will be stored as <code>{outputExample(draft.out_dir, draft.series_folder, draft.organize_by_season, draft.initial_season)}</code>.</p>
      </div>
    {:else if wizardStep === 1}
      <div class="series-wizard-body">
        <div class="series-intro"><span class="step-kicker">02 / Profile & show</span><h3>Review the profile, then pick the exact show</h3><p>Choose how each detected property is used. Required fields filter candidates; preferred fields affect ranking.</p></div>
        {#if draft.reference_filename}<div class="reference-file">{draft.reference_filename}</div>{/if}
        <div class="profile-grid">
          {#if profileFields.length === 0}<div class="profile-empty">No structured properties were returned. You can continue, but strict matching may need backend profile data.</div>{/if}
          {#each profileFields as field (field.key)}
            <div class="profile-field"><div><strong>{field.key.replaceAll('_', ' ')}</strong><small>{field.token || 'Detected value'}{#if field.confidence != null} · {Math.round(Number(field.confidence) * 100)}% confidence{/if}</small></div><span>{String(field.value || '—')}</span><select aria-label={`Matching mode for ${field.key}`} bind:value={field.mode}><option value="required">Required</option><option value="preferred">Preferred</option><option value="ignored">Ignore</option></select></div>
          {/each}
        </div>
        <div class="tvmaze-picker">
          <div class="tvmaze-search"><input type="search" bind:value={tvmazeQuery} placeholder="Search TVmaze by name or paste a TVmaze URL…" on:keydown={(e) => e.key === 'Enter' && onFindShows()} /><button class="btn" type="button" on:click={onFindShows} disabled={tvmazeBusy}>{tvmazeBusy ? 'Searching…' : 'Search'}</button></div>
          {#if tvmazeResults.length}<div class="tvmaze-results">{#each tvmazeResults as show (show.id)}<button class:selected={selectedShow?.id === show.id} class="tvmaze-result" type="button" on:click={() => onChooseShow(show)}><span>{show.name}</span><small>{show.premiered || 'Unknown year'} · {show.network?.name || 'TVmaze'}</small></button>{/each}</div>{/if}
          {#if selectedShow}<div class="selected-show"><span>Selected</span><strong>{selectedShow.name}</strong><small>TVmaze #{selectedShow.id}</small></div>{/if}
        </div>
      </div>
    {:else if wizardStep === 2}
      <div class="series-wizard-body">
        <div class="series-intro"><span class="step-kicker">03 / Policy</span><h3>Choose how far the watcher may automate</h3><p>Strict is the safe default: only an exact release-profile match is queued.</p></div>
        <div class="policy-grid"><label class:chosen={draft.fallback_policy === 'strict'}><input class="policy-choice" type="radio" bind:group={draft.fallback_policy} value="strict" /><strong>Strict</strong><span>Download only an exact match.</span></label><label class:chosen={draft.fallback_policy === 'balanced'}><input class="policy-choice" type="radio" bind:group={draft.fallback_policy} value="balanced" /><strong>Balanced</strong><span>After the preferred wait, allow defined alternatives.</span></label><label class:chosen={draft.fallback_policy === 'manual'}><input class="policy-choice" type="radio" bind:group={draft.fallback_policy} value="manual" /><strong>Manual fallback</strong><span>Queue exact matches automatically; flag other choices for review.</span></label></div>
        <div class="form-grid series-two-col"><div><label for="initial-mode">Initial episode mode</label><select id="initial-mode" bind:value={draft.initial_mode}><option value="template">Use reference as template · future only</option><option value="continue">Continue after reference episode</option><option value="specific">Start at a specific episode</option></select><p class="field-hint">Future only tracks episodes aired after this watcher is activated. Continue includes every episode after the reference, including episodes already aired.</p></div><div><label for="search-title">Webshare search title</label><input id="search-title" bind:value={draft.search_title} placeholder={selectedShow?.name || 'Series title'} /></div></div>
        {#if draft.initial_mode === 'specific'}<div class="form-grid series-two-col"><div><label for="initial-season">Start season</label><input id="initial-season" type="number" min="0" bind:value={draft.initial_season} /></div><div><label for="initial-episode">Start episode</label><input id="initial-episode" type="number" min="1" bind:value={draft.initial_episode} /></div></div>{/if}
        <div class="form-grid series-two-col"><div><label for="release-delay">Release delay (hours)</label><input id="release-delay" type="number" min="0" step="1" value={Math.round(draft.release_delay_seconds / 3600)} on:input={(e) => (draft.release_delay_seconds = Math.max(0, Number(e.currentTarget.value || 0) * 3600))} /><p class="field-hint">Wait this long after an episode airs before searching for a release.</p></div><div><label for="preferred-wait">Preferred wait (hours)</label><input id="preferred-wait" type="number" min="0" step="1" value={Math.round(draft.preferred_wait_seconds / 3600)} on:input={(e) => (draft.preferred_wait_seconds = Math.max(0, Number(e.currentTarget.value || 0) * 3600))} /><p class="field-hint">How long to wait for the preferred profile before Balanced may use an alternative or Manual fallback flags it for review. Strict never relaxes.</p></div></div>
      </div>
    {:else}
      <div class="series-wizard-body">
        <div class="series-intro"><span class="step-kicker">04 / Preview</span><h3>See what would be considered before activating</h3><p>Preview is read-only. Activation starts watching future TVmaze episodes and keeps all downloads in the normal queue.</p></div>
        <div class="review-strip"><div><span>Show</span><strong>{selectedShow?.name || '—'}</strong></div><div><span>Policy</span><strong>{prettyPolicy(draft.fallback_policy)}</strong></div><div><span>Folder</span><strong>{outputExample(draft.out_dir, draft.series_folder, draft.organize_by_season, previewSeason)}</strong></div></div>
        {#if visibleCandidates.length === 0}<div class="profile-empty">{previewTotalCandidates > 0 ? 'All returned candidates belong to a different series and were hidden.' : 'No candidates were returned for the next episode. This is safe to activate; the scheduler will retry after the configured delay.'}</div>{:else}<div class="candidate-list">{#each visibleCandidates as candidate}<div class="candidate-row"><div><strong>{candidateName(candidate)}</strong><small>{candidate.size_bytes ? `${Math.round(candidate.size_bytes / 1048576)} MB` : 'Size unavailable'}{#if candidate.score != null} · score {candidate.score}{/if}</small></div><span class:accepted={candidate.accepted !== false} class="candidate-decision">{candidate.accepted === false ? 'Rejected' : candidate.exact === false ? 'Alternative' : 'Exact'}</span><div class="candidate-reasons">{#each reasons(candidate) as reason}<span class:negative={reason.trim().startsWith('-')}>{reason}</span>{/each}</div></div>{/each}</div>{/if}
      </div>
    {/if}

    <div class="modal-actions series-wizard-actions"><button class="btn ghost" type="button" on:click={onClose}>Cancel</button><div class="actions">{#if wizardStep > 0}<button class="btn ghost" type="button" on:click={onBack} disabled={wizardBusy}>Back</button>{/if}{#if wizardStep < 3}<button class="btn primary" type="button" on:click={onNext} disabled={wizardBusy}>{wizardBusy ? 'Working…' : wizardStep === 0 ? 'Analyze reference' : wizardStep === 2 ? 'Preview candidates' : 'Continue'}</button>{:else}<button class="btn primary" type="button" on:click={onActivate} disabled={wizardBusy}>{wizardBusy ? 'Activating…' : 'Activate watcher'}</button>{/if}</div></div>
  </div>
{/if}
