// Drive the graywolf SPA in *Android mode* and capture Play Store
// screenshots. The Makefile target `android-screenshots` launches a
// local graywolf seeded with a snapshot of a real station's DBs, then
// runs this script against it.
//
// Why the bridge injection: the SPA decides Android-vs-other-platforms
// from globalThis.GraywolfWebInterface.getBearerToken() (the Android
// WebView bridge). A plain browser has no bridge, so the SPA would
// render the macOS/Linux/Windows UI -- showing surfaces hidden on
// Android (Actions, AGW, Simulation). We inject a fake bridge via
// addInitScript so Platform.kind === 'android' and the SPA renders the
// real Android-filtered UI.
//
// Auth wrinkle: injecting the bridge also flips the SPA into bearer-
// token auth (no /login). The local graywolf has no bearer middleware
// (that's Android-only), so we instead authenticate with a normal
// session cookie BEFORE injecting the bridge -- the cookie rides along
// on every request and the ignored Authorization: Bearer header is
// harmless. So the order is: create-user/login (no bridge) -> inject
// bridge -> screenshot.

import { chromium } from 'playwright';
import { mkdir } from 'node:fs/promises';
import process from 'node:process';

const BASE = process.env.GW_SCREENSHOT_BASE || 'http://127.0.0.1:8088';
const OUT = process.env.GW_SCREENSHOT_OUT || 'scratch/ss-work/shots';
// Tablet portrait-ish landscape. The test device reports 1280x800;
// Play accepts tablet screenshots at this size.
const WIDTH = 1280;
const HEIGHT = 800;
const USER = 'admin';
const PASS = 'screenshot-admin-pw';

// Android-visible routes worth showing on the Play listing, each with a
// filename and a selector to wait for so we don't shoot a half-rendered
// page. Keep this list curated -- Play wants 2-8 screenshots.
const ROUTES = [
  { hash: '#/', file: '01-dashboard.png', wait: '.nav-list' },
  { hash: '#/map', file: '02-livemap.png', wait: 'canvas, .maplibregl-canvas' },
  { hash: '#/messages', file: '03-messages.png', wait: '.nav-list' },
  { hash: '#/channels', file: '04-channels.png', wait: '.nav-list' },
  { hash: '#/beacons', file: '05-beacons.png', wait: '.nav-list' },
];

const BRIDGE_INIT = () => {
  // Minimal stand-in for the Android WebView's GraywolfWebInterface.
  // getBearerToken() must return a non-empty string for Platform.kind
  // to resolve to 'android'. The value is otherwise unused here (the
  // local server authenticates us by cookie).
  globalThis.GraywolfWebInterface = {
    getBearerToken: () => 'screenshot-bridge-token',
    // listUsbDevices is consulted by the PTT page's device source; an
    // empty array keeps it from throwing.
    listUsbDevices: () => '[]',
    requestUsbPermission: () => {},
    requestBluetoothPermission: () => {},
  };
};

async function main() {
  await mkdir(OUT, { recursive: true });

  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: WIDTH, height: HEIGHT },
    deviceScaleFactor: 2,
    baseURL: BASE,
  });
  const page = await context.newPage();

  // --- Step 1: authenticate (no bridge yet) ------------------------
  // Drive auth via the API rather than the two-stage UI form: POST
  // /auth/setup creates the first user (the seed DB has none) but does
  // NOT log in, then POST /auth/login sets the graywolf_session cookie.
  // page.request shares the browser context's cookie jar, so the cookie
  // is live for subsequent page.goto navigations. Setup is idempotent
  // enough for our purposes -- if a user already exists it 403s, which
  // we ignore and proceed straight to login.
  const setupResp = await page.request.post('/api/auth/setup', {
    data: { username: USER, password: PASS },
  });
  console.log(`/auth/setup -> ${setupResp.status()}`);
  const loginResp = await page.request.post('/api/auth/login', {
    data: { username: USER, password: PASS },
  });
  if (!loginResp.ok()) {
    throw new Error(`login failed: ${loginResp.status()} ${await loginResp.text()}`);
  }
  const statusResp = await page.request.get('/api/status');
  if (statusResp.status() === 401) {
    throw new Error('still unauthenticated after setup/login; aborting');
  }
  console.log(`auth OK (/api/status -> ${statusResp.status()})`);

  // --- Step 2: pre-ack release notes via the API -------------------
  // Pre-ack release notes so the "What's New" popup never appears and
  // no "Saved" toast pollutes the screenshots. The endpoint acks every
  // note up to the running version; no body required. Must happen before
  // the bridge is injected and before any page navigations.
  const ackResp = await page.request.post('/api/release-notes/ack');
  console.log(`/release-notes/ack -> ${ackResp.status()}`);

  // --- Step 3: inject the Android bridge for all future loads ------
  await context.addInitScript(BRIDGE_INIT);

  // --- Step 4: screenshot each Android-visible route ---------------
  for (const route of ROUTES) {
    await page.goto(`/${route.hash}`, { waitUntil: 'networkidle' });
    // The hash-router needs a tick to mount the route component.
    await page.waitForTimeout(800);
    try {
      await page.waitForSelector(route.wait, { timeout: 8000 });
    } catch {
      console.warn(`  (selector ${route.wait} not found for ${route.hash}; shooting anyway)`);
    }
    if (route.hash === '#/map') {
      // Wait for at least one station marker to appear so the map has
      // auto-fitted to the station cluster (first poll ~5-6s). Fall back
      // gracefully -- warn and shoot rather than aborting the whole run.
      try {
        await page.waitForSelector('.gw-station-marker', { timeout: 15000 });
        // Let the auto-fit fly-to animation settle before shooting.
        await page.waitForTimeout(1500);
      } catch {
        console.warn('  (no station markers appeared on the map; shooting anyway)');
      }
    } else {
      await page.waitForTimeout(1200);
    }
    const path = `${OUT}/${route.file}`;
    await page.screenshot({ path });
    console.log(`shot ${route.hash} -> ${path}`);
  }

  await browser.close();
  console.log(`\nDone. ${ROUTES.length} screenshots in ${OUT}/`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
