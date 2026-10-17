import { PROTOCOLS } from '../shared/constants.js';

// Opening the editor for a new monitor, one monitor or a target group.
export function createDialog(ctx) {
  const { nodeMode, ui, formProtocolOptions, intervalPresets, form, dialog } = ctx;
  const s = ctx.state;

  function setFormMessage(text) {
    ui['form-message'].textContent = text;
    ui['form-message'].hidden = !text;
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
    ctx.renderAgentOptions();
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
    s.editingGroup = null;
    s.inheritedGroup = null;
    s.editorGeneration++;
    s.inheritancePending = false;
    s.editing = monitor || null;
    ui['group-state'].hidden = true;
    ui['enabled-pill'].hidden = false;
    lockInheritedSettings(false);
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

  function lockInheritedSettings(locked) {
    for (const name of ['port', 'dns_server', 'interval_seconds']) form.elements[name].disabled = locked;
    for (const option of formProtocolOptions) option.disabled = locked;
    for (const preset of intervalPresets) preset.disabled = locked;
  }

  async function openGroupDialog(target) {
    const generation = ++s.editorGeneration;
    try {
      const group = await ctx.api('network-monitors/group?target=' + encodeURIComponent(target));
      await ctx.loadAgents();
      if (generation !== s.editorGeneration || !ctx.visible()) return;
      openDialog(group.items[0]);
      s.editingGroup = group;
      ui['form-title'].textContent = 'Edit target group';
      ui['form-subtitle'].textContent = 'Shared settings apply to all ' + group.items.length + ' probers across every page. Identity changes start new history; old history stays archived.';
      ui['agent-actions'].hidden = false;
      ui['agent-hint'].textContent = 'Select probers to add. Uncheck probers to archive their checks and preserve their history.';
      ui['group-state'].hidden = false;
      ui['group-enabled'].value = '';
      ui['enabled-pill'].hidden = true;
      // Seed the membership selection before rendering the complete agent list.
      s.editing = null;
      ctx.renderAgentOptions();
      for (const input of ctx.agentCheckboxes()) input.checked = group.items.some(m => m.agent_id === input.value);
      ctx.renderAgentOptions();
      ctx.updateAgentSummary();
    } catch (e) {
      if (generation === s.editorGeneration) ctx.showError(ui.message, e.message, () => openGroupDialog(target));
    }
  }

  async function inheritTargetSettings() {
    if (s.editingGroup || s.editing || !dialog.open) return;
    let target = form.elements.target.value.trim();
    if (form.elements.protocol.value === 'http' && !target.includes('://') && target) target = 'https://' + target;
    target = target.includes('://') ? target.replace(/^(https?:\/\/)([^/?#]+)/i, (_, scheme, host) => scheme.toLowerCase() + host.toLowerCase()) : target.toLowerCase();
    const generation = ++s.editorGeneration;
    s.inheritedGroup = null;
    s.inheritancePending = false;
    lockInheritedSettings(false);
    if (!target) return;
    s.inheritancePending = true;
    try {
      // The target list determines existence without treating other failures as a missing group.
      const targets = await ctx.api('network-monitors/targets');
      if (generation !== s.editorGeneration || !dialog.open) return;
      if (targets.items.includes(target)) {
        const group = await ctx.api('network-monitors/group?target=' + encodeURIComponent(target));
        if (generation !== s.editorGeneration || !dialog.open) return;
        s.inheritedGroup = group;
        const monitor = group.items[0];
        form.elements.port.value = monitor.port || 443;
        form.elements.dns_server.value = monitor.dns_server || '';
        form.elements.interval_seconds.value = monitor.interval_seconds;
        setFormProtocol(monitor.protocol);
        syncIntervalPresets();
        lockInheritedSettings(true);
        ui['form-subtitle'].textContent = 'This target already has a group. New probers inherit its shared configuration. Use Edit group to change it.';
      } else {
        ui['form-subtitle'].textContent = 'Each selected agent probes the target from its own network.';
        ctx.renderAgentOptions();
      }
      setFormMessage('');
    } catch (e) {
      if (generation === s.editorGeneration) setFormMessage(e.message + ' Retry by leaving the target field again.');
    } finally {
      if (generation === s.editorGeneration) s.inheritancePending = false;
    }
  }

  async function openAddDialog() {
    const generation = ++s.editorGeneration;
    try {
      await ctx.loadAgents();
      if (generation !== s.editorGeneration || !ctx.visible()) return;
      openDialog();
    } catch (e) {
      ui.message.textContent = e.message;
    }
  }

  return { setFormMessage, setFormProtocol, syncIntervalPresets, syncEnabledPill, openDialog, lockInheritedSettings, openGroupDialog, inheritTargetSettings, openAddDialog };
}
