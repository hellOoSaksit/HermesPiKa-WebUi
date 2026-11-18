// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Writes small, made-up media files the dev server shows in the demo chat:
// an animated, textured glTF with its own light, an OBJ + MTL + texture, an STL,
// a PNG render and a CSV. Nothing is downloaded. Usage: node tools/fixtures/make-media.mjs
import { deflateSync } from 'node:zlib';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const out = join(dirname(fileURLToPath(import.meta.url)), 'media');
mkdirSync(out, { recursive: true });

// ---- PNG ----
const crcTable = Array.from({ length: 256 }, (_, n) => { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; return c >>> 0; });
const crc = (buf) => { let c = 0xffffffff; for (const b of buf) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0; };
function chunk(type, data) {
  const len = Buffer.alloc(4); len.writeUInt32BE(data.length);
  const td = Buffer.concat([Buffer.from(type), data]);
  const c = Buffer.alloc(4); c.writeUInt32BE(crc(td));
  return Buffer.concat([len, td, c]);
}
function png(w, hgt, px) { // px(x, y) -> [r, g, b]
  const raw = Buffer.alloc((w * 3 + 1) * hgt);
  for (let y = 0; y < hgt; y++) {
    raw[y * (w * 3 + 1)] = 0;
    for (let x = 0; x < w; x++) { const [r, g, b] = px(x, y); const i = y * (w * 3 + 1) + 1 + x * 3; raw[i] = r; raw[i + 1] = g; raw[i + 2] = b; }
  }
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(hgt, 4); ihdr[8] = 8; ihdr[9] = 2;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}
const checker = (a, b) => (x, y) => (((x >> 4) + (y >> 4)) & 1 ? a : b);
const kartTex = png(128, 128, (x, y) => (y < 20 ? [236, 121, 192] : checker([40, 36, 48], [245, 240, 248])(x, y)));
const crateTex = png(64, 64, (x, y) => (x < 4 || y < 4 || x > 59 || y > 59 || Math.abs(x - y) < 3 ? [92, 58, 30] : [196, 140, 82]));
writeFileSync(join(out, 'crate.png'), crateTex);
writeFileSync(join(out, 'render.png'), png(320, 180, (x, y) => {
  const d = Math.hypot(x - 160, y - 100) / 90;
  return d < 1 ? [Math.round(236 - d * 120), Math.round(121 - d * 40), Math.round(192 - d * 60)] : [20 + (y >> 3), 17 + (y >> 3), 23 + (y >> 3)];
}));

// ---- glTF: a textured box that spins and bounces, lit by its own spot light ----
const faces = [ // normal, u axis, v axis
  [[0, 0, 1], [1, 0, 0], [0, 1, 0]], [[0, 0, -1], [-1, 0, 0], [0, 1, 0]], [[1, 0, 0], [0, 0, -1], [0, 1, 0]],
  [[-1, 0, 0], [0, 0, 1], [0, 1, 0]], [[0, 1, 0], [1, 0, 0], [0, 0, -1]], [[0, -1, 0], [1, 0, 0], [0, 0, 1]],
];
const pos = []; const nor = []; const uv = []; const idx = [];
faces.forEach(([n, u, v], f) => {
  for (const [a, b] of [[-1, -1], [1, -1], [1, 1], [-1, 1]]) {
    pos.push(...n.map((c, i) => (c + a * u[i] + b * v[i]) * 0.5 * [1.6, 0.6, 1][i]));
    nor.push(...n); uv.push((a + 1) / 2, 1 - (b + 1) / 2);
  }
  const o = f * 4; idx.push(o, o + 1, o + 2, o, o + 2, o + 3);
});
const times = [0, 0.5, 1, 1.5, 2];
const spin = times.flatMap((t) => { const a = (t / 2) * Math.PI; return [0, Math.sin(a), 0, Math.cos(a)]; }); // 0..180° about Y
const bounce = times.flatMap((t) => [0, Math.abs(Math.sin(t * Math.PI)) * 0.6, 0]);
const f32 = (a) => Buffer.from(new Float32Array(a).buffer);
const parts = [f32(pos), f32(nor), f32(uv), Buffer.from(new Uint16Array(idx).buffer), f32(times), f32(spin), f32(bounce)];
const pad = (b) => Buffer.concat([b, Buffer.alloc((4 - (b.length % 4)) % 4)]);
let offset = 0;
const views = parts.map((b) => { const v = { buffer: 0, byteOffset: offset, byteLength: b.length }; offset += pad(b).length; return v; });
const bin = Buffer.concat(parts.map(pad));
const min = [0, 1, 2].map((i) => Math.min(...pos.filter((_, j) => j % 3 === i)));
const max = [0, 1, 2].map((i) => Math.max(...pos.filter((_, j) => j % 3 === i)));
const gltf = {
  asset: { version: '2.0', generator: 'hpk fixtures' },
  extensionsUsed: ['KHR_lights_punctual'],
  extensions: { KHR_lights_punctual: { lights: [{ type: 'spot', color: [1, 0.85, 0.95], intensity: 40, spot: { outerConeAngle: 0.8 } }] } },
  scene: 0,
  scenes: [{ nodes: [0, 1] }],
  nodes: [
    { name: 'kart-body', mesh: 0 },
    { name: 'spot', translation: [0, 3, 2], rotation: [-0.4, 0, 0, 0.92], extensions: { KHR_lights_punctual: { light: 0 } } },
  ],
  meshes: [{ name: 'body', primitives: [{ attributes: { POSITION: 0, NORMAL: 1, TEXCOORD_0: 2 }, indices: 3, material: 0 }] }],
  materials: [{ name: 'paint', pbrMetallicRoughness: { baseColorTexture: { index: 0 }, metallicFactor: 0.2, roughnessFactor: 0.5 } }],
  textures: [{ source: 0, sampler: 0 }],
  samplers: [{ magFilter: 9728, minFilter: 9728 }],
  images: [{ uri: 'kart-paint.png' }], // beside the .gltf: loaded through the same file endpoint
  buffers: [{ byteLength: bin.length, uri: 'data:application/octet-stream;base64,' + bin.toString('base64') }],
  bufferViews: views,
  accessors: [
    { bufferView: 0, componentType: 5126, count: pos.length / 3, type: 'VEC3', min, max },
    { bufferView: 1, componentType: 5126, count: nor.length / 3, type: 'VEC3' },
    { bufferView: 2, componentType: 5126, count: uv.length / 2, type: 'VEC2' },
    { bufferView: 3, componentType: 5123, count: idx.length, type: 'SCALAR' },
    { bufferView: 4, componentType: 5126, count: times.length, type: 'SCALAR', min: [0], max: [2] },
    { bufferView: 5, componentType: 5126, count: times.length, type: 'VEC4' },
    { bufferView: 6, componentType: 5126, count: times.length, type: 'VEC3' },
  ],
  animations: [
    { name: 'Spin', samplers: [{ input: 4, output: 5, interpolation: 'LINEAR' }], channels: [{ sampler: 0, target: { node: 0, path: 'rotation' } }] },
    { name: 'Bounce', samplers: [{ input: 4, output: 6, interpolation: 'LINEAR' }], channels: [{ sampler: 0, target: { node: 0, path: 'translation' } }] },
  ],
};
writeFileSync(join(out, 'kart.gltf'), JSON.stringify(gltf, null, 1));
writeFileSync(join(out, 'kart-paint.png'), kartTex);

// ---- OBJ + MTL ----
const obj = ['mtllib crate.mtl', 'o crate'];
for (let i = 0; i < pos.length; i += 3) obj.push(`v ${pos[i] / 1.6 * 1.2} ${pos[i + 1] / 0.6 * 1.2} ${pos[i + 2] * 1.2}`);
for (let i = 0; i < uv.length; i += 2) obj.push(`vt ${uv[i]} ${1 - uv[i + 1]}`);
for (let i = 0; i < nor.length; i += 3) obj.push(`vn ${nor[i]} ${nor[i + 1]} ${nor[i + 2]}`);
obj.push('usemtl wood');
for (let i = 0; i < idx.length; i += 3) obj.push('f ' + [idx[i], idx[i + 1], idx[i + 2]].map((k) => `${k + 1}/${k + 1}/${k + 1}`).join(' '));
writeFileSync(join(out, 'crate.obj'), obj.join('\n') + '\n');
writeFileSync(join(out, 'crate.mtl'), 'newmtl wood\nKd 1 1 1\nNs 20\nmap_Kd crate.png\n');

// ---- STL (ASCII): a small pyramid ----
const P = [[0, 1.2, 0], [-1, 0, -1], [1, 0, -1], [1, 0, 1], [-1, 0, 1]];
const tri = [[0, 2, 1], [0, 3, 2], [0, 4, 3], [0, 1, 4], [1, 2, 3], [1, 3, 4]];
writeFileSync(join(out, 'bracket.stl'), 'solid bracket\n' + tri.map(([a, b, c]) => `facet normal 0 0 0\n outer loop\n${[a, b, c].map((k) => `  vertex ${P[k].join(' ')}`).join('\n')}\n endloop\nendfacet`).join('\n') + '\nendsolid bracket\n');

// ---- CSV ----
const laps = ['lap,time_s,top_speed_kmh,fuel_l'];
for (let i = 1; i <= 12; i++) laps.push(`${i},${(62 - i * 0.8 + (i % 3) * 0.6).toFixed(1)},${(148 + i * 2.1 - (i % 4)).toFixed(1)},${(40 - i * 2.9).toFixed(1)}`);
writeFileSync(join(out, 'laps.csv'), laps.join('\n') + '\n');
console.log('wrote', out);
