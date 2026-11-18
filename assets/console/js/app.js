// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { initI18n, setLang, getLang, t, LANGS } from './i18n.js';
import { api, bus } from './data.js';
import { h, icon, lamp, openDrawer, closeDrawer, toast, rel, privacyTag, avatar, closeMenu } from './ui.js';

import overview from './views/overview.js';
import projects from './views/projects.js';
import wizard from './views/wizard.js';
import sessionsView from './views/sessions.js';
import chatView, { chatList } from './views/chat.js';
import { approvalHref } from './views/overview.js';
import { connect, isLive, needsSignIn, signIn, signOut, teamMode } from './live.js';
import approvals from './views/approvals.js';
import wiki from './views/wiki.js';
import board from './views/board.js';
import gates from './views/gates.js';
import jobs from './views/jobs.js';
import privacy from './views/privacy.js';
import gitView from './views/git.js';
import team from './views/team.js';
import settings from './views/settings.js';
import accounts from './views/accounts.js';
import mcpView from './views/mcp.js';
import vaultView from './views/vault.js';
import teamServerView from './views/teamserver.js';
import browserPage, { initBrowserDock, toggleBrowserDock, browserDockOpen } from './views/browserdock.js';
import './controls.js';
import { setPref } from './prefs.js';
import structure from './views/structure.js';
import { initPWA } from './pwa.js';
import { initDeskNotify } from './desknotify.js';

// Each route belongs to one of seven nav items ("section"). Sections with more
// than one page get a row of tabs above the page instead of more nav items.
const ROUTES = [
  { path: 'overview', view: overview, nav: 'overview', section: 'overview' },
  { path: 'chat/new', view: chatView, nav: 'chat', section: 'chat' },
  { path: 'chat', view: chatView, nav: 'chat', section: 'chat' },
  { path: 'sessions/:id', view: chatView, nav: 'chat', section: 'chat' },
  { path: 'approvals', view: approvals, nav: 'approvals', section: 'approvals' },
  { path: 'board', view: board, nav: 'board', section: 'work' },
  { path: 'sessions', view: sessionsView, nav: 'sessions', section: 'work' },
  { path: 'git', view: gitView, nav: 'git', section: 'work' },
  { path: 'wiki/:tab', view: wiki, nav: 'wiki', section: 'wiki' },
  { path: 'wiki', view: wiki, nav: 'wiki', section: 'wiki' },
  { path: 'projects/new', view: wizard, nav: 'projects', section: 'projects' },
  { path: 'projects', view: projects, nav: 'projects', section: 'projects' },
  { path: 'setup/:tab', view: structure, nav: 'setup', section: 'projects' },
  { path: 'setup', view: structure, nav: 'setup', section: 'projects' },
  { path: 'jobs', view: jobs, nav: 'jobs', section: 'projects' },
  { path: 'settings', view: settings, nav: 'settings', section: 'settings' },
  { path: 'accounts', view: accounts, nav: 'accounts', section: 'settings' },
  { path: 'mcp', view: mcpView, nav: 'mcp', section: 'settings' },
  { path: 'vault', view: vaultView, nav: 'vault', section: 'settings' },
  { path: 'browser', view: browserPage, nav: 'browser', section: 'work' },
  { path: 'gates', view: gates, nav: 'gates', section: 'settings' },
  { path: 'privacy', view: privacy, nav: 'privacy', section: 'settings' },
  { path: 'team', view: team, nav: 'team', section: 'settings' },
  { path: 'teamserver', view: teamServerView, nav: 'teamserver', section: 'settings' },
];
// Pages that belong to a person's own machine: a team server hides them (and its
// API refuses them). The Team server page is how a machine joins one.
const LOCAL_ONLY = new Set(['browser', 'vault', 'teamserver']);
const shown = (key) => !(LOCAL_ONLY.has(key) && teamMode()) && (key !== 'teamserver' || isLive());
// Seven places to go. Order = how often people need them.
const NAV = [
  { key: 'overview', href: 'overview', icon: 'overview' },
  { key: 'chat', href: 'chat', icon: 'chat' },
  { key: 'approvals', href: 'approvals', icon: 'approvals' },
  { key: 'work', href: 'board', icon: 'board' },
  { key: 'wiki', href: 'wiki', icon: 'wiki' },
  { key: 'projects', href: 'projects', icon: 'projects' },
  null, // separator
  { key: 'settings', href: 'settings', icon: 'settings' },
];
// Tabs shown at the top of sections that hold several pages: [route nav key, path].
const SECTION_TABS = {
  work: [['board', 'board'], ['sessions', 'sessions'], ['git', 'git'], ['browser', 'browser']],
  projects: [['projects', 'projects'], ['setup', 'setup'], ['jobs', 'jobs']],
  settings: [['settings', 'settings'], ['accounts', 'accounts'], ['mcp', 'mcp'], ['vault', 'vault'], ['gates', 'gates'], ['privacy', 'privacy'], ['team', 'team'], ['teamserver', 'teamserver']],
};

let cleanup = null;
let current = null;

function match(hash) {
  const parts = hash.replace(/^#\/?/, '').split('/').filter(Boolean);
  for (const r of ROUTES) {
    const rp = r.path.split('/');
    if (rp.length !== parts.length) continue;
    const params = {};
    if (rp.every((seg, i) => (seg.startsWith(':') ? ((params[seg.slice(1)] = decodeURIComponent(parts[i])), true) : seg === parts[i]))) {
      return { route: r, params };
    }
  }
  return null;
}

// Pages that still show sample data even when hpk serve is running.
// Pages still on sample data when served by hpk serve (none since the M8 follow-up).
const DEMO_ONLY = new Set([]);

export function go(path) { location.hash = '#/' + path; }

async function render() {
  // No project yet: only the setup + add-project wizard can open.
  if (isLive() && !api.project() && !location.hash.startsWith('#/projects/new')) { renderNoProject(); return; }
  let m = match(location.hash) || match('#/overview');
  if (!shown(m.route.nav)) m = match('#/overview');
  if (!location.hash || !match(location.hash) || m.route.nav === 'overview') history.replaceState(null, '', '#/overview');
  current = m;
  if (typeof cleanup === 'function') cleanup();
  cleanup = null;
  closeMenu();
  renderNav();
  renderTopbar();
  const main = document.getElementById('main');
  main.dataset.route = m.route.nav;
  const ctx = { params: m.params, go, rerender: render };
  const out = await m.route.view(ctx);
  const node = out?.el || out;
  cleanup = out?.cleanup || null;
  const tabs = bare() ? null : sectionTabs(m.route);
  // Live, but this page has no backend yet: say so plainly instead of passing sample data off as real.
  const banner = isLive() && DEMO_ONLY.has(m.route.path) ? h('div.callout.caution.demo-banner', icon('info'), h('div', h('b', t('shell.demoPageTitle')), h('p', t('shell.demoPageBody')))) : null;
  const swap = () => main.replaceChildren(...[tabs, banner, node].filter(Boolean));
  const motion = document.documentElement.getAttribute('data-motion') !== 'off' && !matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (motion && document.startViewTransition && main.childElementCount && !document.hidden) {
    let swapped = false;
    const once = () => { if (!swapped) { swapped = true; swap(); } };
    const vt = document.startViewTransition(once);
    // A newer navigation may cut this one short; that is expected, not an error.
    vt.ready.catch(() => {}); vt.finished.catch(() => {}); vt.updateCallbackDone.catch(() => {});
    // If the browser is not producing frames (minimised window), the transition never
    // starts; never leave the old screen up — swap directly and drop the animation.
    setTimeout(() => { if (!swapped) { once(); try { vt.skipTransition(); } catch { /* already done */ } } }, 250);
  } else swap();
  document.title = `${t('nav.' + m.route.nav)} · HPK Console`;
  closeRail();
  main.focus({ preventScroll: true });
  window.scrollTo({ top: 0 });
}

// Badges: only things that need attention get a number.
function badges() {
  const ap = api.approvalsSync().length;
  const dup = api.overlapsSync().filter((o) => o.level === 'danger').length;
  const drafts = api.project().drafts;
  const acc = api.accountsSync().filter((a) => a.status === 'exhausted' || a.status === 'error').length;
  return {
    approvals: ap ? h('span.count.hot', ap) : null,
    work: dup ? h('span.count.hot', dup) : null,
    wiki: drafts ? h('span.count.warm', drafts) : null,
    settings: acc ? h('span.count.warm', acc) : null,
    board: dup ? h('span.count.hot', dup) : null,
    accounts: acc ? h('span.count.warm', acc) : null,
  };
}

function sectionTabs(route) {
  const list = SECTION_TABS[route.section];
  if (!list) return null;
  const b = badges();
  return h('nav.section-tabs', { 'aria-label': t('nav.' + route.section) },
    list.filter(([key]) => (key !== 'team' || api.project().kind === 'team') && shown(key))
      // The nav-level "settings" badge belongs to its AI accounts tab, not the General tab.
      .map(([key, path]) => h('a', { href: '#/' + path, 'aria-current': route.nav === key ? 'page' : null }, t('tabs.' + key), key === 'settings' ? null : b[key] || null)));
}

// Live with no project yet (first run): no nav or counts to show.
const bare = () => isLive() && !api.project();

function renderNav() {
  const nav = document.getElementById('nav');
  if (bare()) { nav.replaceChildren(); return; }
  const b = badges();
  nav.replaceChildren(...NAV.map((n) => (n
    ? h('a', { href: '#/' + n.href, 'aria-current': current?.route.section === n.key ? 'page' : null }, icon(n.icon), h('span', t('nav.' + n.key)), b[n.key] || h('span'))
    : h('div.nav-sep', { role: 'separator' }))));
  renderProjectSwitch();
  renderNeeds();
  renderRailChats();
  renderRailFoot();
}

function renderProjectSwitch() {
  const box = document.getElementById('projectSwitch');
  const p = api.project();
  const btn = h('button.project-btn', { type: 'button', 'aria-expanded': 'false', 'aria-controls': 'projMenu' },
    lamp(p.privacy === 'shared' ? 'route' : p.privacy === 'private' ? 'caution' : 'danger'),
    h('span', h('span.p-name', p.name), h('span.p-meta', t('setup.kind.' + p.kind) + ' · ' + t(`privacy.${p.privacy}.name`) + ' · ' + api.profileOf(p.id).name)),
    icon('chevron'));
  box.replaceChildren(btn);
  btn.addEventListener('click', async () => {
    const open = btn.getAttribute('aria-expanded') === 'true';
    if (open) { box.querySelector('.project-menu')?.remove(); btn.setAttribute('aria-expanded', 'false'); return; }
    const list = await api.projects();
    const item = (x) => h('button', { type: 'button', role: 'menuitemradio', 'aria-current': x.id === p.id ? 'true' : null, 'aria-checked': x.id === p.id ? 'true' : 'false',
      onclick: () => { api.setProject(x.id); render(); } },
    lamp(x.privacy === 'shared' ? 'route' : x.privacy === 'private' ? 'caution' : 'danger'), h('span', x.name));
    const menu = h('div.project-menu#projMenu', { role: 'menu' },
      ['team', 'personal'].map((k) => [h('div.pm-head', t('projects.section.' + k)), list.filter((x) => x.kind === k).map(item)]),
      h('button', { type: 'button', role: 'menuitem', onclick: () => go('projects/new') }, icon('plus'), t('projects.add')));
    box.append(menu);
    btn.setAttribute('aria-expanded', 'true');
  });
}

// One line at the top of the sidebar: how many things wait for you, and the first one.
let needsOpen = false;
function renderNeeds() {
  const box = document.getElementById('railNeeds');
  const list = api.approvalsSync();
  if (!list.length) { box.replaceChildren(); return; }
  const first = list[0];
  const btn = h('button.needs-btn', { type: 'button', 'aria-expanded': String(needsOpen), onclick: () => { needsOpen = !needsOpen; renderNeeds(); } },
    lamp('danger', { blink: true }),
    h('span.nb-text', h('b', t('shell.needsN', { n: list.length })), h('span', ' · ' + first.title)),
    icon('chevron', 'i nb-chev'));
  const items = list.slice(0, 6).map((a) => h('li', h('a', { href: approvalHref(a) },
    lamp(a.kind === 'permission' || a.kind === 'duplicate' ? 'danger' : 'caution'), h('span', a.title))));
  if (list.length > 6) items.push(h('li', h('a.faint', { href: '#/approvals' }, t('overview.allApprovals', { n: list.length }))));
  box.replaceChildren(...[btn, needsOpen ? h('ul.needs-list', items) : null].filter(Boolean));
}

// Chats live in the sidebar: main chats with their sub-tasks threaded underneath.
function renderRailChats() {
  const box = document.getElementById('railChats');
  const id = current?.route.path === 'sessions/:id' ? current.params.id : null;
  const scroll = box.querySelector('.cl-groups')?.scrollTop || 0;
  box.replaceChildren(chatList(id, render));
  const g = box.querySelector('.cl-groups'); if (g) g.scrollTop = scroll;
}
let railTimer = null;
const railLater = () => { clearTimeout(railTimer); railTimer = setTimeout(() => { renderRailChats(); renderNeeds(); }, 250); };
bus.addEventListener('sessions', railLater);
bus.addEventListener('chat-meta', railLater);
bus.addEventListener('approvals', railLater);

function renderRailFoot() {
  const me = api.me();
  document.getElementById('railFoot').replaceChildren(
    h('div.who', avatar(me.id), h('div', h('div', { style: { color: 'var(--text)', fontWeight: 600 } }, me.name), h('div', t('roles.' + me.role)))),
    h('div.row', { style: { gap: '6px' } }, lamp('clear'), h('span', t('shell.connected')),
      teamMode() ? h('button.btn.sm.ghost', { type: 'button', onclick: signOut, style: { marginLeft: 'auto' } }, t('team.signOut')) : null),
  );
}

function modeChip() {
  const m = api.state().mode;
  const kind = m.value === 'auto' ? 'clear' : m.value === 'ask' ? 'route' : 'idle';
  const sub = m.value === 'auto' && m.autoUntil ? t('mode.leftMin', { n: Math.max(0, Math.round((m.autoUntil - Date.now()) / 60000)) }) : t('mode.scope.' + m.scope);
  return h('button.mode', { type: 'button', class: m.value === 'auto' ? 'auto' : '', onclick: openModeDrawer, 'aria-label': t('mode.aria', { mode: t('mode.' + m.value + '.name') }) },
    lamp(kind, { blink: m.value === 'auto' }), h('span', t('mode.' + m.value + '.name')), h('span.m-sub', sub));
}

function renderTopbar() {
  const bar = document.getElementById('topbar');
  if (bare()) { bar.replaceChildren(h('div.crumbs', h('strong', 'HPK Console'))); return; }
  const unread = api.notificationsSync().filter((n) => n.unread).length;
  const theme = document.documentElement.getAttribute('data-theme');
  const p = api.project();
  bar.replaceChildren(
    h('button.icon-btn.menu-btn', { type: 'button', 'aria-label': t('shell.openNav'), onclick: openRail }, icon('menu')),
    h('div.crumbs', h('span.hide-sm', p.name), h('span.hide-sm', icon('chevron')), h('strong', t('nav.' + (current?.route.nav || 'overview')))),
    h('span.hide-sm', privacyTag(p.privacy)),
    isLive() ? h('span') : h('span.tag.caution.demo-tag', { title: t('shell.demoHint') }, t('shell.demo')),
    h('div.spacer'),
    modeChip(),
    ...(isLive() && !teamMode() ? [h('button.icon-btn#browserBtn', { type: 'button', 'aria-pressed': browserDockOpen() ? 'true' : 'false', 'aria-label': t('browser.title'), title: t('browser.dockTip'), onclick: () => { toggleBrowserDock(); renderTopbar(); } }, icon('globe'))] : []),
    h('button.icon-btn#bellBtn', { type: 'button', 'aria-label': t('notif.title') + (unread ? ` (${unread})` : ''), onclick: openNotifications }, icon('bell'), unread ? h('span.dot') : null),
    h('button.icon-btn.hide-sm', { type: 'button', 'aria-pressed': document.documentElement.getAttribute('data-motion') !== 'off' ? 'true' : 'false', 'aria-label': t('shell.motion'), title: t('shell.motion'), onclick: () => {
      const next = document.documentElement.getAttribute('data-motion') === 'off' ? 'on' : 'off';
      setPref('motion', next);
      renderTopbar(); toast(t(next === 'on' ? 'shell.motionOn' : 'shell.motionOff'));
    } }, icon(document.documentElement.getAttribute('data-motion') === 'off' ? 'pause' : 'sparkle')),
    h('button.icon-btn', { type: 'button', 'aria-label': t('shell.language'), title: t('shell.language'), onclick: async () => {
      const next = getLang() === 'en' ? 'th' : 'en';
      await setLang(next); render();
    } }, h('span', { style: { fontSize: '11px', fontWeight: 700, fontFamily: 'var(--font-data)' } }, getLang().toUpperCase())),
    h('button.icon-btn', { type: 'button', 'aria-label': t('shell.theme'), title: t('shell.theme'), onclick: () => {
      setPref('theme', theme === 'dark' ? 'light' : 'dark');
      renderTopbar();
    } }, icon(theme === 'dark' ? 'sun' : 'moon')),
  );
}

function openModeDrawer() {
  const m = api.state().mode;
  let value = m.value; let minutes = 120; let scope = m.scope;
  const seg = h('div.seg', { role: 'group', 'aria-label': t('mode.title') },
    ['manual', 'ask', 'auto'].map((v) => h('button', { type: 'button', 'aria-pressed': v === value ? 'true' : 'false', onclick: (e) => {
      value = v; seg.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', 'false')); e.currentTarget.setAttribute('aria-pressed', 'true'); sync();
    } }, lamp(v === 'auto' ? 'clear' : v === 'ask' ? 'route' : 'idle'), t(`mode.${v}.name`))));
  const desc = h('p.muted');
  const autoBox = h('div.stack', { style: { gap: '12px' } },
    h('div.field', h('label', { for: 'autoFor' }, t('mode.for')),
      h('select.input#autoFor', { onchange: (e) => (minutes = Number(e.target.value)) },
        [[30, 'mode.dur.30'], [120, 'mode.dur.120'], [480, 'mode.dur.480'], [0, 'mode.dur.until']].map(([v, k]) => h('option', { value: v, selected: v === 120 ? true : null }, t(k))))),
    h('div.callout.danger', icon('warn'), h('div', h('b', t('mode.redLinesTitle')), h('p', t('mode.redLinesBody')))));
  const scopeSel = h('div.field', h('label', { for: 'modeScope' }, t('mode.scopeLabel')),
    h('select.input#modeScope', { onchange: (e) => (scope = e.target.value) },
      ['session', 'lane', 'project'].map((v) => h('option', { value: v, selected: v === scope ? true : null }, t('mode.scope.' + v)))));
  function sync() { desc.textContent = t(`mode.${value}.desc`); autoBox.hidden = value !== 'auto'; }
  sync();
  openDrawer(t('mode.title'), h('div.stack', { style: { padding: '16px' } }, seg, desc, scopeSel, autoBox,
    h('a', { href: '#/gates', onclick: closeDrawer }, t('mode.seeGates'))),
  [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')),
    h('button.btn.primary', { type: 'button', onclick: () => { api.setMode(value, value === 'auto' ? minutes || 600 : null, scope); closeDrawer(); renderTopbar(); toast(t('mode.saved', { mode: t(`mode.${value}.name`) })); } }, t('common.apply'))]);
}

async function openNotifications() {
  const list = await api.notifications();
  const body = list.length ? list.map((n) => h('a.notif', { href: n.href, class: n.unread ? 'unread' : '', onclick: closeDrawer, style: { color: 'inherit', textDecoration: 'none' } },
    lamp(n.lamp), h('strong', { style: { fontWeight: 600 } }, n.title), h('span.n-time', rel(n.at)), h('span.n-desc', n.desc)))
    : h('p.empty', t('notif.empty'));
  openDrawer(t('notif.title'), [h('p.muted', { style: { padding: '10px 16px 0', fontSize: '12px' } }, t('notif.batching')), body],
    [h('a.btn.ghost', { href: '#/settings', onclick: closeDrawer }, t('notif.settings')),
      h('button.btn', { type: 'button', onclick: () => { api.markAllRead(); closeDrawer(); renderTopbar(); } }, t('notif.markAll'))]);
}

const mobileNav = window.matchMedia('(max-width: 860px)');
function syncRailInert() {
  const rail = document.getElementById('rail');
  rail.inert = mobileNav.matches && !rail.classList.contains('open');
}
function openRail() { const r = document.getElementById('rail'); r.classList.add('open'); syncRailInert(); document.getElementById('scrim').hidden = false; document.getElementById('railClose').focus(); }
function closeRail() { document.getElementById('rail').classList.remove('open'); syncRailInert(); if (document.getElementById('drawer').getAttribute('aria-hidden') === 'true') document.getElementById('scrim').hidden = true; }
mobileNav.addEventListener('change', syncRailInert);
syncRailInert();

function shortcuts() {
  const rows = [['g o', 'nav.overview'], ['g s', 'nav.sessions'], ['g a', 'nav.approvals'], ['g b', 'nav.board'], ['g w', 'nav.wiki'], ['j / k', 'keys.move'], ['a', 'keys.approve'], ['d', 'keys.deny'], ['Enter', 'keys.open'], ['Ctrl+J', 'chat.permit.next'], ['1 2 3', 'chat.permit.keys'], ['?', 'keys.help']];
  openDrawer(t('keys.title'), h('table.t', h('tbody', rows.map(([k, label]) => h('tr', h('td', k.split(' ').map((x) => h('kbd', x)).reduce((a, b) => [a, ' ', b])), h('td', t(label)))))));
}

// Global keys run in the capture phase so a "g x" sequence is consumed here and
// never reaches a focused queue (where "a" would approve).
let gPending = false;
document.addEventListener('keydown', (e) => {
  const typing = /INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName) || document.activeElement?.isContentEditable;
  if (e.key === 'Escape') { closeDrawer(); closeRail(); return; }
  if (e.key === 'Tab') trapDrawerFocus(e);
  // Ctrl/Cmd+K: new chat from anywhere (works while typing too).
  if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 'k') { e.preventDefault(); e.stopPropagation(); go('chat/new'); return; }
  // Ctrl/Cmd+J: jump to the next thing waiting for you, in whatever chat it lives.
  if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 'j') { e.preventDefault(); e.stopPropagation(); nextAsk(); return; }
  if (typing || e.metaKey || e.ctrlKey || e.altKey) return;
  if (gPending) {
    const map = { o: 'overview', s: 'sessions', a: 'approvals', b: 'board', w: 'wiki', p: 'projects', j: 'jobs' };
    gPending = false;
    e.stopPropagation();
    e.preventDefault();
    if (map[e.key]) go(map[e.key]);
    return;
  }
  if (e.key === '?') { e.preventDefault(); e.stopPropagation(); shortcuts(); return; }
  if (e.key === 'g') { gPending = true; e.stopPropagation(); setTimeout(() => (gPending = false), 900); }
}, { capture: true });

// Red-line asks open in the chat that raised them; questions open in the main chat
// they were sent to. Each press moves to the next one, wrapping around.
let askIdx = -1;
function nextAsk() {
  const asks = api.approvalsSync().filter((a) => (a.kind === 'permission' || a.kind === 'question') && a.session);
  if (!asks.length) { toast(t('chat.permit.noneLeft')); return; }
  askIdx = (askIdx + 1) % asks.length;
  const a = asks[askIdx];
  let sess = api.session(a.session);
  if (a.kind === 'question') while (sess?.parent) sess = api.session(sess.parent);
  // No card for it in any chat (e.g. raised by a hook): decide it on the Approvals page.
  const inChat = sess && (api.chatSync(sess.id) || []).some((m) => (a.kind === 'permission' ? m.t === 'permit' && m.id === a.id : m.t === 'clarify' && !m.answered));
  if (!inChat) { go('approvals'); return; }
  const focusIt = (tries = 0) => {
    const el = document.querySelector(a.kind === 'permission' ? `.permit[data-ask="${a.id}"]` : '.clarify');
    if (el) { el.scrollIntoView({ block: 'center' }); el.focus?.({ preventScroll: true }); el.classList.add('flash'); setTimeout(() => el.classList.remove('flash'), 900); }
    else if (tries < 15) setTimeout(() => focusIt(tries + 1), 100);
  };
  if (location.hash !== '#/sessions/' + sess.id) go('sessions/' + sess.id);
  focusIt();
}

function trapDrawerFocus(e) {
  const d = document.getElementById('drawer');
  if (d.getAttribute('aria-hidden') !== 'false') return;
  const f = [...d.querySelectorAll('button, a[href], input, select, textarea, [tabindex]:not([tabindex="-1"])')].filter((x) => !x.disabled && x.offsetParent !== null);
  if (!f.length) return;
  const first = f[0]; const last = f[f.length - 1];
  if (!d.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
  else if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
  else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
}

document.getElementById('railClose').addEventListener('click', closeRail);
document.getElementById('scrim').addEventListener('click', () => { closeRail(); closeDrawer(); });
window.addEventListener('hashchange', render);
['approvals', 'mode', 'notifications', 'project-updated', 'team', 'accounts'].forEach((ev) => bus.addEventListener(ev, () => { renderNav(); renderTopbar(); }));
bus.addEventListener('notify', () => {
  renderTopbar();
  const b = document.getElementById('bellBtn');
  if (b) { b.classList.remove('ring'); void b.offsetWidth; b.classList.add('ring'); }
});

initPWA();
await initI18n();
await connect();
if (isLive()) initDeskNotify();
const mustSignIn = needsSignIn();
document.querySelector('.skip').textContent = t('shell.skip');
document.getElementById('rail').setAttribute('aria-label', t('shell.primaryNav'));
document.getElementById('railClose').setAttribute('aria-label', t('shell.closeNav'));
document.querySelector('.brand').setAttribute('aria-label', t('shell.home'));
if (mustSignIn) renderSignIn(mustSignIn); else { render(); if (!teamMode()) initBrowserDock(); addEventListener('bdock', () => renderTopbar()); }
export { LANGS };

// Live, but nothing added yet: say how to start instead of showing empty screens.
function renderNoProject() {
  document.getElementById('main').replaceChildren(h('div.page', h('div.empty-start',
    h('h1', t('shell.noProjectTitle')),
    h('p.muted', t('shell.noProjectBody')),
    h('pre', 'hpk project add <folder>'),
    teamMode() ? null : h('p', h('a.btn.primary', { href: '#/projects/new' }, icon('plus'), t('shell.noProjectSetup'))))));
  document.getElementById('topbar').replaceChildren(h('div.crumbs', h('strong', 'HPK Console')));
}

// Team mode, not signed in: a token, or the organisation's proxy (header mode).
function renderSignIn(auth) {
  document.getElementById('rail').hidden = true;
  document.getElementById('app').classList.add('signin');
  document.getElementById('topbar').replaceChildren(h('div.crumbs', h('strong', 'HPK Console')));
  const input = h('input.input#signToken', { type: 'password', autocomplete: 'off', placeholder: 'hpk_…', style: { maxWidth: '360px' } });
  const err = h('p.faint', { role: 'alert' });
  const go = async () => {
    err.textContent = '';
    try { await signIn(input.value.trim()); location.reload(); } catch (e) { err.textContent = e.message; input.focus(); }
  };
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });
  document.getElementById('main').replaceChildren(h('div.page', h('div.empty-start',
    h('h1', t('team.signInTitle')),
    auth === 'header'
      ? h('p.muted', t('team.signInProxy'))
      : [h('p.muted', t('team.signInBody')),
        h('div.field', h('label', { for: 'signToken' }, t('team.token')), input), err,
        h('div.row', h('button.btn.primary', { type: 'button', onclick: go }, t('team.signIn')))])));
  if (auth !== 'header') input.focus();
}

// Appearance changed in Settings: the top bar's theme and motion buttons follow.
document.documentElement.addEventListener('hpk:prefs', () => { if (document.getElementById('topbar')?.childElementCount) renderTopbar(); });
