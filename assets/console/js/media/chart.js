// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Small SVG charts for the chat: bar, stacked bar, line, area, scatter, pie/donut.
// Rules (dataviz): categorical colours in a fixed order (--media-s1..8, validated
// on both Console surfaces), thin marks with rounded ends, one y-axis, a legend
// for 2+ series, a hover tooltip per mark (data-tip, drawn by controls.js), and
// a table view next to every chart.
import { s, h } from '../ui.js';

const W = 640; const H = 300;
const PAD = { l: 48, r: 16, t: 14, b: 34 };
const fmt = (v) => (Math.abs(v) >= 1e6 ? (v / 1e6).toFixed(1).replace(/\.0$/, '') + 'M' : Math.abs(v) >= 1e4 ? (v / 1e3).toFixed(0) + 'k' : Number.isInteger(v) ? String(v) : v.toFixed(Math.abs(v) < 1 ? 3 : 2).replace(/0+$/, '').replace(/\.$/, ''));
const color = (i) => `var(--media-s${(i % 8) + 1})`;

function niceTicks(min, max, n = 5) {
  if (min === max) { max = min + 1; }
  const span = max - min;
  const step0 = span / n;
  const mag = 10 ** Math.floor(Math.log10(step0));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((x) => span / x <= n) || mag * 10;
  const lo = Math.floor(min / step) * step;
  const hi = Math.ceil(max / step) * step;
  const out = [];
  for (let v = lo; v <= hi + step / 2; v += step) out.push(+v.toFixed(10));
  return out;
}

// spec: {type, labels, series:[{name,data}], unit}; hidden: Set of series indexes.
export function drawChart(spec, hidden = new Set()) {
  const type = spec.type;
  const svg = s('svg', { class: 'media-chart-svg', viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': spec.title || spec.series.map((x) => x.name).join(', ') });
  const series = spec.series.map((x, i) => ({ ...x, i })).filter((x) => !hidden.has(x.i));
  const unit = spec.unit ? ' ' + spec.unit : '';
  if (type === 'pie' || type === 'donut') return pie(svg, spec, series[0], unit);
  if (type === 'scatter') return scatter(svg, series, unit);

  const n = spec.labels.length;
  const vals = series.flatMap((x) => x.data).filter(Number.isFinite);
  let max = Math.max(0, ...vals); let min = Math.min(0, ...vals);
  if (type === 'stacked') {
    max = Math.max(0, ...spec.labels.map((_, j) => series.reduce((a, x) => a + Math.max(0, x.data[j] || 0), 0)));
    min = Math.min(0, ...spec.labels.map((_, j) => series.reduce((a, x) => a + Math.min(0, x.data[j] || 0), 0)));
  }
  const ticks = niceTicks(min, max);
  const y0 = ticks[0]; const y1 = ticks[ticks.length - 1];
  const iw = W - PAD.l - PAD.r; const ih = H - PAD.t - PAD.b;
  const Y = (v) => PAD.t + ih - ((v - y0) / (y1 - y0 || 1)) * ih;
  // grid + y labels (recessive)
  for (const tv of ticks) {
    svg.append(s('line', { x1: PAD.l, x2: W - PAD.r, y1: Y(tv), y2: Y(tv), class: tv === 0 ? 'media-axis' : 'media-grid' }));
    svg.append(s('text', { x: PAD.l - 8, y: Y(tv) + 4, class: 'media-tick', 'text-anchor': 'end' }, fmt(tv)));
  }
  // x labels: at most ~10
  const every = Math.max(1, Math.ceil(n / 10));
  const band = iw / Math.max(1, n);
  const X = (j) => PAD.l + band * j + band / 2;
  spec.labels.forEach((lb, j) => {
    if (j % every) return;
    svg.append(s('text', { x: X(j), y: H - PAD.b + 18, class: 'media-tick', 'text-anchor': 'middle' }, lb.length > 12 ? lb.slice(0, 11) + '…' : lb));
  });

  if (type === 'bar' || type === 'stacked') {
    const groups = type === 'stacked' ? 1 : series.length;
    const gw = Math.min(band * 0.78, 64 * groups);
    const bw = Math.max(2, gw / groups - 2); // 2px surface gap between bars
    spec.labels.forEach((lb, j) => {
      let pos = 0; let neg = 0;
      series.forEach((x, k) => {
        const v = x.data[j];
        if (!Number.isFinite(v)) return;
        let top; let bot;
        if (type === 'stacked') { if (v >= 0) { bot = pos; top = pos += v; } else { top = neg; bot = neg += v; } } else { top = Math.max(0, v); bot = Math.min(0, v); }
        const x0 = type === 'stacked' ? X(j) - bw / 2 : X(j) - gw / 2 + k * (bw + 2);
        const yTop = Y(top); const hgt = Math.max(1, Y(bot) - yTop - (type === 'stacked' ? 2 : 0));
        svg.append(s('path', { d: barPath(x0, yTop, bw, hgt, v >= 0), fill: color(x.i), class: 'media-mark', 'data-tip': `${lb} · ${x.name}: ${fmt(v)}${unit}` }));
      });
    });
  } else {
    // line / area with a crosshair column per x
    series.forEach((x) => {
      const pts = x.data.map((v, j) => (Number.isFinite(v) ? [X(j), Y(v)] : null));
      const d = pts.reduce((acc, p, j) => (p ? acc + (acc && pts[j - 1] ? 'L' : 'M') + p[0].toFixed(1) + ' ' + p[1].toFixed(1) : acc), '');
      if (type === 'area') {
        const first = pts.findIndex(Boolean); const last = pts.length - 1 - [...pts].reverse().findIndex(Boolean);
        if (first >= 0) svg.append(s('path', { d: `${d}L${pts[last][0]} ${Y(Math.max(0, y0))}L${pts[first][0]} ${Y(Math.max(0, y0))}Z`, fill: color(x.i), class: 'media-area' }));
      }
      svg.append(s('path', { d, stroke: color(x.i), class: 'media-line' }));
      if (n <= 40) pts.forEach((p) => p && svg.append(s('circle', { cx: p[0], cy: p[1], r: 3.5, fill: color(x.i), class: 'media-dot' })));
    });
    spec.labels.forEach((lb, j) => {
      const tip = lb + ' — ' + series.map((x) => `${x.name}: ${Number.isFinite(x.data[j]) ? fmt(x.data[j]) + unit : '—'}`).join(' · ');
      svg.append(s('rect', { x: X(j) - band / 2, y: PAD.t, width: band, height: ih, class: 'media-hit', 'data-tip': tip }));
    });
  }
  return svg;
}

function barPath(x, y, w, hgt, up) {
  const r = Math.min(4, w / 2, hgt); // 4px rounded data end, square at the baseline
  return up
    ? `M${x} ${y + hgt}V${y + r}Q${x} ${y} ${x + r} ${y}H${x + w - r}Q${x + w} ${y} ${x + w} ${y + r}V${y + hgt}Z`
    : `M${x} ${y}V${y + hgt - r}Q${x} ${y + hgt} ${x + r} ${y + hgt}H${x + w - r}Q${x + w} ${y + hgt} ${x + w} ${y + hgt - r}V${y}Z`;
}

function scatter(svg, series, unit) {
  const pts = series.flatMap((x) => x.data.map((d, j) => (Array.isArray(d) ? [d[0], d[1], x] : [j, d, x])).filter((p) => Number.isFinite(p[0]) && Number.isFinite(p[1])));
  const xs = niceTicks(Math.min(...pts.map((p) => p[0])), Math.max(...pts.map((p) => p[0])));
  const ys = niceTicks(Math.min(0, ...pts.map((p) => p[1])), Math.max(...pts.map((p) => p[1])));
  const iw = W - PAD.l - PAD.r; const ih = H - PAD.t - PAD.b;
  const X = (v) => PAD.l + ((v - xs[0]) / (xs.at(-1) - xs[0] || 1)) * iw;
  const Y = (v) => PAD.t + ih - ((v - ys[0]) / (ys.at(-1) - ys[0] || 1)) * ih;
  ys.forEach((v) => { svg.append(s('line', { x1: PAD.l, x2: W - PAD.r, y1: Y(v), y2: Y(v), class: 'media-grid' })); svg.append(s('text', { x: PAD.l - 8, y: Y(v) + 4, class: 'media-tick', 'text-anchor': 'end' }, fmt(v))); });
  xs.forEach((v) => svg.append(s('text', { x: X(v), y: H - PAD.b + 18, class: 'media-tick', 'text-anchor': 'middle' }, fmt(v))));
  pts.forEach(([x, y, se]) => svg.append(s('circle', { cx: X(x), cy: Y(y), r: 4.5, fill: color(se.i), class: 'media-dot ring', 'data-tip': `${se.name}: ${fmt(x)}, ${fmt(y)}${unit}` })));
  return svg;
}

function pie(svg, spec, se, unit) {
  if (!se) return svg;
  const vals = se.data.map((v) => Math.max(0, Number(v) || 0));
  const total = vals.reduce((a, b) => a + b, 0) || 1;
  const cx = W / 2; const cy = H / 2; const r = H / 2 - 16; const inner = spec.type === 'donut' ? r * 0.58 : 0;
  let a0 = -Math.PI / 2;
  vals.forEach((v, j) => {
    if (!v) return;
    const a1 = a0 + (v / total) * Math.PI * 2;
    const large = a1 - a0 > Math.PI ? 1 : 0;
    const p = (a, rr) => [cx + Math.cos(a) * rr, cy + Math.sin(a) * rr];
    const [x0, y0] = p(a0, r); const [x1, y1] = p(a1, r);
    const d = inner
      ? (() => { const [i0x, i0y] = p(a1, inner); const [i1x, i1y] = p(a0, inner); return `M${x0} ${y0}A${r} ${r} 0 ${large} 1 ${x1} ${y1}L${i0x} ${i0y}A${inner} ${inner} 0 ${large} 0 ${i1x} ${i1y}Z`; })()
      : `M${cx} ${cy}L${x0} ${y0}A${r} ${r} 0 ${large} 1 ${x1} ${y1}Z`;
    svg.append(s('path', { d, fill: color(j), class: 'media-slice', 'data-tip': `${spec.labels[j]}: ${fmt(v)}${unit} (${Math.round((v / total) * 100)}%)` }));
    a0 = a1;
  });
  return svg;
}

// Legend: one entry per series (pie: per label); clicking toggles a series.
export function legend(spec, hidden, onToggle) {
  const pieLike = spec.type === 'pie' || spec.type === 'donut';
  const names = pieLike ? spec.labels : spec.series.map((x) => x.name);
  if (names.length < 2) return null;
  return h('div.media-legend', names.slice(0, 12).map((nm, i) => h('button', {
    type: 'button', class: hidden.has(i) ? 'off' : '', 'aria-pressed': String(!hidden.has(i)), disabled: pieLike || null,
    onclick: () => onToggle(i),
  }, h('i', { style: `background:${color(i)}` }), nm)));
}
