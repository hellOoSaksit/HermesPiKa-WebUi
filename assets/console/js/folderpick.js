// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Folder picker: a drawer with breadcrumbs and the folders of one place at a
// time (GET /api/fs/dirs). Up/Down move, Enter opens, Backspace goes up.
import { t } from './i18n.js';
import { api } from './data.js';
import { h, icon, openDrawer, closeDrawer, toast } from './ui.js';

// "C:\code\app" -> [{name:'C:', path:'C:\\'}, {name:'code', path:'C:\\code'}, ...]
function crumbsOf(path) {
  if (!path) return [];
  const win = path.includes('\\') || /^[A-Za-z]:/.test(path);
  const sep = win ? '\\' : '/';
  const parts = path.split(/[\\/]/).filter(Boolean);
  const out = win ? [] : [{ name: '/', path: '/' }];
  let acc = '';
  parts.forEach((p, i) => {
    acc = win ? (i === 0 ? p + sep : acc + (acc.endsWith(sep) ? '' : sep) + p) : acc + sep + p;
    out.push({ name: p, path: acc });
  });
  return out;
}

// pickFolder({ title, start }) -> the chosen absolute path, or null when closed without choosing.
export function pickFolder({ title, start } = {}) {
  return new Promise((resolve) => {
    let cur = { path: '', parent: '', dirs: [], places: [] };
    let active = -1;
    let done = false;
    const drawer = document.getElementById('drawer');
    const crumbs = h('nav.fp-crumbs', { 'aria-label': t('folders.path') });
    const list = h('div.fp-list', { role: 'listbox', tabindex: '0', 'aria-label': t('folders.list') });
    const useBtn = h('button.btn.primary', { type: 'button', disabled: true, onclick: () => finish(cur.path) }, icon('check'), t('folders.use'));
    const cancelBtn = h('button.btn.ghost', { type: 'button', onclick: () => finish(null) }, t('common.cancel'));
    const chosen = h('span.fp-chosen.mono', { 'data-tip': '' });

    const obs = new MutationObserver(() => { if (drawer.getAttribute('aria-hidden') === 'true') finish(null, true); });
    obs.observe(drawer, { attributes: true, attributeFilter: ['aria-hidden'] });
    function finish(v, closed) {
      if (done) return;
      done = true;
      obs.disconnect();
      if (!closed) closeDrawer();
      resolve(v);
    }

    const items = () => [...list.querySelectorAll('.fp-item')];
    const mark = (i) => {
      const els = items();
      if (!els.length) { active = -1; return; }
      active = Math.max(0, Math.min(els.length - 1, i));
      els.forEach((el, k) => { el.classList.toggle('on', k === active); el.setAttribute('aria-selected', k === active ? 'true' : 'false'); });
      els[active].scrollIntoView({ block: 'nearest' });
    };

    function draw() {
      const trail = crumbsOf(cur.path);
      crumbs.replaceChildren(
        h('button.fp-crumb', { type: 'button', onclick: () => go(''), 'aria-label': t('folders.places') }, icon('folder')),
        ...trail.flatMap((c, i) => [
          h('span.fp-sep', '/'),
          i === trail.length - 1 ? h('strong.fp-crumb.now', { 'aria-current': 'location' }, c.name) : h('button.fp-crumb', { type: 'button', onclick: () => go(c.path) }, c.name)]));
      const rows = cur.path ? cur.dirs.map((d) => ({ label: d.name, path: d.path })) : cur.places.map((p) => ({ label: p.label, path: p.path }));
      list.replaceChildren(
        ...(cur.path && cur.parent ? [h('button.fp-item.up', { type: 'button', role: 'option', onclick: () => go(cur.parent) }, icon('up'), h('span', t('folders.up')))] : []),
        ...rows.map((r) => h('button.fp-item', { type: 'button', role: 'option', 'data-tip': r.path, onclick: () => go(r.path) }, icon('folder'), h('span.fp-name', r.label))));
      if (cur.path && !cur.dirs.length) list.append(h('p.muted.fp-empty', t('folders.empty')));
      useBtn.disabled = !cur.path;
      chosen.textContent = cur.path || t('folders.pickHint');
      chosen.setAttribute('data-tip', cur.path || '');
      mark(0);
    }

    async function go(path) {
      try {
        cur = await api.listDirs(path);
        draw();
        list.focus();
      } catch (e) {
        toast(e.message, null, 'deny');
      }
    }

    list.addEventListener('keydown', (e) => {
      const els = items();
      if (e.key === 'ArrowDown') { e.preventDefault(); mark(active + 1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); mark(active - 1); }
      else if (e.key === 'Enter' && active >= 0) { e.preventDefault(); els[active].click(); }
      else if (e.key === 'Backspace' && cur.path) { e.preventDefault(); go(cur.parent || ''); }
    });

    openDrawer(title || t('folders.title'), h('div.fp', crumbs, list), [chosen, cancelBtn, useBtn]);
    // start where asked; when that folder is gone or hidden, start at the places
    (async () => {
      try { cur = await api.listDirs(start || ''); } catch { cur = await api.listDirs('').catch(() => cur); }
      draw();
      list.focus();
    })();
  });
}

// A settings row: the current folder, a Browse button, an optional Reset.
// Picking a folder only proposes it; the change is applied by the second
// click ("Move"/"Use"), because it may move files.
export function folderRow({ id, label, hint, value, applyLabel, onApply, onReset, resetLabel }) {
  const cur = h('code.fp-cur', { 'data-tip': value || '' }, value || '');
  const pending = h('div.fp-pending', { hidden: true });
  const run = async (fn) => {
    try {
      const r = await fn();
      if (r && r.dir) { cur.textContent = r.dir; cur.setAttribute('data-tip', r.dir); }
      toast(r && r.warning ? r.warning : t('folders.saved', { dir: (r && r.dir) || cur.textContent }));
    } catch (e) { toast(e.message, null, 'deny'); }
  };
  const browse = h('button.btn', { type: 'button', id, onclick: async () => {
    const dir = await pickFolder({ title: label, start: cur.textContent });
    if (!dir) return;
    pending.hidden = false;
    pending.replaceChildren(h('span.fp-cur', { 'data-tip': dir }, dir),
      h('button.btn.primary.sm', { type: 'button', onclick: async () => { pending.hidden = true; await run(async () => { const r = await onApply(dir); return { dir, ...(r || {}) }; }); } }, applyLabel),
      h('button.btn.ghost.sm', { type: 'button', onclick: () => { pending.hidden = true; } }, t('common.cancel')));
  } }, icon('folder'), t('folders.browse'));
  return h('div.field', h('label', { for: id }, label),
    h('div.row.fp-row', cur, browse, onReset ? h('button.btn.ghost', { type: 'button', onclick: () => run(onReset) }, resetLabel || t('folders.reset')) : null),
    pending, hint ? h('span.hint', hint) : null);
}
