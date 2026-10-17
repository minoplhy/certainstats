import { el, button } from './format.js';

// Requests, filters and visibility shared by every part.
export function createCore(ctx) {
  const { root, panelPath, nodeMode, parent, ui, targets, sparklines, pending } = ctx;
  const s = ctx.state;

  function visible() {
    if (s.destroyed || !s.active || document.hidden || root.closest('[hidden]')) return false;
    return !parent || (parent.open && !!s.nodeId);
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
    if (!response.ok) {
      const error = new Error(body?.error || body?.message || 'Request failed');
      error.status = response.status;
      throw error;
    }
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
    return nodeMode ? s.nodeId : ui.node.value;
  }

  function hasFilters() {
    return !!(ui.search.value.trim() || s.protocol || ui.state.value || (!nodeMode && (ui.node.value || ui.group.value)));
  }

  function filterParams() {
    const params = new URLSearchParams({
      state: ui.state.value,
      target: ui.search.value.trim(),
      protocol: s.protocol
    });
    if (agentFilter()) params.set('agent_id', agentFilter());
    if (!nodeMode && ui.group.value) params.set('target_exact', ui.group.value);
    return params;
  }

  function rememberTargets(monitors) {
    for (const monitor of monitors) targets.set(monitor.monitor_id, monitor.target);
  }

  return { visible, api, abortRequests, begin, showError, agentFilter, hasFilters, filterParams, rememberTargets };
}
