import test from 'node:test';
import assert from 'node:assert/strict';
import { describeMessageStatus } from './messageStatusLabel.js';

test('describeMessageStatus: maps every known status to its label', () => {
  const cases = {
    received: 'Received',
    queued: 'Waiting',
    tx_submitted: 'Waiting',
    sent_rf: 'Sent',
    sent_is: 'Sent',
    acked: 'Delivered',
    rejected: 'Rejected',
    timeout: 'Timed Out',
    failed: 'Failed',
    aborted: 'Aborted',
    sent: 'Broadcast',
  };
  for (const [status, label] of Object.entries(cases)) {
    assert.equal(
      describeMessageStatus({ direction: 'out', status, attempts: 1 }, false, 4).label,
      label,
    );
  }
});

test('describeMessageStatus: unknown status falls back to the raw value', () => {
  assert.equal(describeMessageStatus({ status: 'made_up' }, false, 4).label, 'made_up');
});

test('describeMessageStatus: unset status falls back to Unknown', () => {
  assert.equal(describeMessageStatus({}, false, 4).label, 'Unknown');
});

test('describeMessageStatus: shows attempts for an in-progress outbound DM', () => {
  const { attemptsText } = describeMessageStatus(
    { direction: 'out', status: 'sent_rf', attempts: 2 },
    false,
    4,
  );
  assert.equal(attemptsText, '2/4');
});

test('describeMessageStatus: hides attempts once delivered (acked)', () => {
  const { attemptsText } = describeMessageStatus(
    { direction: 'out', status: 'acked', attempts: 1 },
    false,
    4,
  );
  assert.equal(attemptsText, null);
});

test('describeMessageStatus: hides attempts for tactical broadcasts', () => {
  const { attemptsText } = describeMessageStatus(
    { direction: 'out', status: 'sent', attempts: 1 },
    true,
    4,
  );
  assert.equal(attemptsText, null);
});

test('describeMessageStatus: hides attempts when nothing has been transmitted yet', () => {
  const { attemptsText } = describeMessageStatus(
    { direction: 'out', status: 'queued', attempts: 0 },
    false,
    4,
  );
  assert.equal(attemptsText, null);
});

test('describeMessageStatus: hides attempts for inbound messages', () => {
  const { attemptsText } = describeMessageStatus(
    { direction: 'in', status: 'received', attempts: 0 },
    false,
    4,
  );
  assert.equal(attemptsText, null);
});
