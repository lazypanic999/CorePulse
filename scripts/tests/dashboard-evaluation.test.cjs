'use strict';

// Render the real dashboard functions against DOM fixtures and reports generated
// from examples/offline. No browser, HTTP server, cluster or real fetch is used.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const root = path.resolve(__dirname, '../..');
const directory = process.env.COREPULSE_SYNTHETIC_DIR || path.join(root, 'reports/neural-synthetic');
const report = JSON.parse(fs.readFileSync(path.join(directory, 'evaluation.json'), 'utf8'));
const references = JSON.parse(fs.readFileSync(path.join(directory, 'references.json'), 'utf8'));
assert.equal(report.synthetic, true, 'Dashboard checks require explicitly synthetic generated inputs');
assert.equal(references.synthetic, true);
assert.equal(report.schema_version, 3);
assert.equal(references.schema_version, 2);
assert.equal(report.windows_sha256, references.windows_sha256);

const app = fs.readFileSync(path.join(root, 'internal/dashboard/web/app.js'), 'utf8');
const html = fs.readFileSync(path.join(root, 'internal/dashboard/web/index.html'), 'utf8');
function actualFunction(start, next) {
  const begin = app.indexOf(start);
  const end = app.indexOf(next, begin);
  assert.ok(begin >= 0 && end > begin, 'Expected real dashboard function boundaries');
  return app.slice(begin, end);
}
const functions = actualFunction('function cell(', '\nasync function refreshRuns(')
  + actualFunction('async function refreshEvaluation()', '\nfunction drawSeries(');

class Element {
  constructor(tag, hidden = false) {
    this.tagName = tag;
    this.hidden = hidden;
    this.children = [];
    this.text = '';
    this.className = '';
  }
  set textContent(value) { this.text = String(value); this.children = []; }
  get textContent() { return this.text + this.children.map(child => child.textContent).join(''); }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.text = ''; this.children = [...children]; }
}

function harness(initial) {
  const elements = new Map();
  for (const match of html.matchAll(/<([a-z][a-z0-9]*)\b([^>]*?)\bid="([^"]+)"([^>]*)>/gi)) {
    elements.set(`#${match[3]}`, new Element(match[1], /\bhidden\b/.test(match[2] + match[4])));
  }
  const select = selector => {
    assert.ok(elements.has(selector), `The real HTML must contain ${selector}`);
    return elements.get(selector);
  };
  let payload = initial;
  let okay = true;
  const context = vm.createContext({
    $: select,
    document: { createElement: tag => new Element(tag) },
    clockTime: value => value,
    fetch: async (url, options) => {
      assert.equal(url, '/api/evaluation', 'Only the mocked offline snapshot endpoint is allowed');
      assert.equal(options.cache, 'no-store');
      return { ok: okay, json: async () => payload };
    },
  });
  vm.runInContext(functions, context, { filename: 'dashboard-evaluation-fixture.js' });
  return {
    element: select,
    render: async (next = payload, ok = true) => {
      payload = next;
      okay = ok;
      await context.refreshEvaluation();
    },
  };
}

test('schema-three render retains four references, four neural seeds, mistakes and abstentions', async () => {
  const before = JSON.stringify(report);
  const ui = harness({ available: true, report });
  assert.equal(ui.element('#evaluation-synthetic').hidden, true);
  await ui.render();
  assert.equal(ui.element('#evaluation-results').hidden, false);
  assert.equal(ui.element('#evaluation-empty').hidden, true);
  assert.equal(ui.element('#evaluation-synthetic').hidden, false);
  const rows = ui.element('#evaluation-body').children;
  assert.equal(rows.length, 8);
  assert.deepEqual(rows.slice(0, 4).map(row => row.children[0].textContent), [
    'Nearest centroid', 'Majority scenario', 'Normal-only deviation', 'Packet rule',
  ]);
  assert.deepEqual(rows.slice(4).map(row => row.children[0].textContent).sort(), [
    'autoencoder_seed_23', 'autoencoder_seed_7', 'mlp_seed_23', 'mlp_seed_7',
  ]);
  const predictions = ui.element('#evaluation-predictions').children;
  assert.equal(predictions.length, 48, 'Every held-out prediction from every method must stay visible');
  assert.ok(predictions.some(row => row.children[4].textContent === 'Abstained'));
  assert.ok(predictions.some(row => row.children[4].children[0].className === 'outcome bad'));
  const seeds = ui.element('#evaluation-seeds').children;
  assert.equal(seeds.length, 2);
  for (const item of seeds) {
    assert.match(item.textContent, /all 2 seeds \(7, 23\)/);
    assert.match(item.textContent, /not a confidence interval/);
  }
  assert.match(ui.element('#evaluation-support').textContent, /Whole-session grouping/);
  assert.equal(JSON.stringify(report), before, 'Rendering cannot change the saved report');
});

test('a legacy report clears extra methods and seed summaries', async () => {
  const ui = harness({ available: true, report });
  await ui.render();
  await ui.render({ available: true, report: references });
  assert.equal(ui.element('#evaluation-body').children.length, 4);
  assert.equal(ui.element('#evaluation-predictions').children.length, 24);
  assert.equal(ui.element('#evaluation-seeds').children.length, 0);
  assert.equal(ui.element('#evaluation-synthetic').hidden, false);
});

test('additional named methods render without a hardcoded neural-family allowlist', async () => {
  const extra = JSON.parse(JSON.stringify(report));
  // Reused reference predictions are a display fixture, not a new trained method.
  extra.models.custom_fixture_method = JSON.parse(JSON.stringify(extra.models.nearest_centroid));
  const ui = harness({ available: true, report: extra });
  await ui.render();
  assert.equal(ui.element('#evaluation-body').children.length, 9);
  assert.equal(ui.element('#evaluation-predictions').children.length, 54);
  assert.ok(ui.element('#evaluation-body').children.some(row => row.children[0].textContent === 'custom_fixture_method'));
});

test('schema-one run reports need no session or seed metadata', async () => {
  const legacy = JSON.parse(JSON.stringify(references));
  legacy.schema_version = 1;
  legacy.split.schema_version = 1;
  for (const key of ['grouping', 'session_assignments', 'train_sessions', 'test_sessions', 'purged_sessions']) {
    delete legacy.split[key];
  }
  const ui = harness({ available: true, report: legacy });
  await ui.render();
  assert.equal(ui.element('#evaluation-results').hidden, false);
  assert.equal(ui.element('#evaluation-body').children.length, 4);
  assert.equal(ui.element('#evaluation-predictions').children.length, 24);
  assert.equal(ui.element('#evaluation-seeds').children.length, 0);
  assert.match(ui.element('#evaluation-support').textContent, /Whole-run grouping/);
  assert.equal(ui.element('#evaluation-synthetic').hidden, false);
});

test('unavailable and failed snapshots hide the score panel', async () => {
  const ui = harness({ available: true, report });
  await ui.render();
  await ui.render({ available: false, message: 'No invented report' });
  assert.equal(ui.element('#evaluation-results').hidden, true);
  assert.equal(ui.element('#evaluation-empty').hidden, false);
  assert.equal(ui.element('#evaluation-message').textContent, 'No invented report');
  await ui.render({}, false);
  assert.equal(ui.element('#evaluation-results').hidden, true);
  assert.match(ui.element('#evaluation-message').textContent, /could not be loaded/);
});
