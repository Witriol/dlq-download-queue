<script>
  import { tick } from 'svelte';

  export let show = false;
  export let stepLabels = [];
  export let wizardStep = 0;
  export let wizardFurthestStep = 0;
  export let wizardBusy = false;
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
  export let onGoToStep = () => {};
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

  let dialog;
  let wasShown = false;

  $: if (show && !wasShown) {
    wasShown = true;
    tick().then(() => dialog?.querySelector('[data-wizard-autofocus]')?.focus());
  } else if (!show) {
    wasShown = false;
  }

  function handleDialogKeydown(event) {
    if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
      return;
    }
    if (event.key !== 'Tab') return;
    const controls = [...dialog.querySelectorAll('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])')]
      .filter((element) => !element.hidden && element.getClientRects().length);
    if (!controls.length) return;
    const first = controls[0];
    const last = controls[controls.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

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

  /** "N MB" or a size-unavailable fallback, reused for both the picked and the other candidates. */
  function candidateSize(candidate) {
    return candidate.size_bytes ? `${Math.round(candidate.size_bytes / 1048576)} MB` : 'Size unavailable';
  }

  function localDate(value) {
    if (!value) return '';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString('cs-CZ', { year: 'numeric', month: 'numeric', day: 'numeric' });
  }
</script>

{#if show}
  <div class="modal-backdrop" role="button" tabindex="0" aria-label="Close series wizard" on:click={onClose} on:keydown={(e) => e.key === 'Escape' && onClose()}></div>
  <div bind:this={dialog} class="modal panel modal-wide series-wizard" role="dialog" aria-modal="true" aria-labelledby="series-wizard-title" tabindex="-1" on:keydown={handleDialogKeydown}>
    <div class="modal-header">
      <div><p class="eyebrow">New automation</p><h2 id="series-wizard-title">Add a series</h2><p class="series-wizard-subtitle">Match and download future episodes automatically.</p></div>
      <button class="btn icon-btn close-btn" type="button" aria-label="Close" on:click={onClose}>×</button>
    </div>
    <div class="series-steps" aria-label="Wizard progress">
      {#each stepLabels as label, index}<button type="button" class:current={wizardStep === index} class:complete={wizardFurthestStep > index} class:available={index <= wizardFurthestStep} aria-current={wizardStep === index ? 'step' : undefined} aria-label={`${label}${wizardFurthestStep > index ? ', complete' : ''}${index <= wizardFurthestStep ? ', available' : ''}`} disabled={wizardBusy || index > wizardFurthestStep} on:click={() => index !== wizardStep && index <= wizardFurthestStep && onGoToStep(index)}><span>{wizardFurthestStep > index ? '✓' : index + 1}</span><strong>{label}</strong></button>{/each}
    </div>
    <div class="series-wizard-scroll">
      {#if wizardError}<div class="series-alert error" role="alert">{wizardError}</div>{/if}
      {#if wizardNotice}<div class="series-alert notice" role="status">{wizardNotice}</div>{/if}

      {#if wizardStep === 0}
        <div class="series-wizard-body">
          <div class="series-intro"><span class="step-kicker">Step 1 of 2</span><h3>Start with one episode you trust</h3><p>We’ll learn the release quality and naming pattern from its filename, then find the matching show. The reference itself will not be downloaded again.</p></div>
          <div class="form-grid series-form-grid">
            <div class="wizard-field wizard-field-primary"><div class="field-heading"><label for="series-reference">Webshare reference URL</label><span>Required</span></div><input id="series-reference" data-wizard-autofocus type="url" value={draft.reference_url} on:input={(event) => setReference(event.currentTarget.value)} placeholder="https://webshare.cz/#/file/..." autocomplete="off" aria-describedby="series-reference-hint" /><p id="series-reference-hint" class="field-hint">Paste a file link for an episode whose quality you want to repeat.</p></div>
          </div>
        </div>
      {:else}
        <div class="series-wizard-body">
          <div class="series-intro"><span class="step-kicker">Step 2 of 2</span><h3>Review before activating</h3><p>Confirm the show, the folder and the policy, then check what the watcher would download right now.</p></div>

          <section class="wizard-section show-summary" aria-labelledby="show-match-heading">
            <div class="wizard-section-heading">
              <div><h4 id="show-match-heading">{selectedShow?.name || 'No show selected'}</h4>{#if selectedShow}<p>{selectedShow.premiered ? selectedShow.premiered.slice(0, 4) : 'Unknown year'} · {selectedShow.network?.name || 'TVmaze'}</p>{/if}</div>
              <button class="btn ghost tiny" type="button" on:click={() => (showShowSearch = !showShowSearch)}>{showShowSearch ? 'Cancel' : 'Change'}</button>
            </div>
            {#if showShowSearch}
              <div class="tvmaze-picker">
                <div class="tvmaze-search"><input id="tvmaze-search" type="search" bind:value={tvmazeQuery} placeholder="Series name or TVmaze URL" aria-label="Search TVmaze" on:keydown={(e) => e.key === 'Enter' && onFindShows()} /><button class="btn" type="button" on:click={onFindShows} disabled={tvmazeBusy}>{tvmazeBusy ? 'Searching…' : 'Search'}</button></div>
                {#if tvmazeResults.length}<div class="tvmaze-results">{#each tvmazeResults as result (result.id)}<button class:selected={selectedShow?.id === result.id} class="tvmaze-result" type="button" on:click={() => onChooseShow(result)}><span>{result.name}</span><small>{result.premiered || 'Unknown year'} · {result.network?.name || 'TVmaze'}</small></button>{/each}</div>{/if}
              </div>
            {/if}
          </section>

          <section class="wizard-section" aria-labelledby="folder-heading">
            <div class="wizard-section-heading"><div><h4 id="folder-heading">Folder</h4><p>Where new episodes will be saved.</p></div></div>
            <div class="wizard-section-body">
              {#if outDirFavorites.length}<div class="presets-row favorite-folders-row"><span class="presets-label">Favorites</span><div class="presets-list favorite-folders-list">{#each outDirFavorites as favorite}<div class:active={draft.out_dir === favorite} class="favorite-folder-chip"><button class="favorite-folder-btn" type="button" title={favorite} on:click={() => setOutputDirectory(favorite)}><span>{favorite}</span></button><button class="favorite-remove-btn" type="button" aria-label={`Remove favorite ${favorite}`} on:click={() => onRemoveFavorite(favorite)}>×</button></div>{/each}</div></div>{/if}
              {#if outDirPresets.length}<div class="presets-row"><span class="presets-label">Presets</span><div class="presets-list">{#each outDirPresets as preset}<button class="preset-btn" type="button" on:click={() => setOutputDirectory(preset)}>{preset}</button>{/each}</div></div>{/if}
              <div class="series-folder-input"><input id="series-out-dir" value={draft.out_dir} on:input={(event) => setOutputDirectory(event.currentTarget.value)} placeholder="Choose the final folder for this series" /><button class="btn ghost" type="button" on:click={onOpenBrowser}>Browse</button></div>
              <label class="series-checkbox"><input type="checkbox" bind:checked={draft.organize_by_season} /> Create a season folder for each season</label>
              <p class="field-hint">Episodes will be stored as <code>{outputExample(draft.out_dir, draft.series_folder, draft.organize_by_season, nextEpisode?.season || draft.initial_season)}</code>.</p>
            </div>
          </section>

          <section class="wizard-section" aria-labelledby="policy-heading">
            <div class="wizard-section-heading"><div><h4 id="policy-heading">If no exact match</h4><p>Choose what happens when no exact release is found.</p></div></div>
            <div class="wizard-section-body">
              <div class="form-grid series-two-col">
                <div><label for="fallback-policy">Policy</label><select id="fallback-policy" value={draft.fallback_policy} on:change={(event) => setPolicyValue('fallback_policy', event.currentTarget.value)}><option value="strict">Strict — exact match only</option><option value="balanced">Balanced — best alternative after a wait</option><option value="manual">Manual — ask me after a wait</option></select></div>
                {#if draft.fallback_policy !== 'strict'}<div><label for="preferred-wait">Wait</label><div class="input-with-suffix"><input id="preferred-wait" type="number" min="0" step="1" value={Math.round(draft.preferred_wait_seconds / 3600)} on:input={(event) => setPolicyValue('preferred_wait_seconds', Math.max(0, Number(event.currentTarget.value || 0) * 3600))} /><span>hours</span></div></div>{/if}
              </div>
            </div>
          </section>

          <section class="wizard-section" aria-labelledby="test-result-heading">
            <div class="wizard-section-heading">
              <div><h4 id="test-result-heading">Test result</h4><p>What the watcher would pick for the reference episode right now.</p></div>
              <button class="btn ghost tiny nowrap" type="button" on:click={onRetest} disabled={wizardBusy || !selectedShow}>{wizardBusy ? 'Testing…' : 'Re-test'}</button>
            </div>
            <div class="test-result-body">
              {#if !testRan}
                <div class="profile-empty">Run a test to see the release the watcher would pick.</div>
              {:else if pickedCandidate}
                <div class="candidate-row"><div><strong>{candidateName(pickedCandidate)}</strong><small>{candidateSize(pickedCandidate)}{#if pickedCandidate.score != null} · score {pickedCandidate.score}{/if}</small></div><span class:accepted={pickedCandidate.accepted !== false} class="candidate-decision">{pickedCandidate.exact === false ? 'Alternative' : 'Exact'}</span><div class="candidate-reasons">{#each reasons(pickedCandidate) as reason}<span class:negative={reason.trim().startsWith('-')}>{reason}</span>{/each}</div></div>
                {#if otherCandidates.length}<details class="other-matches"><summary>{otherCandidates.length} other match{otherCandidates.length === 1 ? '' : 'es'}</summary><div class="candidate-list">{#each otherCandidates as candidate}<div class="candidate-row"><div><strong>{candidateName(candidate)}</strong><small>{candidateSize(candidate)}{#if candidate.score != null} · score {candidate.score}{/if}</small></div><span class:accepted={candidate.accepted !== false} class="candidate-decision">{candidate.exact === false ? 'Alternative' : 'Exact'}</span><div class="candidate-reasons">{#each reasons(candidate) as reason}<span class:negative={reason.trim().startsWith('-')}>{reason}</span>{/each}</div></div>{/each}</div></details>{/if}
                {#if rejectedCount}<p class="field-hint">{rejectedCount} rejected</p>{/if}
              {:else}
                <div class="profile-empty">No accepted release for the reference episode yet. This is safe to activate; the scheduler keeps searching.</div>
              {/if}
              {#if nextEpisode}<p class="next-download-hint">First automatic download: <strong>{episodeCode(nextEpisode)} "{episodeTitle(nextEpisode)}"</strong> · {localDate(nextEpisode.air_timestamp)}</p>{/if}
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

    <div class="modal-actions series-wizard-actions"><div class="actions"><button class="btn ghost" type="button" on:click={onClose}>Cancel</button>{#if wizardStep > 0}<button class="btn ghost wizard-back" type="button" on:click={onBack} disabled={wizardBusy}><span aria-hidden="true">←</span> Back</button>{/if}</div>{#if wizardStep === 0}<button class="btn primary" type="button" on:click={onNext} disabled={wizardBusy}>{wizardBusy ? 'Analyzing…' : 'Analyze'}</button>{:else}<button class="btn primary" type="button" on:click={onActivate} disabled={wizardBusy}>{wizardBusy ? 'Activating…' : 'Activate watcher'}</button>{/if}</div>
  </div>
{/if}
