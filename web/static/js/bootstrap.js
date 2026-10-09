(function () {
  'use strict';
  const node = document.getElementById('page-config');
  if (!node) return;
  const data = JSON.parse(node.textContent);
  const snake = value => {
    if (Array.isArray(value)) return value.map(snake);
    if (!value || typeof value !== 'object') return value;
    const result = {};
    for (const [key, item] of Object.entries(value)) {
      const name = key.replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2').replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase();
      result[name] = snake(item);
    }
    return result;
  };
  const module = node.dataset.module;
  if (module === 'CertainStatsAdminAgents') window[module].init({agents: snake(data.Agents || [])});
  else if (module === 'CertainStatsPublicDashboard') {
    const agents = snake(data.Agents || []).map(agent => Object.assign(agent, agent.net || {}));
    window[module].init({dashId: data.Dashboard.DashboardID, dashSlug: data.Dashboard.Slug, maxDays: data.AccessRules.max_days, allowedMetrics: data.AccessRules.allowed_metrics || [], agents});
  } else if (module === 'CertainStatsDashboardEdit') {
    const order = (data.DashboardAgents || []).map(agent => (agent.AgentID || agent.agent_id));
    window[module].init({selectedAgentsOrder: order, agents_order: order, isDragged: !!data.IsDragged || (data.DashboardAgents || []).some(agent => (agent.SortKey || agent.sort_key)), isCreate: !!data.IsCreate, dashboardId: data.Dashboard.DashboardID});
  } else if (module === 'CertainStatsAdminModals') window[module].initSessions();
  else if (module === 'setup') {
    const pw = document.getElementById('password'), confirm = document.getElementById('confirm_password');
    if (pw && confirm) for (const input of [pw, confirm]) input.addEventListener('input', () => confirm.setCustomValidity(confirm.value && confirm.value !== pw.value ? 'Passwords do not match.' : ''));
  } else if (['CertainStatsAgentManagement', 'CertainStatsAdminAlerts'].includes(module)) window[module].init();
})();
