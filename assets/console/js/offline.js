// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// The offline page (served by the service worker when hpk is not running):
// ask /api/health every few seconds and go back to the Console once hpk answers.
(function () {
  var wait = document.getElementById('wait');
  var tries = 0;
  function check() {
    tries++;
    fetch('/api/health', { cache: 'no-store' }).then(function (r) { return r.ok ? r.json() : null; }).then(function (j) {
      if (j && j.status === 'ok') location.replace('/#/overview');
      else later();
    }).catch(later);
  }
  function later() {
    if (wait) wait.textContent = 'Checking again… (' + tries + ')';
    setTimeout(check, Math.min(15000, 2000 + tries * 1000));
  }
  check();
})();
