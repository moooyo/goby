import { useEffect, useRef } from 'react';
import { Alert, Box, Checkbox, Chip, FormControlLabel, MenuItem, Stack, Switch, TextField, Typography } from './ui';
import { CheckCircleRounded } from './icons';
import { fieldError } from './formFields';
import { OverrideMode, SettingsGroup, SettingsSource } from './SettingsLayout';
import { h264ProfileLimits, hevcProfileLimits, presetsForCodec, softwareToneMapAlgorithms, softwareToneMapSizeLimits } from './runtimeSettings';
import type { RuntimeCodecConfiguration, RuntimeCodecProfileLimits, RuntimeHardware, RuntimeQuality, RuntimeSettings, RuntimeSoftwareToneMapOptions, RuntimeTranscodingPolicy } from './runtimeSettings';
import type { RuntimeDraft, RuntimeQualityDraft } from './runtimeSettingsDraft';
import { useTranslation } from './i18n';
import { HardwareCapabilitiesPanel } from './HardwareCapabilitiesPanel';
import './transcoding-settings.css';

const modeLabel = (value: string) => value === 'software' ? 'CPU' : value === 'vaapi' ? 'AMD VA-API' : (value || 'Unconfigured') + ' (deployment)';
const networkLabel = (host: string, port: number) => (host || 'All interfaces') + ' · ' + (port === 0 ? 'Automatically assigned port' : 'Port ' + port);
const hardwareLabel = (value: RuntimeHardware) => 'Decode: ' + modeLabel(value.Decode) + ' · Encode: ' + modeLabel(value.Encode);
const qualityLabel = (value: RuntimeQuality) => value.Preset + ' · ' + (value.RateControl === 'bitrate' ? 'Bitrate target' : 'Capped CRF ' + value.CRF);

interface Props { settings: RuntimeSettings; draft: RuntimeDraft; disabled: boolean; stale: boolean; errors: Record<string, string | undefined>; mutationError: unknown; section?: 'network' | 'hardware'; onChange: (value: RuntimeDraft) => void }

export function RuntimeSettingsFields({ settings, draft, disabled, stale, errors, mutationError, section = 'hardware', onChange }: Props) {
  const { t } = useTranslation();
  const issue = (field: string) => errors[field] ?? fieldError(mutationError, field);
  const x264Details = useRef<HTMLDetailsElement>(null);
  const x264Issue = issue('Runtime.H264.X264Options');
  useEffect(() => {
    // Clearing an error must not collapse an editor the administrator is using.
    if (x264Issue && x264Details.current) x264Details.current.open = true;
  }, [x264Issue, section]);
  const network = draft.Network;
  const hardware = draft.Hardware.override ? draft.Hardware.value : settings.Defaults.Hardware;
  const knownDevice = settings.Hardware.Devices.find((device) => device.DeviceId === hardware.DeviceId);
  const hardwareDisabled = disabled || !draft.Hardware.override;
  const hardwareMatchesSaved = hardware.Decode === settings.Effective.Hardware.Decode && hardware.Encode === settings.Effective.Hardware.Encode
    && hardware.DeviceId === settings.Effective.Hardware.DeviceId;
  const policy = draft.Transcoding;
  const policyValue = policy.override ? policy.value : { ...settings.Defaults.Transcoding, DownMixAudioBoost: String(settings.Defaults.Transcoding.DownMixAudioBoost) };
  const policyDisabled = disabled || !policy.override;
  const changePolicy = (patch: Partial<typeof policy.value>) => onChange({ ...draft, Transcoding: { ...policy, value: { ...policy.value, ...patch } } });
  const temporaryPath = draft.TranscodingTempPath;
  const toneOptions = draft.SoftwareToneMapOptions;
  const toneValue = toneOptions.override ? toneOptions.value : settings.Defaults.SoftwareToneMapOptions;
  const softwareToneMapping = draft.SoftwareToneMapping.override ? draft.SoftwareToneMapping.value : settings.Defaults.SoftwareToneMapping;
  const changeToneOptions = (patch: Partial<RuntimeSoftwareToneMapOptions>) => onChange({ ...draft, SoftwareToneMapOptions: { ...toneOptions, value: { ...toneOptions.value, ...patch } } });
  const codecs = draft.CodecConfigurations;
  const codecValues = codecs.override ? codecs.value : settings.Defaults.CodecConfigurations.map((value) => ({ ...value, Priority: String(value.Priority) }));
  const changeCodec = (id: RuntimeCodecConfiguration['CodecId'], patch: { IsEnabled?: boolean; Priority?: string }) => onChange({ ...draft, CodecConfigurations: { ...codecs, value: codecs.value.map((value) => value.CodecId === id ? { ...value, ...patch } : value) } });
  const profileLimits = draft.CodecProfileLimits;
  const profileLimitValue = profileLimits.override ? profileLimits.value : settings.Defaults.CodecProfileLimits;
  const changeProfileLimits = (patch: Partial<RuntimeCodecProfileLimits>) => onChange({ ...draft, CodecProfileLimits: { ...profileLimits, value: { ...profileLimits.value, ...patch } } });
  const subtitles = draft.SubtitleOptions;
  const subtitleValue = subtitles.override ? subtitles.value : settings.Defaults.SubtitleOptions;
  const throttling = draft.EnableThrottling;
  const throttlingValue = throttling.override ? throttling.value : settings.Defaults.EnableThrottling;
  const extractionThreads = draft.ExtractionThreadCount;
  const qualitySummary = (value: RuntimeQuality, threads: number) => <>{t(qualityLabel(value))} · {value.ThreadCount === null ? t('Job thread limit: {count}', { count: threads }) : value.ThreadCount === 0 ? t('Automatic encoder threads') : t('Encoder threads: {count}', { count: value.ThreadCount })}</>;
  const policySummary = (value: RuntimeTranscodingPolicy) => <>{t('HEVC')}: {t(value.EnableHevcEncoding ? 'Enabled' : 'Disabled')} · {t('Hardware encoding')}: {t(value.EnableHardwareEncoding ? 'Enabled' : 'Disabled')} · {t('Audio boost')}: {value.DownMixAudioBoost} · {t('Subtitle extraction')}: {t(value.EnableSubtitleExtraction ? 'Enabled' : 'Disabled')} · {t('Font attachments')}: {t(value.EnableOnTheFlyAttachmentExtraction ? 'Enabled' : 'Disabled')}</>;
  const codecSummary = (values: RuntimeCodecConfiguration[]) => <>{values.map((value, index) => <span key={value.CodecId}>{index ? ' · ' : ''}{value.CodecId === 'V-E-libx264' ? 'H.264' : 'HEVC'}: {t(value.IsEnabled ? 'Enabled' : 'Disabled')} ({t('Priority')} {value.Priority})</span>)}</>;
  const toneSizeLabel = (value: RuntimeSoftwareToneMapOptions['ToneMapSizeLimit']) => value === 'None' ? t('No additional height limit') : value.slice(5) + ' px';
  const profileLimitLabel = (value: RuntimeCodecProfileLimits['H264'] | RuntimeCodecProfileLimits['HEVC']) => ({ baseline: 'Baseline', main: 'Main', high: 'High', main10: 'Main 10' })[value];
  const profileLimitSummary = (value: RuntimeCodecProfileLimits) => <><span translate="no">H.264: {profileLimitLabel(value.H264)} · HEVC: {profileLimitLabel(value.HEVC)}</span></>;
  const extractionThreadSummary = (value: number) => value === 0 ? t('Automatic') : t('Thread preference: {count}', { count: value });

  if (section === 'network') return <SettingsGroup title="HTTP listener" description="Saved changes update the desired listener and require a service restart. The active listener continues serving this session until then.">
    {settings.Network.RestartRequired && <Alert severity="warning">{t('The saved listener requires a service restart.')} {settings.Network.ReconnectURL ? <>{t('Reconnect after the restart at')} <Box component="span" className="mono" sx={{ overflowWrap: 'anywhere' }}>{settings.Network.ReconnectURL}</Box>.</> : t('Reconnect after the restart using the configured server address.')}</Alert>}
    <div className="settings-form-rows">
      {(['BindHost', 'HttpPort'] as const).map((field) => {
        const label = field === 'BindHost' ? 'Bind address' : 'HTTP port';
        const value = network[field];
        const change = (patch: Partial<typeof value>) => onChange({ ...draft, Network: { ...network, [field]: { ...value, ...patch } } });
        const defaultLabel = field === 'BindHost'
          ? settings.Defaults.Network.BindHost ? <span translate="no">{settings.Defaults.Network.BindHost}</span> : t('All interfaces')
          : settings.Defaults.Network.HttpPort || t('Automatically assigned port');
        return <section className="settings-setting-row" key={field} aria-label={t(label)}>
          <TextField fullWidth label={label} value={value.override ? value.value : String(settings.Defaults.Network[field])} disabled={disabled || !value.override} onChange={(event) => change({ value: event.target.value })} error={Boolean(issue('Runtime.Network.' + field))} helperText={issue('Runtime.Network.' + field) ?? (field === 'BindHost' ? 'Empty means all interfaces. Use a literal IPv4 or IPv6 address, without a port or zone.' : '1–65535. A deployment value of 0 assigns a port automatically.')} slotProps={{ inputLabel: { shrink: true }, htmlInput: { spellCheck: false, autoComplete: 'off', inputMode: field === 'HttpPort' ? 'numeric' : 'text', maxLength: 128 } }} />
          <div className="settings-default-row"><SettingsSource source={settings.Sources.Network[field]} /><FormControlLabel className="settings-default-label" control={<Checkbox checked={!value.override} disabled={disabled} onChange={(event) => change({ override: !event.target.checked })} slotProps={{ input: { 'aria-label': 'Use deployment default for ' + label.toLowerCase() } }} />} label={<>{t('Use deployment default')} ({defaultLabel})</>} /></div>
        </section>;
      })}
    </div>
    <dl className="settings-listener-status">
      <div><dt>{t(stale ? 'Last confirmed desired listener' : 'Desired listener · next restart')}</dt><dd>{t(networkLabel(settings.Network.Desired.BindHost, settings.Network.Desired.HttpPort))}</dd></div>
      <div><dt>{t('Active listener')}</dt><dd>{t(settings.Network.Active ? networkLabel(settings.Network.Active.BoundHost, settings.Network.Active.HttpPort) : 'No active listener reported')}</dd></div>
    </dl>
    {issue('Runtime.Network') && <Alert severity="error">{issue('Runtime.Network')}</Alert>}
    {issue('Runtime') && <Alert severity="error">{issue('Runtime')}</Alert>}
  </SettingsGroup>;

  return <>
    <SettingsGroup title="Playback transcoding" description="Apply these preferences to new video encoding plans and embedded subtitle delivery. Audio boost applies only when surround audio is encoded as stereo." source={settings.Sources.Transcoding} currentValue={policySummary(settings.Effective.Transcoding)} defaultValue={policySummary(settings.Defaults.Transcoding)} stale={stale}>
      <div className="settings-transcoding-override"><OverrideMode name="Playback transcoding" override={policy.override} disabled={disabled} onChange={(override) => onChange({ ...draft, Transcoding: { ...policy, override } })} /></div>
      <div className="settings-form-rows">
        {([
          ['EnableHevcEncoding', 'Allow HEVC encoding', 'Offer HEVC when the client accepts it. Existing HEVC video can still play without encoding.'],
          ['EnableHardwareEncoding', 'Allow hardware encoding', 'Use the selected hardware encoder when available. Hardware decoding and GPU filters keep their own settings.'],
          ['EnableSubtitleExtraction', 'Extract embedded subtitles during playback', 'Allow embedded text subtitles to be delivered separately. External subtitle files remain available.'],
          ['EnableOnTheFlyAttachmentExtraction', 'Extract subtitle font attachments', 'Use embedded font attachments when burning text subtitles into video.'],
        ] as const).map(([field, label, description]) => <section className="settings-transcoding-toggle" key={field} aria-label={t(label)}>
          <FormControlLabel control={<Switch className="settings-transcoding-switch" checked={policyValue[field]} disabled={policyDisabled} slotProps={{ input: { 'aria-label': t(label), 'aria-describedby': 'transcoding-' + field + '-description' } }} onChange={(event) => changePolicy({ [field]: event.target.checked })} />} label={<Box><Typography variant="body2">{label}</Typography><Typography id={'transcoding-' + field + '-description'} variant="caption" color="text.secondary" component="div">{description}</Typography></Box>} />
          {issue('Runtime.Transcoding.' + field) && <Alert severity="error">{issue('Runtime.Transcoding.' + field)}</Alert>}
        </section>)}
        <TextField className="settings-touch-field" fullWidth label="Audio boost when downmixing" value={policyValue.DownMixAudioBoost} disabled={policyDisabled} onChange={(event) => changePolicy({ DownMixAudioBoost: event.target.value })} error={Boolean(issue('Runtime.Transcoding.DownMixAudioBoost'))} helperText={issue('Runtime.Transcoding.DownMixAudioBoost') ?? '0.1–10. A value of 1 keeps the original volume. Only applies to surround-to-stereo encoding.'} slotProps={{ htmlInput: { inputMode: 'decimal', maxLength: 32 } }} />
      </div>
      {issue('Runtime.Transcoding') && <Alert severity="error">{issue('Runtime.Transcoding')}</Alert>}
      <Box component="section" aria-label={t('Automatic HLS throttling')} sx={{ mt: 2, pt: 2, borderTop: '1px solid var(--line)' }}>
        <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1.25 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>Automatic HLS throttling</Typography><SettingsSource source={settings.Sources.EnableThrottling} /></Stack>
        <div className="settings-transcoding-override"><OverrideMode name="Automatic HLS throttling" override={throttling.override} disabled={disabled} onChange={(override) => onChange({ ...draft, EnableThrottling: { ...throttling, override } })} /></div>
        <div className="settings-transcoding-toggle"><FormControlLabel control={<Switch className="settings-transcoding-switch" checked={throttlingValue} disabled={disabled || !throttling.override} slotProps={{ input: { 'aria-label': t('Enable automatic HLS throttling'), 'aria-describedby': 'transcoding-throttling-description' } }} onChange={(event) => onChange({ ...draft, EnableThrottling: { ...throttling, value: event.target.checked } })} />} label={<Box><Typography variant="body2">Enable automatic HLS throttling</Typography><Typography id="transcoding-throttling-description" variant="caption" color="text.secondary" component="div">For file HLS video transcoding with reported playback progress, wait when enough output is buffered and resume as playback advances. Pending segment requests can continue after a short bounded wait. Applies to new HLS playback sessions.</Typography></Box>} /></div>
        <dl className="settings-confirmed-values">
          <div><dt>{t(stale ? 'Last confirmed' : 'Current value')}</dt><dd>{t(settings.Effective.EnableThrottling ? 'Enabled' : 'Disabled')}</dd></div>
          <div><dt>{t('Deployment default')}</dt><dd>{t(settings.Defaults.EnableThrottling ? 'Enabled' : 'Disabled')}</dd></div>
        </dl>
        {issue('Runtime.EnableThrottling') && <Alert severity="error">{issue('Runtime.EnableThrottling')}</Alert>}
      </Box>
      <Box component="section" aria-label={t('ASS to plain text')} sx={{ mt: 2, pt: 2, borderTop: '1px solid var(--line)' }}>
        <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1.25 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>ASS to plain text</Typography><SettingsSource source={settings.Sources.SubtitleOptions} /></Stack>
        <div className="settings-transcoding-override"><OverrideMode name="ASS to plain text" override={subtitles.override} disabled={disabled} onChange={(override) => onChange({ ...draft, SubtitleOptions: { ...subtitles, override } })} /></div>
        <div className="settings-transcoding-toggle"><FormControlLabel control={<Switch className="settings-transcoding-switch" checked={subtitleValue.AllowAssToText} disabled={disabled || !subtitles.override} slotProps={{ input: { 'aria-label': t('Allow ASS to plain text conversion'), 'aria-describedby': 'transcoding-ass-text-description' } }} onChange={(event) => onChange({ ...draft, SubtitleOptions: { ...subtitles, value: { AllowAssToText: event.target.checked } } })} />} label={<Box><Typography variant="body2">Allow ASS to plain text conversion</Typography><Typography id="transcoding-ass-text-description" variant="caption" color="text.secondary" component="div">During playback negotiation, allow ASS subtitles in plain text formats. Plain text removes ASS styling; ASS delivery retains styling.</Typography></Box>} /></div>
        <dl className="settings-confirmed-values">
          <div><dt>{t(stale ? 'Last confirmed' : 'Current value')}</dt><dd>{t(settings.Effective.SubtitleOptions.AllowAssToText ? 'Enabled' : 'Disabled')}</dd></div>
          <div><dt>{t('Deployment default')}</dt><dd>{t(settings.Defaults.SubtitleOptions.AllowAssToText ? 'Enabled' : 'Disabled')}</dd></div>
        </dl>
        {issue('Runtime.SubtitleOptions.AllowAssToText') && <Alert severity="error">{issue('Runtime.SubtitleOptions.AllowAssToText')}</Alert>}
        {issue('Runtime.SubtitleOptions') && <Alert severity="error">{issue('Runtime.SubtitleOptions')}</Alert>}
      </Box>
    </SettingsGroup>

    <SettingsGroup title="Hardware acceleration" description="Choose how new playback plans decode and encode video. Availability describes the selected device, not support for every media format." source={settings.Sources.Hardware} currentValue={hardwareLabel(settings.Effective.Hardware)} defaultValue={hardwareLabel(settings.Defaults.Hardware)} stale={stale}>
      {!policyValue.EnableHardwareEncoding && <Alert severity="info">Hardware encoding is disabled in the selected preferences. The decoding selection and GPU filter device still apply.</Alert>}
      <div className="settings-device-grid" role="group" aria-label={t('Transcoding device')}>
        {settings.Hardware.Devices.map((device) => <button key={device.DeviceId} type="button" className="settings-device-card" aria-pressed={hardware.DeviceId === device.DeviceId && (hardware.Decode === 'vaapi' || hardware.Encode === 'vaapi')} disabled={disabled || !device.Available} onClick={() => onChange({ ...draft, Hardware: { override: true, value: { Decode: 'vaapi', Encode: 'vaapi', DeviceId: device.DeviceId } } })}>
          <span className="settings-device-radio" aria-hidden="true" /><span><strong>{device.Label || t('AMD VA-API')}</strong><code>{device.DeviceId}</code><small>{t(device.Available ? 'Device available' : 'Device unavailable')}{device.Code ? ' · ' + device.Code : ''}</small></span>
        </button>)}
        <button type="button" className="settings-device-card" aria-pressed={hardware.Decode === 'software' && hardware.Encode === 'software' && !hardware.DeviceId} disabled={disabled} onClick={() => onChange({ ...draft, Hardware: { override: true, value: { Decode: 'software', Encode: 'software', DeviceId: '' } } })}>
          <span className="settings-device-radio" aria-hidden="true" /><span><strong>{t('Software only (CPU)')}</strong><code>libx264 · libx265</code><small>{t('GPU filters require a selected device')}</small></span>
        </button>
      </div>
      <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1.5 }}>
        <OverrideMode name="Hardware acceleration" override={draft.Hardware.override} disabled={disabled} onChange={(override) => onChange({ ...draft, Hardware: { ...draft.Hardware, override } })} />
        {hardwareMatchesSaved && <Chip size="small" icon={settings.Hardware.Available ? <CheckCircleRounded /> : undefined} color={settings.Hardware.Available ? 'success' : 'warning'} label={settings.Hardware.Available ? 'Selected processing available' : 'Selected processing unavailable'} />}
        {!hardwareMatchesSaved && <Chip size="small" label="Availability checked after saving" />}
      </Stack>
      {!settings.Hardware.Available && <Alert severity="warning">The saved hardware selection is unavailable. {settings.Hardware.Code && ('Status: ' + settings.Hardware.Code + '.')} For CPU processing without GPU filters, choose CPU for both axes and clear the AMD device.</Alert>}
      <div className="settings-form-rows">
          {(['Decode', 'Encode'] as const).map((field) => <TextField key={field} fullWidth select label={field === 'Decode' ? 'Video decoding' : 'Video encoding'} value={hardware[field]} disabled={hardwareDisabled} onChange={(event) => onChange({ ...draft, Hardware: { ...draft.Hardware, value: { ...draft.Hardware.value, [field]: event.target.value } } })} error={Boolean(issue('Runtime.Hardware.' + field))} helperText={issue('Runtime.Hardware.' + field)}><MenuItem value="software">CPU</MenuItem><MenuItem value="vaapi">AMD VA-API</MenuItem>{!['software', 'vaapi'].includes(hardware[field]) && <MenuItem value={hardware[field]} disabled>{modeLabel(hardware[field])}</MenuItem>}</TextField>)}
        <TextField className="settings-device-field" select fullWidth label="AMD device" slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }} value={hardware.DeviceId} disabled={hardwareDisabled} onChange={(event) => onChange({ ...draft, Hardware: { ...draft.Hardware, value: { ...draft.Hardware.value, DeviceId: event.target.value } } })} error={Boolean(issue('Runtime.Hardware.DeviceId'))} helperText={issue('Runtime.Hardware.DeviceId') ?? 'Required for AMD VA-API. CPU jobs may also use this device for Vulkan filters. Devices are managed by the deployment.'}>
          <MenuItem value="">No device selected</MenuItem>
          {settings.Hardware.Devices.map((device) => <MenuItem key={device.DeviceId} value={device.DeviceId} disabled={!device.Available}>{device.Label || device.DeviceId}{device.Available ? '' : ' (unavailable)'}</MenuItem>)}
          {hardware.DeviceId && !knownDevice && <MenuItem value={hardware.DeviceId} disabled>Saved device is unavailable ({hardware.DeviceId})</MenuItem>}
        </TextField>
      </div>
      {issue('Runtime.Hardware') && <Alert severity="error">{issue('Runtime.Hardware')}</Alert>}
    </SettingsGroup>

    <HardwareCapabilitiesPanel savedSelectionKey={JSON.stringify(settings.Effective.Hardware)} disabled={disabled} />

    <SettingsGroup title="Software encoder selection" description="Choose whether to offer the H.264 and HEVC software encoders. Higher priorities are tried first among formats accepted by the client; equal priorities preserve the client's order." source={settings.Sources.CodecConfigurations} currentValue={codecSummary(settings.Effective.CodecConfigurations)} defaultValue={codecSummary(settings.Defaults.CodecConfigurations)} stale={stale}>
      <div className="settings-transcoding-override"><OverrideMode name="Software encoder selection" override={codecs.override} disabled={disabled} onChange={(override) => onChange({ ...draft, CodecConfigurations: { ...codecs, override } })} /></div>
      <div className="settings-form-rows">
        {codecValues.map((value) => {
          const label = value.CodecId === 'V-E-libx264' ? 'H.264' : 'HEVC';
          return <section className="settings-software-encoder" key={value.CodecId} aria-label={t(label + ' software encoder')}>
            <div className="settings-transcoding-toggle"><FormControlLabel control={<Switch className="settings-transcoding-switch" checked={value.IsEnabled} disabled={disabled || !codecs.override} onChange={(event) => changeCodec(value.CodecId, { IsEnabled: event.target.checked })} />} label={label + ' software encoder'} /></div>
            <TextField className="settings-touch-field" fullWidth label={label + ' encoder priority'} value={value.Priority} disabled={disabled || !codecs.override || !value.IsEnabled} onChange={(event) => changeCodec(value.CodecId, { Priority: event.target.value })} error={Boolean(issue('Runtime.CodecConfigurations.' + value.CodecId + '.Priority'))} helperText={issue('Runtime.CodecConfigurations.' + value.CodecId + '.Priority') ?? '−1000 to 1000. Higher values are selected first.'} slotProps={{ htmlInput: { inputMode: 'text', maxLength: 5 } }} />
          </section>;
        })}
      </div>
      {issue('Runtime.CodecConfigurations') && <Alert severity="error">{issue('Runtime.CodecConfigurations')}</Alert>}
      <Box component="section" aria-label={t('Video profile limits')} sx={{ mt: 2, pt: 2, borderTop: '1px solid var(--line)' }}>
        <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1.25 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>Video profile limits</Typography><SettingsSource source={settings.Sources.CodecProfileLimits} /></Stack>
        <Typography variant="caption" color="text.secondary">Limit profiles for new encoding plans. A compatible lower profile can still be selected. Existing playback retains its chosen profile.</Typography>
        <div className="settings-transcoding-override"><OverrideMode name="Video profile limits" override={profileLimits.override} disabled={disabled} onChange={(override) => onChange({ ...draft, CodecProfileLimits: { ...profileLimits, override } })} /></div>
        <div className="settings-form-rows">
          <TextField className="settings-touch-field" select fullWidth label="H.264 profile limit" value={profileLimitValue.H264} disabled={disabled || !profileLimits.override} onChange={(event) => changeProfileLimits({ H264: event.target.value as RuntimeCodecProfileLimits['H264'] })} error={Boolean(issue('Runtime.CodecProfileLimits.H264'))} helperText={issue('Runtime.CodecProfileLimits.H264') ?? 'High allows the full supported H.264 profile range.'}>{h264ProfileLimits.map((profile) => <MenuItem key={profile} value={profile} translate="no">{profileLimitLabel(profile)}</MenuItem>)}</TextField>
          <TextField className="settings-touch-field" select fullWidth label="HEVC profile limit" value={profileLimitValue.HEVC} disabled={disabled || !profileLimits.override} onChange={(event) => changeProfileLimits({ HEVC: event.target.value as RuntimeCodecProfileLimits['HEVC'] })} error={Boolean(issue('Runtime.CodecProfileLimits.HEVC'))} helperText={issue('Runtime.CodecProfileLimits.HEVC') ?? 'Main 10 allows the full supported HEVC profile range.'}>{hevcProfileLimits.map((profile) => <MenuItem key={profile} value={profile} translate="no">{profileLimitLabel(profile)}</MenuItem>)}</TextField>
        </div>
        <dl className="settings-confirmed-values">
          <div><dt>{t(stale ? 'Last confirmed' : 'Current value')}</dt><dd>{profileLimitSummary(settings.Effective.CodecProfileLimits)}</dd></div>
          <div><dt>{t('Deployment default')}</dt><dd>{profileLimitSummary(settings.Defaults.CodecProfileLimits)}</dd></div>
        </dl>
        {issue('Runtime.CodecProfileLimits') && <Alert severity="error">{issue('Runtime.CodecProfileLimits')}</Alert>}
      </Box>
    </SettingsGroup>

    <SettingsGroup title="Threads per job" description="Retain the existing Goby job thread limit. Each software encoder can inherit this limit or use its own automatic or manual thread setting." source={settings.Sources.Threads} currentValue={settings.Effective.Threads === 0 ? 'Automatic' : String(settings.Effective.Threads)} defaultValue={settings.Defaults.Threads === 0 ? 'Automatic' : String(settings.Defaults.Threads)} stale={stale}>
      <OverrideMode name="Threads per job" override={draft.Threads.override} disabled={disabled} onChange={(override) => onChange({ ...draft, Threads: { ...draft.Threads, override } })} />
      <TextField label="Threads per job" value={draft.Threads.override ? draft.Threads.value : settings.Defaults.Threads === 0 ? 'Automatic' : String(settings.Defaults.Threads)} disabled={disabled || !draft.Threads.override} onChange={(event) => onChange({ ...draft, Threads: { ...draft.Threads, value: event.target.value } })} error={Boolean(issue('Runtime.Threads'))} helperText={issue('Runtime.Threads') ?? '1–64. Used by encoders that select the job thread limit.'} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 10 } }} />
      <Box component="section" aria-label={t('Image extraction threads')} sx={{ mt: 2, pt: 2, borderTop: '1px solid var(--line)' }}>
        <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1.25 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>Image extraction threads</Typography><SettingsSource source={settings.Sources.ExtractionThreadCount} /></Stack>
        <div className="settings-transcoding-override"><OverrideMode name="Image extraction threads" override={extractionThreads.override} disabled={disabled} onChange={(override) => onChange({ ...draft, ExtractionThreadCount: { ...extractionThreads, override } })} /></div>
        <TextField className="settings-touch-field" fullWidth label="Image extraction thread preference" value={extractionThreads.override ? extractionThreads.value : String(settings.Defaults.ExtractionThreadCount)} disabled={disabled || !extractionThreads.override} onChange={(event) => onChange({ ...draft, ExtractionThreadCount: { ...extractionThreads, value: event.target.value } })} error={Boolean(issue('Runtime.ExtractionThreadCount'))} helperText={issue('Runtime.ExtractionThreadCount') ?? 'Thread preference for the next preview thumbnail extraction. Use 0–16; 0 or a value at least the available logical processor count lets FFmpeg choose automatically.'} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 10 } }} />
        <dl className="settings-confirmed-values">
          <div><dt>{t(stale ? 'Last confirmed' : 'Current value')}</dt><dd>{extractionThreadSummary(settings.Effective.ExtractionThreadCount)}</dd></div>
          <div><dt>{t('Deployment default')}</dt><dd>{extractionThreadSummary(settings.Defaults.ExtractionThreadCount)}</dd></div>
        </dl>
      </Box>
    </SettingsGroup>

    {(['H264', 'HEVC'] as const).map((codec) => {
      const label = codec === 'H264' ? 'H.264' : 'HEVC';
      const quality = draft[codec];
      const defaults = settings.Defaults[codec];
      const value: RuntimeQualityDraft = quality.override ? quality.value : { ...defaults, CRF: String(defaults.CRF), ThreadCount: String(defaults.ThreadCount || Math.min(16, Math.max(1, settings.Defaults.Threads || 2))), ThreadMode: defaults.ThreadCount === null ? 'inherit' : defaults.ThreadCount === 0 ? 'automatic' : 'limited' };
      const change = (patch: Partial<typeof quality.value>) => onChange({ ...draft, [codec]: { ...quality, value: { ...quality.value, ...patch } } });
      return <SettingsGroup key={codec} title={label + ' CPU encoding'} description={'Applies to new ' + label + ' software encoding jobs, including CPU fallback. Hardware encoding uses its own profile.'} source={settings.Sources[codec]} currentValue={qualitySummary(settings.Effective[codec], settings.Effective.Threads)} defaultValue={qualitySummary(settings.Defaults[codec], settings.Defaults.Threads)} stale={stale}>
        <div className="settings-transcoding-override"><OverrideMode name={label + ' CPU encoding'} override={quality.override} disabled={disabled} onChange={(override) => onChange({ ...draft, [codec]: { ...quality, override } })} /></div>
        {codec === 'HEVC' && !policyValue.EnableHevcEncoding && <Typography variant="caption" color="text.secondary">HEVC encoding is disabled in the selected preferences. These settings apply when it is enabled.</Typography>}
        <div className="settings-form-rows">
          <TextField className="settings-touch-field" select fullWidth label={label + ' CPU preset'} value={value.Preset} disabled={disabled || !quality.override} onChange={(event) => change({ Preset: event.target.value as RuntimeQuality['Preset'] })} error={Boolean(issue('Runtime.' + codec + '.Preset'))} helperText={issue('Runtime.' + codec + '.Preset') ?? 'Slower uses more CPU for better compression.'}>{presetsForCodec(codec).map((preset) => <MenuItem key={preset} value={preset}>{preset}</MenuItem>)}</TextField>
          <TextField className="settings-touch-field" select fullWidth label={label + ' rate control'} value={value.RateControl} disabled={disabled || !quality.override} onChange={(event) => change({ RateControl: event.target.value as RuntimeQuality['RateControl'] })} error={Boolean(issue('Runtime.' + codec + '.RateControl'))} helperText={issue('Runtime.' + codec + '.RateControl')}><MenuItem value="bitrate">Bitrate target</MenuItem><MenuItem value="capped_crf">Capped CRF</MenuItem></TextField>
          <TextField className="settings-touch-field" fullWidth label={label + ' CRF'} value={value.CRF} disabled={disabled || !quality.override || value.RateControl !== 'capped_crf'} onChange={(event) => change({ CRF: event.target.value })} error={Boolean(issue('Runtime.' + codec + '.CRF'))} helperText={issue('Runtime.' + codec + '.CRF') ?? '1–51. Lower means higher quality. Only used for capped CRF.'} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 10 } }} />
          <TextField className="settings-touch-field" select fullWidth label={label + ' encoder threads'} value={value.ThreadMode} disabled={disabled || !quality.override} onChange={(event) => change({ ThreadMode: event.target.value as RuntimeQualityDraft['ThreadMode'] })} error={Boolean(issue('Runtime.' + codec + '.ThreadCount'))} helperText={issue('Runtime.' + codec + '.ThreadCount') ?? 'Automatic lets the encoder choose. The job thread limit retains the existing Goby setting.'}><MenuItem value="inherit">Use job thread limit</MenuItem><MenuItem value="automatic">Automatic</MenuItem><MenuItem value="limited">Limit threads</MenuItem></TextField>
          {value.ThreadMode === 'limited' && <TextField className="settings-touch-field" fullWidth label={label + ' thread limit'} value={value.ThreadCount} disabled={disabled || !quality.override} onChange={(event) => change({ ThreadCount: event.target.value })} error={Boolean(issue('Runtime.' + codec + '.ThreadCount'))} helperText={issue('Runtime.' + codec + '.ThreadCount') ?? '0–16. Empty or 0 lets the encoder choose automatically.'} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 10 } }} />}
        </div>
        {codec === 'H264' && <details className="settings-encoder-options" ref={x264Details}>
          <summary>{t('Advanced H.264 options')}</summary>
          <TextField className="settings-touch-field" fullWidth multiline minRows={2} label="H.264 encoder options" value={value.X264Options} disabled={disabled || !quality.override} onChange={(event) => change({ X264Options: event.target.value })} error={Boolean(issue('Runtime.H264.X264Options'))} helperText={issue('Runtime.H264.X264Options') ?? 'Colon-separated x264 settings, such as subme=0:me_range=4:rc_lookahead=10:partitions=none. Use the supported options listed below.'} slotProps={{ htmlInput: { spellCheck: false, autoComplete: 'off', maxLength: 512 } }} />
          <p className="settings-encoder-option-help">{t('Supported x264 options')}: <code>subme=0–9 · me-range=4–16 · rc-lookahead=0–250 · partitions=none|all · aq-mode=0–3 · aq-strength=0–3 · deblock=-6..6,-6..6</code></p>
        </details>}
        {issue('Runtime.' + codec) && <Alert severity="error">{issue('Runtime.' + codec)}</Alert>}
      </SettingsGroup>;
    })}

    <SettingsGroup title="HDR tone mapping" description="Allow software and Vulkan filters to convert HDR video for SDR output. The chosen filter must be available on this server.">
      {(['SoftwareToneMapping', 'VulkanToneMapping'] as const).map((field) => {
        const label = field === 'SoftwareToneMapping' ? 'Software tone mapping' : 'Vulkan tone mapping';
        const value = draft[field].override ? draft[field].value : settings.Defaults[field];
        return <Box key={field} component="section" aria-label={label} sx={{ pb: 1.5, '& + section': { pt: 2, borderTop: '1px solid var(--line)' } }}>
          <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1.25 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>{label}</Typography><SettingsSource source={settings.Sources[field]} /><Typography variant="caption" color="text.secondary">{stale ? 'Last confirmed' : 'Current'}: {settings.Effective[field] ? 'Enabled' : 'Disabled'}</Typography></Stack>
          <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1.5 }}>
            <OverrideMode name={label} override={draft[field].override} disabled={disabled} onChange={(override) => onChange({ ...draft, [field]: { ...draft[field], override } })} />
            <FormControlLabel sx={{ m: 0 }} control={<Switch className="settings-transcoding-switch" checked={value} disabled={disabled || !draft[field].override} onChange={(event) => onChange({ ...draft, [field]: { ...draft[field], value: event.target.checked } })} />} label={'Enable ' + label.toLowerCase()} />
          </Stack>
          {issue('Runtime.' + field) && <Alert severity="error" sx={{ mt: 1 }}>{issue('Runtime.' + field)}</Alert>}
        </Box>;
      })}
      <Box component="section" aria-label={t('Software tone mapping parameters')} sx={{ pt: 2, borderTop: '1px solid var(--line)' }}>
        <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1.25 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>Software tone mapping parameters</Typography><SettingsSource source={settings.Sources.SoftwareToneMapOptions} /></Stack>
        <div className="settings-transcoding-override"><OverrideMode name="Software tone mapping parameters" override={toneOptions.override} disabled={disabled} onChange={(override) => onChange({ ...draft, SoftwareToneMapOptions: { ...toneOptions, override } })} /></div>
        {!softwareToneMapping && <Typography variant="caption" color="text.secondary">Software tone mapping is disabled in the selected preferences. Its parameters apply when it is enabled.</Typography>}
        <div className="settings-form-rows">
          <TextField className="settings-touch-field" select fullWidth label="Software tone mapping algorithm" value={toneValue.SoftwareToneMapAlgorithm} disabled={disabled || !toneOptions.override || !softwareToneMapping} onChange={(event) => changeToneOptions({ SoftwareToneMapAlgorithm: event.target.value as RuntimeSoftwareToneMapOptions['SoftwareToneMapAlgorithm'] })} error={Boolean(issue('Runtime.SoftwareToneMapOptions.SoftwareToneMapAlgorithm'))} helperText={issue('Runtime.SoftwareToneMapOptions.SoftwareToneMapAlgorithm') ?? 'Choose the algorithm used by the software HDR-to-SDR filter.'}>{softwareToneMapAlgorithms.map((algorithm) => <MenuItem key={algorithm} value={algorithm}>{algorithm}</MenuItem>)}</TextField>
          <TextField className="settings-touch-field" select fullWidth label="Software tone mapping height limit" value={toneValue.ToneMapSizeLimit} disabled={disabled || !toneOptions.override || !softwareToneMapping} onChange={(event) => changeToneOptions({ ToneMapSizeLimit: event.target.value as RuntimeSoftwareToneMapOptions['ToneMapSizeLimit'] })} error={Boolean(issue('Runtime.SoftwareToneMapOptions.ToneMapSizeLimit'))} helperText={issue('Runtime.SoftwareToneMapOptions.ToneMapSizeLimit') ?? 'Limit software tone-mapped output height while preserving aspect ratio. Other output limits still apply.'}>{softwareToneMapSizeLimits.map((limit) => <MenuItem key={limit} value={limit}>{limit === 'None' ? 'No additional height limit' : limit.slice(5) + ' px'}</MenuItem>)}</TextField>
        </div>
        <dl className="settings-confirmed-values">
          <div><dt>{t(stale ? 'Last confirmed' : 'Current value')}</dt><dd><span translate="no">{settings.Effective.SoftwareToneMapOptions.SoftwareToneMapAlgorithm}</span> · {toneSizeLabel(settings.Effective.SoftwareToneMapOptions.ToneMapSizeLimit)}</dd></div>
          <div><dt>{t('Deployment default')}</dt><dd><span translate="no">{settings.Defaults.SoftwareToneMapOptions.SoftwareToneMapAlgorithm}</span> · {toneSizeLabel(settings.Defaults.SoftwareToneMapOptions.ToneMapSizeLimit)}</dd></div>
        </dl>
        {issue('Runtime.SoftwareToneMapOptions') && <Alert severity="error">{issue('Runtime.SoftwareToneMapOptions')}</Alert>}
      </Box>
    </SettingsGroup>
    <SettingsGroup title="Transcoding temporary directory" description="Use a dedicated directory that the service can write. A saved directory change applies after a service restart; the active cache remains in use until then." source={settings.Sources.TranscodingTempPath}>
      <div className="settings-transcoding-override"><OverrideMode name="Transcoding temporary directory" override={temporaryPath.override} disabled={disabled} onChange={(override) => onChange({ ...draft, TranscodingTempPath: { ...temporaryPath, override } })} /></div>
      <TextField className="settings-device-field settings-touch-field" fullWidth label="Transcoding temporary directory" value={temporaryPath.override ? temporaryPath.value : settings.Defaults.TranscodingTempPath} disabled={disabled || !temporaryPath.override} onChange={(event) => onChange({ ...draft, TranscodingTempPath: { ...temporaryPath, value: event.target.value } })} error={Boolean(issue('Runtime.TranscodingTempPath'))} helperText={issue('Runtime.TranscodingTempPath') ?? 'Use an absolute directory path, or leave empty for the deployment default. Takes effect after restart.'} slotProps={{ htmlInput: { spellCheck: false, autoComplete: 'off', maxLength: 4096 } }} />
      {settings.TranscodingTempPath.RestartRequired && <Alert severity="info">The saved transcoding directory will be used after a service restart.</Alert>}
      <dl className="settings-listener-status">
        <div><dt>{t(stale ? 'Last confirmed requested directory' : 'Requested directory · next restart')}</dt><dd translate="no">{settings.TranscodingTempPath.Desired}</dd></div>
        <div><dt>{t('Active transcoding directory')}</dt><dd>{settings.TranscodingTempPath.Active === null ? t('No active transcoding directory reported') : <span translate="no">{settings.TranscodingTempPath.Active}</span>}</dd></div>
        <div><dt>{t('Deployment default')}</dt><dd translate="no">{settings.Defaults.TranscodingTempPath}</dd></div>
      </dl>
    </SettingsGroup>
    {issue('Runtime') && <Alert severity="error">{issue('Runtime')}</Alert>}
  </>;
}
