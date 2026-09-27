<script>
  export let show = false;
  export let settingsConcurrency = 2;
  export let settingsMaxAttempts = 5;
  export let settingsAutoDecrypt = true;
  export let settingsError = '';
  export let settingsSaving = false;

  export let settingsTelegramEnabled = false;
  export let settingsTelegramBotToken = '';
  export let settingsTelegramBotTokenSet = false;
  export let settingsTelegramChatId = '';
  export let settingsTelegramEvents = { completed: true, failed: true, retrying: false, extract_failed: true };
  export let settingsTelegramCompletedTemplate = '';
  export let settingsTelegramFailureTemplate = '';
  export let settingsTelegramTesting = false;
  export let settingsTelegramTestResult = '';

  export let onClose = () => {};
  export let onSave = () => {};
  export let onTestTelegram = () => {};

  const TAB_GENERAL = 'general';
  const TAB_NOTIFICATIONS = 'notifications';

  let tab = TAB_GENERAL;

  function onBackdropKeydown(event) {
    if (event.key === 'Enter' || event.key === ' ' || event.key === 'Escape') {
      event.preventDefault();
      onClose();
    }
  }
</script>

{#if show}
  <div
    class="modal-backdrop"
    role="button"
    tabindex="0"
    aria-label="Close dialog"
    on:click={onClose}
    on:keydown={onBackdropKeydown}
  ></div>
  <div class="modal panel settings-dialog" role="dialog" aria-modal="true">
    <div class="modal-header">
      <div>
        <h2 style="margin: 0;">Settings</h2>
        <p class="notice">Configure runtime settings</p>
      </div>
      <button class="btn icon-btn close-btn" type="button" aria-label="Close dialog" on:click={onClose}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="m7 7 10 10M17 7 7 17" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" fill="none" />
        </svg>
      </button>
    </div>
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
      {#if tab === TAB_GENERAL}
        <div class="settings-row">
          <div>
            <label for="settings-concurrency">Concurrency (1-10)</label>
            <input id="settings-concurrency" type="number" min="1" max="10" bind:value={settingsConcurrency} />
            <p class="notice">Number of concurrent downloads</p>
          </div>
          <div>
            <label for="settings-max-attempts">Max attempts (1-20)</label>
            <input id="settings-max-attempts" type="number" min="1" max="20" bind:value={settingsMaxAttempts} />
            <p class="notice">Maximum retries before marking a job failed</p>
          </div>
        </div>
        <div>
          <label class="small" for="settings-auto-decrypt">
            <input id="settings-auto-decrypt" type="checkbox" bind:checked={settingsAutoDecrypt} />
            auto decrypt archives after download
          </label>
        </div>
      {:else}
        <div>
          <label class="small" for="settings-telegram-enabled">
            <input id="settings-telegram-enabled" type="checkbox" bind:checked={settingsTelegramEnabled} />
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
              placeholder={settingsTelegramBotTokenSet ? 'stored — leave empty to keep' : ''}
            />
          </div>
          <div>
            <label for="settings-telegram-chat-id">Chat ID</label>
            <input id="settings-telegram-chat-id" type="text" bind:value={settingsTelegramChatId} />
          </div>
        </div>
        <div class="actions">
          <label class="small" for="settings-telegram-event-completed">
            <input id="settings-telegram-event-completed" type="checkbox" bind:checked={settingsTelegramEvents.completed} />
            completed
          </label>
          <label class="small" for="settings-telegram-event-failed">
            <input id="settings-telegram-event-failed" type="checkbox" bind:checked={settingsTelegramEvents.failed} />
            failed
          </label>
          <label class="small" for="settings-telegram-event-retrying">
            <input id="settings-telegram-event-retrying" type="checkbox" bind:checked={settingsTelegramEvents.retrying} />
            retrying
          </label>
          <label class="small" for="settings-telegram-event-extract-failed">
            <input
              id="settings-telegram-event-extract-failed"
              type="checkbox"
              bind:checked={settingsTelegramEvents.extract_failed}
            />
            extract failed
          </label>
        </div>
        <div>
          <label for="settings-telegram-completed-template">Completed message template</label>
          <textarea id="settings-telegram-completed-template" rows="2" bind:value={settingsTelegramCompletedTemplate}
          ></textarea>
        </div>
        <div>
          <label for="settings-telegram-failure-template">Failure message template</label>
          <textarea id="settings-telegram-failure-template" rows="2" bind:value={settingsTelegramFailureTemplate}
          ></textarea>
        </div>
        <p class="notice">
          Placeholders: {'{name} {filename} {size} {size_bytes} {time} {duration} {speed} {site} {url} {dir} {id} {parts} {status} {event} {error} {error_code} {attempts} {max_attempts} {series} {episode} {episode_title}'}
        </p>
        <div class="actions">
          <button class="btn" type="button" on:click={onTestTelegram} disabled={settingsTelegramTesting}>
            {settingsTelegramTesting ? 'Sending...' : 'Send test'}
          </button>
          {#if settingsTelegramTestResult}
            <span class="notice">{settingsTelegramTestResult}</span>
          {/if}
        </div>
      {/if}
    </div>
    <div class="actions settings-footer">
      <button class="btn primary" on:click={onSave} disabled={settingsSaving}>
        {settingsSaving ? 'Saving...' : 'Save'}
      </button>
      <button class="btn ghost" on:click={onClose}>Cancel</button>
    </div>
    {#if settingsError}
      <p class="notice">Error: {settingsError}</p>
    {/if}
  </div>
{/if}
