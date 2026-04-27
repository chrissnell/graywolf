import { writable } from 'svelte/store';
import { toast as chonkyToast, dismiss } from '@chrissnell/chonky-ui';

// Toast wrapper over chonky-ui's toast system
export const toasts = {
  success: (msg) => chonkyToast(msg, 'success'),
  error: (msg) => chonkyToast(msg, 'danger', 6000),
  dismiss,
};

// Auth state
export const isAuthenticated = writable(false);
export const isFirstRun = writable(false);
