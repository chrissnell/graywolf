// Generate Play Store store-listing graphics from the graywolf brand
// assets: the 512x512 app icon and the 1024x500 feature graphic. Renders
// the wolf SVG in chromium (crisp vector output) and screenshots at the
// exact required dimensions. Output to scratch/ss-work/assets/.
//
// Run: node scripts/screenshots/assets.mjs   (from repo root)

import { chromium } from 'playwright';
import { readFileSync, mkdirSync } from 'node:fs';
import process from 'node:process';

const OUT = process.env.GW_ASSET_OUT || 'scratch/ss-work/assets';
mkdirSync(OUT, { recursive: true });

// Inline the wolf SVG so there are no file:// load races. Strip the XML
// prolog so it embeds cleanly inside HTML.
const wolfSvg = readFileSync('web/src/assets/graywolf.svg', 'utf8')
  .replace(/<\?xml[^>]*\?>/, '')
  .trim();

const AMBER = '#ffaa00';
const INK = '#111111';
const MUTED = '#666666';

async function shoot(page, html, w, h, file) {
  await page.setViewportSize({ width: w, height: h });
  await page.setContent(html, { waitUntil: 'networkidle' });
  await page.screenshot({ path: `${OUT}/${file}`, clip: { x: 0, y: 0, width: w, height: h } });
  console.log(`wrote ${OUT}/${file} (${w}x${h})`);
}

// 512x512 app icon: wolf centered on white, sized to ~62% of the square
// so Play's corner mask never clips it.
const iconHtml = `<!doctype html><html><head><meta charset="utf8">
<style>
  html,body{margin:0;padding:0}
  .canvas{width:512px;height:512px;background:#ffffff;display:flex;
    align-items:center;justify-content:center}
  .logo{height:340px;width:auto;display:block}
  .logo svg{height:340px;width:auto}
</style></head><body>
  <div class="canvas"><div class="logo">${wolfSvg}</div></div>
</body></html>`;

// 1024x500 feature graphic: white field, wolf on the left, wordmark +
// tagline on the right, amber rule under the wordmark to echo the app's
// primary accent.
const featureHtml = `<!doctype html><html><head><meta charset="utf8">
<style>
  html,body{margin:0;padding:0}
  .fg{width:1024px;height:500px;background:#ffffff;display:flex;
    align-items:center;gap:64px;padding:0 80px;box-sizing:border-box;
    font-family:"JetBrains Mono","SFMono-Regular",Menlo,Consolas,monospace}
  .fg .logo svg{height:300px;width:auto;display:block}
  .copy{display:flex;flex-direction:column;gap:14px}
  .wordmark{font-size:84px;font-weight:700;color:${INK};letter-spacing:-2px;line-height:1}
  .rule{width:120px;height:8px;background:${AMBER};border-radius:4px}
  .tagline{font-size:30px;color:${MUTED};line-height:1.3;max-width:520px}
</style></head><body>
  <div class="fg">
    <div class="logo">${wolfSvg}</div>
    <div class="copy">
      <div class="wordmark">Graywolf&nbsp;APRS</div>
      <div class="rule"></div>
      <div class="tagline">A modern APRS stack for your smartphone and tablet</div>
    </div>
  </div>
</body></html>`;

async function main() {
  const browser = await chromium.launch();
  const page = await browser.newPage({ deviceScaleFactor: 1 });
  await shoot(page, iconHtml, 512, 512, 'icon-512.png');
  await shoot(page, featureHtml, 1024, 500, 'feature-1024x500.png');
  await browser.close();
  console.log('\nDone. Play store graphics in', OUT);
}

main().catch((e) => { console.error(e); process.exit(1); });
