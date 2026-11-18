// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { t } from '../i18n.js';
import { h, icon, rel, fmtTokens, fmtCost, pageHead, sessionStatus, avatar, person, openDrawer, closeDrawer, toast, SESSION_LAMP, lamp } from '../ui.js';
import { planBoard } from './plan.js';

function buildTree(list) {
  const kids = {};
  list.forEach((x) => (kids[x.parent || 'root'] = kids[x.parent || 'root'] || []).push(x));
  return kids;
}

export function newChildDrawer(parentId) {
  const parent = api.session(parentId) || api.sessionsSync()[0];
  const titleIn = h('input.input#ncTitle', { placeholder: t('sessions.form.titlePh'), required: true });
  const brief = h('textarea.input#ncBrief', { placeholder: t('sessions.form.briefPh') });
  openDrawer(t('sessions.newChild'), h('form.stack', { style: { padding: '16px' }, onsubmit: (e) => { e.preventDefault(); submit(); } },
    h('p.muted', t('sessions.form.under', { num: parent.num, title: parent.title })),
    h('div.field', h('label', { for: 'ncTitle' }, t('sessions.form.title')), titleIn, h('span.hint', t('sessions.form.titleHint'))),
    h('div.field', h('label', { for: 'ncLane' }, t('sessions.form.lane')),
      h('select.input#ncLane', ['auth', 'billing', 'search', 'docs'].map((l) => h('option', { value: l }, l)))),
    h('div.field', h('label', { for: 'ncBrief' }, t('sessions.form.brief')), brief, h('span.hint', t('sessions.form.briefHint'))),
    h('div.grid.two', { style: { gap: '12px' } },
      h('div.field', h('label', { for: 'ncModel' }, t('sessions.form.model')), h('select.input#ncModel', ['sonnet', 'haiku', 'opus'].map((m) => h('option', m)))),
      h('div.field', h('label', { for: 'ncWs' }, t('sessions.form.workspace')), h('select.input#ncWs', ['worktree', 'scratch', 'directory'].map((m) => h('option', { value: m }, t('sessions.ws.' + m)))))),
    h('div.callout.route', icon('info'), h('div', t('sessions.form.claimNote')))),
  [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')),
    h('button.btn.primary', { type: 'button', onclick: submit }, t('sessions.form.start'))]);
  function submit() {
    if (!titleIn.value.trim()) { titleIn.focus(); titleIn.setAttribute('aria-invalid', 'true'); return; }
    closeDrawer();
    toast(t('sessions.form.started', { title: titleIn.value.trim() }));
  }
}

export default async function sessionsView({ go }) {
  const [list, lanes, approvals, claims] = await Promise.all([api.sessions(), api.lanes(), api.approvals(), api.claims()]);
  const kids = buildTree(list);
  let selected = list.find((x) => x.status === 'waiting')?.id || list[0].id;
  let tab = 'tree';

  const detail = h('section.panel');
  const treeBox = h('ul.tree', { role: 'tree', 'aria-label': t('sessions.treeAria') });

  function node(sess) {
    const k = SESSION_LAMP[sess.status];
    const children = kids[sess.id] || [];
    const li = h('li', { role: 'treeitem', 'aria-expanded': children.length ? 'true' : null },
      h('button.node', { type: 'button', class: sess.id === selected ? 'sel' : '', onclick: () => { selected = sess.id; drawTree(); drawDetail(); },
        style: { width: '100%', textAlign: 'left' } },
      h('span.id', sess.num),
      lamp(k, { blink: sess.status === 'waiting' }),
      h('span.n-title', sess.title),
      h('span.n-meta.hide-sm', sess.lane || t('sessions.coordinator')),
      avatar(sess.owner, 22)),
      children.length ? h('ul', { role: 'group' }, children.map(node)) : null);
    return li;
  }
  function drawTree() { treeBox.replaceChildren(...(kids.root || []).map(node)); }

  function drawDetail() {
    const s = api.session(selected);
    const owner = person(s.owner);
    const myClaims = claims.filter((c) => c.session === s.id);
    const waitingOn = approvals.filter((a) => a.session === s.id);
    const canChat = s.viaHpk;
    detail.replaceChildren(
      h('div.panel-head', h('h2', h('span.id', s.num), s.title), sessionStatus(s)),
      h('div.panel-body.stack',
        h('dl.facts', { style: { border: '0', background: 'transparent' } },
          h('div.fact', h('dt', t('sessions.owner')), h('dd', { style: { fontFamily: 'var(--font-ui)', fontSize: '14px' } }, avatar(s.owner, 22), owner.name)),
          h('div.fact', h('dt', t('sessions.heartbeat')), h('dd', rel(s.heartbeat))),
          h('div.fact', h('dt', t('sessions.tokens')), h('dd', fmtTokens(s.tokens))),
          h('div.fact', h('dt', t('sessions.cost')), h('dd', fmtCost(s.cost)))),
        h('table.t', h('tbody',
          h('tr', h('td.muted', t('sessions.model')), h('td', h('span.mono', s.model))),
          h('tr', h('td.muted', t('sessions.modeLabel')), h('td', t(`mode.${s.mode}.name`), s.autoLeft ? h('span.faint', ' · ' + t('mode.leftMin', { n: s.autoLeft })) : null)),
          h('tr', h('td.muted', t('sessions.workspace')), h('td', s.worktree ? h('code', s.worktree) : h('span.faint', '—'))),
          h('tr', h('td.muted', t('sessions.source')), h('td', s.viaHpk ? t('sessions.viaHpk') : t('sessions.viaOther', { src: s.source || 'cli' }))),
          h('tr', h('td.muted', t('sessions.claims')), h('td', myClaims.length ? myClaims.map((c) => h('div', h('span.id', c.id), ' ', c.paths.join(', '))) : h('span.faint', t('sessions.noClaims')))))),
        waitingOn.length ? h('div.callout.danger', icon('warn'), h('div', h('b', t('sessions.waitingOn', { n: waitingOn.length })), h('p', waitingOn[0].title))) : null,
        !canChat ? h('div.callout', icon('info'), h('div', t('sessions.readOnlyNote'))) : null,
        h('div.row',
          canChat ? h('a.btn.primary', { href: '#/sessions/' + s.id }, t('sessions.openChat')) : null,
          h('button.btn', { type: 'button', onclick: () => newChildDrawer(s.id) }, icon('plus'), t('sessions.newChild')),
          !canChat ? h('button.btn', { type: 'button', onclick: () => toast(t('sessions.askSent', { num: s.num })) }, t('sessions.askStatus')) : null,
          h('div.red-zone', s.status !== 'done' && s.status !== 'stopped' && canChat
            ? h('button.btn.danger', { type: 'button', onclick: () => { api.stopSession(s.id); toast(t('sessions.stopped', { num: s.num })); drawTree(); drawDetail(); } }, icon('stop'), t('sessions.stop'))
            : null))));
  }

  const tabs = h('div.tabs', { role: 'tablist' },
    ['tree', 'lanes'].map((k) => h('button', { role: 'tab', type: 'button', 'aria-selected': k === tab ? 'true' : 'false', onclick: () => { tab = k; swap(); } }, t('sessions.tab.' + k))));
  const body = h('div');
  function swap() {
    tabs.querySelectorAll('button').forEach((b, i) => b.setAttribute('aria-selected', ['tree', 'lanes'][i] === tab ? 'true' : 'false'));
    if (tab === 'tree') body.replaceChildren(h('div.grid.main-side', h('section.panel', h('div.panel-body', treeBox)), detail));
    else body.replaceChildren(h('section.panel.track-panel', planBoard()));
  }
  drawTree(); drawDetail(); swap();

  const el = h('div.page',
    pageHead(t('sessions.title'), t('sessions.lede'), [h('button.btn.primary', { type: 'button', onclick: () => newChildDrawer(selected) }, icon('plus'), t('sessions.newChild'))]),
    tabs, body);
  return { el };
}
