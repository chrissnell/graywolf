<script>
  import { onMount } from 'svelte';
  import { Toggle, Box, Select } from '@chrissnell/chonky-ui';
  import { messagesPreferencesState } from '../lib/settings/messages-preferences-store.svelte.js';
  import { channelsStore, start as startChannels } from '../lib/stores/channels.svelte.js';
  import { getMessagesConfig, putMessagesConfig } from '../api/messages.js';
  import PageHeader from '../components/PageHeader.svelte';

  const fallbackPolicyOptions = [
    { value: 'is_fallback', label: 'Try RF first, fall back to APRS-IS' },
    { value: 'is_only', label: 'APRS-IS only' },
    { value: 'rf_only', label: 'RF only' },
    { value: 'both', label: 'Send on RF and APRS-IS' },
  ];

  let txChannel = $state(0);

  onMount(async () => {
    messagesPreferencesState.fetchPreferences();
    startChannels();
    const cfg = await getMessagesConfig().catch(() => null);
    txChannel = cfg?.tx_channel ?? 0;
  });

  let txChannelOptions = $derived([
    { value: 0, label: 'Auto (first APRS-eligible channel)' },
    ...channelsStore.list
      .filter((c) => c.mode !== 'packet')
      .map((c) => ({ value: c.id, label: c.name })),
  ]);

  async function handleTxChannelChange(v) {
    const next = Number(v);
    txChannel = next;
    try {
      await putMessagesConfig({ tx_channel: next });
    } catch {
      const cfg = await getMessagesConfig().catch(() => null);
      txChannel = cfg?.tx_channel ?? 0;
    }
  }
</script>

<PageHeader title="Messaging" subtitle="APRS message sending options" />

<Box title="Messages">
  <p class="tx-channel-label">Digipeater path</p>
  <input
    class="path-input"
    type="text"
    placeholder="WIDE1-1,WIDE2-1"
    value={messagesPreferencesState.defaultPath}
    disabled={!messagesPreferencesState.loaded || messagesPreferencesState.saving}
    onchange={(e) => messagesPreferencesState.setDefaultPath(e.target.value)}
  />
  <p class="messages-hint">
    The digipeater path used for outbound APRS messages and bulletins.
    <strong>WIDE1-1,WIDE2-1</strong> is the standard for most fixed stations (2 hops).
    Use <strong>WIDE1-1</strong> for portable/mobile or when near a fill-in digipeater.
    Leave empty to transmit direct with no digipeating.
  </p>
  <Toggle
    checked={messagesPreferencesState.allowLong}
    onCheckedChange={(v) => messagesPreferencesState.setAllowLong(v)}
    label="Allow long APRS messages"
    disabled={!messagesPreferencesState.loaded || messagesPreferencesState.saving}
  />
  <p class="messages-hint">
    Lets you send messages up to 200 characters. Some receivers cannot
    decode longer messages and will truncate or drop them. Leave off
    unless you know your contacts support it.
  </p>
  <p class="tx-channel-label">Transmit channel</p>
  <Select
    value={txChannel}
    onValueChange={handleTxChannelChange}
    options={txChannelOptions}
    aria-label="Messages transmit channel"
  />
  <p class="messages-hint">
    Where graywolf sends outbound APRS messages. Auto picks the first
    APRS-eligible channel at send time.
  </p>
  <p class="tx-channel-label">Send path</p>
  <Select
    value={messagesPreferencesState.fallbackPolicy}
    onValueChange={(v) => messagesPreferencesState.setFallbackPolicy(v)}
    options={fallbackPolicyOptions}
    aria-label="Message send path"
    disabled={!messagesPreferencesState.loaded || messagesPreferencesState.saving}
  />
  <p class="messages-hint">
    Choose APRS-IS only if you have no radio channel configured. The
    default tries RF first and silently falls back to APRS-IS when no
    modem is available.
  </p>
</Box>

<Box title="Retry">
  <p class="tx-channel-label">Max retries</p>
  <input
    class="retry-input"
    type="number"
    min="1"
    max="20"
    value={messagesPreferencesState.retryMaxAttempts}
    disabled={!messagesPreferencesState.loaded || messagesPreferencesState.saving}
    onchange={(e) => messagesPreferencesState.setRetryMaxAttempts(e.target.value)}
  />
  <p class="messages-hint">
    How many times to retransmit an unacknowledged message before marking it
    failed. Default is 4 (1 initial send + 3 retries, ~90 seconds total).
  </p>
  <p class="tx-channel-label">Retry interval (seconds)</p>
  <input
    class="retry-input"
    type="number"
    min="10"
    max="120"
    value={messagesPreferencesState.retryIntervalSecs}
    disabled={!messagesPreferencesState.loaded || messagesPreferencesState.saving}
    onchange={(e) => messagesPreferencesState.setRetryIntervalSecs(e.target.value)}
  />
  <p class="messages-hint">
    How long to wait between retries. Default is 30 seconds. Range: 10–120
    seconds.
  </p>
</Box>

<style>
  .messages-hint {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .tx-channel-label {
    display: block;
    margin-top: 16px;
    margin-bottom: 6px;
    font-size: 13px;
    font-weight: 500;
    color: var(--text-default);
  }
  .path-input {
    width: 200px;
    padding: 6px 8px;
    font-size: 13px;
    font-family: monospace;
    border: 1px solid var(--border-default);
    border-radius: 6px;
    background: var(--bg-input, var(--bg-surface));
    color: var(--text-default);
  }
  .path-input:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .retry-input {
    width: 80px;
    padding: 6px 8px;
    font-size: 13px;
    border: 1px solid var(--border-default);
    border-radius: 6px;
    background: var(--bg-input, var(--bg-surface));
    color: var(--text-default);
  }
  .retry-input:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
