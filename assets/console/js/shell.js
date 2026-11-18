// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Split a chained shell line into its commands and name the red line each one
// touches. Approving "build && rm -rf x && git push" must show all three.
// This is a display aid; the real gate runs in hpk's pre-tool hook.

// Split on && || ; | outside quotes.
export function splitCommand(line) {
  const parts = [];
  let cur = '';
  let q = null;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (q) { cur += c; if (c === q) q = null; continue; }
    if (c === '"' || c === "'") { q = c; cur += c; continue; }
    const two = line.slice(i, i + 2);
    if (two === '&&' || two === '||') { parts.push({ cmd: cur.trim(), op: two }); cur = ''; i++; continue; }
    if (c === ';' || c === '|') { parts.push({ cmd: cur.trim(), op: c }); cur = ''; continue; }
    cur += c;
  }
  parts.push({ cmd: cur.trim(), op: null });
  return parts.filter((p) => p.cmd);
}

const RULES = [
  ['secrets', /(^|\s)(cat|less|more|head|tail|type)\s+[^|;&]*\.env\b|printenv|\benv\b\s*$|\$\{?[A-Z_]*(KEY|TOKEN|SECRET|PASSWORD)/],
  ['push', /\bgit\s+(push|tag|merge)\b|\bgh\s+pr\s+merge\b/],
  ['delete', /\brm\s+(-[a-z]*[rf][a-z]*\s+)|\bgit\s+(worktree\s+remove|branch\s+-D|clean\s+-[a-z]*f|reset\s+--hard)\b|\bdrop\s+table\b/i],
  ['control', /(^|[\s/])(AGENTS|CLAUDE|SOUL)\.md\b|\.hermes\/|config\.ya?ml\b/],
  ['egress', /\b(curl|wget|scp|rsync|nc)\b[^|;&]*(https?:\/\/|@|:)/],
];

export function riskOf(cmd) {
  for (const [key, re] of RULES) if (re.test(cmd)) return key;
  return null;
}

export const commandRisks = (line) => splitCommand(line).map((p) => ({ ...p, risk: riskOf(p.cmd) }));
