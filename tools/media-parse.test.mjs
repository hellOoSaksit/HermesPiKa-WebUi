// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// node --test tools/  — the chat's media parser (assets/console/js/media/parse.js)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { blocks, refs, kindOf, tableOf, parseCSV, chartSpec, chartFromTable, rowsFromJSON } from '../assets/console/js/media/parse.js';

test('finds files the agent names, in every common spelling', () => {
  const text = [
    'Saved ![viewport](renders/shot.png) and exported `renders/kart.fbx`.',
    'Windows: C:\\work\\game\\assets\\hero.glb, posix /srv/game/out/run.mp4, home ~/game/a.obj.',
    'Link [the deck](docs/plan.pdf); data at ./data/laps.csv. Not a file: e.g. or v1.2.',
    'Remote: https://example.com/x/y.webm',
  ].join('\n');
  const got = refs(text).map((r) => [r.src, r.kind]);
  assert.deepEqual(got, [
    ['renders/shot.png', 'image'], ['renders/kart.fbx', 'model'],
    ['C:\\work\\game\\assets\\hero.glb', 'model'], ['/srv/game/out/run.mp4', 'video'], ['~/game/a.obj', 'model'],
    ['docs/plan.pdf', 'pdf'], ['./data/laps.csv', 'data'],
    ['https://example.com/x/y.webm', 'video'],
  ]);
  assert.equal(refs('https://example.com/x/y.webm')[0].remote, true);
});

test('data URIs from tool output (e.g. a Blender viewport screenshot)', () => {
  const r = refs('screenshot: data:image/png;base64,' + 'A'.repeat(40) + ' done');
  assert.equal(r.length, 1);
  assert.equal(r[0].kind, 'image');
  assert.equal(r[0].inline, true);
  assert.equal(kindOf('data:video/mp4;base64,AAAA'), 'video');
});

test('blocks: fences, tables and text', () => {
  const b = blocks('Hi\n\n```chart\n{"series":[{"name":"a","data":[1,2]}]}\n```\n| a | b |\n|---|--:|\n| x | 1 |\n| y | 2 |\nbye');
  assert.deepEqual(b.map((x) => x.type), ['text', 'code', 'table', 'text']);
  assert.equal(b[1].lang, 'chart');
  assert.deepEqual(b[2].rows, [['a', 'b'], ['x', '1'], ['y', '2']]);
  // an unclosed fence stays text
  assert.equal(blocks('```js\nnope').length, 1);
});

test('tables become charts when they have numbers', () => {
  const tb = tableOf(parseCSV('lap,time,note\n1,"61.8",ok\n2,61.2,"wet, slow"\n3,60.9,\n'));
  assert.deepEqual(tb.numeric, [true, true, false]);
  assert.equal(tb.rows[1][2], 'wet, slow');
  const spec = chartFromTable(tb);
  assert.deepEqual(spec.labels, ['1', '2', '3']); // a numeric first column with a text column elsewhere: rows are numbered labels
  assert.ok(spec.series.length >= 1);
  assert.equal(chartFromTable(tableOf([['a'], ['x']])), null);
});

test('chart specs are checked', () => {
  assert.equal(chartSpec('not json'), null);
  assert.equal(chartSpec('{"series":[]}'), null);
  const s = chartSpec('{"type":"weird","series":[{"data":[1,"2",3]}]}');
  assert.equal(s.type, 'bar');
  assert.deepEqual(s.series[0].data, [1, 2, 3]);
  assert.deepEqual(s.labels, ['1', '2', '3']);
  assert.deepEqual(rowsFromJSON([{ a: 1, b: { c: 2 } }, { a: 3 }]), [['a', 'b'], ['1', '{"c":2}'], ['3', '']]);
});
