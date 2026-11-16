import { state } from './state.js';
import { el, button, until, zoneName } from './format.js';

// MAX_ROWS caps the zone list; search narrows it.
export const MAX_ROWS = 500;

// zoneStatus sums up a zone across servers: level is ok, warn or bad.
export function zoneStatus(z, now) {
  const expired = z.servers.some(s => s.role === 'secondary' && s.expire_at && s.expire_at <= now);
  if (expired) return {level: 'bad', text: 'Expired'};
  if (z.servers.some(s => s.error)) return {level: 'warn', text: 'Error'};
  if (z.drift) return {level: 'warn', text: z.servers.filter(s => s.behind).length + ' behind'};
  if (z.expire_at && z.expire_at - now < 86400000) return {level: 'warn', text: 'Expiring'};
  if (z.servers.some(s => s.frozen)) return {level: 'warn', text: 'Frozen'};
  return {level: 'ok', text: 'In sync'};
}

export function roles(z) {
  const p = z.servers.filter(s => s.role === 'primary').length;
  const s = z.servers.filter(s => s.role === 'secondary').length;
  const parts = [];
  if (p) parts.push(p + (p === 1 ? ' primary' : ' primaries'));
  if (s) parts.push(s + (s === 1 ? ' secondary' : ' secondaries'));
  return parts.join(', ') || z.servers.length + (z.servers.length === 1 ? ' server' : ' servers');
}

function dot(level) {
  const d = el('span', 'status-dot' + (level === 'ok' ? ' online' : level === 'bad' ? ' offline' : ' warn'));
  d.setAttribute('aria-hidden', 'true');
  return d;
}

export function renderZones(onOpen) {
  const ui = state.ui;
  const now = state.overview?.now || Date.now();
  const term = ui.zoneSearch.value.trim().toLowerCase();
  const list = term ? state.zones.filter(z => z.zone.includes(term)) : state.zones;
  ui.zoneCount.textContent = state.zones.length ? String(state.zones.length) : '';
  ui.zoneList.replaceChildren(...list.slice(0, MAX_ROWS).map(z => {
    const row = el('div', 'kn-zones-row');
    row.setAttribute('role', 'row');
    const name = button(zoneName(z.zone), 'kn-link', () => onOpen(z.zone));
    name.title = 'Show ' + zoneName(z.zone);
    const st = zoneStatus(z, now);
    const status = el('span', 'kn-status');
    status.append(dot(st.level), document.createTextNode(st.text));
    const serial = z.primary_serial === null || z.primary_serial === undefined ? '–' : String(z.primary_serial);
    for (const node of [name, el('span', 'muted', roles(z)), el('span', 'mono', serial),
      el('span', 'mono', z.expire_at ? until(z.expire_at, now) : '–'), status]) {
      node.setAttribute('role', 'cell');
      row.append(node);
    }
    return row;
  }));
  let msg = '';
  if (!state.zones.length) msg = 'No zones reported yet.';
  else if (!list.length) msg = 'No zone matches.';
  else if (list.length > MAX_ROWS) msg = 'Showing ' + MAX_ROWS + ' of ' + list.length + ' zones; search to narrow.';
  ui.zonesMessage.textContent = msg;
}

// renderZoneView shows one zone on every server that serves it.
export function renderZoneView(data) {
  const ui = state.ui;
  const now = state.overview?.now || Date.now();
  const z = data.zone;
  ui.zoneTitle.textContent = zoneName(z.zone);
  const st = zoneStatus(z, now);
  ui.zoneNote.textContent = st.text + (data.zone_stats ? '' : ' · per-zone counters are off (KNOT_ZONE_STATS=true), so charts cannot split this zone; dnstap below can');
  ui.zoneRows.replaceChildren(...z.servers.map(s => {
    const row = el('div', 'kn-zone-row');
    row.setAttribute('role', 'row');
    const serial = s.has_serial ? String(s.serial) + (s.behind ? ' (behind)' : '') : '–';
    const cells = [el('span', '', s.server), el('span', 'muted', s.role || '–'), el('span', 'mono' + (s.behind ? ' kn-warn' : ''), serial),
      el('span', 'mono', until(s.refresh_at, now)), el('span', 'mono', s.role === 'secondary' ? until(s.expire_at, now) : '–'),
      el('span', 'mono', until(s.resign_at, now))];
    for (const c of cells) {
      c.setAttribute('role', 'cell');
      row.append(c);
    }
    if (s.error) {
      const err = el('p', 'kn-row-error', s.error);
      row.append(err);
    }
    return row;
  }));
}
