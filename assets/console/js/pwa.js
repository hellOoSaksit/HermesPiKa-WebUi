// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// The Console as an installable web app (PWA): the service worker, the
// browser's "install" offer, and how to install by hand where there is none.
// Works on any OS the browser runs on: http://127.0.0.1 counts as a secure
// context, and a team server serves the Console over HTTPS.

let offer = null; // the beforeinstallprompt event, kept until the person clicks
const listeners = new Set();
const changed = () => listeners.forEach((f) => { try { f(); } catch { /* a closed page */ } });

export const onInstallChange = (f) => { listeners.add(f); return () => listeners.delete(f); };
export const canPrompt = () => !!offer;
export const swSupported = () => 'serviceWorker' in navigator && window.isSecureContext;

// Running as the installed app (its own window), not in a browser tab.
export function installed() {
  return matchMedia('(display-mode: standalone)').matches || matchMedia('(display-mode: window-controls-overlay)').matches || navigator.standalone === true;
}

export async function promptInstall() {
  if (!offer) return 'unavailable';
  const ev = offer;
  offer = null;
  ev.prompt();
  const choice = await ev.userChoice.catch(() => ({ outcome: 'dismissed' }));
  changed();
  return choice.outcome; // accepted | dismissed
}

// Which hint to show when the browser gives no install offer (an i18n key suffix).
export function browserHint(ua = navigator.userAgent) {
  if (/iPhone|iPad|iPod/.test(ua)) return 'ios';
  if (/Edg\//.test(ua)) return 'edge';
  if (/Firefox\//.test(ua)) return 'firefox';
  if (/Chrome\//.test(ua) || /Chromium\//.test(ua)) return 'chrome';
  if (/Safari\//.test(ua) && /Macintosh/.test(ua)) return 'safari';
  return 'other';
}

export function initPWA() {
  addEventListener('beforeinstallprompt', (e) => { e.preventDefault(); offer = e; changed(); });
  addEventListener('appinstalled', () => { offer = null; changed(); });
  if (!swSupported()) return;
  navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch(() => { /* still a working web page */ });
  // A clicked desktop notice asks this window to show a page.
  navigator.serviceWorker.addEventListener('message', (e) => {
    const href = e.data && e.data.hpk === 'go' && typeof e.data.href === 'string' ? e.data.href : '';
    if (/^#\/[\w/%.-]*$/.test(href)) location.hash = href;
  });
}
