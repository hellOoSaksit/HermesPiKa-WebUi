// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { isLive, teamMode } from '../live.js';
import { setupState, setupDone, firstRun } from './firstrun.js';
import { t, getLang } from '../i18n.js';
import { h, icon, lamp, pageHead, fmtNum, fmtTokens, fmtCost, toast, rel, privacyTag } from '../ui.js';

const STEPS = ['consent', 'setup', 'privacy', 'scan', 'estimate', 'ingest', 'drafts', 'health', 'config'];
const COST_PER_MTOK = 0.8; // illustrative blended price for the estimate

export default async function wizard({ go }) {
  if (isLive()) {
    // First run on a person's own machine: Hermes and hpk-in-Hermes before the first project.
    const st = teamMode() ? null : await setupState().catch(() => null);
    if (!setupDone(st)) {
      const page = h('div.page', pageHead(t('wizard.title'), t('live.wizardLede')));
      page.append(firstRun(st, async () => page.replaceChildren(...(await liveWizard({ go })).el.childNodes)));
      return { el: page };
    }
    return liveWizard({ go });
  }
  const st = { step: 0, path: '~/code/orbit-app', name: 'orbit-app', depth: 'full', kind: 'team', profile: 'senior-coder', members: new Set(['mali']), privacy: 'private', modelPolicy: 'any', scan: null, picked: new Set(), progress: 0, health: new Set(['h-1', 'h-2']) };
  const stepper = h('ol.stepper', { 'aria-label': t('wizard.steps') });
  const body = h('div.panel-body.stack');
  const foot = h('div.panel-body.row.between', { style: { borderTop: '1px solid var(--seam)' } });
  let timer = null;

  function drawStepper() {
    stepper.replaceChildren(...STEPS.map((k, i) => h('li.step', { class: i < st.step ? 'done' : i === st.step ? 'now' : '', 'aria-current': i === st.step ? 'step' : null },
      h('span.s-dot', i < st.step ? icon('check') : i + 1), t('wizard.step.' + k))));
  }
  const nav = (next, opts = {}) => h('div.row', { style: { gap: '8px' } },
    st.step > 0 && !opts.noBack ? h('button.btn.ghost', { type: 'button', onclick: () => { st.step--; draw(); } }, icon('back'), t('common.back')) : null,
    next);
  const nextBtn = (label, onClick, disabled) => h('button.btn.primary', { type: 'button', onclick: onClick || (() => { st.step++; draw(); }), disabled: disabled || null }, label || t('common.next'), icon('chevron'));

  async function draw() {
    clearInterval(timer);
    drawStepper();
    const k = STEPS[st.step];
    foot.replaceChildren();
    if (k === 'consent') {
      const pathIn = h('input.input#wzPath', { value: st.path, oninput: (e) => { st.path = e.target.value; st.name = e.target.value.split(/[\\/]/).filter(Boolean).pop() || ''; } });
      body.replaceChildren(
        h('h2', t('wizard.consent.title')),
        h('div.field', h('label', { for: 'wzPath' }, t('wizard.consent.path')), pathIn, h('span.hint', t('wizard.consent.pathHint'))),
        h('div.choice', { role: 'radiogroup', 'aria-label': t('wizard.consent.title') }, ['full', 'light', 'skip'].map((v) => h('label',
          h('input', { type: 'radio', name: 'depth', checked: st.depth === v || null, onchange: () => (st.depth = v) }),
          h('span.c-title', t(`wizard.depth.${v}.t`)), h('span.c-desc', t(`wizard.depth.${v}.d`))))),
        h('div.callout.route', icon('info'), h('div', t('wizard.consent.note'))));
      foot.append(h('a.btn.ghost', { href: '#/projects' }, t('common.cancel')), nav(nextBtn()));
    }
    if (k === 'setup') {
      const peopleBox = h('div.field');
      const drawPeople = () => {
        peopleBox.hidden = st.kind !== 'team';
        peopleBox.replaceChildren(h('span.label', t('wizard.setup.members')),
          h('div.row', ['mali', 'ken', 'ploy', 'arun'].map((id) => h('label.switch', h('input', { type: 'checkbox', checked: st.members.has(id) || null, disabled: id === 'mali' || null,
            onchange: (e) => (e.target.checked ? st.members.add(id) : st.members.delete(id)) }), api.person(id).name))),
          h('span.hint', t('wizard.setup.membersHint')));
      };
      const profileChoice = h('div.choice', { role: 'radiogroup', 'aria-label': t('wizard.setup.profile') }, api.profiles().map((pr) => {
        const proc = api.processes().find((x) => x.id === pr.process);
        return h('label',
          h('input', { type: 'radio', name: 'prof', checked: st.profile === pr.id || null, onchange: () => (st.profile = pr.id) }),
          h('span.c-title', pr.name, h('span.tag.layer', t('setup.layer.process') + ': ' + proc.name)),
          h('span.c-desc', `${pr.settings.model} · ${t(`mode.${pr.settings.mode || proc.settings.mode}.name`)} · ${t('setup.board.' + proc.settings.board)} — ${proc.desc}`));
      }));
      body.replaceChildren(
        h('h2', t('wizard.setup.title')),
        h('div.choice', { role: 'radiogroup', 'aria-label': t('wizard.setup.kind') }, ['team', 'personal'].map((v) => h('label',
          h('input', { type: 'radio', name: 'kind', checked: st.kind === v || null, onchange: () => { st.kind = v; if (v === 'personal') st.privacy = 'private'; drawPeople(); } }),
          h('span.c-title', icon(v === 'team' ? 'team' : 'lock'), t('setup.kind.' + v)), h('span.c-desc', t('wizard.setup.kindDesc.' + v))))),
        peopleBox,
        h('div.field', h('span.label', t('wizard.setup.profile')), profileChoice, h('span.hint', t('wizard.setup.profileHint'))));
      drawPeople();
      foot.append(h('span'), nav(nextBtn()));
    }
    if (k === 'privacy') {
      body.replaceChildren(
        h('h2', t('wizard.privacy.title')),
        h('div.choice', { role: 'radiogroup' }, ['shared', 'private', 'isolated'].map((v) => h('label',
          h('input', { type: 'radio', name: 'priv', checked: st.privacy === v || null, onchange: () => (st.privacy = v) }),
          h('span.c-title', privacyTag(v)), h('span.c-desc', t(`privacy.${v}.desc`))))),
        h('label.switch', h('input', { type: 'checkbox', checked: st.modelPolicy === 'local_only' || null, onchange: (e) => (st.modelPolicy = e.target.checked ? 'local_only' : 'any') }), t('wizard.privacy.local')),
        h('p.faint', { style: { fontSize: '12px' } }, t('wizard.privacy.change')));
      foot.append(h('span'), nav(st.depth === 'skip'
        ? nextBtn(t('common.next'), () => { st.step = STEPS.indexOf('config'); draw(); })
        : nextBtn(t('wizard.scan.start'), () => { st.step++; draw(); })));
    }
    if (k === 'scan') {
      body.replaceChildren(h('h2', t('wizard.scan.title')), h('div.row', lamp('route', { blink: true }), h('span.muted', t('wizard.scan.running', { path: st.path }))), h('div.meter', h('i', { style: { width: '35%' } })));
      foot.append(h('span'), nav(h('span')));
      if (!st.scan) { st.scan = await api.scanProject(st.path); st.scan.sources.forEach((s) => s.pick && st.picked.add(s.path)); }
      if (STEPS[st.step] !== 'scan') return;
      const sc = st.scan;
      body.replaceChildren(
        h('h2', t('wizard.scan.title')),
        h('div.callout', icon('check'), h('div', h('b', t('wizard.scan.zero')), h('p', t('wizard.scan.zeroBody')))),
        h('dl.facts',
          h('div.fact', h('dt', t('wizard.scan.files')), h('dd', fmtNum(sc.files))),
          h('div.fact', h('dt', t('wizard.scan.commits')), h('dd', fmtNum(sc.commits))),
          h('div.fact', h('dt', t('wizard.scan.people')), h('dd', sc.contributors)),
          h('div.fact', h('dt', t('wizard.scan.last')), h('dd', rel(sc.lastCommit)))),
        h('div.grid.two',
          h('div.stack', { style: { gap: '8px' } }, h('h3', t('wizard.scan.langs')), sc.languages.map(([l, pct]) => h('div', h('div.row.between', h('span', l), h('span.mono.muted', pct + '%')), h('div.meter', h('i', { style: { width: pct + '%' } }))))),
          h('div.stack', { style: { gap: '8px' } }, h('h3', t('wizard.scan.skipped')), h('p.muted', { style: { fontSize: '13px' } }, t('wizard.scan.skippedBody')), h('div.row', sc.skipped.map((x) => h('code', x))))));
      foot.replaceChildren(h('span'), nav(nextBtn()));
    }
    if (k === 'estimate') {
      const rows = h('tbody');
      const total = h('strong.mono');
      const cost = h('strong.mono');
      const recalc = () => {
        const tok = st.scan.sources.filter((s) => st.picked.has(s.path)).reduce((n, s) => n + s.tokens, 0);
        total.textContent = fmtTokens(tok);
        cost.textContent = fmtCost((tok / 1e6) * COST_PER_MTOK);
        over.hidden = tok <= 400_000;
      };
      const over = h('div.callout.caution', icon('warn'), h('div', t('wizard.estimate.over')));
      rows.replaceChildren(...st.scan.sources.map((s) => h('tr',
        h('td', { style: { width: '32px' } }, h('input', { type: 'checkbox', checked: st.picked.has(s.path) || null, 'aria-label': s.path, onchange: (e) => { e.target.checked ? st.picked.add(s.path) : st.picked.delete(s.path); recalc(); } })),
        h('td', h('code', s.path), s.files ? h('span.faint', ' · ' + t('wizard.estimate.files', { n: s.files })) : null),
        h('td.hide-md', h('span.tag', t('wizard.kind.' + s.kind))),
        h('td.num', fmtTokens(s.tokens)))));
      body.replaceChildren(
        h('h2', t('wizard.estimate.title')),
        h('p.muted', t('wizard.estimate.lede')),
        h('section.panel', h('div.table-wrap', h('table.t', h('thead', h('tr', h('th'), h('th', t('wizard.estimate.source')), h('th.hide-md', t('wizard.estimate.kind')), h('th.num', t('wizard.estimate.tokens')))), rows))),
        h('div.row.between', h('span', t('wizard.estimate.total'), ' ', total, ' · ', t('wizard.estimate.cost'), ' ', cost), h('span.faint', { style: { fontSize: '12px' } }, t('wizard.estimate.models'))),
        over);
      recalc();
      foot.append(h('span'), nav(nextBtn(t('wizard.estimate.go'))));
    }
    if (k === 'ingest') {
      const files = st.scan.sources.filter((s) => st.picked.has(s.path));
      const bar = h('i', { style: { width: '0%' } });
      const list = h('tbody');
      const doneBtn = nextBtn(t('wizard.ingest.review'), null, true);
      body.replaceChildren(h('h2', t('wizard.ingest.title')), h('p.muted', t('wizard.ingest.lede')), h('div.meter', bar), h('section.panel', h('table.t', list)));
      foot.append(h('button.btn.danger', { type: 'button', onclick: () => { clearInterval(timer); toast(t('wizard.ingest.paused')); } }, icon('pause'), t('wizard.ingest.pause')), nav(doneBtn, { noBack: true }));
      let i = 0;
      const drawList = () => list.replaceChildren(...files.map((f, j) => h('tr',
        h('td', { style: { width: '28px' } }, lamp(j < i ? 'clear' : j === i ? 'route' : 'idle', { blink: j === i })),
        h('td', h('code', f.path)),
        h('td.num.muted', j < i ? t('wizard.ingest.notes', { n: Math.round(f.tokens / 3000) + 1 }) : j === i ? t('wizard.ingest.reading') : '—'))));
      drawList();
      timer = setInterval(() => {
        i++; bar.style.width = Math.min(100, (i / files.length) * 100) + '%'; drawList();
        if (i >= files.length) { clearInterval(timer); doneBtn.disabled = false; }
      }, 520);
    }
    if (k === 'drafts') {
      const drafts = [
        ['architecture/overview.md', 'new', 'architecture'], ['architecture/data-flow.md', 'new', 'architecture'], ['glossary/terms.md', 'new', 'glossary'],
        ['decisions/adr-0007-queue.md', 'new', 'decisions'], ['decisions/adr-0009-cache.md', 'new', 'decisions'], ['people/maintainers.md', 'new', 'people'], ['lessons/flaky-e2e.md', 'new', 'lessons'],
      ];
      body.replaceChildren(h('h2', t('wizard.drafts.title', { n: drafts.length })), h('p.muted', t('wizard.drafts.lede')),
        h('section.panel', h('table.t', h('tbody', drafts.map(([p, kind, area], i) => h('tr',
          h('td', { style: { width: '32px' } }, h('input', { type: 'checkbox', checked: true, 'aria-label': p })),
          h('td', h('span.id', i + 1 + '. '), h('code', p)),
          h('td', ['decisions', 'architecture', 'people'].includes(area) ? h('span.tag.danger', icon('lock'), t('wiki.coreArea')) : h('span.tag', t('wiki.section.' + area))),
          h('td', h('span.tag.clear', t('wiki.kind.' + kind)))))))),
        h('p.faint', { style: { fontSize: '12px' } }, t('wizard.drafts.later')));
      foot.append(h('span'), nav(nextBtn(t('wizard.drafts.approve'))));
    }
    if (k === 'health') {
      const findings = [
        ['h-1', 'danger', 'contradiction', 'docs/deploy.md says Postgres 14; docker-compose.yml runs Postgres 16.'],
        ['h-2', 'caution', 'deadRef', 'README.md links to docs/setup-windows.md, which no longer exists.'],
        ['h-3', 'caution', 'duplicateRule', '"Never push on Fridays" appears in CLAUDE.md and notes/team.md with different wording.'],
        ['h-4', 'route', 'missing', 'api/billing has 42 files and no documentation.'],
      ];
      body.replaceChildren(h('h2', t('wizard.health.title')), h('p.muted', t('wizard.health.lede')),
        h('ul.signals', { style: { border: '1px solid var(--seam)', borderRadius: '8px', background: 'var(--panel)' } }, findings.map(([id, lvl, kind, text]) => h('li.signal-item',
          h('input', { type: 'checkbox', checked: st.health.has(id) || null, 'aria-label': text, onchange: (e) => (e.target.checked ? st.health.add(id) : st.health.delete(id)) }),
          h('div.s-title', text),
          h('span.tag', { class: lvl === 'route' ? 'route' : lvl }, t('wizard.health.' + kind))))),
        h('div.callout.caution', icon('warn'), h('div', t('wizard.health.repoNote'))));
      foot.append(h('span'), nav(nextBtn()));
    }
    if (k === 'config') {
      const y = (k2, v, c) => [h('span.k', k2), ': ', v.startsWith('"') ? h('span.s', v) : v, c ? h('span.c', '   # ' + c) : '', '\n'];
      body.replaceChildren(h('h2', t('wizard.config.title')), h('p.muted', t('wizard.config.lede')),
        h('pre.yaml', y('name', st.name), y('kind', st.kind), y('profile', st.profile, t('setup.layer.process') + ': ' + api.processOf(st.profile).name), y('root', st.path),
          st.kind === 'team' ? [h('span.k', 'members'), ': [' + [...st.members].join(', ') + ']\n'] : '',
          h('span.c', '# ' + t('wizard.config.inherits') + '\n'), h('span.k', 'privacy'), ':\n  ', y('mode', st.privacy), '  ', y('model_policy', st.modelPolicy),
          h('span.k', 'overrides'), ':\n  ', h('span.k', 'board'), ':\n    ', y('kind', 'markdown', t('wizard.config.detected')), '    ', y('file', 'BOARD.md'),
          h('span.k', 'checks'), ':\n  ', y('test', '"make test"', t('wizard.config.detected')),
          h('span.k', 'gates'), ':\n  ', y('default_mode', 'ask'), '  ', h('span.k', 'red_lines_extra'), ':\n    - ', h('span.s', '"Never push on Fridays"'), h('span.c', '   # ' + t('wizard.config.fromRepo')), '\n',
          h('span.k', 'delivery'), ': console', st.privacy !== 'shared' ? h('span.c', '   # ' + t('wizard.config.localOnly')) : '', '\n'),
        h('div.callout.route', icon('info'), h('div', t('wizard.config.where', { name: st.name }))));
      foot.append(h('span'), nav(h('button.btn.go', { type: 'button', onclick: () => { toast(t('wizard.config.done', { name: st.name })); go('projects'); } }, icon('check'), t('wizard.config.finish'))));
    }
  }

  await draw();
  return {
    el: h('div.page', pageHead(t('wizard.title'), t('wizard.lede')), stepper, h('section.panel', body, foot)),
    cleanup: () => clearInterval(timer),
  };
}

// ---- live: the real flow (docs/DESIGN.md §8) ----
// Scan and health checks read names, sizes and links only (0 tokens); secret files
// are listed as skipped and never opened. Creating the project writes project.yaml,
// the wiki and the reading queue. Reading the sources with a model is a separate,
// paid step the person starts themselves.
const LIVE_STEPS = ['consent', 'privacy', 'scan', 'estimate', 'health', 'config'];

async function liveWizard({ go }) {
  const st = { step: 0, path: '', name: '', depth: 'light', privacy: 'private', modelPolicy: 'any', scan: null, picked: new Set(), done: null, busy: false };
  const stepper = h('ol.stepper', { 'aria-label': t('wizard.steps') });
  const body = h('div.panel-body.stack');
  const foot = h('div.panel-body.row.between', { style: { borderTop: '1px solid var(--seam)' } });
  const steps = () => (st.depth === 'skip' ? ['consent', 'privacy', 'config'] : LIVE_STEPS);
  const back = () => (st.step > 0 ? h('button.btn.ghost', { type: 'button', onclick: () => { st.step--; draw(); } }, icon('back'), t('common.back')) : h('span'));
  const next = (label, onClick) => h('button.btn.primary', { type: 'button', onclick: onClick || (() => { st.step++; draw(); }) }, label || t('common.next'), icon('chevron'));
  const tokens = () => (st.scan ? st.scan.sources.filter((x) => st.picked.has(x.path)).reduce((n, x) => n + (x.tokens || 0), 0) : 0);

  function drawStepper() {
    stepper.replaceChildren(...steps().map((k, i) => h('li.step', { class: i < st.step ? 'done' : i === st.step ? 'now' : '', 'aria-current': i === st.step ? 'step' : null },
      h('span.s-dot', i < st.step ? icon('check') : i + 1), t('wizard.step.' + k))));
  }

  async function draw() {
    drawStepper();
    const k = steps()[st.step];
    foot.replaceChildren();
    if (st.done) {
      body.replaceChildren(h('h2', t('live.added', { name: st.done.name })),
        h('p.muted', t('live.addedBody')),
        st.depth === 'skip' ? null : h('div.callout.route', icon('info'), h('div', h('b', t('live.ingestTitle')), h('p', t('live.ingestBody')), h('code', st.done.next))));
      foot.append(h('span'), h('button.btn.go', { type: 'button', onclick: () => { api.setProject(st.done.name); go('overview'); } }, icon('check'), t('live.openProject')));
      return;
    }
    if (k === 'consent') {
      const pathIn = h('input.input#wzPath', { value: st.path, placeholder: t('live.pathPh'), oninput: (e) => { st.path = e.target.value; st.scan = null; } });
      body.replaceChildren(
        h('h2', t('wizard.consent.title')),
        h('div.field', h('label', { for: 'wzPath' }, t('wizard.consent.path')), pathIn, h('span.hint', t('live.pathHint'))),
        h('div.choice', { role: 'radiogroup', 'aria-label': t('wizard.consent.title') }, ['full', 'light', 'skip'].map((v) => h('label',
          h('input', { type: 'radio', name: 'depth', checked: st.depth === v || null, onchange: () => { st.depth = v; st.scan = null; drawStepper(); } }),
          h('span.c-title', t(`wizard.depth.${v}.t`)), h('span.c-desc', t(`wizard.depth.${v}.d`))))),
        h('div.callout.route', icon('info'), h('div', t('wizard.consent.note'))));
      foot.append(h('a.btn.ghost', { href: '#/projects' }, t('common.cancel')), next(null, () => { if (!st.path.trim()) { toast(t('live.pathNeeded')); return; } st.step++; draw(); }));
    }
    if (k === 'privacy') {
      body.replaceChildren(
        h('h2', t('wizard.privacy.title')),
        h('div.choice', { role: 'radiogroup' }, ['shared', 'private', 'isolated'].map((v) => h('label',
          h('input', { type: 'radio', name: 'priv', checked: st.privacy === v || null, onchange: () => (st.privacy = v) }),
          h('span.c-title', privacyTag(v)), h('span.c-desc', t(`privacy.${v}.desc`))))),
        h('label.switch', h('input', { type: 'checkbox', checked: st.modelPolicy === 'local_only' || null, onchange: (e) => (st.modelPolicy = e.target.checked ? 'local_only' : 'any') }), t('wizard.privacy.local')),
        h('p.faint', { style: { fontSize: '12px' } }, t('live.localOnlyNote')),
        h('p.faint', { style: { fontSize: '12px' } }, t('wizard.privacy.change')));
      foot.append(back(), next(st.depth === 'skip' ? null : t('wizard.scan.start')));
    }
    if (k === 'scan') {
      foot.append(back(), h('span'));
      if (!st.scan) {
        body.replaceChildren(h('h2', t('wizard.scan.title')), h('div.row', lamp('route', { blink: true }), h('span.muted', t('wizard.scan.running', { path: st.path }))));
        try {
          st.scan = await api.scanProject(st.path, st.depth);
        } catch (err) {
          body.replaceChildren(h('h2', t('wizard.scan.title')), h('div.callout.danger', icon('warn'), h('div', err.message)));
          return;
        }
        st.name = st.name || st.scan.name;
        st.picked = new Set(st.scan.sources.filter((x) => x.pick).map((x) => x.path));
        if (steps()[st.step] !== 'scan') return;
      }
      const sc = st.scan;
      body.replaceChildren(
        h('h2', t('wizard.scan.title')),
        h('div.callout', icon('check'), h('div', h('b', t('wizard.scan.zero')), h('p', t('wizard.scan.zeroBody')))),
        h('dl.facts',
          h('div.fact', h('dt', t('wizard.scan.files')), h('dd', fmtNum(sc.files))),
          h('div.fact', h('dt', t('wizard.scan.commits')), h('dd', sc.isRepo ? fmtNum(sc.commits) : '—')),
          h('div.fact', h('dt', t('wizard.scan.people')), h('dd', sc.isRepo ? String(sc.contributors) : '—')),
          h('div.fact', h('dt', t('wizard.scan.last')), h('dd', sc.lastCommit ? rel(sc.lastCommit) : '—'))),
        h('div.grid.two',
          h('div.stack', { style: { gap: '8px' } }, h('h3', t('wizard.scan.langs')), sc.languages.length ? sc.languages.map(([l, pct]) => h('div', h('div.row.between', h('span', l), h('span.mono.muted', pct + '%')), h('div.meter', h('i', { style: { width: pct + '%' } })))) : h('p.faint', '—')),
          h('div.stack', { style: { gap: '8px' } }, h('h3', t('wizard.scan.skipped')), h('p.muted', { style: { fontSize: '13px' } }, t('wizard.scan.skippedBody')),
            h('div.row', sc.skipped.length ? sc.skipped.slice(0, 24).map((x) => h('code', x)) : h('span.faint', '—')))));
      foot.replaceChildren(back(), next());
    }
    if (k === 'estimate') {
      const total = h('strong.mono');
      const cost = h('strong.mono');
      const over = h('div.callout.caution', icon('warn'), h('div', t('wizard.estimate.over')));
      const recalc = () => {
        const tok = tokens();
        total.textContent = fmtTokens(tok);
        cost.textContent = fmtCost((tok / 1e6) * st.scan.pricePerMTok);
        over.hidden = tok <= st.scan.budget;
      };
      const rows = st.scan.sources.map((x) => h('tr',
        h('td', { style: { width: '32px' } }, h('input', { type: 'checkbox', checked: st.picked.has(x.path) || null, 'aria-label': x.path, onchange: (e) => { e.target.checked ? st.picked.add(x.path) : st.picked.delete(x.path); recalc(); } })),
        h('td', h('code', x.path)),
        h('td.hide-md', h('span.tag', t('live.kind.' + x.kind))),
        h('td.num', fmtTokens(x.tokens || 0))));
      body.replaceChildren(
        h('h2', t('wizard.estimate.title')),
        h('p.muted', t('wizard.estimate.lede')),
        rows.length ? h('section.panel', h('div.table-wrap', h('table.t', h('thead', h('tr', h('th'), h('th', t('wizard.estimate.source')), h('th.hide-md', t('wizard.estimate.kind')), h('th.num', t('wizard.estimate.tokens')))), h('tbody', rows))))
          : h('p.faint', t('live.noSources')),
        h('div.row.between', h('span', t('wizard.estimate.total'), ' ', total, ' · ', t('wizard.estimate.cost'), ' ', cost), h('span.faint', { style: { fontSize: '12px' } }, t('live.priceNote', { price: st.scan.pricePerMTok }))),
        over);
      recalc();
      foot.append(back(), next(null, () => { if (tokens() > st.scan.budget) { toast(t('wizard.estimate.over')); return; } st.step++; draw(); }));
    }
    if (k === 'health') {
      const list = st.scan.health || [];
      body.replaceChildren(h('h2', t('wizard.health.title')), h('p.muted', t('live.healthLede')),
        list.length ? h('ul.signals', { style: { border: '1px solid var(--seam)', borderRadius: '8px', background: 'var(--panel)' } }, list.map((f) => h('li.signal-item',
          h('div.s-title', f.detail),
          h('span.tag', { class: f.kind === 'no-readme' || f.kind === 'undocumented' ? 'route' : 'caution' }, t('live.health.' + f.kind)),
          f.file ? h('div.s-meta', h('code', f.file + (f.line ? ':' + f.line : ''))) : null)))
          : h('div.callout', icon('check'), h('div', t('live.healthClean'))),
        h('div.callout.caution', icon('warn'), h('div', t('wizard.health.repoNote'))));
      foot.append(back(), next());
    }
    if (k === 'config') {
      if (!st.name) st.name = (st.path.split(/[\\/]/).filter(Boolean).pop() || '').toLowerCase();
      const nameIn = h('input.input#wzName', { value: st.name, oninput: (e) => (st.name = e.target.value) });
      const y = (k2, v, c) => [h('span.k', k2), ': ', String(v), c ? h('span.c', '   # ' + c) : '', '\n'];
      const sc = st.scan;
      body.replaceChildren(h('h2', t('wizard.config.title')), h('p.muted', t('live.configLede')),
        h('div.field', h('label', { for: 'wzName' }, t('live.name')), nameIn, h('span.hint', t('live.nameHint'))),
        h('pre.yaml', y('root', sc ? sc.path : st.path), h('span.k', 'privacy'), ':\n  ', y('mode', st.privacy), '  ', y('model_policy', st.modelPolicy),
          h('span.k', 'board'), ':\n  ', y('kind', sc && sc.boardFile ? 'markdown' : 'hpk', sc && sc.boardFile ? t('wizard.config.detected') : ''),
          sc && sc.testCmd ? [h('span.k', 'checks'), ':\n  ', y('test', sc.testCmd, t('wizard.config.detected'))] : '',
          h('span.k', 'gates'), ':\n  ', y('default_mode', 'ask'),
          h('span.k', 'ingest'), ':\n  ', y('depth', st.depth),
          h('span.k', 'delivery'), ': console', st.privacy !== 'shared' ? h('span.c', '   # ' + t('wizard.config.localOnly')) : '', '\n'),
        h('div.callout.route', icon('info'), h('div', t('live.configWhere'))));
      const create = h('button.btn.go', { type: 'button', onclick: async () => {
        if (st.busy) return;
        st.busy = true; create.disabled = true;
        try {
          const exclude = sc ? sc.sources.filter((x) => !st.picked.has(x.path)).map((x) => x.path) : [];
          st.done = await api.addProject({ path: st.path, name: st.name.trim(), depth: st.depth, privacy: st.privacy, modelPolicy: st.modelPolicy, exclude, language: getLang() });
          toast(t('wizard.config.done', { name: st.done.name }));
          draw();
        } catch (err) { toast(err.message); } finally { st.busy = false; create.disabled = false; }
      } }, icon('check'), t('wizard.config.finish'));
      foot.append(back(), create);
    }
  }

  await draw();
  return { el: h('div.page', pageHead(t('wizard.title'), t('live.wizardLede')), stepper, h('section.panel', body, foot)) };
}
