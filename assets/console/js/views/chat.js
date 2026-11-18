// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Chat — feature set modelled on what people expect from a full agent chat
// (session list, new chat, per-message actions, slash commands, attachments,
// model/effort pickers, context ring, queue/steer/interrupt), written from
// scratch for HPK and joined to its own ideas: child sessions, child summaries,
// "save to wiki" drafts, Ask/Auto per session, red lines.
import { api, bus } from '../data.js';
import { teamMode } from '../live.js';
import { t } from '../i18n.js';
import { planHeadline, planGraph, evTag, evText } from './plan.js';
import { commandRisks } from '../shell.js';
import { richBlocks, mediaFor, refs } from '../media/view.js';
import { h, s, icon, lamp, clock, rel, sessionStatus, avatar, person, toast, fmtTokens, fmtCost, SESSION_LAMP, menu, confirmRedLine, openDrawer, closeDrawer } from '../ui.js';

// "anthropic:claude-opus" → "claude-opus"
const shortModel = (id) => String(id || '').split(':').pop();

const MODELS = ['haiku', 'sonnet', 'opus'];
const EFFORTS = ['low', 'medium', 'high', 'max'];
const SLASH = ['new', 'split', 'mode', 'model', 'ask', 'effort', 'compress', 'goal', 'title', 'btw', 'stop', 'who', 'wiki', 'help'];
// There is one kind of chat. The agent splits work into sub-tasks by itself;
// sub-tasks are sessions with a parent and are shown inside their main chat.
export function startNewChat() { location.hash = '#/chat/new'; }

export const rootOf = (sess) => { let x = sess; while (x?.parent) x = api.session(x.parent) || null; return x || sess; };
export const subtasksOf = (id) => api.sessionsSync().filter((x) => x.parent === id).flatMap((x) => [x, ...subtasksOf(x.id)]);
const RANK = { waiting: 4, stale: 3, running: 2, queued: 1.5, done: 1, stopped: 0 };
// The lamp of a main chat is the most urgent state anywhere in its tree.
const treeStatus = (root) => [root, ...subtasksOf(root.id)].reduce((a, x) => (RANK[x.status] > RANK[a] ? x.status : a), root.status);

const readView = () => { try { return localStorage.getItem('hpk.chatView') || 'worklog'; } catch { return 'worklog'; } };
const writeView = (v) => { try { localStorage.setItem('hpk.chatView', v); } catch { /* ignore */ } };

// Tiny, safe markdown: paragraphs, `code`, **bold**. Never uses innerHTML.
function md(text) {
  return String(text).split(/\n{2,}/).map((para) => {
    const p = h('p');
    // Bold only for **words** standing on their own, so paths like api/** stay literal.
    para.split(/(`[^`]+`|(?<![\w/*])\*\*[^*\s](?:[^*]*[^*\s])?\*\*(?![\w/*]))/g).forEach((part) => {
      if (/^`[^`]+`$/.test(part)) p.append(h('code', part.slice(1, -1)));
      else if (/^\*\*[^*\s](?:[^*]*[^*\s])?\*\*$/.test(part)) p.append(h('strong', part.slice(2, -2)));
      else if (part) p.append(part);
    });
    return p;
  });
}

function defaultSession() {
  const mains = api.sessionsSync().filter((x) => !x.parent && x.viaHpk && !api.chatMeta(x.id).archived);
  return mains.find((x) => treeStatus(x) === 'waiting') || mains.find((x) => x.status === 'running') || mains[0];
}

// ---------------------------------------------------------------- list ----
// Only main chats are listed. Their sub-tasks fold underneath.
export function chatList(currentId, rerender) {
  let q = '';
  let filter = 'all';
  const current = api.session(currentId);
  const openRoot = current ? rootOf(current).id : null;
  const expanded = new Set(openRoot ? [openRoot] : []);
  const groups = h('div.cl-groups');
  const subRow = (x) => h('a.chat-pick.cp-sub', { href: '#/sessions/' + x.id, 'aria-current': x.id === currentId ? 'page' : null },
    lamp(SESSION_LAMP[x.status], { blink: x.status === 'waiting' }),
    h('span.cp-main', h('span.cp-title', x.title), h('span.cp-meta', x.ev ? evText(x) : t('status.' + x.status))));
  const row = (x) => {
    const meta = api.chatMeta(x.id);
    const subs = subtasksOf(x.id);
    const waiting = subs.filter((s) => s.status === 'waiting').length;
    const st = treeStatus(x);
    const more = h('button.icon-btn.cp-more', { type: 'button', 'aria-label': t('chat.moreFor', { title: x.title }), 'aria-haspopup': 'menu',
      onclick: (e) => { e.preventDefault(); e.stopPropagation(); menu(more, sessionMenuItems(x.id, rerender), { align: 'right' }); } }, icon('more'));
    const isOpen = expanded.has(x.id);
    const toggle = subs.length ? h('button.icon-btn.cp-toggle', { type: 'button', 'aria-expanded': String(isOpen), 'aria-label': t('chat.subtasks.toggle', { n: subs.length }),
      onclick: (e) => { e.preventDefault(); e.stopPropagation(); isOpen ? expanded.delete(x.id) : expanded.add(x.id); draw(); } }, icon('chevron')) : null;
    const metaText = subs.length
      ? t('chat.subtasks.count', { n: subs.length }) + (waiting ? ' · ' + t('chat.subtasks.needsYou', { n: waiting }) : '')
      : t('status.' + x.status) + ' · ' + rel(x.heartbeat);
    return h('div.cp-block',
      h('div.cp-row', h('a.chat-pick', { href: '#/sessions/' + x.id, 'aria-current': x.id === currentId ? 'page' : null },
        // The ring around the avatar carries the most urgent state in this chat's tree.
        h('span.cp-av', { class: 'st-' + st, 'aria-hidden': 'true' }, person(x.owner).initials),
        h('span.cp-main',
          h('span.cp-title', meta.pinned ? icon('pin', 'i cp-pin') : null, h('span.cp-t', x.title), h('span.cp-time', st === 'waiting' ? t('chat.list.waiting') : rel(x.heartbeat))),
          h('span.cp-meta', { class: waiting ? 'hot' : '' }, metaText))), toggle, more),
      isOpen && subs.length ? h('div.cp-subs', subs.map(subRow)) : null);
  };
  const draw = () => {
    const matches = (x) => !q || (x.title + ' ' + subtasksOf(x.id).map((s) => s.title).join(' ')).toLowerCase().includes(q);
    const all = api.sessionsSync().filter((x) => !x.parent && matches(x));
    if (q) all.forEach((x) => expanded.add(x.id));
    const archived = all.filter((x) => api.chatMeta(x.id).archived);
    const live = all.filter((x) => !api.chatMeta(x.id).archived)
      .filter((x) => filter === 'all' || filter === 'archived' || (filter === 'waiting' ? treeStatus(x) === 'waiting' : x.status === 'running'));
    const pinned = live.filter((x) => api.chatMeta(x.id).pinned);
    const mine = live.filter((x) => !api.chatMeta(x.id).pinned && x.viaHpk && x.status !== 'done' && x.status !== 'stopped');
    const other = live.filter((x) => !api.chatMeta(x.id).pinned && (!x.viaHpk || x.status === 'done' || x.status === 'stopped'));
    const sec = (label, list) => (list.length ? [h('div.cl-sep', label), h('div.cl-group', list.map(row))] : []);
    groups.replaceChildren(...(filter === 'archived'
      ? sec(t('chat.list.archived'), archived)
      : [...sec(t('chat.list.pinned'), pinned), ...sec(t('chat.list.active'), mine), ...sec(t('chat.listOther'), other)]));
    if (!groups.childElementCount) groups.append(h('p.empty', t('chat.list.none')));
  };
  const search = h('div.search', icon('search'), h('input.input', { type: 'search', placeholder: t('chat.list.search'), 'aria-label': t('chat.list.search'), oninput: (e) => { q = e.target.value.trim().toLowerCase(); draw(); } }));
  // A small select instead of four buttons: filtering is occasional, searching is common.
  const seg = h('select.input.cl-filter', { 'aria-label': t('chat.list.filter'), onchange: (e) => { filter = e.target.value; draw(); } },
    ['all', 'waiting', 'running', 'archived'].map((k) => h('option', { value: k }, t('chat.list.f.' + k))));
  draw();
  return h('nav.chat-list', { 'aria-label': t('chat.listAria') },
    h('div.cl-head',
      h('h2', t('chat.listTitle')),
      h('button.icon-btn.sm', { type: 'button', onclick: () => startNewChat(), title: t('chat.newChat') + ' · Ctrl+K', 'aria-label': t('chat.newChat') }, icon('plus'))),
    h('div.cl-tools', search, seg), groups);
}

function sessionMenuItems(sid, rerender) {
  const x = api.session(sid);
  const meta = api.chatMeta(sid);
  return [
    { label: t('chat.menu.rename'), icon: 'edit', onSelect: () => renameDrawer(sid, rerender) },
    { label: meta.pinned ? t('chat.menu.unpin') : t('chat.menu.pin'), icon: 'pin', onSelect: () => { api.setChatMeta(sid, { pinned: !meta.pinned }); rerender(); } },
    { label: t('chat.menu.fork'), icon: 'git', onSelect: () => { const id = api.forkSession(sid, api.chatSync(sid).length - 1); toast(t('chat.forked')); location.hash = '#/sessions/' + id; } },
    teamMode() ? { label: t('chat.menu.share'), icon: 'team', onSelect: () => shareDrawer(sid, rerender) } : null,
    { sep: true },
    { label: t('chat.menu.exportMd'), icon: 'file', onSelect: () => exportChat(sid, 'md') },
    { label: t('chat.menu.exportJson'), icon: 'file', onSelect: () => exportChat(sid, 'json') },
    { label: t('chat.menu.board'), icon: 'board', onSelect: () => (location.hash = '#/board') },
    { label: meta.archived ? t('chat.menu.unarchive') : t('chat.menu.archive'), icon: 'folder', onSelect: () => { api.archiveSession(sid, !meta.archived); toast(meta.archived ? t('chat.unarchived') : t('chat.archived')); rerender(); } },
    { sep: true },
    { label: t('sessions.stop'), icon: 'stop', danger: true, disabled: !x.viaHpk || x.status === 'done' || x.status === 'stopped', onSelect: () => { api.stopSession(sid); toast(t('sessions.stopped', { num: x.num })); rerender(); } },
    { label: t('chat.menu.delete'), icon: 'x', danger: true, onSelect: async () => {
      if (await confirmRedLine({ title: t('chat.menu.delete'), body: t('chat.deleteBody', { title: x.title }), redLine: 'delete', confirmLabel: t('chat.menu.delete') })) {
        api.deleteSession(sid); toast(t('chat.deleted'), null, 'deny'); location.hash = '#/chat';
      }
    } },
  ];
}

function renameDrawer(sid, rerender) {
  const x = api.session(sid);
  const input = h('input.input#renIn', { value: x.title });
  const save = () => { api.renameSession(sid, input.value.trim()); closeDrawer(); toast(t('chat.renamed')); rerender(); };
  openDrawer(t('chat.menu.rename'), h('form.stack', { style: { padding: '16px' }, onsubmit: (e) => { e.preventDefault(); save(); } },
    h('div.field', h('label', { for: 'renIn' }, t('chat.titleLabel')), input, h('span.hint', t('chat.renameHint')))),
  [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t('common.cancel')), h('button.btn.primary', { type: 'button', onclick: save }, t('common.save'))]);
}

function exportChat(sid, fmt) {
  const x = api.session(sid);
  const log = api.chatSync(sid);
  const body = fmt === 'json' ? JSON.stringify({ session: x, messages: log }, null, 2)
    : `# ${x.num} ${x.title}\n\n` + log.map((m) => (m.t === 'msg' ? `**${m.who === 'user' ? person(m.author).name : 'Agent'}** (${clock(m.at)})\n\n${m.text}\n` : m.t === 'tool' ? `> ${m.name}: ${m.summary}\n` : '')).join('\n');
  const a = h('a', { href: URL.createObjectURL(new Blob([body], { type: fmt === 'json' ? 'application/json' : 'text/markdown' })), download: `${x.num}-${x.title.replace(/[^\w.-]+/g, '-')}.${fmt === 'json' ? 'json' : 'md'}` });
  document.body.append(a); a.click(); a.remove();
  toast(t('chat.exported'));
}

// ---------------------------------------------------------------- view ----
export default async function chatView({ params, go, rerender }) {
  if (location.hash.startsWith('#/chat/new')) return newChatView({ go });
  const sess = params.id ? api.session(params.id) : defaultSession();
  if (!sess) { go('sessions'); return h('div'); }
  await api.chat(sess.id);
  const wiki = await api.wikiTree();
  const wikiPages = wiki.flatMap((w) => w.pages.map((p) => p.path));
  const meta = api.chatMeta(sess.id);
  const canChat = sess.viaHpk && sess.status !== 'done' && sess.status !== 'stopped';
  let view = readView();
  let attachments = [];
  let editing = null;

  const page = h('div.page.chat-page');
  const log = h('div.chat-log', { role: 'log', 'aria-live': 'polite', 'aria-label': t('chat.logAria') });

  // ---- messages ----
  const actions = (m, idx, isLastAgent) => {
    const btn = (label, ic, fn) => h('button.icon-btn.sm', { type: 'button', 'aria-label': label, title: label, onclick: fn }, icon(ic));
    const list = [btn(t('chat.act.copy'), 'copy', () => { navigator.clipboard?.writeText(m.text || ''); toast(t('chat.copied')); })];
    // A held steer/queue message can be sent at once (interrupts) or dropped;
    // one the agent never read can be sent again.
    if (m.delivery === 'pending' && canChat) {
      list.push(btn(t('chat.delivery.sendNow'), 'send', () => api.sendQueuedNow(sess.id, idx)));
      list.push(btn(t('chat.delivery.drop'), 'x', () => api.dropQueued(sess.id, idx)));
    }
    if (m.delivery === 'unused' && canChat) list.push(btn(t('chat.delivery.resend'), 'steer', () => api.sendMessage(sess.id, m.text, api.isStreaming(sess.id) ? 'steer' : 'send')));
    if (m.who === 'user' && canChat) list.push(btn(t('chat.act.edit'), 'edit', () => { editing = idx; draw(); }));
    if (m.who === 'agent') {
      if (isLastAgent && canChat) list.push(btn(t('chat.act.regenerate'), 'undo', () => api.regenerate(sess.id, idx)));
      list.push(btn(t('chat.act.fork'), 'git', () => { const id = api.forkSession(sess.id, idx); toast(t('chat.forked')); go('sessions/' + id); }));
      list.push(btn(t('chat.act.wiki'), 'wiki', () => { wikiDrawer(idx); }));
      if (canChat) list.push(btn(t('chat.act.quote'), 'quote', () => { input.value = '> ' + (m.text || '').slice(0, 160) + '\n\n' + input.value; input.focus(); autosize(); }));
    }
    return h('div.msg-actions', list);
  };

  const msgNode = (m, idx, isLastAgent) => {
    const isUser = m.who === 'user';
    const who = isUser ? person(m.author) : { name: t('actor.agent') + ' · ' + sess.num };
    let bodyEl;
    if (editing === idx) {
      const ta = h('textarea.input', { 'aria-label': t('chat.act.edit') }, m.text);
      bodyEl = h('div.msg-edit', ta, h('div.row', { style: { gap: '6px' } },
        h('button.btn.sm.primary', { type: 'button', onclick: () => { editing = null; api.editAndResend(sess.id, idx, ta.value.trim() || m.text); } }, t('chat.editSend')),
        h('button.btn.sm.ghost', { type: 'button', onclick: () => { editing = null; draw(); } }, t('common.cancel')),
        h('span.faint', { style: { fontSize: '12px' } }, t('chat.editNote'))));
      setTimeout(() => ta.focus(), 0);
    } else {
      // Finished messages show tables, charts, code and the files they name (images, video, 3D, …).
      bodyEl = h('div.m-body', { class: m.streaming ? 'caret' : '' }, m.text ? (m.streaming ? md(m.text) : richBlocks(m.text, md)) : m.streaming ? h('span.typing', h('i'), h('i'), h('i')) : '');
      if (m.text && !m.streaming) { const media = mediaFor(m.text); if (media) bodyEl.append(media); }
    }
    return h('div.msg', { class: m.who },
      avatar(isUser ? m.author : 'agent', 28),
      h('div.msg-col',
        h('div.m-who', h('strong', who.name), h('span.tnum', clock(m.at)),
          m.how ? h('span.tag.route', icon(m.how === 'queue' ? 'queue' : m.how === 'steer' ? 'steer' : 'stop'), t('chat.how.' + m.how)) : null,
          // Honest delivery: say whether the agent actually read it.
          m.delivery ? h('span.tag.delivery', { class: { pending: '', read: 'clear', unused: 'caution' }[m.delivery], title: t('chat.delivery.' + m.delivery + 'Hint') },
            icon({ pending: 'clock', read: 'check', unused: 'warn' }[m.delivery]), t('chat.delivery.' + m.delivery)) : null,
          m.cut ? h('span.tag', t('chat.cut')) : null),
        bodyEl,
        m.files?.length ? h('div.att-row', m.files.map((f) => h('span.att', icon('file'), f))) : null,
        !m.streaming && editing !== idx ? actions(m, idx, isLastAgent) : null));
  };

  // A tool row stays folded; what it produced (a render, a model, a chart) shows below it.
  const toolNode = (m) => {
    const row = h('details.toolrow',
      h('summary', icon('chevron', 'chev'), icon('tool'), h('span.mono', m.name), h('span', '· ' + m.summary), h('span.faint', { style: { marginLeft: 'auto' } }, clock(m.at))),
      h('pre', String(m.out ?? '').replace(/(data:[a-z]+\/[a-z0-9.+-]+;base64,)[A-Za-z0-9+/=]{80,}/gi, '$1…')));
    const named = new Set(api.chatSync(sess.id).filter((x) => x.t === 'msg' && x.text).flatMap((x) => refs(x.text).map((r) => r.src)));
    const media = mediaFor(String(m.out ?? ''), 12, named);
    return media ? h('div.media-toolrow', row, media) : row;
  };
  const thinkNode = (m) => h('details.toolrow.think', h('summary', icon('chevron', 'chev'), icon('sparkle'), h('span', t('chat.thinking')), h('span.faint', { style: { marginLeft: 'auto' } }, clock(m.at))), h('pre', m.text));

  const permitNode = (m) => {
    const pending = !m.decision && api.approvalsSync().some((a) => a.id === m.id);
    const parts = commandRisks(m.command);
    if (!pending) {
      // Answered: one quiet line, so the log stays readable.
      const d = m.decision || 'decided';
      return h('div.permit-done', { class: d === 'deny' ? 'deny' : '' }, lamp(d === 'deny' ? 'danger' : 'clear'),
        h('span', t('chat.permit.done.' + d)), h('code', parts.length > 1 ? t('chat.permit.parts', { n: parts.length }) : m.command), h('span.faint', clock(m.at)));
    }
    const decide = (d) => {
      m.decision = d;
      api.resolveApproval(m.id, d === 'deny' ? 'deny' : 'approve');
      toast(t('chat.permit.done.' + d), null, d === 'deny' ? 'deny' : 'ok');
      draw();
    };
    const card = h('div.permit', { role: 'group', tabindex: 0, 'data-ask': m.id, 'aria-label': t('chat.permitAria') + '. ' + t('chat.permit.keys'),
      onkeydown: (e) => {
        if (e.target !== card || e.ctrlKey || e.metaKey || e.altKey) return;
        const d = { 1: 'deny', 2: 'once', 3: 'session' }[e.key];
        if (d) { e.preventDefault(); e.stopPropagation(); decide(d); }
      } },
    h('div.permit-head', icon('warn'), t('chat.permitTitle'), h('span.tag.danger', t('redlines.' + m.redLine + '.short'))),
    parts.length > 1
      ? h('ol.cmd-parts', { 'aria-label': t('chat.permit.parts', { n: parts.length }) }, parts.map((x) => h('li', { class: x.risk ? 'risky' : '' },
        h('code', x.cmd), x.op ? h('span.cmd-op.mono', x.op) : null,
        x.risk ? h('span.tag.danger', icon('warn'), t('redlines.' + x.risk + '.short')) : h('span.tag', t('chat.permit.safe')))))
      : h('pre', m.command),
    h('p', m.why),
    h('div.permit-actions',
      h('button.btn.go', { type: 'button', onclick: () => decide('once') }, t('chat.permit.allowOnce'), h('kbd.keyhint', '2')),
      h('button.btn', { type: 'button', onclick: () => decide('session') }, t('chat.allowSession'), h('kbd.keyhint', '3')),
      h('div.red-zone', h('button.btn.danger', { type: 'button', onclick: () => decide('deny') }, t('approvals.deny'), h('kbd.keyhint', '1')))),
    h('p.permit-keys.faint', t('chat.permit.keys'), ' · ', h('kbd', 'Ctrl'), '+', h('kbd', 'J'), ' ', t('chat.permit.next')));
    return card;
  };

  // Save to wiki: show the admission verdict and its reason before anything is written.
  function wikiDrawer(idx) {
    const check = api.checkWiki(sess.id, idx);
    // A sub-task summary names the sub-task as its source, not the main chat.
    const srcS = api.session(api.chatSync(sess.id)[idx]?.session) || sess;
    const text = api.wikiText(sess.id, idx);
    const secret = check.reason === 'secret';
    const lampKind = { keep: 'clear', defer: 'caution', refuse: 'danger' }[check.verdict];
    const done = (as) => {
      const path = api.saveToWiki(sess.id, idx, as);
      closeDrawer();
      if (path) toast(t('wikiAdmit.done.keep'), { label: t('chat.openDrafts'), run: () => go('wiki/drafts') });
      else toast(t('wikiAdmit.done.' + (as === 'defer' ? 'defer' : 'refused')), { label: t('wikiAdmit.openLog'), run: () => go('wiki/log') }, as === 'defer' ? 'ok' : 'deny');
    };
    const foot = [h('button.btn.ghost', { type: 'button', onclick: closeDrawer }, t(check.verdict === 'keep' ? 'common.cancel' : 'wikiAdmit.close'))];
    if (check.verdict === 'keep') foot.push(h('button.btn.primary', { type: 'button', onclick: () => done('keep') }, icon('wiki'), t('wikiAdmit.create')));
    else if (check.verdict === 'defer') foot.push(h('button.btn', { type: 'button', onclick: () => done('defer') }, t('wikiAdmit.deferBtn')), h('button.btn.primary', { type: 'button', onclick: () => done('keep') }, t('wikiAdmit.anyway')));
    else if (!secret) foot.push(h('button.btn', { type: 'button', onclick: () => done('keep') }, t('wikiAdmit.anyway')));
    else foot.push(h('button.btn.danger', { type: 'button', onclick: () => done('refuse') }, t('wikiAdmit.logRefusal')));
    openDrawer(t('wikiAdmit.title'), h('div.stack', { style: { padding: '16px' } },
      h('div.admit', { class: check.verdict }, lamp(lampKind), h('div', h('b', t('wikiAdmit.' + check.verdict)), h('p', t('wikiAdmit.reason.' + check.reason, { src: srcS.num + ' ' + srcS.title })))),
      h('div.field', h('span.label', t('wikiAdmit.preview')), secret ? h('p.callout.danger', icon('lock'), h('div', t('wikiAdmit.hidden'))) : h('pre.admit-preview', text)),
      check.verdict !== 'refuse' || !secret ? h('p.faint', { style: { fontSize: '12px' } }, t('wikiAdmit.target', { path: api.nextDraftPath() })) : null,
      h('p.faint', { style: { fontSize: '12px' } }, t('wikiAdmit.rules'))), foot);
  }

  const clarifyNode = (m, idx) => {
    const other = h('input.input', { placeholder: t('chat.clarify.custom'), 'aria-label': t('chat.clarify.custom') });
    const from = api.session(m.from);
    return h('div.clarify', { role: 'group', tabindex: -1, 'aria-label': t('chat.clarify.aria') },
      h('div.permit-head', icon('info'), t('chat.clarify.title'), from ? h('a.tag', { href: '#/sessions/' + from.id }, from.num) : null),
      h('p', m.question),
      m.answered ? h('p.status.clear', lamp('clear'), t('chat.clarify.answered', { a: m.answered }))
        : h('div.stack', { style: { gap: '8px' } },
          h('div.row', m.options.map((o) => h('button.btn', { type: 'button', onclick: () => api.answerClarify(sess.id, idx, o) }, o))),
          h('form.row', { onsubmit: (e) => { e.preventDefault(); if (other.value.trim()) api.answerClarify(sess.id, idx, other.value.trim()); } }, h('div', { style: { flex: 1 } }, other), h('button.btn.sm', { type: 'submit' }, t('chat.send')))));
  };

  const childNode = (m) => {
    const c = api.session(m.session);
    if (!c) return h('div');
    return h('a.child-card', { href: '#/sessions/' + c.id },
      h('span.cc-rail', { 'aria-hidden': 'true' }), icon('sessions'),
      h('span', h('span.faint', t('chat.child.started') + ' · '), h('strong', c.title)),
      h('span.cc-right', evTag(c), sessionStatus(c), icon('chevron')));
  };

  const summaryNode = (m, idx) => {
    const c = api.session(m.session);
    const row = (k, v) => (v ? h('div.sum-row', h('span.sum-k', t('chat.summary.' + k)), h('span', v)) : null);
    return h('div.summary-card',
      h('div.permit-head', icon('check'), t('chat.summary.title'), c ? h('a.tag', { href: '#/sessions/' + c.id }, c.title) : null, h('span.faint', { style: { marginLeft: 'auto', fontWeight: 400, fontSize: '12px' } }, clock(m.at))),
      row('done', m.done), row('decided', m.decided), row('lessons', m.lessons), row('open', m.open), row('question', m.question),
      c?.ev ? h('div.sum-row', h('span.sum-k', t('chat.summary.evidence')), h('span', evTag(c))) : null,
      c?.ev && c.ev[1] !== 'verified' && !c.merged ? h('p.sum-gate', icon('warn'), t('chat.summary.notVerified')) : null,
      h('div.row',
        c?.merged ? h('span.tag.clear', icon('git'), t('plan.act.merged'))
          : c?.ev?.[1] === 'verified' ? h('button.btn.sm.go', { type: 'button', onclick: () => { if (api.mergeSubtask(c.id)) toast(t('plan.toast.merged', { title: c.title })); } }, icon('git'), t('plan.act.merge'))
            : c?.ev && c.status !== 'stopped' ? h('button.btn.sm', { type: 'button', onclick: () => { api.steerSubtask(c.id, t('chat.summary.askTestsText')); toast(t('plan.toast.steered', { title: c.title })); } }, icon('steer'), t('chat.summary.askTests')) : null,
        h('button.btn.sm', { type: 'button', onclick: () => { wikiDrawer(idx); } }, icon('wiki'), t('chat.summary.toWiki'))));
  };

  const noteNode = (m) => {
    if (m.kind === 'plan') {
      const x = api.session(m.session);
      return h('div.chat-note', icon({ steered: 'steer', stopped: 'stop', retried: 'undo', merged: 'git' }[m.action] || 'info'), t('plan.note.' + m.action, { title: x ? x.title : m.session }));
    }
    return h('div.chat-note', icon(m.kind === 'compressed' ? 'compress' : 'info'),
      m.kind === 'compressed' ? t('chat.compressed', { from: fmtTokens(m.from), to: fmtTokens(m.to) }) : m.text);
  };

  // Where the model changed: a divider with a one-click way back (only the latest).
  const switchNode = (m, isLast) => {
    const back = isLast && canChat && m.from ? h('button.btn.sm.ghost', { type: 'button', onclick: () => switchTo(m.from) }, icon('undo'), t('chat.switch.back', { m: shortModel(m.from) })) : null;
    return h('div.ms-divider', { role: 'separator', 'aria-label': t('chat.switch.here', { m: shortModel(m.to) }) },
      h('span.ms-line'),
      h('span.ms-label', icon('gauge'), h('span', t('chat.switch.here', { m: shortModel(m.to) })),
        h('span.tag', { class: m.way === 'compact' ? 'caution' : 'clear', title: t('chat.switch.way.' + (m.way || 'continue') + 'Hint') }, t('chat.switch.way.' + (m.way || 'continue')))),
      back, h('span.ms-line'));
  };

  function draw() {
    const items = api.chatSync(sess.id);
    const lastAgent = items.map((m, i) => (m.t === 'msg' && m.who === 'agent' ? i : -1)).filter((i) => i >= 0).pop();
    const out = [];
    let work = [];
    const flushWork = () => {
      if (!work.length) return;
      const steps = work.filter((w) => w.m.t === 'tool').length;
      // Thinking on its own is not a worklog: show it as its own folded row.
      if (view === 'worklog' && !steps) { work.forEach((w) => out.push(thinkNode(w.m))); work = []; return; }
      if (view === 'worklog') {
        out.push(h('details.worklog', h('summary', icon('chevron', 'chev'), icon('tool'), h('span', t('chat.worklog', { n: steps })), h('span.faint', work.filter((w) => w.m.t === 'tool').map((w) => w.m.name).join(' · '))),
          h('div.worklog-body', work.map((w) => (w.m.t === 'tool' ? toolNode(w.m) : thinkNode(w.m))))));
      } else if (view === 'stream') work.forEach((w) => out.push(w.m.t === 'tool' ? toolNode(w.m) : thinkNode(w.m)));
      work = [];
    };
    const lastSwitch = items.map((m, i) => (m.t === 'switch' ? i : -1)).filter((i) => i >= 0).pop();
    items.forEach((m, i) => {
      if (m.t === 'tool' || m.t === 'thinking') { work.push({ m, i }); return; }
      flushWork();
      if (m.t === 'msg') out.push(msgNode(m, i, i === lastAgent));
      else if (m.t === 'permit') out.push(permitNode(m));
      else if (m.t === 'clarify') out.push(clarifyNode(m, i));
      else if (m.t === 'child') out.push(childNode(m));
      else if (m.t === 'summary') out.push(summaryNode(m, i));
      else if (m.t === 'note') out.push(noteNode(m));
      else if (m.t === 'switch') out.push(switchNode(m, i === lastSwitch));
    });
    flushWork();
    if (!out.length) out.push(h('div.empty', icon('chat'), h('p', t('chat.emptyLog'))));
    log.replaceChildren(...out);
    log.scrollTop = log.scrollHeight;
    drawFooter();
  }

  // ---- composer ----
  const input = h('textarea.input#chatIn', { rows: 2, title: t('chat.hint.keys'), placeholder: canChat ? t('chat.placeholderShort') : t('chat.readOnlyHint'), disabled: !canChat || null, 'aria-label': t('chat.placeholder'), 'aria-autocomplete': 'list', 'aria-controls': 'chatPalette' });
  input.value = meta.draft || '';
  const autosize = () => { input.style.height = 'auto'; input.style.height = Math.min(240, input.scrollHeight) + 'px'; };
  const palette = h('div.palette#chatPalette', { role: 'listbox', hidden: true });
  let palItems = []; let palIdx = 0; let palMode = null;
  const fileIn = h('input', { type: 'file', multiple: true, hidden: true, onchange: (e) => { attachments.push(...[...e.target.files].map((f) => f.name)); e.target.value = ''; drawAtt(); } });
  const attRow = h('div.att-row');
  const drawAtt = () => attRow.replaceChildren(...attachments.map((f, i) => h('span.att', icon('file'), f, h('button.icon-btn.sm', { type: 'button', 'aria-label': t('chat.removeFile', { f }), onclick: () => { attachments.splice(i, 1); drawAtt(); } }, icon('x')))));

  function showPalette() {
    const v = input.value;
    const caret = input.selectionStart;
    const before = v.slice(0, caret);
    const slash = /^\/(\w*)$/.exec(before);
    const hash = /(?:^|\s)#([\w./-]*)$/.exec(before);
    if (slash) { palMode = 'slash'; palItems = SLASH.filter((c) => c.startsWith(slash[1])).map((c) => ({ value: '/' + c + ' ', label: '/' + c, hint: t('chat.cmd.' + c) })); }
    else if (hash) { palMode = 'hash'; palItems = wikiPages.filter((p) => p.includes(hash[1])).slice(0, 8).map((p) => ({ value: p, label: '#' + p, hint: t('chat.wikiRef') })); }
    else palItems = [];
    if (!palItems.length) { palette.hidden = true; input.setAttribute('aria-expanded', 'false'); return; }
    palIdx = Math.min(palIdx, palItems.length - 1);
    palette.replaceChildren(...palItems.map((it, i) => h('div.pal-item', { role: 'option', id: 'pal-' + i, 'aria-selected': i === palIdx ? 'true' : 'false', onmousedown: (e) => { e.preventDefault(); palIdx = i; pick(); } },
      h('span.mono', it.label), h('span.faint', it.hint))));
    palette.hidden = false; input.setAttribute('aria-expanded', 'true'); input.setAttribute('aria-activedescendant', 'pal-' + palIdx);
  }
  function pick() {
    const it = palItems[palIdx];
    if (!it) return;
    const caret = input.selectionStart;
    const before = input.value.slice(0, caret);
    const after = input.value.slice(caret);
    input.value = palMode === 'slash' ? it.value + after.trimStart() : before.replace(/#([\w./-]*)$/, '#' + it.value + ' ') + after;
    palette.hidden = true; palIdx = 0; input.focus(); autosize();
  }

  // Slash commands handled by the Console; anything else goes to the agent.
  async function runSlash(text) {
    const [cmd, ...rest] = text.slice(1).split(' ');
    const arg = rest.join(' ').trim();
    switch (cmd) {
      case 'new': startNewChat(null); return true;
      case 'split': api.autoSplit(sess.id, arg || sess.title); return true;
      case 'mode': if (['manual', 'ask', 'auto'].includes(arg)) { api.setSessionProp(sess.id, { mode: arg }); toast(t('mode.saved', { mode: t(`mode.${arg}.name`) })); draw(); drawHead(); } else toast(t('chat.cmdUsage', { u: '/mode manual|ask|auto' })); return true;
      case 'model': modelPicker({ want: arg }); return true;
      case 'ask': { const [m, ...q] = arg.split(' '); modelPicker({ want: m, ask: q.join(' ') }); return true; }
      case 'effort': if (EFFORTS.includes(arg)) { api.setChatMeta(sess.id, { effort: arg }); drawFooter(); toast(t('chat.effortSet', { e: t('chat.effort.' + arg) })); } else toast(t('chat.cmdUsage', { u: '/effort ' + EFFORTS.join('|') })); return true;
      case 'compress': api.compress(sess.id); return true;
      case 'goal': api.setChatMeta(sess.id, { goal: arg }); drawGoal(); toast(arg ? t('chat.goalSet') : t('chat.goalCleared')); return true;
      case 'title': if (arg) { api.renameSession(sess.id, arg); drawHead(); toast(t('chat.renamed')); } return true;
      case 'btw': api.chatSync(sess.id).push({ t: 'note', at: Date.now(), kind: 'btw', text: t('chat.btwAnswer', { q: arg }) }); draw(); return true;
      case 'stop': api.stopStreaming(sess.id); return true;
      case 'who': location.hash = '#/board'; return true;
      case 'wiki': location.hash = '#/wiki'; return true;
      case 'help': input.value = '/'; showPalette(); return true;
      default: return false;
    }
  }

  async function send(how) {
    const v = input.value.trim();
    if (!v && !attachments.length) { input.focus(); return; }
    if (v.startsWith('/') && await runSlash(v)) { input.value = ''; api.setChatMeta(sess.id, { draft: '' }); autosize(); return; }
    api.sendMessage(sess.id, v, how, attachments);
    input.value = ''; attachments = []; drawAtt(); autosize();
  }

  // ---- model switching: pictures, not text (docs/model-switch.md) ----
  // Each model shows a ring for how full this chat's history would be there, the
  // first turn's cost (nothing cached yet) and a red-line badge for another provider.
  const ringFor = (o) => {
    const r = 17; const c = 2 * Math.PI * r;
    const arc = (v, cls) => s('circle', { cx: 22, cy: 22, r, class: 'ctx-fg ' + cls, 'stroke-dasharray': `${Math.min(1, v) * c} ${c}`, transform: 'rotate(-90 22 22)' });
    const tone = (v) => (v > 0.85 ? 'danger' : v > 0.6 ? 'caution' : '');
    // The model in use shows how full it is now; another shows what a switch would send.
    const compact = o.way === 'compact' && !o.current;
    return s('svg', { viewBox: '0 0 44 44', class: 'ms-ring', 'aria-hidden': 'true' },
      s('circle', { cx: 22, cy: 22, r, class: 'ctx-bg' }),
      // Compacting: the full history shows faint, what is sent shows solid.
      compact ? arc(o.fill, 'ms-ghost ' + tone(o.fill)) : null,
      arc(compact ? o.after : o.fill, tone(compact ? o.after : o.fill)),
      s('text', { x: 22, y: 25.5, 'text-anchor': 'middle', class: 'ms-pct' }, Math.round((compact ? o.after : o.fill) * 100) + '%'));
  };

  async function switchTo(id, opt) {
    let o = opt;
    if (!o) {
      try { o = (await api.modelPlan(sess.id)).options.find((x) => x.model.id === id); } catch (e) { toast(String(e.message || e), null, 'deny'); return; }
    }
    if (!o) { toast(t('chat.switch.unavailable', { m: shortModel(id) }), null, 'deny'); return; }
    if (o.current) return;
    let confirm = false;
    if (o.crossProvider) {
      confirm = await confirmRedLine({ title: t('chat.model.changeTitle'), body: t('chat.switch.crossBody', { p: o.model.provider, m: o.model.name }), redLine: 'model', confirmLabel: t('chat.model.change', { m: o.model.name }) });
      if (!confirm) return;
    }
    closeDrawer();
    try {
      await api.switchModel(sess.id, o.model.id, { confirm });
      toast(t('chat.switch.done.' + o.way, { m: o.model.name }));
    } catch (e) { toast(String(e.message || e), null, 'deny'); }
  }

  async function askOnce(o, text) {
    if (!text.trim()) return;
    let confirm = false;
    if (o.crossProvider) {
      confirm = await confirmRedLine({ title: t('chat.ask.crossTitle'), body: t('chat.ask.crossBody', { p: o.model.provider, m: o.model.name }), redLine: 'model', confirmLabel: t('chat.ask.send', { m: o.model.name }) });
      if (!confirm) return;
    }
    closeDrawer();
    try {
      await api.askOnce(sess.id, o.model.id, text.trim(), { confirm });
      toast(t('chat.ask.started', { m: o.model.name }));
    } catch (e) { toast(String(e.message || e), null, 'deny'); }
  }

  async function modelPicker({ want = '', ask = '' } = {}) {
    let plan;
    try { plan = await api.modelPlan(sess.id); } catch (e) { toast(String(e.message || e), null, 'deny'); return; }
    if (!plan.live) { toast(t('chat.switch.notLive'), null, 'deny'); return; }
    const card = (o) => {
      const m = o.model;
      const blocked = o.locked === 'local_only';
      const picked = !!want && (m.id === want || m.name === want || shortModel(m.id) === want);
      const askBox = h('textarea.input.ms-ask-in', { rows: 2, placeholder: t('chat.ask.placeholder'), 'aria-label': t('chat.ask.label', { m: m.name }) }, ask || input.value);
      const askForm = h('form.ms-ask', { hidden: !(picked && ask) || null, onsubmit: (e) => { e.preventDefault(); askOnce(o, askBox.value); } },
        askBox, h('div.row', { style: { gap: '6px' } },
          h('button.btn.sm.primary', { type: 'submit' }, icon('send'), t('chat.ask.send', { m: m.name })),
          h('span.faint.ms-note', t('chat.ask.note'))));
      const badges = [
        o.current ? h('span.tag.route', t('chat.switch.current')) : null,
        !o.current ? h('span.tag', { class: o.way === 'compact' ? 'caution' : 'clear', title: t('chat.switch.way.' + o.way + 'Hint') }, t('chat.switch.way.' + o.way)) : null,
        o.crossProvider && !blocked ? h('span.tag.danger', icon('warn'), t('redlines.model.short')) : null,
        blocked ? h('span.tag.danger', icon('lock'), t('chat.switch.localOnly')) : null,
        m.local ? h('span.tag', t('chat.switch.local')) : null,
      ];
      return h('li.ms-card', { class: [o.current ? 'current' : '', blocked ? 'locked' : '', picked ? 'picked' : ''].filter(Boolean).join(' ') },
        h('div.ms-row',
          h('div.ms-ringbox', { title: t('chat.switch.ringHint', { full: Math.round(o.fill * 100), sent: Math.round(o.after * 100), win: fmtTokens(m.window) }) }, ringFor(o)),
          h('div.ms-main',
            h('div.ms-name', h('strong', m.name), h('span.faint', m.provider)),
            h('div.ms-badges', badges),
            h('div.ms-facts.faint', h('span.tnum', fmtTokens(o.current ? Math.round(o.fill * m.window) : o.carry) + ' / ' + fmtTokens(m.window)), m.windowKnown === false ? h('span', ' ' + t('chat.switch.windowGuess')) : null)),
          h('div.ms-cost', { title: t('chat.switch.costHint') },
            h('strong.tnum', o.current ? '—' : m.priced ? fmtCost(o.firstCost) : m.local ? '$0' : '?'),
            h('span.faint', t('chat.switch.firstTurn')))),
        o.current || blocked ? null : h('div.ms-actions',
          h('button.btn.sm', { type: 'button', class: o.crossProvider ? 'danger' : 'primary', disabled: plan.busy || !canChat || null, title: plan.busy ? t('chat.switch.busy') : null, onclick: () => switchTo(m.id, o) }, icon('gauge'), t('chat.model.change', { m: m.name })),
          h('button.btn.sm.ghost', { type: 'button', onclick: () => { askForm.hidden = !askForm.hidden; if (!askForm.hidden) askBox.focus(); } }, icon('chat'), t('chat.ask.btn'))),
        askForm);
    };
    const st = plan.state || {};
    openDrawer(t('chat.switch.title'), h('div.stack.ms-picker', { style: { padding: '16px' } },
      h('p.faint.ms-lede', t('chat.switch.lede', { brief: fmtTokens(st.brief || 0), recall: fmtTokens(st.recall || 0) })),
      plan.busy ? h('p.callout', icon('clock'), h('div', t('chat.switch.busy'))) : null,
      plan.policy === 'local_only' ? h('p.callout', icon('lock'), h('div', t('chat.switch.policy'))) : null,
      h('ul.ms-list', plan.options.map(card)),
      h('p.faint.ms-note', t('chat.switch.rule'))));
  }

  input.addEventListener('input', () => { api.chatMeta(sess.id).draft = input.value; autosize(); showPalette(); draftHint.textContent = input.value ? t('chat.draftSaved') : ''; });
  input.addEventListener('keydown', (e) => {
    if (!palette.hidden) {
      if (e.key === 'ArrowDown') { e.preventDefault(); palIdx = (palIdx + 1) % palItems.length; showPalette(); return; }
      if (e.key === 'ArrowUp') { e.preventDefault(); palIdx = (palIdx - 1 + palItems.length) % palItems.length; showPalette(); return; }
      if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) { e.preventDefault(); pick(); return; }
      if (e.key === 'Escape') { e.preventDefault(); palette.hidden = true; return; }
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); send(api.isStreaming(sess.id) ? 'queue' : 'send'); }
  });
  input.addEventListener('blur', () => setTimeout(() => (palette.hidden = true), 120));

  const footer = h('div.composer-foot');
  const draftHint = h('span.faint.draft-hint', { 'aria-live': 'polite' }, meta.draft ? t('chat.draftSaved') : '');

  function ctxRing() {
    const m = api.chatMeta(sess.id);
    const pct = Math.min(100, Math.round((m.ctxUsed / m.ctxWindow) * 100));
    const r = 8; const c = 2 * Math.PI * r;
    const ring = s('svg', { viewBox: '0 0 20 20', class: 'ctx-ring', 'aria-hidden': 'true' },
      s('circle', { cx: 10, cy: 10, r, class: 'ctx-bg' }),
      s('circle', { cx: 10, cy: 10, r, class: 'ctx-fg ' + (pct > 85 ? 'danger' : pct > 65 ? 'caution' : ''), 'stroke-dasharray': `${(pct / 100) * c} ${c}`, transform: 'rotate(-90 10 10)' }));
    const b = h('button.chip', { type: 'button', 'aria-haspopup': 'menu', 'aria-label': t('chat.ctx.aria', { pct }), onclick: () => menu(b, [
      { heading: t('chat.ctx.used', { used: fmtTokens(m.ctxUsed), win: fmtTokens(m.ctxWindow) }) },
      { label: t('chat.ctx.compress'), icon: 'compress', onSelect: () => api.compress(sess.id) },
      { label: t('chat.ctx.what'), icon: 'info', onSelect: () => toast(t('chat.ctx.explain')) },
    ], { up: true }) }, ring, h('span.tnum', pct + '%'));
    return b;
  }

  // Wiki pages recalled into this chat, against a visible token budget.
  function recallChip() {
    const r = api.chatMeta(sess.id).recall || { budget: 4000, pages: [], dropped: [] };
    const used = r.pages.reduce((n, x) => n + x.tokens, 0);
    const chip = h('button.chip.recall-chip', { type: 'button', 'aria-haspopup': 'menu', 'aria-label': t('chat.recall.aria', { used: fmtTokens(used), budget: fmtTokens(r.budget) }), class: r.dropped.length ? 'cut' : '',
      onclick: () => menu(chip, [
        { heading: t('chat.recall.title') },
        ...(r.pages.length ? r.pages.map((x) => ({ label: x.path, icon: 'wiki', hint: fmtTokens(x.tokens), onSelect: () => go('wiki') })) : [{ heading: t('chat.recall.none') }]),
        ...(r.dropped.length ? [{ sep: true }, { heading: t('chat.recall.droppedTitle') }, ...r.dropped.map((x) => ({ label: x.path, icon: 'x', hint: fmtTokens(x.tokens), disabled: true }))] : []),
        { sep: true },
        { heading: t('chat.recall.budget', { budget: fmtTokens(r.budget) }) },
      ], { up: true }) },
    icon('wiki'), h('span.tnum', fmtTokens(used) + ' / ' + fmtTokens(r.budget)), r.dropped.length ? h('span.recall-cut', '+' + r.dropped.length) : null);
    return chip;
  }

  function drawFooter() {
    const m = api.chatMeta(sess.id);
    const running = api.isStreaming(sess.id);
    // One chip for the three per-chat settings (model · effort · mode) instead of three.
    const setChip = h('button.chip', { type: 'button', 'aria-haspopup': 'menu', disabled: !canChat || null, 'aria-label': t('chat.settingsAria'), onclick: () => menu(setChip, [
      { heading: t('chat.model.heading') },
      { label: sess.model, checked: true },
      { label: t('chat.switch.open'), icon: 'gauge', onSelect: () => modelPicker() },
      { sep: true },
      { heading: t('chat.effort.heading') },
      ...EFFORTS.map((x) => ({ label: t('chat.effort.' + x), checked: x === m.effort, onSelect: () => { api.setChatMeta(sess.id, { effort: x }); drawFooter(); } })),
      { sep: true },
      { heading: t('mode.title') },
      ...['manual', 'ask', 'auto'].map((v) => ({ label: t(`mode.${v}.name`), checked: v === sess.mode, onSelect: () => { api.setSessionProp(sess.id, { mode: v }); drawFooter(); } })),
    ], { up: true }) },
    lamp(sess.mode === 'auto' ? 'clear' : sess.mode === 'ask' ? 'route' : 'idle'),
    h('span', h('span', sess.model), ' · ', t('chat.effort.' + m.effort), ' · ', t(`mode.${sess.mode}.name`)), icon('down', 'i chip-caret'));
    const primaryHow = running ? 'queue' : 'send';
    const sendMain = h('button.btn.primary.split-l', { type: 'button', disabled: !canChat || null, onclick: () => send(primaryHow) }, icon(running ? 'queue' : 'send'), t(running ? 'chat.queue' : 'chat.send'));
    const sendMore = h('button.btn.primary.split-r', { type: 'button', 'aria-label': t('chat.sendOptions'), 'aria-haspopup': 'menu', disabled: !canChat || null, onclick: () => menu(sendMore, [
      { label: t('chat.how.send'), icon: 'send', hint: 'Enter', onSelect: () => send('send') },
      { label: t('chat.how.queue'), icon: 'queue', hint: t('chat.hint.queue'), onSelect: () => send('queue') },
      { label: t('chat.how.steer'), icon: 'steer', hint: t('chat.hint.steer'), disabled: !running, onSelect: () => send('steer') },
      { label: t('chat.how.interrupt'), icon: 'stop', hint: t('chat.hint.interrupt'), disabled: !running, onSelect: () => send('interrupt') },
    ], { align: 'right', up: true }) }, icon('down'));
    footer.replaceChildren(
      h('div.cf-left',
        h('button.icon-btn', { type: 'button', 'aria-label': t('chat.attach'), title: t('chat.attach'), disabled: !canChat || null, onclick: () => fileIn.click() }, icon('clip')),
        setChip, ctxRing(), recallChip()),
      h('div.cf-right',
        running ? h('button.btn.danger', { type: 'button', onclick: () => api.stopStreaming(sess.id) }, icon('stop'), t('chat.stopReply')) : null,
        h('div.split', sendMain, sendMore)));
  }

  const composer = h('div.composer', { ondragover: (e) => { e.preventDefault(); composer.classList.add('drop'); }, ondragleave: () => composer.classList.remove('drop'),
    ondrop: (e) => { e.preventDefault(); composer.classList.remove('drop'); attachments.push(...[...e.dataTransfer.files].map((f) => f.name)); drawAtt(); } },
  palette, attRow, input, fileIn, footer);

  // ---- header ----
  const head = h('div.chat-head');
  // On small screens the plan is an overlay, so it starts closed there.
  const narrowPlan = matchMedia('(max-width: 1100px)').matches;
  const readPlanOpen = () => { if (narrowPlan) return false; try { return localStorage.getItem('hpk.planOpen') !== '0'; } catch { return true; } };
  let planOpen = readPlanOpen();
  const planRoot = rootOf(sess);
  const hasPlan = () => api.sessionsSync().some((x) => x.parent === planRoot.id);
  function setPlan(open) {
    planOpen = open;
    try { localStorage.setItem('hpk.planOpen', open ? '1' : '0'); } catch { /* ignore */ }
    drawHead(); drawSide(); drawGoal();
  }
  function drawHead() {
    const prof = api.profileOf(api.state().projectId);
    const acc = api.accountInUse('anthropic');
    const moreBtn = h('button.icon-btn', { type: 'button', 'aria-label': t('chat.moreFor', { title: sess.title }), 'aria-haspopup': 'menu', onclick: () => menu(moreBtn, [
      // Details that used to crowd the header live here now.
      { heading: [prof ? prof.name : null, acc ? acc.label : null, fmtTokens(sess.tokens) + ' · ' + fmtCost(sess.cost)].filter(Boolean).join(' · ') },
      { sep: true },
      { heading: t('chat.display.heading') },
      ...['worklog', 'stream', 'answers'].map((v) => ({ label: t('chat.display.' + v), checked: v === view, onSelect: () => { view = v; writeView(v); draw(); } })),
      { sep: true },
      ...sessionMenuItems(sess.id, rebuild),
    ], { align: 'right' }) }, icon('more'));
    const titleBtn = h('button.chat-title', { type: 'button', title: t('chat.menu.rename'), onclick: () => renameDrawer(sess.id, rebuild) }, sess.title);
    const main = sess.parent ? planRoot : null;
    const m = api.chatMeta(sess.id);
    // replaceChildren would print "null", so drop empty slots first.
    head.replaceChildren(...[
      h('span.chat-av', { class: 'st-' + (main ? sess.status : treeStatus(sess)), 'aria-hidden': 'true' }, icon(main ? 'git' : 'chat')),
      h('div.ch-titles',
        // A sub-task always shows the way back to its main chat.
        main ? h('a.crumb-back', { href: '#/sessions/' + main.id }, icon('back'), main.title) : null,
        h('h1', {}, main ? h('span.crumb-kind', t('chat.subtasks.one') + ' · ') : null, titleBtn)),
      sessionStatus(sess),
      h('span', { style: { flex: 1 } }),
      h('span.model-pill.hide-sm', lamp('clear'), h('span', sess.model), h('span.faint', t('chat.effort.' + (m.effort || 'medium')))),
      hasPlan() ? h('button.btn.sm.ghost.plan-btn', { type: 'button', 'aria-pressed': String(planOpen), onclick: () => setPlan(!planOpen) }, icon('layers'), t('plan.title')) : null,
      moreBtn,
    ].filter(Boolean));
  }

  // Plan panel on the right: the sub-tasks of this chat's main chat, as boxes you can act on.
  const side = h('aside.plan-side', { 'aria-label': t('plan.title') });
  function drawSide() {
    const show = hasPlan() && planOpen;
    page.classList.toggle('plan-open', show);
    if (!show) { side.replaceChildren(); return; }
    const g = api.chatMeta(planRoot.id).goal;
    side.replaceChildren(...[
      h('div.ps-head',
        h('div.ps-titles', h('h2', planRoot.title), h('p.ps-line', planHeadline(planRoot))),
        h('button.icon-btn', { type: 'button', 'aria-label': t('plan.hide'), onclick: () => setPlan(false) }, icon('x'))),
      g ? h('p.ps-goal', icon('flag'), h('span', g)) : null,
      h('div.ps-body', planGraph(planRoot, { current: sess.id })),
    ].filter(Boolean));
  }
  const goalBox = h('div');
  function drawGoal() {
    const g = api.chatMeta(sess.id).goal;
    // With the plan panel open, the goal lives there.
    goalBox.replaceChildren(g && !(hasPlan() && planOpen) ? h('div.goal', icon('flag'), h('span', h('b', t('chat.goal') + ': '), g),
      h('button.btn.sm.ghost', { type: 'button', onclick: () => { api.setChatMeta(sess.id, { goal: '' }); drawGoal(); } }, t('chat.goalClear'))) : '');
  }
  function rebuild() { location.hash = '#/sessions/' + sess.id; dispatchEvent(new HashChangeEvent('hashchange')); }

  drawHead(); drawGoal(); drawSide(); draw(); autosize();
  // When the agent splits work, refresh the header and the plan (debounced).
  let sessTimer = null;
  const onSessions = () => { clearTimeout(sessTimer); sessTimer = setTimeout(() => { drawHead(); drawSide(); drawGoal(); draw(); }, 250); };
  bus.addEventListener('sessions', onSessions);

  const onChat = (e) => { if (e.detail === sess.id) draw(); };
  const onStream = (e) => {
    if (e.detail !== sess.id) return;
    const items = api.chatSync(sess.id);
    const last = items[items.length - 1];
    const node = [...log.querySelectorAll('.msg')].pop()?.querySelector('.m-body');
    if (node && last && last.t === 'msg') { node.replaceChildren(...md(last.text)); log.scrollTop = log.scrollHeight; }
  };
  const onMeta = (e) => { if (e.detail === sess.id) { drawFooter(); drawHead(); } else onSessions(); };
  const onKey = (e) => {
    if (!e.altKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return;
    const ids = [...document.querySelectorAll('#railChats .chat-pick')].map((a) => a.getAttribute('href'));
    const i = ids.indexOf('#/sessions/' + sess.id);
    const next = ids[i + (e.key === 'ArrowDown' ? 1 : -1)];
    if (next) { e.preventDefault(); location.hash = next; }
  };
  bus.addEventListener('chat', onChat);
  bus.addEventListener('chat-stream', onStream);
  bus.addEventListener('chat-meta', onMeta);
  document.addEventListener('keydown', onKey);

  page.append(h('section.chat', head, goalBox, log, composer), side);
  setTimeout(() => { log.scrollTop = log.scrollHeight; if (canChat) input.focus({ preventScroll: true }); }, 0);
  return { el: page, cleanup: () => { bus.removeEventListener('chat', onChat); bus.removeEventListener('chat-stream', onStream); bus.removeEventListener('chat-meta', onMeta); bus.removeEventListener('sessions', onSessions); clearTimeout(sessTimer); document.removeEventListener('keydown', onKey); } };
}

// ------------------------------------------------------------ new chat ----
// One question: what do you want done? The agent plans and splits it into sub-tasks.
function newChatView({ go }) {
  const p = api.project();
  const prof = api.profileOf(p.id);
  const eff = Object.fromEntries(api.effective(p.id).map((r) => [r.key, r]));
  const st = { mode: eff.mode.value, model: eff.model.value, effort: 'medium', worktree: true };
  const tpl = eff.titleTemplate.value || '{name}';
  const first = h('textarea.input.nc-first#ncFirst', { rows: 5, placeholder: t('chat.newc.firstPh'), 'aria-label': t('chat.newc.first') });
  const verIn = h('input.input#ncVer', { placeholder: '1.5.0', inputmode: 'decimal', style: { maxWidth: '110px' } });
  const nameIn = h('input.input#ncName', { placeholder: t('chat.newc.nameAuto') });
  const src = (r) => h('span.tag.layer', t('setup.layer.' + r.from));
  const starters = ['plan', 'review', 'yesterday', 'tests'].map((k) => h('button.chip', { type: 'button', onclick: () => { first.value = t('chat.newc.starter.' + k + '.text'); first.focus(); } }, icon('sparkle'), t('chat.newc.starter.' + k + '.label')));

  let busy = false;
  const start = () => {
    if (busy) return;
    const text = first.value.trim();
    if (!text) { first.focus(); first.setAttribute('aria-invalid', 'true'); return; }
    // Title: from the template if a name was given, otherwise from the first message.
    const name = nameIn.value.trim() || text.split(/[.\n]/)[0].slice(0, 48);
    // Without a version, the template prefix ("Version …") would read oddly: use the name alone.
    const title = tpl.includes('{version}') && !verIn.value.trim()
      ? name
      : tpl.replace('{version}', verIn.value.trim()).replace('{name}', name).replace(/\s+/g, ' ').trim();
    // Demo data returns an id; hpk serve returns a promise of one.
    // Opening a session in Hermes can take a while (it starts hermes-acp): say so.
    busy = true; first.disabled = true; startBtn.disabled = true;
    startBtn.replaceChildren(icon('clock'), t('chat.newc.starting'));
    const done = () => { busy = false; first.disabled = false; startBtn.disabled = false; startBtn.replaceChildren(icon('send'), t('chat.newc.start')); };
    Promise.resolve(api.newSession({ title, parent: null, mode: st.mode, model: st.model, effort: st.effort, worktree: st.worktree, firstMessage: text }))
      .then((id) => { done(); if (id) go('sessions/' + id); })
      .catch((err) => { done(); toast(String(err.message || err), null, 'deny'); });
  };
  const startBtn = h('button.btn.primary', { type: 'button', onclick: start }, icon('send'), t('chat.newc.start'));
  first.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); start(); } });

  const sel = (id, opts, val, on) => h('select.input#' + id, { 'data-glyph': { ncModel: 'model', ncEffort: 'effort' }[id], onchange: (e) => on(e.target.value) }, opts.map(([v, l]) => h('option', { value: v, selected: v === val || null }, l)));

  const el = h('div.page.newchat',
    h('div.nc-hero',
      h('h1', t('chat.newc.ask')),
      h('p.lede', t('chat.newc.lede')),
      h('div.nc-box', first,
        h('div.nc-bar', h('span.faint.nc-note', t('chat.newc.splitNote')),
          startBtn)),
      h('div.row.nc-starters', starters),
      h('details.nc-more',
        h('summary', icon('chevron', 'chev'), t('chat.newc.moreOptions'), h('span.faint', [st.model || prof.model, t(`mode.${st.mode}.name`), prof.name].filter(Boolean).join(' · '))),
        h('div.nc-grid',
          h('div.field', h('span.label', t('chat.titleLabel')),
            h('div.row', tpl.includes('{version}') ? h('div', h('label.sr-only', { for: 'ncVer' }, t('chat.newc.version')), verIn) : null, h('div', { style: { flex: 1, minWidth: '180px' } }, h('label.sr-only', { for: 'ncName' }, t('chat.newc.name')), nameIn)),
            h('span.hint', t('chat.newc.template', { tpl }))),
          h('div.field', h('label', { for: 'ncMode' }, t('setup.key.mode'), ' ', src(eff.mode)), sel('ncMode', ['manual', 'ask', 'auto'].map((v) => [v, t(`mode.${v}.name`)]), st.mode, (v) => (st.mode = v))),
          h('div.field', h('label', { for: 'ncModel' }, t('setup.key.model'), ' ', src(eff.model)), sel('ncModel', MODELS.map((v) => [v, v]), st.model, (v) => (st.model = v))),
          h('div.field', h('label', { for: 'ncEffort' }, t('chat.effort.heading')), sel('ncEffort', EFFORTS.map((v) => [v, t('chat.effort.' + v)]), st.effort, (v) => (st.effort = v))),
          h('label.switch', h('input', { type: 'checkbox', checked: true, onchange: (e) => (st.worktree = e.target.checked) }), t('chat.newc.worktree'))))));
  setTimeout(() => first.focus(), 30);
  return { el };
}

// Team mode: choose who else may read this chat. The board shows every chat;
// reading one needs its owner's share.
async function shareDrawer(sid, rerender) {
  const x = api.session(sid);
  const res = await fetch('/api/team').then((r) => r.json()).catch(() => ({ members: [] }));
  const me = api.me();
  const shared = new Set((x.sharedWith || []).map((v) => v.toLowerCase()));
  const boxes = (res.members || []).filter((m) => m.id.toLowerCase() !== String(x.owner || me.id).toLowerCase()).map((m) =>
    h('label.switch', h('input', { type: 'checkbox', value: m.id, checked: shared.has(m.id.toLowerCase()) || null }), m.name + (m.name !== m.id ? ' · ' + m.id : '')));
  openDrawer(t('chat.shareTitle', { title: x.title }), h('div.stack', { style: { padding: '16px' } },
    h('p.muted', t('chat.shareBody')),
    boxes.length ? boxes : h('p.faint', t('chat.shareNobody'))),
  h('button.btn.primary', { type: 'button', onclick: async () => {
    const withIds = boxes.map((b) => b.querySelector('input')).filter((i) => i.checked).map((i) => i.value);
    try {
      const r = await fetch('/api/sessions/' + encodeURIComponent(sid) + '/share', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HPK': '1' }, body: JSON.stringify({ project: api.project().id, with: withIds }) });
      const out = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(out.error || r.statusText);
      x.sharedWith = withIds;
      closeDrawer(); toast(t('chat.shared')); if (rerender) rerender();
    } catch (err) { toast(err.message); }
  } }, t('common.save')));
}
