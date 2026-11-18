// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { t } from '../i18n.js';
import { h, icon, lamp, rel, pageHead, toast, avatar, signalGlyph, openDrawer, closeDrawer } from '../ui.js';

const LEVEL_LAMP = { danger: 'danger', caution: 'caution', related: 'route' };
const CLAIM_LAMP = { active: 'clear', 'waiting-human': 'danger', stale: 'caution', done: 'idle' };

function resolveDrawer(o, claims) {
  const a = claims.find((c) => c.id === o.a); const b = claims.find((c) => c.id === o.b);
  const sa = api.session(a.session); const sb = api.session(b.session);
  let choice = 'wait';
  const opts = ['wait', 'other', 'together', 'takeover'];
  openDrawer(t('board.dupTitle'), h('div.stack', { style: { padding: '16px' } },
    h('div.callout.danger', icon('warn'), h('div', h('b', o.files.join(', ')), h('p', t('board.dupBody', { a: sa.num, b: sb.num, owner: api.person(sb.owner).name, ago: rel(b.heartbeat) })))),
    h('div.choice', { role: 'radiogroup', 'aria-label': t('board.dupTitle') }, opts.map((k) => h('label',
      h('input', { type: 'radio', name: 'dup', value: k, checked: k === choice || null, onchange: () => (choice = k) }),
      h('span.c-title', t('board.opt.' + k + '.t'), k === 'takeover' || k === 'together' ? h('span.tag.danger', icon('lock'), t('board.needsHuman')) : null),
      h('span.c-desc', t('board.opt.' + k + '.d'))))),
    h('p.faint', { style: { fontSize: '12px' } }, t('board.dupAutoNote'))),
  [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')),
    h('button.btn.primary', { type: 'button', onclick: () => { closeDrawer(); toast(t('board.opt.' + choice + '.done')); } }, t('common.confirm'))]);
}

export default async function board() {
  const [lanes, sessions, claims, overlaps, approvals] = await Promise.all([api.lanes(), api.sessions(), api.claims(), api.overlaps(), api.approvals()]);

  const alerts = h('ul.signals', overlaps.map((o) => {
    const a = claims.find((c) => c.id === o.a); const b = claims.find((c) => c.id === o.b);
    const sa = api.session(a.session); const sb = api.session(b.session);
    return h('li.signal-item',
      h('span.sig', signalGlyph(LEVEL_LAMP[o.level] === 'route' ? 'clear' : LEVEL_LAMP[o.level], o.level === 'danger')),
      h('div.s-title', t('board.level.' + o.level), ' · ', h('span.mono', o.files.join(', '))),
      h('div.s-actions', o.level === 'danger'
        ? h('button.btn.sm', { type: 'button', onclick: () => resolveDrawer(o, claims) }, t('board.decide'))
        : h('button.btn.sm', { type: 'button', onclick: () => toast(t('board.acked')) }, t('board.ack'))),
      h('div.s-meta', h('span', h('span.id', sa.num), ' ', sa.title), icon('link'), h('span', h('span.id', sb.num), ' ', sb.title), h('span.faint', rel(o.found))));
  }));

  const whoIn = h('input.input#who', { placeholder: t('board.whoPh'), 'aria-label': t('board.whoPh'), style: { maxWidth: '320px' } });
  const whoOut = h('div', { 'aria-live': 'polite' });
  whoIn.addEventListener('input', () => {
    const q = whoIn.value.trim().toLowerCase();
    if (!q) { whoOut.replaceChildren(); return; }
    const hit = claims.filter((c) => c.paths.some((p) => p.toLowerCase().includes(q)) || c.task.toLowerCase().includes(q));
    whoOut.replaceChildren(hit.length
      ? h('p.muted', t('board.whoHit', { n: hit.length }), ' ', hit.map((c) => { const se = api.session(c.session); return h('span', h('span.id', se.num), ' ', api.person(se.owner).name, ' · ', rel(c.heartbeat), '; '); }))
      : h('p.status.clear', lamp('clear'), t('board.whoFree')));
  });

  const table = h('div.table-wrap', h('table.t',
    h('thead', h('tr', h('th', t('board.col.claim')), h('th', t('board.col.task')), h('th.hide-md', t('board.col.paths')), h('th', t('board.col.owner')), h('th', t('board.col.heartbeat')), h('th', t('board.col.status')))),
    h('tbody', claims.map((c) => {
      const se = api.session(c.session);
      const k = CLAIM_LAMP[c.status];
      return h('tr', h('td', h('span.id', c.id)),
        h('td', h('div.cell-title', c.task), h('div.cell-sub', h('a', { href: '#/sessions/' + se.id }, se.num), ' · ', c.lane)),
        h('td.hide-md', c.paths.map((p) => h('div', h('code', p)))),
        h('td', h('span.row', { style: { gap: '8px' } }, avatar(se.owner, 22), api.person(se.owner).name)),
        h('td.tnum', rel(c.heartbeat)),
        h('td', h('span.status', { class: k }, lamp(k, { blink: c.status === 'waiting-human' }), t('board.claim.' + c.status))));
    }))));

  const p = api.project();
  const el = h('div.page',
    pageHead(t('board.title'), t('board.lede', { file: p.board === 'markdown' ? 'BOARD.md' : 'board.db' }),
      [h('button.btn', { type: 'button', onclick: () => toast(t('board.rendered')) }, icon('file'), t('board.render'))]),
    h('section.panel', h('div.panel-head', h('h2', t('board.diagram')), h('span.sub', t('board.diagramSub'))),
      claimMap(lanes, claims, overlaps)),
    h('div.grid.main-side',
      h('section.panel', h('div.panel-head', h('h2', t('board.claims', { n: claims.length })), h('span.sub', t('board.staleAfter', { n: 30 }))), table),
      h('div.stack',
        h('section.panel', h('div.panel-head', h('h2', t('board.alerts')), h('span.sub', t('board.alertsSub'))), alerts),
        h('section.panel', h('div.panel-head', h('h2', t('board.who'))), h('div.panel-body.stack', h('p.muted', t('board.whoBody')), whoIn, whoOut)))));
  return { el };
}

// Who holds which files, by area: one outlined box per claim, a 2px state line on
// its left, and the overlap called out on the box itself.
function claimMap(lanes, claims, overlaps) {
  const clash = (c) => overlaps.find((o) => (o.a === c.id || o.b === c.id) && o.level !== 'related');
  const areas = [...new Set(claims.map((c) => c.lane || 'other'))];
  return h('div.claim-map', areas.map((area) => h('section.cm-area',
    h('h3.cm-name', area === 'other' ? t('board.otherArea') : area, h('span.faint', ' · ' + claims.filter((c) => (c.lane || 'other') === area).length)),
    h('div.cm-row', claims.filter((c) => (c.lane || 'other') === area).map((c) => {
      const se = api.session(c.session);
      const o = clash(c);
      return h('a.cm-box', { href: '#/sessions/' + c.session, class: (CLAIM_LAMP[c.status] || 'idle') + (o ? ' clash-' + o.level : '') },
        h('span.cm-title', lamp(CLAIM_LAMP[c.status] || 'idle', { blink: c.status === 'waiting-human' }), h('span', c.task)),
        h('span.cm-who', avatar(se?.owner || 'agent', 18), h('span', api.person(se?.owner)?.name || '—'), h('span.mono.faint', se ? se.num : ''), h('span.faint', rel(c.heartbeat))),
        h('span.cm-paths.mono', c.paths.join('  ')),
        o ? h('span.cm-clash', icon('warn'), t('board.clashWith', { files: o.files.join(', ') })) : null);
    })))));
}
