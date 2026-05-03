// Tests for the outbound/reply correlation helper.
//   node --test src/lib/remote_actions/reply_match.test.js

import { strict as assert } from 'node:assert';
import {
  recordOutboundFire,
  isActionReply,
  STATUS_PREFIXES,
} from './reply_match.js';

let describe, it;
try {
  const nodeTest = await import('node:test');
  describe = nodeTest.describe;
  it = nodeTest.it;
} catch {
  describe = globalThis.describe;
  it = globalThis.it;
}

describe('isActionReply', () => {
  it('flags inbound text starting with a status prefix from the same peer within 60s', () => {
    const now = Date.now();
    recordOutboundFire('KK7XYZ-9', 'unlock', now);
    const reply = {
      from_call: 'KK7XYZ-9',
      direction: 'in',
      text: 'ok unlock door=front',
      created_at: new Date(now + 5000).toISOString(),
    };
    assert.ok(isActionReply(reply));
  });
  it('does not flag when the prefix is missing', () => {
    const now = Date.now();
    recordOutboundFire('KK7XYZ-9', 'unlock', now);
    const reply = {
      from_call: 'KK7XYZ-9',
      direction: 'in',
      text: 'door is open thanks',
      created_at: new Date(now + 5000).toISOString(),
    };
    assert.ok(!isActionReply(reply));
  });
  it('does not flag when window expired', () => {
    const now = Date.now();
    recordOutboundFire('KK7XYZ-9', 'unlock', now - 120000);
    const reply = {
      from_call: 'KK7XYZ-9',
      direction: 'in',
      text: 'ok',
      created_at: new Date(now).toISOString(),
    };
    assert.ok(!isActionReply(reply));
  });
  it('exposes a non-empty STATUS_PREFIXES list using on-air wire words', () => {
    assert.ok(STATUS_PREFIXES.includes('ok'));
    assert.ok(STATUS_PREFIXES.includes('error'));
    assert.ok(STATUS_PREFIXES.includes('bad otp'));
    assert.ok(STATUS_PREFIXES.includes('rate-limited'));
    assert.ok(STATUS_PREFIXES.includes('no-credential'));
  });
  it('flags a "bad otp" reply (space, not underscore)', () => {
    const now = Date.now();
    recordOutboundFire('KK7XYZ-9', 'unlock', now);
    const reply = {
      from_call: 'KK7XYZ-9',
      direction: 'in',
      text: 'bad otp',
      created_at: new Date(now + 1000).toISOString(),
    };
    assert.ok(isActionReply(reply));
  });
  it('flags an "error: detail" reply', () => {
    const now = Date.now();
    recordOutboundFire('KK7XYZ-9', 'unlock', now);
    const reply = {
      from_call: 'KK7XYZ-9',
      direction: 'in',
      text: 'error: timeout while running',
      created_at: new Date(now + 1000).toISOString(),
    };
    assert.ok(isActionReply(reply));
  });
});
