// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Settings > Team server, on a person's own machine (docs/plans/connected-mode.md §12).
// Join with the server's address and a one-time invite code; then see the team
// (people, work on the board, approvals) and this person's devices. The device
// token stays in hpk on this machine: this page only talks to /api/teamserver/…,
// which adds the token and reaches only a fixed list of server paths.
import { isLive, teamMode } from '../live.js';
import { api } from '../data.js';
import { t } from '../i18n.js';
import { h, icon, lamp, pageHead, toast, panel, avatar, rel } from '../ui.js';

// v1 is solo-first: the team pages wait for a later version (docs/ROADMAP.md).
export const laterPanel = () => h('div.callout.route', icon('info'), h('div', h('b', t('features.later')), h('p', t('features.laterBody'))));

const getJSON = async (path) => {
  const r = await fetch(path, { headers: { Accept: 'application/json' } });
  const out = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(out.error || r.statusText);
  return out;
};
const post = async (path, body) => {
  const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify(body || {}) });
  const out = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(out.error || r.statusText);
  return out;
};
const remote = (path) => getJSON('/api/teamserver/remote/' + path);
const remotePost = (path, body) => post('/api/teamserver/remote/' + path, body);
const when = (iso) => (iso && !iso.startsWith('0001') ? rel(Date.parse(iso)) : t('ts.never'));

export default async function teamServer({ rerender }) {
  const head = pageHead(t('ts.title'), t('ts.lede'));
  // Off in v1, unless this machine already joined (then it keeps working: leave, the team view).
  if (!api.feature('team') && !api.teamJoined()) return { el: h('div.page', head, laterPanel()) };
  if (!isLive() || teamMode()) {
    return { el: h('div.page', head, h('div.callout.route', icon('info'), h('div', t('ts.notHere')))) };
  }
  let st;
  try { st = await getJSON('/api/teamserver'); } catch (err) {
    return { el: h('div.page', head, h('div.callout.danger', icon('warn'), h('div', err.message))) };
  }
  if (!st.available) return { el: h('div.page', head, h('div.callout.route', icon('info'), h('div', t('ts.notHere')))) };
  if (!st.connected) return { el: h('div.page', head, joinForm(rerender)) };
  return { el: h('div.page', head, ...(await connected(st, rerender))) };
}

function joinForm(rerender) {
  const url = h('input.input#tsUrl', { placeholder: 'https://team.example.com', autocomplete: 'off', inputmode: 'url' });
  const code = h('input.input.mono#tsCode', { placeholder: 'ABCDE-FGHJK', autocomplete: 'off', maxlength: 16, style: { maxWidth: '200px', textTransform: 'uppercase' } });
  const dev = h('input.input#tsDevice', { placeholder: t('ts.devicePh'), autocomplete: 'off', maxlength: 60 });
  const go = h('button.btn.primary', { type: 'submit' }, icon('link'), t('ts.join'));
  const form = h('form.stack', { onsubmit: async (e) => {
    e.preventDefault();
    go.disabled = true;
    try {
      const out = await post('/api/teamserver/join', { url: url.value.trim(), code: code.value.trim(), device: dev.value.trim() });
      toast(t('ts.joined', { name: out.name || out.member, role: t('roles.' + out.role) }));
      rerender();
    } catch (err) { toast(err.message, null, 'deny'); } finally { go.disabled = false; }
  } },
  h('div.field', h('label', { for: 'tsUrl' }, t('ts.url')), url, h('span.hint', t('ts.urlHint'))),
  h('div.field', h('label', { for: 'tsCode' }, t('ts.code')), code, h('span.hint', t('ts.codeHint'))),
  h('div.field', h('label', { for: 'tsDevice' }, t('ts.device')), dev, h('span.hint', t('ts.deviceHint'))),
  h('div.row', go));
  return h('div.grid.main-side',
    panel(t('ts.joinTitle'), t('ts.joinSub'), form),
    h('div.stack',
      h('div.callout', icon('key'), h('div', h('b', t('ts.safeTitle')), h('p', t('ts.safeBody')))),
      h('div.callout.route', icon('info'), h('div', h('b', t('ts.ownerTitle')), h('p', t('ts.ownerBody')), h('pre.mono', { style: { whiteSpace: 'pre-wrap' } }, 'hpk team join-code name@example.com')))));
}

const STATE = { ok: 'clear', offline: 'caution', revoked: 'danger', noToken: 'danger' };

async function connected(st, rerender) {
  const owner = st.role === 'owner';
  const ok = st.state === 'ok';
  const leave = async () => {
    let pin = '';
    if (!confirm(t('ts.leaveConfirm', { url: st.url }))) return;
    try { await post('/api/teamserver/leave', { pin }); } catch (err) {
      // The vault PIN guards removing the token when one is set.
      pin = prompt(t('ts.pinAsk')) || '';
      if (!pin) { toast(err.message, null, 'deny'); return; }
      try { await post('/api/teamserver/leave', { pin }); } catch (err2) { toast(err2.message, null, 'deny'); return; }
    }
    toast(t('ts.left'));
    rerender();
  };
  const status = panel(t('ts.status'), null, h('div.stack',
    h('table.t', h('tbody',
      h('tr', h('td.muted', t('ts.server')), h('td.mono', st.url)),
      h('tr', h('td.muted', t('ts.state')), h('td', h('span.status', { class: STATE[st.state] || 'idle' }, lamp(STATE[st.state] || 'idle'), t('ts.st.' + (st.state || 'offline'))),
        st.error ? h('div.cell-sub', st.error) : null)),
      h('tr', h('td.muted', t('ts.you')), h('td', (st.name || st.member) + ' · ', h('span.mono', st.member))),
      h('tr', h('td.muted', t('team.col.role')), h('td', t('roles.' + st.role))),
      h('tr', h('td.muted', t('ts.thisDevice')), h('td', st.device, h('span.faint.mono', ' ' + st.deviceId))),
      h('tr', h('td.muted', t('ts.joinedAt')), h('td', when(st.joined))))),
    st.state === 'revoked' || st.state === 'noToken' || st.state === 'moved' ? h('div.callout.danger', icon('warn'), h('div', t('ts.revokedHelp'))) : null,
    h('div.row',
      h('button.btn', { type: 'button', onclick: () => rerender() }, icon('scan'), t('ts.refresh')),
      h('button.btn.danger', { type: 'button', onclick: leave }, t('ts.leave')))));
  if (!ok) return [status];

  const [teamRes, devRes, snap] = await Promise.all([
    remote('team').catch(() => ({ members: [] })),
    remote('me/devices').catch(() => ({ devices: [] })),
    remote('snapshot').catch(() => null),
  ]);

  // My devices: each can be revoked; this one is marked.
  const myDevices = panel(t('ts.myDevices'), t('ts.myDevicesSub'), devTable(devRes.devices || [], devRes.current, false, async (d) => {
    if (!confirm(t('ts.revokeConfirm', { name: d.name }))) return;
    try { await remotePost('me/devices/revoke', { id: d.id }); rerender(); } catch (err) { toast(err.message, null, 'deny'); }
  }), { flush: true });

  const members = teamRes.members || [];
  const people = panel(t('ts.people', { n: members.length }), null, h('div.table-wrap', h('table.t',
    h('thead', h('tr', h('th', t('team.col.person')), h('th', t('team.col.role')), h('th.hide-md', t('team.projects')))),
    h('tbody', members.map((m) => h('tr',
      h('td', h('span.row', { style: { gap: '10px' } }, avatar(m.id), h('div', h('div.cell-title', m.name), h('div.cell-sub.mono', m.id)))),
      h('td', t('roles.' + m.role)),
      h('td.hide-md', (m.projects && m.projects.length) ? m.projects.join(', ') : t('team.allProjects'))))))), { flush: true });

  const work = snap ? teamWork(snap) : panel(t('ts.work'), null, h('p.muted', { style: { padding: '16px' } }, t('ts.noWork')));
  const out = [status, work, h('div.grid.two', people, myDevices)];
  if (owner) out.push(await ownerTools(st.url));
  return out;
}

// What the team is doing: projects on the server, open claims, waiting approvals.
function teamWork(snap) {
  const claims = (snap.claims || []).filter((c) => c.status !== 'done' && c.status !== 'released');
  const approvals = snap.approvals || [];
  const projects = snap.projects || [];
  return panel(t('ts.work'), t('ts.workSub', { project: (projects.find((p) => p.id === snap.projectId) || {}).name || '—', n: projects.length }), h('div.stack', { style: { padding: '0 0 8px' } },
    claims.length ? h('div.table-wrap', h('table.t',
      h('thead', h('tr', h('th', t('ts.task')), h('th', t('ts.who')), h('th.hide-md', t('ts.lane')), h('th.hide-md', t('ts.heartbeat')))),
      h('tbody', claims.slice(0, 30).map((c) => h('tr',
        h('td', h('div.cell-title', c.task)),
        h('td', h('span.row', { style: { gap: '8px' } }, avatar(c.owner), h('span.mono', c.owner))),
        h('td.hide-md', c.lane || '—'),
        h('td.hide-md', c.heartbeat ? rel(c.heartbeat) : '—')))))) : h('p.muted', { style: { padding: '16px' } }, t('ts.noClaims')),
    h('div', { style: { padding: '0 16px' } },
      h('h3', { style: { fontSize: '14px', margin: '8px 0' } }, t('ts.approvals', { n: approvals.length })),
      approvals.length ? h('ul.stack', { style: { margin: 0, paddingLeft: '18px' } }, approvals.slice(0, 15).map((a) => h('li', h('b', a.title), a.project ? h('span.faint', ' · ' + a.project) : null)))
        : h('p.muted', t('ts.noApprovals')))), { flush: true });
}

function devTable(list, current, showMember, onRevoke) {
  if (!list.length) return h('p.muted', { style: { padding: '16px' } }, t('ts.noDevices'));
  return h('div.table-wrap', h('table.t',
    h('thead', h('tr', showMember ? h('th', t('team.col.person')) : null, h('th', t('ts.device')), h('th', t('ts.lastSeen')), h('th.hide-md', t('ts.lastIP')), h('th'))),
    h('tbody', list.map((d) => h('tr',
      showMember ? h('td.mono', d.member) : null,
      h('td', h('div.cell-title', d.name, d.id === current ? h('span.tag.clear', { style: { marginLeft: '8px' } }, t('ts.thisOne')) : null), h('div.cell-sub', t('ts.added', { when: when(d.added) }))),
      h('td', when(d.lastSeen)),
      h('td.hide-md.mono', d.lastIP || '—'),
      h('td', { style: { textAlign: 'right' } }, onRevoke ? h('button.btn.sm.danger', { type: 'button', onclick: () => onRevoke(d) }, t('ts.revoke')) : null))))));
}

// Owners: every device on the server, read only. Invite codes, people and
// revoking other people's devices are done in the team server's own Console:
// this machine's local API can be called by any program here (the AI too), so
// it never mints credentials or changes the team.
async function ownerTools(serverUrl) {
  const all = await remote('team/devices').catch(() => ({ devices: [] }));
  const href = (serverUrl || '').replace(/\/$/, '') + '/#/team';
  return h('div.grid.two',
    panel(t('ts.inviteTitle'), null, h('div.stack',
      h('div.callout.route', icon('shield'), h('div', h('p', t('ts.ownerOnServer')))),
      h('div.row', h('a.btn.primary', { href, target: '_blank', rel: 'noopener noreferrer' }, icon('external'), t('ts.openServer'))))),
    panel(t('ts.allDevices'), (all.allowIPs && all.allowIPs.length) ? t('ts.allowIPs', { list: all.allowIPs.join(', ') }) : t('ts.noAllowIPs'),
      devTable(all.devices || [], null, true, null), { flush: true }));
}

// A fresh invite code: shown once, with when it stops working.
export function inviteBox(out) {
  const exp = new Date(out.expires);
  return h('div.callout.caution', icon('key'), h('div',
    h('b', t('ts.codeFor', { id: out.member })),
    h('pre.mono', { style: { userSelect: 'all', fontSize: '20px', letterSpacing: '2px' } }, out.code),
    h('p.faint', t('ts.codeOnce', { time: exp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }))));
}

export { devTable };
