import { state } from './state.js';
import { begin, get, isAbort, showError } from './api.js';
import { parseView, toQuery, resolveRange, apiParams, addFilter } from './url/state.js';
import { formatTime } from './format.js';
import { renderHero } from './panels/hero.js';
import { renderTimeline } from './panels/timeline.js';
import { buildCards, renderCard, CARD_LIMIT } from './panels/breakdowns.js';
import { renderChips, bindFilters, syncRangeControls } from './filters/bar.js';
import { renderHead, renderRows, bindPaging } from './table/table.js';
import { bindIPView, loadIPView } from './ip/view.js';
import { loadComponents, bindComponents } from './panels/components.js';

const ROLES = {
  collectorCount: 'collector-count', headline: 'headline', statAlerts: 'stat-alerts', statIps: 'stat-ips', statBans: 'stat-bans',
  statCollectors: 'stat-collectors', refresh: 'refresh', empty: 'empty', components: 'components', componentsCount: 'components-count',
  componentsMessage: 'components-message', componentsNotes: 'components-notes', componentsStat: 'components-stat', componentsOpen: 'components-open',
  machines: 'machines', machinesCount: 'machines-count', machinesEmpty: 'machines-empty', bouncers: 'bouncers', bouncersCount: 'bouncers-count', bouncersEmpty: 'bouncers-empty',
  main: 'main', search: 'search', timePicker: 'time-picker', expired: 'expired',
  timelineNote: 'timeline-note', timelineAlerts: 'timeline-alerts', timelineDecisions: 'timeline-decisions',
  timelineMessage: 'timeline-message', breakdowns: 'breakdowns', count: 'count', tableMessage: 'table-message', tableHead: 'table-head',
  rows: 'rows', page: 'page', prev: 'prev', next: 'next', limit: 'limit', ip: 'ip', ipBack: 'ip-back', ipTitle: 'ip-title',
  ipSubtitle: 'ip-subtitle', ipFirst: 'ip-first', ipLast: 'ip-last', ipAlerts: 'ip-alerts', ipEvents: 'ip-events', ipFilter: 'ip-filter',
  ipCopy: 'ip-copy', ipMessage: 'ip-message', ipRange: 'ip-range', ipScenarios: 'ip-scenarios', ipMachines: 'ip-machines',
  ipDecisions: 'ip-decisions', ipDecisionCount: 'ip-decision-count'
};

const tzOffset = () => -new Date().getTimezoneOffset();

// update records the view in the URL (so Back works and the view can be linked) and reloads.
function update() {
  const query = toQuery(state.view).toString();
  const url = window.location.pathname + (query ? '?' + query : '');
  if (url !== window.location.pathname + window.location.search) history.pushState({}, '', url);
  load();
}

const actions = {
  filter(field, value, exclude = false) {
    addFilter(state.view, field, value, exclude);
    // The "+ Filter" menu opens on the last field used.
    state.filterMenu?.setField(field);
    update();
  },
  openIP(ip) { state.view.ip = ip; update(); window.scrollTo({top: 0}); },
  closeIP() { state.view.ip = ''; update(); },
  filterIP(ip) { state.view.ip = ''; addFilter(state.view, 'ip', ip, false); update(); }
};

function showExpired(...coverages) {
  const before = Math.max(0, ...coverages.map(c => c?.detail_expired_before || 0));
  state.ui.expired.hidden = !before;
  state.ui.expired.textContent = before ? 'Alert detail before ' + formatTime(before) + ' has passed raw retention. Counts and charts still cover it; IP-level detail does not.' : '';
}

async function loadSummary(params, signal) {
  const [summary, top] = await Promise.all([get('summary?' + params, signal), get('groups/scenario?limit=1&' + params, signal)]);
  summary.topScenario = top.items[0]?.value || '';
  state.summary = summary;
  renderHero();
  return summary;
}

async function loadCards(params, signal) {
  // One request per field, in parallel, like every other per-resource read.
  await Promise.all(state.view.groups.map(async (field, slot) => {
    try {
      renderCard(slot, await get('groups/' + field + '?limit=' + CARD_LIMIT + '&' + params, signal), actions.filter);
    } catch (e) {
      if (!isAbort(e)) showError(state.ui.cards[slot].status, 'Could not load: ' + e.message, load);
    }
  }));
}

async function loadTable(params, signal) {
  const v = state.view;
  const q = new URLSearchParams(params);
  q.set('page', String(v.page));
  q.set('limit', String(v.limit));
  q.set('sort', v.sort);
  q.set('order', v.order);
  const page = await get('alerts?' + q, signal);
  renderRows(page, actions);
  return page;
}

export async function load() {
  const ui = state.ui;
  const v = state.view;
  renderChips(update);
  syncRangeControls();
  renderHead(update);
  ui.search.value = v.q;
  ui.ip.hidden = !v.ip;
  renderHero();
  if (v.ip) {
    loadIPView();
    return;
  }
  const controller = begin('page');
  const signal = controller.signal;
  try {
    if (v.all && !state.oldest) {
      // "All" starts at the oldest data, which any response's coverage reports.
      const now = Date.now();
      const probe = await get('summary?' + apiParams(v, {start: now - 20 * 365 * 86400000 + 60000, end: now}, tzOffset()), signal);
      state.oldest = Math.min(...[probe.raw_since, probe.rollup_since].filter(x => x > 0), now);
    }
  } catch (e) {
    if (isAbort(e)) return;
  }
  state.range = resolveRange(v, Date.now(), state.oldest);
  const params = apiParams(v, state.range, tzOffset()).toString();
  ui.timelineMessage.textContent = 'Loading…';
  ui.tableMessage.textContent = 'Loading alerts…';
  const tasks = [
    loadSummary(params, signal).catch(e => { if (!isAbort(e)) { state.summary = null; renderHero(); ui.headline.textContent = 'Alerts could not be loaded: ' + e.message; } }),
    get('timeline?' + params, signal).then(d => renderTimeline(d, zoom)).catch(e => { if (!isAbort(e)) showError(ui.timelineMessage, 'The timeline could not be loaded: ' + e.message, load); }),
    loadCards(params, signal),
    loadTable(params, signal).catch(e => { if (!isAbort(e)) showError(ui.tableMessage, 'Alerts could not be loaded: ' + e.message, load); })
  ];
  const [summary, , , page] = await Promise.all(tasks);
  if (!signal.aborted) showExpired(summary, page);
}

// zoom applies a brushed range from the timeline.
function zoom(start, end) {
  const v = state.view;
  v.all = false;
  v.start = Math.floor(start);
  v.end = Math.floor(end);
  v.page = 1;
  state.picker?.setState({hours: v.hours, customRange: {start: v.start, end: v.end}, all: false});
  update();
}

function bindCollectors() {
  const dialog = window.CertainStatsCollectors;
  if (!dialog) return;
  dialog.subscribe(items => {
    state.collectors = items;
    state.collectorsLoaded = true;
    renderHero();
  });
  dialog.refresh().catch(e => { state.ui.headline.textContent = 'Collectors could not be loaded: ' + e.message; });
  // Collector and component status change on their own; refresh them while the page is visible.
  setInterval(() => { if (!document.hidden) { dialog.refresh().catch(() => {}); loadComponents(); } }, 30000);
}

export function init(root, config) {
  state.panelPath = (document.body?.getAttribute('data-panel-path') || '').replace(/\/+$/, '');
  for (const [key, role] of Object.entries(ROLES)) state.ui[key] = root.querySelector('[data-cs-' + role + ']');
  state.ui.filterMenu = root.querySelector('[data-filter-menu]');
  state.ui.chips = root.querySelector('[data-filter-chips]');
  let remembered = 24;
  try { remembered = Number.parseInt(localStorage.getItem('certainstats_crowdsec_hours') || '24', 10) || 24; } catch (e) { /* storage blocked */ }
  state.view = parseView(window.location.search, remembered);
  buildCards((slot, field) => { state.view.groups[slot] = field; update(); });
  bindFilters(update);
  bindPaging(update);
  bindIPView(actions);
  state.ui.refresh.addEventListener('click', () => { state.oldest = 0; window.CertainStatsCollectors?.refresh().catch(() => {}); loadComponents(); load(); });
  window.addEventListener('popstate', () => {
    state.view = parseView(window.location.search, state.view.hours);
    buildCards((slot, field) => { state.view.groups[slot] = field; update(); });
    const v = state.view;
    state.picker?.setState({hours: v.hours, customRange: v.start && v.end ? {start: v.start, end: v.end} : null, all: v.all});
    load();
  });
  bindCollectors();
  bindComponents();
  loadComponents();
  load();
  return config;
}
