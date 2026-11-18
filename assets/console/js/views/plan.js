// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Plan panel for a main chat: the sub-tasks the agent split off, drawn as a
// small graph you can act on (steer, stop, retry, change model, merge).
// Evidence rule: "reported done" is never "verified"; only verified work merges.
import { api, MAX_REPAIRS } from '../data.js';
import { t } from '../i18n.js';
import { h, s, icon, lamp, SESSION_LAMP, menu, confirmRedLine, openDrawer, closeDrawer, toast } from '../ui.js';

const MODELS = ['haiku', 'sonnet', 'opus'];
export const EV_LAMP = { verified: 'clear', seen: 'route', running: 'route', reported: 'caution', failed: 'danger', blocked: 'danger', cancelled: 'idle', notrun: 'idle' };

export const evText = (x) => (x?.ev ? `${t('ev.stage.' + x.ev[0])} · ${t('ev.conf.' + x.ev[1])}` : '');
export function evTag(x) {
  if (!x?.ev) return null;
  return h('span.ev-tag', { class: 'ev-' + x.ev[1], title: t('ev.aria') }, lamp(EV_LAMP[x.ev[1]] || 'idle'), evText(x));
}

const direct = (root) => api.sessionsSync().filter((x) => x.parent === root.id);
const nested = (id) => api.sessionsSync().filter((x) => x.parent === id).flatMap((x) => [x, ...nested(x.id)]);
const openDeps = (x) => (x.deps || []).map(api.session).filter((d) => d && !(d.status === 'done' && d.ev?.[1] !== 'failed'));

// One sentence instead of cards or a progress bar.
export function planHeadline(root) {
  const subs = direct(root);
  if (!subs.length) return '';
  const all = nested(root.id);
  const verified = subs.filter((x) => x.ev?.[1] === 'verified').length;
  const parts = [t('plan.verifiedOf', { n: verified, total: subs.length })];
  const running = all.filter((x) => x.status === 'running').length;
  const needs = all.filter((x) => x.status === 'waiting').length;
  const queued = subs.filter((x) => x.status === 'queued').length;
  if (running) parts.push(t('plan.running', { n: running }));
  if (needs) parts.push(t('plan.needsYou', { n: needs }));
  if (queued) parts.push(t('plan.queued', { n: queued }));
  const clashes = all.filter((x) => api.scopeConflicts(x.id).length).length;
  if (clashes) parts.push(t('plan.overlaps', { n: clashes }));
  return parts.join(' · ');
}

function steerDrawer(x) {
  const ta = h('textarea.input', { rows: 5, placeholder: t('plan.steerPh'), 'aria-label': t('plan.steerPh') });
  const send = () => { const v = ta.value.trim(); if (!v) { ta.focus(); return; } api.steerSubtask(x.id, v); closeDrawer(); toast(t('plan.toast.steered', { title: x.title })); };
  ta.addEventListener('keydown', (e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); send(); } });
  openDrawer(t('plan.steerTitle', { title: x.title }), h('div.stack', { style: { padding: '16px' } }, h('p.faint', t('plan.steerLede')), ta),
    [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')),
      h('button.btn.primary', { type: 'button', onclick: send }, icon('steer'), t('plan.steerSend'))]);
  setTimeout(() => ta.focus(), 50);
}

async function changeModel(x, m) {
  if (m === x.model) return;
  const ok = await confirmRedLine({ title: t('chat.model.changeTitle'), body: t('chat.model.changeBody', { from: x.model, to: m }), redLine: 'model', confirmLabel: t('chat.model.change', { m }) });
  if (ok) { api.setSessionProp(x.id, { model: m }); toast(t('chat.model.changed', { m })); }
}

export function nodeMenu(anchor, x) {
  const live = x.status === 'running' || x.status === 'waiting';
  const finished = x.status === 'done' || x.status === 'stopped';
  const canRetry = !live && x.status !== 'queued' && x.repairs < MAX_REPAIRS && x.ev?.[1] !== 'verified';
  const canMerge = x.ev?.[1] === 'verified' && !x.merged && x.role !== 'verify' && !live && !nested(x.id).some((k) => k.status === 'running' || k.status === 'waiting');
  menu(anchor, [
    { heading: `${x.num} · ${x.title}` },
    // What it may touch, who else holds those files, and the model chain it falls back along.
    { heading: x.scope?.length ? t('plan.scope.writes', { list: x.scope.join(', ') }) : t('plan.scope.readOnly') },
    ...api.scopeConflicts(x.id).map((r) => ({ heading: '⚠ ' + t('plan.scope.clash', { who: api.person(api.session(r.claim.session)?.owner)?.name || '?', claim: r.claim.id, files: r.files.join(', ') }) })),
    { heading: t('plan.chain', { list: api.categoryChain(x.category).map((c) => `${c.model} · ${c.effort}`).join(' → ') }) },
    x.worktree ? { heading: t('plan.worktree', { path: x.worktree }) } : null,
    x.scope?.length ? { label: t('plan.act.board'), icon: 'board', onSelect: () => { location.hash = '#/board'; } } : null,
    { sep: true },
    { label: t('plan.act.open'), icon: 'chat', onSelect: () => { location.hash = '#/sessions/' + x.id; } },
    { label: t('plan.act.steer'), icon: 'steer', disabled: !live, onSelect: () => steerDrawer(x) },
    { label: x.repairs >= MAX_REPAIRS ? t('plan.act.retryNone') : t('plan.act.retry', { k: x.repairs + 1, max: MAX_REPAIRS }), icon: 'undo', disabled: !canRetry,
      onSelect: () => { if (api.retrySubtask(x.id)) toast(t('plan.toast.retried', { title: x.title })); } },
    { label: canMerge ? t('plan.act.merge') : x.merged ? t('plan.act.merged') : t('plan.act.merge'), icon: 'git', disabled: !canMerge, hint: canMerge || x.merged ? null : t('plan.act.mergeNeeds'),
      onSelect: () => { if (api.mergeSubtask(x.id)) toast(t('plan.toast.merged', { title: x.title })); } },
    { sep: true },
    { heading: t('plan.act.model') },
    ...MODELS.map((m) => ({ label: m, checked: m === x.model, disabled: finished, onSelect: () => changeModel(x, m) })),
    { sep: true },
    { label: t('plan.act.stop'), icon: 'stop', danger: true, disabled: finished, onSelect: () => { api.stopSubtask(x.id); toast(t('plan.toast.stopped', { title: x.title }), null, 'deny'); } },
  ]);
}

function nodeEl(x, root, current) {
  const waits = openDeps(x);
  const kids = nested(x.id);
  const criteria = x.role === 'verify' ? api.chatMeta(root.id).criteria || [] : [];
  const clashes = api.scopeConflicts(x.id);
  const btn = h('button.pg-node', {
    type: 'button', 'data-id': x.id, 'aria-haspopup': 'menu',
    class: [x.status, x.role === 'verify' ? 'verify' : '', x.merged ? 'merged' : '', x.id === current || nested(x.id).some((k) => k.id === current) ? 'current' : ''].join(' '), 'aria-current': x.id === current ? 'true' : null,
    'aria-label': `${x.num} ${x.title}. ${t('status.' + x.status)}. ${evText(x)}`,
  },
  h('span.pg-title', lamp(SESSION_LAMP[x.status] || 'idle', { blink: x.status === 'waiting' }), h('span', x.title), h('span.mono.faint.pg-num', x.num)),
  h('span.pg-route.mono', [x.model, x.effort, t('plan.category.' + x.category)].filter(Boolean).join(' · ')),
  h('span.pg-scope', { class: clashes.length ? 'clash' : x.scope?.length ? 'mono' : 'ro', title: (x.scope || []).join('\n') },
    icon(clashes.length ? 'warn' : x.scope?.length ? 'lock' : 'eye'),
    clashes.length ? t('plan.scope.clashShort', { who: api.person(api.session(clashes[0].claim.session)?.owner)?.name || '?', files: clashes[0].files[0] })
      : x.scope?.length ? x.scope[0] + (x.scope.length > 1 ? ` +${x.scope.length - 1}` : '') : x.role === 'verify' || x.ev?.[0] === 'research' ? t('plan.scope.readOnly') : t('plan.scope.none')),
  h('span.pg-state', evTag(x),
    waits.length && x.status === 'queued' ? h('span.faint', t('plan.waitsOn', { n: waits.length })) : null,
    x.repairs ? h('span.tag.caution', t('plan.repair', { k: x.repairs, max: MAX_REPAIRS })) : null,
    kids.length ? h('span.faint', t('plan.more', { n: kids.length })) : null,
    x.merged ? h('span.tag.clear', icon('git'), t('plan.act.merged')) : null),
  criteria.length ? h('span.pg-criteria', criteria.map((c) => h('span', { class: c.result || 'open' }, icon(c.result === 'pass' ? 'check' : c.result === 'fail' ? 'x' : 'dot'), c.text))) : null);
  btn.addEventListener('click', () => nodeMenu(btn, x));
  return btn;
}

// Layers come from dependencies: a node sits one row below the deepest node it waits on.
function layers(subs) {
  const ids = new Set(subs.map((x) => x.id));
  const memo = {};
  const depth = (x, seen = new Set()) => {
    if (memo[x.id] !== undefined) return memo[x.id];
    if (seen.has(x.id)) return 0;
    seen.add(x.id);
    const ds = (x.deps || []).filter((d) => ids.has(d)).map((d) => depth(subs.find((q) => q.id === d), seen));
    return (memo[x.id] = ds.length ? Math.max(...ds) + 1 : 0);
  };
  const rows = [];
  subs.forEach((x) => { const d = depth(x); (rows[d] = rows[d] || []).push(x); });
  return rows.filter(Boolean);
}

export function planGraph(root, { current = null } = {}) {
  const subs = direct(root);
  const wrap = h('div.pg', { role: 'group', 'aria-label': t('plan.graphAria') });
  const svg = s('svg', { class: 'pg-edges', 'aria-hidden': 'true' });
  const rows = layers(subs);
  const rowsEl = h('div.pg-rows', rows.map((row) => h('div.pg-row', row.map((x) => nodeEl(x, root, current)))));
  wrap.append(svg, rowsEl);
  // Only the edges that matter: if x waits on a and on b, and b already waits on a,
  // the a→x line says nothing new (transitive reduction).
  const ids = new Set(subs.map((x) => x.id));
  const reaches = (from, to, seen = new Set()) => {
    if (from === to) return true;
    if (seen.has(from)) return false;
    seen.add(from);
    const n = subs.find((q) => q.id === from);
    return (n?.deps || []).filter((d) => ids.has(d)).some((d) => reaches(d, to, seen));
  };
  const edges = [];
  subs.forEach((x) => {
    const ds = (x.deps || []).filter((d) => ids.has(d));
    ds.forEach((d) => { if (!ds.some((o) => o !== d && reaches(o, d))) edges.push([d, x]); });
  });
  let tries = 0;
  const draw = () => {
    const box = wrap.getBoundingClientRect();
    // Built before it is attached: try again shortly (frames may not run in a hidden pane).
    if (!box.width) { if (tries++ < 20) setTimeout(draw, 100); return; }
    svg.setAttribute('width', box.width); svg.setAttribute('height', wrap.scrollHeight);
    const rect = {};
    wrap.querySelectorAll('.pg-node').forEach((n) => {
      const r = n.getBoundingClientRect();
      rect[n.dataset.id] = { l: r.left - box.left, r: r.right - box.left, t: r.top - box.top, b: r.bottom - box.top };
    });
    // Visual rows come from where boxes actually landed (a wide layer may wrap).
    const tops = [...new Set(Object.values(rect).map((r) => Math.round(r.t)))].sort((a, b) => a - b);
    const rowOf = (id) => tops.indexOf(Math.round(rect[id].t));
    const between = (ra, rb) => Object.values(rect).filter((r) => { const i = tops.indexOf(Math.round(r.t)); return i > ra && i < rb; });
    const blocked = (x, list) => list.some((r) => x > r.l - 6 && x < r.r + 6);
    // Free vertical lanes: the gaps between boxes of the rows in between.
    const gaps = (list) => {
      const xs = [4, box.width - 4];
      list.forEach((r) => { xs.push(r.l - 6, r.r + 6); });
      return xs.filter((x) => x > 0 && x < box.width && !blocked(x, list));
    };
    const paths = [];
    const arrows = new Set();
    edges.forEach(([d, x]) => {
      const a = rect[d]; const b = rect[x.id];
      if (!a || !b) return;
      const x1 = (a.l + a.r) / 2; const y1 = a.b;
      const x2 = (b.l + b.r) / 2; const y2 = b.t;
      const bus = y2 - 11; // every line into a box joins one bus just above it
      const mid = between(rowOf(d), rowOf(x.id));
      let route;
      if (!mid.length) route = `M${x1} ${y1} V${bus} H${x2} V${y2 - 3}`;
      else if (!blocked(x1, mid)) route = `M${x1} ${y1} V${bus} H${x2} V${y2 - 3}`;
      else {
        const lane = gaps(mid).sort((p, q) => Math.abs(p - x1) + Math.abs(p - x2) - (Math.abs(q - x1) + Math.abs(q - x2)))[0] ?? x1;
        route = `M${x1} ${y1} V${y1 + 11} H${lane} V${bus} H${x2} V${y2 - 3}`;
      }
      const dep = api.session(d);
      paths.push(s('path', { d: route, class: 'pg-edge' + (x.status === 'running' ? ' live' : '') + (dep?.status === 'done' ? ' done' : '') }));
      if (!arrows.has(x.id)) {
        arrows.add(x.id);
        paths.push(s('path', { d: `M${x2 - 4} ${y2 - 7} L${x2} ${y2 - 2} L${x2 + 4} ${y2 - 7}`, class: 'pg-arrow' }));
      }
    });
    svg.replaceChildren(...paths);
  };
  const ro = new ResizeObserver(draw);
  ro.observe(wrap);
  setTimeout(draw, 0);
  return wrap;
}

// Overview: every main chat that split work, as its headline and its plan.
export function planBoard() {
  const roots = api.sessionsSync().filter((x) => !x.parent && api.sessionsSync().some((k) => k.parent === x.id));
  if (!roots.length) return h('p.empty', t('plan.boardEmpty'));
  return h('div.plan-board', roots.map((r) => h('section.pb-card',
    h('div.pb-head',
      h('a.pb-title', { href: '#/sessions/' + r.id }, r.title),
      h('span.pb-line', planHeadline(r))),
    planGraph(r))));
}
