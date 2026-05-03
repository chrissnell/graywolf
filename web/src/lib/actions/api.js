// Hand-written wrapper around the generated openapi-fetch client. Routes
// drop the `/api` prefix because client.ts sets baseUrl to '/api'.
import { api } from '../../api/client';

export const actionsApi = {
  list:     () => api.GET('/actions'),
  get:      (id) => api.GET('/actions/{id}', { params: { path: { id } } }),
  create:   (body) => api.POST('/actions', { body }),
  update:   (id, body) => api.PUT('/actions/{id}', { params: { path: { id } }, body }),
  remove:   (id) => api.DELETE('/actions/{id}', { params: { path: { id } } }),
  testFire: (id, args) => api.POST('/actions/{id}/test-fire', { params: { path: { id } }, body: { args } }),
};

export const credsApi = {
  list:   () => api.GET('/otp-credentials'),
  create: (body) => api.POST('/otp-credentials', { body }),
  remove: (id) => api.DELETE('/otp-credentials/{id}', { params: { path: { id } } }),
};

export const listenersApi = {
  list:   () => api.GET('/actions/listeners'),
  create: (addressee) => api.POST('/actions/listeners', { body: { addressee } }),
  remove: (name) => api.DELETE('/actions/listeners/{name}', { params: { path: { name } } }),
};

export const invocationsApi = {
  list:  (query) => api.GET('/actions/invocations', { params: { query } }),
  clear: () => api.DELETE('/actions/invocations'),
};
