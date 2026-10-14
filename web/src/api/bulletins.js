import { api } from '../lib/api.js';

// --- Bulletins ----------------------------------------------------------

/**
 * GET /api/bulletins — list bulletins.
 * @param {{ direction?: string, unread_only?: boolean }} [params]
 * @returns {Promise<Array>}
 */
export function listBulletins(params = {}) {
  const u = new URLSearchParams();
  if (params.direction) u.set('direction', params.direction);
  if (params.unread_only) u.set('unread_only', 'true');
  const qs = u.toString();
  return api.get(`/bulletins${qs ? '?' + qs : ''}`);
}

/**
 * POST /api/bulletins — create and begin transmitting an outbound bulletin.
 * @param {{ slot: string, text: string }} req
 */
export function sendBulletin(req) {
  return api.post('/bulletins', req);
}

/**
 * DELETE /api/bulletins/:id — soft-delete a bulletin (stops retransmits).
 * @param {number} id
 */
export function deleteBulletin(id) {
  return api.delete(`/bulletins/${id}`);
}

/**
 * POST /api/bulletins/:id/read — mark a single inbound bulletin as read.
 * @param {number} id
 */
export function markBulletinRead(id) {
  return api.post(`/bulletins/${id}/read`);
}

/**
 * POST /api/bulletins/read-all — mark all inbound bulletins as read.
 */
export function markAllBulletinsRead() {
  return api.post('/bulletins/read-all');
}
