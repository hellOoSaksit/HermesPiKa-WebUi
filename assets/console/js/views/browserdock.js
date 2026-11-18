// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// The built-in browser, live: a panel docked on the right or along the top of
// every page (and full size on #/browser). It shows what the AI is opening and
// clicking as it happens; the person can take over with one click and drive it
// (click, type, scroll) until they hand it back.
import { t } from '../i18n.js';
import { h, s, icon, toast, rel } from '../ui.js';
import { isLive } from '../live.js';
import { folderRow } from '../folderpick.js';
import { api } from '../data.js';

const read = (k, d) => { try { return localStorage.getItem(k) ?? d; } catch { return d; } };
const write = (k, v) => { try { localStorage.setItem(k, v); } catch { /* ignore */ } };

const TOOL_ICON = { open: 'globe', click: 'play', type: 'edit', fill_secret: 'key', press: 'chevron', scroll: 'down', back: 'back', forward: 'chevron', reload: 'undo', wait: 'clock', screenshot: 'eye', snapshot: 'file', driver: 'steer' };

async function post(body) {
  const r = await fetch('/api/browser/control', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify(body) });
  const out = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(out.error || r.statusText);
  return out;
}

// One live view, used by the dock and by the browser page.
export function liveView({ big = false } = {}) {
  let st = { running: false, driver: 'agent', width: 1280, height: 800 };
  const img = h('img.bl-frame', { alt: '', draggable: 'false' });
  const ripple = h('span.bl-ripple');
  const empty = h('div.bl-empty', globeArt(), h('p', t('browser.idle')),
    h('button.btn.sm', { type: 'button', onclick: () => post({ op: 'start' }).catch((e) => toast(e.message, null, 'deny')) }, icon('play'), t('browser.start')));
  const screen = h('div.bl-screen', { tabindex: '0', 'aria-label': t('browser.screenAria') }, img, ripple, empty);
  const url = h('input.bl-url', { type: 'text', spellcheck: 'false', autocomplete: 'off', 'aria-label': t('browser.url'), placeholder: 'https://',
    onkeydown: (e) => { if (e.key === 'Enter') { e.preventDefault(); act({ tool: 'open', url: url.value.trim() }); url.blur(); } } });
  const lock = h('span.bl-lock');
  const who = h('button.bl-who', { type: 'button', onclick: () => post({ op: 'driver', driver: st.driver === 'human' ? 'agent' : 'human' }).catch((e) => toast(e.message, null, 'deny')) });
  const busy = h('div.bl-busy', { 'aria-live': 'polite' });
  const steps = h('ol.bl-steps', { 'aria-label': t('browser.steps') });
  const nav = (tool, ic, label) => h('button.icon-btn.sm', { type: 'button', 'aria-label': label, 'data-tip': label, onclick: () => act({ tool }) }, icon(ic));
  const stopBtn = h('button.icon-btn.sm', { type: 'button', 'aria-label': t('browser.close'), 'data-tip': t('browser.close'), onclick: () => post({ op: 'stop' }).catch(() => {}) }, icon('stop'));
  const bar = h('div.bl-bar', nav('back', 'back', t('browser.back')), nav('reload', 'undo', t('browser.reload')), h('div.bl-addr', lock, url), who, stopBtn);

  async function act(a) {
    try { await post({ op: 'act', ...a }); } catch (e) { toast(e.message, null, 'deny'); }
  }
  function drawState() {
    const human = st.driver === 'human';
    screen.classList.toggle('human', human);
    screen.classList.toggle('off', !st.running);
    empty.hidden = !!st.running;
    if (document.activeElement !== url) url.value = st.url && st.url !== 'about:blank' ? st.url : '';
    url.readOnly = !human && !!st.running && st.driver === 'agent' && !!st.busy;
    lock.className = 'bl-lock ' + (st.url?.startsWith('https:') ? 'secure' : st.url?.startsWith('http:') ? 'plain' : '');
    lock.replaceChildren(icon(st.url?.startsWith('https:') ? 'lock' : 'globe'));
    who.className = 'bl-who ' + (human ? 'human' : 'agent');
    who.replaceChildren(h('i'), h('span', t(human ? 'browser.youDrive' : 'browser.aiDrives')), h('b', t(human ? 'browser.handBack' : 'browser.takeOver')));
    busy.replaceChildren(...(st.busy ? [h('span.bl-dots', h('i'), h('i'), h('i')), h('span', st.busy)] : st.loading ? [h('span.bl-dots', h('i'), h('i'), h('i')), h('span', t('browser.loading'))] : []));
    busy.classList.toggle('on', !!(st.busy || st.loading));
  }
  function addStep(sp) {
    const li = h('li.bl-step', { class: (sp.by || 'agent') + (sp.error ? ' err' : '') },
      h('span.bl-step-ic', icon(TOOL_ICON[sp.tool] || 'dot')),
      h('div.bl-step-main', h('div', h('b', t('browser.tool.' + sp.tool) || sp.tool), sp.label ? h('span', ' ' + sp.label) : null),
        sp.error ? h('div.bl-step-err', sp.error) : sp.url ? h('div.bl-step-url', sp.url) : null),
      h('span.bl-step-by', { 'data-tip': t(sp.by === 'human' ? 'browser.byYou' : 'browser.byAI') }, sp.by === 'human' ? icon('steer') : icon('sparkle')));
    steps.prepend(li);
    while (steps.children.length > (big ? 60 : 14)) steps.lastChild.remove();
  }
  function showClick(x, y) {
    const r = img.getBoundingClientRect();
    if (!r.width) return;
    ripple.style.left = (x / st.width) * r.width + 'px';
    ripple.style.top = (y / st.height) * r.height + 'px';
    ripple.classList.remove('go'); void ripple.offsetWidth; ripple.classList.add('go');
  }

  // The person drives: clicks, wheel, keys and paste go to the page.
  const pos = (e) => { const r = img.getBoundingClientRect(); return { x: ((e.clientX - r.left) / r.width) * st.width, y: ((e.clientY - r.top) / r.height) * st.height }; };
  screen.addEventListener('click', (e) => {
    if (st.driver !== 'human' || !st.running || e.target === empty || empty.contains(e.target)) return;
    screen.focus(); const p = pos(e); showClick(p.x, p.y); act({ tool: 'click', x: p.x, y: p.y });
  });
  let wheelAt = 0;
  screen.addEventListener('wheel', (e) => {
    if (st.driver !== 'human' || !st.running) return;
    e.preventDefault();
    if (Date.now() - wheelAt < 250) return; wheelAt = Date.now();
    const p = pos(e); act({ tool: 'scroll', dir: e.deltaY < 0 ? 'up' : 'down', x: p.x, y: p.y });
  }, { passive: false });
  let buf = ''; let bufT = 0;
  const flush = () => { if (buf) { const text = buf; buf = ''; act({ tool: 'type', text }); } };
  const KEYS = { Enter: 1, Tab: 1, Escape: 1, Backspace: 1, Delete: 1, ArrowDown: 1, ArrowUp: 1, ArrowLeft: 1, ArrowRight: 1, PageDown: 1, PageUp: 1, Home: 1, End: 1 };
  screen.addEventListener('keydown', (e) => {
    if (st.driver !== 'human' || !st.running || e.ctrlKey || e.metaKey || e.altKey) return;
    if (KEYS[e.key]) { e.preventDefault(); flush(); act({ tool: 'press', key: e.key }); return; }
    if (e.key.length === 1) { e.preventDefault(); buf += e.key; clearTimeout(bufT); bufT = setTimeout(flush, 250); }
  });
  screen.addEventListener('paste', (e) => {
    if (st.driver !== 'human' || !st.running) return;
    e.preventDefault(); flush(); act({ tool: 'type', text: e.clipboardData.getData('text') });
  });

  // live stream
  let es = null; let loadedRun = '';
  const open = () => {
    if (es || !isLive()) return;
    es = new EventSource('/api/browser/stream');
    es.addEventListener('frame', (e) => { img.src = 'data:image/jpeg;base64,' + e.data; });
    es.addEventListener('state', (e) => {
      const prev = st; st = JSON.parse(e.data); drawState();
      // steps of this run so far (a page opened mid-run starts with its history)
      if (st.run && st.run !== loadedRun) {
        loadedRun = st.run;
        fetch('/api/browser/runs/' + st.run).then((r) => r.json()).then((j) => { steps.replaceChildren(); (j.steps || []).slice(big ? -60 : -14).forEach(addStep); }).catch(() => {});
      }
      el.dispatchEvent(new CustomEvent('bstate', { detail: { st, prev } }));
    });
    es.addEventListener('step', (e) => addStep(JSON.parse(e.data)));
    es.addEventListener('click', (e) => { const p = JSON.parse(e.data); showClick(p.x, p.y); });
    es.onerror = () => { /* the browser reconnects by itself */ };
  };
  const close = () => { es?.close(); es = null; };
  const el = h('div.bl', { class: big ? 'big' : '' }, bar, h('div.bl-stage', screen, busy), h('div.bl-steps-wrap', steps));
  el.open = open; el.close = close;
  drawState();
  if (!isLive()) { empty.replaceChildren(globeArt(), h('p', t('browser.needServe'))); }
  return el;
}

function globeArt() {
  return s('svg', { class: 'bl-art', viewBox: '0 0 64 64', 'aria-hidden': 'true' },
    s('rect', { x: 6, y: 10, width: 52, height: 40, rx: 6 }), s('path', { d: 'M6 20h52' }),
    s('circle', { cx: 12, cy: 15, r: 1.6 }), s('circle', { cx: 17, cy: 15, r: 1.6 }),
    s('path', { d: 'M24 38l6-6 4 4 8-9' }), s('path', { d: 'M40 56l4-8 3 3 5-2-6-4 4-3-8 1z', class: 'cur' }));
}

// ---- the dock ----
let dock = null;
export function initBrowserDock() {
  if (dock || !document.body) return;
  const view = liveView();
  const place = () => read('hpk.browserDock', 'right');
  const head = h('div.bd-head',
    h('strong', icon('globe'), t('browser.title')),
    h('div.row', { style: { gap: '2px', marginLeft: 'auto' } },
      h('button.icon-btn.sm', { type: 'button', 'aria-label': t('browser.dockSwap'), 'data-tip': t('browser.dockSwap'), onclick: () => { write('hpk.browserDock', place() === 'right' ? 'top' : 'right'); layout(); } }, icon('layers')),
      h('a.icon-btn.sm', { href: '#/browser', 'aria-label': t('browser.full'), 'data-tip': t('browser.full'), onclick: () => toggle(false) }, icon('external')),
      h('button.icon-btn.sm', { type: 'button', 'aria-label': t('common.close'), onclick: () => toggle(false) }, icon('close'))));
  dock = h('aside.bdock', { 'aria-label': t('browser.title'), hidden: true }, head, view);
  document.body.append(dock);
  function layout() { dock.dataset.place = place(); document.documentElement.dataset.bdock = dock.hidden ? '' : place(); }
  function toggle(on = dock.hidden) {
    if (location.hash.startsWith('#/browser') && on) { return; }
    dock.hidden = !on; write('hpk.browserDockOpen', on ? '1' : '');
    if (on) view.open(); else view.close();
    layout(); window.dispatchEvent(new CustomEvent('bdock'));
  }
  dock.toggle = toggle;
  // open by itself when the AI starts browsing (if the person allows it)
  view.addEventListener('bstate', (e) => {
    const { st, prev } = e.detail;
    if (dock.hidden && st.busy && !prev.busy && read('hpk.browserAuto', '1') === '1' && !location.hash.startsWith('#/browser')) toggle(true);
  });
  if (isLive()) {
    // a quiet watcher so the dock can open itself
    view.open();
    if (read('hpk.browserDockOpen', '') !== '1') { dock.hidden = true; }
  }
  layout();
  addEventListener('hashchange', () => { if (location.hash.startsWith('#/browser') && !dock.hidden) toggle(false); });
}
export const toggleBrowserDock = () => { initBrowserDock(); dock?.toggle(); };
export const browserDockOpen = () => !!dock && !dock.hidden;

// The full page: #/browser
export default async function browserPage() {
  const { pageHead, panel } = await import('../ui.js');
  const view = liveView({ big: true });
  setTimeout(() => view.open(), 0);
  const runs = h('div.br-runs', h('p.muted', t('browser.loadingRuns')));
  const auto = h('label.switch', h('input', { type: 'checkbox', checked: read('hpk.browserAuto', '1') === '1' || null, onchange: (e) => write('hpk.browserAuto', e.target.checked ? '1' : '0') }), t('browser.autoOpen'));
  loadRuns(runs);
  const el = h('div.page',
    pageHead(t('browser.title'), t('browser.lede'), auto),
    !isLive() ? h('div.callout', icon('info'), h('div', h('p', t('browser.needServe')))) : null,
    view,
    isLive() ? recordingsRow() : null,
    panel(t('browser.runs'), t('browser.runsSub'), runs));
  const obs = new MutationObserver(() => { if (!el.isConnected) { view.close(); obs.disconnect(); } });
  setTimeout(() => obs.observe(document.getElementById('main') || document.body, { childList: true }), 0);
  return { el };
}

async function loadRuns(box) {
  if (!isLive()) { box.replaceChildren(h('p.muted', t('browser.noRuns'))); return; }
  try {
    const r = await (await fetch('/api/browser/runs')).json();
    const list = r.runs || [];
    if (!list.length) { box.replaceChildren(h('p.muted', t('browser.noRuns'))); return; }
    box.replaceChildren(h('div.br-grid', list.map((run) => h('button.br-card', { type: 'button', onclick: () => replay(run) },
      run.cover ? h('img', { src: `/api/browser/runs/${run.id}/${run.cover}`, alt: '', loading: 'lazy' }) : h('div.br-nocover', icon('globe')),
      h('div.br-meta',
        h('b', run.first ? hostOf(run.first) : t('browser.run')),
        h('span', rel(Date.parse(run.started))),
        h('span.br-counts', h('span', { 'data-tip': t('browser.byAI') }, icon('sparkle'), run.agent), h('span', { 'data-tip': t('browser.byYou') }, icon('steer'), run.human),
          run.saved ? h('span.br-kept', { 'data-tip': t('browser.kept') }, icon('pin')) : null))))));
  } catch (e) { box.replaceChildren(h('p.muted', e.message)); }
}
const hostOf = (u) => { try { return new URL(u).host; } catch { return u; } };

// Step through a recording: screenshot, where it clicked, what it did.
async function replay(run) {
  const { openDrawer, closeDrawer } = await import('../ui.js');
  const r = await (await fetch('/api/browser/runs/' + run.id)).json();
  const steps = (r.steps || []);
  let i = steps.length - 1;
  const img = h('img.br-shot', { alt: '' });
  const dot = h('span.br-dot');
  const info = h('div.br-info');
  const range = h('input.br-range', { type: 'range', min: 1, max: Math.max(1, steps.length), value: steps.length, 'aria-label': t('browser.step'), oninput: (e) => { i = +e.target.value - 1; draw(); } });
  const draw = () => {
    const sp = steps[i]; if (!sp) return;
    // nearest screenshot at or before this step
    let k = i; while (k > 0 && !steps[k].shot) k--;
    if (steps[k]?.shot) img.src = `/api/browser/runs/${run.id}/${steps[k].shot}`;
    dot.hidden = !(sp.x || sp.y);
    dot.style.left = (sp.x / 1280) * 100 + '%'; dot.style.top = (sp.y / 800) * 100 + '%';
    info.replaceChildren(...[h('span.tag', { class: sp.by === 'human' ? 'route' : '' }, sp.by === 'human' ? icon('steer') : icon('sparkle'), t(sp.by === 'human' ? 'browser.byYou' : 'browser.byAI')),
      h('b', `${sp.n}. ${t('browser.tool.' + sp.tool) || sp.tool}`), sp.label ? h('span', sp.label) : null,
      sp.url ? h('div.bl-step-url', sp.url) : null, sp.error ? h('div.bl-step-err', sp.error) : null].filter(Boolean));
    range.value = i + 1;
  };
  const step = (d) => { i = Math.max(0, Math.min(steps.length - 1, i + d)); draw(); };
  openDrawer(run.first ? hostOf(run.first) : t('browser.run'), h('div.stack', { style: { padding: '16px', gap: '12px' }, tabindex: '0', onkeydown: (e) => { if (e.key === 'ArrowLeft') step(-1); if (e.key === 'ArrowRight') step(1); } },
    h('div.br-shotbox', img, dot),
    h('div.row', h('button.icon-btn', { type: 'button', 'aria-label': t('browser.prev'), onclick: () => step(-1) }, icon('back')), range,
      h('button.icon-btn', { type: 'button', 'aria-label': t('browser.next'), onclick: () => step(1) }, icon('chevron'))),
    info,
    h('p.faint', { style: { fontSize: '12px', margin: 0 } }, t('browser.privateNote'))),
  [h('button.btn.ghost', { type: 'button', onclick: async () => { await runOp(run.saved ? 'unkeep' : 'keep', run.id); closeDrawer(); location.reload(); } }, icon('pin'), t(run.saved ? 'browser.unkeep' : 'browser.keep')),
    h('button.btn.danger', { type: 'button', onclick: async () => { if (!confirm(t('browser.deleteConfirm'))) return; await runOp('delete', run.id); closeDrawer(); location.reload(); } }, icon('x'), t('browser.delete'))]);
  draw();
}
async function runOp(op, id) {
  const r = await fetch('/api/browser/runs', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify({ op, id }) });
  if (!r.ok) toast((await r.json().catch(() => ({}))).error || r.statusText, null, 'deny');
}

// Where recordings are kept; moving them needs the browser stopped (the server says so).
function recordingsRow() {
  const box = h('div.panel', { hidden: true, style: { padding: '14px 16px' } });
  fetch('/api/browser').then((r) => r.json()).then((b) => {
    if (!b.enabled || !b.recordingsDir) return;
    box.hidden = false;
    box.replaceChildren(folderRow({ id: 'brRecDir', label: t('folders.rec.label'), hint: t('folders.rec.hint'), value: b.recordingsDir,
      applyLabel: t('folders.rec.apply'), onApply: (dir) => api.setRecordingsDir(dir), onReset: () => api.setRecordingsDir(''), resetLabel: t('folders.reset') }));
  }).catch(() => {});
  return box;
}
