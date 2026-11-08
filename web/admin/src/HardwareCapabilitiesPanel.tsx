import { Fragment, useCallback, useEffect, useRef, useState } from 'react';
import { errorText } from './components';
import { useTranslation } from './i18n';
import { RefreshRounded } from './icons';
import { SettingsGroup } from './SettingsLayout';
import { hardwareCapabilitiesApi } from './hardwareCapabilitiesApi';
import type { CodecEncodingProof, CodecInterface, HardwareCapabilities } from './hardwareCapabilitiesApi';
import './hardware-capabilities.css';

function CodecStatus({ value }: { value: CodecInterface }) {
  const { t } = useTranslation();
  const label = !value.Managed ? 'Not offered' : value.Compiled === null ? 'Unknown' : value.Compiled ? 'Interface present' : 'Not compiled';
  return <span className={'hardware-codec-status' + (!value.Managed || value.Compiled === null ? ' hardware-codec-unknown' : value.Compiled ? ' hardware-codec-present' : ' hardware-codec-absent')}
    title={value.Implementations.length ? `${t('Implementations')}: ${value.Implementations.join(', ')}` : undefined}>
    {value.Selected && <i role="img" aria-label={t('Current saved mode')} title={t('Current saved mode')} />}
    <span>{t(label)}</span>
    {value.Implementations.length > 0 && <span className="visually-hidden">{t('Implementations')}: <span translate="no">{value.Implementations.join(', ')}</span></span>}
  </span>;
}

function EncodingProof({ proof, deviceName }: { proof: CodecEncodingProof; deviceName: string }) {
  const { t, locale } = useTranslation();
  return <li>
    <div><strong>{t(proof.State === 'passed' ? 'Passed for this sample' : 'Rejected for this sample')}</strong><span translate="no">{deviceName}</span></div>
    <code>{proof.Profile} · {proof.BitDepth}-bit · {proof.Width} × {proof.Height}{proof.FrameRate > 0 ? ` · ${proof.FrameRate} fps` : ''}{proof.Bitrate > 0 ? ` · ${(proof.Bitrate / 1_000_000).toLocaleString(locale, { maximumFractionDigits: 2 })} Mbps` : ''}</code>
    <small>{t('Valid until')} <time dateTime={proof.ExpiresAt} translate="no">{new Date(proof.ExpiresAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}</time>{proof.Code && <> · <span translate="no">{proof.Code}</span></>}</small>
  </li>;
}

export function HardwareCapabilitiesPanel({ savedSelectionKey, disabled = false }: { savedSelectionKey: string; disabled?: boolean }) {
  const { t, locale } = useTranslation();
  const [snapshot, setSnapshot] = useState<{ data?: HardwareCapabilities; error?: unknown; loading: boolean }>({ loading: true });
  const [clock, setClock] = useState(Date.now);
  const sequence = useRef(0);
  const pending = useRef<AbortController | null>(null);
  const mounted = useRef(false);

  const load = useCallback(async (refresh = false) => {
    const request = ++sequence.current;
    pending.current?.abort();
    const controller = new AbortController();
    pending.current = controller;
    let timedOut = false;
    const deadline = setTimeout(() => { timedOut = true; controller.abort(); }, 15_000);
    setSnapshot((current) => ({ ...current, loading: true, error: undefined }));
    try {
      const data = await (refresh ? hardwareCapabilitiesApi.refresh : hardwareCapabilitiesApi.get)({ signal: controller.signal });
      if (mounted.current && request === sequence.current && !controller.signal.aborted) setSnapshot({ data, loading: false });
    } catch (error) {
      if (mounted.current && request === sequence.current && (!controller.signal.aborted || timedOut)) {
        setSnapshot((current) => ({ ...current, loading: false, error: timedOut ? new Error('Codec capability information did not respond within 15 seconds. Try refreshing again.') : error }));
      }
    } finally {
      clearTimeout(deadline);
      if (pending.current === controller) pending.current = null;
      if (mounted.current && request === sequence.current) setSnapshot((current) => ({ ...current, loading: false }));
    }
  }, []);

  useEffect(() => {
    mounted.current = true;
    const timer = setInterval(() => { if (!document.hidden) setClock(Date.now()); }, 5000);
    return () => { mounted.current = false; ++sequence.current; pending.current?.abort(); clearInterval(timer); };
  }, []);

  useEffect(() => {
    setSnapshot({ loading: true });
    void load();
    return () => { ++sequence.current; pending.current?.abort(); };
  }, [savedSelectionKey, load]);

  const data = snapshot.data;
  const selected = data?.Devices.find((device) => device.DeviceId === data.Selected.DeviceId);
  return <SettingsGroup title="Codec capabilities" description="Compiled interfaces, current device availability, and successful sample execution describe different things. A sample result covers only its recorded codec, profile, bit depth, dimensions, and device.">
    <div className="hardware-capabilities" aria-busy={snapshot.loading}>
      <div className="hardware-capabilities-toolbar">
        <p>{t('Read available codec interfaces without opening media or starting a GPU test.')}</p>
        <button type="button" className="hardware-capabilities-refresh" disabled={disabled || snapshot.loading} onClick={() => void load(true)}><RefreshRounded aria-hidden="true" sx={{ fontSize: 15 }} /><span>{t(snapshot.loading ? 'Reading capabilities…' : 'Refresh codec interfaces')}</span></button>
      </div>
      {snapshot.error != null && <div className="hardware-capabilities-notice" role="alert"><span>{t(data ? 'Showing the last successful capability observation.' : 'Codec capability information is unavailable.')}</span><span>{t(errorText(snapshot.error))}</span><button type="button" disabled={snapshot.loading} onClick={() => void load()}>{t('Read latest observation')}</button></div>}
      {snapshot.loading && !data && <div className="hardware-capabilities-loading" role="status">{t('Loading codec capabilities…')}</div>}
      {data && <>
        <div className="hardware-capabilities-summary">
          <span>{t(data.TranscodingAvailable ? 'Transcoding service available' : 'Transcoding service unavailable')}</span>
          {data.Selected.DeviceId && <span><span translate="no">{selected?.Label || data.Selected.DeviceId}</span> · {t(selected?.Available ? 'Device available' : 'Device unavailable')}</span>}
          {data.FFmpegVersion && <span translate="no">FFmpeg {data.FFmpegVersion}</span>}
        </div>
        {data.CompiledState !== 'current' && <p className="hardware-capabilities-note">{t(data.CompiledState === 'stale' ? 'The codec interface observation expired or the configured tool changed. Refresh to read current information.' : 'Codec interfaces have not been read in this server instance. Refresh to check them.')}</p>}
        <div className="hardware-capabilities-table-wrap">
          <table className="hardware-capabilities-table" aria-label={t('Codec interface matrix')}>
            <thead><tr><th rowSpan={2} scope="col">{t('Codec')}</th><th colSpan={2} scope="colgroup">CPU</th><th colSpan={2} scope="colgroup">AMD VA-API</th></tr><tr><th scope="col">{t('Decode')}</th><th scope="col">{t('Encode')}</th><th scope="col">{t('Decode')}</th><th scope="col">{t('Encode')}</th></tr></thead>
            <tbody>{data.Items.map((item) => {
              const proofs = item.EncodingProofs.filter((proof) => Date.parse(proof.ExpiresAt) > clock);
              return <Fragment key={item.Codec}><tr><th scope="row"><span translate="no">{item.Name}</span></th><td><CodecStatus value={item.SoftwareDecode} /></td><td><CodecStatus value={item.SoftwareEncode} /></td><td><CodecStatus value={item.HardwareDecode} /></td><td><CodecStatus value={item.HardwareEncode} /></td></tr>
                {proofs.length > 0 && <tr className="hardware-capabilities-proof-row"><td colSpan={5}><details><summary>{t('Observed encoding samples')} <span translate="no">{proofs.length.toLocaleString(locale)}</span></summary><p>{t('A successful sample does not establish support for other sources, profiles, filters, or dimensions.')}</p><ul>{proofs.map((proof, index) => <EncodingProof key={`${proof.DeviceId}-${proof.Profile}-${proof.BitDepth}-${proof.Width}-${proof.Height}-${index}`} proof={proof} deviceName={data.Devices.find((device) => device.DeviceId === proof.DeviceId)?.Label || proof.DeviceId} />)}</ul></details></td></tr>}
              </Fragment>;
            })}</tbody>
          </table>
        </div>
        <p className="hardware-capabilities-note">{t('Interface present means FFmpeg includes an interface. Device and source compatibility still require actual execution. Dots mark the current saved mode.')}</p>
        <p className="hardware-capabilities-sampled">{t(snapshot.error ? 'Last successful sample' : 'Last sampled')} <time dateTime={data.ObservedAt} translate="no">{new Date(data.ObservedAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}</time>{data.CompiledAt && <> · {t('Codec interfaces read')} <time dateTime={data.CompiledAt} translate="no">{new Date(data.CompiledAt).toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })}</time></>}</p>
      </>}
    </div>
  </SettingsGroup>;
}
