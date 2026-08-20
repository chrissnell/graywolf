<script>
  import { onMount } from 'svelte';
  import { Box, Button, Toggle, Input } from '@chrissnell/chonky-ui';
  import { channelsStore, start as startChannels } from '../lib/stores/channels.svelte.js';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import FormField from '../components/FormField.svelte';
  import { txPredicate } from '../lib/channelBacking.js';
  import { getWeatherConfig, putWeatherConfig } from '../api/weather.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';

  let loaded = $state(false);
  let saving = $state(false);

  let enabled = $state(false);
  let txChannelId = $state(0);
  let maxDistanceMiles = $state(50);
  // Stored as seconds on the server; the UI presents minutes.
  let minIntervalMinutes = $state(5);
  let alertClearMinutes = $state(10);

  let channels = $derived(channelsStore.list);

  onMount(async () => {
    startChannels();
    const cfg = await getWeatherConfig().catch(() => null);
    if (cfg) {
      enabled = cfg.enabled ?? false;
      txChannelId = cfg.tx_channel_id ?? 0;
      maxDistanceMiles = cfg.max_distance_miles ?? 50;
      minIntervalMinutes = Math.round((cfg.min_interval_seconds ?? 300) / 60);
      alertClearMinutes = cfg.alert_clear_minutes ?? 10;
    }
    loaded = true;
  });

  async function save() {
    saving = true;
    try {
      const minutes = Math.max(5, parseInt(minIntervalMinutes, 10) || 5);
      const clearMins = Math.max(1, parseInt(alertClearMinutes, 10) || 10);
      await putWeatherConfig({
        enabled,
        tx_channel_id: txChannelId,
        max_distance_miles: parseFloat(maxDistanceMiles) || 50,
        min_interval_seconds: minutes * 60,
        alert_clear_minutes: clearMins,
      });
      minIntervalMinutes = minutes;
      alertClearMinutes = clearMins;
      toasts.success('Weather settings saved');
    } catch (e) {
      toasts.error(e?.message || 'Failed to save weather settings');
    } finally {
      saving = false;
    }
  }

  async function handleTxChannelChange(channel) {
    txChannelId = channel?.id ?? 0;
    await save();
  }

  async function handleEnabledChange(v) {
    enabled = v;
    await save();
  }
</script>

<PageHeader title="Weather" subtitle="NWS weather alert forwarding settings" />

<Box title="Weather Alert Forwarding">
  <Toggle
    checked={enabled}
    onCheckedChange={handleEnabledChange}
    label="Enable weather forwarding"
    disabled={!loaded || saving}
  />
  <p class="hint">
    When enabled, NWS weather alert packets received from APRS-IS are
    selectively retransmitted over RF based on the settings below.
  </p>

  <FormField label="Transmit Channel" id="wx-txch" hint="Radio channel for forwarded NWS packets. Required to forward over RF.">
    {#snippet children(describedBy)}
      <ChannelListbox
        id="wx-txch"
        bind:value={txChannelId}
        valueType="number"
        {channels}
        ariaLabelledBy={describedBy}
        capabilityFilter={txPredicate}
        allowNone
        noneLabel="Disabled (no RF forwarding)"
        onChange={handleTxChannelChange}
        disabled={!loaded}
      />
    {/snippet}
  </FormField>

  <FormField
    label="Max distance (miles)"
    id="wx-dist"
    hint="Counties further than this distance from your position will not have alerts forwarded. Applies to both county alerts and NWS position objects."
  >
    <Input
      id="wx-dist"
      type="number"
      min="1"
      step="1"
      bind:value={maxDistanceMiles}
      disabled={!loaded}
    />
  </FormField>

  <FormField
    label="Min interval between sends (minutes)"
    id="wx-interval"
    hint="Minimum time between RF transmissions of the same alert. Minimum 5 minutes. A status or type change bypasses this and forwards immediately."
  >
    <Input
      id="wx-interval"
      type="number"
      min="5"
      step="1"
      bind:value={minIntervalMinutes}
      disabled={!loaded}
    />
  </FormField>

  <FormField
    label="Alert clear time (minutes)"
    id="wx-clear"
    hint="Minutes without a new packet before an active alert is automatically marked clear. Minimum 1 minute."
  >
    <Input
      id="wx-clear"
      type="number"
      min="1"
      step="1"
      bind:value={alertClearMinutes}
      disabled={!loaded}
    />
  </FormField>

  <div class="form-actions">
    <Button variant="primary" onclick={save} disabled={!loaded || saving}>
      {saving ? 'Saving…' : 'Save'}
    </Button>
  </div>
</Box>

<style>
  .form-actions {
    margin-top: 20px;
    display: flex;
    justify-content: flex-end;
  }
  .hint {
    margin: 8px 0 16px;
    font-size: 13px;
    color: var(--text-muted);
  }
</style>
