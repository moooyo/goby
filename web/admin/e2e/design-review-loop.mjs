import { createHash } from 'node:crypto';
import { spawn } from 'node:child_process';
import { copyFile, mkdir, readFile, readdir, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { dirname, isAbsolute, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const adminRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const repositoryRoot = resolve(adminRoot, '../..');
const reviewSpecs = [
  'dashboard-v2.spec.ts', 'design-rewrite.spec.ts', 'design-access.spec.ts', 'design-keyboard.spec.ts', 'design-storage.spec.ts', 'design-counts.spec.ts',
  'playback-management.spec.ts', 'design-media-summary.spec.ts', 'provider-configuration.spec.ts', 'notification-destinations.spec.ts',
  'server-policy.spec.ts', 'series-display-order.spec.ts', 'design-capabilities.spec.ts', 'backup-policy.spec.ts', 'design-user-activity.spec.ts',
  'design-capability-integration.spec.ts', 'design-logo-images.spec.ts', 'automatic-images.spec.ts',
  'design-focus-visibility.spec.ts',
  'setup-wizard.spec.ts', 'server-updates.spec.ts', 'playback-activity.spec.ts',
  'design-name-source.spec.ts',
  'design2-access.spec.ts', 'design2-attention.spec.ts', 'design2-backups.spec.ts', 'design2-collections.spec.ts',
  'design2-images.spec.ts', 'design2-library-images.spec.ts', 'design2-media-integration.spec.ts',
  'design2-storage.spec.ts', 'design2-tasks.spec.ts',
  'design2-metric-layout.spec.ts',
  'design2-task-backups.spec.ts',
  'design2-coverage-layout.spec.ts', 'design2-library-title.spec.ts',
  'design2-analysis-title.spec.ts',
  'design2-device-value-layout.spec.ts',
];
const require = createRequire(join(adminRoot, 'package.json'));
const args = process.argv.slice(2);
const command = args.shift();
const option = (name, fallback = '') => {
  const index = args.indexOf(`--${name}`);
  return index < 0 ? fallback : args[index + 1] ?? '';
};
const output = option('output');
if (!output || !isAbsolute(output)) throw new Error('Provide --output with an absolute evidence directory.');
const outputRoot = resolve(output);
const roundName = option('round');
const journalPath = join(outputRoot, 'loop.json');
const reviewScope = option('scope', 'administrator');
if (!['administrator', 'transcoding'].includes(reviewScope)) throw new Error('Use --scope administrator or transcoding.');
const selectedSpecs = reviewScope === 'transcoding' ? ['runtime-settings.spec.ts'] : reviewSpecs;

async function readJSON(path, fallback) {
  try { return JSON.parse(await readFile(path, 'utf8')); }
  catch (error) { if (error.code === 'ENOENT' && fallback !== undefined) return fallback; throw error; }
}
async function filesUnder(directory) {
  const paths = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) paths.push(...await filesUnder(path));
    else if (entry.isFile()) paths.push(path);
  }
  return paths.sort();
}
async function sourceHash(referencePath = '') {
  const hash = createHash('sha256');
  const files = [
    ...await filesUnder(join(adminRoot, 'src')),
    ...await filesUnder(join(adminRoot, 'e2e')),
    ...await filesUnder(join(adminRoot, 'build')),
    ...await filesUnder(join(adminRoot, 'dist')),
    ...await filesUnder(join(repositoryRoot, 'cmd')),
    ...await filesUnder(join(repositoryRoot, 'internal')),
    ...await filesUnder(join(repositoryRoot, 'web/player/src')),
    ...await filesUnder(join(repositoryRoot, 'web/player/tests')),
    ...await filesUnder(join(repositoryRoot, 'web/player/dist')),
    join(repositoryRoot, 'go.mod'), join(repositoryRoot, 'go.sum'),
    ...['package.json', 'package-lock.json', 'index.html', 'tsconfig.json', 'vite.config.ts', 'playwright.config.ts'].map((path) => join(repositoryRoot, 'web/player', path)),
    join(adminRoot, '../shared/tokens.css'),
    ...['package.json', 'package-lock.json', 'index.html', 'tsconfig.json', 'vite.config.ts', 'playwright.config.ts'].map((path) => join(adminRoot, path)),
  ];
  for (const file of files.sort()) {
    hash.update(relative(adminRoot, file).replaceAll('\\', '/'));
    hash.update(await readFile(file));
  }
  if (referencePath) {
    hash.update('design-reference\0' + resolve(referencePath));
    hash.update(await readFile(referencePath));
  }
  return hash.digest('hex');
}
async function servedProductionIndex(baseURL) {
  const expected = await readFile(join(adminRoot, 'dist', 'index.html'));
  const response = await fetch(new URL('/admin/', baseURL), { cache: 'no-store', signal: AbortSignal.timeout(10_000) });
  const actual = Buffer.from(await response.arrayBuffer());
  if (!response.ok || !actual.equals(expected)) throw new Error('Serve the current production dist before reviewing. The HTTP administrator entry does not match dist/index.html.');
  return createHash('sha256').update(actual).digest('hex');
}
async function saveJSON(path, value) { await writeFile(path, `${JSON.stringify(value, null, 2)}\n`); }
function validRound() {
  if (!/^round-\d{2,}$/.test(roundName)) throw new Error('Use an ordered round name such as --round round-01.');
  return join(outputRoot, roundName);
}
function stringArray(value, name) {
  if (!Array.isArray(value) || value.some((entry) => typeof entry !== 'string' || !entry.trim())) throw new Error(`${name} must be an array of nonempty strings.`);
  return value;
}
function summarize(journal) {
  let clearRounds = 0;
  let lastHash;
  let lastScope;
  for (const round of [...journal.rounds].reverse()) {
    const scope = round.reviewScope ?? 'administrator';
    if (!round.checksPassed || round.recommendations.length || lastHash && lastHash !== round.sourceHash || lastScope && lastScope !== scope) break;
    lastHash = round.sourceHash;
    lastScope = scope;
    clearRounds += 1;
  }
  return { consecutiveClearRounds: clearRounds, exitConditionMet: clearRounds >= 2, rounds: journal.rounds.length, latestRound: journal.rounds.at(-1)?.name ?? null };
}

await mkdir(outputRoot, { recursive: true });
if (command === 'run') {
  const roundDirectory = validRound();
  const current = await readJSON(join(roundDirectory, 'round.json'), null);
  if (current) throw new Error('This round already exists. Keep its raw evidence and choose a new round number.');
  const baseURL = option('base-url', process.env.GOBY_SMOKE_BASE_URL ?? 'http://127.0.0.1:18096');
  const referencePath = option('reference', process.env.GOBY_DESIGN_REFERENCE ?? '');
  if (referencePath && !isAbsolute(referencePath)) throw new Error('Provide --reference with an absolute design document path.');
  const reference = referencePath ? { path: resolve(referencePath), sha256: createHash('sha256').update(await readFile(referencePath)).digest('hex') } : null;
  for (const spec of selectedSpecs) {
    if (!(await stat(join(adminRoot, 'e2e', spec))).isFile()) throw new Error('Every required design review specification must exist.');
  }
  const productionIndexSha256 = await servedProductionIndex(baseURL);
  await mkdir(roundDirectory, { recursive: true });
  const screenshots = join(roundDirectory, 'screenshots');
  const reportPath = join(roundDirectory, 'playwright.json');
  const startedHash = await sourceHash(reference?.path);
  const metadata = { name: roundName, startedAt: new Date().toISOString(), sourceHash: startedHash, reference, reviewScope, reviewSpecs: selectedSpecs, verificationTarget: 'production-dist', productionIndexSha256, status: 'running', screenshots, reportPath };
  await saveJSON(join(roundDirectory, 'round.json'), metadata);
  const exitCode = await new Promise((resolveExit, reject) => {
    const child = spawn(process.execPath, [require.resolve('@playwright/test/cli'), 'test', ...selectedSpecs, '--reporter=list,json', '--output', join(roundDirectory, 'test-results')], {
      cwd: adminRoot,
      env: { ...process.env, GOBY_SMOKE_BASE_URL: baseURL, GOBY_DESIGN_EVIDENCE_DIR: screenshots, PLAYWRIGHT_JSON_OUTPUT_NAME: reportPath },
      stdio: 'inherit', windowsHide: true,
    });
    child.once('error', reject);
    child.once('exit', (code) => resolveExit(code ?? 1));
  });
  const endingHash = await sourceHash(reference?.path);
  let productionIndexUnchanged = false;
  try { productionIndexUnchanged = await servedProductionIndex(baseURL) === productionIndexSha256; } catch { /* A changed or unavailable server invalidates this round. */ }
  const report = await readJSON(reportPath, null);
  if (reviewScope === 'transcoding' && report) {
    // Materialize byte-backed Playwright attachments without changing their
    // content. Real-service captures are retained separately and reviewed too.
    await mkdir(screenshots, { recursive: true });
    const captureAttachments = async (suite) => {
      for (const spec of suite.specs ?? []) for (const test of spec.tests ?? []) for (const result of test.results ?? []) {
        for (const attachment of result.attachments ?? []) {
          if (attachment.contentType !== 'image/png' || !attachment.path) continue;
          const data = await readFile(attachment.path);
          const digest = createHash('sha256').update(data).digest('hex');
          const name = `fixture-${digest}.png`;
          await copyFile(attachment.path, join(screenshots, name));
        }
      }
      for (const child of suite.suites ?? []) await captureAttachments(child);
    };
    await captureAttachments(report);
  }
  const checksPassed = exitCode === 0 && report && report.stats.unexpected === 0 && report.stats.skipped === 0 && report.stats.expected > 0 && startedHash === endingHash && productionIndexUnchanged;
  await saveJSON(join(roundDirectory, 'round.json'), { ...metadata, status: checksPassed ? 'checks-passed-review-pending' : 'checks-failed', checksPassed: Boolean(checksPassed), exitCode, sourceChangedDuringRun: startedHash !== endingHash, productionIndexUnchanged, finishedAt: new Date().toISOString(), stats: report?.stats ?? null });
  await saveJSON(join(roundDirectory, 'review-template.json'), { reviewer: '', screenshotsReviewed: [], recommendations: [], capabilityDifferences: [], notes: ['Review the screenshots against the supplied design before recording this round. Passing automated checks alone is insufficient.'] });
  console.log(JSON.stringify({ round: roundName, checksPassed: Boolean(checksPassed), next: `Fill review-template.json after visual review, then use record --round ${roundName} --review <absolute-review-file>.` }, null, 2));
  process.exitCode = checksPassed ? 0 : 1;
} else if (command === 'record') {
  const roundDirectory = validRound();
  const metadata = await readJSON(join(roundDirectory, 'round.json'));
  const reviewPath = option('review');
  if (!isAbsolute(reviewPath)) throw new Error('Provide --review with the absolute path to the completed review JSON.');
  const review = await readJSON(reviewPath);
  if (typeof review.reviewer !== 'string' || !review.reviewer.trim()) throw new Error('A named reviewer is required.');
  const screenshotsReviewed = stringArray(review.screenshotsReviewed, 'screenshotsReviewed');
  if (!screenshotsReviewed.length) throw new Error('Review at least one captured screenshot and record its filename.');
  for (const screenshot of screenshotsReviewed) {
    const path = resolve(metadata.screenshots, screenshot);
    if (!relative(metadata.screenshots, path) || relative(metadata.screenshots, path).startsWith('..') || !(await stat(path)).isFile()) throw new Error('Reviewed screenshots must belong to this round.');
  }
  const recommendations = stringArray(review.recommendations, 'recommendations');
  const capabilityDifferences = stringArray(review.capabilityDifferences ?? [], 'capabilityDifferences');
  const notes = stringArray(review.notes ?? [], 'notes');
  const currentHash = await sourceHash(metadata.reference?.path);
  if (currentHash !== metadata.sourceHash) throw new Error('Source changed after the capture. Run a fresh round before recording a review.');
  const journal = await readJSON(journalPath, { version: 1, exitCondition: 'Two consecutive checked and visually reviewed rounds with no new modification recommendations for the same source hash.', rounds: [] });
  if (journal.rounds.some((round) => round.name === roundName)) throw new Error('A review for this round is already recorded. Preserve it and create a new round.');
  const round = { name: roundName, sourceHash: metadata.sourceHash, reference: metadata.reference ?? null, reviewScope: metadata.reviewScope ?? 'administrator', reviewSpecs: metadata.reviewSpecs ?? reviewSpecs, verificationTarget: metadata.verificationTarget, productionIndexSha256: metadata.productionIndexSha256, checksPassed: metadata.checksPassed === true, reviewedAt: new Date().toISOString(), reviewer: review.reviewer, screenshotsReviewed, recommendations, capabilityDifferences, notes };
  journal.rounds.push(round);
  journal.rounds.sort((a, b) => a.name.localeCompare(b.name));
  journal.status = summarize(journal);
  await saveJSON(join(roundDirectory, 'review.json'), round);
  await saveJSON(journalPath, journal);
  console.log(JSON.stringify(journal.status, null, 2));
} else if (command === 'status') {
  const journal = await readJSON(journalPath, { rounds: [] });
  const status = summarize(journal);
  const currentHash = await sourceHash(option('reference', journal.rounds.at(-1)?.reference?.path ?? process.env.GOBY_DESIGN_REFERENCE ?? ''));
  if (journal.rounds.length && journal.rounds.at(-1).sourceHash !== currentHash) {
    status.consecutiveClearRounds = 0;
    status.exitConditionMet = false;
    status.sourceChangedSinceReview = true;
  }
  console.log(JSON.stringify(status, null, 2));
  if (args.includes('--require-complete') && !status.exitConditionMet) process.exitCode = 1;
} else {
  throw new Error('Use run, record, or status. See e2e/design-review-checklist.md for the review protocol.');
}
