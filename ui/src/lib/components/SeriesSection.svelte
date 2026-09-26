<script>
  import { onMount, tick } from 'svelte';
  import { formatAirTime, formatDateTime, formatDateTimeShort, localTimeZone, relativeTime } from '$lib/format';
  import { createSeries, getSeriesEvents, listSeries, listSeriesAttention, previewSeries, searchTVMaze, selectSeriesCandidate, seriesAction, updateSeries } from '$lib/api';
  import FolderBrowser from '$lib/components/FolderBrowser.svelte';
  import LogsModal from '$lib/components/LogsModal.svelte';
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

  const defaultFieldModes = { resolution: 'required', codec: 'preferred', source: 'preferred', release_group: 'preferred', container: 'ignored' };
  const episodeDoneStates = new Set(['completed', 'skipped']);

  let watches = [];
  let loading = false;
  let loadError = '';
  let actionError = '';
  let busyAction = '';
  let showWizard = false;
  let wizardOpener = null;
  let wizardStep = 0;
  let wizardBusy = false;
  let wizardError = '';
  let wizardNotice = '';
  let profile = {};
  let profileFields = [];
  let profileTokens = [];
  let candidates = [];
  let nextEpisode = null;
  let testRan = false;
  let tvmazeResults = [];
  let tvmazeQuery = '';
  let tvmazeBusy = false;
  let selectedShow = null;
  let showShowSearch = false;
  let folderEdited = false;
  let browserOpen = false;
  let browserTarget = 'draft';
  let editing = null;
  let editDraft = {};
  let editError = '';
  let previewTotalCandidates = 0;
  let refreshTimer = null;
  let mounted = false;
  let wasActive = false;
  let attentionWatch = null;
  let attentionEpisodes = [];
  let attentionLoading = false;
  let attentionError = '';
  let attentionBusy = '';
  let showLogs = false;
  let logsWatch = null;
  let logsEvents = [];
  let logsLimit = 50;
  let logsAutoRefresh = true;
  let logsInterval = 3;
  let logsError = '';
  let logsLoading = false;
  let logsTimer = null;

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
      fallback_policy: 'balanced',
      preferred_wait_seconds: 43200,
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
    nextEpisode = null;
    testRan = false;
    selectedShow = null;
    showShowSearch = false;
    folderEdited = false;
    tvmazeResults = [];
    tvmazeQuery = '';
    previewTotalCandidates = 0;
    wizardStep = 0;
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
    }
    if (step === 1) {
      if (!selectedShow?.id) return showWizardError('Choose the matching TVmaze show to continue.', 'tvmaze-search');
      if (!draft.out_dir.trim()) return showWizardError('Choose a download folder to continue.', 'series-out-dir');
      if (!['strict', 'balanced', 'manual'].includes(draft.fallback_policy)) return showWizardError('Choose a download policy to continue.');
      if (!draft.search_title.trim()) return showWizardError('Add a Webshare search title to continue.', 'search-title');
      if (!['template', 'continue', 'specific'].includes(draft.initial_mode)) return showWizardError('Choose where episode tracking should start.', 'initial-mode');
      if (draft.initial_mode === 'specific') {
        if (Number(draft.initial_season) < 1) return showWizardError('Season must be 1 or greater.', 'initial-season');
        if (Number(draft.initial_episode) < 1) return showWizardError('Episode must be 1 or greater.', 'initial-episode');
      }
      if (draft.fallback_policy !== 'strict' && (!Number.isFinite(Number(draft.preferred_wait_seconds)) || Number(draft.preferred_wait_seconds) < 0)) {
        return showWizardError('Wait time must be zero or greater.', 'preferred-wait');
      }
    }
    return true;
  }

  /** Drop characters that cannot live in a filesystem path and collapse whitespace. */
  function sanitizeShowName(name) {
    return String(name || '').replace(/[:/\\?*"<>|]/g, '').replace(/\s+/g, ' ').trim();
  }

  function folderRoot() {
    if (outDirFavorites.length) return outDirFavorites[0];
    const tvPreset = outDirPresets.find((preset) => /tv/i.test(preset));
    return tvPreset || outDirPresets[0] || '';
  }

  function prefillFolder(show) {
    const root = folderRoot().replace(/\/+$/, '');
    const name = sanitizeShowName(show?.name);
    if (!root || !name) return '';
    return `${root}/${name}`;
  }

  function applyFolderPrefill() {
    if (folderEdited) return;
    const prefill = prefillFolder(selectedShow);
    if (prefill) draft.out_dir = prefill;
  }

  async function analyzeReference() {
    wizardError = '';
    wizardNotice = '';
    if (!validateWizardStep(0)) return;
    wizardBusy = true;
    try {
      const response = await previewSeries({ reference_url: draft.reference_url.trim() });
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
      if (tvmazeQuery) {
        await findShows();
        if (tvmazeResults.length) await chooseShow(tvmazeResults[0]);
      }
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

  async function chooseShow(show) {
    selectedShow = show;
    draft.tvmaze_id = Number(show.id);
    draft.display_name = show.name;
    if (!draft.search_title) draft.search_title = show.name;
    showShowSearch = false;
    wizardError = '';
    wizardNotice = '';
    applyFolderPrefill();
    await runCandidatePreview();
  }

  function pickWatcherCandidate(list) {
    const exact = list.find((candidate) => candidate.accepted !== false && candidate.exact !== false);
    if (exact) return exact;
    return list.find((candidate) => candidate.accepted !== false) || null;
  }

  async function runCandidatePreview() {
    wizardError = '';
    if (!selectedShow?.id) return;
    syncProfile();
    wizardBusy = true;
    try {
      const response = await previewSeries({ ...draft, start_mode: draft.initial_mode, tvmaze_id: Number(selectedShow.id), quality_profile: draft.quality_profile });
      candidates = Array.isArray(response?.candidates) ? response.candidates : (Array.isArray(response?.matches) ? response.matches : []);
      previewTotalCandidates = Number(response?.total_candidates ?? candidates.length) || candidates.length;
      nextEpisode = response?.next_episode || null;
      testRan = true;
    } catch (err) {
      wizardError = err instanceof Error ? err.message : String(err);
    } finally {
      wizardBusy = false;
    }
  }

  async function activate() {
    wizardError = '';
    for (const step of [0, 1]) {
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
    const fromEdit = action === 'remove' && editing?.id === id;
    if (fromEdit) editError = ''; else actionError = '';
    busyAction = `${id}:${action}`;
    try {
      await seriesAction(id, action);
      if (fromEdit) editing = null;
      await refresh();
      onChanged();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      if (fromEdit) editError = message; else actionError = message;
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
    if (browserTarget === 'edit') {
      editDraft.out_dir = path;
    } else {
      draft.out_dir = path;
      folderEdited = true;
    }
    browserOpen = false;
  }

  function stepBack() {
    wizardError = '';
    wizardNotice = '';
    wizardStep = Math.max(0, wizardStep - 1);
  }

  function stepForward() {
    wizardError = '';
    if (wizardStep === 0) analyzeReference();
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

  /** The episode to show in the list row: the in-flight one, else the upcoming one. */
  let sortKey = 'next';
  let sortDir = 'asc';

  /** Enabled, nothing announced, and the last episode is done. */
  function isIdle(watch) {
    if (!watch.enabled || watch.next_episode) return false;
    const last = watch.last_episode;
    return !last || episodeDoneStates.has(last.state);
  }

  function toggleSort(key) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc';
      return;
    }
    sortKey = key;
    sortDir = 'asc';
  }

  // Reactive so header markup re-renders when the sort changes.
  $: sortIndicator = (key) => (sortKey !== key ? '' : sortDir === 'asc' ? ' ↑' : ' ↓');
  $: ariaSort = (key) => (sortKey !== key ? 'none' : sortDir === 'asc' ? 'ascending' : 'descending');

  function sortValue(watch, key) {
    if (key === 'name') return watch.display_name || '';
    // Idle and paused series have no meaningful time; null keeps them last.
    if (!watch.enabled || isIdle(watch)) return null;
    if (key === 'episode') {
      const ep = episodeInFlight(watch);
      return ep?.air_timestamp ? Date.parse(ep.air_timestamp) : null;
    }
    return watch.next_check_at ? Date.parse(watch.next_check_at) : null;
  }

  function sortWatches(list, key, dir) {
    const sign = dir === 'asc' ? 1 : -1;
    return [...list].sort((a, b) => {
      const av = sortValue(a, key);
      const bv = sortValue(b, key);
      if (av == null || bv == null) {
        return Number(av == null) - Number(bv == null);
      }
      if (key === 'name') {
        return sign * av.localeCompare(bv);
      }
      return sign * (av - bv);
    });
  }

  $: sortedWatches = sortWatches(watches, sortKey, sortDir);

  function episodeInFlight(watch) {
    const last = watch.last_episode;
    if (last && !episodeDoneStates.has(last.state)) return last;
    return watch.next_episode || last || null;
  }

  /** State line (13 px) for the episode shown in the row; ep may be null. */
  function episodeStatus(watch, ep) {
    if (!ep) return { label: 'No episodes yet', at: null };
    const attempts = Number(ep.search_attempts) || 1;
    switch (ep.state) {
      case 'waiting_release':
      case 'scheduled':
        return { label: `Airs ${formatAirTime(ep.air_timestamp)} · ${relativeTime(ep.air_timestamp)}`, at: ep.air_timestamp };
      case 'searching':
      case 'preferred_not_found':
        return { label: `Searching · try ${attempts}`, at: null };
      case 'queued':
        return { label: 'Queued', at: null };
      case 'downloading':
        return { label: 'Downloading', at: null };
      case 'completed': {
        const suffix = watch.next_episode ? '' : (watch.show_status === 'Ended' ? ' · series ended' : ' · waiting for next season');
        return { label: `Downloaded ${relativeTime(ep.updated_at)}${suffix}`, at: ep.updated_at };
      }
      case 'needs_attention':
        return { label: 'Needs review', at: null };
      case 'failed':
        return { label: 'Download failed', at: null };
      default:
        return { label: ep.state || 'Scheduled', at: null };
    }
  }

  function statusTag(watch) {
    if (!watch.enabled) return 'paused';
    if (watch.attention_count > 0 || watch.status === 'needs_attention') return 'needs_attention';
    return 'active';
  }

  /** Inline series-name badge; null when plain active (no badge shown). */
  function seriesBadge(watch) {
    const tag = statusTag(watch);
    if (tag === 'paused') return { tag: 'paused', label: 'Paused', clickable: false, title: null };
    if (tag === 'needs_attention') {
      return { tag: 'needs_attention', label: 'Needs review', clickable: true, title: `Review ${watch.attention_count} warning${watch.attention_count === 1 ? '' : 's'}` };
    }
    if (watch.show_status === 'Ended') return { tag: 'ended', label: 'Ended', clickable: false, title: null };
    if (isIdle(watch)) return { tag: 'idle', label: 'Off-season', clickable: false, title: 'No upcoming episode announced on TVmaze; checked daily' };
    return null;
  }

  /**
   * Next check cell: relative time, or Paused when the watch is disabled. A
   * check before the release search can start only refreshes the TVmaze
   * schedule; the server re-checks daily even when the episode is days away.
   */
  function nextCheckLine(watch) {
    if (!watch.enabled) return { label: 'Paused', at: null, searchAt: null };
    const checkAt = watch.next_check_at ? Date.parse(watch.next_check_at) : NaN;
    const searchAt = watch.next_search_at ? Date.parse(watch.next_search_at) : NaN;
    return { label: relativeTime(watch.next_check_at), at: watch.next_check_at, searchAt: checkAt < searchAt ? watch.next_search_at : null };
  }

  async function refreshLogs() {
    if (!logsWatch?.id) return;
    logsLoading = true;
    logsError = '';
    try {
      logsEvents = await getSeriesEvents(logsWatch.id, Number(logsLimit) || 50);
    } catch (err) {
      logsError = err instanceof Error ? err.message : String(err);
    } finally {
      logsLoading = false;
    }
  }

  function stopLogsTimer() {
    if (logsTimer) {
      clearInterval(logsTimer);
      logsTimer = null;
    }
  }

  function startLogsTimer() {
    stopLogsTimer();
    if (!showLogs || !logsAutoRefresh) return;
    const intervalMs = Math.max(1, Number(logsInterval) || 1) * 1000;
    logsTimer = setInterval(refreshLogs, intervalMs);
  }

  function openLogs(watch) {
    logsWatch = watch;
    logsEvents = [];
    logsError = '';
    showLogs = true;
    refreshLogs();
    startLogsTimer();
  }

  function closeLogs() {
    showLogs = false;
    logsWatch = null;
    logsEvents = [];
    logsError = '';
    stopLogsTimer();
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

  // Mirrors the daemon's episodeOutDir: a picked season folder is replaced, not nested.
  const seasonFolderPattern = /^(s|season[ ._-]?)\d{1,3}$/i;

  function outputExample(root, _folder, organizeBySeason, season = 1) {
    let base = String(root || '').replace(/\/+$/, '');
    if (!base) return 'Choose the series folder';
    if (!organizeBySeason) return base;
    const slash = base.lastIndexOf('/');
    if (slash > 0 && seasonFolderPattern.test(base.slice(slash + 1))) base = base.slice(0, slash);
    return `${base}/s${String(season).padStart(2, '0')}`;
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

  $: watcherModalOpen = showWizard || Boolean(editing) || browserOpen || Boolean(attentionWatch) || showLogs;
  $: activeWatchCount = watches.filter((watch) => watch.enabled).length;
  $: attentionCount = watches.reduce((total, watch) => total + watch.attention_count, 0);
  $: scheduledCount = watches.filter((watch) => Boolean(watch.next_episode || watch.next_episode_at)).length;
  $: visibleCandidates = candidates.filter((candidate) => !hasDifferentSeriesTitle(candidate));
  $: pickedCandidate = pickWatcherCandidate(visibleCandidates);
  $: otherCandidates = visibleCandidates.filter((candidate) => candidate !== pickedCandidate && candidate.accepted !== false);
  $: rejectedCount = Math.max(0, previewTotalCandidates - candidates.filter((candidate) => candidate.accepted !== false).length);
  $: logsSubtitle = logsWatch ? `${logsWatch.display_name} · ${localTimeZone()}` : '';
  $: if (mounted) {
    if (watcherModalOpen || !active) stopRefreshTimer();
    else startRefreshTimer();
  }
  $: if (mounted && active !== wasActive) {
    wasActive = active;
    if (active) refresh();
  }
  $: onModalOpenChange(watcherModalOpen);
  $: {
    logsAutoRefresh;
    logsInterval;
    if (showLogs) startLogsTimer();
  }

  onMount(() => {
    mounted = true;
    wasActive = active;
    refresh();
    if (active) startRefreshTimer();
    return () => {
      mounted = false;
      stopRefreshTimer();
      stopLogsTimer();
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
        <p class="muted">Searches every 15 min after an episode ends, then hourly, then every 3 h for up to 3 days.</p>
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
          <colgroup><col class="automation-col-series" /><col class="automation-col-next" /><col class="automation-col-check" /><col class="automation-col-actions" /></colgroup>
          <thead><tr><th aria-sort={ariaSort('name')}><button class="sort" type="button" on:click={() => toggleSort('name')}>Series{sortIndicator('name')}</button></th><th aria-sort={ariaSort('episode')}><button class="sort" type="button" on:click={() => toggleSort('episode')}>Episode{sortIndicator('episode')}</button></th><th aria-sort={ariaSort('next')}><button class="sort" type="button" on:click={() => toggleSort('next')}>Next check{sortIndicator('next')}</button></th><th class="actions-col">Actions</th></tr></thead>
          <tbody>
            {#each sortedWatches as watch (watch.id)}
              {@const ep = episodeInFlight(watch)}
              {@const epStatus = episodeStatus(watch, ep)}
              {@const badge = seriesBadge(watch)}
              {@const nextCheck = nextCheckLine(watch)}
              <tr data-status={statusTag(watch)} class:automation-row-paused={!watch.enabled} class:automation-row-idle={isIdle(watch)}>
                <td class="cell-name automation-series-cell" data-label="Series">
                  <div class="automation-name-line">
                    <strong>{watch.display_name}</strong>
                    {#if badge}
                      {#if badge.clickable}
                        <button type="button" class="series-status status-btn" data-status={badge.tag} title={badge.title} on:click={() => openAttention(watch)}>{badge.label}</button>
                      {:else}
                        <span class="series-status" data-status={badge.tag}>{badge.label}</span>
                      {/if}
                    {/if}
                  </div>
                  <small class="automation-folder" title={watch.out_dir || 'No output folder'}>{watch.out_dir || 'No output folder'}</small>
                </td>
                <td class="automation-next-cell" data-label="Episode">
                  {#if ep}
                    <div class="automation-episode-line"><strong>{episodeCode(ep)}</strong><span>{episodeTitle(ep)}</span></div>
                    <time title={epStatus.at ? formatDateTimeShort(epStatus.at) : undefined}>{epStatus.label}</time>
                  {:else}
                    <span class="automation-episode-empty">{epStatus.label}</span>
                  {/if}
                  {#if watch.last_error}<div class="automation-warning">{watch.last_error}</div>{/if}
                </td>
                <td class="automation-check-cell" data-label="Next check">
                  <strong title={nextCheck.at ? formatDateTimeShort(nextCheck.at) : undefined}>{nextCheck.label}</strong>
                  {#if nextCheck.searchAt}<small title="The next check only refreshes the TVmaze schedule">search {formatAirTime(nextCheck.searchAt)}</small>{/if}
                  {#if watch.last_checked_at}<small title={formatDateTimeShort(watch.last_checked_at)}>checked {relativeTime(watch.last_checked_at)}</small>{/if}
                </td>
                <td class="actions-col" data-label="Actions">
                  <div class="actions row-actions automation-actions">
                    <button class="btn icon-btn action-btn action-retry" type="button" title={busyAction === `${watch.id}:check-now` ? 'Checking…' : 'Check now'} aria-label={`Check ${watch.display_name} now`} on:click={() => doAction(watch, 'check-now')} disabled={!watch.enabled || busyAction === `${watch.id}:check-now`}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M17.7 6.3A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.8-4.3L13 11h8V3z" /></svg></button>
                    <button class="btn icon-btn action-btn action-logs" type="button" title="Logs" aria-label={`Open logs for ${watch.display_name}`} on:click={() => openLogs(watch)}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 4h8l4 4v12H7z" stroke="currentColor" stroke-width="2" fill="none" /><path d="M15 4v4h4M10 13h6M10 16h6" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" /></svg></button>
                    <button class="btn icon-btn action-btn" class:action-pause={watch.enabled} class:action-resume={!watch.enabled} type="button" title={watch.enabled ? 'Pause' : 'Resume'} aria-label={`${watch.enabled ? 'Pause' : 'Resume'} ${watch.display_name}`} on:click={() => doAction(watch, watch.enabled ? 'pause' : 'resume')} disabled={busyAction === `${watch.id}:${watch.enabled ? 'pause' : 'resume'}`}><svg viewBox="0 0 24 24" aria-hidden="true">{#if watch.enabled}<path d="M7 5h4v14H7zm6 0h4v14h-4z" />{:else}<path d="M8 5v14l11-7z" />{/if}</svg></button>
                    <button class="btn icon-btn action-btn action-edit" type="button" title="Edit" aria-label={`Edit ${watch.display_name}`} on:click={() => startEdit(watch)}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 16.5-.5 4 4-.5L19 8.5 15.5 5zM17 3l4 4-1.5 1.5-4-4z" /></svg></button>
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
  bind:wizardStep
  {wizardBusy}
  {wizardError}
  {wizardNotice}
  bind:draft
  bind:folderEdited
  bind:showShowSearch
  {outDirPresets}
  {outDirFavorites}
  bind:profileFields
  bind:tvmazeQuery
  {tvmazeResults}
  {tvmazeBusy}
  {selectedShow}
  {pickedCandidate}
  {otherCandidates}
  {rejectedCount}
  {testRan}
  {nextEpisode}
  onClose={closeWizard}
  onNext={stepForward}
  onBack={stepBack}
  onActivate={activate}
  onFindShows={findShows}
  onChooseShow={chooseShow}
  onRetest={runCandidatePreview}
  onOpenBrowser={() => openBrowser('draft')}
  {onRemoveFavorite}
  {outputExample}
  {episodeCode}
  {episodeTitle}
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
  saving={busyAction === `${editing?.id}:edit` || busyAction === `${editing?.id}:remove`}
  error={editError}
  onClose={() => (editing = null)}
  onSave={saveEdit}
  onRemove={() => doAction(editing, 'remove')}
  onOpenBrowser={() => openBrowser('edit')}
  {outputExample}
/>

<LogsModal
  show={showLogs}
  title="Series Events"
  subtitle={logsSubtitle}
  {logsEvents}
  bind:logsLimit
  bind:logsAutoRefresh
  bind:logsInterval
  {logsError}
  {logsLoading}
  onClose={closeLogs}
  onRefresh={refreshLogs}
/>

<FolderBrowser
  bind:show={browserOpen}
  favoritePaths={outDirFavorites}
  onSelect={selectFolder}
  {onAddFavorite}
/>
