<script>
  import { onMount } from 'svelte';
  import { Button, Toaster } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';
  import { actionsStore } from '../lib/actions/store.svelte.js';
  import { exampleMessage } from '../lib/actions/grammar.js';
  import ActionsTable from '../components/actions/ActionsTable.svelte';
  import CredentialsTable from '../components/actions/CredentialsTable.svelte';
  import InvocationsPanel from '../components/actions/InvocationsPanel.svelte';

  let auditOpen = $state(false);
  let editOpen = $state(false);
  let newCredOpen = $state(false);

  onMount(() => actionsStore.loadAll());
</script>

<div class="actions-page">
  <PageHeader
    title="Actions"
    subtitle="Execute commands or call webhooks when authorized APRS messages arrive."
  >
    <div class="header-buttons">
      <Button variant="secondary" onclick={() => (auditOpen = true)}>View audit log</Button>
      <Button variant="primary" onclick={() => (editOpen = true)}>+ New Action</Button>
    </div>
  </PageHeader>

  <div class="help-banner">
    Actions trigger when APRS messages addressed to this station begin with the prefix
    <code>@@</code> followed by a six-digit TOTP code, then <code>#&lt;name&gt;</code>, then
    optional <code>key=value</code> arguments. Example:
    <code>{exampleMessage()}</code>
  </div>

  <ActionsTable bind:editOpen />
  <CredentialsTable bind:newCredOpen />
  <InvocationsPanel />
</div>

<Toaster />

<style>
  .actions-page {
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }
  .header-buttons {
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }
  .help-banner {
    background: var(--gw-banner-bg, #fff8e1);
    color: var(--gw-banner-fg, #5b4b1a);
    border-left: 3px solid var(--gw-banner-accent, #f4b400);
    padding: 0.75rem 1rem;
    border-radius: 4px;
    font-size: 0.9rem;
  }
  .help-banner code {
    font-family: 'SauceCodePro Nerd Font', ui-monospace, monospace;
    background: rgba(0, 0, 0, 0.05);
    padding: 1px 4px;
    border-radius: 3px;
  }
</style>
