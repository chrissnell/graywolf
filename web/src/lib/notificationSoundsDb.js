// IndexedDB storage for operator-uploaded custom notification sounds.
// Device-local by design, like notification-prefs-store.svelte.js — an
// uploaded sound file lives in this browser's profile only, not synced
// to the graywolf server. localStorage can't hold binary Blobs (and has
// a much smaller quota), so this uses IndexedDB directly rather than
// pulling in a wrapper library for two keys ('message' / 'bulletin').

const DB_NAME = 'graywolf-notification-sounds';
const DB_VERSION = 1;
const STORE = 'sounds';

/** @returns {Promise<IDBDatabase>} */
function openDb() {
  return new Promise((resolve, reject) => {
    if (typeof indexedDB === 'undefined') {
      reject(new Error('IndexedDB unavailable'));
      return;
    }
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      req.result.createObjectStore(STORE);
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

/**
 * @param {'message'|'bulletin'} kind
 * @param {Blob} blob
 * @param {string} name original file name, shown in the settings UI
 */
export async function putCustomSound(kind, blob, name) {
  const db = await openDb();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite');
    tx.objectStore(STORE).put({ blob, name }, kind);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

/**
 * @param {'message'|'bulletin'} kind
 * @returns {Promise<{blob: Blob, name: string}|null>}
 */
export async function getCustomSound(kind) {
  const db = await openDb();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readonly');
    const req = tx.objectStore(STORE).get(kind);
    req.onsuccess = () => resolve(req.result || null);
    req.onerror = () => reject(req.error);
  });
}

/** @param {'message'|'bulletin'} kind */
export async function deleteCustomSound(kind) {
  const db = await openDb();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite');
    tx.objectStore(STORE).delete(kind);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}
