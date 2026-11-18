// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Git advice from fixed rules: no model, no network, 0 tokens. Given the shape of
// the repository (commits, branches, remote positions, PRs, stashes), each rule
// says when to rebase, merge, push, pull, squash, split or clean up, and why,
// with the numbers it used. The same rules run on the real repository once the
// Git screen is wired to it (docs/plans/git-sync.md §14).
//
// A repo is { commits: [{id, msg, at, parents, files: [[path, +, −]]}],
//             branches: [{name, tip, remote?, base?, owner?, pr?: {ci, draft}}],
//             head, stashes: [{id, msg, at}], me }.

export const LIMITS = {
  staleDays: 7, // a branch with no commit for this long
  stashDays: 3, // a stash older than this is probably forgotten
  bigCommits: 10, // a branch with more commits than this is hard to review
  bigLines: 400, // … or more changed lines
  wip: /^(wip|tmp|temp|todo|fixup!|squash!|amend!|\.|x+|asdf|test)\b/i,
};

const DAY = 86_400_000;

function index(repo) {
  const byId = new Map(repo.commits.map((c) => [c.id, c]));
  const reach = (id, out = new Set()) => {
    const stack = [id];
    while (stack.length) {
      const x = stack.pop();
      if (!x || out.has(x)) continue;
      out.add(x);
      (byId.get(x)?.parents || []).forEach((p) => stack.push(p));
    }
    return out;
  };
  // commits in a but not in b, newest first (the order of repo.commits)
  const only = (a, b) => { const A = reach(a); const B = reach(b); return repo.commits.filter((c) => A.has(c.id) && !B.has(c.id)); };
  return { byId, reach, only };
}

const lines = (cs) => cs.reduce((n, c) => n + (c.files || []).reduce((m, [, a, d]) => m + a + d, 0), 0);
const files = (cs) => new Set(cs.flatMap((c) => (c.files || []).map(([f]) => f)));
const short = (b) => b.replace(/^hpk\/[^/]+\//, '');

// advise returns advice, most urgent first. now is injectable for tests.
export function advise(repo, now = Date.now()) {
  const { only } = index(repo);
  const base = repo.branches.find((b) => b.base) || repo.branches.find((b) => b.name === 'main');
  const out = [];
  const add = (a) => out.push(a);
  if (!base) return out;

  for (const b of repo.branches) {
    const name = short(b.name);
    const mine = !b.owner || b.owner === repo.me || b.name === repo.head;

    if (b.base) {
      // R11: commits made straight on the base branch, not pushed
      const local = b.remote ? only(b.tip, b.remote) : [];
      if (local.length) add({ rule: 'R11', level: 'caution', branch: b.name, title: ['onBase', { n: local.length, b: name }], why: ['onBaseWhy'], actions: [['branchFrom', b.name]] });
      continue;
    }

    const own = only(b.tip, base.tip); // the branch's own commits
    const behind = only(base.tip, b.tip); // base commits the branch does not have

    // R6: everything is already in the base ⇒ merged, can go
    if (own.length === 0) {
      add({ rule: 'R6', level: 'info', branch: b.name, title: ['merged', { b: name }], why: ['mergedWhy'], actions: [['delete', b.name]] });
      continue;
    }

    // R5: the PR's CI is failing
    if (b.pr && b.pr.ci === 'fail') add({ rule: 'R5', level: 'danger', branch: b.name, title: ['ciFail', { b: name, n: b.pr.n }], why: ['ciFailWhy'], actions: [['openPR', b.name]] });

    // R1: the base moved on; rebase or merge, and will it conflict?
    if (behind.length) {
      const clash = [...files(behind)].filter((f) => files(own).has(f));
      const shared = !!b.pr || (b.remote && !mine); // others may build on it
      const how = shared ? 'merge' : 'rebase';
      add({ rule: 'R1', level: clash.length ? 'caution' : 'info', branch: b.name,
        title: [how === 'rebase' ? 'behindRebase' : 'behindMerge', { b: name, n: behind.length, base: short(base.name) }],
        why: [clash.length ? 'behindClash' : 'behindClean', { files: clash.join(', '), reason: shared ? 'shared' : b.remote ? 'pushedSolo' : 'local' }],
        actions: how === 'rebase' ? [['rebase', b.name, base.name], ['merge', base.name, b.name]] : [['merge', base.name, b.name], ['rebase', b.name, base.name]] });
    }

    // R2: work-in-progress commits; tidy before review
    const wip = own.filter((c) => LIMITS.wip.test(c.msg.trim()) || c.msg.trim().length < 6);
    if (wip.length && (!b.pr || b.pr.draft)) add({ rule: 'R2', level: 'info', branch: b.name, title: ['wip', { b: name, n: wip.length }], why: ['wipWhy', { msgs: wip.map((c) => '"' + c.msg + '"').join(', ') }], actions: [['interactive', b.name, base.name]] });

    // R3 / R4: compared with its remote copy
    if (b.remote && b.remote !== b.tip) {
      const ahead = only(b.tip, b.remote).length;
      const behindRemote = only(b.remote, b.tip).length;
      if (behindRemote) add({ rule: 'R4', level: 'caution', branch: b.name, title: ['behindRemote', { b: name, n: behindRemote }], why: [ahead ? 'divergedWhy' : 'behindRemoteWhy', { ahead }], actions: [['pull', b.name]] });
      else if (ahead) add({ rule: 'R3', level: 'info', branch: b.name, title: ['ahead', { b: name, n: ahead }], why: ['aheadWhy'], actions: [['push', b.name]] });
    } else if (!b.remote && mine) {
      add({ rule: 'R3', level: 'info', branch: b.name, title: ['notPushed', { b: name, n: own.length }], why: ['notPushedWhy'], actions: [['push', b.name]] });
    }

    // R10: too big to review in one go
    const ln = lines(own);
    if (own.length > LIMITS.bigCommits || ln > LIMITS.bigLines) add({ rule: 'R10', level: 'info', branch: b.name, title: ['big', { b: name, n: own.length, lines: ln }], why: ['bigWhy', { max: LIMITS.bigLines }], actions: [['branchFrom', b.name]] });

    // R7: nothing new for a long time
    const last = Math.max(...own.map((c) => c.at));
    const days = Math.floor((now - last) / DAY);
    if (days >= LIMITS.staleDays) add({ rule: 'R7', level: 'info', branch: b.name, title: ['stale', { b: name, d: days }], why: ['staleWhy'], actions: [['checkout', b.name]] });
  }

  // R8: two branches change the same files (someone else's, or two of yours)
  const open = repo.branches.filter((b) => !b.base && only(b.tip, base.tip).length);
  for (let i = 0; i < open.length; i++) {
    for (let j = i + 1; j < open.length; j++) {
      const a = open[i], b = open[j];
      const both = [...files(only(a.tip, base.tip))].filter((f) => files(only(b.tip, base.tip)).has(f));
      if (both.length) add({ rule: 'R8', level: 'caution', branch: a.name, title: ['overlap', { a: short(a.name), b: short(b.name) }], why: ['overlapWhy', { files: both.join(', ') }], actions: [['checkout', b.name]] });
    }
  }

  // R9: forgotten stashes
  for (const st of repo.stashes || []) {
    const days = Math.floor((now - st.at) / DAY);
    if (days >= LIMITS.stashDays) add({ rule: 'R9', level: 'info', title: ['oldStash', { id: st.id, d: days }], why: ['oldStashWhy', { m: st.msg }], actions: [['pop', st.id]] });
  }

  const rank = { danger: 0, caution: 1, info: 2 };
  return out.sort((x, y) => rank[x.level] - rank[y.level]);
}
