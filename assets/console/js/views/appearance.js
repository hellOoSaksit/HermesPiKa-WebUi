// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Settings > Appearance: every choice is shown as a picture (a mini window,
// a colour swatch, "Aa" in each size), with the name in a tooltip.
import { t } from '../i18n.js';
import { h, s, icon, toast } from '../ui.js';
import { CHOICES, getPrefs, setPref, resetPrefs, themeNow } from '../prefs.js';

const ACCENT = {
  dark: { rose: '#EC79C0', violet: '#A992FF', blue: '#6DB0FF', teal: '#4FD1BE', coral: '#FF9473', mono: '#E6E1E9' },
  light: { rose: '#C0347E', violet: '#6A4BD6', blue: '#1F68C9', teal: '#0D7A6C', coral: '#C2462A', mono: '#2A242D' },
};
// ground, panel, raised: what each background family looks like
const PALETTE = {
  dark: { plum: ['#0D0B0F', '#1B171F', '#251F2A'], graphite: ['#0E0F11', '#1C1F23', '#262A2F'], ocean: ['#0B0F14', '#17202B', '#212C39'], forest: ['#0B100D', '#18211C', '#222D26'] },
  light: { plum: ['#EDE9EF', '#FFFFFF', '#EFEAF1'], graphite: ['#ECEDEF', '#FFFFFF', '#EEF0F2'], ocean: ['#E8EEF4', '#FFFFFF', '#EBF1F6'], forest: ['#EAEFEB', '#FFFFFF', '#EDF2EE'] },
};

// A tiny window: rail, two bubbles and a button in the accent colour.
function miniWindow(mode, pal, accent) {
  const [g, p, r] = PALETTE[mode][pal];
  const ink = mode === 'dark' ? '#5F5567' : '#CEC6D3';
  return [
    s('rect', { x: 0, y: 0, width: 64, height: 42, rx: 7, fill: g }),
    s('rect', { x: 4, y: 4, width: 15, height: 34, rx: 4, fill: p }),
    s('rect', { x: 7, y: 9, width: 9, height: 2.5, rx: 1.2, fill: ink }),
    s('rect', { x: 7, y: 14, width: 7, height: 2.5, rx: 1.2, fill: ink }),
    s('rect', { x: 23, y: 8, width: 26, height: 7, rx: 3.5, fill: r }),
    s('rect', { x: 34, y: 18, width: 26, height: 7, rx: 3.5, fill: accent, opacity: 0.35 }),
    s('rect', { x: 23, y: 30, width: 14, height: 6, rx: 3, fill: accent }),
  ];
}

export function appearancePanel(onChange) {
  const box = h('div.ap');
  const draw = () => {
    const p = getPrefs();
    const mode = themeNow();
    const acc = ACCENT[mode][p.accent];

    const pic = {
      theme: (v) => {
        if (v === 'auto') {
          return s('svg', { width: 64, height: 42, viewBox: '0 0 64 42', 'aria-hidden': 'true' },
            s('defs', s('clipPath', { id: 'apHalf' }, s('path', { d: 'M0 0H64L0 42Z' }))),
            ...miniWindow('light', p.palette, ACCENT.light[p.accent]),
            s('g', { 'clip-path': 'url(#apHalf)' }, ...miniWindow('dark', p.palette, ACCENT.dark[p.accent])));
        }
        return s('svg', { width: 64, height: 42, viewBox: '0 0 64 42', 'aria-hidden': 'true' }, ...miniWindow(v, p.palette, ACCENT[v][p.accent]));
      },
      accent: (v) => h('span.ap-swatch', { style: { background: ACCENT[mode][v] } }),
      palette: (v) => h('span.ap-pal', PALETTE[mode][v].map((c) => h('i', { style: { background: c } }))),
      size: (v) => h('span.ap-aa', { style: { fontSize: { sm: '12px', md: '15px', lg: '18px', xl: '21px' }[v] } }, 'Aa'),
      density: (v) => {
        const gap = { compact: 4, cozy: 6, roomy: 8.5 }[v];
        return s('svg', { width: 30, height: 24, viewBox: '0 0 30 24', 'aria-hidden': 'true' },
          [0, 1, 2].map((i) => s('rect', { x: 3, y: 12 - gap + i * gap - 1.25, width: 24, height: 2.5, rx: 1.25, fill: i === 1 ? acc : 'currentColor', opacity: i === 1 ? 1 : 0.55 })));
      },
      radius: (v) => s('svg', { width: 26, height: 24, viewBox: '0 0 26 24', 'aria-hidden': 'true' },
        s('path', { d: { sharp: 'M3 21V5a2 2 0 0 1 2-2h18', soft: 'M3 21V10a7 7 0 0 1 7-7h13', round: 'M3 21v-2A16 16 0 0 1 19 3h4' }[v], fill: 'none', stroke: acc, 'stroke-width': 2.4, 'stroke-linecap': 'round' })),
      font: (v) => h('span.ap-font', { style: { fontFamily: v === 'anuphan' ? '"Anuphan"' : '"Segoe UI", "Leelawadee UI", system-ui, sans-serif' } }, 'กข Aa'),
      motion: (v) => s('svg', { width: 30, height: 24, viewBox: '0 0 30 24', 'aria-hidden': 'true' },
        v === 'on'
          ? [s('path', { d: 'M3 17c5-10 9-10 12 0s7 10 12 0', fill: 'none', stroke: acc, 'stroke-width': 2.2, 'stroke-linecap': 'round' }), s('circle', { cx: 27, cy: 17, r: 2.6, fill: acc })]
          : [s('path', { d: 'M3 12h24', fill: 'none', stroke: 'currentColor', 'stroke-width': 2.2, 'stroke-linecap': 'round', opacity: 0.6 }), s('circle', { cx: 15, cy: 12, r: 2.6, fill: 'currentColor' })]),
    };
    const ICON = { theme: 'moon', accent: 'sparkle', palette: 'layers', size: 'lang', density: 'compress', radius: 'edit', font: 'quote', motion: 'play' };
    const captions = new Set(['theme']);

    const row = (key) => {
      const opts = CHOICES[key];
      const buttons = opts.map((v) => h('button.ap-opt', {
        type: 'button', role: 'radio', 'aria-checked': String(p[key] === v), tabindex: p[key] === v ? '0' : '-1',
        'aria-label': t(`settings.ap.o.${key}.${v}`), 'data-tip': captions.has(key) ? null : t(`settings.ap.o.${key}.${v}`),
        onclick: () => { setPref(key, v); draw(); onChange?.(key); },
      }, pic[key](v), captions.has(key) ? h('span', t(`settings.ap.o.${key}.${v}`)) : null));
      const group = h('div.ap-opts', { role: 'radiogroup', 'aria-label': t('settings.ap.' + key),
        onkeydown: (e) => {
          const i = buttons.indexOf(document.activeElement);
          if (i < 0) return;
          const d = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key];
          if (!d) return;
          e.preventDefault();
          const next = opts[(i + d + opts.length) % opts.length];
          setPref(key, next); draw(); onChange?.(key);
          box.querySelector(`[data-key="${key}"] [aria-checked="true"]`)?.focus();
        } }, buttons);
      return h('div.ap-row', { 'data-key': key }, h('span.label', icon(ICON[key]), t('settings.ap.' + key)), group);
    };

    box.replaceChildren(
      ...Object.keys(CHOICES).map(row),
      h('button.btn.sm.ghost.ap-reset', { type: 'button', onclick: () => { resetPrefs(); draw(); onChange?.('reset'); toast(t('settings.ap.resetDone')); } }, icon('undo'), t('settings.ap.reset')));
  };
  draw();
  document.documentElement.addEventListener('hpk:prefs', () => { if (box.isConnected && !box.contains(document.activeElement)) draw(); });
  return box;
}
