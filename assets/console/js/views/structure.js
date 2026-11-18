// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Process → Profile → Project.
// A Process is a reusable way of working; a Profile is an agent built on one
// Process; a Project uses one Profile and is either personal or a team's.
// Settings flow down and the lower layer wins; red lines only ever add up.
import { api, SETTING_KEYS } from '../data.js';
import { isLive } from '../live.js';

// Settings the Console can change in project.yaml (live). Model stays out: changing it is a red line.
const LIVE_EDITABLE = new Set(['mode', 'board', 'language']);
import { t } from '../i18n.js';
import { h, icon, lamp, pageHead, panel, toast, privacyTag, avatar, fmtCost } from '../ui.js';

// Layer tags are neutral (lamp colours mean status, not layers); depth is shown by the bar count.
const LAYER_DEPTH = { process: 1, profile: 2, project: 3, builtin: 0 };
const layerTag = (layer) => h('span.tag.layer', { class: 'l-' + layer },
  h('span.depth', { 'aria-hidden': 'true' }, [1, 2, 3].map((n) => h('i', { class: n <= LAYER_DEPTH[layer] ? 'on' : '' }))), t('setup.layer.' + layer));
const kindTag = (kind) => h('span.tag', { class: kind === 'team' ? 'route' : '' }, icon(kind === 'team' ? 'team' : 'lock'), t('setup.kind.' + kind));

function fmt(key, v) {
  if (v === undefined || v === null) return h('span.faint', '—');
  if (key === 'mode') return t(`mode.${v}.name`);
  if (key === 'autoMax') return t('setup.minutes', { n: v });
  if (key === 'board') return t('setup.board.' + v);
  if (key === 'jobs') return v.length ? v.map((j) => t('jobs.name.' + j)).join(', ') : t('setup.none');
  if (key === 'wikiReview') return t('setup.wikiReview.' + v);
  if (key === 'budgetDaily') return v === 0 ? t('setup.localOnlyBudget') : fmtCost(v) + t('setup.perDay');
  if (key === 'accounts') return v.length ? v.map((id) => api.accountsSync().find((a) => a.id === id)?.label || id).join(', ') : t('setup.noAccounts');
  if (key === 'redLines') return v.length ? v.length + ' ' + t('setup.extraLines') : t('setup.builtInOnly');
  if (key === 'language') return v === 'th' ? 'ไทย' : 'English';
  return String(v);
}

// Inline editors for project-level overrides of single values.
const EDITORS = {
  mode: () => ['manual', 'ask', 'auto'].map((v) => [v, t(`mode.${v}.name`)]),
  board: () => (isLive() ? ['hpk', 'markdown'] : ['hpk', 'markdown', 'hermes-kanban', 'none']).map((v) => [v, t('setup.board.' + v)]),
  wikiReview: () => ['core', 'all'].map((v) => [v, t('setup.wikiReview.' + v)]),
  language: () => [['en', 'English'], ['th', 'ไทย']],
  model: () => ['haiku', 'sonnet', 'opus', 'local:qwen-14b'].map((v) => [v, v]),
  autoMax: 'number', maxChildren: 'number', budgetDaily: 'number', titleTemplate: 'text',
};

export default async function structure({ params, go }) {
  if (isLive()) await api.loadStructure();
  let tab = params.tab || 'map';
  const tabs = h('div.tabs', { role: 'tablist' });
  const body = h('div');
  const TABS = ['map', 'processes', 'profiles', 'effective'];
  const draw = () => {
    tabs.replaceChildren(...TABS.map((k) => h('button', { role: 'tab', type: 'button', 'aria-selected': k === tab ? 'true' : 'false', onclick: () => go(k === 'map' ? 'setup' : 'setup/' + k) }, t('setup.tab.' + k))));
    body.replaceChildren(tab === 'map' ? mapView() : tab === 'processes' ? layerList('process') : tab === 'profiles' ? layerList('profile') : effectiveView());
  };
  draw();
  const liveNote = isLive() ? h('div.callout.route', icon('info'), h('div', t('live.setupNote'))) : null;
  return { el: h('div.page', pageHead(t('setup.title'), t('setup.lede')), liveNote, chainExplainer(), tabs, body) };

  function chainExplainer() {
    const step = (layer, what) => h('div.chain-step', h('div.row', { style: { gap: '8px' } }, layerTag(layer)), h('p.muted', what));
    return h('div.chain', step('process', t('setup.explain.process')), h('span.chain-arrow', icon('chevron')), step('profile', t('setup.explain.profile')), h('span.chain-arrow', icon('chevron')), step('project', t('setup.explain.project')),
      h('p.chain-rule', icon('info'), t('setup.explain.rule')));
  }

  // ---- Map: every process, its profiles, their projects ----
  function mapView() {
    const projectNode = (p) => h('li', h('a.node', { href: '#/setup/effective', onclick: () => api.setProject(p.id) },
      layerTag('project'), lamp(p.sessionsActive ? 'clear' : 'idle'), h('span.n-title', p.name), kindTag(p.kind), h('span.hide-sm', privacyTag(p.privacy))));
    const profileNode = (pr) => {
      const ps = api.projectsSync().filter((p) => p.profile === pr.id);
      return h('li', h('div.node', layerTag('profile'), h('span'), h('span.n-title', pr.name), h('span.n-meta', pr.settings.model), h('span.n-meta.hide-sm', t('setup.projectsN', { n: ps.length }))),
        ps.length ? h('ul', ps.map(projectNode)) : null);
    };
    const groups = api.processes().map((proc) => {
      const prs = api.profiles().filter((p) => p.process === proc.id);
      return h('section.panel', h('div.panel-body',
        h('ul.tree', h('li', h('div.node', layerTag('process'), h('span'), h('span.n-title', proc.name), h('span.n-meta.hide-sm', t(`mode.${proc.settings.mode}.name`) + ' · ' + t('setup.board.' + proc.settings.board)), h('span.n-meta', t('setup.profilesN', { n: prs.length }))),
          prs.length ? h('ul', prs.map(profileNode)) : h('p.faint', { style: { padding: '6px 0 0 24px', fontSize: '12px' } }, t('setup.unusedProcess'))))));
    });
    const personal = api.projectsSync().filter((p) => p.kind === 'personal').length;
    const team = api.projectsSync().length - personal;
    return h('div.stack',
      h('dl.facts', h('div.fact', h('dt', t('setup.layer.process')), h('dd', api.processes().length)), h('div.fact', h('dt', t('setup.layer.profile')), h('dd', api.profiles().length)),
        h('div.fact', h('dt', t('setup.kind.team')), h('dd', team)), h('div.fact', h('dt', t('setup.kind.personal')), h('dd', personal))),
      ...groups);
  }

  // ---- Processes / Profiles ----
  function layerList(layer) {
    const items = layer === 'process' ? api.processes() : api.profiles();
    let sel = items[0].id;
    const nav = h('nav.wiki-nav');
    const detail = h('div.panel-body.stack');
    const drawNav = () => nav.replaceChildren(...items.map((x) => h('button', { type: 'button', 'aria-current': x.id === sel ? 'true' : null, onclick: () => { sel = x.id; drawNav(); drawDetail(); } },
      h('span', x.name), h('span.faint.mono', { style: { fontSize: '11px' } }, layer === 'process' ? api.profiles().filter((p) => p.process === x.id).length : api.projectsSync().filter((p) => p.profile === x.id).length))));
    const drawDetail = () => {
      const x = items.find((i) => i.id === sel);
      const keys = SETTING_KEYS.filter(([, layers]) => layers.includes(layer)).map(([k]) => k);
      const usedBy = layer === 'process' ? api.profiles().filter((p) => p.process === x.id) : api.projectsSync().filter((p) => p.profile === x.id);
      detail.replaceChildren(
        h('div.row.between', h('div.row', layerTag(layer), h('h2', x.name)),
          isLive() ? null : h('button.btn.sm', { type: 'button', onclick: () => toast(t('setup.duplicated', { name: x.name })) }, icon('plus'), t('setup.duplicate'))),
        x.desc ? h('p.muted', x.desc) : null,
        layer === 'profile' && !isLive() ? h('div.field', h('label', { for: 'profProc' }, t('setup.usesProcess')),
          h('select.input#profProc', { style: { maxWidth: '280px' }, onchange: (e) => { api.setProfileProcess(x.id, e.target.value); toast(t('setup.saved')); drawDetail(); } },
            api.processes().map((p) => h('option', { value: p.id, selected: p.id === x.process || null }, p.name))),
          h('span.hint', t('setup.hermesProfile', { name: x.hermesProfile }))) : null,
        h('table.t', h('tbody', keys.map((k) => h('tr', h('td.muted', { style: { width: '40%' } }, t('setup.key.' + k)), h('td', k === 'redLines' ? (x.settings[k]?.length ? x.settings[k].map((r) => h('div', lamp('danger'), ' ', r)) : h('span.faint', t('setup.builtInOnly'))) : fmt(k, x.settings[k])))))),
        h('div', h('span.label', layer === 'process' ? t('setup.usedByProfiles') : t('setup.usedByProjects')),
          h('div.row', { style: { marginTop: '6px' } }, usedBy.length ? usedBy.map((u) => h('span.tag', u.name, layer === 'profile' ? ' · ' + t('setup.kind.' + u.kind) : '')) : h('span.faint', t('setup.notUsed')))),
        h('div.callout.route', icon('info'), h('div', t(layer === 'process' ? 'setup.processNote' : 'setup.profileNote'))));
    };
    drawNav(); drawDetail();
    return h('section.panel', h('div.grid.side-main', { style: { gap: 0 } }, h('div', { style: { borderRight: '1px solid var(--seam)' } }, nav), detail));
  }

  // ---- Effective settings for one project ----
  function effectiveView() {
    const box = h('div');
    const drawEff = () => {
      const p = api.project();
      const prof = api.profileOf(p.id);
      const proc = api.processOf(prof.id);
      const rows = api.effective(p.id);
      const chainCell = (r, layer) => {
        if (!r.layers.includes(layer)) return h('td.faint.hide-md', '·');
        const v = r.chain[layer];
        const winning = r.key !== 'redLines' && r.from === layer;
        const overridden = r.key !== 'redLines' && v !== undefined && !winning;
        return h('td.hide-md', { class: overridden ? 'overridden' : winning ? 'winning' : '' }, v === undefined ? h('span.faint', '—') : fmt(r.key, v));
      };
      const editor = (r) => {
        const kind = isLive() && !LIVE_EDITABLE.has(r.key) ? null : EDITORS[r.key];
        if (!kind || !r.layers.includes('project')) return r.key === 'redLines' ? h('a.btn.sm.ghost', { href: '#/gates' }, t('setup.editRedLines')) : null;
        const hasOverride = r.chain.project !== undefined;
        const ctl = typeof kind === 'function'
          ? h('select.input', { style: { width: '150px', height: '28px' }, 'aria-label': t('setup.key.' + r.key) }, kind().map(([v, label]) => h('option', { value: v, selected: v === r.value || null }, label)))
          : h('input.input', { type: kind, value: r.value ?? '', style: { width: kind === 'text' ? '180px' : '90px', height: '28px' }, 'aria-label': t('setup.key.' + r.key) });
        return h('div.row', { style: { gap: '6px', flexWrap: 'nowrap' } }, ctl,
          h('button.btn.sm', { type: 'button', onclick: async () => { const v = kind === 'number' ? Number(ctl.value) : ctl.value; try { await api.setOverride(p.id, r.key, v); toast(t('setup.overridden', { key: t('setup.key.' + r.key) })); } catch (err) { toast(err.message); } drawEff(); } }, t('setup.override')),
          hasOverride ? h('button.btn.sm.ghost', { type: 'button', onclick: async () => { try { await api.setOverride(p.id, r.key, undefined); toast(t('setup.resetDone')); } catch (err) { toast(err.message); } drawEff(); } }, icon('undo'), t('setup.reset')) : null);
      };
      box.replaceChildren(
        h('div.row', { style: { gap: '10px', marginBottom: '12px' } },
          h('label.label', { for: 'effProj' }, t('setup.forProject')),
          h('select.input#effProj', { style: { maxWidth: '240px' }, onchange: (e) => { api.setProject(e.target.value); drawEff(); } },
            api.projectsSync().map((x) => h('option', { value: x.id, selected: x.id === p.id || null }, x.name + ' · ' + t('setup.kind.' + x.kind)))),
          h('span.chain-inline', layerTag('process'), proc.name, icon('chevron'), layerTag('profile'), prof.name, icon('chevron'), layerTag('project'), p.name)),
        h('section.panel', h('div.table-wrap', h('table.t.eff',
          h('thead', h('tr', h('th', t('setup.col.setting')), h('th', t('setup.col.value')), h('th', t('setup.col.from')), h('th.hide-md', proc.name), h('th.hide-md', prof.name), h('th.hide-md', p.name), h('th'))),
          h('tbody', rows.map((r) => h('tr',
            h('td.cell-title', t('setup.key.' + r.key)),
            h('td', r.key === 'redLines' ? (r.items.length ? r.items.map((it) => h('div.rl-item', lamp('danger'), h('span', it.v), layerTag(it.from))) : h('span.faint', t('setup.builtInOnly'))) : h('strong', { style: { fontWeight: 600 } }, fmt(r.key, r.value))),
            h('td', r.key === 'redLines' ? h('span.faint', { style: { fontSize: '12px' } }, t('setup.addsUp')) : layerTag(r.from)),
            chainCell(r, 'process'), chainCell(r, 'profile'), chainCell(r, 'project'),
            h('td', editor(r)))))))),
        h('p.faint', { style: { fontSize: '12px', marginTop: '8px' } }, t('setup.effNote')));
    };
    drawEff();
    return box;
  }
}
