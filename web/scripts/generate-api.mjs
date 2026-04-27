// Regenerates src/api/generated/api.d.ts from the committed Swagger 2.0
// spec. openapi-typescript v7 only accepts OpenAPI 3.x, so we convert the
// spec in memory with swagger2openapi first and hand the result to the
// generator. Nothing is written to disk except the final .d.ts file.
//
// Driven by `npm run api:generate` and the `make api-client` / `make
// api-client-check` targets. Do not invoke by hand — go through those.
//
// Strict mode: pass `--strict` (or set GRAYWOLF_STRICT=1) and the script
// exits non-zero if swagger2openapi produced any conversion warnings. CI
// runs the generator via `make api-client-check` with `--strict` so a new
// warning fails the build; local dev runs warn-but-don't-fail.

import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import openapiTS, { astToString } from 'openapi-typescript';
import converter from 'swagger2openapi';

const HERE = dirname(fileURLToPath(import.meta.url));
const SPEC_PATH = resolve(HERE, '..', '..', 'pkg', 'webapi', 'docs', 'gen', 'swagger.json');
// Default output path; overridable via GRAYWOLF_API_OUT for the drift
// check in `make api-client-check` (emits to a scratch file).
const OUT_PATH = process.env.GRAYWOLF_API_OUT
  ? resolve(process.env.GRAYWOLF_API_OUT)
  : resolve(HERE, '..', 'src', 'api', 'generated', 'api.d.ts');

const STRICT =
  process.env.GRAYWOLF_STRICT === '1' || process.argv.includes('--strict');

// swagger2openapi's convertObj with warnOnly:true embeds warnings as
// `x-s2o-warning` (and lint results as `x-s2o-lint`) extension properties
// on the nodes of the converted spec — there's no top-level warnings
// array. See swagger2openapi README §Node.js API and the throwOrWarn
// helper in node_modules/swagger2openapi/index.js. We walk the spec to
// surface them.
const WARN_KEYS = new Set(['x-s2o-warning', 'x-s2o-lint']);

function collectWarnings(spec) {
  const found = [];
  const seen = new WeakSet();
  function walk(node, path) {
    if (!node || typeof node !== 'object' || seen.has(node)) return;
    seen.add(node);
    if (Array.isArray(node)) {
      node.forEach((v, i) => walk(v, `${path}/${i}`));
      return;
    }
    for (const key of Object.keys(node)) {
      if (WARN_KEYS.has(key)) {
        const val = node[key];
        const msgs = Array.isArray(val) ? val : [val];
        for (const m of msgs) {
          found.push({ path: path || '/', kind: key, message: String(m) });
        }
      } else {
        walk(node[key], `${path}/${key}`);
      }
    }
  }
  walk(spec, '');
  return found;
}

async function main() {
  const specRaw = await readFile(SPEC_PATH, 'utf8');
  const spec2 = JSON.parse(specRaw);

  // swagger2openapi mutates a shallow copy; options.patch fixes common
  // minor schema issues produced by generators, which matches what swag
  // v1.16.x emits. warnOnly keeps non-fatal conversion problems from
  // aborting the build — we surface them ourselves below.
  const { openapi } = await converter.convertObj(spec2, {
    patch: true,
    warnOnly: true,
  });

  const warnings = collectWarnings(openapi);
  if (warnings.length > 0) {
    for (const w of warnings) {
      console.warn(`[swagger2openapi] ${w.kind} at ${w.path}: ${w.message}`);
    }
    if (STRICT) {
      console.error(
        `[swagger2openapi] strict mode: aborting on ${warnings.length} warning(s).`,
      );
      process.exit(2);
    }
  }

  const ast = await openapiTS(openapi);
  const body = astToString(ast);

  await mkdir(dirname(OUT_PATH), { recursive: true });
  const HEADER = `/**
 * GENERATED FILE — DO NOT EDIT.
 *
 * Regenerate with \`npm run api:generate\` (or \`make api-client\` from
 * the repo root). Source of truth: pkg/webapi/docs/gen/swagger.json.
 */

`;
  await writeFile(OUT_PATH, HEADER + body, 'utf8');
  process.stdout.write(`wrote ${OUT_PATH}\n`);
}

main().catch((err) => {
  process.stderr.write(`generate-api: ${err.stack || err.message}\n`);
  process.exit(1);
});
