// Tests for the actions grammar helpers.
//   node --test src/lib/actions/grammar.test.js

import { strict as assert } from 'node:assert';
import { exampleMessage, parseAllowlist } from './grammar.js';

let describe, it;
try {
  const nodeTest = await import('node:test');
  describe = nodeTest.describe;
  it = nodeTest.it;
} catch {
  describe = globalThis.describe;
  it = globalThis.it;
}

describe('exampleMessage', () => {
  it('renders default values when called with no args', () => {
    assert.equal(exampleMessage(), '@@482910#TurnOnGarageLights state=on');
  });

  it('omits args section when args object is empty', () => {
    assert.equal(
      exampleMessage({ otp: '111111', action: 'Ping', args: {} }),
      '@@111111#Ping',
    );
  });

  it('joins multiple key=value pairs with single spaces', () => {
    const out = exampleMessage({
      otp: '222222',
      action: 'Power',
      args: { state: 'on', room: 'garage' },
    });
    assert.equal(out, '@@222222#Power state=on room=garage');
  });

  it('keeps the @@ prefix and # separator regardless of input', () => {
    const out = exampleMessage({ otp: '0', action: 'X', args: {} });
    assert.match(out, /^@@0#X$/);
  });
});

describe('parseAllowlist', () => {
  it('returns [] for null/undefined/empty', () => {
    assert.deepEqual(parseAllowlist(null), []);
    assert.deepEqual(parseAllowlist(undefined), []);
    assert.deepEqual(parseAllowlist(''), []);
  });

  it('splits on commas', () => {
    assert.deepEqual(parseAllowlist('W1ABC,K2DEF,N3GHI'), ['W1ABC', 'K2DEF', 'N3GHI']);
  });

  it('splits on whitespace', () => {
    assert.deepEqual(parseAllowlist('W1ABC K2DEF\tN3GHI'), ['W1ABC', 'K2DEF', 'N3GHI']);
  });

  it('handles mixed separators and trims surrounding whitespace', () => {
    assert.deepEqual(parseAllowlist(' W1ABC ,  K2DEF , N3GHI '), [
      'W1ABC',
      'K2DEF',
      'N3GHI',
    ]);
  });

  it('drops empty entries from trailing commas', () => {
    assert.deepEqual(parseAllowlist('W1ABC,,K2DEF,'), ['W1ABC', 'K2DEF']);
  });
});
