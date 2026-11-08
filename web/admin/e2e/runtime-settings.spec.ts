import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Locator, Page, Request, Route } from '@playwright/test';
import type { ServerSettings, SettingsUpdateInput } from '../src/api';
import type { RuntimeOverrides, RuntimeSettings } from '../src/runtimeSettings';
import { validRuntimeSettings } from '../src/runtimeSettings';
import { parseRuntimeDraft, runtimeDraft } from '../src/runtimeSettingsDraft';
import { DashboardAPI } from './dashboard-fixture';
import type { DashboardPreferences } from './dashboard-fixture';

// All administrator API requests are synthetic and must match an explicit
// fixture response. Browser assets are the only real server dependency.
const timestamp = '2026-09-20T00:00:00Z';
const csrf = 'synthetic-runtime-settings-csrf';
const administrator = { Id: 'runtime-admin', Name: 'Runtime administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const settingsPath = '/admin/v1/settings';
const noRuntimeOverrides: RuntimeOverrides = { Network: null, Hardware: null, Threads: null, H264: null, HEVC: null, SoftwareToneMapping: null, VulkanToneMapping: null, Transcoding: null, TranscodingTempPath: null, SoftwareToneMapOptions: null, CodecConfigurations: null, EnableThrottling: null, ExtractionThreadCount: null, CodecProfileLimits: null, SubtitleOptions: null };

function runtime(): RuntimeSettings {
  const defaults = {
    Network: { BindHost: '127.0.0.1', HttpPort: 8096 }, Hardware: { Decode: 'software', DeviceId: '', Encode: 'software' }, Threads: 2,
    H264: { CRF: 23, Preset: 'veryfast' as const, RateControl: 'bitrate' as const, ThreadCount: null, X264Options: '' }, HEVC: { CRF: 28, Preset: 'fast' as const, RateControl: 'bitrate' as const, ThreadCount: null, X264Options: '' },
    SoftwareToneMapping: true, VulkanToneMapping: false,
    Transcoding: { EnableHevcEncoding: true, EnableHardwareEncoding: true, DownMixAudioBoost: 1, EnableSubtitleExtraction: true, EnableOnTheFlyAttachmentExtraction: true },
    TranscodingTempPath: '/cache/goby-runtime',
    SoftwareToneMapOptions: { ToneMapSizeLimit: 'None' as const, SoftwareToneMapAlgorithm: 'Hable' as const },
    CodecConfigurations: [{ CodecId: 'V-E-libx264' as const, IsEnabled: true, Priority: 0 }, { CodecId: 'V-E-libx265' as const, IsEnabled: true, Priority: 0 }],
    EnableThrottling: false,
    ExtractionThreadCount: 1,
    CodecProfileLimits: { H264: 'high' as const, HEVC: 'main10' as const },
    SubtitleOptions: { AllowAssToText: true },
  };
  const { Network: _network, TranscodingTempPath: _path, ...effective } = defaults;
  return {
    Defaults: defaults, Overrides: { ...noRuntimeOverrides }, Effective: effective,
    Sources: { Network: { BindHost: 'deployment', HttpPort: 'deployment' }, Hardware: 'deployment', Threads: 'deployment', H264: 'deployment', HEVC: 'deployment', SoftwareToneMapping: 'deployment', VulkanToneMapping: 'deployment', Transcoding: 'deployment', TranscodingTempPath: 'deployment', SoftwareToneMapOptions: 'deployment', CodecConfigurations: 'deployment', EnableThrottling: 'deployment', ExtractionThreadCount: 'deployment', CodecProfileLimits: 'deployment', SubtitleOptions: 'deployment' },
    Network: { Desired: { ...defaults.Network }, Active: { Configured: { ...defaults.Network }, BoundHost: '127.0.0.1', HttpPort: 8096, Revision: '1' }, RestartRequired: false, ReconnectURL: '' },
    Hardware: { Available: true, Code: '', Devices: [{ DeviceId: 'amd-device-one', Label: 'AMD fixture GPU', Available: true, Code: '' }, { DeviceId: 'amd-offline', Label: 'Offline fixture GPU', Available: false, Code: 'device_unavailable' }] },
    TranscodingTempPath: { Desired: defaults.TranscodingTempPath, Active: defaults.TranscodingTempPath, RestartRequired: false },
    Effects: { Network: 'restart', Hardware: 'next_admission', Threads: 'next_admission', H264: 'next_admission', HEVC: 'next_admission', SoftwareToneMapping: 'next_admission', VulkanToneMapping: 'next_admission', Transcoding: 'next_admission', TranscodingTempPath: 'restart', SoftwareToneMapOptions: 'next_admission', CodecConfigurations: 'next_admission', EnableThrottling: 'next_hls_playback', ExtractionThreadCount: 'next_image_extraction', CodecProfileLimits: 'next_admission', SubtitleOptions: 'next_admission' },
    Applicability: { H264: 'software_h264_output', HEVC: 'software_hevc_output', SoftwareToneMapping: 'software_filter', VulkanToneMapping: 'vulkan_filter', Transcoding: 'encoding_and_extraction', TranscodingTempPath: 'cache_root_after_restart', SoftwareToneMapOptions: 'software_tone_mapping', CodecConfigurations: 'software_video_encoding', EnableThrottling: 'file_hls_video_encoding', ExtractionThreadCount: 'image_extraction', CodecProfileLimits: 'new_video_encoding', SubtitleOptions: 'ass_text_negotiation' },
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
  shell = new DashboardAPI();
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
    // Go map DTOs encode string keys in lexical order.
    for (const codec of ['H264', 'HEVC'] as const) {
      const value = current.Overrides[codec];
      if (value) current.Overrides[codec] = { CRF: value.CRF, Preset: value.Preset, RateControl: value.RateControl, ThreadCount: value.ThreadCount, X264Options: value.X264Options };
    }
    if (current.Overrides.Hardware) {
      const value = current.Overrides.Hardware;
      current.Overrides.Hardware = { Decode: value.Decode, DeviceId: value.DeviceId, Encode: value.Encode };
    }
    for (const field of ['Hardware', 'Threads', 'H264', 'HEVC', 'SoftwareToneMapping', 'VulkanToneMapping', 'Transcoding', 'SoftwareToneMapOptions', 'CodecConfigurations', 'EnableThrottling', 'ExtractionThreadCount', 'CodecProfileLimits', 'SubtitleOptions'] as const) {
      Object.assign(current.Effective, { [field]: current.Overrides[field] ?? current.Defaults[field] });
      current.Sources[field] = current.Overrides[field] === null ? 'deployment' : 'database';
    }
    current.TranscodingTempPath.Desired = current.Overrides.TranscodingTempPath || current.Defaults.TranscodingTempPath;
    current.Sources.TranscodingTempPath = current.Overrides.TranscodingTempPath === null ? 'deployment' : 'database';
    current.TranscodingTempPath.RestartRequired = current.TranscodingTempPath.Desired !== current.TranscodingTempPath.Active;
  }
  async install(context: BrowserContext): Promise<void> {
    await this.shell.install(context);
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
        return route.fallback();
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

const test = base.extend<{ api: RuntimeAPI; preferences: DashboardPreferences }>({
  preferences: [{ language: 'en', theme: 'light' }, { option: true }],
  api: async ({ context, page, preferences }, use) => {
  await context.addInitScript((initial) => {
    localStorage.setItem('goby.admin.language', initial.language);
    localStorage.setItem('goby.admin.theme', initial.theme);
  }, preferences);
  const api = new RuntimeAPI(); const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message)); await api.install(context); await use(api);
  expect(api.unexpected).toEqual([]); expect(api.shell.unexpected).toEqual([]); expect(api.shell.writes()).toEqual([]); expect(errors).toEqual([]); expect(api.writes().every((request) => request.csrf === csrf)).toBe(true);
} });
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });
async function openSettings(page: Page, section: 'general' | 'hardware' | 'transcode' = 'hardware'): Promise<void> { await page.goto('/admin/settings/' + section); await expect(page.getByRole('region', { name: section === 'general' ? 'HTTP listener' : section === 'hardware' ? 'Hardware acceleration' : 'Output planning limits', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Reload settings', exact: true })).toBeEnabled(); }
const group = (page: Page, name: string) => page.getByRole('region', { name, exact: true });
async function override(region: Locator, name: string): Promise<void> {
  if (['http port', 'bind address', 'maximum width'].includes(name.toLowerCase())) await region.getByRole('checkbox', { name: `Use deployment default for ${name.toLowerCase()}`, exact: true }).uncheck();
  else await region.getByRole('button', { name: `Customize ${name.toLowerCase()}`, exact: true }).click();
}
async function select(_page: Page, region: Locator, label: string, choice: string): Promise<void> { await region.getByRole('combobox', { name: label, exact: true }).selectOption({ label: choice }); }
async function save(page: Page, status = 200): Promise<void> {
  const [response] = await Promise.all([page.waitForResponse((value) => value.request().method() === 'PUT' && new URL(value.url()).pathname === settingsPath), page.getByRole('button', { name: 'Save changes', exact: true }).click()]);
  expect(response.status()).toBe(status);
  if (status === 200) await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  else await expect(page.getByRole('button', { name: 'Reload latest settings', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reload settings', exact: true })).toBeEnabled();
}

test('network changes retain the active listener and send a complete nullable group with exact CAS', async ({ page, api }) => {
  await openSettings(page, 'general'); const listener = group(page, 'HTTP listener');
  await override(listener, 'HTTP port'); await listener.getByRole('textbox', { name: 'HTTP port', exact: true }).fill('9097');
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Revision).toBe('9007199254740993');
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ Network: { BindHost: null, HttpPort: 9097 } });
  await expect(listener).toContainText('127.0.0.1 · Port 8096'); await expect(listener).toContainText('127.0.0.1 · Port 9097');
  await expect(listener.getByRole('status')).toContainText('requires a service restart');
  expect(new URL(page.url()).pathname).toBe('/admin/settings/general');
  expect(api.writes().every((request) => request.path === settingsPath && request.method === 'PUT')).toBe(true);
});

test('codec choices, explicit false, and null reset preserve separate override meanings', async ({ page, api }) => {
  await openSettings(page); const codec = group(page, 'H.264 CPU encoding'); const tone = group(page, 'Software tone mapping');
  await override(codec, 'H.264 CPU encoding'); await select(page, codec, 'H.264 CPU preset', 'slow'); await select(page, codec, 'H.264 rate control', 'Capped CRF');
  await codec.getByRole('textbox', { name: 'H.264 CRF', exact: true }).fill('20');
  await override(tone, 'software tone mapping'); await tone.getByRole('switch', { name: 'Enable software tone mapping', exact: true }).uncheck();
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ H264: { Preset: 'slow', RateControl: 'capped_crf', CRF: 20, ThreadCount: null, X264Options: '' }, SoftwareToneMapping: false });
  await codec.getByRole('button', { name: 'Use deployment default for h.264 cpu encoding', exact: true }).click();
  await tone.getByRole('button', { name: 'Use deployment default for software tone mapping', exact: true }).click();
  await save(page);
  expect((api.writes()[1].body as SettingsUpdateInput).Runtime).toEqual({ H264: null, SoftwareToneMapping: null });
  await expect(tone.getByRole('switch', { name: 'Enable software tone mapping', exact: true })).toBeChecked();
});

test('only reported available AMD devices can be selected and no device path is submitted', async ({ page, api }) => {
  await openSettings(page); const hardware = group(page, 'Hardware acceleration');
  await override(hardware, 'hardware acceleration'); await select(page, hardware, 'Video decoding', 'AMD VA-API'); await select(page, hardware, 'Video encoding', 'AMD VA-API');
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  const devices = hardware.getByRole('combobox', { name: 'AMD device', exact: true });
  await expect(devices.locator('option').filter({ hasText: 'Offline fixture GPU (unavailable)' })).toBeDisabled();
  await devices.selectOption({ label: 'AMD fixture GPU' }); await save(page);
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
  await openSettings(page, 'general'); const listener = group(page, 'HTTP listener');
  await override(listener, 'HTTP port'); await listener.getByRole('textbox', { name: 'HTTP port', exact: true }).fill('0'); await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await listener.getByRole('textbox', { name: 'HTTP port', exact: true }).fill('65536'); await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await listener.getByRole('textbox', { name: 'HTTP port', exact: true }).fill('8097'); await override(listener, 'bind address'); await listener.getByRole('textbox', { name: 'Bind address', exact: true }).fill('example.com'); await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await listener.getByRole('textbox', { name: 'Bind address', exact: true }).fill('127.0.0.1');
  await page.getByRole('link', { name: 'Transcoding', exact: true }).click();
  const threads = group(page, 'Threads per job'); await override(threads, 'threads per job'); await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('65'); await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('4');
  const codec = group(page, 'HEVC CPU encoding'); await override(codec, 'HEVC CPU encoding'); await select(page, codec, 'HEVC rate control', 'Capped CRF'); await codec.getByRole('textbox', { name: 'HEVC CRF', exact: true }).fill('0'); await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  expect(api.writes()).toEqual([]);
});

test('runtime revision conflicts keep the draft and require an explicit reload before another save', async ({ page, api }) => {
  await openSettings(page); const threads = group(page, 'Threads per job'); await override(threads, 'threads per job'); await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('6');
  api.settings.Revision = '9007199254740994'; await save(page, 409);
  await expect(threads.getByRole('textbox', { name: 'Threads per job', exact: true })).toHaveValue('6'); await expect(threads.getByRole('textbox', { name: 'Threads per job', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Reload latest settings', exact: true }).click();
  await page.getByRole('dialog', { name: 'Discard unsaved settings changes?', exact: true }).getByRole('button', { name: 'Discard draft and reload', exact: true }).click();
  await expect(threads.getByRole('button', { name: 'Use deployment default for threads per job', exact: true })).toHaveAttribute('aria-pressed', 'true');
  expect(api.writes()).toHaveLength(1);
});

test('an invalid mutation response locks runtime controls until the saved state is reloaded', async ({ page, api }) => {
  api.handlers.set(`PUT ${settingsPath}`, async (route, request) => { api.apply(request.postDataJSON() as SettingsUpdateInput); await json(route, { ...api.settings, Runtime: { ...api.settings.Runtime, Effective: { Network: { BindHost: '', HttpPort: 8096 } } } }); });
  await openSettings(page); const threads = group(page, 'Threads per job'); await override(threads, 'threads per job'); await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('8');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByText('The result could not be confirmed.', { exact: false })).toBeVisible();
  await expect(threads.getByRole('textbox', { name: 'Threads per job', exact: true })).toHaveValue('8'); await expect(threads.getByRole('textbox', { name: 'Threads per job', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
});

test('an unrelated output limit edit omits Runtime and retains every runtime override', async ({ page, api }) => {
  api.settings.Runtime!.Overrides.Network = { BindHost: null, HttpPort: null };
  await openSettings(page, 'transcode'); const width = group(page, 'Output planning limits'); await override(width, 'maximum width'); await width.getByRole('textbox', { name: 'Maximum width', exact: true }).fill('1280'); await save(page);
  expect(api.writes()[0].body).not.toHaveProperty('Runtime');
  expect(api.settings.Runtime!.Overrides).toEqual({ ...noRuntimeOverrides, Network: { BindHost: null, HttpPort: null } });
});

test('native software encoder presets, quality bounds, and independent thread settings are submitted exactly', async ({ page, api }) => {
  await openSettings(page);
  const h264 = group(page, 'H.264 CPU encoding');
  const hevc = group(page, 'HEVC CPU encoding');
  await override(h264, 'H.264 CPU encoding');
  await expect(h264.getByRole('combobox', { name: 'H.264 CPU preset', exact: true }).locator('option')).toHaveText(['superfast', 'veryfast', 'faster', 'fast', 'medium', 'slow', 'slower', 'veryslow']);
  await expect(hevc.getByRole('combobox', { name: 'HEVC CPU preset', exact: true }).locator('option')).toHaveText(['ultrafast', 'superfast', 'veryfast', 'faster', 'fast', 'medium', 'slow', 'slower', 'veryslow', 'placebo']);
  await select(page, h264, 'H.264 CPU preset', 'superfast');
  await select(page, h264, 'H.264 rate control', 'Capped CRF');
  await h264.getByRole('textbox', { name: 'H.264 CRF', exact: true }).fill('1');
  await select(page, h264, 'H.264 encoder threads', 'Automatic');
  await override(hevc, 'HEVC CPU encoding');
  await select(page, hevc, 'HEVC CPU preset', 'placebo');
  await select(page, hevc, 'HEVC rate control', 'Capped CRF');
  await hevc.getByRole('textbox', { name: 'HEVC CRF', exact: true }).fill('51');
  await select(page, hevc, 'HEVC encoder threads', 'Limit threads');
  await hevc.getByRole('textbox', { name: 'HEVC thread limit', exact: true }).fill('17');
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await hevc.getByRole('textbox', { name: 'HEVC thread limit', exact: true }).fill('16');
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({
    H264: { Preset: 'superfast', RateControl: 'capped_crf', CRF: 1, ThreadCount: 0, X264Options: '' },
    HEVC: { Preset: 'placebo', RateControl: 'capped_crf', CRF: 51, ThreadCount: 16, X264Options: '' },
  });
});

test('supported x264 analysis options can be saved while file and scheduling options remain invalid', async ({ page, api }) => {
  await openSettings(page);
  const h264 = group(page, 'H.264 CPU encoding');
  await override(h264, 'H.264 CPU encoding');
  await h264.locator('summary').filter({ hasText: 'Advanced H.264 options' }).click();
  const options = h264.getByRole('textbox', { name: 'H.264 encoder options', exact: true });
  const nativeOptions = 'subme=0:me_range=4:rc_lookahead=10:partitions=none';
  await options.fill(nativeOptions);
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime?.H264?.X264Options).toBe(nativeOptions);
  for (const invalid of ['stats=/tmp/encoder.log', 'keyint=100', 'threads=2', 'subme=10', 'aq-mode=1:aq_mode=2']) {
    await options.fill(invalid);
    await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
    await expect(h264).toContainText('Use supported H.264 encoder options only.');
  }
  await options.fill(nativeOptions);
  await expect(options).toBeVisible();
  await expect(options).toBeFocused();
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
});

test('transcoding booleans, downmix boost, and software encoder priorities preserve complete group updates', async ({ page, api }) => {
  await openSettings(page);
  const policy = group(page, 'Playback transcoding');
  await override(policy, 'Playback transcoding');
  for (const name of ['Allow HEVC encoding', 'Allow hardware encoding', 'Extract embedded subtitles during playback', 'Extract subtitle font attachments']) await policy.getByRole('switch', { name, exact: true }).uncheck();
  await policy.getByRole('textbox', { name: 'Audio boost when downmixing', exact: true }).fill('2');
  await expect(policy.getByRole('switch', { name: 'Allow hardware encoding', exact: true })).toHaveAccessibleDescription('Use the selected hardware encoder when available. Hardware decoding and GPU filters keep their own settings.');
  const encoders = group(page, 'Software encoder selection');
  await override(encoders, 'Software encoder selection');
  await encoders.getByRole('switch', { name: 'H.264 software encoder', exact: true }).uncheck();
  await expect(encoders.getByRole('textbox', { name: 'H.264 encoder priority', exact: true })).toBeDisabled();
  await encoders.getByRole('textbox', { name: 'HEVC encoder priority', exact: true }).fill('100');
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({
    Transcoding: { EnableHevcEncoding: false, EnableHardwareEncoding: false, DownMixAudioBoost: 2, EnableSubtitleExtraction: false, EnableOnTheFlyAttachmentExtraction: false },
    CodecConfigurations: [{ CodecId: 'V-E-libx264', IsEnabled: false, Priority: 0 }, { CodecId: 'V-E-libx265', IsEnabled: true, Priority: 100 }],
  });
  await policy.getByRole('button', { name: 'Use deployment default for playback transcoding', exact: true }).click();
  await save(page);
  expect((api.writes()[1].body as SettingsUpdateInput).Runtime).toEqual({ Transcoding: null });
});

test('profile limits and ASS text delivery save independent complete groups and reset without changing encoder policy', async ({ page, api }) => {
  await openSettings(page);
  const profiles = group(page, 'Video profile limits');
  const subtitles = group(page, 'ASS to plain text');
  await expect(profiles.getByRole('combobox', { name: 'H.264 profile limit', exact: true })).toBeDisabled();
  await expect(subtitles.getByRole('switch', { name: 'Allow ASS to plain text conversion', exact: true })).toBeChecked();
  await expect(subtitles.getByRole('switch', { name: 'Allow ASS to plain text conversion', exact: true })).toBeDisabled();
  await override(profiles, 'Video profile limits');
  await expect(profiles.getByRole('combobox', { name: 'H.264 profile limit', exact: true }).locator('option')).toHaveText(['Baseline', 'Main', 'High']);
  await expect(profiles.getByRole('combobox', { name: 'HEVC profile limit', exact: true }).locator('option')).toHaveText(['Main', 'Main 10']);
  await select(page, profiles, 'H.264 profile limit', 'Main');
  await select(page, profiles, 'HEVC profile limit', 'Main');
  await override(subtitles, 'ASS to plain text');
  const allowAss = subtitles.getByRole('switch', { name: 'Allow ASS to plain text conversion', exact: true });
  await allowAss.uncheck();
  await expect(allowAss).toHaveAccessibleDescription('During playback negotiation, allow ASS subtitles in plain text formats. Plain text removes ASS styling; ASS delivery retains styling.');
  await expect(page.getByText('2 unsaved changes', { exact: true })).toBeVisible();
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ CodecProfileLimits: { H264: 'main', HEVC: 'main' }, SubtitleOptions: { AllowAssToText: false } });
  await expect(profiles).toContainText('Database override');
  await expect(subtitles).toContainText('Database override');
  await profiles.getByRole('button', { name: 'Use deployment default for video profile limits', exact: true }).click();
  await save(page);
  expect((api.writes()[1].body as SettingsUpdateInput).Runtime).toEqual({ CodecProfileLimits: null });
  expect(api.settings.Runtime!.Overrides.SubtitleOptions).toEqual({ AllowAssToText: false });
  expect(api.settings.Runtime!.Overrides.CodecConfigurations).toBeNull();
  expect(api.settings.Runtime!.Overrides.Transcoding).toBeNull();
  await subtitles.getByRole('button', { name: 'Use deployment default for ass to plain text', exact: true }).click();
  await save(page);
  expect((api.writes()[2].body as SettingsUpdateInput).Runtime).toEqual({ SubtitleOptions: null });
  await expect(allowAss).toBeChecked();
});

test('HLS throttling and image extraction preferences keep independent scope, zero, and null semantics', async ({ page, api }) => {
  await openSettings(page);
  const throttling = group(page, 'Automatic HLS throttling');
  const extraction = group(page, 'Image extraction threads');
  await expect(throttling.getByRole('switch', { name: 'Enable automatic HLS throttling', exact: true })).not.toBeChecked();
  await expect(extraction.getByRole('textbox', { name: 'Image extraction thread preference', exact: true })).toHaveValue('1');
  await override(throttling, 'Automatic HLS throttling');
  await throttling.getByRole('switch', { name: 'Enable automatic HLS throttling', exact: true }).check();
  await expect(throttling.getByRole('switch', { name: 'Enable automatic HLS throttling', exact: true })).toHaveAccessibleDescription('For file HLS video transcoding with reported playback progress, wait when enough output is buffered and resume as playback advances. Pending segment requests can continue after a short bounded wait. Applies to new HLS playback sessions.');
  await override(extraction, 'Image extraction threads');
  const preference = extraction.getByRole('textbox', { name: 'Image extraction thread preference', exact: true });
  await preference.fill('2');
  await expect(preference).toHaveAccessibleDescription('Thread preference for the next preview thumbnail extraction. Use 0–16; 0 or a value at least the available logical processor count lets FFmpeg choose automatically.');
  await expect(page.getByText('2 unsaved changes', { exact: true })).toBeVisible();
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ EnableThrottling: true, ExtractionThreadCount: 2 });
  expect(api.settings.Runtime!.Effects.EnableThrottling).toBe('next_hls_playback');
  expect(api.settings.Runtime!.Effects.ExtractionThreadCount).toBe('next_image_extraction');
  expect(api.settings.Runtime!.Overrides.Threads).toBeNull();
  expect(api.settings.Runtime!.Overrides.Transcoding).toBeNull();
  await throttling.getByRole('button', { name: 'Use deployment default for automatic hls throttling', exact: true }).click();
  await save(page);
  expect((api.writes()[1].body as SettingsUpdateInput).Runtime).toEqual({ EnableThrottling: null });
  expect(api.settings.Runtime!.Overrides.ExtractionThreadCount).toBe(2);
  await preference.fill('0');
  await save(page);
  expect((api.writes()[2].body as SettingsUpdateInput).Runtime).toEqual({ ExtractionThreadCount: 0 });
  await expect(extraction).toContainText('Automatic');
  await preference.fill('17');
  await expect(preference).toHaveAttribute('aria-invalid', 'true');
  await expect(extraction).toContainText('Enter a whole number from 0 to 16.');
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(3);
  await extraction.getByRole('button', { name: 'Use deployment default for image extraction threads', exact: true }).click();
  await save(page);
  expect((api.writes()[3].body as SettingsUpdateInput).Runtime).toEqual({ ExtractionThreadCount: null });
});

test('stored selected groups remain clean and unrelated edits preserve their complete values', () => {
  const current = runtime();
  const profiles = { HEVC: 'main' as const, H264: 'main' as const };
  current.Overrides.CodecProfileLimits = profiles;
  current.Overrides.SubtitleOptions = { AllowAssToText: false };
  current.Overrides.EnableThrottling = false;
  current.Overrides.ExtractionThreadCount = 0;
  current.Effective.CodecProfileLimits = { ...profiles };
  current.Effective.SubtitleOptions = { AllowAssToText: false };
  current.Effective.EnableThrottling = false;
  current.Effective.ExtractionThreadCount = 0;
  current.Sources.CodecProfileLimits = 'database';
  current.Sources.SubtitleOptions = 'database';
  current.Sources.EnableThrottling = 'database';
  current.Sources.ExtractionThreadCount = 'database';
  const clean = runtimeDraft(current);
  expect(parseRuntimeDraft(clean)).toEqual({ value: {}, errors: {} });
  const threads = structuredClone(clean);
  threads.Threads = { override: true, value: '3' };
  expect(parseRuntimeDraft(threads)).toEqual({ value: { Threads: 3 }, errors: {} });
  const lower = structuredClone(clean);
  lower.CodecProfileLimits.value.H264 = 'baseline';
  expect(parseRuntimeDraft(lower)).toEqual({ value: { CodecProfileLimits: { H264: 'baseline', HEVC: 'main' } }, errors: {} });
  const reset = structuredClone(clean);
  reset.SubtitleOptions.override = false;
  expect(parseRuntimeDraft(reset)).toEqual({ value: { SubtitleOptions: null }, errors: {} });
  expect(current.Overrides.CodecProfileLimits).toEqual(profiles);
  expect(current.Overrides.SubtitleOptions).toEqual({ AllowAssToText: false });
  expect(current.Overrides.EnableThrottling).toBe(false);
  expect(current.Overrides.ExtractionThreadCount).toBe(0);
});

test('selected group response contracts reject unsupported profiles and incomplete subtitle policy', () => {
  const current = runtime();
  expect(validRuntimeSettings(current)).toBe(true);
  for (const profiles of [{ H264: 'high10', HEVC: 'main10' }, { H264: 'high' }, { H264: 2, HEVC: 'main' }, { H264: 'high', HEVC: 'main10', Extra: true }]) {
    const response = structuredClone(current);
    Object.assign(response.Effective, { CodecProfileLimits: profiles });
    expect(validRuntimeSettings(response)).toBe(false);
  }
  for (const subtitles of [{}, { AllowAssToText: null }, { AllowAssToText: 'false' }, { AllowAssToText: true, ExtractFonts: true }]) {
    const response = structuredClone(current);
    Object.assign(response.Effective, { SubtitleOptions: subtitles });
    expect(validRuntimeSettings(response)).toBe(false);
  }
  for (const threads of [-1, 17, 1.5, '1', null]) {
    const response = structuredClone(current);
    Object.assign(response.Effective, { ExtractionThreadCount: threads });
    expect(validRuntimeSettings(response)).toBe(false);
  }
  const wrongEffect = structuredClone(current);
  Object.assign(wrongEffect.Effects, { EnableThrottling: 'next_admission' });
  expect(validRuntimeSettings(wrongEffect)).toBe(false);
  const wrongScope = structuredClone(current);
  Object.assign(wrongScope.Applicability, { ExtractionThreadCount: 'subtitle_extraction' });
  expect(validRuntimeSettings(wrongScope)).toBe(false);
});

test('software tone mapping exposes implemented algorithms and preserves parameters when its gate is disabled', async ({ page, api }) => {
  await openSettings(page);
  const parameters = group(page, 'Software tone mapping parameters');
  await override(parameters, 'Software tone mapping parameters');
  await expect(parameters.getByRole('combobox', { name: 'Software tone mapping algorithm', exact: true }).locator('option')).toHaveText(['Hable', 'Reinhard', 'Mobius']);
  await select(page, parameters, 'Software tone mapping algorithm', 'Mobius');
  await select(page, parameters, 'Software tone mapping height limit', '720 px');
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ SoftwareToneMapOptions: { ToneMapSizeLimit: 'Limit720', SoftwareToneMapAlgorithm: 'Mobius' } });
  const tone = group(page, 'Software tone mapping');
  await override(tone, 'Software tone mapping');
  await tone.getByRole('switch', { name: 'Enable software tone mapping', exact: true }).uncheck();
  await expect(parameters.getByRole('combobox', { name: 'Software tone mapping algorithm', exact: true })).toBeDisabled();
  await expect(parameters.getByRole('combobox', { name: 'Software tone mapping algorithm', exact: true })).toHaveValue('Mobius');
  await save(page);
  expect((api.writes()[1].body as SettingsUpdateInput).Runtime).toEqual({ SoftwareToneMapping: false });
});

test('temporary directory changes retain the active cache and distinguish empty from a cleared override', async ({ page, api }) => {
  await openSettings(page);
  const directory = group(page, 'Transcoding temporary directory');
  await override(directory, 'Transcoding temporary directory');
  const input = directory.getByRole('textbox', { name: 'Transcoding temporary directory', exact: true });
  await input.fill('/cache/goby-next');
  await expect(page.getByText('1 unsaved change', { exact: true })).toBeVisible();
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ TranscodingTempPath: '/cache/goby-next' });
  await expect(directory).toContainText('/cache/goby-next');
  expect(api.settings.Runtime!.TranscodingTempPath).toEqual({ Desired: '/cache/goby-next', Active: '/cache/goby-runtime', RestartRequired: true });
  await expect(directory.getByRole('status')).toContainText('after a service restart');
  await input.fill('');
  await save(page);
  expect((api.writes()[1].body as SettingsUpdateInput).Runtime).toEqual({ TranscodingTempPath: '' });
  expect(api.settings.Runtime!.TranscodingTempPath.RestartRequired).toBe(false);
  await directory.getByRole('button', { name: 'Use deployment default for transcoding temporary directory', exact: true }).click();
  await save(page);
  expect((api.writes()[2].body as SettingsUpdateInput).Runtime).toEqual({ TranscodingTempPath: null });
  expect(api.writes().every((request) => request.method === 'PUT' && request.path === settingsPath)).toBe(true);
});

test('a stored small x264 decimal leaves the initial form clean and stays out of unrelated updates', async ({ page, api }) => {
  const runtime = api.settings.Runtime!;
  const quality = { ...runtime.Defaults.H264, X264Options: 'aq-strength=0.0000001' };
  runtime.Overrides.H264 = quality;
  runtime.Effective.H264 = { ...quality };
  runtime.Sources.H264 = 'database';
  await openSettings(page, 'transcode');
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  const width = group(page, 'Output planning limits');
  await override(width, 'maximum width');
  await width.getByRole('textbox', { name: 'Maximum width', exact: true }).fill('1280');
  await save(page);
  expect(api.writes()[0].body).not.toHaveProperty('Runtime');
  expect(api.settings.Runtime!.Overrides.H264).toEqual(quality);
});

test('map-encoded CPU groups stay out of unrelated runtime and output updates', async ({ page, api }) => {
  const runtime = api.settings.Runtime!;
  const h264 = { CRF: 22, Preset: 'slow' as const, RateControl: 'capped_crf' as const, ThreadCount: 0, X264Options: '' };
  const hevc = { CRF: 28, Preset: 'fast' as const, RateControl: 'bitrate' as const, ThreadCount: 4, X264Options: '' };
  runtime.Overrides.H264 = h264; runtime.Overrides.HEVC = hevc;
  runtime.Effective.H264 = { ...h264 }; runtime.Effective.HEVC = { ...hevc };
  runtime.Sources.H264 = 'database'; runtime.Sources.HEVC = 'database';
  await openSettings(page);
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  const threads = group(page, 'Threads per job');
  await override(threads, 'Threads per job');
  await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('3');
  await save(page);
  expect((api.writes()[0].body as SettingsUpdateInput).Runtime).toEqual({ Threads: 3 });
  const width = group(page, 'Output planning limits');
  await override(width, 'maximum width');
  await width.getByRole('textbox', { name: 'Maximum width', exact: true }).fill('1280');
  await save(page);
  expect(api.writes()[1].body).not.toHaveProperty('Runtime');
  expect(runtime.Overrides.H264).toEqual(h264); expect(runtime.Overrides.HEVC).toEqual(hevc);
});

test('array-shaped codec strings in a mutation response require reload instead of becoming editable settings', async ({ page, api }) => {
  api.handlers.set(`PUT ${settingsPath}`, async (route, request) => {
    api.apply(request.postDataJSON() as SettingsUpdateInput);
    const runtime = api.settings.Runtime!;
    await json(route, { ...api.settings, Runtime: { ...runtime, Effective: { ...runtime.Effective, H264: { ...runtime.Effective.H264, Preset: ['slow'] } } } });
  });
  await openSettings(page);
  const threads = group(page, 'Threads per job');
  await override(threads, 'Threads per job');
  await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill('3');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByText('The result could not be confirmed.', { exact: false })).toBeVisible();
  await expect(threads.getByRole('textbox', { name: 'Threads per job', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
});

test.describe('mobile transcoding controls', () => {
  test.use({ viewport: { width: 375, height: 844 }, preferences: { language: 'zh', theme: 'dark' } });
  test('translated controls retain complete labels and touch targets without horizontal scrolling', async ({ page, api }, testInfo) => {
    await page.goto('/admin/settings/transcode');
    await expect(group(page, '\u64ad\u653e\u8f6c\u7801')).toBeVisible();
    await expect(page.getByRole('switch', { name: '\u5141\u8bb8 HEVC \u7f16\u7801', exact: true })).toBeVisible();
    await expect(page.getByRole('switch', { name: '\u5141\u8bb8 ASS \u8f6c\u7eaf\u6587\u672c', exact: true })).toBeVisible();
    await expect(page.getByRole('switch', { name: '\u542f\u7528 HLS \u81ea\u52a8\u8282\u6d41', exact: true })).toBeVisible();
    await expect(page.getByRole('textbox', { name: '\u56fe\u50cf\u63d0\u53d6\u7ebf\u7a0b\u504f\u597d', exact: true })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'H.264 Profile \u4e0a\u9650', exact: true })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'HEVC Profile \u4e0a\u9650', exact: true }).locator('option')).toHaveText(['Main', 'Main 10']);
    const dimensions = await page.evaluate(() => ({
      width: window.innerWidth, scrollWidth: document.documentElement.scrollWidth,
      targets: [...document.querySelectorAll<HTMLElement>('.settings-transcoding-toggle .goby-control-label')].map((label) => label.getBoundingClientRect().height),
      fieldTargets: [...document.querySelectorAll<HTMLElement>('.settings-touch-field .goby-input, .settings-transcoding-override .settings-override button, .settings-group:has(.settings-touch-field) .settings-group-info > summary')].filter((control) => control.getClientRects().length > 0).map((control) => control.getBoundingClientRect().height),
    }));
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.width);
    expect(dimensions.targets).toHaveLength(8);
    expect(dimensions.targets.every((height) => height >= 44)).toBe(true);
    expect(dimensions.fieldTargets.length).toBeGreaterThan(10);
    expect(dimensions.fieldTargets.every((height) => height >= 44)).toBe(true);
    expect(api.writes()).toEqual([]);
    await testInfo.attach('transcoding-mobile-zh-dark', { body: await page.screenshot(), contentType: 'image/png' });
  });
});
