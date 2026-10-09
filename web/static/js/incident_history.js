(function () {
  'use strict';
  const LABELS = { network_loss: 'Network monitor loss', agent_down: 'Node offline', cpu_usage: 'CPU usage', cpu_iowait: 'CPU I/O wait', cpu_steal: 'CPU steal', ram_usage: 'RAM usage', swap_usage: 'Swap usage', disk_usage: 'Disk usage', net_rx: 'Network in', net_tx: 'Network out', disk_read: 'Disk read', disk_write: 'Disk write' };
  const STATUS = { queued: 'Queued', pending: 'Sending', success: 'Delivered', failed: 'Failed', unknown: 'Outcome unknown', skipped: 'Skipped' };
  function el(tag, cls, text) { const node = document.createElement(tag); if (cls) node.className = cls; if (text != null) node.textContent = text; return node; }
  function date(value) { const t = new Date(value); return Number.isFinite(t.getTime()) ? t.toLocaleString() : 'Unknown time'; }
  function duration(start, end) {
    const ms = new Date(end || Date.now()).getTime() - new Date(start).getTime();
    if (!Number.isFinite(ms)) return 'Unknown';
    const seconds = Math.max(0, Math.floor(ms / 1000));
    if (seconds < 60) return seconds + 's';
    const minutes = Math.floor(seconds / 60); if (minutes < 60) return minutes + 'm ' + seconds % 60 + 's';
    const hours = Math.floor(minutes / 60); if (hours < 24) return hours + 'h ' + minutes % 60 + 'm';
    return Math.floor(hours / 24) + 'd ' + hours % 24 + 'h';
  }
  function metric(type, value) {
    if (type === 'agent_down') return 'Offline';
    if (!type || !Number.isFinite(value)) return 'Unknown';
    return value.toLocaleString(undefined, { maximumFractionDigits: 1 }) + (['net_rx', 'net_tx', 'disk_read', 'disk_write'].includes(type) ? ' KB/s' : '%');
  }
  function badge(status, text) { return el('span', 'incident-badge incident-' + status, text || STATUS[status] || 'Not recorded'); }

  function mount(root, options) {
    options = options || {};
    const panelPath = options.panelPath || '';
    const nodeMode = !!options.nodeMode;
    const parent = nodeMode ? root.closest('details') : null;
    const search = root.querySelector('[data-history-search]');
    const nodeSelect = root.querySelector('[data-history-node]');
    const list = root.querySelector('[data-history-list]');
    const message = root.querySelector('[data-history-message]');
    const count = root.querySelector('[data-history-count]');
    const pageLabel = root.querySelector('[data-history-page]');
    const prev = root.querySelector('[data-history-prev]');
    const next = root.querySelector('[data-history-next]');
    const size = root.querySelector('[data-history-limit]');
    const refresh = root.querySelector('[data-history-refresh]');
    let page = 1, limit = 25, status = 'all', query = '', node = '', totalPages = 0;
    let controller = null, searchTimer = null, lastRefresh = 0, pendingDelivery = false, signature = '', destroyed = false;
    let rows = [], hasLoaded = false, loading = false;
    const expanded = new Set();
    const eventStates = new Map();
    const retrying = new Set();
    if (nodeMode) root.querySelector('[data-history-node-label]').hidden = true;
    let active = true;
    function visible() { return active && !destroyed && !document.hidden && (!nodeMode || (node && parent.open && !root.closest('[hidden]'))); }
    function feedback(text, retry) {
      message.replaceChildren(el('span', '', text));
      if (retry) { const button = el('button', 'btn btn-secondary btn-sm', 'Retry'); button.type = 'button'; button.addEventListener('click', () => load()); message.append(button); }
      message.hidden = !text;
    }
    async function json(path, signal, method) {
      const response = await fetch(panelPath + path, { signal, method: method || 'GET' });
      let data; try { data = await response.json(); } catch (_) { throw new Error('Invalid server response'); }
      if (!response.ok) throw new Error(data.message || data.error || 'Request failed (HTTP ' + response.status + ')');
      return data;
    }
    function updateControls(total) {
      count.textContent = total ? ((page - 1) * limit + 1) + '–' + Math.min(page * limit, total) + ' of ' + total + ' incidents' : '0 incidents';
      pageLabel.textContent = totalPages ? page + ' / ' + totalPages : '';
      prev.disabled = loading || page <= 1; next.disabled = loading || page >= totalPages;
      refresh.disabled = loading; size.value = String(limit);
      root.querySelectorAll('[data-history-status]').forEach(button => { const active = button.dataset.historyStatus === status; button.classList.toggle('active', active); button.setAttribute('aria-pressed', String(active)); });
    }
    function updateSummary(data) {
      const banner = document.getElementById('active-incidents-banner');
      if (banner) { banner.style.display = data.firing ? 'flex' : 'none'; const label = document.getElementById('incidents-count-label'); if (label) label.textContent = data.firing === 1 ? '1 incident is firing' : data.firing + ' incidents are firing'; }
      const selected = nodeSelect.value;
      nodeSelect.replaceChildren(new Option('All nodes', ''));
      (data.nodes || []).forEach(n => nodeSelect.append(new Option(n.nickname || n.agent_id, n.agent_id)));
      if (selected && !Array.from(nodeSelect.options).some(o => o.value === selected)) nodeSelect.append(new Option(selected, selected));
      nodeSelect.value = selected;
    }
    async function load() {
      if (destroyed || (nodeMode && (!node || !parent.open))) return;
      if (controller) controller.abort(); controller = new AbortController(); const request = controller;
      loading = true; root.setAttribute('aria-busy', 'true'); updateControls(rows.length ? Number(root.dataset.total || 0) : 0);
      feedback(hasLoaded ? 'Refreshing…' : 'Loading incident history…');
      try {
        const params = new URLSearchParams({ page: String(page), limit: String(limit), q: query, status }); if (node) params.set('agent_id', node);
        const data = await json('/api/alerts/history?' + params, request.signal);
        if (request.signal.aborted) return;
        totalPages = data.total_pages || 0;
        if (!data.total) page = 1;
        if (data.total > 0 && page > totalPages) { page = totalPages; return load(); }
        rows = data.data || []; hasLoaded = true; root.dataset.total = String(data.total || 0);
        pendingDelivery = rows.some(h => ['queued', 'pending'].includes(h.firing_delivery) || ['queued', 'pending'].includes(h.recovery_delivery));
        const nextSignature = JSON.stringify(rows);
        if (signature !== nextSignature || !list.children.length) { signature = nextSignature; render(); }
        feedback(''); loading = false; updateControls(data.total || 0); lastRefresh = Date.now();
        for (const id of expanded) { const view = list.querySelector('[data-events-id="' + CSS.escape(id) + '"]'); if (view) await loadEvents(id, view, true, request.signal); }
        if (!nodeMode) { try { const summary = await json('/api/alerts/history/summary', request.signal); if (!request.signal.aborted) updateSummary(summary); } catch (error) { if (error.name !== 'AbortError') feedback('History loaded; active incident summary could not be refreshed.', true); } }
      } catch (error) { if (error.name !== 'AbortError' && !request.signal.aborted) { feedback('Could not load incident history. ' + error.message, true); } }
      finally { if (!request.signal.aborted) { loading = false; root.setAttribute('aria-busy', 'false'); updateControls(Number(root.dataset.total || 0)); lastRefresh = Date.now(); } }
    }
    function render() {
      const focusedRecord = document.activeElement && document.activeElement.closest('.incident-record');
      const focusID = focusedRecord && focusedRecord.dataset.incidentId;
      list.replaceChildren();
      if (!rows.length) { list.append(el('div', 'incident-empty', query || status !== 'all' || (!nodeMode && node) ? 'No incidents match these filters.' : 'No incidents recorded yet.')); return; }
      rows.forEach(h => {
        const details = el('details', 'incident-record'); details.dataset.incidentId = h.history_id;
        const summary = el('summary', 'incident-row');
        const identity = el('div', 'incident-identity'); identity.append(el('strong', '', h.alert_nickname || LABELS[h.trigger && h.trigger.type] || 'Rule'), el('span', 'muted', h.agent_nickname || h.agent_id));
        const state = h.closed_at ? 'closed' : h.resolved_at ? 'resolved' : 'firing';
        const times = el('div', 'incident-times'); times.append(el('span', '', date(h.triggered_at)), el('span', 'muted', h.closed_at ? 'Closed ' + date(h.closed_at) : h.resolved_at ? 'Recovered ' + date(h.resolved_at) : 'Awaiting recovery'));
        const elapsed = el('span', 'incident-duration mono', duration(h.triggered_at, h.closed_at || h.resolved_at)); elapsed.dataset.durationStart = h.triggered_at; elapsed.dataset.durationEnd = h.closed_at || h.resolved_at || ''; elapsed.title = h.trigger && h.trigger.type === 'agent_down' ? 'Downtime' : 'Duration';
        const delivery = el('div', 'incident-delivery'); delivery.append(badge(h.firing_delivery || h.notified_status, 'Alert: ' + (STATUS[h.firing_delivery || h.notified_status] || 'Not recorded')));
        if (h.recovery_delivery) delivery.append(badge(h.recovery_delivery, 'Recovery: ' + (STATUS[h.recovery_delivery] || 'Not recorded')));
        summary.append(el('span', 'incident-chevron', '›'), identity, badge(state, state === 'closed' ? 'Closed' : state === 'resolved' ? 'Resolved' : 'Firing'), times, elapsed, delivery);
        const body = el('div', 'incident-body');
        const facts = el('dl', 'incident-facts');
        function fact(label, value) { const pair = el('div'); const detail = el('dd', '', value); pair.append(el('dt', 'muted', label), detail); facts.append(pair); return detail; }
        const trigger = h.trigger || {};
        if (h.monitor_id) {
          fact('Monitor target', h.monitor.target);
          fact('Protocol', h.monitor.protocol);
          if (h.monitor.dns_server) fact('DNS server', h.monitor.dns_server);
        }
        fact(h.legacy ? 'Available condition at migration' : 'Condition at start', trigger.type === 'agent_down' ? 'Node offline' : (LABELS[trigger.type] || trigger.type || 'Unknown condition') + (trigger.type ? ' ' + (trigger.operator || '') + ' ' + metric(trigger.type, trigger.threshold) : ''));
        fact('Breach value', metric(trigger.type, h.trigger_value));
        const liveDuration = fact(trigger.type === 'agent_down' ? 'Downtime' : 'Duration', duration(h.triggered_at, h.closed_at || h.resolved_at)); liveDuration.dataset.durationStart = h.triggered_at; liveDuration.dataset.durationEnd = h.closed_at || h.resolved_at || '';
        fact('Started', date(h.triggered_at)).title = Intl.DateTimeFormat().resolvedOptions().timeZone;
        if (h.resolved_at) fact('Recovered', date(h.resolved_at));
        if (h.closed_at) fact('Closed', date(h.closed_at) + ' — Monitoring removed');
        fact('Node ID', h.agent_id); if (h.target_name || h.target_id) fact('Notification target', h.target_name || h.target_id);
        body.append(facts);
        if (h.legacy) {
          body.append(el('p', 'incident-legacy', 'Legacy record: past notification attempts were not recorded. The saved delivery result has no known delivery timestamp; the condition reflects what was available at migration.'));
          body.append(badge(h.notified_status, 'Stored alert result: ' + (STATUS[h.notified_status] || 'Not recorded')));
          if (h.error_message) body.append(el('p', 'incident-event-error', h.error_message));
          if (h.retry_available && !h.resolved_at && !h.closed_at && ['failed', 'unknown'].includes(h.notified_status)) {
            const button = el('button', 'btn btn-secondary btn-sm', 'Retry alert notification'); button.type = 'button'; button.addEventListener('click', () => retry('/api/alerts/history/retry/' + encodeURIComponent(h.history_id), button, h.notified_status)); body.append(button);
          }
        }
        body.append(el('h3', 'incident-event-heading', 'Event history'));
        const events = el('div', 'incident-events'); events.dataset.eventsId = h.history_id; body.append(events);
        details.append(summary, body); details.open = expanded.has(h.history_id);
        details.addEventListener('toggle', () => { if (!details.isConnected) return; if (details.open) { expanded.add(h.history_id); if (!events.children.length) loadEvents(h.history_id, events); } else expanded.delete(h.history_id); });
        list.append(details);
      });
      if (focusID) { const record = Array.from(list.children).find(n => n.dataset.incidentId === focusID); if (record) record.querySelector('summary').focus({ preventScroll: true }); }
    }
    async function loadEvents(id, view, refreshOnly, signal) {
      if (!controller || controller.signal.aborted) return;
      signal = signal || controller.signal; const ownNode = node;
      const state = eventStates.get(id) || { page: 1, data: [] }; eventStates.set(id, state);
      if (!refreshOnly) view.replaceChildren(el('span', 'muted', 'Loading events…'));
      try {
        const gathered = []; let total = 0;
        for (let p = 1; p <= state.page; p++) { const result = await json('/api/alerts/history/' + encodeURIComponent(id) + '/events?page=' + p + '&limit=50', signal); gathered.push(...result.data); total = result.total; if (p * 50 >= total) break; }
        if (signal.aborted || ownNode !== node || !view.isConnected) return;
        state.data = gathered; view.replaceChildren();
        gathered.forEach(e => {
          const line = el('div', 'incident-event');
          const content = el('div', 'incident-event-content');
          const label = e.kind === 'notification' ? (e.phase === 'recovery' ? 'Recovery notification' : 'Alert notification') + (e.retry_of ? ' · Retry' : '') : e.kind === 'resolved' ? 'Recovered' : e.kind === 'monitoring_removed' ? 'Closed — monitoring removed' : 'Incident started';
          content.append(el('strong', '', label));
          if (e.kind === 'notification') content.append(badge(e.status));
          const when = el('time', 'incident-event-time', date(e.created_at)); when.dateTime = e.created_at; when.title = 'Full local time: ' + date(e.created_at) + ' (' + Intl.DateTimeFormat().resolvedOptions().timeZone + ')';
          line.append(when, content);
          if (e.started_at) content.append(el('span', 'muted', 'Started ' + date(e.started_at)));
          if (e.completed_at) content.append(el('span', 'muted', 'Completed ' + date(e.completed_at)));
          if (e.error_message) content.append(el('span', 'incident-event-error', e.error_message));
          if (e.retry_available) {
            const button = el('button', 'btn btn-secondary btn-sm', e.status === 'unknown' ? 'Retry (may duplicate delivery)' : 'Retry notification'); button.type = 'button'; button.disabled = retrying.has(e.event_id); button.addEventListener('click', () => retry('/api/alerts/history/events/' + encodeURIComponent(e.event_id) + '/retry', button, e.status, e.event_id)); content.append(button);
          }
          view.append(line);
        });
        if (gathered.length < total) { const more = el('button', 'btn btn-secondary btn-sm', 'Load more events (' + gathered.length + ' of ' + total + ')'); more.type = 'button'; more.addEventListener('click', () => { state.page++; loadEvents(id, view); }); view.append(more); }
      } catch (error) {
        if (error.name === 'AbortError' || signal.aborted) return;
        const errorView = el('div', 'incident-message', 'Could not load events. ' + error.message); const button = el('button', 'btn btn-secondary btn-sm', 'Retry'); button.type = 'button'; button.addEventListener('click', () => loadEvents(id, view)); errorView.append(button); view.replaceChildren(errorView);
      }
    }
    async function retry(path, button, state, eventID) {
      if (state === 'unknown' && !window.confirm('The previous delivery outcome is unknown. Retrying may send the notification twice. Retry?')) return;
      if (eventID && retrying.has(eventID)) return;
      if (eventID) retrying.add(eventID); button.disabled = true; button.textContent = 'Queueing…';
      const ownNode = node; const signal = controller && controller.signal;
      try { await json(path, signal, 'POST'); if (ownNode === node && !(signal && signal.aborted)) { feedback('Notification retry queued.'); await load(); } }
      catch (error) { if (error.name !== 'AbortError' && ownNode === node && !(signal && signal.aborted)) { feedback('Could not retry notification. ' + error.message); button.disabled = false; button.textContent = 'Retry notification'; } }
      finally { if (eventID) retrying.delete(eventID); }
    }
    function reset() { page = 1; expanded.clear(); eventStates.clear(); signature = ''; }
    search.addEventListener('input', () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => { query = search.value.trim(); reset(); load(); }, 350); });
    nodeSelect.addEventListener('change', () => { node = nodeSelect.value; reset(); load(); });
    root.querySelectorAll('[data-history-status]').forEach(button => button.addEventListener('click', () => { status = button.dataset.historyStatus; reset(); load(); }));
    size.addEventListener('change', () => { limit = Number(size.value); reset(); load(); });
    prev.addEventListener('click', () => { if (page > 1) { page--; expanded.clear(); load(); } }); next.addEventListener('click', () => { if (page < totalPages) { page++; expanded.clear(); load(); } }); refresh.addEventListener('click', () => load());
    if (parent) parent.addEventListener('toggle', () => { if (parent.open) load(); else if (controller) controller.abort(); });
    const timer = setInterval(() => {
      if (!visible()) return;
      root.querySelectorAll('[data-duration-start]').forEach(n => { if (!n.dataset.durationEnd) n.textContent = duration(n.dataset.durationStart); });
      if (!loading && Date.now() - lastRefresh >= (pendingDelivery ? 3000 : 30000)) load();
    }, 1000);
    const onVisibility = () => { if (document.hidden) { if (controller) controller.abort(); loading = false; } else if (visible()) load(); };
    document.addEventListener('visibilitychange', onVisibility);
    function destroy() { destroyed = true; clearInterval(timer); clearTimeout(searchTimer); if (controller) controller.abort(); document.removeEventListener('visibilitychange', onVisibility); }
    window.addEventListener('pagehide', destroy, { once: true });
    if (!nodeMode) load();
    // The SPA hides this view instead of unloading it; inactive views make no requests.
    function deactivate() {
      active = false;
      if (controller) controller.abort();
      loading = false;
    }
    function activate() {
      active = true;
      if (visible()) load();
    }
    return { refresh: load, destroy, activate, deactivate, setNode(id) {
      if (controller) controller.abort(); clearTimeout(searchTimer); loading = false; node = id || ''; rows = []; hasLoaded = false; pendingDelivery = false; totalPages = 0; reset(); query = ''; status = 'all'; search.value = ''; root.dataset.total = '0'; list.replaceChildren(); feedback(''); updateControls(0); if (parent) parent.open = false;
    } };
  }
  window.CertainStatsIncidentHistory = { mount, duration, metric };
})();
