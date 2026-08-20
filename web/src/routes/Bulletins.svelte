<script>
  // /bulletins — Bulletin management page.
  //
  // Two-column layout matching the Messages shell (full-bleed). The left
  // pane shows the group list; the right pane shows the selected group's
  // settings and 10-slot bulletin table.
  //
  // URL: #/bulletins?group=<id>   (query param drives selection)

  import { onMount, tick } from 'svelte';
  import { AlertDialog } from '@chrissnell/chonky-ui';
  import { push, querystring } from 'svelte-spa-router';
  import { toasts } from '../lib/stores.js';
  import BulletinGroupList from '../components/bulletins/BulletinGroupList.svelte';
  import BulletinGroupPanel from '../components/bulletins/BulletinGroupPanel.svelte';
  import {
    listBulletinGroups,
    createBulletinGroup,
    updateBulletinGroup,
    deleteBulletinGroup,
    upsertBulletinItem,
    clearBulletinItem,
    sendBulletinNow,
  } from '../api/bulletins.js';

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  /** @type {any[]} */
  let groups = $state([]);
  let loading = $state(true);
  let activeGroupId = $state(null);
  let isMobile = $state(false);

  // Add-group modal state.
  let addOpen = $state(false);
  let addName = $state('');
  let addError = $state('');
  let addSaving = $state(false);

  // Delete-group confirmation dialog state.
  let deleteOpen = $state(false);
  let deleteGroup = $state(null);
  let deleting = $state(false);

  // ---------------------------------------------------------------------------
  // Query-param binding (drives active group)
  // ---------------------------------------------------------------------------

  let qs = $state('');
  $effect(() => {
    const unsub = querystring.subscribe((v) => { qs = v || ''; });
    return unsub;
  });

  const qsGroupId = $derived.by(() => {
    const p = new URLSearchParams(qs);
    const v = parseInt(p.get('group') || '', 10);
    return Number.isFinite(v) && v > 0 ? v : null;
  });

  $effect(() => {
    if (qsGroupId !== null) {
      activeGroupId = qsGroupId;
    }
  });

  const activeGroup = $derived(groups.find((g) => g.id === activeGroupId) ?? null);

  // On mobile: show list OR panel, not both.
  const showPanel = $derived(!isMobile || activeGroupId !== null);
  const showList  = $derived(!isMobile || activeGroupId === null);

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function refresh() {
    try {
      const data = await listBulletinGroups();
      groups = data || [];
      // Auto-select Global (id=1) on first load if nothing selected.
      if (activeGroupId === null && groups.length > 0) {
        selectGroup(groups[0]);
      }
    } catch (e) {
      toasts.error('Failed to load bulletin groups');
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    const mq = window.matchMedia('(max-width: 767px)');
    const apply = () => (isMobile = mq.matches);
    apply();
    mq.addEventListener?.('change', apply);
    refresh();
    return () => mq.removeEventListener?.('change', apply);
  });

  // On mobile, show/hide bottom-bar (same trick as Messages).
  $effect(() => {
    if (!document?.body) return;
    document.body.classList.toggle('bulletins-group-open', isMobile && activeGroupId !== null);
    return () => document.body.classList.remove('bulletins-group-open');
  });

  // ---------------------------------------------------------------------------
  // Navigation
  // ---------------------------------------------------------------------------

  function selectGroup(g) {
    if (!g) return;
    const sp = new URLSearchParams(qs);
    sp.set('group', String(g.id));
    push(`/bulletins?${sp.toString()}`);
  }

  function goBack() {
    const sp = new URLSearchParams(qs);
    sp.delete('group');
    const q = sp.toString();
    push(q ? `/bulletins?${q}` : '/bulletins');
  }

  // ---------------------------------------------------------------------------
  // Group CRUD
  // ---------------------------------------------------------------------------

  async function handleSaveGroup(settings) {
    if (!activeGroup) return;
    try {
      await updateBulletinGroup(activeGroup.id, settings);
      await refresh();
    } catch (e) {
      toasts.error(e?.message || 'Failed to save group settings');
    }
  }

  async function openAddGroup() {
    addName = '';
    addError = '';
    addOpen = true;
  }

  async function handleAdd() {
    const name = addName.trim().toUpperCase();
    if (!name || !/^[A-Z0-9]{1,5}$/.test(name)) {
      addError = 'Name must be 1–5 characters (A-Z, 0-9).';
      return;
    }
    addSaving = true;
    try {
      const g = await createBulletinGroup({
        name,
        send_path:    'rf',
        digi_path:    '',
        initial_rate: 60,
        decay_factor: 1.5,
        stable_rate:  600,
        active:       false,
      });
      addOpen = false;
      addName = '';
      await refresh();
      if (g?.id) selectGroup(g);
    } catch (e) {
      addError = e?.message || 'Failed to create group';
    } finally {
      addSaving = false;
    }
  }

  function handleDelete(g) {
    if (!g || g.id === 1) return;
    deleteGroup = g;
    deleteOpen = true;
  }

  async function runDelete() {
    if (!deleteGroup) return;
    deleting = true;
    try {
      await deleteBulletinGroup(deleteGroup.id);
      // If we just deleted the active group, fall back to Global.
      if (activeGroupId === deleteGroup.id) {
        const sp = new URLSearchParams(qs);
        sp.set('group', '1');
        push(`/bulletins?${sp.toString()}`);
      }
      await refresh();
      toasts.success(`Deleted group "${deleteGroup.name}"`);
    } catch (e) {
      toasts.error(e?.message || 'Failed to delete group');
    } finally {
      deleting = false;
      deleteOpen = false;
      deleteGroup = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Item CRUD
  // ---------------------------------------------------------------------------

  async function handleItemSave(slot, req) {
    if (!activeGroup) return;
    try {
      await upsertBulletinItem(activeGroup.id, slot, req);
      // Refresh without losing selection.
      await refresh();
    } catch (e) {
      toasts.error(e?.message || 'Failed to save item');
    }
  }

  async function handleSendNow(slot) {
    if (!activeGroup) return;
    try {
      await sendBulletinNow(activeGroup.id, slot);
      toasts.success(`Bulletin BLN${slot}${activeGroup.name} sent`);
      await refresh();
    } catch (e) {
      toasts.error(e?.message || 'Failed to send bulletin');
    }
  }

  async function handleClearItem(slot) {
    if (!activeGroup) return;
    try {
      await clearBulletinItem(activeGroup.id, slot);
      await refresh();
    } catch (e) {
      toasts.error(e?.message || 'Failed to clear item');
    }
  }
</script>

<div class="bulletins-shell" data-testid="bulletins-shell">
  <!-- Left pane: group list -->
  {#if showList}
    <aside class="side-pane" class:mobile={isMobile}>
      <BulletinGroupList
        {groups}
        {activeGroupId}
        onSelect={selectGroup}
        onAdd={openAddGroup}
        onDelete={handleDelete}
      />
    </aside>
  {/if}

  <!-- Right pane: selected group -->
  {#if showPanel}
    <main class="main-pane">
      {#if isMobile && activeGroupId !== null}
        <div class="mobile-back">
          <button type="button" class="back-btn" onclick={goBack}>
            ← Back
          </button>
        </div>
      {/if}
      <BulletinGroupPanel
        group={activeGroup}
        onSaveGroup={handleSaveGroup}
        onItemSave={handleItemSave}
        onSendNow={handleSendNow}
        onClearItem={handleClearItem}
      />
    </main>
  {/if}
</div>

<AlertDialog bind:open={deleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete this bulletin group?</AlertDialog.Title>
    <AlertDialog.Description>
      All bulletins in group <strong>{deleteGroup?.name}</strong> will be
      deleted from the server. This cannot be undone.
    </AlertDialog.Description>
    <div class="alert-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        class="bulletin-delete-confirm"
        onclick={runDelete}
        disabled={deleting}
      >
        {deleting ? 'Deleting…' : 'Delete'}
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<!-- Add Group modal (inline, no extra dependency) -->
{#if addOpen}
  <div class="modal-overlay" role="dialog" aria-modal="true" aria-label="Add bulletin group">
    <div class="modal-box">
      <h3 class="modal-title">New Bulletin Group</h3>
      <p class="modal-hint">
        Up to 5 characters (A-Z, 0-9). Example: <code>WX</code>, <code>EOC</code>.
      </p>
      <input
        class="modal-input"
        type="text"
        placeholder="Group name"
        bind:value={addName}
        maxlength="5"
        oninput={(e) => { addName = (e.target.value || '').toUpperCase(); e.target.value = addName; addError = ''; }}
        onkeydown={(e) => { if (e.key === 'Enter') handleAdd(); if (e.key === 'Escape') addOpen = false; }}
        aria-label="Group name"
      />
      {#if addError}
        <p class="modal-err" role="alert">{addError}</p>
      {/if}
      <div class="modal-actions">
        <button type="button" class="modal-cancel" onclick={() => (addOpen = false)}>Cancel</button>
        <button type="button" class="modal-confirm" onclick={handleAdd} disabled={addSaving}>
          {addSaving ? 'Creating…' : 'Create'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* ── Two-column shell ───────────────────────────────────────────── */
  .bulletins-shell {
    display: grid;
    grid-template-columns: 240px 1fr;
    height: 100%;
    overflow: hidden;
    background: var(--color-bg);
  }

  .side-pane {
    border-right: 1px solid var(--color-border);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .main-pane {
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  /* ── Mobile single-pane ─────────────────────────────────────────── */
  @media (max-width: 767px) {
    .bulletins-shell {
      grid-template-columns: 1fr;
    }
    .side-pane.mobile {
      border-right: none;
    }
  }

  .mobile-back {
    padding: 8px 12px;
    border-bottom: 1px solid var(--color-border);
  }
  .back-btn {
    background: transparent;
    border: none;
    color: var(--color-primary, #6366f1);
    cursor: pointer;
    font-size: 14px;
    padding: 4px 0;
  }

  /* ── Add-group modal ────────────────────────────────────────────── */
  .modal-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .modal-box {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 24px;
    width: min(360px, 90vw);
    display: flex;
    flex-direction: column;
    gap: 12px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
  }
  .modal-title {
    margin: 0;
    font-size: 16px;
    font-weight: 600;
  }
  .modal-hint {
    margin: 0;
    font-size: 13px;
    color: var(--color-text-muted);
  }
  .modal-hint code {
    font-family: var(--font-mono);
    font-size: 12px;
    padding: 1px 4px;
    border-radius: 3px;
    background: var(--color-surface-raised, rgba(127,127,127,0.1));
  }
  .modal-input {
    padding: 8px 10px;
    border: 1px solid var(--color-border);
    border-radius: 4px;
    background: var(--color-bg);
    color: var(--color-text);
    font-family: var(--font-mono);
    font-size: 14px;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    width: 100%;
    box-sizing: border-box;
  }
  .modal-input:focus {
    outline: 2px solid var(--color-primary, #6366f1);
    outline-offset: 1px;
  }
  .modal-err {
    margin: 0;
    color: var(--color-danger);
    font-size: 12px;
  }
  .modal-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
  .modal-cancel {
    padding: 6px 14px;
    border: 1px solid var(--color-border);
    border-radius: 4px;
    background: transparent;
    color: var(--color-text);
    cursor: pointer;
    font-size: 13px;
  }
  .modal-confirm {
    padding: 6px 14px;
    border: 1px solid var(--color-primary, #6366f1);
    border-radius: 4px;
    background: var(--color-primary, #6366f1);
    color: #fff;
    cursor: pointer;
    font-size: 13px;
    font-weight: 600;
  }
  .modal-confirm:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .alert-footer {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding: 1rem 1.5rem 1.25rem;
  }
  :global(.bulletin-delete-confirm) {
    background: var(--color-danger) !important;
    color: white !important;
    border-color: var(--color-danger) !important;
  }
  :global(.bulletin-delete-confirm:disabled) {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
