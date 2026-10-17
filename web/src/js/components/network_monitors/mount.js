import { PAGE_SIZE, SEARCH_DEBOUNCE_MS, POLL_INTERVAL_MS, STALE_PULSE_MS, DEFAULT_HOURS, ROLES, HERO_ROLES } from './shared/constants.js';
import { createCore } from './shared/core.js';
import { createGroups } from './list/groups.js';
import { createHero } from './panels/hero.js';
import { createAgentPicker } from './editor/agent_picker.js';
import { createDialog } from './editor/dialog.js';
import { createSave } from './editor/save.js';
import { createRows } from './list/rows.js';
import { createListRender } from './list/render.js';
import { createSparklines } from './list/sparklines.js';
import { createGraph } from './panels/graph.js';

let instanceCount = 0;

// One mounted component. Each part is a createX(ctx) factory; parts call each other through ctx.
export function mount(root, options = {}) {
  const telemetry = window.CertainStatsTelemetry;
  const panelPath = options.panelPath || telemetry.getPanelPath();
  const nodeMode = !!options.nodeMode;
  const parent = nodeMode ? root.closest('details') : null;
  const instance = ++instanceCount;

  const ui = {};
  for (const role of ROLES) ui[role] = root.querySelector('[data-network-' + role + ']');
  const protocolOptions = [...root.querySelectorAll('[data-network-protocol-option]')];
  const formProtocolOptions = [...root.querySelectorAll('[data-network-form-protocol]')];
  const intervalPresets = [...root.querySelectorAll('[data-network-interval-preset]')];
  const hero = {};
  if (options.hero) {
    for (const role of HERO_ROLES) hero[role] = options.hero.querySelector('[data-network-hero-' + role + ']');
  }
  const form = ui.form;
  const dialog = ui.dialog;

  // Element IDs must be unique because the component can be mounted twice on a page.
  ui.latency.id = 'network-latency-' + instance;
  ui.loss.id = 'network-loss-' + instance;
  if (ui['form-title']) {
    ui['form-title'].id = 'network-form-title-' + instance;
    dialog.setAttribute('aria-labelledby', ui['form-title'].id);
  }
  if (nodeMode) ui.list.classList?.add('is-node');

  // Values the parts reassign. In node mode the agent is set by setNode; until then nothing loads.
  const s = {
    nodeId: null,
    active: options.active !== false,
    destroyed: false,
    protocol: '',
    page: 1,
    limit: PAGE_SIZE,
    total: 0,
    rows: [],
    agents: [],
    editing: null,
    editingGroup: null,
    inheritedGroup: null,
    editorGeneration: 0,
    inheritancePending: false,

    candidates: [],
    historyData: null,
    charts: [],
    searchTimer: undefined,
    // Rows are keyed by monitor ID so unchanged rows survive live re-renders.
    rowElements: new Map(),
    groupTargets: [],
    hours: undefined,
    customRange: undefined
  };
  const hiddenSeries = new Set();
  const expanded = new Set();
  const collapsedGroups = new Set();
  // Targets seen in any listing, to label hero stats from the live snapshot.
  const targets = new Map();
  const sparklines = { cache: new Map(), pending: new Set(), controller: null };
  // Latest live snapshot, its membership signature, and the feed connection seen.
  const live = { snapshot: undefined, membership: undefined, generation: 0 };
  const pending = { list: null, candidates: null, history: null, groups: null };

  const ctx = {
    root, options, telemetry, panelPath, nodeMode, parent, ui, protocolOptions, formProtocolOptions, intervalPresets, hero, form, dialog,
    hiddenSeries, expanded, collapsedGroups, targets, sparklines, live, pending, state: s
  };

  const picker = telemetry.initCustomTimePicker(ui['time-picker'], {
    onApply(state) {
      s.hours = state.hours;
      s.customRange = state.customRange;
      ctx.loadHistory();
    }
  });
  ({ hours: s.hours, customRange: s.customRange } = picker.getState());
  s.hours = s.hours || DEFAULT_HOURS;

  const listeners = new AbortController();
  const on = (target, type, handler) => target.addEventListener(type, handler, { signal: listeners.signal });
  ctx.picker = picker;
  ctx.on = on;

  Object.assign(ctx,
    createCore(ctx), createGroups(ctx), createHero(ctx), createAgentPicker(ctx), createDialog(ctx), createSave(ctx),
    createRows(ctx), createListRender(ctx), createSparklines(ctx), createGraph(ctx),
    { refreshAll, filtersChanged, selectProtocol, suspend, visibilityChanged });

  function refreshAll() {
    ctx.refreshList();
    ctx.loadCandidates();
    ctx.loadGroups();
  }

  function filtersChanged() {
    s.page = 1;
    hiddenSeries.clear();
    s.historyData = null;
    ctx.abortRequests();
    refreshAll();
  }

  function selectProtocol(value) {
    s.protocol = value;
    for (const option of protocolOptions) {
      const selected = option.dataset.networkProtocolOption === value;
      option.classList?.toggle('active', selected);
      option.setAttribute('aria-pressed', String(selected));
    }
    filtersChanged();
  }

  function suspend() {
    s.editorGeneration++;
    s.inheritancePending = false;
    ctx.abortRequests();
    clearTimeout(s.searchTimer);
    if (dialog.open) dialog.close();
  }

  function visibilityChanged() {
    if (ctx.visible()) {
      ctx.renderHero();
      refreshAll();
    } else {
      suspend();
    }
  }

  on(ui.add, 'click', ctx.openAddDialog);
  for (const option of formProtocolOptions) {
    on(option, 'click', () => {
      ctx.setFormProtocol(option.dataset.networkFormProtocol);
      if (form.elements.target.value.trim()) ctx.inheritTargetSettings();
    });
  }
  for (const preset of intervalPresets) {
    on(preset, 'click', () => {
      form.elements.interval_seconds.value = preset.dataset.networkIntervalPreset;
      ctx.syncIntervalPresets();
    });
  }
  on(form.elements.interval_seconds, 'input', ctx.syncIntervalPresets);
  on(form.elements.enabled, 'change', ctx.syncEnabledPill);
  on(form.elements.dns_server, 'input', ctx.renderAgentOptions);
  on(ui['agents-all'], 'click', () => ctx.selectAllAgents(true));
  on(ui['agents-none'], 'click', () => ctx.selectAllAgents(false));
  on(form, 'submit', ctx.saveDialog);

  on(ui.search, 'input', () => {
    clearTimeout(s.searchTimer);
    s.searchTimer = setTimeout(filtersChanged, SEARCH_DEBOUNCE_MS);
  });
  for (const option of protocolOptions) {
    on(option, 'click', () => selectProtocol(option.dataset.networkProtocolOption));
  }
  on(form.elements.target, 'change', ctx.inheritTargetSettings);
  for (const key of ['node', 'state', 'group']) on(ui[key], 'change', filtersChanged);
  on(ui.limit, 'change', () => {
    s.page = 1;
    s.limit = Number(ui.limit.value);
    ctx.refreshList();
  });
  on(ui.prev, 'click', () => {
    s.page--;
    ctx.refreshList();
  });
  on(ui.next, 'click', () => {
    s.page++;
    ctx.refreshList();
  });
  on(ui.refresh, 'click', refreshAll);

  if (parent) {
    if (ui['node-filter']) ui['node-filter'].hidden = true;
    ui['group-filter'].hidden = true;
    ui['group-message'].hidden = true;
    on(parent, 'toggle', visibilityChanged);
  }
  on(document, 'visibilitychange', visibilityChanged);
  on(window, 'certainstats_feed_connected', () => { ctx.refreshList(); ctx.loadGroups(); });
  on(window, 'certainstats_theme_change', ctx.redrawSparklines);

  const unsubscribe = telemetry.subscribeNetwork?.(snapshot => {
    live.snapshot = snapshot;
    const membership = Object.keys(snapshot).sort().map(id => id + ':' + snapshot[id].enabled).join(',');
    const changed = live.membership !== undefined && live.membership !== membership;
    live.membership = membership;
    ctx.applyLiveReadings();
    if (!ctx.visible()) return;
    ctx.renderHero();
    ctx.renderList();
    // Added, removed or toggled monitors change the page; reconcile through HTTP.
    if (changed) { ctx.refreshList(); ctx.loadGroups(); }
  });

  const pollTimer = setInterval(() => {
    if (!ctx.visible()) return;
    const feed = telemetry.getNetworkFeedState?.();
    const stale = !feed?.connected || !feed.lastPulse || Date.now() - feed.lastPulse > STALE_PULSE_MS;
    // A reconnect may have missed membership changes.
    if (stale || feed.generation !== live.generation) { ctx.refreshList(); ctx.loadGroups(); }
    live.generation = feed?.generation || 0;
    ctx.loadCandidates();
  }, POLL_INTERVAL_MS);

  function destroy() {
    s.destroyed = true;
    unsubscribe?.();
    clearInterval(pollTimer);
    suspend();
    listeners.abort();
    picker.destroy();
    ctx.destroyCharts();
  }
  on(window, 'pagehide', destroy);

  ctx.renderHero();
  if (!nodeMode && s.active) {
    ctx.loadAgents().catch(() => {});
    refreshAll();
  }

  return {
    refresh: refreshAll,
    destroy,
    activate() {
      s.active = true;
      if (!s.agents.length && !nodeMode) ctx.loadAgents().catch(() => {});
      ctx.applyLiveReadings();
      ctx.renderHero();
      refreshAll();
    },
    deactivate() {
      s.active = false;
      suspend();
    },
    setNode(id) {
      suspend();
      s.nodeId = id;
      s.page = 1;
      hiddenSeries.clear();
      expanded.clear();
      s.candidates = [];
      s.historyData = null;
      s.rows = [];
      s.rowElements = new Map();
      ctx.destroyCharts();
      ui.list.replaceChildren();
      ui.legend.replaceChildren();
      ui['chart-message'].textContent = '';
      ui['chart-count'].textContent = '';
      if (parent) parent.open = false;
    }
  };
}
