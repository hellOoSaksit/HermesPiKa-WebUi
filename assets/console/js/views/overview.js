// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { t } from '../i18n.js';
import { h, icon, lamp, rel, clock, fmtTokens, fmtCost, panel, signalGlyph, avatar, toast, leave, countUp } from '../ui.js';
import { bus } from '../data.js';
import { planBoard } from './plan.js';
import { newChildDrawer } from './sessions.js';

export const APPROVAL_LAMP = { permission: 'danger', duplicate: 'danger', question: 'caution', draft: 'caution', stale: 'caution' };
export const approvalHref = (a) => (a.kind === 'draft' ? '#/wiki/drafts' : a.kind === 'duplicate' || a.kind === 'stale' ? '#/board' : '#/sessions/' + a.session);

// Keyboard for a queue list: only active while the list itself has focus.
export function queueKeys(listEl, { count, getCursor, setCursor, onApprove, onDeny, onOpen }) {
  listEl.addEventListener('keydown', (e) => {
    if (e.target !== listEl || e.metaKey || e.ctrlKey || e.altKey) return;
    const n = count();
    if (!n) return;
    const c = getCursor();
    if (e.key === 'j' || e.key === 'ArrowDown') { setCursor(Math.min(n - 1, c + 1)); e.preventDefault(); }
    else if (e.key === 'k' || e.key === 'ArrowUp') { setCursor(Math.max(0, c - 1)); e.preventDefault(); }
    else if (e.key === 'a') { onApprove(c); e.preventDefault(); }
    else if (e.key === 'd') { onDeny(c); e.preventDefault(); }
    else if (e.key === 'Enter') { onOpen(c); e.preventDefault(); }
  });
}

export function approvalItem(a, { onDone, selected = false, id } = {}) {
  const sess = api.session(a.session);
  const isPerm = a.kind === 'permission';
  const decide = (d) => leave(row, d !== 'deny', () => { api.resolveApproval(a.id, d); toast(d === 'deny' ? t('approvals.denied') : t('approvals.approved'), null, d === 'deny' ? 'deny' : 'ok'); onDone && onDone(); });
  const actions = isPerm
    ? h('div.s-actions', h('button.btn.sm.go', { type: 'button', tabindex: -1, onclick: () => decide('approve') }, t('approvals.approve')),
      h('span', { style: { width: '14px' } }),
      h('button.btn.sm.danger', { type: 'button', tabindex: -1, onclick: () => decide('deny') }, t('approvals.deny')))
    : h('div.s-actions', h('a.btn.sm', { href: approvalHref(a), tabindex: -1 }, t('approvals.open.' + a.kind)));
  const row = h('li.signal-item', { id, role: 'option', 'aria-selected': selected ? 'true' : 'false', class: selected ? 'sel' : '' },
    h('span.sig', signalGlyph(APPROVAL_LAMP[a.kind], isPerm)),
    h('div.s-title', a.title),
    actions,
    h('div.s-meta',
      a.redLine ? h('span.tag.danger', icon('warn'), t('redlines.' + a.redLine + '.short')) : h('span.tag', { class: APPROVAL_LAMP[a.kind] }, t('approvals.kind.' + a.kind)),
      sess ? h('a', { href: '#/sessions/' + sess.id, tabindex: -1 }, h('span.id', sess.num), ' ', sess.title) : null,
      h('span.faint', rel(a.created))),
    isPerm ? h('code.s-cmd', a.detail) : null);
  return row;
}

export default async function overview({ go }) {
  const [lanes, sessions, approvals, events] = await Promise.all([api.lanes(), api.sessions(), api.approvals(), api.events()]);
  const p = api.project();
  const st = api.state();
  const running = sessions.filter((x) => x.status === 'running').length;
  const tokens = sessions.reduce((n, x) => n + x.tokens, 0);
  // Sessions whose price is not reported are counted apart, never as $0.
  // Live: what the project spent today (hooks record every request); demo: the sessions shown.
  const cost = st.budget.spentToday != null ? st.budget.spentToday : sessions.reduce((n, x) => n + (x.cost ?? 0), 0);
  const unpriced = sessions.filter((x) => x.cost == null).length;

  // One plain sentence instead of a strip of numbers.
  const costEl = h('b');
  const [costPre, costPost] = (st.budget.daily
    ? t('overview.summaryCost', { v: '\u0000', b: fmtCost(st.budget.daily) })
    : t('overview.summaryCostNoCap', { v: '\u0000' })).split('\u0000');
  const summary = h('p.ov-summary',
    approvals.length ? h('span.ov-hot', lamp('danger'), t('overview.summaryNeeds', { n: approvals.length })) : h('span', lamp('clear'), t('overview.summaryClear')),
    h('span', t('overview.summaryRunning', { n: running, total: sessions.length })),
    // No session has a known price (e.g. live data before pricing exists): say nothing rather than "$0".
    st.budget.spentToday == null && unpriced === sessions.length && sessions.length
      ? h('span.faint.cost-unknown', t('overview.costUnknownN', { n: unpriced }))
      : h('span', costPre, countUp(costEl, cost, (v) => fmtCost(v)), costPost, unpriced ? h('span.faint.cost-unknown', ' ' + t('overview.costUnknownN', { n: unpriced })) : null));

  const trackPanel = h('section.panel.track-panel', { 'aria-labelledby': 'trkTitle' },
    h('div.panel-head', h('h2#trkTitle', t('overview.planTitle')), h('a.btn.sm.ghost', { href: '#/board' }, t('overview.openWork'), icon('chevron'))),
    planBoard());

  // Needs you — a focusable queue; the first thing on the page.
  let cursor = 0;
  const list = h('ul.signals', { role: 'listbox', tabindex: 0, 'aria-label': t('overview.needsTitle') });
  const items = () => api.approvalsSync().slice(0, 4);
  function fill() {
    const all = items();
    cursor = Math.min(cursor, Math.max(0, all.length - 1));
    list.replaceChildren(...all.map((a, i) => approvalItem(a, { id: 'ov-' + a.id, selected: i === cursor, onDone: fill })));
    if (all[cursor]) list.setAttribute('aria-activedescendant', 'ov-' + all[cursor].id);
    if (!all.length) list.replaceChildren(h('li.empty', t('approvals.empty')));
  }
  fill();
  queueKeys(list, {
    count: () => items().length, getCursor: () => cursor, setCursor: (c) => { cursor = c; fill(); },
    onApprove: (c) => { const a = items()[c]; if (a?.kind === 'permission') leave(list.children[c], true, () => { api.resolveApproval(a.id, 'approve'); toast(t('approvals.approved')); fill(); }); },
    onDeny: (c) => { const a = items()[c]; if (a?.kind === 'permission') leave(list.children[c], false, () => { api.resolveApproval(a.id, 'deny'); toast(t('approvals.denied'), null, 'deny'); fill(); }); },
    onOpen: (c) => { const a = items()[c]; if (a) location.hash = a.kind === 'permission' ? '#/approvals' : approvalHref(a); },
  });

  const needs = h('section.panel.needs-panel', { 'aria-labelledby': 'needsTitle' },
    h('div.panel-head', h('h2#needsTitle', t('overview.needsTitle')),
      approvals.length > 4 ? h('a.btn.sm.ghost', { href: '#/approvals' }, t('overview.allApprovals', { n: approvals.length }), icon('chevron')) : null),
    list);

  const eventRow = (e, fresh = false) => {
    const sess = api.session(e.session);
    return h('tr', { class: fresh ? 'arrive' : '' },
      h('td.tt-time', clock(e.at)),
      h('td.tt-lamp', lamp(e.lamp)),
      h('td', sess ? h('a', { href: '#/sessions/' + sess.id, style: { color: 'var(--text)', textDecoration: 'none' } }, e.text) : e.text),
      h('td.tt-who.hide-sm', avatar(e.who, 22)));
  };
  const ttBody = h('tbody', events.map((e) => eventRow(e)));
  const tt = h('table.timetable', ttBody);
  const onEvent = (ev) => {
    ttBody.prepend(eventRow(ev.detail, true));
    while (ttBody.children.length > 12) ttBody.lastElementChild.remove();
  };
  bus.addEventListener('event', onEvent);

  // Recent activity is folded away: open it when you want it.
  const activity = h('details.panel.activity',
    h('summary.panel-head', h('h2', icon('chevron', 'chev'), t('overview.movements')),
      events[0] ? h('span.sub.ov-last', clock(events[0].at) + ' · ' + events[0].text) : null),
    tt);

  const el = h('div.page',
    h('div.page-head', h('div', h('h1', p.name), summary),
      h('div.page-actions', h('a.btn', { href: '#/chat/new' }, icon('plus'), t('chat.newChat')))),
    needs,
    trackPanel,
    activity);
  return { el, cleanup: () => bus.removeEventListener('event', onEvent) };
}
