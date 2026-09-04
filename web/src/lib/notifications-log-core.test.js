// Tests for the pure load/cap logic behind the device-local
// notifications log.
//
// Run with:
//   node --test src/lib/notifications-log-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { parseLogEntries, addLogEntry, MAX_LOG_ENTRIES } from './notifications-log-core.js';

function entry(id, timestamp = 1000) {
  return { id, kind: 'message', title: `Entry ${id}`, body: '', href: '', timestamp };
}

describe('parseLogEntries', () => {
  it('returns [] for null/missing input', () => {
    assert.deepEqual(parseLogEntries(null), []);
  });

  it('returns [] for corrupt JSON', () => {
    assert.deepEqual(parseLogEntries('not json'), []);
  });

  it('returns [] when the JSON is not an array', () => {
    assert.deepEqual(parseLogEntries('{"a":1}'), []);
  });

  it('parses a valid entry array', () => {
    const raw = JSON.stringify([entry('1'), entry('2')]);
    assert.deepEqual(parseLogEntries(raw), [entry('1'), entry('2')]);
  });

  it('drops malformed entries but keeps valid ones', () => {
    const raw = JSON.stringify([entry('1'), { id: '2' }, null, entry('3')]);
    assert.deepEqual(parseLogEntries(raw), [entry('1'), entry('3')]);
  });
});

describe('addLogEntry', () => {
  it('prepends the new entry (newest-first)', () => {
    const out = addLogEntry([entry('old')], entry('new'));
    assert.deepEqual(out, [entry('new'), entry('old')]);
  });

  it('handles an empty/missing starting list', () => {
    assert.deepEqual(addLogEntry([], entry('1')), [entry('1')]);
    assert.deepEqual(addLogEntry(undefined, entry('1')), [entry('1')]);
  });

  it('caps at MAX_LOG_ENTRIES, dropping the oldest', () => {
    let entries = [];
    for (let i = 0; i < MAX_LOG_ENTRIES + 5; i++) {
      entries = addLogEntry(entries, entry(String(i)));
    }
    assert.equal(entries.length, MAX_LOG_ENTRIES);
    // Newest (last added) is first; oldest 5 were dropped off the end.
    assert.equal(entries[0].id, String(MAX_LOG_ENTRIES + 4));
    assert.equal(entries[entries.length - 1].id, '5');
  });
});
