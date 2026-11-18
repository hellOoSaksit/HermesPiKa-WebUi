// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Console-wide control upgrades, applied to whatever the views render:
//  - every <select class="input"> gets a custom picker (the select stays, hidden,
//    as the source of truth, so views keep reading .value and "change" events);
//  - long helper text (panel subtitles, page ledes, field hints, switch
//    descriptions) becomes an (i) glyph with a tooltip; the text stays in the
//    page for screen readers;
//  - native title tooltips are drawn by the same tooltip.
import { h, s } from './ui.js';

// ---- Glyphs: show levels as pictures instead of words ----
const LEVELS = { effort: ['low', 'medium', 'high', 'max'], model: ['haiku', 'sonnet', 'opus'] };
export function levelBars(n, of = 4) {
  const svg = s('svg', { class: 'lvl', viewBox: `0 0 ${of * 5 - 1} 12`, 'aria-hidden': 'true' });
  for (let i = 0; i < of; i++) {
    const hgt = 4 + (8 * (i + 1)) / of;
    svg.append(s('rect', { x: i * 5, y: 12 - hgt, width: 3, height: hgt, rx: 1, class: i < n ? 'on' : '' }));
  }
  return svg;
}
export function tierDots(n, of = 3) {
  const svg = s('svg', { class: 'tier t' + n, viewBox: `0 0 ${of * 7 - 2} 6`, 'aria-hidden': 'true' });
  for (let i = 0; i < of; i++) svg.append(s('circle', { cx: 3 + i * 7, cy: 3, r: 2.6, class: i < n ? 'on' : '' }));
  return svg;
}
const GLYPH = {
  effort: (v) => levelBars(LEVELS.effort.indexOf(v) + 1, 4),
  model: (v) => tierDots(LEVELS.model.indexOf(v) + 1, 3),
};
const glyphFor = (sel, v) => (GLYPH[sel.dataset.glyph] ? GLYPH[sel.dataset.glyph](v) : null);
const caret = () => s('svg', { class: 'pick-caret', viewBox: '0 0 10 6', 'aria-hidden': 'true' }, s('path', { d: 'M1 1l4 4 4-4' }));
const tick = () => s('svg', { class: 'pick-tick', viewBox: '0 0 12 10', 'aria-hidden': 'true' }, s('path', { d: 'M1 5.5l3.5 3L11 1' }));

// ---- Picker ----
let open = null;
let uid = 0;
function face(sel, btn) {
  const o = sel.selectedOptions[0];
  btn.replaceChildren(...[glyphFor(sel, o?.value), h('span.pick-val', o ? o.textContent : ''), caret()].filter(Boolean));
  btn.disabled = sel.disabled;
}
function closePick(refocus = true) {
  if (!open) return;
  const { list, btn, off } = open;
  open = null;
  off();
  list.remove();
  btn.setAttribute('aria-expanded', 'false');
  if (refocus) btn.focus();
}
function openPick(sel, btn) {
  if (open?.btn === btn) { closePick(); return; }
  closePick(false);
  const opts = [...sel.options];
  const id = 'pk' + ++uid;
  const list = h('div.pick-list', { role: 'listbox', tabindex: '-1', id, 'aria-label': btn.getAttribute('aria-label') || btn.textContent });
  const items = opts.map((o, i) => h('div.pick-opt', {
    role: 'option', id: `${id}-${i}`, 'aria-selected': String(o.selected), 'aria-disabled': o.disabled ? 'true' : null,
    onpointerdown: (e) => e.preventDefault(), onclick: () => !o.disabled && choose(i),
  }, glyphFor(sel, o.value), h('span', o.textContent), o.selected ? tick() : null));
  list.append(...items);
  document.body.append(list);
  const r = btn.getBoundingClientRect();
  list.style.minWidth = Math.max(r.width, 140) + 'px';
  const w = list.offsetWidth; const hh = list.offsetHeight;
  let top = r.bottom + 4;
  if (top + hh > innerHeight - 8) top = Math.max(8, r.top - hh - 4);
  Object.assign(list.style, { left: Math.max(8, Math.min(r.left, innerWidth - w - 8)) + 'px', top: top + 'px' });
  let active = Math.max(0, sel.selectedIndex);
  const mark = (i) => {
    items[active]?.classList.remove('active');
    active = i; items[i].classList.add('active');
    list.setAttribute('aria-activedescendant', items[i].id);
    items[i].scrollIntoView({ block: 'nearest' });
  };
  const step = (d) => { let i = active; for (let k = 0; k < opts.length; k++) { i = (i + d + opts.length) % opts.length; if (!opts[i].disabled) return mark(i); } };
  function choose(i) {
    if (sel.selectedIndex !== i) {
      sel.selectedIndex = i;
      sel.dispatchEvent(new Event('input', { bubbles: true }));
      sel.dispatchEvent(new Event('change', { bubbles: true }));
    }
    face(sel, btn);
    closePick();
  }
  let typed = ''; let typedAt = 0;
  list.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); step(1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); step(-1); }
    else if (e.key === 'Home') { e.preventDefault(); mark(0); }
    else if (e.key === 'End') { e.preventDefault(); mark(opts.length - 1); }
    else if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); if (!opts[active].disabled) choose(active); }
    else if (e.key === 'Escape') { e.preventDefault(); closePick(); }
    else if (e.key === 'Tab') closePick(false);
    else if (e.key.length === 1) {
      typed = Date.now() - typedAt > 700 ? e.key.toLowerCase() : typed + e.key.toLowerCase(); typedAt = Date.now();
      const i = opts.findIndex((o) => !o.disabled && o.textContent.trim().toLowerCase().startsWith(typed));
      if (i >= 0) mark(i);
    }
  });
  const onDoc = (e) => { if (!list.contains(e.target) && !btn.contains(e.target)) closePick(false); };
  const onMove = (e) => { if (!list.contains(e.target)) closePick(false); };
  document.addEventListener('pointerdown', onDoc, true);
  addEventListener('resize', onMove); document.addEventListener('scroll', onMove, true);
  const off = () => { document.removeEventListener('pointerdown', onDoc, true); removeEventListener('resize', onMove); document.removeEventListener('scroll', onMove, true); };
  open = { list, btn, off };
  btn.setAttribute('aria-expanded', 'true');
  btn.setAttribute('aria-controls', id);
  mark(active);
  list.focus();
}
function upgrade(sel) {
  if (sel.dataset.pick || sel.multiple || sel.size > 1) return;
  sel.dataset.pick = '1';
  const btn = h('button.pick', { type: 'button', 'aria-haspopup': 'listbox', 'aria-expanded': 'false' });
  for (const c of sel.classList) if (c !== 'input') btn.classList.add(c);
  if (sel.style.maxWidth) btn.style.maxWidth = sel.style.maxWidth;
  if (sel.style.width) btn.style.width = sel.style.width;
  if (sel.hasAttribute('aria-label')) btn.setAttribute('aria-label', sel.getAttribute('aria-label'));
  if (sel.id) {
    btn.id = sel.id + '-pick';
    document.querySelectorAll(`label[for="${CSS.escape(sel.id)}"]`).forEach((l) => (l.htmlFor = btn.id));
  }
  sel.classList.add('pick-native');
  sel.tabIndex = -1;
  sel.setAttribute('aria-hidden', 'true');
  sel.after(btn);
  btn.addEventListener('click', () => openPick(sel, btn));
  btn.addEventListener('keydown', (e) => {
    if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(e.key)) { e.preventDefault(); openPick(sel, btn); }
  });
  sel.addEventListener('change', () => face(sel, btn));
  face(sel, btn);
}

// ---- Tooltip ----
let tipBox = null; let tipFor = null;
function showTip(el) {
  const text = el.dataset.tip;
  if (!text) return;
  tipBox ??= document.body.appendChild(h('div.tipbox', { role: 'tooltip', id: 'tipbox' }));
  tipFor = el;
  tipBox.textContent = text;
  tipBox.classList.add('show');
  const r = el.getBoundingClientRect();
  const w = tipBox.offsetWidth; const hh = tipBox.offsetHeight;
  let top = r.top - hh - 8; let below = false;
  if (top < 8) { top = r.bottom + 8; below = true; }
  tipBox.classList.toggle('below', below);
  Object.assign(tipBox.style, { left: Math.max(8, Math.min(r.left + r.width / 2 - w / 2, innerWidth - w - 8)) + 'px', top: top + 'px' });
}
function hideTip(el) { if (tipBox && (!el || el === tipFor)) { tipBox.classList.remove('show'); tipFor = null; } }
function tipTarget(e) {
  const el = e.target.closest?.('[data-tip], [title]');
  if (!el || el.closest('svg') && !el.matches('svg')) return null;
  if (el.hasAttribute('title')) {
    const v = el.getAttribute('title');
    el.removeAttribute('title');
    if (v) { el.dataset.tip = v; if (!el.textContent.trim() && !el.hasAttribute('aria-label')) el.setAttribute('aria-label', v); }
  }
  return el.dataset.tip ? el : null;
}
document.addEventListener('pointerover', (e) => { const el = tipTarget(e); if (el) showTip(el); else if (tipFor && !tipFor.contains(e.target)) hideTip(); });
document.addEventListener('focusin', (e) => {
  const el = tipTarget(e) || e.target.closest?.('.field')?.querySelector('.tip');
  if (el) showTip(el); else hideTip();
});
document.addEventListener('focusout', () => hideTip());
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') hideTip(); });
addEventListener('scroll', () => hideTip(), true);

// (i) glyph that carries text as a tooltip. The original text stays for screen readers.
const tipGlyph = (text) => h('span.tip', { 'data-tip': text, 'aria-hidden': 'true' },
  s('svg', { viewBox: '0 0 16 16' }, s('circle', { cx: 8, cy: 8, r: 6.5 }), s('path', { d: 'M8 7.2v4' }), s('circle', { cx: 8, cy: 4.9, r: 0.6, class: 'dot' })));
const textOnly = (el) => el.children.length === 0 && el.textContent.trim().length > 0;

function tipify(root) {
  root.querySelectorAll('.panel-head > .sub:not([data-tipped])').forEach((sub) => {
    sub.dataset.tipped = '1';
    if (!textOnly(sub)) return;
    sub.classList.add('sr-only');
    sub.before(tipGlyph(sub.textContent.trim()));
  });
  root.querySelectorAll('.page-head .lede:not([data-tipped])').forEach((p) => {
    p.dataset.tipped = '1';
    if (!textOnly(p)) return;
    p.classList.add('sr-only');
    p.parentElement.querySelector('h1')?.append(tipGlyph(p.textContent.trim()));
  });
  root.querySelectorAll('.field > .hint:not([data-tipped])').forEach((hint) => {
    hint.dataset.tipped = '1';
    if (!textOnly(hint) || hint.closest('[aria-live]') || hint.hasAttribute('aria-live')) return;
    const lab = hint.parentElement.querySelector(':scope > label, :scope > .label');
    if (!lab) return;
    hint.classList.add('sr-only');
    lab.append(tipGlyph(hint.textContent.trim()));
  });
  root.querySelectorAll('.switch:not([data-tipped])').forEach((sw) => {
    sw.dataset.tipped = '1';
    const muted = sw.querySelector('.muted');
    if (!muted || !textOnly(muted)) return;
    for (const n of [...muted.parentElement.childNodes]) if (n.nodeType === 3 && /^\s*[—–-]\s*$/.test(n.textContent)) n.remove();
    muted.classList.add('sr-only');
    muted.after(tipGlyph(muted.textContent.trim()));
  });
  // Long callout bodies fold to two lines; a click opens them.
  root.querySelectorAll('.callout:not([data-tipped])').forEach((c) => {
    c.dataset.tipped = '1';
    const p = c.querySelector('p');
    if (!p || p.textContent.length < 90) return;
    c.classList.add('fold');
    c.addEventListener('click', (e) => { if (!e.target.closest('a, button, input')) c.classList.toggle('open'); });
  });
}

function enhance() {
  document.querySelectorAll('select.input:not([data-pick])').forEach(upgrade);
  document.querySelectorAll('select.pick-native').forEach((sel) => { const b = sel.nextElementSibling; if (b?.classList.contains('pick') && b.disabled !== sel.disabled) b.disabled = sel.disabled; });
  tipify(document);
}
let queued = false;
new MutationObserver(() => {
  if (queued) return;
  queued = true;
  requestAnimationFrame(() => { queued = false; enhance(); });
}).observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ['disabled'] });
enhance();

// ---- Console key: a browser not opened from hpk may read but not change ----
// The server answers 401 + X-HPK-Need-Key; say how to get a key, once.
const nativeFetch = window.fetch.bind(window);
let keyBanner = null;
window.fetch = async (...args) => {
  const res = await nativeFetch(...args);
  if (res.status === 401 && res.headers.get('X-HPK-Need-Key') === '1' && !keyBanner) {
    const { t } = await import('./i18n.js');
    // In Docker there is no `hpk open`: the Start menu shortcut opens a fresh link.
    const boxed = await nativeFetch('/api/console-key').then((r) => r.json()).then((j) => !!j.container).catch(() => false);
    if (keyBanner) return res;
    keyBanner = h('div.key-banner', { role: 'alert' },
      s('svg', { viewBox: '0 0 20 20', 'aria-hidden': 'true' }, s('circle', { cx: 7, cy: 10, r: 3.5 }), s('path', { d: 'M10.5 10H18M15 10v3M17.5 10v2' })),
      boxed ? h('div', h('b', t('key.boxedTitle')), h('p', t('key.boxedBody')))
        : h('div', h('b', t('key.title')), h('p', t('key.body'), ' ', h('code', 'hpk open'))),
      h('button.icon-btn.sm', { type: 'button', 'aria-label': t('common.close'), onclick: () => { keyBanner.remove(); keyBanner = null; } }, '×'));
    document.body.append(keyBanner);
  }
  return res;
};
