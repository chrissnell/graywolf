import { actionsApi, credsApi, invocationsApi, listenersApi } from './api.js';

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
    const { data } = await invocationsApi.list(q);
    this.invocations = data ?? [];
  }
}

export const actionsStore = new ActionsStore();
