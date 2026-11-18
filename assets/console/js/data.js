// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Data layer for HPK Console.
// Today it serves fictional sample data from memory. When `hpk serve` exists,
// replace the bodies of the `api` functions with fetch() calls to its JSON
// endpoints and push live updates through `bus` from its SSE stream.
// Every name, path and number below is invented.

const now = Date.now();
const ago = (min) => now - min * 60_000;

export const bus = new EventTarget();
const emit = (type, detail) => bus.dispatchEvent(new CustomEvent(type, { detail }));

const people = {
  mali: { id: 'mali', name: 'Mali S.', initials: 'MS', role: 'owner', tone: '' },
  ken: { id: 'ken', name: 'Ken T.', initials: 'KT', role: 'maintainer', tone: 'a2' },
  ploy: { id: 'ploy', name: 'Ploy W.', initials: 'PW', role: 'member', tone: 'a3' },
  arun: { id: 'arun', name: 'Arun K.', initials: 'AK', role: 'viewer', tone: 'a4' },
};

// ---- Process → Profile → Project --------------------------------------------
// Process = how work flows (modes, gates, red lines, board, jobs). Reusable.
// Profile = who the agent is (models, AI accounts, skills, personality). Uses one Process.
// Project = what it works on (repo, wiki, privacy, people). Uses one Profile; personal or team.
// Effective value = Project override ?? Profile ?? Process. Red lines add up across layers.
const processes = [
  { id: 'team-delivery', name: 'Team delivery', desc: 'Reviewed work for shared repositories: Ask by default, every push and delete held, board kept in the repo.',
    settings: { mode: 'ask', autoMax: 120, redLines: ['Run make release-check before any merge'], board: 'markdown', jobs: ['nightly', 'weekly', 'digest'], titleTemplate: 'Version {version} {name}', wikiReview: 'core', budgetDaily: 12, maxChildren: 4 } },
  { id: 'solo-fast', name: 'Solo fast', desc: 'For your own side projects: Auto allowed for longer, fewer jobs, board kept by hpk.',
    settings: { mode: 'auto', autoMax: 480, redLines: [], board: 'hpk', jobs: ['nightly'], titleTemplate: '{name}', wikiReview: 'core', budgetDaily: 3, maxChildren: 2 } },
  { id: 'research', name: 'Research & writing', desc: 'Reading and writing knowledge, almost no code: everything into the wiki, all drafts reviewed.',
    settings: { mode: 'ask', autoMax: 60, redLines: ['Never publish outside the wiki'], board: 'none', jobs: ['nightly', 'weekly'], titleTemplate: '{name}', wikiReview: 'all', budgetDaily: 5, maxChildren: 2 } },
];
const profiles = [
  { id: 'senior-coder', name: 'Senior coder', process: 'team-delivery', hermesProfile: 'senior-coder',
    settings: { model: 'sonnet', designModel: 'opus', provider: 'anthropic', accounts: ['acc-1', 'acc-2', 'acc-3'], skills: 43, personality: 'direct', toolsets: 'coordinator', language: 'en' } },
  { id: 'quick-helper', name: 'Quick helper', process: 'solo-fast', hermesProfile: 'quick-helper',
    settings: { model: 'haiku', designModel: 'sonnet', provider: 'anthropic', accounts: ['acc-2'], skills: 18, personality: 'brief', toolsets: 'full', language: 'th', mode: 'ask' } },
  { id: 'scholar', name: 'Scholar', process: 'research', hermesProfile: 'scholar',
    settings: { model: 'local:qwen-14b', designModel: 'local:qwen-14b', provider: 'ollama', accounts: [], skills: 12, personality: 'careful', toolsets: 'read-write-wiki', language: 'en' } },
];
const projects = [
  { id: 'atlas-web', name: 'atlas-web', kind: 'team', profile: 'senior-coder', owner: 'mali', members: ['mali', 'ken', 'ploy', 'arun'], root: '~/code/atlas-web', privacy: 'shared', modelPolicy: 'any', board: 'markdown', sessionsActive: 6, wikiPages: 48, drafts: 4, lastIngest: ago(380), cost30: 31.42, tokens30: 18_400_000, language: 'en', team: true,
    overrides: { budgetDaily: 15 } },
  { id: 'ledger-api', name: 'ledger-api', kind: 'team', profile: 'senior-coder', owner: 'mali', members: ['mali', 'ken'], root: '~/code/ledger-api', privacy: 'private', modelPolicy: 'any', board: 'hpk', sessionsActive: 2, wikiPages: 21, drafts: 1, lastIngest: ago(1300), cost30: 9.87, tokens30: 5_100_000, language: 'th', team: true,
    overrides: { language: 'th', board: 'hpk', accounts: ['acc-1'], redLines: ['Never copy customer data into the wiki'] } },
  { id: 'side-app', name: 'side-app', kind: 'personal', profile: 'quick-helper', owner: 'mali', members: ['mali'], root: '~/code/side-app', privacy: 'private', modelPolicy: 'any', board: 'hpk', sessionsActive: 1, wikiPages: 9, drafts: 0, lastIngest: ago(2000), cost30: 1.12, tokens30: 900_000, language: 'th', team: false,
    overrides: {} },
  { id: 'harbor-ml', name: 'harbor-ml', kind: 'personal', profile: 'scholar', owner: 'mali', members: ['mali'], root: '~/code/harbor-ml', privacy: 'isolated', modelPolicy: 'local_only', board: 'none', sessionsActive: 0, wikiPages: 7, drafts: 0, lastIngest: ago(4300), cost30: 0, tokens30: 2_300_000, language: 'en', team: false,
    overrides: { budgetDaily: 0 } },
];

// Setting keys in the order they are shown, with the layers allowed to set them.
export const SETTING_KEYS = [
  ['mode', ['process', 'profile', 'project']],
  ['autoMax', ['process', 'project']],
  ['redLines', ['process', 'profile', 'project']],
  ['board', ['process', 'project']],
  ['jobs', ['process', 'project']],
  ['titleTemplate', ['process', 'project']],
  ['wikiReview', ['process', 'project']],
  ['maxChildren', ['process', 'project']],
  ['budgetDaily', ['process', 'project']],
  ['model', ['profile', 'project']],
  ['designModel', ['profile']],
  ['accounts', ['profile', 'project']],
  ['skills', ['profile']],
  ['personality', ['profile']],
  ['toolsets', ['profile']],
  ['language', ['profile', 'project']],
];

function resolveSettings(projectId) {
  const p = projects.find((x) => x.id === projectId);
  const prof = profiles.find((x) => x.id === p.profile);
  const proc = processes.find((x) => x.id === prof.process);
  return SETTING_KEYS.map(([key, layers]) => {
    const chain = { process: proc.settings[key], profile: prof.settings[key], project: p.overrides[key] };
    if (key === 'redLines') {
      // Red lines only ever add up: a lower layer can add, never remove.
      const items = [...(chain.process || []).map((v) => ({ v, from: 'process' })), ...(chain.profile || []).map((v) => ({ v, from: 'profile' })), ...(chain.project || []).map((v) => ({ v, from: 'project' }))];
      return { key, layers, chain, value: items.map((x) => x.v), from: items.length ? [...new Set(items.map((x) => x.from))].join('+') : 'builtin', items };
    }
    const from = chain.project !== undefined ? 'project' : chain.profile !== undefined ? 'profile' : chain.process !== undefined ? 'process' : 'builtin';
    return { key, layers, chain, value: chain[from], from };
  });
}

// Lineage-numbered session tree for atlas-web.
const sessions = [
  { id: 's-01', num: '1', parent: null, title: 'v1.4 release', lane: null, owner: 'mali', model: 'sonnet', status: 'running', heartbeat: ago(0.4), tokens: 412_300, cost: 1.84, viaHpk: true, mode: 'ask' },
  { id: 's-02', num: '1.1', parent: 's-01', title: 'auth › refresh token rotation', lane: 'auth', owner: 'mali', model: 'sonnet', status: 'running', heartbeat: ago(1), tokens: 238_900, cost: 0.97, viaHpk: true, mode: 'ask', worktree: 'worktrees/web-auth' },
  { id: 's-03', num: '1.2', parent: 's-01', title: 'auth › api contract tests', lane: 'auth', owner: 'mali', model: 'sonnet', status: 'waiting', heartbeat: ago(3), tokens: 121_400, cost: 0.51, viaHpk: true, mode: 'ask', worktree: 'worktrees/api-auth-tests' },
  { id: 's-04', num: '1.3', parent: 's-01', title: 'billing › invoice export', lane: 'billing', owner: 'mali', model: 'sonnet', status: 'running', heartbeat: ago(2), tokens: 301_200, cost: 1.22, viaHpk: true, mode: 'auto', autoLeft: 102, worktree: 'worktrees/web-billing' },
  { id: 's-05', num: '1.4', parent: 's-01', title: 'docs › onboarding guide', lane: 'docs', owner: 'mali', model: 'haiku', status: 'done', heartbeat: ago(44), tokens: 58_000, cost: 0.08, viaHpk: true, mode: 'ask' },
  { id: 's-06', num: '1.5', parent: 's-01', title: 'search › reindex job', lane: 'search', owner: 'ploy', model: 'sonnet', status: 'stale', heartbeat: ago(42), tokens: 77_800, cost: 0.33, viaHpk: true, mode: 'ask', worktree: 'worktrees/api-search' },
  { id: 's-07', num: '1.2.1', parent: 's-03', title: 'auth › fixture refresh', lane: 'auth', owner: 'mali', model: 'haiku', status: 'running', heartbeat: ago(0.8), tokens: 19_300, cost: 0.03, viaHpk: true, mode: 'ask' },
  { id: 's-09', num: '1.6', parent: 's-01', title: 'Verify v1.4 goal', lane: null, owner: 'mali', model: 'opus', status: 'queued', heartbeat: ago(117), tokens: 0, cost: 0, viaHpk: true, mode: 'ask' },
  { id: 's-08', num: '2', parent: null, title: 'billing hotfix (Ken)', lane: 'billing', owner: 'ken', model: 'opus', status: 'running', heartbeat: ago(1.5), tokens: 188_100, cost: null, viaHpk: false, mode: 'ask', source: 'cli' },
  { id: 's-10', num: '3', parent: null, title: 'kart › Blender model and lap chart', lane: null, owner: 'mali', model: 'sonnet', status: 'done', heartbeat: ago(25), tokens: 96_400, cost: 0.41, viaHpk: true, mode: 'ask' },
];

// Plan facts per sub-task. category picks the model chain; deps order the work;
// ev is evidence: a stage plus how sure we are. "Reported done" is never "verified".
// role 'verify' is the sub-task the agent always adds last: it runs real checks
// against the goal's criteria. A failed node may be repaired at most twice.
const PLAN = {
  's-02': { category: 'deep', effort: 'high', deps: [], ev: ['code', 'running'], scope: ['web/src/auth/**'] },
  's-03': { category: 'deep', effort: 'medium', deps: ['s-02'], ev: ['test', 'verified'], scope: ['api/session/**', 'api/tests/auth/**'] },
  's-04': { category: 'deep', effort: 'high', deps: [], ev: ['code', 'running'], repairs: 1, scope: ['api/invoices/**', 'web/src/billing/export/**'] },
  's-05': { category: 'quick', effort: 'low', deps: [], ev: ['code', 'reported'], scope: ['docs/onboarding/**'] },
  's-06': { category: 'quick', effort: 'medium', deps: [], ev: ['code', 'blocked'], scope: ['api/search/**'] },
  's-07': { category: 'quick', effort: 'low', deps: [], ev: ['code', 'running'], scope: ['api/fixtures/auth/**'] },
  's-09': { category: 'review', effort: 'high', deps: ['s-02', 's-03', 's-04', 's-05', 's-06'], ev: ['verify', 'notrun'], role: 'verify' },
};
export const MAX_REPAIRS = 2;
sessions.forEach((s) => { if (s.parent) Object.assign(s, { category: 'deep', effort: 'medium', deps: [], ev: ['plan', 'notrun'], repairs: 0, role: 'build', merged: false, scope: [] }, PLAN[s.id] || {}); });

// Glob overlap, good enough for the mock: "a/**" covers everything under a/.
const stem = (g) => g.replace(/\/?\*\*.*$/, '').replace(/\*.*$/, '');
export const pathsOverlap = (a, b) => { const x = stem(a); const y = stem(b); return x === y || x.startsWith(y + '/') || y.startsWith(x + '/') || !x || !y; };

// ---- 5. work categories: each picks an ordered model chain ----
// Accounts fail over first (same model, next account); when a model is refused
// or out on every account, the chain moves to the next model. Stored by hpk,
// never written into Hermes' own config.
const categories = [
  { id: 'quick', chain: [{ model: 'haiku', effort: 'low' }, { model: 'sonnet', effort: 'low' }] },
  { id: 'deep', chain: [{ model: 'sonnet', effort: 'high' }, { model: 'opus', effort: 'high' }] },
  { id: 'review', chain: [{ model: 'opus', effort: 'high' }, { model: 'sonnet', effort: 'high' }] },
  { id: 'design', chain: [{ model: 'sonnet', effort: 'medium' }, { model: 'opus', effort: 'medium' }] },
];

// Lanes are tracks; blocks are scope areas along each track.
const lanes = [
  { id: 'auth', name: 'auth', blocks: [
    { id: 'auth-web', label: 'web/src/auth', occupants: ['s-02'] },
    { id: 'auth-api', label: 'api/session', occupants: ['s-03'] },
    { id: 'auth-fix', label: 'api/fixtures', occupants: ['s-07'] },
    { id: 'auth-docs', label: 'docs/auth', occupants: [] },
  ] },
  { id: 'billing', name: 'billing', blocks: [
    { id: 'bill-ui', label: 'web/src/billing', occupants: [] },
    { id: 'bill-export', label: 'api/invoices', occupants: ['s-04', 's-08'] },
    { id: 'bill-db', label: 'db/migrations', occupants: ['s-08'] },
    { id: 'bill-docs', label: 'docs/billing', occupants: [] },
  ] },
  { id: 'search', name: 'search', blocks: [
    { id: 'srch-idx', label: 'api/search', occupants: ['s-06'] },
    { id: 'srch-ui', label: 'web/src/search', occupants: [] },
    { id: 'srch-docs', label: 'docs/search', occupants: [] },
  ] },
  { id: 'docs', name: 'docs', blocks: [
    { id: 'docs-guide', label: 'docs/onboarding', occupants: [] },
    { id: 'docs-ref', label: 'docs/reference', occupants: [] },
  ] },
];

const claims = [
  { id: 'c-0142', task: 'auth: refresh token rotation', lane: 'auth', session: 's-02', paths: ['web/src/auth/**'], wiki: ['decisions/auth-flow.md'], started: ago(96), heartbeat: ago(1), status: 'active' },
  { id: 'c-0143', task: 'auth: api contract tests', lane: 'auth', session: 's-03', paths: ['api/session/**', 'api/tests/auth/**'], wiki: [], started: ago(81), heartbeat: ago(3), status: 'waiting-human' },
  { id: 'c-0145', task: 'billing: invoice export (CSV + PDF)', lane: 'billing', session: 's-04', paths: ['api/invoices/**', 'web/src/billing/export/**'], wiki: ['architecture/billing.md'], started: ago(64), heartbeat: ago(2), status: 'active' },
  { id: 'c-0146', task: 'billing: rounding hotfix', lane: 'billing', session: 's-08', paths: ['api/invoices/totals.ts', 'db/migrations/**'], wiki: [], started: ago(22), heartbeat: ago(1.5), status: 'active' },
  { id: 'c-0139', task: 'search: reindex job', lane: 'search', session: 's-06', paths: ['api/search/**'], wiki: [], started: ago(190), heartbeat: ago(42), status: 'stale' },
  { id: 'c-0147', task: 'auth: fixture refresh', lane: 'auth', session: 's-07', paths: ['api/fixtures/auth/**'], wiki: [], started: ago(9), heartbeat: ago(0.8), status: 'active' },
];

const overlaps = [
  { id: 'o-1', level: 'danger', kind: 'duplicate', a: 'c-0145', b: 'c-0146', files: ['api/invoices/totals.ts'], found: ago(4), note: 'Both claims change invoice totals.' },
  { id: 'o-2', level: 'caution', kind: 'files', a: 'c-0143', b: 'c-0147', files: ['api/fixtures/auth/users.json'], found: ago(7), note: 'Fixture file touched by a child and its parent.' },
  { id: 'o-3', level: 'related', kind: 'wiki', a: 'c-0142', b: 'c-0143', files: ['decisions/auth-flow.md'], found: ago(30), note: 'Same wiki page referenced.' },
];

const approvals = [
  { id: 'a-1', kind: 'permission', redLine: 'push', session: 's-03', title: 'Push branch feat/auth-contract-tests to origin', detail: 'git push origin feat/auth-contract-tests', created: ago(3), project: 'atlas-web' },
  { id: 'a-2', kind: 'duplicate', session: 's-04', title: 'Duplicate work on api/invoices/totals.ts', detail: 'Session 1.3 and Ken\'s hotfix (2) both change invoice totals.', created: ago(4), project: 'atlas-web', overlap: 'o-1' },
  { id: 'a-3', kind: 'question', session: 's-07', title: 'Sub-task 1.2.1 asks: keep or regenerate the legacy user fixtures?', detail: 'Regenerating breaks 3 snapshot tests that 1.2 is still writing.', created: ago(6), project: 'atlas-web' },
  { id: 'a-4', kind: 'draft', session: 's-02', title: '3 wiki drafts from 1.1 (auth)', detail: 'decisions/token-rotation.md (new), lessons/clock-skew.md (new), architecture/auth.md (edit)', created: ago(12), project: 'atlas-web', drafts: ['d-1', 'd-2', 'd-3'] },
  { id: 'a-5', kind: 'permission', redLine: 'delete', session: 's-05', title: 'Remove worktree worktrees/docs-onboarding', detail: 'git worktree remove worktrees/docs-onboarding', created: ago(26), project: 'atlas-web' },
  { id: 'a-7', kind: 'permission', redLine: 'delete', session: 's-04', title: 'Clean old build output and push feat/invoice-export', detail: 'pnpm build && rm -rf dist/legacy && git push origin feat/invoice-export', created: ago(2), project: 'atlas-web' },
  { id: 'a-6', kind: 'stale', session: 's-06', title: 'Claim c-0139 (search reindex) has no heartbeat for 42 min', detail: 'Owner: Ploy W. Ask the session, release the claim, or take it over.', created: ago(12), project: 'atlas-web' },
];

const events = [
  { at: ago(0.4), lamp: 'clear', who: 'agent', session: 's-01', text: '1 · coordinator posted lane summary to BOARD.md' },
  { at: ago(1), lamp: 'clear', who: 'agent', session: 's-02', text: '1.1 · committed 3 files in worktrees/web-auth (local)' },
  { at: ago(3), lamp: 'danger', who: 'agent', session: 's-03', text: '1.2 · stopped at red line: git push' },
  { at: ago(4), lamp: 'danger', who: 'hpk', session: 's-04', text: 'Board · duplicate work detected on api/invoices/totals.ts' },
  { at: ago(6), lamp: 'caution', who: 'agent', session: 's-07', text: '1.2.1 · asked a question for a human' },
  { at: ago(9), lamp: 'route', who: 'mali', session: 's-03', text: '1.2 · spawned child 1.2.1 (auth › fixture refresh)' },
  { at: ago(12), lamp: 'caution', who: 'agent', session: 's-02', text: '1.1 · wrote 3 wiki drafts' },
  { at: ago(18), lamp: 'clear', who: 'auto', session: 's-04', text: '1.3 · Auto approved: run test suite (billing)' },
  { at: ago(22), lamp: 'route', who: 'ken', session: 's-08', text: '2 · Ken claimed api/invoices/totals.ts, db/migrations' },
  { at: ago(44), lamp: 'clear', who: 'agent', session: 's-05', text: '1.4 · finished and released claim c-0138' },
];

const wikiTree = [
  { section: 'decisions', pages: [
    { id: 'w-1', path: 'decisions/auth-flow.md', title: 'Auth flow: BFF session cookie instead of bearer tokens', updated: ago(2900), confidence: 'high' },
    { id: 'w-2', path: 'decisions/invoice-rounding.md', title: 'Invoice rounding happens once, at totals', updated: ago(9000), confidence: 'medium' },
  ] },
  { section: 'architecture', pages: [
    { id: 'w-3', path: 'architecture/auth.md', title: 'Auth components and how they connect', updated: ago(5200), confidence: 'high' },
    { id: 'w-4', path: 'architecture/billing.md', title: 'Billing pipeline', updated: ago(12000), confidence: 'medium' },
  ] },
  { section: 'lessons', pages: [
    { id: 'w-5', path: 'lessons/stale-worktree-lock.md', title: 'A stale lock is usually your own background process', updated: ago(20000), confidence: 'high' },
    { id: 'w-6', path: 'lessons/board-vs-git.md', title: 'Trust git, not the board prose', updated: ago(31000), confidence: 'high' },
  ] },
  { section: 'versions', pages: [{ id: 'w-7', path: 'versions/v1.3.md', title: 'v1.3: what shipped and what slipped', updated: ago(40000), confidence: 'high' }] },
  { section: 'glossary', pages: [{ id: 'w-8', path: 'glossary/terms.md', title: 'Terms used in atlas-web', updated: ago(15000), confidence: 'high' }] },
  { section: 'people', pages: [{ id: 'w-9', path: 'people/team.md', title: 'Who looks after what', updated: ago(26000), confidence: 'medium' }] },
];

const wikiPages = {
  'w-1': {
    sources: ['web/src/auth/session.ts:12', 'commit 4f2a9c1', 'session s-0912'],
    body: [
      ['p', 'The web app keeps sessions in an HttpOnly cookie set by the BFF. The browser never holds a bearer token.'],
      ['h2', 'Why'],
      ['ul', ['Tokens in local storage were readable by any injected script.', 'The BFF already terminates every request, so it can attach the token server-side.', 'Refresh happens in one place instead of in each client.']],
      ['h2', 'Options we did not take'],
      ['ul', ['Short-lived bearer tokens in memory: lost on reload, needed a silent re-auth iframe.', 'A service worker holding the token: hard to debug, uneven browser support.']],
      ['h2', 'Consequences'],
      ['p', 'Cross-site requests need CSRF protection on every mutating route. See architecture/auth.md.'],
    ],
  },
};

const drafts = [
  { id: 'd-1', n: 1, path: 'decisions/token-rotation.md', kind: 'new', area: 'decisions', from: 's-02', summary: 'Rotate refresh tokens on every use; reuse revokes the family.', sources: ['web/src/auth/refresh.ts:40', 'commit 9b1e07d'],
    diff: [['add', '# Refresh token rotation'], ['add', ''], ['add', 'Every refresh issues a new refresh token and revokes the old one.'], ['add', 'If a revoked token is used again, the whole token family is revoked.'], ['add', ''], ['add', '## Why'], ['add', 'A stolen refresh token stops working the next time the real client refreshes.']] },
  { id: 'd-2', n: 2, path: 'lessons/clock-skew.md', kind: 'new', area: 'lessons', from: 's-02', summary: 'Token expiry tests failed on CI because of 2 s clock skew.', sources: ['ci run 5521', 'api/tests/auth/expiry.test.ts:18'],
    diff: [['add', '# Clock skew breaks expiry tests'], ['add', ''], ['add', 'CI runners can drift by up to 2 seconds. Expiry assertions must allow a 5 second leeway.']] },
  { id: 'd-3', n: 3, path: 'architecture/auth.md', kind: 'edit', area: 'architecture', from: 's-02', summary: 'Add the rotation step to the refresh sequence.', sources: ['web/src/auth/refresh.ts:40'],
    diff: [['ctx', '## Refresh sequence'], ['ctx', '1. Browser calls /api/session/refresh'], ['del', '2. BFF exchanges the refresh token for a new access token'], ['add', '2. BFF exchanges the refresh token for a new access token and a new refresh token'], ['add', '3. BFF revokes the old refresh token'], ['ctx', '4. BFF sets the session cookie']] },
  { id: 'd-4', n: 4, path: 'versions/v1.4.md', kind: 'edit', area: 'versions', from: 's-01', summary: 'Record lanes opened today and their owners.', sources: ['BOARD.md', 'session s-01'],
    diff: [['ctx', '## Lanes'], ['add', '- auth: 1.1, 1.2, 1.2.1 (Mali)'], ['add', '- billing: 1.3 (Mali), hotfix 2 (Ken)'], ['add', '- search: 1.5 (Ploy) — stale since 14:02']] },
];

const jobs = [
  { id: 'j-1', key: 'nightly', name: 'Nightly ingest', schedule: '0 2 * * *', next: now + 9.2 * 3600_000, last: ago(1280), lastStatus: 'ok', model: 'haiku (batch)', tokens: 41_200, cost: 0.06, budget: 60_000, paused: false },
  { id: 'j-2', key: 'weekly', name: 'Weekly wiki lint', schedule: '0 8 * * 1', next: now + 3.4 * 86400_000, last: ago(4 * 1440), lastStatus: 'warn', model: 'sonnet', tokens: 88_900, cost: 0.41, budget: 150_000, paused: false, note: '3 findings' },
  { id: 'j-3', key: 'digest', name: 'Auto digest', schedule: '0 18 * * *', next: now + 1.6 * 3600_000, last: ago(1340), lastStatus: 'ok', model: 'haiku', tokens: 3_100, cost: 0.004, budget: 10_000, paused: false, note: 'only on days Auto ran' },
];

const runs = [
  { job: 'j-1', at: ago(1280), status: 'ok', tokens: 41_200, cost: 0.06, out: '14 raw notes, 3 drafts' },
  { job: 'j-3', at: ago(1340), status: 'ok', tokens: 3_100, cost: 0.004, out: '7 Auto decisions' },
  { job: 'j-1', at: ago(2720), status: 'ok', tokens: 38_700, cost: 0.05, out: '9 raw notes, 1 draft' },
  { job: 'j-2', at: ago(4 * 1440), status: 'warn', tokens: 88_900, cost: 0.41, out: '1 contradiction, 2 stale pages' },
  { job: 'j-1', at: ago(4160), status: 'fail', tokens: 0, cost: 0, out: 'Stopped: budget would be exceeded (est. 72k > 60k)' },
];

const redLines = [
  { id: 'r-push', key: 'push', locked: true },
  { id: 'r-delete', key: 'delete', locked: true },
  { id: 'r-secrets', key: 'secrets', locked: true },
  { id: 'r-control', key: 'control', locked: true },
  { id: 'r-model', key: 'model', locked: true },
  { id: 'r-wikicore', key: 'wikicore', locked: true },
  { id: 'r-promote', key: 'promote', locked: true },
  { id: 'r-egress', key: 'egress', locked: true },
  { id: 'r-x1', key: 'custom', text: 'Run `make release-check` before any merge', locked: false },
  { id: 'r-x2', key: 'custom', text: 'Never edit db/migrations that are already applied', locked: false },
];

const audit = [
  { at: ago(1), actor: 'agent', session: 's-02', action: 'write', target: 'wiki/log.md', result: 'ok' },
  { at: ago(3), actor: 'agent', session: 's-03', action: 'request', target: 'git push (red line)', result: 'waiting' },
  { at: ago(18), actor: 'auto', session: 's-04', action: 'approve', target: 'pnpm test --filter billing', result: 'ok', reason: 'read-only test run inside worktree' },
  { at: ago(26), actor: 'auto', session: 's-04', action: 'approve', target: 'lessons/csv-encoding.md', result: 'ok', reason: 'adds to lessons, no conflict' },
  { at: ago(31), actor: 'mali', session: 's-03', action: 'approve', target: 'npm install (lockfile change)', result: 'ok' },
  { at: ago(47), actor: 'auto', session: 's-04', action: 'approve', target: 'edit web/src/billing/export/csv.ts', result: 'ok', reason: 'inside claimed paths' },
  { at: ago(52), actor: 'ken', session: 's-08', action: 'deny', target: 'drop column invoices.legacy_total', result: 'denied' },
  { at: ago(70), actor: 'auto', session: 's-04', action: 'refuse', target: 'git push (red line)', result: 'asked human' },
];

const autoDigest = [
  { id: 'ad-1', n: 1, at: ago(18), session: 's-04', what: 'Ran billing test suite', why: 'read-only, inside worktree', revertable: false },
  { id: 'ad-2', n: 2, at: ago(26), session: 's-04', what: 'Approved lessons/csv-encoding.md', why: 'adds to lessons, no conflict', revertable: true },
  { id: 'ad-3', n: 3, at: ago(47), session: 's-04', what: 'Edited web/src/billing/export/csv.ts', why: 'inside claimed paths', revertable: true },
  { id: 'ad-4', n: 4, at: ago(58), session: 's-04', what: 'Created child 1.3.1 (pdf renderer)', why: 'within agreed plan, limit 4 children', revertable: true },
];

const promotions = [
  { id: 'pr-1', from: 'atlas-web', page: 'lessons/stale-worktree-lock.md', title: 'A stale lock is usually your own background process',
    diff: [['ctx', 'When a session reports SESSION_NOT_OWNED,'], ['del', 'check atlas-web worktrees/web-auth first'], ['add', 'check your own background processes first'], ['ctx', 'before assuming another person holds it.'], ['redact', '[project name removed] · [path removed] · [person removed]']] },
  { id: 'pr-2', from: 'atlas-web', page: 'people/team.md#mali', title: 'Prefers approvals in batches, numbered',
    diff: [['ctx', 'Working style'], ['add', 'Prefers decisions grouped into one numbered list.'], ['add', 'Answers with "approve 1,3".']] },
];

const notifications = [
  { id: 'n-1', at: ago(3), lamp: 'danger', title: '1.2 stopped at a red line', desc: 'git push · waiting for approval', unread: true, href: '#/approvals' },
  { id: 'n-2', at: ago(4), lamp: 'danger', title: 'Duplicate work on api/invoices/totals.ts', desc: 'Session 1.3 and Ken\'s hotfix (2)', unread: true, href: '#/board' },
  { id: 'n-3', at: ago(6), lamp: 'caution', title: '1.2.1 asked a question', desc: 'Keep or regenerate legacy fixtures?', unread: true, href: '#/approvals' },
  { id: 'n-4', at: ago(12), lamp: 'caution', title: 'Claim c-0139 went stale', desc: 'search reindex · Ploy W.', unread: false, href: '#/board' },
  { id: 'n-5', at: ago(44), lamp: 'clear', title: '1.4 finished', desc: 'docs › onboarding guide · summary saved to wiki/raw', unread: false, href: '#/sessions' },
  { id: 'n-6', at: ago(130), lamp: 'route', title: 'Someone asked about your session', desc: 'Ken asked 1.3 for status (status only)', unread: false, href: '#/sessions/s-04' },
];

const doctor = [
  { key: 'hermes', status: 'clear', value: 'hermes 0.23.1' },
  { key: 'acp', status: 'clear', value: 'hermes-acp ready' },
  { key: 'wikiPath', status: 'clear', value: '~/.hermes/profiles/atlas-web/wiki' },
  { key: 'hooks', status: 'caution', value: '5 of 6 installed' },
  { key: 'cron', status: 'clear', value: '3 of 3 scheduled' },
  { key: 'stateDb', status: 'clear', value: 'read-only, schema v14' },
  { key: 'auth', status: 'caution', value: 'off · bound to 127.0.0.1' },
  { key: 'hookLatency', status: 'clear', value: 'p95 11 ms' },
];

// Per-session chat state (pin, archive, model, effort, context, draft, goal).
// Wiki admission: every "save to wiki" is kept, refused or deferred, with a reason.
// Secrets are refused with no override; logs and task progress can be kept by a human.
const admissions = [
  { at: ago(12), from: 's-02', verdict: 'keep', reason: 'keep', summary: 'Clock skew breaks token refresh near expiry', path: 'lessons/clock-skew.md' },
  { at: ago(35), from: 's-03', verdict: 'refuse', reason: 'log', summary: '✓ 38 passed (4.2s)', path: null },
  { at: ago(70), from: 's-04', verdict: 'refuse', reason: 'secret', summary: '(hidden)', path: null },
  { at: ago(140), from: 's-05', verdict: 'defer', reason: 'short', summary: 'Use one page.', path: null },
];
export function admitToWiki(text) {
  const x = String(text || '').trim();
  if (/(sk-[A-Za-z0-9_-]{6,}|-----BEGIN [A-Z ]*PRIVATE KEY|\bpassword\s*[:=]|\bapi[_-]?key\s*[:=]|(^|\s)\.env\b|\b[A-Z_]*(SECRET|TOKEN)\s*=)/i.test(x)) return { verdict: 'refuse', reason: 'secret', override: false };
  const lines = x.split(/\n/);
  const logish = lines.filter((l) => /^\s*(\d{2}:\d{2}|\[\w+\]|✓|✗|PASS|FAIL|at\s+\S+:\d+|>\s)/.test(l) || /\b\d+ passed\b/.test(l)).length;
  if (logish && logish >= Math.ceil(lines.length / 2)) return { verdict: 'refuse', reason: 'log', override: true };
  if (/^(I('| a)m |Starting|Reading|Claimed|Working on|Adjusting|Noted|Understood|Stopped|Heads up)/i.test(x)) return { verdict: 'refuse', reason: 'progress', override: true };
  if (x.length < 60) return { verdict: 'defer', reason: 'short', override: true };
  return { verdict: 'keep', reason: 'keep', override: true };
}
// Wiki pages recalled into each chat, cut to a token budget. Pages that do not
// fit are named, never dropped silently.
const RECALL = {
  's-01': { pages: [['versions/v1.3.md', 1800], ['decisions/release-checklist.md', 760]], dropped: [] },
  's-03': { pages: [['decisions/auth-flow.md', 940], ['architecture/auth.md', 1210]], dropped: [] },
  's-04': { pages: [['architecture/billing.md', 1480], ['decisions/invoice-rounding.md', 620]], dropped: [['lessons/csv-encoding.md', 2300]] },
};

// Models Hermes offers a chat (ACP availableModels) with models.dev facts:
// window in tokens, USD per million input tokens, cache-write rate.
const MODEL_CATALOG = [
  { id: 'anthropic:haiku', name: 'haiku', provider: 'anthropic', window: 200_000, windowKnown: true, inputPerM: 1, cacheWritePerM: 1.25, priced: true, local: false },
  { id: 'anthropic:sonnet', name: 'sonnet', provider: 'anthropic', window: 200_000, windowKnown: true, inputPerM: 3, cacheWritePerM: 3.75, priced: true, local: false },
  { id: 'anthropic:opus', name: 'opus', provider: 'anthropic', window: 1_000_000, windowKnown: true, inputPerM: 5, cacheWritePerM: 6.25, priced: true, local: false },
  { id: 'openai:gpt-5', name: 'gpt-5', provider: 'openai', window: 400_000, windowKnown: true, inputPerM: 1.25, priced: true, local: false },
  { id: 'ollama:qwen-14b', name: 'qwen-14b', provider: 'ollama', window: 32_000, windowKnown: true, priced: false, local: true },
];
const modelById = (x) => MODEL_CATALOG.find((m) => m.id === x || m.name === x || 'local:' + m.name === x);
// Same rule as internal/modelswitch.Plan: continue when the full history fills
// at most 60% of the new window, else compact to a summary + the last 20 messages.
function planSwitch(cur, st, to, policy) {
  const full = st.history + st.brief + st.recall;
  const fill = full / to.window;
  const way = fill <= 0.6 ? 'continue' : 'compact';
  const carry = way === 'continue' ? full : Math.min(2000 + st.tail, st.history) + st.brief + st.recall;
  const current = to.id === cur.id;
  return { model: to, current, way, fill, after: carry / to.window, carry,
    firstCost: to.priced ? carry * Math.max(to.inputPerM, to.cacheWritePerM || 0) / 1e6 : 0,
    crossProvider: cur.provider !== to.provider, dropsThinking: !current,
    locked: policy === 'local_only' && !to.local ? 'local_only' : current ? 'current' : '' };
}

const chatMeta = {};
sessions.forEach((s) => {
  const rc = RECALL[s.id] || { pages: [], dropped: [] };
  chatMeta[s.id] = { recall: { budget: 4000, pages: rc.pages.map(([path, tokens]) => ({ path, tokens })), dropped: rc.dropped.map(([path, tokens]) => ({ path, tokens })) }, pinned: s.id === 's-03', archived: false, effort: 'medium', ctxUsed: Math.round(s.tokens * 0.35), ctxWindow: 200_000, draft: '', goal: s.id === 's-01' ? 'Ship v1.4 with token rotation and invoice export' : '' , criteria: s.id === 's-01' ? ['Token rotation: auth suite passes', 'Invoice CSV and PDF export covered by tests', 'Onboarding guide builds without warnings'].map((text) => ({ text, result: null })) : [] };
});

const chat = {
  // Rich media demo: files come from tools/fixtures/media through the dev server's /api/files.
  's-10': [
    { t: 'msg', who: 'user', author: 'mali', at: ago(31), text: 'In Blender: model the kart body, paint it, add a spin and a bounce, light it, export glTF. Also the crate as OBJ and the bracket as STL. Then chart the lap times.' },
    { t: 'tool', at: ago(29), name: 'mcp__blender__execute_blender_code', summary: 'export glTF · 2 animations · 1 light', out: 'bpy.ops.export_scene.gltf(filepath="renders/kart.gltf", export_animations=True, export_lights=True)\nExported renders/kart.gltf (1 mesh, 2 clips: Spin, Bounce; 1 spot light)\nViewport screenshot saved to renders/render.png' },
    { t: 'tool', at: ago(27), name: 'mcp__blender__execute_blender_code', summary: 'export OBJ + STL', out: 'Exported renders/crate.obj with renders/crate.mtl (texture crate.png)\nExported renders/bracket.stl' },
    { t: 'msg', who: 'agent', at: ago(26), text: [
      'Done. Here is the viewport:',
      '',
      '![Viewport](renders/render.png)',
      '',
      'Files: `renders/kart.gltf` (play **Spin** or **Bounce**), `renders/crate.obj` and `renders/bracket.stl`. Lap data is in `renders/laps.csv`.',
      '',
      '| Part | Triangles | Texture |',
      '|---|---:|---|',
      '| kart body | 12 | kart-paint.png |',
      '| crate | 12 | crate.png |',
      '| bracket | 6 | none |',
      '',
      '```chart',
      '{"type":"line","title":"Lap time and fuel left","labels":["1","2","3","4","5","6","7","8","9","10","11","12"],"series":[{"name":"Lap time (s)","data":[61.8,61.6,61.4,58.8,58.6,58.4,55.8,55.6,55.4,52.8,52.6,52.4]},{"name":"Fuel left (l)","data":[37.1,34.2,31.3,28.4,25.5,22.6,19.7,16.8,13.9,11,8.1,5.2]}]}',
      '```',
    ].join('\n') },
  ],
  's-01': [
    { t: 'msg', who: 'user', author: 'mali', at: ago(120), text: 'Plan v1.4. Lanes: auth (token rotation), billing (invoice export), search (reindex), docs (onboarding). Open a child per lane.' },
    { t: 'thinking', at: ago(119), text: 'Four lanes, no shared files except api/invoices with the billing hotfix Ken mentioned. Check the board before opening billing. Docs is small enough for haiku.' },
    { t: 'msg', who: 'agent', at: ago(118), text: 'Read BOARD.md and wiki versions/v1.3.md. Plan posted to the board. Opening four children, one per lane, each in its own worktree.' },
    { t: 'child', at: ago(117), session: 's-02' },
    { t: 'child', at: ago(117), session: 's-03' },
    { t: 'child', at: ago(116), session: 's-04' },
    { t: 'child', at: ago(116), session: 's-05' },
    { t: 'switch', at: ago(60), from: 'anthropic:opus', to: 'anthropic:sonnet', way: 'continue', fill: 0.14 },
    { t: 'summary', at: ago(44), session: 's-05', done: 'Wrote docs/onboarding/getting-started.md and two screenshots placeholders.', decided: 'Keep onboarding to one page; link out to reference docs.', lessons: 'The setup script assumes bash; Windows users need the PowerShell note.', open: 'Screenshots need real captures.', question: '' },
    { t: 'clarify', at: ago(6), from: 's-07', question: 'Sub-task 1.2.1 asks: keep or regenerate the legacy user fixtures?', options: ['Keep legacy fixtures', 'Regenerate all', 'Regenerate, but keep the 3 snapshot users'], answered: null },
  ],
  's-03': [
    { t: 'msg', who: 'user', author: 'mali', at: ago(40), text: 'Write contract tests for the session endpoints. Use the fixtures in api/fixtures/auth. Push when green.' },
    { t: 'thinking', at: ago(39.5), text: 'Contract tests should not depend on the BFF cookie. Read decisions/auth-flow.md first so the tests assert the documented behaviour, not the current code.' },
    { t: 'msg', who: 'agent', at: ago(39), text: 'Reading wiki: decisions/auth-flow.md and architecture/auth.md first. Claiming api/session/** and api/tests/auth/**.' },
    { t: 'tool', at: ago(38), name: 'hpk board claim', summary: 'claim c-0143 · 2 paths · no overlap', out: 'claimed c-0143\npaths: api/session/**, api/tests/auth/**\noverlap check: none' },
    { t: 'tool', at: ago(30), name: 'edit_file', summary: 'api/tests/auth/session.contract.test.ts · +142 lines', out: 'created api/tests/auth/session.contract.test.ts (142 lines)' },
    { t: 'tool', at: ago(12), name: 'terminal', summary: 'pnpm test --filter auth · 38 passed', out: '✓ 38 passed (4.2s)' },
    { t: 'msg', who: 'agent', at: ago(9.5), text: 'Tests pass. The legacy user fixtures are out of date, so I started sub-task 1.2.1 to refresh them. Committed locally on feat/auth-contract-tests.' },
    { t: 'child', at: ago(9), session: 's-07' },
    { t: 'permit', at: ago(3), id: 'a-1', redLine: 'push', command: 'git push origin feat/auth-contract-tests', why: 'You asked me to push when green. Push is a red line for this project.' },
  ],
  's-04': [
    { t: 'msg', who: 'user', author: 'mali', at: ago(66), text: 'Add CSV and PDF export to invoices. Auto for 2 hours is fine.' },
    { t: 'msg', who: 'agent', at: ago(65), text: 'Claimed api/invoices/** and web/src/billing/export/**. Starting with CSV.' },
    { t: 'msg', who: 'user', author: 'mali', at: ago(52), how: 'steer', delivery: 'read', text: 'Keep the PDF to A4 only. No letter size.' },
    { t: 'msg', who: 'agent', at: ago(51), text: 'Noted: A4 only for the PDF layout.' },
    { t: 'msg', who: 'user', author: 'mali', at: ago(21), how: 'steer', delivery: 'unused', text: 'Use the shared money formatter for totals.' },
    { t: 'note', at: ago(20.5), kind: 'stopped', text: 'You stopped the step before the agent read your steer.' },
    { t: 'tool', at: ago(47), name: 'edit_file', summary: 'web/src/billing/export/csv.ts · Auto approved', out: 'auto: inside claimed paths' },
    { t: 'msg', who: 'agent', at: ago(5), text: 'Heads up: Ken\'s hotfix session also changes api/invoices/totals.ts. I paused edits on that file and asked you how to proceed.' },
    { t: 'permit', at: ago(2), id: 'a-7', redLine: 'delete', command: 'pnpm build && rm -rf dist/legacy && git push origin feat/invoice-export', why: 'CSV export is done. I want to clear the old build output and push the branch for review.' },
    { t: 'msg', who: 'user', author: 'mali', at: ago(1), how: 'queue', delivery: 'pending', text: 'After CSV, write the PDF tests before the PDF code.' },
  ],
};

// ---- AI accounts (stored by Hermes' credential pool; hpk only orders and switches them) ----
// Keys are never held here in full: only a masked fingerprint for display.
const providers = [
  { id: 'anthropic', name: 'Anthropic' },
  { id: 'openai', name: 'OpenAI' },
  { id: 'openrouter', name: 'OpenRouter' },
  { id: 'google', name: 'Google Gemini' },
];
const accounts = [
  { id: 'acc-1', provider: 'anthropic', label: 'Company', method: 'api_key', fp: 'sk-ant-…a91f', order: 1, enabled: true, status: 'active', used: 3_820_000, limit: 5_000_000, unit: 'tokens', window: '5h', resetAt: now + 94 * 60_000, lastUsed: ago(0.3), projects: ['atlas-web', 'ledger-api'] },
  { id: 'acc-2', provider: 'anthropic', label: 'Personal', method: 'oauth', fp: 'claude.ai · m…@example.test', order: 2, enabled: true, status: 'ready', used: 410_000, limit: 2_500_000, unit: 'tokens', window: '5h', resetAt: now + 30 * 60_000, lastUsed: ago(1300), projects: ['atlas-web', 'harbor-ml'] },
  { id: 'acc-3', provider: 'anthropic', label: 'Team pool #2', method: 'api_key', fp: 'sk-ant-…07cd', order: 3, enabled: true, status: 'exhausted', used: 5_000_000, limit: 5_000_000, unit: 'tokens', window: '5h', resetAt: now + 132 * 60_000, lastUsed: ago(170), projects: ['atlas-web'] },
  { id: 'acc-4', provider: 'openai', label: 'Codex sign-in', method: 'oauth', fp: 'chatgpt · k…@example.test', order: 1, enabled: true, status: 'active', used: 18, limit: 50, unit: 'percent', window: 'week', resetAt: now + 3.2 * 86400_000, lastUsed: ago(2900), projects: ['atlas-web'] },
  { id: 'acc-5', provider: 'openai', label: 'Backup key', method: 'api_key', fp: 'sk-proj-…5b2e', order: 2, enabled: false, status: 'disabled', used: 0, limit: 40, unit: 'usd', window: 'month', resetAt: now + 14 * 86400_000, lastUsed: ago(40000), projects: [] },
  { id: 'acc-6', provider: 'openrouter', label: 'Shared credits', method: 'api_key', fp: 'sk-or-…9e40', order: 1, enabled: true, status: 'active', used: 5.8, limit: 20, unit: 'usd', window: 'credits', resetAt: null, lastUsed: ago(600), projects: ['atlas-web', 'ledger-api'] },
];
const failoverPolicy = {
  triggers: { quota: true, budget: true, errors: true, auth: true },
  returnTo: 'on_reset',          // on_reset | next_session | never
  sticky: true,                  // running sessions stay on their account unless it fails
  crossProvider: false,          // falling back to another provider = model change = red line
  notify: true,
};
const switchLog = [
  { at: ago(170), provider: 'anthropic', from: 'acc-3', to: 'acc-1', reason: 'quota', session: 's-04' },
  { at: ago(2600), provider: 'anthropic', from: 'acc-1', to: 'acc-2', reason: 'budget', session: 's-01' },
  { at: ago(2480), provider: 'anthropic', from: 'acc-2', to: 'acc-1', reason: 'reset', session: null },
  { at: ago(5200), provider: 'openai', from: 'acc-5', to: 'acc-4', reason: 'auth', session: null },
];

let state = {
  projectId: 'atlas-web',
  mode: { value: 'ask', autoUntil: null, scope: 'project' },
  teamMode: true,
  budget: { daily: 12, monthly: 200 },
};

const delay = (v, ms = 60) => new Promise((r) => setTimeout(() => r(structuredClone(v)), ms));

export const api = {
  me: () => people.mali,
  people: () => delay(Object.values(people)),
  person: (id) => people[id],
  state: () => state,
  setProject(id) { state.projectId = id; emit('project', id); },
  setMode(value, minutes = null, scope = 'project') {
    state.mode = { value, autoUntil: minutes ? Date.now() + minutes * 60_000 : null, scope };
    emit('mode', state.mode);
  },
  setTeamMode(on) { state.teamMode = on; emit('team', on); },
  projects: () => delay(projects),
  projectsSync: () => projects,
  processes: () => processes,
  profiles: () => profiles,
  processOf: (profileId) => processes.find((x) => x.id === profiles.find((p) => p.id === profileId)?.process),
  profileOf: (projectId) => profiles.find((x) => x.id === projects.find((p) => p.id === projectId)?.profile),
  effective: (projectId) => resolveSettings(projectId || state.projectId),
  setOverride(projectId, key, value) {
    const p = projects.find((x) => x.id === projectId);
    if (value === undefined) delete p.overrides[key]; else p.overrides[key] = value;
    emit('project-updated', projectId);
  },
  setProjectProfile(projectId, profileId) { projects.find((x) => x.id === projectId).profile = profileId; emit('project-updated', projectId); },
  setProfileProcess(profileId, processId) { profiles.find((x) => x.id === profileId).process = processId; emit('project-updated'); },
  project: (id) => projects.find((p) => p.id === (id || state.projectId)),
  updateProject(id, patch) { Object.assign(projects.find((p) => p.id === id), patch); emit('project-updated', id); },
  sessions: () => delay(sessions),
  sessionsSync: () => sessions,
  session: (id) => sessions.find((s) => s.id === id),
  lanes: () => delay(lanes),
  claims: () => delay(claims),
  overlaps: () => delay(overlaps),
  overlapsSync: () => overlaps,
  approvals: () => delay(approvals),
  approvalsSync: () => approvals,
  resolveApproval(id, decision) {
    const i = approvals.findIndex((a) => a.id === id);
    if (i < 0) return null;
    const [a] = approvals.splice(i, 1);
    audit.unshift({ at: Date.now(), actor: 'mali', session: a.session, action: decision, target: a.title, result: decision === 'deny' ? 'denied' : 'ok' });
    if (a.kind === 'permission' && a.session === 's-03') {
      const s = sessions.find((x) => x.id === 's-03');
      s.status = decision === 'deny' ? 'running' : 'running';
    }
    emit('approvals', approvals.length);
    return a;
  },
  events: () => delay(events),
  wikiTree: () => delay(wikiTree),
  wikiPage: (id) => {
    const meta = wikiTree.flatMap((s) => s.pages).find((p) => p.id === id);
    const body = wikiPages[id] || { sources: ['wiki/raw/2026-11-02-notes.md'], body: [['p', meta ? meta.title + '.' : '']] };
    return delay({ ...meta, ...body });
  },
  drafts: () => delay(drafts),
  resolveDrafts(ids, decision) {
    ids.forEach((id) => {
      const i = drafts.findIndex((d) => d.id === id);
      if (i >= 0) drafts.splice(i, 1);
    });
    const a = approvals.find((x) => x.kind === 'draft');
    if (a) {
      a.drafts = a.drafts.filter((d) => !ids.includes(d));
      if (!a.drafts.length) approvals.splice(approvals.indexOf(a), 1);
    }
    audit.unshift({ at: Date.now(), actor: 'mali', action: decision, target: ids.length + ' wiki drafts', result: 'ok' });
    emit('approvals', approvals.length);
  },
  jobs: () => delay(jobs),
  runs: () => delay(runs),
  toggleJob(id) { const j = jobs.find((x) => x.id === id); j.paused = !j.paused; emit('jobs'); return j; },
  redLines: () => delay(redLines),
  addRedLine(text) { redLines.push({ id: 'r-' + Math.random().toString(36).slice(2, 7), key: 'custom', text, locked: false }); },
  removeRedLine(id) { const i = redLines.findIndex((r) => r.id === id && !r.locked); if (i >= 0) redLines.splice(i, 1); },
  audit: () => delay(audit),
  autoDigest: () => delay(autoDigest),
  revertAuto(id) { const i = autoDigest.findIndex((x) => x.id === id); if (i >= 0) autoDigest.splice(i, 1); audit.unshift({ at: Date.now(), actor: 'mali', action: 'revert', target: 'Auto decision ' + id, result: 'ok' }); },
  promotions: () => delay(promotions),
  resolvePromotion(id) { const i = promotions.findIndex((x) => x.id === id); if (i >= 0) promotions.splice(i, 1); },
  notifications: () => delay(notifications),
  notificationsSync: () => notifications,
  markAllRead() { notifications.forEach((n) => (n.unread = false)); emit('notifications'); },
  doctor: () => delay(doctor, 400),
  chat: (sid) => delay(chat[sid] || []),
  chatSync: (sid) => (chat[sid] = chat[sid] || []),
  chatMeta: (sid) => (chatMeta[sid] = chatMeta[sid] || { pinned: false, archived: false, effort: 'medium', ctxUsed: 0, ctxWindow: 200_000, draft: '', goal: '' }),
  isStreaming: (sid) => (chat[sid] || []).some((m) => m.streaming),
  // how: send | queue | steer | interrupt
  sendMessage(sid, text, how = 'send', attachments = []) {
    const log = (chat[sid] = chat[sid] || []);
    if (how === 'interrupt') { log.forEach((m) => { if (m.streaming) { m.streaming = false; m.text += ' …'; m.cut = true; } }); this._unread(sid, 'unused'); }
    log.push({ t: 'msg', who: 'user', author: 'mali', at: Date.now(), text, how: how === 'send' ? null : how, files: attachments, delivery: how === 'queue' || how === 'steer' ? 'pending' : null });
    const meta = this.chatMeta(sid); meta.draft = ''; meta.ctxUsed += Math.round(text.length * 1.3) + 400;
    emit('chat', sid);
    const replies = {
      send: 'Understood. I will keep working locally and stop at the next decision point.',
      queue: 'Noted. I will pick this up after the current step.',
      steer: 'Adjusting course now without stopping: I will apply that to the remaining steps.',
      interrupt: 'Stopped the previous step. Starting again with your new instruction.',
    };
    this._reply(sid, replies[how] || replies.send, 600, log[log.length - 1]);
  },
  _reply(sid, full, delayMs = 600, trigger = null) {
    const log = chat[sid];
    const reply = { t: 'msg', who: 'agent', at: Date.now() + delayMs, text: '', streaming: true };
    setTimeout(() => {
      if (trigger?.delivery === 'unused') return; // stopped before it was read: no reply to it
      log.push(reply); emit('chat', sid);
      let i = 0;
      const tick = setInterval(() => {
        if (!reply.streaming) { clearInterval(tick); emit('chat', sid); return; }
        i += 3; reply.text = full.slice(0, i); emit('chat-stream', sid);
        if (i >= full.length) { clearInterval(tick); reply.streaming = false; this.chatMeta(sid).ctxUsed += full.length * 2; this._unread(sid, 'read'); emit('chat', sid); }
      }, 28);
    }, delayMs);
  },
  stopStreaming(sid) { (chat[sid] || []).forEach((m) => { if (m.streaming) { m.streaming = false; m.cut = true; } }); this._unread(sid, 'unused'); emit('chat', sid); },
  // Settle every waiting steer/queue message: read by the agent, or never used.
  _unread(sid, to) { (chat[sid] || []).forEach((m) => { if (m.delivery === 'pending') m.delivery = to; }); },
  // Pending message actions: send it now (interrupt), or drop it.
  sendQueuedNow(sid, idx) { const m = chat[sid][idx]; if (!m || m.delivery !== 'pending') return; chat[sid].splice(idx, 1); this.sendMessage(sid, m.text, 'interrupt'); },
  dropQueued(sid, idx) { const m = chat[sid][idx]; if (!m || m.delivery !== 'pending') return; chat[sid].splice(idx, 1); emit('chat', sid); },
  // Who already holds files that overlap this sub-task's scope (other sessions only).
  scopeConflicts(id) {
    const x = sessions.find((q) => q.id === id); if (!x?.scope?.length) return [];
    return claims.filter((c) => c.session !== id && c.status !== 'released')
      .map((c) => ({ claim: c, files: c.paths.filter((cp) => x.scope.some((sp) => pathsOverlap(sp, cp))) }))
      .filter((r) => r.files.length);
  },
  categories: () => categories,
  categoryChain: (id) => categories.find((c) => c.id === id)?.chain || [],
  moveInChain(cat, i, d) { const ch = this.categoryChain(cat); const j = i + d; if (j < 0 || j >= ch.length) return false; [ch[i], ch[j]] = [ch[j], ch[i]]; emit('categories'); return true; },
  setChainStep(cat, i, patch) { Object.assign(this.categoryChain(cat)[i], patch); emit('categories'); },
  addToChain(cat) { const ch = this.categoryChain(cat); const used = new Set(ch.map((c) => c.model)); const m = ['haiku', 'sonnet', 'opus'].find((q) => !used.has(q)); if (!m) return false; ch.push({ model: m, effort: 'medium' }); emit('categories'); return true; },
  removeFromChain(cat, i) { const ch = this.categoryChain(cat); if (ch.length <= 1) return false; ch.splice(i, 1); emit('categories'); return true; },
  // Edit a user message and send it again: everything after it is dropped (kept in a fork first).
  editAndResend(sid, idx, text) {
    const log = chat[sid];
    log.splice(idx);
    this.sendMessage(sid, text);
  },
  regenerate(sid, idx) {
    const log = chat[sid];
    log.splice(idx);
    this._reply(sid, 'Here is another take: I would finish the contract tests first, then ask you about the push in one batch with the fixture question.', 200);
    emit('chat', sid);
  },
  // Fork: a new top-level session that starts with the conversation up to idx.
  forkSession(sid, idx) {
    const src = sessions.find((x) => x.id === sid);
    const id = 's-' + Math.random().toString(36).slice(2, 7);
    const roots = sessions.filter((x) => !x.parent).length;
    sessions.push({ ...src, id, num: String(roots + 1), parent: null, title: src.title + ' (fork)', status: 'running', heartbeat: Date.now(), tokens: 0, cost: 0, viaHpk: true, owner: 'mali' });
    chat[id] = structuredClone(chat[sid].slice(0, idx + 1));
    chatMeta[id] = { ...this.chatMeta(sid), pinned: false, draft: '' };
    emit('sessions'); return id;
  },
  newSession({ title, parent = null, lane = null, mode = 'ask', model = 'sonnet', effort = 'medium', worktree = true, firstMessage = '', plan = null }) {
    const id = 's-' + Math.random().toString(36).slice(2, 7);
    const siblings = sessions.filter((x) => x.parent === parent);
    const p = parent ? sessions.find((x) => x.id === parent) : null;
    const num = p ? `${p.num}.${siblings.length + 1}` : String(sessions.filter((x) => !x.parent).length + 1);
    const sub = parent ? { category: 'deep', effort, deps: [], ev: ['plan', 'notrun'], repairs: 0, role: 'build', merged: false, ...(plan || {}) } : {};
    sessions.push({ id, num, parent, title, lane, owner: 'mali', model, status: sub.status || 'running', heartbeat: Date.now(), tokens: 0, cost: 0, viaHpk: true, mode, worktree: worktree ? `worktrees/${(lane || 'main')}-${id.slice(2)}` : null, ...sub });
    chat[id] = [];
    chatMeta[id] = { pinned: false, archived: false, effort, ctxUsed: 2_400, ctxWindow: 200_000, draft: '', goal: parent ? '' : firstMessage.split(/\r?\n/)[0].slice(0, 120), criteria: [] };
    if (p) (chat[p.id] = chat[p.id] || []).push({ t: 'child', at: Date.now(), session: id });
    if (firstMessage) {
      this.sendMessage(id, firstMessage);
      // A main chat splits the work into sub-tasks by itself (simulated here).
      if (!parent) setTimeout(() => this.autoSplit(id, firstMessage), 2600);
    }
    emit('sessions'); return id;
  },
  // The agent decides how to split work; the human never has to pick "parent" or "child".
  // It always ends the plan with a verify sub-task that checks the goal's criteria.
  autoSplit(sid, goal) {
    const log = chat[sid];
    if (!log) return;
    const base = goal.split(/[.\n]/)[0].slice(0, 40).trim() || 'task';
    const slug = base.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 24) || 'task';
    const meta = this.chatMeta(sid);
    meta.criteria = [`Goal met: ${base}`, 'Tests pass on the changed code', 'No red line crossed without approval'].map((text) => ({ text, result: null }));
    log.push({ t: 'msg', who: 'agent', at: Date.now(), text: `I split this into 4 sub-tasks. The two that write files claimed their scope on the BOARD (no overlap found); research and verify are read-only. The last one checks the goal against ${meta.criteria.length} criteria before anything is merged.` });
    const ids = [];
    const steps = [
      { title: `Research: ${base}`, model: 'haiku', plan: { category: 'quick', effort: 'low', ev: ['research', 'running'], scope: [] } },
      { title: `Change: ${base}`, model: 'sonnet', plan: { category: 'deep', effort: 'high', ev: ['code', 'notrun'], status: 'queued', scope: [`src/${slug}/**`] }, after: [0] },
      { title: `Tests: ${base}`, model: 'sonnet', plan: { category: 'deep', effort: 'medium', ev: ['test', 'notrun'], status: 'queued', scope: [`tests/${slug}/**`] }, after: [1] },
      { title: 'Verify the goal', model: 'opus', plan: { category: 'review', effort: 'high', ev: ['verify', 'notrun'], status: 'queued', role: 'verify', scope: [] }, after: [0, 1, 2] },
    ];
    steps.forEach((st, k) => setTimeout(() => {
      // Model and effort come from the category's chain, not from a guess.
      const head = categories.find((c) => c.id === st.plan.category)?.chain[0] || { model: st.model, effort: st.plan.effort };
      ids[k] = this.newSession({ title: st.title, parent: sid, model: head.model, effort: head.effort, plan: { ...st.plan, effort: head.effort, deps: (st.after || []).map((j) => ids[j]) } });
      // Every sub-task that writes claims its scope on the BOARD before it starts.
      const x = sessions.find((q) => q.id === ids[k]);
      if (x.scope.length) claims.push({ id: 'c-' + String(148 + claims.length).padStart(4, '0'), task: st.title, lane: null, session: x.id, paths: x.scope.slice(), wiki: [], started: Date.now(), heartbeat: Date.now(), status: 'active' });
      if (k === steps.length - 1) this._simulatePlan(sid, ids);
    }, 300 * (k + 1)));
    emit('chat', sid);
  },
  // Mock only: walk the plan forward so the panel shows real movement.
  _simulatePlan(root, ids) {
    const step = (ms, fn) => new Promise((r) => setTimeout(() => { fn(); emit('sessions'); r(); }, ms));
    const set = (id, patch) => { const x = sessions.find((q) => q.id === id); if (x && x.status !== 'stopped') Object.assign(x, patch, { heartbeat: Date.now() }); return x; };
    (async () => {
      await step(5000, () => { set(ids[0], { status: 'done', ev: ['research', 'seen'] }); set(ids[1], { status: 'running', ev: ['code', 'running'] }); });
      await step(6000, () => { set(ids[1], { status: 'done', ev: ['code', 'reported'] }); set(ids[2], { status: 'running', ev: ['test', 'running'] }); });
      await step(6000, () => { set(ids[2], { status: 'done', ev: ['test', 'verified'] }); set(ids[1], { ev: ['test', 'verified'] }); set(ids[3], { status: 'running', ev: ['verify', 'running'] }); });
      await step(5000, () => {
        const v = set(ids[3], { status: 'done', ev: ['verify', 'verified'] });
        if (!v || v.status !== 'done') return;
        this.chatMeta(root).criteria.forEach((c) => { c.result = 'pass'; });
        (chat[root] = chat[root] || []).push({ t: 'summary', at: Date.now(), session: ids[1], done: 'Change made and covered by tests.', decided: 'Kept the change inside its worktree until merge.', lessons: '', open: '', question: '' });
        emit('chat', root);
      });
    })();
  },
  steerSubtask(id, text) { this.sendMessage(id, text, 'steer'); this._planNote(id, 'steered'); },
  stopSubtask(id) {
    const x = sessions.find((q) => q.id === id); if (!x) return;
    x.status = 'stopped'; x.ev = [x.ev[0], 'cancelled'];
    this.stopStreaming(id); this._planNote(id, 'stopped'); emit('sessions');
  },
  // Retry = a repair attempt; capped so a broken node cannot loop forever.
  retrySubtask(id) {
    const x = sessions.find((q) => q.id === id); if (!x || x.repairs >= MAX_REPAIRS) return false;
    x.repairs += 1; x.status = 'running'; x.ev = [x.ev[0] === 'verify' ? 'verify' : 'code', 'running']; x.heartbeat = Date.now();
    this._planNote(id, 'retried'); emit('sessions'); return true;
  },
  // Merging into the main chat needs evidence: a passing test, review or verify.
  mergeSubtask(id) {
    const x = sessions.find((q) => q.id === id); if (!x || x.ev[1] !== 'verified' || x.merged || x.status === 'running' || x.status === 'waiting') return false;
    x.merged = true; x.status = 'done'; this._planNote(id, 'merged'); emit('sessions'); return true;
  },
  _planNote(id, action) {
    const x = sessions.find((q) => q.id === id); if (!x?.parent) return;
    let root = x; while (root.parent) root = sessions.find((q) => q.id === root.parent) || { parent: null };
    if (!root.id) return;
    (chat[root.id] = chat[root.id] || []).push({ t: 'note', at: Date.now(), kind: 'plan', action, session: id });
    emit('chat', root.id);
  },
  renameSession(sid, title) { const s = sessions.find((x) => x.id === sid); if (s && title) s.title = title; emit('sessions'); },
  setChatMeta(sid, patch) { Object.assign(this.chatMeta(sid), patch); emit('chat-meta', sid); },
  setSessionProp(sid, patch) { Object.assign(sessions.find((x) => x.id === sid), patch); emit('chat-meta', sid); },
  archiveSession(sid, on = true) { this.chatMeta(sid).archived = on; emit('sessions'); },
  deleteSession(sid) { const i = sessions.findIndex((x) => x.id === sid); if (i >= 0) sessions.splice(i, 1); delete chat[sid]; emit('sessions'); },
  compress(sid) { const m = this.chatMeta(sid); const before = m.ctxUsed; m.ctxUsed = Math.round(m.ctxUsed * 0.32); (chat[sid] = chat[sid] || []).push({ t: 'note', at: Date.now(), kind: 'compressed', from: before, to: m.ctxUsed }); emit('chat', sid); },
  // ---- model switching (docs/model-switch.md; internal/modelswitch plans the same way) ----
  modelPlan(sid) {
    const sess = sessions.find((x) => x.id === sid);
    const meta = this.chatMeta(sid);
    const cur = modelById(sess.model) || MODEL_CATALOG[1];
    const recall = Math.min(4000, (meta.recall?.pages || []).reduce((n, x) => n + x.tokens, 0));
    const state = { history: meta.ctxUsed, tail: Math.min(meta.ctxUsed, 18_000), brief: 900, recall };
    const policy = this.project().modelPolicy === 'local_only' ? 'local_only' : '';
    const options = MODEL_CATALOG.map((m) => planSwitch(cur, state, m, policy));
    const switches = (chat[sid] || []).filter((m) => m.t === 'switch');
    return delay({ live: !!sess.viaHpk, busy: this.isStreaming(sid), current: cur.id, policy, state, options, switches }, 120);
  },
  async switchModel(sid, id, { confirm = false } = {}) {
    const plan = await this.modelPlan(sid);
    const o = plan.options.find((x) => x.model.id === id);
    if (!o) throw new Error('no such model');
    if (o.locked === 'local_only') throw new Error('local models only');
    if (o.crossProvider && !confirm) throw new Error('confirm the red line');
    if (plan.busy) throw new Error('busy');
    const sess = sessions.find((x) => x.id === sid);
    const meta = this.chatMeta(sid);
    (chat[sid] = chat[sid] || []).push({ t: 'switch', at: Date.now(), from: plan.current, to: id, way: o.way, fill: o.fill });
    sess.model = o.model.name;
    meta.ctxWindow = o.model.window;
    meta.ctxUsed = o.carry;
    emit('chat', sid); emit('chat-meta', sid); emit('sessions');
    return { ok: true, way: o.way };
  },
  // A one-question sub-task on another model; it reports back here. This chat keeps its model.
  async askOnce(sid, id, text, { confirm = false } = {}) {
    const plan = await this.modelPlan(sid);
    const o = plan.options.find((x) => x.model.id === id);
    if (!o || o.locked === 'local_only') throw new Error('local models only');
    if (o.crossProvider && !confirm) throw new Error('confirm the red line');
    const sess = sessions.find((x) => x.id === sid);
    const cid = this.newSession({ title: `${sess.title} › ask ${o.model.name}`, parent: sid, model: o.model.name, worktree: false, firstMessage: text,
      plan: { category: 'review', role: 'ask', ev: null, askOnce: true } });
    setTimeout(() => {
      const c = sessions.find((x) => x.id === cid);
      if (c) c.status = 'done';
      (chat[sid] = chat[sid] || []).push({ t: 'summary', at: Date.now(), session: cid, done: `${o.model.name}: the plan holds; add a test for token expiry before merging.`, decided: '', lessons: '', open: '', question: '' });
      emit('chat', sid); emit('sessions');
    }, 2400);
    return cid;
  },
  answerClarify(sid, idx, answer) { const c = chat[sid][idx]; c.answered = answer; this.sendMessage(sid, answer); },
  // Turn a message into a wiki draft (needs approval like every core write).
  wikiText(sid, idx) { const m = chat[sid][idx]; return m ? (m.text || [m.done, m.decided, m.lessons].filter(Boolean).join('\n')) : ''; },
  checkWiki(sid, idx) { return admitToWiki(this.wikiText(sid, idx)); },
  admissions: () => admissions,
  // as: 'keep' creates a draft (which still needs approval); 'defer' only logs it.
  saveToWiki(sid, idx, as = 'keep') {
    const text = this.wikiText(sid, idx);
    const from = chat[sid][idx]?.session || sid; // a sub-task summary is sourced from the sub-task
    const check = admitToWiki(text);
    if (check.reason === 'secret') { admissions.unshift({ at: Date.now(), from, verdict: 'refuse', reason: 'secret', summary: '(hidden)', path: null }); emit('wiki-admit'); return null; }
    if (as === 'defer') { admissions.unshift({ at: Date.now(), from, verdict: 'defer', reason: check.reason, summary: text.slice(0, 90), path: null }); emit('wiki-admit'); return null; }
    const n = drafts.length ? Math.max(...drafts.map((d) => d.n)) + 1 : 1;
    const path = 'lessons/from-chat-' + n + '.md';
    drafts.push({ id: 'd-' + Date.now(), n, path, kind: 'new', area: 'lessons', from, summary: text.slice(0, 90), sources: ['session ' + from], diff: [['add', '# Note from chat'], ['add', ''], ...text.split('\n').map((l) => ['add', l])] });
    admissions.unshift({ at: Date.now(), from, verdict: 'keep', reason: check.verdict === 'keep' ? 'keep' : check.reason, override: check.verdict !== 'keep', summary: text.slice(0, 90), path });
    const pr = projects.find((x) => x.id === state.projectId); pr.drafts += 1;
    emit('project-updated', pr.id); emit('wiki-admit');
    return path;
  },
  nextDraftPath() { const n = drafts.length ? Math.max(...drafts.map((d) => d.n)) + 1 : 1; return 'lessons/from-chat-' + n + '.md'; },
  providers: () => providers,
  accounts: () => delay(accounts),
  accountsSync: () => accounts,
  accountInUse: (provider) => accounts.find((a) => a.provider === provider && a.status === 'active'),
  failoverPolicy: () => failoverPolicy,
  setPolicy(patch) { Object.assign(failoverPolicy, patch); emit('accounts'); },
  switchLog: () => delay(switchLog),
  // Move an account up (-1) or down (+1) within its provider's order.
  moveAccount(id, delta) {
    const a = accounts.find((x) => x.id === id);
    const peers = accounts.filter((x) => x.provider === a.provider).sort((x, y) => x.order - y.order);
    const i = peers.indexOf(a); const j = i + delta;
    if (j < 0 || j >= peers.length) return false;
    [peers[i].order, peers[j].order] = [peers[j].order, peers[i].order];
    emit('accounts'); return true;
  },
  placeAccount(id, beforeId) {
    const a = accounts.find((x) => x.id === id);
    const peers = accounts.filter((x) => x.provider === a.provider && x.id !== id).sort((x, y) => x.order - y.order);
    const at = beforeId ? peers.findIndex((x) => x.id === beforeId) : peers.length;
    peers.splice(at < 0 ? peers.length : at, 0, a);
    peers.forEach((x, k) => (x.order = k + 1));
    emit('accounts');
  },
  setAccountEnabled(id, on) {
    const a = accounts.find((x) => x.id === id);
    a.enabled = on;
    if (!on && a.status === 'active') { a.status = 'disabled'; this.failover(a.provider, 'manual'); }
    else a.status = on ? (a.used >= a.limit ? 'exhausted' : 'ready') : 'disabled';
    emit('accounts');
  },
  addAccount({ provider, label, method, key, limit, unit, window: win, projects: pr }) {
    const peers = accounts.filter((x) => x.provider === provider);
    const fp = method === 'oauth' ? 'signed in' : (key ? key.slice(0, 6) + '…' + key.slice(-4) : '—');
    accounts.push({ id: 'acc-' + Math.random().toString(36).slice(2, 7), provider, label, method, fp, order: peers.length + 1, enabled: true, status: peers.some((x) => x.status === 'active') ? 'ready' : 'active', used: 0, limit: Number(limit) || 0, unit, window: win, resetAt: null, lastUsed: null, projects: pr });
    emit('accounts');
  },
  removeAccount(id) { const i = accounts.findIndex((x) => x.id === id); if (i >= 0) accounts.splice(i, 1); emit('accounts'); },
  // Switch a provider to the next usable account in order. Returns the log entry.
  failover(provider, reason = 'quota') {
    const list = accounts.filter((x) => x.provider === provider).sort((x, y) => x.order - y.order);
    const cur = list.find((x) => x.status === 'active');
    if (cur && reason !== 'manual') { cur.status = 'exhausted'; cur.used = cur.limit; }
    const next = list.find((x) => x.enabled && x.status === 'ready');
    if (!next) { emit('accounts'); return null; }
    next.status = 'active'; next.lastUsed = Date.now();
    const entry = { at: Date.now(), provider, from: cur?.id, to: next.id, reason, session: 's-04' };
    switchLog.unshift(entry);
    if (failoverPolicy.notify) {
      notifications.unshift({ id: 'n-' + Date.now(), at: Date.now(), lamp: 'caution', title: `${cur?.label || '—'} → ${next.label}`, desc: provider, unread: true, href: '#/accounts' });
      emit('notify');
    }
    emit('accounts');
    return entry;
  },
  stopSession(sid) { const s = sessions.find((x) => x.id === sid); if (s) s.status = 'stopped'; emit('sessions'); },
  scanProject: (path) => delay({
    path,
    files: 1843, languages: [['TypeScript', 61], ['Markdown', 14], ['SQL', 9], ['YAML', 6], ['Other', 10]],
    commits: 2417, contributors: 6, lastCommit: ago(55),
    sources: [
      { path: 'README.md', kind: 'readme', tokens: 2_100, pick: true },
      { path: 'docs/**', kind: 'docs', files: 34, tokens: 61_400, pick: true },
      { path: 'notes/**', kind: 'notes', files: 58, tokens: 92_800, pick: true },
      { path: 'adr/**', kind: 'adr', files: 11, tokens: 18_300, pick: true },
      { path: 'CHANGELOG.md', kind: 'changelog', tokens: 44_900, pick: false },
      { path: 'AGENTS.md, CLAUDE.md', kind: 'agents', files: 2, tokens: 6_200, pick: true },
      { path: '.github/workflows/**', kind: 'ci', files: 5, tokens: 3_900, pick: true },
      { path: 'docker-compose.yml', kind: 'infra', tokens: 900, pick: true },
    ],
    skipped: ['.env', '.env.local', 'secrets/**', 'node_modules/**', 'dist/**'],
  }, 900),
};

// Simulated live movements so the page feels alive (replace with SSE).
const liveFeed = [
  { lamp: 'clear', who: 'agent', session: 's-02', text: '1.1 · tests green on token rotation (42 passed)' },
  { lamp: 'clear', who: 'agent', session: 's-07', text: '1.2.1 · regenerated 3 fixtures, kept legacy users' },
  { lamp: 'route', who: 'agent', session: 's-01', text: '1 · coordinator updated lane summary' },
  { lamp: 'clear', who: 'auto', session: 's-04', text: '1.3 · Auto approved: run pdf snapshot tests' },
  { lamp: 'caution', who: 'agent', session: 's-04', text: '1.3 · wrote a wiki draft: lessons/pdf-fonts.md' },
  { lamp: 'clear', who: 'agent', session: 's-02', text: '1.1 · committed 2 files in worktrees/web-auth (local)' },
];
let feedIdx = 0;
setInterval(() => {
  if (document.hidden || state.live) return; // demo only: never invent events over real data
  const e = { ...liveFeed[feedIdx++ % liveFeed.length], at: Date.now() };
  events.unshift(e);
  if (events.length > 14) events.pop();
  emit('event', e);
  if (e.lamp === 'caution') {
    notifications.unshift({ id: 'n-' + Date.now(), at: Date.now(), lamp: 'caution', title: e.text, desc: '', unread: true, href: '#/wiki/drafts' });
    emit('notify');
  }
}, 18_000);

// Simulated live heartbeat so lamps move while the page is open.
setInterval(() => {
  if (document.hidden || state.live) return;
  sessions.forEach((s) => { if (s.status === 'running') s.heartbeat = Date.now() - Math.random() * 60_000; });
  emit('heartbeat');
}, 15_000);

// ---- Live data (hpk serve) ----
// live.js swaps the contents of these in place, so every view keeps working with the
// same shapes. Without hpk serve (the dev server) the demo data above stays.
export const store = { processes, profiles, people, projects, sessions, claims, overlaps, approvals, wikiTree, wikiPages, drafts, notifications, events, chat, chatMeta, lanes, audit, accounts };
export const fill = (arr, items) => { arr.splice(0, arr.length, ...items); };
export const clearObject = (o) => { Object.keys(o).forEach((k) => delete o[k]); };
export { emit };
