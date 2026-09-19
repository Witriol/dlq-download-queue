<script>
  import { browse, mkdir } from '$lib/api';
  import BrowserModal from '$lib/components/BrowserModal.svelte';

  export let show = false;
  export let favoritePaths = [];
  export let onSelect = () => {};
  export let onAddFavorite = () => {};

  let browserPath = '';
  let browserDirs = [];
  let browserParent = '';
  let browserIsRoot = false;
  let browserError = '';
  let browserLoading = false;
  let browserNewFolderName = '';
  let opened = false;

  async function loadBrowser(path = '') {
    browserLoading = true;
    browserError = '';
    try {
      const result = await browse(path || undefined);
      browserPath = result.path;
      browserParent = result.parent;
      browserDirs = result.dirs || [];
      browserIsRoot = result.is_root;
      browserNewFolderName = '';
    } catch (err) {
      browserError = err instanceof Error ? err.message : String(err);
    } finally {
      browserLoading = false;
    }
  }

  async function createFolder() {
    const name = browserNewFolderName.trim();
    if (!name) return;
    const path = browserPath ? `${browserPath}/${name}` : `/${name}`;
    browserError = '';
    try {
      await mkdir(path);
      selectPath(path);
    } catch (err) {
      browserError = err instanceof Error ? err.message : String(err);
    }
  }

  function selectPath(path) {
    onSelect(path);
    show = false;
  }

  $: if (show && !opened) {
    opened = true;
    loadBrowser();
  }
  $: if (!show) opened = false;
</script>

<BrowserModal
  {show}
  {browserPath}
  {browserDirs}
  {browserParent}
  {browserIsRoot}
  {browserError}
  {browserLoading}
  browserFavoritePaths={favoritePaths}
  bind:browserNewFolderName
  onClose={() => (show = false)}
  onLoadBrowser={loadBrowser}
  onCreateFolder={createFolder}
  onSelectPath={selectPath}
  {onAddFavorite}
/>
