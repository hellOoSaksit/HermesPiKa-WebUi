// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { isLive, teamMode } from '../live.js';
import { t } from '../i18n.js';
import { h, icon, lamp, pageHead, toast, avatar, panel, openDrawer, closeDrawer } from '../ui.js';
import { devTable, inviteBox, laterPanel } from './teamserver.js';

const ROLES = ['owner', 'maintainer', 'member', 'viewer'];
const APPROVE = {
  wikiCore: ['owner', 'maintainer'],
  redLines: ['owner', 'maintainer'],
  promote: ['owner'],
  drafts: ['owner', 'maintainer', 'member'],
  duplicate: ['owner', 'maintainer', 'member'],
};

export default async function team() {
  if (!api.feature('team')) return { el: h('div.page', pageHead(t('team.title'), t('team.lede')), laterPanel()) };
  if (isLive() && teamMode()) return liveTeam();
  if (isLive()) {
    // One person until team mode (docs/DESIGN.md §19, M9): say so instead of showing made-up members.
    const me = api.me();
    const mine = api.sessionsSync().filter((x) => x.owner === me.id && x.status !== 'done').length;
    return { el: h('div.page', pageHead(t('team.title'), t('team.lede')),
      h('div.callout.route', icon('info'), h('div', h('b', t('live.soloTitle')), h('p', t('live.soloBody')))),
      panel(t('team.members', { n: 1 }), null, h('div.table-wrap', h('table.t',
        h('thead', h('tr', h('th', t('team.col.person')), h('th', t('team.col.role')), h('th.hide-md', t('team.col.sessions')))),
        h('tbody', h('tr',
          h('td', h('span.row', { style: { gap: '10px' } }, avatar(me.id), h('div.cell-title', me.name))),
          h('td', t('roles.owner')),
          h('td.hide-md.num', String(mine)))))), { flush: true })) };
  }
  const proj = api.project();
  const people = (await api.people()).filter((p) => proj.members.includes(p.id));
  const on = api.state().teamMode;
  if (proj.kind === 'personal') {
    return { el: h('div.page', pageHead(t('team.title'), t('team.lede')),
      h('div.callout', icon('lock'), h('div', h('b', t('team.personalTitle', { name: proj.name })), h('p', t('team.personalBody')))),
      h('div.row', h('a.btn', { href: '#/projects' }, t('team.seeTeamProjects')))) };
  }

  const members = h('div.table-wrap', h('table.t',
    h('thead', h('tr', h('th', t('team.col.person')), h('th', t('team.col.role')), h('th.hide-md', t('team.col.sessions')), h('th.hide-md', t('team.col.budget')), h('th'))),
    h('tbody', people.map((p) => h('tr',
      h('td', h('span.row', { style: { gap: '10px' } }, avatar(p.id), h('span', h('div.cell-title', p.name), h('div.cell-sub.mono', p.id + '@example.test')))),
      h('td', h('label.sr-only', { for: 'role-' + p.id }, t('team.col.role')),
        h('select.input#role-' + p.id, { style: { width: '150px' }, disabled: p.role === 'owner' || null, onchange: (e) => toast(t('team.roleSaved', { name: p.name, role: t('roles.' + e.target.value) })) },
          ROLES.map((r) => h('option', { value: r, selected: r === p.role || null }, t('roles.' + r))))),
      h('td.hide-md.num', api.sessionsSync().filter((s) => s.owner === p.id && s.status !== 'done').length),
      h('td.hide-md.num', '$' + (p.role === 'viewer' ? '0' : p.role === 'member' ? '40' : '120') + t('team.perMonth')),
      h('td', { style: { textAlign: 'right' } }, p.role === 'owner' ? null : h('button.btn.sm.danger', { type: 'button', onclick: () => toast(t('team.removeAsk', { name: p.name })) }, t('common.remove'))))))));

  const matrix = h('div.table-wrap', h('table.t',
    h('thead', h('tr', h('th', t('team.approves')), ROLES.map((r) => h('th', t('roles.' + r))))),
    h('tbody', Object.entries(APPROVE).map(([k, who]) => h('tr',
      h('td', h('div.cell-title', t('team.ap.' + k + '.t')), h('div.cell-sub', t('team.ap.' + k + '.d'))),
      ROLES.map((r) => h('td', who.includes(r) ? h('span.status.clear', lamp('clear'), t('common.yes')) : h('span.faint', '—'))))))));

  function invite() {
    openDrawer(t('team.invite'), h('form.stack', { style: { padding: '16px' }, onsubmit: (e) => e.preventDefault() },
      h('div.field', h('label', { for: 'invEmail' }, t('team.email')), h('input.input#invEmail', { type: 'email', placeholder: 'name@example.test' })),
      h('div.field', h('label', { for: 'invRole' }, t('team.col.role')), h('select.input#invRole', ROLES.slice(1).map((r) => h('option', { value: r }, t('roles.' + r))))),
      h('div.callout.route', icon('info'), h('div', t('team.inviteNote')))),
    [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')), h('button.btn.primary', { type: 'button', onclick: () => { closeDrawer(); toast(t('team.invited')); } }, t('team.send'))]);
  }

  return { el: h('div.page',
    pageHead(t('team.title'), t('team.lede'), [
      h('label.switch', h('input', { type: 'checkbox', checked: on || null, onchange: (e) => { api.setTeamMode(e.target.checked); toast(e.target.checked ? t('team.on') : t('team.off')); } }), t('team.mode')),
      h('button.btn.primary', { type: 'button', onclick: invite }, icon('plus'), t('team.invite'))]),
    h('div.callout', icon('key'), h('div', h('b', t('team.authTitle')), h('p', t('team.authBody')))),
    panel(t('team.members', { n: people.length }), null, members, { flush: true }),
    panel(t('team.matrix'), t('team.matrixSub'), matrix, { flush: true })) };
}

// Team mode (hpk team init): the real members from team.yaml. Owners invite,
// change roles, remove people and issue new tokens; tokens are shown once.
async function liveTeam(shown) {
  const me = api.me();
  const owner = me.role === 'owner';
  const res = await fetch('/api/team').then((r) => r.json());
  const post = async (path, body) => {
    const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify(body) });
    const out = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(out.error || r.statusText);
    return out;
  };
  const tokenBox = h('div');
  const showToken = (id, token) => {
    if (!token) return;
    tokenBox.replaceChildren(h('div.callout.caution', icon('key'), h('div',
      h('b', t('team.tokenOnce', { id })), h('pre', { style: { userSelect: 'all' } }, token), h('p.faint', t('team.tokenOnceHint')))));
  };
  if (shown) showToken(shown.id, shown.token);
  const usd = (v) => '$' + Number(v || 0).toFixed(2);
  const editCaps = (m) => {
    const d = h('input.input#capDay', { inputmode: 'decimal', value: String(m.budgetDaily || 0) });
    const mo = h('input.input#capMonth', { inputmode: 'decimal', value: String(m.budgetMonthly || 0) });
    openDrawer(t('team.capsFor', { id: m.name }), h('div.stack', { style: { padding: '16px' } },
      h('div.field', h('label', { for: 'capDay' }, t('team.capDay')), d),
      h('div.field', h('label', { for: 'capMonth' }, t('team.capMonth')), mo),
      h('p.faint', { style: { fontSize: '12px' } }, t('team.capsHint'))),
    h('button.btn.primary', { type: 'button', onclick: async () => {
      const daily = Number(d.value), monthly = Number(mo.value);
      if (![daily, monthly].every((n) => Number.isFinite(n) && n >= 0)) { toast(t('live.usdNumber')); return; }
      try { await post('/api/team/budget', { id: m.id, daily, monthly }); closeDrawer(); redraw(); } catch (err) { toast(err.message); }
    } }, t('common.save')));
  };
  const redraw = async (next) => { const page = await liveTeam(next); document.querySelector('#main .page').replaceWith(page.el); };
  const rows = (res.members || []).map((m) => h('tr',
    h('td', h('span.row', { style: { gap: '10px' } }, avatar(m.id), h('div', h('div.cell-title', m.name), h('div.cell-sub.mono', m.id)))),
    h('td', owner && m.id !== me.id
      ? h('select.input', { 'aria-label': t('team.col.role'), onchange: async (e) => { try { await post('/api/team/role', { id: m.id, role: e.target.value }); toast(t('settings.saved')); } catch (err) { toast(err.message); redraw(); } } },
        ROLES.map((r) => h('option', { value: r, selected: r === m.role || null }, t('roles.' + r))))
      : t('roles.' + m.role)),
    h('td.hide-md', (m.projects && m.projects.length) ? m.projects.join(', ') : t('team.allProjects')),
    h('td.hide-md', h('div.mono', usd(m.spentToday) + (m.budgetDaily ? ' / ' + usd(m.budgetDaily) : '')),
      h('div.cell-sub', m.budgetDaily || m.budgetMonthly ? t('team.capLine', { month: usd(m.spentMonth), cap: m.budgetMonthly ? usd(m.budgetMonthly) : '—' }) : t('team.noCap'))),
    h('td', { style: { textAlign: 'right', whiteSpace: 'nowrap' } }, owner ? [
      h('button.btn.sm.ghost', { type: 'button', title: t('ts.inviteHint'), onclick: async () => {
        try { const out = await post('/api/team/join-code', { id: m.id }); tokenBox.replaceChildren(inviteBox(out)); tokenBox.scrollIntoView({ block: 'nearest' }); } catch (err) { toast(err.message); }
      } }, t('ts.makeCode')),
      h('button.btn.sm.ghost', { type: 'button', onclick: () => editCaps(m) }, t('team.caps')),
      res.auth === 'token' ? h('button.btn.sm.ghost', { type: 'button', onclick: async () => {
        if (!confirm(t('team.rotateConfirm', { id: m.id }))) return;
        try { const out = await post('/api/team/rotate', { id: m.id }); showToken(m.id, out.token); } catch (err) { toast(err.message); }
      } }, t('team.newToken')) : null,
      m.id !== me.id ? h('button.btn.sm.danger', { type: 'button', onclick: async () => {
        if (!confirm(t('team.removeConfirm', { id: m.id }))) return;
        try { await post('/api/team/remove', { id: m.id }); redraw(); } catch (err) { toast(err.message); }
      } }, t('team.remove')) : null] : null)));
  const idIn = h('input.input#tmId', { placeholder: 'name@example.com', autocomplete: 'off' });
  const nameIn = h('input.input#tmName', { autocomplete: 'off' });
  const roleIn = h('select.input#tmRole', ROLES.map((r) => h('option', { value: r, selected: r === 'member' || null }, t('roles.' + r))));
  const projIn = h('input.input#tmProj', { placeholder: t('team.allProjects') });
  const invite = owner ? panel(t('team.invite'), t('team.inviteSub'), h('div.stack',
    h('div.grid.two', h('div.field', h('label', { for: 'tmId' }, t('team.id')), idIn), h('div.field', h('label', { for: 'tmName' }, t('team.name')), nameIn)),
    h('div.grid.two', h('div.field', h('label', { for: 'tmRole' }, t('team.col.role')), roleIn), h('div.field', h('label', { for: 'tmProj' }, t('team.projects')), projIn)),
    h('div.row', h('button.btn.primary', { type: 'button', onclick: async () => {
      const projects = projIn.value.split(',').map((x) => x.trim()).filter(Boolean);
      const id = idIn.value.trim();
      try { const out = await post('/api/team/invite', { id, name: nameIn.value.trim(), role: roleIn.value, projects }); await redraw({ id, token: out.token }); } catch (err) { toast(err.message); }
    } }, icon('plus'), t('team.add'))))) : null;
  // Joined devices (a person's own hpk): owners see all, everyone sees their own.
  const devs = owner ? await fetch('/api/team/devices').then((r) => r.json()).catch(() => ({ devices: [] }))
    : await fetch('/api/me/devices').then((r) => r.json()).catch(() => ({ devices: [] }));
  const devicesPanel = panel(owner ? t('ts.allDevices') : t('ts.myDevices'),
    owner ? ((devs.allowIPs && devs.allowIPs.length) ? t('ts.allowIPs', { list: devs.allowIPs.join(', ') }) : t('ts.noAllowIPs')) : t('ts.myDevicesSub'),
    devTable(devs.devices || [], devs.current, owner, async (d) => {
      if (!confirm(t('ts.revokeConfirm', { name: d.name + ' (' + d.member + ')' }))) return;
      try { await post(owner ? '/api/team/devices/revoke' : '/api/me/devices/revoke', { id: d.id }); redraw(); } catch (err) { toast(err.message); }
    }), { flush: true });
  return { el: h('div.page', pageHead(t('team.title'), t('team.ledeLive', { auth: res.auth === 'header' ? t('team.authHeader') : t('team.authToken') })),
    tokenBox,
    panel(t('team.members', { n: (res.members || []).length }), null, h('div.table-wrap', h('table.t',
      h('thead', h('tr', h('th', t('team.col.person')), h('th', t('team.col.role')), h('th.hide-md', t('team.projects')), h('th.hide-md', t('team.spentToday')), h('th'))),
      h('tbody', rows))), { flush: true }),
    invite,
    devicesPanel,
    h('p.faint', { style: { fontSize: '12px' } }, t('team.rolesNote'))) };
}
