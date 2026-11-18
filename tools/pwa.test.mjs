// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// The Console as an installable web app: manifest, icons, service worker and
// the desktop-notice rules.
//
//   node --test tools/
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import vm from 'node:vm';
import { parse, inQuiet, classify, shouldShow, streaming } from '../assets/console/js/notifyrules.js';

const C = 'assets/console';
const read = (p) => readFileSync(join(C, p));

test('manifest has what browsers need to offer install', () => {
  const m = JSON.parse(read('manifest.webmanifest'));
  assert.equal(m.display, 'standalone');
  assert.equal(m.start_url, '/#/overview');
  assert.equal(m.scope, '/');
  assert.ok(m.name && m.short_name);
  // theme and background follow the dark tokens in console.css
  const css = read('css/console.css').toString();
  const dark = css.slice(css.indexOf(':root[data-theme="dark"]'));
  assert.ok(dark.includes(`--ground: ${m.background_color};`), 'background_color is --ground');
  assert.ok(dark.includes(`--panel: ${m.theme_color};`), 'theme_color is --panel');
  const pngs = m.icons.filter((i) => i.type === 'image/png');
  for (const want of ['192x192', '512x512']) assert.ok(pngs.some((i) => i.sizes === want && i.purpose === 'any'), 'png ' + want);
  assert.ok(m.icons.some((i) => i.purpose === 'maskable'), 'a maskable icon');
  for (const i of m.icons) {
    assert.ok(existsSync(join(C, i.src)), i.src);
    if (i.type === 'image/png') {
      const b = read(i.src);
      assert.equal(b.toString('latin1', 1, 4), 'PNG', i.src);
      const [w, h] = [b.readUInt32BE(16), b.readUInt32BE(20)];
      assert.equal(`${w}x${h}`, i.sizes, i.src);
    }
  }
});

test('index.html links the manifest and icons from the same origin', () => {
  const html = read('index.html').toString();
  assert.match(html, /<link rel="manifest" href="manifest\.webmanifest">/);
  assert.match(html, /<meta name="theme-color"/);
  for (const m of html.matchAll(/<link rel="(?:icon|apple-touch-icon|manifest)" href="([^"]+)"/g)) {
    assert.ok(!/^(https?:)?\/\//.test(m[1]), 'no other host: ' + m[1]);
    assert.ok(existsSync(join(C, m[1])), m[1]);
  }
});

function loadWorker() {
  const handlers = {};
  const self = { location: { origin: 'http://127.0.0.1:7878' }, addEventListener: (t, f) => { handlers[t] = f; }, clients: {}, skipWaiting() {} };
  vm.runInNewContext(read('sw.js').toString(), { self, caches: {}, fetch() {}, URL, Response: {}, console });
  return { self, handlers };
}

test('service worker never touches /api and caches only this origin', () => {
  const { self, handlers } = loadWorker();
  for (const t of ['install', 'activate', 'fetch', 'notificationclick']) assert.equal(typeof handlers[t], 'function', t);
  const r = (u, m = 'GET') => self.hpkRoute(new URL(u), m);
  assert.equal(r('http://127.0.0.1:7878/api/snapshot'), 'skip');
  assert.equal(r('http://127.0.0.1:7878/api/events'), 'skip');
  assert.equal(r('http://127.0.0.1:7878/api/health'), 'skip');
  assert.equal(r('http://127.0.0.1:7878/index.html', 'POST'), 'skip');
  assert.equal(r('https://example.test/fonts/x.woff2'), 'skip');
  assert.equal(r('http://127.0.0.1:7878/fonts/anuphan-thai.woff2'), 'cache-first');
  assert.equal(r('http://127.0.0.1:7878/vendor/three/build/three.module.js'), 'cache-first');
  assert.equal(r('http://127.0.0.1:7878/k/one-time-code'), 'skip', 'Console key links are never cached');
  assert.equal(r('http://127.0.0.1:7878/'), 'network-first');
  const res = (status, key) => ({ status, headers: { get: (h) => (h === 'X-HPK-Need-Key' ? key : null) } });
  assert.equal(self.hpkNeedsKey(res(401, '1')), true);
  assert.equal(self.hpkNeedsKey(res(401, null)), false, 'a plain 401 (team sign-in) is not the Console key');
  assert.equal(self.hpkNeedsKey(res(200, '1')), false);
  assert.equal(r('http://127.0.0.1:7878/js/app.js'), 'network-first');
});

test('every file the worker pre-caches exists, and the offline page says how to start hpk', () => {
  const src = read('sw.js').toString();
  const list = JSON.parse(src.slice(src.indexOf('const SHELL = ') + 14, src.indexOf('];', src.indexOf('const SHELL')) + 1).replace(/'/g, '"').replace(/,\s*\]/, ']'));
  assert.ok(list.includes('offline.html'));
  for (const f of list) if (f !== '/') assert.ok(existsSync(join(C, f)), f);
  const off = read('offline.html').toString();
  assert.match(off, /hpk is not running/);
  assert.match(off, /<code>hpk<\/code>/);
  assert.match(off, /<code>hpk open<\/code>/);
  const nk = read('needkey.html').toString();
  assert.match(nk, /<code>hpk open<\/code>/);
  assert.ok(!/<script/.test(nk), 'the key page runs no script (it would loop back to the Console)');
  assert.ok(list.includes('needkey.html'));
  assert.ok(!/<script>/.test(off), 'no inline script (CSP)');
  for (const m of off.matchAll(/(?:src|href)="([^"#]+)"/g)) if (!m[1].startsWith('/')) assert.ok(list.includes(m[1]), 'offline page needs ' + m[1] + ' cached');
});

test('quiet hours, including past midnight', () => {
  const at = (h, m = 0) => new Date(2026, 10, 18, h, m);
  const night = { on: true, from: '22:00', to: '07:00' };
  assert.equal(inQuiet(night, at(23)), true);
  assert.equal(inQuiet(night, at(3)), true);
  assert.equal(inQuiet(night, at(7)), false);
  assert.equal(inQuiet(night, at(12)), false);
  const lunch = { on: true, from: '12:00', to: '13:00' };
  assert.equal(inQuiet(lunch, at(12, 30)), true);
  assert.equal(inQuiet(lunch, at(13)), false);
  assert.equal(inQuiet({ ...night, on: false }, at(23)), false);
  assert.equal(inQuiet({ on: true, from: '09:00', to: '09:00' }, at(9)), false);
});

test('settings parse safely and decide what shows', () => {
  const d = parse('not json');
  assert.equal(d.on, false, 'off until the person opts in');
  assert.equal(d.kinds.redline, true);
  const cfg = parse(JSON.stringify({ on: true, kinds: { chat: false }, quiet: { on: true, from: '25:00', to: '07:00' } }));
  assert.equal(cfg.quiet.from, '22:00', 'a bad time falls back');
  assert.equal(shouldShow(cfg, 'chat', {}), false, 'kind turned off');
  assert.equal(shouldShow(cfg, 'redline', { visible: true, now: new Date(2026, 0, 1, 12) }), false, 'Console in front');
  assert.equal(shouldShow(cfg, 'redline', { visible: false, now: new Date(2026, 0, 1, 12) }), true);
  assert.equal(shouldShow(cfg, 'redline', { visible: false, now: new Date(2026, 0, 1, 23) }), false, 'quiet hours');
});

test('approval cards map to notice kinds', () => {
  assert.equal(classify({ kind: 'permission', redLine: 'push' }), 'redline');
  assert.equal(classify({ kind: 'permission' }), 'approval');
  assert.equal(classify({ kind: 'question' }), 'approval');
  assert.equal(classify({ kind: 'draft' }), '');
  assert.equal(classify({ kind: 'stale' }), '');
  assert.equal(streaming([{ t: 'msg' }, { t: 'msg', streaming: true }]), true);
  assert.equal(streaming([{ t: 'msg' }]), false);
});
