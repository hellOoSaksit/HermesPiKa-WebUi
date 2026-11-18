// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Pure rules behind desktop notices (no DOM), so tools/pwa.test.mjs can check them.

export const KINDS = ['approval', 'redline', 'chat', 'browser'];
export const DEFAULTS = { on: false, away: true, kinds: { approval: true, redline: true, chat: true, browser: true }, quiet: { on: false, from: '22:00', to: '07:00' } };

// Settings live per browser (notification permission is per browser too).
export function parse(raw) {
  let v = {};
  try { v = JSON.parse(raw || '{}') || {}; } catch { v = {}; }
  const hhmm = (s, d) => (typeof s === 'string' && /^([01]\d|2[0-3]):[0-5]\d$/.test(s) ? s : d);
  return {
    on: v.on === true,
    away: v.away !== false,
    kinds: Object.fromEntries(KINDS.map((k) => [k, !(v.kinds && v.kinds[k] === false)])),
    quiet: { on: !!(v.quiet && v.quiet.on), from: hhmm(v.quiet && v.quiet.from, DEFAULTS.quiet.from), to: hhmm(v.quiet && v.quiet.to, DEFAULTS.quiet.to) },
  };
}

const mins = (s) => { const [h, m] = s.split(':').map(Number); return h * 60 + m; };

// inQuiet: is the local time `now` inside quiet hours? from == to means never;
// from > to wraps past midnight (22:00–07:00).
export function inQuiet(quiet, now = new Date()) {
  if (!quiet || !quiet.on) return false;
  const a = mins(quiet.from), b = mins(quiet.to), n = now.getHours() * 60 + now.getMinutes();
  if (a === b) return false;
  return a < b ? n >= a && n < b : n >= a || n < b;
}

// classify an approval card from the snapshot: a red line, a plain approval, or
// nothing worth a desktop notice (wiki drafts and stale claims wait quietly).
export function classify(a) {
  if (a.kind === 'permission') return a.redLine ? 'redline' : 'approval';
  if (a.kind === 'question' || a.kind === 'duplicate') return 'approval';
  return '';
}

// A chat finished a turn when its log was streaming and no longer is.
export const streaming = (msgs) => Array.isArray(msgs) && msgs.some((m) => m && m.streaming);

// shouldShow: the person's settings and the moment decide.
export function shouldShow(cfg, kind, { visible = false, now = new Date() } = {}) {
  if (!cfg.on || !cfg.kinds[kind]) return false;
  if (cfg.away && visible) return false;
  return !inQuiet(cfg.quiet, now);
}
