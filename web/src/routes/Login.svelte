<script>
  import { push } from 'svelte-spa-router';
  import { api } from '../lib/api.js';
  import { toasts, isAuthenticated, isFirstRun } from '../lib/stores.js';
  import TextInput from '../components/TextInput.svelte';
  import FormField from '../components/FormField.svelte';
  import Btn from '../components/Btn.svelte';

  let setupMode = $state(false);
  let username = $state('');
  let password = $state('');
  let passwordConfirm = $state('');
  let loading = $state(false);
  let errors = $state({});

  // Check if first run on mount
  $effect(() => {
    let unsub = isFirstRun.subscribe((v) => { setupMode = v; });
    return unsub;
  });

  function validate() {
    const e = {};
    if (!username.trim()) e.username = 'Username is required';
    if (!password) e.password = 'Password is required';
    if (password.length < 8) e.password = 'Minimum 8 characters';
    if (setupMode && password !== passwordConfirm) e.passwordConfirm = 'Passwords do not match';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSubmit(e) {
    e.preventDefault();
    if (!validate()) return;
    loading = true;
    try {
      if (setupMode) {
        await api.post('/auth/setup', { username, password });
        toasts.success('Admin account created');
        setupMode = false;
        isFirstRun.set(false);
      } else {
        await api.post('/auth/login', { username, password });
        toasts.success('Logged in');
        isAuthenticated.set(true);
        push('/');
      }
    } catch (err) {
      toasts.error(err.message || 'Authentication failed');
    } finally {
      loading = false;
    }
  }
</script>

<div class="login-page">
  <div class="login-card">
    <h1 class="login-logo">graywolf</h1>
    <h2 class="login-title">{setupMode ? 'Create Admin Account' : 'Sign In'}</h2>
    {#if setupMode}
      <p class="login-subtitle">No credentials found. Create the initial admin account.</p>
    {/if}

    <form onsubmit={handleSubmit}>
      <FormField label="Username" error={errors.username} id="username">
        <TextInput id="username" bind:value={username} placeholder="admin" />
      </FormField>

      <FormField label="Password" error={errors.password} id="password">
        <TextInput id="password" type="password" bind:value={password} placeholder="••••••••" />
      </FormField>

      {#if setupMode}
        <FormField label="Confirm Password" error={errors.passwordConfirm} id="passwordConfirm">
          <TextInput id="passwordConfirm" type="password" bind:value={passwordConfirm} placeholder="••••••••" />
        </FormField>
      {/if}

      <Btn variant="primary" type="submit" disabled={loading}>
        {loading ? 'Please wait...' : setupMode ? 'Create Account' : 'Sign In'}
      </Btn>
    </form>
  </div>
</div>

<style>
  .login-page {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    padding: 16px;
    background: var(--bg-primary);
  }
  .login-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    padding: 32px;
    max-width: 380px;
    width: 100%;
  }
  .login-logo {
    font-size: 24px;
    font-weight: 700;
    color: var(--accent);
    letter-spacing: 1px;
    margin-bottom: 8px;
  }
  .login-title {
    font-size: 16px;
    font-weight: 500;
    margin-bottom: 4px;
  }
  .login-subtitle {
    font-size: 13px;
    color: var(--text-muted);
    margin-bottom: 16px;
  }
  form {
    margin-top: 20px;
  }
  form :global(.btn) {
    width: 100%;
    justify-content: center;
    margin-top: 8px;
  }
</style>
