import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../../api/messages.js', () => ({
  getPreferences: vi.fn(),
  putPreferences: vi.fn(),
}));
vi.mock('../stores.js', () => ({
  toasts: { success: vi.fn(), error: vi.fn() },
}));

import { messagesPreferencesState } from './messages-preferences-store.svelte.js';
import { getPreferences, putPreferences } from '../../api/messages.js';

describe('messagesPreferencesState.setFallbackPolicy', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('sends the new policy and preserves sibling fields', async () => {
    getPreferences.mockResolvedValueOnce({
      default_path: 'auto',
      fallback_policy: 'is_fallback',
      retention_days: 30,
      retry_max_attempts: 3,
      max_message_text_override: 0,
    });
    putPreferences.mockResolvedValueOnce({
      default_path: 'auto',
      fallback_policy: 'is_only',
      retention_days: 30,
      retry_max_attempts: 3,
      max_message_text_override: 0,
    });

    await messagesPreferencesState.fetchPreferences();
    await messagesPreferencesState.setFallbackPolicy('is_only');

    expect(putPreferences).toHaveBeenCalledWith({
      default_path: 'auto',
      fallback_policy: 'is_only',
      retention_days: 30,
      retry_max_attempts: 3,
      max_message_text_override: 0,
    });
    expect(messagesPreferencesState.fallbackPolicy).toBe('is_only');
  });

  it('rejects unknown policies', async () => {
    getPreferences.mockResolvedValueOnce({
      default_path: 'auto',
      fallback_policy: 'is_fallback',
      retention_days: 30,
      retry_max_attempts: 3,
      max_message_text_override: 0,
    });
    await messagesPreferencesState.fetchPreferences();
    await messagesPreferencesState.setFallbackPolicy('garbage');
    expect(putPreferences).not.toHaveBeenCalled();
    expect(messagesPreferencesState.fallbackPolicy).toBe('is_fallback');
  });
});
