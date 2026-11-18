// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Console checks for CI: every script parses, and the English and Thai string
// files have the same keys with the same {placeholders}.
//
//   node tools/check-console.mjs
import { execFileSync } from 'node:child_process';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

let bad = 0;
const js = (d) => readdirSync(d, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? js(join(d, e.name)) : e.name.endsWith('.js') ? [join(d, e.name)] : []));
for (const f of [...js('assets/console/js'), 'assets/console/sw.js']) {
  try { execFileSync(process.execPath, ['--check', f], { stdio: 'pipe' }); } catch (e) { console.log(`${f}: ${String(e.stderr).split('\n').slice(0, 4).join(' ')}`); bad++; }
}

const flat = (o, p = '', out = {}) => {
  for (const [k, v] of Object.entries(o)) {
    if (v && typeof v === 'object' && !Array.isArray(v)) flat(v, p + k + '.', out); else out[p + k] = v;
  }
  return out;
};
const load = (l) => flat(JSON.parse(readFileSync(`assets/console/i18n/${l}.json`, 'utf8')));
const en = load('en'), th = load('th');
const holes = (s) => [...String(s).matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(',');
for (const k of Object.keys(en)) {
  if (!(k in th)) { console.log(`th.json is missing ${k}`); bad++; continue; }
  if (holes(en[k]) !== holes(th[k])) { console.log(`${k}: placeholders differ (en {${holes(en[k])}} th {${holes(th[k])}})`); bad++; }
}
for (const k of Object.keys(th)) if (!(k in en)) { console.log(`en.json is missing ${k}`); bad++; }

if (bad) { console.log(`\nconsole check: ${bad} problem(s)`); process.exit(1); }
console.log(`console check: clean (${Object.keys(en).length} strings in each language)`);
