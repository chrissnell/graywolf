<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';

  let form = $state({
    enabled: false,
    db_path: './graywolf-history.db',
  });
  let loading = $state(false);

  onMount(async () => {
    const data = await api.get('/position-log');
    if (data) form = { enabled: data.enabled, db_path: data.db_path };
  });

  async function handleSave(e) {
    e.preventDefault();
    loading = true;
    try {
      await api.put('/position-log', form);
      toasts.success('Position log config saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader title="Position Log" subtitle="Persistent station position history" />

<Box>
  <form onsubmit={handleSave}>
    <Toggle bind:checked={form.enabled} label="Enable persistent position log" />
    {#if form.enabled}
      <FormField label="Database Path" id="pl-path">
        <Input id="pl-path" bind:value={form.db_path} placeholder="./graywolf-history.db" />
      </FormField>
    {/if}
    <div class="form-actions">
      <Button variant="primary" type="submit" disabled={loading}>
        {loading ? 'Saving...' : 'Save'}
      </Button>
    </div>
  </form>
</Box>

<div class="info-box">
  <h3>About Position Logging</h3>
  <p>
    When enabled, graywolf stores station positions in a separate SQLite
    database so the live map is populated immediately after a restart.
    Positions are retained for 30 days and automatically pruned.
    The database file is created automatically if it doesn&rsquo;t exist.
  </p>

  <h3>Raspberry Pi &amp; SD Card Users</h3>
  <p>
    <strong>Do not enable this feature with the default path if your system
    runs from an SD card.</strong> Frequent SQLite writes will wear out the
    card over time. Instead, point the database path at a RAM disk such as
    <code>/tmp/graywolf-history.db</code> &mdash; Raspberry Pi OS typically
    mounts <code>/tmp</code> as a tmpfs by default.
  </p>
  <p>
    On a RAM disk the database is lost on reboot, which is fine &mdash;
    graywolf recreates it automatically and the map simply starts fresh,
    populating as stations are heard.
  </p>
  <p>
    See the <a href="https://chrissnell.com/software/graywolf/history-database.html"
    target="_blank" rel="noopener">handbook guide</a> for detailed Raspberry
    Pi setup instructions including systemd overrides and tmpfs verification.
  </p>
</div>

<style>
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }

  .info-box {
    margin-top: 24px;
    padding: 16px 20px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    font-size: 13px;
    line-height: 1.6;
    color: var(--text-secondary);
  }
  .info-box h3 {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0 0 6px;
  }
  .info-box h3:not(:first-child) {
    margin-top: 16px;
  }
  .info-box p {
    margin: 0 0 8px;
  }
  .info-box code {
    font-size: 12px;
    padding: 1px 5px;
    background: var(--bg-secondary);
    border-radius: 3px;
  }
  .info-box a {
    color: var(--accent);
  }
</style>
