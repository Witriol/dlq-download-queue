<script>
  import CandidateRow from '$lib/components/CandidateRow.svelte';
  import FolderPicker from '$lib/components/FolderPicker.svelte';
  import Modal from '$lib/components/Modal.svelte';

  export let show = false;
  export let wizardStep = 0;
  export let wizardBusy = false;
  export let wizardBusyAction = '';
  export let wizardError = '';
  export let wizardNotice = '';
  export let draft = {};
  export let folderEdited = false;
  export let showShowSearch = false;
  export let outDirPresets = [];
  export let outDirFavorites = [];
  export let profileFields = [];
  export let tvmazeQuery = '';
  export let tvmazeResults = [];
  export let tvmazeBusy = false;
  export let selectedShow = null;
  export let pickedCandidate = null;
  export let otherCandidates = [];
  export let rejectedCount = 0;
  export let testRan = false;
  export let nextEpisode = null;
  export let onClose = () => {};
  export let onNext = () => {};
  export let onBack = () => {};
  export let onActivate = () => {};
  export let onFindShows = () => {};
  export let onChooseShow = () => {};
  export let onRetest = () => {};
  export let onOpenBrowser = () => {};
  export let onRemoveFavorite = () => {};
  export let outputExample = () => '';
  export let episodeCode = () => '';
  export let episodeTitle = () => '';
  export let candidateName = (candidate) => candidate?.name || '';
  export let reasons = () => [];

  function setReference(value) {
    draft.reference_url = value;
  }

  function setOutputDirectory(value) {
    draft.out_dir = value;
    folderEdited = true;
  }

  function setProfileMode(field, value) {
    field.mode = value;
  }

  function setPolicyValue(key, value) {
    draft[key] = value;
  }

  function localDate(value) {
    if (!value) return '';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString('cs-CZ', { year: 'numeric', month: 'numeric', day: 'numeric' });
  }

  /** "<year> · <network or web channel>", omitting whichever part is missing. */
  function showSubline(tvShow) {
    const year = tvShow?.premiered ? tvShow.premiered.slice(0, 4) : '';
    const channel = tvShow?.network?.name || tvShow?.webChannel?.name || '';
    return [year, channel].filter(Boolean).join(' · ');
  }
</script>

<Modal {show} title="Add series" className="modal-wide series-wizard" {onClose}>
    <div class="series-wizard-scroll">
      {#if wizardError}<div class="alert error" role="alert">{wizardError}</div>{/if}
      {#if wizardNotice}<div class="alert notice" role="status">{wizardNotice}</div>{/if}

      {#if wizardStep === 0}
        <div class="series-wizard-body">
          <div class="form-grid series-form-grid">
            <div class="wizard-field wizard-field-primary"><div class="field-heading"><label for="series-reference">Webshare reference URL</label><span>Required</span></div><input id="series-reference" type="url" value={draft.reference_url} on:input={(event) => setReference(event.currentTarget.value)} placeholder="https://webshare.cz/#/file/…" autocomplete="off" aria-describedby="series-reference-hint" /><p id="series-reference-hint" class="field-hint">Paste a file link for an episode whose quality you want to repeat.</p></div>
          </div>
        </div>
      {:else}
        <div class="series-wizard-body">
          <section class="wizard-section show-summary" aria-labelledby="show-match-heading">
            <div class="wizard-section-heading">
              <div>
                <h4 id="show-match-heading">{selectedShow?.name || 'No show selected'}</h4>
                {#if selectedShow && showSubline(selectedShow)}<p>{showSubline(selectedShow)}</p>{/if}
                {#if nextEpisode}<p class="next-download-hint">Next: {episodeCode(nextEpisode)} "{episodeTitle(nextEpisode)}" · {localDate(nextEpisode.air_timestamp)}</p>{/if}
              </div>
              <button class="btn ghost tiny" type="button" on:click={() => (showShowSearch = !showShowSearch)}>{showShowSearch ? 'Cancel' : 'Change'}</button>
            </div>
            {#if showShowSearch}
              <div class="tvmaze-picker">
                <div class="tvmaze-search"><input id="tvmaze-search" type="search" bind:value={tvmazeQuery} placeholder="Series name or TVmaze URL" aria-label="Search TVmaze" on:keydown={(e) => e.key === 'Enter' && onFindShows()} /><button class="btn" type="button" on:click={onFindShows} disabled={tvmazeBusy}>{tvmazeBusy ? 'Searching…' : 'Search'}</button></div>
                {#if tvmazeResults.length}<div class="tvmaze-results">{#each tvmazeResults as result (result.id)}<button class:selected={selectedShow?.id === result.id} class="tvmaze-result" type="button" on:click={() => onChooseShow(result)}><span>{result.name}</span><small>{result.premiered || 'Unknown year'} · {result.network?.name || 'TVmaze'}</small></button>{/each}</div>{/if}
              </div>
            {/if}
          </section>

          <section class="wizard-section" aria-label="Download folder">
            <div class="wizard-section-body">
              <FolderPicker
                inputId="series-out-dir"
                value={draft.out_dir}
                presets={outDirPresets}
                favorites={outDirFavorites}
                placeholder="Choose the final folder for this series"
                onBrowse={onOpenBrowser}
                {onRemoveFavorite}
                onInput={setOutputDirectory}
              />
              <label class="series-checkbox"><input type="checkbox" bind:checked={draft.organize_by_season} /> Season folders → <code>{outputExample(draft.out_dir, draft.series_folder, draft.organize_by_season, nextEpisode?.season || draft.initial_season)}</code></label>
            </div>
          </section>

          <section class="wizard-section" aria-label="Fallback when no exact release">
            <div class="wizard-section-body">
              <p class="fallback-sentence">If the exact release isn't out{#if draft.fallback_policy !== 'strict'}{' '}after <span class="input-with-suffix fallback-hours"><input id="preferred-wait" type="number" min="0" step="1" value={Math.round(draft.preferred_wait_seconds / 3600)} on:input={(event) => setPolicyValue('preferred_wait_seconds', Math.max(0, Number(event.currentTarget.value || 0) * 3600))} aria-label="Hours to wait" /><span>h</span></span>{/if} <span class="fallback-arrow" aria-hidden="true">→</span> <select id="fallback-policy" value={draft.fallback_policy} on:change={(event) => setPolicyValue('fallback_policy', event.currentTarget.value)} aria-label="Fallback action"><option value="balanced">Download the best alternative</option><option value="manual">Ask me</option><option value="strict">Keep waiting for exact (max 3 days)</option></select></p>
              <p class="field-hint">Exact = same resolution, codec and release group as the reference.</p>
            </div>
          </section>

          <section class="wizard-section" aria-labelledby="test-result-heading">
            <div class="wizard-section-heading">
              <div><h4 id="test-result-heading">Test result</h4><p>What the watcher would pick for the reference episode right now.</p></div>
              <button class="btn ghost tiny nowrap" type="button" on:click={onRetest} disabled={wizardBusy || !selectedShow}>{wizardBusyAction === 'preview' ? 'Testing…' : 'Re-test'}</button>
            </div>
            <div class="test-result-body">
              {#if !testRan}
                <div class="profile-empty">Run a test to see the release the watcher would pick.</div>
              {:else if pickedCandidate}
                <CandidateRow candidate={pickedCandidate} name={candidateName(pickedCandidate)} />
                {#if otherCandidates.length}<details class="other-matches"><summary>{otherCandidates.length} other match{otherCandidates.length === 1 ? '' : 'es'}</summary><div class="candidate-list">{#each otherCandidates as candidate}<CandidateRow {candidate} name={candidateName(candidate)} reasons={reasons(candidate)} showScore showReasons />{/each}</div></details>{/if}
                {#if rejectedCount}<p class="field-hint">{rejectedCount} rejected</p>{/if}
              {:else}
                <div class="profile-empty">No accepted release for the reference episode yet. This is safe to activate; the scheduler keeps searching.</div>
              {/if}
            </div>
          </section>

          <details class="wizard-advanced">
            <summary>Advanced</summary>
            <div class="wizard-advanced-body">
              <section class="wizard-section" aria-labelledby="release-profile-heading">
                <div class="wizard-section-heading"><div><h4 id="release-profile-heading">Release profile</h4>{#if draft.reference_filename}<p class="reference-file">{draft.reference_filename}</p>{/if}</div><span>{profileFields.length} detected</span></div>
                <div class="profile-list">
                  {#if profileFields.length === 0}<div class="profile-empty">No structured qualities were detected. You can continue with title and episode matching.</div>{/if}
                  {#each profileFields as field (field.key)}
                    <div class="profile-field">
                      <div class="profile-field-name"><strong>{field.key.replaceAll('_', ' ')}</strong><small>{field.token || 'Detected value'}{#if field.confidence != null} · {Math.round(Number(field.confidence) * 100)}%{/if}</small></div>
                      <span class="profile-field-value">{String(field.value || '—')}</span>
                      <label class="profile-field-mode"><span>Matching</span><select aria-label={`Matching mode for ${field.key}`} value={field.mode} on:change={(event) => setProfileMode(field, event.currentTarget.value)}><option value="required">Required</option><option value="preferred">Preferred</option><option value="ignored">Ignore</option></select></label>
                    </div>
                  {/each}
                </div>
              </section>
              <div class="form-grid series-two-col">
                <div><label for="initial-mode">Start tracking</label><select id="initial-mode" value={draft.initial_mode} on:change={(event) => setPolicyValue('initial_mode', event.currentTarget.value)}><option value="template">Future episodes only</option><option value="continue">After the reference episode</option><option value="specific">A specific episode</option></select><p class="field-hint">Choose where DLQ begins checking this series.</p></div>
                <div><label for="search-title">Webshare search title</label><input id="search-title" value={draft.search_title} on:input={(event) => setPolicyValue('search_title', event.currentTarget.value)} placeholder={selectedShow?.name || 'Series title'} /><p class="field-hint">Adjust it when Webshare uses a different title.</p></div>
              </div>
              {#if draft.initial_mode === 'specific'}<div class="form-grid series-two-col episode-start-fields"><div><label for="initial-season">Season</label><input id="initial-season" type="number" min="1" value={draft.initial_season} on:input={(event) => setPolicyValue('initial_season', Number(event.currentTarget.value))} /></div><div><label for="initial-episode">Episode</label><input id="initial-episode" type="number" min="1" value={draft.initial_episode} on:input={(event) => setPolicyValue('initial_episode', Number(event.currentTarget.value))} /></div></div>{/if}
            </div>
          </details>
        </div>
      {/if}
    </div>

    <div slot="footer" class="modal-actions series-wizard-actions">
      <div class="actions">
        <button class="btn ghost" type="button" on:click={onClose}>Cancel</button>
        {#if wizardStep > 0}<button class="btn ghost wizard-back" type="button" on:click={onBack} disabled={wizardBusy}><span aria-hidden="true">←</span> Back</button>{/if}
      </div>
      {#if wizardStep === 0}
        <button class="btn primary" type="button" on:click={onNext} disabled={wizardBusy}>{wizardBusyAction === 'analyze' ? 'Analyzing…' : 'Analyze'}</button>
      {:else}
        <button class="btn primary" type="button" on:click={onActivate} disabled={wizardBusy}>{wizardBusyAction === 'activate' ? 'Activating…' : 'Activate watcher'}</button>
      {/if}
    </div>
</Modal>
