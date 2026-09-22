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
  export let onGoToStep = () => {};
  export let onInvalidateFrom = () => {};
  export let onActivate = () => {};
  export let onFindShows = () => {};
  export let onChooseShow = () => {};
  export let onOpenBrowser = () => {};
  export let onRemoveFavorite = () => {};
  export let outputExample = () => '';
  export let prettyPolicy = (value) => value;
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
    onInvalidateFrom(0);
  }

  function setOutputDirectory(value) {
    draft.out_dir = value;
    // The destination affects the final preview, but not reference analysis or
    // the selected show. Keep those completed steps available.
    onInvalidateFrom(2);
  }

  function setProfileMode(field, value) {
    field.mode = value;
    onInvalidateFrom(2);
  }

  function setPolicyValue(key, value) {
    draft[key] = value;
    onInvalidateFrom(2);
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
          <div class="series-intro"><span class="step-kicker">Step 1 of 4</span><h3>Start with one episode you trust</h3><p>We’ll learn the release quality and naming pattern from its filename. The reference itself will not be downloaded again.</p></div>
          <div class="form-grid series-form-grid">
            <div class="wizard-field wizard-field-primary"><div class="field-heading"><label for="series-reference">Webshare reference URL</label><span>Required</span></div><input id="series-reference" data-wizard-autofocus type="url" value={draft.reference_url} on:input={(event) => setReference(event.currentTarget.value)} placeholder="https://webshare.cz/#/file/..." autocomplete="off" aria-describedby="series-reference-hint" /><p id="series-reference-hint" class="field-hint">Paste a file link for an episode whose quality you want to repeat.</p></div>
            <div class="wizard-field">
              <div class="field-heading"><label for="series-out-dir">Download folder</label><span>Required</span></div>
              {#if outDirFavorites.length}
                <div class="presets-row favorite-folders-row"><span class="presets-label">Favorites</span><div class="presets-list favorite-folders-list">{#each outDirFavorites as favorite}<div class:active={draft.out_dir === favorite} class="favorite-folder-chip"><button class="favorite-folder-btn" type="button" title={favorite} on:click={() => setOutputDirectory(favorite)}><span>{favorite}</span></button><button class="favorite-remove-btn" type="button" aria-label={`Remove favorite ${favorite}`} on:click={() => onRemoveFavorite(favorite)}>×</button></div>{/each}</div></div>
              {/if}
              {#if outDirPresets.length}<div class="presets-row"><span class="presets-label">Presets</span><div class="presets-list">{#each outDirPresets as preset}<button class="preset-btn" type="button" on:click={() => setOutputDirectory(preset)}>{preset}</button>{/each}</div></div>{/if}
              <div class="series-folder-input"><input id="series-out-dir" value={draft.out_dir} on:input={(event) => setOutputDirectory(event.currentTarget.value)} placeholder="Choose the final folder for this series" /><button class="btn ghost" type="button" on:click={onOpenBrowser}>Browse</button></div>
              <label class="series-checkbox"><input type="checkbox" bind:checked={draft.organize_by_season} /> Create a season folder for each season</label>
              <p class="field-hint">Episodes will be stored as <code>{outputExample(draft.out_dir, draft.series_folder, draft.organize_by_season, draft.initial_season)}</code>.</p>
            </div>
          </div>
        </div>
      {:else if wizardStep === 1}
        <div class="series-wizard-body">
          <div class="series-intro"><span class="step-kicker">Step 2 of 4</span><h3>Confirm the profile and show</h3><p>Decide which detected qualities matter, then choose the matching series.</p></div>
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
          <section class="wizard-section" aria-labelledby="show-match-heading">
            <div class="wizard-section-heading"><div><h4 id="show-match-heading">Series match</h4><p>Choose one result from TVmaze.</p></div>{#if selectedShow}<span class="selection-confirmed">Selected</span>{/if}</div>
            <div class="tvmaze-picker">
              <div class="tvmaze-search"><input id="tvmaze-search" type="search" bind:value={tvmazeQuery} placeholder="Series name or TVmaze URL" aria-label="Search TVmaze" on:keydown={(e) => e.key === 'Enter' && onFindShows()} /><button class="btn" type="button" on:click={onFindShows} disabled={tvmazeBusy}>{tvmazeBusy ? 'Searching…' : 'Search'}</button></div>
              {#if tvmazeResults.length}<div class="tvmaze-results">{#each tvmazeResults as show (show.id)}<button class:selected={selectedShow?.id === show.id} class="tvmaze-result" type="button" on:click={() => onChooseShow(show)}><span>{show.name}</span><small>{show.premiered || 'Unknown year'} · {show.network?.name || 'TVmaze'}</small></button>{/each}</div>{/if}
              {#if selectedShow}<div class="selected-show"><strong>{selectedShow.name}</strong><small>TVmaze #{selectedShow.id}</small></div>{/if}
            </div>
          </section>
        </div>
      {:else if wizardStep === 2}
        <div class="series-wizard-body">
          <div class="series-intro"><span class="step-kicker">Step 3 of 4</span><h3>Set the automation policy</h3><p>Choose what happens when an exact release is unavailable.</p></div>
          <div class="policy-grid" aria-label="Fallback policy"><label class:chosen={draft.fallback_policy === 'strict'}><input class="policy-choice" type="radio" name="fallback-policy" checked={draft.fallback_policy === 'strict'} on:change={() => setPolicyValue('fallback_policy', 'strict')} value="strict" /><strong>Strict</strong><span>Only an exact profile match downloads.</span></label><label class:chosen={draft.fallback_policy === 'balanced'}><input class="policy-choice" type="radio" name="fallback-policy" checked={draft.fallback_policy === 'balanced'} on:change={() => setPolicyValue('fallback_policy', 'balanced')} value="balanced" /><strong>Balanced</strong><span>Use the best alternative after a wait.</span></label><label class:chosen={draft.fallback_policy === 'manual'}><input class="policy-choice" type="radio" name="fallback-policy" checked={draft.fallback_policy === 'manual'} on:change={() => setPolicyValue('fallback_policy', 'manual')} value="manual" /><strong>Manual</strong><span>Keep alternatives for your review.</span></label></div>
          <section class="wizard-section policy-settings" aria-label="Automation settings">
            <div class="form-grid series-two-col"><div><label for="initial-mode">Start tracking</label><select id="initial-mode" value={draft.initial_mode} on:change={(event) => setPolicyValue('initial_mode', event.currentTarget.value)}><option value="template">Future episodes only</option><option value="continue">After the reference episode</option><option value="specific">A specific episode</option></select><p class="field-hint">Choose where DLQ begins checking this series.</p></div><div><label for="search-title">Webshare search title</label><input id="search-title" value={draft.search_title} on:input={(event) => setPolicyValue('search_title', event.currentTarget.value)} placeholder={selectedShow?.name || 'Series title'} /><p class="field-hint">Adjust it when Webshare uses a different title.</p></div></div>
            {#if draft.initial_mode === 'specific'}<div class="form-grid series-two-col episode-start-fields"><div><label for="initial-season">Season</label><input id="initial-season" type="number" min="1" value={draft.initial_season} on:input={(event) => setPolicyValue('initial_season', Number(event.currentTarget.value))} /></div><div><label for="initial-episode">Episode</label><input id="initial-episode" type="number" min="1" value={draft.initial_episode} on:input={(event) => setPolicyValue('initial_episode', Number(event.currentTarget.value))} /></div></div>{/if}
            {#if draft.fallback_policy === 'balanced'}<div class="preferred-wait-field"><div><label for="preferred-wait">Alternative wait</label><p class="field-hint">After this time, the best non-exact release can download.</p></div><div class="input-with-suffix"><input id="preferred-wait" type="number" min="0" step="1" value={Math.round(draft.preferred_wait_seconds / 3600)} on:input={(e) => setPolicyValue('preferred_wait_seconds', Math.max(0, Number(e.currentTarget.value || 0) * 3600))} /><span>hours</span></div></div>{/if}
            <div class="schedule-note"><strong>Search schedule</strong><span>Starts 2h after airtime</span><span>Up to 4 searches</span>{#if draft.fallback_policy === 'balanced'}<span>Final check at alternative wait</span>{:else}<span>2h between retries</span>{/if}</div>
          </section>
        </div>
      {:else}
        <div class="series-wizard-body">
        <div class="series-intro"><span class="step-kicker">Step 4 of 4</span><h3>Review before activating</h3><p>This preview is read-only. Future matches will enter the normal download queue.</p></div>
        <div class="review-strip"><div><span>Show</span><strong>{selectedShow?.name || '—'}</strong></div><div><span>Policy</span><strong>{prettyPolicy(draft.fallback_policy)}</strong></div><div><span>Folder</span><strong>{outputExample(draft.out_dir, draft.series_folder, draft.organize_by_season, previewSeason)}</strong></div></div>
        {#if visibleCandidates.length === 0}<div class="profile-empty">{previewTotalCandidates > 0 ? 'All returned candidates belong to a different series and were hidden.' : 'No candidates were returned for the next episode. This is safe to activate; the scheduler will retry after the configured delay.'}</div>{:else}<div class="candidate-list">{#each visibleCandidates as candidate}<div class="candidate-row"><div><strong>{candidateName(candidate)}</strong><small>{candidate.size_bytes ? `${Math.round(candidate.size_bytes / 1048576)} MB` : 'Size unavailable'}{#if candidate.score != null} · score {candidate.score}{/if}</small></div><span class:accepted={candidate.accepted !== false} class="candidate-decision">{candidate.accepted === false ? 'Rejected' : candidate.exact === false ? 'Alternative' : 'Exact'}</span><div class="candidate-reasons">{#each reasons(candidate) as reason}<span class:negative={reason.trim().startsWith('-')}>{reason}</span>{/each}</div></div>{/each}</div>{/if}
        </div>
      {/if}
    </div>

    <div class="modal-actions series-wizard-actions"><div class="actions"><button class="btn ghost" type="button" on:click={onClose}>Cancel</button>{#if wizardStep > 0}<button class="btn ghost wizard-back" type="button" on:click={onBack} disabled={wizardBusy}><span aria-hidden="true">←</span> Back</button>{/if}</div>{#if wizardStep < 3}<button class="btn primary" type="button" on:click={onNext} disabled={wizardBusy}>{wizardBusy ? 'Working…' : wizardStep < wizardFurthestStep ? 'Continue' : wizardStep === 0 ? 'Analyze reference' : wizardStep === 2 ? 'Preview candidates' : 'Continue'}</button>{:else}<button class="btn primary" type="button" on:click={onActivate} disabled={wizardBusy}>{wizardBusy ? 'Activating…' : 'Activate watcher'}</button>{/if}</div>
  </div>
{/if}
