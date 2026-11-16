// The page's view lives in the URL, so Back works and a view can be linked.
// Every filter holds one value; the API takes one value per filter too.

export const RANGES = ['1h', '6h', '24h', '7d', '30d', '90d'];
export const FILTERS = ['zone', 'qname', 'qtype', 'rcode'];

export function parseView(search) {
  const q = new URLSearchParams(search || '');
  const v = {server: q.get('server') || '', range: q.get('range') || '24h', zone: '', qname: '', qtype: '', rcode: ''};
  if (!RANGES.includes(v.range)) v.range = '24h';
  for (const f of FILTERS) v[f] = normalize(f, q.get(f) || '');
  return v;
}

export function toQuery(v) {
  const q = new URLSearchParams();
  if (v.server) q.set('server', v.server);
  q.set('range', v.range);
  for (const f of FILTERS) if (v[f]) q.set(f, v[f]);
  return q;
}

// apiParams builds the API query string; the server filter is called collector there.
export function apiParams(v, extra) {
  const q = new URLSearchParams();
  q.set('range', v.range);
  if (v.server) q.set('collector', v.server);
  for (const f of FILTERS) if (v[f]) q.set(f, v[f]);
  for (const [k, val] of Object.entries(extra || {})) q.set(k, val);
  return q.toString();
}

// counterParams drops the filters Knot's counters cannot apply.
export function counterParams(v, extra) {
  return apiParams({server: v.server, range: v.range, zone: v.zone}, extra);
}

// normalize writes names as Knot does: lowercase with a final dot; types and codes upper case.
export function normalize(field, value) {
  let s = String(value || '').trim();
  if (!s) return '';
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
  return Boolean(v.qname || v.qtype || v.rcode);
}
