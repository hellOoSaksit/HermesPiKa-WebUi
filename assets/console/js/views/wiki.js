// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { isLive } from '../live.js';
import { t } from '../i18n.js';
import { h, icon, rel, pageHead, toast, emptyState, clock, dateTime, openDrawer, closeDrawer } from '../ui.js';

function diffView(lines) {
  return h('div.diff', lines.map(([k, txt]) => h('div', { class: k === 'redact' ? 'ctx' : k }, k === 'redact' ? h('span.redact', txt) : txt)));
}

export default async function wiki({ params, go }) {
  const tab = params.tab || 'pages';
  const tabs = h('div.tabs', { role: 'tablist' },
    ['pages', 'drafts', 'log'].map((k) => h('button', { role: 'tab', type: 'button', 'aria-selected': k === tab ? 'true' : 'false', onclick: () => go(k === 'pages' ? 'wiki' : 'wiki/' + k) },
      t('wiki.tab.' + k), k === 'drafts' ? h('span.tag.caution', String(api.project().drafts)) : null)));

  let body;
  if (tab === 'pages') body = await pages();
  else if (tab === 'drafts') body = await drafts();
  else body = logView();

  const p = api.project();
  const el = h('div.page',
    pageHead(t('wiki.title'), t('wiki.lede', { pages: p.wikiPages }),
      [h('div.search', icon('search'), h('input.input', { type: 'search', placeholder: t('wiki.searchPh'), 'aria-label': t('wiki.searchPh'), style: { width: '260px' } }))]),
    tabs, body);
  return { el };
}

async function pages() {
  const tree = (await api.wikiTree()).filter((sec) => sec.pages && sec.pages.length);
  if (!tree.length) {
    const n = api.project().drafts || 0;
    return h('div.panel', h('div.panel-body.empty', h('p', t('wiki.empty')), n ? h('p.faint', t('wiki.emptyDrafts', { n })) : null));
  }
  let current = tree[0].pages[0].id;
  const nav = h('nav.wiki-nav', { 'aria-label': t('wiki.sections') });
  const view = h('article.panel-body');
  function drawNav() {
    nav.replaceChildren(...tree.flatMap((sec) => [
      h('h3', t('wiki.section.' + sec.section)),
      ...sec.pages.map((pg) => h('button', { type: 'button', 'aria-current': pg.id === current ? 'true' : null, onclick: () => { current = pg.id; drawNav(); drawPage(); } },
        h('span', pg.title), h('span.faint.mono', { style: { fontSize: '11px' } }, rel(pg.updated)))),
    ]));
  }
  async function drawPage() {
    const pg = await api.wikiPage(current);
    view.replaceChildren(h('div.prose',
      h('div.row.between', h('span.id', pg.path), h('div.row', { style: { gap: '6px' } },
        h('button.btn.sm', { type: 'button', onclick: () => toast(t('wiki.draftStarted')) }, t('wiki.proposeEdit')),
        isLive() && api.project().privacy === 'shared' ? h('button.btn.sm', { type: 'button', onclick: () => promoteDrawer(pg) }, icon('globe'), t('wiki.promote')) : null,
        h('button.btn.sm.ghost', { type: 'button', onclick: () => toast(t('wiki.historyNote')) }, icon('git'), t('wiki.history')))),
      h('h1', { style: { marginTop: '10px' } }, pg.title),
      h('div.frontmatter',
        h('span', t('wiki.updated'), ' ', h('b', rel(pg.updated))),
        h('span', t('wiki.confidence'), ' ', h('b', t('wiki.conf.' + pg.confidence))),
        h('span', t('wiki.sources'), ' ', pg.sources.map((src, i) => h('span', h('code', src), i < pg.sources.length - 1 ? ' ' : '')))),
      pg.body.map(([k, v]) => k === 'ul' ? h('ul', v.map((x) => h('li', x))) : h(k, v))));
  }
  drawNav(); drawPage();
  return h('section.panel', h('div.grid.side-main', { style: { gap: 0 } }, h('div', { style: { borderRight: '1px solid var(--seam)' } }, nav), view));
}

async function drafts() {
  const list = await api.drafts();
  if (!list.length) return h('section.panel', emptyState(t('wiki.noDrafts')));
  const picked = new Set();
  let focus = list[0].id;
  const rows = h('div');
  const view = h('div.stack');
  const batchIn = h('input.input#batch', { placeholder: t('wiki.batchPh'), 'aria-describedby': 'batchHint', style: { maxWidth: '260px' } });

  function draw() {
    rows.replaceChildren(...list.map((d) => h('label.signal-item', { class: d.id === focus ? 'sel' : '', style: { cursor: 'pointer', gridTemplateColumns: '22px minmax(0,1fr) auto' }, onclick: (e) => { if (e.target.tagName !== 'INPUT') { focus = d.id; draw(); } } },
      h('input', { type: 'checkbox', checked: picked.has(d.id) || null, 'aria-label': t('wiki.pick', { n: d.n }), onchange: (e) => { e.target.checked ? picked.add(d.id) : picked.delete(d.id); syncBtns(); } }),
      h('div.s-title', h('span.id', d.n + '. '), d.path),
      h('span.tag', { class: d.kind === 'new' ? 'clear' : 'route' }, t('wiki.kind.' + d.kind)),
      h('div.s-meta', h('span', d.summary), h('span.faint', t('wiki.from', { num: api.session(d.from)?.num || '?' })),
        ['decisions', 'architecture', 'people'].includes(d.area) ? h('span.tag.danger', icon('lock'), t('wiki.coreArea')) : null))));
    const d = list.find((x) => x.id === focus) || list[0];
    view.replaceChildren(
      h('div.row.between', h('h3', d.path), h('span.faint', t('wiki.from', { num: api.session(d.from)?.num }))),
      h('p', d.summary),
      h('div.frontmatter', h('span', t('wiki.sources')), d.sources.map((x) => h('code', x))),
      diffView(d.diff));
  }
  const approveBtn = h('button.btn.go', { type: 'button', onclick: () => resolve('approve') });
  const changesBtn = h('button.btn', { type: 'button', onclick: () => toast(t('wiki.changesAsked')) }, t('wiki.requestChanges'));
  const discardBtn = h('button.btn.danger', { type: 'button', onclick: () => resolve('discard') });
  function syncBtns() {
    const n = picked.size;
    approveBtn.textContent = n ? t('wiki.approveN', { n }) : t('wiki.approveSel');
    discardBtn.textContent = n ? t('wiki.discardN', { n }) : t('wiki.discard');
    [approveBtn, discardBtn].forEach((b) => (b.disabled = !n));
    changesBtn.disabled = !n;
  }
  function resolve(decision) {
    const ids = [...picked];
    api.resolveDrafts(ids, decision);
    ids.forEach((id) => list.splice(list.findIndex((x) => x.id === id), 1));
    api.updateProject(api.project().id, { drafts: list.length });
    picked.clear();
    toast(decision === 'approve' ? t('wiki.approvedToast', { n: ids.length }) : t('wiki.discardedToast', { n: ids.length }), decision === 'discard' ? { label: t('common.undo'), run: () => toast(t('wiki.restored')) } : null);
    if (!list.length) { document.querySelector('.page')?.replaceChildren(h('section.panel', emptyState(t('wiki.noDrafts')))); return; }
    focus = list[0].id; draw(); syncBtns();
  }
  batchIn.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter') return;
    const m = batchIn.value.toLowerCase().match(/(approve|อนุมัติ|discard|ทิ้ง)\s*([\d,\s-]+)/);
    if (!m) { batchIn.setAttribute('aria-invalid', 'true'); toast(t('wiki.batchBad')); return; }
    batchIn.removeAttribute('aria-invalid');
    const nums = m[2].split(',').flatMap((x) => { const [a, b] = x.split('-').map(Number); return b ? Array.from({ length: b - a + 1 }, (_, i) => a + i) : [a]; });
    picked.clear(); list.filter((d) => nums.includes(d.n)).forEach((d) => picked.add(d.id));
    resolve(/approve|อนุมัติ/.test(m[1]) ? 'approve' : 'discard');
    batchIn.value = '';
  });
  draw(); syncBtns();
  return h('div.grid.two',
    h('section.panel',
      h('div.panel-head', h('h2', t('wiki.draftsTitle', { n: list.length })), h('span.sub', t('wiki.draftsSub'))),
      rows,
      h('div.panel-body.stack', { style: { borderTop: '1px solid var(--seam)' } },
        h('div.field', h('label', { for: 'batch' }, t('wiki.batchLabel')), batchIn, h('span.hint#batchHint', t('wiki.batchHint'))),
        h('div.row', approveBtn, changesBtn, h('div.red-zone', discardBtn)))),
    h('section.panel', h('div.panel-head', h('h2', t('wiki.preview'))), h('div.panel-body', view)));
}

function logView() {
  const now = Date.now();
  const lines = [
    [now - 60_000, 'agent', 'raw/2026-11-16-s-02.md', 'session 1.1 summary'],
    [now - 12 * 60_000, 'agent', '_drafts/1-token-rotation.md', 'draft from 1.1'],
    [now - 26 * 60_000, 'auto', 'lessons/csv-encoding.md', 'approved by Auto (reason: adds to lessons, no conflict)'],
    [now - 9 * 3600_000, 'nightly', 'raw/2026-11-16-git.md', 'nightly ingest: 14 notes, 3 drafts, 41.2k tokens, $0.06'],
    [now - 26 * 3600_000, 'mali', 'decisions/invoice-rounding.md', 'approved (edit)'],
  ];
  // Admission log: every save request with its verdict and reason (refused secrets stay hidden).
  const LAMP = { keep: 'clear', defer: 'caution', refuse: 'danger' };
  const admission = h('section.panel',
    h('div.panel-head', h('h2', t('wikiAdmit.logTitle')), h('span.sub', t('wikiAdmit.logSub'))),
    h('table.timetable', h('tbody', api.admissions().map((x) => h('tr',
      h('td.tt-time', dateTime(x.at)),
      h('td.tt-lamp', h('span.lamp', { class: LAMP[x.verdict] })),
      h('td',
        h('strong', { style: { fontWeight: 600 } }, t('wikiAdmit.' + x.verdict) + (x.override ? ' · ' + t('wikiAdmit.override') : '')), ' ',
        x.path ? h('code', x.path) : null, ' ', h('span.muted', '“' + x.summary + '”'),
        h('div.cell-sub', t('wikiAdmit.reason.' + x.reason, { src: api.session(x.from)?.num || x.from }))),
      h('td.tt-who.mono', api.session(x.from)?.num || ''))))));
  return h('div.stack', admission, h('section.panel',
    h('div.panel-head', h('h2', 'log.md'), h('span.sub', t('wiki.logSub'))),
    h('table.timetable', h('tbody', lines.map(([at, who, path, what]) => h('tr', h('td.tt-time', dateTime(at)), h('td', h('code', path), ' ', h('span.muted', what)), h('td.tt-who', h('span.mono', who))))))));
}

// Send an approved page to a shared wiki: the team's, or your own. hpk redacts
// project names, paths and people first; a person approves it there.
function promoteDrawer(pg) {
  let to = 'team';
  const opt = (v) => h('label.choice', h('input', { type: 'radio', name: 'promTo', value: v, checked: v === to || null, onchange: () => (to = v) }),
    h('span', h('b', t('wiki.promoteTo.' + v)), h('span.faint', ' · ' + t('wiki.promoteToSub.' + v))));
  openDrawer(t('wiki.promoteTitle', { title: pg.title }), h('div.stack', { style: { padding: '16px' } },
    h('p.muted', t('wiki.promoteBody')), opt('team'), opt('mine')),
  h('button.btn.primary', { type: 'button', onclick: async () => {
    try {
      const r = await fetch('/api/wiki/promote', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify({ project: api.project().id, page: pg.path, to }) });
      const out = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(out.error || r.statusText);
      closeDrawer(); toast(t('wiki.promoted', { n: out.n }));
    } catch (err) { toast(err.message); }
  } }, t('wiki.promote')));
}
