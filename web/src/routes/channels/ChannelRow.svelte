<script>
  import { Badge, Button } from '@chrissnell/chonky-ui';
  import {
    healthGlyph,
    healthText,
    summaryLabel,
    ariaLabel as backingAriaLabel,
    tooltipText as backingTooltip,
    HEALTH_LIVE,
    HEALTH_DOWN,
  } from '../../lib/channelBacking.js';
  import {
    summaryLine as pttSummaryLine,
    pttState,
    ariaLabel as pttAriaLabel,
  } from '../../lib/channelPtt.js';

  let { channel, txTiming, audioDevices = [], onEdit, onDelete } = $props();

  let isKissOnly = $derived(channel.input_device_id == null);

  function deviceName(id) {
    if (!id || id === 0) return null;
    const d = audioDevices.find(d => d.id === id);
    return d ? d.name : `Device #${id}`;
  }

  function channelLabel(ch) {
    return ch === 0 ? 'Left/Mono' : ch === 1 ? 'Right' : `Ch ${ch}`;
  }
</script>

<div class="channel-card">
  <div class="channel-header">
    <span class="channel-name">{channel.name}</span>
    <div class="channel-badges">
      {#if isKissOnly}
        <Badge variant="info">KISS-TNC only</Badge>
      {:else}
        <Badge variant="default">{channel.modem_type.toUpperCase()}</Badge>
        {#if channel.output_device_id && channel.output_device_id !== 0}
          <Badge variant="success">RX/TX</Badge>
        {:else}
          <Badge variant="info">RX</Badge>
        {/if}
      {/if}
      {#if channel.mode === 'packet'}
        <Badge variant="warning">Packet</Badge>
      {:else if channel.mode === 'aprs+packet'}
        <Badge variant="info">APRS + Packet</Badge>
      {:else}
        <Badge variant="info">APRS</Badge>
      {/if}
    </div>
  </div>

  {#if !isKissOnly}
    <div class="channel-devices">
      <div class="device-link">
        <span class="device-direction">RX</span>
        <div class="device-info">
          <span class="device-name-ref">{deviceName(channel.input_device_id) || '—'}</span>
          <span class="device-ch">{channelLabel(channel.input_channel)}</span>
        </div>
      </div>
      {#if channel.output_device_id && channel.output_device_id !== 0}
        <div class="device-link">
          <span class="device-direction tx">TX</span>
          <div class="device-info">
            <span class="device-name-ref">{deviceName(channel.output_device_id)}</span>
            <span class="device-ch">{channelLabel(channel.output_channel)}</span>
          </div>
        </div>
      {/if}
    </div>
  {:else}
    <div class="channel-kiss-only-note">
      Serviced by a KISS TNC interface (configured on the KISS page).
    </div>
  {/if}

  {#if channel.backing}
    {@const h = channel.backing.health}
    {@const glyphClass = h === HEALTH_LIVE ? 'live' : h === HEALTH_DOWN ? 'down' : 'unbound'}
    <div class="backing-row"
         aria-label={backingAriaLabel(channel)}
         title={backingTooltip(channel.backing)}>
      <span class="backing-label">Backing</span>
      <span class="backing-summary">
        <span class="glyph {glyphClass}" aria-hidden="true">{healthGlyph(h)}</span>
        <span class="backing-text">{summaryLabel(channel.backing)} · {healthText(h)}</span>
      </span>
    </div>
  {/if}

  <!-- PTT indicator (issue #112). Only shown for modem-backed TX
       channels: KISS-TNC channels handle keying inside the TNC
       firmware, and an RX-only modem channel can't transmit so
       PTT has no role to play either. -->
  {#if !isKissOnly && channel.output_device_id && channel.output_device_id !== 0}
    {@const pttGlyphClass = pttState(channel.ptt)}
    <div class="backing-row"
         aria-label={pttAriaLabel(channel.ptt)}>
      <span class="backing-label">PTT</span>
      <span class="backing-summary">
        <span class="glyph {pttGlyphClass}" aria-hidden="true">
          {pttGlyphClass === 'live' ? '●' : '○'}
        </span>
        <span class="backing-text">{pttSummaryLine(channel.ptt)}</span>
      </span>
    </div>
  {/if}

  <div class="channel-details">
    {#if !isKissOnly}
      <div class="detail-row">
        <span class="detail-label">Bit Rate</span>
        <span class="detail-value">{channel.bit_rate} bps</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">Mark / Space</span>
        <span class="detail-value">{channel.mark_freq} / {channel.space_freq} Hz</span>
      </div>
      {#if channel.output_device_id && channel.output_device_id !== 0 && txTiming}
        {@const t = txTiming}
        <div class="detail-row">
          <span class="detail-label">TXD / Tail</span>
          <span class="detail-value">{t.tx_delay_ms} / {t.tx_tail_ms} ms</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">CSMA</span>
          <span class="detail-value">p{t.persist} slot {t.slot_ms}ms{t.full_dup ? ' FDX' : ''}</span>
        </div>
      {/if}
    {/if}
  </div>

  <div class="channel-actions">
    <Button variant="ghost" onclick={() => onEdit?.(channel)}>Edit</Button>
    <Button variant="danger" onclick={() => onDelete?.(channel)}>Delete</Button>
  </div>
</div>

<style>
  .channel-card {
    display: flex;
    flex-direction: column;
    padding: 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
  }

  .channel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
    gap: 8px;
  }
  .channel-name {
    font-weight: 600;
    font-size: 15px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .channel-badges {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }

  /* RX/TX device links */
  .channel-devices {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
    padding: 10px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
  }
  .device-link {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .device-direction {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    color: var(--color-info);
    background: var(--color-info-muted);
    padding: 2px 6px;
    border-radius: 3px;
    flex-shrink: 0;
    min-width: 26px;
    text-align: center;
  }
  .device-direction.tx {
    color: var(--color-success);
    background: var(--color-success-muted);
  }
  .device-info {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    font-size: 13px;
  }
  .device-name-ref {
    color: var(--text-primary);
    font-weight: 500;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .device-ch {
    color: var(--text-secondary);
    font-size: 12px;
    flex-shrink: 0;
  }

  .channel-details {
    display: flex;
    flex-direction: column;
    gap: 6px;
    flex: 1;
  }
  .detail-row {
    display: flex;
    justify-content: space-between;
    font-size: 13px;
    gap: 12px;
  }
  .detail-label {
    color: var(--text-secondary);
    flex-shrink: 0;
  }
  .detail-value {
    font-family: var(--font-mono);
    color: var(--text-primary);
    text-align: right;
  }

  .channel-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }

  /* Backing summary row on each channel card. */
  .backing-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 10px;
    padding: 6px 10px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
    font-size: 12px;
    color: var(--text-secondary);
  }
  .backing-label {
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
  }
  .backing-summary {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .backing-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .glyph {
    display: inline-flex;
    width: 12px;
    height: 12px;
    line-height: 1;
    font-size: 12px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }
  .glyph.live {
    color: var(--color-success, #2ea043);
  }
  .glyph.down {
    color: var(--color-warning, #d4a72c);
  }
  .glyph.unbound {
    color: var(--text-muted, #888);
  }

  .channel-kiss-only-note {
    padding: 10px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
    font-size: 13px;
    color: var(--text-secondary);
    margin-bottom: 12px;
  }
</style>
