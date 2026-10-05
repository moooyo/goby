import { request } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, readdir, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { resolve } from 'node:path';

assert.equal(process.platform, 'linux');
const scope = '/opt/goby-test/player-live-20261004-165c/external-timeline-check';
const root = scope + '/live';
assert.equal((await readFile(root + '/.owner', 'utf8')).trim(), 'goby-external-timeline-165c-20261005');
const config = JSON.parse(await readFile(root + '/live.private.json', 'utf8'));
const baseline = JSON.parse(await readFile(root + '/artifacts/deployment-before-copy-final.json', 'utf8'));
const oracle = JSON.parse(await readFile(root + '/artifacts/scanned-tracks.json', 'utf8'));
const admin = await request.newContext();
const consumer = await request.newContext({ extraHTTPHeaders: { 'X-Emby-Token': config.token } });
try {
  const session = await admin.post(config.baseURL + '/admin/v1/session', { data: { Name: config.username, Password: config.password }, headers: { Origin: config.playerURL } });
  assert.equal(session.status(), 200);
  const tasks = await (await admin.get(config.baseURL + '/admin/v1/tasks')).json();
  const task = tasks.Items.find(value => value.Key === 'media.subtitle_timeline_generation');
  assert(task.Description.includes('external SUP files') && task.Description.includes('multilingual IDX/SUB pairs'));
  const initialRuns = (await (await admin.get(config.baseURL + `/admin/v1/tasks/${task.Id}/runs?Limit=1&StartIndex=0`)).json()).TotalRecordCount;
  const descriptorResponse = await consumer.get(config.baseURL + `/emby/Items/${config.itemId}/SubtitleTimelines?UserId=${config.userId}`);
  assert.equal(descriptorResponse.status(), 200);
  const descriptor = await descriptorResponse.json();
  assert.equal(descriptor.Available, true); assert.equal(descriptor.Stale, undefined);
  assert.equal(descriptor.SourceVersion, baseline.currentPublication.sourceStamp);
  assert.equal(descriptor.Streams.length, 3);
  const tracks = [];
  for (const expected of oracle.tracks) {
    const stream = descriptor.Streams.find(value => value.StreamIndex === expected.StreamIndex);
    assert(stream && stream.Codec === expected.Codec);
    const response = await consumer.get(config.baseURL + stream.Url); assert.equal(response.status(), 200);
    const payload = await response.json(); assert.deepEqual(payload.Intervals, expected.Intervals);
    assert.equal(payload.SourceVersion, baseline.currentPublication.sourceStamp);
    tracks.push({ streamIndex: stream.StreamIndex, intervals: payload.Intervals });
  }
  const adminDist = resolve(scope, 'source/web/admin/dist');
  const panel = (await readdir(adminDist + '/assets')).filter(name => name.startsWith('SubtitleTimelineDialog-') && name.endsWith('.js'));
  assert.equal(panel.length, 1);
  const served = [];
  for (const file of ['index.html', 'assets/' + panel[0]]) {
    const response = await consumer.get(config.baseURL + '/admin/' + (file === 'index.html' ? '' : file));
    assert.equal(response.status(), 200);
    const actual = createHash('sha256').update(await response.body()).digest('hex');
    const expected = createHash('sha256').update(await readFile(resolve(adminDist, file))).digest('hex');
    assert.equal(actual, expected);
    served.push({ path: file, sha256: actual });
  }
  const finalRuns = (await (await admin.get(config.baseURL + `/admin/v1/tasks/${task.Id}/runs?Limit=1&StartIndex=0`)).json()).TotalRecordCount;
  assert.equal(finalRuns, initialRuns);
  const result = { passed: true, taskDescription: task.Description, available: descriptor.Available,
    sourceVersionPreserved: true, generationPreserved: baseline.currentPublication.generation, tracks,
    embeddedAdminAssetsMatchFinalBuild: served, taskRunsBefore: initialRuns, taskRunsAfter: finalRuns };
  await writeFile(root + '/artifacts/backend-copy-final-verification.json', JSON.stringify(result, null, 2));
  console.log(JSON.stringify({ passed: true, availableTracks: tracks.length, generationPreserved: result.generationPreserved, adminAssetsMatched: served.length, taskRunsUnchanged: true }));
} finally {
  await admin.dispose(); await consumer.dispose();
}
