import { state, PUB_METADATA_SYNC_INTERVAL_MS } from './state.js';
import { loadPublicDetailMetrics } from './detail/load.js';
import { getPublicBasePath } from './nav/dom.js';
import { applyPublicTelemetryUpdates, renderPubInpageLiveState } from './detail/live.js';
import { setPublicViewMode } from './nav/routing.js';
import { syncPublicDashboardMetadata } from './sync.js';
import { initTimeRangeBar } from './time/range_bar.js';

export function init(options) {
  options = options || {};
  state.dashId = options.dashId || '';
  state.dashSlug = options.dashSlug || '';
  state.maxDays = options.maxDays ?? 0;
  state.pubAllowedMetrics = options.allowedMetrics || [];
  state.pubAgentsData = options.agents || [];

  // Initial calculation of cluster stats from static agent data
  window.CertainStatsTelemetry.renderClusterStats('pub-', state.pubAgentsData, state.pubLiveMetricsStore);

  const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
  const wsUrl = (publicPath ? publicPath : '') + '/api/public/ws/' + encodeURIComponent(state.dashId);

  // 1. Handshake WebSocket immediately without waiting for DOMContentLoaded (0ms delay)
  window.CertainStatsTelemetry.initWebSocket(wsUrl, applyPublicTelemetryUpdates);

  // 2. Setup DOM-dependent UI on DOM Ready
  window.CertainStatsTelemetry.onReady(function() {
    let viewMode = 'list';
    try { viewMode = localStorage.getItem('certainstats_public_view_mode') || 'list'; } catch (e) {}
    setPublicViewMode(viewMode);

    // In-Place Public Router (BASE_PATH/{PUB_AGENT_ID})
    window.CertainStatsTelemetry.initRouter({
      basePath: getPublicBasePath(),
      onNavigate: function(pubId) {
        const overviewView = document.getElementById('public-overview-view');
        const detailView = document.getElementById('public-detail-view');

        if (!pubId || !state.pubAgentsData.some(agent => agent.public_id === pubId)) {
          if (detailView) detailView.hidden = true;
          if (overviewView) overviewView.hidden = false;
          const previousId = state.currentActivePubId;
          state.currentActivePubId = null;
          const returnTarget=Array.from(document.querySelectorAll('[data-pub-id]')).find(node=>node.getAttribute('data-pub-id')===previousId && node.getClientRects().length);
          returnTarget?.querySelector('a,button')?.focus({preventScroll:true});
 window.CertainStatsRequests?.cancelAll();
          window.scrollTo({ top: state.savedPubScrollY, behavior: 'instant' });
          return;
        }

        state.savedPubScrollY = window.scrollY;
        state.currentActivePubId = pubId;
        if (overviewView) overviewView.hidden = true;
        if (detailView) {
          detailView.hidden = false;
 const heading=detailView.querySelector("h1,h2");if (heading) {heading.setAttribute("tabindex","-1");heading.focus({preventScroll:true});}
          window.scrollTo({ top: 0, behavior: 'instant' });

          renderPubInpageLiveState(pubId);

          initTimeRangeBar('pub-inpage-time-range-bar', {
            maxDays: state.maxDays,
            isPublic:true,
            onSelect: function(h) {
              loadPublicDetailMetrics(pubId, h);
            }
          });
          let initPubHours = 6;
          try { initPubHours = parseInt(localStorage.getItem('certainstats_public_active_hours') || '6', 10); } catch (e) {}
          loadPublicDetailMetrics(pubId, isNaN(initPubHours) || initPubHours <= 0 ? 6 : initPubHours);
        }
      }
    });
  });

  const metadataTimer=setInterval(syncPublicDashboardMetadata, PUB_METADATA_SYNC_INTERVAL_MS);window.addEventListener("pagehide",()=>clearInterval(metadataTimer),{once:true});
}
