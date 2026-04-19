// Messages REST wrapper. Thin layer over lib/api.js that:
//   - builds URLs + query strings for each of the 16 message endpoints
//   - mirrors the shape of the backend DTOs (JSDoc references to the
//     generated TS client at ./generated/api.d.ts so tooling sees the
//     types without re-deriving them)
//
// Why this sits on lib/api.js (not openapi-fetch client.ts):
//   - Every existing route in src/routes/ imports the `api` object from
//     `lib/api.js`. Using that convention keeps the messages feature
//     consistent with Dashboard.svelte / Beacons.svelte / Igate.svelte
//     and avoids a second auth/401-handling codepath.
//   - The generated TS client is the authoritative type source; if
//     Phase 7 or later wants strict typing, these functions map 1:1
//     to `operations[*]` in api.d.ts.
//   - lib/api.js handles 401→/#/login + mock fallback for dev-without-
//     backend; we inherit both by routing through it.
//
// SSE is NOT handled here — see messagesTransport.js for the
// EventSource path and polling fallback. The /api/messages/events
// endpoint doesn't round-trip cleanly through a JSON fetch helper.

import { api } from '../lib/api.js';

/**
 * @typedef {import('./generated/api').components['schemas']['dto.MessageResponse']} MessageResponse
 * @typedef {import('./generated/api').components['schemas']['dto.MessageListResponse']} MessageListResponse
 * @typedef {import('./generated/api').components['schemas']['dto.MessageChange']} MessageChange
 * @typedef {import('./generated/api').components['schemas']['dto.SendMessageRequest']} SendMessageRequest
 * @typedef {import('./generated/api').components['schemas']['dto.ConversationSummary']} ConversationSummary
 * @typedef {import('./generated/api').components['schemas']['dto.MessagePreferencesRequest']} MessagePreferencesRequest
 * @typedef {import('./generated/api').components['schemas']['dto.MessagePreferencesResponse']} MessagePreferencesResponse
 * @typedef {import('./generated/api').components['schemas']['dto.TacticalCallsignRequest']} TacticalCallsignRequest
 * @typedef {import('./generated/api').components['schemas']['dto.TacticalCallsignResponse']} TacticalCallsignResponse
 * @typedef {import('./generated/api').components['schemas']['dto.ParticipantsEnvelope']} ParticipantsEnvelope
 * @typedef {import('./generated/api').components['schemas']['dto.StationAutocomplete']} StationAutocomplete
 */

/** Build a `?a=1&b=2` string from a plain object; skip nullish values. */
function qs(params) {
  if (!params) return '';
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue;
    u.set(k, String(v));
  }
  const s = u.toString();
  return s ? `?${s}` : '';
}

// --- Messages -------------------------------------------------------

/**
 * GET /api/messages — cursor-paginated list. Returns {cursor, changes[]}.
 * @param {object} [params]
 * @param {string} [params.folder]       inbox | sent | all
 * @param {string} [params.peer]         peer callsign filter
 * @param {string} [params.thread_kind]  dm | tactical
 * @param {string} [params.thread_key]   peer callsign or tactical label
 * @param {string} [params.since]        RFC3339 lower bound
 * @param {string} [params.cursor]       opaque pagination cursor
 * @param {boolean} [params.unread_only]
 * @param {number} [params.limit]        1..500
 * @returns {Promise<MessageListResponse>}
 */
export function listMessages(params) {
  return api.get(`/messages${qs(params)}`);
}

/**
 * POST /api/messages — returns 202 with the persisted row.
 * @param {SendMessageRequest} req
 * @returns {Promise<MessageResponse>}
 */
export function sendMessage(req) {
  return api.post('/messages', req);
}

/**
 * GET /api/messages/{id}
 * @param {number} id
 * @returns {Promise<MessageResponse>}
 */
export function getMessage(id) {
  return api.get(`/messages/${encodeURIComponent(id)}`);
}

/**
 * DELETE /api/messages/{id} — soft delete (204).
 * @param {number} id
 */
export function deleteMessage(id) {
  return api.delete(`/messages/${encodeURIComponent(id)}`);
}

/**
 * POST /api/messages/{id}/read — 204.
 * @param {number} id
 */
export function markRead(id) {
  return api.post(`/messages/${encodeURIComponent(id)}/read`);
}

/**
 * POST /api/messages/{id}/unread — 204.
 * @param {number} id
 */
export function markUnread(id) {
  return api.post(`/messages/${encodeURIComponent(id)}/unread`);
}

/**
 * POST /api/messages/{id}/resend — 202 with the refreshed row, or 409
 * if the message is not in a terminal/failed state.
 * @param {number} id
 * @returns {Promise<MessageResponse>}
 */
export function resendMessage(id) {
  return api.post(`/messages/${encodeURIComponent(id)}/resend`);
}

// --- Conversations --------------------------------------------------

/**
 * GET /api/messages/conversations — rollup per thread.
 * @param {object} [params]
 * @param {number} [params.limit] 1..500 (default 200)
 * @returns {Promise<ConversationSummary[]>}
 */
export function listConversations(params) {
  return api.get(`/messages/conversations${qs(params)}`);
}

// --- Preferences ----------------------------------------------------

/**
 * GET /api/messages/preferences
 * @returns {Promise<MessagePreferencesResponse>}
 */
export function getPreferences() {
  return api.get('/messages/preferences');
}

/**
 * PUT /api/messages/preferences
 * @param {MessagePreferencesRequest} req
 * @returns {Promise<MessagePreferencesResponse>}
 */
export function putPreferences(req) {
  return api.put('/messages/preferences', req);
}

// --- Tactical callsigns --------------------------------------------

/**
 * GET /api/messages/tactical
 * @returns {Promise<TacticalCallsignResponse[]>}
 */
export function listTacticals() {
  return api.get('/messages/tactical');
}

/**
 * POST /api/messages/tactical
 * @param {TacticalCallsignRequest} req
 * @returns {Promise<TacticalCallsignResponse>}
 */
export function createTactical(req) {
  return api.post('/messages/tactical', req);
}

/**
 * PUT /api/messages/tactical/{id}
 * @param {number} id
 * @param {TacticalCallsignRequest} req
 * @returns {Promise<TacticalCallsignResponse>}
 */
export function updateTactical(id, req) {
  return api.put(`/messages/tactical/${encodeURIComponent(id)}`, req);
}

/**
 * DELETE /api/messages/tactical/{id} — 204.
 * @param {number} id
 */
export function deleteTactical(id) {
  return api.delete(`/messages/tactical/${encodeURIComponent(id)}`);
}

/**
 * GET /api/messages/tactical/{key}/participants
 * @param {string} key
 * @param {object} [params]
 * @param {string} [params.within]  Nd or Go duration, e.g. "7d" or "72h"
 * @returns {Promise<ParticipantsEnvelope>}
 */
export function getTacticalParticipants(key, params) {
  return api.get(`/messages/tactical/${encodeURIComponent(key)}/participants${qs(params)}`);
}

// --- Tactical invite accept ----------------------------------------

/**
 * POST /api/tacticals — subscribe the operator to a tactical callsign
 * in response to an `!GW1 INVITE <TAC>` invite bubble's Accept button.
 *
 * The endpoint is tactical-keyed, not message-keyed: "accept" means
 * "enable my subscription to TAC". `source_message_id` lets the server
 * stamp the triggering invite row with `invite_accepted_at` for audit;
 * omit (or pass 0) when there isn't a bubble context.
 *
 * `already_member=true` is a normal 200 OK with a distinct UX meaning
 * ("Already a member of TAC") — clients must not treat it as an error.
 *
 * Phase-invite-2 may finalize the path as `/tacticals/subscribe`; Phase
 * 3 defaulted to `/tacticals` per the plan. If Phase 2 landed a different
 * confirmed path, update the string below and regenerate the TS client.
 *
 * @param {{callsign: string, source_message_id?: number}} req
 * @returns {Promise<{tactical: import('./generated/api').components['schemas']['dto.TacticalCallsignResponse'], already_member: boolean}>}
 */
export function acceptTactical(req) {
  const body = {
    callsign: (req?.callsign || '').toUpperCase(),
    source_message_id: req?.source_message_id || 0,
  };
  return api.post('/tacticals', body);
}

// --- Station autocomplete (out-of-band helper) ---------------------

/**
 * GET /api/stations/autocomplete — merges bot directory + station
 * cache + message history into a ranked suggestion list.
 * @param {object} [params]
 * @param {string} [params.q]       prefix query
 * @param {number} [params.limit]   1..50
 * @returns {Promise<StationAutocomplete[]>}
 */
export function autocompleteStations(params) {
  return api.get(`/stations/autocomplete${qs(params)}`);
}
