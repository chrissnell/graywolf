// Tests for the invite-validation helpers.
//
// Pure-JS; no Svelte runtime. Runnable with either Node's built-in
// `node --test` or Vitest.
//   node --test src/lib/inviteValidation.test.js

import { strict as assert } from 'node:assert';
import {
  CALLSIGN_RE,
  classifyCommit,
  classifyPasteList,
  isValidCall,
  normalizeCall,
  PASTE_SPLIT_RE,
  splitPasteList,
} from './inviteValidation.js';

let describe, it;
try {
  const nodeTest = await import('node:test');
  describe = nodeTest.describe;
  it = nodeTest.it;
} catch {
  describe = globalThis.describe;
  it = globalThis.it;
}

describe('normalizeCall', () => {
  it('trims and uppercases', () => {
    assert.equal(normalizeCall('  w1abc  '), 'W1ABC');
    assert.equal(normalizeCall('n0call-7'), 'N0CALL-7');
  });
  it('handles empty / null', () => {
    assert.equal(normalizeCall(''), '');
    assert.equal(normalizeCall(null), '');
    assert.equal(normalizeCall(undefined), '');
  });
});

describe('isValidCall (CALLSIGN_RE)', () => {
  it('accepts 1..9 A-Z 0-9 -', () => {
    for (const c of ['W', 'W1', 'W1A', 'W1ABC', 'W1ABC-15', 'TAC-NET', 'N0CALL-7']) {
      assert.ok(isValidCall(c), `expected valid: ${c}`);
    }
  });
  it('rejects lowercase, too-long, and invalid chars', () => {
    for (const c of ['w1abc', 'TAC_NET', 'TOOLONGCALL', 'W1 ABC', '', '1234567890']) {
      assert.ok(!isValidCall(c), `expected invalid: ${c}`);
    }
  });
  it('exposes the regex for external consumers', () => {
    assert.ok(CALLSIGN_RE instanceof RegExp);
    assert.match('W5ABC', CALLSIGN_RE);
  });
});

describe('splitPasteList', () => {
  it('splits on commas, whitespace, semicolons', () => {
    assert.deepEqual(
      splitPasteList('W1ABC, N0CALL-7; K5FOO\tK7BAR'),
      ['W1ABC', 'N0CALL-7', 'K5FOO', 'K7BAR'],
    );
  });
  it('collapses runs of separators', () => {
    assert.deepEqual(splitPasteList('A,,,B\n\nC'), ['A', 'B', 'C']);
  });
  it('returns empty for empty / null input', () => {
    assert.deepEqual(splitPasteList(''), []);
    assert.deepEqual(splitPasteList(null), []);
    assert.deepEqual(splitPasteList(undefined), []);
  });
  it('PASTE_SPLIT_RE matches any separator char', () => {
    assert.ok(PASTE_SPLIT_RE.test('a,b'));
    assert.ok(PASTE_SPLIT_RE.test('a b'));
    assert.ok(PASTE_SPLIT_RE.test('a;b'));
    assert.ok(!PASTE_SPLIT_RE.test('a-b'));
  });
});

describe('classifyCommit', () => {
  it('returns ok for a fresh valid call', () => {
    assert.equal(classifyCommit('W1ABC', [], ''), 'ok');
  });
  it('returns invalid for malformed tokens', () => {
    assert.equal(classifyCommit('', [], ''), 'invalid');
    assert.equal(classifyCommit('TOOLONGCALL', [], ''), 'invalid');
    assert.equal(classifyCommit('bad_char', [], ''), 'invalid');
  });
  it('returns self for operator callsign', () => {
    assert.equal(classifyCommit('W1ME', [], 'W1ME'), 'self');
    assert.equal(classifyCommit('w1me', [], 'W1ME'), 'self');
  });
  it('returns duplicate for an existing chip (case-insensitive)', () => {
    assert.equal(classifyCommit('W1ABC', ['W1ABC'], ''), 'duplicate');
    assert.equal(classifyCommit('w1abc', ['W1ABC'], ''), 'duplicate');
  });
  it('self beats duplicate (self-filter runs first)', () => {
    // Not possible in practice, but codifies the order.
    assert.equal(classifyCommit('W1ME', ['W1ME'], 'W1ME'), 'self');
  });
  it('invalid beats self', () => {
    assert.equal(classifyCommit('', [], ''), 'invalid');
  });
});

describe('classifyPasteList', () => {
  it('groups added / duplicate / invalid across a mixed list', () => {
    const result = classifyPasteList(
      'W1ABC, N0CALL-7, bad_char, W1ABC, W9XYZ',
      [],
      '',
    );
    assert.deepEqual(result.added, ['W1ABC', 'N0CALL-7', 'W9XYZ']);
    assert.deepEqual(result.duplicate, ['W1ABC']);
    assert.deepEqual(result.invalid, ['BAD_CHAR']);
    assert.deepEqual(result.self, []);
  });

  it('deduplicates across the existing chip set', () => {
    const result = classifyPasteList('W1ABC, W1NEW', ['W1ABC'], '');
    assert.deepEqual(result.added, ['W1NEW']);
    assert.deepEqual(result.duplicate, ['W1ABC']);
  });

  it('deduplicates a pasted token against itself', () => {
    const result = classifyPasteList('W1ABC W1ABC W1ABC', [], '');
    assert.equal(result.added.length, 1);
    assert.equal(result.duplicate.length, 2);
  });

  it('calls out self-invites in the self bucket', () => {
    const result = classifyPasteList('W1ME, W9XYZ', [], 'W1ME');
    assert.deepEqual(result.self, ['W1ME']);
    assert.deepEqual(result.added, ['W9XYZ']);
    assert.deepEqual(result.invalid, []);
  });

  it('tolerates empty / whitespace-only input', () => {
    const result = classifyPasteList('   \n\t,,;;', [], '');
    assert.equal(result.added.length, 0);
    assert.equal(result.invalid.length, 0);
  });
});
