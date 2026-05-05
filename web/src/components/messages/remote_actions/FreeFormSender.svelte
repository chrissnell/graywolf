<script>
  // FreeFormSender -- input row at the bottom of the drawer for ad-hoc
  // command fires.
  //
  // Behaviour:
  //   - "Active OTP" picker selects the credential whose code is
  //     auto-injected. None reveals a manual six-digit input.
  //   - "OTP <code> . next Ns" line is driven by otp_timer.js when a
  //     credential is selected.
  //   - Pre-flight wire-length check disables SEND with a tooltip
  //     when the assembled string exceeds the active APRS budget
  //     (67 / 200 chars).
  import { Button, Icon, Input, Tooltip } from '@chrissnell/chonky-ui';
  import CredentialPicker from './CredentialPicker.svelte';
  import { remoteActionsStore } from '../../../lib/remote_actions/store.svelte.js';
  import { remoteOtpApi } from '../../../lib/remote_actions/api.js';
  import { fetchAndScheduleNext } from '../../../lib/remote_actions/otp_timer.js';
  import { assembleWireString } from '../../../lib/remote_actions/send.js';

  let { target, maxLen = 67, onFire = async () => {}, onSaveAsMacro = () => {} } = $props();

  let credId = $state(null);
  let cmd = $state('');
  let manualOtp = $state('');
  let code = $state('');
  let secs = $state(0);
  let firing = $state(false);

  // Re-prime the credential picker default whenever target changes.
  // Capturing remoteActionsStore.defaultCredFor(target) directly inside
  // $state would freeze at mount-time (Svelte 5 state_referenced_locally
  // warning) and never advance when the operator switches DM threads.
  let lastTarget = null;
  $effect.pre(() => {
    if (target !== lastTarget) {
      credId = remoteActionsStore.defaultCredFor(target);
      lastTarget = target;
    }
  });

  // Split cmd into actionName + argsString at the first space. Action
  // names are case-insensitive on the wire and stored uppercase
  // server-side, so canonicalize here too.
  const parsed = $derived.by(() => {
    const trimmed = cmd.trim();
    const sp = trimmed.indexOf(' ');
    if (sp < 0) return { actionName: trimmed.toUpperCase(), argsString: '' };
    return { actionName: trimmed.slice(0, sp).toUpperCase(), argsString: trimmed.slice(sp + 1) };
  });

  const otpToUse = $derived(credId == null ? manualOtp : code);
  const wire = $derived(assembleWireString({ otp: otpToUse, actionName: parsed.actionName, argsString: parsed.argsString }));
  const wireLen = $derived(wire.length);
  const overBudget = $derived(wireLen > maxLen);

  const canFire = $derived(
    !firing &&
      parsed.actionName.length > 0 &&
      !overBudget &&
      (credId != null ? code.length === 6 : /^[0-9]{6}$/.test(manualOtp)),
  );

  $effect(() => {
    if (credId == null) {
      code = '';
      secs = 0;
      return;
    }
    const dispose = fetchAndScheduleNext(
      async () => {
        const { data } = await remoteOtpApi.generate(credId);
        return data;
      },
      (c, s) => {
        code = c;
        secs = s;
      },
    );
    return dispose;
  });

  async function fire() {
    firing = true;
    try {
      await onFire({ otp: otpToUse, actionName: parsed.actionName, argsString: parsed.argsString });
      remoteActionsStore.rememberCredForTarget(target, credId ?? 0);
      cmd = '';
      manualOtp = '';
    } finally {
      firing = false;
    }
  }

  function saveAsMacro() {
    onSaveAsMacro({
      label: parsed.actionName,
      action_name: parsed.actionName,
      args_string: parsed.argsString,
      remote_otp_credential_id: credId,
    });
  }
</script>

<section class="freeform">
  <h3>Free-form</h3>
  <CredentialPicker bind:value={credId} label="Active OTP" />
  <div class="field">
    <label for="ff-cmd">Command</label>
    <Input id="ff-cmd" bind:value={cmd} placeholder="UNLOCK door=front" />
    <p class="hint">Enter command and optional args -- no @@ prefix needed.</p>
  </div>
  {#if credId == null}
    <div class="field">
      <label for="ff-otp">OTP (6 digits)</label>
      <Input id="ff-otp" bind:value={manualOtp} maxlength={6} />
    </div>
  {:else}
    <p class="otp" data-testid="otp-line">OTP <strong>{code || '------'}</strong> . next {secs}s</p>
  {/if}

  <button type="button" class="save-link" onclick={saveAsMacro} disabled={parsed.actionName.length === 0}>
    Save as macro...
  </button>

  <div class="send-row">
    <span class="len" class:over={overBudget}>{wireLen} / {maxLen}</span>
    {#if overBudget}
      <Tooltip>
        <Tooltip.Trigger>
          <Button variant="primary" disabled>SEND ACTION</Button>
        </Tooltip.Trigger>
        <Tooltip.Content>Line exceeds APRS budget. Shorten args or shorten action name.</Tooltip.Content>
      </Tooltip>
    {:else}
      <Button variant="primary" disabled={!canFire} onclick={fire}>
        <Icon name="zap" size="sm" /> SEND ACTION
      </Button>
    {/if}
  </div>
</section>

<style>
  .freeform { display: flex; flex-direction: column; gap: 10px; padding-top: 12px; border-top: 1px solid var(--color-border); }
  .freeform h3 { margin: 0; font-size: 13px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--color-text-muted); }
  .field { display: flex; flex-direction: column; gap: 4px; }
  .field label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
  }
  .hint { margin: 0; font-size: 11px; color: var(--color-text-muted, var(--text-muted)); }
  .otp { font-family: var(--font-mono); font-size: 0.875rem; margin: 0; color: var(--color-text-muted); }
  .save-link { background: transparent; border: none; color: var(--color-primary); font-size: 0.8125rem; cursor: pointer; padding: 0; align-self: flex-start; }
  .save-link:disabled { color: var(--color-text-dim); cursor: not-allowed; }
  .send-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .len { font-family: var(--font-mono); font-size: 0.75rem; color: var(--color-text-muted); }
  .len.over { color: var(--color-danger); }
</style>
