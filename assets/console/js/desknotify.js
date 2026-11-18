// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Desktop notices through the Web Notifications API, on any OS the browser
// runs on: approvals waiting, red lines, a chat that finished a turn, and the
// built-in browser waiting for the person. Opt-in per browser in Settings, with
// quiet hours. The Console's own bell keeps every notice either way.
import { bus, store } from './data.js';
import { t } from './i18n.js';
import { parse, classify, streaming, shouldShow } from './notifyrules.js';

const KEY = 'hpk.notify';
export const supported = () => 'Notification' in window && window.isSecureContext;
export const permission = () => (supported() ? Notification.permission : 'unsupported');
export function getNotify() { try { return parse(localStorage.getItem(KEY)); } catch { return parse(''); } }
export function setNotify(v) { try { localStorage.setItem(KEY, JSON.stringify(v)); } catch { /* storage unavailable */ } }

export async function askPermission() {
  if (!supported()) return 'unsupported';
  if (Notification.permission !== 'default') return Notification.permission;
  return Notification.requestPermission();
}

const visible = () => document.visibilityState === 'visible' && document.hasFocus();

export async function show(kind, title, body, href, tag) {
  const cfg = getNotify();
  if (permission() !== 'granted' || !shouldShow(cfg, kind, { visible: visible() })) return false;
  const opts = { body, tag: tag || kind, icon: 'icons/icon-192.png', badge: 'icons/icon-192.png', data: { href }, renotify: false };
  try {
    const reg = 'serviceWorker' in navigator ? await navigator.serviceWorker.getRegistration() : null;
    if (reg) { await reg.showNotification(title, opts); return true; }
  } catch { /* fall back to a page notice */ }
  const n = new Notification(title, opts);
  n.onclick = () => { focus(); if (href) location.hash = href; n.close(); };
  return true;
}

// test sends one notice now, ignoring "only when away" and quiet hours.
export function test() {
  const opts = { body: t('desk.testBody'), tag: 'hpk-test', icon: 'icons/icon-192.png', data: { href: '#/settings' } };
  return navigator.serviceWorker?.getRegistration().then((r) => (r ? r.showNotification(t('desk.testTitle'), opts) : new Notification(t('desk.testTitle'), opts)))
    .catch(() => new Notification(t('desk.testTitle'), opts));
}

let primed = false;
const seen = new Set();
const wasStreaming = {};

function onApprovals() {
  const list = store.approvals || [];
  if (!primed) { list.forEach((a) => seen.add(a.id)); primed = true; return; }
  for (const a of list) {
    if (seen.has(a.id)) continue;
    seen.add(a.id);
    const kind = classify(a);
    if (!kind) continue;
    const title = kind === 'redline' ? t('desk.redline', { line: a.redLine }) : t('desk.approval');
    show(kind, title, a.title || a.detail || '', '#/approvals', 'hpk-' + a.id);
  }
}

function onChat(sid) {
  if (!sid) return;
  const now = streaming(store.chat[sid]);
  const before = wasStreaming[sid];
  wasStreaming[sid] = now;
  if (!before || now) return;
  const s = (store.sessions || []).find((x) => x.id === sid);
  show('chat', t('desk.chatDone'), s ? s.title : sid, '#/sessions/' + encodeURIComponent(sid), 'hpk-chat-' + sid);
}

function onBrowser(why) {
  show('browser', t('desk.browser'), why === 'vault' ? t('desk.browserVault') : t('desk.browserDriver'), '#/browser', 'hpk-browser');
}

export function initDeskNotify() {
  bus.addEventListener('approvals', onApprovals);
  bus.addEventListener('chat', (e) => onChat(e.detail));
  bus.addEventListener('browser-needs', (e) => onBrowser(e.detail));
  onApprovals(); // remember what is already waiting: only new cards notify
}
