// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Leak check: fails when tracked files carry things that must never ship —
// personal paths, real email addresses, keys and tokens. Sample data must be
// fictional (example.com, example.invalid).
//
// Private words (a real project or company name you never want in the repo) go
// in .leakcheck-local, one per line. That file is git-ignored, so the words
// themselves never ship either.
//
//   node tools/leakcheck.mjs            # files git tracks (or all files outside git)
import { execSync } from 'node:child_process';
import { readFileSync, existsSync, statSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

const skipDirs = new Set(['.git', 'dist', 'node_modules', '.claude']);
const skipFiles = new Set(['tools/leakcheck.mjs', 'go.sum', '.leakcheck-local']);

function files() {
  try {
    return execSync('git ls-files -z --cached --others --exclude-standard', { encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] })
      .split('\0').filter(Boolean);
  } catch {
    const out = [];
    const walk = (d) => {
      for (const e of readdirSync(d, { withFileTypes: true })) {
        if (skipDirs.has(e.name)) continue;
        const p = d === '.' ? e.name : join(d, e.name).replaceAll('\\', '/');
        if (e.isDirectory()) walk(p); else out.push(p);
      }
    };
    walk('.');
    return out;
  }
}

const rules = [
  ['personal Windows path', /[A-Za-z]:[\\/]+Users[\\/]+(?!Public\b|<|%|\{|\$|\.\.\.|you\b|name\b|me\b)[A-Za-z0-9._-]+/],
  ['personal home path', /\/(?:home|Users)\/(?!runner\b|<|\{|\$|you\b|name\b|me\b|\.\.\.)[a-z][a-z0-9._-]+\//],
  ['real email address', /\b[A-Za-z0-9._%+-]+@(?!example\.(?:com|org|net|invalid|test)\b|[A-Za-z0-9-]+\.test\b|corp\b|t\b|anthropic\.com\b|users\.noreply\.github\.com\b)[A-Za-z0-9-]+\.[A-Za-z.]{2,}\b/],
  ['private key', /-----BEGIN [A-Z ]*PRIVATE KEY-----/],
  ['hpk access token', /\bhpk_[0-9a-f]{40,}\b/],
  ['API key', /\b(?:sk-ant-[A-Za-z0-9_-]{20,}|sk-[A-Za-z0-9]{32,}|ghp_[A-Za-z0-9]{30,}|AKIA[0-9A-Z]{16}|xox[bp]-[A-Za-z0-9-]{20,})\b/],
];

// Test fixtures prove the guards refuse secrets and odd paths; they may carry fake ones.
const fixture = (f) => /_test\.go$/.test(f);

let words = [];
if (existsSync('.leakcheck-local')) {
  words = readFileSync('.leakcheck-local', 'utf8').split(/\r?\n/).map((w) => w.trim()).filter((w) => w && !w.startsWith('#'));
}

let bad = 0;
for (const f of files()) {
  if (skipFiles.has(f) || f.split('/').some((p) => skipDirs.has(p))) continue;
  let text;
  try {
    if (statSync(f).size > 4 << 20) continue;
    text = readFileSync(f, 'utf8');
  } catch { continue; }
  if (text.includes('\0')) continue; // binary
  text.split(/\r?\n/).forEach((line, i) => {
    for (const [what, re] of rules) {
      if (fixture(f) && what !== 'real email address') continue;
      const m = line.match(re);
      if (m) { console.log(`${f}:${i + 1}: ${what}: ${m[0]}`); bad++; }
    }
    for (const w of words) {
      if (line.toLowerCase().includes(w.toLowerCase())) { console.log(`${f}:${i + 1}: private word from .leakcheck-local`); bad++; }
    }
  });
}
if (bad) {
  console.log(`\nleak check: ${bad} finding(s). Use fictional data (example.com, <hermes home>).`);
  process.exit(1);
}
console.log(`leak check: clean (${words.length} private words checked)`);
