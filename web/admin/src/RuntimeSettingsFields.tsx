import type { ReactNode } from 'react';
import { Alert, Box, Chip, FormControlLabel, MenuItem, Paper, Stack, Switch, TextField, Typography } from '@mui/material';
import { fieldError } from './formFields';
import { cpuPresets } from './runtimeSettings';
import type { RuntimeHardware, RuntimeQuality, RuntimeSettings } from './runtimeSettings';
import type { RuntimeDraft } from './runtimeSettingsDraft';

const sourceLabel = (value: string) => value === 'database' ? 'Database override' : 'Deployment default';
const modeLabel = (value: string) => value === 'software' ? 'CPU' : value === 'vaapi' ? 'AMD VA-API' : `${value || 'Unconfigured'} (deployment)`;
const networkLabel = (host: string, port: number) => `${host || 'All interfaces'} · ${port === 0 ? 'Automatically assigned port' : `Port ${port}`}`;
const hardwareLabel = (value: RuntimeHardware) => `Decode: ${modeLabel(value.Decode)} · Encode: ${modeLabel(value.Encode)}`;
const qualityLabel = (value: RuntimeQuality) => `${value.Preset} · ${value.RateControl === 'bitrate' ? 'Bitrate target' : `Capped CRF ${value.CRF}`}`;

interface Props { settings: RuntimeSettings; draft: RuntimeDraft; disabled: boolean; stale: boolean; errors: Record<string, string | undefined>; mutationError: unknown; onChange: (value: RuntimeDraft) => void }

function Group({ name, source, effective, defaultValue, stale, children }: { name: string; source: string; effective: string; defaultValue: string; stale: boolean; children: ReactNode }) {
  return <Paper component="section" aria-label={name} variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}><Stack spacing={2}>
    <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography component="h2" variant="h3">{name}</Typography><Chip size="small" variant="outlined" label={sourceLabel(source)} /></Stack>
    <Typography variant="body2">{stale ? 'Last confirmed value' : 'Current value'}: {effective}</Typography>
    <Typography variant="caption" color="text.secondary">Deployment default: {defaultValue}</Typography>
    {children}
  </Stack></Paper>;
}

export function RuntimeSettingsFields({ settings, draft, disabled, stale, errors, mutationError, onChange }: Props) {
  const issue = (field: string) => errors[field] ?? fieldError(mutationError, field);
  const toggle = (name: string, checked: boolean, onToggle: (value: boolean) => void) => <FormControlLabel control={<Switch checked={!checked} disabled={disabled} onChange={(event) => onToggle(!event.target.checked)} />} label={`Use deployment default for ${name}`} />;
  const network = draft.Network;
  const hardware = draft.Hardware.override ? draft.Hardware.value : settings.Defaults.Hardware;
  const knownDevice = settings.Hardware.Devices.find((device) => device.DeviceId === hardware.DeviceId);
  const hardwareDisabled = disabled || !draft.Hardware.override;
  return <Stack spacing={3}>
    <Paper component="section" aria-label="HTTP listener" variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}><Stack spacing={2}>
      <Typography component="h2" variant="h3">HTTP listener</Typography>
      <Typography variant="body2">Saving changes the desired listener. Restart the server service to apply it. The active listener continues serving this session until then.</Typography>
      <Box component="dl" sx={{ m: 0, display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, gap: 2 }}>
        <Box><Typography component="dt" variant="caption" color="text.secondary">{stale ? 'Last confirmed desired listener' : 'Desired listener'}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, overflowWrap: 'anywhere' }}>{networkLabel(settings.Network.Desired.BindHost, settings.Network.Desired.HttpPort)}</Typography></Box>
        <Box><Typography component="dt" variant="caption" color="text.secondary">Active listener</Typography><Typography component="dd" variant="body2" sx={{ m: 0, overflowWrap: 'anywhere' }}>{settings.Network.Active ? networkLabel(settings.Network.Active.BoundHost, settings.Network.Active.HttpPort) : 'No active listener is reported.'}</Typography></Box>
      </Box>
      {settings.Network.RestartRequired && <Alert severity="warning">The saved listener requires a service restart. Reconnect after the restart{settings.Network.ReconnectURL ? `: ${settings.Network.ReconnectURL}` : ' using the configured server address.'}</Alert>}
      {!settings.Network.RestartRequired && settings.Network.Active && <Typography variant="caption" color="text.secondary">The desired listener matches the active configuration.</Typography>}
      {(['BindHost', 'HttpPort'] as const).map((field) => {
        const name = field === 'BindHost' ? 'bind address' : 'HTTP port';
        const label = field === 'BindHost' ? 'Bind address' : 'HTTP port';
        const value = network[field];
        const change = (patch: Partial<typeof value>) => onChange({ ...draft, Network: { ...network, [field]: { ...value, ...patch } } });
        return <Box key={field} component="section" aria-label={label}><Stack spacing={1.5}>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>{toggle(name, value.override, (override) => change({ override }))}<Chip size="small" variant="outlined" label={sourceLabel(settings.Sources.Network[field])} /></Stack>
          <TextField fullWidth label={label} value={value.override ? value.value : String(settings.Defaults.Network[field])} disabled={disabled || !value.override} onChange={(event) => change({ value: event.target.value })} error={Boolean(issue(`Runtime.Network.${field}`))} helperText={issue(`Runtime.Network.${field}`) ?? (field === 'BindHost' ? 'Empty means all interfaces. Otherwise use a canonical literal IPv4 or IPv6 address without a port or zone.' : 'Use a port from 1 to 65535. A deployment value of 0 lets the operating system assign a port.')} slotProps={{ htmlInput: { spellCheck: false, autoComplete: 'off', inputMode: field === 'HttpPort' ? 'numeric' : 'text', maxLength: 128 } }} />
          <Typography variant="caption" color="text.secondary">Deployment default: {field === 'BindHost' ? settings.Defaults.Network.BindHost || 'All interfaces' : settings.Defaults.Network.HttpPort || 'Automatically assigned port'}</Typography>
        </Stack></Box>;
      })}
      {issue('Runtime.Network') && <Alert severity="error">{issue('Runtime.Network')}</Alert>}
    </Stack></Paper>

    <Group name="Hardware acceleration" source={settings.Sources.Hardware} effective={hardwareLabel(settings.Effective.Hardware)} defaultValue={hardwareLabel(settings.Defaults.Hardware)} stale={stale}>
      <Typography variant="body2" color="text.secondary">Hardware choices apply to new playback plans and jobs. Existing jobs keep their admitted configuration.</Typography>
      {!settings.Hardware.Available && <Alert severity="warning">The current hardware selection is unavailable. {settings.Hardware.Code && `Status: ${settings.Hardware.Code}.`} Review the device. For CPU processing without GPU filters, choose CPU for both axes and clear the AMD device.</Alert>}
      {toggle('hardware acceleration', draft.Hardware.override, (override) => onChange({ ...draft, Hardware: { ...draft.Hardware, override } }))}
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
        {(['Decode', 'Encode'] as const).map((field) => <TextField key={field} fullWidth select label={field === 'Decode' ? 'Video decoding' : 'Video encoding'} value={hardware[field]} disabled={hardwareDisabled} onChange={(event) => onChange({ ...draft, Hardware: { ...draft.Hardware, value: { ...draft.Hardware.value, [field]: event.target.value } } })} error={Boolean(issue(`Runtime.Hardware.${field}`))} helperText={issue(`Runtime.Hardware.${field}`)}><MenuItem value="software">CPU</MenuItem><MenuItem value="vaapi">AMD VA-API</MenuItem>{!['software', 'vaapi'].includes(hardware[field]) && <MenuItem value={hardware[field]} disabled>{modeLabel(hardware[field])}</MenuItem>}</TextField>)}
      </Stack>
      <TextField select fullWidth label="AMD device" value={hardware.DeviceId} disabled={hardwareDisabled} onChange={(event) => onChange({ ...draft, Hardware: { ...draft.Hardware, value: { ...draft.Hardware.value, DeviceId: event.target.value } } })} error={Boolean(issue('Runtime.Hardware.DeviceId'))} helperText={issue('Runtime.Hardware.DeviceId') ?? 'Required for AMD VA-API. CPU decoding and encoding may also use this device for Vulkan filters. Device paths are managed by the deployment.'}>
        <MenuItem value="">No device selected</MenuItem>
        {settings.Hardware.Devices.map((device) => <MenuItem key={device.DeviceId} value={device.DeviceId} disabled={!device.Available}>{device.Label || device.DeviceId}{device.Available ? '' : ' (unavailable)'}</MenuItem>)}
        {hardware.DeviceId && !knownDevice && <MenuItem value={hardware.DeviceId} disabled>Saved device is unavailable ({hardware.DeviceId})</MenuItem>}
      </TextField>
      {issue('Runtime.Hardware') && <Alert severity="error">{issue('Runtime.Hardware')}</Alert>}
    </Group>

    <Group name="Threads per job" source={settings.Sources.Threads} effective={settings.Effective.Threads === 0 ? 'Automatic' : String(settings.Effective.Threads)} defaultValue={settings.Defaults.Threads === 0 ? 'Automatic' : String(settings.Defaults.Threads)} stale={stale}>
      {toggle('threads per job', draft.Threads.override, (override) => onChange({ ...draft, Threads: { ...draft.Threads, override } }))}
      <TextField label="Threads per job" value={draft.Threads.override ? draft.Threads.value : String(settings.Defaults.Threads)} disabled={disabled || !draft.Threads.override} onChange={(event) => onChange({ ...draft, Threads: { ...draft.Threads, value: event.target.value } })} error={Boolean(issue('Runtime.Threads'))} helperText={issue('Runtime.Threads') ?? 'Use 1 to 64 threads. The change applies to newly admitted jobs.'} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 10 } }} />
    </Group>

    {(['H264', 'HEVC'] as const).map((codec) => {
      const label = codec === 'H264' ? 'H.264' : 'HEVC';
      const quality = draft[codec];
      const value = quality.override ? quality.value : { ...settings.Defaults[codec], CRF: String(settings.Defaults[codec].CRF) };
      const change = (patch: Partial<typeof quality.value>) => onChange({ ...draft, [codec]: { ...quality, value: { ...quality.value, ...patch } } });
      return <Group key={codec} name={`${label} CPU encoding`} source={settings.Sources[codec]} effective={qualityLabel(settings.Effective[codec])} defaultValue={qualityLabel(settings.Defaults[codec])} stale={stale}>
        <Typography variant="body2" color="text.secondary">Applies when a new job uses the {label} software encoder. Hardware encoding retains its own profile.</Typography>
        {toggle(`${label} CPU encoding`, quality.override, (override) => onChange({ ...draft, [codec]: { ...quality, override } }))}
        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
          <TextField select fullWidth label={`${label} CPU preset`} value={value.Preset} disabled={disabled || !quality.override} onChange={(event) => change({ Preset: event.target.value as RuntimeQuality['Preset'] })} error={Boolean(issue(`Runtime.${codec}.Preset`))} helperText={issue(`Runtime.${codec}.Preset`) ?? 'Slower presets spend more CPU time improving compression.'}>{cpuPresets.map((preset) => <MenuItem key={preset} value={preset}>{preset}</MenuItem>)}</TextField>
          <TextField select fullWidth label={`${label} rate control`} value={value.RateControl} disabled={disabled || !quality.override} onChange={(event) => change({ RateControl: event.target.value as RuntimeQuality['RateControl'] })} error={Boolean(issue(`Runtime.${codec}.RateControl`))} helperText={issue(`Runtime.${codec}.RateControl`)}><MenuItem value="bitrate">Bitrate target</MenuItem><MenuItem value="capped_crf">Capped CRF</MenuItem></TextField>
          <TextField fullWidth label={`${label} CRF`} value={value.CRF} disabled={disabled || !quality.override || value.RateControl !== 'capped_crf'} onChange={(event) => change({ CRF: event.target.value })} error={Boolean(issue(`Runtime.${codec}.CRF`))} helperText={issue(`Runtime.${codec}.CRF`) ?? '18–35. Lower means higher quality. Used by capped CRF mode.'} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 10 } }} />
        </Stack>
        {issue(`Runtime.${codec}`) && <Alert severity="error">{issue(`Runtime.${codec}`)}</Alert>}
      </Group>;
    })}

    {(['SoftwareToneMapping', 'VulkanToneMapping'] as const).map((field) => {
      const label = field === 'SoftwareToneMapping' ? 'Software tone mapping' : 'Vulkan tone mapping';
      const value = draft[field].override ? draft[field].value : settings.Defaults[field];
      return <Group key={field} name={label} source={settings.Sources[field]} effective={settings.Effective[field] ? 'Enabled' : 'Disabled'} defaultValue={settings.Defaults[field] ? 'Enabled' : 'Disabled'} stale={stale}>
        <Typography variant="body2" color="text.secondary">Applies to new HDR conversion jobs that use the {field === 'SoftwareToneMapping' ? 'software' : 'Vulkan'} filter path. The selected path must be available on this server.</Typography>
        {toggle(label.toLowerCase(), draft[field].override, (override) => onChange({ ...draft, [field]: { ...draft[field], override } }))}
        <FormControlLabel control={<Switch checked={value} disabled={disabled || !draft[field].override} onChange={(event) => onChange({ ...draft, [field]: { ...draft[field], value: event.target.checked } })} />} label={`Enable ${label.toLowerCase()}`} />
        {issue(`Runtime.${field}`) && <Alert severity="error">{issue(`Runtime.${field}`)}</Alert>}
      </Group>;
    })}
    {issue('Runtime') && <Alert severity="error">{issue('Runtime')}</Alert>}
  </Stack>;
}
