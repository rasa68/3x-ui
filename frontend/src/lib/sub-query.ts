// Extra query parameters the admin wants on every subscription link. Mirrors
// internal/util/subquery so links built here match the ones the server builds.
export interface SubQueryParam {
  key: string;
  value: string;
}

export const SUB_QUERY_MAX_PARAMS = 12;
export const SUB_QUERY_MAX_VALUE_LEN = 128;
const KEY_RE = /^[A-Za-z0-9_-]{1,32}$/;
const RESERVED = new Set(['html', 'view', 'format']);

// Keys the panel understands when a subscription is fetched.
export const SUB_QUERY_KEY_PRESETS = [
  'hwid',
  'device_id',
  'deviceId',
  'client_id',
  'clientId',
  'uid',
  'model',
  'device',
  'device_model',
  'deviceModel',
  'phone',
  'deviceName',
  'os',
  'app',
];

export function isValidSubQueryKey(key: string): boolean {
  const k = key.trim();
  return KEY_RE.test(k) && !RESERVED.has(k.toLowerCase());
}

export function parseSubQueryParams(raw: string | null | undefined): SubQueryParam[] {
  if (!raw || !raw.trim()) return [];
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(data)) return [];
  const out: SubQueryParam[] = [];
  const seen = new Set<string>();
  for (const item of data) {
    if (!item || typeof item !== 'object') continue;
    const key = String((item as SubQueryParam).key ?? '').trim();
    const value = String((item as SubQueryParam).value ?? '').trim();
    if (!isValidSubQueryKey(key) || [...value].length > SUB_QUERY_MAX_VALUE_LEN) continue;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push({ key, value });
    if (out.length === SUB_QUERY_MAX_PARAMS) break;
  }
  return out;
}

export function serializeSubQueryParams(params: SubQueryParam[]): string {
  const clean = params
    .map((p) => ({ key: p.key.trim(), value: p.value.trim() }))
    .filter((p) => p.key !== '');
  return clean.length ? JSON.stringify(clean) : '';
}

// URLSearchParams escaping (space -> '+') matches Go's url.QueryEscape.
export function appendSubQuery(link: string, raw: string | null | undefined): string {
  if (!link) return link;
  const params = parseSubQueryParams(raw);
  if (!params.length) return link;
  const qIndex = link.indexOf('?');
  const base = qIndex === -1 ? link : link.slice(0, qIndex);
  const query = qIndex === -1 ? '' : link.slice(qIndex + 1);
  const existing = new URLSearchParams(query);
  const parts: string[] = [];
  for (const p of params) {
    if (existing.has(p.key)) continue;
    parts.push(new URLSearchParams({ [p.key]: p.value }).toString());
  }
  if (!parts.length) return link;
  const extra = parts.join('&');
  return base + '?' + (query ? query + '&' : '') + extra;
}
