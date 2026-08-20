// Bulletins REST wrapper. Thin layer over lib/api.js that builds URLs
// for each of the bulletin CRUD endpoints. Follows the same conventions
// as web/src/api/messages.js.
import { api } from '../lib/api.js';

/**
 * GET /api/bulletins/config — returns the global bulletins config singleton.
 * @returns {Promise<{tx_channel: number, send_path: string}>}
 */
export function getBulletinsConfig() {
  return api.get('/bulletins/config');
}

/**
 * PUT /api/bulletins/config — update global bulletins config.
 * @param {{ tx_channel: number, send_path: string }} req
 * @returns {Promise<{tx_channel: number, send_path: string}>}
 */
export function putBulletinsConfig(req) {
  return api.put('/bulletins/config', req);
}

/**
 * GET /api/bulletins — returns all groups with their items.
 * Global group (name="") is always first.
 * @returns {Promise<Array>}
 */
export function listBulletinGroups() {
  return api.get('/bulletins');
}

/**
 * POST /api/bulletins — create a new bulletin group.
 * @param {{ name: string, send_path: string, digi_path: string, initial_rate: number, decay_factor: number, stable_rate: number, active: boolean }} req
 * @returns {Promise<object>}
 */
export function createBulletinGroup(req) {
  return api.post('/bulletins', req);
}

/**
 * GET /api/bulletins/{id}
 * @param {number} id
 * @returns {Promise<object>}
 */
export function getBulletinGroup(id) {
  return api.get(`/bulletins/${encodeURIComponent(id)}`);
}

/**
 * PUT /api/bulletins/{id} — update group settings.
 * @param {number} id
 * @param {{ name?: string, send_path: string, digi_path: string, initial_rate: number, decay_factor: number, stable_rate: number, active: boolean }} req
 * @returns {Promise<object>}
 */
export function updateBulletinGroup(id, req) {
  return api.put(`/bulletins/${encodeURIComponent(id)}`, req);
}

/**
 * DELETE /api/bulletins/{id} — 204. Returns 403 for the Global group.
 * @param {number} id
 */
export function deleteBulletinGroup(id) {
  return api.delete(`/bulletins/${encodeURIComponent(id)}`);
}

/**
 * PUT /api/bulletins/{id}/items/{slot} — upsert one slot.
 * @param {number} groupId
 * @param {number} slot  0-9
 * @param {{ text: string, active: boolean }} req
 */
export function upsertBulletinItem(groupId, slot, req) {
  return api.put(`/bulletins/${encodeURIComponent(groupId)}/items/${encodeURIComponent(slot)}`, req);
}

/**
 * DELETE /api/bulletins/{id}/items/{slot} — clear one slot (204).
 * @param {number} groupId
 * @param {number} slot  0-9
 */
export function clearBulletinItem(groupId, slot) {
  return api.delete(`/bulletins/${encodeURIComponent(groupId)}/items/${encodeURIComponent(slot)}`);
}

/**
 * POST /api/bulletins/{id}/items/{slot}/send — transmit immediately (204).
 * @param {number} groupId
 * @param {number} slot  0-9
 */
export function sendBulletinNow(groupId, slot) {
  return api.post(`/bulletins/${encodeURIComponent(groupId)}/items/${encodeURIComponent(slot)}/send`);
}
