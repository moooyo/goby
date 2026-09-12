#!/usr/bin/env node
/** Exercise the production DOM observer with authored documents in an isolated Linux network namespace. */
import fs from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import path from 'node:path';
import { networkInterfaces } from 'node:os';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';

const NODE = '/opt/goby-test/exec-work-m3e/client-library-changed-source44-tool-01/node';
const NODE_SHA256 = '3517c2df0b2f8cd7f422b4b8450ef81c6889f08eb03e281d6de9079b15e6a327';
const PLAYWRIGHT = '/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright';
const DRIVER_NAME = 'client-browser-library-changed-source55.mjs';
const SHA256 = /^[0-9a-f]{64}$/;
const NETNS = /^net:\[[1-9][0-9]*\]$/;
const ITEM = '268051d3ca734aefcf94e245fb25ad55';
const LIBRARY = 'a9993591e72f0f2e7babcbf8b9c50790';
const SERVER = 'c7cfd76b1dee728b2bad523793a37ccb';
const NAME = 'M3e Client Movie';
const FORBIDDEN = 'Offline DOM regression marker';
const DOCUMENT = 'http://127.0.0.1:18196/web/index.html';
const MOVIES = DOCUMENT + '#!/videos?serverId=' + SERVER + '&parentId=' + LIBRARY;
const MOVIES_ROUTE = new URL(MOVIES).pathname + new URL(MOVIES).hash;
const REQUIRED = ['driver', 'driver-sha256', 'outer-netns'];
const WORK_DEADLINE = performance.now() + 60000;

function requireThat(value, code) {
  if (!value) throw Object.assign(new Error('offline_dom_regression_failed'), { safeCode: code });
}

async function within(operation, maximum = 5000, code = 'operation_deadline', cleanup = false) {
  const remaining = cleanup ? maximum : Math.min(maximum, WORK_DEADLINE - performance.now());
  requireThat(remaining > 0, 'work_deadline');
  let timer;
  try {
    return await Promise.race([Promise.resolve().then(operation), new Promise((_, reject) => {
      timer = setTimeout(() => reject(Object.assign(new Error('offline_dom_regression_failed'), { safeCode: code })), remaining);
    })]);
  } finally { clearTimeout(timer); }
}

function argumentsRecord(argv) {
  requireThat(argv.length === REQUIRED.length * 2, 'arguments');
  const result = {};
  for (let index = 0; index < argv.length; index += 2) {
    const option = argv[index];
    requireThat(typeof option === 'string' && option.startsWith('--'), 'arguments');
    const name = option.slice(2), value = argv[index + 1];
    requireThat(REQUIRED.includes(name) && !Object.hasOwn(result, name) && typeof value === 'string' && value.length > 0, 'arguments');
    result[name] = value;
  }
  requireThat(path.isAbsolute(result.driver) && path.normalize(result.driver) === result.driver &&
    path.basename(result.driver) === DRIVER_NAME && SHA256.test(result['driver-sha256']) && NETNS.test(result['outer-netns']), 'arguments');
  return result;
}

async function fileHash(filename) {
  const info = await fs.lstat(filename);
  requireThat(info.isFile() && !info.isSymbolicLink() && info.size > 0 && info.size <= 1024 * 1024 * 1024, 'runtime_file');
  const hash = createHash('sha256');
  for await (const bytes of createReadStream(filename)) hash.update(bytes);
  return hash.digest('hex');
}

async function requireIsolatedNetwork(outer) {
  const current = await fs.readlink('/proc/self/ns/net');
  const initial = await fs.readlink('/proc/1/ns/net');
  const devices = (await fs.readFile('/proc/self/net/dev', 'utf8')).trim().split('\n').slice(2)
    .map(line => line.split(':', 1)[0].trim());
  const routes = (await fs.readFile('/proc/self/net/route', 'utf8')).trim().split('\n');
  const routes6 = (await fs.readFile('/proc/self/net/ipv6_route', 'utf8')).trim().split('\n').filter(Boolean);
  // Linux can retain unreachable loopback sentinel routes in an otherwise empty namespace.
  const unreachable6 = routes6.every(line => { const fields = line.trim().split(/\s+/);
    return fields.length === 10 && fields[9] === 'lo' && /^[0-9a-f]{8}$/i.test(fields[8]) &&
      (Number.parseInt(fields[8], 16) & 0x200) !== 0; });
  requireThat(NETNS.test(current) && NETNS.test(initial) && current !== outer && current !== initial &&
    devices.length === 1 && devices[0] === 'lo' && routes.length === 1 && unreachable6 &&
    Object.keys(networkInterfaces()).length === 0, 'network_namespace');
}

const title = () => '<button class="cardTextActionButton" type="button">' + NAME + '</button>';
const card = (buttons = title(), attributes = '') => '<div class="card"' + attributes +
  '><div class="cardBox"><div class="cardText">' + buttons + '</div></div></div>';
const container = (contents = '', className = '') => '<div class="itemsContainer ' + className + '">' + contents + '</div>';
const counts = (containers, owners, cards, buttons, titles, consistent, observed = titles === 1 ? NAME : null) => ({
  visible_items_containers: containers, visible_card_containers: owners, visible_cards: cards,
  visible_target_cards: cards, visible_title_buttons: buttons, target_title_count: titles,
  forbidden_title_count: 0, explicit_identity_consistent: consistent, observed_title: observed,
});

const CASES = Object.freeze([
  { name: 'one_owner', positive: true, body: container(card()), expected: counts(1, 1, 1, 1, 1, true) },
  { name: 'three_visible_containers_one_owner', positive: true,
    body: container() + container(card()) + container(), expected: counts(3, 1, 1, 1, 1, true) },
  { name: 'zero_size_owner_visible_card', positive: true,
    body: container(card(), 'zero'), expected: counts(0, 1, 1, 1, 1, true) },
  { name: 'hidden_sibling_ignored', positive: true,
    body: container(card(), 'hidden') + container(card()), expected: counts(1, 1, 1, 1, 1, true) },
  { name: 'two_populated_containers', positive: false,
    body: container(card()) + container(card()), expected: counts(2, 2, 2, 2, 2, false) },
  { name: 'two_cards_one_container', positive: false,
    body: container(card() + card()), expected: counts(1, 1, 2, 2, 2, false) },
  { name: 'duplicate_title_buttons', positive: false,
    body: container(card(title() + title())), expected: counts(1, 1, 1, 2, 2, false) },
  { name: 'title_belongs_to_another_container', positive: false,
    body: container(card('')) + container(title()), expected: counts(2, 1, 1, 1, 1, false) },
  { name: 'only_hidden_container', positive: false,
    body: container(card(), 'hidden'), expected: counts(0, 0, 0, 0, 0, false) },
  { name: 'conflicting_explicit_identity', positive: false,
    body: container(card(title(), ' data-id="11111111111111111111111111111111"')),
    expected: counts(1, 1, 1, 1, 1, false) },
  { name: 'orphan_card_beside_owned_card', positive: false,
    body: container(card()) + card(''), expected: counts(1, 1, 2, 1, 1, false),
    orphan_counts: { visible_cards: 1 } },
  { name: 'orphan_title_beside_owned_card', positive: false,
    body: container(card()) + title(), expected: counts(1, 1, 1, 2, 2, false),
    orphan_counts: { visible_title_buttons: 1, target_title_count: 1 } },
  { name: 'zero_size_owner_clips_card', positive: false,
    body: container(card(), 'zero clipped'), expected: counts(0, 0, 0, 0, 0, false),
    require_positive_card_bounds: true },
]);

function documentHTML(body, caseName) {
  return '<!doctype html><html><head><meta charset="utf-8">' +
    '<meta http-equiv="Content-Security-Policy" content="default-src \'none\'; style-src \'unsafe-inline\'; base-uri \'none\'; form-action \'none\'">' +
    '<title>Offline DOM regression</title><style>' +
    'html,body{margin:0;padding:0}body{padding:12px;font:16px sans-serif}' +
    '.itemsContainer{position:relative;width:640px;min-height:32px;margin:0 0 12px;padding:0}' +
    '.card{box-sizing:border-box;width:320px;min-height:64px;padding:12px;margin:0 0 8px}' +
    '.cardBox,.cardText{min-height:32px}.cardTextActionButton{display:inline-block;padding:4px;font:inherit}' +
    '.itemsContainer.hidden{display:none}.itemsContainer.zero{width:0;height:0;min-height:0;margin:0;overflow:visible}' +
    '.itemsContainer.zero.clipped{overflow:hidden}' +
    '.zero>.card{position:absolute;left:0;top:0}' +
    '</style></head><body data-offline-dom-case="' + caseName + '">' + body + '</body></html>';
}

function assertObservation(value, testCase) {
  requireThat(value !== null && typeof value === 'object' && value.route === MOVIES_ROUTE &&
    value.document_id === 'offline-dom-document' && value.expected_name === NAME && value.started_elapsed_ms === null &&
    value.target_id === null && value.identity_mode === 'unbound' && value.wire_identity === null &&
    value.identity_proven === false && value.passed === false && value.media_inactive === true, 'raw_identity');
  for (const [key, expected] of Object.entries(testCase.expected)) requireThat(value[key] === expected, 'dom_counts');
  const observations = value.container_observations;
  requireThat(Array.isArray(observations) && observations.length >= 1 && observations.length <= 3, 'container_observations');
  for (const [index, owner] of observations.entries()) {
    requireThat(owner.index === index && typeof owner.visible === 'boolean' && owner.tag === 'div' &&
      Array.isArray(owner.classes) && owner.classes.length === 1 && owner.classes[0] === 'itemsContainer' &&
      ['visible_cards', 'visible_title_buttons', 'target_title_count', 'forbidden_title_count']
        .every(key => Number.isSafeInteger(owner[key]) && owner[key] >= 0) &&
      ['x', 'y', 'width', 'height'].every(key => Number.isFinite(owner.rect?.[key])), 'container_observations');
  }
  requireThat(observations.filter(owner => owner.visible).length === value.visible_items_containers &&
    observations.filter(owner => owner.visible_cards > 0).length === value.visible_card_containers, 'container_ownership');
  for (const key of ['visible_cards', 'visible_title_buttons', 'target_title_count', 'forbidden_title_count']) {
    const owned = observations.reduce((sum, owner) => sum + owner[key], 0), orphan = testCase.orphan_counts?.[key] ?? 0;
    requireThat(value[key] - owned === orphan, 'container_ownership');
  }
  if (testCase.orphan_counts) requireThat(value.explicit_identity_consistent === false &&
    Object.values(testCase.orphan_counts).some(count => count > 0), 'orphan_identity');
  const singleton = value.visible_card_containers === 1 && value.visible_cards === 1 && value.visible_title_buttons === 1 &&
    value.target_title_count === 1 && value.forbidden_title_count === 0 && value.explicit_identity_consistent === true;
  requireThat(singleton === testCase.positive, 'singleton_structure');
}

const summary = { marker: 'goby-source55-offline-dom-regression-v1', result: 'failed', phase: 'arguments', cases_total: CASES.length,
  client_acceptance: false, library_changed_client_acceptance: false, full_m3_complete: false,
  cases_passed: 0, positive_cases: CASES.filter(value => value.positive).length,
  negative_cases: CASES.filter(value => !value.positive).length, network_isolated: false,
  fulfilled_documents: 0, aborted_requests: 0 };
let browser = null, context = null, currentCase = null, failed = false;

try {
  const args = argumentsRecord(process.argv.slice(2));
  summary.phase = 'runtime_environment';
  requireThat(process.platform === 'linux' && process.getuid?.() === 0 && process.versions.node.split('.')[0] === '22' &&
    process.execPath === NODE && ['DEBUG', 'PWDEBUG', 'NODE_OPTIONS', 'NODE_PATH'].every(key => !process.env[key]), 'runtime_environment');
  summary.phase = 'network_namespace';
  await within(() => requireIsolatedNetwork(args['outer-netns']));
  summary.network_isolated = true;
  summary.phase = 'runtime_hash';
  const [nodeHash, driverHash] = await within(() => Promise.all([fileHash(process.execPath), fileHash(args.driver)]), 30000);
  requireThat(nodeHash === NODE_SHA256 && driverHash === args['driver-sha256'], 'runtime_hash');
  summary.driver_sha256 = args['driver-sha256'];
  summary.phase = 'playwright_module';
  const require = createRequire(import.meta.url);
  const playwrightVersion = require(path.join(PLAYWRIGHT, 'package.json')).version;
  requireThat(playwrightVersion === '1.63.0', 'playwright_version');
  summary.playwright_version = playwrightVersion;
  const { chromium } = require(PLAYWRIGHT);
  summary.phase = 'chromium_path';
  const executable = chromium.executablePath();
  requireThat(path.isAbsolute(executable), 'chromium_path');
  summary.phase = 'production_observer_import';
  const { observeLibraryChangedDOM } = await within(() => import(pathToFileURL(args.driver).href));
  requireThat(typeof observeLibraryChangedDOM === 'function', 'production_observer');
  summary.phase = 'browser_launch';
  // Playwright owns the debugging transport and supplies its default pipe.
  browser = await within(() => chromium.launch({ executablePath: executable, headless: true, timeout: 15000,
    env: { HOME: '/root', LANG: 'C.UTF-8', PATH: '/usr/bin:/bin' },
    args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking',
      '--disable-component-update', '--disable-sync', '--disable-default-apps', '--disable-extensions',
      '--no-first-run', '--no-default-browser-check', '--metrics-recording-only'] }), 15000, 'browser_launch');
  summary.phase = 'browser_version';
  const browserVersion = browser.version();
  requireThat(typeof browserVersion === 'string' && /^[0-9]+(?:\.[0-9]+){1,3}$/.test(browserVersion), 'chromium_version');
  summary.chromium_version = browserVersion;
  for (const testCase of CASES) {
    currentCase = testCase.name;
    summary.phase = 'case_network_namespace';
    await within(() => requireIsolatedNetwork(args['outer-netns']));
    summary.phase = 'context_create';
    context = await within(() => browser.newContext({ viewport: { width: 1200, height: 1000 }, serviceWorkers: 'block',
      acceptDownloads: false, javaScriptEnabled: true }));
    context.setDefaultTimeout(5000);
    context.setDefaultNavigationTimeout(5000);
    summary.phase = 'context_route_install';
    await within(() => context.route('**/*', async route => { summary.aborted_requests++; await route.abort('blockedbyclient'); }));
    summary.phase = 'page_create';
    const page = await within(() => context.newPage());
    let documents = 0;
    summary.phase = 'page_route_install';
    await within(() => page.route('**/*', async route => {
      const request = route.request();
      if (documents === 0 && request.url() === DOCUMENT && request.method() === 'GET' &&
        request.resourceType() === 'document' && request.isNavigationRequest() && request.frame() === page.mainFrame()) {
        documents++; summary.fulfilled_documents++;
        await route.fulfill({ status: 200, contentType: 'text/html; charset=utf-8', body: documentHTML(testCase.body, testCase.name),
          headers: { 'Cache-Control': 'no-store', 'Content-Security-Policy': "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'" } });
      } else {
        summary.aborted_requests++;
        await route.abort('blockedbyclient');
      }
    }));
    summary.phase = 'document_navigation';
    await within(() => page.goto(MOVIES, { waitUntil: 'load', timeout: 5000 }), 5000, 'document_navigation');
    summary.phase = 'document_identity';
    requireThat(documents === 1 && page.url() === MOVIES &&
      await within(() => page.locator('body').getAttribute('data-offline-dom-case')) === testCase.name, 'authored_document');
    if (testCase.require_positive_card_bounds) {
      summary.phase = 'clipped_card_bounds';
      const bounds = await within(() => page.locator('.card').boundingBox());
      requireThat(bounds !== null && bounds.width > 0 && bounds.height > 0, 'clipped_card_bounds');
    }
    summary.phase = 'dom_observation';
    assertObservation(await within(() => observeLibraryChangedDOM(page, { id: ITEM, library_id: LIBRARY, name: NAME },
      NAME, FORBIDDEN, 'offline-dom-document')), testCase);
    summary.phase = 'context_cleanup';
    await within(() => context.close(), 5000, 'context_cleanup'); context = null;
    summary.cases_passed++;
  }
  summary.phase = 'case_inventory';
  requireThat(summary.cases_passed === CASES.length && summary.fulfilled_documents === CASES.length, 'case_inventory');
} catch (error) {
  failed = true;
  summary.failure_phase = summary.phase;
  summary.failure = typeof error?.safeCode === 'string' && /^[a-z_]+$/.test(error.safeCode) ? error.safeCode : 'browser_or_dom_operation';
  if (currentCase !== null) summary.failure_case = currentCase;
} finally {
  if (!failed) summary.phase = 'browser_cleanup';
  for (const resource of [context, browser]) if (resource !== null) {
    try { await within(() => resource.close(), 5000, 'browser_cleanup', true); }
    catch {
      failed = true; summary.cleanup_failed = true;
      if (!summary.failure) { summary.failure = 'browser_cleanup'; summary.failure_phase = 'browser_cleanup'; }
    }
  }
}
summary.result = failed ? 'failed' : 'passed';
summary.phase = failed ? summary.failure_phase : 'complete';
process.stdout.write(JSON.stringify(summary) + '\n');
if (failed) process.exitCode = 1;
