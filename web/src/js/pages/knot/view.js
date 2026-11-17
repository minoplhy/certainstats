// The page's view lives in the URL, so Back works and a view can be linked.
// The time range uses the keys every chart page uses: hours, or start and end
// in Unix ms for a custom or zoomed range. Every filter holds one value; the
// API takes one value per filter too.
import { TIME_RANGES } from '../../lib/time_ranges.js';

export const FILTERS = ['zone', 'qname', 'qtype', 'rcode', 'client'];

// The traffic chart shows one counter group, the dnstap list one dimension;
// both are page choices, kept in the URL but never sent to the API as filters.
export const CHART_GROUPS = ['protocol', 'rcode', 'qtype', 'operation', 'bytes'];
export const TOP_DIMS = ['qname', 'qtype', 'rcode', 'zone', 'size', 'client'];

// Links from before the shared picker said range=7d.
const LEGACY_RANGES = {'1h': 1, '6h': 6, '24h': 24, '7d': 168, '30d': 720, '90d': 2160};

function quick(hours) {
  return TIME_RANGES.some(r => r.value === hours);
}

function int(value) {
  const n = Number.parseInt(value || '', 10);
  return Number.isFinite(n) ? n : 0;
}

export function parseView(search, fallbackHours = 24) {
  const q = new URLSearchParams(search || '');
  const v = {server: q.get('server') || '', hours: quick(fallbackHours) ? fallbackHours : 24, start: null, end: null, zone: '', qname: '', qtype: '', rcode: '', client: '',
    chart: CHART_GROUPS.includes(q.get('chart')) ? q.get('chart') : CHART_GROUPS[0],
    top: TOP_DIMS.includes(q.get('top')) ? q.get('top') : TOP_DIMS[0]};
  const hours = int(q.get('hours')) || LEGACY_RANGES[q.get('range')] || 0;
  if (quick(hours)) v.hours = hours;
  const start = int(q.get('start')), end = int(q.get('end'));
  if (start > 0 && end > start) { v.start = start; v.end = end; }
  for (const f of FILTERS) v[f] = normalize(f, q.get(f) || '');
  return v;
}

// setRange writes the time range to q: start and end when custom, else hours.
function setRange(q, v) {
  if (v.start && v.end) { q.set('start', String(v.start)); q.set('end', String(v.end)); }
  else q.set('hours', String(v.hours));
}

export function toQuery(v) {
  const q = new URLSearchParams();
  if (v.server) q.set('server', v.server);
  setRange(q, v);
  for (const f of FILTERS) if (v[f]) q.set(f, v[f]);
  if (v.chart && v.chart !== CHART_GROUPS[0]) q.set('chart', v.chart);
  if (v.top && v.top !== TOP_DIMS[0]) q.set('top', v.top);
  return q;
}

// writeUrl records the view in the address bar: push for a new view Back
// should return to, replace for a choice of what to show.
export function writeUrl(v, push = false) {
  const query = toQuery(v).toString();
  const url = window.location.pathname + (query ? '?' + query : '');
  if (url === window.location.pathname + window.location.search) return;
  if (push) history.pushState({}, '', url);
  else history.replaceState({}, '', url);
}

// apiParams builds the API query string; the server filter is called collector there.
export function apiParams(v, extra) {
  const q = new URLSearchParams();
  setRange(q, v);
  if (v.server) q.set('collector', v.server);
  for (const f of FILTERS) if (v[f]) q.set(f, v[f]);
  for (const [k, val] of Object.entries(extra || {})) q.set(k, val);
  return q.toString();
}

// setCustom applies a custom or zoomed range; clearing it returns to hours.
export function setCustom(v, start, end) {
  if (start && end && end > start) { v.start = Math.floor(start); v.end = Math.floor(end); }
  else { v.start = null; v.end = null; }
  return v;
}

// counterParams drops the filters Knot's counters cannot apply.
export function counterParams(v, extra) {
  return apiParams({server: v.server, hours: v.hours, start: v.start, end: v.end, zone: v.zone}, extra);
}

// normalize writes names as Knot does: lowercase with a final dot; types and codes
// upper case; client networks (an address or CIDR) lowercase.
export function normalize(field, value) {
  let s = String(value || '').trim();
  if (!s) return '';
  if (field === 'client') return s.toLowerCase();
  if (field === 'zone' || field === 'qname') {
    s = s.toLowerCase();
    if (s !== '(none)' && s !== '(other)' && !s.endsWith('.')) s += '.';
    return s;
  }
  return s.toUpperCase();
}

export function addFilter(v, field, value) {
  if (!FILTERS.includes(field)) return v;
  v[field] = normalize(field, value);
  return v;
}

export function removeFilter(v, field) {
  if (FILTERS.includes(field)) v[field] = '';
  return v;
}

// dnstapOnly reports whether the view filters on something only dnstap can answer.
export function dnstapOnly(v) {
  return Boolean(v.qname || v.qtype || v.rcode || v.client);
}
