// The page's view (time range, filters, sort, paging, breakdown fields, open IP)
// round-trips through the query string, so every view can be linked.

export const FIELDS = ['ip', 'range', 'scenario', 'country', 'machine', 'asn', 'action', 'collector'];
export const FIELD_LABELS = {ip: 'IP', range: 'Range', scenario: 'Scenario', country: 'Country', machine: 'Machine', asn: 'ASN', action: 'Action', collector: 'Collector'};
export const DEFAULT_GROUPS = ['scenario', 'country', 'machine', 'asn'];
export const SORTS = ['time', 'ip', 'scenario', 'country', 'machine', 'action', 'events'];
const LIMITS = [25, 50, 100];
const MAX_SPAN = 20 * 365 * 24 * 3600 * 1000;

export function defaultView(hours = 24) {
  return {hours, start: null, end: null, all: false, include: {}, exclude: {}, q: '', sort: 'time', order: 'desc', page: 1, limit: 25, groups: [...DEFAULT_GROUPS], ip: ''};
}

function int(value, fallback) {
  const n = Number.parseInt(value, 10);
  return Number.isFinite(n) ? n : fallback;
}

export function parseView(search, fallbackHours = 24) {
  const q = new URLSearchParams(search);
  const v = defaultView(fallbackHours);
  const hours = int(q.get('hours'), 0);
  if (hours > 0) v.hours = hours;
  const start = int(q.get('start'), 0), end = int(q.get('end'), 0);
  if (start > 0 && end > start) { v.start = start; v.end = end; }
  v.all = q.get('all') === '1';
  for (const f of FIELDS) {
    const inc = q.getAll(f).filter(Boolean), exc = q.getAll('not_' + f).filter(Boolean);
    if (inc.length) v.include[f] = [...new Set(inc)];
    if (exc.length) v.exclude[f] = [...new Set(exc)];
  }
  v.q = (q.get('q') || '').trim();
  if (SORTS.includes(q.get('sort'))) v.sort = q.get('sort');
  if (q.get('order') === 'asc') v.order = 'asc';
  v.page = Math.max(1, int(q.get('page'), 1));
  if (LIMITS.includes(int(q.get('limit'), 0))) v.limit = int(q.get('limit'), 25);
  const groups = q.getAll('group').filter(g => FIELDS.includes(g));
  if (groups.length === DEFAULT_GROUPS.length) v.groups = groups;
  v.ip = (q.get('ip_view') || '').trim();
  return v;
}

// toQuery writes only what differs from the defaults.
export function toQuery(v) {
  const q = new URLSearchParams();
  if (v.all) q.set('all', '1');
  else if (v.start && v.end) { q.set('start', String(v.start)); q.set('end', String(v.end)); }
  else q.set('hours', String(v.hours));
  for (const f of FIELDS) {
    for (const value of v.include[f] || []) q.append(f, value);
    for (const value of v.exclude[f] || []) q.append('not_' + f, value);
  }
  if (v.q) q.set('q', v.q);
  if (v.sort !== 'time') q.set('sort', v.sort);
  if (v.order !== 'desc') q.set('order', v.order);
  if (v.page > 1) q.set('page', String(v.page));
  if (v.limit !== 25) q.set('limit', String(v.limit));
  if (v.groups.join() !== DEFAULT_GROUPS.join()) for (const g of v.groups) q.append('group', g);
  if (v.ip) q.set('ip_view', v.ip);
  return q;
}

// range resolves the view to absolute times. "All" starts at the oldest data known.
export function resolveRange(v, now, oldest) {
  if (v.all) {
    const start = oldest > 0 ? Math.max(oldest - 3600 * 1000, now - MAX_SPAN + 60000) : now - 24 * 3600 * 1000;
    return {start, end: now, custom: true};
  }
  if (v.start && v.end) return {start: v.start, end: Math.min(v.end, now), custom: true};
  return {start: now - v.hours * 3600 * 1000, end: now, custom: false};
}

// apiParams is the filter half of every data request: range, filters, search, time zone.
export function apiParams(v, range, tzOffsetMinutes) {
  const q = new URLSearchParams();
  q.set('start', String(Math.floor(range.start)));
  q.set('end', String(Math.floor(range.end)));
  for (const f of FIELDS) {
    for (const value of v.include[f] || []) q.append(f, value);
    for (const value of v.exclude[f] || []) q.append('not_' + f, value);
  }
  if (v.q) q.set('q', v.q);
  q.set('tz_offset', String(tzOffsetMinutes));
  return q;
}

export function addFilter(v, field, value, exclude) {
  if (!FIELDS.includes(field) || !value) return v;
  const side = exclude ? v.exclude : v.include, other = exclude ? v.include : v.exclude;
  side[field] = [...new Set([...(side[field] || []), value])];
  if (other[field]) {
    other[field] = other[field].filter(x => x !== value);
    if (!other[field].length) delete other[field];
  }
  v.page = 1;
  return v;
}

export function removeFilter(v, field, value, exclude) {
  const side = exclude ? v.exclude : v.include;
  if (side[field]) {
    side[field] = side[field].filter(x => x !== value);
    if (!side[field].length) delete side[field];
  }
  v.page = 1;
  return v;
}

export function hasFilters(v) {
  return Object.keys(v.include).length > 0 || Object.keys(v.exclude).length > 0 || !!v.q;
}
