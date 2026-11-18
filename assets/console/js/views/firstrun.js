// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// First-run setup, before the add-project wizard: is Hermes Agent here, and is
// hpk added to it? When Hermes is missing, hpk offers to install the tested
// version into its own folder (uv + portable Python), only after the person
// agrees with the size and sources on screen. Then the same steps as
// `hpk install --yes`, after showing the change to Hermes' config.yaml.
import { t } from '../i18n.js';
import { h, icon, lamp, toast } from '../ui.js';

const post = async (url, body) => {
  const r = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify(body) });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
};

export async function setupState() {
  const r = await fetch('/api/setup', { cache: 'no-store' });
  if (!r.ok) return null; // a team server, or another machine: nothing to set up here
  return r.json();
}

export const setupDone = (st) => !st || (st.hermes.found && st.hpk.installed);

// firstRun draws the setup and calls onReady once both parts are done.
export function firstRun(initial, onReady) {
  const box = h('div.stack');
  let st = initial;
  let timer = null;
  let agreed = false;

  const reload = async () => {
    try { st = await setupState(); } catch { /* keep the last state */ }
    draw();
  };
  const row = (ok, title, sub) => h('div.row', { style: { gap: '10px', alignItems: 'flex-start' } },
    lamp(ok ? 'clear' : 'caution'), h('div', h('div.cell-title', title), sub ? h('div.cell-sub', sub) : null));

  function hermesPart() {
    const he = st.hermes, job = st.job || {}, o = st.offer;
    if (he.found) {
      return row(true, t('setup.hermesFound', { version: he.version || 'hermes' }), he.managed ? t('setup.managed', { dir: o.dir }) : he.cli);
    }
    if (job.running) {
      clearTimeout(timer);
      timer = setTimeout(reload, 1500);
      return h('div.stack', row(false, t('setup.installing', { step: t('setup.step.' + (job.step || 'start')) })),
        h('pre.setup-log', { 'aria-live': 'polite' }, (job.log || []).slice(-12).join('\n')));
    }
    const offer = h('div.stack', { style: { gap: '10px' } },
      row(false, t('setup.hermesMissing'), t('setup.hermesWhy')),
      h('div.callout.route', icon('info'), h('div.setup-offer',
        h('b', t('setup.offerTitle', { version: o.version })),
        h('div', t('setup.offerBody', { dir: o.dir })),
        h('table.t', h('tbody', o.steps.map((s) => h('tr',
          h('td', h('div.cell-title', s.name), h('div.cell-sub.mono', s.source), s.sha256 ? h('div.cell-sub.mono', 'SHA-256 ' + s.sha256) : null),
          h('td.mono.muted', { style: { whiteSpace: 'nowrap', textAlign: 'right' } }, t('setup.size', { dl: s.downloadMB, disk: s.diskMB })))))),
        h('div', h('b', t('setup.total', { dl: o.downloadMB, disk: o.diskMB }))),
        h('div.faint', t('setup.licence', { licence: o.licence }) + ' ' + o.note))),
      job.error ? h('div.callout.danger', icon('warn'), h('div', t('setup.failed'), h('pre.setup-log', job.error))) : null);
    if (!o.supported) {
      offer.append(h('p.muted', t('setup.unsupported', { platform: o.platform || '?' })));
      return offer;
    }
    const go = h('button.btn.primary', { type: 'button', disabled: !agreed || null, onclick: async () => {
      go.disabled = true;
      try { await post('/api/setup/hermes', { consent: true }); await reload(); } catch (e) { toast(e.message, null, 'deny'); go.disabled = false; }
    } }, icon('play'), t('setup.installBtn', { version: o.version }));
    offer.append(
      h('label.switch', h('input', { type: 'checkbox', checked: agreed || null, onchange: (e) => { agreed = e.target.checked; go.disabled = !agreed; } }), t('setup.agree')),
      h('div.row', { style: { gap: '8px', flexWrap: 'wrap' } }, go, h('span.hint', t('setup.self'))));
    return offer;
  }

  function hpkPart() {
    const p = st.hpk.plan || {};
    if (st.hpk.installed) return row(true, t('setup.hpkIn', { version: st.hpk.version }), p.config);
    if (!st.hermes.found) return row(false, t('setup.hpkWait'));
    const add = h('button.btn.primary', { type: 'button', disabled: !!p.error || null, onclick: async () => {
      add.disabled = true;
      try { await post('/api/setup/install', { consent: true }); toast(t('setup.hpkDone')); await reload(); } catch (e) { toast(e.message, null, 'deny'); add.disabled = false; }
    } }, icon('check'), t('setup.hpkBtn'));
    return h('div.stack', { style: { gap: '8px' } },
      row(false, t('setup.hpkNeeded'), t('setup.hpkWhat', { config: p.config || '' })),
      p.error ? h('div.callout.danger', icon('warn'), h('div', p.error)) : null,
      (p.changes || []).length ? h('ul', p.changes.map((c) => h('li.mono', c))) : null,
      (p.diff || []).length ? h('details', h('summary', t('setup.diff')), h('pre.setup-log', p.diff.join('\n'))) : null,
      h('p.faint', t('setup.hpkNote')),
      h('div.row', add));
  }

  function draw() {
    if (!box.isConnected && box.childElementCount) { clearTimeout(timer); return; }
    const done = setupDone(st);
    box.replaceChildren(
      h('h2', t('setup.title')),
      h('p.muted', t('setup.lede')),
      h('section.panel', h('div.panel-body.stack', h('b', '1 · Hermes Agent'), hermesPart())),
      h('section.panel', h('div.panel-body.stack', h('b', '2 · hpk ↔ Hermes'), hpkPart())),
      h('section.panel', h('div.panel-body.stack', h('b', t('setup.modelTitle')), h('p.muted', t('setup.modelBody')), h('code', 'hermes setup'))),
      h('div.row.between',
        h('span.faint', done ? t('setup.ready') : t('setup.notYet')),
        h('button.btn' + (done ? '.go' : '.ghost'), { type: 'button', onclick: () => { clearTimeout(timer); onReady(); } }, done ? t('setup.continue') : t('setup.skip'), icon('chevron'))));
  }
  draw();
  return box;
}
