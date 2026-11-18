// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// i18n: every visible string lives in i18n/<lang>.json.
// Add a language by dropping a new JSON file in i18n/ and listing it in LANGS.
export const LANGS = [
  { code: 'en', label: 'English' },
  { code: 'th', label: 'ไทย' },
];
const DEFAULT = 'en';
let dict = {};
let fallback = {};
let lang = DEFAULT;

const read = () => { try { return localStorage.getItem('hpk.lang'); } catch { return null; } };
const write = (v) => { try { localStorage.setItem('hpk.lang', v); } catch { /* storage unavailable */ } };

async function load(code) {
  const res = await fetch(`i18n/${code}.json`, { cache: 'no-cache' });
  if (!res.ok) throw new Error(`Missing language file: ${code}`);
  return res.json();
}

export async function initI18n() {
  fallback = await load(DEFAULT);
  const fromUrl = new URLSearchParams(location.search).get('lang');
  const saved = fromUrl || read();
  lang = LANGS.some((l) => l.code === saved) ? saved : DEFAULT;
  dict = lang === DEFAULT ? fallback : await load(lang).catch(() => fallback);
  document.documentElement.lang = lang;
}

export async function setLang(code) {
  if (!LANGS.some((l) => l.code === code)) return;
  dict = code === DEFAULT ? fallback : await load(code);
  lang = code;
  write(code);
  document.documentElement.lang = code;
}

export const getLang = () => lang;
export const locale = () => (lang === 'th' ? 'th-TH' : 'en-GB');

const lookup = (obj, key) => key.split('.').reduce((o, k) => (o && o[k] !== undefined ? o[k] : undefined), obj);

// t('a.b', { n: 3 }) → string with {n} replaced. Plurals: { one, other } objects.
export function t(key, params = {}) {
  let v = lookup(dict, key);
  if (v === undefined) v = lookup(fallback, key);
  if (v === undefined) return key;
  if (typeof v === 'object' && 'other' in v) {
    const n = Number(params.n ?? 0);
    v = n === 1 && v.one !== undefined ? v.one : v.other;
  }
  return String(v).replace(/\{(\w+)\}/g, (_, k) => (params[k] !== undefined ? params[k] : `{${k}}`));
}
