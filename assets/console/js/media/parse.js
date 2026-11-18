// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Reading agent text and tool output for things the chat can show:
// fenced code blocks (```chart, ```csv …), markdown tables, images/links,
// file paths with a media extension, and data: URIs (e.g. a Blender viewport
// screenshot returned by an MCP tool). Pure functions, no DOM.

export const KINDS = {
  image: ['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'svg'],
  video: ['mp4', 'webm', 'mov', 'm4v', 'ogv'],
  audio: ['mp3', 'wav', 'ogg', 'oga', 'flac', 'm4a'],
  model: ['glb', 'gltf', 'fbx', 'obj', 'stl', 'ply', 'dae', '3mf'],
  pdf: ['pdf'],
  data: ['csv', 'tsv', 'json'],
};
const EXT_KIND = Object.fromEntries(Object.entries(KINDS).flatMap(([k, list]) => list.map((e) => [e, k])));
const EXT_RE = Object.keys(EXT_KIND).sort((a, b) => b.length - a.length).join('|');

export function extOf(p) {
  const m = String(p).split(/[?#]/)[0].match(/\.([a-z0-9]+)$/i);
  return m ? m[1].toLowerCase() : '';
}
export function kindOf(p) {
  if (/^data:image\//i.test(p)) return 'image';
  if (/^data:video\//i.test(p)) return 'video';
  if (/^data:audio\//i.test(p)) return 'audio';
  return EXT_KIND[extOf(p)] || '';
}
export const baseName = (p) => (/^data:/i.test(p) ? p.slice(5, p.indexOf(';')) : String(p).split(/[?#]/)[0].split(/[\\/]/).pop());

// Split text into blocks: text, code (with lang), table (rows of cells).
export function blocks(text) {
  const out = [];
  const lines = String(text ?? '').replace(/\r\n?/g, '\n').split('\n');
  let buf = [];
  const flush = () => { if (buf.length) { out.push({ type: 'text', text: buf.join('\n') }); buf = []; } };
  for (let i = 0; i < lines.length; i++) {
    const fence = lines[i].match(/^\s*(```+|~~~+)\s*([\w+.-]*)\s*$/);
    if (fence) {
      const end = lines.findIndex((l, j) => j > i && l.trim().startsWith(fence[1]));
      if (end > i) {
        flush();
        out.push({ type: 'code', lang: fence[2].toLowerCase(), code: lines.slice(i + 1, end).join('\n') });
        i = end;
        continue;
      }
    }
    // markdown table: header | sep | rows
    if (/^\s*\|.*\|\s*$/.test(lines[i]) && /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(lines[i + 1] || '')) {
      flush();
      const rows = [lines[i]];
      let j = i + 2;
      while (j < lines.length && /^\s*\|.*\|\s*$/.test(lines[j])) rows.push(lines[j++]);
      out.push({ type: 'table', rows: rows.map(splitRow) });
      i = j - 1;
      continue;
    }
    buf.push(lines[i]);
  }
  flush();
  return out;
}
const splitRow = (l) => l.trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim());

// Media references inside plain text, in order, without duplicates.
const REF_RE = new RegExp([
  String.raw`!\[([^\]]*)\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`, // ![alt](src)
  String.raw`\[([^\]]+)\]\(\s*<?([^)\s>]+)>?\s*\)`, // [text](src)
  String.raw`(data:(?:image|video|audio)\/[a-z0-9.+-]+;base64,[A-Za-z0-9+/=]{16,})`, // data URI
  String.raw`(https?:\/\/[^\s<>()"'\x60]+?\.(?:${EXT_RE}))(?![\w])`, // URL with a media extension
  String.raw`[\x60"']([^\x60"'\n]*[^\x60"'\n\s]\.(?:${EXT_RE}))[\x60"']`, // a quoted path (may contain spaces)
  String.raw`((?:[A-Za-z]:[\\/]|~[\\/]|\.{1,2}[\\/]|[\\/])?(?:[\w@.()+-]+[\\/])*[\w@.()+-]*[\w)-]\.(?:${EXT_RE}))(?![\w(])`, // a bare path (not a call like export_scene.gltf(…))
].join('|'), 'gi');

export function refs(text) {
  const out = [];
  const seen = new Set();
  const add = (src, alt) => {
    src = src.replace(/^["'`]|["'`.,;:]+$/g, '');
    const kind = kindOf(src);
    if (!kind || seen.has(src)) return;
    // a bare word like "image.png" inside a sentence is still a file; "e.g." is not
    seen.add(src);
    out.push({ src, kind, alt: alt || baseName(src), remote: /^https?:/i.test(src), inline: /^data:/i.test(src) });
  };
  for (const m of String(text ?? '').matchAll(REF_RE)) {
    if (m[2]) add(m[2], m[1]);
    else if (m[4]) add(m[4], m[3]);
    else add(m[5] || m[6] || m[7] || m[8]);
  }
  return out;
}

// Turn rows of strings into a typed table: header + rows, numeric columns marked.
export function tableOf(rows) {
  if (!rows?.length) return null;
  const [head, ...body] = rows;
  const width = Math.max(head.length, ...body.map((r) => r.length));
  const cols = Array.from({ length: width }, (_, i) => head[i] ?? `col ${i + 1}`);
  const num = (v) => {
    if (v == null) return NaN;
    const s = String(v).trim().replace(/[,_\s]/g, '').replace(/^\$|%$/g, '');
    return s === '' ? NaN : Number(s);
  };
  const numeric = cols.map((_, i) => body.length > 0 && body.every((r) => r[i] == null || String(r[i]).trim() === '' || Number.isFinite(num(r[i]))));
  return { cols, rows: body, numeric, num };
}

export function parseCSV(text, sep) {
  sep ??= /\t/.test(text.split('\n')[0]) ? '\t' : ',';
  const rows = []; let row = []; let cell = ''; let q = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (q) {
      if (c === '"' && text[i + 1] === '"') { cell += '"'; i++; } else if (c === '"') q = false; else cell += c;
    } else if (c === '"') q = true;
    else if (c === sep) { row.push(cell); cell = ''; }
    else if (c === '\n' || c === '\r') {
      if (c === '\r' && text[i + 1] === '\n') i++;
      row.push(cell); cell = '';
      if (row.some((x) => x !== '')) rows.push(row);
      row = [];
    } else cell += c;
  }
  row.push(cell);
  if (row.some((x) => x !== '')) rows.push(row);
  return rows;
}

// JSON that looks like a list of flat records → rows.
export function rowsFromJSON(v) {
  if (Array.isArray(v) && v.length && v.every((x) => x && typeof x === 'object' && !Array.isArray(x))) {
    const keys = [...new Set(v.flatMap((x) => Object.keys(x)))].slice(0, 30);
    return [keys, ...v.slice(0, 5000).map((x) => keys.map((k) => (x[k] == null ? '' : typeof x[k] === 'object' ? JSON.stringify(x[k]) : String(x[k]))))];
  }
  return null;
}

// A ```chart block: JSON {type, title, labels, series:[{name, data}]}.
export function chartSpec(code) {
  try {
    const v = JSON.parse(code);
    if (!v || !Array.isArray(v.series) || !v.series.length) return null;
    return {
      type: ['bar', 'line', 'area', 'pie', 'donut', 'scatter', 'stacked'].includes(v.type) ? v.type : 'bar',
      title: String(v.title || ''),
      labels: Array.isArray(v.labels) ? v.labels.map(String) : v.series[0].data.map((_, i) => String(i + 1)),
      series: v.series.slice(0, 8).map((s, i) => ({ name: String(s.name ?? `Series ${i + 1}`), data: (s.data || []).map((d) => (Array.isArray(d) ? d.map(Number) : Number(d))) })),
      unit: v.unit ? String(v.unit) : '',
    };
  } catch { return null; }
}

// A table → chart spec: the first column names the rows (x), the other number
// columns are series.
export function chartFromTable(tb) {
  if (!tb || tb.rows.length < 2) return null;
  const lc = tb.cols.length > 1 ? 0 : -1;
  const cols = tb.numeric.map((n, i) => (n && i !== lc ? i : -1)).filter((i) => i >= 0).slice(0, 8);
  if (!cols.length) return null;
  return {
    type: tb.rows.length > 14 ? 'line' : 'bar', title: '',
    labels: tb.rows.slice(0, 500).map((r, i) => (lc >= 0 ? String(r[lc]) : String(i + 1))),
    series: cols.map((c) => ({ name: tb.cols[c], data: tb.rows.slice(0, 500).map((r) => tb.num(r[c])) })),
  };
}
