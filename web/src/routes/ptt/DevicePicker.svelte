<!-- web/src/routes/ptt/DevicePicker.svelte -->
<script>
  import { Button, Badge } from '@chrissnell/chonky-ui';

  // Device list with recommended/other split + optional Android permission
  // CTA. Pure presentation: parent supplies the filtered device array,
  // current selection, and (Android) permission-request callback.
  let {
    devices,             // array of AvailableDevice / PttDevice
    selectedPath,        // string | null; matches device.path
    onSelect,            // (device) => void
    onRequestPermission, // (device) => void; Android only — desktop omits
  } = $props();

  // Server-side classification: `recommended:bool` on every device row.
  let recommended = $derived(devices.filter(d => d.recommended));
  let others = $derived(devices.filter(d => !d.recommended));

  function typeBadgeVariant(type) {
    if (type === 'serial') return 'info';
    if (type === 'gpio') return 'info';
    if (type === 'cm108') return 'success';
    return 'default';
  }
</script>

{#if devices.length === 0}
  <div class="empty">
    No matching devices detected. Plug in the adapter and click Refresh.
    If the adapter is plugged in but still not detected after a swap,
    restart Graywolf.
  </div>
{:else}
  {#if recommended.length > 0}
    <section class="section">
      <h4 class="section-title">Recommended</h4>
      <ul class="device-list">
        {#each recommended as d (d.path || `${d.usb_vendor}:${d.usb_product}`)}
          <li>
            <button
              type="button"
              class="device-card"
              class:selected={d.path === selectedPath}
              onclick={() => onSelect(d)}
            >
              <div class="row">
                <strong>{d.description || d.name}</strong>
                <Badge variant={typeBadgeVariant(d.type)}>{d.type}</Badge>
              </div>
              {#if d.path}
                <span class="path">{d.path}</span>
              {/if}
              {#if d.usb_vendor && d.usb_product}
                <span class="usb">USB {d.usb_vendor}:{d.usb_product}</span>
              {/if}
              {#if d.has_permission === false && onRequestPermission}
                <Button onclick={(e) => { e.stopPropagation(); onRequestPermission(d); }}>
                  Request permission
                </Button>
              {/if}
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}
  {#if others.length > 0}
    <section class="section section-muted">
      <h4 class="section-title">Other detected devices</h4>
      <ul class="device-list">
        {#each others as d (d.path || `${d.usb_vendor}:${d.usb_product}`)}
          <li>
            <button
              type="button"
              class="device-card device-card-muted"
              class:selected={d.path === selectedPath}
              onclick={() => onSelect(d)}
            >
              <div class="row">
                <strong>{d.description || d.name}</strong>
                <Badge variant={typeBadgeVariant(d.type)}>{d.type}</Badge>
              </div>
              {#if d.path}
                <span class="path">{d.path}</span>
              {/if}
              {#if d.warning}
                <span class="warning">{d.warning}</span>
              {/if}
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}
{/if}

<style>
  .section { margin-bottom: 16px; }
  .section-title {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-secondary, #555);
    margin: 0 0 6px 0;
  }
  .section-muted .section-title { color: var(--text-muted, #888); }
  .device-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 6px; }
  .device-card {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 8px 10px;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    background: var(--bg-surface, #fff);
    text-align: left;
    cursor: pointer;
  }
  .device-card.selected { border-color: var(--accent, #3b82f6); background: var(--bg-selected, #eff6ff); }
  .device-card-muted { opacity: 0.85; }
  .row { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
  .path { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; color: var(--text-secondary, #555); }
  .usb { font-size: 11px; color: var(--text-muted, #888); }
  .warning { font-size: 12px; color: #b45309; }
  .empty { padding: 12px; color: var(--text-muted, #888); text-align: center; }
</style>
