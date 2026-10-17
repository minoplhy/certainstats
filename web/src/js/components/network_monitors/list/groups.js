import { el } from '../shared/format.js';

// The target group filter on the Network view.
export function createGroups(ctx) {
  const { nodeMode, ui, pending } = ctx;
  const s = ctx.state;

  function renderGroupOptions() {
    const selected = ui.group.value;
    const all = el('option', '', 'All groups');
    all.value = '';
    ui.group.replaceChildren(all);
    const options = new Set(s.groupTargets);
    if (selected) options.add(selected);
    for (const target of [...options].sort()) {
      const option = el('option', '', target);
      option.value = target;
      ui.group.append(option);
    }
    ui.group.value = selected;
  }

  async function loadGroups() {
    if (nodeMode || !ctx.visible()) return;
    const controller = ctx.begin('groups');
    const params = ctx.filterParams();
    params.delete('target');
    params.delete('target_exact');
    try {
      const data = await ctx.api('network-monitors/targets?' + params, { signal: controller.signal });
      if (controller !== pending.groups || !ctx.visible()) return;
      s.groupTargets = data.items;
      renderGroupOptions();
      ui['group-message'].textContent = '';
    } catch (e) {
      if (e.name !== 'AbortError' && controller === pending.groups && ctx.visible()) {
        ctx.showError(ui['group-message'], e.message, loadGroups);
      }
    }
  }

  return { renderGroupOptions, loadGroups };
}
