<script>
  import { onMount } from 'svelte';
  import { Button, toast } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';
  import { actionsStore } from '../lib/actions/store.svelte.js';
  import { exampleMessage } from '../lib/actions/grammar.js';
  import ActionsTable from '../components/actions/ActionsTable.svelte';
  import CredentialsTable from '../components/actions/CredentialsTable.svelte';
  import InvocationsPanel from '../components/actions/InvocationsPanel.svelte';
  import EditActionModal from '../components/actions/EditActionModal.svelte';
  import TestActionDialog from '../components/actions/TestActionDialog.svelte';
  import NewCredentialModal from '../components/actions/NewCredentialModal.svelte';

  // Modal-coordination state.
  let editingAction = $state(null);
  let testingAction = $state(null);
  let editOpen = $state(false);
  let testOpen = $state(false);
  let auditOpen = $state(false);
  let newCredOpen = $state(false);

  function openEdit(action) {
    editingAction = action;
    editOpen = true;
  }

  function openTest(action) {
    testingAction = action;
    testOpen = true;
  }

  function openAudit() {
    auditOpen = true;
    toast('Full audit log view lands in the next release.', 'info');
  }

  function openNewCred() {
    newCredOpen = true;
  }

  onMount(() => actionsStore.loadAll());
</script>

<div class="actions-page">
  <PageHeader
    title="Actions"
    subtitle="Execute commands or call webhooks when authorized APRS messages arrive."
  >
    <Button variant="secondary" onclick={openAudit}>View audit log</Button>
    <Button variant="primary" class="actions-solid" onclick={() => openEdit(null)}>+ New Action</Button>
  </PageHeader>

  {#if actionsStore.error}
    <div class="error-banner" role="alert">
      <strong>Failed to load actions:</strong>
      {actionsStore.error}
      <button type="button" class="retry" onclick={() => actionsStore.loadAll()}>Retry</button>
    </div>
  {/if}

  <div class="help-banner">
    Actions trigger when APRS messages addressed to this station begin with the prefix
    <code>@@</code> followed by an optional six-digit TOTP code, then
    <code>#&lt;name&gt;</code>, then optional <code>key=value</code> arguments. The TOTP
    code is required only for Actions configured with "Require valid one-time code".
    Example: <code>{exampleMessage()}</code>
  </div>

  <ActionsTable onEdit={openEdit} onTest={openTest} />
  <CredentialsTable onNew={openNewCred} />
  <InvocationsPanel />
</div>

<EditActionModal
  bind:open={editOpen}
  action={editingAction}
  onClose={() => (editingAction = null)}
  onManageCredentials={openNewCred}
/>

<TestActionDialog
  bind:open={testOpen}
  action={testingAction}
  onClose={() => (testingAction = null)}
/>

<NewCredentialModal bind:open={newCredOpen} />

<style>
  @import '../lib/actions/buttons.css';
  .actions-page {
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }
  .error-banner {
    background: var(--color-danger-muted, rgba(220, 38, 38, 0.12));
    color: var(--color-danger, #b91c1c);
    border-left: 3px solid var(--color-danger, #b91c1c);
    padding: 0.75rem 1rem;
    border-radius: 4px;
    font-size: 0.9rem;
    display: flex;
    gap: 0.75rem;
    align-items: center;
  }
  .retry {
    margin-left: auto;
    background: none;
    border: 1px solid currentColor;
    color: inherit;
    padding: 2px 10px;
    border-radius: 3px;
    cursor: pointer;
    font: inherit;
  }
  .help-banner {
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    border-left: 3px solid var(--accent);
    padding: 0.75rem 1rem;
    border-radius: 4px;
    font-size: 0.9rem;
  }
  .help-banner code {
    font-family: 'SauceCodePro Nerd Font', ui-monospace, monospace;
    background: var(--accent-bg, rgba(0, 0, 0, 0.05));
    color: var(--text-primary);
    padding: 1px 4px;
    border-radius: 3px;
  }
</style>
