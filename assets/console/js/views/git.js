// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Git: the project's history as a picture, and the commands people need on it
// (fetch, pull, push, branch, merge, rebase, interactive rebase, cherry-pick,
// revert, reset, stash, undo, conflicts, draft PR). Every action shows the exact
// git commands first, marks red lines, and sets an undo point before anything
// that rewrites history. Served by `hpk serve` it reads the project's repository
// (GET /api/git) and runs every command on it (POST /api/git/action); on the
// plain dev server it runs on sample data (docs/plans/git-sync.md).
import { t } from '../i18n.js';
import { advise } from '../gitadvice.js';
import { api } from '../data.js';
import { isLive } from '../live.js';
import { h, s, icon, lamp, rel, pageHead, toast, menu, openDrawer, closeDrawer } from '../ui.js';

// Authors in git are names, not Console people: a plain initials badge.
const avatar = (name, size = 20) => h('span.git-av', { title: name, style: { width: size + 'px', height: size + 'px', fontSize: Math.round(size * 0.45) + 'px' } }, String(name).slice(0, 2).toUpperCase());

const ROW = 36; // px per commit row (graph and list share it)
const LANE = 18; // px between lanes
const LANE_COLORS = ['var(--accent)', 'var(--route)', 'var(--clear)', 'var(--caution)', 'var(--track)'];
const now = Date.now();
const ago = (min) => now - min * 60_000;

// ---- sample repository (fictional) ----
function sample() {
  const c = (id, msg, author, min, parents, extra = {}) => ({ id, msg, author, at: ago(min), parents, files: extra.files || [], ...extra });
  const commits = [
    c('d3a4b55', 'Validate berth length in the routes', 'olivia', 15, ['c7e81aa'], { files: [['api/routes.ts', 12, 3]] }),
    c('a91f3c2', 'Show berth prices in the booking form', 'nia', 25, ['5d20e7b'], { files: [['web/booking.tsx', 42, 6], ['api/pricing.ts', 18, 2], ['api/routes.ts', 2, 0]] }),
    c('5d20e7b', 'Add pricing table per berth size', 'nia', 70, ['c7e81aa'], { files: [['db/migrations/014_pricing.sql', 31, 0], ['api/pricing.ts', 64, 0]] }),
    c('e4b0d19', 'Export bookings as CSV with a header row', 'max', 40, ['9a6c2f4'], { files: [['api/export.ts', 56, 12], ['api/export.test.ts', 38, 0]] }),
    c('9a6c2f4', 'wip', 'max', 95, ['31f9b07'], { files: [['api/export.ts', 21, 3]] }),
    c('31f9b07', 'Add export endpoint', 'max', 160, ['c7e81aa'], { files: [['api/routes.ts', 9, 1], ['api/export.ts', 40, 0]] }),
    c('c7e81aa', 'Merge pull request #54: berth search', 'olivia', 200, ['b2d4e19', 'f00d311'], { files: [['web/search.tsx', 88, 14]] }),
    c('f00d311', 'Search berths by length and date', 'olivia', 260, ['72ac0b5'], { files: [['web/search.tsx', 88, 14], ['api/search.ts', 47, 0]] }),
    c('b2d4e19', 'Fix time zone in booking emails', 'max', 300, ['72ac0b5'], { files: [['api/mail.ts', 6, 4]] }),
    c('72ac0b5', 'Booking API: create, list, cancel', 'olivia', 1440, ['0e1d9f8'], { files: [['api/bookings.ts', 120, 0]] }),
    c('0e1d9f8', 'Harbor API skeleton', 'olivia', 2880, [], { files: [['README.md', 12, 0]] }),
  ];
  const branches = [
    { name: 'main', tip: 'd3a4b55', remote: 'd3a4b55', base: true },
    { name: 'hpk/harbor-api/1.1-search-olivia', tip: 'f00d311', remote: 'f00d311', owner: 'olivia', session: '1.1' },
    { name: 'hpk/harbor-api/1.2-export-csv-max', tip: 'e4b0d19', remote: '9a6c2f4', owner: 'max', claim: 'api/export.*', session: '1.2' },
    { name: 'hpk/harbor-api/1.3-pricing-nia', tip: 'a91f3c2', remote: 'a91f3c2', owner: 'nia', claim: 'api/pricing.*', session: '1.3', pr: { n: 57, draft: true, ci: 'fail' } },
  ];
  return { commits, branches, head: 'hpk/harbor-api/1.2-export-csv-max', stashes: [{ id: 'stash@{0}', msg: 'half-done README edit', at: ago(50) }, { id: 'stash@{1}', msg: 'try another CSV library', at: ago(4 * 1440) }], me: 'max', undo: [], selected: 'e4b0d19', conflict: null };
}
let repo = sample();
let live = false;

// ---- live: the project's real repository ----
async function load() {
  const p = api.project();
  const res = await fetch('/api/git?project=' + encodeURIComponent(p ? p.id : ''), { headers: { Accept: 'application/json' } });
  const m = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(m.error || res.statusText);
  const keep = repo && live ? repo.selected : null;
  repo = {
    commits: (m.commits || []).map((c) => ({ ...c, parents: c.parents || [], files: c.files || [] })),
    branches: m.branches || [],
    head: m.head || '',
    stashes: m.stashes || [],
    undo: (m.undo || []).map((u) => ({ id: u.id, what: u.what, by: u.by, at: u.at })),
    me: m.me || '',
    dirty: m.dirty || [],
    inProgress: m.inProgress ? { ...m.inProgress, files: m.inProgress.files || [] } : null,
    remoteURL: m.remote || '',
    error: m.error || '',
    project: p ? p.name : 'project',
    selected: null,
    conflict: null,
  };
  const hb = branch(repo.head);
  repo.selected = keep && commitOf(keep) ? keep : hb ? hb.tip : repo.commits[0] && repo.commits[0].id;
}

// send runs one action on the server, then reloads. Errors show what git said.
let busy = false;
async function send(req, done) {
  if (busy) return false;
  busy = true;
  toast(t('git.live.running'));
  let out = {};
  let ok = false;
  try {
    const res = await fetch('/api/git/action', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify({ project: api.project().id, ...req }) });
    out = await res.json().catch(() => ({}));
    ok = res.ok;
    if (!ok && !out.error) out.error = res.statusText;
  } catch (e) { out = { error: e.message }; }
  busy = false;
  await load().catch((e) => { repo.error = e.message; });
  rerender();
  if (!ok) { failed(out); return false; }
  if (out.conflict) { toast(t('git.live.stopped')); conflictView(); return false; }
  toast(out.note || done || t('git.live.ok'));
  return true;
}
function failed(out) {
  openDrawer(t('git.live.failed'), h('div.stack', { style: { padding: '16px' } },
    h('div.callout.danger', icon('warn'), h('div', h('b.git-errtext', out.error))),
    out.commands && out.commands.length ? h('div', h('div.faint.git-cap', t('git.live.ran')), h('pre.git-cmds', out.commands.join('\n'))) : null,
    out.output ? h('div', h('div.faint.git-cap', t('git.live.output')), h('pre.git-cmds.git-out', out.output)) : null),
  [h('button.btn.primary', { type: 'button', onclick: closeDrawer }, t('common.close'))]);
}
const baseName = () => (repo.branches.find((b) => b.base) || { name: 'main' }).name;
const headTip = () => (branch(repo.head) || { tip: repo.commits[0] && repo.commits[0].id }).tip;

// ---- lanes: assign every commit a column, newest first ----
function layout(commits) {
  const lanes = []; // lanes[i] = id of the commit this lane is waiting for
  const pos = {};
  commits.forEach((c, row) => {
    let lane = lanes.indexOf(c.id);
    if (lane < 0) { lane = lanes.indexOf(null); if (lane < 0) lane = lanes.length; }
    lanes.forEach((v, i) => { if (v === c.id && i !== lane) lanes[i] = null; });
    pos[c.id] = { row, lane };
    lanes[lane] = c.parents[0] || null;
    c.parents.slice(1).forEach((p) => {
      if (lanes.includes(p)) return;
      let free = lanes.indexOf(null);
      if (free < 0) free = lanes.length;
      lanes[free] = p;
    });
  });
  const width = Math.max(1, ...Object.values(pos).map((p) => p.lane + 1));
  return { pos, width };
}

const x = (lane) => 12 + lane * LANE;
const y = (row) => row * ROW + ROW / 2;

function graphSVG(commits, pos, width, headTip) {
  const svg = s('svg', { class: 'git-graph', width: 24 + width * LANE, height: commits.length * ROW, 'aria-hidden': 'true' });
  commits.forEach((c) => {
    const a = pos[c.id];
    c.parents.forEach((p) => {
      const b = pos[p];
      if (!b) return;
      const color = LANE_COLORS[(a.lane === b.lane ? a.lane : Math.max(a.lane, b.lane)) % LANE_COLORS.length];
      const d = a.lane === b.lane
        ? `M${x(a.lane)},${y(a.row)} L${x(b.lane)},${y(b.row)}`
        : `M${x(a.lane)},${y(a.row)} C${x(a.lane)},${y(a.row) + ROW * 0.8} ${x(b.lane)},${y(b.row) - ROW * 0.8} ${x(b.lane)},${y(b.row)}`;
      svg.append(s('path', { d, stroke: color, 'stroke-width': 2, fill: 'none' }));
    });
  });
  commits.forEach((c) => {
    const a = pos[c.id];
    const color = LANE_COLORS[a.lane % LANE_COLORS.length];
    const merge = c.parents.length > 1;
    svg.append(s('circle', { cx: x(a.lane), cy: y(a.row), r: c.id === headTip ? 6 : merge ? 5 : 4.5, fill: merge ? 'var(--panel)' : color, stroke: color, 'stroke-width': c.id === headTip ? 3 : 2 }));
  });
  return svg;
}

const short = (b) => b.replace(/^hpk\/[^/]+\//, '');
const branchAt = (id) => repo.branches.filter((b) => b.tip === id);
const remoteAt = (id) => repo.branches.filter((b) => b.remote === id && b.remote !== b.tip);
const commitOf = (id) => repo.commits.find((c) => c.id === id);
const branch = (name) => repo.branches.find((b) => b.name === name);
const pushed = (b) => !!b.remote;

// Commits reachable from a branch tip that are not in another branch.
function only(tipName, notName) {
  const reach = (id, out = new Set()) => { if (!id || out.has(id)) return out; out.add(id); (commitOf(id)?.parents || []).forEach((p) => reach(p, out)); return out; };
  const a = reach(branch(tipName).tip); const b = reach(branch(notName).tip);
  return repo.commits.filter((c) => a.has(c.id) && !b.has(c.id));
}

// ---- the action sheet: what will happen, the exact commands, red line, undo ----
function sheet({ title, what, before, after, cmds, redLine, rewrites, confirm, onConfirm, req, done }) {
  const undoNote = rewrites ? h('div.callout', icon('undo'), h('div', h('b', t('git.undoPoint')), h('p', t('git.undoPointBody')))) : null;
  openDrawer(title, h('div.stack', { style: { padding: '16px' } },
    h('p', what),
    before && after ? h('div.git-ba', h('div', h('div.faint', t('git.before')), before), icon('chevron'), h('div', h('div.faint', t('git.after')), after)) : null,
    h('div', h('div.faint', { style: { fontSize: '12px', marginBottom: '6px' } }, t('git.commands')),
      h('pre.git-cmds', cmds.join('\n'))),
    redLine ? h('div.callout.danger', icon('warn'), h('div', h('b', t('redlines.' + redLine + '.short')), h('p', redLine === 'push' && rewrites ? t('git.rewritePushed') : t('git.redLineBody')))) : null,
    undoNote),
  [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')),
    h(redLine ? 'button.btn.danger' : 'button.btn.primary', { type: 'button', onclick: () => { closeDrawer(); if (live) { send(req, done); return; } if (rewrites) saveUndo(title); onConfirm(); rerender(); } }, confirm)]);
}

// a tiny picture of a line of commits for before/after
function strip(list, tone = 'accent') {
  return h('div.git-strip', list.map((c) => h('div.git-strip-item', h('span.git-dot', { class: tone }), h('span.git-strip-msg', c.msg))));
}

function saveUndo(what) {
  repo.undo.unshift({ id: 'refs/hpk/undo/' + (repo.undo.length + 1), what, at: Date.now(), snapshot: JSON.stringify({ commits: repo.commits, branches: repo.branches, head: repo.head }) });
}

let seq = 0;
const newId = () => (Math.random().toString(16).slice(2, 9) + (seq++).toString(16)).slice(0, 7);

// ---- the commands ----
function doFetch() {
  sheet({ title: t('git.a.fetch'), what: t('git.w.fetch'), cmds: ['git fetch --prune --all'], confirm: t('git.a.fetch'), req: { action: 'fetch' }, done: t('git.live.fetched'),
    onConfirm: () => toast(t('git.done.fetch')) });
}
function doPull(name = repo.head) {
  const b = branch(name);
  sheet({ title: t('git.a.pull'), what: t('git.w.pull', { b: short(b.name) }), cmds: ['git pull --ff-only'], confirm: t('git.a.pull'), req: { action: 'pull', branch: b.name }, done: t('git.live.pulled'),
    onConfirm: () => toast(t('git.done.upToDate')) });
}
function doPush(name = repo.head) {
  const b = branch(name);
  sheet({ title: t('git.a.push'), what: t('git.w.push', { b: short(b.name) }),
    cmds: [b.remote ? 'git push' : `git push -u origin ${b.name}`], redLine: 'push', confirm: t('git.a.push'), req: { action: 'push', branch: b.name }, done: t('git.done.pushed'),
    onConfirm: () => { b.remote = b.tip; toast(t('git.done.pushed')); } });
}
// New branch: a form in the drawer (name follows the project's pattern, checked
// against git's rules as you type; the command updates live).
function doBranch(at = headTip()) {
  const c = commitOf(at);
  const slug = (v) => v.toLowerCase().normalize('NFKD').replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40);
  const next = '1.' + (Math.max(0, ...repo.branches.map((b) => Number((/\/1\.(\d+)-/.exec(b.name) || [])[1] || 0))) + 1);
  const task = h('input.input#gbTask', { placeholder: t('git.nb.taskPh'), autocomplete: 'off' });
  const name = h('input.input.mono#gbName', { autocomplete: 'off', spellcheck: 'false' });
  const err = h('p.git-err', { role: 'alert' });
  const cmd = h('pre.git-cmds');
  const sw = h('input', { type: 'checkbox', checked: true });
  let touched = false;
  const check = (v) => {
    if (!v) return t('git.nb.empty');
    if (/\s/.test(v)) return t('git.nb.space');
    if (/\.\.|[~^:?*\[\\]|@\{|\/\/|^\/|\/$|\.$|\.lock$|^-/.test(v)) return t('git.nb.bad');
    if (repo.branches.some((b) => b.name === v)) return t('git.nb.exists');
    return '';
  };
  const update = () => {
    if (!touched) name.value = 'hpk/' + (live ? slug(repo.project) || 'project' : 'harbor-api') + '/' + next + '-' + (slug(task.value) || 'new-work') + '-' + repo.me;
    const e = check(name.value.trim());
    err.textContent = e;
    create.disabled = !!e;
    cmd.textContent = (sw.checked ? 'git switch -c ' : 'git branch ') + name.value.trim() + ' ' + at;
  };
  task.addEventListener('input', update);
  name.addEventListener('input', () => { touched = true; update(); });
  sw.addEventListener('change', update);
  const create = h('button.btn.primary', { type: 'button', onclick: () => {
    const v = name.value.trim();
    if (check(v)) return;
    closeDrawer();
    if (live) { send({ action: 'branch', name: v, at, switch: sw.checked }, t('git.done.branch', { b: short(v) })); return; }
    repo.branches.push({ name: v, tip: at, owner: repo.me });
    if (sw.checked) repo.head = v;
    toast(t('git.done.branch', { b: short(v) }));
    rerender();
  } }, t('git.a.branch'));
  openDrawer(t('git.a.branch'), h('div.stack', { style: { padding: '16px' } },
    h('div.git-from', h('span.faint', t('git.nb.from')), h('span.git-dot'), h('b', c ? c.msg : at), h('span.mono.faint', at)),
    h('div.field', h('label', { for: 'gbTask' }, t('git.nb.task')), task, h('span.hint', t('git.nb.taskHint'))),
    h('div.field', h('label', { for: 'gbName' }, t('git.nb.name')), name, h('span.hint', t('git.nb.nameHint'))),
    err,
    h('label.switch', sw, t('git.nb.switch')),
    h('div', h('div.faint', { style: { fontSize: '12px', marginBottom: '6px' } }, t('git.commands')), cmd)),
  [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')), create]);
  update();
  setTimeout(() => task.focus(), 40);
}
function doDelete(b) {
  sheet({ title: t('git.deleteBranch'), what: t('git.w.delete', { b: short(b.name) }),
    cmds: [`git branch -d ${b.name}`, ...(b.remote ? [`git push origin --delete ${b.name}`] : [])], redLine: 'delete', rewrites: true, confirm: t('git.deleteBranch'), req: { action: 'delete', branch: b.name, remote: !!b.remote }, done: t('git.live.deleted', { b: short(b.name) }),
    onConfirm: () => { repo.branches = repo.branches.filter((x) => x !== b); if (repo.head === b.name) repo.head = baseName(); } });
}
function doStash() {
  sheet({ title: t('git.a.stash'), what: t('git.w.stash'), cmds: ['git stash push --include-untracked -m "hpk: ' + new Date().toISOString().slice(0, 16) + '"'], confirm: t('git.a.stash'), req: { action: 'stash' }, done: t('git.live.stashed'),
    onConfirm: () => { repo.stashes.unshift({ id: 'stash@{0}', msg: t('git.stashMsg'), at: Date.now() }); repo.stashes.forEach((st, i) => (st.id = `stash@{${i}}`)); } });
}
function doPop(st) {
  sheet({ title: t('git.a.pop'), what: t('git.w.pop', { m: st.msg }), cmds: [`git stash pop ${st.id}`], confirm: t('git.a.pop'), req: { action: 'pop', stash: st.id }, done: t('git.live.popped'),
    onConfirm: () => { repo.stashes = repo.stashes.filter((x) => x !== st); repo.stashes.forEach((x, i) => (x.id = `stash@{${i}}`)); } });
}
function doUndo() {
  const list = repo.undo;
  openDrawer(t('git.a.undo'), h('div.stack', { style: { padding: '16px' } },
    h('p.muted', t('git.w.undo')),
    list.length ? list.map((u) => h('div.row.between.git-undo', h('div', h('b', u.what), h('div.faint.mono', u.id + ' · ' + (u.by ? u.by + ' · ' : '') + rel(u.at))),
      h('button.btn.sm', { type: 'button', onclick: () => {
        if (live) { closeDrawer(); send({ action: 'undo', undo: u.id }, t('git.done.undo')); return; }
        const snap = JSON.parse(u.snapshot); repo.commits = snap.commits; repo.branches = snap.branches; repo.head = snap.head;
        repo.undo = repo.undo.filter((x) => x !== u); closeDrawer(); toast(t('git.done.undo')); rerender();
      } }, t('git.restore')))) : h('p.faint', t('git.noUndo')),
    h('pre.git-cmds', 'git reset --keep refs/hpk/undo/<n>')),
  [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.close'))]);
}
function doCheckout(name) {
  sheet({ title: t('git.a.checkout'), what: t('git.w.checkout', { b: short(name) }), cmds: [`git switch ${name}`], confirm: t('git.a.checkout'), req: { action: 'checkout', branch: name }, done: t('git.live.switched', { b: short(name) }),
    onConfirm: () => (repo.head = name) });
}
function doMerge(from, into) {
  const list = only(from, into);
  sheet({ title: t('git.a.merge'), what: t('git.w.merge', { a: short(from), b: short(into), n: list.length }),
    before: strip(list), after: strip([{ msg: t('git.mergeCommit', { a: short(from) }) }], 'route'),
    cmds: [`git switch ${into}`, `git merge --no-ff ${from}`], redLine: branch(into).base ? 'push' : null, rewrites: live, confirm: t('git.a.merge'), req: { action: 'merge', from, into }, done: t('git.live.merged', { a: short(from), b: short(into) }),
    onConfirm: () => {
      const m = { id: newId(), msg: `Merge ${short(from)} into ${short(into)}`, author: 'you', at: Date.now(), parents: [branch(into).tip, branch(from).tip], files: [] };
      repo.commits.unshift(m); branch(into).tip = m.id; repo.selected = m.id;
    } });
}
function doRebase(name, onto) {
  const b = branch(name);
  const list = only(name, onto);
  const rewrites = pushed(b);
  sheet({ title: t('git.a.rebase'), what: t('git.w.rebase', { a: short(name), b: short(onto), n: list.length }),
    before: strip(list.slice().reverse()), after: strip(list.slice().reverse().map((c) => ({ msg: c.msg + ' ′' })), 'clear'),
    cmds: [`git switch ${name}`, `git rebase ${onto}`, ...(rewrites ? ['git push --force-with-lease'] : [])],
    redLine: rewrites ? 'push' : null, rewrites: true, confirm: t('git.a.rebase'), req: { action: 'rebase', branch: name, onto }, done: t('git.done.rebased', { n: list.length }),
    onConfirm: () => {
      if (name.includes('1.3') && onto.includes('1.2')) { repo.conflict = { name, onto, file: 'api/routes.ts' }; conflictView(); return; }
      replay(name, onto, list.slice().reverse());
      toast(t('git.done.rebased', { n: list.length }));
    } });
}
// move a branch's own commits on top of another tip (new ids: history is rewritten)
function replay(name, onto, oldestFirst, edits = null) {
  const b = branch(name);
  let parent = branch(onto).tip;
  const out = [];
  oldestFirst.forEach((c, i) => {
    const e = edits ? edits[i] : { action: 'pick', msg: c.msg };
    if (e.action === 'drop') return;
    if ((e.action === 'squash' || e.action === 'fixup') && out.length) {
      const last = out[out.length - 1];
      if (e.action === 'squash') last.msg = last.msg + ' + ' + e.msg;
      last.files = [...last.files, ...c.files];
      return;
    }
    const n = { ...c, id: newId(), parents: [parent], msg: e.msg, files: [...c.files], at: Date.now() };
    out.push(n); parent = n.id;
  });
  const gone = new Set(oldestFirst.map((c) => c.id));
  const keep = repo.commits.filter((c) => !gone.has(c.id) || repo.branches.some((o) => o !== b && o.tip === c.id));
  repo.commits = [...out.reverse(), ...keep];
  b.tip = out.length ? out[0].id : branch(onto).tip;
  repo.selected = b.tip;
}
function doInteractive(name = repo.head, base = baseName()) {
  const list = only(name, base).slice().reverse(); // oldest first, like the todo list
  const rows = list.map((c) => ({ c, action: 'pick', msg: c.msg }));
  const b = branch(name);
  const ACTIONS = ['pick', 'reword', 'squash', 'fixup', 'drop'];
  const box = h('div.git-irb');
  const preview = h('div');
  const draw = () => {
    box.replaceChildren(...rows.map((r, i) => h('div.git-irb-row', { draggable: 'true', class: r.action,
      ondragstart: (e) => e.dataTransfer.setData('text/plain', String(i)),
      ondragover: (e) => e.preventDefault(),
      ondrop: (e) => { e.preventDefault(); const from = Number(e.dataTransfer.getData('text/plain')); const [m] = rows.splice(from, 1); rows.splice(i, 0, m); draw(); } },
    h('span.git-grip', { 'aria-hidden': 'true' }, '⋮⋮'),
    h('div.git-seg', { role: 'radiogroup', 'aria-label': t('git.action') }, ACTIONS.map((a) => h('button', { type: 'button', role: 'radio', 'aria-checked': r.action === a ? 'true' : 'false', disabled: i === 0 && (a === 'squash' || a === 'fixup') || null, onclick: () => { r.action = a; draw(); } }, t('git.irb.' + a)))),
    r.action === 'reword' ? h('input.input', { value: r.msg, oninput: (e) => { r.msg = e.target.value; drawPreview(); } }) : h('span.git-irb-msg', r.c.msg),
    h('span.mono.faint', r.c.id),
    h('span.git-irb-move', i > 0 ? h('button.btn.sm.ghost', { type: 'button', 'aria-label': t('git.up'), onclick: () => { [rows[i - 1], rows[i]] = [rows[i], rows[i - 1]]; draw(); } }, icon('up')) : null,
      i < rows.length - 1 ? h('button.btn.sm.ghost', { type: 'button', 'aria-label': t('git.down'), onclick: () => { [rows[i + 1], rows[i]] = [rows[i], rows[i + 1]]; draw(); } }, icon('down')) : null))));
    drawPreview();
  };
  const drawPreview = () => {
    const res = [];
    rows.forEach((r) => {
      if (r.action === 'drop') return;
      if ((r.action === 'squash' || r.action === 'fixup') && res.length) { if (r.action === 'squash') res[res.length - 1].msg += ' + ' + r.msg; return; }
      res.push({ msg: r.msg });
    });
    preview.replaceChildren(h('div.faint', { style: { fontSize: '12px', margin: '10px 0 6px' } }, t('git.irbResult', { n: res.length, was: rows.length })), strip(res, 'clear'));
  };
  draw();
  const todo = () => rows.map((r) => `${r.action} ${r.c.id} ${r.action === 'reword' ? r.msg : r.c.msg}`).join('\n');
  openDrawer(t('git.a.interactive'), h('div.stack', { style: { padding: '16px' } },
    h('p.muted', t('git.w.interactive', { b: short(name), base })),
    box, preview,
    pushed(b) ? h('div.callout.danger', icon('warn'), h('div', h('b', t('redlines.push.short')), h('p', t('git.rewritePushed')))) : null,
    h('div.callout', icon('undo'), h('div', h('b', t('git.undoPoint')), h('p', t('git.undoPointBody'))))),
  [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')),
    h('button.btn.ghost', { type: 'button', onclick: () => { toast(t('git.cmdCopied')); navigator.clipboard?.writeText(`git rebase -i ${base}\n# todo:\n${todo()}`).catch(() => {}); } }, t('git.showCommands')),
    h(pushed(b) ? 'button.btn.danger' : 'button.btn.primary', { type: 'button', onclick: () => {
      closeDrawer();
      if (live) { send({ action: 'interactive', branch: name, onto: base, steps: rows.map((r) => ({ sha: r.c.id, action: r.action, msg: r.msg })) }, t('git.done.interactive')); return; }
      saveUndo(t('git.a.interactive'));
      replay(name, base, rows.map((r) => r.c), rows.map((r) => ({ action: r.action, msg: r.msg })));
      toast(t('git.done.interactive')); rerender();
    } }, t('git.startRebase'))]);
}
function doCherry(c) {
  const b = branch(repo.head);
  sheet({ title: t('git.a.cherry'), what: t('git.w.cherry', { c: c.msg, b: short(b.name) }), before: strip([c]), after: strip([{ msg: c.msg + ' ′' }], 'clear'),
    cmds: [`git cherry-pick ${c.id}`], confirm: t('git.a.cherry'), req: { action: 'cherry', sha: c.id }, done: t('git.live.picked'),
    onConfirm: () => { const n = { ...c, id: newId(), parents: [b.tip], at: Date.now() }; repo.commits.unshift(n); b.tip = n.id; repo.selected = n.id; } });
}
function doRevert(c) {
  const b = branch(repo.head);
  sheet({ title: t('git.a.revert'), what: t('git.w.revert', { c: c.msg }), cmds: [`git revert --no-edit ${c.id}`], confirm: t('git.a.revert'), req: { action: 'revert', sha: c.id }, done: t('git.live.reverted'),
    onConfirm: () => { const n = { id: newId(), msg: `Revert "${c.msg}"`, author: 'you', at: Date.now(), parents: [b.tip], files: c.files.map(([f, a, d]) => [f, d, a]) }; repo.commits.unshift(n); b.tip = n.id; repo.selected = n.id; } });
}
function doReset(c, mode) {
  const b = branch(repo.head);
  const hard = mode === 'hard';
  sheet({ title: t('git.a.reset' + mode), what: t('git.w.reset' + mode, { b: short(b.name), c: c.msg }),
    cmds: [`git reset --${mode} ${c.id}`], req: { action: 'reset', sha: c.id, mode }, done: t('git.live.reset'),
    redLine: hard ? 'delete' : pushed(b) ? 'push' : null, rewrites: true, confirm: t('git.a.reset' + mode),
    onConfirm: () => { b.tip = c.id; repo.selected = c.id; } });
}
function doPR(name, into = baseName()) {
  const b = branch(name);
  sheet({ title: t('git.a.pr'), what: t('git.w.pr', { a: short(name), b: into }),
    cmds: [...(pushed(b) ? [] : [`git push -u origin ${name}`]), `gh pr create --draft --base ${into} --head ${name} --body-file <report of ${b.session || 'this task'}>`, '# GitLab: glab mr create --draft …'],
    redLine: 'push', confirm: t('git.a.pr'), req: { action: 'pr', branch: name, into }, done: t('git.live.prOpened'), onConfirm: () => { b.remote = b.tip; b.pr = { n: 58, draft: true, ci: 'running' }; } });
}

// ---- conflicts (3-way: theirs / result / mine) ----
// Live: the files git stopped on, each with what is already there and what is
// coming in; keep one side per file, then continue (or abort: nothing changes).
function liveConflict() {
  const ip = repo.inProgress;
  if (!ip) return;
  const sides = (f) => h('div.git-3way',
    h('div', h('div.faint', t('git.live.oursHead')), h('pre', f.ours || t('git.live.noFile'))),
    h('div', h('div.faint', t('git.live.theirsHead')), h('pre', f.theirs || t('git.live.noFile'))));
  openDrawer(t('git.live.conflictTitle', { kind: ip.kind, n: ip.files.length }), h('div.stack', { style: { padding: '16px' } },
    h('div.git-cf-meter', { role: 'img', 'aria-label': t('git.live.leftToResolve', { n: ip.files.length }) },
      ip.files.map(() => h('i'))),
    ...ip.files.map((f) => h('div.git-cf-file',
      h('div.row.between', h('b.mono', f.path)),
      sides(f),
      h('div.row', { style: { gap: '6px', flexWrap: 'wrap' } },
        h('button.btn.sm', { type: 'button', onclick: () => { closeDrawer(); send({ action: 'resolve', file: f.path, side: 'ours' }, t('git.live.resolved', { f: f.path })).then((ok) => ok && reopen()); } }, t('git.live.ours')),
        h('button.btn.sm', { type: 'button', onclick: () => { closeDrawer(); send({ action: 'resolve', file: f.path, side: 'theirs' }, t('git.live.resolved', { f: f.path })).then((ok) => ok && reopen()); } }, t('git.live.theirs'))))),
    ip.files.length ? null : h('div.callout', icon('check'), h('div', t('git.live.allResolved'))),
    h('pre.git-cmds', `git checkout --ours|--theirs -- <file>\ngit add <file>\ngit ${ip.kind} --continue\n# ${t('git.orAbort')}\ngit ${ip.kind} --abort`)),
  [h('button.btn.ghost', { type: 'button', onclick: () => { closeDrawer(); send({ action: 'abort' }, t('git.live.aborted')); } }, t('git.abort')),
    h('button.btn.primary', { type: 'button', disabled: ip.files.length ? true : null, onclick: () => { closeDrawer(); send({ action: 'continue' }, t('git.live.continued')); } }, t('git.continue'))]);
}
const reopen = () => { if (repo.inProgress) liveConflict(); };

function conflictView() {
  if (live) return liveConflict();
  const cf = repo.conflict;
  const hunks = [
    { base: "router.get('/bookings', list)", mine: "router.get('/bookings', list)\nrouter.get('/pricing', prices)", theirs: "router.get('/bookings', list)\nrouter.get('/export.csv', exportCsv)", pick: null },
  ];
  const result = h('pre.git-cmds');
  const draw = () => { result.textContent = hunks.map((x) => x.pick === 'mine' ? x.mine : x.pick === 'theirs' ? x.theirs : x.pick === 'both' ? x.theirs + '\n' + x.mine.split('\n').slice(1).join('\n') : '<<<<<<< ' + t('git.unresolved')).join('\n'); };
  draw();
  const picks = [];
  const choice = (k) => { const b = h('button.btn.sm', { type: 'button', 'aria-pressed': 'false', onclick: () => { hunks[0].pick = k; picks.forEach((x) => x.setAttribute('aria-pressed', String(x === b))); draw(); } }, t('git.take.' + k)); picks.push(b); return b; };
  openDrawer(t('git.conflictTitle', { f: cf.file }), h('div.stack', { style: { padding: '16px' } },
    h('div.callout.caution', icon('warn'), h('div', h('b', t('git.conflictStop')), h('p', t('git.conflictBody', { a: short(cf.name), b: short(cf.onto) })))),
    h('div.git-3way',
      h('div', h('div.faint', t('git.theirs', { b: short(cf.onto) })), h('pre', hunks[0].theirs)),
      h('div', h('div.faint', t('git.mine', { b: short(cf.name) })), h('pre', hunks[0].mine))),
    h('div.row', { style: { gap: '6px', flexWrap: 'wrap' } }, choice('theirs'), choice('mine'), choice('both')),
    h('div', h('div.faint', { style: { fontSize: '12px', marginBottom: '6px' } }, t('git.result')), result),
    h('pre.git-cmds', `# ${t('git.conflictCmds')}\ngit add ${cf.file}\ngit rebase --continue\n# ${t('git.orAbort')}\ngit rebase --abort`)),
  [h('button.btn.ghost', { type: 'button', onclick: () => { repo.conflict = null; closeDrawer(); toast(t('git.done.aborted')); rerender(); } }, t('git.abort')),
    h('button.btn.primary', { type: 'button', onclick: () => {
      if (!hunks[0].pick) { toast(t('git.pickFirst')); return; }
      const { name, onto } = repo.conflict; repo.conflict = null; closeDrawer();
      replay(name, onto, only(name, onto).slice().reverse()); toast(t('git.done.rebased', { n: 2 })); rerender();
    } }, t('git.continue'))]);
}

// ---- branch labels: click for actions, drag onto another to merge / rebase / PR ----
function chip(b) {
  const el = h('span.git-chip', { class: (b.name === repo.head ? 'head ' : '') + (b.base ? 'base' : ''), draggable: 'true', tabindex: '0', role: 'button',
    title: t('git.dragHint'),
    ondragstart: (e) => { e.dataTransfer.setData('text/hpk-branch', b.name); el.classList.add('dragging'); },
    ondragend: () => el.classList.remove('dragging'),
    ondragover: (e) => { if (e.dataTransfer.types.includes('text/hpk-branch')) { e.preventDefault(); el.classList.add('drop'); } },
    ondragleave: () => el.classList.remove('drop'),
    ondrop: (e) => { e.preventDefault(); el.classList.remove('drop'); const from = e.dataTransfer.getData('text/hpk-branch'); if (from && from !== b.name) dropMenu(el, from, b.name); },
    onclick: (e) => { e.stopPropagation(); branchMenu(el, b); },
    onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); branchMenu(el, b); } } },
  b.name === repo.head ? icon('check') : null, short(b.name),
  b.remote === b.tip ? h('span.git-sync', { title: t('git.inSync') }, '⇅') : b.remote ? h('span.git-sync.ahead', { title: t('git.ahead') }, '↑') : h('span.git-sync.local', { title: t('git.localOnly') }, '●'),
  b.pr ? h('span.git-pr', { class: b.pr.ci }, '#' + b.pr.n) : null,
  b.owner ? avatar(b.owner, 16) : null);
  return el;
}
function dropMenu(anchor, from, onto) {
  menu(anchor, [
    { label: t('git.dropMerge', { a: short(from), b: short(onto) }), icon: 'git', onSelect: () => doMerge(from, onto) },
    { label: t('git.dropRebase', { a: short(from), b: short(onto) }), icon: 'layers', onSelect: () => doRebase(from, onto) },
    { label: t('git.dropPR', { a: short(from), b: short(onto) }), icon: 'external', onSelect: () => doPR(from, onto) },
  ]);
}
function branchMenu(anchor, b) {
  menu(anchor, [
    b.name !== repo.head ? { label: t('git.a.checkout'), icon: 'check', onSelect: () => doCheckout(b.name) } : null,
    !b.base ? { label: t('git.updateFromBase'), icon: 'git', onSelect: () => doMerge(baseName(), b.name) } : null,
    !b.base ? { label: t('git.rebaseOnBase'), icon: 'layers', onSelect: () => doRebase(b.name, baseName()) } : null,
    !b.base ? { label: t('git.a.interactive'), icon: 'edit', onSelect: () => doInteractive(b.name, baseName()) } : null,
    !b.base && !b.pr ? { label: t('git.a.pr'), icon: 'external', onSelect: () => doPR(b.name) } : null,
    { sep: true },
    !b.base ? { label: t('git.deleteBranch'), icon: 'x', danger: true, onSelect: () => doDelete(b) } : null,
  ]);
}
function commitMenu(anchor, c) {
  const onHead = c.id === headTip();
  menu(anchor, [
    { label: t('git.a.branch'), icon: 'plus', onSelect: () => doBranch(c.id) },
    !onHead ? { label: t('git.a.cherry'), icon: 'copy', onSelect: () => doCherry(c) } : null,
    { label: t('git.a.revert'), icon: 'undo', onSelect: () => doRevert(c) },
    { sep: true },
    !onHead ? { label: t('git.a.resetsoft'), icon: 'back', onSelect: () => doReset(c, 'soft') } : null,
    !onHead ? { label: t('git.a.resetmixed'), icon: 'back', onSelect: () => doReset(c, 'mixed') } : null,
    !onHead ? { label: t('git.a.resethard'), icon: 'warn', danger: true, onSelect: () => doReset(c, 'hard') } : null,
    { sep: true },
    { label: t('git.copySha'), icon: 'copy', onSelect: () => { navigator.clipboard?.writeText(c.id).catch(() => {}); toast(c.id); } },
  ]);
}

// ---- advice: fixed rules, no AI (gitadvice.js) ----
function act([kind, a, b]) {
  switch (kind) {
    case 'rebase': return doRebase(a, b);
    case 'merge': return doMerge(a, b);
    case 'interactive': return doInteractive(a, b);
    case 'push': return doPush(a);
    case 'pull': return doPull(a);
    case 'delete': return doDelete(branch(a));
    case 'checkout': return doCheckout(a);
    case 'branchFrom': return doBranch(branch(a).tip);
    case 'pop': return doPop(repo.stashes.find((x) => x.id === a));
    case 'openPR': { const pr = branch(a).pr; if (live && pr.url) { window.open(pr.url, '_blank', 'noopener'); return; } return toast(t('git.adv.openPRNote', { n: pr.n })); }
  }
}
const actLabel = ([kind, a, b]) => t('git.adv.act.' + kind, { a: a && short(a), b: b && short(b) });
let showAll = false;
function advicePanel() {
  const list = advise({ commits: repo.commits, branches: repo.branches, head: repo.head, stashes: repo.stashes, me: repo.me });
  if (!list.length) return h('div.callout', icon('check'), h('div', t('git.adv.none')));
  const shown = showAll ? list : list.slice(0, 4);
  return h('section.panel.git-advice',
    h('div.panel-head', h('h2', t('git.adv.title', { n: list.length })), h('span.sub', t('git.adv.sub'))),
    h('div.panel-body.git-adv-list', shown.map((a) => h('div.git-adv', { class: a.level },
      lamp(a.level === 'info' ? 'route' : a.level),
      h('div.git-adv-body',
        h('div.git-adv-title', t('git.adv.' + a.title[0], a.title[1] || {})),
        h('div.git-adv-why', t('git.adv.' + a.why[0], { ...(a.why[1] || {}), reason: a.why[1] && a.why[1].reason ? t('git.adv.reason.' + a.why[1].reason) : '' })),
        h('div.git-adv-acts', a.actions.map((x, i) => h(i === 0 ? 'button.btn.sm.primary' : 'button.btn.sm', { type: 'button', onclick: () => act(x) }, actLabel(x))),
          h('span.git-rule', { title: t('git.adv.ruleHint') }, a.rule)))))),
    list.length > 4 ? h('div.panel-foot', h('button.btn.sm.ghost', { type: 'button', onclick: () => { showAll = !showAll; rerender(); } }, showAll ? t('git.adv.less') : t('git.adv.more', { n: list.length - 4 }))) : null);
}

// ---- page ----
let mount = null;
function rerender() { if (mount) mount.replaceWith(mount = render()); }

// Live, nothing to draw: not a repository, no commits yet, or the read failed.
function emptyPage(msg) {
  return h('div.page.git-page',
    pageHead(t('git.title'), t('git.lede'), [refreshBtn()]),
    h('div.callout.caution', icon('info'), h('div', h('b', msg))));
}
const refreshBtn = () => h('button.btn.sm.ghost', { type: 'button', 'aria-label': t('git.live.refresh'), title: t('git.live.refresh'),
  onclick: async () => { await load().catch((e) => { repo.error = e.message; }); rerender(); } }, icon('scan'));

// The state of the checkout at a glance: the branch you are on, uncommitted
// files, an operation stopped on a conflict.
function statusStrip() {
  const items = [];
  if (repo.inProgress) {
    items.push(h('button.git-stat.danger', { type: 'button', onclick: conflictView },
      icon('warn'), h('b', t('git.live.stoppedShort', { kind: repo.inProgress.kind })), h('span.tag.danger', String(repo.inProgress.files.length)), h('span', t('git.live.resolve'))));
  }
  if (repo.dirty && repo.dirty.length) {
    items.push(h('span.git-stat.caution', { title: repo.dirty.slice(0, 20).join('\n') + (repo.dirty.length > 20 ? '\n…' : '') },
      icon('edit'), h('b', String(repo.dirty.length)), h('span', t('git.live.dirty'))));
  }
  if (!repo.head) items.push(h('span.git-stat.caution', icon('info'), h('span', t('git.live.detached'))));
  return items.length ? h('div.git-status', items) : null;
}

function render() {
  if (live && repo.error) return emptyPage(repo.error === 'not a git repository' ? t('git.live.notRepo') : repo.error);
  if (live && !repo.commits.length) return emptyPage(t('git.live.noCommits'));
  const { pos, width } = layout(repo.commits);
  const head = branch(repo.head) || { name: '', tip: '', remote: '' };
  const sel = commitOf(repo.selected) || repo.commits[0];
  const list = h('div.git-list', repo.commits.map((c) => h('div.git-row', { class: (c.id === sel.id ? 'sel ' : '') + (c.id === head.tip ? 'head' : ''), tabindex: '0', role: 'option', 'aria-selected': c.id === sel.id ? 'true' : 'false',
    onclick: () => { repo.selected = c.id; rerender(); },
    oncontextmenu: (e) => { e.preventDefault(); commitMenu(e.currentTarget, c); },
    onkeydown: (e) => { if (e.key === 'Enter') { repo.selected = c.id; rerender(); } } },
  h('span.git-msgcell', ...branchAt(c.id).map(chip), ...remoteAt(c.id).map((b) => h('span.git-chip.remote', { title: t('git.remoteHint') }, 'origin/' + short(b.name))), h('span.git-msg', c.msg)),
  h('span.git-who', avatar(c.author, 18)),
  h('span.git-when.faint', rel(c.at)),
  h('span.mono.faint.git-sha', c.id),
  h('button.btn.sm.ghost.git-more', { type: 'button', 'aria-label': t('git.commitActions'), onclick: (e) => { e.stopPropagation(); commitMenu(e.currentTarget, c); } }, icon('more')))));
  const graph = h('div.git-graphwrap', graphSVG(repo.commits, pos, width, head.tip), list);
  graph.setAttribute('role', 'listbox');
  graph.setAttribute('aria-label', t('git.history'));

  const total = sel.files.reduce((n, [, a, d]) => n + a + d, 0) || 1;
  const details = h('section.panel.git-details',
    h('div.panel-head', h('h2', sel.msg), h('span.sub.mono', sel.id)),
    h('div.panel-body.stack',
      h('div.row', { style: { gap: '8px' } }, avatar(sel.author, 22), h('b', sel.author), h('span.faint', rel(sel.at)), sel.parents.length > 1 ? h('span.tag.route', t('git.mergeTag')) : null),
      h('div.faint', { style: { fontSize: '12px' } }, t('git.filesChanged', { n: sel.files.length })),
      h('div.git-files', sel.files.map(([f, a, d]) => h('div.git-file', h('span.mono', f),
        h('span.git-bar', h('i.add', { style: { width: (a / total) * 100 + '%' } }), h('i.del', { style: { width: (d / total) * 100 + '%' } })),
        h('span.mono.git-num', h('span.add', '+' + a), ' ', h('span.del', '−' + d))))),
      h('div.row', { style: { gap: '6px', flexWrap: 'wrap' } },
        sel.id !== head.tip ? h('button.btn.sm', { type: 'button', onclick: () => doCherry(sel) }, t('git.a.cherry')) : null,
        h('button.btn.sm', { type: 'button', onclick: () => doRevert(sel) }, t('git.a.revert')),
        h('button.btn.sm', { type: 'button', onclick: () => doBranch(sel.id) }, t('git.a.branch')),
        h('button.btn.sm.ghost', { type: 'button', onclick: (e) => commitMenu(e.currentTarget, sel) }, icon('more'), t('git.more')))));

  const noHead = !head.name;
  const bar = (key, ic, fn, badge, off) => h('button.btn.git-tool', { type: 'button', onclick: fn, title: t('git.tip.' + key), disabled: off ? true : null }, icon(ic), h('span', t('git.a.' + key)), badge ? h('span.tag', badge) : null);
  const ahead = noHead ? null : head.remote && head.remote !== head.tip ? '↑' : !head.remote ? '●' : null;
  const toolbar = h('div.git-toolbar', { role: 'toolbar', 'aria-label': t('git.commandsBar') },
    bar('fetch', 'down', doFetch), bar('pull', 'down', () => doPull(), null, noHead || !head.remote), bar('push', 'up', () => doPush(), ahead, noHead),
    h('span.git-sep'),
    bar('branch', 'plus', () => doBranch()), bar('interactive', 'edit', () => doInteractive(repo.head, baseName()), null, noHead || head.base),
    bar('stash', 'queue', doStash, null, live && !(repo.dirty && repo.dirty.length)),
    h('span.git-sep'),
    bar('undo', 'undo', doUndo, repo.undo.length ? String(repo.undo.length) : null));

  const branches = h('section.panel.git-side',
    h('div.panel-head', h('h2', t('git.branches'))),
    h('div.panel-body.stack', repo.branches.map((b) => h('div.git-branch', { class: b.name === repo.head ? 'head' : '' },
      lamp(b.pr ? (b.pr.ci === 'fail' ? 'danger' : 'route') : b.name === repo.head ? 'clear' : 'idle'),
      h('div', h('div.cell-title', short(b.name)),
        h('div.cell-sub', [b.session ? t('git.task', { n: b.session }) : null, b.claim ? t('git.claim', { p: b.claim }) : null, b.pr ? t('git.prLine', { n: b.pr.n, ci: t('git.ci.' + b.pr.ci) }) : null].filter(Boolean).join(' · '))),
      b.owner ? avatar(b.owner, 20) : null)),
    repo.stashes.length ? h('div', h('div.faint', { style: { fontSize: '12px', margin: '8px 0 4px' } }, t('git.stashes')),
      repo.stashes.map((st) => h('div.row.between', h('span', h('span.mono.faint', st.id + ' '), st.msg), h('button.btn.sm.ghost', { type: 'button', onclick: () => doPop(st) }, t('git.a.pop'))))) : null));

  return h('div.page.git-page',
    pageHead(t('git.title'), t('git.lede'), [live ? refreshBtn() : h('span.tag.caution', t('git.sample'))]),
    statusStrip(),
    toolbar,
    live && repo.inProgress ? null : advicePanel(),
    h('p.faint.git-hint', t('git.hint')),
    h('div.git-layout', branches, h('section.panel.git-main', graph), details));
}

export default async function gitView() {
  live = isLive();
  if (live) await load().catch((e) => { repo = { ...repo, commits: [], branches: [], error: e.message }; });
  mount = render();
  return { el: h('div', mount) };
}
