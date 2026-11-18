// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { isLive } from '../live.js';
import { t } from '../i18n.js';
import { h, icon, lamp, rel, dateTime, pageHead, fmtTokens, fmtCost, toast, panel } from '../ui.js';
import { bgPanel } from './bg.js';

const RUN_LAMP = { ok: 'clear', warn: 'caution', fail: 'danger', none: 'idle' };

export default async function jobs() {
  const list = await api.jobs(); // live: runs come with the jobs
  const runs = await api.runs();
  const rows = h('tbody');
  function draw() {
    rows.replaceChildren(...list.map((j) => {
      const pct = j.budget ? Math.round((j.tokens / j.budget) * 100) : 0;
      const when = isLive()
        ? (!j.installed ? t('live.jobNotInstalled') : j.paused ? t('jobs.paused') + (j.note ? ' · ' + j.note : '') : t('live.jobInstalled'))
        : (j.paused ? t('jobs.paused') : t('jobs.next', { when: rel(j.next) }));
      return h('tr',
        h('td', h('div.cell-title', t('jobs.name.' + j.key)), h('div.cell-sub', t('jobs.desc.' + j.key))),
        h('td', h('code', j.schedule), h('div.cell-sub', when)),
        h('td.hide-md', h('span.mono', j.model || '—')),
        h('td', h('span.status', { class: RUN_LAMP[j.lastStatus] }, lamp(RUN_LAMP[j.lastStatus]), j.lastStatus === 'none' ? t('live.jobNever') : t('jobs.status.' + j.lastStatus)), j.last ? h('div.cell-sub', rel(j.last)) : null),
        h('td', { style: { minWidth: '150px' } },
          h('div.row.between', { style: { fontSize: '12px' } }, h('span.mono', fmtTokens(j.tokens) + ' / ' + fmtTokens(j.budget)), j.cost != null ? h('span.mono.muted', fmtCost(j.cost)) : null),
          h('div.meter', { class: pct > 90 ? 'danger' : pct > 70 ? 'caution' : 'clear', role: 'meter', 'aria-valuenow': pct, 'aria-valuemin': 0, 'aria-valuemax': 100, 'aria-label': t('jobs.budget') }, h('i', { style: { width: Math.min(100, pct) + '%' } }))),
        h('td', { style: { textAlign: 'right', whiteSpace: 'nowrap' } },
          isLive()
            ? (j.installed && !j.paused ? h('button.btn.sm', { type: 'button', onclick: async (e) => {
              if (!confirm(t('live.runNowConfirm', { name: t('jobs.name.' + j.key), budget: fmtTokens(j.budget) }))) return;
              e.currentTarget.disabled = true;
              try { await api.runJob(j.id); toast(t('jobs.started', { name: t('jobs.name.' + j.key) })); } catch (err) { toast(err.message); } finally { e.currentTarget.disabled = false; }
            } }, icon('play'), t('jobs.runNow')) : null)
            : h('button.btn.sm', { type: 'button', onclick: () => toast(t('jobs.started', { name: t('jobs.name.' + j.key) })) }, icon('play'), t('jobs.runNow')), ' ',
          h('button.btn.sm.ghost', { type: 'button', onclick: async () => { try { await api.toggleJob(j.id); draw(); } catch (err) { toast(err.message); } } }, icon(j.paused ? 'play' : 'pause'), j.paused ? t('jobs.resume') : t('jobs.pause'))));
    }));
  }
  draw();
  const history = h('table.timetable', h('tbody', runs.map((r) => {
    const j = list.find((x) => x.id === r.job);
    return h('tr', h('td.tt-time', dateTime(r.at)), h('td.tt-lamp', lamp(RUN_LAMP[r.status])),
      h('td', h('strong', { style: { fontWeight: 600 } }, t('jobs.name.' + (j ? j.key : r.job))), r.out ? [' · ', h('span.muted', r.out)] : null),
      h('td.tt-who.mono', fmtTokens(r.tokens) + (r.cost != null ? ' · ' + fmtCost(r.cost) : '')));
  })));
  return { el: h('div.page',
    pageHead(t('jobs.title'), t('jobs.lede')),
    bgPanel(),
    isLive() && !list.some((j) => j.installed) ? h('div.callout.route', icon('info'), h('div', h('b', t('live.jobsNoneTitle')), h('p', t('live.jobsNoneBody')), h('code', api.jobsInstallCmd()))) : null,
    h('section.panel', h('div.table-wrap', h('table.t',
      h('thead', h('tr', ['job', 'schedule', 'model', 'last', 'budget'].map((c) => h('th', { class: c === 'model' ? 'hide-md' : '' }, t('jobs.col.' + c))), h('th'))), rows))),
    h('div.grid.main-side',
      panel(t('jobs.history'), t('jobs.historySub'), runs.length ? history : h('p.muted', { style: { padding: '16px' } }, t('live.runsEmpty')), { flush: true }),
      panel(t('jobs.rulesTitle'), null, h('ul', { style: { margin: 0, paddingLeft: '18px', display: 'grid', gap: '8px', color: 'var(--text-2)', fontSize: '13px' } },
        ['lock', 'resume', 'budget', 'zero', 'batch'].map((k) => h('li', t('jobs.rule.' + k))))))) };
}
