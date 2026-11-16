// Text and DOM helpers for the Knot DNS page.

export function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined && text !== null) node.textContent = String(text);
  return node;
}

export function button(label, className, onClick) {
  const node = el('button', className || 'btn btn-ghost btn-sm', label);
  node.type = 'button';
  if (onClick) node.addEventListener('click', onClick);
  return node;
}

function short(n, digits) {
  return Number(n.toFixed(digits)).toString();
}

// formatCount writes 482, 48.2k, 12.4M.
export function formatCount(n) {
  if (n === null || n === undefined || !isFinite(n)) return '–';
  const a = Math.abs(n);
  if (a < 1000) return String(Math.round(n));
  if (a < 1e6) return short(n / 1e3, a < 1e4 ? 1 : 0) + 'k';
  if (a < 1e9) return short(n / 1e6, 1) + 'M';
  return short(n / 1e9, 1) + 'B';
}

// formatRate writes a per-second rate with sensible precision.
export function formatRate(n) {
  if (n === null || n === undefined || !isFinite(n)) return '–';
  if (n === 0) return '0';
  if (n < 1) return short(n, 2);
  if (n < 100) return short(n, 1);
  return formatCount(n);
}

export function formatBytes(n) {
  if (!isFinite(n)) return '–';
  const units = ['B', 'kB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (Math.abs(n) >= 1000 && i < units.length - 1) { n /= 1000; i++; }
  return short(n, n < 10 && i > 0 ? 1 : 0) + ' ' + units[i];
}

export function formatShare(part, whole) {
  if (!whole) return '–';
  const p = 100 * part / whole;
  return (p > 0 && p < 0.1 ? '<0.1' : short(p, p < 10 ? 1 : 0)) + '%';
}

const RANGE_LABELS = {'1h': 'last hour', '6h': 'last 6 hours', '24h': 'last 24 hours', '7d': 'last 7 days', '30d': 'last 30 days', '90d': 'last 90 days'};
export function rangeLabel(r) {
  return RANGE_LABELS[r] || r;
}

// span writes a duration such as 3d 4h, 5h 20m or 12m.
export function span(ms) {
  const m = Math.round(Math.abs(ms) / 60000);
  if (m < 60) return m + 'm';
  const h = Math.floor(m / 60);
  if (h < 48) return h + 'h' + (m % 60 ? ' ' + (m % 60) + 'm' : '');
  const d = Math.floor(h / 24);
  return d + 'd' + (h % 24 ? ' ' + (h % 24) + 'h' : '');
}

// until writes a future time relative to now: "in 3h 20m", or "overdue 5m".
export function until(ms, now) {
  if (!ms) return '–';
  const d = ms - now;
  return d >= 0 ? 'in ' + span(d) : 'overdue ' + span(d);
}

export function zoneName(z) {
  if (!z) return '';
  if (z === '.' || z === '(none)' || z === '(other)') return z;
  return z.endsWith('.') ? z.slice(0, -1) : z;
}

// headline says in one line where things stand.
export function headline(overview, totals, view) {
  const servers = overview?.servers?.length || 0;
  if (!servers) return 'No Knot DNS servers yet';
  const checks = overview.checks || [];
  const bad = checks.filter(c => c.level === 'bad').length;
  if (bad) return bad + (bad === 1 ? ' issue needs' : ' issues need') + ' attention';
  if (totals?.collecting) return 'Collecting, first numbers in about a minute';
  const queries = formatCount(totals?.requests || 0) + ' queries in the ' + rangeLabel(view.range);
  if (checks.length) return queries + ', ' + checks.length + (checks.length === 1 ? ' thing' : ' things') + ' to look at';
  return queries + ', all clear';
}
