<script>
  import { onMount } from 'svelte';
  import { Box } from '@chrissnell/chonky-ui';
  import { channelsStore, start as startChannels } from '../lib/stores/channels.svelte.js';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import FormField from '../components/FormField.svelte';
  import { txPredicate } from '../lib/channelBacking.js';
  import { getBulletinsConfig, putBulletinsConfig } from '../api/bulletins.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';

  const sendPathOptions = [
    { value: 'rf',      label: 'RF only' },
    { value: 'both',    label: 'RF + APRS-IS' },
    { value: 'is_only', label: 'APRS-IS only' },
  ];

  let txChannel = $state(0);
  let sendPath = $state('rf');
  let loaded = $state(false);

  onMount(async () => {
    startChannels();
    const cfg = await getBulletinsConfig().catch(() => null);
    txChannel = cfg?.tx_channel ?? 0;
    sendPath  = cfg?.send_path  ?? 'rf';
    loaded = true;
  });

  let channels = $derived(channelsStore.list);

  async function handleTxChannelChange(channel) {
    const next = channel?.id ?? 0;
    try {
      const updated = await putBulletinsConfig({ tx_channel: next, send_path: sendPath });
      txChannel = updated?.tx_channel ?? next;
      toasts.success('Bulletins settings saved');
    } catch (e) {
      toasts.error(e?.message || 'Failed to save TX channel');
      const cfg = await getBulletinsConfig().catch(() => null);
      txChannel = cfg?.tx_channel ?? 0;
    }
  }

  async function handleSendPathChange(e) {
    const next = e.target.value;
    try {
      const updated = await putBulletinsConfig({ tx_channel: txChannel, send_path: next });
      sendPath = updated?.send_path ?? next;
      toasts.success('Bulletins settings saved');
    } catch (err) {
      toasts.error(err?.message || 'Failed to save send path');
      const cfg = await getBulletinsConfig().catch(() => null);
      sendPath = cfg?.send_path ?? 'rf';
    }
  }
</script>

<PageHeader title="Bulletins" subtitle="APRS bulletin sending options" />

<Box title="Bulletins">
  <FormField label="Transmit Channel" id="bltn-txch" hint="Which radio channel bulletins are sent on. Auto picks the first APRS-eligible channel at send time.">
    {#snippet children(describedBy)}
      <ChannelListbox
        id="bltn-txch"
        bind:value={txChannel}
        valueType="number"
        {channels}
        ariaLabelledBy={describedBy}
        capabilityFilter={txPredicate}
        allowNone
        noneLabel="Auto (first APRS-eligible channel)"
        onChange={handleTxChannelChange}
        disabled={!loaded}
      />
    {/snippet}
  </FormField>
  <FormField label="Send path" id="bltn-sendpath" hint="Where bulletins are transmitted. Applies to all bulletin groups.">
    {#snippet children(describedBy)}
      <select
        id="bltn-sendpath"
        class="send-path-select"
        value={sendPath}
        onchange={handleSendPathChange}
        aria-describedby={describedBy}
        disabled={!loaded}
      >
        {#each sendPathOptions as opt}
          <option value={opt.value}>{opt.label}</option>
        {/each}
      </select>
    {/snippet}
  </FormField>
</Box>

<style>
  .send-path-select {
    width: 100%;
    padding: 8px 10px;
    border: 1px solid var(--color-border, #ddd);
    border-radius: var(--radius, 4px);
    background: var(--color-surface, #fff);
    color: var(--color-text, #222);
    font-size: 14px;
    cursor: pointer;
  }
  .send-path-select:disabled {
    opacity: 0.6;
    cursor: default;
  }
</style>
