// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Appearance preferences. Kept per browser (localStorage "hpk.ui"); boot.js
// applies them before first paint, this module changes them at run time.
// Each choice is an attribute on <html>; console.css maps it to tokens.

export const CHOICES = {
  theme: ['dark', 'light', 'auto'],
  accent: ['rose', 'violet', 'blue', 'teal', 'coral', 'mono'],
  palette: ['plum', 'graphite', 'ocean', 'forest'],
  size: ['sm', 'md', 'lg', 'xl'],
  density: ['compact', 'cozy', 'roomy'],
  radius: ['sharp', 'soft', 'round'],
  font: ['anuphan', 'system'],
  motion: ['on', 'off'],
};
export const DEFAULTS = { theme: 'auto', accent: 'rose', palette: 'plum', size: 'md', density: 'cozy', radius: 'soft', font: 'anuphan', motion: 'on' };

function load() {
  let ui = {};
  try { ui = JSON.parse(localStorage.getItem('hpk.ui') || '{}') || {}; } catch { /* storage unavailable */ }
  try {
    if (!ui.theme) ui.theme = localStorage.getItem('hpk.theme') || undefined;
    if (!ui.motion) ui.motion = localStorage.getItem('hpk.motion') || undefined;
  } catch { /* storage unavailable */ }
  const out = { ...DEFAULTS };
  for (const k of Object.keys(CHOICES)) if (CHOICES[k].includes(ui[k])) out[k] = ui[k];
  return out;
}
let prefs = load();
export const getPrefs = () => ({ ...prefs });
export const themeNow = () => (prefs.theme === 'auto' ? (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark') : prefs.theme);

const darkQuery = matchMedia('(prefers-color-scheme: light)');
function apply() {
  const d = document.documentElement;
  d.setAttribute('data-theme', prefs.theme === 'auto' ? (darkQuery.matches ? 'light' : 'dark') : prefs.theme);
  d.setAttribute('data-motion', prefs.motion);
  for (const k of ['accent', 'palette', 'size', 'density', 'radius', 'font']) {
    if (prefs[k] === DEFAULTS[k]) d.removeAttribute('data-' + k); else d.setAttribute('data-' + k, prefs[k]);
  }
}
darkQuery.addEventListener?.('change', () => { if (prefs.theme === 'auto') { apply(); listeners.forEach((f) => f(prefs)); } });

const listeners = new Set();
export const onPrefs = (f) => { listeners.add(f); return () => listeners.delete(f); };

export function setPref(key, value) {
  if (!CHOICES[key]?.includes(value)) return;
  prefs = { ...prefs, [key]: value };
  save();
}
export function resetPrefs() { prefs = { ...DEFAULTS }; save(); }
function save() {
  try {
    localStorage.setItem('hpk.ui', JSON.stringify(prefs));
    // older keys, still read by older builds
    localStorage.setItem('hpk.theme', prefs.theme === 'auto' ? '' : prefs.theme);
    localStorage.setItem('hpk.motion', prefs.motion);
  } catch { /* storage unavailable: the choice lasts for this page only */ }
  const d = document.documentElement;
  const swap = () => apply();
  // A short cross-fade between themes when motion is on.
  if (document.startViewTransition && prefs.motion === 'on' && !matchMedia('(prefers-reduced-motion: reduce)').matches) {
    // Quick clicks skip the previous fade; that is fine, so swallow the rejections.
    const vt = document.startViewTransition(swap);
    for (const p of [vt.ready, vt.finished, vt.updateCallbackDone]) p?.catch(() => {});
  }
  else swap();
  d.dispatchEvent(new CustomEvent('hpk:prefs'));
  listeners.forEach((f) => f(prefs));
}
apply();
