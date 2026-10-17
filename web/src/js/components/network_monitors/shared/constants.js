export const PAGE_SIZE = 25;
// The graphs show the first matches in list order, independent of pagination.
export const GRAPH_LIMIT = 10;
export const SEARCH_DEBOUNCE_MS = 350;
export const POLL_INTERVAL_MS = 10000;
// Rows fall back to HTTP polling when the live feed has been quiet this long.
export const STALE_PULSE_MS = 45000;
export const DEFAULT_HOURS = 6;
// Row sparklines: 24h of history, refetched at most this often, requests staggered.
export const SPARK_HOURS = 24;
export const SPARK_MAX_AGE_MS = 5 * 60 * 1000;
export const SPARK_STAGGER_MS = 60;
// States in which an enabled monitor needs attention in the hero headline.
export const ATTENTION_STATES = ['offline', 'stale', 'unsupported', 'unknown'];

export const ROLES = [
  'group-state', 'group-enabled', 'add', 'search', 'node', 'node-filter', 'group', 'group-filter', 'group-message', 'state', 'refresh',
  'time-picker', 'chart-count', 'chart-message', 'latency', 'loss', 'legend',
  'count', 'message', 'list', 'prev', 'next', 'limit',
  'dialog', 'form', 'form-title', 'form-subtitle', 'form-message', 'protocol-hint', 'port', 'server',
  'agent-options', 'agent-count', 'agent-actions', 'agent-hint', 'agents-all', 'agents-none', 'enabled-pill', 'submit'
];
// Editor protocol choices: what each probe measures and an example target.
export const PROTOCOLS = {
  icmp: { hint: 'Ping round-trip time and packet loss.', placeholder: '1.1.1.1 or host.example.com' },
  tcp: { hint: 'Time to open a TCP connection to the port.', placeholder: 'db.example.com' },
  http: { hint: 'HTTP(S) request time. HTTPS targets also report their TLS certificate.', placeholder: 'https://example.com/health' },
  dns: { hint: 'Time to resolve the hostname, optionally through a specific DNS server.', placeholder: 'example.com' }
};
export const HERO_ROLES = ['count', 'headline', 'enabled', 'paused', 'response', 'loss', 'loss-target', 'sync'];
