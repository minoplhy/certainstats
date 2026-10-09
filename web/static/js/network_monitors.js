/* Network monitor list, graphs and editor, mounted on the Network view and in node details. */
(function () {
  'use strict';

  const PAGE_SIZE = 25;
  // The graphs show the first matches in list order, independent of pagination.
  const GRAPH_LIMIT = 10;
  const SEARCH_DEBOUNCE_MS = 350;
  const POLL_INTERVAL_MS = 10000;
  // Rows fall back to HTTP polling when the live feed has been quiet this long.
  const STALE_PULSE_MS = 45000;
  const DEFAULT_HOURS = 6;
  // Row sparklines: 24h of history, refetched at most this often, requests staggered.
  const SPARK_HOURS = 24;
  const SPARK_MAX_AGE_MS = 5 * 60 * 1000;
  const SPARK_STAGGER_MS = 60;
  // States in which an enabled monitor needs attention in the hero headline.
  const ATTENTION_STATES = ['offline', 'stale', 'unsupported', 'unknown'];

  const ROLES = [
    'add', 'search', 'node', 'node-filter', 'state', 'refresh',
    'time-picker', 'chart-count', 'chart-message', 'latency', 'loss', 'legend',
    'count', 'message', 'list', 'prev', 'next', 'limit',
    'dialog', 'form', 'form-title', 'form-subtitle', 'form-message', 'protocol-hint', 'port', 'server',
    'agent-options', 'agent-count', 'agent-actions', 'agent-hint', 'agents-all', 'agents-none', 'enabled-pill', 'submit'
  ];
  // Editor protocol choices: what each probe measures and an example target.
  const PROTOCOLS = {
    icmp: { hint: 'Ping round-trip time and packet loss.', placeholder: '1.1.1.1 or host.example.com' },
    tcp: { hint: 'Time to open a TCP connection to the port.', placeholder: 'db.example.com' },
    http: { hint: 'HTTP(S) request time. HTTPS targets also report their TLS certificate.', placeholder: 'https://example.com/health' },
    dns: { hint: 'Time to resolve the hostname, optionally through a specific DNS server.', placeholder: 'example.com' }
  };
  const HERO_ROLES = ['count', 'headline', 'enabled', 'paused', 'response', 'loss', 'loss-target', 'sync'];

  let instanceCount = 0;

  function el(tag, className, text) {
    const element = document.createElement(tag);
    if (className) element.className = className;
    if (text != null) element.textContent = text;
    return element;
  }

  function formatMs(value) {
    if (!Number.isFinite(value)) return 'Unavailable';
    return value.toLocaleString(undefined, { maximumFractionDigits: 2 }) + ' ms';
  }

  function formatPercent(value) {
    return Number.isFinite(value) ? value.toFixed(value >= 10 ? 1 : 2) + '%' : '–';
  }

  function plural(count, word) {
    return count + ' ' + word + (count === 1 ? '' : 's');
  }

  // Compact relative time, e.g. "45s ago" or "3d ago".
  function timeAgo(timestamp) {
    const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000));
    if (seconds < 60) return seconds + 's ago';
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return minutes + 'm ago';
    const hours = Math.round(minutes / 60);
    if (hours < 48) return hours + 'h ago';
    return Math.round(hours / 24) + 'd ago';
  }

  function daysUntil(timestamp) {
    const days = Math.floor((timestamp - Date.now()) / 86400000);
    if (days < 0) return 'expired';
    return days === 0 ? 'today' : 'in ' + plural(days, 'day');
  }

  function supports(agent, feature) {
    const capability = agent.extensions?.runtime?.capabilities?.['network_monitor.' + feature];
    return capability?.state === 'supported';
  }

  // Stable per-monitor colour, so a series keeps its colour across refreshes.
  function seriesColor(monitorId) {
    let hash = 0;
    for (const char of monitorId) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
    return 'hsl(' + (hash % 360) + ' 65% 50%)';
  }

  function probeDetails(monitor) {
    return (monitor.port ? ' · port ' + monitor.port : '') + (monitor.dns_server ? ' · DNS ' + monitor.dns_server : '');
  }

  function probeLine(monitor) {
    return monitor.protocol.toUpperCase() + (monitor.port ? ' :' + monitor.port : '') +
      ' · every ' + monitor.interval_seconds + 's' + (monitor.dns_server ? ' · DNS ' + monitor.dns_server : '');
  }

  function seriesLabel(monitor) {
    return monitor.agent_name + ' · ' + monitor.target + ' · ' + monitor.protocol.toUpperCase() +
      (monitor.port ? ':' + monitor.port : '') + (monitor.dns_server ? ' · DNS ' + monitor.dns_server : '');
  }

  function needsAttention(monitor) {
    if (!monitor.enabled) return false;
    if (ATTENTION_STATES.includes(monitor.state)) return true;
    return monitor.state === 'active' && monitor.latest?.loss_pct >= 100;
  }

  // Badge text, badge class and status dot class for a monitor row.
  function describeState(monitor) {
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

  function button(text, onClick, className = 'btn btn-secondary btn-sm') {
    const element = el('button', className, text);
    element.type = 'button';
    element.addEventListener('click', onClick);
    return element;
  }

  function median(values) {
    if (!values.length) return NaN;
    const sorted = [...values].sort((a, b) => a - b);
    const middle = Math.floor(sorted.length / 2);
    return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
  }

  function mount(root, options = {}) {
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

    // In node mode the agent is set by setNode; until then nothing loads.
    let nodeId = null;
    let active = options.active !== false;
    let destroyed = false;
    let protocol = '';
    let page = 1;
    let limit = PAGE_SIZE;
    let total = 0;
    let rows = [];
    let agents = [];
    let editing = null;
    let candidates = [];
    let historyData = null;
    let charts = [];
    let searchTimer;
    const hiddenSeries = new Set();
    // Rows are keyed by monitor ID so unchanged rows survive live re-renders.
    let rowElements = new Map();
    const expanded = new Set();
    // Targets seen in any listing, to label hero stats from the live snapshot.
    const targets = new Map();
    const sparklines = { cache: new Map(), pending: new Set(), controller: null };
    // Latest live snapshot, its membership signature, and the feed connection seen.
    const live = { snapshot: undefined, membership: undefined, generation: 0 };
    const pending = { list: null, candidates: null, history: null };

    const picker = telemetry.initCustomTimePicker(ui['time-picker'], {
      onApply(state) {
        hours = state.hours;
        customRange = state.customRange;
        loadHistory();
      }
    });
    let { hours, customRange } = picker.getState();
    hours = hours || DEFAULT_HOURS;

    const listeners = new AbortController();
    const on = (target, type, handler) => target.addEventListener(type, handler, { signal: listeners.signal });

    function visible() {
      if (destroyed || !active || document.hidden || root.closest('[hidden]')) return false;
      return !parent || (parent.open && !!nodeId);
    }

    async function api(path, init = {}) {
      const headers = init.body ? { 'Content-Type': 'application/json' } : undefined;
      const response = await fetch(panelPath + '/api/' + path, { ...init, headers });
      if (response.status === 204) return null;
      let body = null;
      try {
        body = await response.json();
      } catch (e) {
        // Error pages may not be JSON; fall through to the generic message.
      }
      if (!response.ok) throw new Error(body?.error || body?.message || 'Request failed');
      return body;
    }

    function abortRequests() {
      for (const key of Object.keys(pending)) {
        pending[key]?.abort();
        pending[key] = null;
      }
      sparklines.controller?.abort();
      sparklines.controller = null;
      sparklines.pending.clear();
    }

    function begin(key) {
      pending[key]?.abort();
      const controller = new AbortController();
      pending[key] = controller;
      return controller;
    }

    function showError(target, message, retry) {
      target.replaceChildren(el('span', '', message + ' '), button('Retry', retry));
    }

    function agentFilter() {
      return nodeMode ? nodeId : ui.node.value;
    }

    function hasFilters() {
      return !!(ui.search.value.trim() || protocol || ui.state.value || (!nodeMode && ui.node.value));
    }

    function filterParams() {
      const params = new URLSearchParams({
        state: ui.state.value,
        target: ui.search.value.trim(),
        protocol
      });
      if (agentFilter()) params.set('agent_id', agentFilter());
      return params;
    }

    function rememberTargets(monitors) {
      for (const monitor of monitors) targets.set(monitor.monitor_id, monitor.target);
    }

    // --- Hero ---

    function setHero(role, text) {
      if (hero[role]) hero[role].textContent = text;
    }

    function renderHero() {
      if (!options.hero) return;
      const snapshot = live.snapshot;
      if (!snapshot) {
        setHero('headline', 'Checking monitors…');
        for (const role of ['enabled', 'response', 'loss', 'sync']) setHero(role, '–');
        setHero('paused', '');
        setHero('loss-target', '');
        return;
      }

      const monitors = Object.entries(snapshot).map(([id, monitor]) => ({ monitor_id: id, ...monitor }));
      const enabled = monitors.filter(monitor => monitor.enabled);
      const attention = enabled.filter(needsAttention).length;
      setHero('count', plural(monitors.length, 'monitor'));

      const headline = hero.headline;
      if (headline) {
        if (!monitors.length) {
          headline.replaceChildren(document.createTextNode('Add a monitor to start probing.'));
        } else if (!attention) {
          const text = enabled.length === 1 ? 'Your monitor is healthy.' : 'All ' + enabled.length + ' monitors are healthy.';
          headline.replaceChildren(document.createTextNode(enabled.length ? text : 'All monitors are paused.'));
        } else {
          const bad = el('span', 'is-bad', plural(attention, 'monitor') + (attention === 1 ? ' needs' : ' need') + ' attention.');
          const healthy = enabled.length - attention;
          headline.replaceChildren(bad);
          if (healthy) headline.append(document.createTextNode(' The other ' + healthy + (healthy === 1 ? ' is' : ' are') + ' healthy.'));
        }
      }

      const paused = monitors.length - enabled.length;
      setHero('enabled', String(enabled.length));
      setHero('paused', paused ? paused + ' paused' : '');

      const responses = enabled
        .filter(monitor => monitor.latest?.success_count > 0 && Number.isFinite(monitor.latest.response_avg_ms))
        .map(monitor => monitor.latest.response_avg_ms);
      setHero('response', responses.length ? formatMs(median(responses)) : '–');

      let worst = null;
      for (const monitor of enabled) {
        const loss = monitor.latest?.loss_1h_pct;
        if (Number.isFinite(loss) && (!worst || loss > worst.latest.loss_1h_pct)) worst = monitor;
      }
      setHero('loss', worst ? formatPercent(worst.latest.loss_1h_pct) : '–');
      setHero('loss-target', worst && worst.latest.loss_1h_pct > 0 ? (targets.get(worst.monitor_id) || '') : '');

      const syncing = monitors.filter(monitor => monitor.sync && monitor.sync.desired_generation > monitor.sync.acknowledged_generation).length;
      setHero('sync', String(syncing));
    }

    // --- Editor dialog ---

    function setFormMessage(text) {
      ui['form-message'].textContent = text;
      ui['form-message'].hidden = !text;
    }

    async function loadAgents(signal) {
      agents = await api('agents', { signal });
      const selected = ui.node.value;
      const all = el('option', '', 'All nodes');
      all.value = '';
      ui.node.replaceChildren(all);
      for (const agent of agents) {
        const option = el('option', '', agent.nickname || agent.agent_id);
        option.value = agent.agent_id;
        ui.node.append(option);
      }
      ui.node.value = selected;
    }

    function agentCheckboxes() {
      return [...ui['agent-options'].querySelectorAll('input[name=agent_ids]')];
    }

    // Updates the selected count and the submit label, e.g. "Add to 3 agents".
    function updateAgentSummary() {
      const selected = agentCheckboxes().filter(input => input.checked).length;
      ui['agent-count'].textContent = editing ? '' : selected + ' selected';
      ui.submit.textContent = editing ? 'Save changes' : selected > 1 ? 'Add to ' + selected + ' agents' : 'Add monitor';
    }

    function renderAgentOptions() {
      const selectedProtocol = form.elements.protocol.value;
      const customServer = form.elements.dns_server.value.trim();
      const group = ui['agent-options'];
      const checked = new Set(agentCheckboxes().filter(input => input.checked).map(input => input.value));
      group.replaceChildren();

      for (const agent of agents) {
        if (nodeMode && agent.agent_id !== nodeId) continue;
        if (editing && agent.agent_id !== editing.agent_id) continue;
        const capable = supports(agent, 'configure') && supports(agent, selectedProtocol) &&
          (!customServer || supports(agent, 'custom_dns_server'));
        const runtime = agent.extensions?.runtime;
        const reason = capable ? '' : (runtime?.capabilities?.['network_monitor.configure']?.message || 'Can\'t run ' + selectedProtocol.toUpperCase() + ' probes');

        const checkbox = el('input');
        checkbox.type = 'checkbox';
        checkbox.name = 'agent_ids';
        checkbox.value = agent.agent_id;
        checkbox.disabled = !capable || !!editing;
        checkbox.checked = capable && (editing || nodeMode || checked.has(agent.agent_id));

        const pill = el('label', 'node-select-pill' + (checkbox.checked ? ' active' : '') + (capable ? '' : ' is-disabled'));
        if (reason) pill.title = reason;
        const text = el('span', 'node-select-text');
        text.append(
          el('strong', '', agent.nickname || agent.agent_id),
          el('span', 'mono node-select-id', reason || agent.agent_type + ' ' + (runtime?.agent_version || 'version unknown'))
        );
        pill.append(checkbox, text);
        checkbox.addEventListener('change', () => {
          pill.classList.toggle('active', checkbox.checked);
          updateAgentSummary();
        });
        group.append(pill);
      }
      if (!group.children.length) group.append(el('p', 'form-hint', 'No agents yet. Add an agent before creating monitors.'));
      updateAgentSummary();
    }

    function selectAllAgents(select) {
      for (const input of agentCheckboxes()) {
        if (input.disabled) continue;
        input.checked = select;
        input.closest('.node-select-pill')?.classList.toggle('active', select);
      }
      updateAgentSummary();
    }

    function setFormProtocol(value) {
      const protocolInfo = PROTOCOLS[value] || PROTOCOLS.icmp;
      form.elements.protocol.value = value;
      for (const option of formProtocolOptions) {
        const selected = option.dataset.networkFormProtocol === value;
        option.classList.toggle('active', selected);
        option.setAttribute('aria-pressed', String(selected));
      }
      ui['protocol-hint'].textContent = protocolInfo.hint;
      form.elements.target.placeholder = protocolInfo.placeholder;
      ui.port.hidden = value !== 'tcp';
      ui.server.hidden = value !== 'dns';
      renderAgentOptions();
    }

    function syncIntervalPresets() {
      const seconds = Number(form.elements.interval_seconds.value);
      for (const preset of intervalPresets) {
        const selected = Number(preset.dataset.networkIntervalPreset) === seconds;
        preset.classList.toggle('active', selected);
        preset.setAttribute('aria-pressed', String(selected));
      }
    }

    function syncEnabledPill() {
      ui['enabled-pill'].classList.toggle('active', form.elements.enabled.checked);
    }

    function openDialog(monitor) {
      editing = monitor || null;
      form.reset();
      setFormMessage('');
      ui['form-title'].textContent = monitor ? 'Edit network monitor' : 'Add network monitor';
      ui['form-subtitle'].textContent = monitor
        ? 'Changing the target, protocol, port or DNS server starts new history; the old history stays in the archive.'
        : 'Each selected agent probes the target from its own network.';
      ui['agent-actions'].hidden = !!monitor || nodeMode;
      ui['agent-hint'].textContent = monitor
        ? 'A monitor stays on its agent. To probe from another agent, add a new monitor.'
        : 'Agents that can\'t run this probe are greyed out with the reason.';
      if (monitor) {
        form.elements.target.value = monitor.target || '';
        form.elements.port.value = monitor.port || 443;
        form.elements.interval_seconds.value = monitor.interval_seconds || 60;
        form.elements.dns_server.value = monitor.dns_server || '';
        form.elements.enabled.checked = monitor.enabled;
      }
      setFormProtocol(monitor?.protocol || 'icmp');
      syncIntervalPresets();
      syncEnabledPill();
      dialog.showModal();
      form.elements.target.focus();
    }

    async function openAddDialog() {
      try {
        await loadAgents();
        openDialog();
      } catch (e) {
        ui.message.textContent = e.message;
      }
    }

    // Client-side checks for the common mistakes; the server validates everything.
    function formProblem(data) {
      if (!data.target.trim()) return 'Enter a target to probe.';
      if (!Number.isInteger(data.interval_seconds) || data.interval_seconds < 60 || data.interval_seconds > 3600) {
        return 'The probe interval must be between 60 and 3600 seconds.';
      }
      if (data.agent_ids && !data.agent_ids.length) return 'Select at least one agent.';
      return '';
    }

    async function saveDialog(event) {
      event.preventDefault();
      const data = {
        target: form.elements.target.value,
        protocol: form.elements.protocol.value,
        interval_seconds: Number(form.elements.interval_seconds.value),
        enabled: form.elements.enabled.checked
      };
      if (data.protocol === 'tcp') data.port = Number(form.elements.port.value);
      if (data.protocol === 'dns') data.dns_server = form.elements.dns_server.value;
      if (!editing) data.agent_ids = agentCheckboxes().filter(input => input.checked).map(input => input.value);
      const problem = formProblem(data);
      if (problem) {
        setFormMessage(problem);
        return;
      }

      ui.submit.disabled = true;
      try {
        const path = editing ? 'network-monitors/' + encodeURIComponent(editing.monitor_id) : 'network-monitors';
        await api(path, { method: editing ? 'PATCH' : 'POST', body: JSON.stringify(data) });
        dialog.close();
        page = 1;
        refreshAll();
      } catch (e) {
        setFormMessage(e.message);
      } finally {
        ui.submit.disabled = false;
      }
    }

    async function mutate(monitor, method, body) {
      try {
        await api('network-monitors/' + encodeURIComponent(monitor.monitor_id), {
          method,
          body: body ? JSON.stringify(body) : undefined
        });
        refreshAll();
      } catch (e) {
        ui.message.textContent = e.message;
      }
    }

    // --- Monitor list ---

    function listHeader() {
      const header = el('div', 'network-list-head');
      const columns = nodeMode
        ? ['Monitor', 'Response', '24h', 'Hourly loss', 'Last probe', 'State', '']
        : ['Monitor', 'Node', 'Response', '24h', 'Hourly loss', 'Last probe', 'State', ''];
      for (const label of columns) header.append(el('span', '', label));
      header.setAttribute('aria-hidden', 'true');
      return header;
    }

    function rowAction(text, onClick, label) {
      const action = button(text, event => {
        // Actions sit inside the row summary; keep them from toggling the row.
        event.preventDefault();
        event.stopPropagation?.();
        onClick();
      }, 'btn btn-ghost btn-sm');
      action.setAttribute('aria-label', label);
      return action;
    }

    function detailFacts(monitor) {
      const latest = monitor.latest;
      const facts = el('dl', 'network-row-details');
      const fact = (label, value) => {
        const pair = el('div');
        pair.append(el('dt', 'muted', label), el('dd', '', value));
        facts.append(pair);
      };

      const hourlyAvailable = latest && latest.loss_1h_pct < 100;
      fact('Hourly average', hourlyAvailable ? formatMs(latest.response_avg_1h_ms) : 'Unavailable');
      fact('Hourly min / max', hourlyAvailable ? formatMs(latest.response_min_1h_ms) + ' / ' + formatMs(latest.response_max_1h_ms) : 'Unavailable');
      fact('Last window', latest ? latest.success_count + ' of ' + latest.attempt_count + ' probes succeeded' : 'Waiting for the first probe');
      fact('Last probe', latest ? new Date(latest.last_probe_at).toLocaleString() : '–');
      if (latest?.certificate) {
        const certificate = latest.certificate;
        fact('Certificate', (certificate.issuer || 'Unknown issuer') + ' · expires ' + daysUntil(certificate.expires));
      }
      if (nodeMode) fact('Node', monitor.agent_name);
      const syncPending = monitor.sync.desired_generation > monitor.sync.acknowledged_generation;
      fact('Configuration', syncPending ? (monitor.sync.error || 'Pending agent synchronization') : 'Applied on the agent');
      if (monitor.archived_at) fact('Archived', new Date(monitor.archived_at).toLocaleString());
      if (monitor.replacement_monitor_id) fact('Replaced by', monitor.replacement_monitor_id);
      if (monitor.replaces_monitor_id) fact('Replaces', monitor.replaces_monitor_id);
      return facts;
    }

    function buildRow(monitor) {
      const latest = monitor.latest;
      const state = describeState(monitor);
      const row = el('details', 'network-row');
      row.open = expanded.has(monitor.monitor_id);
      row.addEventListener('toggle', () => {
        if (row.open) expanded.add(monitor.monitor_id);
        else expanded.delete(monitor.monitor_id);
      });

      const summary = el('summary', 'network-row-main');
      const identity = el('div', 'network-cell-monitor');
      const text = el('div', 'network-monitor-text');
      const target = el('span', 'network-target mono', monitor.target);
      target.title = monitor.target;
      text.append(target, el('span', 'network-row-sub', probeLine(monitor)));
      identity.append(el('span', 'status-dot' + (state.dot ? ' ' + state.dot : '')), text);
      summary.append(identity);
      if (!nodeMode) summary.append(el('span', 'network-cell-node', monitor.agent_name));

      summary.append(el('span', 'network-cell-response mono', latest && latest.success_count > 0 ? formatMs(latest.response_avg_ms) : '–'));

      const spark = el('canvas', 'network-spark');
      spark.setAttribute('role', 'img');
      spark.setAttribute('aria-label', 'Response time over the last 24 hours');
      summary.append(spark);

      const loss = el('div', 'network-cell-loss');
      const track = el('div', 'usage-bar-track compact');
      const segment = el('div', 'usage-segment seg-loss');
      segment.style.width = latest ? Math.min(100, Math.max(0, latest.loss_1h_pct)) + '%' : '0%';
      track.append(segment);
      loss.append(track, el('span', 'mono', latest ? formatPercent(latest.loss_1h_pct) : '–'));
      summary.append(loss);

      const probe = el('span', 'network-cell-time muted', latest ? timeAgo(latest.last_probe_at) : 'Waiting');
      if (latest) probe.title = new Date(latest.last_probe_at).toLocaleString();
      summary.append(probe);

      const badges = el('div', 'network-cell-state');
      badges.append(el('span', 'badge ' + state.badge, state.label));
      if (monitor.sync.desired_generation > monitor.sync.acknowledged_generation) {
        const chip = el('span', 'badge badge-warn network-sync-chip', 'Sync pending');
        if (monitor.sync.error) chip.title = monitor.sync.error;
        badges.append(chip);
      }
      summary.append(badges);

      const actions = el('div', 'network-row-actions');
      if (monitor.archived_at) {
        actions.append(el('span', 'muted', 'Archived'));
      } else {
        actions.append(
          rowAction('Edit', () => openDialog(monitor), 'Edit ' + monitor.target),
          rowAction(monitor.enabled ? 'Pause' : 'Resume', () => mutate(monitor, 'PATCH', { enabled: !monitor.enabled }),
            (monitor.enabled ? 'Pause ' : 'Resume ') + monitor.target),
          rowAction('Delete', () => {
            if (window.confirm('Remove this monitor? Its measurements and incident history will be preserved.')) mutate(monitor, 'DELETE');
          }, 'Delete ' + monitor.target)
        );
      }
      summary.append(actions);

      row.append(summary, detailFacts(monitor));
      return { element: row, spark };
    }

    function emptyState() {
      if (hasFilters()) return el('p', 'network-empty muted', 'No network monitors match these filters.');
      const empty = el('div', 'empty-state');
      empty.append(
        el('h3', 'empty-state-title', 'No monitors yet'),
        el('p', '', 'Monitors probe a target from an agent, so you see response time and failed checks from that machine.'),
        button('Add monitor', openAddDialog, 'btn btn-primary')
      );
      return empty;
    }

    function renderList() {
      const next = new Map();
      const elements = [];
      const added = [];
      for (const monitor of rows) {
        const signature = JSON.stringify(monitor);
        const previous = rowElements.get(monitor.monitor_id);
        const entry = previous && previous.signature === signature ? previous : { ...buildRow(monitor), signature };
        if (entry !== previous) added.push(monitor.monitor_id);
        next.set(monitor.monitor_id, entry);
        elements.push(entry.element);
      }
      rowElements = next;

      if (rows.length) ui.list.replaceChildren(listHeader(), ...elements);
      else ui.list.replaceChildren(emptyState());

      ui.count.textContent = total ? ((page - 1) * limit + 1) + '–' + Math.min(page * limit, total) + ' of ' + total : '';
      ui.prev.disabled = page <= 1;
      ui.next.disabled = page * limit >= total;
      updateSparklines(added);
    }

    // Live readings replace HTTP readings for the monitors on this page.
    function applyLiveReadings() {
      if (!live.snapshot) return;
      rows = rows.map(monitor => live.snapshot[monitor.monitor_id] ? { ...monitor, ...live.snapshot[monitor.monitor_id] } : monitor);
    }

    async function refreshList() {
      if (!visible()) return;
      const controller = begin('list');
      try {
        const params = filterParams();
        params.set('page', String(page));
        params.set('limit', String(limit));
        const data = await api('network-monitors?' + params, { signal: controller.signal });
        if (controller !== pending.list || !visible()) return;

        rows = data.items;
        total = data.total;
        rememberTargets(rows);
        applyLiveReadings();
        // A deletion may have emptied the current page.
        if (page > 1 && (page - 1) * limit >= total) {
          page = Math.max(1, Math.ceil(total / limit));
          return refreshList();
        }
        ui.message.textContent = '';
        renderList();
        renderHero();
      } catch (e) {
        if (e.name !== 'AbortError' && controller === pending.list) showError(ui.message, e.message, refreshList);
      }
    }

    // --- Row sparklines ---

    function drawSparkline(monitorId) {
      const entry = rowElements.get(monitorId);
      const cached = sparklines.cache.get(monitorId);
      if (!entry || !cached) return;
      window.CertainStatsChart.drawSparkline?.(entry.spark, cached.points, { color: '--s1', hours: SPARK_HOURS });
    }

    // Draws new rows from the cache and fetches stale histories, one request
    // per monitor like the agent cards' sparklines.
    function updateSparklines(monitorIds) {
      const stale = [];
      for (const monitorId of monitorIds) {
        const cached = sparklines.cache.get(monitorId);
        if (cached) drawSparkline(monitorId);
        if ((!cached || Date.now() - cached.fetchedAt > SPARK_MAX_AGE_MS) && !sparklines.pending.has(monitorId)) stale.push(monitorId);
      }
      if (!stale.length || !visible()) return;

      if (!sparklines.controller) sparklines.controller = new AbortController();
      const signal = sparklines.controller.signal;
      stale.forEach((monitorId, index) => {
        sparklines.pending.add(monitorId);
        const timer = setTimeout(async () => {
          if (signal.aborted) return;
          let points = [];
          try {
            const data = await api('network-monitors/' + encodeURIComponent(monitorId) + '/history?hours=' + SPARK_HOURS, { signal });
            points = data.points.map(point => ({ timestamp: point.timestamp, value: point.response_avg_ms }));
          } catch (e) {
            if (e.name === 'AbortError' || signal.aborted) return;
          } finally {
            sparklines.pending.delete(monitorId);
          }
          sparklines.cache.set(monitorId, { points, fetchedAt: Date.now() });
          drawSparkline(monitorId);
        }, index * SPARK_STAGGER_MS);
        signal.addEventListener('abort', () => clearTimeout(timer), { once: true });
      });
    }

    function redrawSparklines() {
      for (const monitorId of rowElements.keys()) drawSparkline(monitorId);
    }

    // --- Graphs ---

    function renderLegend() {
      ui.legend.replaceChildren();
      const groups = new Map();
      for (const monitor of candidates) {
        const key = monitor.target + ' · ' + monitor.protocol.toUpperCase();
        if (!groups.has(key)) {
          const group = el('div', 'network-legend-group');
          group.append(el('strong', 'network-legend-title', key));
          ui.legend.append(group);
          groups.set(key, group);
        }

        const isHidden = hiddenSeries.has(monitor.monitor_id);
        const toggle = el('button', 'network-series-toggle' + (isHidden ? ' is-hidden' : ''));
        toggle.type = 'button';
        toggle.setAttribute('aria-pressed', String(!isHidden));
        toggle.setAttribute('aria-label', 'Show ' + seriesLabel(monitor));
        const dot = el('span', 'chart-legend-dot');
        dot.style.backgroundColor = seriesColor(monitor.monitor_id);
        toggle.append(dot, el('span', '', monitor.agent_name + probeDetails(monitor)));
        toggle.addEventListener('click', () => {
          if (hiddenSeries.has(monitor.monitor_id)) hiddenSeries.delete(monitor.monitor_id);
          else hiddenSeries.add(monitor.monitor_id);
          renderLegend();
          paintHistory();
        });
        groups.get(key).append(toggle);
      }
    }

    function paintHistory() {
      if (!historyData) return;
      const byId = new Map(candidates.map(monitor => [monitor.monitor_id, monitor]));
      const shown = historyData.items.filter(item => byId.has(item.monitor_id) && !hiddenSeries.has(item.monitor_id));
      const series = key => shown.map(item => ({
        label: seriesLabel(byId.get(item.monitor_id)),
        color: seriesColor(item.monitor_id),
        fill: false,
        data: item.points.map(point => ({ timestamp: point.timestamp, value: point[key] }))
      }));
      const latency = series('response_avg_ms');
      const loss = series('loss_pct');

      // Brushing either chart zooms both and updates the picker's custom range.
      const zoom = (start, end) => {
        customRange = { start: Math.floor(start), end: Math.floor(end) };
        picker.setState({ hours, customRange });
        loadHistory();
      };
      if (!charts.length) {
        const shared = { customRange, hours, queryEndTime: historyData.end, onZoom: zoom };
        charts = [
          window.CertainStatsChart.renderMultiChart(ui.latency.id, { ...shared, seriesList: latency, formatter: v => v.toFixed(2) + ' ms' }),
          window.CertainStatsChart.renderMultiChart(ui.loss.id, { ...shared, seriesList: loss, formatter: v => v.toFixed(2) + '%', yMax: 100, maxCap: 100 })
        ];
      } else {
        charts[0]?.updateSeries(latency, hours, null, historyData.end, customRange);
        charts[1]?.updateSeries(loss, hours, 100, historyData.end, customRange);
      }

      const hasPoints = [...latency, ...loss].some(s => s.data.some(point => point.value != null));
      let message = '';
      if (!candidates.length) message = 'No monitors match these filters.';
      else if (!latency.length) message = 'All series are hidden. Use the legend to show a monitor.';
      else if (!hasPoints) message = 'No history for the selected time range yet.';
      ui['chart-message'].replaceChildren(el('span', '', message));
    }

    function destroyCharts() {
      charts.forEach(chart => chart?.destroy());
      charts = [];
    }

    async function loadCandidates() {
      if (!visible()) return;
      pending.history?.abort();
      pending.history = null;
      const controller = begin('candidates');
      if (!historyData) ui['chart-message'].textContent = 'Loading monitors…';
      try {
        const params = filterParams();
        params.set('page', '1');
        params.set('limit', String(GRAPH_LIMIT));
        const data = await api('network-monitors?' + params, { signal: controller.signal });
        if (controller !== pending.candidates || !visible()) return;

        candidates = data.items;
        rememberTargets(candidates);
        ui['chart-count'].textContent = 'Showing ' + candidates.length + ' of ' + data.total + ' matching monitors' +
          (data.total > GRAPH_LIMIT ? ' · Narrow the filters to see other monitors.' : '');
        renderLegend();
        await loadHistory();
      } catch (e) {
        if (e.name !== 'AbortError' && controller === pending.candidates) showError(ui['chart-message'], e.message, loadCandidates);
      }
    }

    // Each monitor's history is its own request, like the agent metrics graphs.
    async function loadHistory() {
      if (!visible()) return;
      if (!candidates.length) {
        pending.history?.abort();
        historyData = { items: [], end: Date.now() };
        paintHistory();
        return;
      }
      const controller = begin('history');
      ui['chart-message'].textContent = 'Loading history…';

      const params = new URLSearchParams();
      if (customRange) {
        params.set('start', customRange.start);
        params.set('end', customRange.end);
      } else {
        params.set('hours', String(hours));
      }
      const results = await Promise.all(candidates.map(monitor =>
        api('network-monitors/' + encodeURIComponent(monitor.monitor_id) + '/history?' + params, { signal: controller.signal })
          .then(data => ({ data }), error => ({ error }))
      ));
      if (controller !== pending.history || !visible()) return;

      const loaded = results.filter(result => result.data).map(result => result.data);
      const failure = results.find(result => result.error && result.error.name !== 'AbortError')?.error;
      // Keep the current plots when nothing loaded; otherwise show what did.
      if (loaded.length || !failure) {
        historyData = { items: loaded, end: Math.max(0, ...loaded.map(data => data.end)) || Date.now() };
        paintHistory();
      }
      if (failure) showError(ui['chart-message'], failure.message, loadHistory);
    }

    // --- Lifecycle ---

    function refreshAll() {
      refreshList();
      loadCandidates();
    }

    function filtersChanged() {
      page = 1;
      hiddenSeries.clear();
      historyData = null;
      abortRequests();
      refreshAll();
    }

    function selectProtocol(value) {
      protocol = value;
      for (const option of protocolOptions) {
        const selected = option.dataset.networkProtocolOption === value;
        option.classList?.toggle('active', selected);
        option.setAttribute('aria-pressed', String(selected));
      }
      filtersChanged();
    }

    function suspend() {
      abortRequests();
      clearTimeout(searchTimer);
      if (dialog.open) dialog.close();
    }

    function visibilityChanged() {
      if (visible()) {
        renderHero();
        refreshAll();
      } else {
        suspend();
      }
    }

    on(ui.add, 'click', openAddDialog);
    for (const option of formProtocolOptions) {
      on(option, 'click', () => setFormProtocol(option.dataset.networkFormProtocol));
    }
    for (const preset of intervalPresets) {
      on(preset, 'click', () => {
        form.elements.interval_seconds.value = preset.dataset.networkIntervalPreset;
        syncIntervalPresets();
      });
    }
    on(form.elements.interval_seconds, 'input', syncIntervalPresets);
    on(form.elements.enabled, 'change', syncEnabledPill);
    on(form.elements.dns_server, 'input', renderAgentOptions);
    on(ui['agents-all'], 'click', () => selectAllAgents(true));
    on(ui['agents-none'], 'click', () => selectAllAgents(false));
    on(form, 'submit', saveDialog);

    on(ui.search, 'input', () => {
      clearTimeout(searchTimer);
      searchTimer = setTimeout(filtersChanged, SEARCH_DEBOUNCE_MS);
    });
    for (const option of protocolOptions) {
      on(option, 'click', () => selectProtocol(option.dataset.networkProtocolOption));
    }
    for (const key of ['node', 'state']) on(ui[key], 'change', filtersChanged);
    on(ui.limit, 'change', () => {
      page = 1;
      limit = Number(ui.limit.value);
      refreshList();
    });
    on(ui.prev, 'click', () => {
      page--;
      refreshList();
    });
    on(ui.next, 'click', () => {
      page++;
      refreshList();
    });
    on(ui.refresh, 'click', refreshAll);

    if (parent) {
      if (ui['node-filter']) ui['node-filter'].hidden = true;
      on(parent, 'toggle', visibilityChanged);
    }
    on(document, 'visibilitychange', visibilityChanged);
    on(window, 'certainstats_feed_connected', refreshList);
    on(window, 'certainstats_theme_change', redrawSparklines);

    const unsubscribe = telemetry.subscribeNetwork?.(snapshot => {
      live.snapshot = snapshot;
      const membership = Object.keys(snapshot).sort().map(id => id + ':' + snapshot[id].enabled).join(',');
      const changed = live.membership !== undefined && live.membership !== membership;
      live.membership = membership;
      applyLiveReadings();
      if (!visible()) return;
      renderHero();
      renderList();
      // Added, removed or toggled monitors change the page; reconcile through HTTP.
      if (changed) refreshList();
    });

    const pollTimer = setInterval(() => {
      if (!visible()) return;
      const feed = telemetry.getNetworkFeedState?.();
      const stale = !feed?.connected || !feed.lastPulse || Date.now() - feed.lastPulse > STALE_PULSE_MS;
      // A reconnect may have missed membership changes.
      if (stale || feed.generation !== live.generation) refreshList();
      live.generation = feed?.generation || 0;
      loadCandidates();
    }, POLL_INTERVAL_MS);

    function destroy() {
      destroyed = true;
      unsubscribe?.();
      clearInterval(pollTimer);
      suspend();
      listeners.abort();
      picker.destroy();
      destroyCharts();
    }
    on(window, 'pagehide', destroy);

    renderHero();
    if (!nodeMode && active) {
      loadAgents().catch(() => {});
      refreshAll();
    }

    return {
      refresh: refreshAll,
      destroy,
      activate() {
        active = true;
        if (!agents.length && !nodeMode) loadAgents().catch(() => {});
        applyLiveReadings();
        renderHero();
        refreshAll();
      },
      deactivate() {
        active = false;
        suspend();
      },
      setNode(id) {
        suspend();
        nodeId = id;
        page = 1;
        hiddenSeries.clear();
        expanded.clear();
        candidates = [];
        historyData = null;
        rows = [];
        rowElements = new Map();
        destroyCharts();
        ui.list.replaceChildren();
        ui.legend.replaceChildren();
        ui['chart-message'].textContent = '';
        ui['chart-count'].textContent = '';
        if (parent) parent.open = false;
      }
    };
  }

  window.CertainStatsNetworkMonitors = { mount };
})();
