// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Media cards for the chat: image (zoom/pan), video, audio, PDF, data tables,
// charts and 3D models. Everything is built with DOM nodes (no innerHTML).
import { t } from '../i18n.js';
import { h, s, icon, toast } from '../ui.js';
import { isLive } from '../live.js';
import { api } from '../data.js';
import { blocks, refs, kindOf, extOf, baseName, tableOf, parseCSV, rowsFromJSON, chartSpec, chartFromTable } from './parse.js';
import { drawChart, legend } from './chart.js';

// ---- where files come from ----
const norm = (p) => String(p).replace(/\\/g, '/').replace(/\/+$/, '');
export function relPath(path, root) {
  const p = norm(path);
  if (/^(data|blob|https?):/i.test(p)) return null;
  const abs = /^([a-z]:)?\//i.test(p) || p.startsWith('~/');
  if (!abs) return p.replace(/^\.\//, '');
  if (!root) return null;
  let r = norm(root);
  const cmp = (a) => (/^[a-z]:/i.test(a) ? a.toLowerCase() : a);
  if (r.startsWith('~/')) {
    const tail = r.slice(1); // "/code/app"
    const i = cmp(p).indexOf(cmp(tail) + '/');
    return i >= 0 ? p.slice(i + tail.length + 1) : null;
  }
  return cmp(p).startsWith(cmp(r) + '/') ? p.slice(r.length + 1) : null;
}
export function fileURL(path) {
  if (/^(data|blob):/i.test(path)) return path;
  if (/^https?:/i.test(path)) return null; // other hosts: link only (the Console loads nothing from elsewhere)
  const p = api.project?.();
  const rel = relPath(path, p?.root);
  if (rel == null) return null;
  const q = new URLSearchParams();
  if (isLive() && p?.id) q.set('project', p.id);
  q.set('path', rel);
  return '/api/files?' + q.toString();
}

// ---- quality (3D resolution, video preload); starts at the lowest ----
const QKEY = 'hpk.media.quality';
export const QUALITIES = ['low', 'medium', 'high', 'ultra'];
export function defaultQuality() { try { const v = localStorage.getItem(QKEY); return QUALITIES.includes(v) ? v : 'low'; } catch { return 'low'; } }
function setDefaultQuality(v) { try { localStorage.setItem(QKEY, v); } catch { /* this page only */ } }

// ---- cards ----
const KIND_ICON = { image: 'eye', video: 'play', audio: 'play', model: 'layers', pdf: 'file', data: 'gauge', chart: 'gauge', table: 'gauge' };
function card(kind, title, url, body, extra) {
  return h('figure.media-card', { class: 'media-' + kind },
    h('figcaption.media-head',
      h('span.media-kind', { 'data-tip': t('media.kind.' + kind) }, icon(KIND_ICON[kind] || 'file')),
      h('span.media-name', { 'data-tip': title }, title),
      extra || null,
      url && !/^data:/i.test(url) ? h('a.icon-btn.sm', { href: url, target: '_blank', rel: 'noopener noreferrer', 'aria-label': t('media.open'), 'data-tip': t('media.open') }, icon('external')) : null),
    body);
}
const problem = (msg) => h('div.media-problem', icon('warn'), h('span', msg));

// A reference that cannot be shown here: outside the project, or on another site.
function linkCard(ref) {
  const why = ref.remote ? t('media.remote') : t('media.outside');
  return h('div.media-link', icon(ref.remote ? 'globe' : 'lock'), h('code', ref.alt || ref.src),
    ref.remote ? h('a', { href: ref.src, target: '_blank', rel: 'noopener noreferrer' }, t('media.openSite')) : null,
    h('span.faint', why));
}

export function mediaCard(ref) {
  const url = fileURL(ref.src);
  if (!url) return linkCard(ref);
  const name = ref.inline ? t('media.inline', { type: baseName(ref.src) }) : baseName(ref.src);
  switch (ref.kind) {
    case 'image': return imageCard(url, name, ref.alt);
    case 'video': {
      const v = h('video.media-video', { src: url, controls: true, playsinline: true, preload: defaultQuality() === 'low' ? 'none' : 'metadata' });
      v.addEventListener('error', () => v.replaceWith(problem(t('media.cannotPlay'))));
      return card('video', name, url, v);
    }
    case 'audio': return card('audio', name, url, h('audio.media-audio', { src: url, controls: true, preload: 'none' }));
    case 'pdf': return card('pdf', name, url, h('iframe.media-pdf', { src: url, loading: 'lazy', title: name }));
    case 'data': return dataCard(url, name, extOf(ref.src));
    case 'model': return modelCard(url, name, extOf(ref.src), ref.src);
    default: return linkCard(ref);
  }
}

// ---- image + lightbox ----
function imageCard(url, name, alt) {
  const img = h('img.media-img', { src: url, alt: alt || name, loading: 'lazy', decoding: 'async' });
  img.addEventListener('error', () => img.replaceWith(problem(t('media.cannotLoad'))));
  const fig = card('image', name, url, h('button.media-img-btn', { type: 'button', 'aria-label': t('media.zoom'), onclick: () => lightbox(url, alt || name) }, img));
  img.addEventListener('load', () => { const d = fig.querySelector('.media-head .media-dim'); if (!d) fig.querySelector('.media-name').after(h('span.media-dim', `${img.naturalWidth}×${img.naturalHeight}`)); });
  return fig;
}
function lightbox(url, alt) {
  let z = 1; let x = 0; let y = 0; let drag = null;
  const img = h('img', { src: url, alt, draggable: 'false' });
  const apply = () => { img.style.transform = `translate(${x}px, ${y}px) scale(${z})`; level.textContent = Math.round(z * 100) + '%'; };
  const level = h('span.media-lb-level', '100%');
  const zoom = (f) => { z = Math.min(16, Math.max(0.1, z * f)); apply(); };
  const close = () => { box.remove(); document.removeEventListener('keydown', onKey); };
  const onKey = (e) => { if (e.key === 'Escape') close(); else if (e.key === '+' || e.key === '=') zoom(1.25); else if (e.key === '-') zoom(0.8); else if (e.key === '0') { z = 1; x = y = 0; apply(); } };
  const box = h('div.media-lb', { role: 'dialog', 'aria-modal': 'true', 'aria-label': alt,
    onwheel: (e) => { e.preventDefault(); zoom(e.deltaY < 0 ? 1.15 : 0.87); },
    onpointerdown: (e) => { if (e.target === img) { drag = [e.clientX - x, e.clientY - y]; img.setPointerCapture(e.pointerId); } },
    onpointermove: (e) => { if (drag) { x = e.clientX - drag[0]; y = e.clientY - drag[1]; apply(); } },
    onpointerup: () => (drag = null),
    onclick: (e) => { if (e.target === box) close(); } },
  img,
  h('div.media-lb-bar',
    h('button.icon-btn', { type: 'button', 'aria-label': t('media.zoomOut'), onclick: () => zoom(0.8) }, h('span.media-pm', '−')),
    level,
    h('button.icon-btn', { type: 'button', 'aria-label': t('media.zoomIn'), onclick: () => zoom(1.25) }, h('span.media-pm', '+')),
    h('button.icon-btn', { type: 'button', 'aria-label': t('media.zoomFit'), onclick: () => { z = 1; x = y = 0; apply(); } }, icon('compress')),
    h('a.icon-btn', { href: url, target: '_blank', rel: 'noopener noreferrer', 'aria-label': t('media.open') }, icon('external')),
    h('button.icon-btn', { type: 'button', 'aria-label': t('common.close'), onclick: close }, icon('close'))));
  document.body.append(box);
  document.addEventListener('keydown', onKey);
  box.querySelector('.media-lb-bar button').focus();
}

// ---- tables and charts ----
export function tableCard(tb, title, url) {
  const spec = chartFromTable(tb);
  let showChart = false;
  const body = h('div.media-table-body');
  const LIMIT = 200;
  const drawTable = (all) => {
    const rows = all ? tb.rows : tb.rows.slice(0, LIMIT);
    return h('div.media-table-wrap', h('table.media-table',
      h('thead', h('tr', tb.cols.map((c, i) => h('th', { class: tb.numeric[i] ? 'num' : '' }, c)))),
      h('tbody', rows.map((r) => h('tr', tb.cols.map((_, i) => h('td', { class: tb.numeric[i] ? 'num' : '' }, r[i] ?? '')))))),
    !all && tb.rows.length > LIMIT ? h('button.btn.sm.ghost', { type: 'button', onclick: () => body.replaceChildren(drawTable(true)) }, t('media.showAll', { n: tb.rows.length })) : null);
  };
  const draw = () => body.replaceChildren(showChart && spec ? chartBody(spec) : drawTable(false));
  draw();
  const toggle = spec ? h('div.seg.media-seg', { role: 'group' },
    h('button', { type: 'button', 'aria-pressed': 'true', onclick: (e) => { showChart = false; press(e); draw(); } }, t('media.table')),
    h('button', { type: 'button', 'aria-pressed': 'false', onclick: (e) => { showChart = true; press(e); draw(); } }, t('media.chart'))) : null;
  return card('table', title || t('media.tableN', { r: tb.rows.length, c: tb.cols.length }), url, body, toggle);
}
const press = (e) => e.currentTarget.parentElement.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', String(b === e.currentTarget)));

function chartBody(spec0) {
  const spec = { ...spec0 };
  const hidden = new Set();
  const wrap = h('div.media-chart');
  const types = spec.series.some((x) => x.data.some(Array.isArray)) ? ['scatter'] : spec.series.length === 1 ? ['bar', 'line', 'area', 'pie'] : ['bar', 'stacked', 'line', 'area'];
  const draw = () => wrap.replaceChildren(
    types.length > 1 ? h('div.seg.media-seg', { role: 'group', 'aria-label': t('media.chartType') }, types.map((ty) => h('button', { type: 'button', 'aria-pressed': String(spec.type === ty), 'data-tip': t('media.type.' + ty), onclick: () => { spec.type = ty; draw(); } }, typeGlyph(ty)))) : null,
    legend(spec, hidden, (i) => { if (hidden.has(i)) hidden.delete(i); else if (hidden.size < spec.series.length - 1) hidden.add(i); draw(); }),
    drawChart(spec, hidden));
  draw();
  return wrap;
}
function typeGlyph(ty) {
  const p = { bar: 'M3 15V9M8 15V5M13 15V11', stacked: 'M5 15V7M5 11H5M11 15V4', line: 'M2 13l4-5 4 3 6-7', area: 'M2 14l4-6 4 3 6-7V15H2Z', pie: 'M9 2a7 7 0 1 0 7 7H9Z', scatter: 'M4 12h.1M8 7h.1M12 10h.1M14 4h.1' }[ty];
  return s('svg', { class: 'media-type', viewBox: '0 0 18 18', 'aria-hidden': 'true' }, s('path', { d: p }));
}
export function chartCard(spec) {
  const tb = { cols: [t('media.label'), ...spec.series.map((x) => x.name)], rows: spec.labels.map((l, j) => [l, ...spec.series.map((x) => (Array.isArray(x.data[j]) ? x.data[j].join(', ') : String(x.data[j] ?? '')))]), numeric: [false, ...spec.series.map(() => true)] };
  let tableView = false;
  const body = h('div');
  const draw = () => body.replaceChildren(tableView ? tableCard({ ...tb, num: Number }, '', null).querySelector('.media-table-body') : chartBody(spec));
  draw();
  const toggle = h('div.seg.media-seg', { role: 'group' },
    h('button', { type: 'button', 'aria-pressed': 'true', onclick: (e) => { tableView = false; press(e); draw(); } }, t('media.chart')),
    h('button', { type: 'button', 'aria-pressed': 'false', onclick: (e) => { tableView = true; press(e); draw(); } }, t('media.table')));
  return card('chart', spec.title || t('media.chart'), null, body, toggle);
}

function dataCard(url, name, ext) {
  const body = h('div.media-loading', h('span.media-spin'), t('media.loading'));
  const fig = card('data', name, url, body);
  fetch(url).then(async (r) => {
    if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
    const len = Number(r.headers.get('content-length') || 0);
    if (len > 5e6) throw new Error(t('media.tooBig'));
    const text = await r.text();
    let rows;
    if (ext === 'json') { rows = rowsFromJSON(JSON.parse(text)); if (!rows) { body.replaceWith(h('pre.media-json', text.slice(0, 20000))); return; } }
    else rows = parseCSV(text, ext === 'tsv' ? '\t' : undefined);
    const tc = tableCard(tableOf(rows), name, url);
    fig.replaceWith(tc);
  }).catch((e) => body.replaceWith(problem(e.message)));
  return fig;
}

// ---- 3D ----
const live = []; // mounted viewers, oldest first; at most 4 WebGL views at once
function modelCard(url, name, ext, src) {
  const stage = h('div.media-3d-stage');
  const bar = h('div.media-3d-bar');
  const side = h('div.media-3d-stats', { hidden: true });
  const progress = h('div.media-3d-progress', h('i'));
  const poster = h('button.media-3d-poster', { type: 'button', onclick: () => mount() }, icon('layers'), h('span', t('media.load3d')), h('small', '.' + ext));
  stage.append(poster);
  const sibling = (newExt) => fileURL(src.replace(/\.[a-z0-9]+$/i, newExt));
  const resolve = (rel) => {
    const dir = norm(src).split('/').slice(0, -1).join('/');
    return fileURL(dir ? dir + '/' + rel : rel) || 'data:,';
  };
  let viewer = null;
  async function mount() {
    poster.remove(); stage.append(progress);
    try {
      const mod = await import('./viewer3d.js');
      if (['gltf', 'glb'].includes(ext)) { /* compressed meshes need wasm decoders, which the Console's CSP does not allow */ }
      const model = await mod.loadModel({ url, ext, resolve, sibling, onProgress: (f) => progress.firstChild.style.setProperty('width', Math.round(f * 100) + '%') });
      progress.remove();
      viewer = new mod.Viewer(stage, model, { quality: defaultQuality(), bg: 'theme' });
      live.push(viewer);
      while (live.filter((v) => !v.gone).length > 4) { const old = live.find((v) => !v.gone && v !== viewer); old?.host.dispatchEvent(new Event('media-sleep')); }
      toolbar(mod, viewer);
      new ResizeObserver(() => !viewer.gone && viewer.resize()).observe(stage);
      new IntersectionObserver((es) => { if (!viewer.gone) viewer.paused = !es[0].isIntersecting; }).observe(stage);
    } catch (e) {
      progress.remove();
      const msg = /KHR_draco|EXT_meshopt|KHR_texture_basisu/i.test(e.message) ? t('media.compressed') : e.message;
      stage.append(problem(t('media.cannot3d', { why: msg })));
    }
  }
  stage.addEventListener('media-sleep', () => {
    if (!viewer || viewer.gone) return;
    const shot = viewer.screenshot();
    viewer.dispose();
    stage.replaceChildren(h('button.media-3d-poster.slept', { type: 'button', onclick: () => { stage.replaceChildren(); bar.replaceChildren(); mount(); } }, h('img', { src: shot, alt: '' }), h('span', icon('play'), t('media.resume'))));
    bar.replaceChildren(); side.hidden = true;
  });
  // start when it scrolls into view
  const io = new IntersectionObserver((es) => { if (es[0].isIntersecting) { io.disconnect(); if (poster.isConnected) mount(); } }, { rootMargin: '200px' });
  io.observe(stage);

  function toolbar(mod, v) {
    const btn = (ic, label, on, pressed) => h('button.icon-btn.sm', { type: 'button', 'aria-label': label, 'data-tip': label, 'aria-pressed': pressed == null ? null : String(pressed), onclick: on }, typeof ic === 'string' ? icon(ic) : ic);
    const stats = mod.statsOf(v.model.root, v.model.clips);
    side.replaceChildren(h('dl', ...[['meshes', stats.meshes], ['triangles', stats.triangles.toLocaleString()], ['vertices', stats.vertices.toLocaleString()], ['materials', stats.materials], ['textures', stats.textures], ['bones', stats.bones], ['lights', stats.lights], ['clips', stats.clips]]
      .flatMap(([k, n]) => [h('dt', t('media.st.' + k)), h('dd', String(n))])));
    const groups = [];
    if (v.mixer) {
      const playBtn = btn(v.playing ? 'pause' : 'play', t('media.playPause'), () => { v.toggle(); playBtn.replaceChildren(icon(v.playing ? 'pause' : 'play')); });
      const scrub = h('input.media-scrub', { type: 'range', min: 0, max: 1000, value: 0, 'aria-label': t('media.scrub'),
        oninput: (e) => { if (v.playing) { v.toggle(); playBtn.replaceChildren(icon('play')); } v.seek((e.target.value / 1000) * v.duration()); } });
      const timeEl = h('span.media-time', '0.0s');
      v.onTime = (tt) => { scrub.value = Math.round((tt / (v.duration() || 1)) * 1000); timeEl.textContent = tt.toFixed(1) + 's'; };
      const clipSel = v.model.clips.length > 1 ? h('select.input.sm', { 'aria-label': t('media.clip'), onchange: (e) => v.play(Number(e.target.value)) },
        v.model.clips.map((c, i) => h('option', { value: i }, c.name || t('media.clipN', { n: i + 1 })))) : h('span.media-clip', v.model.clips[0].name || t('media.clipN', { n: 1 }));
      const speed = h('select.input.sm', { 'aria-label': t('media.speed'), onchange: (e) => (v.speed = Number(e.target.value)) },
        [0.25, 0.5, 1, 1.5, 2].map((x) => h('option', { value: x, selected: x === 1 || null }, x + '×')));
      groups.push(h('div.media-3d-group.anim', playBtn, clipSel, scrub, timeEl, speed));
    }
    const lights = h('select.input.sm', { 'aria-label': t('media.lights'), onchange: (e) => { v.opts.lights = e.target.value; v.applyLights(); } },
      ['both', 'scene', 'studio'].map((x) => h('option', { value: x, selected: v.opts.lights === x || null }, t('media.light.' + x))));
    const wire = btn(s('svg', { class: 'media-glyph', viewBox: '0 0 18 18' }, s('path', { d: 'M3 14L9 3l6 11zM6 8.5h6M9 3v11' })), t('media.wire'), (e) => { v.opts.wire = !v.opts.wire; v.applyWire(); e.currentTarget.setAttribute('aria-pressed', String(v.opts.wire)); }, v.opts.wire);
    const grid = btn(s('svg', { class: 'media-glyph', viewBox: '0 0 18 18' }, s('path', { d: 'M2 6h14M2 12h14M6 2v14M12 2v14' })), t('media.grid'), (e) => { v.opts.grid = !v.opts.grid; v.applyGrid(); e.currentTarget.setAttribute('aria-pressed', String(v.opts.grid)); }, v.opts.grid);
    const bgSel = h('select.input.sm', { 'aria-label': t('media.bg'), onchange: (e) => { v.opts.bg = e.target.value; v.applyBg(); } },
      ['theme', 'dark', 'light'].map((x) => h('option', { value: x }, t('media.bgs.' + x))));
    groups.push(h('div.media-3d-group', lights, wire, grid, bgSel));
    // quality: per view, plus "use as my default"
    const qSel = h('select.input.sm', { 'data-glyph': 'effort', 'aria-label': t('media.quality'), onchange: (e) => { v.setQuality(e.target.value); star.disabled = e.target.value === defaultQuality(); } },
      ['low', 'medium', 'high', 'ultra'].map((x) => h('option', { value: x, selected: v.opts.quality === x || null }, t('media.q.' + x))));
    const star = h('button.btn.sm.ghost', { type: 'button', disabled: true, 'data-tip': t('media.qDefaultTip'), onclick: () => { setDefaultQuality(qSel.value); star.disabled = true; toast(t('media.qSaved', { q: t('media.q.' + qSel.value) })); } }, icon('check'), t('media.qDefault'));
    groups.push(h('div.media-3d-group', qSel, star));
    groups.push(h('div.media-3d-group.end',
      btn('gauge', t('media.stats'), (e) => { side.hidden = !side.hidden; e.currentTarget.setAttribute('aria-pressed', String(!side.hidden)); }, false),
      btn('compress', t('media.fit'), () => v.fit()),
      btn('copy', t('media.shot'), () => { const a = h('a', { href: v.screenshot(), download: name.replace(/\.[^.]+$/, '') + '.png' }); document.body.append(a); a.click(); a.remove(); }),
      btn('external', t('media.full'), () => { const el = stage.closest('.media-card'); if (document.fullscreenElement) document.exitFullscreen(); else el.requestFullscreen?.(); })));
    bar.replaceChildren(...groups);
    stage.append(side);
  }
  return card('model', name, url, h('div.media-3d', stage, bar), h('span.tag', '.' + ext));
}

// ---- reading a message ----
// Returns { nodes: text/code/table nodes, media: cards for referenced files }.
export function richBlocks(text, md) {
  const nodes = [];
  for (const b of blocks(text)) {
    if (b.type === 'text') {
      // ![alt](file) becomes a card below, so the raw markdown is not shown twice
      const rest = b.text.replace(/!\[[^\]]*\]\([^)]*\)/g, '').replace(/\n{3,}/g, '\n\n').trim();
      if (rest) nodes.push(...md(rest));
    }
    else if (b.type === 'table') { const tb = tableOf(b.rows); if (tb) nodes.push(tableCard(tb)); }
    else if (b.type === 'code') {
      const spec = b.lang === 'chart' || b.lang === 'vega-lite' ? chartSpec(b.code) : null;
      if (spec) nodes.push(chartCard(spec));
      else if (b.lang === 'csv' || b.lang === 'tsv') { const tb = tableOf(parseCSV(b.code, b.lang === 'tsv' ? '\t' : undefined)); if (tb) nodes.push(tableCard(tb)); }
      else nodes.push(h('pre.media-code', { 'data-lang': b.lang || null }, h('code', b.code)));
    }
  }
  return nodes;
}
// skip: files shown elsewhere (a tool row leaves to the reply the files the reply names)
export function mediaFor(text, max = 12, skip) {
  const list = refs(text).filter((r) => !skip?.has(r.src)).slice(0, max);
  return list.length ? h('div.media-list', list.map(mediaCard)) : null;
}
export { kindOf, refs };
