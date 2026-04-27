<script>
  import { onMount } from 'svelte';
  import { Button, Input, Box } from '@chrissnell/chonky-ui';
  import { api, ApiError } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';

  // Local reactive form state. The server persists the uppercased /
  // trimmed value, but we keep the in-flight user input verbatim so
  // focus/blur and mid-edit keystrokes don't fight the user. Visual
  // uppercase comes from `text-transform: uppercase` on the input.
  let callsign = $state('');
  let loading = $state(false);
  let saving = $state(false);
  let loadError = $state('');
  // Auto-disable notice. Populated only on a successful PUT whose
  // response `disabled` array is non-empty. Persists on-page until the
  // next save — the user acknowledges it implicitly by re-saving or
  // navigating away.
  let disabledList = $state([]);

  // Button disable logic: in flight, OR the page is still doing its
  // initial load. We do NOT require a non-empty value — the operator
  // is allowed to save an empty callsign to intentionally clear it
  // (auto-disabling iGate / Digipeater per D7).
  let saveDisabled = $derived(saving || loading);

  onMount(async () => {
    loading = true;
    loadError = '';
    try {
      const data = await api.get('/station/config');
      callsign = data?.callsign ?? '';
    } catch (err) {
      // Legacy `api.js` throws ApiError on non-2xx; network failures
      // fall through to its mock data path. Either way, surface a
      // short inline message and leave the input empty so the user
      // can still enter a value.
      loadError = 'Failed to load station callsign';
      // Best-effort: show the server's error text if we have one.
      if (err instanceof ApiError && err.body?.error) {
        loadError = err.body.error;
      }
    } finally {
      loading = false;
    }
  });

  async function handleSave(e) {
    e.preventDefault();
    if (saving) return;
    saving = true;
    // Clear the prior auto-disable notice and any stale load-error at
    // the start of a new save; either may be repopulated from the
    // fresh response if applicable.
    disabledList = [];
    loadError = '';
    const normalized = callsign.trim().toUpperCase();
    try {
      const res = await api.put('/station/config', { callsign: normalized });
      // Mirror the server's canonicalized value back into the form so
      // the user sees exactly what's stored. Preserves "" if they
      // cleared it.
      callsign = res?.callsign ?? '';
      // Surface auto-disable side-effect inline. `disabled` is omitted
      // when empty (per Phase 3B handoff), so guard on length.
      if (Array.isArray(res?.disabled) && res.disabled.length > 0) {
        disabledList = res.disabled;
      }
      toasts.success('Station callsign saved');
    } catch (err) {
      // Preserve the user's typed input on failure — do NOT wipe it.
      const msg =
        (err instanceof ApiError && err.body?.error) ||
        err?.message ||
        'Failed to save station callsign';
      toasts.error(msg);
    } finally {
      saving = false;
    }
  }

  // Build the auto-disable notice from the canonical feature names
  // (`"igate"`, `"digipeater"`) returned by the server. Capitalization
  // follows the plan: iGate, Digipeater.
  let disabledNotice = $derived.by(() => {
    if (!disabledList.length) return '';
    const hasIgate = disabledList.includes('igate');
    const hasDigi = disabledList.includes('digipeater');
    if (hasIgate && hasDigi) {
      return 'iGate and Digipeater were disabled because the station callsign was cleared.';
    }
    if (hasIgate) {
      return 'iGate was disabled because the station callsign was cleared.';
    }
    if (hasDigi) {
      return 'Digipeater was disabled because the station callsign was cleared.';
    }
    return '';
  });
</script>

<PageHeader
  title="Station Callsign"
  subtitle="This callsign is used for APRS-IS login, messages, and as the default for beacons and the digipeater."
/>

<Box>
  <form onsubmit={handleSave}>
    <FormField
      label="Callsign"
      id="station-callsign"
      error={loadError}
    >
      {#snippet children(describedBy)}
        <Input
          id="station-callsign"
          class="callsign-input"
          bind:value={callsign}
          placeholder="e.g. KE7XYZ-9"
          autocomplete="off"
          spellcheck={false}
          disabled={loading}
          aria-describedby={describedBy}
        />
      {/snippet}
    </FormField>
    <div class="form-actions">
      <Button variant="primary" type="submit" disabled={saveDisabled}>
        {saving ? 'Saving...' : 'Save'}
      </Button>
    </div>
    {#if disabledNotice}
      <div class="disabled-notice" role="status">
        {disabledNotice}
      </div>
    {/if}
  </form>
</Box>

<style>
  /* Visual-only uppercase hint. Persistence-side uppercase happens on
     save (trim + toUpperCase) so leading/trailing whitespace is also
     normalized. Chonky's <Input> forwards `class` straight onto the
     underlying <input>, so :global is required to reach it through
     Svelte's scoped-CSS boundary. */
  :global(input.callsign-input) {
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .form-actions {
    display: flex;
    justify-content: flex-end;
    margin-top: 16px;
  }
  /* Auto-disable notice: amber callout, matching the pattern used in
     Igate.svelte / Digipeater.svelte for RF-side warnings. Persists
     until the user re-saves or navigates away. */
  .disabled-notice {
    margin: 16px 0 0;
    padding: 10px 12px;
    border: 1px solid var(--color-warning, #d4a72c);
    border-left-width: 4px;
    border-radius: 4px;
    background: var(--color-warning-bg, rgba(212, 167, 44, 0.12));
    color: var(--text-primary);
    font-size: 13px;
    line-height: 1.45;
    max-width: 720px;
  }
</style>
