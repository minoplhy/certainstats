export function formatBytes(bytes) {
  if (bytes == null || !Number.isFinite(bytes)) return 'Unavailable';
  if (bytes === 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let v = bytes, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return v.toFixed(1) + ' ' + (units[i] || 'TiB');
}

export function formatBps(bps) {
  if (bps == null || !Number.isFinite(bps)) return 'Unavailable';
  if (bps === 0) return '0 B/s';
  const units = ['B/s', 'KiB/s', 'MiB/s', 'GiB/s'];
  let v = bps, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return v.toFixed(1) + ' ' + (units[i] || 'GiB/s');
}

export function formatTimeAxis(timestamp, spanMs) {
  const d = new Date(timestamp);
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const pad = (n) => (n < 10 ? '0' + n : n);

  if (spanMs <= 24 * 3600 * 1000) {
    return pad(d.getHours()) + ':' + pad(d.getMinutes());
  }
  if (spanMs <= 7 * 86400 * 1000) {
    return months[d.getMonth()] + ' ' + d.getDate() + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
  }
  if (spanMs <= 90 * 86400 * 1000) {
    return months[d.getMonth()] + ' ' + d.getDate();
  }
  return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
}

export function formatDuration(ms) {
  const m = Math.round(ms / 60000);
  if (m < 60) return m + 'm';
  const h = Math.floor(m / 60);
  if (h < 48) return h + 'h' + (m % 60 ? ' ' + (m % 60) + 'm' : '');
  return Math.round(h / 24) + 'd';
}
