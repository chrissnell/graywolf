// web/src/lib/remote_actions/api.js
//
// Hand-written wrapper around the generated openapi-fetch client. Routes
// drop the `/api` prefix because client.ts sets baseUrl to '/api'.
// Mirrors web/src/lib/actions/api.js so callers in the same feature
// area look familiar.
import { api } from '../../api/client';

export const remoteCredsApi = {
  list:   ()       => api.GET('/remote-actions/credentials'),
  create: (body)   => api.POST('/remote-actions/credentials', { body }),
  update: (id, b)  => api.PUT('/remote-actions/credentials/{id}', { params: { path: { id } }, body: b }),
  remove: (id)     => api.DELETE('/remote-actions/credentials/{id}', { params: { path: { id } } }),
};

export const remoteMacrosApi = {
  list:    (target) => api.GET('/remote-actions/macros', { params: { query: { target } } }),
  create:  (body)   => api.POST('/remote-actions/macros', { body }),
  update:  (id, b)  => api.PUT('/remote-actions/macros/{id}', { params: { path: { id } }, body: b }),
  remove:  (id)     => api.DELETE('/remote-actions/macros/{id}', { params: { path: { id } } }),
  reorder: (target_call, ids) => api.POST('/remote-actions/macros/reorder', { body: { target_call, ids } }),
};

export const remoteOtpApi = {
  generate: (id) => api.POST('/remote-actions/otp/{id}', { params: { path: { id } } }),
};
