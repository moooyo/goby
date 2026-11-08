// Match the supported libx264 analysis controls before sending a settings write.
// The server remains authoritative for validation and canonical storage.
export function normalizeX264Options(raw: string): string | undefined {
  if (raw === '') return '';
  if (raw.length > 512 || /[^\x21-\x7e]|['"\\]/.test(raw)) return undefined;
  const items = raw.split(':');
  if (items.length > 7) return undefined;
  const seen = new Set<string>();
  const normalized: string[] = [];
  for (const item of items) {
    const pair = item.split('=');
    if (pair.length !== 2 || !pair[0] || !pair[1]) return undefined;
    let key = pair[0].replaceAll('_', '-');
    if (key === 'merange') key = 'me-range';
    if (seen.has(key)) return undefined;
    seen.add(key);
    let value = pair[1];
    const integer = (minimum: number, maximum: number): boolean => {
      if (!/^[+-]?\d+$/.test(value)) return false;
      const number = Number(value);
      if (!Number.isSafeInteger(number) || number < minimum || number > maximum) return false;
      value = String(number);
      return true;
    };
    switch (key) {
      case 'subme': if (!integer(0, 9)) return undefined; break;
      case 'me-range': if (!integer(4, 16)) return undefined; break;
      case 'rc-lookahead': if (!integer(0, 250)) return undefined; break;
      case 'aq-mode': if (!integer(0, 3)) return undefined; break;
      case 'aq-strength': {
        if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(value)) return undefined;
        const number = Number(value);
        if (!Number.isFinite(number) || number < 0 || number > 3) return undefined;
        value = String(number);
        break;
      }
      case 'deblock': {
        const values = value.split(',');
        if (values.length !== 2 || values.some((part) => !/^[+-]?\d+$/.test(part) || !Number.isSafeInteger(Number(part)) || Number(part) < -6 || Number(part) > 6)) return undefined;
        value = values.map((part) => String(Number(part))).join(',');
        break;
      }
      case 'partitions': if (value !== 'none' && value !== 'all') return undefined; break;
      default: return undefined;
    }
    normalized.push(key + '=' + value);
  }
  return normalized.sort().join(':');
}
