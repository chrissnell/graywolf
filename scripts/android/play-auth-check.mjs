// Probe whether a Play upload service account can actually manage the
// app's testing tracks yet -- i.e. whether Google has finished
// propagating the permission grant (can take up to 24h after you invite
// the account in Play Console).
//
// Authenticates with the service-account JSON and opens + immediately
// deletes a Play "edit" on the package. The edit is a transient,
// side-effect-free transaction object; creating one requires the same
// "Release apps to testing tracks" permission the CI auto-upload uses,
// so a successful insert proves the account is ready.
//
// Usage: node scripts/android/play-auth-check.mjs <service-account.json>
//        (or: make android-play-check JSON=path/to/sa.json)

import { GoogleAuth } from 'google-auth-library';
import process from 'node:process';

const keyFile = process.argv[2];
if (!keyFile) {
  console.error('usage: node play-auth-check.mjs <service-account.json>');
  process.exit(2);
}
const PKG = process.env.GW_PACKAGE || 'com.nw5w.graywolf';
const base = `https://androidpublisher.googleapis.com/androidpublisher/v3/applications/${PKG}/edits`;

const auth = new GoogleAuth({
  keyFile,
  scopes: ['https://www.googleapis.com/auth/androidpublisher'],
});

try {
  const client = await auth.getClient();
  // Open a transient edit...
  const res = await client.request({ url: base, method: 'POST' });
  const editId = res.data.id;
  // ...and clean it up so we leave no half-open edit behind.
  try {
    await client.request({ url: `${base}/${editId}`, method: 'DELETE' });
  } catch { /* best-effort cleanup; edits auto-expire anyway */ }
  console.log(`OK (HTTP ${res.status}) -- service account can manage ${PKG} testing tracks.`);
  console.log('Propagation is complete; the CI auto-upload will work.');
  process.exit(0);
} catch (e) {
  const status = e?.response?.status;
  if (status === 403) {
    console.error(`HTTP 403 -- authenticated, but no permission on ${PKG} yet.`);
    console.error('Permission grant has not propagated. Wait and re-run (up to 24h).');
  } else if (status === 401) {
    console.error('HTTP 401 -- authentication failed. Check the JSON key is valid');
    console.error('and belongs to the account you invited in Play Console.');
  } else if (status === 404) {
    console.error(`HTTP 404 -- package ${PKG} not found for this account.`);
    console.error('Confirm the app exists and the account was granted access to it.');
  } else {
    console.error(`error${status ? ` (HTTP ${status})` : ''}: ${e?.message || e}`);
  }
  process.exit(1);
}
