// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Background jobs: what runs for the agent right now (Hermes' background
// commands and local MCP programs), with a Stop button. Used on the Jobs page
// and inside an MCP's details.
import { t } from '../i18n.js';
import { h, s, icon, toast, rel, panel } from '../ui.js';
import { isLive } from '../live.js';

let demoJobs = [
  { id: 'proc:p1', kind: 'process', title: 'npm run dev', cmd: 'npm run dev -- --port 5173', cwd: '/code/atlas-web', project: 'atlas-web', pid: 4120, startedAt: Date.now() - 42 * 60e3, session: '20261118_0912_a1', running: true, canKill: true, children: 3 },
  { id: 'proc:p2', kind: 'process', title: 'pytest -x --watch', cmd: 'pytest -x --watch tests/', cwd: '/code/ledger-api', project: 'ledger-api', pid: 4388, startedAt: Date.now() - 8 * 60e3, session: '20261118_1003_b7', running: true, canKill: true },
  { id: 'proc:p3', kind: 'process', title: 'docker compose up', cmd: 'docker compose up', cwd: '/code/atlas-web', project: 'atlas-web', pid: 0, startedAt: Date.now() - 3 * 3600e3, where: 'sandbox', running: false, canKill: false },
  { id: 'mcp:blender:5012', kind: 'mcp', title: 'blender', mcp: 'blender', cmd: 'uvx blender-mcp', pid: 5012, running: true, canKill: true, children: 1 },
];

export async function loadJobs() {
  if (!isLive()) return { jobs: structuredClone(demoJobs), canKill: true };
  const r = await fetch('/api/bg', { headers: { Accept: 'application/json' } });
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
  return r.json();
}
async function kill(id) {
  if (!isLive()) { await new Promise((r) => setTimeout(r, 600)); demoJobs = demoJobs.filter((j) => j.id !== id); return; }
  const r = await fetch('/api/bg/kill', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify({ id }) });
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
}

const kindIcon = (j) => j.kind === 'mcp'
  ? s('svg', { class: 'mcp-kind', viewBox: '0 0 20 20', 'aria-hidden': 'true' }, s('rect', { x: 2.5, y: 3.5, width: 15, height: 10, rx: 1.8 }), s('path', { d: 'M7 17h6M10 13.5V17' }))
  : s('svg', { class: 'mcp-kind', viewBox: '0 0 20 20', 'aria-hidden': 'true' }, s('path', { d: 'M3.5 5l4 4-4 4M9.5 14h7' }));

// One row: live dot, what, where, since when, Stop.
export function jobRow(j, canKill, after) {
  const stop = h('button.btn.sm.danger.bg-stop', { type: 'button', disabled: !canKill || !j.canKill || !j.running || null,
    'data-tip': !j.running ? t('bg.ended') : j.where ? t('bg.sandbox') : t('bg.stopTip', { pid: j.pid }),
    onclick: async () => {
      if (!confirm(t('bg.confirm', { what: j.title, pid: j.pid }))) return;
      stop.disabled = true; row.classList.add('stopping');
      try { await kill(j.id); toast(t('bg.stopped', { what: j.title })); after?.(); }
      catch (err) { toast(err.message, null, 'deny'); stop.disabled = false; row.classList.remove('stopping'); }
    } }, icon('stop'), t('bg.stop'));
  const row = h('li.bg-row', { class: j.running ? 'on' : 'off' },
    h('span.bg-dot', { 'data-tip': t(j.running ? 'bg.running' : j.where ? 'bg.sandbox' : 'bg.ended') }),
    h('span.bg-kind', { 'data-tip': t('bg.kind.' + j.kind) }, kindIcon(j)),
    h('div.bg-main',
      h('div.bg-title', h('code', j.title), j.children ? h('span.bg-kids', { 'data-tip': t('bg.children', { n: j.children }) }, '+' + j.children) : null),
      h('div.bg-meta',
        j.project ? h('span', { 'data-tip': j.cwd || '' }, icon('folder'), j.project) : j.cwd ? h('span.mono', icon('folder'), j.cwd) : null,
        j.mcp ? h('span', icon('link'), 'MCP ' + j.mcp) : null,
        j.session ? h('span.mono', { 'data-tip': t('bg.session') }, icon('chat'), j.session.slice(-8)) : null,
        j.pid ? h('span.mono', 'pid ' + j.pid) : null,
        j.startedAt ? h('span', icon('clock'), rel(j.startedAt)) : null)),
    stop);
  row.title = j.cmd;
  return row;
}

// The panel: refreshes every 5 s while it is on the page.
export function bgPanel({ filter, title, compact } = {}) {
  const box = h('ul.bg-list', h('li.bg-row.off', h('span.bg-dot'), h('span.muted', t('bg.loading'))));
  const count = h('span.bg-count');
  let timer = 0;
  const draw = async () => {
    if (!box.isConnected && timer) { clearInterval(timer); return; }
    try {
      const res = await loadJobs();
      let jobs = res.jobs || [];
      if (filter) jobs = jobs.filter(filter);
      jobs.sort((a, b) => (b.running - a.running) || (b.startedAt || 0) - (a.startedAt || 0));
      const running = jobs.filter((j) => j.running).length;
      count.replaceChildren(h('i', { class: running ? 'on' : '' }), String(running));
      count.dataset.tip = t('bg.countTip', { n: running });
      box.replaceChildren(...(jobs.length ? jobs.map((j) => jobRow(j, res.canKill, draw)) : [h('li.bg-empty', icon('check'), t(filter ? 'bg.noneHere' : 'bg.none'))]));
      if (res.error) box.append(h('li.bg-empty', icon('warn'), res.error));
    } catch (err) { box.replaceChildren(h('li.bg-empty', icon('warn'), err.message)); }
  };
  draw();
  timer = setInterval(draw, 5000);
  if (compact) return h('div.bg-compact', box);
  return panel(h('span.row', { style: { gap: '8px' } }, title || t('bg.title'), count), t('bg.sub'), box, { flush: true });
}
