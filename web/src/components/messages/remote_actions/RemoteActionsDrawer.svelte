<script>
  // RemoteActionsDrawer -- slide-in panel anchored right on desktop and
  // bottom on mobile. Owns drawer-level state (mode flip, cooldown,
  // staged macro draft) and delegates per-row rendering to MacroTile
  // / MacroEditRow / FreeFormSender.
  //
  // Cooldown source-of-truth:
  //   - The receiver's replay ring is the actual authority. The
  //     drawer's cooldown is a UX hint that prevents the operator
  //     from triggering a `bad_otp:replay` reply.
  //   - When a macro fires we record the next-step boundary (from the
  //     OTP endpoint response) and disable that tile until then.
  //   - For the free-form path, the SEND button shares the same
  //     boundary; the entire FreeFormSender is the cooled-down unit.
  import { Button, Icon, toast } from '@chrissnell/chonky-ui';
  import MacroTile from './MacroTile.svelte';
  import MacroEditRow from './MacroEditRow.svelte';
  import FreeFormSender from './FreeFormSender.svelte';
  import { remoteActionsStore } from '../../../lib/remote_actions/store.svelte.js';
  import { remoteMacrosApi, remoteOtpApi } from '../../../lib/remote_actions/api.js';
  import { sendActionFire } from '../../../lib/remote_actions/send.js';
  import { sendMessage } from '../../../api/messages.js';

  let {
    open = $bindable(false),
    target,
    maxLen = 67,
  } = $props();

  let mode = $state('fire'); // 'fire' | 'edit'
  let cooldownByMacro = $state({}); // { macroId: expiresAtMs }
  let now = $state(Date.now());

  // Per-second tick so the cooldown numbers refresh.
  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(t);
  });

  $effect(() => {
    if (open && target) {
      remoteActionsStore.refreshTarget(target);
    }
  });

  const macros = $derived(
    (remoteActionsStore.macrosByTarget[target] ?? [])
      .slice()
      .sort((a, b) => (a.position ?? 0) - (b.position ?? 0)),
  );

  function cooldownFor(macroId) {
    const expiresMs = cooldownByMacro[macroId];
    if (!expiresMs) return 0;
    const left = Math.ceil((expiresMs - now) / 1000);
    return left > 0 ? left : 0;
  }

  async function fireMacro(m) {
    let otp = '';
    let nextBoundaryMs = Date.now() + 30_000;
    if (m.remote_otp_credential_id != null) {
      const { data, error } = await remoteOtpApi.generate(m.remote_otp_credential_id);
      if (error || !data) {
        toast('OTP fetch failed', 'error');
        return;
      }
      otp = data.code;
      nextBoundaryMs = new Date(data.expires_at).getTime();
    } else {
      // Manual OTP not supported on bare macro tap; the operator must
      // promote the macro to free-form (Save as macro path) or remove
      // its credential binding via edit mode then enter the OTP there.
      toast('Macro has no credential. Edit it to add one or use the free-form sender.', 'warning');
      return;
    }
    try {
      await sendActionFire({
        target,
        otp,
        actionName: m.action_name,
        argsString: m.args_string,
        sendMessage,
      });
      cooldownByMacro = { ...cooldownByMacro, [m.id]: nextBoundaryMs };
      remoteActionsStore.rememberCredForTarget(target, m.remote_otp_credential_id);
    } catch (e) {
      toast(`Fire failed: ${e?.message ?? e}`, 'error');
    }
  }

  async function fireFreeForm({ otp, actionName, argsString }) {
    try {
      await sendActionFire({ target, otp, actionName, argsString, sendMessage });
      toast('Action sent', 'success');
    } catch (e) {
      toast(`Fire failed: ${e?.message ?? e}`, 'error');
    }
  }

  async function updateMacro(patched) {
    const { error } = await remoteMacrosApi.update(patched.id, patched);
    if (error) {
      toast('Update failed', 'error');
      return;
    }
    await remoteActionsStore.loadMacros(target);
  }

  async function deleteMacro(m) {
    const { error } = await remoteMacrosApi.remove(m.id);
    if (error) {
      toast('Delete failed', 'error');
      return;
    }
    await remoteActionsStore.loadMacros(target);
  }

  async function moveMacro(m, dir) {
    const list = macros.slice();
    const idx = list.findIndex((x) => x.id === m.id);
    const swap = dir === 'up' ? idx - 1 : idx + 1;
    if (swap < 0 || swap >= list.length) return;
    const ids = list.map((x) => x.id);
    [ids[idx], ids[swap]] = [ids[swap], ids[idx]];
    const { error } = await remoteMacrosApi.reorder(target, ids);
    if (error) {
      toast('Reorder failed', 'error');
      return;
    }
    await remoteActionsStore.loadMacros(target);
  }

  let savingDraft = $state(null);
  function saveDraft(draft) {
    savingDraft = { ...draft, target_call: target, position: macros.length };
    mode = 'edit';
  }

  async function commitDraft() {
    const { error } = await remoteMacrosApi.create(savingDraft);
    savingDraft = null;
    if (error) {
      toast('Create failed', 'error');
      return;
    }
    await remoteActionsStore.loadMacros(target);
  }
</script>

{#if open}
  <aside class="drawer" data-testid="remote-actions-drawer">
    <header class="head">
      <h2>{mode === 'edit' ? 'EDIT MACROS' : `MACROS . ${target}`}</h2>
      <div class="head-actions">
        {#if mode === 'edit'}
          <Button variant="primary" size="sm" onclick={() => (mode = 'fire')}>Done</Button>
        {:else}
          <button type="button" class="gear" aria-label="Edit macros" onclick={() => (mode = 'edit')}>
            <Icon name="settings" size="sm" />
          </button>
        {/if}
        <button type="button" class="close" aria-label="Close" onclick={() => (open = false)}>
          <Icon name="x" size="sm" />
        </button>
      </div>
    </header>

    {#if mode === 'fire'}
      <ul class="tiles">
        {#each macros as m (m.id)}
          <li><MacroTile macro={m} cooldownSec={cooldownFor(m.id)} onFire={() => fireMacro(m)} /></li>
        {/each}
      </ul>
      <FreeFormSender
        {target}
        {maxLen}
        onFire={fireFreeForm}
        onSaveAsMacro={saveDraft}
      />
    {:else}
      <div class="edit-list">
        {#each macros as m (m.id)}
          <MacroEditRow
            macro={m}
            onChange={updateMacro}
            onDelete={() => deleteMacro(m)}
            onMoveUp={() => moveMacro(m, 'up')}
            onMoveDown={() => moveMacro(m, 'down')}
          />
        {/each}
      </div>
      {#if savingDraft}
        <div class="draft">
          <h3>New macro</h3>
          <MacroEditRow
            macro={savingDraft}
            onChange={(p) => (savingDraft = p)}
            onDelete={() => (savingDraft = null)}
          />
          <Button variant="primary" onclick={commitDraft}>Create</Button>
        </div>
      {:else}
        <Button variant="ghost" onclick={() => (savingDraft = { target_call: target, label: '', action_name: '', args_string: '', remote_otp_credential_id: null, position: macros.length })}>
          + Add new macro
        </Button>
      {/if}
    {/if}
  </aside>
{/if}

<style>
  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(420px, 100vw);
    background: var(--color-surface);
    border-left: 1px solid var(--color-border);
    box-shadow: -2px 0 12px rgba(0,0,0,0.15);
    display: flex;
    flex-direction: column;
    padding: 16px;
    overflow-y: auto;
    z-index: 50;
  }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 12px; }
  .head h2 { margin: 0; font-size: 0.9375rem; letter-spacing: 0.05em; text-transform: uppercase; }
  .head-actions { display: flex; gap: 6px; align-items: center; }
  .gear, .close { background: transparent; border: none; cursor: pointer; color: var(--color-text-muted); padding: 4px; }
  .tiles { list-style: none; padding: 0; margin: 0 0 12px; display: flex; flex-direction: column; gap: 6px; }
  .edit-list { display: flex; flex-direction: column; gap: 4px; margin-bottom: 12px; }
  .draft { padding: 12px; border: 1px dashed var(--color-border); border-radius: var(--radius); margin-bottom: 12px; }

  @media (max-width: 767px) {
    .drawer { top: auto; left: 0; right: 0; bottom: 0; width: 100vw; max-height: 80vh; border-left: none; border-top: 1px solid var(--color-border); }
  }
</style>
