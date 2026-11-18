// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { t } from '../i18n.js';
import { h, icon, lamp, rel, pageHead, fmtTokens, fmtCost, privacyTag, avatar } from '../ui.js';

// Projects are either personal (only you) or a team's (members and roles).
// Each uses one Profile, which in turn uses one Process.
export default async function projects({ go }) {
  const list = await api.projects();
  const cur = api.state().projectId;

  const table = (rows) => h('div.table-wrap', h('table.t',
    h('thead', h('tr', ['name', 'chain', 'privacy', 'people', 'sessions', 'wiki', 'cost'].map((c) => h('th', { class: ['sessions', 'wiki', 'cost'].includes(c) ? 'num' : c === 'people' || c === 'wiki' ? 'hide-md' : '' }, t('projects.col.' + c))), h('th'))),
    h('tbody', rows.map((p) => {
      const prof = api.profileOf(p.id);
      const proc = api.processOf(prof.id);
      return h('tr.clickable', { class: p.id === cur ? 'sel' : '', onclick: () => { api.setProject(p.id); go('overview'); } },
        h('td', h('div.cell-title', p.name), h('div.cell-sub.mono', p.root)),
        h('td', h('a.chain-link', { href: '#/setup/effective', onclick: (e) => { e.stopPropagation(); api.setProject(p.id); } }, h('span.muted', proc.name), icon('chevron'), h('strong', prof.name))),
        h('td', privacyTag(p.privacy), p.modelPolicy === 'local_only' ? h('div.cell-sub', t('projects.localOnly')) : null),
        h('td.hide-md', h('span.row', { style: { gap: '2px' } }, p.members.slice(0, 4).map((m) => avatar(m, 22)), p.members.length > 4 ? h('span.faint', '+' + (p.members.length - 4)) : null)),
        h('td.num', h('span.row', { style: { justifyContent: 'flex-end', gap: '8px' } }, lamp(p.sessionsActive ? 'clear' : 'idle'), p.sessionsActive)),
        h('td.num.hide-md', p.wikiPages, p.drafts ? h('div.cell-sub', t('projects.draftsN', { n: p.drafts })) : null),
        h('td.num', fmtCost(p.cost30), h('div.cell-sub', fmtTokens(p.tokens30))),
        h('td', { style: { textAlign: 'right' } }, h('button.btn.sm', { type: 'button', onclick: (e) => { e.stopPropagation(); api.setProject(p.id); go('overview'); } }, p.id === cur ? t('projects.current') : t('projects.open'))));
    }))));

  const section = (kind) => {
    const rows = list.filter((p) => p.kind === kind);
    return h('section.panel',
      h('div.panel-head', h('h2', icon(kind === 'team' ? 'team' : 'lock'), t('projects.section.' + kind), h('span.tag', rows.length)), h('span.sub', t('projects.sectionSub.' + kind))),
      rows.length ? table(rows) : h('p.empty', t('projects.emptyKind.' + kind)));
  };

  return { el: h('div.page',
    pageHead(t('projects.title'), t('projects.lede'), [
      h('a.btn', { href: '#/setup' }, icon('sessions'), t('projects.manageChain')),
      h('a.btn.primary', { href: '#/projects/new' }, icon('plus'), t('projects.add'))]),
    list.some((p) => p.kind === 'team') ? section('team') : null,
    section('personal'),
    h('div.callout.route', icon('info'), h('div', t('projects.chainNote')))) };
}
