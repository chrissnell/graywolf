// Tests for the wire-string assembler and send helper.
//   node --test src/lib/remote_actions/send.test.js

import { strict as assert } from 'node:assert';
import { assembleWireString, lengthFor, sendActionFire } from './send.js';

let describe, it, mock;
try {
  const nodeTest = await import('node:test');
  describe = nodeTest.describe;
  it = nodeTest.it;
  mock = nodeTest.mock;
} catch {
  describe = globalThis.describe;
  it = globalThis.it;
}

describe('assembleWireString', () => {
  it('builds @@<otp>#cmd without args', () => {
    assert.equal(
      assembleWireString({ otp: '418273', actionName: 'status' }),
      '@@418273#status',
    );
  });
  it('builds with args', () => {
    assert.equal(
      assembleWireString({ otp: '418273', actionName: 'unlock', argsString: 'door=front' }),
      '@@418273#unlock door=front',
    );
  });
  it('omits otp when empty (action with otp_required=false on receiver)', () => {
    assert.equal(
      assembleWireString({ otp: '', actionName: 'status' }),
      '@@#status',
    );
  });
});

describe('lengthFor', () => {
  it('matches assembled length', () => {
    assert.equal(
      lengthFor({ otp: '418273', actionName: 'unlock', argsString: 'door=front' }),
      '@@418273#unlock door=front'.length,
    );
  });
});

describe('sendActionFire', () => {
  it('posts via the supplied sendMessage helper and records the fire', async () => {
    const sendMessage = mock.fn(async () => ({ id: 1, text: '@@418273#unlock' }));
    await sendActionFire({
      target: 'KK7XYZ-9',
      otp: '418273',
      actionName: 'unlock',
      argsString: '',
      sendMessage,
    });
    assert.equal(sendMessage.mock.callCount(), 1);
    assert.deepEqual(sendMessage.mock.calls[0].arguments[0], {
      to: 'KK7XYZ-9',
      text: '@@418273#unlock',
    });
  });
});
