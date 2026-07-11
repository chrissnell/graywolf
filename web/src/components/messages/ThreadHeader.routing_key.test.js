// web/src/components/messages/ThreadHeader.routing_key.test.js
//
// Regression for issue #453 follow-up: ConversationRoutingMenu holds local
// per-conversation $state (send path, wait-for-ack, loaded, open). Svelte
// reuses the component instance when only threadKey changes, so without a
// forced remount one conversation's routing bled into the next (open DM A,
// set APRS-IS only; open DM B, it wrongly showed APRS-IS only). Each mount
// must be wrapped in {#key <threadKey>} so a new conversation gets a fresh
// component with default state.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const HEADER_PATH = new URL('./ThreadHeader.svelte', import.meta.url);

test('DM routing menu is keyed by thread.key', async () => {
  const src = await readFile(HEADER_PATH, 'utf8');
  const m = src.match(/\{#key thread\.key\}[\s\S]*?<ConversationRoutingMenu kind="dm"[\s\S]*?\{\/key\}/);
  assert.ok(m,
    'DM ConversationRoutingMenu must be wrapped in {#key thread.key} so switching conversations remounts it with clean routing state');
});

test('tactical routing menu is keyed by tacticalKey', async () => {
  const src = await readFile(HEADER_PATH, 'utf8');
  const m = src.match(/\{#key tacticalKey\}[\s\S]*?<ConversationRoutingMenu kind="tactical"[\s\S]*?\{\/key\}/);
  assert.ok(m,
    'tactical ConversationRoutingMenu must be wrapped in {#key tacticalKey} so switching conversations remounts it with clean routing state');
});
