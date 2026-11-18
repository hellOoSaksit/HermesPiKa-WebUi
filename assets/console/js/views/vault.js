// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Personal > Vault: the person's passwords, logins, tokens and .env values.
// The AI only ever sees names ({{secret:name}}); values are typed by hpk on the
// allowed sites, written into .env files on request, or shown here after a
// confirmation. Nothing here goes into the wiki.
import { t } from '../i18n.js';
import { h, s, icon, toast, rel, pageHead, panel, openDrawer, closeDrawer } from '../ui.js';
import { isLive } from '../live.js';
import { api } from '../data.js';

// The vault unlock, held in memory only: lost on reload, never in storage or a cookie.
let unlockToken = '';
const vaultHeaders = () => ({ 'Content-Type': 'application/json', 'X-HPK': '1', ...(unlockToken ? { 'X-HPK-Vault': unlockToken } : {}) });

const KINDS = ['login', 'password', 'token', 'env', 'note'];
const KIND_ICON = { login: 'team', password: 'key', token: 'link', env: 'file', note: 'edit' };

let demo = [
  { name: 'github-login', kind: 'login', hasUser: true, domains: ['github.com'], length: 18, uses: 12, lastUsed: new Date(Date.now() - 3600e3).toISOString(), created: new Date(Date.now() - 9 * 864e5).toISOString() },
  { name: 'figma', kind: 'password', hasUser: false, domains: ['figma.com'], length: 14, uses: 3, lastUsed: new Date(Date.now() - 864e5 * 2).toISOString(), created: new Date(Date.now() - 20 * 864e5).toISOString() },
  { name: 'stripe-test', kind: 'env', envKey: 'STRIPE_SECRET_KEY', project: 'atlas-web', envFile: '.env', length: 32, uses: 0, domains: [], created: new Date(Date.now() - 864e5).toISOString() },
];
async function load() {
  if (!isLive()) return { entries: structuredClone(demo), keeper: 'demo' };
  const r = await fetch('/api/vault', { headers: { Accept: 'application/json', ...(unlockToken ? { 'X-HPK-Vault': unlockToken } : {}) } });
  const out = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(out.error || r.statusText);
  return out;
}
async function op(body) {
  if (!isLive()) {
    await new Promise((r) => setTimeout(r, 300));
    if (body.op === 'put') demo = [...demo.filter((x) => x.name !== body.name), { name: body.name, kind: body.kind, hasUser: !!body.user, domains: body.domains || [], envKey: body.envKey, project: body.project, envFile: body.envFile, length: (body.value || '').length || 10, uses: 0, created: new Date().toISOString() }];
    if (body.op === 'delete') demo = demo.filter((x) => x.name !== body.name);
    if (body.op === 'reveal') return { user: 'demo-user', value: 'demo-value-not-real' };
    if (body.op === 'writeEnv') return { files: ['.env'] };
    return { ok: true };
  }
  const r = await fetch('/api/vault', { method: 'POST', headers: vaultHeaders(), body: JSON.stringify(body) });
  const out = await r.json().catch(() => ({}));
  if (r.status === 403 && out.locked && !body._retry) {
    // needs the PIN: ask, then try once more
    if (await askPin(out.hasPin)) return op({ ...body, _retry: true });
    throw new Error(t('vault.pin.needed'));
  }
  if (!r.ok) throw new Error(out.error || r.statusText);
  return out;
}

// The PIN guards everything that could leak or redirect a secret. It is typed
// here only; the AI never sees it, so a program the AI runs cannot unlock.
function askPin(hasPin) {
  return new Promise((resolve) => {
    let done = false;
    const finish = (v) => { if (done) return; done = true; closeDrawer(); resolve(v); };
    const pin = h('input.input.mono#vtPin', { type: 'password', autocomplete: 'off' });
    const pin2 = h('input.input.mono#vtPin2', { type: 'password', autocomplete: 'off' });
    const err = h('p.vt-pin-err', { 'aria-live': 'polite' });
    const go = async () => {
      if (!hasPin && pin.value !== pin2.value) { err.textContent = t('vault.pin.mismatch'); return; }
      const r = await fetch('/api/vault', { method: 'POST', headers: vaultHeaders(), body: JSON.stringify({ op: hasPin ? 'unlock' : 'setPin', pin: pin.value }) });
      pin.value = ''; pin2.value = '';
      const out = await r.json().catch(() => ({}));
      if (!r.ok) { err.textContent = out.error || r.statusText; pin.focus(); return; }
      unlockToken = out.unlock || '';
      setTimeout(() => { unlockToken = ''; }, 10 * 60 * 1000);
      toast(t(hasPin ? 'vault.pin.unlocked' : 'vault.pin.set'));
      finish(true);
    };
    openDrawer(t(hasPin ? 'vault.pin.unlockTitle' : 'vault.pin.setTitle'), h('form.stack', { style: { padding: '16px' }, onsubmit: (e) => { e.preventDefault(); go(); } },
      h('div.vt-pin-art', icon('lock')),
      h('p', t(hasPin ? 'vault.pin.unlockBody' : 'vault.pin.setBody')),
      h('div.field', h('label', { for: 'vtPin' }, t('vault.pin.pin')), pin),
      hasPin ? null : h('div.field', h('label', { for: 'vtPin2' }, t('vault.pin.again')), pin2),
      err),
    [h('button.btn.ghost', { type: 'button', onclick: () => finish(false) }, t('common.cancel')), h('button.btn.primary', { type: 'button', onclick: go }, icon('lock'), t(hasPin ? 'vault.pin.unlock' : 'vault.pin.setBtn'))]);
    const d = document.getElementById('drawer');
    const obs = new MutationObserver(() => { if (d.getAttribute('aria-hidden') === 'true') { obs.disconnect(); finish(false); } });
    obs.observe(d, { attributes: true, attributeFilter: ['aria-hidden'] });
    setTimeout(() => pin.focus(), 50);
  });
}

// A row of dots as long as the secret (capped), so nothing of it is shown.
const dots = (n) => h('span.vt-dots', { 'aria-label': t('vault.length', { n }) }, '•'.repeat(Math.min(Math.max(n, 4), 16)));

export default async function vaultView({ rerender }) {
  let st;
  try { st = await load(); } catch (err) { st = { entries: [], error: err.message }; }
  let bst = null;
  if (isLive()) bst = await fetch('/api/browser').then((r) => r.json()).catch(() => null);

  const card = (e) => h('article.vt-card', { tabindex: '0', onclick: () => form(e), onkeydown: (ev) => { if (ev.key === 'Enter') form(e); } },
    h('span.vt-ic', { class: e.kind }, icon(KIND_ICON[e.kind] || 'key')),
    h('div.vt-main',
      h('div.vt-name', h('code', '{{secret:' + e.name + '}}')),
      h('div.vt-val', e.hasUser ? h('span.vt-user', icon('team'), '••••') : null, dots(e.length)),
      h('div.vt-meta',
        e.kind === 'env'
          ? h('span', icon('file'), `${e.project} · ${e.envFile || '.env'} · ${e.envKey}`)
          : (e.domains?.length ? e.domains.map((d) => h('span.vt-site', icon('globe'), d)) : h('span.vt-warn', { 'data-tip': t('vault.noSitesTip') }, icon('warn'), t('vault.noSites'))),
        h('span.vt-uses', { 'data-tip': e.lastUsed ? t('vault.lastUsed', { when: rel(Date.parse(e.lastUsed)) }) : t('vault.neverUsed') }, icon('play'), e.uses || 0))));

  const byKind = KINDS.map((k) => [k, (st.entries || []).filter((e) => e.kind === k)]).filter(([, l]) => l.length);
  const list = (st.entries || []).length
    ? h('div.stack', { style: { gap: '14px' } }, byKind.map(([k, l]) => h('div.vt-group', h('span.label', icon(KIND_ICON[k]), t('vault.kind.' + k), h('span.faint', ` ${l.length}`)), h('div.vt-grid', l.map(card)))))
    : h('div.vt-empty', icon('lock'), h('p', t('vault.none')));

  // .env per project
  const envProjects = [...new Set((st.entries || []).filter((e) => e.kind === 'env').map((e) => e.project))];
  const envPanel = envProjects.length ? panel(t('vault.envTitle'), t('vault.envSub'), h('div.vt-env', envProjects.map((p) =>
    h('div.vt-env-row', icon('folder'), h('b', p), h('span.faint', t('vault.envKeys', { n: (st.entries || []).filter((e) => e.kind === 'env' && e.project === p).length })),
      h('button.btn.sm', { type: 'button', onclick: async (ev) => {
        ev.currentTarget.disabled = true;
        try { const r = await op({ op: 'writeEnv', project: p }); toast(t('vault.envWritten', { files: (r.files || []).join(', '), p })); } catch (err) { toast(err.message, null, 'deny'); }
        ev.currentTarget.disabled = false;
      } }, icon('file'), t('vault.writeEnv')))))) : null;

  // How the AI gets the browser: one MCP, local only
  const connect = bst?.enabled ? h('div.vt-connect',
    h('div', h('b', t('vault.mcpTitle')), h('p.faint', t('vault.mcpBody'))),
    h('button.btn.sm', { type: 'button', onclick: async (ev) => {
      ev.currentTarget.disabled = true;
      try {
        const r = await fetch('/api/mcp', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify({ op: 'add', name: 'hpk-browser', command: bst.hpkPath, args: ['mcp-browser'] }) });
        const out = await r.json().catch(() => ({}));
        if (!r.ok) throw new Error(out.error || r.statusText);
        toast(t('vault.mcpDone', { n: out.result?.tools?.length || 0 }));
      } catch (err) { toast(err.message, null, 'deny'); }
      ev.currentTarget.disabled = false;
    } }, icon('link'), t('vault.mcpConnect'))) : null;

  const lockBtn = isLive() ? h('button.btn.sm', { type: 'button', class: st.unlocked ? 'vt-open' : '', 'data-tip': t(st.unlocked ? 'vault.pin.lockTip' : 'vault.pin.unlockTip'),
    onclick: async () => {
      if (st.unlocked) { await fetch('/api/vault', { method: 'POST', headers: vaultHeaders(), body: JSON.stringify({ op: 'lock' }) }); unlockToken = ''; rerender(); return; }
      if (await askPin(st.hasPin)) rerender();
    } }, icon(st.unlocked ? 'eye' : 'lock'), t(st.unlocked ? 'vault.pin.lock' : st.hasPin ? 'vault.pin.unlock' : 'vault.pin.setBtn')) : null;
  const keeperTag = st.keeper ? h('span.tag', { class: st.keeper === 'file' ? 'caution' : 'clear', 'data-tip': t('vault.keeper.' + st.keeper + 'Tip') }, icon('shield'), t('vault.keeper.' + st.keeper)) : null;

  function form(e) {
    const isNew = !e;
    let kind = e?.kind || 'login';
    const name = h('input.input.mono#vtName', { value: e?.name || '', autocomplete: 'off', spellcheck: 'false', placeholder: 'github-login', readonly: !isNew || null });
    const user = h('input.input#vtUser', { autocomplete: 'off', spellcheck: 'false', placeholder: isNew ? 'name@example.com' : t('vault.keepSame') });
    const value = h('input.input.mono#vtVal', { type: 'password', autocomplete: 'new-password', spellcheck: 'false', placeholder: isNew ? '' : t('vault.keepSame') });
    const show = h('button.icon-btn.sm', { type: 'button', 'aria-label': t('vault.show'), onclick: () => { value.type = value.type === 'password' ? 'text' : 'password'; } }, icon('eye'));
    const sites = h('input.input#vtSites', { value: (e?.domains || []).join(', '), autocomplete: 'off', spellcheck: 'false', placeholder: 'github.com, gitlab.example.com' });
    const envKey = h('input.input.mono#vtEnvKey', { value: e?.envKey || '', autocomplete: 'off', spellcheck: 'false', placeholder: 'API_KEY' });
    const project = h('select.input#vtProj', api.projectsSync().map((p) => h('option', { value: p.id, selected: p.id === (e?.project || api.project().id) || null }, p.name)));
    const envFile = h('input.input.mono#vtEnvFile', { value: e?.envFile || '.env', autocomplete: 'off', spellcheck: 'false' });
    const note = h('input.input#vtNote', { value: e?.note || '', autocomplete: 'off' });
    const fUser = h('div.field', h('label', { for: 'vtUser' }, t('vault.f.user')), user);
    const fSites = h('div.field', h('label', { for: 'vtSites' }, t('vault.f.sites')), sites, h('span.hint', t('vault.f.sitesHint')));
    const fEnv = h('div.grid.two', { style: { gap: '12px' } }, h('div.field', h('label', { for: 'vtEnvKey' }, t('vault.f.envKey')), envKey), h('div.field', h('label', { for: 'vtEnvFile' }, t('vault.f.envFile')), envFile));
    const fProj = h('div.field', h('label', { for: 'vtProj' }, t('vault.f.project')), project);
    const sync = () => { fUser.hidden = kind !== 'login'; fSites.hidden = kind === 'env' || kind === 'note'; fEnv.hidden = fProj.hidden = kind !== 'env'; };
    const seg = h('div.seg.vt-kinds', { role: 'group', 'aria-label': t('vault.f.kind') }, KINDS.map((k) => h('button', { type: 'button', 'aria-pressed': String(k === kind), disabled: (!isNew && k !== kind) || null,
      onclick: (ev) => { kind = k; seg.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', 'false')); ev.currentTarget.setAttribute('aria-pressed', 'true'); sync(); } }, icon(KIND_ICON[k]), t('vault.kind.' + k))));
    const revealBox = h('div.vt-reveal');
    sync();
    openDrawer(isNew ? t('vault.add') : '{{secret:' + e.name + '}}', h('form.stack', { style: { padding: '16px' }, autocomplete: 'off', onsubmit: (ev) => { ev.preventDefault(); save(); } },
      h('div.callout.route', icon('shield'), h('div', h('p', t('vault.aiNever')))),
      seg,
      h('div.field', h('label', { for: 'vtName' }, t('vault.f.name')), name, h('span.hint', t('vault.f.nameHint'))),
      fUser,
      h('div.field', h('label', { for: 'vtVal' }, t('vault.f.value')), h('div.row', { style: { gap: '6px', flexWrap: 'nowrap' } }, value, show)),
      fSites, fProj, fEnv,
      h('div.field', h('label', { for: 'vtNote' }, t('vault.f.note')), note),
      !isNew ? h('div.row', { style: { gap: '8px' } },
        h('button.btn.sm.ghost', { type: 'button', onclick: async () => {
          if (!confirm(t('vault.revealConfirm', { name: e.name }))) return;
          try {
            const r = await op({ op: 'reveal', name: e.name });
            // its own drawer (the PIN prompt may have closed the form); gone after 20 s
            const box = h('div.stack.vt-reveal', { style: { padding: '16px' } },
              ...[r.user ? h('div', h('span.faint', t('vault.f.user') + ': '), h('code', r.user)) : null, h('div', h('span.faint', t('vault.f.value') + ': '), h('code', r.value))].filter(Boolean));
            openDrawer('{{secret:' + e.name + '}}', box);
            setTimeout(() => { box.replaceChildren(); if (box.isConnected) closeDrawer(); }, 20000);
          } catch (err) { toast(err.message, null, 'deny'); }
        } }, icon('eye'), t('vault.reveal')),
        h('button.btn.sm.danger', { type: 'button', onclick: async () => {
          if (!confirm(t('vault.deleteConfirm', { name: e.name }))) return;
          try { await op({ op: 'delete', name: e.name }); closeDrawer(); toast(t('vault.deleted')); rerender(); } catch (err) { toast(err.message, null, 'deny'); }
        } }, icon('x'), t('vault.delete'))) : null,
      revealBox),
    [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')), h('button.btn.primary', { type: 'button', onclick: save }, icon('lock'), t('vault.save'))]);

    async function save() {
      const body = { op: 'put', name: name.value.trim().toLowerCase(), kind, value: value.value, note: note.value.trim() };
      if (kind === 'login') body.user = user.value.trim();
      if (kind !== 'env' && kind !== 'note') body.domains = sites.value.split(/[\s,]+/).filter(Boolean);
      if (kind === 'env') Object.assign(body, { envKey: envKey.value.trim(), project: project.value, envFile: envFile.value.trim() || '.env' });
      if (value.value && value.value.length < 4) toast(t('vault.shortWarn'), null, 'deny');
      try { await op(body); value.value = ''; user.value = ''; closeDrawer(); toast(t('vault.saved', { name: body.name })); rerender(); }
      catch (err) { toast(err.message, null, 'deny'); value.value = ''; }
    }
  }

  return { el: h('div.page',
    pageHead(t('vault.title'), t('vault.lede'), [keeperTag, lockBtn, h('button.btn.primary', { type: 'button', onclick: async () => { if (isLive() && !st.hasPin && !(await askPin(false))) return; if (isLive() && !st.hasPin) { rerender(); } form(null); } }, icon('plus'), t('vault.add'))]),
    st.error ? h('div.callout.danger', icon('warn'), h('div', h('p', st.error))) : null,
    h('div.vt-how', vaultHow()),
    panel(t('vault.listTitle', { n: (st.entries || []).length }), t('vault.listSub'), list),
    envPanel,
    connect ? panel(t('vault.mcpPanel'), null, connect) : null) };
}

// A three-step picture: the AI asks by name → hpk types the value → the AI sees the name again.
function vaultHow() {
  const step = (ic, title, sub) => h('div.vt-how-step', h('span.vt-how-ic', icon(ic)), h('b', title), h('span', sub));
  const arrow = () => s('svg', { class: 'vt-how-arrow', viewBox: '0 0 24 12', 'aria-hidden': 'true' }, s('path', { d: 'M1 6h20M16 1l5 5-5 5' }));
  return [step('sparkle', t('vault.how1'), '{{secret:github-login}}'), arrow(), step('lock', t('vault.how2'), t('vault.how2s')), arrow(), step('globe', t('vault.how3'), t('vault.how3s')), arrow(), step('eye', t('vault.how4'), t('vault.how4s'))];
}
