import { state, routeScroll, ADMIN_METADATA_SYNC_INTERVAL_MS } from './state.js';
import { loadDetailMetrics, renderInpageLiveState } from './detail/load.js';
import { loadSparklines, syncAdminAgentsMetadata } from './hub/hub.js';
import { applyTelemetryUpdates } from './detail/live.js';
import { saveInpageHeaderInlineNote, cancelInpageHeaderNoteEdit } from './editing/notes.js';
import { resetInpageEditStates, saveInpageAgentName, cancelInpageAgentNameEdit } from './editing/rename.js';
import { handleSpaLinkClick, markCurrentNavLink, focusHeading } from './nav/routing.js';
import { setAgentViewMode } from './hub/view.js';

export function init(options) {
  options = options || {};
  state.panelPath = options.panelPath || window.CertainStatsTelemetry.getPanelPath();
  state.agentsData = options.agents || [];

  // Initial calculation of cluster stats from static agent data
  window.CertainStatsTelemetry.renderClusterStats('admin-', state.agentsData, state.liveMetricsStore);

  // 1. Initiate WebSocket handshake immediately without waiting for DOMContentLoaded (0ms delay)
  window.CertainStatsTelemetry.initWebSocket(state.panelPath + '/api/ws', applyTelemetryUpdates);

  // 2. Setup DOM-dependent UI on DOM Ready
  window.CertainStatsTelemetry.onReady(function() {
    let viewMode = 'grid';
    try {
      viewMode = localStorage.getItem('certainstats_view_mode') || 'grid';
    } catch (e) {}
    setAgentViewMode(viewMode);

    const networkRoot = document.querySelector('#agents-detail-view [data-network-monitors]');
    if (networkRoot) state.nodeNetwork = window.CertainStatsNetworkMonitors.mount(networkRoot, { panelPath: state.panelPath, nodeMode: true });
    const historyRoot = document.querySelector('[data-incident-history]');
    if (historyRoot) state.nodeHistory = window.CertainStatsIncidentHistory.mount(historyRoot, { panelPath: state.panelPath, nodeMode: true });

    const networkView = document.getElementById('network-spa-view');
    const agentsView = document.getElementById('agents-spa-view');
    const networkPageRoot = networkView?.querySelector('[data-network-monitors]');
    if (networkPageRoot) state.networkPage = window.CertainStatsNetworkMonitors.mount(networkPageRoot, { panelPath: state.panelPath, active: false, hero: networkView.querySelector('[data-network-hero]') });
    document.addEventListener('click', handleSpaLinkClick);

    document.addEventListener('visibilitychange', () => {
      if (document.hidden) {
        window.CertainStatsRequests.cancel('agents-detail-view');
        window.CertainStatsRequests.cancel('agents-overview-view');
      } else if (state.activeRoute === 'overview') {
        loadSparklines();
      } else if (state.activeRoute !== 'network' && state.currentActiveAgentId && state.inpageTimePicker) {
        const pickerState = state.inpageTimePicker.getState();
        loadDetailMetrics(state.currentActiveAgentId, pickerState.hours, pickerState.customRange);
      }
    });

    // In-Place SPA Router (BASE_PATH/{AGENT_ID})
    window.CertainStatsTelemetry.initRouter({
      basePath: state.panelPath,
      // Panel routes that share the path space with agent IDs.
      reservedSegments: ['agents', 'dashboards', 'alerts', 'settings', 'management', 'login', 'logout', 'dashboard'],
      onNavigate: function(agentId) {
        const overviewView = document.getElementById('agents-overview-view');
        const detailView = document.getElementById('agents-detail-view');

        const route = agentId === 'network-monitors' ? 'network' : agentId && state.agentsData.some(a => a.agent_id === agentId) ? agentId : 'overview';
        const previous = state.activeRoute;
        if (previous) routeScroll.set(previous, window.scrollY);
        state.activeRoute = route;
        const isNetwork = route === 'network';
        if (agentsView) agentsView.hidden = isNetwork;
        if (networkView) networkView.hidden = !isNetwork;
        document.title = (isNetwork ? 'Network Monitors' : route === 'overview' ? 'Agent Hub' : 'Agent Details') + ' — CertainStats';
        markCurrentNavLink(isNetwork ? state.panelPath + '/network-monitors' : state.panelPath);
        if (previous !== route) {
          resetInpageEditStates();
          window.CertainStatsRequests.cancel('agents-detail-view');
          window.CertainStatsRequests.cancel('agents-overview-view');
          state.nodeNetwork?.deactivate();
          state.nodeHistory?.deactivate();
          agentsView?.querySelectorAll('dialog[open]').forEach(dialog => dialog.close());
        }
        if (isNetwork) {
          state.networkPage?.activate();
          focusHeading(networkView, 'h1');
          window.scrollTo({ top: routeScroll.get(route) || 0, behavior: 'instant' });
          return;
        }
        state.networkPage?.deactivate();

        if (!agentId || !state.agentsData.some(agent => agent.agent_id === agentId)) {
          if (detailView) detailView.hidden = true;
          if (overviewView) overviewView.hidden = false;
          loadSparklines();
          focusHeading(overviewView, 'h1');
          state.currentActiveAgentId = null;
          window.scrollTo({ top: routeScroll.get('overview') ?? state.savedScrollY, behavior: 'instant' });
          return;
        }

        state.savedScrollY = window.scrollY;
        const changedAgent = state.detailAgentId !== agentId;
        state.detailAgentId = agentId;
        state.currentActiveAgentId = agentId;
        if (changedAgent && state.nodeNetwork) state.nodeNetwork.setNode(agentId);
        if (changedAgent && state.nodeHistory) state.nodeHistory.setNode(agentId);
        if (overviewView) overviewView.hidden = true;
        if (detailView) {
          detailView.hidden = false;
          focusHeading(detailView, 'h1,h2');
          window.scrollTo({ top: routeScroll.get(route) || 0, behavior: 'instant' });

          renderInpageLiveState(agentId);
          
          let activeHours = 6;
          try { activeHours = parseInt(localStorage.getItem('certainstats_active_hours') || '6', 10); } catch (e) {}
          if (!state.inpageTimePicker) {
            state.inpageTimePicker = window.CertainStatsTelemetry.initCustomTimePicker('inpage-detail-time-picker-container', {
              activeHours,
              onApply: function(opts) {
                state.inpageCustomRange = opts.customRange;
                if (state.currentActiveAgentId) loadDetailMetrics(state.currentActiveAgentId, opts.hours, opts.customRange);
              }
            });
          }
          const selected = state.inpageTimePicker.getState();
          loadDetailMetrics(agentId, selected.hours, selected.customRange);
          state.nodeNetwork?.activate();
          state.nodeHistory?.activate();
        }
      }
    });

    // Inpage Header Notes Enter/Escape Key Handler
    const inpageHeaderInput = document.getElementById('inpage-header-notes-input');
    if (inpageHeaderInput) {
      inpageHeaderInput.addEventListener('keydown', function(e) {
        if (e.key === 'Enter') saveInpageHeaderInlineNote();
        if (e.key === 'Escape') cancelInpageHeaderNoteEdit();
      });
    }

    // Inpage Header Agent Name Enter/Escape Key Handler
    const inpageNameInput = document.getElementById('detail-name-input');
    if (inpageNameInput) {
      inpageNameInput.addEventListener('keydown', function(e) {
        if (e.key === 'Enter') saveInpageAgentName();
        if (e.key === 'Escape') cancelInpageAgentNameEdit();
      });
    }
  });

  const metadataTimer = setInterval(() => {
    if (state.activeRoute !== 'network' && !document.hidden) syncAdminAgentsMetadata();
  }, ADMIN_METADATA_SYNC_INTERVAL_MS);
  window.addEventListener('pagehide', () => {
    clearInterval(metadataTimer);
    state.inpageTimePicker?.destroy();
    [state.inpageCpuChart, state.inpageRamChart, state.inpageNetChart].forEach(chart => chart?.destroy());
    Object.values(state.inpageDiskCharts).forEach(charts => {
      charts.usageChart?.destroy();
      charts.ioChart?.destroy();
    });
  }, { once: true });
}
