import { readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const artifacts = resolve(dirname(fileURLToPath(import.meta.url)), '../../../.artifacts/player-acceptance');
const files = process.argv.slice(2).map((path) => resolve(path));
if (!files.length) throw new Error('Provide the full-run report followed by any targeted rerun reports.');
const scenarios = new Map();
const runs = [];
const unknownRequests = [];
let auditCount = 0;

for (const file of files) {
  const report = JSON.parse(await readFile(file, 'utf8'));
  runs.push({ file, stats: report.stats });
  async function visit(suite, parents = []) {
    const titles = [...parents, suite.title];
    for (const spec of suite.specs ?? []) {
      for (const test of spec.tests) {
        const key = `${test.projectName}:${suite.file}:${spec.title}`;
        const scenario = scenarios.get(key) ?? { title: [...titles.slice(1), spec.title].join(' / '), project: test.projectName, file: suite.file, runs: [] };
        for (const result of test.results) {
          scenario.runs.push({ report: file, status: result.status, startTime: result.startTime, duration: result.duration, errors: result.errors.map((error) => error.message) });
          for (const attachment of result.attachments ?? []) {
            if (attachment.name !== 'fixture-api-audit') continue;
            const text = attachment.body ? Buffer.from(attachment.body, 'base64').toString('utf8') : await readFile(attachment.path, 'utf8');
            const audit = JSON.parse(text);
            unknownRequests.push(...audit.unknownRequests);
            auditCount += 1;
          }
        }
        scenario.status = scenario.runs.at(-1).status;
        scenarios.set(key, scenario);
      }
    }
    for (const child of suite.suites ?? []) await visit(child, titles);
  }
  for (const suite of report.suites) await visit(suite);
}

const cases = [...scenarios.values()];
const result = {
  generatedAt: new Date().toISOString(),
  scope: 'Local Chromium acceptance against an isolated in-memory API fixture and a real VP8/Opus video.',
  latestResultPerScenario: true,
  total: cases.length,
  passed: cases.filter((entry) => entry.status === 'passed').length,
  failed: cases.filter((entry) => entry.status !== 'passed').length,
  apiAuditAttachments: auditCount,
  unknownRequests,
  reportRuns: runs,
  scenarios: cases,
};
await writeFile(resolve(artifacts, 'acceptance-summary.json'), `${JSON.stringify(result, null, 2)}\n`);
const rows = cases.map((entry, index) => `| ${index + 1} | ${entry.title.replaceAll('|', '\\|')} | ${entry.status} | ${entry.runs.length} |`).join('\n');
const markdown = `# Player acceptance results\n\n${result.passed} of ${result.total} scenarios passed; ${result.failed} remain failed. ${auditCount} API audit attachments contain ${unknownRequests.length} unknown requests.\n\nThis summary selects the latest result for each scenario. Original reports and all earlier outcomes remain in the JSON evidence; a targeted rerun does not rewrite a full-run report.\n\n| # | Scenario | Latest result | Recorded runs |\n| --- | --- | --- | --- |\n${rows}\n\n## Scope\n\nReal Chromium decoded the generated 30-second VP8/Opus file and exercised seeking, resume, subtitles, reporting, controls and episode transitions. The API fixture only modifies in-memory test data. These results do not establish real-server HLS encoding, GPU operation, storage performance or external-player application behavior.\n\n## Source reports\n\n${runs.map((entry) => `- ${entry.file}`).join('\n')}\n`;
await writeFile(resolve(artifacts, 'acceptance-summary.md'), markdown);
console.log(JSON.stringify({ total: result.total, passed: result.passed, failed: result.failed, apiAuditAttachments: auditCount, unknownRequests: unknownRequests.length }));
