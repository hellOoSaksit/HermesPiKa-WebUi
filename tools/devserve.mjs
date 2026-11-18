// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Development-only static server for the HPK Console front-end.
// In production the Console is served by `hpk serve` (Go). Usage: node tools/devserve.mjs [port]
import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { extname, join, normalize, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(fileURLToPath(new URL('../assets/console/', import.meta.url)));
const port = Number(process.argv[2] || process.env.PORT || 5173);
const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8', '.json': 'application/json; charset=utf-8', '.svg': 'image/svg+xml', '.woff2': 'font/woff2',
  '.png': 'image/png', '.jpg': 'image/jpeg', '.webp': 'image/webp', '.mp4': 'video/mp4', '.webm': 'video/webm', '.pdf': 'application/pdf', '.csv': 'text/csv; charset=utf-8',
  '.glb': 'model/gltf-binary', '.gltf': 'model/gltf+json', '.fbx': 'application/octet-stream', '.obj': 'text/plain', '.mtl': 'text/plain', '.stl': 'model/stl' };
// Demo chat media (tools/fixtures/media, made by make-media.mjs); hpk serve has the real /api/files.
const fixtures = resolve(fileURLToPath(new URL('./fixtures/media/', import.meta.url)));

createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://localhost');
    let path = normalize(join(root, decodeURIComponent(url.pathname)));
    if (url.pathname === '/api/files') path = normalize(join(fixtures, (url.searchParams.get('path') || '').replace(/^.*[\\/]/, '')));
    if (!path.startsWith(root) && !path.startsWith(fixtures)) { res.writeHead(403).end(); return; }
    if ((await stat(path).catch(() => null))?.isDirectory()) path = join(path, 'index.html');
    const body = await readFile(path);
    res.writeHead(200, { 'Content-Type': types[extname(path)] || 'application/octet-stream', 'Cache-Control': 'no-store' });
    res.end(body);
  } catch {
    res.writeHead(404, { 'Content-Type': 'text/plain' }).end('Not found');
  }
}).listen(port, '127.0.0.1', () => console.log(`HPK Console dev server: http://127.0.0.1:${port}/`));
