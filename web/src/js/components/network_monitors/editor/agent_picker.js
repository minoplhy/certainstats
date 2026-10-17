import { el, supports } from '../shared/format.js';

// Agent checkboxes in the editor dialog.
export function createAgentPicker(ctx) {
  const { nodeMode, ui, form } = ctx;
  const s = ctx.state;

  async function loadAgents(signal) {
    s.agents = await ctx.api('agents', { signal });
    const selected = ui.node.value;
    const all = el('option', '', 'All nodes');
    all.value = '';
    ui.node.replaceChildren(all);
    for (const agent of s.agents) {
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
    ui['agent-count'].textContent = selected + ' selected';
    if (s.editingGroup) {
      const checked = new Set(agentCheckboxes().filter(input => input.checked).map(input => input.value));
      const existing = new Set(s.editingGroup.items.map(m => m.agent_id));
      const added = [...checked].filter(id => !existing.has(id)).length;
      const removed = [...existing].filter(id => !checked.has(id)).length;
      ui['agent-hint'].textContent = (existing.size - removed) + ' retained, ' + added + ' added, ' + removed + ' removed. Removed checks are archived with their history. New probers start active when keeping individual states.';
    }
    ui.submit.textContent = (s.editingGroup || s.editing) ? 'Save changes' : selected > 1 ? 'Add to ' + selected + ' agents' : 'Add monitor';
  }

  function renderAgentOptions() {
    const selectedProtocol = form.elements.protocol.value;
    const customServer = form.elements.dns_server.value.trim();
    const group = ui['agent-options'];
    const checked = new Set(agentCheckboxes().filter(input => input.checked).map(input => input.value));
    group.replaceChildren();

    for (const agent of s.agents) {
      if (nodeMode && !s.editingGroup && agent.agent_id !== s.nodeId) continue;
      if (s.editing && !s.editingGroup && agent.agent_id !== s.editing.agent_id) continue;
      const capable = supports(agent, 'configure') && supports(agent, selectedProtocol) &&
        (!customServer || supports(agent, 'custom_dns_server'));
      const runtime = agent.extensions?.runtime;
      const reason = capable ? '' : (runtime?.capabilities?.['network_monitor.configure']?.message || 'Can\'t run ' + selectedProtocol.toUpperCase() + ' probes');

      const checkbox = el('input');
      checkbox.type = 'checkbox';
      checkbox.name = 'agent_ids';
      checkbox.value = agent.agent_id;
      const existing = s.editingGroup?.items.some(m => m.agent_id === agent.agent_id);
      const duplicate = s.inheritedGroup?.items.some(m => m.agent_id === agent.agent_id);
      checkbox.disabled = duplicate || (!capable && !existing) || (!!s.editing && !s.editingGroup);
      checkbox.checked = !duplicate && ((existing && checked.has(agent.agent_id)) || (capable && ((s.editing && !s.editingGroup) || (!s.editingGroup && nodeMode) || checked.has(agent.agent_id))));

      const pill = el('label', 'node-select-pill' + (checkbox.checked ? ' active' : '') + (checkbox.disabled ? ' is-disabled' : ''));
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

  return { loadAgents, agentCheckboxes, updateAgentSummary, renderAgentOptions, selectAllAgents };
}
