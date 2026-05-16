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
</style>
