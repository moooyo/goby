import type { Route } from '@playwright/test';
import type { ProviderConfiguration, ProviderConfigurationInput, ProviderId } from '../src/providersApi';
import type { DashboardAPI } from './dashboard-fixture';

const checkedAt = '2026-11-06T08:00:00Z';
const deployed: Record<ProviderId, Record<string, boolean>> = {
  tmdb: { Token: true }, musicbrainz: { UserAgent: true }, opensubtitles: { APIKey: true, Username: true, Password: true, UserAgent: true },
};
async function json(route: Route, value: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}
export function providerConfigurationFixture(): ProviderConfiguration {
  return {
    Revision: '9007199254740993', DeploymentEnabled: true, Enabled: true,
    Items: [
      { Id: 'tmdb', Name: 'The Movie Database', Configured: true, Fields: [{ Id: 'Token', Label: 'Access token', Secret: true, HasValue: true, Source: 'deployment' }] },
      { Id: 'musicbrainz', Name: 'MusicBrainz', Configured: true, Fields: [{ Id: 'UserAgent', Label: 'Application identification', Secret: false, HasValue: true, Source: 'deployment' }] },
      { Id: 'opensubtitles', Name: 'OpenSubtitles', Configured: true, Fields: [
        { Id: 'APIKey', Label: 'API key', Secret: true, HasValue: true, Source: 'deployment' },
        { Id: 'Username', Label: 'Username', Secret: false, HasValue: true, Source: 'deployment' },
        { Id: 'Password', Label: 'Password', Secret: true, HasValue: true, Source: 'deployment' },
        { Id: 'UserAgent', Label: 'Application identification', Secret: false, HasValue: true, Source: 'deployment' },
      ] },
    ],
  };
}
export function installProviderConfigurationFixture(api: DashboardAPI) {
  const fixture = {
    saved: providerConfigurationFixture(),
    apply(providerId: ProviderId, input: ProviderConfigurationInput) {
      const provider = this.saved.Items.find((value) => value.Id === providerId)!;
      for (const field of provider.Fields) {
        const change = input.Fields[field.Id];
        if (!change) continue;
        field.Source = change.Mode === 'default' ? 'deployment' : 'override';
        field.HasValue = change.Mode === 'replace' ? Boolean(change.Value) : change.Mode === 'default' && deployed[providerId][field.Id];
      }
      provider.Configured = this.saved.Enabled && provider.Fields.every((field) => field.HasValue);
      this.saved.Revision = (BigInt(this.saved.Revision) + 1n).toString();
    },
  };
  api.handlers.set('GET /admin/v1/providers/configuration', async (route) => json(route, fixture.saved));
  api.handlers.set('GET /admin/v1/providers', async (route) => json(route, {
    Enabled: fixture.saved.Enabled,
    Items: fixture.saved.Items.map((provider) => ({
      Id: provider.Id, Name: provider.Name, Configured: provider.Configured,
      Capabilities: provider.Id === 'tmdb' ? ['metadata', 'images'] : provider.Id === 'musicbrainz' ? ['metadata'] : ['subtitles'],
      Attribution: provider.Id === 'tmdb' ? 'Movie and television metadata' : '', Website: '',
    })),
  }));
  for (const providerId of ['tmdb', 'musicbrainz', 'opensubtitles'] as const) {
    api.handlers.set(`PUT /admin/v1/providers/${providerId}/configuration`, async (route, request) => {
      const input = request.postDataJSON() as ProviderConfigurationInput;
      if (input.Revision !== fixture.saved.Revision) return json(route, { Error: { Code: 'provider_configuration_conflict', Message: 'Provider configuration changed. Reload before saving.' } }, 409);
      fixture.apply(providerId, input); return json(route, fixture.saved);
    });
    api.handlers.set(`POST /admin/v1/providers/${providerId}/test`, async (route, request) => {
      if (request.postDataJSON().Revision !== fixture.saved.Revision) return json(route, { Error: { Code: 'provider_configuration_conflict', Message: 'Provider configuration changed. Reload before testing.' } }, 409);
      return json(route, { Provider: providerId, Status: 'passed', CheckedAt: checkedAt });
    });
  }
  return fixture;
}
