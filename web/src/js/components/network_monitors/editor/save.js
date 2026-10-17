import { button } from '../shared/format.js';

// Saving the editor and row actions.
export function createSave(ctx) {
  const { ui, form, dialog, hiddenSeries, collapsedGroups } = ctx;
  const s = ctx.state;

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
    if (!s.editing || s.editingGroup) data.agent_ids = ctx.agentCheckboxes().filter(input => input.checked).map(input => input.value);
    if (s.inheritancePending) { ctx.setFormMessage('Wait for the target settings to load.'); return; }
    if (s.editingGroup) {
      data.original_target = s.editingGroup.items[0].target;
      data.revision = s.editingGroup.revision;
      delete data.enabled;
      if (ui['group-enabled'].value) data.enabled = ui['group-enabled'].value === 'true';
    }
    const problem = formProblem(data);
    if (problem) {
      ctx.setFormMessage(problem);
      return;
    }

    ui.submit.disabled = true;
    try {
      const path = s.editingGroup ? 'network-monitors/group' : s.editing ? 'network-monitors/' + encodeURIComponent(s.editing.monitor_id) : 'network-monitors';
      const saved = await ctx.api(path, { method: s.editingGroup || s.editing ? 'PATCH' : 'POST', body: JSON.stringify(data) });
      if (s.editingGroup) {
        const oldTarget = s.editingGroup.items[0].target;
        const newTarget = saved.items[0].target;
        const wasSelected = ui.group.value === oldTarget;
        if (collapsedGroups.delete(oldTarget)) collapsedGroups.add(newTarget);
        if (newTarget !== oldTarget) {
          s.groupTargets = s.groupTargets.filter(target => target !== oldTarget);
          s.groupTargets.push(newTarget);
          // Install the renamed option before restoring the selection.
          ctx.renderGroupOptions();
          if (wasSelected) ui.group.value = newTarget;
        }
        hiddenSeries.clear();
        s.historyData = null;
      }
      dialog.close();
      s.page = 1;
      ctx.refreshAll();
    } catch (e) {
      ctx.setFormMessage(e.message);
      if (s.editingGroup && e.status === 409) {
        ui['form-message'].append(button('Reload group', () => { dialog.close(); ctx.openGroupDialog(s.editingGroup.items[0].target); }, 'btn btn-secondary btn-sm'));
      }
    } finally {
      ui.submit.disabled = false;
    }
  }

  async function mutate(monitor, method, body) {
    try {
      await ctx.api('network-monitors/' + encodeURIComponent(monitor.monitor_id), {
        method,
        body: body ? JSON.stringify(body) : undefined
      });
      ctx.refreshAll();
    } catch (e) {
      ui.message.textContent = e.message;
    }
  }

  return { formProblem, saveDialog, mutate };
}
