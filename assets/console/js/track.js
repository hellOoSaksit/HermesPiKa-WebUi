// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Interlocking track diagram.
// Mainline = coordinator session. Every lane branches from one trunk through a
// 45° turnout, so lanes never cross. Blocks are scope areas on a lane; occupied
// blocks are lit by the state of the sessions inside them. Two sessions in one
// block light it red. A signal stands at the entry of every occupied block.
// Focusing or hovering a train "sets the route": its path lights in route blue.
import { s, h } from './ui.js';
import { t } from './i18n.js';

const MAIN_Y = 50;
const LANE_GAP = 76;
const FIRST_LANE = 132;
const TRUNK_X = 104;
const TURN = 20;
const BLOCK_X0 = 160;
const MIN_W = 680;

function blockState(occ, byId, approvals) {
  if (!occ.length) return 'empty';
  if (occ.length > 1) return 'danger';
  const sess = byId[occ[0]];
  if (!sess) return 'idle';
  if (sess.status === 'waiting') return 'danger';
  if (sess.status === 'stale') return 'caution';
  if (sess.status === 'done' || sess.status === 'stopped') return 'idle';
  if (approvals.some((a) => a.session === sess.id && a.kind === 'question')) return 'caution';
  return 'clear';
}

// Returns a wrapper element that re-renders the SVG at the container's real
// pixel width, so text is never scaled down.
export function trackDiagram(opts) {
  const scroller = h('div.diagram');
  const pins = h('div.lane-pins', { 'aria-hidden': 'true' });
  const wrap = h('div.diagram-wrap', scroller, pins);
  let lastW = 0;
  let first = true;
  // Narrowest width at which every block still fits its signal and plates.
  const byId = Object.fromEntries(opts.sessions.map((x) => [x.id, x]));
  const need = Math.max(...opts.lanes.map((l) => l.blocks.reduce((sum, b) => {
    const occ = b.occupants.map((id) => byId[id]).filter(Boolean);
    const plates = occ.reduce((n, x) => n + plateW(x), 0) + PLATE_GAP * Math.max(0, occ.length - 1);
    return sum + Math.max(120, plates + 40);
  }, 0)));
  const minW = Math.max(MIN_W, BLOCK_X0 + 18 + need);
  const draw = () => {
    const w = Math.max(minW, Math.round(wrap.clientWidth || 1100));
    if (Math.abs(w - lastW) < 4) return;
    lastW = w;
    const { svg, lanesY, firstDangerX } = renderTrack({ ...opts, width: w, animate: first });
    first = false;
    scroller.replaceChildren(svg);
    pins.replaceChildren(
      // Mainline label sits below its line; plates sit above lines, so they never meet.
      h('span.pin', { style: { top: MAIN_Y + 6 + 'px' } }, t('track.mainline')),
      ...lanesY.map(([name, y]) => h('span.pin', { style: { top: y - 10 + 'px' } }, name)));
    if (opts.scrollToDanger !== false && wrap.clientWidth < minW && firstDangerX) scroller.scrollLeft = Math.max(0, firstDangerX - 140);
  };
  const ro = new ResizeObserver(draw);
  ro.observe(wrap);
  setTimeout(draw, 0); // rAF does not fire in hidden tabs; a timer always does
  return wrap;
}

const plateW = (sess) => 34 + sess.num.length * 7.5 + (sess.owner ? 22 : 0);
const PLATE_GAP = 12;

export function renderTrack({ lanes, sessions, approvals = [], onOpen, width = 1100, animate = false }) {
  const W = width;
  const RIGHT = W - 18;
  const byId = Object.fromEntries(sessions.map((x) => [x.id, x]));
  const H = FIRST_LANE + (lanes.length - 1) * LANE_GAP + 52;
  const svg = s('svg', { viewBox: `0 0 ${W} ${H}`, width: W, height: H, role: 'group', 'aria-label': t('track.aria'), class: animate ? 'enter' : '' });
  const lanesY = [];
  let firstDangerX = null;
  let seq = 0; // drawing order, used to stagger the draw-in animation
  // Stagger the draw-in, but cap it so the whole diagram is in place within ~1 s.
  const stagger = (el, ms = 18) => { el.style.setProperty('--d', `${Math.min(700, (seq++) * ms)}ms`); return el; };
  const track = (d) => stagger(s('path', { d, class: 'trk', pathLength: 1 }));

  const lastY = FIRST_LANE + (lanes.length - 1) * LANE_GAP;
  const routes = {}; // laneId -> [path elements] lit when a train on that lane is selected
  const mainline = track(`M16 ${MAIN_Y}H${RIGHT}`);
  const trunk = track(`M${TRUNK_X} ${MAIN_Y}V${lastY - TURN}`);
  svg.append(mainline, trunk);

  // Route-set: light the path one piece after another, mainline first.
  const setRoute = (laneId, on) => {
    const parts = [mainline, ...(laneId ? [trunk, ...(routes[laneId] || [])] : [])];
    parts.forEach((p, k) => { p.style.transitionDelay = on ? `${k * 70}ms` : '0ms'; p.classList.toggle('set', on); });
  };

  const root = sessions.find((x) => !x.parent && x.num === '1');
  const layer = s('g', { class: 'layer' }); // trains and signals drawn above all track

  lanes.forEach((lane, i) => {
    const y = FIRST_LANE + i * LANE_GAP;
    lanesY.push([lane.name, y]);
    const branch = track(`M${TRUNK_X} ${y - TURN}L${TRUNK_X + TURN} ${y}H${BLOCK_X0}`);
    svg.append(branch);
    routes[lane.id] = [branch];
    const n = lane.blocks.length;
    // Each block is as long as it needs to be for its signal and plates,
    // then the lane is stretched to the full width in proportion.
    const needs = lane.blocks.map((b) => {
      const occ = b.occupants.map((sid) => byId[sid]).filter(Boolean);
      return Math.max(120, occ.reduce((n2, x) => n2 + plateW(x), 0) + PLATE_GAP * Math.max(0, occ.length - 1) + 40);
    });
    const scale = (RIGHT - BLOCK_X0) / needs.reduce((a, b2) => a + b2, 0);
    let cursorX = BLOCK_X0;
    lane.blocks.forEach((b, j) => {
      const x0 = cursorX;
      const span = needs[j] * scale;
      const x1 = x0 + span;
      cursorX = x1;
      const st = blockState(b.occupants, byId, approvals);
      const seg = track(`M${x0} ${y}H${x1}`);
      svg.append(seg);
      if (j === 0 || b.occupants.length) routes[lane.id].push(seg);
      svg.append(s('path', { d: `M${x0} ${y - 6}V${y + 6}`, class: 'joint' }));
      if (j === n - 1) svg.append(s('path', { d: `M${x1} ${y - 6}V${y + 6}`, class: 'joint' }));
      if (st !== 'empty') {
        // idle blocks are dashed, so they keep real units (no pathLength).
        svg.append(stagger(s('path', { d: `M${x0 + 4} ${y}H${x1 - 4}`, class: `blk-occ ${st}`, pathLength: st === 'idle' ? null : 1 })));
        // Moving sessions carry a flow of light along their block.
        if (st === 'clear') svg.append(s('path', { d: `M${x0 + 4} ${y}H${x1 - 4}`, class: 'blk-flow' }));
        if (st === 'danger') svg.append(s('path', { d: `M${x0 + 4} ${y}H${x1 - 4}`, class: 'blk-alarm' }));
      }
      if (st === 'danger' && firstDangerX === null) firstDangerX = x0;
      svg.append(s('text', { x: x0 + 10, y: y + 22, class: 'blk-label' }, b.label));
      if (st !== 'empty') layer.append(stagger(signal(x0 + 12, y, st)));
      // Plates sit side by side by their real widths, never overlapping.
      // The signal stands at the block entry, so plates are centred in the space after it.
      const occ = b.occupants.map((sid) => byId[sid]).filter(Boolean);
      const total = occ.reduce((n2, x) => n2 + plateW(x), 0) + PLATE_GAP * (occ.length - 1);
      const zoneL = x0 + 26; const zoneR = x1 - 8;
      let left = Math.max(zoneL, (zoneL + zoneR) / 2 - total / 2);
      occ.forEach((sess) => {
        const w = plateW(sess);
        layer.append(stagger(train(sess, left + w / 2, y, st, onOpen, (on) => setRoute(sess.lane, on))));
        left += w + PLATE_GAP;
      });
    });
  });
  if (root) layer.append(train(root, BLOCK_X0 + 90, MAIN_Y, 'clear', onOpen, (on) => setRoute(null, on)));
  svg.append(layer);
  return { svg, lanesY, firstDangerX };
}

function signal(x, y, state) {
  const aspect = state === 'danger' ? 'danger' : state === 'caution' ? 'caution' : state === 'clear' ? 'clear' : 'off';
  const g = s('g', { 'aria-hidden': 'true', class: 'sig' });
  g.append(
    s('line', { x1: x, y1: y - 8, x2: x, y2: y - 22, class: 'sig-post' }),
    s('rect', { x: x - 8, y: y - 52, width: 16, height: 30, rx: 8, class: 'sig-head' }),
    s('circle', { cx: x, cy: y - 44, r: 4.4, class: 'sig-lamp ' + (aspect === 'danger' ? 'danger blink' : 'off') }),
    s('circle', { cx: x, cy: y - 30, r: 4.4, class: 'sig-lamp ' + (aspect === 'caution' ? 'caution' : aspect === 'clear' ? 'clear' : 'off') }),
  );
  return g;
}

function train(sess, cx, y, state, onOpen, onRoute) {
  const owner = sess.owner ? sess.owner.slice(0, 2).toUpperCase() : '';
  const w = plateW(sess);
  const g = s('g', {
    class: 'train' + (state === 'danger' ? ' danger' : state === 'caution' ? ' caution' : ''),
    tabindex: 0, role: 'link',
    'aria-label': t('track.trainAria', { num: sess.num, title: sess.title, status: t(`status.${sess.status}`) }),
    onclick: () => onOpen && onOpen(sess.id),
    onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onOpen && onOpen(sess.id); } },
    onfocus: () => onRoute(true), onblur: () => onRoute(false),
    onmouseenter: () => onRoute(true), onmouseleave: () => onRoute(false),
  });
  g.append(
    s('title', {}, `${sess.num} · ${sess.title}`),
    s('rect', { class: 'ring', x: cx - w / 2 - 4, y: y - 37, width: w + 8, height: 30, rx: 7 }),
    s('rect', { class: 'plate', x: cx - w / 2, y: y - 33, width: w, height: 22, rx: 4 }),
    s('text', { x: cx - w / 2 + 10, y: y - 18 }, sess.num),
  );
  if (owner) g.append(s('text', { x: cx + w / 2 - 24, y: y - 18, class: 't-owner' }, owner));
  return g;
}

export function trackLegend() {
  return h('div.diagram-legend',
    h('span', h('i.legend-bar.clear'), t('track.legend.clear')),
    h('span', h('i.legend-bar.caution'), t('track.legend.caution')),
    h('span', h('i.legend-bar.danger'), t('track.legend.danger')),
    h('span', h('i.legend-bar.idle'), t('track.legend.idle')),
    h('span.faint', t('track.legend.route')));
}
