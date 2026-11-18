// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { isLive } from '../live.js';
import { t } from '../i18n.js';
import { h, icon, lamp, rel, pageHead, toast, avatar, clock, panel } from '../ui.js';

// Kept across redraws (live data refreshes the page after every change).
let lastTab = 'modes';

export default async function gates() {
  let tab = lastTab;
  const tabs = h('div.tabs', { role: 'tablist' });
  const body = h('div');
  const TABS = ['modes', 'redlines', 'audit', 'digest'];
  function drawTabs() {
    tabs.replaceChildren(...TABS.map((k) => h('button', { role: 'tab', type: 'button', 'aria-selected': k === tab ? 'true' : 'false', onclick: () => { tab = lastTab = k; drawTabs(); draw(); } }, t('gates.tab.' + k))));
  }
  async function draw() {
    if (tab === 'modes') body.replaceChildren(modes());
    if (tab === 'redlines') body.replaceChildren(await redlines());
    if (tab === 'audit') body.replaceChildren(await audit());
    if (tab === 'digest') body.replaceChildren(await digest());
  }
  drawTabs(); await draw();
  return { el: h('div.page', pageHead(t('gates.title'), t('gates.lede')), tabs, body) };
}

function modes() {
  const m = api.state().mode;
  const card = (v) => h('label',
    h('input', { type: 'radio', name: 'mode', value: v, checked: m.value === v || null, onchange: () => { api.setMode(v, v === 'auto' ? 120 : null, m.scope); toast(t('mode.saved', { mode: t(`mode.${v}.name`) })); } }),
    h('span.c-title', lamp(v === 'auto' ? 'clear' : v === 'ask' ? 'route' : 'idle'), t(`mode.${v}.name`), v === 'ask' ? h('span.tag', t('gates.default')) : null),
    h('span.c-desc', t(`mode.${v}.desc`)));
  const gatesRows = ['start', 'plan', 'lanes', 'childStuck', 'merge', 'wiki', 'delete', 'weekly'];
  // Points that stay with a human even in Auto (red lines or genuine questions).
  const HUMAN_IN_AUTO = new Set(['start', 'childStuck', 'merge', 'delete']);
  const pointRow = (k) => h('tr',
    h('td', h('div.cell-title', t(`gates.p.${k}.t`)), h('div.cell-sub', t(`gates.p.${k}.d`))),
    h('td', h('span.status.route', lamp('route'), t('gates.human'))),
    h('td', HUMAN_IN_AUTO.has(k)
      ? h('span.status.danger', lamp('danger'), t('gates.human'))
      : h('span.status.clear', lamp('clear'), t(`gates.p.${k}.auto`))));
  const pointsTable = h('div.table-wrap', h('table.t',
    h('thead', h('tr', h('th', t('gates.point')), h('th', t('mode.ask.name')), h('th', t('mode.auto.name')))),
    h('tbody', gatesRows.map(pointRow))));
  const modeBox = h('div.stack',
    h('div.choice', { role: 'radiogroup', 'aria-label': t('gates.modeTitle') }, ['manual', 'ask', 'auto'].map(card)),
    h('div.callout.danger', icon('warn'), h('div', h('b', t('mode.redLinesTitle')), h('p', t('mode.redLinesBody')))));
  return h('div.grid.two',
    panel(t('gates.modeTitle'), t('gates.modeSub'), modeBox),
    panel(t('gates.pointsTitle'), t('gates.pointsSub'), pointsTable, { flush: true }));
}

async function redlines() {
  const list = await api.redLines();
  const input = h('input.input#rlNew', { placeholder: t('gates.rlPh') });
  const rows = h('tbody');
  function draw(items) {
    rows.replaceChildren(...items.map((r) => h('tr',
      h('td', { style: { width: '28px' } }, lamp('danger')),
      h('td', h('div.cell-title', r.key === 'custom' ? r.text : t(`redlines.${r.key}.short`)), r.key !== 'custom' ? h('div.cell-sub', t(`redlines.${r.key}.long`)) : null),
      h('td', r.locked ? h('span.tag', icon('lock'), t('gates.builtIn')) : h('span.tag.route', t('gates.project'))),
      h('td', { style: { textAlign: 'right' } }, r.locked ? null : h('button.btn.sm.danger', { type: 'button', onclick: async () => { try { await api.removeRedLine(r.id); draw(await api.redLines()); toast(t('gates.rlRemoved')); } catch (err) { toast(err.message); } } }, t('common.remove'))))));
  }
  draw(list);
  return h('div.stack',
    h('div.callout.danger', icon('warn'), h('div', h('b', t('gates.rlTitle')), h('p', t('gates.rlBody')))),
    isLive() ? h('div.callout.route', icon('info'), h('div', t('live.rlHow'))) : null,
    h('section.panel', h('div.table-wrap', h('table.t', rows)),
      h('form.panel-body.row', { style: { borderTop: '1px solid var(--seam)' }, onsubmit: async (e) => { e.preventDefault(); if (!input.value.trim()) return; try { await api.addRedLine(input.value.trim()); input.value = ''; draw(await api.redLines()); toast(t('gates.rlAdded')); } catch (err) { toast(err.message); } } },
        h('label.sr-only', { for: 'rlNew' }, t('gates.rlPh')), h('div', { style: { flex: 1, minWidth: '240px' } }, input), h('button.btn', { type: 'submit' }, icon('plus'), t('gates.rlAdd')))));
}

async function audit() {
  const list = await api.audit();
  let who = 'all';
  const rows = h('tbody');
  function draw() {
    rows.replaceChildren(...list.filter((a) => who === 'all' || (who === 'auto' ? a.actor === 'auto' : who === 'human' ? !['agent', 'auto'].includes(a.actor) : a.actor === 'agent')).map((a) => {
      const se = a.session ? api.session(a.session) : null;
      const k = a.result === 'denied' ? 'danger' : a.result === 'waiting' || a.result === 'asked human' ? 'caution' : 'clear';
      return h('tr',
        h('td.mono.muted', clock(a.at)),
        h('td', h('span.row', { style: { gap: '8px' } }, avatar(a.actor, 22), h('span', t('audit.actor.' + (['agent', 'auto'].includes(a.actor) ? a.actor : 'human'))))),
        h('td', h('span.tag', t('audit.action.' + a.action))),
        h('td', h('div', a.target), a.reason ? h('div.cell-sub', t('audit.reason', { r: a.reason })) : null),
        h('td.hide-md', se ? h('span.id', se.num) : '—'),
        h('td', h('span.status', { class: k }, lamp(k), a.result)));
    }));
  }
  draw();
  const seg = h('div.seg', { role: 'group', 'aria-label': t('audit.filter') }, ['all', 'human', 'auto', 'agent'].map((k) => h('button', { type: 'button', 'aria-pressed': k === who ? 'true' : 'false', onclick: (e) => {
    who = k; seg.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', 'false')); e.currentTarget.setAttribute('aria-pressed', 'true'); draw();
  } }, t('audit.f.' + k))));
  return h('section.panel', h('div.panel-head', h('h2', t('audit.title')), seg),
    h('div.table-wrap', h('table.t', h('thead', h('tr', ['time', 'actor', 'action', 'target', 'session', 'result'].map((c) => h('th', { class: c === 'session' ? 'hide-md' : '' }, t('audit.col.' + c))))), rows)),
    h('p.faint', { style: { padding: '10px 16px', fontSize: '12px', borderTop: '1px solid var(--seam)' } }, t('audit.files')));
}

async function digest() {
  const list = await api.autoDigest();
  const box = h('ul.signals');
  function draw(items) {
    box.replaceChildren(...items.map((d) => h('li.signal-item',
      h('span.id', d.n),
      h('div.s-title', d.what),
      h('div.s-actions', d.revertable ? h('button.btn.sm', { type: 'button', onclick: async () => { api.revertAuto(d.id); draw(await api.autoDigest()); toast(t('digest.reverted', { n: d.n })); } }, icon('undo'), t('digest.revert')) : h('span.faint', { style: { fontSize: '12px' } }, t('digest.noRevert'))),
      h('div.s-meta', h('span', h('span.id', api.session(d.session)?.num || (d.session ? d.session.slice(0, 8) : '—'))), h('span', t('audit.reason', { r: d.why })), h('span.faint', rel(d.at))))));
  }
  draw(list);
  const input = h('input.input', { placeholder: t('digest.batchPh'), 'aria-label': t('digest.batchPh'), style: { maxWidth: '240px' } });
  return h('section.panel',
    h('div.panel-head', h('h2', t('digest.title', { n: list.length })), h('span.sub', t('digest.sub'))),
    list.length ? box : h('p.muted', { style: { padding: '16px' } }, t('live.digestEmpty')),
    isLive() ? null : h('div.panel-body.row', { style: { borderTop: '1px solid var(--seam)' } }, input, h('button.btn', { type: 'button', onclick: () => toast(t('digest.batchDone')) }, t('digest.batchRun')), h('span.faint', { style: { fontSize: '12px' } }, t('digest.how'))));
}
