// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Files: the project's rule folders as cards, the current folder as a list,
// drop or Upload to save files where the rules say, and Tidy (with Undo) to move
// files that sit outside their folder. Nothing moves without the Confirm click.
import { api } from '../data.js';
import { isLive, teamMode } from '../live.js';
import { t } from '../i18n.js';
import { h, icon, lamp, rel, toast, pageHead, panel, openDrawer, closeDrawer, emptyState, fmtNum } from '../ui.js';

// ---- demo data (the plain dev server) ----
const now = Date.now();
const DEMO_RULES = { state: 'ok', bad: [], rows: [
  { folder: 'Export', what: 'Models for the engine', exts: ['.fbx', '.obj'] },
  { folder: 'Textures', what: 'Images used by models', exts: ['.png', '.jpg', '.tga'] },
  { folder: 'docs', what: 'Notes and specs', exts: ['.md', '.pdf'] },
] };
const DEMO_LIST = {
  '': [['Export', true], ['Textures', true], ['docs', true], ['hero.fbx', false, 'Export'], ['readme.txt', false, '']],
  Export: [['ship.fbx', false, 'Export'], ['crate.obj', false, 'Export']],
  Textures: [['ship_albedo.png', false, 'Textures']],
  docs: [['plan.md', false, 'docs'], ['brief.pdf', false, 'docs'], ['notes.md', false, 'docs']],
};
const demoPlan = () => ({ state: 'ok', canUndo: false, homeless: ['readme.txt'],
  moves: [{ from: 'hero.fbx', to: 'Export/hero.fbx', size: 482113, mtime: new Date(now - 3600e3).toISOString() }] });

const rulesOf = (p) => (isLive() ? api.folderRules(p) : Promise.resolve(structuredClone(DEMO_RULES)));
const listOf = (p, path) => (isLive() ? api.listFiles(p, path)
  : Promise.resolve({ path, entries: (DEMO_LIST[path] || []).map(([name, dir, folder]) => ({ name, dir, folder: folder || '', size: dir ? 0 : 20480, mtime: Math.floor((now - 7200e3) / 1000) })) }));
const planOf = (p) => (isLive() ? api.tidyPlan(p) : Promise.resolve(demoPlan()));

export function fmtSize(n) {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(u.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  const v = n / 1024 ** i;
  return (i === 0 ? String(n) : v.toFixed(v < 10 ? 1 : 0)) + ' ' + u[i];
}
const lower = (s) => String(s).toLowerCase();
const under = (path, folder) => lower(path + '/').startsWith(lower(folder.replace(/^\/+|\/+$/g, '')) + '/');
const dirOf = (p) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '');

export default async function files() {
  const project = api.project().id;
  const canWrite = isLive() ? !teamMode() : true;
  let path = '';
  let rules = { state: 'none', rows: [], bad: [] };
  let plan = null;

  const banner = h('div.fl-banner');
  const cards = h('div.fl-grid', { role: 'list' });
  const crumbs = h('nav.fp-crumbs', { 'aria-label': t('folders.path') });
  const list = h('ul.fl-list');
  const progress = h('div.fl-progress', { hidden: true }, h('div.meter', h('i', { style: { width: '0%' } })), h('span.mono'));
  const fileIn = h('input', { type: 'file', multiple: true, hidden: true, onchange: () => { upload([...fileIn.files]); fileIn.value = ''; } });

  const offTip = canWrite ? null : t('files.soloOnly');
  const uploadBtn = h('button.btn.primary', { type: 'button', disabled: !canWrite || null, 'data-tip': offTip, onclick: () => fileIn.click() }, icon('up'), t('files.upload'));
  const tidyCount = h('span.count.warm', { hidden: true });
  const tidyBtn = h('button.btn', { type: 'button', disabled: true, onclick: () => tidyDrawer() }, icon('sparkle'), t('files.tidy.button'), tidyCount);
  const undoBtn = h('button.btn.ghost', { type: 'button', hidden: true, onclick: () => undo() }, icon('undo'), t('files.undo'));

  // ---- state banner ----
  function drawBanner() {
    const bad = rules.bad?.length ? h('span.tag.caution', { 'data-tip': rules.bad.join('\n') }, icon('warn'), t('files.state.bad', { n: rules.bad.length })) : null;
    const box = {
      none: () => h('div.callout.route', icon('sparkle'), h('div.fl-banner-row', h('span', t('files.state.none')))),
      pending: () => h('div.callout.caution', icon('info'), h('div.fl-banner-row', h('span', t('files.state.pending')),
        h('a.btn.sm', { href: '#/wiki/drafts' }, icon('edit'), t('files.openDraft')))),
      changed: () => h('div.callout.caution', icon('warn'), h('div.fl-banner-row', h('span', t('files.state.changed')), bad,
        h('a.btn.sm', { href: '#/wiki' }, icon('wiki'), t('files.openPage')))),
    }[rules.state];
    banner.replaceChildren(...(box ? [box()] : []));
  }

  // ---- rule cards: one per rule folder, plus the root ----
  async function drawCards() {
    const rows = rules.rows || [];
    const lists = await Promise.all(['', ...rows.map((r) => r.folder)].map((f) => listOf(project, f).catch(() => null)));
    const counts = lists.map((l) => (l ? l.entries.filter((e) => !e.dir).length : null));
    const max = Math.max(1, ...counts.map((n) => n || 0));
    const on = rules.state === 'ok';
    const card = (folder, label, what, exts, n, root) => h('button.fl-card', { type: 'button', role: 'listitem', class: (on ? 'on' : '') + (lower(folder) === lower(path) ? ' here' : ''),
      'aria-label': label, onclick: () => open(folder) },
    h('span.fl-badge', icon(root ? 'projects' : 'folder')),
    h('span.fl-main',
      h('span.fl-name', root ? null : lamp(on ? 'clear' : 'idle'), h('span', label)),
      what ? h('span.fl-what', { title: what }, what) : null,
      exts?.length ? h('span.fl-exts', exts.slice(0, 6).map((e) => h('span.tag', e)), exts.length > 6 ? h('span.tag', '+' + (exts.length - 6)) : null) : null,
      h('span.fl-foot',
        h('span.meter', h('i', { style: { width: (n ? Math.max(4, (n / max) * 100) : 0) + '%' } })),
        h('span.fl-n', { 'data-tip': n == null ? t('files.noFolder') : t('files.count', { n }) }, icon('file'), n == null ? '–' : fmtNum(n)))));
    cards.replaceChildren(
      card('', t('files.root'), null, null, counts[0], true),
      ...rows.map((r, i) => card(r.folder, r.folder, r.what, r.exts, counts[i + 1], false)));
  }

  // ---- the current folder ----
  function drawCrumbs() {
    const segs = path ? path.split('/') : [];
    crumbs.replaceChildren(
      h(segs.length ? 'button.fp-crumb' : 'span.fp-crumb.now', { type: segs.length ? 'button' : null, 'aria-label': t('files.root'), onclick: segs.length ? () => open('') : null }, icon('projects')),
      ...segs.flatMap((s, i) => {
        const to = segs.slice(0, i + 1).join('/');
        const last = i === segs.length - 1;
        return [h('span.fp-sep', '/'), last ? h('span.fp-crumb.now', s) : h('button.fp-crumb', { type: 'button', onclick: () => open(to) }, s)];
      }));
  }
  async function drawList() {
    drawCrumbs();
    let res;
    try { res = await listOf(project, path); } catch (err) { list.replaceChildren(h('li.fl-empty', icon('warn'), t('files.loadFailed'), ' ', h('span.faint', err.message))); return; }
    const rows = [];
    if (path) rows.push(h('li', h('button.fl-row.dir', { type: 'button', onclick: () => open(dirOf(path)) }, icon('up'), h('span.fl-file', t('folders.up')))));
    for (const e of res.entries) {
      const sub = path ? path + '/' + e.name : e.name;
      if (e.dir) {
        rows.push(h('li', h('button.fl-row.dir', { type: 'button', onclick: () => open(sub) }, icon('folder'), h('span.fl-file', e.name), h('span'), h('span'), h('span.faint.mono', rel(e.mtime * 1000)))));
        continue;
      }
      const away = e.folder && !under(sub, e.folder);
      rows.push(h('li', h('div.fl-row', { class: away ? 'away' : '' }, icon('file'), h('span.fl-file', { title: e.name }, e.name),
        away ? h('span.tag.caution', { 'data-tip': t('files.misplaced', { folder: e.folder }) }, icon('chevron'), e.folder) : h('span'),
        h('span.faint.mono', fmtSize(e.size)), h('span.faint.mono', rel(e.mtime * 1000)))));
    }
    if (!res.entries.length) rows.push(h('li.fl-empty', icon('folder'), t('files.empty')));
    list.replaceChildren(...rows);
  }
  function open(folder) { path = folder; drawList(); cards.querySelectorAll('.fl-card').forEach((c, i) => c.classList.toggle('here', lower(i === 0 ? '' : (rules.rows[i - 1]?.folder || '')) === lower(path))); }

  // ---- tidy state (count + Undo) ----
  async function drawTidy() {
    plan = canWrite ? await planOf(project).catch(() => null) : null;
    const n = plan?.state === 'ok' ? plan.moves.length : 0;
    tidyBtn.disabled = !plan || plan.state !== 'ok';
    tidyBtn.dataset.tip = !canWrite ? t('files.soloOnly') : plan?.state !== 'ok' ? t('files.tidy.off') : t('files.tidy.countTip', { n });
    tidyCount.hidden = !n;
    tidyCount.textContent = String(n);
    undoBtn.hidden = !plan?.canUndo;
  }

  async function refresh() {
    rules = await rulesOf(project).catch(() => ({ state: 'none', rows: [], bad: [] }));
    drawBanner();
    await Promise.all([drawCards(), drawList(), drawTidy()]);
  }

  // ---- upload ----
  async function upload(fl) {
    if (!fl.length || !canWrite) return;
    if (!isLive()) { toast(t('files.uploaded', { n: fl.length })); return; }
    const bar = progress.querySelector('i');
    const pct = progress.querySelector('span');
    progress.hidden = false; uploadBtn.disabled = true;
    const set = (k) => { bar.style.width = Math.round(k * 100) + '%'; pct.textContent = Math.round(k * 100) + '%'; };
    set(0);
    try {
      const res = await api.uploadFiles(project, fl, set);
      const where = {};
      for (const f of res.files || []) where[f.folder || t('files.root')] = (where[f.folder || t('files.root')] || 0) + 1;
      toast(t('files.uploaded', { n: (res.files || []).length }) + ' · ' + Object.entries(where).map(([k, v]) => `${k} ${v}`).join(' · '));
    } catch (err) {
      toast(t('files.uploadFailed', { err: err.message }), null, 'deny');
    } finally {
      progress.hidden = true; uploadBtn.disabled = false;
      refresh();
    }
  }

  // ---- tidy drawer ----
  async function tidyDrawer() {
    tidyBtn.disabled = true;
    const p = await planOf(project).catch((err) => { toast(err.message, null, 'deny'); return null; });
    tidyBtn.disabled = false;
    if (!p) return;
    if (p.state !== 'ok') { toast(t('files.tidy.off'), null, 'deny'); return; }
    const picked = new Set(p.moves.map((_, i) => i));
    const groups = new Map();
    p.moves.forEach((m, i) => { const d = dirOf(m.to); if (!groups.has(d)) groups.set(d, []); groups.get(d).push(i); });
    const sel = h('span.fl-sel.mono');
    const go = h('button.btn.primary', { type: 'button', onclick: () => apply() }, icon('check'));
    const sync = () => {
      sel.textContent = t('files.tidy.selected', { n: picked.size, total: p.moves.length });
      go.replaceChildren(icon('check'), t('files.tidy.confirm', { n: picked.size }));
      go.disabled = !picked.size;
      body.querySelectorAll('input[data-group]').forEach((g) => {
        const idx = groups.get(g.dataset.group);
        const k = idx.filter((i) => picked.has(i)).length;
        g.checked = k === idx.length; g.indeterminate = k > 0 && k < idx.length;
      });
    };
    const groupEl = (dir, idx) => {
      const boxes = idx.map((i) => h('input', { type: 'checkbox', checked: true, 'aria-label': p.moves[i].from,
        onchange: (e) => { if (e.target.checked) picked.add(i); else picked.delete(i); sync(); } }));
      return h('section.fl-group',
        h('label.fl-group-head', h('input', { type: 'checkbox', checked: true, 'data-group': dir, 'aria-label': dir,
          onchange: (e) => { idx.forEach((i, k) => { boxes[k].checked = e.target.checked; if (e.target.checked) picked.add(i); else picked.delete(i); }); sync(); } }),
        icon('folder'), h('b.mono', dir || t('files.root')), h('span.count', String(idx.length))),
        h('ul.fl-moves', idx.map((i, k) => h('li', h('label', boxes[k], h('span.fl-file.mono', { title: p.moves[i].from }, p.moves[i].from), h('span.faint.mono', fmtSize(p.moves[i].size)))))));
    };
    const homeless = p.homeless?.length ? h('section.fl-group.homeless',
      h('div.fl-group-head', { 'data-tip': t('files.tidy.homelessTip') }, icon('info'), h('b', t('files.tidy.homeless')), h('span.count', String(p.homeless.length))),
      h('ul.fl-moves', p.homeless.slice(0, 50).map((f) => h('li', h('span.fl-file.mono.faint', { title: f }, f))),
        p.homeless.length > 50 ? h('li.faint', t('files.tidy.more', { n: p.homeless.length - 50 })) : null)) : null;
    const body = h('div.fl-tidy',
      p.moves.length ? [...groups].map(([d, idx]) => groupEl(d, idx)) : emptyState(t('files.tidy.none')),
      homeless);
    async function apply() {
      const moves = p.moves.filter((_, i) => picked.has(i));
      go.disabled = true;
      try {
        const res = isLive() ? await api.tidyApply(project, moves) : { done: moves, skipped: [] };
        closeDrawer();
        toast(t('files.tidy.done', { n: res.done.length }), res.done.length ? { label: t('files.undo'), run: () => undo() } : null);
        if (res.skipped?.length) toast(t('files.tidy.skipped', { n: res.skipped.length }), null, 'deny');
      } catch (err) { toast(err.message, null, 'deny'); go.disabled = false; return; }
      refresh();
    }
    openDrawer(t('files.tidy.title'), body, p.moves.length
      ? [sel, h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')), go]
      : [h('button.btn', { type: 'button', onclick: closeDrawer }, t('common.close'))]);
    if (p.moves.length) sync();
  }

  async function undo() {
    try {
      const res = isLive() ? await api.tidyUndo(project) : { done: [], skipped: [] };
      toast(t('files.undone', { n: res.done.length }));
      if (res.skipped?.length) toast(t('files.tidy.skipped', { n: res.skipped.length }), null, 'deny');
    } catch (err) { toast(err.message, null, 'deny'); }
    refresh();
  }

  // ---- drop anywhere on the view ----
  let depth = 0;
  const el = h('div.page.fl-page', {
    ondragenter: (e) => { if (!canWrite || !e.dataTransfer?.types?.includes('Files')) return; e.preventDefault(); depth++; el.classList.add('fl-drop'); },
    ondragover: (e) => { if (canWrite && e.dataTransfer?.types?.includes('Files')) e.preventDefault(); },
    ondragleave: () => { if (--depth <= 0) { depth = 0; el.classList.remove('fl-drop'); } },
    ondrop: (e) => { if (!canWrite) return; e.preventDefault(); depth = 0; el.classList.remove('fl-drop'); upload([...e.dataTransfer.files]); },
  },
  pageHead(t('files.title'), null, [undoBtn, tidyBtn, uploadBtn, fileIn]),
  progress, banner, cards,
  panel(null, null, [crumbs, list], { flush: true }),
  h('div.fl-dropmask', { 'aria-hidden': 'true' }, icon('up'), h('span', t('files.dropHint'))));

  await refresh();
  return { el };
}
