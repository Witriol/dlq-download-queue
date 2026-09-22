<script>
  import { onMount, tick } from 'svelte';
  import { formatDateTime } from '$lib/format';
  import { createSeries, listSeries, listSeriesAttention, previewSeries, searchTVMaze, selectSeriesCandidate, seriesAction, updateSeries } from '$lib/api';
  import FolderBrowser from '$lib/components/FolderBrowser.svelte';
  import SeriesAttentionModal from '$lib/components/SeriesAttentionModal.svelte';
  import SeriesEditModal from '$lib/components/SeriesEditModal.svelte';
  import SeriesWizard from '$lib/components/SeriesWizard.svelte';

  export let outDirPresets = [];
  export let outDirFavorites = [];
  export let onAddFavorite = () => {};
  export let onRemoveFavorite = () => {};
  export let onModalOpenChange = () => {};
  export let onChanged = () => {};
  export let active = false;

  const stepLabels = ['Reference', 'Profile & show', 'Policy', 'Preview'];
  const defaultFieldModes = { resolution: 'required', codec: 'preferred', source: 'preferred', release_group: 'preferred', container: 'ignored' };

  let watches = [];
  let loading = false;
  let loadError = '';
  let actionError = '';
  let busyAction = '';
  let showWizard = false;
  let wizardOpener = null;
  let wizardStep = 0;
  let wizardFurthestStep = 0;
  let wizardBusy = false;
  let wizardError = '';
  let wizardNotice = '';
  let profile = {};
  let profileFields = [];
  let profileTokens = [];
  let candidates = [];
  let tvmazeResults = [];
  let tvmazeQuery = '';
  let tvmazeBusy = false;
  let selectedShow = null;
  let browserOpen = false;
  let browserTarget = 'draft';
  let editing = null;
  let editDraft = {};
  let editError = '';
  let previewTotalCandidates = 0;
  let previewSeason = 1;
  let refreshTimer = null;
  let mounted = false;
  let wasActive = false;
  let attentionWatch = null;
  let attentionEpisodes = [];
  let attentionLoading = false;
  let attentionError = '';
  let attentionBusy = '';

  let draft = blankDraft();

  function blankDraft() {
    return {
      reference_url: '',
      out_dir: '',
      display_name: '',
      search_title: '',
      tvmaze_id: undefined,
      initial_mode: 'template',
      initial_season: 1,
      initial_episode: 1,
      fallback_policy: 'strict',
      release_delay_seconds: 7200,
      preferred_wait_seconds: 86400,
      series_folder: '',
      organize_by_season: true,
      quality_profile: {}
    };
  }

  function unwrap(value, keys) {
    if (!value || typeof value !== 'object') return value;
    for (const key of keys) if (key in value) return value[key];
    return value;
  }

  function normalizeWatch(raw) {
    const watch = raw || {};
    return {
      ...watch,
      id: watch.id ?? watch.watch_id,
      display_name: watch.display_name || watch.name || watch.title || 'Unnamed series',
      out_dir: watch.out_dir || watch.output_dir || '',
      enabled: watch.enabled !== false && watch.status !== 'paused',
      status: watch.status || (watch.enabled === false ? 'paused' : 'active'),
      next_episode: watch.next_episode || watch.next || null,
      last_episode: watch.last_episode || watch.last || null,
      attention_count: Number(watch.attention_count || watch.problem_count || 0)
    };
  }

  async function refresh() {
    loading = true;
    loadError = '';
    try {
      const response = await listSeries();
      watches = response.map(normalizeWatch);
    } catch (err) {
      loadError = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  function openWizard() {
    wizardOpener = typeof document !== 'undefined' && document.activeElement instanceof HTMLElement ? document.activeElement : null;
    draft = blankDraft();
    profile = {};
    profileFields = [];
    profileTokens = [];
    candidates = [];
    selectedShow = null;
    tvmazeResults = [];
    tvmazeQuery = '';
    previewTotalCandidates = 0;
    previewSeason = 1;
    wizardStep = 0;
    wizardFurthestStep = 0;
    wizardError = '';
    wizardNotice = '';
    showWizard = true;
  }

  function closeWizard() {
    if (wizardBusy) return;
    showWizard = false;
    browserOpen = false;
    restoreWizardFocus();
  }

  function restoreWizardFocus() {
    tick().then(() => wizardOpener?.focus());
  }

  function profileObject(response) {
    const value = response?.quality_profile || response?.profile
      || (response && (response.resolution || response.codec || response.search_title || response.raw_tokens) ? response : {});
    if (typeof value === 'string') {
      try { return JSON.parse(value); } catch { return {}; }
    }
    return value && typeof value === 'object' ? value : {};
  }

  function profileEntries(value) {
	const editableKeys = new Set(['resolution', 'codec', 'source', 'release_group', 'container', 'hdr']);
	return Object.entries(value || {}).filter(([key, raw]) => editableKeys.has(key) && raw != null && raw !== '').map(([key, raw]) => {
      const field = raw && typeof raw === 'object' && !Array.isArray(raw)
        ? { ...raw }
        : { value: raw };
      return {
        key,
        token: field.token || field.raw || String(field.normalized ?? field.value ?? ''),
        value: field.normalized ?? field.value ?? '',
        confidence: field.confidence,
        mode: field.mode || defaultFieldModes[key] || 'preferred'
      };
    });
  }

  function showWizardError(message, fieldId) {
    wizardError = message;
    if (fieldId && typeof document !== 'undefined') {
      requestAnimationFrame(() => document.getElementById(fieldId)?.focus());
    }
    return false;
  }

  function validateWizardStep(step) {
    wizardError = '';
    if (step === 0) {
      if (!draft.reference_url.trim()) return showWizardError('Add a Webshare reference URL to continue.', 'series-reference');
      if (!/^https?:\/\/(?:www\.)?webshare\.cz\//i.test(draft.reference_url.trim())) {
        return showWizardError('Use a valid webshare.cz file URL.', 'series-reference');
      }
      if (!draft.out_dir.trim()) return showWizardError('Choose a download folder to continue.', 'series-out-dir');
    }
    if (step === 1 && !selectedShow?.id) {
      return showWizardError('Choose the matching TVmaze show to continue.', 'tvmaze-search');
    }
    if (step === 2) {
      if (!['strict', 'balanced', 'manual'].includes(draft.fallback_policy)) return showWizardError('Choose a download policy to continue.');
      if (!draft.search_title.trim()) return showWizardError('Add a Webshare search title to continue.', 'search-title');
      if (!['template', 'continue', 'specific'].includes(draft.initial_mode)) return showWizardError('Choose where episode tracking should start.', 'initial-mode');
      if (draft.initial_mode === 'specific') {
        if (Number(draft.initial_season) < 1) return showWizardError('Season must be 1 or greater.', 'initial-season');
        if (Number(draft.initial_episode) < 1) return showWizardError('Episode must be 1 or greater.', 'initial-episode');
      }
      if (!Number.isFinite(Number(draft.preferred_wait_seconds)) || Number(draft.preferred_wait_seconds) < 0) {
        return showWizardError('Preferred wait must be zero or greater.', 'preferred-wait');
      }
    }
    return true;
  }

  async function analyzeReference() {
    wizardError = '';
    wizardNotice = '';
    if (!validateWizardStep(0)) return;
    wizardBusy = true;
    try {
      const response = await previewSeries({ reference_url: draft.reference_url.trim(), out_dir: draft.out_dir });
      profile = profileObject(response);
      profileFields = profileEntries(profile);
      profileTokens = Array.isArray(response?.tokens) ? response.tokens : (response?.raw_tokens || []);
      draft.reference_webshare_ident = response?.reference_webshare_ident || response?.webshare_ident;
      draft.reference_filename = response?.reference_filename || response?.filename || profile.filename;
      draft.search_title = response?.search_title || profile.search_title || draft.search_title;
      draft.display_name = response?.display_name || draft.display_name;
      if (response?.error) wizardNotice = response.error;
      wizardStep = 1;
      wizardFurthestStep = Math.max(wizardFurthestStep, 1);
      tvmazeQuery = draft.search_title || draft.display_name || '';
      if (tvmazeQuery) await findShows();
    } catch (err) {
      wizardError = err instanceof Error ? err.message : String(err);
    } finally {
      wizardBusy = false;
    }
  }

  function syncProfile() {
    const next = {};
    for (const field of profileFields) {
      next[field.key] = {
        value: field.value,
        normalized: field.value,
        mode: field.mode,
        ...(field.confidence == null ? {} : { confidence: field.confidence }),
        ...(field.token ? { token: field.token } : {})
      };
    }
    draft.quality_profile = next;
  }

  function normalizeTVMazeQuery(value) {
    const query = value.trim();
    if (/^(?:https?:\/\/)?(?:www\.)?tvmaze\.com\/shows\//i.test(query)) return query;
    return query.replace(/[._]+/g, ' ').replace(/\s+/g, ' ');
  }

  async function findShows() {
    const query = normalizeTVMazeQuery(tvmazeQuery);
    if (!query) {
      showWizardError('Enter a series name or TVmaze URL.', 'tvmaze-search');
      return;
    }
    selectedShow = null;
    draft.tvmaze_id = undefined;
    invalidateWizardFrom(1);
    tvmazeBusy = true;
    wizardError = '';
    try {
      tvmazeResults = await searchTVMaze(query);
      wizardNotice = tvmazeResults.length ? '' : 'No TVmaze shows found. Try another title or paste the show URL.';
    } catch (err) {
      wizardError = err instanceof Error ? err.message : String(err);
    } finally {
      tvmazeBusy = false;
    }
  }

  function chooseShow(show) {
    selectedShow = show;
    draft.tvmaze_id = Number(show.id);
    draft.display_name = show.name;
    if (!draft.search_title) draft.search_title = show.name;
    wizardError = '';
    wizardNotice = '';
    invalidateWizardFrom(2);
  }

  async function runCandidatePreview() {
    wizardError = '';
    wizardNotice = '';
    if (!validateWizardStep(1) || !validateWizardStep(2)) return;
    syncProfile();
    wizardBusy = true;
    try {
      const response = await previewSeries({ ...draft, start_mode: draft.initial_mode, tvmaze_id: Number(selectedShow.id), quality_profile: draft.quality_profile });
      candidates = Array.isArray(response?.candidates) ? response.candidates : (Array.isArray(response?.matches) ? response.matches : []);
      previewTotalCandidates = Number(response?.total_candidates ?? candidates.length) || candidates.length;
      if (response?.episode) {
        draft.preview_episode = response.episode.id || response.episode.episode;
        previewSeason = Number(response.episode.season) || Number(draft.initial_season) || 1;
      }
      wizardNotice = `${candidates.length} of ${previewTotalCandidates} candidate${previewTotalCandidates === 1 ? '' : 's'} shown. Different series titles are hidden.`;
      wizardStep = 3;
      wizardFurthestStep = Math.max(wizardFurthestStep, 3);
    } catch (err) {
      wizardError = err instanceof Error ? err.message : String(err);
    } finally {
      wizardBusy = false;
    }
  }

  async function activate() {
    wizardError = '';
    for (const step of [0, 1, 2]) {
      if (!validateWizardStep(step)) { wizardStep = step; return; }
    }
    syncProfile();
    wizardBusy = true;
    try {
      await createSeries({ ...draft, start_mode: draft.initial_mode, tvmaze_id: Number(selectedShow.id), display_name: selectedShow.name, quality_profile: draft.quality_profile });
      showWizard = false;
      restoreWizardFocus();
      await refresh();
      onChanged();
    } catch (err) {
      wizardError = err instanceof Error ? err.message : String(err);
    } finally {
      wizardBusy = false;
    }
  }

  async function doAction(watch, action) {
    const id = watch?.id;
    if (id == null) return;
    if (action === 'remove' && !confirm(`Remove the ${watch.display_name} watcher? Existing download jobs are not removed.`)) return;
    actionError = '';
    busyAction = `${id}:${action}`;
    try {
      await seriesAction(id, action);
      await refresh();
      onChanged();
    } catch (err) {
      actionError = err instanceof Error ? err.message : String(err);
    } finally {
      busyAction = '';
    }
  }

  function normalizeAttentionEpisode(raw) {
    const episode = raw || {};
    return {
      ...episode,
      candidates: Array.isArray(episode.candidates) ? episode.candidates : []
    };
  }

  async function loadAttention() {
    if (!attentionWatch?.id) return;
    attentionLoading = true;
    attentionError = '';
    try {
      const response = await listSeriesAttention(attentionWatch.id);
      attentionEpisodes = response.map(normalizeAttentionEpisode);
    } catch (err) {
      attentionError = err instanceof Error ? err.message : String(err);
      attentionEpisodes = [];
    } finally {
      attentionLoading = false;
    }
  }

  function openAttention(watch) {
    attentionWatch = watch;
    attentionEpisodes = [];
    attentionError = '';
    attentionBusy = '';
    loadAttention();
  }

  function closeAttention() {
    if (attentionBusy) return;
    attentionWatch = null;
    attentionEpisodes = [];
    attentionError = '';
  }

  async function queueCandidate(episode, candidate) {
    const episodeId = episode?.id;
    const ident = candidate?.ident;
    if (episodeId == null || !ident) {
      attentionError = 'This candidate is missing its persisted release identifier.';
      return;
    }
    const key = `${episodeId}:${ident}`;
    attentionError = '';
    attentionBusy = key;
    try {
      await selectSeriesCandidate(attentionWatch.id, episodeId, ident);
      await loadAttention();
      await refresh();
      onChanged();
    } catch (err) {
      attentionError = err instanceof Error ? err.message : String(err);
    } finally {
      attentionBusy = '';
    }
  }

  function startEdit(watch) {
    editing = watch;
    editError = '';
    editDraft = {
      search_title: watch.search_title || watch.display_name,
      out_dir: watch.out_dir || '',
      fallback_policy: watch.fallback_policy || 'strict',
      preferred_wait_hours: Math.round(Number(watch.preferred_wait_seconds ?? 86400) / 3600),
      organize_by_season: watch.organize_by_season !== false
    };
  }

  async function saveEdit() {
    if (!editing) return;
    editError = '';
    if (!String(editDraft.search_title || '').trim()) {
      editError = 'Add a Webshare search title.';
      requestAnimationFrame(() => document.getElementById('edit-search-title')?.focus());
      return;
    }
    if (!String(editDraft.out_dir || '').trim()) {
      editError = 'Choose a series folder.';
      requestAnimationFrame(() => document.getElementById('edit-series-folder')?.focus());
      return;
    }
    busyAction = `${editing.id}:edit`;
    try {
      const { preferred_wait_hours, ...payload } = editDraft;
      await updateSeries(editing.id, {
        ...payload,
        preferred_wait_seconds: Math.max(0, Number(preferred_wait_hours || 0) * 3600)
      });
      editing = null;
      await refresh();
      onChanged();
    } catch (err) {
      editError = err instanceof Error ? err.message : String(err);
    } finally {
      busyAction = '';
    }
  }

  async function openBrowser(target = 'draft') {
    browserTarget = target;
    browserOpen = true;
  }

  function selectFolder(path) {
    if (browserTarget === 'edit') editDraft.out_dir = path;
    else {
      draft.out_dir = path;
      invalidateWizardFrom(2);
    }
    browserOpen = false;
  }

  function stepBack() {
    wizardError = '';
    wizardNotice = '';
    wizardStep = Math.max(0, wizardStep - 1);
  }

  function invalidateWizardFrom(step) {
    wizardFurthestStep = Math.min(wizardFurthestStep, step);
    if (wizardStep > step) wizardStep = step;
    if (step < 3) {
      candidates = [];
      previewTotalCandidates = 0;
    }
  }

  function goToWizardStep(step) {
    if (wizardBusy || step < 0 || step > wizardFurthestStep || step === wizardStep) return;
    wizardError = '';
    wizardNotice = '';
    wizardStep = step;
  }

  function stepForward() {
    wizardError = '';
    if (wizardStep < wizardFurthestStep) {
      wizardStep += 1;
      return;
    }
    if (wizardStep === 0) analyzeReference();
    else if (wizardStep === 1) {
      if (!validateWizardStep(1)) return;
      wizardStep = 2;
      wizardFurthestStep = Math.max(wizardFurthestStep, 2);
    } else if (wizardStep === 2) {
      if (!validateWizardStep(2)) return;
      runCandidatePreview();
    }
  }

  function formatDate(value) {
    return formatDateTime(value);
  }

  function episodeLabel(ep) {
    if (!ep) return '—';
    const season = Number(ep.season);
    const episode = Number(ep.episode);
    const code = Number.isFinite(season) && Number.isFinite(episode)
      ? `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}` : '';
    return [code, ep.episode_name || ep.name].filter(Boolean).join(' · ') || 'Scheduled';
  }

  function episodeCode(ep) {
    if (!ep) return 'No episode';
    const season = Number(ep.season);
    const episode = Number(ep.episode);
    return Number.isFinite(season) && Number.isFinite(episode)
      ? `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}`
      : 'Upcoming';
  }

  function episodeTitle(ep) {
    return ep?.episode_name || ep?.name || 'Waiting for schedule';
  }

  function nextEpisodeTime(watch) {
    return watch.next_episode?.air_timestamp || watch.next_episode_at;
  }

  function prettyPolicy(value) {
    return ({ strict: 'Strict', balanced: 'Balanced', manual: 'Manual fallback' }[value] || value || 'Strict');
  }

  function candidateName(candidate) {
    return candidate.filename || candidate.name || candidate.title || candidate.webshare_ident || candidate.ident || 'Unnamed candidate';
  }

  function reasons(candidate) {
	const positive = Array.isArray(candidate.reasons) ? candidate.reasons : (typeof candidate.reasons === 'string' ? [candidate.reasons] : []);
	const rejected = Array.isArray(candidate.reject_reasons) ? candidate.reject_reasons : (typeof candidate.reject_reasons === 'string' ? [candidate.reject_reasons] : []);
	const explanations = Array.isArray(candidate.explanations) ? candidate.explanations : [];
	return [...positive, ...rejected.map((reason) => `- ${reason}`), ...explanations];
  }

  function hasDifferentSeriesTitle(candidate) {
    const details = [candidate?.reject_reasons, candidate?.reasons, candidate?.explanations]
      .flatMap((value) => Array.isArray(value) ? value : [value])
      .filter(Boolean).join(' ').toLowerCase();
    return details.includes('different series title');
  }

  function outputExample(root, _folder, organizeBySeason, season = 1) {
    const pieces = [String(root || '').replace(/\/+$/, '')];
    if (organizeBySeason) pieces.push(`s${String(season).padStart(2, '0')}`);
    return pieces.filter(Boolean).join('/') || 'Choose the series folder';
  }

  function stopRefreshTimer() {
    if (refreshTimer) {
      clearInterval(refreshTimer);
      refreshTimer = null;
    }
  }

  function startRefreshTimer() {
    stopRefreshTimer();
    if (!mounted || !active) return;
    refreshTimer = setInterval(refresh, 15000);
  }

  $: watcherModalOpen = showWizard || Boolean(editing) || browserOpen || Boolean(attentionWatch);
  $: activeWatchCount = watches.filter((watch) => watch.enabled).length;
  $: attentionCount = watches.reduce((total, watch) => total + watch.attention_count, 0);
  $: scheduledCount = watches.filter((watch) => Boolean(watch.next_episode || watch.next_episode_at)).length;
  $: visibleCandidates = candidates.filter((candidate) => !hasDifferentSeriesTitle(candidate));
  $: if (mounted) {
    if (watcherModalOpen || !active) stopRefreshTimer();
    else startRefreshTimer();
  }
  $: if (mounted && active !== wasActive) {
    wasActive = active;
    if (active) refresh();
  }
  $: onModalOpenChange(watcherModalOpen);

  onMount(() => {
    mounted = true;
    wasActive = active;
    refresh();
    if (active) startRefreshTimer();
    return () => {
      mounted = false;
      stopRefreshTimer();
      onModalOpenChange(false);
    };
  });
</script>

<section class="series-section" aria-labelledby="series-heading">
  <div class="stats automation-stats" aria-label="Automation overview">
    <div class="stat stat-active"><span>Active</span><strong>{activeWatchCount}</strong></div>
    <div class="stat stat-eta"><span>Episodes on deck</span><strong>{scheduledCount}</strong></div>
    <div class="stat" class:stat-failed={attentionCount > 0}><span>Warnings</span><strong>{attentionCount}</strong></div>
  </div>

  <div class="panel automation-panel">
    <div class="table-toolbar automation-toolbar">
      <div>
        <h2 id="series-heading">Automations</h2>
        <p class="muted">New series are first checked two hours after airtime.</p>
      </div>
      <div class="actions">
        <button class="btn ghost" type="button" on:click={refresh} disabled={loading}>{loading ? 'Refreshing…' : 'Refresh'}</button>
        <button class="btn primary" type="button" on:click={openWizard}>Add series</button>
      </div>
    </div>

    {#if loadError}<div class="series-alert error" role="alert">{loadError}</div>{/if}
    {#if actionError}<div class="series-alert error" role="alert">{actionError}</div>{/if}

    {#if watches.length === 0 && !loading}
      <div class="series-empty">
        <div class="series-empty-icon">✦</div>
        <h3>No series watched yet</h3>
        <p>Use a real Webshare episode as a profile, then choose the exact TVmaze show.</p>
        <button class="btn primary" type="button" on:click={openWizard}>Create your first watcher</button>
      </div>
    {:else}
      <div class="table-wrap">
        <table class="table automation-table">
          <colgroup><col class="automation-col-status" /><col class="automation-col-series" /><col class="automation-col-next" /><col class="automation-col-path" /><col class="automation-col-actions" /></colgroup>
          <thead><tr><th>Status</th><th>Series</th><th>Next episode</th><th>Download path</th><th class="actions-col">Actions</th></tr></thead>
          <tbody>
            {#each watches as watch (watch.id)}
              <tr data-status={watch.status} class:automation-row-paused={!watch.enabled}>
                <td class="cell-status" data-label="Status">
                  <span class="status" data-status={watch.status}>{watch.enabled ? (watch.status === 'needs_attention' ? 'warning' : watch.status || 'active') : 'paused'}</span>
                  <small class="automation-last-check">Checked {formatDate(watch.last_checked_at)}</small>
                </td>
                <td class="cell-name automation-series-cell" data-label="Series">
                  <strong>{watch.display_name}</strong>
                  <small>{prettyPolicy(watch.fallback_policy)} policy</small>
                </td>
                <td class="automation-next-cell" data-label="Next episode">
                  <div class="automation-episode-line"><strong>{episodeCode(watch.next_episode)}</strong><span>{episodeTitle(watch.next_episode)}</span></div>
                  <time datetime={nextEpisodeTime(watch) || undefined}>{formatDate(nextEpisodeTime(watch))}</time>
                  {#if watch.attention_count > 0}<div class="automation-warning">Release not found after four searches for {watch.attention_count} {watch.attention_count === 1 ? 'episode' : 'episodes'}. {watch.fallback_policy === 'manual' ? 'Review the saved alternatives.' : 'Retry to start a fresh search cycle.'}</div>{/if}
                  {#if watch.last_error}<div class="automation-warning">{watch.last_error}</div>{/if}
                </td>
                <td class="cell-path automation-path-cell" data-label="Download path" title={watch.out_dir || 'No output folder'}>{watch.out_dir || 'No output folder'}</td>
                <td class="actions-col" data-label="Actions">
                  <div class="actions row-actions automation-actions">
                    {#if watch.attention_count > 0 && watch.fallback_policy === 'manual'}
                      <button class="btn icon-btn action-btn action-stop" type="button" title={`Review ${watch.attention_count} warning${watch.attention_count === 1 ? '' : 's'}`} aria-label={`Review warnings for ${watch.display_name}`} on:click={() => openAttention(watch)}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2 1 21h22L12 2zm-1 6h2v7h-2zm0 9h2v2h-2z" /></svg></button>
                    {/if}
                    <button class="btn icon-btn action-btn action-retry" type="button" title={busyAction === `${watch.id}:check-now` ? 'Checking…' : watch.attention_count > 0 && watch.fallback_policy !== 'manual' ? 'Retry search cycle' : 'Check now'} aria-label={watch.attention_count > 0 && watch.fallback_policy !== 'manual' ? `Retry searches for ${watch.display_name}` : `Check ${watch.display_name} now`} on:click={() => doAction(watch, 'check-now')} disabled={!watch.enabled || busyAction === `${watch.id}:check-now`}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M17.7 6.3A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.8-4.3L13 11h8V3z" /></svg></button>
                    <button class="btn icon-btn action-btn" class:action-pause={watch.enabled} class:action-resume={!watch.enabled} type="button" title={watch.enabled ? 'Pause' : 'Resume'} aria-label={`${watch.enabled ? 'Pause' : 'Resume'} ${watch.display_name}`} on:click={() => doAction(watch, watch.enabled ? 'pause' : 'resume')} disabled={busyAction === `${watch.id}:${watch.enabled ? 'pause' : 'resume'}`}><svg viewBox="0 0 24 24" aria-hidden="true">{#if watch.enabled}<path d="M7 5h4v14H7zm6 0h4v14h-4z" />{:else}<path d="M8 5v14l11-7z" />{/if}</svg></button>
                    <button class="btn icon-btn action-btn action-logs" type="button" title="Edit" aria-label={`Edit ${watch.display_name}`} on:click={() => startEdit(watch)}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 16.5-.5 4 4-.5L19 8.5 15.5 5zM17 3l4 4-1.5 1.5-4-4z" /></svg></button>
                    <button class="btn icon-btn action-btn action-remove" type="button" title="Remove" aria-label={`Remove ${watch.display_name}`} on:click={() => doAction(watch, 'remove')} disabled={busyAction === `${watch.id}:remove`}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 7h10l-1 13H8zm2-3h6l1 2h4v2H4V6h4z" /></svg></button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>
</section>

<SeriesWizard
  show={showWizard}
  {stepLabels}
  bind:wizardStep
  bind:wizardFurthestStep
  {wizardBusy}
  {wizardError}
  {wizardNotice}
  bind:draft
  {outDirPresets}
  {outDirFavorites}
  bind:profileFields
  bind:tvmazeQuery
  {tvmazeResults}
  {tvmazeBusy}
  {selectedShow}
  {visibleCandidates}
  {previewTotalCandidates}
  {previewSeason}
  onClose={closeWizard}
  onNext={stepForward}
  onBack={stepBack}
  onGoToStep={goToWizardStep}
  onInvalidateFrom={invalidateWizardFrom}
  onActivate={activate}
  onFindShows={findShows}
  onChooseShow={chooseShow}
  onOpenBrowser={() => openBrowser('draft')}
  {onRemoveFavorite}
  {outputExample}
  {prettyPolicy}
  {candidateName}
  {reasons}
/>

<SeriesAttentionModal
  watch={attentionWatch}
  episodes={attentionEpisodes}
  loading={attentionLoading}
  error={attentionError}
  busy={attentionBusy}
  onClose={closeAttention}
  onQueueCandidate={queueCandidate}
  {episodeLabel}
  {formatDate}
  {candidateName}
  {reasons}
/>

<SeriesEditModal
  watch={editing}
  bind:draft={editDraft}
  saving={busyAction === `${editing?.id}:edit`}
  error={editError}
  onClose={() => (editing = null)}
  onSave={saveEdit}
  onOpenBrowser={() => openBrowser('edit')}
  {outputExample}
/>

<FolderBrowser
  bind:show={browserOpen}
  favoritePaths={outDirFavorites}
  onSelect={selectFolder}
  {onAddFavorite}
/>
