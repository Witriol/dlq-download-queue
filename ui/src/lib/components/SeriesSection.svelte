<script>
  import { onMount } from 'svelte';
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
  let wizardStep = 0;
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
  let previewTotalCandidates = 0;
  let previewSeason = 1;
  let refreshTimer = null;
  let mounted = false;
  let wasActive = false;
  let autoSeriesFolder = '';
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
      release_delay_seconds: 21600,
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
    autoSeriesFolder = '';
    wizardStep = 0;
    wizardError = '';
    wizardNotice = '';
    showWizard = true;
  }

  function closeWizard() {
    if (wizardBusy) return;
    showWizard = false;
    browserOpen = false;
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

  async function analyzeReference() {
    wizardError = '';
    wizardNotice = '';
    if (!draft.reference_url.trim()) {
      wizardError = 'Paste a Webshare reference URL first.';
      return;
    }
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
    if (!query) return;
    selectedShow = null;
    draft.tvmaze_id = undefined;
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
    const nextAutoFolder = slugify(show.name);
    if (!draft.series_folder || draft.series_folder === autoSeriesFolder) {
      draft.series_folder = nextAutoFolder;
    }
    autoSeriesFolder = nextAutoFolder;
    if (!draft.search_title) draft.search_title = show.name;
  }

  async function runCandidatePreview() {
    wizardError = '';
    wizardNotice = '';
    if (!selectedShow?.id) {
      wizardError = 'Choose the exact TVmaze show before previewing candidates.';
      return;
    }
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
    } catch (err) {
      wizardError = err instanceof Error ? err.message : String(err);
    } finally {
      wizardBusy = false;
    }
  }

  async function activate() {
    wizardError = '';
    if (!draft.out_dir.trim()) { wizardError = 'Choose an output folder.'; wizardStep = 0; return; }
    if (!selectedShow?.id) { wizardError = 'Choose a TVmaze show.'; wizardStep = 1; return; }
    syncProfile();
    wizardBusy = true;
    try {
      await createSeries({ ...draft, start_mode: draft.initial_mode, tvmaze_id: Number(selectedShow.id), display_name: selectedShow.name, quality_profile: draft.quality_profile });
      showWizard = false;
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
    editDraft = {
      search_title: watch.search_title || watch.display_name,
      out_dir: watch.out_dir || '',
      fallback_policy: watch.fallback_policy || 'strict',
      release_delay_hours: Math.round(Number(watch.release_delay_seconds ?? 21600) / 3600),
      preferred_wait_hours: Math.round(Number(watch.preferred_wait_seconds ?? 86400) / 3600),
      series_folder: watch.series_folder || slugify(watch.display_name),
      organize_by_season: watch.organize_by_season !== false
    };
  }

  async function saveEdit() {
    if (!editing) return;
    actionError = '';
    busyAction = `${editing.id}:edit`;
    try {
      const { release_delay_hours, preferred_wait_hours, ...payload } = editDraft;
      await updateSeries(editing.id, {
        ...payload,
        release_delay_seconds: Math.max(0, Number(release_delay_hours || 0) * 3600),
        preferred_wait_seconds: Math.max(0, Number(preferred_wait_hours || 0) * 3600)
      });
      editing = null;
      await refresh();
      onChanged();
    } catch (err) {
      actionError = err instanceof Error ? err.message : String(err);
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
    else draft.out_dir = path;
    browserOpen = false;
  }

  function stepBack() {
    wizardError = '';
    wizardStep = Math.max(0, wizardStep - 1);
  }

  function stepForward() {
    wizardError = '';
    if (wizardStep === 0) analyzeReference();
    else if (wizardStep === 1) {
      if (!selectedShow) { wizardError = 'Choose a TVmaze show to continue.'; return; }
      wizardStep = 2;
    } else if (wizardStep === 2) runCandidatePreview();
  }

  function formatDate(value) {
    if (!value) return '—';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
  }

  function episodeLabel(ep) {
    if (!ep) return '—';
    const season = Number(ep.season);
    const episode = Number(ep.episode);
    const code = Number.isFinite(season) && Number.isFinite(episode)
      ? `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}` : '';
    return [code, ep.episode_name || ep.name].filter(Boolean).join(' · ') || 'Scheduled';
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

  function slugify(value) {
    return String(value || '')
      .toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-+|-+$/g, '');
  }

  function hasDifferentSeriesTitle(candidate) {
    const details = [candidate?.reject_reasons, candidate?.reasons, candidate?.explanations]
      .flatMap((value) => Array.isArray(value) ? value : [value])
      .filter(Boolean).join(' ').toLowerCase();
    return details.includes('different series title');
  }

  function outputExample(root, folder, organizeBySeason, season = 1) {
    const normalizedFolder = slugify(folder || selectedShow?.name || draft.display_name) || 'series';
    const pieces = [String(root || '').replace(/\/+$/, ''), normalizedFolder];
    if (organizeBySeason) pieces.push(`s${String(season).padStart(2, '0')}`);
    return pieces.filter(Boolean).join('/') || 'Choose a library root';
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

<section class="panel series-section" aria-labelledby="series-heading">
  <div class="series-section-header">
    <div>
      <p class="eyebrow">Automation</p>
      <h2 id="series-heading">Series</h2>
      <p class="muted">Keep future episodes matched to a trusted Webshare release profile.</p>
    </div>
    <div class="actions">
      <button class="btn ghost" type="button" on:click={refresh} disabled={loading}>{loading ? 'Refreshing…' : 'Refresh'}</button>
      <button class="btn primary" type="button" on:click={openWizard}>Add series</button>
    </div>
  </div>

  {#if loadError}<div class="series-alert error">{loadError}</div>{/if}
  {#if actionError}<div class="series-alert error">{actionError}</div>{/if}

  {#if watches.length === 0 && !loading}
    <div class="series-empty">
      <div class="series-empty-icon">✦</div>
      <h3>No series watched yet</h3>
      <p>Use a real Webshare episode as a profile, then choose the exact TVmaze show.</p>
      <button class="btn primary" type="button" on:click={openWizard}>Create your first watcher</button>
    </div>
  {:else}
    <div class="series-list">
      {#each watches as watch (watch.id)}
        <article class="series-card" class:series-card-paused={!watch.enabled}>
          <div class="series-card-main">
            <div class="series-title-row">
              <h3>{watch.display_name}</h3>
              <span class="series-status" data-status={watch.status}>{watch.enabled ? (watch.status || 'active') : 'paused'}</span>
              {#if watch.attention_count > 0}<span class="series-status attention">{watch.attention_count} attention</span>{/if}
            </div>
            <p class="series-subtitle">{watch.search_title || watch.display_name} · {watch.out_dir || 'No output folder'}</p>
            <div class="series-facts">
              <div><span>Next episode</span><strong>{episodeLabel(watch.next_episode)}</strong><small>{formatDate(watch.next_episode?.air_timestamp || watch.next_episode_at)}</small></div>
              <div><span>Last check</span><strong>{formatDate(watch.last_checked_at)}</strong><small>Next: {formatDate(watch.next_check_at)}</small></div>
              <div><span>Profile</span><strong>{prettyPolicy(watch.fallback_policy)}</strong><small>{watch.reference_filename || 'Reference profile'}</small></div>
            </div>
            {#if watch.last_error}<p class="series-last-error">{watch.last_error}</p>{/if}
          </div>
          <div class="series-card-actions">
            {#if watch.attention_count > 0}<button class="btn tiny attention-action" type="button" on:click={() => openAttention(watch)}>{`Review attention (${watch.attention_count})`}</button>{/if}
            <button class="btn tiny" type="button" on:click={() => doAction(watch, 'check-now')} disabled={!watch.enabled || busyAction === `${watch.id}:check-now`}>{busyAction === `${watch.id}:check-now` ? 'Checking…' : 'Check now'}</button>
            <button class="btn tiny ghost" type="button" on:click={() => doAction(watch, watch.enabled ? 'pause' : 'resume')} disabled={busyAction === `${watch.id}:${watch.enabled ? 'pause' : 'resume'}`}>{watch.enabled ? 'Pause' : 'Resume'}</button>
            <button class="btn tiny ghost" type="button" on:click={() => startEdit(watch)}>Edit</button>
            <button class="btn tiny danger" type="button" on:click={() => doAction(watch, 'remove')} disabled={busyAction === `${watch.id}:remove`}>Remove</button>
          </div>
        </article>
      {/each}
    </div>
  {/if}
</section>

<SeriesWizard
  show={showWizard}
  {stepLabels}
  bind:wizardStep
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
