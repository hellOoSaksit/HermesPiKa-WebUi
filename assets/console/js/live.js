// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Live mode: when the Console is served by `hpk serve`, real project data replaces
// the demo data in place (same shapes), writes go to the JSON API, and server-sent
// events keep it fresh. Served by the plain dev server, nothing here activates and
// the demo data stays, with a "Demo data" badge in the top bar.
import { api, store, fill, clearObject, emit } from './data.js';

let live = false;
let signin = null; // team mode, not signed in: 'token' or 'header'
let teamOn = false;
let accountsLoaded = false;
let projectId = null;
const read = (k) => { try { return localStorage.getItem(k); } catch { return null; } };
const write = (k, v) => { try { localStorage.setItem(k, v); } catch { /* ignore */ } };

export const isLive = () => live;
export const needsSignIn = () => signin;
export const teamMode = () => teamOn;

// Team mode: sign in with a personal token (hpk team add …), sign out.
export async function signIn(token) {
  const res = await fetch('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify({ token }) });
  const out = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(out.error || res.statusText);
  return out;
}
export async function signOut() {
  await fetch('/api/logout', { method: 'POST', headers: { 'X-HPK': '1' } }).catch(() => {});
  location.reload();
}

async function getJSON(path) {
  const res = await fetch(path, { headers: { Accept: 'application/json' } });
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || res.statusText);
  return res.json();
}

async function postJSON(path, body) {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-HPK': '1' },
    body: JSON.stringify({ project: projectId, ...body }),
  });
  const out = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(out.error || res.statusText);
  return out;
}

const q = () => (projectId ? '?project=' + encodeURIComponent(projectId) : '');

// ---- snapshot ----

function person(id) {
  if (!id || store.people[id]) return;
  const initials = id.replace(/[._-]+/g, ' ').split(' ').filter(Boolean).map((w) => w[0].toUpperCase()).join('').slice(0, 2) || '?';
  store.people[id] = { id, name: id, initials, role: 'member' };
}

function apply(snap) {
  const st = api.state();
  st.live = true;
  projectId = snap.projectId || null;
  st.projectId = projectId;
  st.mode = snap.mode || { value: 'ask', autoUntil: null, scope: 'project' };
  st.teamMode = teamOn;
  st.features = snap.features || { team: false };
  const cur = (snap.projects || []).find((x) => x.id === snap.projectId) || {};
  st.budget = { daily: cur.moneyDaily || 0, monthly: cur.moneyMonthly || 0, spentToday: cur.spentToday || 0 };
  clearObject(store.people);
  store.people[snap.me.id] = { id: snap.me.id, name: snap.me.name, initials: snap.me.initials, role: snap.me.role };
  api.me = () => store.people[snap.me.id];
  // Processes and profiles are not stored by hpk yet (docs/DESIGN.md §25): show one honest entry each.
  fill(store.processes, [{ id: 'hpk-default', name: 'hpk defaults', desc: 'Built-in hpk settings until processes are configured.',
    settings: { mode: 'ask', redLines: [], board: 'hpk', jobs: ['nightly', 'weekly', 'digest'], titleTemplate: '{name}', wikiReview: 'core', maxChildren: 4 } }]);
  fill(store.profiles, [{ id: 'hermes', name: 'Hermes (active profile)', process: 'hpk-default', hermesProfile: 'default', settings: {} }]);
  fill(store.projects, (snap.projects || []).map((p) => ({ ...p, profile: 'hermes', overrides: {} })));
  fill(store.sessions, snap.sessions || []);
  store.sessions.forEach((s) => { person(s.owner); api.chatMeta(s.id); });
  fill(store.claims, snap.claims || []);
  store.claims.forEach((c) => person(c.owner));
  fill(store.overlaps, snap.overlaps || []);
  fill(store.approvals, snap.approvals || []);
  fill(store.wikiTree, snap.wikiTree || []);
  clearObject(store.wikiPages);
  fill(store.drafts, (snap.drafts || []).map((d) => ({ ...d, diff: d.diff || [] })));
  fill(store.notifications, snap.notifications || []);
  fill(store.events, snap.events || []);
  fill(store.lanes, []);
  fill(store.audit, []);
  if (!accountsLoaded) fill(store.accounts, []); // real ones arrive from Hermes (patchPages)
  // Chat logs are fetched per session when opened.
  Object.keys(store.chat).forEach((k) => { if (!store.sessions.some((s) => s.id === k)) delete store.chat[k]; });
}

async function refresh() {
  const snap = await getJSON('/api/snapshot' + q());
  apply(snap);
  ['sessions', 'approvals', 'project-updated', 'notify', 'heartbeat'].forEach((e) => emit(e, projectId));
  // Pages without their own live listeners redraw, unless the person is typing.
  const typing = /INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName || '');
  const onChat = /^#\/(chat|sessions\/)/.test(location.hash);
  // A multi-step form keeps its own state: redrawing it would throw away the person's progress.
  // The team page may be showing a token that appears only once.
  const inForm = /^#\/(projects\/new|team)/.test(location.hash);
  if (!typing && !onChat && !inForm) dispatchEvent(new HashChangeEvent('hashchange'));
}

// ---- chat ----

function toConsole(m, sid) {
  if (m.t === 'summary') return { t: 'summary', at: m.at, session: m.id, done: m.text, decided: '', lessons: '', open: '', question: '' };
  if (m.t === 'clarify') return { t: 'clarify', at: m.at, from: m.id, question: m.text, options: [], answered: null };
  if (m.t === 'permit') return { ...m, redLine: m.redLine || 'control', decision: m.decision ? (m.decision === 'deny' ? 'deny' : m.decision === 'session' ? 'session' : 'once') : undefined };
  if (m.t === 'tool') return { ...m, out: m.out || m.summary || '' };
  return m;
}

async function loadChat(sid) {
  const out = await getJSON(`/api/sessions/${encodeURIComponent(sid)}/chat` + q());
  store.chat[sid] = (out.messages || []).map((m) => toConsole(m, sid));
  const meta = api.chatMeta(sid);
  meta.live = out.live;
  return store.chat[sid];
}

// ---- markdown → the wiki view's blocks ----

function blocks(md) {
  const out = [];
  let list = null;
  let para = [];
  const flush = () => { if (para.length) { out.push(['p', para.join(' ')]); para = []; } };
  for (const raw of String(md || '').split('\n')) {
    const line = raw.trimEnd();
    const h = /^(#{1,3})\s+(.*)$/.exec(line);
    const li = /^\s*[-*]\s+(.*)$/.exec(line);
    if (h) { flush(); list = null; out.push([h[1].length === 1 ? 'h2' : 'h3', h[2]]); continue; }
    if (li) { flush(); if (!list) { list = []; out.push(['ul', list]); } list.push(li[1]); continue; }
    if (!line.trim()) { flush(); list = null; continue; }
    list = null;
    para.push(line.trim());
  }
  flush();
  return out;
}

// ---- patch the data API with live versions ----

function patch() {
  const delay = (v) => Promise.resolve(structuredClone(v));

  api.setProject = (id) => { projectId = id; write('hpk.project', id); refresh().catch(() => {}); };

  api.setMode = (value, minutes = null) => {
    postJSON('/api/mode', { value, minutes: minutes || 0 }).then(refresh).catch((e) => console.warn(e));
    const st = api.state();
    st.mode = { value, autoUntil: minutes ? Date.now() + minutes * 60_000 : null, scope: 'project' };
    emit('mode', value);
  };

  api.resolveApproval = (id, decision) => {
    const i = store.approvals.findIndex((a) => a.id === id);
    const [a] = i >= 0 ? store.approvals.splice(i, 1) : [null];
    const d = decision === 'deny' ? 'deny' : decision === 'session' ? 'session' : 'approve';
    postJSON('/api/approvals/' + encodeURIComponent(id), { decision: d }).then(refresh).catch((e) => { console.warn(e); refresh(); });
    emit('approvals', store.approvals.length);
    return a;
  };

  api.resolveDrafts = (ids, decision) => {
    const nums = ids.map((id) => Number(String(id).replace(/^d-/, ''))).filter(Boolean);
    if (decision === 'approve' || decision === 'discard') {
      postJSON('/api/wiki/drafts', { nums, decision }).then(refresh).catch((e) => console.warn(e));
    }
    ids.forEach((id) => { const i = store.drafts.findIndex((d) => d.id === id); if (i >= 0) store.drafts.splice(i, 1); });
    emit('project-updated', projectId);
  };

  api.wikiPage = async (id) => {
    const meta = store.wikiTree.flatMap((s) => s.pages).find((p) => p.id === id) || { id, path: id, title: id };
    const page = await getJSON('/api/wiki/page' + q() + (q() ? '&' : '?') + 'path=' + encodeURIComponent(id));
    return { ...meta, title: page.title || meta.title, sources: page.sources, body: blocks(page.markdown) };
  };

  api.chat = async (sid) => delay(await loadChat(sid).catch(() => (store.chat[sid] = store.chat[sid] || [])));

  api.sendMessage = (sid, text, how = 'send', attachments = []) => {
    postJSON(`/api/sessions/${encodeURIComponent(sid)}/prompt`, { text, how, attachments }).catch((e) => {
      (store.chat[sid] = store.chat[sid] || []).push({ t: 'note', at: Date.now(), kind: 'info', text: e.message });
      emit('chat', sid);
    });
  };

  // Files: upload into the folder the project's rules name (XHR, for progress), and list a folder.
  api.uploadFiles = (project, files, onProgress) => new Promise((resolve, reject) => {
    const fd = new FormData();
    [...files].forEach((f) => fd.append('file', f, f.name));
    const x = new XMLHttpRequest();
    x.open('POST', '/api/files/upload' + (project ? '?project=' + encodeURIComponent(project) : ''));
    x.setRequestHeader('X-HPK', '1');
    x.responseType = 'json';
    x.upload.onprogress = (e) => { if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total); };
    x.onerror = () => reject(new Error('network'));
    x.onload = () => (x.status >= 200 && x.status < 300 ? resolve(x.response || { files: [] }) : reject(new Error((x.response && x.response.error) || x.statusText)));
    x.send(fd);
  });
  api.listFiles = (project, path = '') => getJSON('/api/files/list?' + new URLSearchParams({ ...(project ? { project } : {}), path }));
  // Folder rules and tidy. A tidy plan answers 409 {state} while the rules are not approved.
  const pq = (project) => (project ? '?project=' + encodeURIComponent(project) : '');
  api.folderRules = (project) => getJSON('/api/files/rules' + pq(project));
  api.tidyPlan = async (project) => {
    const res = await fetch('/api/files/tidy' + pq(project), { headers: { Accept: 'application/json' } });
    const out = await res.json().catch(() => ({}));
    if (res.status === 409 && out.state) return { state: out.state, moves: [], homeless: [], canUndo: false };
    if (!res.ok) throw new Error(out.error || res.statusText);
    return { state: 'ok', ...out };
  };
  api.tidyApply = (project, moves) => postJSON('/api/files/tidy', { project, moves });
  api.tidyUndo = (project) => postJSON('/api/files/tidy/undo', { project });

  // Model switching: hpk serve plans with Hermes' model list and models.dev facts.
  const sp = (sid) => `/api/sessions/${encodeURIComponent(sid)}`;
  api.modelPlan = (sid) => {
    const recall = (api.chatMeta(sid).recall?.pages || []).map((x) => x.path).join(',');
    return getJSON(withProject(sp(sid) + '/models' + (recall ? '?recall=' + encodeURIComponent(recall) : '')));
  };
  api.switchModel = async (sid, model, { confirm = false } = {}) => {
    const recall = (api.chatMeta(sid).recall?.pages || []).map((x) => x.path);
    const out = await postJSON(sp(sid) + '/model', { model, confirm, recall });
    refresh().catch(() => {});
    return out;
  };
  api.askOnce = async (sid, model, text, { confirm = false } = {}) => {
    const sub = await postJSON(sp(sid) + '/ask', { model, text, confirm });
    await refresh();
    return sub.id;
  };

  api.stopStreaming = (sid) => { postJSON(`/api/sessions/${encodeURIComponent(sid)}/cancel`, {}).catch(() => {}); };

  api.newSession = async ({ title, parent = null, firstMessage = '' }) => {
    const s = await postJSON('/api/sessions', { name: title, parent: parent || '', firstMessage, start: !!firstMessage });
    await refresh();
    return s.id;
  };

  api.renameSession = (sid, title) => {
    const s = store.sessions.find((x) => x.id === sid);
    if (s && title) s.title = title;
    postJSON(`/api/sessions/${encodeURIComponent(sid)}/rename`, { title }).catch((e) => console.warn(e));
    emit('sessions');
  };

  api.markAllRead = () => {
    store.notifications.forEach((n) => { n.unread = false; });
    postJSON('/api/notices/read', {}).catch(() => {});
  };

  // Demo-only behaviour that has no meaning against real sessions.
  api.autoSplit = () => {};
  api._simulatePlan = () => {};

  patchPages();
}

// ---- the settings-side pages (gates, privacy, jobs, accounts, structure, settings, team, add project) ----

const withProject = (path) => path + (path.includes('?') ? '&' : '?') + q().slice(1);
let gatesCache = null;
async function gatesData() { gatesCache = await getJSON(withProject('/api/gates')); return gatesCache; }

function patchPages() {
  // Gates: red lines are built-in (locked) plus the project's gates.red_lines_extra.
  api.redLines = async () => (await gatesData()).redLines;
  api.addRedLine = (text) => postJSON('/api/gates/redlines', { add: text });
  api.removeRedLine = (id) => postJSON('/api/gates/redlines', { remove: id });
  api.audit = async () => (await gatesData()).audit;
  api.autoDigest = async () => (await gatesData()).digest;
  api.revertAuto = () => {};

  // Privacy: closing up is immediate; opening up needs the person's confirm.
  api.updateProject = async (id, patch) => {
    const body = {};
    if (patch.privacy) { body.mode = patch.privacy; body.confirm = !!patch.confirm; }
    if (patch.modelPolicy) body.modelPolicy = patch.modelPolicy;
    await postJSON('/api/privacy', body);
    await refresh();
  };
  api.promotions = () => getJSON(withProject('/api/promotions'));
  const promo = (id) => { const [wiki, n] = String(id).split('-'); return { wiki: wiki === 'mine' ? 'mine' : 'team', n: Number(n) }; };
  api.editPromotion = (id, body) => postJSON('/api/promotions', { ...promo(id), decision: 'edit', body });
  api.resolvePromotion = (id, decision = 'discard') => postJSON('/api/promotions', { ...promo(id), decision });

  // Scheduled jobs: hpk's own state plus Hermes' usage log; runs are Hermes cron's.
  let jobsCache = { jobs: [], runs: [] };
  api.jobs = async () => { jobsCache = await getJSON(withProject('/api/jobs')); return jobsCache.jobs; };
  api.runs = async () => jobsCache.runs;
  api.jobsInstallCmd = () => jobsCache.installCmd;
  api.runJob = (id) => postJSON('/api/jobs/' + encodeURIComponent(id) + '/run', {});
  api.toggleJob = async (id) => {
    const j = jobsCache.jobs.find((x) => x.id === id);
    await postJSON(`/api/jobs/${encodeURIComponent(id)}/pause`, { paused: !j.paused });
    j.paused = !j.paused;
    return j;
  };

  // AI accounts: Hermes' credential pool, read with `hermes auth list` (no keys ever).
  api.credentials = async () => {
    const res = await getJSON('/api/accounts');
    accountsLoaded = true;
    fill(store.accounts, (res.credentials || []).map((c) => ({ id: c.id, provider: c.provider, label: c.label, order: c.priority, enabled: true,
      status: c.status ? 'exhausted' : c.current ? 'active' : 'ready' })));
    emit('accounts');
    return res;
  };
  api.credentials().catch(() => {}); // fills the badge and the account shown in chat

  // Structure: Hermes profiles are real; hpk has one process (its defaults) until processes ship.
  api.loadStructure = async () => {
    const [st, set] = await Promise.all([getJSON('/api/structure'), getJSON(withProject('/api/project/settings')).catch(() => ({})), gatesData().catch(() => null)]);
    fill(store.profiles, st.profiles.map((pr) => ({ id: 'hp-' + pr.name, name: pr.name, process: 'hpk-default', hermesProfile: pr.name, settings: { model: pr.model || undefined } })));
    const def = store.profiles[0]?.id;
    const extra = (gatesCache && gatesCache.redLines || []).filter((r) => r.key === 'custom').map((r) => r.text);
    store.projects.forEach((p) => {
      p.profile = def;
      if (p.id === projectId) {
        p.overrides = {};
        if (set.mode) p.overrides.mode = set.mode;
        if (set.board) p.overrides.board = set.board;
        if (set.language) p.overrides.language = set.language;
        if (extra.length) p.overrides.redLines = extra;
      }
    });
    return set;
  };
  api.setOverride = async (id, key, value) => {
    await postJSON('/api/project/settings', { key, value: value === undefined ? null : value });
    await api.loadStructure();
    emit('project-updated', id);
  };
  api.setProfileProcess = () => {};
  api.setProjectSetting = (key, value) => postJSON('/api/project/settings', { key, value });
  api.projectSettings = () => getJSON(withProject('/api/project/settings'));

  // Folders: the picker lists folders on this machine; the moves are solo-only.
  api.listDirs = (path) => getJSON('/api/fs/dirs?path=' + encodeURIComponent(path || ''));
  api.setWikiDir = (project, dir) => postJSON('/api/project/wiki-dir', { project, dir });
  api.setProjectRoot = (project, dir) => postJSON('/api/project/root', { project, dir });
  api.setRecordingsDir = (dir) => postJSON('/api/browser/recordings-dir', { dir });

  // Settings: the doctor is the same one `hpk doctor` runs.
  api.doctor = () => getJSON('/api/doctor');
  api.version = async () => (await getJSON('/api/health')).version;

  // Add a project: the scan reads names and sizes only; adding needs consent.
  api.scanProject = (path, depth) => postJSON('/api/projects/scan', { path, depth });
  api.addProject = (body) => postJSON('/api/projects', { ...body, consent: true });
}

// ---- events ----

function listen() {
  const es = new EventSource('/api/events');
  let timer = null;
  const later = () => { clearTimeout(timer); timer = setTimeout(() => refresh().catch(() => {}), 200); };
  const chatTimers = {};
  const chatLater = (sid) => {
    clearTimeout(chatTimers[sid]);
    chatTimers[sid] = setTimeout(() => loadChat(sid).then(() => emit('chat', sid)).catch(() => {}), 120);
  };
  es.addEventListener('changed', later);
  es.addEventListener('sessions', later);
  es.addEventListener('approvals', later);
  es.addEventListener('chat', (e) => { chatLater(e.data); later(); });
  es.addEventListener('chat-stream', (e) => chatLater(e.data));
  es.addEventListener('browser-needs', (e) => emit('browser-needs', e.data)); // desktop notice (desknotify.js)
}

// connect tries hpk serve; false means demo data.
export async function connect() {
  try {
    const health = await getJSON('/api/health');
    if (health.status !== 'ok') return false;
    const me = await getJSON('/api/me').catch(() => ({}));
    teamOn = !!me.team;
    if (me.team && me.signedIn === false) { signin = me.auth || 'token'; return false; }
    projectId = read('hpk.project');
    let snap = await getJSON('/api/snapshot' + q());
    if (projectId && snap.projectId !== projectId) { projectId = null; snap = await getJSON('/api/snapshot'); }
    live = true;
    apply(snap);
    // A machine that already joined a team server keeps that page live while the team feature is off.
    if (!snap.features?.team) api.state().teamJoined = !!(await getJSON('/api/teamserver').catch(() => ({}))).connected;
    patch();
    listen();
    return true;
  } catch {
    live = false;
    return false;
  }
}
