import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { useApp } from '../context';
import { chooseWaveformLevel, loadAudioWaveform, loadAudioWaveforms, waveformBucketValid, type AudioWaveformData, type AudioWaveforms } from '../lib/audioWaveforms';

type WaveformStatus = 'idle' | 'loading' | 'ready' | 'missing' | 'stale' | 'error';

export interface WaveformState {
  scope: string;
  status: WaveformStatus;
  data?: AudioWaveforms;
}

export function useAudioWaveforms(itemId: string, sourceId: string | undefined, active: boolean): WaveformState {
  const { session } = useApp();
  const scope = JSON.stringify([session.serverUrl, session.ServerId, session.User.Id, session.AccessToken, itemId, sourceId]);
  const [state, setState] = useState<WaveformState>({ scope: '', status: 'idle' });
  useEffect(() => {
    if (!active) { setState({ scope, status: 'idle' }); return; }
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 15_000);
    setState({ scope, status: 'loading' });
    let retired = false;
    void loadAudioWaveforms(itemId, sourceId, controller.signal).then(data => {
      if (retired) return;
      setState({ scope, status: data.stale ? 'stale' : data.available ? 'ready' : 'missing', data });
    }).catch(() => {
      if (!retired) setState({ scope, status: 'error' });
    }).finally(() => clearTimeout(timeout));
    return () => { retired = true; clearTimeout(timeout); controller.abort(); };
  }, [scope, itemId, sourceId, active]);
  return !active ? { scope, status: 'idle' } : state.scope === scope && state.status !== 'idle' ? state : { scope, status: 'loading' };
}

interface WaveformPaths {
  peaks: string;
  silence: string;
  gaps: { start: number; width: number }[];
  validCount: number;
}

function pathsFor(data: AudioWaveformData, width: number): WaveformPaths {
  const scale = 1000 / data.bucketCount;
  const barCount = Math.min(data.bucketCount, 180, Math.max(1, Math.floor(width / 6)));
  const paths: WaveformPaths = { peaks: '', silence: '', gaps: [], validCount: 0 };
  let gapStart = -1;
  const closeGap = (end: number) => {
    if (gapStart < 0) return;
    paths.gaps.push({ start: gapStart * scale, width: (end - gapStart) * scale });
    gapStart = -1;
  };
  for (let index = 0; index < data.bucketCount;) {
    if (!waveformBucketValid(data, index)) { if (gapStart < 0) gapStart = index; index += 1; continue; }
    closeGap(index);
    let end = index;
    let peak = 0;
    // Use evenly spaced display bins while preserving every invalid gap.
    const limit = Math.floor(Math.ceil((index + 1) * barCount / data.bucketCount) * data.bucketCount / barCount);
    while (end < limit && waveformBucketValid(data, end)) {
      peak = Math.max(peak, data.peaks[end]);
      end += 1;
    }
    const length = end - index;
    paths.validCount += length;
    const x = (index * scale).toFixed(3);
    const barWidth = (scale * length * .56).toFixed(3);
    const bar = (value: number) => {
      const height = value / 65535 * 19;
      return `M${x} ${(20 - height).toFixed(3)}h${barWidth}v${(height * 2).toFixed(3)}h-${barWidth}Z`;
    };
    if (peak === 0) paths.silence += `M${x} 20h${(scale * length).toFixed(3)}`;
    else paths.peaks += bar(peak);
    index = end;
  }
  closeGap(data.bucketCount);
  return paths;
}

const statusText: Record<WaveformStatus, string> = {
  idle: '暂无波形数据', loading: '正在载入波形', ready: '此音轨暂无波形', missing: '暂无波形数据',
  stale: '片源已变化，请重新生成波形', error: '波形暂时无法载入',
};

export function WaveformLane({ streamIndex, label, selected, active, waveforms }: {
  streamIndex: number;
  label: string;
  selected: boolean;
  active: boolean;
  waveforms: WaveformState;
}) {
  const container = useRef<HTMLDivElement>(null);
  const patternId = `waveform-gaps-${useId().replace(/:/g, '')}`;
  const [width, setWidth] = useState(0);
  const [loaded, setLoaded] = useState<{ scope: string; key: string; data?: AudioWaveformData; failed?: boolean }>();
  const cache = useRef<{ scope: string; entries: Map<string, AudioWaveformData> }>({ scope: '', entries: new Map() });
  const stream = waveforms.data?.streams.find(entry => entry.streamIndex === streamIndex);
  const level = stream ? chooseWaveformLevel(stream.levels, width) : undefined;
  const ticks = waveforms.data?.durationTicks;
  const resourceKey = level && ticks !== undefined ? JSON.stringify([level.url, streamIndex, level.bucketCount, ticks.toString(), waveforms.data?.sourceVersion]) : undefined;
  useEffect(() => {
    const element = container.current;
    if (!active || !element) return;
    const update = () => setWidth(Math.max(1, element.clientWidth));
    update();
    const observer = new ResizeObserver(update);
    observer.observe(element);
    return () => observer.disconnect();
  }, [active]);
  useEffect(() => {
    if (cache.current.scope !== waveforms.scope) cache.current = { scope: waveforms.scope, entries: new Map() };
    if (!active || waveforms.status !== 'ready' || !level || ticks === undefined || !resourceKey || width === 0) return;
    const scope = waveforms.scope;
    const key = resourceKey;
    const saved = cache.current.entries.get(key);
    if (saved) { setLoaded({ scope, key, data: saved }); return; }
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 15_000);
    let retired = false;
    setLoaded({ scope, key });
    void loadAudioWaveform(streamIndex, level, ticks, controller.signal).then(data => {
      if (retired) return;
      // Bound the in-memory data retained by this account, item, and source lane.
      if (cache.current.entries.size >= 4) cache.current.entries.clear();
      cache.current.entries.set(key, data);
      setLoaded({ scope, key, data });
    }).catch(() => { if (!retired) setLoaded({ scope, key, failed: true }); }).finally(() => clearTimeout(timeout));
    return () => { retired = true; clearTimeout(timeout); controller.abort(); };
  }, [active, waveforms.scope, waveforms.status, resourceKey, ticks, streamIndex, width === 0]);
  const current = active && waveforms.status === 'ready' && loaded?.scope === waveforms.scope && loaded.key === resourceKey ? loaded : undefined;
  const paths = useMemo(() => current?.data ? pathsFor(current.data, width) : undefined, [current?.data, width]);
  const status = waveforms.status === 'ready' && level ? current?.failed ? 'error' : paths ? 'ready' : 'loading' : waveforms.status;
  const text = paths && !paths.validCount ? '此音轨没有有效波形片段' : statusText[status];
  const description = `${label}波形${selected ? '，当前音轨' : ''}；显示真实音量峰值，细线表示静音${paths?.gaps.length ? '；斜纹区表示没有有效数据' : ''}`;
  return <div ref={container} className={`detail-audio-lane ${selected ? 'is-selected' : ''} ${paths ? 'has-waveform' : ''}`} data-stream-index={streamIndex} data-waveform-status={status} aria-busy={status === 'loading'}>
    {paths ? <svg className="detail-waveform" viewBox="0 0 1000 40" preserveAspectRatio="none" role="img" aria-label={description} data-bucket-count={current?.data?.bucketCount}>
      <title>{description}</title>
      <defs><pattern id={patternId} width="7" height="7" patternUnits="userSpaceOnUse"><path d="M-1 1L1-1M0 7L7 0M6 8L8 6" stroke="currentColor" strokeWidth="1" /></pattern></defs>
      {paths.gaps.map(gap => <rect className="detail-waveform-gap" key={gap.start} x={gap.start} y="0" width={gap.width} height="40" fill={`url(#${patternId})`} />)}
      <path className="detail-waveform-peaks" d={paths.peaks} fill="currentColor" />
      <path className="detail-waveform-silence" d={paths.silence} stroke="currentColor" strokeWidth="1" vectorEffect="non-scaling-stroke" />
    </svg> : null}
    {!paths || !paths.validCount ? <span role="status">{text}</span> : null}
  </div>;
}
