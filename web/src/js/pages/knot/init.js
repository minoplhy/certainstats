import { state } from './state.js';
import { begin, get, isAbort } from './api.js';
import { parseView, writeUrl, counterParams, addFilter, removeFilter, dnstapOnly, setCustom, FILTERS } from './view.js';
import { zoneName } from './format.js';
import { renderHero, renderChecks } from './hero.js';
import { loadTraffic } from './charts.js';
import { loadDnstap } from './dnstap.js';
import { renderZones, renderZoneView } from './zones.js';

const ROLES = {
  main: 'main', empty: 'empty', refresh: 'refresh', headline: 'headline', serverCount: 'server-count',
  statRequests: 'stat-requests', statQps: 'stat-qps', statNxdomain: 'stat-nxdomain', statServfail: 'stat-servfail',
  statTransfers: 'stat-transfers', statNote: 'stat-note', server: 'server', timePicker: 'time-picker', sourceNote: 'source-note',
  checks: 'checks', checkList: 'check-list', zoneView: 'zone-view', zoneTitle: 'zone-title', zoneNote: 'zone-note',
  zoneRows: 'zone-rows', zoneClose: 'zone-close', dnstap: 'dnstap', dnstapNote: 'dnstap-note', dnstapTotal: 'dnstap-total',
  dnstapMessage: 'dnstap-message', topTabs: 'top-tabs', topList: 'top-list', topStatus: 'top-status', topFoot: 'top-foot',
  trafficTabs: 'traffic-tabs', legend: 'legend', chartMessage: 'chart-message',
  zoneCount: 'zone-count', zoneSearch: 'zone-search', zoneList: 'zone-list', zonesMessage: 'zones-message'
};

const FIELD_LABELS = {zone: 'Zone', qname: 'Name', qtype: 'Query type', rcode: 'Response code', client: 'Client network'};

// update records the view in the URL and reloads.
function update() {
  writeUrl(state.view, true);
  load();
}

// showTraffic switches the traffic chart to another counter group, alone.
function showTraffic(group) {
  if (group === state.view.chart) return;
  state.view.chart = group;
  writeUrl(state.view);
  loadTraffic(begin('traffic'), sourceNote());
}

const HOURS_KEY = 'certainstats_knot_hours';

// syncRange keeps the picker's label in step with the view (Back, zoom).
function syncRange() {
  const p = state.picker;
  if (!p) return;
  const v = state.view;
  const cur = p.getState();
  const custom = v.start && v.end ? {start: v.start, end: v.end} : null;
  const same = cur.hours === v.hours &&
    (cur.customRange ? custom && cur.customRange.start === custom.start && cur.customRange.end === custom.end : !custom);
  if (!same) p.setState({hours: v.hours, customRange: custom});
}

// zoom applies a range brushed on any chart to the whole page.
function zoom(start, end) {
  setCustom(state.view, start, end);
  syncRange();
  update();
}

function mountPicker() {
  const v = state.view;
  state.picker = window.CertainStatsTelemetry?.initCustomTimePicker(state.ui.timePicker, {
    storageKey: HOURS_KEY,
    activeHours: v.hours,
    compact: true,
    onApply: ({hours, customRange}) => {
      state.view.hours = hours;
      setCustom(state.view, customRange?.start, customRange?.end);
      update();
    }
  }) || null;
  syncRange();
}

function filter(field, value) {
  addFilter(state.view, field, value);
  state.filterMenu?.setField(field);
  update();
}

const PLACEHOLDERS = {zone: 'e.g. example.com', qname: 'e.g. www.example.com', qtype: 'e.g. AAAA', rcode: 'e.g. NXDOMAIN', client: 'e.g. 192.0.2.0/24'};

function renderChips() {
  const v = state.view;
  const chips = FILTERS.filter(f => v[f]).map(f => ({
    label: FIELD_LABELS[f], value: f === 'zone' || f === 'qname' ? zoneName(v[f]) : v[f], mono: true,
    onRemove: () => { removeFilter(v, f); update(); }
  }));
  window.CertainStatsTelemetry?.renderFilterChips(state.ui.chips, chips, {onClear: () => { for (const f of FILTERS) removeFilter(v, f); update(); }});
}

function renderServers() {
  const sel = state.ui.server;
  const servers = state.overview?.servers || [];
  const want = [''].concat(servers.map(s => s.collector_id));
  const have = [...sel.options].map(o => o.value);
  if (want.join('\n') !== have.join('\n')) {
    sel.replaceChildren(new Option('All servers', ''), ...servers.map(s => new Option(s.name, s.collector_id)));
  }
  if (state.view.server && !servers.some(s => s.collector_id === state.view.server)) state.view.server = '';
  sel.value = state.view.server;
}

// sourceNote explains which source answers the current filters.
function sourceNote() {
  const v = state.view;
  const servers = state.overview?.servers || [];
  const zoneStats = servers.some(s => s.zone_stats);
  const dnstap = servers.some(s => s.dnstap || s.last_dnstap_ms);
  const notes = [];
  let chartNote = '';
  if (v.zone && !zoneStats) {
    chartNote = 'Per-zone counters are off, so charts cannot split by zone. Set KNOT_ZONE_STATS=true on the collector.';
    notes.push(chartNote);
  }
  if (dnstapOnly(v)) {
    notes.push('Knot\'s counters cannot filter by name, query type, response code or client network, so the charts above show all traffic' + (v.zone ? ' for the zone' : '') + '. ' +
      (dnstap ? 'Queries from dnstap below answer the filtered question.' : 'Switch on dnstap (KNOT_DNSTAP=true) to filter by these.'));
  }
  if (v.client && dnstap && !servers.some(s => s.dnstap?.clients)) {
    notes.push('No server counts client networks, so nothing matches a client filter. Set KNOT_DNSTAP_CLIENTS=true on the collector.');
  }
  state.ui.sourceNote.hidden = !notes.length;
  state.ui.sourceNote.textContent = notes.join(' ');
  return chartNote;
}

export async function load() {
  const ui = state.ui;
  const signal = begin('load');
  renderChips();
  syncRange();
  try {
    state.overview = await get('', signal);
  } catch (e) {
    if (isAbort(e)) return;
    ui.headline.textContent = 'Could not load Knot DNS: ' + e.message;
    return;
  }
  const servers = state.overview.servers;
  ui.empty.hidden = servers.length > 0;
  ui.main.hidden = servers.length === 0;
  renderServers();
  renderChecks();
  if (!servers.length) {
    renderHero(null);
    return;
  }
  const chartNote = sourceNote();
  const v = state.view;
  const tasks = [
    get('totals?' + counterParams(v), signal).then(t => renderHero(chartNote ? null : t)).catch(e => { if (!isAbort(e)) ui.statNote.textContent = 'Could not load totals: ' + e.message; }),
    loadTraffic(signal, chartNote),
    loadDnstap(signal, filter),
    get('zones', signal).then(z => { state.zones = z.zones; renderZones(openZone); }).catch(e => { if (!isAbort(e)) ui.zonesMessage.textContent = 'Could not load zones: ' + e.message; })
  ];
  ui.zoneView.hidden = !v.zone || v.zone === '(none)';
  if (!ui.zoneView.hidden) {
    tasks.push(get('zones/' + encodeURIComponent(v.zone), signal).then(renderZoneView).catch(e => {
      if (isAbort(e)) return;
      ui.zoneTitle.textContent = zoneName(v.zone);
      ui.zoneNote.textContent = e.status === 404 ? 'No server reports this zone.' : 'Could not load: ' + e.message;
      ui.zoneRows.replaceChildren();
    }));
  }
  await Promise.all(tasks);
  state.loaded = true;
}

// arrowTabs moves between tabs with the arrow keys, as a tab list does.
function arrowTabs(e) {
  if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
  const list = [...e.currentTarget.querySelectorAll('[role="tab"]')];
  const at = list.indexOf(document.activeElement);
  if (at < 0) return;
  const next = list[(at + (e.key === 'ArrowRight' ? 1 : list.length - 1)) % list.length];
  next.focus();
  next.click();
  e.preventDefault();
}

function openZone(zone) {
  filter('zone', zone);
  window.scrollTo({top: 0});
}

export function init(root, config) {
  state.panelPath = (document.body?.getAttribute('data-panel-path') || '').replace(/\/+$/, '');
  let remembered = 24;
  try { remembered = Number.parseInt(localStorage.getItem(HOURS_KEY) || '24', 10) || 24; } catch (e) { /* storage blocked */ }
  state.view = parseView(window.location.search, remembered);
  state.onZoom = zoom;
  for (const [key, role] of Object.entries(ROLES)) state.ui[key] = root.querySelector('[data-kn-' + role + ']');
  const ui = state.ui;
  ui.refresh.addEventListener('click', load);
  ui.server.addEventListener('change', () => { state.view.server = ui.server.value; update(); });
  mountPicker();
  ui.filterMenu = root.querySelector('[data-filter-menu]');
  ui.chips = root.querySelector('[data-filter-chips]');
  state.filterMenu = window.CertainStatsTelemetry?.createFilterMenu(ui.filterMenu, {
    fields: FILTERS.map(f => ({value: f, label: FIELD_LABELS[f], placeholder: PLACEHOLDERS[f]})),
    exclude: false,
    onAdd: (field, value) => filter(field, value)
  });
  ui.trafficTabs.addEventListener('click', e => {
    const tab = e.target.closest('[data-kn-traffic-tab]');
    if (tab) showTraffic(tab.dataset.knTrafficTab);
  });
  for (const box of [ui.trafficTabs, ui.topTabs]) box.addEventListener('keydown', arrowTabs);
  ui.zoneClose.addEventListener('click', () => { removeFilter(state.view, 'zone'); update(); });
  ui.zoneSearch.addEventListener('input', () => renderZones(openZone));
  window.addEventListener('popstate', () => { state.view = parseView(window.location.search, state.view.hours); load(); });
  // Counters arrive every minute; refresh while the page is visible.
  setInterval(() => { if (!document.hidden && state.loaded) load(); }, 60000);
  load();
}
