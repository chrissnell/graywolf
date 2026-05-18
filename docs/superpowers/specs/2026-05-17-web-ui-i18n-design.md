# Web UI Internationalization — Design

Date: 2026-05-17
Status: Approved (brainstorming), pending implementation plan
Scope: `web/` Svelte SPA + bounded backend error-string contract

## Goal

Make the Graywolf web UI translatable by community contributors using the
translation tools they already know, without making i18n a tax on day-to-day
feature development or a constraint on future platforms.

## Decisions (locked during brainstorming)

| Decision | Choice | Why |
|---|---|---|
| Language ambition (now) | Framework + English only | Ship the infrastructure and a complete English catalog; real translations land later as `.po` files drop in. |
| Extraction scope | Full extraction now | Every user-facing string in all ~86 `.svelte` files wrapped this project, so the whole UI is translation-ready when a language arrives. |
| Server-originated text | Client UI + API text **shown in the UI** | API error/validation text that reaches the operator's screen is translatable. Internal errors, log-stream lines, release notes are out of scope. |
| Catalog format | Gettext PO/POT | Widest existing-tool reach for volunteer translators (Poedit, Weblate, Pontoon, Crowdin, Transifex all native). |
| Client runtime | Hand-rolled Svelte 5 runes resolver on compiled JSON | Matches the existing `theme-store`/`units-store` pattern; zero new runtime deps; plain artifacts portable to the Android-served embedded UI; correctness-hard parts delegated to platform `Intl`. |
| Runtime locale switch | Reactive, no reload | `t()` reads the locale rune; templates re-render live. Mirrors how the theme store already behaves. |

The PO choice deliberately accepts a build step (PO -> runtime JSON) in
exchange for community-tool reach. The mitigations in §2 and §6 keep the
cost off the feature developer.

## Non-goals

- No non-English translations authored in this project (catalog is English-only at ship).
- No `Accept-Language` negotiation — the persisted preference is the sole source.
- No RTL/CJK layout work (no such languages targeted yet; not precluded later).
- No translation of `@chrissnell/chonky-ui` internal strings (separate repo and
  release workflow — documented limitation, see §3).
- No translation of log-stream lines, release notes, debug output, or generic
  internal 500 prose.

---

## §1 Architecture & core primitives

### Msgid strategy: English source strings as msgids

Code calls `t('Channels')`, not `t('channels.title')`. This is idiomatic
gettext and the single biggest factor in volunteer-translator usability — a
Poedit/Weblate translator sees `"Channels" -> ____`, not an opaque key.

Tradeoff, explicit: changing English wording produces a new msgid. Mitigation
is standard gettext `msgmerge` fuzzy-matching (prior translation pre-filled,
flagged "needs review"), plus `msgctxt` disambiguation only where identical
English carries different meaning (e.g. "Open" verb vs. adjective).

### Module layout (`web/src/lib/i18n/`)

| Path | Role | Committed? |
|---|---|---|
| `messages.pot` | Extracted template; translation source of truth | Yes |
| `locales/<lang>.po` | One per language; English needs none (msgid *is* English) | Yes |
| `compiled/<lang>.json` | Build artifact from `.po` | No (gitignored) |
| `i18n-store.svelte.js` | Runes store; localStorage `lang` mirror + server sync + boot-applied | Yes |
| `t.js` | Resolver: `t()`, `tn()`, number/date helpers | Yes |
| `extract.mjs` | Svelte-aware extractor -> `messages.pot` + `msgmerge` | Yes |
| `compile.mjs` | `.po` -> `compiled/<lang>.json` (Vite prebuild) | Yes |

### Store (`i18n-store.svelte.js`)

Exact structural mirror of `web/src/lib/settings/theme-store.svelte.js`:

- localStorage key `lang`, normalized/validated on read.
- `GET/PUT /api/preferences/language` for server persistence; offline/401
  falls back to the localStorage mirror.
- Exposes reactive `locale` getter and `setLocale(next)`.
- Active locale's `compiled/<lang>.json` lazy-loaded via `import.meta.glob`.
  English path needs no fetch — the resolver falls back to the msgid.

### Resolver (`t.js`)

- `t(msgid, params?)` — lookup in active catalog -> else English msgid ->
  the resolver never returns blank and never throws.
- `tn(singular, plural, count, params?)` — plural form selected by the active
  PO's `Plural-Forms` header, compiled to a selector at build time. Each
  language's rule comes from the translator's tool, which sets that header.
- Named `{name}` placeholders — order-independent, tool-checkable.
- Number/date helpers wrap `Intl.NumberFormat` / `Intl.DateTimeFormat` bound
  to the active locale. Gettext does not cover these; delegating to `Intl` is
  correct and keeps CLDR out of our code.
- Reactivity: `t()` reads `i18nState.locale` (a `$state` getter), so any
  template or `$derived` using it re-runs on locale change. Imperatively
  produced strings (an open toast, a thrown error) reflect the locale at emit
  time — correct behavior, not a bug.

### Boot script (`web/index.html`)

Extend the existing inline theme-applying script to also read
`localStorage.lang` and set `<html lang>` before mount — avoids a flash of
the wrong language and keeps the `lang` attribute correct for a11y.

### Dependency posture

Runtime stays zero-new-dependency. Build tooling does **not** hand-roll a PO
parser (PO has real edge cases: multiline, escaping, fuzzy/obsolete entries,
contexts) — it uses `gettext-parser` as a devDependency. The extractor walks
`.svelte`/`.js` with `svelte/compiler` (already a dependency).

### Invariants

- English is always the zero-cost fallback (it is the msgid).
- A missing or failed lookup degrades to English, then to the literal — never
  blank, never a crash.
- The persisted preference is the sole language source; `Accept-Language`
  is unused.
- Only string-literal msgids (lint-enforced, §6) — extraction is then complete
  and deterministic.

---

## §2 Developer & translator loop

This loop is the test of "i18n must not be an impediment to new development."

**Developer adds a string:** writes `t('Save channel')` inline. No key file,
no parallel English JSON — the English *is* the call argument. Only rule:
literal first argument (lint-enforced, §6).

**Before release / on demand:** `make i18n-extract` runs the Svelte-aware
extractor, regenerating `messages.pot` and `msgmerge`-ing every
`locales/*.po` (new strings appear untranslated; reworded strings appear
fuzzy with the prior translation pre-filled). CI fails if `messages.pot` is
stale vs. source (`make i18n-extract && git diff --exit-code`) — a mechanical
gate, the only extraction bookkeeping a developer ever feels.

**Build:** a Vite prebuild step compiles `locales/*.po` ->
`compiled/*.json`. Missing entries fall through to English at runtime;
nothing ships blank.

**Translator:** opens `messages.pot` or `<lang>.po` in Poedit, Weblate,
Pontoon, Crowdin, Transifex — natively, no conversion. Sees English source
strings with placeholder hints, submits a `.po`. Drop it in `locales/`,
rebuild, done.

Net cost: one CI freshness gate plus a build step. Net protection: a feature
developer never does i18n bookkeeping beyond wrapping a literal.

---

## §3 Extraction & rollout across all ~86 files

Batched by area; each batch independently reviewable and testable; maps 1:1
to implementation-plan phases.

1. **Resolver + store + build wiring** — `t.js`, `tn`, `i18n-store`,
   `extract.mjs`, `compile.mjs`, lint rule (§6), boot script,
   `/api/preferences/language`. No UI text changes yet.
2. **Shared chrome** — `Sidebar`, `PageHeader`, `ConfirmDialog`, `Modal`,
   common components, `Login`. Highest visibility, smallest count, proves the
   pattern end to end.
3. **Routes** — the 25 route pages, grouped (settings cluster, ops cluster,
   map/messages/terminal cluster). Largest volume.
4. **`lib/*.js` user-facing strings** — toast text, client-side validation.
5. **Backend error-key conversion** — see §4.

**Wrapping rules (handed to every batch):**

- Text nodes -> `{t('...')}`
- User-facing attributes (`placeholder`, `title`, `aria-label`, `alt`,
  `label`) -> `attr={t('...')}`
- Counts -> `tn(singular, plural, n)`
- Interpolation -> named `{name}` params

**Do-not-translate list (authoritative; prevents over-extraction):**
callsigns, frequencies, APRS/AX.25/KISS protocol tokens, log-stream lines,
unit symbols (handled by `units-store` + `Intl`), proper nouns (Graywolf,
APRS, Winlink, Igate identifiers), code identifiers.

**Pseudo-locale for completeness QA:** a build-generated `pseudo` locale that
accents and ~30% length-expands every msgid. Plain-ASCII text on screen under
pseudo = a missed extraction; clipped layout = an overflow bug. This is how
"full extraction" is *verified* cheaply, with no real translation.

**Documented limitation:** `@chrissnell/chonky-ui` renders some of its own
strings (Toaster internals, FormField validation). It lives in a separate
repo with its own release workflow — **out of scope here**, noted rather than
silently ignored.

---

## §4 Backend error-key contract

**Response shape:** existing error JSON gains `error_key` (stable dotted
string, e.g. `channel.name_taken`) and optional `params` map. Existing
`error` prose stays as ultimate fallback — nothing breaks if a key is missed
or a client is stale.

**Scope (only what reaches the screen):** form/validation errors and
operational failures surfaced via toast or inline UI. **Not** in scope:
generic internal 500s (one shared `internal.error` key suffices), log-stream
lines, debug output.

**Mechanics:** error keys are constants in one Go package so the contract is
enumerable and auditable. The client maps `error_key` -> an English source
string carried in the *same* PO under `msgctxt "error"`, rendered via `t()`
with params. Missing key -> server prose. A Go test enumerates the key
constants; a catalog test asserts each has an entry — drift fails CI.

**Audit method:** grep user-facing handler error responses; convert the
displayed ones. Bounded list, not the whole error tree.

---

## §5 Testing

- **Resolver units:** fallback chain (translated -> English msgid -> never
  blank/crash); `{name}` interpolation; missing-param behavior; plural
  selection driven by `Plural-Forms` (a multi-form rule + a no-plural
  language); `Intl` number/date per locale.
- **CI gates:** `messages.pot` freshness (`make i18n-extract && git diff
  --exit-code`); every `.po` parses; placeholder sets match between msgid and
  msgstr (catches translator drift); every Go `error_key` constant has a
  catalog entry.
- **Store:** mirrors existing theme-store tests — localStorage read, server
  sync, offline/401 fallback.
- **Pseudo-locale pass:** build pseudo, Playwright-screenshot key pages,
  eyeball for un-accented (missed) text and overflow.
- **Backend:** handler tests assert converted sites include `error_key`.
- **Regression:** full existing suite green; new literal-only-msgid lint rule
  has its own tests.

---

## §6 PR linter — catch unwrapped user-facing strings

**Feasible because it shares the extractor's AST walk.** The linter is the
inverse pass over the same `svelte/compiler` AST: flag strings that should
have been `t()` but were not. No new parser, no new toolchain.

**Flags (deliberately narrow, to keep noise low):**

- Markup **text nodes** containing letters not inside `{t(...)}`/`{tn(...)}`.
- String literals on a fixed allowlist of **user-facing attributes**
  (`placeholder`, `title`, `aria-label`, `alt`, `label`) that are not `t()`
  calls.
- Everything else ignored *by construction* — `class`, `id`, `href`, `src`,
  `role`, `type`, `data-*`, styles, event handlers, non-literal expressions.

**Noise controls:** the §3 do-not-translate allowlist feeds it; an inline
`i18n-ignore` escape-hatch comment covers the rare legitimate raw string.

**Diff-scoped during rollout, whole-tree after.** Until the §3 batches land
the tree is full of not-yet-wrapped strings, so the PR check parses the
`git diff` and fails only on **newly added** offending lines. Once §3
completes and the tree is clean, the same linter flips to whole-tree
enforcement (one config change).

**Where it runs:** a `make i18n-lint` target + a GitHub Actions PR check.

**Honest limits:** catches the common case (a developer typing visible text),
not every case (a string built dynamically or passed through a variable). The
pseudo-locale pass (§3) is the structural backstop. Together they are a
strong net; neither alone is complete.

---

## Wiki maintenance follow-up

New cross-system surfaces this introduces (must land in `docs/wiki/` in the
implementing change, per `CLAUDE.md`):

- `code-map.md`: `web/src/lib/i18n/` module, `make i18n-*` targets,
  `/api/preferences/language` handler.
- `build-pipelines.md`: PO extract/compile build stage; pseudo-locale build.
- `invariants.md`: English-is-fallback; literal-only msgids; persisted-pref
  is sole language source; `error_key` contract.
