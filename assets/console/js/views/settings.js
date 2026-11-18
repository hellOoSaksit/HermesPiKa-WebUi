// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { isLive, teamMode } from '../live.js';
import { installed, canPrompt, promptInstall, browserHint, onInstallChange } from '../pwa.js';
import { supported as noticesSupported, permission, askPermission, getNotify, setNotify, test as testNotice } from '../desknotify.js';
import { KINDS } from '../notifyrules.js';
import { t, LANGS, getLang, setLang } from '../i18n.js';
import { h, icon, lamp, pageHead, toast, panel } from '../ui.js';
import { appearancePanel } from './appearance.js';
import { folderRow } from '../folderpick.js';

export default async function settings({ rerender }) {
  const doctorBox = h('div', h('p.muted', { style: { padding: '16px' } }, t('settings.checking')));
  const LEVEL = { ok: 'clear', warn: 'caution', fail: 'danger', info: 'idle' };
  // Live rows come from hpk doctor: { level, name, detail, fix }.
  const liveRow = (r) => h('tr',
    h('td', { style: { width: '28px' } }, lamp(LEVEL[r.level] || 'idle')),
    h('td', h('div.cell-title', r.name), h('div.cell-sub', r.detail)),
    h('td', { style: { textAlign: 'right' } }, r.fix && r.level !== 'ok' ? h('code', r.fix) : null));
  api.doctor().then((rows) => doctorBox.replaceChildren(h('table.t', h('tbody', rows.map((r) => r.name ? liveRow(r) : h('tr',
    h('td', { style: { width: '28px' } }, lamp(r.status)),
    h('td', h('div.cell-title', t('doctor.' + r.key + '.t')), h('div.cell-sub', t('doctor.' + r.key + '.d'))),
    h('td.mono.muted', r.value),
    h('td', { style: { textAlign: 'right' } }, r.status !== 'clear' ? h('button.btn.sm', { type: 'button', onclick: () => toast(t('doctor.' + r.key + '.fix')) }, t('settings.fix')) : null)))))));

  const general = h('div.stack',
    h('div.field', h('label', { for: 'setLang' }, t('settings.language')),
      h('select.input#setLang', { style: { maxWidth: '260px' }, onchange: async (e) => { await setLang(e.target.value); rerender(); } },
        LANGS.map((l) => h('option', { value: l.code, selected: l.code === getLang() || null }, l.label))),
      h('span.hint', t('settings.languageHint'))),
    appRow(rerender),
    isLive() && !teamMode() ? loginRow() : null,
    isLive() && !teamMode() ? projectFolders() : null);
  const appearance = panel(t('settings.ap.title'), t('settings.ap.sub'), appearancePanel());

  const notify = isLive() ? liveNotify() : h('div.stack',
    h('div.field', h('label', { for: 'setCh' }, t('settings.channel')),
      h('select.input#setCh', { style: { maxWidth: '260px' } }, ['console', 'telegram', 'line', 'email'].map((c) => h('option', { value: c }, t('settings.ch.' + c)))),
      h('span.hint', t('settings.channelHint'))),
    h('label.switch', h('input', { type: 'checkbox', checked: true }), t('settings.batch')),
    h('label.switch', h('input', { type: 'checkbox', checked: true }), t('settings.morning')));

  const budgets = isLive() ? await liveBudgets() : h('div.stack',
    h('div.grid.two', { style: { gap: '12px' } },
      h('div.field', h('label', { for: 'bDay' }, t('settings.daily')), h('input.input#bDay', { value: '12.00', inputmode: 'decimal' })),
      h('div.field', h('label', { for: 'bMonth' }, t('settings.monthly')), h('input.input#bMonth', { value: '200.00', inputmode: 'decimal' }))),
    h('div.field', h('label', { for: 'bIngest' }, t('settings.ingestCap')), h('input.input#bIngest', { value: '400000', inputmode: 'numeric', style: { maxWidth: '200px' } }), h('span.hint', t('settings.ingestHint'))),
    h('div.row', h('button.btn.primary', { type: 'button', onclick: () => toast(t('settings.saved')) }, t('common.save'))));

  const about = h('table.t', h('tbody',
    h('tr', h('td.muted', t('settings.version')), h('td.mono', isLive() ? 'hpk ' + (await api.version()) : 'hpk 0.1.0-dev')),
    h('tr', h('td.muted', t('settings.license')), h('td', t('settings.licenseValue'))),
    h('tr', h('td.muted', t('settings.data')), h('td', t('settings.dataValue'))),
    h('tr', h('td.muted', t('settings.sample')), h('td', isLive() ? h('span.tag.clear', t('live.dataLive')) : h('span.tag.caution', t('settings.sampleValue'))))));

  return { el: h('div.page',
    pageHead(t('settings.title'), t('settings.lede'), [h('button.btn', { type: 'button', onclick: () => { doctorBox.replaceChildren(h('p.muted', { style: { padding: '16px' } }, t('settings.checking'))); api.doctor().then(() => rerender()); } }, icon('scan'), t('settings.runDoctor'))]),
    h('div.grid.main-side',
      panel(t('settings.doctor'), t('settings.doctorSub'), doctorBox, { flush: true }),
      h('div.stack', appearance, panel(t('settings.general'), null, general), panel(t('settings.about'), null, about, { flush: true }))),
    h('div.grid.two', panel(t('settings.notify'), null, h('div.stack', deskNotices(), notify)), panel(t('settings.budgets'), null, budgets))) };
}

// Live budgets: the token and money caps hpk enforces, from project.yaml.
async function liveBudgets() {
  const cur = await api.projectSettings().catch(() => ({}));
  const ing = h('input.input#bIngest', { value: String(cur.ingestMax ?? 400000), inputmode: 'numeric', style: { maxWidth: '200px' } });
  const night = h('input.input#bNight', { value: String(cur.nightlyMax ?? 60000), inputmode: 'numeric', style: { maxWidth: '200px' } });
  const daily = h('input.input#bDaily', { value: String(cur.dailyMax ?? 150000), inputmode: 'numeric', style: { maxWidth: '200px' } });
  const usd = (v) => '$' + Number(v || 0).toFixed(2);
  const mDay = h('input.input#bMoneyDay', { value: String(cur.moneyDaily ?? 0), inputmode: 'decimal', style: { maxWidth: '200px' } });
  const mMonth = h('input.input#bMoneyMonth', { value: String(cur.moneyMonthly ?? 0), inputmode: 'decimal', style: { maxWidth: '200px' } });
  return h('div.stack',
    h('div.field', h('label', { for: 'bIngest' }, t('settings.ingestCap')), ing, h('span.hint', t('settings.ingestHint'))),
    h('div.field', h('label', { for: 'bNight' }, t('live.nightlyCap')), night, h('span.hint', t('live.nightlyHint'))),
    h('div.field', h('label', { for: 'bDaily' }, t('live.dailyCap')), daily, h('span.hint', t('live.dailyHint'))),
    h('div.field', h('label', { for: 'bMoneyDay' }, t('live.moneyDay')), mDay, h('span.hint', t('live.spentToday', { usd: usd(cur.spentToday) }))),
    h('div.field', h('label', { for: 'bMoneyMonth' }, t('live.moneyMonth')), mMonth, h('span.hint', t('live.spentMonth', { usd: usd(cur.spentMonth) }))),
    h('p.faint', { style: { fontSize: '12px' } }, t('live.moneyNote')),
    h('div.row', h('button.btn.primary', { type: 'button', onclick: async () => {
      const a = Number(ing.value), b = Number(night.value), c = Number(daily.value), d = Number(mDay.value), m = Number(mMonth.value);
      if (![a, b, c].every((n) => Number.isInteger(n) && n >= 0)) { toast(t('live.wholeNumber')); return; }
      if (![d, m].every((n) => Number.isFinite(n) && n >= 0)) { toast(t('live.usdNumber')); return; }
      try { for (const [k, v] of [['ingestMax', a], ['nightlyMax', b], ['dailyMax', c], ['moneyDaily', d], ['moneyMonthly', m]]) await api.setProjectSetting(k, v); toast(t('settings.saved')); } catch (err) { toast(err.message); }
    } }, t('common.save'))));
}

// Live notices: the Console always; shared projects may also send through Hermes
// (`hermes send`), so hpk never holds a messaging token.
function liveNotify() {
  const p = api.project();
  const box = h('div.stack');
  const local = p.privacy !== 'shared';
  const input = h('input.input#setTo', { placeholder: 'telegram, slack:#builds', list: 'setToList', style: { maxWidth: '260px' }, disabled: local || null });
  const list = h('datalist#setToList');
  api.projectSettings().then((cur) => { input.value = cur.notifyTo || ''; }).catch(() => {});
  if (!local) {
    fetch('/api/notify/targets').then((r) => r.json()).then((r) => list.replaceChildren(...(r.targets || []).map((v) => h('option', { value: v })))).catch(() => {});
  }
  const save = h('button.btn.primary', { type: 'button', disabled: local || null, onclick: async () => {
    try { await api.setProjectSetting('notifyTo', input.value.trim()); toast(t('settings.saved')); } catch (err) { toast(err.message); }
  } }, t('common.save'));
  const test = h('button.btn', { type: 'button', disabled: local || null, onclick: async () => {
    test.disabled = true;
    try {
      const r = await fetch('/api/notify/test', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify({ project: p.id }) });
      const j = await r.json().catch(() => ({}));
      toast(r.ok ? t('live.notifySent') : (j.error || r.statusText));
    } finally { test.disabled = false; }
  } }, t('live.notifyTest'));
  box.append(
    h('div.field', h('label', { for: 'setTo' }, t('live.notifyTo')), input, list,
      h('span.hint', local ? t('live.notifyLocal') : t('live.notifyHint'))),
    h('div.row', save, test),
    h('p.muted', { style: { fontSize: '13px' } }, t('live.batchNote')));
  return box;
}

// "Install as app": the browser's own offer when it gives one, otherwise how
// to do it in this browser. The app is the same Console in its own window.
function appRow() {
  const box = h('div.field', h('label', t('pwa.label')));
  const body = h('div.stack', { style: { gap: '6px' } });
  const draw = () => {
    if (installed()) body.replaceChildren(h('span.tag.clear', icon('check'), t('pwa.installed')));
    else if (canPrompt()) {
      body.replaceChildren(h('div.row', h('button.btn', { type: 'button', onclick: async () => {
        const r = await promptInstall();
        toast(t(r === 'accepted' ? 'pwa.done' : 'pwa.later'));
      } }, icon('plus'), t('pwa.install'))), h('span.hint', t('pwa.why')));
    } else body.replaceChildren(h('span.hint', t('pwa.hint.' + browserHint())));
  };
  draw();
  const off = onInstallChange(() => { if (!box.isConnected) off(); else draw(); });
  box.append(body);
  return box;
}

// Start at login: per user, the one OS-specific part of hpk (docs/install.md).
function loginRow() {
  const input = h('input', { type: 'checkbox', disabled: true });
  const hint = h('span.hint', t('login.hint'));
  const label = h('label.switch', input, t('login.label'));
  fetch('/api/autostart').then((r) => r.json()).then((st) => {
    if (!st.supported) { hint.textContent = t('login.unsupported', { os: st.os || '?' }); return; }
    input.checked = !!st.enabled;
    input.disabled = false;
    hint.textContent = t('login.how.' + (st.os === 'windows' || st.os === 'darwin' ? st.os : 'linux'));
  }).catch(() => { hint.textContent = t('login.unavailable'); });
  input.addEventListener('change', async () => {
    input.disabled = true;
    try {
      const r = await fetch('/api/autostart', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify({ on: input.checked }) });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(j.error || r.statusText);
      input.checked = !!j.enabled;
      toast(t(j.enabled ? 'login.on' : 'login.off'));
    } catch (e) { input.checked = !input.checked; toast(e.message, null, 'deny'); } finally { input.disabled = false; }
  });
  return h('div.field', label, hint);
}

// Desktop notices on this computer (Web Notifications API), opt-in, with quiet hours.
function deskNotices() {
  const box = h('div.stack', { style: { gap: '10px', paddingBottom: '12px', borderBottom: '1px solid var(--seam)' } });
  const draw = () => {
    const cfg = getNotify();
    const save = (fn) => { const c = getNotify(); fn(c); setNotify(c); draw(); };
    if (!noticesSupported()) { box.replaceChildren(h('b', t('desk.title')), h('p.muted', t('desk.unsupported'))); return; }
    const perm = permission();
    const on = h('input', { type: 'checkbox', checked: cfg.on && perm === 'granted' || null, onchange: async (e) => {
      if (e.target.checked) {
        const p = await askPermission();
        if (p !== 'granted') { toast(t('desk.denied'), null, 'deny'); draw(); return; }
      }
      save((c) => { c.on = e.target.checked; });
    } });
    const live = cfg.on && perm === 'granted';
    const time = (key) => h('input.input', { type: 'time', value: cfg.quiet[key], style: { maxWidth: '130px' }, disabled: !cfg.quiet.on || null,
      'aria-label': t('desk.quiet' + (key === 'from' ? 'From' : 'To')), onchange: (e) => save((c) => { c.quiet[key] = e.target.value; }) });
    box.replaceChildren(
      h('b', t('desk.title')),
      h('label.switch', on, t('desk.enable')),
      perm === 'denied' ? h('p.faint', { style: { fontSize: '13px' } }, t('desk.blocked')) : h('span.hint', t('desk.hint')),
      live ? h('div.stack', { style: { gap: '8px' } },
        h('div.row', { style: { flexWrap: 'wrap', gap: '14px' } }, KINDS.map((k) => h('label.switch', h('input', { type: 'checkbox', checked: cfg.kinds[k] || null, onchange: (e) => save((c) => { c.kinds[k] = e.target.checked; }) }), t('desk.kind.' + k)))),
        h('label.switch', h('input', { type: 'checkbox', checked: cfg.away || null, onchange: (e) => save((c) => { c.away = e.target.checked; }) }), t('desk.away')),
        h('div.row', { style: { gap: '10px', flexWrap: 'wrap', alignItems: 'center' } },
          h('label.switch', h('input', { type: 'checkbox', checked: cfg.quiet.on || null, onchange: (e) => save((c) => { c.quiet.on = e.target.checked; }) }), t('desk.quiet')),
          time('from'), h('span.muted', '–'), time('to')),
        h('div.row', h('button.btn.sm', { type: 'button', onclick: () => testNotice() }, icon('bell'), t('desk.test')))) : null);
  };
  draw();
  return box;
}

// Where this project's folder and wiki live (solo mode): pick, then confirm the move.
function projectFolders() {
  const box = h('div.stack');
  const p = api.project();
  api.projectSettings().then((cur) => {
    if (!cur.root && !cur.wikiDir) return;
    box.replaceChildren(
      folderRow({ id: 'setRoot', label: t('folders.root.label'), hint: t('folders.root.hint'), value: cur.root, applyLabel: t('folders.root.apply'), onApply: (dir) => api.setProjectRoot(p.id, dir) }),
      folderRow({ id: 'setWiki', label: t('folders.wiki.label'), hint: t('folders.wiki.hint'), value: cur.wikiDir, applyLabel: t('folders.wiki.apply'), onApply: (dir) => api.setWikiDir(p.id, dir) }));
  }).catch(() => {});
  return box;
}
