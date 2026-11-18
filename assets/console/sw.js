// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// HPK Console service worker. It exists so the Console can be installed as an
// app and opens fast, and so a closed hpk shows a clear page instead of the
// browser's error. Rules:
//   - /api/* and the one-time key links /k/* are never touched (no cache,
//     no respondWith): live data only.
//   - fonts, icons and bundled libraries (vendor/): cache first; they change
//     only with a new hpk build, and a new build ships a new CACHE name.
//   - the Console shell (JS, CSS, i18n): network first, so a running hpk
//     always serves its own version; the cached copy is used only when hpk is
//     not answering (the offline page uses the fonts and icons).
//   - a page load while hpk is not running gets offline.html (not the cached
//     Console, which would show demo data as if it were real).
const CACHE = 'hpk-console-v2';
const SHELL = [
  'offline.html', 'needkey.html', 'manifest.webmanifest',
  'css/console.css', 'js/boot.js', 'js/offline.js',
  'fonts/anuphan-latin.woff2', 'fonts/anuphan-latin-ext.woff2', 'fonts/anuphan-thai.woff2',
  'icons/icon.svg', 'icons/icon-192.png', 'icons/icon-512.png',
];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(caches.keys()
    .then((keys) => Promise.all(keys.filter((k) => k.startsWith('hpk-console-') && k !== CACHE).map((k) => caches.delete(k))))
    .then(() => self.clients.claim()));
});

// route decides how a request is handled (tools/pwa.test.mjs checks it).
const STATIC = /^\/(fonts|icons|vendor)\//;
function route(url, method) {
  if (method !== 'GET') return 'skip';
  if (url.origin !== self.location.origin) return 'skip';
  if (url.pathname.startsWith('/api/')) return 'skip';
  if (url.pathname.startsWith('/k/')) return 'skip'; // one-time Console key links: the browser alone
  if (STATIC.test(url.pathname)) return 'cache-first';
  return 'network-first';
}
self.hpkRoute = route;

self.addEventListener('fetch', (e) => {
  const req = e.request;
  const how = route(new URL(req.url), req.method);
  if (how === 'skip') return;
  if (how === 'cache-first') {
    e.respondWith(caches.match(req).then((hit) => hit || fetch(req).then((res) => keep(req, res))));
    return;
  }
  // A page load while hpk is down gets offline.html, never the cached Console:
  // the Console would fall back to its demo data, which looks like real work.
  if (req.mode === 'navigate') {
    // hpk answers but this browser has no Console key: say how to get one.
    e.respondWith(fetch(req).then((res) => (needsKey(res) ? caches.match('needkey.html').then((p) => p || res) : res))
      .catch(() => caches.match('offline.html')));
    return;
  }
  e.respondWith(fetch(req).then((res) => keep(req, res)).catch(() => caches.match(req, { ignoreSearch: true })
    .then((hit) => hit || Response.error())));
});

const needsKey = (res) => res.status === 401 && res.headers.get('X-HPK-Need-Key') === '1';
self.hpkNeedsKey = needsKey;

function keep(req, res) {
  if (res && res.ok && res.type === 'basic') {
    const copy = res.clone();
    caches.open(CACHE).then((c) => c.put(req, copy)).catch(() => {});
  }
  return res;
}

// A desktop notice was clicked: bring a Console window forward on that page.
self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const href = (e.notification.data && e.notification.data.href) || '#/overview';
  const target = new URL('/' + href.replace(/^\/?/, ''), self.location.origin).href;
  e.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
    const win = list.find((c) => new URL(c.url).origin === self.location.origin);
    if (!win) return self.clients.openWindow(target);
    win.postMessage({ hpk: 'go', href });
    return win.focus();
  }));
});
