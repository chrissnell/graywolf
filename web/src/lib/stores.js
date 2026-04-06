import { writable } from 'svelte/store';

// Toast notification store
function createToastStore() {
  const { subscribe, update } = writable([]);
  let nextId = 0;

  return {
    subscribe,
    success(message) {
      const id = nextId++;
      update((t) => [...t, { id, type: 'success', message }]);
      setTimeout(() => this.dismiss(id), 4000);
    },
    error(message) {
      const id = nextId++;
      update((t) => [...t, { id, type: 'error', message }]);
      setTimeout(() => this.dismiss(id), 6000);
    },
    dismiss(id) {
      update((t) => t.filter((x) => x.id !== id));
    },
  };
}

export const toasts = createToastStore();

// Auth state
export const isAuthenticated = writable(false);
export const isFirstRun = writable(false);
