import { api } from '../api.js';
import { toasts } from '../stores.js';

let useSDCard = $state(false);
let sdCardAvailable = $state(false);
let sdCardPath = $state('');
let internalPath = $state('');

async function fetchConfig() {
  const data = await api.get('/android/storage');
  if (!data) return;
  useSDCard = data.use_sd_card;
  sdCardAvailable = data.sd_card_available;
  sdCardPath = data.sd_card_path;
  internalPath = data.internal_path;
}

export const storageState = {
  get useSDCard() { return useSDCard; },
  get sdCardAvailable() { return sdCardAvailable; },
  get sdCardPath() { return sdCardPath; },
  get internalPath() { return internalPath; },
  // active path: where data currently lives
  get activePath() { return useSDCard && sdCardPath ? sdCardPath : internalPath; },
  fetchConfig,
};
