import { ATTENTION_STATES } from './constants.js';

export function el(tag, className, text) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (text != null) element.textContent = text;
  return element;
}

export function formatMs(value) {
  if (!Number.isFinite(value)) return 'Unavailable';
  return value.toLocaleString(undefined, { maximumFractionDigits: 2 }) + ' ms';
}

export function formatPercent(value) {
  return Number.isFinite(value) ? value.toFixed(value >= 10 ? 1 : 2) + '%' : '–';
}

export function plural(count, word) {
  return count + ' ' + word + (count === 1 ? '' : 's');
}

// Compact relative time, e.g. "45s ago" or "3d ago".
export function timeAgo(timestamp) {
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000));
  if (seconds < 60) return seconds + 's ago';
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return minutes + 'm ago';
  const hours = Math.round(minutes / 60);
  if (hours < 48) return hours + 'h ago';
  return Math.round(hours / 24) + 'd ago';
}

export function daysUntil(timestamp) {
  const days = Math.floor((timestamp - Date.now()) / 86400000);
  if (days < 0) return 'expired';
  return days === 0 ? 'today' : 'in ' + plural(days, 'day');
}

export function supports(agent, feature) {
  const capability = agent.extensions?.runtime?.capabilities?.['network_monitor.' + feature];
  return capability?.state === 'supported';
}

// Stable per-monitor colour, so a series keeps its colour across refreshes.
export function seriesColor(monitorId) {
  let hash = 0;
  for (const char of monitorId) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  return 'hsl(' + (hash % 360) + ' 65% 50%)';
}

export function probeDetails(monitor) {
  return (monitor.port ? ' · port ' + monitor.port : '') + (monitor.dns_server ? ' · DNS ' + monitor.dns_server : '');
}

export function probeLine(monitor) {
  return monitor.protocol.toUpperCase() + (monitor.port ? ' :' + monitor.port : '') +
    ' · every ' + monitor.interval_seconds + 's' + (monitor.dns_server ? ' · DNS ' + monitor.dns_server : '');
}

export function seriesLabel(monitor) {
  return monitor.agent_name + ' · ' + monitor.target + ' · ' + monitor.protocol.toUpperCase() +
    (monitor.port ? ':' + monitor.port : '') + (monitor.dns_server ? ' · DNS ' + monitor.dns_server : '');
}

export function needsAttention(monitor) {
  if (!monitor.enabled) return false;
  if (ATTENTION_STATES.includes(monitor.state)) return true;
  return monitor.state === 'active' && monitor.latest?.loss_pct >= 100;
}

// Badge text, badge class and status dot class for a monitor row.
export function describeState(monitor) {
  if (monitor.archived_at) return { label: 'Archived', badge: 'badge', dot: '' };
  if (!monitor.enabled || monitor.state === 'paused') return { label: 'Paused', badge: 'badge', dot: '' };
  const latest = monitor.latest;
  switch (monitor.state) {
    case 'active':
      if (latest?.loss_pct >= 100) return { label: 'Failing', badge: 'badge-offline', dot: 'offline' };
      if (latest?.loss_1h_pct > 0) return { label: 'Degraded', badge: 'badge-warn', dot: 'warn' };
      return { label: 'Healthy', badge: 'badge-online', dot: 'online' };
    case 'stale':
      return { label: 'Stale', badge: 'badge-warn', dot: 'warn' };
    case 'offline':
      return { label: 'Agent offline', badge: 'badge-offline', dot: 'offline' };
    case 'unsupported':
      return { label: 'Unsupported', badge: 'badge-warn', dot: 'warn' };
    case 'unknown':
      return { label: 'Unknown', badge: 'badge-warn', dot: 'warn' };
    default:
      return { label: 'Waiting', badge: 'badge', dot: '' };
  }
}

export function button(text, onClick, className = 'btn btn-secondary btn-sm') {
  const element = el('button', className, text);
  element.type = 'button';
  element.addEventListener('click', onClick);
  return element;
}

export function median(values) {
  if (!values.length) return NaN;
  const sorted = [...values].sort((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}
