// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// AI accounts: several accounts per provider, an explicit order of use, and
// automatic failover to the next account when one runs out (quota, budget,
// errors, bad credentials). Accounts live in Hermes' credential pool; the
// Console only shows a masked fingerprint and never the key itself.
import { api, bus } from '../data.js';
import { isLive } from '../live.js';
import { t } from '../i18n.js';
import { h, icon, lamp, rel, clock, dateTime, pageHead, panel, toast, openDrawer, closeDrawer, fmtTokens, fmtCost, motionOn } from '../ui.js';

const STATUS_LAMP = { active: 'route', ready: 'clear', exhausted: 'danger', error: 'danger', disabled: 'idle' };

function usageText(a) {
  if (a.unit === 'tokens') return t('accounts.usage.tokens', { used: fmtTokens(a.used), limit: fmtTokens(a.limit) });
  if (a.unit === 'percent') return t('accounts.usage.percent', { used: a.used, limit: a.limit });
  return t('accounts.usage.usd', { used: fmtCost(a.used), limit: fmtCost(a.limit) });
}
const pct = (a) => (a.limit ? Math.min(100, Math.round((a.used / a.limit) * 100)) : 0);

// FLIP: remember where rows were, re-render, then slide them from old to new place.
function flip(container, render) {
  const before = new Map([...container.querySelectorAll('[data-acc]')].map((el) => [el.dataset.acc, el.getBoundingClientRect().top]));
  render();
  if (!motionOn()) return;
  container.querySelectorAll('[data-acc]').forEach((el) => {
    const old = before.get(el.dataset.acc);
    if (old === undefined) return;
    const dy = old - el.getBoundingClientRect().top;
    if (!dy) return;
    el.animate([{ transform: `translateY(${dy}px)` }, { transform: 'none' }], { duration: 380, easing: 'cubic-bezier(0.16, 1, 0.3, 1)' });
  });
}

export default async function accounts() {
  if (isLive()) return liveAccounts();
  await api.accounts();
  const policy = api.failoverPolicy();
  const body = h('div.stack');
  const logBox = h('tbody');
  let dragId = null;

  function providerPanel(p) {
    const list = api.accountsSync().filter((a) => a.provider === p.id).sort((x, y) => x.order - y.order);
    // In use = the active account, or (if none is active yet) the first ready one in order.
    const inUse = list.find((a) => a.status === 'active') || list.find((a) => a.enabled && a.status === 'ready');
    const ol = h('ol.acc-route', { 'aria-label': t('accounts.orderAria', { provider: p.name }) });
    const draw = () => {
      const rows = api.accountsSync().filter((a) => a.provider === p.id).sort((x, y) => x.order - y.order);
      ol.replaceChildren(...rows.map((a, i) => row(a, i, rows.length, draw)));
    };
    draw();
    const head = h('div.panel-head',
      h('h2', p.name, h('span.tag', t('accounts.countN', { n: list.length }))),
      h('span.sub', inUse ? h('span.row', { style: { gap: '8px' } }, lamp('route'), t('accounts.inUse', { label: inUse.label })) : list.length ? h('span.status.danger', lamp('danger', { blink: true }), t('accounts.noneUsable')) : null));
    return h('section.panel', { 'data-provider': p.id }, head,
      list.length ? ol : h('div.empty', h('p', t('accounts.emptyProvider', { provider: p.name })), h('button.btn.sm', { type: 'button', onclick: () => addDrawer(p.id) }, icon('plus'), t('accounts.add'))));

    function row(a, i, n, redraw) {
      const k = STATUS_LAMP[a.status];
      const li = h('li.acc-row', { 'data-acc': a.id, class: a.status, draggable: 'true', tabindex: 0, 'aria-label': t('accounts.rowAria', { n: i + 1, label: a.label, status: t('accounts.status.' + a.status) }),
        ondragstart: (e) => { dragId = a.id; li.classList.add('dragging'); e.dataTransfer.effectAllowed = 'move'; },
        ondragend: () => { dragId = null; li.classList.remove('dragging'); ol.querySelectorAll('.drop').forEach((x) => x.classList.remove('drop')); },
        ondragover: (e) => { if (!dragId || dragId === a.id) return; const other = api.accountsSync().find((x) => x.id === dragId); if (other?.provider !== a.provider) return; e.preventDefault(); li.classList.add('drop'); },
        ondragleave: () => li.classList.remove('drop'),
        ondrop: (e) => { e.preventDefault(); li.classList.remove('drop'); const id = dragId; flip(ol, () => { api.placeAccount(id, a.id); redraw(); }); toast(t('accounts.reordered')); },
        onkeydown: (e) => {
          if (e.target !== li) return;
          if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) { e.preventDefault(); move(e.key === 'ArrowUp' ? -1 : 1); }
        } },
      // The route rail: a vertical track joining the accounts in order of use.
      h('span.acc-rail', { 'aria-hidden': 'true' }, h('span.acc-num', i + 1), lamp(k, { blink: a.status === 'exhausted' })),
      h('div.acc-main',
        h('div.acc-title', h('strong', a.label),
          h('span.tag', { class: k === 'idle' ? '' : k }, t('accounts.status.' + a.status)),
          h('span.tag', icon(a.method === 'oauth' ? 'globe' : 'key'), t('accounts.method.' + a.method))),
        h('div.acc-meta', h('code', a.fp), a.lastUsed ? h('span', t('accounts.lastUsed', { when: rel(a.lastUsed) })) : h('span.faint', t('accounts.neverUsed')),
          a.projects.length ? h('span', t('accounts.projectsN', { list: a.projects.join(', ') })) : h('span.faint', t('accounts.noProjects')))),
      h('div.acc-usage',
        h('div.row.between', { style: { fontSize: '12px' } }, h('span.mono', usageText(a)), h('span.faint', a.resetAt ? t('accounts.resets', { when: rel(a.resetAt) }) : t('accounts.window.' + a.window))),
        h('div.meter', { class: pct(a) >= 100 ? 'danger' : pct(a) > 80 ? 'caution' : a.status === 'active' ? '' : 'clear', role: 'meter', 'aria-valuenow': pct(a), 'aria-valuemin': 0, 'aria-valuemax': 100, 'aria-label': t('accounts.usageAria') }, h('i', { style: { width: pct(a) + '%' } })),
        a.resetAt ? h('span.faint', { style: { fontSize: '11px' } }, t('accounts.window.' + a.window)) : null),
      h('div.acc-actions',
        h('button.icon-btn', { type: 'button', 'aria-label': t('accounts.up', { label: a.label }), disabled: i === 0 || null, onclick: () => move(-1) }, icon('up')),
        h('button.icon-btn', { type: 'button', 'aria-label': t('accounts.down', { label: a.label }), disabled: i === n - 1 || null, onclick: () => move(1) }, icon('down')),
        h('label.switch', { title: t('accounts.enabled') }, h('input', { type: 'checkbox', checked: a.enabled || null, 'aria-label': t('accounts.enabledFor', { label: a.label }), onchange: (e) => { api.setAccountEnabled(a.id, e.target.checked); redraw(); drawLog(); } })),
        h('button.btn.sm.danger', { type: 'button', onclick: () => { api.removeAccount(a.id); toast(t('accounts.removed', { label: a.label }), null, 'deny'); render(); } }, t('common.remove'))));
      function move(d) {
        flip(ol, () => { if (api.moveAccount(a.id, d)) redraw(); });
        ol.querySelector(`[data-acc="${a.id}"]`)?.focus();
      }
      return li;
    }
  }

  // Work categories: the agent tags each sub-task (quick, deep, review, design);
  // the category picks the first model and what to fall back to. Accounts above
  // switch first; the chain moves on only when a model is refused or out everywhere.
  function categoriesPanel() {
    const box = h('div.cat-list');
    const MODELS = ['haiku', 'sonnet', 'opus'];
    const EFFORTS = ['low', 'medium', 'high', 'max'];
    const draw = () => box.replaceChildren(...api.categories().map((c) => {
      const used = api.sessionsSync().filter((x) => x.category === c.id && (x.status === 'running' || x.status === 'queued')).length;
      return h('div.cat-row', { 'data-cat': c.id },
        h('div.cat-name', h('strong', t('plan.category.' + c.id)), h('span.sr-only', t('accounts.cat.desc.' + c.id)),
          h('span.tip', { 'data-tip': t('accounts.cat.desc.' + c.id), 'aria-hidden': 'true' }, icon('info')),
          used ? h('span.cat-live', { 'data-tip': t('accounts.cat.inUse', { n: used }) }, used, h('span.sr-only', t('accounts.cat.inUse', { n: used }))) : null),
        h('ol.cat-chain', { 'aria-label': t('accounts.cat.chainAria', { cat: t('plan.category.' + c.id) }) },
          c.chain.map((step, i) => h('li.cat-step',
            h('span.acc-num', i + 1),
            h('select.input.sm', { 'data-glyph': 'model', 'aria-label': t('accounts.cat.model', { n: i + 1 }), onchange: (e) => { api.setChainStep(c.id, i, { model: e.target.value }); toast(t('accounts.cat.saved')); } },
              MODELS.map((m) => h('option', { value: m, selected: m === step.model || null }, m))),
            h('select.input.sm', { 'data-glyph': 'effort', 'aria-label': t('accounts.cat.effort', { n: i + 1 }), onchange: (e) => { api.setChainStep(c.id, i, { effort: e.target.value }); toast(t('accounts.cat.saved')); } },
              EFFORTS.map((m) => h('option', { value: m, selected: m === step.effort || null }, t('chat.effort.' + m)))),
            h('span.cat-acts',
              h('button.icon-btn.sm', { type: 'button', 'aria-label': t('accounts.cat.up', { n: i + 1 }), disabled: i === 0 || null, onclick: () => { api.moveInChain(c.id, i, -1); draw(); } }, icon('up')),
              h('button.icon-btn.sm', { type: 'button', 'aria-label': t('accounts.cat.down', { n: i + 1 }), disabled: i === c.chain.length - 1 || null, onclick: () => { api.moveInChain(c.id, i, 1); draw(); } }, icon('down')),
              h('button.icon-btn.sm', { type: 'button', 'aria-label': t('accounts.cat.remove', { n: i + 1 }), disabled: c.chain.length <= 1 || null, onclick: () => { api.removeFromChain(c.id, i); draw(); } }, icon('x'))),
            i < c.chain.length - 1 ? h('span.cat-then', { 'aria-hidden': 'true' }, icon('chevron')) : null)),
          c.chain.length < MODELS.length ? h('li.cat-add', h('button.btn.sm.ghost', { type: 'button', onclick: () => { api.addToChain(c.id); draw(); } }, icon('plus'), t('accounts.cat.add'))) : null));
    }));
    draw();
    return h('section.panel',
      h('div.panel-head', h('h2', t('accounts.cat.title')), h('span.sub', [t('accounts.cat.sub'), t('accounts.cat.order'), t('accounts.cat.note')].join(' '))),
      h('div.stack', { style: { padding: '14px 16px 16px', gap: '12px' } }, box));
  }

  function policyPanel() {
    const trig = (k) => h('label.switch', h('input', { type: 'checkbox', checked: policy.triggers[k] || null, onchange: (e) => { api.setPolicy({ triggers: { ...policy.triggers, [k]: e.target.checked } }); } }),
      h('span', h('b', { style: { fontWeight: 600 } }, t('accounts.trig.' + k + '.t')), ' — ', h('span.muted', t('accounts.trig.' + k + '.d'))));
    return panel(t('accounts.policyTitle'), t('accounts.policySub'), h('div.stack',
      h('div.stack', { style: { gap: '10px' } }, h('span.label', t('accounts.switchWhen')), ['quota', 'budget', 'errors', 'auth'].map(trig)),
      h('div.field', h('label', { for: 'retTo' }, t('accounts.returnTo')),
        h('select.input#retTo', { style: { maxWidth: '340px' }, onchange: (e) => api.setPolicy({ returnTo: e.target.value }) },
          ['on_reset', 'next_session', 'never'].map((v) => h('option', { value: v, selected: policy.returnTo === v || null }, t('accounts.ret.' + v))))),
      h('label.switch', h('input', { type: 'checkbox', checked: policy.sticky || null, onchange: (e) => api.setPolicy({ sticky: e.target.checked }) }),
        h('span', h('b', { style: { fontWeight: 600 } }, t('accounts.sticky.t')), ' — ', h('span.muted', t('accounts.sticky.d')))),
      h('label.switch', h('input', { type: 'checkbox', checked: policy.crossProvider || null, onchange: (e) => { api.setPolicy({ crossProvider: e.target.checked }); toast(t(e.target.checked ? 'accounts.crossOn' : 'accounts.crossOff')); } }),
        h('span', h('b', { style: { fontWeight: 600 } }, t('accounts.cross.t')), ' ', h('span.tag.danger', icon('warn'), t('redlines.model.short')), ' — ', h('span.muted', t('accounts.cross.d')))),
      h('label.switch', h('input', { type: 'checkbox', checked: policy.notify || null, onchange: (e) => api.setPolicy({ notify: e.target.checked }) }), t('accounts.notify'))));
  }

  async function drawLog() {
    const log = await api.switchLog();
    const acc = (id) => api.accountsSync().find((x) => x.id === id)?.label || '—';
    logBox.replaceChildren(...log.map((e, i) => h('tr', { class: i === 0 && Date.now() - e.at < 4000 ? 'arrive' : '' },
      h('td.tt-time', dateTime(e.at)),
      h('td.tt-lamp', lamp(e.reason === 'reset' ? 'clear' : e.reason === 'manual' ? 'route' : 'caution')),
      h('td', h('strong', { style: { fontWeight: 600 } }, acc(e.from)), ' ', icon('chevron'), ' ', h('strong', { style: { fontWeight: 600 } }, acc(e.to)),
        h('span.muted', ' · ' + api.providers().find((p) => p.id === e.provider).name + ' · ' + t('accounts.reason.' + e.reason))),
      h('td.tt-who.mono', e.session ? api.session(e.session)?.num : ''))));
  }

  function accessMatrix() {
    const projects = ['atlas-web', 'ledger-api', 'harbor-ml'];
    const list = api.accountsSync().slice().sort((x, y) => x.provider.localeCompare(y.provider) || x.order - y.order);
    return h('div.table-wrap', h('table.t',
      h('thead', h('tr', h('th', t('accounts.account')), projects.map((p) => h('th', p)))),
      h('tbody', list.map((a) => h('tr',
        h('td', h('div.cell-title', a.label), h('div.cell-sub', api.providers().find((p) => p.id === a.provider).name)),
        projects.map((p) => h('td', h('input', { type: 'checkbox', checked: a.projects.includes(p) || null, 'aria-label': t('accounts.allowAria', { label: a.label, project: p }),
          onchange: (e) => { a.projects = e.target.checked ? [...a.projects, p] : a.projects.filter((x) => x !== p); toast(t('accounts.accessSaved')); render(); } }))))))));
  }

  function render() {
    body.replaceChildren(
      h('div.callout', icon('lock'), h('div', h('b', t('accounts.safeTitle')), h('p', t('accounts.safeBody')))),
      ...api.providers().map(providerPanel),
      categoriesPanel(),
      h('div.grid.two', policyPanel(),
        panel(t('accounts.logTitle'), t('accounts.logSub'), h('table.timetable', logBox), { flush: true })),
      panel(t('accounts.accessTitle'), t('accounts.accessSub'), accessMatrix(), { flush: true }));
    drawLog();
  }

  function addDrawer(provider = 'anthropic') {
    let method = 'api_key';
    const keyField = h('div.field', h('label', { for: 'accKey' }, t('accounts.form.key')),
      h('input.input#accKey', { type: 'password', autocomplete: 'off', spellcheck: 'false', placeholder: 'sk-…' }),
      h('span.hint', t('accounts.form.keyHint')));
    const oauthNote = h('div.callout.route', { hidden: true }, icon('globe'), h('div', t('accounts.form.oauthNote')));
    const label = h('input.input#accLabel', { placeholder: t('accounts.form.labelPh') });
    openDrawer(t('accounts.add'), h('form.stack', { style: { padding: '16px' }, onsubmit: (e) => { e.preventDefault(); submit(); } },
      h('div.field', h('label', { for: 'accProv' }, t('accounts.form.provider')),
        h('select.input#accProv', api.providers().map((p) => h('option', { value: p.id, selected: p.id === provider || null }, p.name)))),
      h('div.field', h('label', { for: 'accLabel' }, t('accounts.form.label')), label, h('span.hint', t('accounts.form.labelHint'))),
      h('div.field', h('span.label', t('accounts.form.method')),
        h('div.seg', { role: 'group' }, ['api_key', 'oauth', 'hermes'].map((m) => h('button', { type: 'button', 'aria-pressed': m === method ? 'true' : 'false', onclick: (e) => {
          method = m; e.currentTarget.parentElement.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', 'false')); e.currentTarget.setAttribute('aria-pressed', 'true');
          keyField.hidden = m !== 'api_key'; oauthNote.hidden = m === 'api_key';
          oauthNote.querySelector('div').textContent = t(m === 'oauth' ? 'accounts.form.oauthNote' : 'accounts.form.hermesNote');
        } }, t('accounts.method.' + m))))),
      keyField, oauthNote,
      h('div.grid.two', { style: { gap: '12px' } },
        h('div.field', h('label', { for: 'accLimit' }, t('accounts.form.limit')), h('input.input#accLimit', { inputmode: 'numeric', placeholder: '5000000' })),
        h('div.field', h('label', { for: 'accUnit' }, t('accounts.form.unit')), h('select.input#accUnit', ['tokens', 'usd', 'percent'].map((u) => h('option', { value: u }, t('accounts.unit.' + u)))))),
      h('div.field', h('label', { for: 'accWin' }, t('accounts.form.window')), h('select.input#accWin', ['5h', 'day', 'week', 'month', 'credits'].map((w) => h('option', { value: w }, t('accounts.window.' + w))))),
      h('p.faint', { style: { fontSize: '12px' } }, t('accounts.form.orderNote'))),
    [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')), h('button.btn.primary', { type: 'button', onclick: submit }, t('accounts.form.save'))]);
    function submit() {
      if (!label.value.trim()) { label.focus(); label.setAttribute('aria-invalid', 'true'); return; }
      const key = document.getElementById('accKey')?.value || '';
      api.addAccount({ provider: document.getElementById('accProv').value, label: label.value.trim(), method, key,
        limit: document.getElementById('accLimit').value, unit: document.getElementById('accUnit').value, window: document.getElementById('accWin').value, projects: [api.project().id] });
      // The key goes to Hermes' credential store and is dropped from the page right away.
      const k = document.getElementById('accKey'); if (k) k.value = '';
      closeDrawer(); toast(t('accounts.added', { label: label.value.trim() })); render();
    }
  }

  function testFailover() {
    const p = 'anthropic';
    const before = api.accountInUse(p)?.label;
    const e = api.failover(p, 'quota');
    if (!e) { toast(t('accounts.noneLeft'), null, 'deny'); render(); return; }
    const after = api.accountInUse(p)?.label;
    render();
    toast(t('accounts.switched', { from: before, to: after }));
  }

  const onAcc = () => drawLog();
  bus.addEventListener('accounts', onAcc);
  render();
  return {
    el: h('div.page', pageHead(t('accounts.title'), t('accounts.lede'), [
      h('button.btn', { type: 'button', onclick: testFailover }, icon('play'), t('accounts.test')),
      h('button.btn.primary', { type: 'button', onclick: () => addDrawer() }, icon('plus'), t('accounts.add'))]), body),
    cleanup: () => bus.removeEventListener('accounts', onAcc),
  };
}

// ---- live: Hermes' credential pool, read-only ----
// hpk reads it with `hermes auth list`, which never prints a key. Adding, removing
// and reordering accounts stays in Hermes, so hpk never handles a secret.
async function liveAccounts() {
  const res = await api.credentials();
  const manage = !!res.canManage;
  const post = async (body) => {
    const r = await fetch('/api/accounts', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify(body) });
    const out = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(out.error || r.statusText);
    return out;
  };
  const redraw = async () => { const page = await liveAccounts(); document.querySelector('#main .page').replaceWith(page.el); };
  const act = async (btn, body, done) => {
    btn.disabled = true;
    try { await post(body); toast(done); await redraw(); } catch (err) { toast(err.message); btn.disabled = false; }
  };
  const by = {};
  (res.credentials || []).forEach((c) => (by[c.provider] = by[c.provider] || []).push(c));
  const groups = Object.entries(by).map(([prov, list]) => {
    list.sort((a, b) => a.priority - b.priority);
    return panel(prov, t('live.acc.count', { n: list.length }),
      h('div.table-wrap', h('table.t',
        h('thead', h('tr', h('th', t('live.acc.order')), h('th', t('live.acc.label')), h('th.hide-md', t('live.acc.type')), h('th.hide-md', t('live.acc.source')), h('th', t('live.acc.status')), manage ? h('th') : null)),
        h('tbody', list.map((c, i) => h('tr',
          h('td.num', String(c.priority)),
          h('td', h('div.cell-title', c.label), h('div.cell-sub.mono', 'id ' + c.id)),
          h('td.hide-md', h('span.tag', c.type)),
          h('td.hide-md.mono.muted', c.source || '—'),
          h('td', c.status ? h('span.status.caution', lamp('caution'), c.status) : c.current ? h('span.status.clear', lamp('clear'), t('live.acc.next')) : h('span.status', lamp('idle'), t('live.acc.ready'))),
          manage ? h('td', { style: { textAlign: 'right', whiteSpace: 'nowrap' } },
            i > 0 ? h('button.btn.sm.ghost', { type: 'button', title: t('live.acc.up'), 'aria-label': t('live.acc.up'), onclick: (e) => act(e.currentTarget, { action: 'priority', provider: prov, target: c.id, priority: list[i - 1].priority }, t('live.acc.moved')) }, icon('up')) : null,
            c.status ? h('button.btn.sm.ghost', { type: 'button', onclick: (e) => act(e.currentTarget, { action: 'reset', provider: prov, target: c.id }, t('live.acc.resetDone')) }, t('live.acc.resetOne')) : null,
            h('button.btn.sm.danger', { type: 'button', onclick: (e) => {
              if (!confirm(t('live.acc.removeConfirm', { label: c.label, provider: prov }))) return;
              act(e.currentTarget, { action: 'remove', provider: prov, target: c.id }, t('live.acc.removed'));
            } }, t('live.acc.removeOne'))) : null))))), { flush: true });
  });

  const providers = ['anthropic', 'openrouter', 'openai', 'gemini', 'deepseek', 'xai', 'mistral', 'groq', 'nvidia', 'huggingface', 'zai', 'kimi-coding', 'minimax', 'lmstudio', ...Object.keys(by)];
  const provIn = h('input.input#accProv', { list: 'accProvList', placeholder: 'anthropic', autocomplete: 'off' });
  const labelIn = h('input.input#accLabel', { autocomplete: 'off', placeholder: t('live.acc.labelPh') });
  const keyIn = h('input.input#accKey', { type: 'password', autocomplete: 'off', spellcheck: 'false', placeholder: 'sk-…' });
  const firstIn = h('input', { type: 'checkbox' });
  const addBtn = h('button.btn.primary', { type: 'button', onclick: async () => {
    const provider = provIn.value.trim().toLowerCase();
    if (!provider || !keyIn.value.trim()) { toast(t('live.acc.needBoth')); return; }
    addBtn.disabled = true;
    try {
      await post({ action: 'add', provider, label: labelIn.value.trim(), key: keyIn.value, priority: firstIn.checked ? 0 : undefined });
      keyIn.value = '';
      toast(t('live.acc.added', { provider }));
      await redraw();
    } catch (err) { toast(err.message); } finally { keyIn.value = ''; addBtn.disabled = false; }
  } }, icon('plus'), t('live.acc.addBtn'));
  const addPanel = !manage ? null : res.canAddKeys
    ? panel(t('live.acc.addTitle'), t('live.acc.addSub'), h('div.stack',
      h('div.grid.two',
        h('div.field', h('label', { for: 'accProv' }, t('live.acc.provider')), provIn, h('datalist#accProvList', [...new Set(providers)].map((p) => h('option', { value: p })))),
        h('div.field', h('label', { for: 'accLabel' }, t('live.acc.label')), labelIn)),
      h('div.field', h('label', { for: 'accKey' }, t('live.acc.key')), keyIn, h('span.hint', t('live.acc.keyHint'))),
      h('label.switch', firstIn, t('live.acc.first')),
      h('div.row', addBtn),
      h('p.faint', { style: { fontSize: '12px' } }, t('live.acc.oauthNote'))))
    : panel(t('live.acc.addTitle'), null, h('div.stack', h('p.muted', t('live.acc.windowsNote')), h('code', 'hermes auth add <provider>')));

  return { el: h('div.page',
    pageHead(t('accounts.title'), manage ? t('live.acc.ledeManage') : t('live.acc.lede')),
    res.error ? h('div.callout.danger', icon('warn'), h('div', res.error)) : null,
    groups.length ? h('div.stack', groups) : h('div.callout', icon('info'), h('div', t('live.acc.none'))),
    addPanel,
    manage ? null : panel(t('live.acc.manage'), t('live.acc.manageSub'), h('div.stack',
      [['live.acc.add', 'hermes auth add <provider>'], ['live.acc.order2', 'hermes auth priority <provider> <id> <n>'], ['live.acc.remove', 'hermes auth remove <provider> <id>'], ['live.acc.reset', 'hermes auth reset <provider>']]
        .map(([k, cmd]) => h('div.row.between', h('span', t(k)), h('code', cmd)))))) };
}
