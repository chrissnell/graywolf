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
  { hash: '#/ptt', file: '05-ptt.png', wait: '.nav-list' },
  { hash: '#/beacons', file: '06-beacons.png', wait: '.nav-list' },
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

  // --- Step 2: inject the Android bridge for all future loads ------
  await context.addInitScript(BRIDGE_INIT);

  // --- Step 2b: dismiss the "What's New" release-notes popup -------
  // App.svelte mounts NewsPopup whenever the user has unseen release
  // notes (tracked server-side). A fresh user has the current build's
  // note unseen, so the popup covers every page. Clicking "Got it"
  // acks it server-side, so it stays dismissed for the rest of the run.
  await page.goto('/#/', { waitUntil: 'networkidle' });
  await page.waitForTimeout(1000);
  const gotIt = page.locator('button:has-text("Got it")');
  if (await gotIt.count()) {
    await gotIt.first().click();
    await page.waitForTimeout(500);
    console.log('dismissed What\'s New popup');
  }

  // --- Step 3: screenshot each Android-visible route ---------------
  for (const route of ROUTES) {
    await page.goto(`/${route.hash}`, { waitUntil: 'networkidle' });
    // The hash-router needs a tick to mount the route component.
    await page.waitForTimeout(800);
    try {
      await page.waitForSelector(route.wait, { timeout: 8000 });
    } catch {
      console.warn(`  (selector ${route.wait} not found for ${route.hash}; shooting anyway)`);
    }
    // Map tiles + history markers stream in async; give them time.
    await page.waitForTimeout(route.hash === '#/map' ? 4000 : 1200);
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
