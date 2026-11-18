// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { t, locale } from './i18n.js';
import { api } from './data.js';

// h('div.cls#id', { attrs }, ...children)
export function h(sel, attrs, ...kids) {
  if (attrs === null || typeof attrs !== 'object' || attrs instanceof Node || Array.isArray(attrs)) {
    kids.unshift(attrs); attrs = {};
  }
  const [, tag = 'div', rest = ''] = sel.match(/^([a-z0-9-]*)(.*)$/i);
  const el = document.createElement(tag || 'div');
  rest.replace(/([.#])([\w-]+)/g, (_, k, v) => { if (k === '.') el.classList.add(v); else el.id = v; });
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') el.className += (el.className ? ' ' : '') + v;
    else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
    else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2).toLowerCase(), v);
    else if (k === 'html') el.innerHTML = v;
    else if (v === true) el.setAttribute(k, '');
    else el.setAttribute(k, v);
  }
  append(el, kids);
  return el;
}
function append(el, kids) {
  for (const k of kids.flat(Infinity)) {
    if (k === null || k === undefined || k === false) continue;
    el.append(k instanceof Node ? k : document.createTextNode(String(k)));
  }
}

const SVGNS = 'http://www.w3.org/2000/svg';
export function s(tag, attrs = {}, ...kids) {
  const el = document.createElementNS(SVGNS, tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === null || v === false) continue;
    if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2).toLowerCase(), v);
    else el.setAttribute(k, v);
  }
  for (const k of kids.flat(Infinity)) {
    if (k === null || k === undefined || k === false) continue;
    el.append(k instanceof Node ? k : document.createTextNode(String(k)));
  }
  return el;
}

export function icon(name, cls = 'i') {
  const svg = s('svg', { class: cls, 'aria-hidden': 'true', focusable: 'false' });
  svg.append(s('use', { href: `#i-${name}` }));
  return svg;
}

// ---- Status vocabulary (signal aspects) ----
export const SESSION_LAMP = { running: 'clear', waiting: 'danger', stale: 'caution', queued: 'idle', done: 'idle', stopped: 'idle' };
export function lamp(kind, opts = {}) {
  return h('span.lamp', { class: kind + (opts.blink ? ' blink' : ''), 'aria-hidden': 'true' });
}
export function sessionStatus(sess) {
  const k = SESSION_LAMP[sess.status] || 'idle';
  return h('span.status', { class: k }, lamp(k, { blink: sess.status === 'waiting' }), t(`status.${sess.status}`));
}
// Queue marker: a soft dot in the state colour (the old signal post is retired).
export function signalGlyph(kind, blink = false) {
  return h('span.sig-dot', { class: kind + (blink ? ' blink' : ''), 'aria-hidden': 'true' });
}

export function rel(ts) {
  const diff = (ts - Date.now()) / 1000;
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(locale(), { numeric: 'auto', style: 'short' });
  if (abs < 45) return t('time.now');
  if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour');
  return rtf.format(Math.round(diff / 86400), 'day');
}
export const clock = (ts) => new Intl.DateTimeFormat(locale(), { hour: '2-digit', minute: '2-digit', hour12: false }).format(ts);
export const dateTime = (ts) => new Intl.DateTimeFormat(locale(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hour12: false }).format(ts);
export const fmtNum = (n) => new Intl.NumberFormat(locale()).format(n);
export function fmtTokens(n) {
  if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 0 : 1) + 'M';
  if (n >= 1e4) return Math.round(n / 1e3) + 'k';
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'k';
  return String(n);
}
// A price we do not know is "unknown", never $0.
export const fmtCost = (n) => (n == null || Number.isNaN(n) ? t('common.costUnknown') : '$' + (n < 0.1 && n > 0 ? n.toFixed(3) : n.toFixed(2)));

export function person(id) {
  if (id === 'agent' || id === 'auto' || id === 'hpk') return { id, name: t(`actor.${id}`), initials: id === 'auto' ? 'A' : id === 'hpk' ? 'H' : 'AI', tone: 'agent' };
  return api.person(id) || { id, name: id, initials: '?', tone: 'agent' };
}
export function avatar(id, size) {
  const p = person(id);
  return h('span.avatar', { class: p.tone || '', title: p.name, style: size ? { width: size + 'px', height: size + 'px' } : undefined }, p.initials);
}

// ---- Toasts ----
export function toast(msg, action, kind = 'ok') {
  const box = document.getElementById('toasts');
  const life = action ? 7000 : 3600;
  const el = h('div.toast', { style: { '--life': life + 'ms' } }, icon(kind === 'deny' ? 'x' : 'check'), h('span', msg),
    action ? h('button.btn.sm.ghost', { type: 'button', onclick: () => { action.run(); el.remove(); } }, action.label) : null);
  box.append(el);
  setTimeout(() => el.remove(), life);
}

export const motionOn = () => document.documentElement.getAttribute('data-motion') !== 'off' && !matchMedia('(prefers-reduced-motion: reduce)').matches;

// Animate a queue row out (green for approve, red for deny), then continue.
export function leave(el, ok, then) {
  if (!el || !motionOn()) { then(); return; }
  el.classList.add('leaving', ok ? 'ok' : 'no');
  setTimeout(then, 420);
}

// Count a number up from 0 when it first appears.
export function countUp(el, to, fmt = String, ms = 900) {
  if (!motionOn()) { el.textContent = fmt(to); return el; }
  const t0 = performance.now();
  el.classList.add('counting');
  const step = (now) => {
    const k = Math.min(1, (now - t0) / ms);
    const eased = 1 - Math.pow(1 - k, 3);
    el.textContent = fmt(to * eased);
    if (k < 1) setTimeout(() => step(performance.now()), 16);
    else el.classList.remove('counting');
  };
  step(t0);
  return el;
}

// ---- Drawer ----
let lastFocus = null;
export function openDrawer(title, body, foot) {
  const d = document.getElementById('drawer');
  lastFocus = document.activeElement;
  d.replaceChildren(
    h('div.drawer-head', h('h2#drawerTitle', title), h('button.icon-btn', { type: 'button', 'aria-label': t('common.close'), onclick: closeDrawer }, icon('close'))),
    h('div.drawer-body', body),
    ...(foot ? [h('div.drawer-foot', foot)] : []),
  );
  d.setAttribute('aria-hidden', 'false');
  d.inert = false;
  document.getElementById('scrim').hidden = false;
  setTimeout(() => d.querySelector('button, a, input, select, textarea')?.focus(), 30);
}
export function closeDrawer() {
  const d = document.getElementById('drawer');
  if (d.getAttribute('aria-hidden') === 'true') return;
  d.setAttribute('aria-hidden', 'true');
  d.inert = true;
  if (!document.getElementById('rail').classList.contains('open')) document.getElementById('scrim').hidden = true;
  lastFocus?.focus?.();
}

// ---- Popover menu: menu(anchorEl, [{ label, icon, onSelect, danger, sep, hint, checked }]) ----
let openMenu = null;
export function closeMenu() {
  if (!openMenu) return;
  const { el, anchor, onDoc } = openMenu;
  el.remove(); anchor.setAttribute('aria-expanded', 'false');
  document.removeEventListener('pointerdown', onDoc, true);
  openMenu = null;
  anchor.focus();
}
export function menu(anchor, items, { align = 'left', up = false } = {}) {
  if (openMenu?.anchor === anchor) { closeMenu(); return; }
  closeMenu();
  const el = h('div.pop-menu', { role: 'menu' });
  const buttons = [];
  items.filter(Boolean).forEach((it) => {
    if (it.sep) { el.append(h('div.pm-sep', { role: 'separator' })); return; }
    if (it.heading) { el.append(h('div.pm-heading', it.heading)); return; }
    const b = h('button', { type: 'button', role: it.checked !== undefined ? 'menuitemradio' : 'menuitem', class: it.danger ? 'danger' : '', 'aria-checked': it.checked !== undefined ? String(!!it.checked) : null, disabled: it.disabled || null,
      onclick: () => { closeMenu(); it.onSelect && it.onSelect(); } },
    it.icon ? icon(it.icon) : h('span.pm-ic'), h('span.pm-label', it.label), it.hint ? h('span.pm-hint', it.hint) : it.checked ? icon('check') : null);
    buttons.push(b); el.append(b);
  });
  document.body.append(el);
  const r = anchor.getBoundingClientRect();
  const w = el.offsetWidth; const hgt = el.offsetHeight;
  let left = align === 'right' ? r.right - w : r.left;
  left = Math.max(8, Math.min(left, window.innerWidth - w - 8));
  let top = up ? r.top - hgt - 6 : r.bottom + 6;
  if (top + hgt > window.innerHeight - 8) top = Math.max(8, r.top - hgt - 6);
  Object.assign(el.style, { left: left + 'px', top: top + 'px' });
  anchor.setAttribute('aria-expanded', 'true');
  el.addEventListener('keydown', (e) => {
    const i = buttons.indexOf(document.activeElement);
    if (e.key === 'ArrowDown') { e.preventDefault(); buttons[(i + 1) % buttons.length].focus(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); buttons[(i - 1 + buttons.length) % buttons.length].focus(); }
    else if (e.key === 'Escape' || e.key === 'Tab') { e.preventDefault(); closeMenu(); }
  });
  const onDoc = (e) => { if (!el.contains(e.target) && !anchor.contains(e.target)) closeMenu(); };
  document.addEventListener('pointerdown', onDoc, true);
  openMenu = { el, anchor, onDoc };
  buttons.find((b) => !b.disabled)?.focus();
}

// Confirm a red-line action in the drawer. Resolves true/false.
export function confirmRedLine({ title, body, redLine, confirmLabel }) {
  return new Promise((resolve) => {
    let done = false;
    const finish = (v) => { if (done) return; done = true; closeDrawer(); resolve(v); };
    openDrawer(title, h('div.stack', { style: { padding: '16px' } },
      h('div.callout.danger', icon('warn'), h('div', h('b', t('redlines.' + redLine + '.short')), h('p', body)))),
    [h('button.btn.ghost', { type: 'button', onclick: () => finish(false) }, t('common.cancel')),
      h('button.btn.danger', { type: 'button', onclick: () => finish(true) }, confirmLabel)]);
    const d = document.getElementById('drawer');
    const obs = new MutationObserver(() => { if (d.getAttribute('aria-hidden') === 'true') { obs.disconnect(); finish(false); } });
    obs.observe(d, { attributes: true, attributeFilter: ['aria-hidden'] });
  });
}

export const pageHead = (title, lede, actions) =>
  h('div.page-head', h('div', h('h1', title), lede ? h('p.lede', lede) : null), actions ? h('div.page-actions', actions) : null);

export const panel = (title, sub, body, opts = {}) =>
  h('section.panel', { 'aria-label': typeof title === 'string' ? title : undefined },
    title ? h('div.panel-head', h('h2', title), sub ? (sub instanceof Node ? sub : h('span.sub', sub)) : null) : null,
    h('div.panel-body', { class: opts.flush ? 'flush' : '' }, body));

export function emptyState(text, action) {
  const art = s('svg', { class: 'track-empty', viewBox: '0 0 160 40', 'aria-hidden': 'true' },
    s('path', { d: 'M4 30h152' }), s('path', { d: 'M40 26v8M80 26v8M120 26v8' }), s('circle', { cx: 140, cy: 12, r: 4 }));
  return h('div.empty', art, h('p', text), action || null);
}

export function privacyTag(mode) {
  const kind = mode === 'shared' ? 'route' : mode === 'private' ? 'caution' : 'danger';
  const ic = mode === 'shared' ? 'globe' : mode === 'private' ? 'lock' : 'shield';
  return h('span.tag', { class: kind, title: t(`privacy.${mode}.desc`) }, icon(ic), t(`privacy.${mode}.name`));
}
