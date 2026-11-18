// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// node --test tools/  — the Git advice rules (assets/console/js/gitadvice.js).
import test from 'node:test';
import assert from 'node:assert/strict';
import { advise } from '../assets/console/js/gitadvice.js';

const now = Date.UTC(2026, 10, 18);
const ago = (d) => now - d * 86_400_000;
const c = (id, msg, days, parents, files = []) => ({ id, msg, at: ago(days), parents, files });
const rules = (a) => a.map((x) => x.rule + ':' + x.title[0]).sort();

test('behind the base, only local: rebase; conflict expected on shared files', () => {
  const repo = {
    commits: [c('m2', 'Fix routes', 0, ['m1'], [['api/routes.ts', 3, 1]]), c('f1', 'Add export', 0, ['m1'], [['api/routes.ts', 5, 0]]), c('m1', 'Start', 2, [])],
    branches: [{ name: 'main', tip: 'm2', remote: 'm2', base: true }, { name: 'feat', tip: 'f1' }], head: 'feat', stashes: [], me: 'max',
  };
  const a = advise(repo, now);
  const r1 = a.find((x) => x.rule === 'R1');
  assert.equal(r1.title[0], 'behindRebase');
  assert.equal(r1.why[0], 'behindClash');
  assert.match(r1.why[1].files, /api\/routes\.ts/);
  assert.deepEqual(r1.actions[0], ['rebase', 'feat', 'main']);
  assert.ok(a.some((x) => x.rule === 'R3' && x.title[0] === 'notPushed'));
});

test('behind the base with an open PR: merge instead of rewriting', () => {
  const repo = {
    commits: [c('m2', 'Fix', 0, ['m1']), c('f1', 'Add pricing', 0, ['m1'], [['api/pricing.ts', 9, 0]]), c('m1', 'Start', 2, [])],
    branches: [{ name: 'main', tip: 'm2', base: true }, { name: 'feat', tip: 'f1', remote: 'f1', pr: { n: 3, ci: 'fail' } }], head: 'feat', stashes: [], me: 'max',
  };
  const a = advise(repo, now);
  assert.equal(a[0].rule, 'R5'); // failing CI first
  const r1 = a.find((x) => x.rule === 'R1');
  assert.equal(r1.title[0], 'behindMerge');
  assert.equal(r1.why[0], 'behindClean');
  assert.equal(r1.why[1].reason, 'shared');
});

test('merged, wip, ahead, stale, big, overlap, old stash, commits on main', () => {
  const big = Array.from({ length: 12 }, (_, i) => [`f${i}.ts`, 40, 0]);
  const repo = {
    commits: [
      c('l1', 'hotfix', 0, ['m1']),
      c('w2', 'wip', 9, ['w1'], [['a.ts', 1, 0]]), c('w1', 'Add a', 9, ['m1'], [['a.ts', 2, 0], ...big]),
      c('o1', 'Change a', 0, ['m1'], [['a.ts', 1, 1]]),
      c('m1', 'Start', 20, []),
    ],
    branches: [
      { name: 'main', tip: 'l1', remote: 'm1', base: true },
      { name: 'done', tip: 'm1', remote: 'm1' },
      { name: 'old', tip: 'w2', remote: 'w1' },
      { name: 'other', tip: 'o1', remote: 'o1', owner: 'nia' },
    ],
    head: 'old', stashes: [{ id: 'stash@{0}', msg: 'try', at: ago(5) }], me: 'max',
  };
  const got = rules(advise(repo, now));
  for (const want of ['R11:onBase', 'R6:merged', 'R2:wip', 'R3:ahead', 'R7:stale', 'R10:big', 'R8:overlap', 'R9:oldStash']) {
    assert.ok(got.includes(want), want + ' missing in ' + got.join(' '));
  }
});

test('a clean repo gives no advice', () => {
  const repo = { commits: [c('m1', 'Start', 0, [])], branches: [{ name: 'main', tip: 'm1', remote: 'm1', base: true }], head: 'main', stashes: [], me: 'max' };
  assert.deepEqual(advise(repo, now), []);
});
