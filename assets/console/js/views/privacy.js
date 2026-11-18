// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
import { api } from '../data.js';
import { isLive } from '../live.js';
import { t } from '../i18n.js';
import { h, icon, pageHead, toast, privacyTag, panel, emptyState } from '../ui.js';

export default async function privacy() {
  const p = api.project();
  const promos = await api.promotions();
  let mode = p.privacy;

  const matrix = h('div.table-wrap', h('table.t',
    h('thead', h('tr', h('th', t('privacy.matrix.mode')), h('th', t('privacy.matrix.write')), h('th', t('privacy.matrix.read')), h('th.hide-md', t('privacy.matrix.channels')))),
    h('tbody', ['shared', 'private', 'isolated'].map((m) => h('tr', { class: m === mode ? 'sel' : '' },
      h('td', privacyTag(m)),
      h('td', t(`privacy.${m}.write`)), h('td', t(`privacy.${m}.read`)), h('td.hide-md', t(`privacy.${m}.channels`)))))));

  const choice = h('div.choice', { role: 'radiogroup', 'aria-label': t('privacy.modeTitle') }, ['shared', 'private', 'isolated'].map((m) => h('label',
    h('input', { type: 'radio', name: 'pm', checked: m === mode || null, onchange: () => (mode = m) }),
    h('span.c-title', privacyTag(m)), h('span.c-desc', t(`privacy.${m}.desc`)))));

  const promoList = h('div.stack');
  function drawPromos(list) {
    if (p.privacy !== 'shared') { promoList.replaceChildren(h('div.callout.caution', icon('lock'), h('div', t('privacy.promoOff', { mode: t(`privacy.${p.privacy}.name`) })))); return; }
    if (!list.length) { promoList.replaceChildren(emptyState(t('privacy.promoEmpty'))); return; }
    promoList.replaceChildren(...list.map((x) => h('section.panel',
      h('div.panel-head', h('h2', x.title), h('span.sub.mono', x.page + (x.wiki ? ' · ' + t('wiki.promoteTo.' + x.wiki) : ''))),
      h('div.panel-body.stack',
        h('div.diff', x.diff.map(([k, txt]) => h('div', { class: k === 'redact' ? 'ctx' : k }, k === 'redact' ? h('span.redact', txt) : txt))),
        h('div.row',
          h('button.btn.go', { type: 'button', onclick: async () => { try { await api.resolvePromotion(x.id, 'approve'); drawPromos(await api.promotions()); toast(t('privacy.promoted')); } catch (err) { toast(err.message); } } }, icon('check'), t('privacy.promote')),
          isLive()
            ? h('button.btn', { type: 'button', onclick: (e) => {
              // Edit the redacted text in place, then save it back to the draft.
              const sec = e.currentTarget.closest('section');
              const area = h('textarea.input', { rows: 10, style: { width: '100%', fontFamily: 'var(--mono)' } });
              area.value = x.diff.map(([, txt]) => txt).join(String.fromCharCode(10));
              const save = h('button.btn.primary', { type: 'button', onclick: async () => {
                try { await api.editPromotion(x.id, area.value); drawPromos(await api.promotions()); toast(t('settings.saved')); } catch (err) { toast(err.message); }
              } }, t('common.save'));
              sec.querySelector('.diff').replaceWith(h('div.stack', area, h('p.faint', { style: { fontSize: '12px' } }, t('live.editRedactHint')), h('div.row', save)));
            } }, t('privacy.editRedact'))
            : h('button.btn', { type: 'button', onclick: () => toast(t('privacy.editRedaction')) }, t('privacy.editRedact')),
          h('div.red-zone', h('button.btn.danger', { type: 'button', onclick: async () => { try { await api.resolvePromotion(x.id, 'discard'); drawPromos(await api.promotions()); toast(t('privacy.kept')); } catch (err) { toast(err.message); } } }, t('privacy.keepLocal'))))))));
  }
  drawPromos(promos);

  return { el: h('div.page',
    pageHead(t('privacy.title'), t('privacy.lede', { project: p.name })),
    h('div.grid.two',
      panel(t('privacy.modeTitle'), privacyTag(p.privacy), h('div.stack', choice,
        h('label.switch', h('input', { type: 'checkbox', checked: p.modelPolicy === 'local_only' || null, onchange: async (e) => {
          if (!isLive()) return;
          try { await api.updateProject(p.id, { modelPolicy: e.target.checked ? 'local_only' : 'any' }); toast(t('setup.saved')); } catch (err) { toast(err.message); }
        } }), t('wizard.privacy.local')),
        isLive() ? h('p.faint', { style: { fontSize: '12px' } }, t('live.localOnlyNote')) : null,
        isLive() && p.modelRoute ? (p.modelRoute.local
          ? h('div.callout', icon('check'), h('div', t('live.localOk', { profile: p.modelRoute.profile, where: p.modelRoute.where })))
          : h('div.callout.danger', icon('warn'), h('div', t('live.localBad', { profile: p.modelRoute.profile, where: p.modelRoute.where })))) : null,
        h('div.callout.caution', icon('warn'), h('div', t('privacy.changeNote'))),
        h('div.row', h('button.btn.primary', { type: 'button', onclick: async () => {
          if (mode === p.privacy) return;
          const rank = { shared: 0, private: 1, isolated: 2 };
          const opening = rank[mode] < rank[p.privacy];
          if (!isLive()) {
            if (opening && mode === 'shared') { toast(t('privacy.needsApproval')); return; }
            api.updateProject(p.id, { privacy: mode }); toast(t('privacy.saved', { mode: t(`privacy.${mode}.name`) })); return;
          }
          if (opening && !confirm(t('live.openUpConfirm', { name: p.name, mode: t(`privacy.${mode}.name`) }))) return;
          try { await api.updateProject(p.id, { privacy: mode, confirm: opening }); toast(t('privacy.saved', { mode: t(`privacy.${mode}.name`) })); } catch (err) { toast(err.message); }
        } }, t('common.save'))))),
      panel(t('privacy.matrixTitle'), null, matrix, { flush: true })),
    h('div.stack', h('div.page-head', h('div', h('h2', t('privacy.promoTitle')), h('p.lede', t('privacy.promoLede')))), promoList)) };
}
