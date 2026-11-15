// Small helpers shared by the Collectors page.

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

export function panelPath() {
  return (document.body?.getAttribute('data-panel-path') || '').replace(/\/+$/, '');
}

// JSON request to the panel API; errors carry the server's message.
export async function api(path, init = {}) {
  const headers = init.body ? {'Content-Type': 'application/json'} : undefined;
  const response = await fetch(panelPath() + '/api/' + path, {...init, headers});
  if (response.status === 204) return null;
  let body = null;
  try { body = await response.json(); } catch (e) { /* not JSON: use the generic message */ }
  if (!response.ok) {
    const error = new Error(body?.error || 'Request failed (' + response.status + ')');
    error.status = response.status;
    throw error;
  }
  return body;
}

export function timeAgo(ms, now = Date.now()) {
  if (!ms) return 'never';
  const s = Math.max(0, Math.round((now - ms) / 1000));
  if (s < 60) return 'just now';
  if (s < 3600) return Math.floor(s / 60) + 'm ago';
  if (s < 86400) return Math.floor(s / 3600) + 'h ago';
  return Math.floor(s / 86400) + 'd ago';
}

// typeState says, in words, where one type of one collector stands.
// level is 'ok', 'warn' or 'bad', for the status dot.
export function typeState(collector, type) {
  const env = 'COLLECT_' + type.type.toUpperCase();
  if (!type.enabled) return {level: 'warn', text: 'Disabled'};
  if (!collector.connected) {
    if (!collector.first_connected_at) return {level: 'warn', text: 'Waiting for first connection'};
    return {level: 'bad', text: 'Offline, last seen ' + timeAgo(collector.last_seen_at)};
  }
  if (!type.reported_on) return {level: 'warn', text: 'Connected, ' + type.label + ' not switched on (' + env + '=true)'};
  if (type.status === 'error') return {level: 'bad', text: 'Connected, ' + (type.error || 'reporting an error')};
  if (type.status === 'ok') return {level: 'ok', text: 'Reporting'};
  return {level: 'warn', text: 'Connected, waiting for first report'};
}

export async function copy(text, label) {
  try {
    await navigator.clipboard.writeText(text);
    window.CertainStatsTelemetry?.showToast((label || 'Value') + ' copied', true);
  } catch (e) {
    window.CertainStatsTelemetry?.showToast('Copy failed; select the text and copy it by hand', false);
  }
}
