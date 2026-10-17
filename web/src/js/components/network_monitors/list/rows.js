import { el, formatMs, formatPercent, timeAgo, daysUntil, probeLine, describeState, button } from '../shared/format.js';

// Monitor rows and the empty state.
export function createRows(ctx) {
  const { nodeMode, expanded } = ctx;

  function listHeader() {
    const header = el('div', 'network-list-head');
    const columns = [nodeMode ? 'Monitor' : 'Node', 'Response', '24h', 'Hourly loss', 'Last probe', 'State', ''];
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
    const target = el('span', 'network-target' + (nodeMode ? ' mono' : ''), nodeMode ? monitor.target : monitor.agent_name);
    target.title = nodeMode ? monitor.target : monitor.agent_name + ' · ' + monitor.target;
    text.append(target, el('span', 'network-row-sub', probeLine(monitor)));
    identity.append(el('span', 'status-dot' + (state.dot ? ' ' + state.dot : '')), text);
    summary.append(identity);

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
        rowAction('Edit', () => ctx.openGroupDialog(monitor.target), 'Edit group ' + monitor.target),
        rowAction(monitor.enabled ? 'Pause' : 'Resume', () => ctx.mutate(monitor, 'PATCH', { enabled: !monitor.enabled }),
          (monitor.enabled ? 'Pause ' : 'Resume ') + monitor.target),
        rowAction('Delete', () => {
          if (window.confirm('Remove this monitor? Its measurements and incident history will be preserved.')) ctx.mutate(monitor, 'DELETE');
        }, 'Delete ' + monitor.target)
      );
    }
    summary.append(actions);

    row.append(summary, detailFacts(monitor));
    return { element: row, spark };
  }

  function emptyState() {
    if (ctx.hasFilters()) return el('p', 'network-empty muted', 'No network monitors match these filters.');
    const empty = el('div', 'empty-state');
    empty.append(
      el('h3', 'empty-state-title', 'No monitors yet'),
      el('p', '', 'Monitors probe a target from an agent, so you see response time and failed checks from that machine.'),
      button('Add monitor', ctx.openAddDialog, 'btn btn-primary')
    );
    return empty;
  }

  return { listHeader, rowAction, detailFacts, buildRow, emptyState };
}
