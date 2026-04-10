// Polyfill crypto.randomUUID for non-secure contexts (plain HTTP on LAN).
if (typeof crypto !== 'undefined' && !crypto.randomUUID) {
  crypto.randomUUID = function () {
    const bytes = crypto.getRandomValues(new Uint8Array(16));
    bytes[6] = (bytes[6] & 0x0f) | 0x40; // version 4
    bytes[8] = (bytes[8] & 0x3f) | 0x80; // variant 1
    const h = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
    return `${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`;
  };
}

import App from './App.svelte';
import { mount } from 'svelte';

const app = mount(App, { target: document.getElementById('app') });
export default app;
