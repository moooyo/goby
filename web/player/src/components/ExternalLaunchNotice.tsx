import { Icon } from '../components';

export interface ExternalLaunch {
  name: string;
  launchUrl: string;
  streamUrl: string;
}

export function ExternalLaunchNotice({ value, close, notify }: { value: ExternalLaunch; close: () => void; notify: (message: string) => void }) {
  const copy = async () => {
    try { await navigator.clipboard.writeText(value.streamUrl); notify('串流链接已复制'); }
    catch { window.prompt('请复制下方串流链接到播放器：', value.streamUrl); }
  };
  return <aside className="external-launch-notice" aria-label="外部播放器">
    <button className="external-launch-close" onClick={close} aria-label="关闭外部播放提示"><Icon name="close" size={16}/></button>
    <p>{value.launchUrl ? `已请求用 ${value.name} 播放` : `请在 ${value.name} 中打开串流链接`}</p>
    <div>{value.launchUrl && <a href={value.launchUrl}>再次打开 {value.name}</a>}<button onClick={() => void copy()}>复制串流链接</button></div>
  </aside>;
}
