import { el } from '../shared/format.js';

// Rendering and refreshing the paginated monitor list.
export function createListRender(ctx) {
  const { nodeMode, ui, collapsedGroups, live, pending } = ctx;
  const s = ctx.state;

  function renderList() {
    const next = new Map();
    const elements = [];
    const groups = new Map();
    const groupCounts = new Map();
    for (const monitor of s.rows) groupCounts.set(monitor.target, (groupCounts.get(monitor.target) || 0) + 1);
    const added = [];
    for (const monitor of s.rows) {
      const signature = JSON.stringify(monitor);
      const previous = s.rowElements.get(monitor.monitor_id);
      const entry = previous && previous.signature === signature ? previous : { ...ctx.buildRow(monitor), signature };
      if (entry !== previous) added.push(monitor.monitor_id);
      next.set(monitor.monitor_id, entry);
      if (nodeMode) {
        elements.push(entry.element);
      } else {
        let group = groups.get(monitor.target);
        if (!group) {
          group = el('details', 'network-target-group');
          group.open = !collapsedGroups.has(monitor.target);
          const summary = el('summary', 'network-target-heading');
          const heading = el('h3', 'network-group-title');
          const count = groupCounts.get(monitor.target);
          heading.append(el('span', 'network-group-chevron', '›'), el('span', 'network-group-target mono', monitor.target),
            el('span', 'network-group-count muted', count + (count === 1 ? ' monitor' : ' monitors')));
          summary.append(heading);
          if (!monitor.archived_at) summary.append(ctx.rowAction('Edit group', () => ctx.openGroupDialog(monitor.target), 'Edit group ' + monitor.target));
          group.append(summary);
          const currentGroup = group;
          currentGroup.addEventListener('toggle', event => {
            // Nested monitor details also emit toggle events.
            if (event.target !== currentGroup || !currentGroup.isConnected) return;
            if (currentGroup.open) collapsedGroups.delete(monitor.target);
            else collapsedGroups.add(monitor.target);
          });
          groups.set(monitor.target, group);
          elements.push(group);
        }
        group.append(entry.element);
      }
    }
    s.rowElements = next;

    if (s.rows.length) ui.list.replaceChildren(ctx.listHeader(), ...elements);
    else ui.list.replaceChildren(ctx.emptyState());

    ui.count.textContent = s.total ? ((s.page - 1) * s.limit + 1) + '–' + Math.min(s.page * s.limit, s.total) + ' of ' + s.total : '';
    ui.prev.disabled = s.page <= 1;
    ui.next.disabled = s.page * s.limit >= s.total;
    ctx.updateSparklines(added);
  }

  // Live readings replace HTTP readings for the monitors on this page.
  function applyLiveReadings() {
    if (!live.snapshot) return;
    s.rows = s.rows.map(monitor => live.snapshot[monitor.monitor_id] ? { ...monitor, ...live.snapshot[monitor.monitor_id] } : monitor);
  }

  async function refreshList() {
    if (!ctx.visible()) return;
    const controller = ctx.begin('list');
    try {
      const params = ctx.filterParams();
      params.set('page', String(s.page));
      params.set('limit', String(s.limit));
      const data = await ctx.api('network-monitors?' + params, { signal: controller.signal });
      if (controller !== pending.list || !ctx.visible()) return;

      s.rows = data.items;
      s.total = data.total;
      ctx.rememberTargets(s.rows);
      applyLiveReadings();
      // A deletion may have emptied the current page.
      if (s.page > 1 && (s.page - 1) * s.limit >= s.total) {
        s.page = Math.max(1, Math.ceil(s.total / s.limit));
        return refreshList();
      }
      ui.message.textContent = '';
      renderList();
      ctx.renderHero();
    } catch (e) {
      if (e.name !== 'AbortError' && controller === pending.list) ctx.showError(ui.message, e.message, refreshList);
    }
  }

  return { renderList, applyLiveReadings, refreshList };
}
