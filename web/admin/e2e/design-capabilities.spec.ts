import { csrf, expect, json, library, stamp, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';
import type { LibraryStorageDirectory } from '../src/api';
import type { CodecInterface, HardwareCapabilities } from '../src/hardwareCapabilitiesApi';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

function mode(compiled: boolean | null, selected = false): CodecInterface {
  return { Managed: true, Compiled: compiled, Selected: selected, Implementations: ['fixture-interface'] };
}

function capabilities(current = false): HardwareCapabilities {
  return {
    ObservedAt: stamp, CompiledAt: current ? stamp : null, CompiledState: current ? 'current' : 'unknown', FFmpegVersion: current ? 'fixture-9.0.1' : '',
    TranscodingAvailable: true, Selected: { Decode: 'vaapi', Encode: 'vaapi', DeviceId: 'amd-device' },
    Devices: [{ DeviceId: 'amd-device', Label: 'AMD Radeon GPU', Available: true, Code: '' }],
    Items: [{ Codec: 'h264', Name: 'H.264 / AVC', SoftwareDecode: mode(current ? true : null), SoftwareEncode: mode(current ? true : null),
      HardwareDecode: mode(current ? true : null, true), HardwareEncode: mode(current ? true : null, true), EncodingProofs: [] }],
  };
}

test('codec interfaces are read explicitly and never presented as successful GPU execution', async ({ page, api }, testInfo) => {
  let observed = capabilities();
  api.handlers.set('GET /admin/v1/hardware/codecs', async (route) => json(route, observed));
  api.handlers.set('POST /admin/v1/hardware/codecs/refresh', async (route) => { observed = capabilities(true); await json(route, observed); });
  await page.goto('/admin/settings/transcode');
  const panel = page.getByRole('region', { name: 'Codec capabilities', exact: true });
  // Settings groups use semantic sections, whose label supplies the region.
  await expect(panel.getByRole('table', { name: 'Codec interface matrix', exact: true })).toBeVisible();
  await expect(panel.getByText('Unknown', { exact: true })).toHaveCount(4);
  expect(api.writes()).toEqual([]);
  await panel.getByRole('button', { name: 'Refresh codec interfaces', exact: true }).click();
  await expect(panel.getByText('Interface present', { exact: true })).toHaveCount(4);
  await expect(panel).not.toContainText('Passed for this sample');
  await expect(panel).toContainText('Device and source compatibility still require actual execution.');
  const writes = api.writes();
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({ method: 'POST', path: '/admin/v1/hardware/codecs/refresh', body: {}, csrf });
  await noOverflow(page);
  await panel.scrollIntoViewIfNeeded();
  await evidence(page, testInfo, 'codec-interface-matrix-en-light');
});

test('codec refresh failure keeps a visibly stale observation and does not retry automatically', async ({ page, api }) => {
  api.handlers.set('GET /admin/v1/hardware/codecs', async (route) => json(route, capabilities(true)));
  api.handlers.set('POST /admin/v1/hardware/codecs/refresh', async (route) => json(route, { Error: { Code: 'codec_inventory_unavailable', Message: 'The configured FFmpeg capability information could not be read. Check the server tool configuration and try again.' } }, 503));
  await page.goto('/admin/settings/transcode');
  const panel = page.getByRole('region', { name: 'Codec capabilities', exact: true });
  await expect(panel.getByText('Interface present', { exact: true })).toHaveCount(4);
  await panel.getByRole('button', { name: 'Refresh codec interfaces', exact: true }).click();
  await expect(panel.getByRole('alert')).toContainText('Showing the last successful capability observation.');
  await expect(panel.getByText('Interface present', { exact: true })).toHaveCount(4);
  expect(api.writes()).toHaveLength(1);
  await panel.getByRole('button', { name: 'Read latest observation', exact: true }).click();
  await expect(panel.getByRole('alert')).toHaveCount(0);
  expect(api.writes()).toHaveLength(1);
});

test.describe('Chinese mobile directory access', () => {
  test.use({ viewport: { width: 390, height: 844 }, preferences: { language: 'zh', theme: 'dark' } });
  test('read-only, writable, and unknown access retain separate server observations', async ({ page, api }, testInfo) => {
    const capacity = { TotalBytes: '107374182400', UsedBytes: '42949672960', AvailableBytes: '59055800320' };
    const items: LibraryStorageDirectory[] = [
      { Path: '/media/read-only', Available: true, Readable: true, Writable: false, ...capacity },
      { Path: '/media/writable', Available: true, Readable: true, Writable: true, ...capacity },
      { Path: '/media/unknown', Available: true, Readable: true, Writable: null, ...capacity, AvailableBytes: null },
    ];
    api.handlers.set('GET /admin/v1/libraries', async (route) => json(route, {
      Items: [{ ...library, Paths: items.map((item) => item.Path) }], TotalRecordCount: 1,
    }));
    api.handlers.set('GET /admin/v1/storage/library-directories', async (route) => json(route, { Items: items, TotalRecordCount: items.length }));
    await page.goto('/admin/media/libraries');
    const directories = page.getByRole('region', { name: '媒体目录', exact: true });
    await expect(directories.getByRole('listitem', { name: '/media/read-only', exact: true })).toContainText('只读');
    await expect(directories.getByRole('listitem', { name: '/media/writable', exact: true })).toContainText('可读写');
    await expect(directories.getByRole('listitem', { name: '/media/unknown', exact: true })).toContainText('写权限未知');
    await expect(directories.getByRole('listitem', { name: '/media/read-only', exact: true })).toHaveAttribute('data-access', 'success');
    await expect(directories.getByRole('listitem', { name: '/media/writable', exact: true })).toHaveAttribute('data-access', 'success');
    await expect(directories.getByRole('listitem', { name: '/media/unknown', exact: true })).toHaveAttribute('data-access', 'unknown');
    for (const item of items) await expect(directories.getByRole('listitem', { name: item.Path, exact: true }).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '40');
    await noOverflow(page);
    await directories.scrollIntoViewIfNeeded();
    await evidence(page, testInfo, 'directory-access-zh-dark-mobile');
    expect(api.writes()).toEqual([]);
  });
});
