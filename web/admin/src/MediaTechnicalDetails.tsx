import { useEffect, useState } from 'react';
import { Alert, Button, Skeleton } from './ui';
import { ErrorNotice } from './components';
import { useTranslation } from './i18n';
import { mediaBitrate, mediaDuration, mediaSize, mediaSummaryApi } from './mediaSummaryApi';
import type { IndexedMediaAnalysis, MediaInfoResponse, MediaStreamInfo, MediaTechnicalSummary } from './mediaSummaryApi';
import './media-summary.css';

const analysisFields: { field: keyof Omit<IndexedMediaAnalysis, 'PreviewCount'>; label: string }[] = [
  { field: 'Intro', label: 'Intro' }, { field: 'Credits', label: 'Credits' },
  { field: 'BackgroundPreview', label: 'Background clips' }, { field: 'AudioWaveform', label: 'Audio waveforms' },
  { field: 'SubtitleTimeline', label: 'Subtitle timelines' },
];
const statusLabels: Record<string, string> = {
  queued: 'Queued', pending: 'Pending', running: 'Running', ready: 'Ready', failed: 'Failed', cancelled: 'Cancelled',
  qualified: 'Qualified', review: 'Review required', no_result: 'No result', rejected: 'Rejected', stale: 'Stale',
  unavailable: 'Unavailable', disabled: 'Disabled', unverified: 'Unverified', manual: 'Manual',
};

export function streamDescription(stream: MediaStreamInfo | null | undefined): string {
  if (!stream) return '';
  return [stream.Codec.toUpperCase(), stream.Width && stream.Height ? `${stream.Width} × ${stream.Height}` : '',
    stream.Channels ? stream.ChannelLayout || `${stream.Channels} ch` : ''].filter(Boolean).join(' · ');
}

function useMediaInfo(itemId: string) {
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{ itemId: string; data?: MediaInfoResponse; error?: unknown; loading: boolean }>({ itemId, loading: true });
  useEffect(() => {
    const controller = new AbortController();
    let disposed = false, expired = false;
    setState((current) => ({ itemId, data: current.itemId === itemId ? current.data : undefined, loading: true }));
    let deadline: ReturnType<typeof setTimeout> | undefined;
    const timeout = new Promise<never>((_resolve, reject) => {
      deadline = setTimeout(() => { expired = true; controller.abort(); reject(new Error('Media details did not respond within 15 seconds. Refresh to try again.')); }, 15_000);
    });
    void Promise.race([mediaSummaryApi.info(itemId, { signal: controller.signal }), timeout])
      .then((data) => { if (!disposed) setState({ itemId, data, loading: false }); })
      .catch((error: unknown) => { if (!disposed) setState((current) => ({ itemId, data: current.itemId === itemId ? current.data : undefined,
        error: expired ? new Error('Media details did not respond within 15 seconds. Refresh to try again.') : error, loading: false })); })
      .finally(() => clearTimeout(deadline));
    return () => { disposed = true; controller.abort(); clearTimeout(deadline); };
  }, [itemId, revision]);
  return { data: state.itemId === itemId ? state.data : undefined, error: state.itemId === itemId ? state.error : undefined,
    loading: state.itemId !== itemId || state.loading, reload: () => setRevision((value) => value + 1) };
}

export function MediaCatalogTechnical({ summary, onOpen, compact = false, disabled = false }: { summary?: MediaTechnicalSummary; onOpen?: () => void; compact?: boolean; disabled?: boolean }) {
  const { t, locale } = useTranslation();
  const description = summary?.State === 'available' ? streamDescription(summary.Video ?? summary.Audio) : '';
  const state = !summary ? 'Unknown' : summary.State === 'not_applicable' ? 'Folder' : summary.State === 'not_scanned' ? 'Not scanned' : 'Unknown';
  const extra = summary && summary.State !== 'not_applicable' ? [summary.Video && streamDescription(summary.Audio), mediaSize(summary.Size, locale), mediaBitrate(summary.Bitrate, locale)].filter(Boolean).join(' · ') : '';
  const contents = <><span translate={description ? 'no' : undefined}>{description || t(state)}</span>{extra && <span className="media-secondary-line media-mono" translate="no">{extra}</span>}{compact && summary?.DurationTicks != null && <span className="media-summary-compact-duration media-mono" translate="no">{mediaDuration(summary.DurationTicks)}</span>}</>;
  return onOpen ? <button type="button" className={`media-summary-details-link${compact ? ' media-summary-compact' : ''}`} disabled={disabled} onClick={onOpen} aria-label={t('View media details')} title={[description || t(state), extra].filter(Boolean).join('\n')}>{contents}</button>
    : <span className={`media-summary-technical${compact ? ' media-summary-compact' : ''}`}>{contents}</span>;
}

export function MediaCatalogImages({ summary, onOpen, disabled = false }: { summary?: MediaTechnicalSummary; onOpen: () => void; disabled?: boolean }) {
  const { t, locale } = useTranslation();
  const labels: Record<string, string> = { Primary: 'Poster', Backdrop: 'Backdrop', Logo: 'Logo' };
  return <button type="button" className="media-summary-images" disabled={disabled} onClick={onOpen} aria-label={t('Manage indexed images')}>
    <span className="media-mono">{summary ? new Intl.NumberFormat(locale).format(summary.Images.Count) : '—'}</span>
    <span className="media-summary-image-types">{(['Primary', 'Backdrop', 'Logo'] as const).map((type) => <span key={type} title={t(labels[type])} className={summary?.Images.Types.includes(type) ? 'media-summary-image-present' : ''} aria-label={`${t(labels[type])}: ${t(!summary ? 'Unknown' : summary.Images.Types.includes(type) ? 'Present' : 'Not indexed')}`}><i aria-hidden="true" />{t({ Primary: 'P', Backdrop: 'B', Logo: 'L' }[type])}</span>)}</span>
  </button>;
}

export function MediaCatalogAnalysis({ summary, onOpen, disabled = false }: { summary?: MediaTechnicalSummary; onOpen: () => void; disabled?: boolean }) {
  const { t } = useTranslation();
  const statuses = summary ? analysisFields.map(({ field }) => summary.Analysis[field]).filter((value): value is string => value !== null) : [];
  const recorded = statuses.length > 0 || Boolean(summary?.Analysis.PreviewCount);
  const label = !summary ? 'Unknown' : !recorded ? 'No records' : statuses.some((status) => status === 'failed') ? 'Failed'
    : statuses.some((status) => ['queued', 'pending', 'running'].includes(status)) ? 'In progress' : 'Recorded';
  const tone = label === 'Failed' ? 'warning' : label === 'In progress' ? 'active' : recorded ? 'recorded' : '';
  return <button type="button" className={`media-summary-analysis ${tone}`} disabled={disabled} onClick={onOpen} aria-label={t('View indexed analysis')}><i aria-hidden="true" />{t(label)}</button>;
}

function MediaReadFeedback({ resource }: { resource: ReturnType<typeof useMediaInfo> }) {
  const { t } = useTranslation();
  return <>
    {resource.error != null && <ErrorNotice error={resource.error} retry={resource.reload} />}
    {resource.data && resource.error != null && <p className="editor-note" role="status">{t('Showing the last successful media details update.')}</p>}
    {resource.loading && !resource.data && <Skeleton height={180} aria-label="Loading media details" />}
  </>;
}

function IndexedAnalysisDetails({ summary }: { summary: MediaTechnicalSummary }) {
  const { t, locale } = useTranslation();
  return <section className="editor-detail-section"><h3>{t('Indexed analysis')}</h3>
    <dl className="editor-detail-list">{analysisFields.map(({ field, label }) => <div key={field}><dt>{t(label)}</dt><dd>{summary.Analysis[field] === null ? t('No records') : t(statusLabels[summary.Analysis[field]!] ?? summary.Analysis[field]!.replaceAll('_', ' '))}</dd></div>)}
      <div><dt>{t('Seek preview records')}</dt><dd>{new Intl.NumberFormat(locale).format(summary.Analysis.PreviewCount)}</dd></div>
    </dl><p className="editor-note">{t('Recorded analysis may need refreshing after the source file changes.')}</p>
  </section>;
}

function StreamDetails({ stream }: { stream: MediaStreamInfo }) {
  const { t, locale } = useTranslation();
  const values: [string, string | number | null][] = [
    ['Codec', stream.Codec || null], ['Codec profile', stream.Profile || null], ['Level', stream.Level],
    ['Language', stream.Language || null], ['Resolution', stream.Width && stream.Height ? `${stream.Width} × ${stream.Height}` : null],
    ['Bitrate', mediaBitrate(stream.Bitrate, locale)], ['Frame rate', stream.FrameRate || null],
    ['Bit depth', stream.BitDepth], ['Pixel format', stream.PixelFormat || null],
    ['Channels', stream.Channels], ['Channel layout', stream.ChannelLayout || null],
    ['Sample rate', stream.SampleRate === null ? null : `${new Intl.NumberFormat(locale).format(stream.SampleRate)} Hz`],
    ['Video range', stream.VideoRange || null], ['Color space', stream.ColorSpace || null],
    ['Color transfer', stream.ColorTransfer || null], ['Color primaries', stream.ColorPrimaries || null],
  ];
  const flags = [[stream.IsDefault, 'Default'], [stream.IsForced, 'Forced'], [stream.IsExternal, 'External'], [stream.IsAttachedPicture, 'Embedded cover'], [stream.IsHearingImpaired, 'Hearing impaired']] as const;
  return <details className="media-stream-details">
    <summary><span className="media-mono">#{stream.Index}</span><span>{t(({ video: 'Video', audio: 'Audio', subtitle: 'Subtitle', attachment: 'Attachment', data: 'Data' } as Record<string, string>)[stream.Type] ?? stream.Type)}</span><strong className="media-mono" translate="no">{stream.Codec || t('Unknown')}</strong>{stream.Title && <span className="media-stream-title" translate="no">{stream.Title}</span>}</summary>
    <div><div className="media-stream-flags">{flags.filter(([enabled]) => enabled).map(([, label]) => <span key={label} className="editor-badge">{t(label)}</span>)}</div>
      <dl className="media-stream-facts">{values.filter(([, value]) => value !== null).map(([label, value]) => <div key={label}><dt>{t(label)}</dt><dd className="media-mono" translate="no">{value}</dd></div>)}</dl>
    </div>
  </details>;
}

export function MediaTechnicalDetails({ itemId, onProcessing, processingDisabled }: { itemId: string; onProcessing?: () => void; processingDisabled: boolean }) {
  const { t, locale } = useTranslation();
  const resource = useMediaInfo(itemId);
  const data = resource.data;
  return <div className="media-technical-details" aria-busy={resource.loading}>
    <MediaReadFeedback resource={resource} />
    {data && <>
      {data.Summary.State === 'not_applicable' ? <p className="editor-note">{t('Folders do not have a media source.')}</p> : <>
          {data.Summary.State === 'not_scanned' && <Alert severity="info">Media details have not been scanned. Scan this library to read the source information.</Alert>}
          <section className="editor-detail-section"><h3>{t('Media source')}</h3><dl className="editor-detail-list">
            <div><dt>{t('Container')}</dt><dd className="media-mono" translate="no">{data.Summary.Container || t('Unknown')}</dd></div>
            <div><dt>{t('Duration')}</dt><dd className="media-mono" translate="no">{mediaDuration(data.Summary.DurationTicks) ?? t('Unknown')}</dd></div>
            <div><dt>{t('File size')}</dt><dd className="media-mono" translate="no">{mediaSize(data.Summary.Size, locale) ?? t('Unknown')}</dd></div>
            <div><dt>{t('Bitrate')}</dt><dd className="media-mono" translate="no">{mediaBitrate(data.Summary.Bitrate, locale) ?? t('Unknown')}</dd></div>
            <div><dt>{t('Last catalog update')}</dt><dd>{new Date(data.UpdatedAt).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' })}</dd></div>
          </dl><p className="editor-note">{t('Media details are from the last scan. Refresh this library after changing files.')}</p></section>
          {data.Summary.State === 'available' && <section className="editor-detail-section"><h3>{t('Media streams')} <span className="editor-inline-note">{new Intl.NumberFormat(locale).format(data.StreamCount)}</span></h3>
            {data.Streams.length === 0 && <p className="editor-note">{t('No streams are recorded for this source.')}</p>}
            <div className="media-stream-list">{data.Streams.map((entry) => <StreamDetails key={entry.Index} stream={entry} />)}</div>
            {data.StreamsTruncated && <Alert severity="info">{t('Showing the first {count} recorded streams.', { count: data.Streams.length })}</Alert>}
            {onProcessing && <div className="editor-inline-actions"><Button type="button" variant="outlined" disabled={processingDisabled} onClick={onProcessing}>Media processing</Button></div>}
          </section>}
        </>}
      <IndexedAnalysisDetails summary={data.Summary} />
      <div className="editor-inline-actions"><Button type="button" disabled={resource.loading} onClick={resource.reload}>Reload technical information</Button></div>
    </>}
  </div>;
}

export function MediaChaptersDetails({ itemId }: { itemId: string }) {
  const { t, locale } = useTranslation();
  const resource = useMediaInfo(itemId);
  const [page, setPage] = useState(0);
  const data = resource.data;
  const pageSize = 100;
  const maximum = Math.max(0, Math.ceil((data?.Chapters.length ?? 0) / pageSize) - 1);
  const currentPage = Math.min(page, maximum);
  const entries = data?.Chapters.slice(currentPage * pageSize, (currentPage + 1) * pageSize) ?? [];
  return <div className="media-chapters-details" aria-busy={resource.loading}>
    <MediaReadFeedback resource={resource} />
    {data && <>
      {data.Summary.State === 'not_applicable' ? <p className="editor-note">{t('Folders do not have media chapters.')}</p>
        : data.Summary.State === 'not_scanned' ? <p className="editor-note">{t('Scan this library to read its media chapters.')}</p>
          : data.Chapters.length === 0 ? <p className="editor-note">{t('No chapters are recorded in this source.')}</p> : <>
            <div className="editor-stream-table media-chapter-table"><table aria-label={t('Recorded chapters')}><thead><tr><th scope="col">#</th><th scope="col">{t('Time')}</th><th scope="col">{t('Chapter')}</th></tr></thead><tbody>{entries.map((chapter) => <tr key={chapter.Index}><td className="media-mono">{chapter.Index + 1}</td><td className="media-mono">{mediaDuration(chapter.StartTicks)}{chapter.EndTicks !== null && <span className="editor-stream-note">– {mediaDuration(chapter.EndTicks)}</span>}</td><td translate="no">{chapter.Name || t('Untitled chapter')}</td></tr>)}</tbody></table></div>
            <div className="media-chapter-pagination"><span>{t('Chapters {start}–{end} of {total}', { start: new Intl.NumberFormat(locale).format(currentPage * pageSize + 1), end: new Intl.NumberFormat(locale).format(currentPage * pageSize + entries.length), total: new Intl.NumberFormat(locale).format(data.ChapterCount) })}</span>{maximum > 0 && <><Button type="button" size="small" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>Previous</Button><Button type="button" size="small" disabled={currentPage >= maximum} onClick={() => setPage(currentPage + 1)}>Next</Button></>}</div>
          </>}
      {data.ChaptersTruncated && <Alert severity="info">{t('Showing the first {count} recorded chapters.', { count: data.Chapters.length })}</Alert>}
      <Button type="button" size="small" disabled={resource.loading} onClick={resource.reload}>Refresh chapters</Button>
    </>}
  </div>;
}
