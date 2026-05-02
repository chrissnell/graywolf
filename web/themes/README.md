# graywolf themes

Every color theme graywolf ships lives here as a single `.css` file.
Adding a new theme is a small, self-contained pull request — no Go
changes, no OpenAPI regen, no build-config edits.

## Add a theme in six steps

1. **Pick an id.** Lowercase, numbers, and hyphens only, 1–64 chars
   (regex: `^[a-z0-9][a-z0-9-]{0,63}$`). Example: `field-day-2026`.
2. **Copy an existing theme file** as a starting point:
   ```sh
   cp grayscale.css field-day-2026.css
   ```
3. **Rename the selector** inside the new file from
   `:root:root[data-theme="grayscale"]` to
   `:root:root[data-theme="field-day-2026"]`.
   The doubled `:root:root` prefix matters: it gives the rule
   specificity (0,2,1) so your theme wins against chonky-ui's
   `@media (prefers-color-scheme: dark) :root:not([data-theme="light"])`
   fallback at (0,1,1). Without the doubling, OS dark mode (e.g. on
   Windows) silently overrides any light theme the operator picks.
   Sub-rules like `:root[data-theme="X"] .badge` already sit at (0,1,2)
   and don't need the bump.
4. **Tweak the CSS variables** (full list below). You don't have to
   override every one — unset variables fall through to chonky-ui's
   `:root` defaults, but most themes override the whole palette for
   consistency.
5. **Add an entry to `themes.json`** (order determines dropdown order):
   ```json
   {
     "id": "field-day-2026",
     "name": "Field Day 2026",
     "description": "Team colors for this year's Field Day."
   }
   ```
6. **Preview locally.** From `graywolf/web/`:
   ```sh
   npm run dev
   ```
   Open Preferences in the browser, pick your theme, and click around
   Dashboard, Messages, and Live Map. Adjust palette until it reads
   well on every surface.

That's the whole PR. The Vite build auto-imports every `*.css` file in
this directory, so you don't need to register your file anywhere else.

## Required CSS variables

Every theme should override at least these. Grouped by role; most
themes will want to set all of them.

### Surfaces
- `--color-bg` — app background.
- `--color-surface` — card / panel background.
- `--color-surface-raised` — elevated surface (modal, popover, raised button).
- `--color-border` — default border.
- `--color-border-subtle` — dividers inside cards.

### Text
- `--color-text` — body copy.
- `--color-text-muted` — secondary text (hint, caption).
- `--color-text-dim` — tertiary text (placeholder, timestamp).

### Primary / accent
- `--color-primary` — interactive elements (links, primary buttons).
- `--color-primary-hover` — hovered primary.
- `--color-primary-muted` — tinted background for active-nav / selected row.
- `--color-primary-fg` — text color when placed on `--color-primary`.
- `--color-accent` — secondary accent; often used for positive affordances.

### Status colors
Each of `danger`, `success`, `warning`, `info` has a trio:
`--color-<status>`, `--color-<status>-muted`, `--color-<status>-fg`.
`success` additionally has `--color-success-fill` for solid fills.

### Buttons
- `--color-btn-bg` — neutral (non-primary) button background.
- `--color-btn-bg-hover` — neutral button hover.
- `--color-btn-border` — button border.

### Typography / shape (optional)
- `--font-mono` — monospaced stack. Leave alone unless you have reason.
- `--radius` — corner radius in px. Most shipped themes use 2px;
  Graywolf Night bumps it to 6px for a softer feel at low contrast.

## Contrast and legibility

Please eyeball these before submitting:

- Text on every surface (`--color-bg`, `--color-surface`,
  `--color-surface-raised`) should meet WCAG AA (4.5:1) against its
  paired text color.
- Neutral buttons should have a visible border in light themes — the
  chonky-ui button uses `--color-btn-border`, not a box-shadow.
- Primary buttons should have enough contrast between
  `--color-primary` and `--color-primary-fg` that the label is still
  legible when the button is disabled at reduced opacity.

## Why no Go change?

The Go backend validates theme ids by regex only — it doesn't know the
set of shipped themes. That's by design: anyone with CSS skills can
contribute a theme without touching the backend, and the API surface
doesn't churn when the theme list changes.

If a stale DB row references a theme that was later removed, the
frontend falls back to the default automatically (see
`src/lib/themes/registry.js` and `theme-store.svelte.js`).
