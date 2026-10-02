<script>
  import Modal from '$lib/components/Modal.svelte';
  import { notificationsEnabled, notificationsUnavailableReason, setNotificationsEnabled } from '$lib/notifications';

  export let show = false;
  export let settingsConcurrency = 2;
  export let settingsMaxAttempts = 5;
  export let settingsAutoDecrypt = true;
  export let settingsError = '';
  export let settingsSaving = false;
  export let settingsLoading = false;
  export let settingsLoadError = '';

  export let settingsTelegramEnabled = false;
  export let settingsTelegramBotToken = '';
  export let settingsTelegramBotTokenSet = false;
  export let settingsTelegramChatId = '';
  export let settingsTelegramEvents = { completed: true, failed: true, retrying: false, extract_failed: true };
  export let settingsTelegramCompletedTemplate = '';
  export let settingsTelegramFailureTemplate = '';
  export let settingsTelegramTesting = false;
  export let settingsTelegramTestResult = '';
  export let settingsTelegramTestError = '';

  export let onClose = () => {};
  export let onSave = () => {};
  export let onTestTelegram = () => {};
  export let onRetryLoad = () => {};

  const TAB_GENERAL = 'general';
  const TAB_NOTIFICATIONS = 'notifications';

  let tab = TAB_GENERAL;

  const notifyUnavailable = notificationsUnavailableReason();
  let notifyOn = notificationsEnabled();

  async function onNotifyChange() {
    notifyOn = await setNotificationsEnabled(notifyOn);
  }

  $: controlsDisabled = settingsLoading || !!settingsLoadError;
  $: telegramDisabled = controlsDisabled || !settingsTelegramEnabled;
</script>

<Modal {show} title="Settings" subtitle="Configure runtime settings" className="settings-dialog" closeLabel="Close dialog" {onClose}>
  <div class="settings-tabs" role="tablist">
    <button
      class="settings-tab"
      class:active={tab === TAB_GENERAL}
      type="button"
      role="tab"
      aria-selected={tab === TAB_GENERAL}
      on:click={() => (tab = TAB_GENERAL)}
    >
      General
    </button>
    <button
      class="settings-tab"
      class:active={tab === TAB_NOTIFICATIONS}
      type="button"
      role="tab"
      aria-selected={tab === TAB_NOTIFICATIONS}
      on:click={() => (tab = TAB_NOTIFICATIONS)}
    >
      Notifications
    </button>
  </div>
  <div class="form-grid settings-body">
    {#if settingsLoading}
      <p class="notice">Loading settings…</p>
    {/if}
    {#if settingsLoadError}
      <div class="alert error" role="alert">
        <span>{settingsLoadError}</span>
        <button class="btn tiny ghost" type="button" on:click={onRetryLoad}>Retry</button>
      </div>
    {/if}
    {#if settingsError}
      <p class="alert error" role="alert">{settingsError}</p>
    {/if}
    {#if tab === TAB_GENERAL}
      <div class="settings-row">
        <div>
          <label for="settings-concurrency">Concurrency (1-10)</label>
          <input id="settings-concurrency" type="number" min="1" max="10" bind:value={settingsConcurrency} disabled={controlsDisabled} />
          <p class="notice">Number of concurrent downloads</p>
        </div>
        <div>
          <label for="settings-max-attempts">Max attempts (1-20)</label>
          <input id="settings-max-attempts" type="number" min="1" max="20" bind:value={settingsMaxAttempts} disabled={controlsDisabled} />
          <p class="notice">Maximum retries before marking a job failed</p>
        </div>
      </div>
      <div>
        <label class="small" for="settings-auto-decrypt">
          <input id="settings-auto-decrypt" type="checkbox" bind:checked={settingsAutoDecrypt} disabled={controlsDisabled} />
          Auto decrypt archives after download
        </label>
      </div>
    {:else}
      <div>
        <label class="small" for="settings-notify">
          <input id="settings-notify" type="checkbox" bind:checked={notifyOn} disabled={!!notifyUnavailable} on:change={onNotifyChange} />
          Browser notifications when jobs finish or fail
        </label>
        <p class="notice">{notifyUnavailable || 'This browser only; works while this tab is open.'}</p>
      </div>
      <div>
        <label class="small" for="settings-telegram-enabled">
          <input id="settings-telegram-enabled" type="checkbox" bind:checked={settingsTelegramEnabled} disabled={controlsDisabled} />
          Telegram notifications
        </label>
      </div>
      <div class="settings-row">
        <div>
          <label for="settings-telegram-token">Bot token</label>
          <input
            id="settings-telegram-token"
            type="password"
            autocomplete="off"
            bind:value={settingsTelegramBotToken}
            disabled={telegramDisabled}
            placeholder={settingsTelegramBotTokenSet ? 'stored — leave empty to keep' : ''}
          />
        </div>
        <div>
          <label for="settings-telegram-chat-id">Chat ID</label>
          <input id="settings-telegram-chat-id" type="text" bind:value={settingsTelegramChatId} disabled={telegramDisabled} />
        </div>
      </div>
      <div class="actions">
        <label class="small" for="settings-telegram-event-completed">
          <input id="settings-telegram-event-completed" type="checkbox" bind:checked={settingsTelegramEvents.completed} disabled={telegramDisabled} />
          Completed
        </label>
        <label class="small" for="settings-telegram-event-failed">
          <input id="settings-telegram-event-failed" type="checkbox" bind:checked={settingsTelegramEvents.failed} disabled={telegramDisabled} />
          Failed
        </label>
        <label class="small" for="settings-telegram-event-retrying">
          <input id="settings-telegram-event-retrying" type="checkbox" bind:checked={settingsTelegramEvents.retrying} disabled={telegramDisabled} />
          Retrying
        </label>
        <label class="small" for="settings-telegram-event-extract-failed">
          <input
            id="settings-telegram-event-extract-failed"
            type="checkbox"
            bind:checked={settingsTelegramEvents.extract_failed}
            disabled={telegramDisabled}
          />
          Extract failed
        </label>
      </div>
      <div>
        <label for="settings-telegram-completed-template">Completed message template</label>
        <textarea id="settings-telegram-completed-template" rows="2" bind:value={settingsTelegramCompletedTemplate} disabled={telegramDisabled}
        ></textarea>
      </div>
      <div>
        <label for="settings-telegram-failure-template">Failure message template</label>
        <textarea id="settings-telegram-failure-template" rows="2" bind:value={settingsTelegramFailureTemplate} disabled={telegramDisabled}
        ></textarea>
      </div>
      <p class="notice">
        Placeholders: {'{name} {filename} {size} {size_bytes} {time} {duration} {speed} {site} {url} {dir} {id} {parts} {status} {event} {error} {error_code} {attempts} {max_attempts} {series} {episode} {episode_title}'}
      </p>
      <div class="actions">
        <button class="btn" type="button" on:click={onTestTelegram} disabled={settingsTelegramTesting || telegramDisabled}>
          {settingsTelegramTesting ? 'Sending…' : 'Send test'}
        </button>
      </div>
      {#if settingsTelegramTestResult}
        <p class="alert success" role="status">{settingsTelegramTestResult}</p>
      {/if}
      {#if settingsTelegramTestError}
        <p class="alert error" role="alert">{settingsTelegramTestError}</p>
      {/if}
    {/if}
  </div>
  <div class="actions settings-footer">
    <button class="btn primary" type="button" on:click={onSave} disabled={settingsSaving || controlsDisabled}>
      {settingsSaving ? 'Saving…' : 'Save'}
    </button>
    <button class="btn ghost" type="button" on:click={onClose}>Cancel</button>
  </div>
</Modal>
