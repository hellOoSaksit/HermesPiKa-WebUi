// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// MCP connections: tools from other programs (Blender, Roblox Studio, …) and
// web services. Hermes keeps the setting; hpk adds, tests and removes through
// `hermes mcp` (see internal/mcp). On a team server only web MCPs are allowed.
import { t } from '../i18n.js';
import { h, s, icon, toast, rel, pageHead, panel, openDrawer, closeDrawer } from '../ui.js';
import { bgPanel, loadJobs } from './bg.js';
import { isLive } from '../live.js';
import { CATALOG, GROUPS } from '../mcpcatalog.js';

// ---- pictures ----
const glyph = {
  local: () => s('svg', { class: 'mcp-kind', viewBox: '0 0 20 20', 'aria-hidden': 'true' },
    s('rect', { x: 2.5, y: 3.5, width: 15, height: 10, rx: 1.8 }), s('path', { d: 'M7 17h6M10 13.5V17' })),
  url: () => icon('globe', 'mcp-kind'),
  server: () => s('svg', { class: 'mcp-kind', viewBox: '0 0 20 20', 'aria-hidden': 'true' },
    s('rect', { x: 3, y: 3, width: 14, height: 6, rx: 1.5 }), s('rect', { x: 3, y: 11, width: 14, height: 6, rx: 1.5 }),
    s('circle', { cx: 6.5, cy: 6, r: 0.9, class: 'fill' }), s('circle', { cx: 6.5, cy: 14, r: 0.9, class: 'fill' })),
};
const badge = (name, hue) => {  const el = h('span.mcp-badge', { 'aria-hidden': 'true' }, name.replace(/[^A-Za-z0-9]/g, '').slice(0, 2).replace(/^./, (c) => c.toUpperCase()) || '?');  el.style.setProperty('--h', String(hue ?? hueOf(name)));  return el;};
const hueOf = (n) => [...n].reduce((a, c) => (a * 31 + c.charCodeAt(0)) % 360, 7);

// ---- demo data (the dev server has no Hermes) ----
const demo = {
  place: 'local', canManage: true, canSendTokens: true, os: 'demo',
  servers: [
    { name: 'blender', kind: 'local', command: 'uvx', args: ['blender-mcp'], enabled: true, tools: [], toolCount: 17 },
    { name: 'deepwiki', kind: 'url', url: 'https://mcp.deepwiki.com/mcp', enabled: true, toolCount: 3 },
    { name: 'roblox', kind: 'local', command: 'rbx-studio-mcp', args: ['--stdio'], enabled: false },
  ],
};
async function load() {
  // demo: ?place=server shows what a team server allows
  if (!isLive()) return { ...structuredClone(demo), place: new URLSearchParams(location.search).get("place") === "server" ? "server" : "local" };
  const r = await fetch('/api/mcp', { headers: { Accept: 'application/json' } });
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
  return r.json();
}
// What each MCP is used by (recorded by hpk's hook on every MCP tool call).
const H = 3600e3;
const demoUsage = {
  blender: { calls: 214, blocked: 0, last: Date.now() - 0.2 * H, days: [0, 3, 8, 0, 0, 12, 30, 22, 0, 4, 18, 41, 46, 30],
    tools: [['execute_blender_code', 96], ['get_scene_info', 61], ['get_object_info', 33], ['set_material', 14], ['download_polyhaven_asset', 10]],
    projects: [['kart-racer', 160, '/code/kart-racer'], ['atlas-web', 54, '/code/atlas-web']],
    sessions: [['20261118_1003_b7', 41, 0.2], ['20261117_1520_c2', 88, 20], ['20261116_0930_d4', 85, 46]],
    skills: [['scene-blockout'], ['asset-export']] },
  deepwiki: { calls: 37, blocked: 2, last: Date.now() - 26 * H, days: [2, 0, 0, 5, 3, 0, 0, 9, 1, 0, 0, 12, 5, 0],
    tools: [['ask_question', 30], ['read_wiki_structure', 7]], projects: [['ledger-api', 37, '/code/ledger-api']], sessions: [['20261117_1101_e9', 12, 26]], skills: [] },
  roblox: { calls: 58, blocked: 0, last: Date.now() - 72 * H, days: [6, 10, 0, 0, 14, 20, 8, 0, 0, 0, 0, 0, 0, 0],
    tools: [['run_code', 31], ['insert_model', 19], ['get_studio_mode', 8]], projects: [['obby-world', 58, '/code/obby-world']], sessions: [['20261115_1400_f1', 58, 72]], skills: [['level-builder']] },
};
async function loadUsage() {
  if (!isLive()) {
    const out = {};
    for (const [k, u] of Object.entries(demoUsage)) {
      out[k] = { ...u, last: new Date(u.last).toISOString(),
        tools: u.tools.map(([name, calls]) => ({ name, calls })),
        projects: u.projects.map(([name, calls, root]) => ({ name, calls, root, last: new Date(Date.now() - 3 * H).toISOString() })),
        sessions: u.sessions.map(([name, calls, ago]) => ({ name, calls, last: new Date(Date.now() - ago * H).toISOString() })),
        skills: u.skills.map(([name]) => ({ name, path: '~/.hermes/skills/' + name + '/SKILL.md' })) };
    }
    return out;
  }
  const r = await fetch('/api/mcp/usage', { headers: { Accept: 'application/json' } });
  return r.ok ? (await r.json()).usage || {} : {};
}

// 14 small bars, one per day (oldest first).
function spark(days, big) {
  const max = Math.max(1, ...days);
  const w = big ? 18 : 4; const gap = big ? 6 : 2; const hgt = big ? 64 : 16;
  const svg = s('svg', { class: 'mcp-spark' + (big ? ' big' : ''), viewBox: `0 0 ${days.length * (w + gap) - gap} ${hgt}`, 'aria-hidden': 'true', preserveAspectRatio: 'none' });
  days.forEach((n, i) => {
    const bh = n ? Math.max(2, (n / max) * hgt) : 1;
    svg.append(s('rect', { x: i * (w + gap), y: hgt - bh, width: w, height: bh, rx: big ? 3 : 1, class: n ? (i === days.length - 1 ? 'today' : 'on') : '' }));
  });
  return svg;
}
// A list of names with a bar for their share.
function barList(rows, { label, sub, href, icn }) {
  const max = Math.max(1, ...rows.map((r) => r.calls));
  return h('ul.mcp-bars', rows.map((r) => h('li',
    h('div.mcp-bar-top',
      icn ? icon(icn) : null,
      href ? h('a', { href: href(r) }, label(r)) : h('span', label(r)),
      h('span.num', String(r.calls))),
    h('div.mcp-bar', h('i', { style: { width: Math.round((r.calls / max) * 100) + '%' } })),
    sub ? h('div.mcp-bar-sub', sub(r)) : null)));
}

async function send(body) {
  if (!isLive()) {
    await new Promise((r) => setTimeout(r, 700));
    if (body.op === 'remove') { demo.servers = demo.servers.filter((x) => x.name !== body.name); return { ok: true }; }
    if (body.op === 'test') return { ok: true, result: { connected: body.name !== 'roblox', tools: ['get_scene_info', 'execute_code'], message: body.name === 'roblox' ? 'Studio is not open' : '' } };
    const sv = { name: body.name, kind: body.url ? 'url' : 'local', url: body.url, command: body.command, args: body.args, enabled: true, toolCount: 4, auth: body.token ? 'token' : '' };
    demo.servers = [...demo.servers.filter((x) => x.name !== body.name), sv];
    return { ok: true, result: { connected: true, saved: true, enabled: true, tools: ['list_items', 'get_item', 'create_item', 'search'] } };
  }
  const r = await fetch('/api/mcp', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify(body) });
  const out = await r.json().catch(() => ({}));
  if (!r.ok) { const e = new Error(out.error || r.statusText); e.result = out.result; throw e; }
  return out;
}

// Why this place will not run an MCP ("" = fine).
function blockedHere(place, kind, url, onlyHere) {
  if (place !== 'server') return '';
  if (kind === 'local') return t('mcp.blockLocal');
  if (onlyHere || /^https?:\/\/(localhost|127\.|\[::1\])/i.test(url || '')) return t('mcp.blockLoop');
  return '';
}

export default async function mcpView({ rerender }) {
  let st;
  let usage = {}; let jobs = [];
  try {
    [st, usage, jobs] = await Promise.all([load(), loadUsage().catch(() => ({})), loadJobs().then((r) => r.jobs || []).catch(() => [])]);
  } catch (err) { st = { servers: [], place: 'local', error: err.message }; }
  const server = st.place === 'server';
  const runningFor = (name) => jobs.filter((j) => j.mcp === name && j.running);
  const redraw = () => rerender();

  // Where this Console runs and what it may connect.
  const placeCard = h('div.mcp-place', { role: 'img', 'aria-label': t(server ? 'mcp.placeServer' : 'mcp.placeLocal') },
    h('span.mcp-place-ic', server ? glyph.server() : glyph.local()),
    h('strong', t(server ? 'mcp.server' : 'mcp.local')),
    h('span.mcp-allow', { class: 'ok', 'data-tip': t('mcp.allowWeb') }, glyph.url(), icon('check')),
    h('span.mcp-allow', { class: server ? 'no' : 'ok', 'data-tip': t(server ? 'mcp.noLocal' : 'mcp.allowLocal') }, glyph.local(), icon(server ? 'x' : 'check')));

  // ---- connected ----
  const card = (sv) => {
    const blocked = sv.blocked || blockedHere(st.place, sv.kind, sv.url);
    const state = blocked ? 'blocked' : sv.enabled ? 'on' : 'off';
    const n = sv.tools?.length || sv.toolCount || 0;
    const statusBox = h('span.mcp-state', { class: state, 'data-tip': blocked || t('mcp.state.' + state) }, h('i'), h('span.sr-only', blocked || t('mcp.state.' + state)));
    const testBtn = h('button.icon-btn.sm', { type: 'button', 'aria-label': t('mcp.test', { name: sv.name }), 'data-tip': t('mcp.testTip'), disabled: !st.canManage || !!blocked || null,
      onclick: async (e) => {
        e.stopPropagation(); testBtn.disabled = true; c.classList.add('busy');
        try {
          const { result } = await send({ op: 'test', name: sv.name });
          c.classList.remove('busy');
          c.classList.add(result?.connected ? 'flash-ok' : 'flash-bad');
          toast(result?.connected ? t('mcp.testOk', { name: sv.name, n: result.tools?.length || 0 }) : t('mcp.testBad', { name: sv.name, why: result?.message || '' }), null, result?.connected ? 'ok' : 'deny');
        } catch (err) { toast(err.message, null, 'deny'); } finally { testBtn.disabled = false; c.classList.remove('busy'); }
      } }, icon('play'));
    const rmBtn = h('button.icon-btn.sm', { type: 'button', 'aria-label': t('mcp.remove', { name: sv.name }), 'data-tip': t('mcp.removeTip'), disabled: !st.canManage || null,
      onclick: async (e) => {
        e.stopPropagation();
        if (!confirm(t('mcp.removeConfirm', { name: sv.name }))) return;
        try { await send({ op: 'remove', name: sv.name }); toast(t('mcp.removed', { name: sv.name })); redraw(); } catch (err) { toast(err.message, null, 'deny'); }
      } }, icon('x'));
    const c = h('article.mcp-card', { class: state, tabindex: '0', 'aria-label': sv.name, onclick: () => details(sv, blocked), onkeydown: (e) => { if (e.key === 'Enter') details(sv, blocked); } },
      badge(sv.name, CATALOG.find((x) => x.id === sv.name)?.hue),
      h('div.mcp-main',
        h('div.mcp-name', sv.name, statusBox),
        h('div.mcp-meta', sv.kind === 'url' ? glyph.url() : glyph.local(),
          n ? h('span.mcp-tools', { 'data-tip': t('mcp.toolsN', { n }) }, icon('tool'), n) : null,
          sv.auth ? h('span', { 'data-tip': t('mcp.auth.' + sv.auth) }, icon('key')) : null,
          runningFor(sv.name).length ? h('span.mcp-run', { 'data-tip': t('mcp.u.runningN', { n: runningFor(sv.name).length }) }, h('i'), runningFor(sv.name).length) : null),
        usage[sv.name]?.calls ? h('div.mcp-use-line', { 'data-tip': t('mcp.u.sparkTip', { n: usage[sv.name].calls }) },
          spark(usage[sv.name].days || []), h('span', String(usage[sv.name].calls)), h('span.faint', rel(Date.parse(usage[sv.name].last)))) : null),
      h('div.mcp-acts', testBtn, rmBtn));
    return c;
  };
  const list = st.servers?.length
    ? h('div.mcp-grid', st.servers.map(card))
    : h('div.mcp-empty', glyph.url(), glyph.local(), h('p', t('mcp.none')));

  // ---- catalog ----
  const addCard = (it) => {
    const blocked = blockedHere(st.place, it.kind, it.url, it.onlyHere);
    const have = st.servers?.some((x) => x.name === it.id);
    return h('button.mcp-add', { type: 'button', class: blocked ? 'blocked' : have ? 'have' : '', disabled: !st.canManage || null,
      'data-tip': blocked || (have ? t('mcp.have') : t('mcp.addTip', { name: it.name })),
      onclick: () => form(it, blocked) },
    badge(it.name, it.hue), h('span.mcp-add-name', it.name),
    h('span.mcp-add-kind', it.kind === 'local' ? glyph.local() : glyph.url(), blocked ? icon('lock') : have ? icon('check') : icon('plus')));
  };
  const catalog = h('div.stack', { style: { gap: '14px' } },
    ...GROUPS.map((g) => h('div.mcp-group', h('span.label', t('mcp.group.' + g)), h('div.mcp-add-grid', CATALOG.filter((x) => x.group === g).map(addCard)))),
    h('div.mcp-group', h('span.label', t('mcp.group.custom')), h('div.mcp-add-grid',
      h('button.mcp-add.custom', { type: 'button', disabled: !st.canManage || null, onclick: () => form({ id: '', name: '', kind: 'url', url: 'https://' }, '') },
        h('span.mcp-badge.plus', icon('plus')), h('span.mcp-add-name', t('mcp.custom')), h('span.mcp-add-kind', glyph.url())),
      server ? null : h('button.mcp-add.custom', { type: 'button', disabled: !st.canManage || null, onclick: () => form({ id: '', name: '', kind: 'local', command: '', args: [] }, '') },
        h('span.mcp-badge.plus', icon('plus')), h('span.mcp-add-name', t('mcp.customLocal')), h('span.mcp-add-kind', glyph.local())))));

  // ---- drawers ----
  function details(sv, blocked) {
    const u = usage[sv.name] || { calls: 0, days: [], tools: [], projects: [], sessions: [], skills: [] };
    const run = runningFor(sv.name);
    const stat = (n, label, cls) => h('div.mcp-stat', { class: cls || '' }, h('b', n), h('span', label));
    const section = (icn, title, body) => h('section.mcp-sec', h('h3', icon(icn), title), body);
    const none = (k) => h('p.faint.mcp-none', t(k));
    openDrawer(sv.name, h('div.stack', { style: { padding: '16px', gap: '18px' } },
      h('div.mcp-form-head', badge(sv.name, CATALOG.find((x) => x.id === sv.name)?.hue),
        h('span', sv.kind === 'url' ? glyph.url() : glyph.local(), t('mcp.kind.' + sv.kind)),
        h('span.mcp-state', { class: blocked ? 'blocked' : sv.enabled ? 'on' : 'off' }, h('i'), t('mcp.state.' + (blocked ? 'blocked' : sv.enabled ? 'on' : 'off')))),
      blocked ? h('div.callout.danger', icon('lock'), h('div', h('p', blocked))) : null,
      // numbers at a glance
      h('div.mcp-stats',
        stat(String(u.calls || 0), t('mcp.u.calls')),
        stat(String(u.projects.length), t('mcp.u.projects')),
        stat(String(u.sessions.length), t('mcp.u.chats')),
        stat(String(run.length), t('mcp.u.running'), run.length ? 'live' : ''),
        u.blocked ? stat(String(u.blocked), t('mcp.u.blocked'), 'warn') : null),
      u.calls ? h('figure.mcp-chart', spark(u.days || [], true),
        h('figcaption', h('span', t('mcp.u.days14')), h('span', t('mcp.u.last', { when: rel(Date.parse(u.last)) })))) : none('mcp.u.never'),
      section('folder', t('mcp.u.projectsT'), u.projects.length ? barList(u.projects, {
        label: (r) => r.name, icn: 'git',
        sub: (r) => h('span.mono', r.root || ''),
      }) : none('mcp.u.noProjects')),
      section('tool', t('mcp.u.toolsT'), u.tools.length ? barList(u.tools, { label: (r) => r.name }) : (sv.tools?.length ? h('div.mcp-chips', sv.tools.map((x) => h('code', x))) : none('mcp.u.noTools'))),
      section('chat', t('mcp.u.chatsT'), u.sessions.length ? barList(u.sessions, {
        label: (r) => r.name, sub: (r) => rel(Date.parse(r.last)),
      }) : none('mcp.u.noChats')),
      section('sparkle', t('mcp.u.skillsT'), u.skills.length
        ? h('ul.mcp-skills', u.skills.map((k) => h('li', { 'data-tip': k.path }, icon('file'), h('code', k.name))))
        : none('mcp.u.noSkills')),
      section('play', t('mcp.u.runningT'), bgPanel({ filter: (j) => j.mcp === sv.name, compact: true })),
      h('details.mcp-cfg', h('summary', icon('settings'), t('mcp.u.config')),
      h('dl.mcp-dl',
        h('dt', t('mcp.f.kind')), h('dd', sv.kind === 'url' ? glyph.url() : glyph.local(), t('mcp.kind.' + sv.kind)),
        sv.url ? [h('dt', 'URL'), h('dd.mono', sv.url)] : [h('dt', t('mcp.f.command')), h('dd.mono', [sv.command, ...(sv.args || [])].join(' '))],
        sv.env?.length ? [h('dt', t('mcp.f.env')), h('dd.mono', sv.env.join(', '))] : null,
        h('dt', t('mcp.f.state')), h('dd', t('mcp.state.' + (sv.enabled ? 'on' : 'off')))),
      h('p.faint', { style: { fontSize: '12px' } }, t('mcp.reloadNote')))));
  }

  function form(it, blocked) {
    if (blocked) { toast(blocked, null, 'deny'); return; }
    const isLocal = it.kind === 'local';
    const name = h('input.input#mcpName', { value: it.id || '', autocomplete: 'off', spellcheck: 'false', placeholder: 'blender' });
    const url = h('input.input#mcpUrl', { value: it.url || '', autocomplete: 'off', spellcheck: 'false', placeholder: 'https://…/mcp' });
    const cmd = h('input.input.mono#mcpCmd', { value: it.command || '', autocomplete: 'off', spellcheck: 'false', placeholder: 'uvx' });
    const args = h('input.input.mono#mcpArgs', { value: (it.args || []).join(' '), autocomplete: 'off', spellcheck: 'false', placeholder: 'blender-mcp --stdio' });
    const envRows = (it.env || []).map((k) => ({ k, input: h('input.input.mono', { id: 'mcpEnv' + k, autocomplete: 'off', spellcheck: 'false', placeholder: k === 'GODOT_PATH' ? 'C:\\Godot\\godot.exe' : '' }) }));
    const token = h('input.input#mcpTok', { type: 'password', autocomplete: 'off', spellcheck: 'false', placeholder: '••••' });
    const out = h('div.mcp-result', { 'aria-live': 'polite' });
    const go = h('button.btn.primary', { type: 'button', onclick: submit }, icon('link'), t('mcp.connect'));
    const tokenBlocked = it.token && !st.canSendTokens;

    openDrawer(it.name || t(isLocal ? 'mcp.customLocal' : 'mcp.custom'), h('form.stack', { style: { padding: '16px' }, onsubmit: (e) => { e.preventDefault(); submit(); } },
      h('div.mcp-form-head', badge(it.name || '+', it.hue), h('span', isLocal ? glyph.local() : glyph.url(), t('mcp.kind.' + it.kind)),
        it.docs ? h('a.btn.sm.ghost', { href: it.docs, target: '_blank', rel: 'noopener noreferrer' }, icon('external'), t('mcp.docs')) : null),
      isLocal || it.onlyHere ? h('div.callout.route', icon('info'), h('div', h('p', t(it.id ? 'mcp.need.' + it.id : 'mcp.need.local')))) : it.id ? h('div.callout.route', icon('info'), h('div', h('p', t('mcp.need.' + it.id)))) : null,
      h('div.field', h('label', { for: 'mcpName' }, t('mcp.f.name')), name, h('span.hint', t('mcp.f.nameHint'))),
      isLocal
        ? [h('div.field', h('label', { for: 'mcpCmd' }, t('mcp.f.command')), cmd, h('span.hint', t('mcp.f.commandHint'))),
          h('div.field', h('label', { for: 'mcpArgs' }, t('mcp.f.args')), args, h('span.hint', t('mcp.f.argsHint'))),
          ...envRows.map(({ k, input }) => h('div.field', h('label', { for: 'mcpEnv' + k }, k), input))]
        : [h('div.field', h('label', { for: 'mcpUrl' }, 'URL'), url),
          it.token || !it.id ? h('div.field', h('label', { for: 'mcpTok' }, t('mcp.f.token')), token, h('span.hint', t(tokenBlocked ? 'mcp.f.tokenWin' : 'mcp.f.tokenHint'))) : null],
      out),
    [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')), go]);
    if (tokenBlocked) { token.disabled = true; }

    async function submit() {
      const body = { op: 'add', name: name.value.trim().toLowerCase() };
      if (!/^[a-z0-9][a-z0-9_-]{0,39}$/.test(body.name)) { name.setAttribute('aria-invalid', 'true'); name.focus(); toast(t('mcp.f.nameBad'), null, 'deny'); return; }
      if (isLocal) {
        body.command = cmd.value.trim();
        body.args = splitArgs(args.value);
        if (body.args.some((a) => /^<.*>$/.test(a))) { args.focus(); toast(t('mcp.f.fillArgs'), null, 'deny'); return; }
        const env = {}; envRows.forEach(({ k, input }) => { if (input.value.trim()) env[k] = input.value.trim(); });
        if (Object.keys(env).length) body.env = env;
      } else {
        body.url = url.value.trim();
        if (token.value.trim()) body.token = token.value;
      }
      go.disabled = true;
      out.replaceChildren(h('div.mcp-probe', h('span.mcp-dots', h('i'), h('i'), h('i')), t('mcp.probing')));
      try {
        const { result } = await send(body);
        token.value = '';
        const ok = result?.connected || result?.enabled;
        out.replaceChildren(ok
          ? h('div.mcp-probe.ok', icon('check'), h('div', h('b', t('mcp.added', { name: body.name, n: result.tools?.length || 0 })), h('div.mcp-chips', (result.tools || []).slice(0, 24).map((x) => h('code', x)))))
          : h('div.mcp-probe.bad', icon('warn'), h('div', h('b', t('mcp.savedOff')), h('p', result?.message || ''))));
        go.textContent = t('common.close'); go.disabled = false; go.onclick = () => { closeDrawer(); redraw(); };
      } catch (err) {
        token.value = '';
        out.replaceChildren(h('div.mcp-probe.bad', icon('warn'), h('div', h('b', t('mcp.failed')), h('p', err.message))));
        go.disabled = false;
      }
    }
  }

  return { el: h('div.page',
    pageHead(t('mcp.title'), t('mcp.lede'), placeCard),
    st.error ? h('div.callout.danger', icon('warn'), h('div', h('p', st.error))) : null,
    !st.canManage ? h('div.callout', icon('lock'), h('div', h('p', t('mcp.viewOnly')))) : null,
    panel(t('mcp.connected', { n: st.servers?.length || 0 }), t('mcp.connectedSub'), list),
    bgPanel(),
    panel(t('mcp.addTitle'), t(server ? 'mcp.addSubServer' : 'mcp.addSub'), catalog)) };
}

// "a 'b c' d" -> ['a', 'b c', 'd']
function splitArgs(sv) {
  const outArgs = []; let cur = ''; let q = '';
  for (const ch of sv.trim()) {
    if (q) { if (ch === q) q = ''; else cur += ch; }
    else if (ch === '"' || ch === "'") q = ch;
    else if (/\s/.test(ch)) { if (cur) { outArgs.push(cur); cur = ''; } }
    else cur += ch;
  }
  if (cur) outArgs.push(cur);
  return outArgs;
}
