// web/src/routes/channels/ChannelEditModal.no_ptt.test.js
//
// File-level regression: the channel-edit modal must not reference any
// PTT-related symbols. PTT config has moved entirely to the PTT tab.
// If a future change re-adds an Android PTT block here, this test
// catches it before review.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const MODAL_PATH = new URL('./ChannelEditModal.svelte', import.meta.url);

test('ChannelEditModal.svelte does not import AndroidPttFields', async () => {
  const src = await readFile(MODAL_PATH, 'utf8');
  assert.ok(!src.includes('AndroidPttFields'),
    'ChannelEditModal references AndroidPttFields — PTT config must live in Ptt.svelte only');
});

test('ChannelEditModal.svelte does not reference androidPttMethod', async () => {
  const src = await readFile(MODAL_PATH, 'utf8');
  assert.ok(!src.includes('androidPttMethod'),
    'ChannelEditModal still threads androidPttMethod through handleSave — strip it');
});

test('ChannelEditModal.svelte template has no PTT-related blocks', async () => {
  const src = await readFile(MODAL_PATH, 'utf8');
  // No PTT-* class names, no postChannelPtt, no /api/ptt POSTs.
  for (const banned of ['postChannelPtt', '/api/ptt', 'ptt-method', 'PTT (Android)']) {
    assert.ok(!src.includes(banned),
      `ChannelEditModal references banned PTT marker: ${banned}`);
  }
});
