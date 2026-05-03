import { actionsApi, credsApi, invocationsApi, listenersApi } from './api.js';

// openapi-fetch returns `{ data, error, response }` instead of throwing
// on HTTP errors. The store inspects `error` from each call and surfaces
// a single message via `actionsStore.error`; consumers render a banner
// on top of the page rather than guessing why a list is empty.
function describe(error, fallback) {
  if (!error) return fallback;
  if (typeof error === 'string') return error;
  return error.error ?? error.message ?? fallback;
}

class ActionsStore {
  actions = $state([]);
  creds = $state([]);
  listeners = $state([]);
  invocations = $state([]);
  loading = $state(false);
  error = $state(null);
  invocationFilter = $state({ q: '', actionId: '', status: '', source: '' });

  async loadAll() {
    this.loading = true;
    try {
      const [a, c, l, i] = await Promise.all([
        actionsApi.list(),
        credsApi.list(),
        listenersApi.list(),
        invocationsApi.list({ limit: 100 }),
      ]);
      const firstError = a.error || c.error || l.error || i.error;
      if (firstError) {
        this.error = describe(firstError, 'Failed to load actions data');
        return;
      }
      this.actions = a.data ?? [];
      this.creds = c.data ?? [];
      this.listeners = l.data ?? [];
      this.invocations = i.data ?? [];
      this.error = null;
    } catch (e) {
      this.error = e?.message ?? String(e);
    } finally {
      this.loading = false;
    }
  }

  async refreshInvocations() {
    const f = this.invocationFilter;
    const q = { limit: 100 };
    if (f.q) q.q = f.q;
    if (f.actionId) q.action_id = Number(f.actionId);
    if (f.status) q.status = f.status;
    if (f.source) q.source = f.source;
    try {
      const { data, error } = await invocationsApi.list(q);
      if (error) {
        this.error = describe(error, 'Failed to refresh invocations');
        return;
      }
      this.invocations = data ?? [];
    } catch (e) {
      this.error = e?.message ?? String(e);
    }
  }
}

export const actionsStore = new ActionsStore();
