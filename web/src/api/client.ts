// Typed OpenAPI client built on the generated path/schema types. The
// generated spec anchors all paths under basePath `/api`, so baseUrl is
// set here to match. Consumers call e.g.:
//
//   import { api } from '../api/client';
//   const { data, error } = await api.GET('/channels');
//
// This file is hand-written. The types it imports are generated —
// regenerate with `npm run api:generate` (or `make api-client`).
//
// Behavior parity with the legacy hand-written src/lib/api.js:
//  - On 401 responses, redirect to the hash-router login route
//    (unless we're already there) — matches `window.location.hash = '#/login'`.
//  - Consumers get the standard openapi-fetch `{ data, error, response }`
//    tuple. The `error` field is whatever the server returned as the
//    response body; we don't throw. This is a deliberate divergence from
//    lib/api.js (which throws), because openapi-fetch's contract is a
//    tuple return and callers of api.GET(...) expect that shape.

import createClient, { type Middleware } from 'openapi-fetch';
import type { paths } from './generated/api';

// Match lib/api.js: hash-based router target. Guard prevents a redirect
// loop when a 401 happens on a request fired from the login page itself
// (e.g. during an in-flight request when the session just expired).
const LOGIN_HASH = '#/login';

function isOnLoginRoute(): boolean {
  if (typeof window === 'undefined') return true; // SSR/tests: no-op
  return window.location.hash.startsWith(LOGIN_HASH);
}

function redirectToLogin(): void {
  if (typeof window === 'undefined') return;
  if (isOnLoginRoute()) return;
  window.location.hash = LOGIN_HASH;
}

// Middleware that mirrors lib/api.js's 401 handling. We only observe the
// response; we don't mutate it, so callers still see the 401 surface via
// the normal `{ error, response }` tuple and can render appropriate UI
// state before the hash change takes effect.
const authMiddleware: Middleware = {
  onResponse({ response }) {
    if (response.status === 401) {
      redirectToLogin();
    }
    // Returning undefined leaves the response unchanged.
  },
};

export const api = createClient<paths>({
  baseUrl: '/api',
  credentials: 'same-origin',
});

api.use(authMiddleware);

export type Api = typeof api;
