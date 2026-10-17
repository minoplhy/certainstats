import { state } from '../state.js';
import { resetInpageEditStates } from '../editing/rename.js';

// In-place navigation between the hub, the Network view and agent details.

export function handleAgentItemClick(event, agentId) {
  if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0) {
    return; // Allow standard new tab opening
  }
  event.preventDefault();
  navigateToAgent(agentId);
}

export function navigateToAgent(agentId) {
  resetInpageEditStates();
  const targetUrl = (state.panelPath ? state.panelPath : '') + '/' + agentId;
  if (window.location.pathname !== targetUrl) {
    history.pushState({ agentId: agentId }, '', targetUrl);
  }
  window.dispatchEvent(new PopStateEvent('popstate'));
}

export function closeAgentDetail() {
  resetInpageEditStates();
  const targetUrl = state.panelPath ? (state.panelPath + '/') : '/';
  if (window.location.pathname !== targetUrl) {
    history.pushState({}, '', targetUrl);
  }
  window.dispatchEvent(new PopStateEvent('popstate'));
}

// Moves focus to a view's heading after an SPA route change, for screen readers.
export function focusHeading(container, selector) {
  const heading = container?.querySelector(selector);
  if (!heading) return;
  heading.setAttribute('tabindex', '-1');
  heading.focus({ preventScroll: true });
}

export function markCurrentNavLink(pathname) {
  document.querySelectorAll('.nav-link, .subnav-link').forEach(link => {
    const target = new URL(link.href, window.location.href).pathname.replace(/\/+$/, '');
    if (target === pathname) link.setAttribute('aria-current', 'page');
    else link.removeAttribute('aria-current');
  });
}

// Same-origin links to the hub, an agent or the Network view switch views in place.
export function handleSpaLinkClick(event) {
  if (event.defaultPrevented || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  const link = event.target.closest('a[href]');
  if (!link || link.target || link.hasAttribute('download')) return;
  const url = new URL(link.href, window.location.href);
  if (url.origin !== window.location.origin || url.search || url.hash) return;
  if (state.panelPath && !(url.pathname === state.panelPath || url.pathname.startsWith(state.panelPath + '/'))) return;
  const relative = url.pathname.slice(state.panelPath.length).replace(/^\/+|\/+$/g, '');
  if (relative !== '' && relative !== 'network-monitors' && !state.agentsData.some(a => a.agent_id === relative)) return;
  event.preventDefault();
  if (url.pathname !== window.location.pathname) history.pushState({}, '', url.pathname);
  window.dispatchEvent(new PopStateEvent('popstate'));
}
