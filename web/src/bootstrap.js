// First-evaluated module in the SPA. Its only job is to install the
// Android bearer-token wrappers around globalThis.fetch and
// globalThis.WebSocket BEFORE any other module begins evaluating.
// main.js imports this file as its very first import so the wrappers
// are in place by the time any store, component, or library issues
// its first request.
//
// This file is intentionally tiny and import-light. Adding heavy
// imports here defeats the purpose: if you import a store from here,
// that store's top-level fetches fire before the wrappers install.

import { installSecureFetch, installSecureWebSocket } from './lib/secureFetch.js';

installSecureFetch();
installSecureWebSocket();
