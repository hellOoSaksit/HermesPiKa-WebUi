// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { t } from '../i18n.js';
import { h, icon, pageHead, toast, signalGlyph, rel, emptyState, leave } from '../ui.js';
import { APPROVAL_LAMP, approvalHref, queueKeys } from './overview.js';
import { commandRisks } from '../shell.js';

export default async function approvals({ go }) {
  await api.approvals();
  let filter = 'all';
  let cursor = 0;
  const list = h('ul.signals', { role: 'listbox', tabindex: 0, 'aria-label': t('approvals.queue') });
  const detail = h('section.panel#apDetail', { tabindex: -1, 'aria-live': 'polite' });
  const narrow = () => window.matchMedia('(max-width: 1180px)').matches;

  const items = () => api.approvalsSync().filter((a) => filter === 'all' || (filter === 'redline' ? a.kind === 'permission' : filter === 'drafts' ? a.kind === 'draft' : a.kind !== 'permission' && a.kind !== 'draft'));

  // Only red lines can be approved or denied here; everything else opens where it is decided.
  function decide(a, decision) {
    if (!a || a.kind !== 'permission' || (decision !== 'approve' && decision !== 'deny')) return;
    leave(document.getElementById('ap-' + a.id), decision === 'approve', () => {
      api.resolveApproval(a.id, decision);
      toast(decision === 'deny' ? t('approvals.denied') : t('approvals.approved'), null, decision === 'deny' ? 'deny' : 'ok');
      draw();
      list.focus();
    });
  }
  const open = (a) => {
    if (!a) return;
    if (a.kind === 'permission') { detail.scrollIntoView({ block: 'start', behavior: 'smooth' }); detail.focus({ preventScroll: true }); return; }
    go(approvalHref(a).slice(2));
  };

  function draw() {
    const all = items();
    if (!all.length) {
      list.replaceChildren(h('li', emptyState(t('approvals.empty'))));
      list.removeAttribute('aria-activedescendant');
      detail.replaceChildren(h('div.panel-body', h('p.muted', t('approvals.emptyDetail'))));
      return;
    }
    cursor = Math.min(cursor, all.length - 1);
    list.replaceChildren(...all.map((a, i) => {
      const sess = api.session(a.session);
      return h('li.signal-item', { id: 'ap-' + a.id, class: i === cursor ? 'sel' : '', role: 'option', 'aria-selected': i === cursor ? 'true' : 'false', style: { cursor: 'pointer' },
        onclick: () => { cursor = i; draw(); if (narrow()) detail.scrollIntoView({ block: 'start', behavior: 'smooth' }); } },
      h('span.sig', signalGlyph(APPROVAL_LAMP[a.kind], a.kind === 'permission')),
      h('div.s-title', a.title),
      h('span.id', '#' + (i + 1)),
      h('div.s-meta',
        a.redLine ? h('span.tag.danger', icon('warn'), t('redlines.' + a.redLine + '.short')) : h('span.tag', { class: APPROVAL_LAMP[a.kind] }, t('approvals.kind.' + a.kind)),
        sess ? h('span', h('span.id', sess.num), ' ', sess.title) : null,
        h('span.faint', rel(a.created))));
    }));
    list.setAttribute('aria-activedescendant', 'ap-' + all[cursor].id);
    const a = all[cursor];
    const sess = api.session(a.session);
    const isPerm = a.kind === 'permission';
    detail.replaceChildren(
      h('div.panel-head', h('h2', t('approvals.kind.' + a.kind)), h('span.sub', rel(a.created))),
      h('div.panel-body.stack',
        h('h3', { style: { fontSize: '16px' } }, a.title),
        isPerm ? cmdView(a.detail) : h('p', a.detail),
        a.redLine ? h('div.callout.danger', icon('warn'), h('div', h('b', t('redlines.' + a.redLine + '.short')), h('p', t('approvals.redLineNote')))) : null,
        sess ? h('table.t', h('tbody',
          h('tr', h('td.muted', t('approvals.session')), h('td', h('a', { href: '#/sessions/' + sess.id }, h('span.id', sess.num), ' ', sess.title))),
          h('tr', h('td.muted', t('approvals.owner')), h('td', api.person(sess.owner)?.name)),
          h('tr', h('td.muted', t('approvals.whoCan')), h('td', isPerm ? t('approvals.whoCanRed') : t('approvals.whoCanAny'))))) : null,
        isPerm
          ? h('div.row',
            h('button.btn.go', { type: 'button', onclick: () => decide(a, 'approve') }, icon('check'), t('approvals.approve'), h('kbd.keyhint', 'a')),
            h('button.btn', { type: 'button', onclick: () => decide(a, 'approve') }, t('chat.allowSession')),
            h('div.red-zone', h('button.btn.danger', { type: 'button', onclick: () => decide(a, 'deny') }, icon('x'), t('approvals.deny'), h('kbd.keyhint', 'd'))))
          : h('div.row', h('button.btn.primary', { type: 'button', onclick: () => open(a) }, t('approvals.open.' + a.kind)))));
  }

  queueKeys(list, {
    count: () => items().length,
    getCursor: () => cursor,
    setCursor: (c) => { cursor = c; draw(); },
    onApprove: (c) => decide(items()[c], 'approve'),
    onDeny: (c) => decide(items()[c], 'deny'),
    onOpen: (c) => open(items()[c]),
  });

  const seg = h('div.seg', { role: 'group', 'aria-label': t('approvals.filter') },
    ['all', 'redline', 'drafts', 'other'].map((k) => h('button', { type: 'button', 'aria-pressed': k === filter ? 'true' : 'false', onclick: (e) => {
      filter = k; cursor = 0; seg.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', 'false')); e.currentTarget.setAttribute('aria-pressed', 'true'); draw();
    } }, t('approvals.f.' + k))));

  draw();
  setTimeout(() => list.focus({ preventScroll: true }), 50);
  const el = h('div.page',
    pageHead(t('approvals.title'), t('approvals.lede'), [seg]),
    h('div.grid.main-side',
      h('section.panel', h('div.panel-head', h('h2', t('approvals.queue')), h('span.sub.keyhint', h('kbd', 'j'), ' ', h('kbd', 'k'), ' ', t('keys.move'))), list),
      detail));
  return { el };
}

// A chained command is shown one part per row with the red line each part touches.
function cmdView(line) {
  const parts = commandRisks(line);
  if (parts.length < 2) return h('pre.yaml', line);
  return h('ol.cmd-parts', { 'aria-label': t('chat.permit.parts', { n: parts.length }) }, parts.map((x) => h('li', { class: x.risk ? 'risky' : '' },
    h('code', x.cmd), x.op ? h('span.cmd-op.mono', x.op) : null,
    x.risk ? h('span.tag.danger', icon('warn'), t('redlines.' + x.risk + '.short')) : h('span.tag', t('chat.permit.safe')))));
}
