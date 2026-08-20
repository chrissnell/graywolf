import { api } from '../lib/api.js';

export async function getWeatherConfig() {
  return api.get('/weather/config');
}

export async function putWeatherConfig(cfg) {
  return api.put('/weather/config', cfg);
}

export async function getWeatherCounties(state) {
  const qs = state ? `?state=${encodeURIComponent(state)}` : '';
  return api.get(`/weather/counties${qs}`);
}

export async function putCountyPrefs(fips, allowTX) {
  return api.put(`/weather/counties/${encodeURIComponent(fips)}/prefs`, { allow_tx: allowTX });
}
