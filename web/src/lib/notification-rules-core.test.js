// Tests for the pure new-activity-notification suppression rules.
//
// Run with:
//   node --test src/lib/notification-rules-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import {
  shouldNotifyMessage,
  shouldNotifyBulletin,
  shouldFireOsNotification,
} from './notification-rules-core.js';

describe('shouldNotifyMessage', () => {
  it('suppresses when the thread is muted', () => {
    assert.equal(shouldNotifyMessage({ muted: true, isActiveThread: false }), false);
  });

  it('suppresses when the thread is currently open', () => {
    assert.equal(shouldNotifyMessage({ muted: false, isActiveThread: true }), false);
  });

  it('suppresses when both muted and active', () => {
    assert.equal(shouldNotifyMessage({ muted: true, isActiveThread: true }), false);
  });

  it('notifies otherwise', () => {
    assert.equal(shouldNotifyMessage({ muted: false, isActiveThread: false }), true);
  });
});

describe('shouldNotifyBulletin', () => {
  it('suppresses while the Bulletins page is mounted', () => {
    assert.equal(shouldNotifyBulletin({ pageActive: true }), false);
  });

  it('notifies when the Bulletins page is not mounted', () => {
    assert.equal(shouldNotifyBulletin({ pageActive: false }), true);
  });
});

describe('shouldFireOsNotification', () => {
  it('requires enabled, granted, and (hidden or forced)', () => {
    assert.equal(
      shouldFireOsNotification({ enabled: true, permission: 'granted', documentHidden: true }),
      true,
    );
  });

  it('is false when disabled even if hidden and granted', () => {
    assert.equal(
      shouldFireOsNotification({ enabled: false, permission: 'granted', documentHidden: true }),
      false,
    );
  });

  it('is false when permission is not granted', () => {
    assert.equal(
      shouldFireOsNotification({ enabled: true, permission: 'denied', documentHidden: true }),
      false,
    );
  });

  it('is false when the tab is visible and not forced', () => {
    assert.equal(
      shouldFireOsNotification({ enabled: true, permission: 'granted', documentHidden: false }),
      false,
    );
  });

  it('force bypasses the documentHidden requirement (test-button path)', () => {
    assert.equal(
      shouldFireOsNotification({
        enabled: true,
        permission: 'granted',
        documentHidden: false,
        force: true,
      }),
      true,
    );
  });

  it('force does not bypass enabled/permission requirements', () => {
    assert.equal(
      shouldFireOsNotification({
        enabled: false,
        permission: 'granted',
        documentHidden: false,
        force: true,
      }),
      false,
    );
    assert.equal(
      shouldFireOsNotification({
        enabled: true,
        permission: 'denied',
        documentHidden: false,
        force: true,
      }),
      false,
    );
  });
});
