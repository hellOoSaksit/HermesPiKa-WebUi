// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package browser

// Scripts run in the page. They read the DOM only; values of fields hpk filled
// from the vault (data-hpk-secret) and of password fields are never read out.

// snapshotJS numbers the visible interactive elements (data-hpk-ref="eN") and
// returns them with headings and text, as lines the agent can act on.
const snapshotJS = `(() => {
  document.querySelectorAll('[data-hpk-ref]').forEach(e => e.removeAttribute('data-hpk-ref'));
  const vis = (e) => { const r = e.getBoundingClientRect(); if (r.width < 2 || r.height < 2) return false; const s = getComputedStyle(e); return s.visibility !== 'hidden' && s.display !== 'none' && s.opacity !== '0'; };
  const clean = (s, n = 120) => (s || '').replace(/\s+/g, ' ').trim().slice(0, n);
  const label = (e) => {
    const id = e.getAttribute('aria-labelledby');
    const lab = id ? (document.getElementById(id)?.innerText) : (e.labels && e.labels[0] ? e.labels[0].innerText : '');
    return clean(e.getAttribute('aria-label') || lab || (e.tagName === 'INPUT' ? '' : e.innerText) || e.getAttribute('placeholder') || e.getAttribute('title') || e.getAttribute('alt') || e.getAttribute('name') || (e.type === 'submit' || e.type === 'button' ? e.value : ''));
  };
  const role = (e) => {
    const r = e.getAttribute('role'); if (r) return r;
    const t = e.tagName.toLowerCase();
    if (t === 'a') return 'link';
    if (t === 'input') { const ty = (e.type || 'text').toLowerCase(); return ({ checkbox: 'checkbox', radio: 'radio', submit: 'button', button: 'button', reset: 'button', range: 'slider', file: 'file' })[ty] || (ty === 'password' ? 'password' : 'textbox'); }
    if (t === 'textarea') return 'textbox';
    if (t === 'select') return 'select';
    if (e.isContentEditable) return 'textbox';
    return t;
  };
  const sel = 'a[href],button,input:not([type=hidden]),select,textarea,summary,[role=button],[role=link],[role=checkbox],[role=tab],[role=menuitem],[role=option],[role=switch],[role=combobox],[contenteditable=""],[contenteditable=true]';
  const lines = []; let n = 0; let more = false;
  const filled = !!document.querySelector('[data-hpk-secret]');
  const walker = document.createTreeWalker(document.body || document.documentElement, NodeFilter.SHOW_ELEMENT);
  const seenText = new Set();
  for (let e = walker.currentNode; e; e = walker.nextNode()) {
    if (lines.length > 450) { more = true; break; }
    const t = e.tagName;
    if (t === 'SCRIPT' || t === 'STYLE' || t === 'NOSCRIPT' || t === 'SVG') continue;
    if (e.matches(sel)) {
      if (!vis(e) || e.closest('[aria-hidden=true]')) continue;
      const r = e.getBoundingClientRect();
      if (r.top > innerHeight * 3) { more = true; continue; }
      const ref = 'e' + (++n); e.setAttribute('data-hpk-ref', ref);
      const ro = role(e); let extra = '';
      const sec = e.getAttribute('data-hpk-secret');
      if (sec) extra = ' = {{secret:' + sec.replace(/:value$/, '').replace(/:user$/, '.user') + '}}';
      else if (ro === 'password') extra = e.value ? ' = (filled)' : ' (empty)';
      else if ((ro === 'textbox' || ro === 'combobox') && filled) extra = e.value ? ' (filled)' : ' (empty)';
      else if (ro === 'textbox' || ro === 'combobox') extra = e.value ? ' = ' + JSON.stringify(clean(e.value, 80)) : ' (empty)';
      else if (ro === 'checkbox' || ro === 'radio' || ro === 'switch') extra = (e.checked || e.getAttribute('aria-checked') === 'true') ? ' [x]' : ' [ ]';
      else if (ro === 'select') extra = ' = ' + JSON.stringify(clean(e.options[e.selectedIndex]?.text || '', 60));
      if (t === 'A') { const h = e.getAttribute('href') || ''; if (h && !h.startsWith('javascript')) extra += ' -> ' + clean(h, 100); }
      if (e.disabled) extra += ' (disabled)';
      lines.push('[' + ref + '] ' + ro + ' ' + JSON.stringify(label(e)) + extra);
      continue;
    }
    if (/^H[1-4]$/.test(t) && vis(e)) { const s = clean(e.innerText, 140); if (s) lines.push('#'.repeat(+t[1]) + ' ' + s); continue; }
    if ((t === 'P' || t === 'LI' || t === 'TD' || t === 'LABEL' || t === 'SPAN' || t === 'DIV') && e.childElementCount === 0 && vis(e)) {
      const s = clean(e.innerText, 200);
      if (s.length > 2 && !seenText.has(s)) { seenText.add(s); lines.push('  ' + s); }
    }
  }
  return { URL: location.href, Title: document.title, Text: lines.join('\n'), Count: n, More: more || (document.documentElement.scrollHeight > scrollY + innerHeight * 3) };
})()`

// findJS: mode (ref | text | focus) and key; returns the centre of the element.
const findJS = `(() => {
  const mode = %q, key = %q;
  let e = null;
  if (mode === 'ref' || mode === 'focus') e = document.querySelector('[data-hpk-ref="' + key + '"]');
  else {
    const k = key.toLowerCase();
    const all = [...document.querySelectorAll('a,button,input[type=submit],input[type=button],[role=button],[role=link],[role=tab],[role=menuitem],summary,label')];
    e = all.find(x => (x.innerText || x.value || x.getAttribute('aria-label') || '').trim().toLowerCase() === k)
     || all.find(x => (x.innerText || x.value || x.getAttribute('aria-label') || '').toLowerCase().includes(k));
  }
  if (!e) return { Err: mode === 'text' ? 'nothing clickable says ' + JSON.stringify(key) : 'no element ' + key + ' (take a new snapshot: refs change when the page changes)' };
  e.scrollIntoView({ block: 'center', inline: 'center' });
  const r = e.getBoundingClientRect();
  const editable = e.isContentEditable || e.tagName === 'TEXTAREA' || (e.tagName === 'INPUT' && !['checkbox','radio','submit','button','reset','file','range','color','image'].includes((e.type || 'text').toLowerCase()));
  if (mode === 'focus') {
    e.focus();
    if (editable && e.select) { try { e.select(); } catch (_) {} }
    else if (e.isContentEditable) { const s = getSelection(); s.selectAllChildren(e); }
  }
  const name = (e.getAttribute('aria-label') || (e.labels && e.labels[0] && e.labels[0].innerText) || e.getAttribute('placeholder') || e.innerText || e.value || e.getAttribute('name') || e.tagName).replace(/\s+/g, ' ').trim().slice(0, 60);
  const lab = (e.getAttribute('data-hpk-ref') ? '[' + e.getAttribute('data-hpk-ref') + '] ' : '') + (e.type === 'password' ? 'password field' : name);
  return { X: r.left + r.width / 2, Y: r.top + r.height / 2, Label: lab, Editable: editable };
})()`

// fillJS sets a secret into one field, in a single step inside the page:
// args ref, host, protocol, part ("value" | "user"), marker, value (JSON).
// The password goes only into a password field; the user name only into a text
// or e-mail field of a form that has a password field (or autocomplete=username).
const fillJS = `(() => {
  const ref = %q, host = %q, proto = %q, part = %q, marker = %q, v = %s;
  if (location.hostname !== host || location.protocol !== proto) return { OK: false, Err: 'the page changed before the fill; take a new snapshot' };
  const e = document.querySelector('[data-hpk-ref="' + ref + '"]');
  if (!e) return { OK: false, Err: 'no element ' + ref + ' (take a new snapshot)' };
  if (e.tagName !== 'INPUT' || e.disabled || e.readOnly) return { OK: false, Err: 'secrets go only into input fields' };
  const ty = (e.getAttribute('type') || 'text').toLowerCase();
  if (part === 'value' && ty !== 'password') return { OK: false, Err: 'a password goes only into a password field' };
  if (part === 'user') {
    if (!['text', 'email', 'tel', ''].includes(ty)) return { OK: false, Err: 'a user name goes only into a text or e-mail field' };
    const ac = (e.getAttribute('autocomplete') || '').toLowerCase();
    const scope = e.form || e.closest('form,[role=form],main,body');
    if (!ac.includes('username') && !(scope && scope.querySelector('input[type=password]'))) return { OK: false, Err: 'a user name goes only into a sign-in form (one with a password field)' };
  }
  e.setAttribute('data-hpk-secret', marker);
  e.style.setProperty('-webkit-text-security', 'disc', 'important');
  e.scrollIntoView({ block: 'center' });
  e.focus();
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
  set.call(e, v);
  e.dispatchEvent(new Event('input', { bubbles: true }));
  e.dispatchEvent(new Event('change', { bubbles: true }));
  const r = e.getBoundingClientRect();
  const name = (e.getAttribute('aria-label') || (e.labels && e.labels[0] && e.labels[0].innerText) || e.getAttribute('placeholder') || e.getAttribute('name') || 'field').replace(/\s+/g, ' ').trim().slice(0, 60);
  return { OK: true, Label: '[' + ref + '] ' + (ty === 'password' ? 'password field' : name), X: r.left + r.width / 2, Y: r.top + r.height / 2 };
})()`

// pageHasFill: a page where hpk filled a secret answers no text questions
// (wait/click by text could test the value one guess at a time).
const pageHasFill = `!!document.querySelector('[data-hpk-secret]')`
