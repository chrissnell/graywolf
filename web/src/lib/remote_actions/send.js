// web/src/lib/remote_actions/send.js
//
// Assembles `@@<otp>#<action> [args]` and posts it via the existing
// /api/messages send path. We deliberately reuse the same endpoint
// hand-typed Action lines go through so outbound Actions are
// indistinguishable on the wire and in the audit log.
//
// The `sendMessage` dependency is injected (typically the helper from
// web/src/api/messages.js) so this module stays testable without
// hitting the real client.
import { recordOutboundFire } from './reply_match.js';

export function assembleWireString({ otp, actionName, argsString }) {
  const head = `@@${otp ?? ''}#${actionName}`;
  const args = (argsString ?? '').trim();
  return args ? `${head} ${args}` : head;
}

export function lengthFor(opts) {
  return assembleWireString(opts).length;
}

export async function sendActionFire({
  target,
  otp,
  actionName,
  argsString,
  sendMessage,
}) {
  const text = assembleWireString({ otp, actionName, argsString });
  const result = await sendMessage({
    to: target,
    text,
  });
  // Record only after a successful await: a thrown send didn't make it
  // to the wire, so there's no inbound reply to correlate with.
  recordOutboundFire(target, actionName, Date.now());
  return result;
}
