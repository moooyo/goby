import { forwardRef, useId, type CSSProperties, type ReactNode, type SVGProps } from 'react';

export type IconSx = Record<string, unknown> | readonly (Record<string, unknown> | false | undefined)[];
export interface IconProps extends Omit<SVGProps<SVGSVGElement>, 'color'> {
  fontSize?: 'inherit' | 'small' | 'medium' | 'large' | number | string;
  color?: string;
  sx?: IconSx;
  titleAccess?: string;
}

const paths = {
  grid: <><rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.5"/></>,
  menu: <path d="M4 7h16M4 12h16M4 17h16"/>,
  library: <path d="m16 6 4 14M12 6v14M8 8v12M4 4v16"/>,
  image: <><rect x="3" y="3" width="18" height="18" rx="2.5"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.1-3.1a2 2 0 0 0-2.8 0L6 21"/></>,
  layers: <><path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 13 9 5 9-5M3 18l9 5 9-5"/></>,
  waveform: <path d="M3 10v4M7 6v12M11 3v18M15 8v8M19 5v14"/>,
  users: <><circle cx="9" cy="8.5" r="3.5"/><path d="M2.8 19.5c.9-3 3.3-4.6 6.2-4.6s5.3 1.6 6.2 4.6M16 5.2a3.4 3.4 0 0 1 0 6.6M18.4 14.9c1.5.7 2.5 2.2 2.9 4.6"/></>,
  user: <><circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/></>,
  userAdd: <><circle cx="9" cy="8" r="3.5"/><path d="M2.5 20v-1a6.5 6.5 0 0 1 13 0v1M19 8v6M16 11h6"/></>,
  userSettings: <><circle cx="8" cy="7" r="3"/><path d="M2 19v-1a6 6 0 0 1 10-4.5M18 11v2m0 7v2M13.2 13.7l1.5 1M21.3 18.3l1.5 1M13.2 19.3l1.5-1M21.3 14.7l1.5-1"/><circle cx="18" cy="16.5" r="3.2"/></>,
  monitor: <><rect x="3.5" y="4" width="17" height="12.5" rx="2"/><path d="M8.5 20h7M12 16.5V20"/></>,
  devices: <><rect x="2.5" y="4" width="15" height="11" rx="1.5"/><path d="M6 19h6M9 15v4"/><rect x="15" y="10" width="6.5" height="11" rx="1.4"/></>,
  radio: <><circle cx="12" cy="12" r="2"/><path d="M16.2 7.8a6 6 0 0 1 0 8.4M7.8 16.2a6 6 0 0 1 0-8.4M19.1 4.9a10 10 0 0 1 0 14.2M4.9 19.1a10 10 0 0 1 0-14.2"/></>,
  key: <><circle cx="7.5" cy="15.5" r="4.5"/><path d="m10.7 12.3 9.8-9.8M17 6l3 3M14.5 8.5 17 11"/></>,
  tasks: <path d="m3 17 2 2 4-4M3 7l2 2 4-4M13 6h8M13 12h8M13 18h8"/>,
  pulse: <path d="M22 12h-4l-3 9L9 3l-3 9H2"/>,
  bell: <><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.9 1.9 0 0 0 3.4 0"/></>,
  archive: <><rect x="2.5" y="3.5" width="19" height="5" rx="1"/><path d="M4.5 8.5v10a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2v-10M10 12.5h4"/></>,
  sliders: <><path d="M4 7h9m4 0h3M4 17h3m4 0h9"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/></>,
  gauge: <><path d="m12 14 4-4M3.3 19a10 10 0 1 1 17.4 0"/></>,
  cpu: <><rect x="5" y="5" width="14" height="14" rx="2"/><rect x="9.5" y="9.5" width="5" height="5" rx=".5"/><path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"/></>,
  memory: <><rect x="3" y="6" width="18" height="11" rx="2"/><path d="M7 17v3M12 17v3M17 17v3M7.5 10.5v2M12 10.5v2M16.5 10.5v2"/></>,
  subtitles: <><rect x="2" y="4" width="20" height="16" rx="3"/><path d="M6 10h4m4 0h4M6 15h6m3 0h3"/></>,
  globe: <><circle cx="12" cy="12" r="9.5"/><path d="M12 2.5a14 14 0 0 0 0 19 14 14 0 0 0 0-19M2.5 12h19"/></>,
  filter: <path d="M4 7h16M7 12h10M10 17h4"/>,
  filterOff: <><path d="M4 7h16M7 12h6M10 17h4M3 3l18 18"/></>,
  copy: <><rect x="8" y="8" width="13" height="13" rx="2"/><path d="M16 8V3H3v13h5"/></>,
  search: <><circle cx="11" cy="11" r="7"/><path d="m20 20-3.6-3.6"/></>,
  sun: <><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></>,
  moon: <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>,
  plus: <path d="M12 5v14M5 12h14"/>,
  more: <><circle cx="5" cy="12" r=".8" fill="currentColor"/><circle cx="12" cy="12" r=".8" fill="currentColor"/><circle cx="19" cy="12" r=".8" fill="currentColor"/></>,
  lock: <><rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/></>,
  unlock: <><rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 7.6-1.8"/></>,
  folder: <path d="M20 19.5a1.5 1.5 0 0 0 1.5-1.5V8.5A1.5 1.5 0 0 0 20 7h-7.5L10 4.5H4A1.5 1.5 0 0 0 2.5 6v12A1.5 1.5 0 0 0 4 19.5Z"/>,
  folderOpen: <><path d="M2.5 18V6A1.5 1.5 0 0 1 4 4.5h6L12.5 7H20v4"/><path d="M4 20h15.2a1 1 0 0 0 1-.7l2.2-7.6a.6.6 0 0 0-.6-.7H6.5L4 20Z"/></>,
  refresh: <><path d="M20 7v5h-5M4 17v-5h5"/><path d="M5 8a8 8 0 0 1 13-3l2 2M4 17l2 2a8 8 0 0 0 13-3"/></>,
  check: <path d="m5 12 4.5 4.5L19 7"/>,
  checkCircle: <><circle cx="12" cy="12" r="9"/><path d="m7.5 12 3 3 6-6"/></>,
  close: <path d="m6 6 12 12M6 18 18 6"/>,
  cancel: <><circle cx="12" cy="12" r="9"/><path d="m8.5 8.5 7 7m0-7-7 7"/></>,
  block: <><circle cx="12" cy="12" r="9"/><path d="M5.6 5.6l12.8 12.8"/></>,
  chevronDown: <path d="m6 9 6 6 6-6"/>,
  chevronUp: <path d="m6 15 6-6 6 6"/>,
  chevronRight: <path d="m9 5 7 7-7 7"/>,
  arrowLeft: <path d="M20 12H4m7-7-7 7 7 7"/>,
  arrowRight: <path d="M4 12h16m-7-7 7 7-7 7"/>,
  arrowUp: <path d="M12 20V4m-7 7 7-7 7 7"/>,
  arrowDown: <path d="M12 4v16m-7-7 7 7 7-7"/>,
  play: <path d="m8 4 13 8-13 8Z"/>,
  pause: <><circle cx="12" cy="12" r="9"/><path d="M9 8v8M15 8v8"/></>,
  stop: <rect x="6" y="6" width="12" height="12" rx="2"/>,
  alert: <><path d="M10.3 4.2 2.6 17.5a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 4.2a2 2 0 0 0-3.4 0ZM12 9.5v4M12 17h.01"/></>,
  error: <><circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 17v.1"/></>,
  info: <><circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7v.1"/></>,
  clock: <><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></>,
  history: <><path d="M3 4v6h6M3 10a9 9 0 1 1 2 8"/><path d="M12 7v5l3 2"/></>,
  server: <><rect x="3" y="3.5" width="18" height="7" rx="2"/><rect x="3" y="13.5" width="18" height="7" rx="2"/><path d="M7 7h.01M7 17h.01"/></>,
  scan: <path d="M3 7V5a2 2 0 0 1 2-2h2M17 3h2a2 2 0 0 1 2 2v2M21 17v2a2 2 0 0 1-2 2h-2M7 21H5a2 2 0 0 1-2-2v-2M7 12h10"/>,
  shield: <path d="M12 21s-7-3-7-9V5.5L12 3l7 2.5V12c0 6-7 9-7 9Z"/>,
  shieldCheck: <><path d="M12 21s-7-3-7-9V5.5L12 3l7 2.5V12c0 6-7 9-7 9Z"/><path d="m8.5 11.5 2.5 2.5 4.5-5"/></>,
  edit: <><path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/></>,
  note: <><path d="M13 4H4v16h16v-8M8 8h4M8 12h2M8 16h6"/><path d="m14 10 6-6 2 2-6 6-3 1Z"/></>,
  tv: <><rect x="3" y="6" width="18" height="15" rx="2"/><path d="m8 2 4 4 4-4"/></>,
  phone: <><rect x="7" y="2.5" width="10" height="19" rx="2.5"/><path d="M11 18.5h2"/></>,
  tablet: <><rect x="4.5" y="2.5" width="15" height="19" rx="2.2"/><path d="M11 18.5h2"/></>,
  download: <path d="M12 4v11M7 10l5 5 5-5M4 20h16"/>,
  upload: <path d="M12 16V4M7 9l5-5 5 5M4 20h16"/>,
  uploadFile: <><path d="M14 2.5H5v19h14v-14L14 2.5Z"/><path d="M14 2.5v5h5M12 18v-7m-3 3 3-3 3 3"/></>,
  external: <><path d="M14 3h7v7m0-7L10 14"/><path d="M10 3H3v18h18v-7"/></>,
  film: <><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M7 3v18M17 3v18M3 8h4M3 16h4M17 8h4M17 16h4"/></>,
  eye: <><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/></>,
  eyeOff: <><path d="m3 3 18 18M10.6 5.1A12 12 0 0 1 12 5c6.5 0 10 7 10 7a18 18 0 0 1-4 4.8M6.5 6.5A18 18 0 0 0 2 12s3.5 7 10 7a11 11 0 0 0 5.5-1.5M10 10a2.8 2.8 0 0 0 4 4"/></>,
  trash: <path d="M4 7h16M9 7V4h6v3M6 7l1 14h10l1-14M10 11v6M14 11v6"/>,
  save: <><path d="M4 3h13l4 4v14H3V3h1ZM7 3v6h10V3M7 21v-8h10v8"/></>,
  book: <><path d="M12 5C8 2.5 4.5 3 2.5 4v16c3-1.5 6.5-1 9.5 1 3-2 6.5-2.5 9.5-1V4c-2-1-5.5-1.5-9.5 1Z"/><path d="M12 5v16"/></>,
  music: <><path d="M8 18V5l12-2v13M8 8l12-2"/><ellipse cx="5" cy="18" rx="3" ry="3"/><ellipse cx="17" cy="16" rx="3" ry="3"/></>,
  playlist: <><path d="M3 5h14M3 10h14M3 15h8M16 13l6 4-6 4Z"/></>,
  playlistCheck: <path d="M3 5h14M3 10h14M3 15h7m3 3 3 3 6-7"/>,
  list: <><path d="M8 5h13M8 12h13M8 19h13"/><circle cx="3" cy="5" r=".5" fill="currentColor"/><circle cx="3" cy="12" r=".5" fill="currentColor"/><circle cx="3" cy="19" r=".5" fill="currentColor"/></>,
  text: <path d="M4 5h16M4 10h12M4 15h16M4 20h9"/>,
  tag: <><path d="M2.5 11.5V3h8.5L22 14l-8 8L2.5 11.5Z"/><circle cx="7.5" cy="7.5" r="1"/></>,
  building: <><rect x="4" y="3" width="16" height="18" rx="1.5"/><path d="M8 7h1M15 7h1M8 11h1M15 11h1M10 21v-6h4v6"/></>,
  category: <><path d="m6 2 4 7H2l4-7Z"/><rect x="14" y="2" width="8" height="7" rx="1"/><circle cx="6" cy="17" r="4"/><path d="m18 13 4 8h-8l4-8Z"/></>,
  broom: <><path d="m14 3-6 9M6 11l7 4-3 7-9-5 5-6ZM4.5 15l4 2"/><path d="M17 12h5M17 16h4M15 20h4"/></>,
  cloudCheck: <><path d="M6 18a4.5 4.5 0 0 1-.3-9 6.5 6.5 0 0 1 12.4-1.5A5.2 5.2 0 0 1 19 18"/><path d="m9 17 3 3 5-6"/></>,
  compare: <path d="M3 7h17m-4-4 4 4-4 4M21 17H4m4-4-4 4 4 4"/>,
  calendar: <><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M7 2v6M17 2v6M3 10h18m-13 6 3 3 5-6"/></>,
  calendarEdit: <><path d="M13 21H3V5h18v6M7 2v6M17 2v6M3 10h18m-7 8 6-6 2 2-6 6-3 1 1-3Z"/></>,
  circle: <circle cx="12" cy="12" r="9"/>,
  science: <><path d="M9 3h6M10 3v6L3.5 19a1.5 1.5 0 0 0 1.3 2h14.4a1.5 1.5 0 0 0 1.3-2L14 9V3M7 14h10"/></>,
  selectAll: <><path d="M3 7V3h4m10 0h4v4M3 17v4h4m10 0h4v-4"/><rect x="7" y="7" width="10" height="10" rx="1"/></>,
  login: <path d="M14 4h6v16h-6M3 12h12m-5-5 5 5-5 5"/>,
  logout: <path d="M10 4H4v16h6m-1-8h12m-5-5 5 5-5 5"/>,
  language: <><path d="M3 5h12M9 2v3M5 5c1 6 4 9 9 12M13 5c-1 6-4 9-10 12M14 21l4-10 4 10m-7-3h6"/></>,
} satisfies Record<string, ReactNode>;

type IconName = keyof typeof paths;

const palette: Record<string, string> = {
  inherit: 'inherit', primary: 'var(--acc-ink)', secondary: 'var(--ink2)', action: 'var(--ink2)', disabled: 'var(--ink3)',
  success: 'var(--ok)', warning: 'var(--warn)', error: 'var(--err)', info: 'var(--acc-ink)',
  'primary.main': 'var(--acc-ink)', 'primary.dark': 'var(--acc-ink)', 'text.primary': 'var(--ink)',
  'text.secondary': 'var(--ink2)', 'text.disabled': 'var(--ink3)', 'success.main': 'var(--ok)',
  'warning.main': 'var(--warn)', 'error.main': 'var(--err)', 'info.main': 'var(--acc-ink)',
};

function colorValue(value: unknown): string | undefined {
  return typeof value === 'string' ? palette[value] ?? value : undefined;
}

function sxStyle(sx: IconSx | undefined): CSSProperties {
  const result: Record<string, unknown> = {};
  const sources = Array.isArray(sx) ? sx : [sx];
  const aliases: Record<string, string[]> = {
    m: ['margin'], mx: ['marginLeft', 'marginRight'], my: ['marginTop', 'marginBottom'], mt: ['marginTop'], mr: ['marginRight'], mb: ['marginBottom'], ml: ['marginLeft'],
    p: ['padding'], px: ['paddingLeft', 'paddingRight'], py: ['paddingTop', 'paddingBottom'], pt: ['paddingTop'], pr: ['paddingRight'], pb: ['paddingBottom'], pl: ['paddingLeft'],
    bgcolor: ['backgroundColor'],
  };
  for (const source of sources) {
    if (!source || typeof source !== 'object') continue;
    for (const [key, value] of Object.entries(source)) {
      if (value == null || typeof value === 'object' || key.startsWith('&') || key.startsWith('@')) continue;
      const keys = aliases[key] ?? [key];
      const resolved = key === 'color' || key === 'bgcolor' || key === 'backgroundColor' ? colorValue(value)
        : /^(m|p)([trblxy])?$/.test(key) && typeof value === 'number' ? value * 8 : value;
      for (const name of keys) result[name] = resolved;
    }
  }
  return result as CSSProperties;
}

function sizeValue(fontSize: IconProps['fontSize']): string | number {
  if (fontSize === 'inherit') return '1em';
  if (fontSize === 'small') return 18;
  if (fontSize === 'large') return 32;
  return !fontSize || fontSize === 'medium' ? 22 : fontSize;
}

function createIcon(name: IconName, displayName: string) {
  const Component = forwardRef<SVGSVGElement, IconProps>(function LineIcon({ fontSize, color, sx, titleAccess, className = '', style, children, ...props }, ref) {
    const titleId = useId();
    const size = sizeValue(fontSize);
    const accessible = Boolean(titleAccess || props['aria-label'] || props['aria-labelledby']);
    return <svg ref={ref} viewBox="0 0 24 24" width="1em" height="1em" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round"
      focusable="false" aria-hidden={accessible ? undefined : true} role={accessible ? 'img' : undefined} aria-labelledby={titleAccess ? titleId : undefined}
      className={`goby-icon MuiSvgIcon-root${fontSize ? ` MuiSvgIcon-fontSize${String(fontSize).charAt(0).toUpperCase()}${String(fontSize).slice(1)}` : ''} ${className}`.trim()}
      style={{ display: 'inline-block', flexShrink: 0, fontSize: size, color: colorValue(color), verticalAlign: 'middle', ...sxStyle(sx), ...style }} {...props}>
      {titleAccess && <title id={titleId}>{titleAccess}</title>}{paths[name]}{children}
    </svg>;
  });
  Component.displayName = displayName;
  return Component;
}

export const AddRounded = createIcon('plus', 'AddRounded');
export const AdminPanelSettingsRounded = createIcon('shieldCheck', 'AdminPanelSettingsRounded');
export const ArrowBackRounded = createIcon('arrowLeft', 'ArrowBackRounded');
export const ArrowDownwardRounded = createIcon('arrowDown', 'ArrowDownwardRounded');
export const ArrowForwardRounded = createIcon('arrowRight', 'ArrowForwardRounded');
export const ArrowUpwardRounded = createIcon('arrowUp', 'ArrowUpwardRounded');
export const BackupOutlined = createIcon('archive', 'BackupOutlined');
export const BlockOutlined = createIcon('block', 'BlockOutlined');
export const BlockRounded = createIcon('block', 'BlockRounded');
export const BusinessOutlined = createIcon('building', 'BusinessOutlined');
export const CancelOutlined = createIcon('cancel', 'CancelOutlined');
export const CategoryOutlined = createIcon('category', 'CategoryOutlined');
export const CheckCircleRounded = createIcon('checkCircle', 'CheckCircleRounded');
export const CheckRounded = createIcon('check', 'CheckRounded');
export const CleaningServicesRounded = createIcon('broom', 'CleaningServicesRounded');
export const CloseRounded = createIcon('close', 'CloseRounded');
export const CloudDoneRounded = createIcon('cloudCheck', 'CloudDoneRounded');
export const CollectionsBookmarkRounded = createIcon('layers', 'CollectionsBookmarkRounded');
export const CompareArrowsRounded = createIcon('compare', 'CompareArrowsRounded');
export const ContentCopyRounded = createIcon('copy', 'ContentCopyRounded');
export const DeleteOutlineRounded = createIcon('trash', 'DeleteOutlineRounded');
export const DesktopWindowsRounded = createIcon('monitor', 'DesktopWindowsRounded');
export const DevicesOutlined = createIcon('devices', 'DevicesOutlined');
export const DownloadRounded = createIcon('download', 'DownloadRounded');
export const EditCalendarRounded = createIcon('calendarEdit', 'EditCalendarRounded');
export const EditNoteRounded = createIcon('note', 'EditNoteRounded');
export const EditOutlined = createIcon('edit', 'EditOutlined');
export const ErrorOutlineRounded = createIcon('error', 'ErrorOutlineRounded');
export const EventAvailableOutlined = createIcon('calendar', 'EventAvailableOutlined');
export const ExpandLessRounded = createIcon('chevronUp', 'ExpandLessRounded');
export const ExpandMoreRounded = createIcon('chevronDown', 'ExpandMoreRounded');
export const FilterAltOffOutlined = createIcon('filterOff', 'FilterAltOffOutlined');
export const FilterListRounded = createIcon('filter', 'FilterListRounded');
export const FolderOpenOutlined = createIcon('folderOpen', 'FolderOpenOutlined');
export const FolderOutlined = createIcon('folder', 'FolderOutlined');
export const HistoryRounded = createIcon('history', 'HistoryRounded');
export const ImageOutlined = createIcon('image', 'ImageOutlined');
export const InfoOutlined = createIcon('info', 'InfoOutlined');
export const Inventory2Outlined = createIcon('archive', 'Inventory2Outlined');
export const KeyRounded = createIcon('key', 'KeyRounded');
export const KeyboardArrowDownRounded = createIcon('chevronDown', 'KeyboardArrowDownRounded');
export const LibraryBooksOutlined = createIcon('book', 'LibraryBooksOutlined');
export const LibraryMusicOutlined = createIcon('music', 'LibraryMusicOutlined');
export const ListAltRounded = createIcon('list', 'ListAltRounded');
export const LiveTvOutlined = createIcon('tv', 'LiveTvOutlined');
export const LocalOfferOutlined = createIcon('tag', 'LocalOfferOutlined');
export const LockOutlined = createIcon('lock', 'LockOutlined');
export const LockResetRounded = createIcon('unlock', 'LockResetRounded');
export const LoginRounded = createIcon('login', 'LoginRounded');
export const LogoutRounded = createIcon('logout', 'LogoutRounded');
export const ManageAccountsOutlined = createIcon('userSettings', 'ManageAccountsOutlined');
export const MemoryOutlined = createIcon('cpu', 'MemoryOutlined');
export const MonitorHeartOutlined = createIcon('pulse', 'MonitorHeartOutlined');
export const MonitorHeartRounded = createIcon('pulse', 'MonitorHeartRounded');
export const MoreVertRounded = createIcon('more', 'MoreVertRounded');
export const MovieFilterOutlined = createIcon('film', 'MovieFilterOutlined');
export const MovieOutlined = createIcon('film', 'MovieOutlined');
export const MusicNoteOutlined = createIcon('music', 'MusicNoteOutlined');
export const NavigateNextRounded = createIcon('chevronRight', 'NavigateNextRounded');
export const OpenInNewRounded = createIcon('external', 'OpenInNewRounded');
export const PauseCircleOutlineRounded = createIcon('pause', 'PauseCircleOutlineRounded');
export const PeopleOutlineRounded = createIcon('users', 'PeopleOutlineRounded');
export const PeopleRounded = createIcon('users', 'PeopleRounded');
export const PermMediaOutlined = createIcon('image', 'PermMediaOutlined');
export const PersonAddAltRounded = createIcon('userAdd', 'PersonAddAltRounded');
export const PersonOutlineRounded = createIcon('user', 'PersonOutlineRounded');
export const PlayArrowRounded = createIcon('play', 'PlayArrowRounded');
export const PlaylistAddCheckRounded = createIcon('playlistCheck', 'PlaylistAddCheckRounded');
export const PlaylistPlayRounded = createIcon('playlist', 'PlaylistPlayRounded');
export const PublicRounded = createIcon('globe', 'PublicRounded');
export const QueueMusicRounded = createIcon('playlist', 'QueueMusicRounded');
export const RadioButtonUncheckedRounded = createIcon('circle', 'RadioButtonUncheckedRounded');
export const RefreshRounded = createIcon('refresh', 'RefreshRounded');
export const RestartAltRounded = createIcon('history', 'RestartAltRounded');
export const RestoreRounded = createIcon('history', 'RestoreRounded');
export const SaveOutlined = createIcon('save', 'SaveOutlined');
export const ScheduleOutlined = createIcon('clock', 'ScheduleOutlined');
export const ScienceOutlined = createIcon('science', 'ScienceOutlined');
export const SearchRounded = createIcon('search', 'SearchRounded');
export const SelectAllRounded = createIcon('selectAll', 'SelectAllRounded');
export const SensorsRounded = createIcon('radio', 'SensorsRounded');
export const SettingsOutlined = createIcon('sliders', 'SettingsOutlined');
export const SettingsRounded = createIcon('sliders', 'SettingsRounded');
export const ShieldOutlined = createIcon('shield', 'ShieldOutlined');
export const SmartphoneRounded = createIcon('phone', 'SmartphoneRounded');
export const SpaceDashboardOutlined = createIcon('grid', 'SpaceDashboardOutlined');
export const SpaceDashboardRounded = createIcon('grid', 'SpaceDashboardRounded');
export const StopRounded = createIcon('stop', 'StopRounded');
export const StorageOutlined = createIcon('server', 'StorageOutlined');
export const SubjectRounded = createIcon('text', 'SubjectRounded');
export const SubtitlesRounded = createIcon('subtitles', 'SubtitlesRounded');
export const SyncRounded = createIcon('refresh', 'SyncRounded');
export const TabletMacRounded = createIcon('tablet', 'TabletMacRounded');
export const TuneRounded = createIcon('sliders', 'TuneRounded');
export const TvRounded = createIcon('tv', 'TvRounded');
export const UploadFileRounded = createIcon('uploadFile', 'UploadFileRounded');
export const UploadRounded = createIcon('upload', 'UploadRounded');
export const VerifiedUserRounded = createIcon('shieldCheck', 'VerifiedUserRounded');
export const VideoLibraryOutlined = createIcon('library', 'VideoLibraryOutlined');
export const VideoLibraryRounded = createIcon('library', 'VideoLibraryRounded');
export const VideoSettingsRounded = createIcon('film', 'VideoSettingsRounded');
export const VisibilityOffOutlined = createIcon('eyeOff', 'VisibilityOffOutlined');
export const VisibilityOutlined = createIcon('eye', 'VisibilityOutlined');
export const VpnKeyOutlined = createIcon('key', 'VpnKeyOutlined');
export const WarningAmberRounded = createIcon('alert', 'WarningAmberRounded');
export const WorkHistoryOutlined = createIcon('clock', 'WorkHistoryOutlined');

export const MenuRounded = createIcon('menu', 'MenuRounded');
export const NotificationsOutlined = createIcon('bell', 'NotificationsOutlined');
export const LightModeOutlined = createIcon('sun', 'LightModeOutlined');
export const DarkModeOutlined = createIcon('moon', 'DarkModeOutlined');
export const LanguageRounded = createIcon('language', 'LanguageRounded');
export const MemoryUsageOutlined = createIcon('memory', 'MemoryUsageOutlined');
export const SpeedOutlined = createIcon('gauge', 'SpeedOutlined');
export const AudioWaveformOutlined = createIcon('waveform', 'AudioWaveformOutlined');
export const ScanOutlined = createIcon('scan', 'ScanOutlined');
