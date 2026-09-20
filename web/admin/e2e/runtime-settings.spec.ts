import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Locator, Page, Request, Route } from '@playwright/test';
import type { ServerSettings, SettingsUpdateInput } from '../src/api';
import type { RuntimeOverrides, RuntimeSettings } from '../src/runtimeSettings';

// All administrator API requests are synthetic and must match an explicit
// fixture response. Browser assets are the only real server dependency.
const timestamp = '2026-09-20T00:00:00Z';
const csrf = 'synthetic-runtime-settings-csrf';
const administrator = { Id: 'runtime-admin', Name: 'Runtime administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const settingsPath = '/admin/v1/settings';
const noRuntimeOverrides: RuntimeOverrides = { Network: null, Hardware: null, Threads: null, H264: null, HEVC: null, SoftwareToneMapping: null, VulkanToneMapping: null };

function runtime(): RuntimeSettings {
  const defaults = {
    Network: { BindHost: '127.0.0.1', HttpPort: 8096 }, Hardware: { Decode: 'software', Encode: 'software', DeviceId: '' }, Threads: 2,
    H264: { Preset: 'veryfast' as const, RateControl: 'bitrate' as const, CRF: 23 }, HEVC: { Preset: 'fast' as const, RateControl: 'bitrate' as const, CRF: 28 },
    SoftwareToneMapping: true, VulkanToneMapping: false,
  };
  const { Network: _network, ...effective } = defaults;
  return {
    Defaults: defaults, Overrides: { ...noRuntimeOverrides }, Effective: effective,
    Sources: { Network: { BindHost: 'deployment', HttpPort: 'deployment' }, Hardware: 'deployment', Threads: 'deployment', H264: 'deployment', HEVC: 'deployment', SoftwareToneMapping: 'deployment', VulkanToneMapping: 'deployment' },
    Network: { Desired: { ...defaults.Network }, Active: { Configured: { ...defaults.Network }, BoundHost: '127.0.0.1', HttpPort: 8096, Revision: '1' }, RestartRequired: false, ReconnectURL: '' },
    Hardware: { Available: true, Code: '', Devices: [{ DeviceId: 'amd-device-one', Label: 'AMD fixture GPU', Available: true, Code: '' }, { DeviceId: 'amd-offline', Label: 'Offline fixture GPU', Available: false, Code: 'device_unavailable' }] },
    Effects: { Network: 'restart', Hardware: 'next_admission', Threads: 'next_admission', H264: 'next_admission', HEVC: 'next_admission', SoftwareToneMapping: 'next_admission', VulkanToneMapping: 'next_admission' },
    Applicability: { H264: 'software_h264_output', HEVC: 'software_hevc_output', SoftwareToneMapping: 'software_filter', VulkanToneMapping: 'vulkan_filter' },
  };
}

function settings(): ServerSettings {
  const defaults = { ServerName: 'Runtime fixture server', MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2 };
  return {
    Revision: '9007199254740993', Defaults: defaults, Effective: { ...defaults }, Overrides: { ServerName: null, MaxBitrate: null, MaxWidth: null, MaxHeight: null, MaxAudioChannels: null },
    Sources: { ServerName: 'deployment', MaxBitrate: 'deployment', MaxWidth: 'deployment', MaxHeight: 'deployment', MaxAudioChannels: 'deployment' },
    UpdatedAt: timestamp, ServerNameMode: 'deployment', Encoding: { TranscodingMaxWidth: 0 },
    Deployment: { HostName: 'runtime-fixture', TranscodingEnabled: true, HardwareDecoder: 'software', HardwareEncoder: 'software', Threads: 2, MaxJobs: 4, MaxUserJobs: 2, MaxSessionJobs: 1 }, Runtime: runtime(),
  };
}

async function json(route: Route, value: unknown, status = 200): Promise<void> { await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) }); }
interface Captured { method: string; path: string; body: unknown; csrf?: string }
class RuntimeAPI {
  settings = settings(); requests: Captured[] = []; unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();
  writes(): Captured[] { return this.requests.filter((request) => request.method !== 'GET'); }
  apply(input: SettingsUpdateInput): void {
    this.settings.Revision = (BigInt(this.settings.Revision) + 1n).toString();
    this.settings.Overrides = input.Overrides; this.settings.ServerNameMode = input.ServerNameMode; this.settings.Encoding = input.Encoding;
    for (const field of ['MaxBitrate', 'MaxWidth', 'MaxHeight', 'MaxAudioChannels'] as const) {
      this.settings.Effective[field] = input.Overrides[field] ?? this.settings.Defaults[field];
      this.settings.Sources[field] = input.Overrides[field] === null ? 'deployment' : 'database';
    }
    const current = this.settings.Runtime!;
    if (input.Runtime === undefined) return;
    current.Overrides = input.Runtime === null ? { ...noRuntimeOverrides } : { ...current.Overrides, ...input.Runtime };
    current.Network.Desired = { BindHost: current.Overrides.Network?.BindHost ?? current.Defaults.Network.BindHost, HttpPort: current.Overrides.Network?.HttpPort ?? current.Defaults.Network.HttpPort };
    current.Sources.Network = { BindHost: current.Overrides.Network?.BindHost != null ? 'database' : 'deployment', HttpPort: current.Overrides.Network?.HttpPort != null ? 'database' : 'deployment' };
    current.Network.RestartRequired = JSON.stringify(current.Network.Desired) !== JSON.stringify(current.Network.Active?.Configured);
    current.Network.ReconnectURL = current.Network.RestartRequired ? `http://${current.Network.Desired.BindHost || '127.0.0.1'}:${current.Network.Desired.HttpPort}/admin/` : '';
    for (const field of ['Hardware', 'Threads', 'H264', 'HEVC', 'SoftwareToneMapping', 'VulkanToneMapping'] as const) {
      Object.assign(current.Effective, { [field]: current.Overrides[field] ?? current.Defaults[field] });
      current.Sources[field] = current.Overrides[field] === null ? 'deployment' : 'database';
    }
  }
  async install(context: BrowserContext): Promise<void> {
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const captured = { method: request.method(), path: new URL(request.url()).pathname, body: request.postData() ? request.postDataJSON() as unknown : null, csrf: request.headers()['x-csrf-token'] };
      this.requests.push(captured);
      const handler = this.handlers.get(`${captured.method} ${captured.path}`);
      if (handler) return handler(route, request);
      if (captured.method === 'GET') {
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (captured.path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrf });
        if (captured.path === settingsPath) return json(route, this.settings);
        if (captured.path === '/admin/v1/media-diagnostics') return json(route, { InstanceId: 'a'.repeat(32), StartToken: '', StartTokenExpiresAt: timestamp, Available: false, UnavailableReason: 'Synthetic fixture', HardwareConfigured: false, RetentionSeconds: 1800, MaxRetainedRuns: 32, Items: [] });
      }
      if (captured.method === 'PUT' && captured.path === settingsPath) {
        const input = captured.body as SettingsUpdateInput;
        if (input.Revision !== this.settings.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'Settings changed. Reload before saving.' } }, 409);
        this.apply(input); return json(route, this.settings);
      }
      this.unexpected.push(`${captured.method} ${captured.path}`);
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' } }, 501);
    });
  }
}

const test = base.extend<{ api: RuntimeAPI }>({ api: async ({ context, page }, use) => {
  const api = new RuntimeAPI(); const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message)); await api.install(context); await use(api);
  expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); expect(api.writes().every((request) => request.csrf === csrf)).toBe(true);
} });
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });
async function openSettings(page: Page): Promise<void> { await page.goto('/admin/settings'); await expect(page.getByRole('region', { name: 'HTTP listener', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Reload settings', exact: true })).toBeEnabled(); }
const group = (page: Page, name: string) => page.getByRole('region', { name, exact: true });
async function override(region: Locator, name: string): Promise<void> { await region.getByRole('switch', { name: `Use deployment default for ${name}`, exact: true }).uncheck(); }
async function select(page: Page, region: Locator, label: string, choice: string): Promise<void> { await region.getByRole('combobox', { name: label, exact: true }).click(); await page.getByRole('option', { name: choice, exact: true }).click(); }
async function save(page: Page, status = 200): Promise<void> {
  const [response] = await Promise.all([page.waitForResponse((value) => value.request().method() === 'PUT' && new URL(value.url()).pathname === settingsPath), page.getByRole('button', { name: 'Save settings', exact: true }).click()]);
  expect(response.status()).toBe(status);
  if (status === 200) await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  else await expect(page.getByRole('button', { name: 'Reload latest settings', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reload settings', exact: true })).toBeEnabled();
}

test('network changes retain the active listener and send a complete nullable group with exact CAS', async ({ page, api }) => {
  await openSettings(page); const listener = group(page, 'HTTP listener');
  await override(listener, 'HTTP port'); await listener.getByRole('textbox', { name: 'HTTP port', exact: true }).fill('9097');
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Revision).toBe('9007199254740993');
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ Network: { BindHost: null, HttpPort: 9097 } });
  await expect(listener).toContainText('127.0.0.1 · Port 8096'); await expect(listener).toContainText('127.0.0.1 · Port 9097');
  await expect(listener.getByRole('alert')).toContainText('requires a service restart');
  expect(new URL(page.url()).pathname).toBe('/admin/settings');
  expect(api.writes().every((request) => request.path === settingsPath && request.method === 'PUT')).toBe(true);
});

test('codec choices, explicit false, and null reset preserve separate override meanings', async ({ page, api }) => {
  await openSettings(page); const codec = group(page, 'H.264 CPU encoding'); const tone = group(page, 'Software tone mapping');
  await override(codec, 'H.264 CPU encoding'); await select(page, codec, 'H.264 CPU preset', 'slow'); await select(page, codec, 'H.264 rate control', 'Capped CRF');
  await codec.getByRole('textbox', { name: 'H.264 CRF', exact: true }).fill('20');
  await override(tone, 'software tone mapping'); await tone.getByRole('switch', { name: 'Enable software tone mapping', exact: true }).uncheck();
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ H264: { Preset: 'slow', RateControl: 'capped_crf', CRF: 20 }, SoftwareToneMapping: false });
  await codec.getByRole('switch', { name: 'Use deployment default for H.264 CPU encoding', exact: true }).check();
  await tone.getByRole('switch', { name: 'Use deployment default for software tone mapping', exact: true }).check();
  await save(page);
  expect((api.writes()[1].body as SettingsUpdateInput).Runtime).toEqual({ H264: null, SoftwareToneMapping: null });
  await expect(tone.getByRole('switch', { name: 'Enable software tone mapping', exact: true })).toBeChecked();
});

test('only reported available AMD devices can be selected and no device path is submitted', async ({ page, api }) => {
  await openSettings(page); const hardware = group(page, 'Hardware acceleration');
  await override(hardware, 'hardware acceleration'); await select(page, hardware, 'Video decoding', 'AMD VA-API'); await select(page, hardware, 'Video encoding', 'AMD VA-API');
  await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
  await hardware.getByRole('combobox', { name: 'AMD device', exact: true }).click();
  await expect(page.getByRole('option', { name: 'Offline fixture GPU (unavailable)', exact: true })).toBeDisabled();
  await page.getByRole('option', { name: 'AMD fixture GPU', exact: true }).click(); await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ Hardware: { Decode: 'vaapi', Encode: 'vaapi', DeviceId: 'amd-device-one' } });
  await select(page, hardware, 'Video decoding', 'CPU'); await select(page, hardware, 'Video encoding', 'CPU'); await save(page);
  expect((api.writes()[1].body as SettingsUpdateInput).Runtime).toEqual({ Hardware: { Decode: 'software', Encode: 'software', DeviceId: 'amd-device-one' } });
  expect(JSON.stringify(api.writes())).not.toContain('/dev/');
});

test('legacy deployment hardware and an unavailable saved device are displayed without silent replacement', async ({ page, api }) => {
  const current = api.settings.Runtime!;
  current.Defaults.Hardware = { Decode: 'qsv', Encode: 'qsv', DeviceId: '' };
  current.Overrides.Hardware = { Decode: 'vaapi', Encode: 'vaapi', DeviceId: 'amd-missing' };
  current.Effective.Hardware = { ...current.Overrides.Hardware }; current.Sources.Hardware = 'database'; current.Hardware.Available = false; current.Hardware.Code = 'hardware_device_missing';
  await openSettings(page); const hardware = group(page, 'Hardware acceleration');
  await expect(hardware).toContainText('qsv (deployment)'); await expect(hardware.getByRole('combobox', { name: 'AMD device', exact: true })).toContainText('Saved device is unavailable');
  const threads = group(page, 'Threads per job'); await override(threads, 'threads per job'); await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('4'); await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ Threads: 4 });
});

test('invalid ports, literal addresses, thread counts, and CRF values cannot be saved', async ({ page, api }) => {
  await openSettings(page); const listener = group(page, 'HTTP listener');
  await override(listener, 'HTTP port'); await listener.getByRole('textbox', { name: 'HTTP port', exact: true }).fill('0'); await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
  await listener.getByRole('textbox', { name: 'HTTP port', exact: true }).fill('65536'); await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
  await listener.getByRole('textbox', { name: 'HTTP port', exact: true }).fill('8097'); await override(listener, 'bind address'); await listener.getByRole('textbox', { name: 'Bind address', exact: true }).fill('example.com'); await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
  await listener.getByRole('textbox', { name: 'Bind address', exact: true }).fill('127.0.0.1');
  const threads = group(page, 'Threads per job'); await override(threads, 'threads per job'); await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('65'); await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
  await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('4');
  const codec = group(page, 'HEVC CPU encoding'); await override(codec, 'HEVC CPU encoding'); await select(page, codec, 'HEVC rate control', 'Capped CRF'); await codec.getByRole('textbox', { name: 'HEVC CRF', exact: true }).fill('17'); await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
  expect(api.writes()).toEqual([]);
});

test('runtime revision conflicts keep the draft and require an explicit reload before another save', async ({ page, api }) => {
  await openSettings(page); const threads = group(page, 'Threads per job'); await override(threads, 'threads per job'); await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('6');
  api.settings.Revision = '9007199254740994'; await save(page, 409);
  await expect(threads.getByRole('textbox', { name: 'Threads per job', exact: true })).toHaveValue('6'); await expect(threads.getByRole('textbox', { name: 'Threads per job', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Reload latest settings', exact: true }).click();
  await page.getByRole('dialog', { name: 'Discard unsaved settings changes?', exact: true }).getByRole('button', { name: 'Discard draft and reload', exact: true }).click();
  await expect(threads.getByRole('switch', { name: 'Use deployment default for threads per job', exact: true })).toBeChecked();
  expect(api.writes()).toHaveLength(1);
});

test('an invalid mutation response locks runtime controls until the saved state is reloaded', async ({ page, api }) => {
  api.handlers.set(`PUT ${settingsPath}`, async (route, request) => { api.apply(request.postDataJSON() as SettingsUpdateInput); await json(route, { ...api.settings, Runtime: { ...api.settings.Runtime, Effective: { Network: { BindHost: '', HttpPort: 8096 } } } }); });
  await openSettings(page); const threads = group(page, 'Threads per job'); await override(threads, 'threads per job'); await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('8');
  await page.getByRole('button', { name: 'Save settings', exact: true }).click();
  await expect(page.getByText('The result could not be confirmed.', { exact: false })).toBeVisible();
  await expect(threads.getByRole('textbox', { name: 'Threads per job', exact: true })).toHaveValue('8'); await expect(threads.getByRole('textbox', { name: 'Threads per job', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
});

test('an unrelated output limit edit omits Runtime and retains every runtime override', async ({ page, api }) => {
  api.settings.Runtime!.Overrides.Network = { BindHost: null, HttpPort: null };
  await openSettings(page); const width = group(page, 'Maximum width'); await override(width, 'maximum width'); await width.getByRole('textbox', { name: 'Maximum width', exact: true }).fill('1280'); await save(page);
  expect(api.writes()[0].body).not.toHaveProperty('Runtime');
  expect(api.settings.Runtime!.Overrides).toEqual({ ...noRuntimeOverrides, Network: { BindHost: null, HttpPort: null } });
});
