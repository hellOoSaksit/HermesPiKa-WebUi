// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Resolve the appearance (theme, accent, background, size, density, corners,
// font, motion) before first paint so there is no flash. A classic (blocking)
// script on purpose: it must run before the body renders. prefs.js owns the
// same keys for the Settings page; this file only applies them.
(function () {
  var d = document.documentElement;
  var ui = {};
  try { ui = JSON.parse(localStorage.getItem('hpk.ui') || '{}') || {}; } catch (e) {}
  try {
    if (!ui.theme) ui.theme = localStorage.getItem('hpk.theme');
    if (!ui.motion) ui.motion = localStorage.getItem('hpk.motion');
  } catch (e) {}
  var q = new URLSearchParams(location.search).get('theme');
  if (q) ui.theme = q;
  var t = ui.theme;
  if (t !== 'light' && t !== 'dark') {
    t = window.matchMedia && matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  }
  d.setAttribute('data-theme', t);
  d.setAttribute('data-motion', ui.motion === 'off' ? 'off' : 'on');
  var keys = ['accent', 'palette', 'size', 'density', 'radius', 'font'];
  for (var i = 0; i < keys.length; i++) if (ui[keys[i]]) d.setAttribute('data-' + keys[i], ui[keys[i]]);
})();
