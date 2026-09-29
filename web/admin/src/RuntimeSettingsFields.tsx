import { Alert, Box, Checkbox, Chip, FormControlLabel, MenuItem, Stack, Switch, TextField, Typography } from '@mui/material';
import CheckCircleRounded from '@mui/icons-material/CheckCircleRounded';
import { fieldError } from './formFields';
import { OverrideMode, SettingsGroup, SettingsSource } from './SettingsLayout';
import { cpuPresets } from './runtimeSettings';
import type { RuntimeHardware, RuntimeQuality, RuntimeSettings } from './runtimeSettings';
import type { RuntimeDraft } from './runtimeSettingsDraft';

const modeLabel = (value: string) => value === 'software' ? 'CPU' : value === 'vaapi' ? 'AMD VA-API' : (value || 'Unconfigured') + ' (deployment)';
const networkLabel = (host: string, port: number) => (host || 'All interfaces') + ' · ' + (port === 0 ? 'Automatically assigned port' : 'Port ' + port);
const hardwareLabel = (value: RuntimeHardware) => 'Decode: ' + modeLabel(value.Decode) + ' · Encode: ' + modeLabel(value.Encode);
const qualityLabel = (value: RuntimeQuality) => value.Preset + ' · ' + (value.RateControl === 'bitrate' ? 'Bitrate target' : 'Capped CRF ' + value.CRF);
const fieldGrid = { display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2 };

interface Props { settings: RuntimeSettings; draft: RuntimeDraft; disabled: boolean; stale: boolean; errors: Record<string, string | undefined>; mutationError: unknown; section?: 'network' | 'hardware'; onChange: (value: RuntimeDraft) => void }

export function RuntimeSettingsFields({ settings, draft, disabled, stale, errors, mutationError, section = 'hardware', onChange }: Props) {
  const issue = (field: string) => errors[field] ?? fieldError(mutationError, field);
  const network = draft.Network;
  const hardware = draft.Hardware.override ? draft.Hardware.value : settings.Defaults.Hardware;
  const knownDevice = settings.Hardware.Devices.find((device) => device.DeviceId === hardware.DeviceId);
  const hardwareDisabled = disabled || !draft.Hardware.override;
  const hardwareMatchesSaved = JSON.stringify(hardware) === JSON.stringify(settings.Effective.Hardware);

  if (section === 'network') return <SettingsGroup title="HTTP listener" description="Saved changes update the desired listener and require a service restart. The active listener continues serving this session until then.">
    <Box component="dl" sx={{ ...fieldGrid, m: 0, gap: 1.5 }}>
      <Box sx={{ bgcolor: '#F3F6FB', borderRadius: '12px', p: 2 }}><Typography component="dt" variant="caption" color="text.secondary">{stale ? 'Last confirmed desired listener' : 'Desired listener · next restart'}</Typography><Typography component="dd" variant="body2" className="mono" sx={{ m: 0, mt: 0.5, overflowWrap: 'anywhere' }}>{networkLabel(settings.Network.Desired.BindHost, settings.Network.Desired.HttpPort)}</Typography></Box>
      <Box sx={{ bgcolor: '#F3F6FB', borderRadius: '12px', p: 2 }}><Typography component="dt" variant="caption" color="text.secondary">Active listener</Typography><Typography component="dd" variant="body2" className="mono" sx={{ m: 0, mt: 0.5, overflowWrap: 'anywhere' }}>{settings.Network.Active ? networkLabel(settings.Network.Active.BoundHost, settings.Network.Active.HttpPort) : 'No active listener reported'}</Typography></Box>
    </Box>
    {settings.Network.RestartRequired && <Alert severity="warning">The saved listener requires a service restart. Reconnect after the restart{settings.Network.ReconnectURL ? <> at <Box component="span" className="mono" sx={{ overflowWrap: 'anywhere' }}>{settings.Network.ReconnectURL}</Box>.</> : ' using the configured server address.'}</Alert>}
    <Box sx={fieldGrid}>
      {(['BindHost', 'HttpPort'] as const).map((field) => {
        const label = field === 'BindHost' ? 'Bind address' : 'HTTP port';
        const value = network[field];
        const change = (patch: Partial<typeof value>) => onChange({ ...draft, Network: { ...network, [field]: { ...value, ...patch } } });
        const defaultLabel = field === 'BindHost' ? settings.Defaults.Network.BindHost || 'All interfaces' : settings.Defaults.Network.HttpPort || 'Automatically assigned port';
        return <Box key={field} component="section" aria-label={label}>
          <TextField fullWidth label={label} value={value.override ? value.value : String(settings.Defaults.Network[field])} disabled={disabled || !value.override} onChange={(event) => change({ value: event.target.value })} error={Boolean(issue('Runtime.Network.' + field))} helperText={issue('Runtime.Network.' + field) ?? (field === 'BindHost' ? 'Empty means all interfaces. Use a literal IPv4 or IPv6 address, without a port or zone.' : '1–65535. A deployment value of 0 assigns a port automatically.')} slotProps={{ inputLabel: { shrink: true }, htmlInput: { spellCheck: false, autoComplete: 'off', inputMode: field === 'HttpPort' ? 'numeric' : 'text', maxLength: 128 } }} sx={{ opacity: value.override ? 1 : 0.45 }} />
          <FormControlLabel sx={{ m: 0, mt: 0.5, alignItems: 'flex-start', '& .MuiCheckbox-root': { p: 0.5, mr: 0.5 }, '& .MuiFormControlLabel-label': { fontSize: 12, pt: 0.5 } }} control={<Checkbox checked={!value.override} disabled={disabled} onChange={(event) => change({ override: !event.target.checked })} slotProps={{ input: { 'aria-label': 'Use deployment default for ' + label.toLowerCase() } }} />} label={'Use deployment default (' + defaultLabel + ')'} />
          <Box sx={{ mt: 0.5 }}><SettingsSource source={settings.Sources.Network[field]} /></Box>
        </Box>;
      })}
    </Box>
    {issue('Runtime.Network') && <Alert severity="error">{issue('Runtime.Network')}</Alert>}
    {issue('Runtime') && <Alert severity="error">{issue('Runtime')}</Alert>}
  </SettingsGroup>;

  return <>
    <SettingsGroup title="Hardware acceleration" description="Choose how new playback plans decode and encode video. Availability describes the selected device, not support for every media format." source={settings.Sources.Hardware} currentValue={hardwareLabel(settings.Effective.Hardware)} defaultValue={hardwareLabel(settings.Defaults.Hardware)} stale={stale}>
      <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1.5 }}>
        <OverrideMode name="Hardware acceleration" override={draft.Hardware.override} disabled={disabled} onChange={(override) => onChange({ ...draft, Hardware: { ...draft.Hardware, override } })} />
        {hardwareMatchesSaved && <Chip size="small" icon={settings.Hardware.Available ? <CheckCircleRounded /> : undefined} color={settings.Hardware.Available ? 'success' : 'warning'} label={settings.Hardware.Available ? 'Selected hardware available' : 'Selected hardware unavailable'} />}
        {!hardwareMatchesSaved && <Chip size="small" label="Availability checked after saving" />}
      </Stack>
      {!settings.Hardware.Available && <Alert severity="warning">The saved hardware selection is unavailable. {settings.Hardware.Code && ('Status: ' + settings.Hardware.Code + '.')} For CPU processing without GPU filters, choose CPU for both axes and clear the AMD device.</Alert>}
      <Stack spacing={2} sx={{ opacity: draft.Hardware.override ? 1 : 0.45 }}>
        <Box sx={fieldGrid}>
          {(['Decode', 'Encode'] as const).map((field) => <TextField key={field} fullWidth select label={field === 'Decode' ? 'Video decoding' : 'Video encoding'} value={hardware[field]} disabled={hardwareDisabled} onChange={(event) => onChange({ ...draft, Hardware: { ...draft.Hardware, value: { ...draft.Hardware.value, [field]: event.target.value } } })} error={Boolean(issue('Runtime.Hardware.' + field))} helperText={issue('Runtime.Hardware.' + field)}><MenuItem value="software">CPU</MenuItem><MenuItem value="vaapi">AMD VA-API</MenuItem>{!['software', 'vaapi'].includes(hardware[field]) && <MenuItem value={hardware[field]} disabled>{modeLabel(hardware[field])}</MenuItem>}</TextField>)}
        </Box>
        <TextField select fullWidth label="AMD device" slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }} value={hardware.DeviceId} disabled={hardwareDisabled} onChange={(event) => onChange({ ...draft, Hardware: { ...draft.Hardware, value: { ...draft.Hardware.value, DeviceId: event.target.value } } })} error={Boolean(issue('Runtime.Hardware.DeviceId'))} helperText={issue('Runtime.Hardware.DeviceId') ?? 'Required for AMD VA-API. CPU jobs may also use this device for Vulkan filters. Devices are managed by the deployment.'}>
          <MenuItem value="">No device selected</MenuItem>
          {settings.Hardware.Devices.map((device) => <MenuItem key={device.DeviceId} value={device.DeviceId} disabled={!device.Available}>{device.Label || device.DeviceId}{device.Available ? '' : ' (unavailable)'}</MenuItem>)}
          {hardware.DeviceId && !knownDevice && <MenuItem value={hardware.DeviceId} disabled>Saved device is unavailable ({hardware.DeviceId})</MenuItem>}
        </TextField>
      </Stack>
      {issue('Runtime.Hardware') && <Alert severity="error">{issue('Runtime.Hardware')}</Alert>}
    </SettingsGroup>

    <SettingsGroup title="Threads per job" description="The FFmpeg thread setting captured by each newly admitted job." source={settings.Sources.Threads} currentValue={settings.Effective.Threads === 0 ? 'Automatic' : String(settings.Effective.Threads)} defaultValue={settings.Defaults.Threads === 0 ? 'Automatic' : String(settings.Defaults.Threads)} stale={stale}>
      <OverrideMode name="Threads per job" override={draft.Threads.override} disabled={disabled} onChange={(override) => onChange({ ...draft, Threads: { ...draft.Threads, override } })} />
      <TextField label="Threads per job" value={draft.Threads.override ? draft.Threads.value : settings.Defaults.Threads === 0 ? 'Automatic' : String(settings.Defaults.Threads)} disabled={disabled || !draft.Threads.override} onChange={(event) => onChange({ ...draft, Threads: { ...draft.Threads, value: event.target.value } })} error={Boolean(issue('Runtime.Threads'))} helperText={issue('Runtime.Threads') ?? '1–64 threads.'} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 10 } }} sx={{ maxWidth: 360, opacity: draft.Threads.override ? 1 : 0.45 }} />
    </SettingsGroup>

    {(['H264', 'HEVC'] as const).map((codec) => {
      const label = codec === 'H264' ? 'H.264' : 'HEVC';
      const quality = draft[codec];
      const value = quality.override ? quality.value : { ...settings.Defaults[codec], CRF: String(settings.Defaults[codec].CRF) };
      const change = (patch: Partial<typeof quality.value>) => onChange({ ...draft, [codec]: { ...quality, value: { ...quality.value, ...patch } } });
      return <SettingsGroup key={codec} title={label + ' CPU encoding'} description={'Applies to new ' + label + ' software encoding jobs, including CPU fallback. Hardware encoding uses its own profile.'} source={settings.Sources[codec]} currentValue={qualityLabel(settings.Effective[codec])} defaultValue={qualityLabel(settings.Defaults[codec])} stale={stale}>
        <OverrideMode name={label + ' CPU encoding'} override={quality.override} disabled={disabled} onChange={(override) => onChange({ ...draft, [codec]: { ...quality, override } })} />
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(3, minmax(0, 1fr))' }, gap: 2, opacity: quality.override ? 1 : 0.45 }}>
          <TextField select fullWidth label={label + ' CPU preset'} value={value.Preset} disabled={disabled || !quality.override} onChange={(event) => change({ Preset: event.target.value as RuntimeQuality['Preset'] })} error={Boolean(issue('Runtime.' + codec + '.Preset'))} helperText={issue('Runtime.' + codec + '.Preset') ?? 'Slower uses more CPU for better compression.'}>{cpuPresets.map((preset) => <MenuItem key={preset} value={preset}>{preset}</MenuItem>)}</TextField>
          <TextField select fullWidth label={label + ' rate control'} value={value.RateControl} disabled={disabled || !quality.override} onChange={(event) => change({ RateControl: event.target.value as RuntimeQuality['RateControl'] })} error={Boolean(issue('Runtime.' + codec + '.RateControl'))} helperText={issue('Runtime.' + codec + '.RateControl')}><MenuItem value="bitrate">Bitrate target</MenuItem><MenuItem value="capped_crf">Capped CRF</MenuItem></TextField>
          <TextField fullWidth label={label + ' CRF'} value={value.CRF} disabled={disabled || !quality.override || value.RateControl !== 'capped_crf'} onChange={(event) => change({ CRF: event.target.value })} error={Boolean(issue('Runtime.' + codec + '.CRF'))} helperText={issue('Runtime.' + codec + '.CRF') ?? '18–35. Lower means higher quality. Only used for capped CRF.'} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 10 } }} sx={{ opacity: quality.override && value.RateControl !== 'capped_crf' ? 0.45 : 1 }} />
        </Box>
        {issue('Runtime.' + codec) && <Alert severity="error">{issue('Runtime.' + codec)}</Alert>}
      </SettingsGroup>;
    })}

    <SettingsGroup title="HDR tone mapping" description="Allow software and Vulkan filters to convert HDR video for SDR output. The chosen filter must be available on this server.">
      {(['SoftwareToneMapping', 'VulkanToneMapping'] as const).map((field) => {
        const label = field === 'SoftwareToneMapping' ? 'Software tone mapping' : 'Vulkan tone mapping';
        const value = draft[field].override ? draft[field].value : settings.Defaults[field];
        return <Box key={field} component="section" aria-label={label} sx={{ pb: 1.5, '& + section': { pt: 2, borderTop: '1px solid #E3E8F1' } }}>
          <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1.25 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>{label}</Typography><SettingsSource source={settings.Sources[field]} /><Typography variant="caption" color="text.secondary">{stale ? 'Last confirmed' : 'Current'}: {settings.Effective[field] ? 'Enabled' : 'Disabled'}</Typography></Stack>
          <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 2 }}>
            <OverrideMode name={label} override={draft[field].override} disabled={disabled} onChange={(override) => onChange({ ...draft, [field]: { ...draft[field], override } })} />
            <FormControlLabel sx={{ opacity: draft[field].override ? 1 : 0.45, m: 0 }} control={<Switch checked={value} disabled={disabled || !draft[field].override} onChange={(event) => onChange({ ...draft, [field]: { ...draft[field], value: event.target.checked } })} />} label={'Enable ' + label.toLowerCase()} />
          </Stack>
          {issue('Runtime.' + field) && <Alert severity="error" sx={{ mt: 1 }}>{issue('Runtime.' + field)}</Alert>}
        </Box>;
      })}
    </SettingsGroup>
    {issue('Runtime') && <Alert severity="error">{issue('Runtime')}</Alert>}
  </>;
}
