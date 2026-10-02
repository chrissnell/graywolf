// Per-row channel-pill classification for the Beacons list card.
//
// A beacon's `channel` field overloads 0 with two distinct meanings
// depending on `send_path`: "no RF leg" for an is_only beacon, or
// "Auto (resolve at send time)" for an rf/both beacon. A bare
// channelRefStatus() lookup only sees the numeric FK and can't tell
// these apart, so this helper resolves send_path first and only falls
// through to channelRefStatus() for a real, non-zero channel
// reference. Deliberately kept out of channelRefStatus.js itself: that
// helper is shared with Digipeater.svelte's from_channel/to_channel,
// which have no Auto concept and where 0 genuinely means "unset".
import { channelRefStatus, STATUS_OK, STATUS_DELETED } from './channelRefStatus.js';

export const AUTO_CHANNEL_LABEL = 'Auto (first APRS-eligible channel)';

/**
 * @typedef {Object} BeaconChannelDisplay
 * @property {'is_only'|'auto'|'ok'|'unreachable'|'deleted'} mode
 * @property {string} label Text for the small label span.
 * @property {string|undefined} value Text for the channel-value span;
 *   undefined when showValue is false.
 * @property {boolean} showValue Whether the value span should render.
 * @property {string} ariaLabel aria-label for the label span ('' when
 *   not meaningful, i.e. is_only/auto).
 * @property {string} title title attribute for the label span.
 * @property {boolean} broken Drives the card's `broken`/`danger` CSS.
 */

/**
 * Classifies one beacon row's channel reference for list-card display.
 *
 * @param {{channel: number, send_path?: string}} beaconRow
 * @param {Map<number, object>} channelsById
 * @returns {BeaconChannelDisplay}
 */
export function beaconChannelDisplay(beaconRow, channelsById) {
  const channel = beaconRow?.channel;

  if (beaconRow?.send_path === 'is_only') {
    return {
      mode: 'is_only',
      label: 'Send to',
      value: 'APRS-IS only (no radio)',
      showValue: true,
      ariaLabel: '',
      title: '',
      broken: false,
    };
  }

  if (channel === 0) {
    return {
      mode: 'auto',
      label: 'Channel',
      value: AUTO_CHANNEL_LABEL,
      showValue: true,
      ariaLabel: '',
      title: '',
      broken: false,
    };
  }

  const refStatus = channelRefStatus(channel, channelsById);

  if (refStatus.status === STATUS_DELETED) {
    return {
      mode: 'deleted',
      label: 'Channel deleted',
      value: undefined,
      showValue: false,
      ariaLabel: `Channel #${channel} deleted`,
      title: `Channel #${channel} deleted`,
      broken: true,
    };
  }

  const value = refStatus.channel?.name ?? `Channel #${channel}`;

  if (refStatus.status !== STATUS_OK) {
    return {
      mode: 'unreachable',
      label: `Unreachable: ${refStatus.reason}`,
      value,
      showValue: true,
      ariaLabel: `${refStatus.channel?.name ?? `Channel #${channel}`} unreachable: ${refStatus.reason}`,
      title: `Unreachable: ${refStatus.reason}`,
      broken: true,
    };
  }

  return {
    mode: 'ok',
    label: 'Channel',
    value,
    showValue: true,
    ariaLabel: `Channel ${refStatus.channel?.name ?? `#${channel}`}`,
    title: '',
    broken: false,
  };
}
